# KYC & wallet-limits: audit and provisional design

**Status:** audit complete, design provisional, **implementation not started**
**Date:** 2026-10-16
**Scope:** NIN/BVN verification, age eligibility, KYC tiers, wallet limits
**Rule for this document:** every regulatory number is **UNCONFIRMED**. Nothing here has been
verified against a primary source. See [kyc-monnify-questions.md](kyc-monnify-questions.md) and
[kyc-counsel-questions.md](kyc-counsel-questions.md) for what is outstanding.

---

## 1. What already exists and works

**Do not rewrite any of this.**

| Area | Implementation | Notes |
|---|---|---|
| One wallet per user, atomic with registration | `FarmerRepository.Create` (`internal/repository/farmer_repository.go:36-79`) runs `INSERT INTO farmers … RETURNING id` then `INSERT INTO wallets` in one transaction | `wallets.farmer_id` is `UNIQUE`, which is what makes duplicates impossible. `RunMigration` backfills older accounts (`internal/database/database.go:449-451`) |
| Zero balance, NGN | `balance NUMERIC(12,2) NOT NULL DEFAULT 0 CHECK (balance >= 0)`, `currency VARCHAR(3) NOT NULL DEFAULT 'NGN'` | Confirmed intact |
| Idempotent deposit crediting | `CompleteDeposit` (`wallet_repository.go:158`) | `SELECT … FOR UPDATE` + `status='pending'` guard + `RowsAffected()==1` + unique ledger reference |
| Server-side payment verification | Both the browser callback and the webhook re-verify with the provider; `verif-hash` compared with `crypto/subtle` | Browser success screens are never trusted |
| Reserve-first payouts | `CreateWithdrawal` (`wallet_repository.go:291`) | `WHERE balance >= amount` inside the transaction, then OTP, then admin approval |
| Immutable ledger | `wallet_transactions`, append-only, `reference` UNIQUE, `type` CHECK-constrained | Balance change and ledger row commit together |
| Integer money arithmetic | kobo throughout (`splitCommission`, `koboToAmount`) | No floats in financial decisions |
| Disciplined logging | user IDs only; never phone, name, amount, or secret | OTP codes hashed at rest |

**Everything below is additive. No existing table, column, balance, or history needs to change.**

---

## 2. Gaps

| # | Gap | Consequence |
|---|---|---|
| G1 | No identity verification of any kind. Zero hits for `bvn`, `nin`, `kyc`, `biometric`, `date_of_birth`, `dob` across `*.go`, `*.html`, `*.js` | Nothing to build tiers on |
| G2 | **No phone-verification concept.** `otp_codes.purpose` allows only `withdrawal`, `transfer`, `password_reset` (`database.go:746`) | Phone ownership is never proven at registration — only incidentally, later, by requesting a payout |
| G3 | **No date-of-birth field anywhere** | Age eligibility is unenforceable today |
| G4 | No account or wallet status column | A wallet cannot be suspended or restricted |
| G5 | No limit policy, and no tier to key one against | — |
| G6 | No DB-backed admin audit trail; admin actions are `log.Printf` only | No actor/reason/prior-state record |
| G7 | No encryption capability beyond bcrypt | Raw NIN/BVN storage would be new infrastructure, not a small change |
| G8 | No CSRF token; no `Secure` cookie flag; sessions never expire server-side | Defence-in-depth gaps |
| G9 | No rate limiting outside OTP issuance | Money and (future) verification endpoints unthrottled |
| G10 | `created_at` is `TIMESTAMP` **without time zone**, `DEFAULT CURRENT_TIMESTAMP`, written by Go's local `time.Now()` | What is stored depends on the connection's session timezone. **Daily windows cannot be built on this as-is** |
| G11 | `backend/agro-shield.db` — a tracked SQLite file containing a real person's name, phone, and bcrypt hash | NDPA exposure; see §6 |
| G12 | `wallet_integration_test.go` has never been executed | No DB-backed verification of any financial guarantee |
| G13 | Declared-but-unwritten deposit states: `processing`, `expired`, `reversed` exist in the schema and in `models/wallet.go:34-38` but **no code ever sets them** | The schema promises lifecycle states that do not exist; a stale-deposit sweeper and a reversal handler would need them |
| G14 | **No reversal path for marketplace orders.** A completed `PayForOrder` cannot be undone | Phase 6 requires refunds/reversals "where applicable" — for order payments, none exists |

### Correction to an earlier statement

An earlier draft of this audit said "`role` is never read for authorization". **That was wrong.**
`IsBuyer()` is checked server-side in the checkout path and shapes the UI elsewhere:
`handlers/payment_handler.go:47`, `handlers/order.go:107`, `handlers/market.go:55`,
`handlers/cart.go:91`, `handlers/complete_profile.go:67`, `handlers/delivery.go:106`,
`services/payment_service.go:87`, plus template shaping in `dashboard.go`, `order.go`,
`delivery.go`, `market.go`, `profile.go`, `sales.go`, `storage.go`.

