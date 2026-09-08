# OPL Cloud Current Status

Owner: `one-person-lab-cloud`
Purpose: `replaceable_current_evidence_snapshot`
State: `current_snapshot`

This page reports current implementation and the latest retained evidence. It
is not a work log. Target architecture lives in
[architecture.md](./architecture.md); open outcomes live in
[roadmap.md](./roadmap.md).

## Conclusion

OPL Cloud has reached a basically usable administrator-operated stage: the
public Console and health endpoint respond, current source implements the
account, Workspace, provider, billing, and evidence paths, and a protected
medopl run has verified two existing Workspaces through restart, storage,
private-state, and Package readback.

This does not mean Public Beta or a current Product Release is ready. Public
registration, complete Renewal/Delete recovery, alert and restore qualification,
one exact-current Local plus Tencent/TKE Candidate cohort, and same-byte public
promotion remain open. The only public Product Release is the older `v0.1.7`.

## Evidence Matrix

| Layer | Current evidence | What it does not prove |
| --- | --- | --- |
| Audited source baseline | Documentation reconciliation inspected remote `main` at `efb5f07b` on 2026-09-07, including merged Console UX-03 and Support retirement | Deployment, public Release, production acceptance, or a permanently current `main` identity |
| Local Console simplification | On 2026-09-09, the local diff over `b1d811ea` removed unused presentation helpers, route sensitivity metadata and duplicate selected-Key ID state; 39 focused route, API lifecycle, Gateway Usage and logout/Secret tests plus `npm run verify:local` passed | Canonical merge, Candidate qualification or deployment |
| D1 local finance development | The 2026-09-08 worktree on `b1d811eab2a2fcff5d77b4cf9685e801d7681360` passes `verify:local:full` with zero PostgreSQL test skips, 6 HTTP business chains plus 11 reservation/CAS cases under race, and focused Renewal/Wallet recovery race tests | Canonical merge, actual Gateway adoption, a released Candidate, provider capacity, or production financial qualification |
| D2 local reconciliation and customer billing | The retained 2026-09-08 cumulative worktree passes `verify:local:full` with zero PostgreSQL skips; original purchase/renewal/refund HTTP chains, pending and recovery, exact receipt lookup beyond 10k history, historical schema-2 financial readback, race checks and desktop/mobile billing tests pass | Canonical merge, actual Gateway adoption, a deployed Candidate or Tencent/TKE production qualification |
| D3 local Launch recovery and capacity | Provider-neutral original-result recovery, ordinary operator result checks, four-slot keyset scheduling, fifty-account PostgreSQL admission and actual Tencent Launch NodePool FIFO/lease are implemented; focused business, HTTP, restart and race tests pass, as does `verify:local:full`. Final Control Plane source was rechecked with its complete PostgreSQL suite and the explicit capacity test | Full failed-Launch resource/Key/refund closure, actual Tencent cloud capacity, Instance adoption or deployment |
| D1 isolated Sub2API | Cloud's real HTTP client and isolated patched Sub2API pass original-transaction lookup, atomic insufficient-balance rejection, concurrent debit/refund, response-loss recovery and historical unverified-debit rejection | Production Gateway byte identity or adoption; a local patched image is not a published upstream release |
| Public endpoint | On 2026-08-31, `https://cloud.medopl.com/` and `/api/healthz` both returned HTTP `200` | Login, purchase, Workspace lifecycle, provider health, or billing correctness |
| Local runtime | Retained 2026-08-19 Linux/arm64 runs exercised customer-owned and platform-owned Local-Docker Workspace paths, including real model use and restart | One exact-current clean-host create/read/use/delete journey |
| Public Product Release | `v0.1.7`, product SHA `a59bde68397528186a5220f73195fa1f3eda311b`, GHCR digest `sha256:e64504731f8b61c0864cf59faa647a1150e8a2a5eada34b26faf3a5487d28e8f`, five public assets | Current `main`, the current ten-asset Candidate format, or current Instance qualification |
| Medopl Instance | The 2026-08-30 `workspace-private-state-repair` receipt passed for two existing Workspaces and recorded zero-mutation post-repair readback | A fresh Workspace purchase, full lifecycle, rollback, or qualification of current `main` |
| NodePool maintenance | Source implements guarded image-GC configuration, taint recovery and bounded redacted readback | Production mutation; the retained evidence here contains no matching Instance execution receipt |

