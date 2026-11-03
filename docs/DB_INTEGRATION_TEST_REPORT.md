# AGRO-009 PostgreSQL execution report

## Scope and audit validation

Approved baseline: `main` at `ef60c2d61a3a3b41f41700eec47ec31108417c3a`. Branch: `feature/agro-009-013-014-db-quality`. The starting workspace was clean. None of MASTER_ISSUE_REGISTER.md, REMEDIATION_ROADMAP.md, DATABASE_AUDIT.md, SECURITY_AUDIT.md, FINANCIAL_SYSTEM_AUDIT.md, BACKEND_API_AUDIT.md, TEST_RESULTS.md, PRODUCTION_READINESS.md or FEATURE_COMPLETENESS.md was present. Issue IDs come from the assignment; missing audit documents cannot be reconciled directly. The actual counts below supersede estimates for this inspected baseline.

Confirmed: two helpers skipped without TEST_DATABASE_URL, accepted arbitrary databases and shared schema state; no CI workflow existed; several cleanup errors were ignored. Existing real-PostgreSQL tests already covered deposits, concurrent duplicate credits, checkout idempotency, rollback, registration ownership and migration balance preservation. These were retained rather than reimplemented. No build-tag exclusions, optional provider database tests or other skip sites existed in the inspected backend database suite.

Changes: a guarded local-only test URL, private schema per test, deterministic real migrations, UTC test connections, cleanup assertions, REQUIRE_DB_TESTS mode, explicit required-test manifest and Go JSON verifier, PostgreSQL 16 GitHub Actions service, evidence-checker unit tests, additional financial/migration/timestamp regressions and shared conventions. Runtime wallet policies and production database setup are unchanged. See DATABASE_TESTING_AND_MIGRATION_GUIDE.md for ownership and migration contracts.

## Execution environment and commands

Executed on 2026-11-03, Linux amd64, Go 1.25.0, PostgreSQL 16.15. Docker was unavailable because daemon access was denied even outside the sandbox. Available PostgreSQL binaries were used to initialize a new disposable cluster under `/tmp/agro-member8-pg`, bound only to loopback port 55438, with synthetic fixtures. No existing cluster or production database was used. The local database encoding is SQL_ASCII (C locale); CI's standard PostgreSQL image initializes UTF8. The local financial/locking results do not establish UTF8/collation coverage. Server default timezone was Africa/Casablanca; the helper explicitly sets every test connection to UTC and final runs set the Go process `TZ=UTC`.

From `backend`, with TEST_DATABASE_URL configured as documented (never print its value):

```sh
TZ=UTC REQUIRE_DB_TESTS=1 go test -count=1 -json ./... > ../test-results.json
python3 ../scripts/check_db_tests.py ../test-results.json
go vet ./...
TZ=UTC REQUIRE_DB_TESTS=1 go test -race -count=1 -json ./... > ../race-results.json
python3 ../scripts/check_db_tests.py ../race-results.json
python3 -m unittest discover -s ../scripts -p 'test_*.py'
go test ./render -run '^$' -bench '^BenchmarkTemplateRendering$' -benchmem -count=3
```

The local command explicitly selected the installed Go 1.25.0 binary and temporary module/build caches; the commands above are equivalent when `go version` reports that version. All required tests ran without a `-run` filter in final normal/race verification. Benchmarks and full results are in TEMPLATE_PERFORMANCE_REPORT.md. `go vet` exited 0. The seven Python evidence-checker tests passed.

## Final results

| Run | Top-level tests | Subtests | Total terminal test results | Passed | Failed | Skipped | Required database tests passed |
|---|---:|---:|---:|---:|---:|---:|---:|
| Go 1.25 normal | 350 | 371 | 721 | 721 | 0 | 0 | 43/43 |
| Go 1.25 race | 350 | 371 | 721 | 721 | 0 | 0 | 43/43 |
| Baseline selected database tests, no DB | 35 | 2 | 37 | 1 | 0 | 36 | 0/35 |

Totals include parent test events and child events separately; they are not unique leaf-case counts. Packages reporting no test files are excluded from test skip counts. Normal and race are separate runs and must not be added together as distinct coverage. Of the 43 required top-level tests, 40 are in repository, two in services and one in testdb. The race detector supplements the actual row-lock/constraint tests and does not detect database races by itself.

