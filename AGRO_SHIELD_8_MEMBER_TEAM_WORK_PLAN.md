# AGRO-SHIELD — 8-MEMBER ENGINEERING REMEDIATION PLAN

**Based on:** October 30, 2026 audit package, commit `7b8c4ff`  
**Scope:** 20 findings (`AGRO-001`–`AGRO-020`) from `MASTER_ISSUE_REGISTER.md`  
**Purpose:** Assign one accountable owner to each finding, coordinate implementation and verification, and prepare a controlled release.  
**Status:** Proposed assignments; findings must be confirmed against the current code before fixes are merged.

> **Important:** This is a division of reported audit findings, not a claim that each vulnerability has been independently reproduced. The audit reports differ on the count of skipped DB tests (23 vs. 28); Member 8 must reconcile this by executing the tests. The source reports identify some items as future-plan gaps; confirm release scope before implementing them.

## 1. Team assignment at a glance

| Member | Workstream | Owned issue IDs | Highest priority | Branch |
|---|---|---|---|---|
| 1 | Security, privacy and browser hardening | AGRO-001, AGRO-002, AGRO-020 | P0 | `fix/security-privacy` |
| 2 | Authentication, sessions and account recovery | AGRO-003, AGRO-006, AGRO-016 | P1 | `fix/auth-sessions` |
| 3 | Marketplace cart and external checkout | AGRO-004, AGRO-005 | P0 | `fix/marketplace-checkout` |
| 4 | Financial integrity, KYC and deposit lifecycle | AGRO-012, AGRO-015, AGRO-017 | P1 | `fix/wallet-kyc` |
| 5 | Order disputes, refunds and reversals | AGRO-010 | P1 | `feat/order-disputes-refunds` |
| 6 | Administration and accountability | AGRO-008, AGRO-019 | P1 | `feat/admin-governance` |
| 7 | Agricultural intelligence and post-harvest tools | AGRO-007, AGRO-011, AGRO-018 | P2 | `feat/agri-intelligence` |
| 8 | Database quality, performance and test infrastructure | AGRO-009, AGRO-013, AGRO-014 | P1 | `fix/db-tests-performance` |

**Coverage check:** 3 + 3 + 2 + 3 + 1 + 2 + 3 + 3 = **20 unique issues**. Every issue has exactly one primary owner. Workload is **risk-based, not numerically equal**: Member 5 owns a complex financial workflow, while Member 8 owns shared verification infrastructure.

## 2. Individual assignments

### MEMBER 1 — Security, privacy and browser hardening
**Role:** Application Security Engineer  
**Issues:** `AGRO-001` (P0), `AGRO-002` (P0), `AGRO-020` (P3)  
**Primary areas:** `backend/agro-shield.db`, Git history, `.gitignore`, `backend/middleware/auth.go`, security-header middleware, frontend external assets.

**Problems:** A tracked SQLite file reportedly contains personal information and bcrypt hashes; session cookies omit the `Secure` attribute; the Content Security Policy and externally loaded assets need hardening.

**Tasks:**
1. Confirm the sensitive file is tracked and assess exposure without printing personal data.
2. Prepare a coordinated repository-history cleanup plan; notify the team before any history rewrite or force-push. Preserve forensic evidence securely as appropriate; assess notification and credential-rotation obligations.
3. Add appropriate ignore rules and preventive secret/PII scanning.
4. Ensure authentication cookies are `Secure` in production behind trusted HTTPS proxies; preserve `HttpOnly`, appropriate `SameSite`, expiry, and logout behavior.
5. Tighten CSP incrementally, auditing scripts/styles for compatibility and removing unnecessary remote dependencies.

**Deliverables:** Privacy incident remediation record, clean repository tracking/history verification, secure cookie tests, CSP regression evidence.

**Acceptance:** No sensitive DB blob remains in approved reachable Git history; production cookies are never sent over HTTP; no required frontend behavior is broken by CSP.

**Dependencies:** Coordinate cookie changes with Member 2 and history changes with all members. **Never force-push without team agreement.**

### MEMBER 2 — Authentication, sessions and account recovery
**Role:** Backend Authentication Engineer  
**Issues:** `AGRO-003` (P1), `AGRO-006` (P1), `AGRO-016` (P2)  
**Primary areas:** `backend/middleware/auth.go`, `backend/handlers/forgot_password.go`, profile phone-change handlers, OTP service, session storage.

**Problems:** Sessions are process-local without a server-side TTL; password-reset responses reveal account existence; phone-number changes need verification of the new number.

