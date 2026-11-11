# Escrow, refunds and payment exceptions: contract for Members 3, 4 and 5 (v2)

Supersedes the first version. Schema: `backend/migrations/financial_integrity.sql`.
Go contract: `backend/internal/escrow` (types, errors, `Settler` interface, `SplitRefund`).
Money is NUMERIC(12,2) in the database and `money.Kobo` (int64) in Go. No floats.

If you applied the first version in a dev database, drop its three tables first
(nothing writes to them yet): `DROP TABLE escrow_events, escrow_holds, payment_exceptions CASCADE;`
`CREATE TABLE IF NOT EXISTS` will not add the new columns to the old tables.

## Who owns what

| Area | Owner |
|---|---|
| Wallet balances, held balances, escrow holds and events, ledger writes, refund settlement, `payment_exceptions` writes from wallet code, this migration | Member 4 |
| Disputes, eligibility, quality evidence, admin refund decisions, the `refunds` table and statuses | Member 5 |
| Cart, checkout, order creation, buyer confirmation, calling `ReleaseEscrow` | Member 3 |

## Ledger types added: `sale_reversal` (seller), `order_refund` (buyer)

## wallets.held_balance

`balance` is what the farmer sees; `held_balance` is the part in escrow; spendable is
`balance - held_balance`. A CHECK (`0 <= held_balance <= balance`) makes it impossible for any
statement to take a balance below its held funds. Withdrawals and transfers must test
`balance - held_balance >= amount`.

## escrow_holds (one row per order, UNIQUE order_id)

| Column | Meaning |
|---|---|
| gross_amount | what the buyer paid |
| commission_amount | the platform's share, snapshotted at payment so later rate changes never affect a refund |
| amount | the seller's net in escrow; CHECK `gross = amount + commission` |
| released_amount | paid out to the seller on buyer confirmation |
| refunded_amount | taken back from the seller's held funds |
| commission_reversed | the platform's share of refunds so far |
| refunded_gross | what buyers have been refunded; CHECK `= refunded_amount + commission_reversed` |
| status | `held`, `disputed` (frozen), `closed` |
| closed_reason | `released`, `refunded`, `split` (required when closed) |
| release_after | automatic release deadline; NULL until product sets one. A job must skip `disputed` holds |

Database rules: `released + refunded <= amount`; `commission_reversed <= commission`; `refunded_gross <= gross`;
a closed hold has nothing left; an open hold always has something left.
Invariant: per wallet, `held_balance = SUM(amount - released_amount - refunded_amount)` over `held` and `disputed` holds.

## escrow_events (append-only; UPDATE and DELETE are rejected)

`escrow_hold_id, event (held|released|refunded|disputed|dispute_cleared), amount, actor_id, reference, detail, created_at`.
UNIQUE `(escrow_hold_id, event, reference)` makes each step idempotent.
References: `refund:<refunds.id>`, `release:<order id>`, `<disputeRef>` for freezes.

## platform_revenue_reversals (append-only)

`order_id, refund_id, amount > 0, reference UNIQUE ('refund:<refunds.id>')`. `platform_revenue` rows stay positive;
net platform revenue for an order is its `platform_revenue.amount` minus its reversals.

## payment_exceptions

`kind`: `duplicate_payment | deposit_reversal_unfunded | refund_unfunded | held_deposit | amount_mismatch | unknown_reference | other`.
`status`: `open | investigating | resolved | dismissed` (resolved and dismissed rows need `resolved_at`).
Fields: `reference` (required), `provider_transaction_id, wallet_id, order_id, refund_id, amount, currency, detail,
resolution_note, resolved_by`. Deduplicated on `(kind, reference, provider_transaction_id)`: insert with
`ON CONFLICT DO NOTHING`. `refund_id` has no foreign key; the `refunds` table is Member 5's.

## Go interface (`internal/escrow`)

```go
type Settler interface {
    ReleaseEscrow(ctx, orderID int64, actorID int) error
    FreezeEscrow(ctx, orderID int64, actorID int, disputeRef string) error
    UnfreezeEscrow(ctx, orderID int64, actorID int, disputeRef string) error
    RefundOrder(ctx, RefundRequest) (*RefundResult, error)
}
type RefundRequest struct { RefundID, OrderID int64; Amount money.Kobo /* gross */; ActorID int }
type RefundResult  struct { BuyerCredit, SellerReversal, CommissionReversal money.Kobo; AlreadyApplied bool }
```

Errors: `ErrRefundRequiresReview` (exception written, no balance changed: set the refund to `requires_review`),
`ErrRefundExceedsPaid`, `ErrEscrowNotFound`, `ErrEscrowDisputed`, `ErrEscrowClosed`.
`FreezeEscrow` and `UnfreezeEscrow` take a `disputeRef` (an addition to your sketch) because it is the idempotency key.

## Commission on refunds

Pass only the gross amount. The wallet derives both portions from the order's recorded payment, so there is one
rule in one place. `escrow.SplitRefund(paid, commission, alreadyRefunded, refund)` is the function it uses, and you
can call it to show the buyer or admin the split before approving.

- N20,000 paid, N1,000 commission, refund N2,000: seller N1,900, commission N100, buyer N2,000.
- Full N20,000 refund: seller N19,000, commission N1,000.
- It computes cumulative totals (`round_half_up(refunded * commission / paid)`), so any number of partial refunds
  adds up to exactly the recorded commission, never a kobo more or less. Tested over 20,000 random orders.
- The buyer always receives exactly the approved gross amount.

## RefundOrder, step by step (one transaction)

1. Lock the hold and both wallets in ascending id order. Look up the refund by `refund:<RefundID>`: if already applied, return the original result.
2. Reject if the hold is `closed` (already released or refunded): write a `refund_unfunded` exception, return `ErrRefundRequiresReview`.
3. `SplitRefund`. If the seller portion exceeds what the hold still contains, write the exception, change nothing, return `ErrRefundRequiresReview`.
4. Debit the seller (`sale_reversal`), reduce `held_balance`, credit the buyer (`order_refund`), update the hold's totals, write the commission reversal and an `escrow_event`.
5. Exempt from tier limits and the balance cap; still bounded by the column ceiling.