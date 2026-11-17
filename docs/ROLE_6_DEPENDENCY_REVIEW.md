# Role 6 dependencies and requirement verification

Reviewed November 17, 2026 against the local working tree on
`feat/admin-governance`, based on `ce4f884`. See the
[eight-member plan](../AGRO_SHIELD_8_MEMBER_TEAM_WORK_PLAN.md) and
[current integration handoff](ROLE_6_INTEGRATION_HANDOFF.md).

The administration work now includes the combined dispute/refund workflow,
private evidence and wallet escrow implementation. The earlier session/audit-only
handoff is superseded by the implemented contracts in the current handoff.

## Requirements and evidence

| Requirement | Implemented behavior |
| --- | --- |
| Explicit roles and endpoint authorization | Existing governance permissions plus HTTP/service/transaction dispute guards; moderators, ordinary and wallet-only users are denied review; fresh permissions apply after revocation. |
| Attributable append-only history | Governance and financial transactions share audit writes; immutable dispute/escrow/commission history; audit failure rolls back settlement. |
| User and listing management | Existing masked directory, suspension/reactivation, protected staff, stale listing checks and concurrent decision protections remain enforced. |
| Clear role-aware review and confirmation | Dispute queue/detail and payment exception links; buyer/seller pages; required confirmations and record versions; independent administrator required for a participant's case. |
| Sensitive-data minimization | Private evidence authorization, upload content checks, size/count limits and download integrity; private keys/hashes and free-form complaint material are omitted from public/general audit payloads. |
| Shared payment contract | Paid wallet checkout records a hold; available-balance spending; receipt release; exact/idempotent partial and full wallet refunds with seller and commission reversals; unfunded payments stay reviewable. |
| Shared schema/test agreement | Preserving `order_refunds` upgrade, explicit ambiguity failure, repeat migration and union of required PostgreSQL tests. |
| Shared authentication | Durable hashed sessions, atomic password reset/revocation, generic failed recovery responses, expiry and authentication revisions. |

## Checked dependency references

The earlier review inspected Member 2 persistent sessions at `b4757b2`, recovery at
`b866136`, Member 5 disputes at `ffa2441`, Member 4 wallet contract at `e7023e3`,
and Member 8 database quality at `aae4def`. Those source snapshots are background
references; they are not claims of independent owner approval, merges, or current
remote branch state. Compatible components are integrated in this working tree,
with governance authorization replacing wallet-only review authorization.

## Remaining dependencies

- **Members 3/4/5:** Verified escrow/funding and settlement for direct-provider and
  historical payments; original-payment-method refunds if product requires them.
  Wallet refunds are implemented. Missing holds produce financial-review records
  and never justify taking unrelated spendable funds.
- **Member 5/product:** Review reason/quality-video policy, dispute eligibility
  deadlines and any automatic release deadline. Receipt confirmation is implemented;
  automatic deadline release has no approved policy/configuration.
- **Member 2:** Independent recovery/authentication review and production message
  delivery configuration. Local SMS logging remains free for development.
- **Member 8/release owner:** Independent combined migration review, private shared
  evidence storage/backups, historical timezone interpretation, rollback rehearsal
  and hosted CI for the final combined commit.

## Verification records

[Combined implementation evidence](test-evidence/admin-governance/outstanding-implementation.json)
records the latest local checks. Earlier
[session/audit integration evidence](test-evidence/admin-governance/role-integration.json),
[admin evidence](test-evidence/admin-governance/results.json) and
[November 16 review](test-evidence/admin-governance/dependency-review.json) retain
historical results, including hosted CI for `db94ab1`.

The canonical issue register, roadmap and original audit package are absent.
The available plan supports this integration review; it cannot establish that
all twenty original findings or product/release approvals are closed.