Evidence applies only to the exact identity and layer named in its row. An older
Candidate or Instance receipt cannot upgrade current source to release or
production-ready status.

The public Release list was read again through the GitHub API on 2026-09-07 and
still contained only `v0.1.7`. Endpoint, Local runtime and Instance rows retain
their original observation dates; this documentation audit did not requalify
those environments.

## Current Product Cut

The current product is administrator-provisioned. One Console user maps to one
Account and one Sub2API identity/wallet; an Account may own multiple independent
Workspaces. Basic and Pro are the visible Workspace packages. Control Plane owns
quotes and purchase eligibility, while Sub2API remains the sole owner of
spendable balance, Keys, routing, and usage.

Console calls Control Plane product APIs. Control Plane, Fabric, and Ledger are
separate processes and PostgreSQL schema owners. Fabric provides Local-Docker
and Tencent/TKE adapters through one provider-neutral boundary. The medopl
Instance selects and configures Tencent/TKE; Cloud does not carry its production
domain, Secrets, or provider profile as defaults.

Public registration, customer-operated payment or top-up, shared multi-user
Workspaces, high availability, and GPU are not current customer capabilities.

The Console customer experience and Support retirement are merged in PR #530.
Current interaction rules belong to the product experience guide, and browser
state boundaries belong to the Console implementation reference. Retained
Support data is historical custody, not an available ticket capability.

## Implemented Capability

- Workspace Launch is a durable Control Plane operation that coordinates the
  Sub2API Key and debit, Fabric stages, Workspace activation, and one Ledger
  purchase Receipt. Exact replay and bounded recovery preserve the original
  identities and fail closed on unproven provider results.
- Workspace Delete is permanent and performs no refund or wallet mutation.
  Source has typed resource observations, but complete Tencent Gateway Secret
  and asymmetric PV/PVC residue convergence remain open.
- Renewal authorization is persisted and exposed through Control Plane and
  Console. Expired-Workspace reactivation and live exactly-once renewal remain
  incomplete.
- Fabric owns compute, storage, attachment, Secret, Runtime, provider mutation,
  and authoritative readback. Local-Docker enforces immutable Workspace images,
  cgroup limits, and project-quota storage on a supported Linux host.
- Tencent/TKE supports protected prepaid provisioning, typed delayed-readiness
  recovery, immutable Workspace image catalogs, active-release selection, and
  image-only replacement of an existing Workspace Runtime.
- New and existing Tencent Workspace NodePools have a source-owned, explicit
  image-GC threshold path. Existing pools are changed only through a separately
  confirmed mutation with owner inventory and post-mutation readback.
- Console uses capability-specific controllers for Workspace Launch, access
  Secrets, Delete, Renewal, Gateway budget and usage, customer and operator
  reads, billing/Receipts, Wallet adjustment, and announcements. Customer
  navigation and presentation are task-oriented; the retired Support client and
  its live requests are absent.
- Ledger owns append-only receipts, reconciliation evidence, and the Cloud
  Evidence Index. It does not own spendable balance or provider mutation.
- Candidate tooling builds one `linux/amd64` plus `linux/arm64` image index and
  one checksum-bound ten-asset installation bundle from an exact Cloud SHA.

The detailed dependency and operation boundaries remain in
[implementation-architecture.md](./implementation-architecture.md).

## Retained Runtime Evidence

The D1 local finance evidence is retained outside Git at
`/Users/huangrende/Documents/ChatGPT/d1-delivery-20260908/VALIDATION.md` and its
`logs/` and `sub2api/` artifacts. The business chains use real Control Plane,
HTTP clients, PostgreSQL and Ledger, with explicit Sub2API/Fabric fixtures.
They exercise concurrent partial refunds, account/amount conflicts, response
loss, restart and receipt-only recovery. Isolated real Sub2API evidence is a
separate layer; its applied-amount patch must precede Cloud adoption. Historical
transactions without a verified applied amount remain unverified. The actual
production Gateway image identity and adoption are still an Instance obligation.

D2 local source evidence is retained separately at
`/Users/huangrende/Documents/ChatGPT/d2-delivery-20260908/VALIDATION.md`, with
`cloud.patch`, `source-manifest.json` and `logs/`. Its snapshot includes the
retained D1 changes; it does not overwrite the D1 snapshot. Tests prove two
purchases plus partial refunds, all paid renewal periods after Workspace
removal, missing/conflicting/duplicate receipts, normal receipt progress without
blocking new buyers, manual-review refund recovery, automatic refund accounting,
and schema-2 historical readback. Ledger migration and exact lookup pass with
10,005 retained receipts. The final full local run passes PostgreSQL and Docker
integration with no required skips. Tests use separate local stores and fixtures;
engineering evidence is not written into the production business Ledger.