Compact, credential-free Go JSON terminal events are retained in `docs/test-evidence`. They include every final test/package outcome; baseline skipped events also retain their explicit reason. Full CI JSON output is uploaded as an Actions artifact. Recheck retained evidence from the repository root:

```sh
python3 scripts/check_db_tests.py docs/test-evidence/member8-final-go125.jsonl
python3 scripts/check_db_tests.py docs/test-evidence/member8-race-go125.jsonl
```

## Required database execution inventory

| Package | Required test | Normal | Race |
|---|---|---|---|
| `backend/internal/repository` | `TestDeleteAccountAnonymisesAndKeepsRecords` | pass | pass |
| `backend/internal/repository` | `TestDeleteAccountRefusesWhileMoneyIsAttached` | pass | pass |
| `backend/internal/repository` | `TestPayForOrderIsIdempotentPerCheckoutToken` | pass | pass |
| `backend/internal/repository` | `TestConcurrentPayForOrderWithSameTokenChargesOnce` | pass | pass |
| `backend/internal/repository` | `TestSearchAvailableFiltersByNameLocationAndSeller` | pass | pass |
| `backend/internal/repository` | `TestConcurrentDistinctCreditsPreserveBalance` | pass | pass |
| `backend/internal/repository` | `TestConcurrentDebitsEnforceFundsAndOwnership` | pass | pass |
| `backend/internal/repository` | `TestDepositRollbackCanRetryAfterConstraintConflict` | pass | pass |
| `backend/internal/repository` | `TestMigrationRepeatPreservesConstraintsAndIndexes` | pass | pass |
| `backend/internal/repository` | `TestMigrationRestoresSupportedLegacyWithdrawalColumns` | pass | pass |
| `backend/internal/repository` | `TestOnboardingMigrationRollsBackOnConstraintFailure` | pass | pass |
| `backend/internal/repository` | `TestOnboardingMigrationPreservesExistingSteps` | pass | pass |
| `backend/internal/repository` | `TestPhoneVerificationAtomicConsumptionAndPhoneChange` | pass | pass |
| `backend/internal/repository` | `TestInvalidatingFailedDeliveryPreservesNewerOTPAndIssuanceAudit` | pass | pass |
| `backend/internal/repository` | `TestPhoneVerificationRejectsExpiredExhaustedAndMismatchedChallenges` | pass | pass |
| `backend/internal/repository` | `TestUpdateProfileRejectsAnotherAccountsPhone` | pass | pass |
| `backend/internal/repository` | `TestExplicitTimestampInterpretationIgnoresSessionTimezone` | pass | pass |
| `backend/internal/repository` | `TestWalletPINPersistsOnWalletAndSurvivesMigration` | pass | pass |
| `backend/internal/repository` | `TestWalletPINSetupRequiresVerificationAndCreatesOnlyOnce` | pass | pass |
| `backend/internal/repository` | `TestWalletPINLockoutPersistsAndExpires` | pass | pass |
| `backend/internal/repository` | `TestConcurrentWalletPINGuessesCannotBypassLockout` | pass | pass |
| `backend/internal/repository` | `TestAccountClosureRemovesWalletPIN` | pass | pass |
| `backend/internal/repository` | `TestRegistrationCreatesExactlyOneNGNWallet` | pass | pass |
| `backend/internal/repository` | `TestEnsureWalletIsIdempotent` | pass | pass |
| `backend/internal/repository` | `TestCreateDepositWithoutWalletIsRejected` | pass | pass |
| `backend/internal/repository` | `TestCompleteDepositCreditsExactlyOnce` | pass | pass |
| `backend/internal/repository` | `TestConcurrentCompleteDepositCreditsOnce` | pass | pass |
| `backend/internal/repository` | `TestCompleteDepositRejectsMismatchedAmountOrCurrency` | pass | pass |
| `backend/internal/repository` | `TestCompleteDepositRollsBackWhenLedgerWriteFails` | pass | pass |
| `backend/internal/repository` | `TestMigrationPreservesExistingBalances` | pass | pass |
| `backend/internal/repository` | `TestWithdrawalReservesAndRejectsInsufficientBalance` | pass | pass |
| `backend/internal/repository` | `TestDepositRecordsProviderAndScopesTransactionIDUniqueness` | pass | pass |
| `backend/internal/repository` | `TestVirtualAccountAttemptIsUniquePerFarmer` | pass | pass |
| `backend/internal/repository` | `TestVirtualAccountDatabaseConstraintsRejectInvalidAccounts` | pass | pass |
| `backend/internal/repository` | `TestVirtualAccountCreditIsExactAndIdempotent` | pass | pass |
| `backend/internal/repository` | `TestVirtualAccountPendingTransferIsCreditedOnceWhenConfirmed` | pass | pass |
| `backend/internal/repository` | `TestVirtualAccountReversalDebitsOnlyWhenFunded` | pass | pass |
| `backend/internal/repository` | `TestConcurrentVirtualAccountCreditMovesMoneyOnce` | pass | pass |
| `backend/internal/repository` | `TestWalletStatusReportsCommittedStateWithoutWriting` | pass | pass |
| `backend/internal/repository` | `TestWalletStatusReportsMissingWallet` | pass | pass |
| `backend/internal/services` | `TestAcceptNegotiationAddsTheDealToTheCart` | pass | pass |
| `backend/internal/services` | `TestAcceptNegotiationTwiceAddsOneCartItem` | pass | pass |
| `backend/internal/testdb` | `TestSchemasAreIsolatedAndCleaned` | pass | pass |