**Tasks:**
1. Implement a persistent, expiring session store (PostgreSQL or Redis) with server-side expiry, logout revocation, cleanup, and multi-instance support.
2. Standardize password-reset responses and comparable response behavior without weakening OTP abuse controls.
3. Stage a new phone number until a valid OTP challenge is completed; preserve the old verified number until confirmation.
4. Test expired/revoked sessions, concurrent logins, restarts, password-reset enumeration, OTP replay, and failed phone changes.

**Deliverables:** Session storage/migration changes, authentication tests, recovery/phone-change workflow tests, concise design notes.

**Acceptance:** Session expiration is enforced server-side; restart does not unexpectedly invalidate durable sessions; reset responses do not disclose account existence; phone changes cannot bypass verification.

**Dependencies:** Member 1 owns cookie flags; Member 8 coordinates DB migration conventions.

### MEMBER 3 — Marketplace cart and external checkout
**Role:** Full-Stack Marketplace Engineer  
**Issues:** `AGRO-004` (P0), `AGRO-005` (P1)  
**Primary areas:** `backend/handlers/cart.go`, `frontend/pages/cart.html`, `frontend/pages/marketplace.html`, order/payment handlers and services.

**Problems:** Accepted negotiated items cannot be checked out from the cart; external Flutterwave checkout exists in the backend but is not reachable from the user interface.

**Tasks:**
1. Implement cart checkout for accepted offers with server-side verification of buyer ownership, price, quantity, stock, offer validity, and totals.
2. Reuse existing order/payment services rather than duplicating wallet mutation logic.
3. Provide wallet and supported Flutterwave checkout paths, with clear pending/success/failure states.
4. Ensure checkout tokens and payment references are idempotent; never trust browser redirects as proof of payment.
5. Test duplicate clicks, insufficient funds, expired negotiations, stock races, gateway failures, and unauthorized cart access.

**Deliverables:** Working cart-to-order journey, payment choice UI, safe order creation, automated tests, manual browser walkthrough.

**Acceptance:** A buyer can purchase an accepted negotiated item exactly once; both supported payment paths reach a correct final order state without duplicate charges.

**Dependencies:** Member 4 owns wallet policy changes; Member 5 owns post-purchase disputes. Coordinate shared order/payment files before editing.

### MEMBER 4 — Financial integrity, KYC and deposit lifecycle
**Role:** Financial Backend Engineer  
**Issues:** `AGRO-012` (P1), `AGRO-015` (P2), `AGRO-017` (P3)  
**Primary areas:** Wallet services/repositories, KYC policy checks, deposit state transitions, related database constraints.

**Problems:** Documented KYC tiers and transaction limits are not fully enforced; wallet credit paths need upper-bound validation; declared deposit states are not all transitioned by the application.

**Tasks:**
1. Trace documented KYC requirements and define a single enforceable policy for deposits, withdrawals, transfers, and purchases where required.
2. Apply policy checks server-side at every relevant financial entry point, including USSD where applicable.
3. Add checked balance arithmetic and appropriate maximum-balance safeguards without introducing floating-point money calculations.
4. Implement safe pending-deposit expiration/reconciliation handling; distinguish provider-confirmed reversals from local timeouts.
5. Verify transaction atomicity, duplicate event handling, ledger consistency, and concurrency against isolated PostgreSQL.

**Deliverables:** KYC policy matrix, enforcement code/tests, safe balance checks, deposit lifecycle tests, financial invariant report.

**Acceptance:** No unsupported KYC tier can exceed defined limits; balances cannot overflow; deposit transitions are auditable and idempotent.

**Dependencies:** Member 8 provides PostgreSQL test environment; coordinate with Members 3 and 5 before changing shared wallet code.

### MEMBER 5 — Order disputes, refunds and reversals
**Role:** Payments and Order Lifecycle Engineer  
**Issue:** `AGRO-010` (P1)  
**Primary areas:** Orders, payments, wallet ledger, refund/dispute services, buyer/seller UI, admin review integration.

**Problem:** The audit reports no complete cancellation, dispute, and refund mechanism for marketplace orders.

**Tasks:**
1. Define eligible cancellation/refund/dispute states, deadlines, evidence requirements, and authorized actors based on product requirements; flag policy questions for product-owner approval.
2. Implement buyer dispute submission, seller response where required, and administrator review.
3. Build safe partial/full refund and reversal paths consistent with the existing wallet ledger and gateway settlement status.
4. Prevent duplicate refunds, over-refunds, unauthorized reversals, and negative balances; retain an immutable audit trail.
5. Add end-to-end tests for approvals, rejections, partial refunds, retries, and payment-provider failures.

