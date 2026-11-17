# Role 6 integration and team handoff

Updated November 17, 2026 for the local working tree on `feat/admin-governance`,
based on `ce4f884`. The [eight-member plan](../AGRO_SHIELD_8_MEMBER_TEAM_WORK_PLAN.md)
assigns AGRO-008/019 to Member 6. Member numbers are used because the plan does not
identify individual assignees.

## Implemented agreement between roles

| Owner | Shared contract now implemented | Remaining owner work |
| --- | --- | --- |
| Member 2 — authentication | Live persistent hashed sessions, server expiry, logout/revocation, fresh permissions and authentication revision checks; OTP-verified password reset changes the password and revokes sessions atomically. Recovery issuance and unsuccessful verification use generic responses. | Independent authentication review and a production recovery delivery configuration. Development SMS logging does not deliver messages. |
| Member 5 — disputes | Buyer complaint/withdrawal, seller response, private evidence, admin queue/detail, versioned review/rejection/approval/retry, and transactional governance authorization/audit. | Review the reason list and quality-video requirement with product; approve dispute eligibility/deadline policy. |
| Members 3/4/5 — payments | New wallet purchases create escrow holds; all wallet spending uses available balance; buyer receipt confirmation releases the hold; approved refunds return money to the buyer wallet and reverse seller/platform portions atomically. | Direct-provider and historical payments need their own verified funding/settlement integration. They are never backfilled into escrow or refunded from unrelated spendable funds. Agree any automatic release deadline and original-payment-method refund policy. |
| Member 8 — database/QA | Ordered additive escrow migration, preserving refund-table upgrade, immutable financial history and the union of required integration tests. | Independent migration/deployment review, shared evidence storage/backups, historical timestamp interpretation, and hosted CI on the final combined commit. |

## Daily workflow

1. Open **Orders → View → Report an issue**. A complaint requires a confirmed
   matching NGN payment and an amount within the paid order total.
2. **My disputes** lists both purchases and sales. The seller can respond.
   Both parties can upload private JPEG/PNG/PDF/MP4 evidence, up to 20 MB each
   and 10 files per case. Photos are decoded, MP4 containers must include a
   video track, and PDFs require header/end markers. File names and caller MIME
   claims do not establish validation; reviewers still assess the actual content.
3. Open **Admin console → Disputes & refunds**. Review the order, complaint,
   response and evidence. Confirm before starting review, rejecting, approving
   a refund, or retrying settlement. Quality refunds require buyer video evidence
   validated by the upload path; historical MIME-only evidence does not qualify.
4. Approval records the exact amount and administrator. Settlement credits the
   buyer wallet, reverses the seller's held share and the original commission
   share, and resolves the dispute only after all records commit.
5. If funding cannot be verified, the approval remains available and an open
   **Payment exceptions** record links to the case. Retry the approved record
   after investigating; retries cannot create another refund or pay twice.
6. A buyer can withdraw an unreviewed complaint. When no active dispute remains,
   **Order → Confirm receipt** releases the remaining proceeds to the seller.

## Authorization, revisions and audit

`DisputeReviewAccess` uses `disputes.review`, not wallet approval access.
Ordinary accounts, moderators and wallet-only staff cannot adjudicate cases.
An administrator who bought or sold the order cannot review, reject, approve
or settle it. Private evidence is available only to the parties and current
reviewers; a revoked role loses access immediately.

Administrative writes authorize inside their PostgreSQL transaction under the
same governance lock used for role changes. Interactive actions require the
version shown on the review page and an explicit confirmation. Seller responses,
evidence uploads, approvals and settlement results advance the case version,
so new information invalidates stale decisions. Order locks consistently precede
related dispute/refund locks where both are needed. Receipt confirmation cannot
race past a dispute freeze.

Dispute/settlement events mirror into `admin_audit_events` in the same transaction.
Audit failure rolls back the sensitive change. Complaint text, evidence paths
and free-form notes stay in the private case instead of the general audit.
Dispute history, escrow events and commission reversals reject ordinary
UPDATE/DELETE/TRUNCATE. Repeated refund/release requests preserve exactly one
financial result. An approval that committed before a failed settlement remains
visible for retry.

## Financial behavior and migration

`wallets.balance` includes held proceeds. `held_balance` is unavailable for
withdrawals, transfers, purchases or deposit reversals; the wallet displays
`balance - held_balance`. A database bound protects held funds as well as the
repository and service checks.

Every new wallet checkout snapshots the seller and gross/commission/net amounts
in the same transaction as the buyer debit, seller credit and order creation.
Refund arithmetic uses integer kobo and cumulative commission rounding. Several
partial refunds cannot exceed the original paid value, and a full refund reverses
exactly the original seller proceeds and commission. Platform totals subtract
commission reversals. No external payment API or paid SMS API is used by these
wallet refund transactions.

The existing migration runner creates dispute/payment tables, upgrades
`order_refunds` to `refunds`, installs financial integration, then installs admin
triggers. Legacy refund IDs, references, provider fields and history survive the
rename/backfill. If both table names already exist, migration fails explicitly
without choosing or deleting records. Historical timestamps are retained without
assigning a guessed source timezone. New escrow records use TIMESTAMPTZ.

Old and direct-provider payments without a verified hold remain financial-review
cases. The current direct-provider verification marks an order paid but does not
create an escrow hold. Settlement therefore does not assert those funds are held
or debit other seller money. This is the concrete payment dependency still needed
from Members 3/4/5.

## Storage and deployment

Set `DISPUTE_EVIDENCE_DIR` to a private persistent directory outside `frontend`.
The default is `backend/private/dispute-evidence` when launched from `backend`.
Application instances must share that directory. Files use random keys and
owner-only creation modes. Downloads enforce current case access, check the
recorded size and SHA-256, and use attachment/no-store/nosniff headers. Private
files are git-ignored and never served by the static route. Back up evidence
alongside its database records.

Keep the session, financial and audit schema with this application version.
Do not deploy the old startup code against the new refund schema: it recreates
`order_refunds` beside `refunds`, which creates an ambiguous future upgrade.
Review a rollback using matched code/database snapshots in a disposable
rehearsal, accounting for any new held payments and retaining audit history.

The requested development onboarding bypass and dashboard login behavior remain
configured for development. The bypass does not create verification evidence or
grant staff/financial authority and is rejected in production.

## Verification and release status

The previous session/audit-only verification remains in
[role-integration.json](test-evidence/admin-governance/role-integration.json).
Current combined implementation results are recorded separately in
[outstanding-implementation.json](test-evidence/admin-governance/outstanding-implementation.json).
Standard and race runs each passed 478 top-level tests / 925 test and subtest
results, including all 105 required PostgreSQL tests, with no failures or skips.
Vet, the build, eight evidence-checker tests, JavaScript/diff checks and 28
synthetic Chrome viewport checks also passed. Tests cover actual endpoints,
private evidence, role/revision rejection, partial
and full money movement, concurrent retries, audit rollback, receipt/withdrawal,
legacy refund preservation and repeated migration. The required PostgreSQL
inventory includes the imported Member 5 tests and the new combined regressions.

Implementation and local verification do not provide another team member's
independent review or product approval. Hosted CI, deployment rehearsal and the
specific owner items in the table above remain release work. The original
`MASTER_ISSUE_REGISTER.md`, `REMEDIATION_ROADMAP.md` and full audit package are
absent from this checkout; this handoff cannot independently reconcile their
remaining findings.
