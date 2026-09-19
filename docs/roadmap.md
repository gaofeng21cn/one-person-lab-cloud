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

Local development for these work packages is complete; the remaining acceptance
of each one is the Instance, Candidate and production qualification named in its
row. Completed development proceeds through PR review, CI, merge and Candidate
construction, and Instance owns the subsequent deployment and qualification.

| ID | State | Owner | Remaining acceptance |
| --- | --- | --- | --- |
| `D1-FINANCIAL-INTEGRITY-01` | `cloud_complete` | Control Plane finance and Sub2API; Instance owns adoption | Local source/full regression and isolated real Sub2API validation pass; [owner evidence](./status.md#retained-runtime-evidence) retains the exact worktree and patch identity. The native official 0.2.4 integration must pass isolated business tests and Instance readback without Gateway changes; missing audit evidence after dispatch remains manual review and cannot authorize another monetary write. Historical unverified transactions cannot be certified from current balance. |
| `D2-SETTLEMENT-RECONCILIATION-01` | `cloud_complete` | Control Plane finance, Ledger and Console | Original-operation business chains, failure/recovery, historical records, more than 10k unrelated receipts, customer browser tests and full PostgreSQL/Docker regression pass. [Owner evidence](./status.md#retained-runtime-evidence) binds the local source snapshot; Instance adoption remains a separate deployment action. Unverifiable older split-resource records remain explicit review exceptions. |
| `D3-LAUNCH-RECOVERY-CAPACITY-01` | `cloud_complete` | Control Plane Launch; Fabric and Console | Original late-result recovery, bounded scheduling, and owner-authorized failed-Launch closure are implemented and pass focused plus full local verification. The closeout chain freezes the original operation, retains Gateway Keys, proves five Fabric resource absences, refunds only the original account's remaining charge, and records `billing.workspace_closed.v1`. Unknown outcomes retain pending state and pool claims. Tencent adoption and actual provisioning capacity remain Instance obligations; no deployment is part of D3 local development. |
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

The current operator-observation repair under F/J connects Gateway totals and
provider-neutral resource/Runtime read models without adding a recovery action.
Acceptance requires complete mapped-account totals, stopped/absent/unknown
resource distinctions, normal suspension, missing and unmatched Runtime
visibility, and unchanged financial/procurement behavior. Cloud source verification is complete; exact-Candidate Instance readback
remains an external evidence obligation in
[status](status.md). Unmatched production objects require an owner-reviewed
disposition; their visibility does not authorize deletion or shared-pool scale.

The same observation lane distinguishes CVM `pending_deletion`/`deleting` from
ordinary transitions and preserves missing TKE associations independently.
Installation image consistency now reports verified retained Workspace targets,
no running sample, and unverifiable identity separately. The source evidence is
in [status](status.md); the selected Instance owns its scan interval and must
read back its process configuration, unchanged existing images and cloud
lifecycle facts from the adopted Candidate.

The external-resource reconciliation under C/F/J now follows successful Workspace
bindings instead of relying on old resource projections. Cloud implements
confirmed-loss suspension, closed auto-renew/access, preserved financial identity,
and retained-operation protection in Runtime observations. Its focused local
business and persistence evidence is recorded in [status](status.md). Exact-Candidate
Instance readback must still prove convergence of the affected Workspace and no
impact to the running control; authorized historical Runtime retirement remains
an Instance operation with separate immutable evidence.
## Workspace Application Decoupling

最终用户结果是：管理员在现有 Console 中选择一个已开通的 Workspace，
从 `uswccr.ccs.tencentyun.com/oplcloud` 命名空间先选择 repository，再选择
tag/版本并解析固定 digest，补齐受支持的应用配置和数据输入后部署；用户打开该应用，
其真实业务可用，后续更新、重启和兼容回滚保持已声明的持久数据。
首个实际投放目标是 `ws-609081bc2298edd18e`：将 OPL App 切换为完整 IBD，
保留原计算/存储身份、购买历史和隔离的 OPL App 数据。

这是 Workspace 应用分发能力的交付范围。每个 Workspace 初期允许零个或一个
选定应用，该应用可包含多个私有支持服务；OPL App 是默认应用。资源开通、
应用部署、数据恢复各有独立操作和结果。管理员更新 A 不改变 B 的版本或数据，
更改安装默认值或上传新镜像也不会自动升级现有 Workspace。
客户自助分发、多独立应用共存、隐式扩容采购和自动备份不在本次范围内。

本节唯一维护交付物、推进依赖和验收结果：

- [DDD 模型](architecture.md#ddd-model-and-consistency-boundaries)、
  [独立事实](architecture.md#independent-facts-and-completion)和
  [工程分层](architecture.md#engineering-layers-within-existing-owners)由架构文档维护。
- [源码落点](architecture.md#implementation-seams)和
  [存量消费者迁移](architecture.md#existing-owners-and-migration)定位实际修改边界。
- [当前基线](status.md#current-capability-baseline)记录实现及验证证据。
- [管理员与客户体验](product/workspace-experience.md#application-selection-target)
  维护用户交互；[发行操作](runtime/release.md)维护 Candidate 与 Product Release 机制。

资源独立开通、应用登记和独立部署已有源码消费者；默认安装、替换与生命周期
统一正在完成本地验证。Cloud 源码不代表 IBD 实际投放，具体证据由
[status](status.md#current-capability-baseline) 维护。

### Required Deliverables

| 最终交付物 | 生产者 → 接收者 | 可拿到、可使用的具体内容 | 接收条件 |
| --- | --- | --- | --- |
| Cloud 平台实现与 portable Candidate | Console / Control Plane / Fabric / Ledger → 本地安装与 Instance owner | 源码、实际 typed HTTP API、管理员界面、领域规则、owner-local schema/decoder/存量迁移、操作恢复和测试；确定 Cloud SHA 对应的 GHCR 多架构镜像，以及既有安装包、`opl-cloud-candidate.json` 和 `SHA256SUMS`。 | 五个闭环中相应 Cloud 行为有实际验证；Candidate 绑定 SHA/tree、index/platform digests、安装文件校验和及构建 provenance；支持另一已满足能力要求的应用时，只需登记其材料，无需再改代码或重建 Cloud。 |
| OPL App 显式应用描述与存量迁移 | App 发布者 + Cloud 迁移 owner → 新部署与现有 Workspace | 现有镜像的版本化运行描述；原端口/探针/配置/凭据/挂载声明；可执行、可恢复的成功与未完成 Launch/Workspace 绑定迁移。迁移代码随 Cloud 交付，应用描述保留发布者归属。 | OPL App 经独立应用操作安装；原 `/data`、`/projects`、凭据、资源和购买事实保持；旧操作能按原合同完成，迁移后的真实查询/访问/生命周期调用可用。 |
| IBD 应用交付材料 | IBD 发布者，Fabric 提供通用存储执行能力 → Cloud 准入与 Instance owner | 版本化、机器可读的运行描述；主/依赖 OCI 的完整不可变引用和平台；组件、端口、探针、资源、连接、配置与 Secret 接口、持久/临时挂载；数据/模型/PDF/索引引用和一致性校验和；版本化恢复、验证工具及兼容性说明。 | 主镜像和六个知识服务或明确的外部绑定均齐备；Docker-volume 数据可通过已实现且验证的通用 TKE/PVC 导入路径使用；恢复后的真实检索、问答、SSE 和引用证据通过；材料不包含凭据值。 |
| 指定 Workspace 的实际部署结果与不可变凭证 | `opl-instance-medopl` → 操作者与最终用户 | 对应 Candidate 的安装；Registry/Secret/provider/DNS/TLS/origin 绑定；限定目标的 IBD 部署、数据导入和可用入口；实际 Runtime、数据、使用及恢复/回滚读回；owner `receipts/` 下的新凭证。 | 凭证同时绑定准确的 Cloud、应用/依赖、数据与恢复工具身份、目标 Workspace、时间及实际结果。只有该层完成，才能声明 IBD 已在目标 Workspace 可用。 |
| 操作说明与验收证据 | 各 owner → 后续运维和接入应用的操作者 | 可复现的登记、预检查、部署、更新、恢复、兼容回滚和故障续接说明；输入格式/示例、支持能力、权限边界；Cloud 检查证据、应用验证结果和 Instance receipt 引用。 | 操作者能用同一版本材料复现完整操作，并区分完成、失败、处理中和结果未知；镜像更新与数据恢复有各自明确步骤和副作用。 |

交付形态分为三条可独立版本化的产物链：

1. **平台产物。** 沿用现有 Cloud Candidate workflow 和安装包格式。
   Cloud 镜像包含平台程序；Candidate manifest 的 authority 仍是 Cloud 源码、
   镜像及安装文件，不加入 IBD 镜像、具体域名、Secret 或 Instance 状态。
2. **应用材料。** 镜像保留在 TCR 等批准的 OCI Registry；运行描述使用对应
   CP/Fabric 实际 API 消费的机器可读格式，数据与恢复工具位于批准的产物存储。
   交付清单关联这些确定引用和校验和。简单应用可以由管理员表单生成同样的
   部署描述，不强制新增 OPL Package、市场发布或第二个 Registry。
3. **安装结果。** Instance 使用平台和应用材料，加上自己的配置/Secret 绑定
   执行投放，通过 receipt 关联上述身份。应用版本发布不等同于 Cloud Product
   Release；正式 Product Release 若另行请求，按现有机制提升同一已验证 digest。

### Business Work Packages

| ID | 业务结果 | 主责与协作 | 所属闭环 |
| --- | --- | --- | --- |
| `WORKSPACE-PROVISION-DEPLOY-SEPARATION-01` | 资源购买可独立完成，之后应用失败不重购、不推翻已交付资源。 | Control Plane 购买/Workspace；Fabric 资源；Ledger；Console。 | 1 |
| `WORKSPACE-REGISTRY-ENDPOINT-OWNERSHIP-01` | 镜像目录的写入者明确为安装方：端点、允许仓库集合与浏览凭据由 Instance 提供，Cloud 无默认值、无匿名降级、不枚举。 | Instance 提供端点、允许集合与 Secret 注入；Control Plane 消费；Fabric 保持自己的拉取绑定。 | 2 |
| `WORKSPACE-APPLICATION-MODEL-01` | 管理员动态登记应用，在选定 Workspace 上独立部署和更新。 | Control Plane 应用准入/部署；Fabric 执行；Console；应用发布者。 | 2 |
| `WORKSPACE-APPLICATION-ACCESS-01` | 按选定访问策略打开应用，保留应用自己的根路径、Cookie、API 和流式通信。 | Control Plane 访问；Fabric 网络/入口；Instance DNS/TLS；Console。 | 3 |
| `WORKSPACE-APPLICATION-ORIGIN-01` | 每个 Workspace–应用绑定使用派生 origin；平台凭据不进入应用；应用 Cookie/Authorization 语义保持。 | Control Plane 路由与访问边界；Instance 通配 DNS/TLS。 | 3 |
| `WORKSPACE-APPLICATION-DATA-01` | 真实数据跨兼容更新/重启保留，初始数据可显式、一致地导入。 | Control Plane 逻辑数据/恢复操作；应用发布者的数据算法；Fabric 存储/执行；Ledger。 | 2 的稳定绑定，3 的导入和数据使用 |
| `WORKSPACE-APPLICATION-LIFECYCLE-01` | 所有当前/未完成应用组件随 Workspace 权益、续费、到期、删除和配置操作正确演进。 | Control Plane 生命周期；Fabric；Ledger；Sub2API 保留钱包/Key authority。 | 每个闭环随能力迁移，4 完成全部存量与竞争验证 |
| `WORKSPACE-IBD-DEPLOYMENT-01` | IBD 经 OPL App 相同的通用路径，在指定 Workspace 真实可用。 | IBD 发布者提供材料；Cloud 完成公共能力；Instance 负责实际采用。 | 材料可立即准备，3 本地使用，5 指定目标投放 |

### Implementation Sequence

交付顺序按资源、应用执行和实际投放之间的依赖组织。每项能力交付当前
调用链及相应验证；合同随两端消费者一并更新，本地检查、Candidate 与
Instance 结果分别记录。

| 能力 | 交付内容 | 完成条件与当前状态 |
| --- | --- | --- |
| 客户开通只交付资源 | 普通 Console 开通不含默认应用安装；形状由请求显式声明，不再依赖服务器默认。 | 2026-09-17 Cloud 侧完成：Console 开通控制器统一组合 `provisioningMode: "resource_only"`，普通开通不再产生 OPL App 默认安装操作；省略该字段的保留路径与历史操作义务不变。证据见 [客户开通的 provisioning 形状](status.md#customer-launch-provisioning-shape-local-development)。Instance 侧尚无对应部署读回。 |
| 绑定独立 origin | 每个 Workspace–应用绑定一个派生 host；路由、平台凭据边界、Cookie 约束与外部 Host/scheme 传递。 | 2026-09-17 Cloud 侧完成：origin 由 `(workspaceId, applicationId)` 派生（无分配表、无第二写入者），Host 直接解析 Workspace，应用分量对当前绑定校验，被取代的 origin 返回 410；绑定 origin 在管理路由表之前分派，本服务管理路由不在应用 host 应答；请求只剥离平台凭据，应用保留自己的 Authorization 与 Cookie；响应 Cookie 去除 `Domain`；代理自行声明 `X-Forwarded-Host`/`X-Forwarded-Proto`；保留路径入口行为不变。取证见 [绑定独立 origin](status.md#per-binding-application-origin-local-development)。2026-09-17 布局定案并公网验证基础设施层：origin 挂一级域（`OPL_WORKSPACE_APPLICATION_DOMAIN`），落在公共代理免费边缘证书范围内，**不需要任何 origin 侧证书**；三个派生地址实测 DNS 解析、TLS 握手与 HTTPS 响应全部通过。2026-09-18 已在实例部署并读回：Candidate `dc34b4de` 部署成功（实例 receipt `receipts/2026-09-18-application-origin-deploy-35253804870.json`，rollback 0），origin 分派已生效——origin 形状的 host 不再落到 Console 静态兜底，未知 Workspace 返回 404，而非 origin 形状的既有 host 行为不变。剩余：真实应用经新 origin 打开的浏览器验收（需先有部署了应用的 Workspace）；镜像选择的响应读回需管理员会话。 |
| 单用例部署（镜像＋规格） | 部署命令直接接受镜像与运行规格，不再要求先单独登记一次全局应用版本；revision 快照仍只有唯一 owner。 | 2026-09-17 Cloud 侧完成：`POST /api/operator/application-deployments` 接受可选内联 `revision`，在同一命令内写入既有准入表并复用同一部署链；冲突内容拒绝且不改写已存 revision，信封身份不一致拒绝。Console 部署动作已发送该内联 revision，"登记应用版本"保留为发布者预置能力而非前置。取证见 [单用例部署命令](status.md#unified-deployment-command-with-inline-revision-local-development)。剩余：Console 四步界面收敛（阶段 5）；Instance 无部署读回。 |
| 独立应用部署 | CP→Fabric 账号/资源/操作身份与授权、后台继续推进、原子激活、声明入口及真实健康读回、必要 Console 字段。 | 2026-09-13 本地修复与验证完成：全量检查的 18 个 PostgreSQL/Docker 包零跳过；最终 provider/HTTP 回归含真实 Docker，68 个测试及子用例通过、零跳过。[源码与验证证据](status.md#independent-application-deployment-verification)分别绑定全量检查和最后账户读回修正。Cloud 源码集成见 [PR #551](https://github.com/gaofeng21cn/one-person-lab-cloud/pull/551)。2026-09-16 补齐组件级 CPU/内存声明并用它约束两个 provider 的容器，并把 OPL App 与所有应用统一到同一个安装网关入口（provider 报告解析后的目的地，Control Plane 拥有路由），见 [组件资源声明](status.md#component-compute-envelope-local-development)与[统一应用入口](status.md#unified-application-entry-local-development)；腾讯实例部署仍需 owner 验证。 |
| 默认安装与应用替换 | OPL App 作为默认应用走独立部署操作；同一 Workspace 保持一个选定应用；按预检查确定中断/替换方式，清理旧实例及无引用镜像，保留隔离持久数据。 | 默认安装、同应用更新、切换与原命令恢复已实现；资源/购买不变，生命周期按当前部署工作。2026-09-14 Cloud 本地验证完成：`verify:local:full` 通过全部源码/浏览器检查及 18 个 PostgreSQL/Docker 测试包、零跳过；真实 Docker 覆盖替换、数据/凭据保持、停止/恢复/删除与旧运行/无引用镜像清理。证据见 [默认应用替换验证](status.md#default-application-replacement-verification)；Instance 采用和实际镜像验收仍未完成。 |
| 仓库与镜像选择 | 在指定命名空间列 repository，再选 tag/版本，解析固定 digest/platform，连接现有登记、预检查和部署操作。 | `one-person-lab-app` 与 `chaokang_agent_ibd` 从同一选择入口使用；不把命名空间当作单个镜像仓库。命名空间内 repository/tag 浏览与 tag→固定 digest 解析已实现（管理员路由、Console 表单、OCI Distribution API 客户端）。2026-09-17 修正镜像引用身份：解析响应带 catalog `host`，Console 按 `host/namespace/repository@digest` 精确比对，不再用 `repository@digest` 误拒正确响应；端点默认值移除，未配置端点显式返回 503，配置错误启动失败。2026-09-18 改为声明允许仓库集合：实测同一凭据可读私有仓库 tag（200）但 `_catalog` 返回空列表（200），枚举无法回答"允许哪些"，故 `ListRepositories` 与 `_catalog` 一并移除，改由 `OPL_WORKSPACE_REGISTRY_REPOSITORIES` 声明 `<namespace>/<repository>` 集合；声明为空或含越界命名空间即启动失败。证据见 [镜像引用身份](status.md#image-reference-identity-local-development)与 [Registry 端点归属](status.md#registry-endpoint-and-credential-ownership-local-development)。剩余：多平台 manifest 的逐平台 digest 选择；Instance 尚未提供端点与浏览凭据注入，因此目标环境的真实目录仍待 owner 读回。 |
| IBD 材料与依赖 | 主 OCI、六个知识服务或明确外部绑定、配置/Secret 接口、挂载、健康检查和必需的数据/恢复材料。 | 七个镜像（agent+ragflow+tei+es01+mysql+minio-silo+valkey，tag `20260909-verified`）已推送 TCR 且服务端 digest 与交付包冻结锁一致；依赖组件完整规格（端口/探针/挂载/命令/Secret）的合同与 Local/Tencent 执行已实现并通过 focused 测试。当前按原 20260909 成功部署补齐通用配置文件、Secret 消费、依赖就绪与运行条件，必要通用能力已通过完整回归（18 个 PostgreSQL/Docker 测试包零跳过）并随 [PR #555](https://github.com/gaofeng21cn/one-person-lab-cloud/pull/555) 合并；原镜像与隔离复制知识数据已使用用户指定的 glm-5.3-flash 完成真实检索、问答、SSE、引用与生命周期验收，见 status 中运行凭证。当前补齐 Console 完整发布描述、配置文件和版本化 Secret 引用到 CP/Fabric 的传递。剩余：真实 Console→CP→Fabric 的同 Workspace 替换整体验收、产品数据恢复执行，以及 Tencent 不支持运行要求的兑现；预置数据验证不能冒充产品数据恢复完成。 |
| 既有 Workspace 部署 IBD | Instance owner 安装准确 Candidate，在既有资源上替换 OPL App，完成真实访问、数据与恢复读回并保存不可变 receipt。 | IBD 实际可用，旧实例/无引用镜像清理，资源和原应用数据保持。未完成，属于受保护 Instance 工作流。 |

独立的数据材料目录/登记功能（#550）不是修通现有部署路径的前置。
后续按 IBD 的实际数据输入与恢复消费者确定必要范围。

下面的业务验收场景覆盖上述能力及其完整交付条件。独立部署路径的源码
修复不代表默认安装、应用替换或 IBD 业务投放已经完成。

#### 验收场景 1. Resource-only Workspace

**可交付结果：** 用户购买一个没有应用的 Workspace，资源已开通；可查询、
续费、处理到期并删除。无需应用镜像、应用密码、Gateway Key 或 Runtime。

**真实链路：** 现有购买入口的独立资源请求 → Control Plane 权益/报价和 Fabric
只读资源 preflight → 操作持久化与原账户的一次确认扣款 → Fabric
compute/storage/attachment 执行与读回 → Control Plane 资源激活
→ Ledger 购买 Receipt 写入/精确读回
→ Console 分别展示资源状态和“未安装应用”。随后由真实生命周期 API/worker
完成空 Workspace 的续费、到期和删除。必要的只读采购预检查在扣款及资源变更前完成。

Control Plane 负责新资源操作的领域规则、request/decoder、事务和恢复。
Fabric 同时修改 typed HTTP 输入、preflight、完整性绑定、资源 stage 验证和
持久化 decoder，使新资源合同不再要求应用镜像；旧 Launch 继续按其原合同执行。
仅在 CP 跳过 Key/Secret/Runtime stage 不足以验收这一闭环。

**验收：** 在现有隔离测试基础上经真实 API 和持久化路径完成购买/查询/续费/删除；
响应丢失和进程重启继续原操作，不重复扣款或采购；未知金融结果保留原有 review
语义；Receipt 失败只重试证据写入；成功和未完成的旧 Launch 保留原身份、解码和
履约义务。空 Workspace 生命周期不得伪造 Runtime ready。

**依赖：** 无需 IBD 数据、生产 Secret、DNS 或目标生产资源，即可开始本地实现。

#### 验收场景 2. Administrator Application Deployment

**可交付结果：** 管理员通过现有 Console 在空 Workspace 上独立安装 OPL App，
并可只更新所选 Workspace。默认安装也组合“资源开通”和“应用部署”两个操作。

**真实链路：** Registry/repository/tag 与运行描述输入 → CP 准入不可变 revision
→ 预检查配置、数据绑定、平台/容量和中断方式 → 持久化部署意图及 Workspace
版本预约 → Fabric 通用 Runtime 创建/更新和组件读回 → CP 原子激活当前绑定
→ Ledger 部署结果 → 管理员操作查询和客户应用入口。
新 revision 的准入结果必须到达 CP 与 Fabric，运行固定 digest。

OPL App 的端口、探针、配置、凭据和挂载移到显式描述。
首次部署即引入稳定逻辑数据绑定、独立操作恢复和真实 provider ports；登记、
执行与 Console 在此闭环共同完成。接入当前应用时，同时迁移它所需的查询、
访问/凭据、续费/到期和删除消费者，使本闭环可独立使用。

**验收：** 应用安装失败保留资源购买成功；预检查失败不停止原应用；支持的新
revision 不需要重建/重部署 Cloud；A 更新不改 B 和安装默认值；真实写入在兼容
更新/重启后可读，资源和数据身份保持；部署/配置不重复扣款；OPL App 原登录
和挂载行为可用。失联/迟到结果不能越过预约和版本检查激活。

**依赖：** 闭环 1。复用现有操作/store/provider 基础，扩展实际消费路径。

#### 验收场景 3. General Application Access And Data

**可交付结果：** 一个简单非 OPL HTTP 应用证明通用托管；完整 IBD 在隔离环境中
通过同一套入口、依赖、配置和数据机制运行。

**访问链路：** 管理员部署非 OPL 应用 → 声明端口、配置、Secret 和访问策略
→ Fabric 入口/网络读回 → CP 当前应用访问判断 → 浏览器真实资产/API/Cookie/SSE。
应用绑定使用独立 origin；兼容更新保留其可用访问，不相关应用切换隔离原有
浏览器存储/service worker。管理凭据不转发给应用，匿名访客不获得部署权限。

**数据链路：** 发布者给出完整依赖与一致性数据材料 → CP 独立恢复操作固定
输入和空目标绑定 → Fabric 提供独占写入、运行恢复工具并读回工作负载/存储
→ 应用工具验证数据一致性 → CP 确认数据绑定并记录 Ledger 证据
→ 部署完整组件并验证 IBD 检索、问答、SSE 和引用证据。

恢复业务操作归 CP，数据格式/一致性算法归发布者，存储执行归 Fabric。
先完成并验证 Docker-volume 到 TKE/PVC 的导入能力，再接受相应数据交付的完成
声明。TKE 执行验证由 Instance owner 在明确授权的隔离环境中与 Cloud 迭代配合，
使用已有或另行明确授权的资源；它与闭环 5 对目标 Workspace 的正式投放分开。
本地 Docker 结果只证明本地路径，尚缺的 TKE 证据保留为该 provider 的未完成验收。
普通更新只复用稳定数据；恢复、初始化和 schema migration 使用各自显式语义。
`/state`、tmpfs 或内存会话仍按应用声明处理，挂盘不自动提供持久会话。

**验收：** 非 OPL 根路径、Host/scheme、Cookie、Secret 和 SSE 可用；验证跨
Workspace 和不相关应用的浏览器/数据隔离；真实业务数据跨兼容更新/重启可读；
恢复响应丢失不重复破坏性导入；不兼容数据版本有明确结果，回滚不自动覆盖现有数据。
OPL App 的旧数据保留并隔离，所有依赖与恢复 workload 纳入各自 owner 生命周期。

**依赖与并行：** 闭环 2。共享合同由一个写入者与两端消费者一起更新后，访问和
数据工作可按独立写集并行；IBD 材料准备可从闭环 1 开始。缺少 IBD 材料时，
继续不依赖该材料的实现和非 OPL 验证，只将 IBD 对应验收保留为未完成。

#### 验收场景 4. Retained State And Lifecycle Completion

The retained installation-gateway status/repair URL-consumer regression is
fixed at the source layer; [verification evidence](status.md#retained-runtime-entry-projection-repair)
tracks its checks. Instance deployment and customer entry/credential recovery
remain open and are not implied by source integration.

**可交付结果：** 现有 Workspace 和未完成操作完成 owner-local 迁移；所有当前
消费者在并发、失联和重启后仍正确，原购买历史可核对。

**真实链路：** 精确存量事实与 Fabric 读回 → owner-local schema/decoder/绑定迁移
→ 真实 deploy/update/restore/配置/Secret/renewal/expiry/delete API 和 worker
→ Workspace 预约/CAS 与 Fabric 版本约束 → 全部当前、候选和恢复组件读回
→ 正确当前绑定、权益投影及 Ledger 结果。

此处完成存量覆盖和全面竞争验证；每个前序闭环已经包含其必要生命周期行为。
凭据/Gateway/network recovery 使用当前部署声明；金融核对仍使用原购买/续费
证据。只从精确已存事实和实际读回迁移，不以默认配置虚构已成功部署。

**验收：** 覆盖 deploy/deploy、deploy/expiry、deploy/delete、Secret 轮换与恢复的
竞争，响应丢失、进程和数据库重启、旧响应迟到；到期/删除不会被晚到部署复活；
删除覆盖所有 owned 组件、临时 Secret 和恢复 workload，排除外部共享依赖并保留
Sub2API Key 的既定 authority。验证无重复扣款、保留数据、原财务记录、兼容回滚
及迁移中断续接。完成实际消费者与保留义务迁移后退役固定 ABI/image-only 路径。

**依赖：** 闭环 2、3；通过所需 PostgreSQL 和 Linux Local-Docker 全量验证后，
才进入通用应用的实际 Instance 投放。

#### 验收场景 5. Exact Artifacts And Instance Adoption

**Cloud 交付链路：** 完成前四项 Cloud 实现与应用隔离验证 → 所需源码/跨模块/
PostgreSQL/Local-Docker 检查 → 通过既有 PR/CI/main 流程形成确定 canonical SHA
→ 既有 Candidate workflow 构造确定 digest 与安装包 → 对该 Candidate 进行干净
Linux Local-Docker 安装/使用资格验证。源码检查和 Candidate 验证分别保留证据。

**Instance 交付链路：** Instance 读取同一 Candidate + OPL App 迁移材料 + IBD
完整材料，落实自身配置/Secret/Registry/域名和目标状态 → 受保护 owner 工作流
部署平台并完成必要的存量迁移 → 对目标 Workspace 预检查、中断/替换、导入数据
和部署 IBD → Runtime/数据/真实使用、重启及兼容回滚读回 → 新的不可变 receipt。
目标资源满足全组件及恢复峰值才执行；容量不足返回明确缺项，扩容采购是独立动作。

**验收：** 操作者收到上述五组交付物；目标 Workspace 的完整 IBD 可访问并真实
使用，原资源身份、财务历史和被隔离的 OPL App 数据保持；操作可从相同输入
复现，恢复/回滚程序有实际验证。A/B 独立性在隔离资格环境验证，不扩大实际投放
的 Workspace 范围。正式 Product Release 继续走已有的单独发布流程。

### Handoff Inputs And Parallel Work

The Launch resource-read repair is source-complete (see
[current evidence](status.md#launch-resource-readback)). The Instance owner still
needs to deploy the exact Candidate and observe the original resource identities;
source acceptance does not require that independent production operation.

这些是对应 owner 的材料/运行事实收集任务，可与 Cloud 实现并行。它们不重新
定义架构，也不成为等待全部材料齐备才开始闭环 1 的总开关。IBD 发布者的逐项
材料清单见 [delivery checklist](./delivery/ibd-delivery-materials-checklist.md)。

| 交接输入 | 提供与核验 owner | 最迟消费点 |
| --- | --- | --- |
| OPL App 完整镜像引用、现有登录/端口/挂载、成功及未完成操作的精确绑定 | App 发布者；CP/Fabric migration owner；生产部分由 Instance 读回 | 闭环 2 定义和局部迁移，4 完整存量验证，5 生产迁移 |
| IBD 主 OCI 的完整 registry/repository/digest/platform；六个知识服务、连接图或明确外部服务绑定 | IBD 发布者；Fabric 校验可执行能力 | 闭环 3 的完整 IBD 部署与实际使用 |
| 归档/模型/PDF/索引的一致性集合和校验和；初始化或迁移恢复的明确选择；恢复/验证工具及兼容版本 | IBD 发布者；CP 数据操作与 Fabric 执行 owner | 闭环 3 数据导入和更新/回滚验证 |
| 所有组件及恢复峰值的 CPU/内存/存储/平台要求；目标可用容量、存量绑定和中断约束 | 发布者声明需求；Fabric 能力读回；Instance 核对实际目标 | 隔离测试在闭环 3；生产预检查在闭环 5 |
| Registry 连接、Secret 的接口/格式/消费者、受保护版本引用、DNS/TLS/origin 和 provider policy | 应用发布者声明接口；Instance 提供并管理安装绑定 | 通用接口可用非敏感 fixture 开发；生产值仅在闭环 5 的 owner 边界消费 |

同一共享合同、同一持久化迁移或同一文件保持一个写入者；不同 owner 的实现、
应用材料准备，以及合同确定后的访问/数据能力按独立写集并行。
生产状态和副作用由 Instance 现有保护流程承担；Cloud 本地实现与验证自主推进。

### Acceptance And Completion

| 完成层级 | 必须拿到的证据 | 可以声明的结果 |
| --- | --- | --- |
| 单个业务闭环 | 实际请求到持久化、执行/读回、结果投影及必要 Receipt 的链路；该能力的成功、失败和恢复验证。 | 该项 Cloud 能力已经实现并在所述环境验证。合同/编译/模拟回包单独不能关闭闭环。 |
| Cloud 实现完成 | 前四项涉及的领域、HTTP、schema/decoder、管理员/客户浏览器、保留义务、并发与本地 provider 验证。 | 源码与相应本地行为完成；status 保留检查身份，roadmap 对应项转为 Cloud 完成。 |
| 平台 Candidate 可交接 | 确定 canonical SHA/digest 的安装包，通过现有校验和与 provenance 验证；同一 Candidate 的本地安装/使用凭证。 | Instance 可以消费该平台产物；具体 IBD/Instance 身份仍在各自交付材料和凭证中。 |
| 实际投放完成 | 同一 Candidate、应用/依赖、数据和恢复身份在目标 Workspace 的受保护实际读回；真实业务、数据和恢复/回滚结果及不可变 receipt。 | 完整 IBD 已在选定 Workspace 交付。缺少这一层时，整体投放仍保持未完成，已通过的 Cloud 进度照实记录。 |

优先扩展现有 owning tests：CP 的 Launch/finance/persistence、D4 lifecycle、D5
image business 与 route tests；Fabric 的资源 stage、Tencent vertical、Runtime
readback/update tests；Console 的 Workspace/admin browser tests。它们是测试基础，
通用应用的行为必须新增或更新相应正向用例，不能以旧固定镜像测试代替。

每次变更先跑 focused checks；普通源码变更使用 `npm run verify:local`。
跨模块合同、持久化、迁移、PostgreSQL、资源容量或 Local-Docker 变化以及 Candidate
交付前使用 `npm run verify:local:full`，所需数据库测试不得跳过。
Linux Local-Docker 与 Tencent/TKE 各自提供所声明能力的实际 provider 证据。
健康探针、存在的 PVC、可拉取镜像或只有 desired spec 均不足以证明完整业务交付。

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
explicitly disposable Launch, regenerate its exact owner-derived plan, converge owned Fabric resource
absence before exact debit compensation while retaining Gateway Keys, retain all audit/financial history,
append the reset Receipt and CAS-terminalize the original operation. Unknown,
conflicting or unrelated owner facts must prevent mutation. This is distinct
from normal activated-Workspace Delete. Preview must require schema-valid
`debit/manual_review`, no Workspace projection, exact disposable authority and
no conflicting non-terminal owner operation. Stage position is not absence.
Apply must reject plan drift and use deterministic step identities for response-
loss recovery. Confirmed debit compensation equals the original debit exactly;
unknown debit stops before compensation or terminalization. Existing Receipts,
financial history and the original Launch row are retained. Final redacted
readback must prove zero remaining owned Fabric resources or unreconciled debit,
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