**Deliverables:** Approved state machine, refund/dispute APIs and UI, ledger reconciliation tests, operational runbook.

**Acceptance:** Every permitted refund is authorized, traceable, idempotent, and financially balanced; invalid refund attempts fail safely.

**Dependencies:** Member 3 establishes completed-order flows; Member 4 owns wallet policy; Member 6 supplies admin review/audit infrastructure. Do not process real refunds during testing.

### MEMBER 6 — Administration and accountability
**Role:** Admin Platform Engineer  
**Issues:** `AGRO-008` (P1), `AGRO-019` (P1)  
**Primary areas:** Admin routes, wallet admin handlers, admin UI, audit log storage, user and listing repositories.

**Problems:** Sensitive admin actions lack a persistent audit trail; the existing wallet-focused administration UI lacks general user management and listing moderation.

**Tasks:**
1. Define explicit admin roles/permissions and apply server-side authorization to every new admin endpoint.
2. Add tamper-resistant, append-only audit records for approvals, rejections, role changes, suspensions, and moderation decisions.
3. Implement user search, review, suspension/reactivation, and listing moderation within approved product scope.
4. Build admin screens with clear permissions and confirmation steps for sensitive actions.
5. Test privilege escalation, unauthorized direct API access, audit completeness, and sensitive-data minimization.

**Deliverables:** Admin RBAC matrix, management endpoints/UI, audit log schema, authorization tests, admin workflow guide.

**Acceptance:** Ordinary users cannot invoke admin operations; sensitive admin actions leave attributable persistent records; moderators can perform required tasks without direct database access.

**Dependencies:** Member 2 for authentication; Member 5 for dispute review; Member 8 for schema migration coordination.

### MEMBER 7 — Agricultural intelligence and post-harvest tools
**Role:** Agricultural Features Engineer  
**Issues:** `AGRO-007` (P2), `AGRO-011` (P2), `AGRO-018` (P3)  
**Primary areas:** `frontend/pages/dashboard.html`, storage handlers/services, weather integration, `backend/ussd/`.

**Problems:** Dashboard weather card is commented out; storage is inventory CRUD rather than the specified post-harvest protection feature; USSD weather is not exposed.

**Tasks:**
1. Restore and verify the weather dashboard using the project's intended provider and location handling; display graceful fallbacks.
2. Define and implement required storage-facility records, condition monitoring, safe-duration rules, and spoilage alerts; distinguish manual readings from sensor data.
3. Add relevant validation, notification behavior, and clear farmer-facing storage status UI.
4. Expose localized weather information through USSD with concise messages and sensible provider-failure behavior.
5. Test location accuracy, missing readings, alert thresholds, offline/provider failure, and mobile/USSD usability.

**Deliverables:** Functional weather dashboard, scoped smart-storage module, USSD weather option, user-flow tests and feature documentation.

**Acceptance:** Farmers can view weather; storage monitoring meets the approved release requirements; alerts are explainable; USSD weather responds safely.

**Dependencies:** Product owner must approve hardware/sensor scope before implementation; Member 8 coordinates new tables.

### MEMBER 8 — Database quality, performance and test infrastructure
**Role:** Database / QA / DevOps Engineer  
**Issues:** `AGRO-009` (P1), `AGRO-013` (P2), `AGRO-014` (P2)  
**Primary areas:** CI configuration, `backend/internal/repository/*_test.go`, database migrations, timestamp columns, chat/notification template rendering.

**Problems:** Database integration tests were skipped; timestamps are not consistently timezone-aware; some templates are parsed repeatedly on demand.

**Tasks:**
1. Provision isolated PostgreSQL for CI and local integration tests; set `TEST_DATABASE_URL` securely and fail CI when required tests skip unexpectedly.
2. Reconcile the audit's inconsistent skipped-test counts by recording actual test names, executed/skipped totals, and outcomes.
3. Test wallet concurrency, idempotency, rollback, migrations, and ownership protections against real PostgreSQL.
4. Plan and apply safe `TIMESTAMPTZ` migration with explicit assumptions for existing data, backups, and rollback procedures.
5. Cache or preparse chat/notification templates safely and verify rendered output remains unchanged.
6. Provide shared test/migration conventions for the other seven members.

**Deliverables:** Reproducible CI database tests, test evidence, timestamp migration and rollback plan, template benchmark/regression tests.

**Acceptance:** Required DB tests run (not silently skip) in CI; migrations preserve existing timestamp meaning; template behavior is unchanged and repeated parsing is reduced.

