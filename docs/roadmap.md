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

## Agent read carrier adoption

Cloud owns the authenticated, stateless read-only MCP endpoint and exact quote
review described in [implementation architecture](implementation-architecture.md#agent-reads-and-quote-review).
The source path reuses CloudIdentity's current authorization and existing
owner-authorized GET routes. A short-lived, parent-session-bound credential now
connects the browser download, Cloud-owned helper and Framework's opaque
credential provider. The credential is limited to read actions, a frozen
Tenant and at most one hour; revocation and logout take effect on the next
owner authorization. It is not OAuth or a renewable PAT.

Remaining adoption requires Instance rollout and an installed Agent Host
exercising discovery, Workspace/quote/original-operation reads, revocation and
exact quote review under the same account. Local synthetic qualification proves
source behavior, not an installed production connection. No charge or deployment
is needed for this read acceptance; purchasing remains a separately authorized
Workspace operation.

## Workspace 1042 original deletion

Workspace owns the original deletion operation, deletion receipt and separate
refund; Fabric owns provider readback and the fenced final release of accepted
compute/storage resources. Instance owns installation configuration and
deployment evidence, not the deletion execution path. The
[November 9 retained observation](status.md#workspace-1042-deployed-deletion-and-evidence-custody-2026-11-09)
confirms CBS absence but keeps compute readback unavailable, the original
operation in `needs_attention` and the refund unconfirmed.

The exact Candidate is installed and independently deployment-verified. The
[Cloud-owned live compute diagnostic](runtime/release.md#live-compute-readback)
and its companion protected Instance access adapter are merged. Their first
protected run and Cloud custody import completed, but the diagnostic stopped
before Fabric GET with an ambiguous binding error. Admit the revised Cloud
diagnostic after merge and repeat the bounded read to distinguish Workspace
access/decoding from Fabric access/decoding. This observes the installed image
without another Candidate build. Once the original binding validates, read
the compute through the existing Fabric GET, verify the pinned installation
before and after the read, and import the actual run and receipt into Cloud
custody.
The log diagnostic remains available for exact-operation historical errors.
Source now separates changing compute observation fields from stable resource
identity. Candidate adoption and a fresh owner read are still required before
that fix can establish the installed result.
Keep Instance deployment receipts separate and preserve the Cloud business
imports' original producer and schema. No second Instance deletion entry is
required. Neither local tests nor a successful custody import complete deletion.

Once the exact compute failure is known, repair its Cloud owner and verify the
real consumer before a new Candidate/adoption step. Continue the original
Workspace operation through provider-confirmed compute absence and its deletion
receipt. Confirm refund through the original charge and refund owner separately;
preserve unknown outcomes and historical attempts throughout. Diagnosis,
Candidate installation and CBS absence cannot independently establish deletion
or refund success.

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

## Current Delivery Lane: Owner Convergence And Retained-Writer Retirement (2026-11-05)

The customer-facing native-v2 Default App path was accepted and #1042 was
closed on 2026-11-02. The current architecture objective is to converge on one
writer per fact and one customer-facing path, then remove obsolete implementation
and tests capability by capability. Do not repeat the customer purchase. The
next source work is selected from the retained-writer/caller inventory below:
move or prove absent each live caller, preserve historical obligations under
their existing owner, and remove the retired command, worker, adapters,
configuration and exclusive tests together.

The [November 1 installed evidence](status.md#native-runtime-admission-and-serve-upgrade-authorization-2026-11-01)
records successful installation verification, the original Workspace's native
Serve upgrade and same-operation response-loss replay, and the resolved public
HTTPS entry. The [November 2 customer acceptance](status.md#native-runtime-admission-and-serve-upgrade-authorization-2026-11-01)
records official App login, a real Gateway-backed model response, restart and
response-loss recovery, one confirmed charge and no duplicate purchase or Key.
Local v2 remains diagnostic `NOT_READY`; it is a separate source qualification
profile, not a reason to reopen the accepted customer path.

The protected payer declaration, same-Candidate J0 READY and W01 ordering are
separate engineering/release qualification evidence. They are not prerequisites
for ordinary source-level owner convergence or for retiring a legacy capability
whose callers and persisted obligations have already been accounted for. The
existing Release admission still requires its protected qualification, bound to the exact
Candidate and owner readbacks; never relabel #1042's customer evidence
as that receipt. The App/Studio immutable-publication obligation also remains
separate for retained path consumers.

[#1018](https://github.com/gaofeng21cn/one-person-lab-cloud/issues/1018)
records completed historical-order closeout, with [protected owner evidence](status.md#original-order-closeout-scope-2026-10-23)
for the original resource, route, Key, Ledger and charge obligations. That
acceptance is historical-order evidence and is not a substitute for the separate
protected engineering qualification receipt. Evidence housekeeping in
[#1058](https://github.com/gaofeng21cn/one-person-lab-cloud/issues/1058),
including its archival policy and bounded receipt moves, may finish as a
maintenance lane but does not advance owner convergence. The modularity,
test-retirement and generated-output proposals in #1053, #1054 and #1057 are
not prerequisites for this work and must not expand the current delivery
write set without a demonstrated blocker.

Do not add new feature scope to this lane. After each source slice, update the
current evidence in [status.md](status.md), retire superseded roadmap claims,
and leave historical receipts in place when they remain referenced or owned by
their original producer.

## Custom Agent Local Delivery (2026-10-21)

[PR #1003](https://github.com/gaofeng21cn/one-person-lab-cloud/pull/1003)
is limited to the IBD Local-Docker delivery closure: uploaded Package → real
BuildKit OCI digest → CapabilityVersion → Workspace/Serve/Fabric Local-Docker →
ready route, password login and a real Agent response through the authenticated
native HTTP API. IBD is the feasibility exemplar. The exact historical runtime
evidence and its source/fixture boundaries are owned by
[status](status.md#linux-custom-agent-owner-qualification-2026-10-21);
[Issue #1002](https://github.com/gaofeng21cn/one-person-lab-cloud/issues/1002)
records the scoped source closure and its historical review/CI. Generic WebUI
chat changes, browser-driven sending and a new provider are outside this scope.

The [customer test and admitted WebUI input](status.md#custom-agent-publication-inputs-2026-11-05)
leave customer upload pending a compatible Runtime and effective Build defaults.
The WebUI artifact is published and its approved version was read back through
the protected Instance workflow; do not rebuild or register a duplicate merely
to retry delivery, or add a registry bridge without an observed retrieval need.
The Capability fix for the first admission's create/list timestamp discrepancy
has local persistence evidence; installed adoption remains unverified. Exact
read-only replay confirms the stored version but does not qualify the first
response. Obtain the Runtime publisher's real fixed recipe and a Package delivery whose
carrier references agree with its declared installation working directory. The
successful offline relative-source control does not qualify the original
transport, require a new Runtime installer, or make Framework's official Package
publication channel mandatory for generic Cloud uploads. The IBD Package's
container-local evidence service also needs an executable binding and actual
retrieval verification. Admit the compatible inputs and set the effective Build
policy, then retry upload/build and customer delivery with a real Agent request.

Same-Candidate and installed Tencent/TKE acceptance remain open. The local
`LOCAL_NO_CHARGE` order did not activate a paid subscription: after positive
checks, customer deletion was refused with 422. Real resource absence, Ledger
deletion receipt and original-charge refund still require their original paid
Workspace and separately authorized owner execution. The test's cleanup closes
only its disposable fixtures. This outcome adds no provider or deletion bypass.

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
scoped the first launch to the published Default App OCI, official login, a real
model response, resource/payment integrity and recovery. That customer outcome
is complete under #1042. Cloud-operated model configuration remains a separate
capability; no new App release, purchase or full Local run is needed to repeat
this completed acceptance. Custom Agent Build retains its own recipe, format
and qualification requirements.

The table below lists remaining outcomes. Source, installed readback and
protected qualification evidence stay separate; a source retirement does not
claim unattended deployment or formal Product Release.

| Priority | Outcome | Cloud owner | Current state | Completion evidence |
| --- | --- | --- | --- | --- |
| P0 | Reconcile authoritative owner, writer, caller and retained-obligation inventory | Cloud architecture/implementation owners; Instance for its callers | Target owners are defined in [architecture](architecture.md); current callers and prior withdrawals are inventoried in [implementation architecture](implementation-architecture.md#retained-writer-and-caller-inventory-2026-11-03) and the dated status evidence. The inventory must be reconciled against the current `main` source and latest available owner observations before selecting the next retirement | Per fact, identify sole owner, all writers/callers, persisted obligations, readback, and retirement evidence; record unknown runtime traffic as unknown rather than infer zero |
| P0 | Retire one obsolete Control Plane capability per source PR | Control Plane migration owner plus the actual successor owner; Instance owns its callers | R1/R2/R3/R8 proposals and withdrawals are capability-specific; #1112 is merged source, #1114 is delivered, and other installed adoption/traffic/obligation evidence is bounded rather than globally complete | Move or prove absent every live caller, preserve historical readers/records, then remove command, worker, config, adapters and exclusive tests together; verify surviving owner paths and replay |
| P0 | Converge native customer lifecycle consumers on their authoritative facts | Workspace, CloudIdentity, Gateway Integration and Console | [Customer lifecycle convergence](#customer-lifecycle-owner-convergence) retains the personal-versus-Tenant balance and selected-model admission source gaps; Account eligibility is consumed at source, with installed adoption and eventual owner migration still unverified. #1042 is accepted business evidence, not proof these generic consumers are fixed | Repair each demonstrated consumer at its existing owner, without another eligibility writer, wallet or model allowlist |
| P1 | Adopt Serve current-selection readback and replacement/deletion repairs | Serve, Workspace and Console; Instance owns installation | [Current-selection](status.md#serve-owned-current-application-readback-2026-11-04) and [replacement/deletion](status.md#workspace-deletion-after-application-replacement-2026-11-04) repairs have source evidence; installed adoption and live readback remain open | Current deployment/access identities agree; upgrade then deletion closes the original lineage, resources, receipt and original-charge settlement through their owners |
| P1 | Finish retained model-configuration and Gateway lifecycle migration | Workspace, Gateway Integration, Serve and retained Control Plane owners | BFF/UI model configuration uses Workspace, but retained callers still need disposition and owner/browser acceptance. #1042 proves App model use; it does not qualify the Cloud model-configuration API, Group administration or arbitrary Key rotation | Classify retained writers and their operations; verify model apply/readback, Group administration and rotation/predecessor closeout only at their owners; permanent revocation remains unavailable |
| P1 | Confirm the repaired Catalog refund-policy administrator read | Resource Catalog and Console | The encoding repair is installed in the verified `78dbe9b4` Candidate; the administrator-page HTTP readback remains unverified | Read the repaired administrator list at HTTP 200; source [repair evidence](evidence/source-checks/2026-10-09-refund-policy-algorithm-enum-encoding.json) is not browser acceptance |
| P1 | Deliver other declared Runtime SecretInputs | Serve and Fabric | Gateway-managed input is covered; other declared inputs remain refused and are not qualified by #1042 | Approved opaque binding/version delivery and application authorization/readback tests, with no raw Secret in evidence |
| P1 | Complete protected Default App engineering/release qualification | Cloud release owner; Instance owns protected execution receipts | Payer declaration, same-Candidate J0 READY, W01 per-use ordering and complete protected receipt are separate from #1042 and are not prerequisites for unrelated source retirement | Preserve the existing Release admission requirements and bind the exact Candidate and owner readbacks in the Instance/Cloud receipt boundary |
| P1 | Qualify Custom Agent and later provider/product profiles | Capability, Build, Runtime Control, Serve, Workspace and Instance | Default App customer acceptance is complete under #1042. Original external OMA admission, hosted tenant-specific OCI publication, Custom Agent TKE acceptance and broader Runtime/provider profiles remain separate outcomes | Exact Package/WebUI/Runtime inputs, actual tenant registry destination, immutable OCI and separately scoped route/restart/recovery evidence |
| P1 | Public registration, administrator top-up and controlled purchase | Control Plane, CloudIdentity, Console | Existing operator/admin path exists; public self-service chain is incomplete | Concurrent registration, zero initial balance, one audited top-up, quote/purchase idempotency and protected browser readback |
| P1 | Backup, restore, alert and data-exit contracts | Cloud owners; Instance operates them | Owner boundaries are described; production schedules and receivers are external | Isolated restore identity/readback, stable active/recovered signals, stop-purchase boundary and export/delete receipts |
| P1 | MAP domain contracts and evaluation gate | MAP medical domain owner/Instance | Cloud logical Connect boundary is documented; no clinical domain or qualified MAP adapter/backend combination is claimed | MAP task/adapter contract, selected OPL Connect backend, approved data classes, source/evidence model, human review states, evaluation set, release gate and institution acceptance; no medical-specific Cloud adapter is required |
| P2 | Customer-operated payment, multi-user Workspace, HA/GPU and broader provider profiles | Product and Instance owners | Deliberately outside the current beta cut | Separate product decision, owner and qualification evidence before admission |


## Cloud Admin Workspace Read Adoption (2026-11-07)

Workspace/Fabric/BFF/UI source now separates business Workspace facts from
provider observations and removes retained CP Workspace reads from Cloud Admin
list/detail. The overview total uses Workspace; unsupported resource aggregation
is explicit, and Account counts identify their historical CP scope. This source
change does not retire independent CP capabilities or merge historical records
into native identities.

Remaining acceptance: build and adopt one exact Candidate containing the updated
contracts, authorization policy, owner implementations, BFF/UI and portable
BFF-to-Fabric wiring. Instance must verify the configured peer and platform-admin
session over list/detail/overview, compare one native Workspace's business fields
with its Customer read, preserve Fabric unavailable/unknown/absence, and verify
that no retained Workspace read or resource mutation occurs. No installed
readback or production retirement is claimed by local tests.

The same Candidate must include Serve's dedicated platform-admin read RPCs and
the generated CloudIdentity policy. Compare Admin and Customer application
availability/access metadata for the same selected deployment, including no
deployment and readiness changes. Verify tenantless platform-admin login and
reads, Tenant-role refusal on operator reads and unchanged Customer isolation.
This read acceptance does not qualify application login, credential reveal,
version update or rollback for platform administration.

## Console Interface Adoption

The [Console interface refinement](status.md#console-interface-refinement-2026-11-05)
has local source and fixture/browser evidence. Installed acceptance requires
the adopted Candidate's customer and operator surfaces, real authenticated
owner reads, and representative desktop/mobile navigation. Publication and
Instance deployment remain with their existing owners.

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

The former adoption table referenced completed or withdrawn Issues. It is
retired as a current work list: [#840](https://github.com/gaofeng21cn/one-person-lab-cloud/issues/840),
[#856](https://github.com/gaofeng21cn/one-person-lab-cloud/issues/856),
[#926](https://github.com/gaofeng21cn/one-person-lab-cloud/issues/926), and
[#928](https://github.com/gaofeng21cn/one-person-lab-cloud/issues/928) retain their
original source evidence and closure reasons. The Instance retirement proposal
was [Instance Issue #517](https://github.com/gaofeng21cn/opl-instance-medopl/issues/517),
not Cloud PR #517. Closing those targets did not prove installed financial,
provider or formal Release acceptance. New exact failures are assessed against
the current owner and caller instead of automatically restoring a historical
acceptance duty.

Workspace create/delete convergence includes completed original-owner acceptance
under [#1018](https://github.com/gaofeng21cn/one-person-lab-cloud/issues/1018)
and reusable native unfinished-order closeout delivered through
[#1043](https://github.com/gaofeng21cn/one-person-lab-cloud/issues/1043).
[#1028](https://github.com/gaofeng21cn/one-person-lab-cloud/issues/1028)
owns new-purchase caller cutover. The untouched and partially dispatched order
tracking in #1025 and #1027 was consolidated into #1018; their closure and the
unmerged #1026 proposal do not establish cancellation or absence. Activation
and operator command routing follow the remaining callers in #1028; #1031 and
#1032 retain their historical source changes and closure reasons.

### Retained code-removal review (2026-11-03)

The independent R2b proposal withdraws only the old CP Local-Docker network
recovery command, its service/client adapters and exclusive types/tests. The
current protected census has no such accepted operation; archive/traffic and
installation adoption remain unproved. Fabric provider-network execution and
strict readback stay with Fabric pending their own caller/obligation disposition.
The separate R2c image slice removes CP replacement creation, preview, dispatch
and retry/worker execution and Console/BFF callers. The existing policy activation
writer remains necessary for original Launch image-revision recovery; migrate its
selection authority before retiring its atomic revision/audit store.
It retains the direct CP policy and historical replacement GETs, historical
operation/policy decoders, pending-transition/resource-reconcile barriers and
original Launch revision recovery. Fabric retains its own provider/replacement,
journal and strict readback. The [image reference](implementation/workspace-images.md)
owns the source boundary and [status](status.md#retained-runtime-image-command-withdrawal-2026-11-04)
records its bounded evidence.

R2c is based on `173d1afbcbd3dc120f0839707c9e69eb2ba2787b`, after merged
Cloud #1120. Instance #675 rollout/rollback and policy-mutation caller withdrawal
is MERGED with 2/2 CI checks SUCCESS; [status](status.md#retained-runtime-image-command-withdrawal-2026-11-04)
records its exact merge/head/run evidence. Configured callers are withdrawn at source. Cloud adoption remains unproven;
check in-flight legacy executions before adopting the entrypoint-removal version
through the existing Instance process. Retain the shared Instance image client
for registry retention/node-cache retirement. Acceptance requires source
verification and exact-version installed readback of caller withdrawal,
the removed entrypoints and surviving policy/history/recovery consumers. The
existing seven terminal image operations among 44 current operations, all on
currently `data_deleted` old Workspaces, do not establish zero traffic, archive
absence, later state or other-installation disposition. Original Launch runtime
repair and remaining accepted credential/Key obligations stay separate work.

The user approved actual code deletion, capability by capability. Independent
proposals cover the unused Key prototype ([Cloud #1112](https://github.com/gaofeng21cn/one-person-lab-cloud/pull/1112)), refused activation alias
([Cloud #1114](https://github.com/gaofeng21cn/one-person-lab-cloud/pull/1114)), existing Instance observation projection ([Instance #673](https://github.com/gaofeng21cn/opl-instance-medopl/pull/673))
and fixed-slot procurement ([Cloud #1113](https://github.com/gaofeng21cn/one-person-lab-cloud/pull/1113)) at the inventoried baselines. Since then,
#1112 merged at `2026-11-03T06:57:24Z` as
`0064c770d87eb25daeaf1f0dc55aeef1c4d99c60`, at source only and not installed.
The original inventory is a documentation snapshot; subsequent source withdrawals
are described separately. Verification belongs to each source revision; neither
this snapshot nor a source merge establishes installed state or completed Control
Plane retirement.

The [writer/caller inventory](implementation-architecture.md#retained-writer-and-caller-inventory-2026-11-03)
binds the exact Cloud/Instance baselines, includes external source callers and
separates source reachability from the protected observation. Preserve accepted
historical operations and financial records while retiring their execution paths.

| Review scope | Proposed decision | Evidence needed before implementation withdrawal |
| --- | --- | --- |
| R8 fixed-slot provider procurement | [Cloud #1113](https://github.com/gaofeng21cn/one-person-lab-cloud/pull/1113) removes route/executor, dedicated config, unused create-claim machinery and exclusive tests; the source HOLD is resolved by fresh owner evidence | Observation `41598975341` excludes current accepted fixed-slot and historical single-slot operations through complete current-table identity comparison; no recovery owner migration is needed for absent operations. Preserve historical identity/billing readers. Installation and later accepted operations remain separate |
| R9 test-only Key lifecycle prototype; retired browser activation alias | [Cloud #1112](https://github.com/gaofeng21cn/one-person-lab-cloud/pull/1112) is merged source only, not installed; [Cloud #1114](https://github.com/gaofeng21cn/one-person-lab-cloud/pull/1114) delivers alias retirement and a standalone existing-account reader, with the reproduced Modal focus race repaired | Fresh legacy runner migration remains an open qualification outcome, not a prerequisite for retiring the already-refused alias. Keep topology admission strict; a future runner must consume the declared preactivated installation and preserve its persistent state. No new provisioning capability is needed here. Native lifecycle, internal activation and Tenant/wallet binding remain |
| R1 application admission/default install/deployment | R1a removes operator admission/create commands and Console controls; R1b removes default-install execution/resume and its final revision-admission writer. R1c removes the test-only application configuration/Secret bootstrap and its exclusive service wrapper. R1d deletes the generic deployment builder, transition and reinstall-inheritance rules; the only constructor is an accepted Key-rotation same-selection child. Instance #674 removes the installer caller. Shared engine retirement remains open | Fresh observation `41665805194` links all three current default-install and five deployment records to later succeeded Workspace deletions; preserve original records and separate refunds. Accepted credential deployments and Workspace-Key rotation still need the shared worker, activation writers and frozen operator retry. Withdraw or migrate those callers before deleting the engine; source removal does not establish installed adoption |
| R2 runtime/image/password maintenance; R3 Workspace Key rotation | R2a removes password creation; R2c removes CP image replacement creation/preview/dispatch/retry worker and Console/BFF callers. Original-Launch policy activation, Launch repair, historical readers/barriers and accepted credential/Key recovery remain | Instance #675 caller withdrawal is MERGED, CI 2/2 SUCCESS; Cloud entrypoint-removal adoption remains unproven; check in-flight legacy executions before that adoption. Source merge/CI do not establish zero traffic or clearance of business obligations. Source verification and exact-version installed adoption require separate evidence. Observation `41750316687` has seven image records (five succeeded, two failed, none started) among 44 current operations, on currently data-deleted old Workspaces; archive/later/other-installation and zero-traffic conclusions remain unproved. Preserve direct policy/history GETs, Instance retention/node-cache consumers and Fabric capability; resolve remaining obligations before shared-engine retirement |
| R5 retained lifecycle/renewal/delete/refund; R6 pricing/eligibility | Review full execution-engine removal with its worker, store/client and external-tool slice | Establish other accepted operations/renewal/financial obligations, independently settle original refunds, remove or migrate history consumers, preserve the native path; the three #1018 orders need no further mutations |
| R4 personal-Key/admin-wallet/Account provisioning | Keep while product requirement or actual BFF caller remains; decide separately | Existing native readers/managed-Key commands do not supply personal-Key writes, arbitrary wallet adjustment or external-user/Account provisioning parity |
| R7 history, announcements/ACK, identity proof and shared CP transport | Retain under their current owners in this scope | Separate capability migration is needed before removing their writers, readers, proof/configuration or the CP process; Workspace cleanup alone supplies no such evidence |

R3a withdraws fresh CP Key rotation acceptance and deletes its preparation,
store claim and unused DTO. The existing POST retains only original-operation
recovery; no record means HTTP 410, while unknown or damaged state remains an
error. Current observations exclude rotation records only in their returned
44-operation sets, not in archives or external Key/Secret state. Accepted parent
recovery and retained Launch Key stages therefore remain explicit R3/R5 work;
child deployment success does not prove predecessor closeout or parent receipt
completion. Retire those implementations only after their original obligations
and surviving consumers are resolved. This slice is based on merged password
withdrawal #1118 and changes only Key rotation acceptance and its consumers.

A selected retirement is complete only when its command/implementation, scheduler
or worker registration, exclusive configuration, caller adapters and dedicated
obsolete tests are removed together, and the real surviving consumers pass
owner/history/replay checks. A hidden button, `enabled=false`, refusal route or
permanent compatibility branch is not the requested delivery. Necessary shared
read/decoding code can remain with its named history consumer; source removal
does not authorize dropping financial records, receipts, customer data or schema
history. Do not build legacy feature parity unless the review establishes a
current product requirement.

Protected observation `41564384194` confirms two enumerated CP records were
`data_deleted`, but its projection is not an audit archive, Accounts are a
first-page sample and runtime health was unavailable. It does not establish a
complete pending-Key/wallet/deployment-operation census or route-specific
zero-new-business interval. Its own zero mutation counters prove that the
observation was read-only. Four unmatched runtime observations require native
Serve/Fabric attribution; they are not a resource cleanup list. Use existing
protected owner readbacks for remaining evidence; this review creates no new
qualification gate, metrics service or authority.

Instance proposal [Instance #673](https://github.com/gaofeng21cn/opl-instance-medopl/pull/673) extends the existing observation producer to
project every runtime operation in its current owner API result, taking status
only from the owner status field and preserving missing status as unknown.
Protected observation `41598975341` now supplies fresh evidence from this merged
extension. Its successful CP projection has 44 distinct operations below the
installed owner's unfiltered 100-row page limit, with no current fixed-slot or
historical single-slot identity or acceptance action digest. The executor saves
operations before provider execution and retention does not remove them; this
resolves R8's current-operation HOLD without restoring procurement or adding a
new capability. The global observation remains partial and does not prove a
financial archive, zero traffic or future absence. It preserves the old
Workspace scope and adds
current/reserved deployment identity digests with explicit unknown/empty
semantics. Fresh installed binding readback, independent wallet/rotation state
and Account enumeration remain unverified.

Closed #1028/#1042/#1052 remain historical references. #1042's customer outcome
and #1018's three-order closeout stay complete. The scopes in this section are
the current review boundary; they neither reopen those Issues nor imply their
closure physically removed every retained writer.

### Customer lifecycle owner convergence

The Cloud customer path is CloudIdentity session → Catalog quote → Workspace
order/deletion → Gateway, Fabric, Serve and Ledger owner commands/readbacks.
A reachable deletion gap after application replacement is repaired in Serve:
close the original reservation's successor lineage while preserving the original
Workspace order and payment. Source and isolated-owner tests do not establish
installed adoption; validate upgrade → delete through the existing Instance
process after adoption. Retained history/recovery is not a fallback for a native
Workspace, and its obligations remain separate.

The remaining bounded source gaps are the launch page's use of a personal wallet
balance to gate a Tenant-funded order, and managed-Key issuance with non-empty
model selections when the user API supplies no model-scope projection. The
[November 5 check](status.md#existing-default-app-provisioning-closure-readback-2026-11-05)
reproduces the latter through the actual Gateway user-API issuer and managed-Key
owner: issuance succeeds externally, then the scope comparison leaves the order
pending before Secret/binding delivery. Resolve each at its existing owner with
its actual consumer; do not copy requested models into an unobserved effective
scope, bypass authorization or add another wallet/Gateway allowlist. Verify
non-empty selection and original-command recovery through the real issuer.
The new-purchase source now consumes the Account policy; its installed adoption
and eventual policy-owner migration remain below.

The existing Default App still answers actual model turns and preserves its
session/reply after page reload. This does not qualify a fresh unattended order
or prove adoption of later source repairs. The Instance customer-readback receipt
remains blocked on the unauthenticated entry's HTTP 302; reconcile that producer
with the application's actual login boundary before claiming automated acceptance.
The separately reported background-service warning is not evidence that the
observed model turn failed. Preserve both observations without extending this
check into unrelated App capabilities.

After adopting the Serve reload admission repair, verify that a Runtime without
a declared model-configuration interface is refused before Secret handover,
preserving its active binding and applied version. Source owner tests establish
this ordering; they do not establish installed adoption.

#### Workspace purchase eligibility adoption

New-purchase source admission consumes the existing Account eligibility owner;
validate the installed owner/client configuration and grant → new purchase →
revoke → refused new purchase sequence after adoption. Confirm that the accepted
original order still replays and recovers when eligibility is later revoked or
its reader is unavailable. A source test or successful CI is not installed proof.

The retained Account remains the sole eligibility writer until its provisioning,
grant/revoke commands, historical true/false values, audit custody and new-purchase
reader migrate together.


### Native v2 protected qualification and CP retirement (post-#1042)

Public stable v0.1.8 completed the non-spending installation and source-path
publication acceptance. The following live business and retirement outcomes are
independent of that completed publication. They remain unverified
until separately authorized execution supplies their native evidence.

Workspace owns purchase and deletion orchestration; CloudIdentity owns the live
session and Tenant authorization, Serve owns deployment/access, Fabric owns
provider execution/absence, Gateway/Sub2API owns charges, refunds and Keys, and
Ledger owns evidence. The qualification tools consume these facts. Control Plane
retains historical orders and financial obligations until their migration is
proved. The #1041 source fence rejects fresh CP purchase keys before business
writes and removes the legacy new/repurchase affordances; historical resume and
the retained DELETE path remain. It does not authorize blanket route removal.
The current Instance launch readback tool already calls native v2 routes at
Instance `e8ee04df`; its older local checkout is not a current CP caller.
The closed #1041/#1052 tracking does not establish complete caller or writer
retirement. The current [code-removal review](#retained-code-removal-review-2026-11-03)
records source callers, bounded installed evidence and missing operation/traffic
coverage. Legacy customer Launch auto-discovery needs its actual caller evidence;
operator recovery and server read/replay/resume do not prove that browser
capability. Instance #593 adopted the separate protected business-qualification
consumer source migration, as recorded by Cloud #1072. Its installed declarations, genuine same-Candidate
J0 and complete protected execution remain an independent engineering lane after
the customer acceptance recorded in #1042; source adoption alone does not qualify
the consumer.

The [source changes](status.md#native-v2-qualification-evidence-2026-10-24) bind
native charges/deletion/refunds to their original owner operations and preserve
replay identity. The
[October 26 installation evidence](status.md#delivery-lane-reconciliation-2026-10-26)
now binds successful Candidate construction, Deploy and read-only
`deployment_verified`/installation verification to the exact Cloud source/tree,
Candidate manifest and image index. Native Local-Docker qualification is now
complete for frozen source `5e3d58dc`: the main Clean Host run `41968911003`
returned a version-two READY receipt for the admitted image, lifecycle and zero
remaining Workspace resources. [Status](status.md#native-local-docker-release-qualification-2026-11-05)
retains the original evidence and the LOCAL_NO_CHARGE scope. The following are
the remaining independent qualification and migration outcomes; they do not
reopen #1042's completed customer acceptance:

- Complete the installed use of the adopted Instance protected consumer:
  RuntimeVersion/compute/storage, authenticated Tenant/payer and a genuine
  dedicated-sandbox J0 READY receipt must be explicitly bound to the same
  Candidate. The original customer Workspace already has successful native App
  update and same-operation response-loss replay evidence, plus official App
  login, a Gateway-backed model response and restart recovery. Those customer
  observations are distinct from the protected qualification and W01's separate
  per-use admission/revocation ordering.
  Preserve Candidate/J0, protected-access and Release admission gates; see the [current installed evidence](status.md#native-runtime-admission-and-serve-upgrade-authorization-2026-11-01).
- Produce the protected Default App business qualification receipt for that
  Candidate: usable business entry, runtime identity/readback, Gateway facts and
  restart/response-loss recovery must agree at their owners. The completed
  customer observations are not relabeled as this protected engineering
  receipt; a protected workflow run and its owner-bound receipt remain the open
  evidence.
- Remove remaining live Control Plane identity/account and historical-duplicate
  lookup dependencies only after their original obligations have a proved owner
  path. Then qualify the same flow on TKE and the separately authorized live
  Gateway/Sub2API refund boundary before claiming those acceptance layers.

CP process retirement additionally requires disposition of its live identity,
announcement, historical financial and static-hosting consumers. Per-capability
removal uses the [current review scope](#retained-code-removal-review-2026-11-03)
and preserves owner-approved historical retention/readback. #1042's customer
acceptance and #1018's original-order closeouts are complete; this separate
source-removal review does not reopen them.

### Original-order Workspace closeout (#1018)

All three original orders are closed. The [authoritative closeout summary](status.md#original-order-closeout-scope-2026-10-23)
retains the exact protected runs, resource absence, deletion/settlement receipts,
original-charge refunds and native Key dispositions. Do not repurchase, delete
or refund those selectors again. The detailed completed execution plan is
retired from this roadmap; its evidence stays with its original owners.

Completed #1018 acceptance does not establish retirement of every retained
Control Plane capability. Remaining caller and historical recovery obligations
belong to the [current removal scope](#retained-code-removal-review-2026-11-03),
with [implementation boundaries](implementation-architecture.md#original-order-closeout-boundaries).

## Development Governance Migration (2026-10-13)

Cloud development tooling owns adoption of the
[ordinary source development decision](decisions.md#2026-10-13-ordinary-source-development-uses-git-scope-and-current-ci).
Local changes reconcile host admission, verification selection, CI and
contributor instructions with the adopted default. The source entry points and CI are implemented. Ordinary PRs
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

The historical #787 investigation recorded a separate original launch HTTP 502. The current immutable-Candidate/run-owned-fixture path is delivered
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
