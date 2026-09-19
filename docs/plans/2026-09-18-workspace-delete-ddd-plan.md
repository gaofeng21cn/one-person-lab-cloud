# medopl Instance 最小业务闭环：部署前检查与验收交接

**更新：2026-09-19**
**原则：非必要不开发。优先复用已有业务入口、受保护工作流和回执。**

**执行形态澄清：** 本次新增的是部署后的一个有界专项测试，不是把所有既有 Instance
资格工具重构为新验收平台。在 Instance 开发分支编写最小专项检查/必要适配，
按既有保护规则交付执行；验收完成后退役仅服务本次的 workflow/helper/专用测试入口，
保留业务修复、必要回归测试和不可变 receipts。分支开发不等于获准从任意分支运行生产。

**Candidate 当前可执行约束（已核对 workflow）：**
`build-opl-cloud-candidate.yml` 的运行 ref 必须为 Cloud `main`，输入是已合入
main 的准确 `product_sha`。因此当前不是“只能从 feature branch 构建”。
不要混淆 workflow 运行分支、构建源码 SHA、Instance 测试开发分支。
如果另行要求改为 branch-only Candidate，这是构建入口政策变更，不属于本专项测试，
不能默认取消现有 main/actor 校验。

## 0. 当前阶段与权威

Cloud 开发及本地交付已经进入主干：PR #567，合并 SHA
`f91f8e565ed1554cc5479da89a9e9073008d3501`。此前开发步骤不再是待办，
不得从旧 Step 0 重做，也不重新设计删除、部署或退款功能。

本文件替代此前的活动开发清单，只描述尚未执行的 Instance 检查和验收，
不是生产执行授权或运行事实证明。执行时重新核对 GitHub、部署和运行状态。

- Cloud 源码/产品规则/本地证据仍由 Cloud 的 architecture、invariants、status、roadmap 归口。
- Instance 部署、环境、资格核验和生产 receipts 唯一 owner 是
  `/Users/huangrende/Documents/ChatGPT/opl-instance-medopl`。
- 先读取 Instance `AGENTS.md`、`docs/runtime/production-runbook.md`、
  `docs/runtime/workspace-qualification.md`、`docs/runtime/operator-workflows.md`
  及 `receipts/README.md`。必要操作说明或工具修正写入这些既有 owner，
  不在 Cloud 新建 medopl 工作流或复制生产环境事实。
- 本文件为本地交接材料，未作为 PR #567 产品文档提交；不再追加完成历史。

## 1. 唯一最小业务链

### 已由用户指定的测试输入（不再询问或另选）

- **客户账号：** `testcloud4@medopl.com`。使用现有账号，不另开户，也不拿其他测试账号替代。
- **密码：** 用户已在当前会话提供；不写入本 plan、脚本、workflow 参数、日志或 receipt。
  执行任务通过批准的 Secret/安全输入取得，不能假定另一个任务自动拥有当前会话密码。
  既有测试客户 Secret 若对应其他身份，不擅自覆盖或使用。
- **购买规格：** 8 vCPU / 16 GiB，核对当前产品目录中的 Pro 映射，不能沿用老 Basic/2c4g 验收条件。
  CBS 容量按该套餐的实际产品规格和实时报价核定，不自行降配或增加资源。
  安装示例目前 Pro 对应100 GB，但示例不是本次运行事实。
- **应用：** IBD OCI，不替换为 OPL App、hello-world 或纯测试镜像。
  当前已存交付材料的主镜像仓库是
  `uswccr.ccs.tencentyun.com/oplcloud/chaokang_agent_ibd`，
  查验 `docs/delivery/ibd-image-lock.md` 和 IBD 发布者运行描述，实际拉取前解析/核验固定 digest及平台。
  沿用已批准交付版本，不擅自选择漂移的 latest。
- **IBD 必需依赖：** 一个目标应用不等于一个容器。现有 IBD 交付材料包含依赖服务；
  只采用该应用运行确实需要的组件、配置和数据，不重做应用、不开发通用依赖平台。
  主页面能打开但必需业务依赖缺失不能算 IBD 功能通过。