The accurate statement is:

- `role` **is** enforced as a farmer/buyer capability gate on buyer-only actions.
- There are only **two** roles. No admin, agent, or business/company role exists.
- Administrative authority is **not** role-based at all — it is a comma-separated list of user IDs
  in `WALLET_ADMIN_USER_IDS` (`WalletService.IsAdmin`, `wallet_service.go:143`). Any account,
  of either role, is a wallet admin if its id is in that variable.
- **A company's CAC registration is not represented in the data model at all.** There is no
  organisation, business, or corporate entity — every account is an individual.

---

## 3. Provisional design (not approved, not implemented)

Only the parts that cannot be invalidated by what the regulations turn out to say.

### 3.1 Tables (all additive)

- **`identity_verifications`** — one row per attempt, modelled on `otp_codes`, which is the
  existing "attempt + audit" table that already works: user id, document type, provider,
  provider reference, outcome, timestamps, **masked identifier only**. Raw NIN/BVN is not stored;
  the provider's verification reference is.
- **`kyc_policies`** — tier → requirement and limit rows, each carrying `source_reference`,
  `effective_from`, direction, and window. **No limit is ever a literal in Go.** This is the
  mechanism that makes "do not hardcode unverified limits" structurally true rather than a promise.
- **`account_restrictions`** — suspension, manual review, policy restriction, with actor and reason.
- **`admin_audit_log`** — actor, timestamp, reason, prior state, new state.

### 3.2 Seven separate statuses

Kept deliberately independent, per the brief. One must never satisfy another:

1. Phone verification — *new*
2. NIN verification — *new*
3. BVN verification — *new*
4. Age eligibility — *new*, and defaults to **`unconfirmed`, not `adult`**
5. KYC tier — *new*, derived server-side from 1–4 plus the effective policy row
6. Wallet status — *new* (active / restricted / suspended)
7. Payment status — *existing*, unchanged

### 3.3 Verification state machine

`not_started → pending → verified | failed | manual_review`, plus `revalidation_required`.
Kept strictly separate from payment status. A successful payment never marks identity verified,
and a verified identity never marks a payment successful.

### 3.4 Age derivation

Derived on the backend from the **provider's** verified date of birth only. Never from a frontend
date, never from an adulthood checkbox, and never inferred from NIN possession, BVN possession,
phone verification, CAC registration, or payment success. If the provider returns no DOB, age
stays `unconfirmed` — **not** adult. Date arithmetic must handle leap years and the birthday
boundary correctly (a 29-Feb birthday in a non-leap year).

### 3.5 Enforcement placement

Limits are enforced **inside the repository transaction**, never in a handler. This is not a
stylistic preference — see [kyc-limit-enforcement-map.md](kyc-limit-enforcement-map.md) §2, where
the same money-moving operation turns out to be reachable from three different handlers.

---

## 4. Files this would touch

**New:** `internal/models/identity.go`, `internal/models/kyc.go`,
`internal/repository/identity_repository.go`, `internal/repository/kyc_policy_repository.go`,
`internal/services/identity_service.go`, `internal/services/limit_service.go`,
`internal/services/identity/<provider>.go`, `handlers/kyc.go`,
`frontend/pages/verify-identity.html`, and tests.

**Modified:** `internal/database/database.go` (additive only), `internal/config/config.go`,
`internal/appcontainer.go`, `main.go`, `routes/Routes.go`,
`internal/services/wallet_service.go` (call the limit check),
`internal/repository/wallet_repository.go` (lock ordering only), `handlers/wallet.go`,
`frontend/pages/wallet.html`, `.env.example`.

**Must not be touched:** the `FarmerRepository.Create` wallet-creation path, `CompleteDeposit`,
`CreateWithdrawal`, `flutterwave.go`, `payment_service.go`'s verification logic.

---

## 5. Risks

1. **Migrations auto-apply on restart.** `RunMigration` is called at `main.go:37`, so anything
   merged reaches production the next time the process starts. Additive only.
2. **The timezone problem (G10) must be settled before any daily limit exists.** A daily window
   computed from a `TIMESTAMP` written in the connection's session timezone resets at an
   unpredictable hour, and no test would catch it.
3. **Tier derivation is the highest-risk logic in the system.** It is exactly where "NIN submitted"
   could silently become "adult".
4. **A KYC gate added carelessly locks out existing users with real balances.** That needs an
   explicit migration policy, not an accident.
5. **`internal/app/app.go` is still an unreferenced duplicate container** (a second `package app`
   that nothing imports). Awaiting a decision; it is a trap for anyone wiring up a new service.

---

## 6. `backend/agro-shield.db`

Investigated separately in §6 of the accompanying report. Summary: tracked by git, referenced by
no code, no SQLite driver in `go.mod`, contains at least one real user record. No action proposed
without approval.