## Financial, ownership and migration conclusions

- Concurrent distinct deposits: twelve committed 100.00 credits produced balance 1200.00 and twelve ledger entries, without lost updates.
- Concurrent debits: twelve 100.00 requests against 500.00 produced exactly five reservations, seven insufficient-funds responses, balance zero and five withdrawal records. Another user could not cancel the owner's withdrawal or see the owner's transactions through owner-scoped listing.
- Existing concurrent duplicate deposit, virtual-account callback and checkout-token tests passed. New failure-injection coverage verifies that a deposit ledger constraint failure leaves the deposit pending and balance unchanged, and retry succeeds exactly once after synthetic conflict repair. This exercises the repository's existing retry behavior, not a new provider/refund policy.
- Fresh schema initialization runs for every required test. Repeated startup migrations preserve index/constraint definitions and balances. A representative pre-name-check/reminder withdrawal-column fixture upgrades successfully. Transactional onboarding migration fails on an invalid legacy purpose, rolls back an earlier added column, preserves the invalid row, and succeeds after fixture repair. Arbitrary legacy schema upgrade and global startup rollback are not supported contracts and are not claimed.
- Repository owner-scoped methods and cancellation are tested at their boundary. Methods accepting a farmer ID are trusted internal APIs; authenticated caller identity belongs to HTTP/service checks. Existing service authorization and route/handler tests ran in the full suite. No parallel ledger writer or financial policy change was introduced.
- Six synthetic timezone interpretation cases pass under UTC/Lagos/Tokyo sessions; these verify conversion mechanics, not historical data meaning. Twelve template output-equivalence cases, concurrent rendering and missing-template handling passed. Details are in the dedicated reports.

## Skipped-test reconciliation

The baseline was exported from Git into a separate temporary directory and its 35 integration-test function names selected explicitly in repository/services packages, with TEST_DATABASE_URL and REQUIRE_DB_TESTS absent. This exploratory baseline run used available Go 1.27.3; final verification uses declared Go 1.25.0. It produced 34 skipped top-level tests plus two skipped children of `TestCompleteDepositRejectsMismatchedAmountOrCurrency`; that parent reported pass. Thus **35 database test functions, 36 skipped test events and 37 terminal test events** are all accurate descriptions with different denominators. No database assertions executed in that baseline run. Final CI requires all of these tests plus eight new top-level regressions to pass.

Baseline per-test evidence:

| Package | Test/subtest | Result | Skip reason |
|---|---|---|---|
| `backend/internal/repository` | `TestAccountClosureRemovesWalletPIN` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestCompleteDepositCreditsExactlyOnce` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestCompleteDepositRejectsMismatchedAmountOrCurrency` | pass | parent passed after child skips |
| `backend/internal/repository` | `TestCompleteDepositRejectsMismatchedAmountOrCurrency/amount` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestCompleteDepositRejectsMismatchedAmountOrCurrency/currency` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestCompleteDepositRollsBackWhenLedgerWriteFails` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestConcurrentCompleteDepositCreditsOnce` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestConcurrentPayForOrderWithSameTokenChargesOnce` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestConcurrentVirtualAccountCreditMovesMoneyOnce` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestConcurrentWalletPINGuessesCannotBypassLockout` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestCreateDepositWithoutWalletIsRejected` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestDeleteAccountAnonymisesAndKeepsRecords` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestDeleteAccountRefusesWhileMoneyIsAttached` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestDepositRecordsProviderAndScopesTransactionIDUniqueness` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestEnsureWalletIsIdempotent` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestInvalidatingFailedDeliveryPreservesNewerOTPAndIssuanceAudit` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestMigrationPreservesExistingBalances` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestOnboardingMigrationPreservesExistingSteps` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestPayForOrderIsIdempotentPerCheckoutToken` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestPhoneVerificationAtomicConsumptionAndPhoneChange` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestPhoneVerificationRejectsExpiredExhaustedAndMismatchedChallenges` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestRegistrationCreatesExactlyOneNGNWallet` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestSearchAvailableFiltersByNameLocationAndSeller` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestUpdateProfileRejectsAnotherAccountsPhone` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestVirtualAccountAttemptIsUniquePerFarmer` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestVirtualAccountCreditIsExactAndIdempotent` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestVirtualAccountDatabaseConstraintsRejectInvalidAccounts` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestVirtualAccountPendingTransferIsCreditedOnceWhenConfirmed` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestVirtualAccountReversalDebitsOnlyWhenFunded` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestWalletPINLockoutPersistsAndExpires` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestWalletPINPersistsOnWalletAndSurvivesMigration` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestWalletPINSetupRequiresVerificationAndCreatesOnlyOnce` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestWalletStatusReportsCommittedStateWithoutWriting` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestWalletStatusReportsMissingWallet` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/repository` | `TestWithdrawalReservesAndRejectsInsufficientBalance` | skip | set TEST_DATABASE_URL to run wallet integration tests |
| `backend/internal/services` | `TestAcceptNegotiationAddsTheDealToTheCart` | skip | set TEST_DATABASE_URL to run negotiation integration tests |
| `backend/internal/services` | `TestAcceptNegotiationTwiceAddsOneCartItem` | skip | set TEST_DATABASE_URL to run negotiation integration tests |

## Failures and limitations retained honestly

1. An initial exploratory full run with Go process Africa/Casablanca and database UTC returned 694 passed, one failed, zero skipped (695 test/subtest results). `TestInvalidatingFailedDeliveryPreservesNewerOTPAndIssuanceAudit` observed issuance count 0 instead of 2. Naive database-created timestamps and Go-local cutoffs do not represent a common instant convention. The assertion is unchanged; final reproducibility pins both test timezones to UTC. This existing deployment defect remains unresolved. Historical timezone conversion is gated by the owner's instruction; see TIMESTAMP_MIGRATION_PLAN.md.
2. Intentional negative probe: required mode without TEST_DATABASE_URL failed `TestSchemasAreIsolatedAndCleaned` with “TEST_DATABASE_URL is required”. This is expected evidence that required execution cannot silently skip.
3. Intentional negative probe: an unreachable loopback test endpoint failed with a sanitized connection-failure message. Output contained neither the synthetic password nor the connection URL.
4. CI configuration is implemented; actual hosted execution status must be checked on the PR. A local passing suite does not prove hosted CI ran. Local SQL_ASCII encoding is a coverage limitation until CI confirms UTF8 execution.
5. No production schema conversion, migration framework replacement, refund/KYC change or merge into main was performed. Members 3/4 should review financial expectations; Member 4 and deployment owners must resolve timestamp history and business-window policy. Owner acknowledgments are not assumed.
