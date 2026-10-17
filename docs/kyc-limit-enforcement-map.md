# Wallet limit enforcement: verified write map and concurrency design

**Status:** design only. No code written. No limits chosen — every number remains **UNCONFIRMED**.
**Date:** 2026-10-16

This document exists because an earlier draft listed **five** financial writes as the places limits
would need to attach. That list was **wrong**: it named user-facing entry points, not balance
mutations. The verified count is **nine**. The four that were missed are the refund/reversal legs —
precisely where limit accounting goes wrong.

---

## 1. Every write to a wallet balance

Produced by grepping `UPDATE wallets SET balance` across the whole backend. There are **nine**, all
in `internal/repository/wallet_repository.go`. **No other file in the repository mutates a balance** —
not a handler, not a service, not a route, not `ussd/`.

| # | Line | Method | Direction | Ledger `type` | Ledger `reference` | Trigger |
|---|---|---|---|---|---|---|
| 1 | 205 | `CompleteDeposit` | **+** credit | `deposit` | `deposit:<reference>` | Verified funding |
| 2 | 315 | `CreateWithdrawal` | **−** reserve | `withdrawal_reserved` | `withdrawal:<reference>` | User requests payout |
| 3 | 417 | `PayForOrder` | **−** debit | `order_payment` | `order-payment:<orderID>` | Buyer pays from wallet |
| 4 | 457 | `PayForOrder` | **+** credit | `sale_credit` | `sale-credit:<orderID>` | Seller receives net of commission |
| 5 | 670 | `RejectWithdrawal` | **+** refund | `withdrawal_refund` | `withdrawal-refund:<withdrawalID>` | Admin rejects payout |
| 6 | 734 | `CompleteTransfer` | **+** refund | `withdrawal_refund` | `transfer-refund:<withdrawalRef>` | Provider reports FAILED/REVERSED |
| 7 | 848 | `CreateTransfer` | **−** reserve | `transfer_out` | `transfer:<reference>` | User requests wallet-to-wallet |
| 8 | 944 | `ApproveTransfer` | **+** credit | `transfer_in` | `transfer-in:<transferRef>` | Admin approves transfer |
| 9 | 1005 | `RejectTransfer` | **+** refund | `transfer_refund` | `transfer-refund:<transferRef>` | Admin rejects transfer |

Plus one non-balance write: `platform_revenue` (line 474). Commission deliberately does **not** land
in any wallet — it is a separate table so platform money can never be spent as a user balance. Keep
it that way.

### Findings from the verification

**F1 — Three entry points, not one.** `PayForOrder` is reachable from three separate handlers:

- `handlers/payment_handler.go` → `POST /orders/pay` (routes/Routes.go:938)
- `handlers/order.go:120` (`OrderHandler.Place`, routes/Routes.go:738)
- `handlers/market.go:61` (marketplace page form)

**A limit enforced in any one handler is bypassable through the other two.** Enforcement must sit
inside the repository transaction, below all three.

**F2 — `CompleteTransfer` is misnamed.** It handles **Flutterwave payout webhooks for withdrawals**,
not wallet-to-wallet transfers. Anyone reading the limits design must not assume it is the transfer
path.

**F3 — The ledger prefix `transfer-refund:` is used for two different things.** #6 (a failed bank
payout) and #9 (a rejected wallet-to-wallet transfer) share the prefix. They are distinguishable only
because the *suffix* generators differ (`WITHDRAW-<id>-<hex>` vs `TRANSFER-<id>-<hex>`,
`wallet_service.go:322,438,549`). It works today, but the prefix is misleading.

**F4 — The ledger `type` alone cannot attribute a refund.** #5 and #6 both write
`withdrawal_refund`. Nothing in `wallet_transactions` distinguishes "an admin rejected this" from
"the provider reported it failed". Attribution requires joining back to `wallet_withdrawals`.
For limit accounting, reconciliation, and dispute handling this matters, and it is a reason to
record the *reason* alongside the ledger row rather than infer it.

**F5 — Credit legs can breach a future maximum balance silently.** #4 (`sale_credit`) and #8
(`transfer_in`) credit a third party who is not the caller. There is no maximum-balance check today.
If a credit pushes `balance` past `NUMERIC(12,2)`, PostgreSQL raises `22003 numeric field overflow`,
which rolls the transaction back and surfaces as a 500 — safe, but opaque. A maximum-balance rule
must therefore be checked **before** the `UPDATE`, so the user gets a clear rejection.

