# OPL Cloud Roadmap And Current Gaps

Owner: OPL Cloud
Purpose: open outcomes and acceptance
State: active

This page contains open outcomes and their completion evidence. It does not
repeat historical execution logs or act as a second status ledger. The current
source snapshot is [status.md](status.md); target ownership is
[architecture.md](architecture.md); durable choices are [decisions.md](decisions.md).

## SSOT Reading Order

Use the following order when a source, document and receipt disagree:

1. The latest durable decision and its canonical owner define the target.
2. Source, contracts and focused tests define Cloud implementation facts.
3. [status.md](status.md) records the current evidence boundary.
4. This page records the remaining gap, owner and acceptance evidence.
5. Candidate, Instance and production claims require the external owner
   readback named by the row.

A passing test, generated contract, merged PR or queued workflow proves only its
own layer. It does not close an Instance or production outcome.

## PR CI Cache Reuse And Parallel Verification

Cloud development tools own this outcome. #970 merged the grouped exhaustive
checks and the separate main cache producer. Current-revision PR CI and the
first successful main producer are recorded in
[status](status.md#pr-ci-cache-reuse-and-parallel-verification-2026-10-18).
#971's first run proved consumption of all four main-cache keys from a new PR
scope with unchanged executed coverage. Its subsequent revision is bound to
its current-revision CI. Dependency-change executions and sustained timing/cost
sampling remain open: record executed coverage, cache hits, per-shard elapsed
time and cumulative runner duration. The observed parallel runs shortened the
critical path while increasing runner duration relative to the adjacent
baseline; unequal browser workloads remain visible. A same-PR warm rerun does
not replace the new-PR proof. Keep every mandatory result blocking `validate`
on failure, cancellation or skipping.

## Current Target

The reusable Cloud product has two application combinations:

- **Default OPL App**: an approved Runtime Release plus its declared built-in UI.
- **Custom Agent**: an admitted Package, an approved Runtime Release and a
  compatible independent WebUI, fixed into one immutable OCI.

Workspace owns identity, entitlement, resource intent and model-configuration
intent. Gateway Integration coordinates the Cloud-to-Gateway mapping, approved
Group commands, external Key creation or rotation, lifecycle disable/enable and
Secret delivery. `gateway.medopl.com`/Sub2API remains the effective authority
for Key, Group, wallet, routing and usage facts. Fabric owns provider resources
and Secret bindings. Serve owns application deployment, readiness, route and
runtime apply/readback. Ledger records evidence. Control Plane remains only
where a real caller has not yet moved.

The current product target is an exact Cloud Candidate deployed and qualified
through the managed TKE Instance path, with Local-Docker remaining a supported
source qualification profile. The target is not complete until a user can open
the confirmed application route and the exact bytes, owner facts and receipts
can be read back.

## Custom Agent Local Delivery (2026-10-21)

[PR #1003](https://github.com/gaofeng21cn/one-person-lab-cloud/pull/1003)
is limited to the IBD Local-Docker delivery closure: uploaded Package → real
BuildKit OCI digest → CapabilityVersion → Workspace/Serve/Fabric Local-Docker →
ready route, password login and a real Agent response through the authenticated
native HTTP API. IBD is the feasibility exemplar. The exact historical runtime
evidence and its source/fixture boundaries are owned by
[status](status.md#linux-custom-agent-owner-qualification-2026-10-21);
[Issue #1002](https://github.com/gaofeng21cn/one-person-lab-cloud/issues/1002)
tracks the scoped source closure and current-revision review/CI. Generic WebUI
chat changes, browser-driven sending and a new provider are outside this scope.

Same-Candidate and installed Tencent/TKE acceptance remain open. The local
`LOCAL_NO_CHARGE` order did not activate a paid subscription: after positive
checks, customer deletion was refused with 422. Real resource absence, Ledger
deletion receipt and original-charge refund still require their original paid
Workspace and separately authorized owner execution. The test's cleanup closes
only its disposable fixtures. This outcome adds no provider or deletion bypass.

## Independent Agent WebUI Delivery

The independent WebUI now uses the real Runtime native Agent directory/message
protocol. Its isolated IBD browser demo passed native login and two real,
canonical completed turns on one thread; the standalone WebUI OCI was built and
read back locally. [Status](status.md#independent-agent-webui-local-demo-2026-10-21)
owns the bound inputs and limits. This is separate from the historical #1003
HTTP API delivery scope.

Still open: publish the immutable WebUI artifact, admit compatible Package-build
Runtime/WebUI inputs and their effective owner policy, build the final Agent OCI,
and validate that exact artifact through the Tencent Workspace/Serve/Fabric lane.
The existing Instance catalog-admission workflow has no WebUI/Build-policy phase;
any required protected operation entry belongs to Instance. Reuse the installed
Cloud Candidate when its product bytes are unchanged. Resource procurement,
business deletion, Ledger deletion receipt and original-charge refund retain
their existing authorization and evidence boundaries.

## Team Financial Read Delivery (2026-10-16)

The Gateway/BFF transaction list, transaction detail and 14-UTC-day confirmed
charge/refund trend are
implemented for team owners/admins. Original wallet records remain the source;
there is no new wallet or relaxation of Ledger's actor fence. The UI preserves
unknown/requested states and exact amounts. Exact-Candidate installed access and
real historical-data reconciliation remain in the customer acceptance outcome;
source and fixture evidence do not close that outcome.

## Priority Outcomes

### First Tencent/TKE Default App Launch (2026-10-10)

The [October 10 decision](decisions.md#2026-10-10-first-tencenttke-launch-without-cloud-model-configuration)
narrows the first launch: Cloud Console model selection/update is not a release
gate. Reuse the existing published default App OCI at its verified digest; retain
authentication, tenant isolation, Gateway credentials, data safety, actual message
execution, money reconciliation and exact deployment evidence. Runtime publication
may omit undeclared model/Build capabilities; source consumers and Instance admission
must adopt the approved schema without inventing defaults. Custom Agent Build still
requires its original recipe/format contracts and has separate acceptance.

Consume the existing Ingress repair, Candidate and binding receipts at their actual
evidence layers. First implement only the affected admission/launch/UI seams, then
integrate an exact branch SHA and build a Candidate if Cloud bytes changed. The
Instance owner adopts the exact schema digest and proves deployment of those
bytes; Cloud's product owners retain business qualification. The current
protected execution and release-evidence migration is tracked below. Finally
resume the original operation and verify ready URL, App login, one real answer,
original order/charge and Gateway/Ledger readback under the existing authorization
limits. Unchanged checks are reused; changed boundaries receive focused tests. Do
not start a new App release for the deferred model-management interface or repeat
a Local full run by default. The broader table below is not the first-launch gate.

The implementation handoff is complete only when each affected owner records its
exact input SHA/contract digest, executed red/green checks, output/readback, upstream
receipt references and the next consumer/command. This prose/schema change is not
a deployed Candidate or final business PASS.

| Priority | Outcome | Cloud owner | Current state | Completion evidence |
| --- | --- | --- | --- | --- |
| P0 | Move live model-configuration callers to Workspace/Gateway/Fabric/Serve | Workspace, Console BFF/UI, Control Plane migration | Owner-separated source path is implemented; the BFF/UI model-configuration routes already read and write through the Workspace owner, so the first completion clause is met and this row stays open only for the old writer's remaining callers and owner/browser acceptance | BFF/UI uses the Workspace owner for read/write, old writer has no configured caller, owner and browser tests pass |
| P0 | Complete live Gateway mapping, Group management and Workspace Key rotation | Gateway Integration plus external Gateway | Cloud now routes Workspace Key creation through the logged-in Gateway user's existing `/api/v1/groups/available` and `/api/v1/keys` APIs, carries the operation idempotency key, stores only external Key identity/fingerprint/Secret reference, and leaves effective routing to the external Group. Group administration, key rotation/lifecycle closeout and deployed external readback remain open. Cloud local tests cover the adapter contract; they do not establish production Gateway state | Prove the deployed Cloud login-to-Gateway mapping, existing `Codex` Group readback, one Workspace Key creation and Secret/config handover, retry replay without a second Key, rotation/old-Key closeout and no raw Key in persistence or receipts |
| P0 | Finish Serve typed application execution and route activation | Serve; Instance owns ingress/DNS/TLS | Source route owner and model apply/readback exist; initial dependency progression and Workspace retirement intent have owner and executor boundary tests; owner-local conditional route CAS is implemented; typed Fabric application RPC and installation-level Ingress/route-object CAS remain | Typed execution contract, conditional route CAS, real TKE Service/readiness/access request and lost-response recovery |
| P0 | Close the Workspace deletion through the Ledger receipt to the refund | Workspace and Ledger | Ledger now admits the confirmed-deletion provider-action receipt into its own non-financial writer and resolves it back through `ReadReceiptByReference`, so the structural refusal that blocked the closure is gone at source over real gRPC and PostgreSQL. Workspace now limits consecutive unconfirmed automatic passes of one deletion step to twelve, then leaves the operation at needs_attention with its original unknown/rejected observation; a newly authorized acknowledged delete replay resumes that same operation. After a pending Fabric read it replays the frozen original DeleteResources command, because ReadResources alone cannot advance Fabric. Isolated PostgreSQL tests cover the actual Workspace worker/caller with synthetic owner clients; the full Workspace-to-real-Ledger-to-Gateway closure and provider adoption remain unproven | One owner-authoritative end-to-end deletion that reaches the confirmed refund against the recorded absence receipt; source retry/recovery bounds are delivered, not production absence or refund acceptance |
| P0 | Repair the Resource Catalog refund policy list encoding defect | Resource Catalog | `scanRefundPolicy` normalized neither separator when it mapped the stored hyphenated algorithm, so the required `algorithm` property silently became the unspecified member and the public encoder refused the administrator list page once a row existed; the source is repaired and the focused list-read test over a stored hyphenated algorithm passes, while the repaired source has reached the verified `78dbe9b4` Instance Candidate but the administrator list page still needs a fresh owner-authoritative readback | [defect source check](evidence/source-checks/2026-10-08-refund-policy-list-enum-encoding-defect.json); [repair source check](evidence/source-checks/2026-10-09-refund-policy-algorithm-enum-encoding.json); the administrator refund policy list reads back at 200 on a Candidate built from the repaired source |
| P0 | Complete runtime SecretInput delivery | Serve, Fabric and contract owners | Gateway-managed input is covered; other declared runtime inputs are refused | Approved opaque binding/version delivery through RuntimeDeployCommand, application authorization and readback tests |
| P0 | Build one exact Candidate and install the complete owner topology | Cloud release owner; Instance deployment owner | The `78dbe9b4` Candidate has a verified ten-owner deployment and twelve passing checks; newer `d430825c` has local fixture READY evidence but no Instance adoption evidence in this snapshot | Exact SHA/tree/index/digest manifest, complete owner deployment, protected health/readiness and route readback |
| P0 | Qualify Default App and Custom Agent on the same Candidate | Serve, Workspace, Instance | Local owner journeys exist; no protected TKE user acceptance | One confirmation reaches a ready application in each mode, restart and response-loss recovery, exact image and route evidence |
| P1 | Retire retained Control Plane application route and duplicate DTOs | Control Plane migration owner and Serve | Retained route/proxy remains for un-migrated callers | Caller inventory is empty or explicitly historical, old route writer and current-selection projection are removed |
| P1 | Public registration, administrator top-up and controlled purchase | Control Plane, CloudIdentity, Console | Existing operator/admin path exists; public self-service chain is incomplete | Concurrent registration, zero initial balance, one audited top-up, quote/purchase idempotency and protected browser readback |
| P1 | Backup, restore, alert and data-exit contracts | Cloud owners; Instance operates them | Owner boundaries are described; production schedules and receivers are external | Isolated restore identity/readback, stable active/recovered signals, stop-purchase boundary and export/delete receipts |
| P1 | MAP domain contracts and evaluation gate | MAP medical domain owner/Instance | Cloud logical Connect boundary is documented; no clinical domain or qualified MAP adapter/backend combination is claimed | MAP task/adapter contract, selected OPL Connect backend, approved data classes, source/evidence model, human review states, evaluation set, release gate and institution acceptance; no medical-specific Cloud adapter is required |
| P2 | Customer-operated payment, multi-user Workspace, HA/GPU and broader provider profiles | Product and Instance owners | Deliberately outside the current beta cut | Separate product decision, owner and qualification evidence before admission |

## Workspace Key And Customer Read Boundaries

The [October 18 Gateway external truth and rotation decision](decisions.md#2026-10-18-gateway-external-truth-cloud-mapping-and-workspace-key-rotation)
settles the current product intent; the October 15 lifecycle decision remains
historical where it conflicts. Each source slice records its own evidence; source adoption
does not qualify a deployed Candidate. Existing API/protobuf schemas and release
validators describe their still-consumed implementation boundary; upgrade them
with their real callers, tests and evidence producers rather than relabeling
prose as a delivered interface.

### Gateway 外部契约输入清单（2026-10-18）

Cloud 已确认 Gateway 不改代码；新的 Cloud 实现切片复用登录时取得的
Gateway 用户凭证和现有 Gateway 用户 API。下面的清单保留为实现验收边界，
不预先命名不存在的 endpoint、RPC、字段或权限。

| 输入 | 必须确认的事实 | Cloud 后续消费者 |
| --- | --- | --- |
| Login credential | Cloud 登录如何获得 Gateway 用户 bearer；它如何绑定 Cloud session、Cloud actor 和 external subject；凭证只保存在进程内并在 session/Gateway TTL 到期时失效 | Gateway Integration user-scoped Key client |
| Group administration identity | 若需要创建或修改 Group，使用哪个已有 Gateway administrator identity 和现有 admin API；普通用户登录不自动获得该权限 | Gateway Integration Group command；Workspace 开通只要求已有可见的 `Codex` Group |
| External subject mapping | Cloud User/Workspace 如何对应 Gateway User/subject；创建、查询、归属校验和已存在映射的读回 | Gateway identity mapping、Workspace 开通和重试收敛 |
| Group command | 如何查询或创建默认 `Codex` Group；Cloud 被允许管理哪些 Group 属性；返回的 Group ID、版本/状态和幂等规则 | Workspace 开通、Group 管理命令和 Gateway 真相投影 |
| Key creation | 如何为指定 external subject 创建 Workspace Key；请求中的 Workspace、Group、用途、模型范围和幂等身份；成功响应中的 external Key ID、有效 Group/范围和一次性 raw Key 交付边界 | Gateway Integration mapping、Secret delivery 和 Runtime handover |
| Key readback | 如何按 external Key ID 或原始命令身份读回有效 Key、Group、状态和模型范围；普通读回不得返回 raw Key | Cloud projection、重试、未知结果收敛和 Runtime 验证 |
| Key rotation | 复用当前 Key 与创建新 Key 的判定；新旧 Key 的交接顺序；旧 Key 的明确收尾动作、授权和读回 | Workspace 配置变更、Secret 替换和 predecessor mapping |
| Failure semantics | 拒绝、超时、响应丢失、重复命令、部分成功分别如何编码；原始幂等命令如何查询，不得通过重试重复创建 Key | Gateway operation state、Workspace recovery 和审计证据 |
| Secret delivery | raw Key 是否只在受控 Secret/config 写入响应中出现；谁读取、谁写入、如何确认 fingerprint/version；普通 Cloud DB、日志、receipt、DTO 均不得保存 raw Key | Fabric Secret binding、Serve/Runtime config readback |
| Usage and routing readback | Key/Group 的有效路由、usage、quota 等事实如何通过 Gateway 读取；不可用或冲突时的错误语义 | `/console/api/keys`、usage、groups 和 Workspace status projection |
| Version and deployment evidence | 契约对应的 Sub2API commit/image/deployment、启用的 service identity、真实 Gateway readback 和可重复的隔离测试证据 | `docs/status.md`、CI/qualification evidence 和后续 Issue acceptance |

当前源码能证明的是用户 JWT 下的 `/api/v1/keys` CRUD、用户可用 Group
读取和管理员 Group CRUD；Cloud 复用这些已有路径，不把用户凭证提升为服务
凭证，也不把本地 binding 当作外部 Key 或 Group 真相。`/api/v1/service/workspace-keys`
继续视为废弃假设，不进入实现。

| Outcome | Owner | Remaining adoption and acceptance |
| --- | --- | --- |
| Workspace-managed Key lifecycle (#821, #868) | Workspace and Gateway Integration; Sub2API external authority | #868 is closed as not planned for the current delivery scope. The existing Cloud lifecycle source wiring is retained as historical implementation evidence, not as a reason to add Gateway disable/enable/state-readback APIs or migrate callers now. Key rotation and the Workspace opening path remain governed by the current Gateway mapping contract above. |
| Workspace Key mapping and rotation | Workspace, Gateway Integration, external Gateway, Fabric and Serve | The former same-Key source slice remains historical evidence. The current target allows Key reuse or rotation: Gateway creates or manages the effective Key/Group, Cloud records the opaque mapping, Fabric/Serve deliver and read back the new Secret/config, and Gateway closes the predecessor when authorized. The retained Control Plane Workspace caller still needs migration and the external path needs deployed readback. | One authenticated Workspace opening and one rotation read back the same Gateway-owned facts through Gateway and Cloud, with no duplicate operation, leaked raw Key or local projection treated as external truth. |
| Personal and Tenant financial reads (#840, #870) | Gateway Integration and retained financial owner; Console BFF/UI | Keep wallet/Token reads on the logged-in User. Tenant owner/admin read Tenant bills; original owners read proven historical bills without automatic admin inheritance. Personal history now has a typed BFF/original-owner list, detail and trend reader with live identity, reciprocal Account/User proof and original-order projections. Local original-history and isolation checks are in [status](status.md#personal-historical-billing-source-repair-2026-10-16). Team transaction list/detail and confirmed charge/refund trend are now implemented through Gateway/BFF under the live Tenant owner/admin role. Ledger now exposes original funded-charge receipt/accepted-quote details under the current Tenant role without exposing private execution evidence. Missing receipt kinds remain explicitly unavailable; no refund receipt is invented from a Gateway reference. Installed exact-Candidate acceptance remains unproved; never derive Tenant financial authority from the personal-history path. |
| Announcements and historical ACKs (#840, #871) | Existing announcement owner, CloudIdentity and Console BFF/UI | The source now routes Cloud list/ACK through the BFF to the original owner, revalidates live identity, and folds proven historical ACKs with original timestamps. Local owner, PostgreSQL, BFF and browser checks are recorded in [status](status.md#cloud-announcement-source-repair-2026-10-15). Source CI and Instance wiring source are delivered in [Cloud PR 882](https://github.com/gaofeng21cn/one-person-lab-cloud/pull/882) and [Instance PR 505](https://github.com/gaofeng21cn/opl-instance-medopl/pull/505). Remaining: protected peer/token/certificate/topology adoption and authenticated same-Candidate acceptance; #840 retains its other reads. |
| Remaining retained Gateway reads (#840) | Gateway Integration and Console BFF/UI | Keys list/detail, per-Key usage/summary, groups, personal balance history and endpoint now use session-bound Cloud BFF/Gateway reads with a live CloudIdentity proof; the retained `/api/gateway/*` adapters remain legacy-only, and Gateway custody of the delegated provider credential is still transitional. The legacy package pricing catalog and preview have no Cloud owner yet, so a cloud build refuses them explicitly instead of calling the retained route. The protected `customer_reads` producer now asserts wallet, usage summary, fee records and announcements in one session; no installed run is claimed. Reproduce each actual authenticated caller before changing it; do not treat recharge records as complete balance history or claim the parent is fully covered. The caller/owner inventory is [Console customer read inventory (#840)](implementation/840-console-customer-reads.md). |
| First managed-TKE customer journey (#856) | Cloud product owners; Instance supplies deployment readback | The first-use outcome is separate from formal Product Release qualification. Prove one deployed revision can authenticate the designated customer, complete the authorized Workspace opening, reach a ready runtime, return one real application response, preserve operation identity across response loss/retry, and reconcile the original order, charge and Gateway/Ledger facts. A successful first-use revision may be recorded before it is selected for formal Release. |
| Formal protected qualification and Release admission (#926, #928) | Cloud release/product owners; Instance supplies the protected execution and deployment evidence | Post-first-use hardening. The Cloud-owned producer, fabric J0 prerequisite and typed release consumer now bind the protected Instance workflow and its exact artifact; schema 7 reads the run with the Instance evidence token and validates `product-source.json` against the released Cloud source ([status](status.md#instance-hosted-business-qualification-consumer-binding-2026-10-19)). The remaining work is the declared protected environment inputs, a real sandbox J0 receipt, the settled exact-Candidate business receipt and formal Release readback. This does not block #856. |
| Transitional Instance business workflows (#517) | Cloud and Instance owners | Post-first-use architecture cleanup. The current 42-workflow inventory classifies 15 transitional Cloud business-authority paths, 27 retained Instance paths and the protected qualification executor; retire callers only after their successors execute while preserving deployment, rollback, Secret custody and read-only installed-state evidence. This does not block #856. |
| Cloud customer-read coverage (#840) | Gateway Integration, retained owners and Console BFF/UI | Post-launch product-read completeness. Finish the remaining caller inventory, identity/persistence ownership and protected readback for the selected customer-read surfaces. This does not block #856. |

## Development Governance Migration (2026-10-13)

Cloud development tooling owns adoption of the
[ordinary source development decision](decisions.md#2026-10-13-ordinary-source-development-uses-git-scope-and-current-ci).
Local changes reconcile host admission, verification selection, CI and
contributor instructions with the adopted default. Their current-revision CI
and merge are still pending. Ordinary PRs
use actual local results and current-revision CI without a separate source-check
JSON, and independent agents may perform their technical review; high-consequence
operations retain owner review and authorization.

Completion requires the actual entry points, focused checks, CI, `AGENTS.md`,
`DEV_GUIDE.md` and PR template to agree with the adopted rules. Routine source
maintenance must work without a restricted-host admission, while explicitly
admitted host runs retain scope enforcement and actual execution evidence.
Verification must exercise both paths and preserve Candidate and Instance
evidence requirements. Unfinished migration outcomes remain tracked by their
canonical owners rather than disappearing when execution-plan coupling is removed.

## Clean Host Qualification Recovery (2026-10-13)

Cloud qualification tooling owns #793's PostgreSQL scratch cleanup and #787's
image/launch verification path. Merged #801 owns teardown and Workspace-fixture
admission while preserving the supplied Cloud digest; #804 preserves bounded
DELETE failure diagnostics.

#793's harness acceptance is complete: exact Candidate
`78dbe9b474e6b614ca0def565fd2ab60cdb37e4e` / Cloud digest
`sha256:05992806bf27789adfc0956c2df5bdf10d8e127c3b0641430cf38028e8f6e987`
passed Clean Host `38420381927`, including owner DELETE, Ledger evidence and
zero resource residuals. The original, validator-accepted READY receipt is
preserved in [status](status.md#verified-clean-host-qualification).

#787 remains open for owner disposition of the patrol's separate original launch
HTTP 502. The current immutable-Candidate/run-owned-fixture path is delivered
and has valid READY evidence. The exact old-input replay reached the known
pre-launch cleanup failure before login or launch, so it cannot identify the
historical owner or stage. The current immutable-Candidate/run-owned-fixture path is delivered and has valid
READY evidence. Canonical App, live Sub2API, TKE and original paid-order
acceptance remain separate product outcomes; they do not enlarge this issue's
local qualification scope. Earlier generic DELETE errors were not reproduced
in the successful runs; improved diagnostics do not establish their historical
runtime cause.

## Cloud-Identity Customer Workspace Read (2026-09-30)

The cloud Console identity reads Workspaces from the Workspace owner through the
BFF. The remaining gap is the owner-backed maintenance surface: renewal,
deletion, credential rotation and application-installation actions must be
implemented by their owning service and then projected by the BFF/UI. The
legacy identity remains on the retained Control Plane path until its callers
move.

Evidence: [cloud Console owner read](evidence/source-checks/2026-09-30-cloud-console-workspace-owner-read.json).

## Workspace Application Decoupling

The decoupling outcome is complete only when the target owner chain carries
real callers:

    Workspace entitlement and model intent
      -> Gateway managed key
      -> Fabric Secret binding/rebinding
      -> Serve execution, readiness and route
      -> Console BFF/UI readback

The current source contains a historical same-Key model-update path, Serve
publisher apply/readback and a stable access owner. The current product target
allows Gateway Key reuse or rotation. Remaining work is caller migration to the
Gateway mapping path, external Gateway Key/Group/rotation readback, typed
execution/route activation and protected Instance qualification. The retained
Control Plane proxy remains a migration source until those callers have moved.

The medical platform does not require a medical-specific Cloud adapter service.
Its institution-system integration follows MAP: a callable Agent owns the
TaskEngine, semantic adapters, site profile and clinical workflow, while the
logical OPL Connect contract selects a qualified backend such as
`glkvm-native`. Cloud supplies the runtime and recovery primitives that MAP may
use. The adapter/backend combination is an Instance/MAP qualification outcome,
not a new Cloud service or P0 platform dependency.

### Required Deliverables

A completion package must contain:

- the exact Cloud source SHA, tree digest and Candidate image index/digests;
- the owner topology and baked Console identity consumed by the Instance;
- focused Cloud source and browser receipts for both product combinations;
- protected Instance deployment, health, route, image and rollback readback;
- Gateway/Sub2API issuer, usage and revocation evidence without raw secrets;
- runtime model apply/readback and unknown-result recovery evidence;
- the medical domain package/evaluation/clinical-review evidence when the
  medical platform is included.

A source receipt or a Candidate manifest without the corresponding Instance
readback leaves the outcome open.

### Implementation Sequence

1. Finish the typed contracts and generated consumers for Serve execution,
   route activation and runtime SecretInput delivery.
2. Move Console BFF/UI model and Workspace maintenance callers to their owner
   services; remove the old writer only after readback proves no live caller.
3. Resolve the live Gateway/Sub2API issuer and usage boundary in the approved
   owner workflow.
4. Build the exact Candidate and install the complete Cloud owner topology in
   the protected Instance.
5. Qualify Default App and Custom Agent with the same Candidate, including
   restart, response-loss, route and model readback.
6. Promote only those qualified bytes to a Product Release; keep Instance
   deployment and rollback receipts with the Instance owner.
7. Add the MAP domain contracts, adapter qualification, evaluation and
   institution acceptance as a domain layer on top of the qualified platform.

This sequence is a dependency order for the open outcomes. It does not require
unrelated legacy lifecycle work to block the typed owner migration.

### Handoff Inputs And Parallel Work

Cloud can prepare contracts, owner tests, topology assets, Candidate manifests
and local qualification in parallel when they have disjoint write sets.
Integration of shared contracts, generated bindings, main, Candidate
construction and Instance state is serialized.

The Instance handoff must receive the exact Cloud SHA, image/index digests,
topology declaration, required non-secret configuration names, protected secret
references, provider profile and acceptance commands. The Instance returns
deployment and runtime receipts; it does not redefine Cloud ownership or add a
per-customer provisioning writer.

## Qualification Boundaries

### Local Development

Local development proves source behavior, owner boundaries and disposable
fixtures. The macOS Docker daemon limitation recorded in [status](status.md)
does not authorize changing provider guards or calling a real provider.

### Candidate

A Candidate is a replaceable input built from an exact Cloud SHA. It is not a
Product Release. Any write-access collaborator may dispatch the Candidate
workflow against an exact commit at the head of a pushed branch, and the run must
be self-attributed: the account that triggered it is the account GitHub
attributes it to. Promotion remains main-only and is decided separately.

### Instance

The Instance owner supplies protected installation configuration, installs the
complete owner topology and returns installation, deployment, service-health,
route and deployment-rollback evidence. Cloud product owners retain business,
financial and Fabric provider qualification under separately bounded protected
authorization. First-use business acceptance is tracked by #856 and may cite a
successful deployed revision before formal Candidate Release qualification.
Formal Release evidence binds its selected Candidate to the owner receipts;
Cloud source tests cannot certify installed state or product business
acceptance.

### Production

Formal publication requires the exact qualified Candidate bytes promoted without
a rebuild. Real customer money, provider resources, private networks and
clinical data remain outside ordinary Cloud CI and E2E.

## Deferred Product Scope

The following are intentionally deferred until a separate owner and acceptance
boundary exist: customer-operated payment, shared multi-user Workspaces, HA,
GPU scheduling, institution-wide multi-region operation, arbitrary customer
providers, autonomous clinical action and automatic conversion of model output
into medical orders.

## Completion Evidence

An outcome is complete only when its row has:

- a canonical owner and current caller;
- source/contract evidence at the Cloud layer;
- the exact Candidate identity where packaging is involved;
- owner-authoritative Instance readback where deployment/provider state is
  involved; and
- no unresolved unknown result that could change the next external side effect.

The current unresolved rows and their receipts are maintained in this page and
[status.md](status.md). Historical plans and completed execution transcripts
belong in Git history or [docs/history](history/README.md).
