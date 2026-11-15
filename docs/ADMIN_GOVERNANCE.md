# Administration and accountability

Member 6 implementation for **AGRO-008** (persistent administrative audit) and **AGRO-019** (user management and listing moderation).

## Opening the console

Sign in with a fully onboarded staff account and open **Profile → Admin console**, or visit `/admin`. Moderators and administrators enter the community overview. Accounts with only wallet approval access go to the existing wallet approvals page.

The console provides an overview, a searchable user directory, listing moderation, and an administrator-only audit history. It works on narrow screens and supports keyboard navigation. Management dialogs show the target, the effect of the selected change, a required reason, and an explicit review checkbox. Closing a dialog makes no change.

## Permissions

Staff roles are independent of the farmer/buyer account type and wallet approval access.

| Capability | Member | Moderator | Administrator | Wallet approver only |
|---|---|---|---|---|
| Open community overview and look up members | No | Yes | Yes | No |
| Block or restore another member's listings | No | Yes | Yes | No |
| Suspend or reactivate accounts | No | No | Yes | No |
| Assign staff roles or wallet approval access | No | No | Yes | No |
| Read audit history | No | No | Yes | No |
| Approve, reject, or repair wallets | No | No | Only with wallet access | Yes |

`disputes.review` is reserved for administrators so Member 5 can use the same permission model. This change does not add dispute adjudication or refund business rules.

Existing `farmers.is_admin` flags retain **wallet approval access only**. IDs explicitly configured in `WALLET_ADMIN_USER_IDS` bootstrap protected administrators with both governance and wallet access. Configuration is checked against a live, non-deleted, non-suspended account; a configured ID cannot reactivate a disabled account. Review the configured IDs when upgrading. No default password or staff account is created.

Administrators cannot change their own access or change a configured administrator through the console. To suspend a staff administrator or wallet approver, another administrator must first remove those privileges in separate, recorded decisions. Moderators cannot moderate their own listings. These restrictions also apply to direct API calls.

## Daily workflows

### Review an account

1. Open **Users** and search by name, account ID, or an exact full phone number. Results show only the last four phone digits; they do not include passwords, bank details, or full phone numbers.
2. Select **Manage** beside the account. Review the current role and account status.
3. Select the change and a reason, read its consequences, and tick the review confirmation before applying it.
4. A saved banner confirms the change. If another administrator changed the record first, the request returns a conflict: reload and review the new state.

Suspension disables login and authenticated browser/USSD access, hides the seller's marketplace listings, and blocks new purchases involving the account. Durable sessions are revoked in the same transaction. Existing in-memory sessions are invalidated against the account's revocation revision, including logins racing with suspension. Reactivation requires a fresh sign-in and does not remove individual listing blocks. Historical orders and financial records are preserved.

### Review a listing

Search **Listings** by produce, seller, or listing ID. Select **Review**, choose a reason, confirm, and apply the decision. A block is separate from the seller's listing flag, so relisting the crop cannot remove moderation. A restored listing appears only if the seller is active, it is listed for sale, and stock is available.

Marketplace search, direct product visibility, wallet checkout, legacy order creation, and initiation of an external payment respect moderation and suspension. Financial provider callbacks still settle already-started payments; a moderation decision does not silently discard a payment or invent a refund.

### Review an audit record

The audit log identifies the actor's account ID, action, target, reason, previous and resulting state, and UTC timestamp. Account IDs remain attributable after account anonymization; audit records deliberately avoid contact information, request bodies, credentials, and provider errors. Reasons are fixed categories instead of free text.

**Applied** describes a committed state change. Wallet actions record **Requested** before calling the existing service, then **Response sent** or **Unsuccessful** when the handler finishes. A response is not proof of settlement: separate wallet status events record the actual committed financial transition. Matching request references connect the attempt and its response. A request with no response record requires checking the wallet state and application logs before retrying.

## HTTP interface

All routes require an authenticated, onboarded account and fresh server-side authorization. Mutations are POST-only and require the application's same-origin CSRF evidence. Administrative responses use `Cache-Control: no-store`.

| Method | Route | Permission |
|---|---|---|
| GET | `/admin`, `/api/admin/overview` | Community read; HTML redirects wallet-only staff to approvals |
| GET | `/admin/users`, `/api/admin/users` | `users.read` |
| GET | `/admin/listings`, `/api/admin/listings` | `listings.moderate` |
| GET | `/admin/audit`, `/api/admin/audit` | `audit.read` |
| POST | `/admin/users/change`, `/api/admin/users/change` | `users.manage`; role changes also require `roles.manage` |
| POST | `/admin/listings/change`, `/api/admin/listings/change` | `listings.moderate` |
| Existing wallet admin routes | `/admin/wallet-withdrawals…`, `/admin/wallet-transfers…`, `/admin/wallets/repair` | `wallet.manage` with durable request audit |

The former `/admin/wallet-admins` endpoint uses the user-change contract and governance authorization. The old unversioned grant/remove handler is removed, and the wallet page links authorized administrators to the user directory.