**F6 — Order payments have no reversal path.** #3/#4 commit irreversibly. There is no refund, no
dispute, no reversal for a marketplace sale anywhere in the codebase.

**F7 — Three declared deposit states are never written.** `processing`, `expired`, and `reversed`
exist in the `wallet_deposits` CHECK constraint and in `models/wallet.go:34-38`, but no code sets
them. Only `pending`, `successful`, and `failed` occur. A stale-deposit sweeper would want `expired`;
a provider reversal would want `reversed`.

**F8 — The payout webhook still routes on a reference prefix.**
`wallet_service.go:756` checks `strings.HasPrefix(payload.Data.Ref, "WITHDRAW-")`. This is the same
reference-shape discriminator that was removed from the deposit path in the previous stage (where
routing now reads the recorded provider). It is inconsistent, and it is the fragile pattern.

**F9 — `CompleteTransfer`'s FAILED/REVERSED branch refunds without the standard guard shape.**
The `RowsAffected()==1` check at line 726 happens inside the branch, and the outer `result` is
re-checked at 749. It is correct, but the refund at 734 runs before that outer check — worth a test
that a FAILED webhook with a mismatched amount cannot refund.

---

## 2. Where limits must attach

Enforce **inside** the transaction of each of the nine writes, with a helper that returns the
applicable policy row and the running totals. Never in a handler, never in the template, never in
JavaScript.