- **范围：** 该账号下本次正常购买产生的一个新 Workspace；已有 Workspace 不作为删除目标。
  只删除本次目标，不把同账号其他资源自动纳入。
- **仍需核定：** 该账号实时状态/余额、套餐报价与购买金额上限、最终 IBD 不可变引用/运行材料。
  已指定的账号、规格、应用不再当作待选项；实际价格尚未知，不代表可任意扣款或充值。

### IBD 的“开箱即用”和打包边界

“开箱即用”不等于把所有进程强行塞进一个 OCI image。这里采用可泛化的
**OCI application bundle**：一个不可变的 bundle descriptor 绑定主镜像、依赖镜像、
端口、健康检查、内部连接、Secret interface、持久卷、scratch、数据恢复材料和入口；
Cloud/Fabric 仍按一个 Application Deployment operation 编排它们。用户只选择一次 IBD，
平台在一个 Workspace 内启动主组件和依赖组件，最终只呈现一个应用入口。

当前 IBD 的已锁定材料是一个主镜像加六个依赖镜像，不是一个可独立运行的主镜像。
把它们当成一个“应用包”是泛化方向；把它们合成一个巨型容器会失去独立健康、持久化、
重启、扩展、删除和恢复边界，除非另行决定采用单容器运行时，不能在本专项中偷偷切换。

用户不需要填写内部数据库密码，但生产密码也不能烘焙进 OCI image：镜像可被拉取、
无法按 Workspace 轮换、会跨客户复用。正确的开箱体验是平台在 Workspace 创建时生成或绑定
内部 Secret，并按 bundle descriptor 注入给 MySQL/MinIO/Redis/Elasticsearch/Ragflow 等组件；
用户只提供确实属于应用的外部模型/API Secret。Secret 值只在 Instance 的受保护边界消费，
不进 Cloud source、descriptor、receipt 或日志。仅供本地演示的固定默认密码不得升级为生产策略。

因此，“有 agent 镜像和 CVM”有两种结果：

1. 单组件 agent 使用当前 Tencent 支持的运行描述时，可以直接部署到已开通的 Workspace；
2. 完整 IBD bundle 还必须逐项满足 Tencent provider capability 和数据恢复合同。当前 IBD
   descriptor 声明了 init、自定义 seccomp、scratch 权限和 TEI/知识数据输入，而 Tencent
   adapter 对其中部分能力 fail-closed。不能在部署时静默丢字段；要么给 IBD 提供
   Tencent-compatible descriptor，要么由 Cloud/Fabric 为这些已证明必需的能力补齐实现和读回。

这不是阻止泛化，而是泛化的边界：**Bundle contract 可以泛化，provider capability 必须
逐项可证明，数据恢复不能由“镜像已上传”推断完成。** 在这两个条件未满足前，专项测试可以
验证阻塞并保存 receipt，但不应购买真实 8c16g Workspace。

### 已选方案：IBD all-in-one OCI（方案 B）

本项目现在选择方案 B，目标是让 IBD 采用与 OPL App 相同的用户体验：

```text
一个 IBD OCI image
→ 一个 Workspace application component
→ 一个 Web entry
→ 一个 Workspace CBS 持久化根目录
```

这不是把现有 `chaokang_agent_ibd` 直接当成完整 IBD，也不是在 Cloud 里写 IBD 专用特例拼接
七个镜像。IBD 发布者必须从当前已验证 bundle 生成新的完整 all-in-one OCI artifact；
内容改变就产生新的 digest、新的运行描述和新的资格证据。旧七镜像锁仍保留为历史参考，
不能冒充新的 all-in-one artifact。

#### All-in-one 运行合同

- 一个外部 Web entry，目标沿用 IBD 的 `8082` + `/api/health`；实际值由新 descriptor
  固定，Cloud 不猜端口或健康路径。