**Dependencies:** Start this workstream immediately because Members 3–6 depend on its integration-test infrastructure.

## 3. Coordination rules and shared-file ownership

1. **One primary owner per issue.** A second engineer may review but must not independently implement the same change.
2. **Shared code changes need coordination.** Member 1 owns cookie flags; Member 2 owns session persistence. Member 4 owns wallet balance/KYC policies; Members 3 and 5 call agreed financial interfaces rather than introducing parallel ledger writers.
3. **Migrations:** Member 8 reviews and sequences schema changes from Members 2, 4, 5, 6, and 7. Do not run incompatible startup migrations concurrently.
4. **Admin/disputes:** Member 6 owns admin permissions and audit records; Member 5 owns refund business rules and supplies admin review actions through agreed interfaces.
5. **Source-of-truth policy:** `MASTER_ISSUE_REGISTER.md` controls issue IDs; `REMEDIATION_ROADMAP.md` provides proposed priorities. Recheck current code and product requirements before implementation.
6. **Sensitive data:** Never paste credentials, real user data, payment keys, or complete DB records into tickets, commits, or CI logs.

## 4. Suggested execution sequence

| Stage | When | Primary work | Gate |
|---|---|---|---|
| A — Immediate | Days 1–3 | Member 1 handles exposure/cookies; Member 3 starts cart; Member 8 establishes DB test environment | P0 exposure contained; reproducible tests available |
| B — Core stabilization | Week 1–2 | Members 2, 4, 6 build session, wallet policy, admin foundations; Member 3 completes checkout | Security and checkout tests pass |
| C — Financial workflow | Week 2–3 | Member 5 implements dispute/refund flow; Member 3 external checkout; Member 4 deposit lifecycle | End-to-end ledger and gateway sandbox tests pass |
| D — Product completion | Week 3–5 | Member 7 builds agricultural features; Member 8 timestamp/performance changes; remaining hardening | Functional acceptance tests pass |
| E — Integration/release | After dependencies merge | All eight members run cross-functional regression, security review, and deployment rehearsal | Release owner signs off; no unresolved P0/P1 blockers |

**Schedule is illustrative, not a guarantee.** Complex refund policies, sensor integrations, and production data migrations may require additional time.

## 5. Standard delivery checklist for every member

- [ ] Confirm assigned findings against current code and requirements; record false positives.
- [ ] Create a feature branch from the agreed baseline; avoid unrelated refactoring.
- [ ] Write or update tests before marking a task complete.
- [ ] Run `go test ./...`, `go vet ./...`, and relevant integration tests; document skips.
- [ ] Check authorization, input validation, failure paths, concurrency, and privacy where relevant.
- [ ] Document migrations, environment changes, rollback plan, and API contract changes.
- [ ] Open a pull request linking the exact AGRO issue IDs with evidence and screenshots where useful.
- [ ] Obtain at least one independent code review; financial/security changes require specialist review.
- [ ] Merge only after CI and integration checks pass.

## 6. Definition of done for the whole team

- Every `AGRO-001` through `AGRO-020` finding is marked **fixed**, **verified false positive**, or **explicitly deferred with release approval**.
- No unresolved critical or high-severity release blocker remains.
- User data and secrets are not exposed in repository history or logs.
- Registration, login, onboarding, marketplace, cart checkout, wallet payments, refunds, admin review, and farmer tools have documented end-to-end results appropriate to release scope.
- PostgreSQL-backed financial tests execute successfully in CI, including concurrency and rollback cases.
- New admin/financial actions have enforceable permissions and audit records.
- Production deployment, backup/restore, observability, and incident-response readiness are independently reviewed.
- A final audit is performed against the **merged release commit**, not only individual branches.

## 7. Source documents

- `MASTER_ISSUE_REGISTER.md` — canonical AGRO-001 through AGRO-020 issue descriptions.
- `REMEDIATION_ROADMAP.md` — proposed phases and priorities.
- `SECURITY_AUDIT.md`, `FINANCIAL_SYSTEM_AUDIT.md`, `BACKEND_API_AUDIT.md`, `DATABASE_AUDIT.md`, `FRONTEND_UI_AUDIT.md` — domain findings.
- `FEATURE_COMPLETENESS.md`, `PRODUCTION_READINESS.md`, `TEST_RESULTS.md`, `EXECUTIVE_SUMMARY.md` — scope and verification limitations.

**Project owner action:** Replace “Member 1” through “Member 8” with actual assignees, approve product-policy decisions, and confirm the release scope before work starts.
