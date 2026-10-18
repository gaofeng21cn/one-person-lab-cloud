# Instance Business-Path Retirement Inventory

Owner: `opl-cloud`
Question: which `opl-instance-medopl` workflows and tools still perform or
evidence customer business or business-qualification operations, and what must
move before those paths can retire?
State: open migration, not an executed retirement
Observed: 2026-10-18

- Cloud baseline: `6d5e854a` (`main`,
  [Cloud #929](https://github.com/gaofeng21cn/one-person-lab-cloud/issues/929),
  parent [Cloud #872](https://github.com/gaofeng21cn/one-person-lab-cloud/issues/872)).
  The proposed #928 consumer on `feat/qualification-release-admission` is
  source-only and is summarized in the release-input table below; it has not
  executed a Release.
- Instance baseline: `37141dc9ebbd5acbeedd32e679d3fdc55a9600a4`
  (`feat/launch-cloud-path`, the checked-out
  `/Users/huangrende/Documents/Cloud/opl-instance-medopl` worktree), one commit
  ahead of `origin/main` `2d1dc5aaaca6590300e4f2ac6649052cf0ed0b36`.
  The production Instance entry point remains `origin/main`; the local branch
  only changes the launch caller and does not transfer business authority.
  Instance retirement is tracked by
  [Instance #517](https://github.com/gaofeng21cn/opl-instance-medopl/issues/517).

This inventory is a classification and sequencing record. It does not authorize
workflow dispatch, purchase, deletion, refund, Key mutation, release publication
or production access. It does not remove or relax any gate. The shared
provider-neutral qualification flow and deployed purchase entry are now present
on Cloud `main` (#937/#942), but the protected #926 producer is still absent:
no `.github/workflows/cloud-business-qualification.yml` run or protected Cloud
business qualification receipt exists. The #928 consumer admits that class as a
separate release requirement in source; it does not claim a real qualification.
The transitional Instance gates therefore stay in force.

## Fixed Boundary

Cloud owns customer purchase, renewal, deletion, refund, managed-Key lifecycle,
customer reads and business qualification. `opl-instance-medopl` owns medopl
installation configuration and Secrets, immutable Candidate deployment, health,
rollback and read-only installed-state observation. Installation evidence and
product/business qualification evidence are separate classes and must bind the
same immutable Candidate.

A read-only path is not automatically an Instance path. A path that reads a
customer balance, Workspace lifecycle, managed Key, Package delivery or launch
operation is a business evidence path even when it has no mutation; it belongs
to the Cloud owner that publishes those facts.

## Measurement

At the observed Instance commit there are 41 workflow files:

```sh
cd opl-instance-medopl
git checkout 37141dc9ebbd5acbeedd32e679d3fdc55a9600a4
ls .github/workflows/*.yml | wc -l
# 41
```

The Instance canonical operator inventory marks 11 rows as transitional business
handoffs. They are the measured core of #929, not the whole set of business
reads:

```sh
python3 - <<'PY'
import re
for i, line in enumerate(open("docs/runtime/operator-workflows.md"), 1):
    if "过渡 handoff" in line or "过渡 evidence handoff" in line:
        m = re.search(r"github/workflows/([^)]+)", line)
        if m:
            print(i, m.group(1))
PY
# 49 console-browser-login-readback.yml
# 50 console-customer-workspace-readback.yml
# 51 workspace-billing-settlement-probe.yml
# 52 workspace-application-login-readback.yml
# 53 package-build-delivery-readback.yml
# 62 tenant-admission.yml
# 63 platform-catalog-admission.yml
# 64 cloud-workspace-launch-readback.yml
# 65 workspace-special-chain-acceptance.yml
# 68 workspace-delete-refund.yml
# 72 resume-original-workspace-launch.yml
```

Source review adds four business-semantic paths that the 11-row operator
measurement does not include:

- `qualification-decision.yml` and `verify.yml`, which the
  [Production Runbook](https://github.com/gaofeng21cn/opl-instance-medopl/blob/37141dc9ebbd5acbeedd32e679d3fdc55a9600a4/docs/runtime/production-runbook.md)
  keeps as the current business qualification handoff;
- `workspace-launch-stage-observation.yml` and
  `workspace-operation-attribution-readback.yml`, which read Workspace launch,
  billing, resource and managed-Key operation facts.

The final classification is therefore **15 Cloud business-authority paths** and
**26 retained Instance paths** (24 deployment/read-only plus two bounded
application operations identified in [Open Classifications](#open-classifications)).
The 15 include the 11 measured rows; the additional two scenario gates and two
observation lanes are recorded so that #517's "complete inventory" is not
limited to the operator table.

## Cloud Business Authority

These paths must delegate to the Cloud owner contract or be retired. None may
gain a new business capability in Instance in the meantime.

| Instance entry | Current effect verified from source | Destination and retirement condition |
| --- | --- | --- |
| `tenant-admission.yml` | Writes Tenant, owner membership and repository binding through `POST /api/v2/admin/tenants` (`tools/tenant-admission-readback.mjs:265`). | Cloud business authority (CloudIdentity admin route through the Console BFF). Retire the Instance caller after the protected Cloud producer can provision or assume its declared Tenant without an Instance dispatch. |
| `platform-catalog-admission.yml` | Writes publisher namespace, compute/storage plans, price/refund/retention policies and runtime release through the Cloud admin catalog routes (`tools/platform-catalog-admission.mjs:908`, `:1022`, `:1289`). | Cloud business authority (Resource Catalog admin routes). Retire after the Cloud producer or installation admission supplies the declared catalog without this caller. |
| `cloud-workspace-launch-readback.yml` | Binds the Tenant wallet (`PUT /api/v2/admin/tenants/{id}/wallet-binding`, `tools/cloud-workspace-launch-readback.mjs:883`) and creates one Workspace launch (`POST /api/workspace-launches`, `:1040`). | Cloud purchase authority. Retire only after the #926/#933 replacement producer is executable for the same Candidate and no release input consumes this run. |
| `resume-original-workspace-launch.yml` | Submits one operator resume request (`POST /api/operator/workspace-launches/{id}/resume`, `tools/resume-original-workspace-launch.mjs:418`). | Cloud launch-recovery authority. Retire after the Cloud qualification/recovery flow can resume the same original operation without an Instance caller. |
| `workspace-delete-refund.yml` | Issues one customer Workspace `DELETE` (`tools/workspace-delete-refund.mjs:567`) and reads the platform deletion and refund projections. | Cloud deletion/refund authority. Retire after the Cloud owner path for the same bounded operation is executable and validated. |
| `workspace-special-chain-acceptance.yml` | Reads the purchase/deletion/refund chain and writes deployed application model config, including a declared model-provider key (`POST /api/agent/config`, `tools/workspace-special-chain-acceptance.mjs:893`), and one real application run (`POST /api/runs`, `:921`). It does not buy, deploy, delete, refund or mutate the Gateway/Sub2API managed Key. | Cloud product/business acceptance authority. Retire after the Cloud exact-Candidate acceptance ([#856](https://github.com/gaofeng21cn/one-person-lab-cloud/issues/856)) covers the application journey. |
| `workspace-billing-settlement-probe.yml` | Read-only customer settlements and Sub2API wallet census; only `POST /api/auth/login` (`tools/workspace-billing-settlement-probe.mjs:94`) and `GET /api/billing/workspace-settlements` (`:100`). | Cloud financial evidence authority. Retire after the Cloud financial qualification/readback covers the same customer settlements facts. |
| `console-browser-login-readback.yml` | Read-only customer Console login, Candidate and account check. | Cloud customer-read authority. Retire after the protected Cloud customer readback covers the same login facts. |
| `console-customer-workspace-readback.yml` | Read-only customer Console Workspace list, detail, deployment and access reads. | Cloud customer-read authority. Retire after the Cloud customer readback covers those Workspace reads. |
| `workspace-application-login-readback.yml` | Read-only application login, runtime-credential reveal and user read. | Cloud product/customer-read authority. Retire after the Cloud acceptance covers application login. |
| `package-build-delivery-readback.yml` | Read-only Package -> Capability -> Build -> immutable OCI -> Serve delivery chain for a declared customer package. | Cloud product-delivery read authority. Retire after Cloud qualification/readback covers the delivery chain. |
| `qualification-decision.yml` | Emits the Instance qualification decision/gate receipt and binds a deployment run; no business mutation. | Cloud business-qualification authority. Retire after #928 consumes Cloud business evidence and the release input is removed. |
| `verify.yml` | Produces `verify-admission` and `verify-post-activation`; admission is a read-only probe (`tools/fresh-workspace-admission.mjs probe`, workflow line 476), post-activation reads an existing purchase and emits `workspace_verified` (`tools/fresh-workspace-readback.mjs:495`). | Cloud business-qualification authority. Retire after #928 replaces the workspace run ID and validator. |
| `workspace-launch-stage-observation.yml` | Read-only launch-stage observation; source records `operatorResumeRequests: 0`, `workspacePurchaseRequests: 0`, `providerMutations: 0`, `kubernetesMutations: 0` (`tools/workspace-launch-stage-observation.mjs:411-416`). | Cloud business-read authority. Migrate to the Cloud qualification/readback producer or retire; do not treat it as an Instance deployment probe. |
| `workspace-operation-attribution-readback.yml` | Read-only `SELECT` attribution across `workspace.operations`, `workspace.saga_steps`, `gateway.idempotency_records` and `gateway.key_bindings` (`tools/workspace-operation-attribution.mjs:502-579`; header line 31 says it never writes). | Cloud business-read authority. Migrate to the Cloud qualification/readback or retire with the managed-Key/launch owner evidence. |

## The Five Initially Flagged Writers

Five workflows were initially flagged as verified writers in the #929 working
inventory. Source confirms three mutate Cloud business state. Two are read-only
business evidence paths; calling them writers overstates their effect and would
produce a wrong retirement test.

| Workflow | Source-verified effect | Mutates Cloud business state? | Classification |
| --- | --- | --- | --- |
| `workspace-delete-refund.yml` | `DELETE /api/workspaces/{id}` with the customer session (`tools/workspace-delete-refund.mjs:567`); then reads deletion, receipt and refund projections. | Yes, the platform deletion/refund operation is started once. | Cloud business authority. |
| `resume-original-workspace-launch.yml` | `POST /api/operator/workspace-launches/{id}/resume` (`tools/resume-original-workspace-launch.mjs:418`). | Yes, one bounded resume of the original operation. | Cloud business authority. |
| `cloud-workspace-launch-readback.yml` | `PUT .../wallet-binding` (`:883`) and `POST /api/workspace-launches` (`:1040`). | Yes, wallet binding and one Workspace purchase. | Cloud purchase authority. |
| `workspace-billing-settlement-probe.yml` | `POST /api/auth/login` (`:94`), `GET /api/billing/workspace-settlements` (`:100`), then Sub2API admin reads. | No. The tool header says it never mutates platform or provider state. | Cloud financial evidence authority, read-only. |
| `workspace-launch-stage-observation.yml` | `POST /api/auth/login` (`:84`), `POST /fabric/provider-facts/batch` (a batch read, `:211`), and `kubectl get`/`exec printenv` reads. | No. The receipt's `writes` block is all zero (`:411-416`). | Cloud business-read authority, read-only. |

Two additional writers are not in the initial five-workflow list but are in the measured
11 and must be retired with it: `tenant-admission.yml` and
`platform-catalog-admission.yml`. Their admin `POST` calls are cited in the table
above.

### Script Map

| Instance script | Invoked by | Verified effect |
| --- | --- | --- |
| `tools/tenant-admission-readback.mjs` | `tenant-admission.yml` | Writes Tenant, membership and binding (`POST /api/v2/admin/tenants`, `:264`). |
| `tools/platform-catalog-admission.mjs` | `platform-catalog-admission.yml` | Writes catalog facts (`POST` at `:908`, `:1022`, `:1289`). |
| `tools/cloud-workspace-launch-readback.mjs` | `cloud-workspace-launch-readback.yml` | Wallet binding (`:883`) and Workspace launch (`:1040`). |
| `tools/resume-original-workspace-launch.mjs` | `resume-original-workspace-launch.yml` | Operator resume (`:418`). |
| `tools/workspace-delete-refund.mjs` | `workspace-delete-refund.yml` | Workspace `DELETE` (`:567`) and refund readback. |
| `tools/workspace-special-chain-acceptance.mjs` | `workspace-special-chain-acceptance.yml` | Application model config (`:893`) and one application run (`:921`); no purchase, deployment, deletion, refund or managed-Key mutation. |
| `tools/fresh-workspace-admission.mjs` | `verify.yml` admission phase | Read-only probe invoked at `verify.yml:476`; the admission receipt mutation counts stay zero. |
| `tools/fresh-workspace-readback.mjs` | `verify.yml` post-activation; release admission | Read-only validator; emits `workspace_verified` (`:495`) from existing purchase facts. |
| `tools/qualification-decision.mjs` | `qualification-decision.yml`; release admission | Decision/gate receipt only; no business mutation. |
| `tools/workspace-billing-settlement-probe.mjs` | `workspace-billing-settlement-probe.yml` | Read-only settlements and wallet census (`:94`, `:100`). |
| `tools/workspace-launch-stage-observation.mjs` | `workspace-launch-stage-observation.yml` | Read-only launch-stage observation; zero writes (`:411-416`). |
| `tools/workspace-operation-attribution.mjs` | `workspace-operation-attribution-readback.yml` | Read-only SQL attribution; header line 31 says it never writes. |

## Retained Instance Paths

The remaining 26 workflows stay with Instance as deployment, read-only
installed-state or bounded application operations. They install, deploy, verify,
roll back, observe installed state or maintain deployment artifacts; they do not
decide a customer purchase, refund, deletion, Key lifecycle or qualification.
For the requested three-way classification they are `Instance deployment/read-only
evidence`, except the two bounded application operations
(`workspace-application-install.yml` and `project-explorer-persistence-acceptance.yml`)
that are called out in [Open Classifications](#open-classifications).

| Workflow | Retained Instance duty |
| --- | --- |
| `bootstrap.yml` | Installation; owner databases/roles and base runtime. |
| `cluster-identity-enrollment.yml` | Read-only cluster identity binding. |
| `deploy.yml` | Candidate deployment reconciliation. |
| `deployment-verify.yml` | Independent deployment/image/health verification. |
| `first-install.yml` | Create-only installation of absent owner slices. |
| `gateway-workspace-key-capability-readback.yml` | Read-only installed Gateway key-route capability probe; no customer or Key mutation. Revisit with #868 for managed-Key ownership. |
| `owner-lane-provision.yml` | Owner-lane materials, databases, PKI and network preparation. |
| `preflight.yml` | Read-only environment and capability preflight. |
| `project-explorer-persistence-acceptance.yml` | Application persistence acceptance with bounded conversation/file writes; outside the #929 business set. |
| `rollout-readiness-gate-readback.yml` | Read-only deployment readiness diagnostics. |
| `tcr-repository-observation.yml` | Read-only registry observation. |
| `tke-basic-nodepool-create.yml` | Bounded TKE node-pool infrastructure operation. |
| `validate-instance.yml` | Instance source/CI validation. |
| `webui-recovery-registry-bridge.yml` | Deployment recovery image copy between registries. |
| `workspace-application-install.yml` | Submits a Cloud application-deployment operation for an existing Workspace; deployment-adjacent, outside the #929 business set. |
| `workspace-application-preflight.yml` | Read-only application deployment preflight. |
| `workspace-execution-probe.yml` | Bounded native execution probe with temporary Pod cleanup. |
| `workspace-image-identity-observation.yml` | Read-only image identity observation. |
| `workspace-image-retention.yml` | TCR image retention operation. |
| `workspace-image-rollout.yml` | Workspace image rollout. |
| `workspace-node-image-gc.yml` | Node image garbage collection. |
| `workspace-package-readback.yml` | Read-only installed package state. |
| `workspace-private-state-repair.yml` | Bounded private-state repair. |
| `workspace-runtime-image-rollback.yml` | Runtime image rollback. |
| `workspace-runtime-retirement.yml` | Retire inactive Runtime Deployments. |
| `workspace-state-observation.yml` | Read-only installed Workspace/provider/runtime state. |

`workspace-state-observation.yml` and `workspace-package-readback.yml` read
Workspace/package projections that can look customer-facing. They are retained
as installed-state observation, but if Cloud publishes an installed-state read
contract, those reads should migrate to it. That boundary is recorded in the
open-items section rather than silently treated as settled.

## Release Inputs And Gates

Nothing in this table may be removed or relaxed until the Cloud replacement is
executable and validated. The exact order is: #926 producer executable, #928
consumer validated, then #929 retires the Instance paths and removes their
inputs. The #928 consumer source is now locally validated (the Cloud business
class and the Instance deployment class are separate, Candidate-bound
requirements), but #926 has not produced a protected receipt and no Release has
executed the consumer. A source-only branch, local fixture or documentation
change does not satisfy that order.

| Gate | Current implementation | What it admits | Required successor |
| --- | --- | --- | --- |
| `cloud_business_qualification_run_id` | #928 consumer on `feat/qualification-release-admission`: `release-opl-cloud-image.yml` input/env, exact Cloud run read, artifact requirement, `evidence/business` download, and `tools/cloud-release-evidence.ts` typed validation. | A protected Cloud producer run emitting the #923 `opl.workspace-qualification.purchase.v1` receipt: exact Candidate receipt digest, source SHA/tree, Cloud image digest, terminal state `settled`, exact receipt bytes and producer provenance. The producer workflow `.github/workflows/cloud-business-qualification.yml` is declared but does not exist yet (#926). | **#926 must implement the protected producer and execute it for the exact Candidate.** Do not claim a real qualification or remove any retained Instance gate until that receipt is actually consumed in a Release. |
| `instance_workspace_verification_run_id` | `release-opl-cloud-image.yml:34-37` (input), `:73` (env), `:104` (run read), `:143` (workflow check), `:170` (artifact check), `:275-282` (download), `:471` (validator). | The Instance `verify.yml` `post_activation` run and `verify-post-activation` receipt with `workspace_verified`. | A Cloud business qualification run/receipt ID and a Cloud-owned validator. #928 now admits the Cloud class in source; remove the Instance input and artifact only in #929 after the #926 producer and the #928 consumer actually execute for the same Candidate. |
| `instance_qualification_decision_run_id` | `release-opl-cloud-image.yml:30-33` (input), `:72` (env), `:103` (run read), `:142` (workflow check), `:169` (artifact check), `:266-273` (download), `:463-470` (validator and gate). | The Instance `qualification-decision.yml` decision receipt and its `qualificationRequired` gate. | A Cloud qualification decision/business receipt and a Cloud-owned gate. #928 now admits the Cloud class in source; remove the Instance input and artifact only in #929 after the #926 producer and the #928 consumer actually execute for the same Candidate. |
| `tools/fresh-workspace-readback.mjs` | Instance tool invoked by `release-opl-cloud-image.yml:471` from the exact Instance workspace authority checkout. | The Instance workspace receipt schema and `workspace_verified` status. | A Cloud business receipt validator. Retire the Instance validator together with `verify.yml`. |
| `tools/qualification-decision.mjs` | Instance tool invoked by `release-opl-cloud-image.yml:463-470`. | The Instance decision receipt and its Candidate/deployment binding. | A Cloud qualification gate/validator. Retire with `qualification-decision.yml`. |
| `verify-post-activation` and `opl-cloud-qualification-decision-<run>` artifacts | `release-opl-cloud-image.yml:269` and `:278`; produced by Instance `verify.yml:800` and `qualification-decision.yml:230`. | The two Instance business qualification artifacts. | Cloud business evidence artifacts. The #928 consumer downloads the separate `cloud-business-qualification` artifact and validates it; remove the Instance downloads only in #929 after the #926 producer and #928 consumer execute. |
| `OPL_INSTANCE_EVIDENCE_TOKEN` | `release-opl-cloud-image.yml:262`, `:271`, `:280`; also used for the installation receipt download. | Instance run/artifact reads for installation and (currently) business qualification. | Keep the token limited to `Actions: read` and `Contents: read` for installation/deployment evidence; remove decision/workspace artifact reads. The #928 Cloud business receipt uses the Cloud token and does not broaden this credential. |
| `requiredEvidence` in `packages/contracts/opl-cloud-distribution-contract.json` | Contract schema 6 replaces the flat list with typed classes: `candidate`, `localInstallation`, `cloudBusinessQualification`, `cloudCustomerReads`, `instanceDeployment`, `instanceQualificationDecision`, `instanceWorkspaceVerification`. | The release contract's separate evidence classes. | #928 adds the `cloudBusinessQualification` class and keeps every previous evidence name. In #929, retire the two Instance business-qualification entries only after the #926 producer and #928 consumer execute; keep `instance_deployment_verified` and `cloud_customer_reads_verified`. |
| `tools/validate-product-boundary.mjs` | Asserts the typed `requiredEvidence` classes, the Cloud business producer/receipt/terminal-state contract, the separate Cloud run input, and the two-class admission step; neither class may substitute for the other. | The distribution boundary check. | Keep the class separation; update only with a successor contract. |
| `tests/contracts/product-distribution.test.ts` | Asserts the typed class contract, the Cloud business input, the distinct installation step, and the `validateReleaseAdmissionEvidence` pair step. | Contract and gate shape. | Keep the two-class assertions; extend only when the producer/consumer contract changes. |
| `release-opl-cloud-public-readback.yml` | Line 124 asserts `.qualifications.instance.workspace.status == "workspace_verified"` in the sealed manifest. | The public readback of the sealed release. | #928 adds the separate `qualifications.business` record and provenance `evidenceClasses` without changing the manifest `schemaVersion` or the existing readback assertions; historical manifests remain readable. Do not add a readback requirement that would refuse historical releases. |
| `instance_deployment_verification_run_id` | `release-opl-cloud-image.yml:22-25`, `:70`, `:102`, `:141`, `:168`, `:260-265`. | Independent Instance installation identity and health evidence. | **Keep.** This is the separate deployment class; do not remove it while retiring business qualification. |
| `cloud_customer_readback_run_id` | `release-opl-cloud-image.yml:26-29`, `:71`, `:84`, `:133-134`, `:165`, `:255`, and the protected `cloud-business-readback.yml` consumer. | Cloud `customer_reads_verified` scope only; the receipt explicitly sets `fullBusinessQualification: false`, `workspaceLifecycleVerified: false` and `keyLifecycleVerified: false` (`tools/cloud-business-readback.ts:34`), and its schema version is 2 since the usage and fee-record checks joined the schema-validated check set. | Keep as the customer-read class. It is not a substitute for the #926 business qualification producer or the retained installation evidence. |

The current `docs/runtime/release.md` Publication section says the same thing:
the Instance qualification-decision and Workspace gates remain until full parity
is actually verified. The #928 branch changes the consumer to require the Cloud
business class separately and keeps every retained Instance gate; the sentence
stays true until #929 removes those gates. This inventory does not edit the
release workflow, the distribution contract, the boundary validator or any
test.

## Historical And Immutable Evidence

Do not delete, rewrite or relabel historical evidence. It is not current
authority and must never be presented as a current Cloud qualification receipt.

- Instance tracked receipts under `opl-instance-medopl/receipts/**`, including
  the authorized-purchase, admission, post-activation, deletion/refund and
  state-observation receipts. Examples:
  `receipts/2026-10-08-candidate-55f4266a-authorized-purchase.json`,
  `receipts/2026-10-13-candidate78-state-observation-38425732453.json`,
  `receipts/2026-10-14-deleted-workspace-state-observation-38529859344.json`.
- Historical Instance Actions runs and their artifacts retained by the existing
  retention policy. Expiry or deletion of an artifact is not a rewrite of the
  historical decision.
- Cloud release manifests and provenance already published with
  `.qualifications.instance.workspace.status == "workspace_verified"`. They
  describe the release that consumed them; a later schema change does not
  retroactively reclassify them.
- Cloud `docs/evidence/**`, dated `docs/status.md` receipts and Git history.
  These remain evidence of their original layer and date, not current proof.

No historical receipt is copied into a new Cloud receipt kind or renamed as
Cloud business qualification. A new schema starts a new record.

## Open Classifications

These paths are intentionally not forced into the three buckets by this
inventory. They do not block the conclusion that no active Instance path may
mutate purchase, refund, deletion, managed-Key lifecycle or business
qualification.

- `workspace-application-install.yml` submits a Cloud application-deployment
  operation. It is deployment-adjacent but writes Cloud product application
  state, not customer business state. The target split between "Instance
  deployment" and "Cloud product application lifecycle" is not explicit enough
  to retire it under #929; retain it and decide its owner with the Cloud
  application-deployment entry.
- `project-explorer-persistence-acceptance.yml` creates/removes bounded
  application conversations and files. It is neither read-only nor a Cloud
  business authority. Retain it as an Instance application acceptance outside
  the #929 retirement set.
- `workspace-state-observation.yml` and `workspace-package-readback.yml` are
  read-only installed-state paths whose output includes Workspace/package
  projections. They are retained under Instance read-only observation; migrate
  them if Cloud publishes an installed-state read contract that makes the
  boundary explicit.
- `gateway-workspace-key-capability-readback.yml` is a read-only probe of the
  Gateway key-issuance route. It does not read a customer Key or mutate the Key
  lifecycle, but it is adjacent to the Cloud managed-Key authority. Retain it as
  deployment capability evidence and revisit with #868.

## Reproduce The Source Evidence

Run from a clone at the observed commits. Line numbers are from those commits;
use `git show <commit>:<path>` when a later checkout has moved them.

```sh
cd opl-instance-medopl
git checkout 37141dc9ebbd5acbeedd32e679d3fdc55a9600a4

# 41 workflows
ls .github/workflows/*.yml | wc -l

# The measured 11 transitional rows
grep -n '^| \[.*过渡' docs/runtime/operator-workflows.md

# The three mutating initially flagged writers
grep -n 'method: "DELETE"' tools/workspace-delete-refund.mjs
grep -n '/resume`' tools/resume-original-workspace-launch.mjs
grep -n 'wallet-binding\|POST /api/workspace-launches\|method: "PUT"' tools/cloud-workspace-launch-readback.mjs

# The two initially flagged "writers" that are read-only
grep -n 'method: "POST"\|method: "GET"\|api/billing/workspace-settlements' tools/workspace-billing-settlement-probe.mjs
grep -n 'method: "POST"\|provider-facts/batch\|operatorResumeRequests\|workspacePurchaseRequests\|providerMutations\|kubernetesMutations' tools/workspace-launch-stage-observation.mjs

# The two additional admin writers
grep -n 'admin/tenants' tools/tenant-admission-readback.mjs
grep -n 'admin/catalog' tools/platform-catalog-admission.mjs

# The qualification gate sources
grep -n 'workspace_verified\|MUTATION_COUNTS' tools/fresh-workspace-readback.mjs
grep -n 'gateWorkspaceQualification\|\.github/workflows/qualification-decision.yml' tools/qualification-decision.mjs
grep -n 'verify-admission\|verify-post-activation\|fresh-workspace-admission.mjs\|fresh-workspace-readback.mjs' .github/workflows/verify.yml

# The read-only attribution lane
grep -n 'SELECT\|never writes' tools/workspace-operation-attribution.mjs
```

```sh
cd opl-cloud-release
git checkout 6d5e854a

# Exact release inputs and retained gates on the observed main baseline
grep -n 'instance_workspace_verification_run_id\|instance_qualification_decision_run_id' .github/workflows/release-opl-cloud-image.yml
grep -n 'fresh-workspace-readback\|qualification-decision.mjs' .github/workflows/release-opl-cloud-image.yml
grep -n 'instance_qualification_decision\|instance_workspace_verified\|instance_deployment_verified' packages/contracts/opl-cloud-distribution-contract.json tools/validate-product-boundary.mjs tests/contracts/product-distribution.test.ts
grep -n 'workspace_verified' .github/workflows/release-opl-cloud-public-readback.yml

# The shared qualification flow is present; the protected #926 producer is still absent
test -e tools/workspace-qualification-orchestrator.ts
test -e tools/deployed-workspace-purchase.ts
test ! -e .github/workflows/cloud-business-qualification.yml
grep -n 'fullBusinessQualification: false\|workspaceLifecycleVerified: false\|keyLifecycleVerified: false' tools/cloud-business-readback.ts

# The #928 branch (not on main) adds the separate Cloud business class
git show feat/qualification-release-admission:.github/workflows/release-opl-cloud-image.yml | grep -n 'cloud_business_qualification_run_id\|validateReleaseAdmissionEvidence'
git show feat/qualification-release-admission:packages/contracts/opl-cloud-distribution-contract.json | grep -n 'cloudBusinessQualification\|instanceDeployment'
```

The producer-absence check is expected to succeed at the observed main
baseline, while the #928 consumer exists only on its branch. If the producer
appears, this inventory's sequencing section is stale: re-read #926/#928 before
changing any gate.