- IBD 内部组件在一个容器进程组内通过 localhost 或受控内部连接通信；数据库、搜索、
  对象存储、Ragflow、TEI 不再作为 Cloud application dependencies 暴露给 Fabric。
- 入口 readiness 只有在主服务、必需内部组件、模型和知识数据全部可用时才通过。页面能打开
 但检索不可用不算成功。
- 只声明 Tencent 当前已支持的能力：`RuntimeDefault`、普通用户身份、一个 `/state`
  持久挂载、普通临时目录和 HTTP health check。不得继续依赖当前适配器拒绝的 `init`、
  自定义 seccomp、scratch mode/user/group/executable；若 IBD 确实需要它们，方案 B 停止，
  不能删除字段伪造兼容。
- 内部 MySQL/Redis/MinIO/Elasticsearch 密码在 Workspace 启动时生成或由 Instance Secret
  注入，不写入 image layer。用户只提供外部模型/API Key。
- **模型配置在部署后输入：** 这次专项测试不把 model/API key 烘焙进 image，也不要求
  在 Application Deployment descriptor 中预先声明它们。Workspace 成功部署并有入口后，
  用户通过 IBD 的受保护配置入口提交 `modelName` 和 `modelApiKey`；`modelBaseUrl` 如需
  可变也一并提交。`config.toml` 只作为本地/专项测试 fixture，不能成为生产默认配置或
  Cloud source 的 Secret。
- 回读必须是安全的：modelName/baseUrl 可以回读，API Key 只能回读 `configured/hasKey`
  和 fingerprint/digest，不能回读原文。只有“配置回读 + 使用该配置完成一次真实模型调用”
  才算模型配置成功；页面能打开或仅返回 `hasApiKey=true` 不算通过。
- 当前 artifact 若只把配置放进进程内存，本地测试可以验证“部署后输入→回读→调用”；
  真实用户验收前还必须证明重启后配置可通过 `/data` 的安全状态或 Workspace Secret
  重新注入，不能要求用户每次重启手工再填 API Key。原始 Key 不进入 Cloud operation、
  receipt、日志或 image layer。
- `/state` 使用固定、版本化子目录。首次启动从镜像内只读 seed/模型材料初始化空目录，
  之后只使用持久目录；初始化幂等，不覆盖客户数据。
- IBD 发布者提供 seed manifest、模型/知识数据 checksum、初始化版本和业务验证命令；
  Cloud/Fabric 不理解 IBD 私有格式，只执行 descriptor。

#### Owner 分工

- **IBD 发布者：** 重打 all-in-one image，合并内部组件/seed/初始化，提供 descriptor、
  部署后配置入口（modelName/modelApiKey/baseUrl）、数据布局、健康/业务检查和不可变
  digest。内部凭据由安装生成；外部模型 Key 由 Workspace 用户在部署后输入。
- **Cloud Control Plane/Fabric：** 不增加 IBD 专用分支，按普通单组件 Application Deployment
  合同、`/state`、Secret binding、HTTP entry 和 readback 部署。
- **Instance：** 注入新 digest，在受保护环境部署并保存资格 receipt；不临时改镜像、拷贝旧卷
  或写数据库。
- **Sub2API/Ledger：** 保持钱包与回执职责不变。

#### 最小交付顺序

1. **IBD 可行性：** 证明七镜像 bundle 能在一个 OCI 内启动，列出进程、端口、内部依赖、
   seed、Secret 和 `/state` 布局；不先改 Cloud。
2. **构建新 artifact：** 空状态启动、重启、已有状态升级和删除清理通过本地测试；架构
   明确为 `linux/amd64` 或同时提供其他平台。
3. **Tencent-compatible descriptor：** 只声明已支持的入口、用户、普通挂载、Secret 和
   健康检查；通过 Fabric preflight，不触发 unsupported capability。