Read responses use `{"data": …, "next": "?before=…&q=…"}`. Directories accept `q` (at most 100 bytes) and a nonnegative `before` cursor; audit accepts `before`. Pages contain at most 25 records in descending ID order. HTML pages provide next-page and first-page links.

Change requests accept JSON or form fields, limited to 4 KiB:

```json
{
  "target_id": 42,
  "version": 3,
  "action": "suspend",
  "reason": "policy_violation"
}
```

Use the version supplied by the directory. JSON must include `version`; unknown fields and trailing JSON are rejected. User actions: `suspend`, `reactivate`, `role_none`, `role_moderator`, `role_administrator`, `wallet_grant`, `wallet_revoke`. Listing actions: `block`, `restore`. Reasons: `policy_violation`, `suspected_fraud`, `incorrect_listing`, `review_cleared`, `staff_assignment`.

Successful JSON mutations return `{"status":"saved"}`. Validation errors return 400, denied/protected operations 403, missing targets 404, stale decisions 409, and unavailable storage 503. Database errors are not returned to the browser. A denied or stale decision does not produce a committed-change audit event.

## Database and operational behavior

`RunAdminMigration` runs inside the existing ordered startup migration runner. It adds staff roles, suspension/revocation timestamps, record versions, separate listing moderation flags, and `admin_audit_events`. New timestamps use `TIMESTAMPTZ`; no historical timestamps are converted. Existing farmers and crops retain their state. The migration is transactional and repeatable.

Governance changes, session revocation, and audit insertion commit together. A transaction-scoped advisory lock serializes administrative decisions; optimistic record versions reject outdated forms. Auditing failure rolls back the associated governance change. Wallet requests fail before calling financial services if their audit intent cannot be persisted. Database triggers record committed withdrawal and transfer status changes inside their existing financial transactions. Payment rules and ledger arithmetic remain owned by the existing services.

Triggers reject audit UPDATE, DELETE, and TRUNCATE, and request-stage uniqueness prevents duplicate response records. This protects against application-level alteration; database owners and superusers can change triggers or schema. Production operators should use restricted runtime credentials, separate migration ownership, and independent backups or log export when evidence must survive privileged database access. The current application still performs startup migrations, so credential separation requires deployment coordination with Member 8.

Deploy the application and schema changes together using the project's existing migration procedure. For a code rollback, keep the additive schema and audit data: the previous code ignores the new columns, but also stops enforcing the new restrictions. Do not drop audit records as part of rollback. Review rollback with the release owner and Member 8 before production use.

## Verification

Use only the disposable PostgreSQL environment described in [Database testing and migration conventions](DATABASE_TESTING_AND_MIGRATION_GUIDE.md). New tests are included in `scripts/required_db_tests.json` so CI rejects missing or skipped execution.

```sh
cd backend
go test -count=1 -json ./... > /tmp/agro-admin-tests.json
python3 ../scripts/check_db_tests.py /tmp/agro-admin-tests.json
go vet ./...
go test -race -count=1 -json ./... > /tmp/agro-admin-race.json
python3 ../scripts/check_db_tests.py /tmp/agro-admin-race.json
```

Set `TEST_DATABASE_URL`, `REQUIRE_DB_TESTS=1`, and `TZ=UTC` as documented before running database checks. Tests cover the permission matrix, direct API and legacy-route access, CSRF, privacy and pagination, session invalidation, stale/concurrent decisions, audit failure rollback, append-only protections, repeated migration, wallet attribution, and purchase restrictions. Browser review should also exercise account and listing dialogs, cancellation, required confirmation, keyboard navigation, narrow screens, and role-specific navigation with synthetic accounts.

### Local verification, November 15, 2026

Both the standard and race runs passed **410 top-level Go tests / 804 test and subtest results**, including **all 69 required database tests**, with no failures or skipped tests. The latest runs used Go 1.25.0 and PostgreSQL **16.15**, matching CI's PostgreSQL major version. Earlier standard and race runs also passed on PostgreSQL 18.6. `go vet ./...`, JavaScript syntax validation, and all eight evidence-checker tests passed. Hosted CI has not yet run for this feature branch.

Chrome checks passed at widths of 320, 390, 768, 1280, and 1440 pixels, including required confirmation, cancellation and keyboard dismissal, suspension/reactivation without restoring old sessions, user search, role assignment, listing block/restore with marketplace visibility, persistent audit history, and moderator restrictions. The final interface review also checked visible text contrast, mobile account navigation, touch targets, input sizing, and keyboard focus restoration after closing a dialog. Two targeted template regressions passed after the interface refinements. Browser fixtures used synthetic accounts in isolated PostgreSQL 16.15 schemas.

Before merging, complete hosted CI and independent code review as required by the remediation plan. Coordinate the session changes with Member 2, migration and deployment ownership with Member 8, and the reserved dispute-review permission with Member 5.

- [Verification summary](test-evidence/admin-governance/results.json)
- [Overview screenshot](test-evidence/admin-governance/overview.png)
- [User directory screenshot](test-evidence/admin-governance/users.png)
- [Mobile account review screenshot](test-evidence/admin-governance/mobile-review.png)
