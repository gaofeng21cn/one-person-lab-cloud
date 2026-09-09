# OPL Cloud Roadmap And Current Gaps

Owner: `one-person-lab-cloud`
Purpose: `single_active_gap_and_priority_owner`
State: `active_planning`

This file contains only open outcomes. Current evidence belongs in
[status.md](./status.md); architecture and durable decisions belong in
[architecture.md](./architecture.md) and [decisions.md](./decisions.md).

## Priority

`P0` blocks the current portable Workspace Release. `P1` is the next accepted
product or integrity outcome. `P2` is deferred until its named trigger exists.
An `external_owner` item proceeds in that owner's repository.

## Public Beta Work Packages

D1 finance and D2 original-settlement reconciliation have completed local
development and verification. Its local source
changes address original-transaction confirmation, original-account refund
limits, successful Renewal refund parsing and exact financial lookup. Its
source checks and isolated runtime evidence belong in [status.md](./status.md).
D2 now covers retained original purchases, all renewal periods and refunds,
exact receipt lookup and the existing customer fees page. D3 additionally
closes local original-Launch recovery, bounded admission and failed-Launch
resource/Key/refund obligations. D4 closes expiry, explicit recovery and background deletion with focused and
full local verification. D5 completes local fixed-image rollout, original-operation
recovery and explicit CVM cache retirement development; production qualification
and actual node readback remain external. TCR deletion is optional. Completed development proceeds through PR review, CI,
merge and Candidate construction; Instance owns subsequent deployment and
qualification. Artifact hygiene and test-data isolation are checks in those
existing paths, not a separate D6 development stage.