4. **本地业务验收：** 真实入口、部署后配置入口、配置安全回读、SSE/检索/引用、seed
   初始化通过。使用 `config.toml` fixture 注入一个非默认 modelName/API key，验证真实模型
   调用；fixture 值不得进入 image、receipt 或日志。随后单独验证重启：配置要么从 `/data`
   安全状态恢复，要么由 Workspace Secret 自动重新注入，不能要求用户重新填写。
5. **Instance 专项验收：** 用 `testcloud4@medopl.com`、8c16g、新 digest 执行
   `opening → application → post-deploy model/API-key configuration → application readback
   and real call → closeout`；资源删除和平台退款复用 Cloud 现有链路。
6. **旧材料退役：** 新 artifact 通过验收后，再决定是否退役一次性七镜像专项入口；历史锁和
   receipts 不删除。

**停止条件：** 如果无法在不损失 IBD 业务语义的情况下合并依赖、模型和数据，或进程组需要
Tencent 当前不支持的能力，则记录 blocker、停止购买，不通过删除声明字段制造“成功”。


```text
准确 Cloud Candidate
→ Instance 部署平台并确认运行版本/健康
→ 指定测试账号购买一次 Workspace（CVM + CBS）
→ 在该 Workspace 直接部署一个指定镜像
→ 通过生成的链接访问并验证最小真实功能
→ 用户通过正常产品入口删除该 Workspace
→ 权威确认对应运行对象/挂载/CBS/Machine/CVM 清理完成
→ 平台按已确定小时策略退款一次
→ 读取删除回执、钱包交易与退款回执并核对
```

指定的 testcloud4 客户、一个8c16g新 Workspace、IBD 一个目标应用、一笔购买、一次删除与退款。
不默认追加升级/回滚、多账号、多套餐、自动备份或压力测试；只有本链实际故障需要时才扩大诊断。
已存在的其他客户资源不作为测试目标，不以“测试清理”为名删除它们。

分清：**平台部署**更新 CP/Fabric/Ledger/Console，**应用部署**在购买后的 Workspace 内运行镜像。
两者不是同一个发布动作。Candidate 不是正式 Product Release。

## 2. 本轮明确允许和禁止

**本轮任务是“完成专项测试的准备与本地实现”，不是只交一份建议，也不是立即上线。**
在 Instance 新建 `codex/` 开发分支前先检查工作树，不覆盖其他未提交工作；
Cloud 不再开开发任务，不改变 Candidate 构建规则。

本轮只执行：

- 本地文件、源码、GitHub 元数据及已脱敏 receipts 检查。
- 必要的既有受保护只读工作流；每次先确认绑定当前已部署 Candidate，不能拿未来 Candidate 冒充当前运行版本。
- 在 Instance 开发分支为本次 Candidate 增加最小部署后专项测试；已有入口确实不兼容时做必要、可逆的局部适配。
- 本地验证，并给出下一轮明确的执行对象、金额界限和待授权动作。

本轮不执行：

- Candidate 构建 dispatch、正式 Release、平台部署、配置/Secret 修改、worker 开关调整。
- 创建账号、充值、真实购买、应用部署、真实删除、退款或历史任务恢复。
- commit、push、PR、合并。
- 本地机器直连生产私网、数据库或调用云资源管理接口。

凭据只能由受保护 workflow 在运行时使用；不下载密码、Token、kubeconfig，
不打印 Secret 值。Secret 名称存在只证明已配置，不能证明能登录。
如无现成合法只读入口，本轮将缺少的事实列为未验证，不能绕过授权边界。

## 3. 部署前检查：四项即可

### Check A：版本、Candidate 与平台部署条件

**必要性：** 确保要部署的是已合并代码，且所有证据描述同一份字节。

检查：

1. PR #567 合并 SHA/tree 与当前主干状态；默认目标为准确合并 SHA。
   后续主干若变化，列明差异，不静默换目标。
2. 是否已有与目标 SHA/tree 匹配的 Candidate：镜像 index/platform digest、
   安装资产校验和、构建 provenance、Candidate receipt 均可校验。