D3 evidence is retained separately at
`/Users/huangrende/Documents/ChatGPT/d3-delivery-20260908/VALIDATION.md` and its
source manifest, patch and logs. The source baseline is local commit
`0a1a78d6aa2caf4898c1e35c08c30c2a906f371b`, which preserves the verified D1/D2
work before D3. Tests include fifty concurrent PostgreSQL admissions, the 51st
rejection, 10,001 unrelated retained operations, keyset ties, one slow account
while 49 others advance, first-dispatch CAS competition, and late exact debit
confirmation after restart on both provider profiles. Real typed HTTP tests
cover long queue wait, original dispatch, provisioning, ownership and stage
advance. Actual Tencent adapter tests use isolated provider fixtures and
PostgreSQL, not live Tencent resources. The full local gate passes with zero required PostgreSQL skips. After the final
Control Plane recovery edits, its complete suite passes again (2,380 test events
plus the explicitly rerun opt-in 1,000-user data-scale case); focused recovery
and concurrency tests pass under race. Browser tests cover desktop/mobile
original-order return and ordinary administrator recovery without technical
budgets. Test stores and evidence remain outside production state.

D3 remains active for full failed-Launch closure. Fabric partial resource
cleanup does not by itself close Key and financial obligations: current Key
mutation requires a short-lived customer delegated credential, and normal
Workspace Delete requires an immutable succeeded Launch. No new terminal refund
or cleanup shortcut was added. Exact owner-authorized closure is the remaining
business development work, distinct from deployment qualification. The existing
Tencent Instance explicitly enables the Launch worker and still overrides
admission to one; adopting the new source does not silently change that setting.

The 2026-08-19 Local-Docker runs covered two ownership modes:

- `customer_owned` created two independent Workspaces, retained them across
  control-service restart, and completed a real model request with one matching
  Sub2API usage increment;
- `platform_owned` created one prepaid Workspace, one Workspace Key, one
  `52,580,000` USD-micros debit, and one linked purchase Receipt. Runtime repair
  retained the confirmed Key, debit, compute, storage, attachment, and Secret,
  and exact replay did not duplicate resources or the Receipt.

The retained evidence is outside Git under
`/Users/huangrende/Desktop/opl-cloud/evidence/2026-08-18-v2` and
`/Users/huangrende/Desktop/opl-cloud/evidence/2026-08-19-platform-owned-repair`.
These runs did not prove final Delete absence and its Receipt on one
exact-current clean host.

The Instance receipt
`opl-instance-medopl/receipts/2026-08-30-workspace-private-state-repair.json`
binds product SHA `eaa1a95bbdc587b5cd49d38fbc0395013821331a`, Cloud image digest
`sha256:625c47b32def08ba3519a5ecb7dddb475a42bd8f082ad691b3c0d45d0f6b784b`,
and its Workspace image digest. It verifies two retained Workspace storage
bindings, private Framework state modes, Workspace image binding, a second
restart, all seven official Packages installed, five professional Packages
launchable, and five shortcuts visible. Its later readback made no Control
Plane, Kubernetes, Package, or Workspace-restart mutation. It is strong
evidence for those existing
Workspaces, not for a new purchase or current-source qualification.

## Distribution Boundary

The five `v0.1.7` assets and their public API digests match the Release manifest
and `SHA256SUMS`. The Release contains the base Compose file and its historical
Local-Docker overlay, but not the current three deployment overlays, two Fabric
overlays, or current complete Workspace installation contract. See
[installation.md](./installation.md) for the executable boundary.

Current source separates Candidate construction, Local qualification, Instance
qualification, publication, and public readback, and is designed to promote the
qualified image digest without rebuilding it. No hosted cohort has completed
that path for one current Candidate, so a successor Product Release is not yet
proven.

## Readiness

"Basically usable" describes the presently demonstrated administrator-operated
surface. Public Beta requires the Cloud and Instance evidence named by the A-N
work packages in [roadmap.md](./roadmap.md). In particular, current evidence
does not yet close public registration, complete lifecycle recovery, data
restore and alert operations, clean exact-Candidate qualification, rollback,
or same-byte publication.
