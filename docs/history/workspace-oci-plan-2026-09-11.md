# 历史：Workspace OCI 应用交付分层计划

> **状态：已被取代，不能作为执行指令或完成证据。**
> 当前方案与交付顺序由 [roadmap](../roadmap.md#workspace-application-decoupling)
> 唯一维护；DDD 边界由 [architecture](../architecture.md#ddd-model-and-consistency-boundaries)
> 维护。以下保留原草稿，防止重新引入独立合同阶段、先验收部署后实现执行器、
> 先验收 IBD 问答后恢复数据等顺序问题。旧草稿中的 digest 和材料仍须由产物 owner
> 核验；本次归档不表示任一能力或生产部署已经完成。

> **Goal:** 将 Workspace 从固定 OPL App Runtime 解耦为资源独立开通、管理员选择 OCI 应用、稳定 CBS 数据绑定和可验证应用替换的通用交付链路。
>
> **Architecture:** Control Plane 拥有 Workspace、应用准入、部署意图和当前应用；Fabric 拥有计算、存储、Runtime、Secret 绑定和 Provider readback；Ledger 记录证据；Console 只调用 Control Plane。保留现有服务边界，不新增工作流引擎或全局事件总线。
>
> **Scope:** 默认 OPL App；管理员从 TCR Registry/Repository/Tag 选择其他已准入 OCI；实际运行固定 digest；不同 Workspace 的版本和数据独立；测试数据只进入本地或隔离资格环境。

## 阶段与交付门槛

### Phase 0 — 基线和合同（已开始）

**Owner:** Cloud 架构/Contracts。

**交付：**

- `WorkspaceApplicationRevision`：digest、平台、端口、探针、资源、挂载、Secret、依赖、访问策略。
- `WorkspaceApplicationDeployment`：目标 Workspace、前驱版本、配置摘要、Secret/Data binding、期望 Workspace 版本、幂等键。
- 严格校验和合同单测。
- 现状基线、DDD 边界、路线与证据层级文档。

**验收：** Go 合同测试、产品边界检查、链接检查通过；不宣称通用部署已实现。

### Phase 1 — Resource-only Workspace

**Owner:** Control Plane；Fabric 负责资源读回；Ledger 负责购买 Receipt；Console 负责状态展示。

**代码范围：**

- `services/control-plane/internal/server/workspace_launch_reconciler.go`
- `services/control-plane/internal/server/workspace_launch_fabric_stages.go`
- `services/control-plane/internal/server/workspace_launch_activation.go`
- `services/control-plane/internal/server/routes_workspace_launch.go`
- `services/control-plane/internal/server/ent_state_store_workspace.go`
- `packages/contracts/go/stage.go`

**实现：**

- 新增 resource-only provisioning mode/stage plan；旧 Launch stage plan 保持不变。
- 新资源开通只要求 debit、compute、storage、attachment、resource activation、Receipt。
- 不要求应用镜像、Gateway Key、应用 Secret 或 Runtime。
- Workspace 激活状态使用 `resource_ready`/`application_pending`，不得伪造 Runtime facts。
- 空 Workspace 的查询、续费、到期、删除和恢复读回使用资源/权益事实。

**验收：**

- 空 Workspace 可完成开通并有购买 Receipt。
- 开通失败不重复扣款、不删除已交付资源。
- 空 Workspace 可以查询、续费和删除。
- 旧 Launch 的 decoder、幂等、恢复和全部既有测试继续通过。
- 测试 fixture 不连接真实生产、不写真实账户。

### Phase 2 — OPL App 默认应用

**Owner:** Control Plane/Fabric；Console 管理员界面。

**实现：**

- 登记 OPL App `ApplicationRevision`。
- 资源开通完成后由独立部署操作安装 OPL App。
- 将端口、探针、挂载、Secret 和访问策略从固定运行常量迁移到描述。
- 保留既有 OPL App 数据绑定和登录行为。

**验收：**

- 资源开通与 OPL App 部署有独立状态。
- OPL App 部署失败不反向失败购买。
- 默认 Workspace 的当前应用为 OPL App。
- 旧 OPL App 访问、凭据和数据回归通过。

### Phase 3 — Registry/Repository/Version 与管理员准入

**Owner:** Control Plane；Instance 提供 Registry 凭据和准入策略；Console 展示选择器。

**实现：**

- 管理员选择 Registry → Repository → Tag。
- 后端解析 tag 为 digest；部署只绑定 digest。
- 保存应用运行描述和不可变 revision。
- Registry 凭据不进入浏览器或应用容器。
- CP/Fabric 使用同一份准入结果，不再只依赖环境镜像目录。

**验收：**

- 可以登记 `one-person-lab-app` 和 `chaokang_agent_ibd`。
- 普通用户不能调用管理员分发 API。
- `latest` 不成为运行绑定。
- 新 Repository/Revision 不需要重建 Cloud。

### Phase 4 — Preview/DeployApplication 与 Fabric 通用 Runtime

**Owner:** Control Plane 编排；Fabric Runtime/Provider；Contracts。

**实现：**

- 管理员指定 Workspace 和 ApplicationRevision。
- 预检查端口、平台、资源、Secret、依赖、CBS/PVC 和访问策略。
- 持久化部署意图并使用 Workspace CAS/幂等操作。
- Fabric 接收声明式 RuntimeGroup，而不是 OPL App 固定输入。
- 停止旧 Runtime、删除旧 Deployment/Service/临时 Secret，保留 CVM/CBS/TCR/历史记录。
- 部署目标 OCI 并执行组件级 readback。

**验收：**

- 预检查失败不停止旧应用、不删数据、不扣款。
- 新 Runtime digest、平台、端口、探针、Secret、挂载均与声明一致。
- CVM 和 CBS/PVC 身份保持不变。
- response loss 可恢复原 operation；迟到结果不能覆盖新部署。

### Phase 5 — 应用验证、URL 切换和生命周期迁移

**Owner:** Control Plane Access/Lifecycle；Fabric；Console；Ledger。

**实现：**

- 健康检查后执行真实业务验证。
- 成功后切换 Workspace 当前应用指针和 URL。
- 访问代理支持应用自身 Cookie、匿名访问、SSE 和独立 origin。
- 迁移 renewal、expiry、delete、credential、Gateway/network recovery 到当前部署/资源证明。
- 配置、Secret、exposure 变化形成后继部署版本。

**验收：**

- IBD 真实请求、检索、问答和 SSE 通过。
- OPL App 自有登录保持可用。
- 当前 URL 只指向已验证应用。
- 删除覆盖当前、候选和 restore workload，但不删除外部共享依赖。

### Phase 6 — CBS 数据恢复、更新和回滚

**Owner:** Fabric Storage/Restore；应用作者；Control Plane 数据操作；Ledger 证据。

**实现：**

- 逻辑 DataBinding 与镜像 digest 解耦。
- Tencent 使用稳定 StorageVolume/PV/PVC/CBS 绑定和声明式 mount。
- Docker named-volume 数据恢复器适配到 TKE/PVC 空目标。
- 普通更新复用已验证数据；Restore 是独立操作；Schema migration 独立记录。

**验收：**

- IBD v1→v2 后真实业务数据可读。
- 重启、更新、兼容回滚不重复恢复、不覆盖数据。
- Workspace A 更新不改变 Workspace B 的 digest 或数据。
- OPL App 旧数据保留但不自动挂载给 IBD。
- `/state`、tmpfs 和容器 writable layer 不被宣称为持久数据。

### Phase 7 — IBD 受保护投放

**Owner:** `opl-instance-medopl`；Cloud 提供 Candidate；IBD 作者提供应用材料。

**前置：** Phase 1–6 通过，Cloud Candidate 绑定确定 SHA/digest，目标 Workspace 不是测试数据入口。

**应用材料：**

- 主 OCI digest `sha256:2fcfa6cd799ada43f6977621da9d7e2595a0b608c9207b9eaf06dd150f6fcf64`。
- 六个知识服务或已批准的等价依赖绑定。
- knowledge 数据卷、模型配置、Secret、PDF/索引材料及校验摘要。
- 运行/恢复/真实问答验证说明。

**验收：** Instance receipt 必须绑定 Cloud Candidate、主/依赖 digest、CBS/PVC 身份、数据摘要、时间和实际 readback。只有该 receipt 才能声明目标 Workspace 已部署 IBD。

## 全局验收规则

- Cloud、本地资格环境、Instance 生产环境的证据分开保存。
- 测试账户、测试数据、测试 Key、测试 Workspace 和测试 Receipt 不进入真实生产。
- `npm run verify:local` 是普通检查；涉及 PostgreSQL、Fabric、持久化、合同或 CBS 时运行 `npm run verify:local:full`。
- Provider claim 必须有对应 Provider 的实际 readback；Cloud 测试不能替代 Instance receipt。
- 任何外部 mutation 都必须有确定 digest、目标身份、幂等/CAS、超时只读 reconcile 和最终 owner readback。
- 不能把合同、代码编译、健康探针或 PVC 存在误报成完整应用交付。