3. 最近部署回执与当前运行读回分别是什么版本；旧成功记录不是当前健康证明。
4. 现有 Preflight/Deploy/Deployment Verification 能否使用该 Candidate；
   protected production runner、数据库/必要迁移、权限及回滚输入是否具备。
5. 部署前先完成 Check C 的历史副作用核查，不能只依据健康检查宣布可部署。

**通过标准：** 目标身份唯一且证据一致，当前版本已知，部署/回滚入口可用。
**未满足处理：** 没有 Candidate 就记录“待构建”，给现有 Cloud workflow 的准确输入；
不要临时换镜像、重建一套发布流程，也不要在本轮擅自构建或发布。

### Check B：现有测试账号、额度与镜像

**必要性：** 优先使用已有测试身份，不重复开户，不购买后才发现权限或镜像不可用。

检查：

1. production 已配置管理员与测试客户 Secret 引用：
   `OPL_SUB2API_ADMIN_EMAIL/PASSWORD`、`OPL_TENCENT_MVP_CUSTOMER_EMAIL/PASSWORD`。
2. 使用已有受保护只读入口核对测试客户当前登录、账号启用、唯一 Cloud/Sub2API
   映射、购买权限、余额、billing guard、未终结订单。
   只有 Secret 名称清单或历史登录证据时，明确“本次登录尚未验证”。
3. 一次目标套餐的实时平台报价、对应规格、余额是否足够；采购保持现有预付月付规则，
   不能用 POSTPAID_BY_HOUR。平台按小时退款不等于腾讯资源按小时采购。
4. TCR 浏览和实际拉取所需 Secret 引用、允许 repository、选定 tag→digest、
   目标平台、真实端口、必需挂载/Secret、DNS/TLS 和入口配置。
   用户已经指定 IBD；不再请求选应用，只核对其已批准交付材料并固定 digest。

**通过标准：** 一个可识别的授权测试身份、足额报价和购买许可、一个可部署镜像。
**未满足处理：** 明确缺少账号状态、额度还是镜像材料；不新开户、不充值、
不猜默认端口/目录，不把管理员账号当成购买客户。

### Check C：部署会不会继续处理历史任务

**必要性：** 新 worker 启动后可能自动继续原删除或扫描已成功删除的记录进行退款。
部署平台虽然不直接调用购买接口，也不天然等于零业务副作用。

检查：

1. 当前 launch/delete/renewal/refund 未终结操作及有效授权范围，避免重复购买。
2. **包括历史 succeeded delete**：新 worker 会否将其纳入退款评估；是否已有退款、
   原始身份和扣款证据是否完整、可能触发的钱包或资源动作是什么。
3. 当前 worker 配置及待部署 Candidate 的启动行为；说明对非本次测试对象的影响。
4. 仅保存所需脱敏身份摘要、状态、数量与风险结论；不做无边界客户数据导出。

**通过标准：** 可能自动续接的对象和副作用已知，明确属于已有有效授权或需新增授权；
不得只说“会自动恢复，所以安全”。
**未满足处理：** 暂不部署，列出具体待确认对象/范围。
禁止为过关而在本轮关 worker、清历史记录、补造 receipt，或新建特殊 test billing 模式。

### Check D：部署后的专项测试如何最小落地

**必要性：** 老资格工具可能验证的是另一条业务场景，不能为了本次测试强行改写其语义。
本次在平台部署并通过 Deployment Verification 后，独立执行一个新链路专项测试；
不把真实购买、删除或退款隐式挂到 Deploy 成功的自动后续步骤。

对准确 Candidate 的接口/schema 核对现有：

- `tools/fresh-workspace-admission.mjs`
- `tools/fresh-workspace-readback.mjs`
- `.github/workflows/verify.yml`
- `workspace-state-observation` 及现有应用 preflight/readback 能力。

重点：

1. 资源开通完成应验证扣款、CVM/CBS/绑定和购买回执，不强行要求此时已部署应用。
2. 应用部署另验证所选 digest、配置、Runtime、入口和实际访问结果；
   无相关凭据需求时不得强行要求新增 Gateway Key。