| Operation | Repository method | Limit dimensions that apply |
|---|---|---|
| Fund | `CompleteDeposit` (#1) | Per-receipt cap; daily receiving total; max balance |
| Withdraw | `CreateWithdrawal` (#2) | Per-transaction cap; daily outgoing total; balance |
| Pay for order | `PayForOrder` (#3) | Per-transaction cap; daily outgoing total; balance |
| Transfer | `CreateTransfer` (#7) | Per-transaction cap; daily outgoing total; balance |
| Credit legs | #4, #8 | Max balance only — these are not user-initiated |
| Refund legs | #5, #6, #9 | Max balance; and whether a refund restores the day's allowance |

**The refund question is the policy question.** #5, #6, #9 return money to a wallet. If a withdrawal
is rejected the same day, does that day's outgoing total go back down? Gross and net give different
answers and different compliance exposure. **This is UNCONFIRMED and must be answered by the
regulatory source, not chosen by us.**

---

## 3. Concurrency design

### 3.1 The isolation level in play

Every write uses `db.BeginTx(ctx, nil)` → PostgreSQL's default, **READ COMMITTED**. Under READ
COMMITTED:

- `UPDATE` and `SELECT … FOR UPDATE` take real row locks that serialize correctly.
- A plain `SELECT SUM(…)` takes **no** lock. Two concurrent transactions can each sum the day's
  ledger, each conclude they are under the limit, and each insert — the classic write skew.

**Therefore a naive "sum the day, then insert" check is unsafe**, even though the balance checks
around it are correct. This must not be repeated.

### 3.2 The fix, and why it is cheap here

Every one of the nine writes already touches the user's `wallets` row. Taking
`SELECT … FROM wallets WHERE id = $1 FOR UPDATE` as the **first** statement of the transaction
therefore serializes all of a single wallet's financial operations for free, and makes any
subsequent sum accurate within that transaction.

Order of operations inside each transaction becomes:

1. Lock the wallet row(s) `FOR UPDATE`.
2. Read the user's verified tier and the effective policy rows.
3. Sum the relevant ledger totals for the window, from `wallet_transactions`.
4. Check per-transaction, daily, and balance rules.
5. Apply the balance change, write the ledger row, record the limit decision.
6. Commit.

**Multi-wallet operations must lock in a deterministic order.** `PayForOrder` touches the buyer and
the seller; `ApproveTransfer` and `RejectTransfer` touch sender and recipient. Locking in ascending
`wallet id` order is required to remove the deadlock risk that today's update order
(buyer-then-seller) does not fully avoid.

### 3.3 Why totals come from the ledger, not the balance

The brief forbids deriving daily totals from the current balance, and the code agrees: a user can
receive and spend on the same day. The authoritative sources are:

- **Outgoing:** `withdrawal_reserved` + `order_payment` + `transfer_out` rows in
  `wallet_transactions` for the window.
- **Incoming:** `deposit` + `sale_credit` + `transfer_in` rows for the window.
- **Refunds:** `withdrawal_refund` + `transfer_refund` rows, applied per whichever convention the
  regulator confirms (§2).

### 3.4 Pending operations

The existing design is already reservation-based and this is a strength to preserve:

- A withdrawal **debits at request time** (`CreateWithdrawal`, #2) and stays `pending` until an
  admin approves `BeginWithdrawal` → `processing`. So the money is already out of the balance while
  pending, and the ledger already records the reservation.
- A transfer likewise debits the sender at request (#7) and only credits the recipient on approval
  (#8).

Consequently **pending outgoing operations are already counted** in any ledger-derived daily total —
no separate pending figure is needed for outgoing limits, provided the window is measured from
`withdrawal_reserved` / `transfer_out` and not from settlement.

**Pending incoming is different.** A deposit is `pending` until a verified callback arrives; it has
no ledger row yet, because no balance has moved. Whether pending deposits count toward a daily
*receiving* limit is **UNCONFIRMED** — it depends on the regulator's definition of the window.
If it must count, the sum would need to include `wallet_deposits` rows in `pending`/`processing`,
which is a straightforward addition but must not be guessed.

### 3.5 Duplicate payment callbacks

**What exists:** `webhook_events` with `UNIQUE (transaction_id, event)`. The check is
`SELECT processed_at IS NOT NULL …` followed by `INSERT … ON CONFLICT DO NOTHING`
(`payment_repository.go:93-117`). This is **check-then-act**: two concurrent first deliveries can
both see "not seen", both proceed, and the `ON CONFLICT DO NOTHING` does not tell either caller
whether *it* was the one that inserted.

**So `webhook_events` is dedup bookkeeping, not a serialization primitive.** It is worth keeping for
audit, but no correctness guarantee may rest on it.

**What actually protects the money today:**

- `CompleteDeposit` — `SELECT … FOR UPDATE` on `wallet_deposits`, plus the `status='pending'` guard
  in the `UPDATE`, plus `RowsAffected()==1`, plus the unique ledger reference. A replay blocks on the
  lock, then sees `successful` with a matching transaction id and returns nil.
- `CompleteTransfer` — `FOR UPDATE` plus a `processing` status guard and `RowsAffected()==1`.
- `CreateWithdrawal` / `CreateTransfer` — `WHERE balance >= amount` and `RowsAffected()==1`, so two
  concurrent requests cannot both spend the same money.

This is correct and should not be changed.

**The risk to avoid:** if limit accounting is implemented as a mutable counter (a `daily_totals` row
incremented on each operation), it would inherit exactly the check-then-act flaw that the webhook
table has. **Derive totals from the immutable ledger inside the locked transaction instead.** That
is idempotent by construction: a duplicate callback that finds the deposit already `successful`
writes no new ledger row, so it cannot double-count.

### 3.6 Timezone and daily boundaries

`wallet_transactions.created_at` is `TIMESTAMP` **without** time zone, defaulted by
`CURRENT_TIMESTAMP` and written by Go's local `time.Now()`. What is actually stored therefore depends
on the connection's session timezone.

Two design options, both requiring a decision before any daily limit is built:

- **Store `timestamptz`** and convert explicitly to `Africa/Lagos` when computing windows. Cleanest,
  but touches existing columns.
- **Pin the session timezone** and keep `timestamp`. Smaller blast radius, but the correctness of
  every window then rests on a connection setting.

Whichever is chosen, the window boundary and the reset timezone must come from the policy row's
`source_reference`, not from a constant in Go. Nigeria is WAT (UTC+1) with no DST, but that is a
fact to confirm with the regulatory source rather than assume.

### 3.7 Policy changes mid-flight

Because policy rows carry `effective_from`, a policy change must not retroactively alter a decision
already committed. The design should record, on each limit decision, **which policy row id** produced
it — so a historical rejection remains explicable after the policy table changes.

---

## 4. What is still needed before this can be implemented

1. Confirmed limits and windows, per tier and per direction — see
   [kyc-monnify-questions.md](kyc-monnify-questions.md) and
   [kyc-counsel-questions.md](kyc-counsel-questions.md).
2. A ruling on the refund convention (§2) and on whether pending incoming deposits count (§3.4).
3. A decision on the timezone approach (§3.6).
4. Approval of the lock-ordering change to `PayForOrder`, `ApproveTransfer`, and `RejectTransfer`.
