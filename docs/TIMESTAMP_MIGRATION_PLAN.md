# AGRO-013 timestamp migration plan

## Status and verified evidence

Data conversion is **gated**. The owner confirmed that historical timezone is unknown and instructed us to keep conversion gated. No production connection or application timestamp conversion was performed. There is no numbered migration framework currently; reserving and executing a number requires migration coordination rather than adding an independent startup runner.

`database.RunMigration` defines mostly `TIMESTAMP` (PostgreSQL timestamp without time zone), with `CURRENT_TIMESTAMP` defaults. `wallets.transaction_pin_locked_until` is already `TIMESTAMPTZ`. Repository writes use both `CURRENT_TIMESTAMP` and Go `time.Now()`; OTP expiry and consumption are application-created. `ConnectDB` does not pin the production session timezone. Therefore historical session/process settings could have changed what wall-clock value was stored. Code alone cannot prove UTC, Lagos or a single interpretation across rows. Existing KYC documentation (`docs/kyc-wallet-audit.md`, G10; `docs/kyc-limit-enforcement-map.md`, timezone section) independently identifies this dependency.

Go models scan into `time.Time` / `sql.NullTime`; JSON normally preserves the time value's offset. A naive timestamp has no stored instant, so calling `.UTC()` after scanning cannot recover its original timezone. Chat markup uses `.CreatedAt.UTC.Format` for its datetime attribute, but this does not resolve historical ambiguity. Some day/month windows use database session timezone; their intended business timezone belongs to Member 4's policy review. Test sessions are pinned to UTC solely for reproducibility; production behavior is unchanged.

## Column inventory

The 68-column inventory below (67 naive timestamps and one TIMESTAMPTZ) is derived from current schema statements, including ALTER additions. Defaults and nullability must be verified against the target database's catalog before approving a migration; deployed legacy schemas may differ. No live production catalog was inspected.