3. 删除/退款在后续独立阶段核验：原 Workspace、资源身份、删除回执、
   原平台订单、实际钱包退款与 business_refund 回执相互对应。
4. 开通阶段检查“没有意外退款”与删除阶段检查“应退且仅退一次”不可混用。
5. 操作状态 GET 与真实 provider 读回各证据层分开；不把 HTTP 200 或页面消失当成物理删除。

**通过标准：** 一条专项测试可按本次新业务语义核对各阶段，成功/失败都有持久证据；
没有套用旧测试中“一次 Launch 必须产生 Runtime/Key”的条件，且不破坏原测试。
**允许的最小修正：** 复用已有 Candidate、认证、保护、只读访问和脱敏能力；
仅增加本次场景缺少的专项 helper/workflow、必要 schema/validator 及其测试。
若现有部署入口本身与 Candidate 不兼容，先复现，再做局部必要修正；
不能把“旧测试不是本场景”当成重构整个 Instance 的理由。
真实购买/部署应用/删除仍通过正常产品命令，并在专项阶段拥有明确对象和额度授权；
只读 verify 原有零写入约束不被放松，不添加第二业务执行器。
新专项生产入口仍遵守 Instance main + protected production + authorized runner；
开发分支用于修改和审查，不利用旧诊断分支特例绕过保护。
产品缺陷如被实际复现则交还 Cloud owner，不用 Instance 直接改数据库兜底。

**专项退出条件：** 本次准确 Candidate 的限定业务链完成并保存有效 receipts 后，
删除仅为本次服务的 workflow/helper、专用测试及文档入口，并检查没有活动调用者。
保留不可变 receipts、它们必要的读取/校验能力、已修业务源码和仍有消费者的通用回归测试。
测试失败时先保留证据与继续条件，不先删除调查所需工具；退役走正常审查，不能修改历史回执。

## 4. 本轮交付物与结束标准

输出一张检查表：`检查项 / 通过或未验证或阻塞 / 证据 / 最小下一步`。
其中明确：

- 目标 Cloud SHA、Candidate 是否已存在；当前运行版本是否已读回。
- 测试账号凭据是否已配置、当前是否登录验证、权限和余额是否满足。
- 历史任务在部署后可能自动执行什么，是否需要授权。
- 验收工具是否兼容；若修改，给具体根因、实际 write set、focused tests 与本地验证结果。
- 下一轮拟采用的一个账号、一个套餐、一次购买总额上限、一个镜像 digest、
  一个目标 Workspace（购买后由原 operation 确定）、一次删除与自动退款范围。
- 未核定的值明确留为“待核定”，不能猜金额、账号、镜像或退款值。

必要 Instance 修改执行仓库现有 `npm run validate`、`npm test`、`git diff --check`，
先 focused 后仓库验证；无修改不为形式重复全套 Cloud 测试。

只读 workflow 已运行则按 producing tool 的 schema/validator 保存脱敏回执，
包括 unavailable/blocked。检查结论不能冒充部署或业务成功 receipt。
不为一张准备检查表新建通用 receipt 类型。

完成 A–D 检查并明确缺口/执行输入即结束本轮。等待授权不是产品开发未完成，
不继续扩大检查或默认替用户执行下一阶段。

### 最小专项交付的具体验收

- 一个明确命名的部署后专项入口，或复用已有可表达本场景的入口；不同时新建多套。
- 绑定准确 Candidate、成功的 Deployment Verification、指定账号和唯一购买 operation；
  开通后产生的 Workspace 身份从该原操作取得，不能按“最新创建的资源”猜测。
- 阶段明确区分资源开通、应用部署/访问、删除、退款；记录各自原始 operation/receipt 引用。
- 沿用既有幂等/身份和结果未知时读回语义，不创建另一套持久业务状态机。
- 本轮用本地 fixture 验证通过路径及必要拒绝路径：错误 Candidate/账号/Workspace、
  部分资源仍存在、读取未知、重复运行。不得为了验证工具而真实购买。
