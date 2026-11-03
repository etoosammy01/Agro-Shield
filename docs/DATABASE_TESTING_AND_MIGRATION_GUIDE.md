# Database testing and migration conventions

Member 8 scope: AGRO-009/013/014. Approved baseline: ef60c2d.

## Local PostgreSQL

Use a disposable PostgreSQL 16 instance, never a production database or restored personal data.
Example local container (credentials below are disposable development fixtures):

```sh
docker run --name agro-member8-test --rm -d -p 127.0.0.1:55438:5432 \
  -e POSTGRES_USER=agro_test -e POSTGRES_PASSWORD=local-test-only \
  -e POSTGRES_DB=agroshield_test postgres:16
docker exec agro-member8-test pg_isready -U agro_test -d agroshield_test
export TEST_DATABASE_URL='postgres://agro_test:local-test-only@127.0.0.1:55438/agroshield_test?sslmode=disable'
export REQUIRE_DB_TESTS=1
export TZ=UTC
cd backend
go test -count=1 -json ./... > ../test-results.json
python3 ../scripts/check_db_tests.py ../test-results.json
go vet ./...
go test -race -count=1 -json ./... > ../race-results.json
python3 ../scripts/check_db_tests.py ../race-results.json
```

Keep connection strings in environment variables; do not log them. Real credentials and user fixtures must never be committed. The helper accepts only loopback URLs naming `agroshield_test`, rejects connection override options, and checks the connected database name. This is defense in depth, not proof a local database is disposable: provision a dedicated instance and role yourself. CI uses an ephemeral service container.

## Isolation and fixtures

Use `testdb.Open(t)` from `backend/internal/testdb`. Each test receives a randomly named schema, an independent connection pool, UTC sessions, and the real startup migrations. Cleanup closes the pool and drops only that schema, failing on cleanup errors. All pooled connections carry the schema search path; concurrency tests must not use a single wrapping transaction, which would hide real commit/locking behavior. Use synthetic fixtures and real repository methods. Explicit child cleanup can remain, but schema cleanup is the final isolation boundary. Do not share a database handle across tests or set a global search path.

## Required execution and CI

All current database integration tests are required in CI; they are optional only for a local unit run without PostgreSQL. `REQUIRE_DB_TESTS=1` makes missing configuration fail. Connection and migration failures fail regardless of mode. No optional external-provider or build-tag exclusions were found in this database suite.

`scripts/required_db_tests.json` is the explicit inventory of required top-level tests. The checker also compares the manifest with integration-test source declarations to detect inventory drift. Add entries whenever adding database regression tests; never delete an entry simply to pass CI. The checker consumes Go JSON terminal events, requires every listed test and package to pass, and rejects skipped descendants. Missing, renamed, omitted, failed or skipped tests cannot satisfy it. CI runs uncached tests and a race run against PostgreSQL 16 and uploads JSON evidence. Python checker regression tests exercise missing, failed, skipped and incomplete execution.

## Migration contract and coordination

The current application calls `database.RunMigration` at startup, executing ordered SQL statements without a global transaction or migration-version ledger. Repetition is supported by guards; full rollback and arbitrary legacy upgrades are not promised. Do not introduce a second startup migration runner concurrently. New numbered migration proposals should use `NNNN_description.up.sql` / `.down.sql`, reserve numbers in review, identify schema owners, and document prerequisites before wiring execution. No timestamp conversion is authorized until historical interpretation is established.

Review foreign keys, check constraints, indexes, lock duration, query behavior, table size and compatibility with the preceding app version. Test fresh initialization and supported upgrade fixtures on isolated schemas. A successful repeated startup does not prove all historical schemas upgrade. Production requires a verified backup and restore rehearsal, a reviewed maintenance plan, and one coordinated schema executor.

## Ownership

Member 1: cookie flags; Member 2: session persistence; Member 3: cart/checkout; Member 4: wallet balances/KYC; Member 5: refunds; Member 6: admin/audit; Member 7: agricultural products; Member 8: database test infrastructure, migration review and assigned template optimization. Financial tests exercise existing interfaces and policies. Repository methods accepting a farmer ID are internal APIs: HTTP/service authentication must supply that ID; repository tests cannot prove caller authentication. Members 3 and 4 should review financial regression expectations; Member 4 must approve any daily-window timezone policy. Shared interface changes require affected-owner review.

## Known timezone defect

Both the Go process (`TZ=UTC`) and database sessions must use UTC for this reproducible fixture environment. Initial execution with Go in Africa/Casablanca and database UTC failed `TestInvalidatingFailedDeliveryPreservesNewerOTPAndIssuanceAudit`: database-created naive issuance timestamps and a Go-local naive cutoff are not comparable as instants. The test assertion is unchanged. This is an existing application defect, not repaired by setting test timezones; historical conversion remains gated. Deployments must review this risk before interpreting any passing UTC-only suite as timezone independence. See `TIMESTAMP_MIGRATION_PLAN.md`.

## Production review limits

No migration framework replacement or wallet policy changes are included here. Startup schema initialization remains sequential and is not globally transactional. `RunOnboardingMigration` is transactional; the new failure-injection regression verifies rollback and retry after repairing a synthetic invalid legacy row. The supported upgrade fixture covers payout verification/reminder columns, not arbitrary SQLite-era or historical PostgreSQL schemas.