| Table | Column | Type | Declaration/default |
|---|---|---|---|
| `bank_account_lookups` | `probed_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `cart_items` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `chat_messages` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `conversation_members` | `joined_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `conversation_members` | `last_read_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `conversations` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `conversations` | `updated_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `crop_images` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `crops` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `crops` | `first_listed_at` | `TIMESTAMP` | `nullable; no default` |
| `crops` | `first_order_at` | `TIMESTAMP` | `nullable; no default` |
| `crops` | `sold_out_at` | `TIMESTAMP` | `nullable; no default` |
| `crops` | `updated_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `deliveries` | `cancelled_at` | `TIMESTAMP` | `nullable; no default` |
| `deliveries` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `deliveries` | `delivered_at` | `TIMESTAMP` | `nullable; no default` |
| `deliveries` | `estimated_arrival` | `TIMESTAMP` | `nullable; no default` |
| `deliveries` | `picked_up_at` | `TIMESTAMP` | `nullable; no default` |
| `deliveries` | `scheduled_at` | `TIMESTAMP` | `nullable; no default` |
| `deliveries` | `updated_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `diagnoses` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `diagnosis_media` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `external_market_prices` | `collected_at` | `TIMESTAMP` | `NOT NULL` |
| `external_market_prices` | `imported_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `farmers` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `farmers` | `deleted_at` | `TIMESTAMP` | `nullable; no default` |
| `farmers` | `phone_verified_at` | `TIMESTAMP` | `nullable; no default` |
| `farmers` | `selfie_captured_at` | `TIMESTAMP` | `nullable; no default` |
| `farmers` | `updated_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `feedback` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `inventory_history` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `market_events` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `marketplace_searches` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `negotiation_messages` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `negotiations` | `accepted_at` | `TIMESTAMP` | `nullable; no default` |
| `negotiations` | `closed_at` | `TIMESTAMP` | `nullable; no default` |
| `negotiations` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `negotiations` | `expires_at` | `TIMESTAMP` | `nullable; no default` |
| `negotiations` | `updated_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `notifications` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `orders` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `otp_codes` | `consumed_at` | `TIMESTAMP` | `nullable; no default` |
| `otp_codes` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `otp_codes` | `expires_at` | `TIMESTAMP` | `NOT NULL` |
| `payments` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `payments` | `updated_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `platform_revenue` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `produce_price_history` | `observed_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `wallet_deposits` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `wallet_deposits` | `updated_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `wallet_transactions` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `wallet_transfers` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `wallet_transfers` | `reminded_at` | `TIMESTAMP` | `nullable; no default` |
| `wallet_transfers` | `updated_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `wallet_virtual_accounts` | `consent_at` | `TIMESTAMP` | `nullable; no default` |
| `wallet_virtual_accounts` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `wallet_virtual_accounts` | `last_checked_at` | `TIMESTAMP` | `nullable; no default` |
| `wallet_virtual_accounts` | `updated_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `wallet_withdrawals` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `wallet_withdrawals` | `name_check_at` | `TIMESTAMP` | `nullable; no default` |
| `wallet_withdrawals` | `reminded_at` | `TIMESTAMP` | `nullable; no default` |
| `wallet_withdrawals` | `updated_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `wallets` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `wallets` | `transaction_pin_created_at` | `TIMESTAMP` | `nullable; no default` |
| `wallets` | `transaction_pin_locked_until` | `TIMESTAMPTZ` | `nullable; no default` |
| `wallets` | `updated_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `webhook_events` | `created_at` | `TIMESTAMP` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` |
| `webhook_events` | `processed_at` | `TIMESTAMP` | `nullable; no default` |

## Confirmed execution defect

The initial full integration run used an Africa/Casablanca Go process and UTC database sessions. `TestInvalidatingFailedDeliveryPreservesNewerOTPAndIssuanceAudit` failed: issuance count was 0 instead of 2. `otp_codes.created_at` is a database-generated naive UTC wall clock while `CountIssuedSince` receives a Go-local wall clock through a naive timestamp parameter. The unchanged test passes only when the two conventions agree. The reproducible CI environment sets both to UTC; this does not repair deployments or authorize conversion. This finding requires coordinated application timestamp work after the historical decision gate.

## Decision gate and execution design

1. Obtain historical deployment configuration: database/role/session `TimeZone`, application process TZ, connection options and changes over time. Compare known synthetic/support-approved events with provider timestamps; do not export private records into review artifacts.
2. Record a source timezone per column and deployment epoch. Mixed data needs an approved row-level classification/backfill plan. Missing evidence is a stop condition, not permission to assume UTC.
3. Coordinate with Members 3, 4, 5 and 6 for financial windows, checkout, refunds and audit retention. Reserve the next migration number in the agreed migration ledger/framework. None exists today.
4. Prepare an explicit forward conversion, e.g. `USING created_at AT TIME ZONE 'UTC'` only for verified naive UTC values, or `AT TIME ZONE 'Africa/Lagos'` for verified Lagos values. Never rely on an implicit cast/session timezone. The reverse is `TYPE TIMESTAMP USING created_at AT TIME ZONE '<verified source zone>'`.
5. Inventory defaults, indexes, constraints, views and dependent queries first. Keep a reviewed ordered column list. `TIMESTAMPTZ` defaults may use `CURRENT_TIMESTAMP` but boundary queries require explicit intended timezone. Avoid altering already-aware columns.
6. Rehearse on a sanitized representative restore. The present schema-setup function is not an all-or-nothing versioned migration engine. A conversion should run once under an operator-controlled transaction with a short `lock_timeout` and bounded `statement_timeout`, not concurrently at startup.

## Backup and recovery runbook

Before deployment, take an encrypted `pg_dump --format=custom` backup plus schema/role configuration snapshots using the approved secret manager and restricted storage. Keep credentials out of arguments and logs (use a protected password file). Record source PostgreSQL version and backup checksum. Verify the archive with `pg_restore --list`, then restore into an isolated compatible PostgreSQL instance and run schema, row-count, constraint and financial reconciliation checks. Archive listing alone does not establish restorability.

Schedule maintenance with a single migration executor. Pause writers, background reconciliation, import jobs and other schema executors; drain/recycle application pools so active transactions cannot span the type change. `ALTER COLUMN TYPE` takes ACCESS EXCLUSIVE locks, may rewrite tables and rebuild indexes, and can invalidate prepared statements. Assess table size, WAL/disk headroom, replicas and lock blockers before setting a maintenance window.

Within the reviewed transaction: capture column types/defaults, sample only approved reference instants, apply explicit timezone conversions in agreed order, and check counts, nullability, indexes, constraints, financial window queries and epoch equality against the rehearsal reference. Validate with sessions in UTC, Lagos and another timezone. Resume only after smoke checks; otherwise roll back the transaction before commit.

After commit, rollback is a separate coordinated migration: pause writers again, back up the changed database, assess any newly written aware timestamps, and use the verified source zone explicitly when converting back. This discards timezone-aware type semantics and may lose distinctions in DST-overlap regions or mixed-origin data; do not claim it restores arbitrary mixed data. If original interpretation or data integrity is uncertain, restore the verified pre-migration backup to a separate instance, validate it, and switch traffic through the established recovery process. Preserve/reconcile post-backup transactions under owner approval rather than blindly overwriting financial history. Define acceptable downtime and recovery-point loss with the deployment owner before production execution.

Useful preflight catalog query (no row data):

```sql
SELECT table_schema, table_name, column_name, data_type, is_nullable, column_default
FROM information_schema.columns
WHERE table_schema = current_schema()
  AND data_type IN ('timestamp without time zone', 'timestamp with time zone')
ORDER BY table_name, ordinal_position;
```

## Regression evidence

`TestExplicitTimestampInterpretationIgnoresSessionTimezone` uses isolated synthetic data, verifies both UTC and Lagos interpretations under UTC/Lagos/Tokyo sessions, checks instant equality, UTC JSON serialization and reverse wall-clock conversion. This establishes the proposed conversion technique, not historical data meaning. Final execution outcomes are recorded in `DB_INTEGRATION_TEST_REPORT.md`.