- 失败明确留在哪个阶段，保留已用授权/操作引用，不能从头再买一个或重复退款。
- 显式标明专项入口/helper、对应专用测试及文档的退役清单；成功 receipts 和其必要
  校验能力不在删除清单。退役在真实验收完成后另行交付，不在本轮提前删除。

**本轮完成口径：** “专项入口与必要适配已实现，本地测试通过，生产执行输入及待授权
范围已明确”。没有 Candidate、镜像选择或有效账号事实时，逐项如实列明，不假装全部就绪。

## 5. 后续授权执行顺序

1. **构建准确 Candidate**：已有合格 Candidate 则复用；无则走现有 Cloud 构建流程，
   不做正式 Release。必要 Instance 本地工具修改先按独立授权交付到 Instance main。
2. **部署前基线与预检**：使用当前运行身份记录基线；确认账户/历史动作范围和回滚输入。
3. **Instance 部署平台及独立版本/健康读回**：准确 Candidate 输入；此阶段授权必须覆盖
   Check C 识别的后台自动动作影响，不能默认扩大。
4. **资格决策与购买前 admission**：本次涉及生命周期/计费行为，应按现有 impact
   路径判定，不能把旧 Workspace receipt当作新链验证。只有部署和准入成功才购买。
5. **正常购买一次**：限 testcloud4 账号、8c16g套餐及明确金额上限；不自动充值、不自动续费、不重发未知购买。
6. **同一 Workspace 部署 IBD 并访问**：经正常产品入口，主镜像及必要依赖固定digest，
   验证真实HTTP/必要业务功能；如声明持久数据，仅使用可删除的合成测试数据验证。
7. **正常删除并验退款**：同一原操作等待 Runtime控制器/子对象、挂载、CBS、Machine/CVM
   权威删除完成。停止或SHUTDOWN不算完成；平台按已确定720小时政策和有效付款期间退款，
   不依赖腾讯退给平台多少钱，不新增本地计费公式的第二实现。
8. **最终核对**：删除/退款回执可取回；退款对应原扣款且只执行一次、金额正确；
   刷新/replay不重复副作用；无目标资源残留、不影响其他Workspace。未知时保留原操作和
   已消费授权，不再次购买/退款或另找资源“补一个成功”。

**最终验收标准：** 同一 Candidate、测试账号、Workspace 的购买、部署、访问、删除、
平台退款与回执均能关联到实际 owner 结果。任何失败只定位最早不确定阶段，
不追加新的产品能力来绕过。不承诺所有外部操作必成功，但不得将请求已提交报为成功。

## 6. 当前执行批次：四个明确动作

本批次由 DeepSeek 在 `opl-instance-medopl` 的开发分支准备并按受保护 workflow 执行。
不能用本地 shell 直接连接生产，也不能把 `/Users/huangrende/.codex/config.toml`
复制进仓库、镜像、日志、receipt 或 workflow 参数。模型名和 API Key 只在受控进程内读取；
API Key 不能出现在回读原文，最多留下 `hasKey` 和 fingerprint。

### 动作 1：部署平台并打开 Console

**目的：** 让准确 Cloud Candidate 在 `cloud.medopl.com` 运行，确认平台本身先可用。

**步骤：**

1. 验证/推送 IBD `modelcfg` 新 artifact 到批准 TCR，并回读最终 digest；未回读就不能使用本地 digest。
2. 若 Candidate 尚不存在，按既有 Cloud `main` Candidate workflow 构建合并 SHA
   `f91f8e565ed1554cc5479da89a9e9073008d3501`；不改 Candidate 构建规则，不把 Instance 分支当构建源。
3. 在 Instance `main` 的 protected production 流程执行 Preflight → Deploy → Deployment Verification。
4. 打开/读取 `cloud.medopl.com` 及健康端点，核对实际 Product SHA、Cloud image digest、
   三个 owner chain、TLS/Ingress 和健康；失败只保留 blocked receipt，不进入购买。

