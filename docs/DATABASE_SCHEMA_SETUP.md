# Database schema setup and explicit legacy import

The current backend uses `farmers` and `crops`. The observed older database uses
`users` and `products`, with incompatible shared tables and foreign keys. For
example, old `inventory_history.product_id` references `products`, whereas this
backend expects `inventory_history.crop_id` referencing `crops`.
`CREATE TABLE IF NOT EXISTS` does not upgrade that structure. Adding or renaming
one column cannot establish correct ownership across accounts, orders, and wallets.

Use a separate application schema in the same database. The explicit import
command maps the observed source into it while retaining the source unchanged.
This is a supporting migration for Role 6, not a replacement payment workflow or
a general migration for every historical Agro-Shield database.

## Connection setup

Select only the application schema in `DATABASE_URL`, preserving credentials,
database, SSL settings, and other provider options. With placeholders:

```dotenv
DATABASE_URL=postgresql://APP_USER:APP_PASSWORD@DIRECT_DB_HOST/DB_NAME?sslmode=require&search_path=agro_shield_app
```

Use the provider's **direct PostgreSQL endpoint** for migrations and schema
selection. The connection helper explicitly runs parameterized
`set_config('search_path',…,false)` after every physical connection opens because
the observed hosted endpoint ignores this setting in the startup message.
Transaction-pooling endpoints may not retain session settings across transactions;
the connection hook does not make those endpoints equivalent to a direct session.
Do not include `public` as a fallback: a missing application table could resolve
to an incompatible source table.

Run commands from `backend` to load its `.env`. Hosted deployments need the same
schema selection in their own environment. Keep environment backups private.

## Review the import before writing

Stop processes that write either schema or run startup DDL. Coordinate a single
migration executor with Member 8. Retain a verified backup and restore plan for
production, in addition to the untouched source schema.

```sh
cd backend
go run ./cmd/legacy-migrate --source public --target agro_shield_app --report /tmp/agro-legacy-preflight.json
```

The default mode uses a read-only, consistent snapshot. It reports table counts,
digests, a source fingerprint, aggregate wallet balances, wallet-number conversion
counts, and incompatibilities. It creates no schema or records. Reports contain
no credentials, account contact details, wallet addresses, or provider payloads.
Every issue must be resolved before apply; a nonempty destination is rejected.

## Apply the reviewed snapshot

After reviewing a zero-issue report, use its exact fingerprint:

```sh
go run ./cmd/legacy-migrate --source public --target agro_shield_app --apply --expected-fingerprint REVIEWED_FINGERPRINT --report /tmp/agro-legacy-applied.json
go run . --check-database
```

Apply initializes the target through the existing startup migration runner. The
data import then locks the source and destination, checks the fingerprint again,
copies records, verifies ownership and financial values, advances identity
sequences, and commits its mappings, completion marker, and aggregate
`system.legacy_import` audit event together. An audit or reconciliation failure
rolls back imported records. Empty bookkeeping tables and sequence advancement
may remain after failure; repairing the cause and retrying does not replay money.
The initialization DDL uses the existing runner and is not globally transactional.

An unchanged repeat run returns `already_applied` without copying or replaying
financial activity. A changed source after a committed import is rejected;
incremental synchronization is unsupported. Startup check mode exits before
payment providers, reconciliation jobs, or HTTP serving start. Normal startup
retains the existing provider reconciliation behavior; use check mode for
verification.

## Mapping and access guarantees

- Explicit `legacy_import_mappings` connect accounts, products, wallets, orders,
  and single order items to current records. Account passwords, account types,
  wallet balances, financial references and supported states are retained.
- Valid ten-digit wallet numbers are preserved exactly. Other identifiers become
  `8` plus the account ID padded to nine digits. `legacy_wallet_numbers` records
  the exact old value, new value, and ownership. Conversion never strips digits
  into a transfer address; old identifiers are not transfer aliases. Collisions
  and account IDs beyond capacity are rejected. Bank and virtual-account numbers
  are preserved, not converted.
- Existing `is_admin` flags retain wallet access only. No governance roles,
  staff accounts, PINs, phone/selfie verification, or active sessions are invented.
  Existing members sign in with their credentials and complete missing onboarding.
  Review `WALLET_ADMIN_USER_IDS` against the explicitly mapped account IDs.
- Listing, supported order/payment, ledger, deposit, withdrawal, transfer,
  virtual-account, negotiation, diagnosis, notification, and chat records are
  copied through explicit fields. Timestamp-without-time-zone values retain their
  original representation; no historical timezone interpretation is introduced.
- Source-only fields and tables remain in the source archive, including product
  status history, reviews/favorites, organization records, and old session/OTP
  evidence. They are not invented as new moderation audit events. Departed chat
  members are not restored as active members. The report inventories these rows.
- Organization ownership, multi-item orders, unsupported financial states or
  ledger types, ambiguous payout accounts, chat attachments, and incompatible
  numeric precision fail preflight. This command does not guess new policies.

See [Administration and accountability](ADMIN_GOVERNANCE.md) for console access
and [Database testing and migration conventions](DATABASE_TESTING_AND_MIGRATION_GUIDE.md)
for disposable PostgreSQL tests and migration coordination.

## Rollback and handoff

Before any writes in the target, switching configuration back to the old
application and untouched source is possible through a reviewed maintenance
procedure. After new target activity, the source is stale: switching back would
lose that activity without reconciliation. Do not drop audit records or either
schema as routine rollback. Retain the source archive with the same privacy and
retention controls as the active database. Restrict access to migration mappings;
the admin API does not expose legacy wallet identifiers.

The November 16 recovery created `agro_shield_app` and selected the direct
endpoint in the local ignored environment file. No legacy table was renamed,
emptied, or dropped. The import was applied and verified: 16 accounts, 6 listings, 16 wallets, 11
deposits, 3 ledger rows, and 2 virtual accounts were copied with their related
supported history. All 16 wallet-number conversions retained exact original
identifiers privately. Source and target wallet totals remained NGN 0.00; the
source fingerprint was unchanged, startup check mode passed, and repeat apply
was a no-op. See the [verification record](test-evidence/admin-governance/results.json)
for local test results and the actual import outcome.

## Canonical phone migration and confirmed test duplicates

Startup now runs `RunPhoneMigration` after the admin schema migration. It converts
valid active contacts to international Nigerian digits and creates a unique index
over normalized active numbers. Formatting differences cannot claim a number
already owned by another account. Invalid legacy contacts remain unchanged for
explicit correction. Changed-contact OTP challenges are retired; verification
and PIN evidence are preserved rather than invented. The backfill records one
aggregate system audit event and is a no-op on repeat runs.

If migration reports `duplicate phone accounts require explicit cleanup`, review
actual admin access and account ownership before choosing the retained account.
For explicitly confirmed test funding data, `cmd/cleanup-test-duplicates` validates
the exact selected IDs in its default rollback mode; `--apply` commits the repair
and phone migration together. It requires `--test-records`, protects administrators,
and refuses nonzero balances or cross-account commercial activity. Actual financial
histories require their normal account-closure and reconciliation procedures.
The source archive is never modified by this operator repair.

The November 16 owner-confirmed repair retained configured administrator 14 and
closed/anonymized duplicates 1 and 4, removing four test deposits and one test
ledger row. Cleanup and canonicalization committed with two attributable closure
audit events. The active schema now has 14 accounts, no normalized-phone duplicate
groups and no noncanonical valid contacts; three invalid unrelated contacts remain
for correction. Administrator access, credentials and keeper-owned records were
preserved. The 16-account public source archive remains untouched. Real local
sign-in to the retained account passed four phone formats after restart.