| ID | State | Owner | Remaining acceptance |
| --- | --- | --- | --- |
| `D1-FINANCIAL-INTEGRITY-01` | `cloud_complete` | Control Plane finance and Sub2API; Instance owns adoption | Local source/full regression and isolated real Sub2API validation pass; [owner evidence](./status.md#retained-runtime-evidence) retains the exact worktree and patch identity. Instance must identify its exact Gateway source/image, adopt atomic debit plus exact lookup before Cloud, and retain owner readback. Historical unverified transactions cannot be certified from current balance. |
| `D2-SETTLEMENT-RECONCILIATION-01` | `cloud_complete` | Control Plane finance, Ledger and Console | Original-operation business chains, failure/recovery, historical records, more than 10k unrelated receipts, customer browser tests and full PostgreSQL/Docker regression pass. [Owner evidence](./status.md#retained-runtime-evidence) binds the local source snapshot; Instance adoption remains a separate deployment action. Unverifiable older split-resource records remain explicit review exceptions. |
| `D3-LAUNCH-RECOVERY-CAPACITY-01` | `cloud_complete` | Control Plane Launch; Fabric and Console | Original late-result recovery, bounded scheduling, and owner-authorized failed-Launch closure are implemented and pass focused plus full local verification. The closeout chain freezes the original operation, revokes exact Key identity, proves five resource absences, refunds only the original account's remaining charge, and records `billing.workspace_closed.v1`. Unknown outcomes retain pending state and pool claims. Tencent adoption and actual provisioning capacity remain Instance obligations; no deployment is part of D3 local development. |
| `D4-WORKSPACE-LIFECYCLE-01` | `cloud_complete` | Control Plane lifecycle; Fabric, Sub2API and Console | Original-period renewal, unpaid Runtime suspension, explicit recovery and background non-refunding Delete are implemented. Focused business, restart, concurrency and browser tests plus `verify:local:full` pass with zero required PostgreSQL skips. [Owner evidence](./status.md#retained-runtime-evidence) retains the isolated source and tests. Instance must adopt the same bytes and qualify actual Tencent/TKE and Gateway behavior. |
| `D5-WORKSPACE-IMAGE-LIFECYCLE-01` | `cloud_complete` | Control Plane/Fabric replacement and CRI maintenance command; Instance rollout/node retirement | Fixed approved digest, paid lifecycle preservation, persisted replacement recovery, bounded fleet results and exact node-cache removal pass focused/full local verification. [Owner evidence](./status.md#retained-runtime-evidence) binds source and isolated containerd results. Instance must adopt the same bytes, configure read-only qualification evidence access, qualify the exact linux/amd64 manifest and retain protected node/space readback. TCR deletion is optional, not a completion prerequisite. |

Runtime packaging includes only current consumers. Existing PR/CI and Instance
deployment checks keep test accounts, money, orders, Keys, Workspaces, receipts,
seeds, snapshots and volumes outside production. A development-named mode that
selects a production environment is not an isolated test environment. A concrete
packaging or deployment-input defect is fixed in its owner; there is no separate
D6 implementation gate for otherwise completed business work.

The accepted delivery target is public registration with zero initial balance,
administrator top-up, controlled Workspace purchase, complete lifecycle
recovery, and one portable Candidate qualified without changing its bytes. The
letters A-N are stable portfolio aliases; the IDs below are the canonical gap
identities.

| Alias / ID | State | Priority | Cloud owner and current gap | Focused acceptance | External completion |
| --- | --- | --- | --- | --- | --- |
| A / `PB-A-FABRIC-DELETE-01` | `cloud_complete` | `P0` | Fabric now converges standalone Gateway Secrets and partial PV/PVC bindings through exact persisted ownership, including restart and retained success readback | Fabric adapter tests inject each single residue, prove exact-owner deletion and final `absent`, and reject unknown/conflict without unrelated mutation | Same-Candidate Tencent/TKE protected deletion readback |
| B / `PB-B-WORKSPACE-RECEIPT-01` | `verify` | `P0` | Control Plane Workspace projection and Launch continuation; source atomically projects the Receipt, confirms identity-valid ready Fabric stages without mutation, and replaces an unused failed Compute continuation through the original key, but no exact-current Candidate proves that path through customer readback | PostgreSQL CAS tests prove first projection, same-Receipt replay, conflicting Receipt rejection, identity mismatch rejection, restart readback, and delayed compute continuation with one ScaleNodePool call and one Receipt | Local and Instance purchase receipts name the same projected Ledger Receipt after delayed compute readiness |
| C / `PB-C-LIFECYCLE-RECONCILERS-01` | `cloud_complete` | `P0` | Control Plane now owns explicit original-period recovery, Runtime suspension/readback and service-authorized background Delete; focused/full local regression is complete; actual deployed qualification remains external | Separate Renewal and Delete tests prove claim/lease/CAS, restart recovery, one debit/provider mutation/Receipt, full Delete absence, and explicit manual review | Instance runs the same Candidate through bounded Renewal and Delete readback |
| D / `PB-D-PUBLIC-REGISTRATION-01` | `next` | `P0` | Control Plane Account and Access plus Console; no public route, durable Registration operation, or Sub2API identity convergence exists | Concurrent same-email requests create one User, Account, and Sub2API identity; response loss resumes without raw password persistence; initial balance is zero and registration performs no purchase/Fabric mutation | Instance exposes the admitted public route behind its ingress |
| E / `PB-E-REGISTER-TOPUP-PURCHASE-01` | `planned` | `P0` | Control Plane admission and settlement; administrator top-up, quote, balance, and Launch exist but are not proven for a self-registered Account | Focused chain proves zero/insufficient balance has zero debit/provider writes, one audited top-up, exact-balance purchase, and idempotent replay without duplicate money/resources | Protected Instance test funds only its bounded beta identity |
| F / `PB-F-TYPED-OPERATIONS-01` | `active` | `P1` | Fabric persistence/provider capabilities, Launch Stage and Runtime-read engines, the Control Plane Launch reconciler, and current multi-step Console lifecycles now have narrow typed owners. Remaining work is limited to live changed Control Plane/Fabric paths that still cross generic map/string boundaries and retirement of Acceptance B after consumer proof | Tighten the next real changed path at its owner; do not split stable single-request Console projections merely to reduce file size | Instance readback proves no configured or non-terminal Acceptance B consumer before removal |
| G / `PB-G-EXACT-CANDIDATE-01` | `next` | `P0` | Cloud Candidate owner; no exact-current clean Local and Tencent/TKE receipt pair exists for one SHA/tree/index digest | Candidate tooling validates canonical manifest, bundle checksums, both platform digests, and one complete clean Local PostgreSQL business journey | `opl-instance-medopl` qualifies the same digest and returns provider/runtime/billing readback |
| H / `PB-H-NODEPOOL-LEASE-01` | `cloud_complete` | `P1` | Fabric capacity; the actual Launch path now uses the durable NodePool FIFO/lease, exact child journal and stale-owner fencing. Cloud admission defaults to fifty and worker execution to four; existing Instance overrides remain unchanged | Local memory/PostgreSQL actual Tencent-adapter tests cover queued head reads, delayed Machine ownership, response loss, restarts and fencing; Control Plane tests cover long queue wait and subsequent provisioning/ownership continuation | Protected Instance must explicitly adopt its desired limit and qualify actual Tencent concurrency; local tests do not prove cloud latency or capacity |
| I / `PB-I-PRODUCT-POLICY-01` | `verify` | `P0` | Control Plane and Console source now agree on Basic/Pro admission and `balance >= quote`; the public-beta chain must preserve that decision | Existing catalog, quote, customer DTO, Console, and server admission focused tests pass unchanged after D/E integration | Instance profile selects only Cloud-admitted plans and cannot override price |
| J / `PB-J-OPERATIONS-RECOVERY-01` | `active` | `P0` | Control Plane Operations and Recovery; identity-valid late owner results across provider profiles, ordinary operator result checks, recoverable Compute ownership, and authoritative initial Storage absence now converge through system-owned original-operation authorizations, while Registration/Reset recovery and unified closure evidence are incomplete | Every supported manual-review operation appears once with owner, blocker and allowed action; recovery is idempotent, owner-read, audited, and cannot write another owner's state | Protected Instance operator executes only Cloud-authorized commands and retains receipts |
| K / `PB-K-DATA-RECOVERY-01` | `planned` | `P0` | Cloud defines schema/migration and restore validation obligations; it does not operate production backups | Cloud restore validator proves Control Plane, Fabric, and Ledger identities, operations, bindings, and receipts after an isolated restore | Instance owns encrypted schedules, retention, RPO/RTO, restore execution, and restore receipt |
| L / `PB-L-ALERT-OPERATIONS-01` | `planned` | `P0` | Cloud service owners expose stable failure/recovery signals; current alerts are mainly process logs and Console projection | Fault tests emit stable active/recovered signals for worker, manual review, database, provider, Ledger, backup-readiness, and purchase-stop states | Instance routes signals to an external receiver and proves acknowledge, stop-purchase, recover, readback, close |
| M / `PB-M-PUBLIC-BOUNDARY-01` | `planned` | `P0` | Control Plane owns auth/session/account lifecycle, Console owns public UI, and portable assets own ingress requirements; public registration abuse and data-exit paths are incomplete | Auth tests prove rate limits, non-enumerating failures, CSRF/session boundaries, disable revocation, no-store secret reads, and operator-assisted recovery/data exit | Instance owns TLS, domains, ingress, production Secrets, and public-browser receipt |
| N / `PB-N-DISTRIBUTION-RELEASE-01` | `verify` | `P0` | Cloud can bind a ten-asset Candidate, Local decision, Instance decision, and `workspace_verified` evidence, then promote the admitted digest without rebuild. The only public Release remains the older five-asset `v0.1.7`; no hosted cohort has proved the current path | One clean Candidate validates domain/Profile/image catalog, Local Workspace use, exact assets/checksums, and same-digest promotion | Instance deploys that exact Candidate, proves new Launch plus existing-Workspace update and rollback, and retains the required receipts |

F is not a preliminary global rewrite. Each live capability tightens its own
types while implementing the business outcome. D3 activated H through the real concurrent Launch caller. Its Cloud-side
serialization is verified locally; Instance concurrency qualification remains external.

## Execution And Test Policy

- Independent Account, Commerce, and Lifecycle owners may develop in parallel
  in separate worktrees with disjoint write sets.
- A shared public contract, schema migration, generated projection, or canonical
  `main` has one writer during its mutation. Candidate builds are scoped to one
  Product SHA; only the public publication job holds the global release lock.
- Each work package closes first with domain, application, persistence, and
  boundary focused tests owned by that capability.
- `npm run verify:local` remains the ordinary merge regression gate.
  `npm run verify:local:full` runs at persistence/cross-module integration
  checkpoints and before Candidate construction, not as a substitute for the
  work package's focused acceptance.
- Cloud can mark G/K/L/M/N only `cloud_complete` while their required Instance
  receipts remain external. Public-beta readiness requires both layers.

## Deferred Product Scope

Customer-operated payment/top-up, shared multi-user Workspaces, customer
Suspend/Resume, HA, GPU, managed-resource policy, project/artifact continuation,
connectors, Package projection, Serve, and shared Runway integration are not
public-beta prerequisites.

## Independent Deferred Integrity Outcomes

| ID | State | Priority | Current gap | Owner | Acceptance |
| --- | --- | --- | --- | --- | --- |
| `FABRIC-OPERATION-HISTORY-01` | `verify` | `P2` | Bounded lookup, heartbeat reuse, and pagination are in source, but the earlier external finding has no fresh sealed scan against current canonical source | Fabric and repository security owner | Focused persistence/HTTP/caller tests pass and a fresh scan no longer reports the operation-history exhaustion path |
| `WORKSPACE-IMAGE-LIFECYCLE-01` | `verify` | `P1` | Cloud and Instance source now implement GC policy reconciliation plus explicit unused-image retirement through CRI; isolated containerd verifies removal, but there is no protected receipt for actual CVM cache or disk-space recovery | Fabric owns the Tencent provisioner path; Instance owns the protected mutation and receipt | An authorized Instance run protects system/current/rollback/stopped references, removes only the specified unused Workspace cache, records per-node absence and independent filesystem usage, and cleans its own maintenance Pods |
| `SECRET-VALIDITY-SETTING-01` | `external_owner` | `P2` | GitHub still reported secret validity checks disabled after an attempted setting change | Repository owner and GitHub feature availability | GitHub readback reports enabled, or the owner records that the feature is unavailable for this repository |

## Disposable Reset Acceptance

The disposable-reset portion of J currently has only a protected read-only
preview. The source does not provide an apply API. Completion must bind an
explicitly disposable Launch, regenerate its exact owner-derived plan, converge resource and Key
absence before exact debit compensation, retain all audit/financial history,
append the reset Receipt and CAS-terminalize the original operation. Unknown,
conflicting or unrelated owner facts must prevent mutation. This is distinct
from normal activated-Workspace Delete. Preview must require schema-valid
`debit/manual_review`, no Workspace projection, exact disposable authority and
no conflicting non-terminal owner operation. Stage position is not absence.
Apply must reject plan drift and use deterministic step identities for response-
loss recovery. Confirmed debit compensation equals the original debit exactly;
unknown debit stops before Key deletion or terminalization. Existing Receipts,
financial history and the original Launch row are retained. Final redacted
readback must prove zero remaining owned resources, Keys or unreconciled debit,
with scope matching the plan. Shared infrastructure and unrelated accounts are
outside this operation.

## Completion Evidence

- Each A-N item records its owner, exact Cloud SHA, focused tests, persistence or
  typed-boundary readback, remaining external receipt, and terminal status.
- Cross-module changes update the owning public contract and both consumers.
- Local and Instance qualification name the exact Candidate SHA and
  multi-architecture digest they exercised.
- Formal publication promotes that digest without a rebuild.
- Money, Secret, persisted-data, provider-resource, and production claims close
  only from their authoritative owner and readback surface.