**完成证据：** Candidate receipt、deployment receipt、deployment verification receipt、
   实际运行版本读回。平台健康不等于 Workspace 业务成功。

### 动作 2：开通指定测试 Workspace 并部署 IBD

**目的：** 验证 `testcloud4@medopl.com` 的 8c16g 购买和单组件 IBD 部署，不影响该账号其他 Workspace。

**步骤：**

1. 先执行购买前 admission，确认账号登录、唯一映射、`workspacePurchaseEnabled`、余额足够、
   billing guard、没有会改变范围的未终结操作；密码只从 protected Secret 读取。
2. 通过正常产品购买一次 `pro`/8 vCPU/16 GiB Workspace；按实时报价核定金额上限，
   不自动充值、不重发未知购买。
3. 保存该次唯一 purchase operation identity，不按“最新 Workspace”猜目标。
4. 将已回读的新 IBD all-in-one digest 作为应用镜像，在该 Workspace 通过正常部署入口部署。
   只使用一个目标应用和合成测试数据。

**完成证据：** purchase operation/receipt、Workspace identity、CVM/CBS identity、deployment
   operation/receipt、Runtime ready/readback 和唯一入口。只开通资源但未部署应用不能伪称应用已通过。

### 动作 3：部署后输入 model/config.toml 并验证真实 IBD

**目的：** 验证用户真正关心的流程：部署后输入模型名和 API Key，而不是依赖镜像默认值。

**步骤：**

1. 从 `/Users/huangrende/.codex/config.toml` 读取本地测试 fixture 的 model name、base URL（如有）
   和 API Key；只在受控进程内使用，先检查必需字段，不打印值。
2. 通过 IBD 自己的受保护配置接口提交 model name/API Key；不把它们伪装成 Cloud 固定 descriptor，
   不把 Key 写入 Cloud operation、receipt、日志或镜像。
3. 回读配置状态：model name/base URL 可见，`modelConfigured/hasKey` 和 fingerprint 正确，
   API Key 原文不可见。
4. 提交一个只依赖 IBD seed 知识的真实问题，要求真实回答包含知识事实和引用；只看到 HTTP 200、
   页面打开或 `hasKey=true` 不算通过。
5. 重启/恢复后再次回读并调用，证明配置没有丢失；如 provider 返回 401，记录为外部凭据阻塞，
   不把配置链路误判为通过，也不换一个未授权 Key。

**完成证据：** IBD 配置回读、fingerprint、模型调用状态、脱敏回答/引用摘要和重启读回。
不保存答案原文或 API Key。

### 动作 4：删除同一 Workspace 并验证退款

**目的：** 完成最小闭环，证明部署后的 Workspace 可安全删除且平台只退款一次。

**步骤：**

1. 通过正常 Workspace 删除入口提交删除；绑定动作 2 的原 purchase/Workspace operation。
2. 等待 Runtime 控制器/子对象、TKE 挂载、PVC/PV、CBS、Machine、CVM 权威确认删除；
   `SHUTDOWN` 或页面消失不算完成。
3. 等待 `workspace.deleted.v1` 回执，读取阶段证据摘要。
4. 等待平台 `business_refund` operation 与退款回执，核对当前有效付款期间、720h 策略、
   原扣款和金额上限；刷新/重跑不产生第二笔退款。
5. 做最终只读核对：目标资源无残留，其他 Workspace 未受影响，删除/退款 receipts 可由 owner 读取。

**完成证据：** 删除回执、资源最终读回、钱包退款记录、退款回执，以及无重复 mutation 的计数。

### 本批次停止条件

- Candidate、IBD digest、账号、购买 operation 或模型凭据任何一个无法权威核对：停止，不猜。
- IBD 真实模型调用失败：先区分配置链路、provider 凭据和应用框架错误；不自动换 Key、不重复购买。
- 删除或退款结果未知：续接原 operation，不重买、不重删、不重退。
- 任意动作超出当前授权对象或金额上限：停止并保留 blocked receipt。
