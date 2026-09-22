# 01 领域、服务与可替换边界

> 目标设计；范围和状态以00为准，字段以02/SQL为准，外部操作以03为准。Domain不等于插件，不等于必须立刻建一个仓库。

## 1. 目标调用图

```text
客户/管理员浏览器
  └─ Console UI（React/TypeScript）
      └─ Console BFF（REST产品接口、会话、CSRF、聚合；无业务库）
          ├─ Capability：Agent/Package/目录/准入引用
          │    └─ Build：不可变输入→构建→OCI确认→完成事件
          ├─ Workspace：报价接受/生命周期/业务Saga/选中Deployment
          │    ├─ Resource Catalog：批准套餐与价格/只读报价
          │    ├─ Gateway Integration：身份/钱包主体/Key/扣退款接口
          │    │    └─ Sub2API：身份、可消费余额、Key、Token用量
          │    ├─ Runtime Control：实例部署/验证/切换/回滚
          │    │    └─ Fabric：资源、挂载、Secret、Runtime执行与实际回读
          │    └─ Ledger：原单、执行/费用/删除证据
          └─ 各owner操作详情/审计只读聚合

Framework/OPL App ──发布版本化Runtime合约──> 管理员准入目录
Instance ──提供部署/Storage/Registry/Provider配置和Secrets──> 各执行服务
```

Cloud产品仓库负责可移植服务和发布，Instance负责真实实例配置/部署/资格。图上的边是明确API或事件，不是跨库读取权限。

## 2. 领域分工与内部lane

| Owner/部署单元 | 内部lane及职责 | 数据写入面 | 真实调用方 | 明确不拥有 |
|---|---|---|---|---|
| Console UI | 路由/表单；只读model；异步Operation展示；会话生命周期 | 只保存UI草稿与非秘密轮询身份 | 客户/管理员 | 钱包事实、业务状态机、provider、业务库 |
| Console BFF | Gateway认证会话；CSRF/请求上下文；产品DTO聚合 | 仅服务端session安全存储，无业务operation登记表 | Console UI | 扣费决定、Saga、跨域表查询 |
| Capability | Namespace可见性；Package元数据/上传；版本目录；Runtime/WebUI准入；引用claim | Package、版本、目录、reference claims及本域事件 | BFF/Build/Workspace/Runtime Control | 构建执行、运行事实、钱包、Registry实现 |
| Build | 任务准入；输入固定；隔离构建；推送读回；注册交接；任务证据 | BuildJob/输入摘要/步骤/产物/日志索引 | BFF/Capability的typed命令 | Package元数据writer、运行资源购买、版本目录writer |
| Workspace | 归属与权益；报价接受/订阅；部署/升级/调整规格；续费/删除/退款Saga；只读产品结果 | Workspace/Deployment/周期义务/业务Operation/steps | BFF/到期worker/管理员命令 | 可消费余额、provider原始事实、Runtime实现 |
| Runtime Control | 准入契约；期望实例；启动/健康；切换/回滚；config reload | RuntimeInstance/配置版本引用/执行Operation | Workspace/BFF只读 | Workspace购买历史、付费决定、应用内部业务 |
| Fabric | provider适配；资源分配；挂载；Secret注入；运行执行；实际回读 | provider operation、机器/存储/挂载/执行绑定 | Runtime Control/Workspace/受保护operator | 套餐销售价格、客户余额、Ledger证据writer |
| Gateway Integration | Gateway身份适配；Cloud主体/Tenant权限映射；钱包主体委托；Key；扣退款一次性调用与回读 | 映射/授权上下文/交易请求身份及观察结果 | BFF/Workspace | 可花费余额缓存、密码、第二套资金流水权威 |
| Resource Catalog | provider-neutral套餐；价格版本；兼容性/可售性；报价生成 | 套餐/价格策略/不可变报价及有效期限 | BFF/Workspace/Fabric preflight读取 | 资源执行、钱包记账、已接受订阅义务writer |
| Ledger | 类型化receipt验证；append-only保存；索引；对账证据 | Receipt/证据索引/对账报告 | 所有业务owner | provider mutation、业务Saga、付款重试 |

BFF鉴权不意味着拥有Identity。Tenant权限属于Cloud业务映射，认证结果来自Gateway。一个Tenant的钱包主体与成员登录身份显式区分，不能把“知道用户ID”当成跨用户扣费授权。

## 3. 物理边界与仓库落点

- 目标业务服务使用Go、独立module、独立database/角色与镜像；数据库名称/字段以02为准。proto中的service只分组API，不按接口组增加部署进程；CloudIdentity与Gateway适配同一部署单元。
- 新服务可依次从现有Control Plane提取为独立交付仓库。目标逻辑名：opl-cloud-capability、opl-cloud-build、opl-cloud-workspace、opl-cloud-runtime-control、opl-cloud-gateway-integration、opl-cloud-resource-catalog。
- 这些是方案约定的交付单元名，不表示远端仓库已经创建；创建/发布由后续明确任务执行。前后端不得依靠共享数据库目录来集成。
- Fabric与Ledger保持既有模块/权威；没有第二个Fabric或Evidence账本。Ledger新增build事件类型必须更新真实验证器，而不是新建一个匿名收据服务绕过规则。
- Console UI保留现仓apps/console-ui；BFF交付路径apps/console-bff。部署到哪里由Instance配置，前端不携带provider选项。
- 同服务内部先用直接函数与类型；只有真正跨Owner边界用gRPC或事件，不能把每条lane都变网络服务。

## 4. 可替换组件契约（“一切可插件”的可检验含义）

| 扩展点 | 不可变输入/合约 | 权威/实现方 | 更换时必须验证 | 不能做的事 |
|---|---|---|---|---|
| Agent Package | OMA/Framework定义的包版本、manifest摘要、字节摘要 | Package发布者；Capability准入；Build执行 | 格式版本/签名策略/依赖/路径安全/权限需求 | Cloud另造不兼容Package格式 |
| Runtime | RuntimeVersion含OCI digest、platform、runtimeContractVersion、入口/端口、健康探针、Package/WebUI集成方式、Secret/配置/数据契约 | Runtime发布者；管理员准入 | digest可拉取、构建输入兼容、Key不泄露、模型可用、数据升级/回滚条件 | 写死所有Runtime都使用/opt/opl-app/start.sh |
| WebUI | WebuiVersion含制品digest、UI协议版本、构建/运行集成方式、静态根路径 | UI发布者；管理员准入 | 与Runtime协议兼容、路由/静态资产/登录/SSE正常 | 用随机COPY路径假设所有UI相同 |
| Storage Provider | Put/Head/Read/Verify/Delete已授权对象；对象引用+sha256+size；只读短期凭据 | storage adapter/Instance凭据 | 精确对象版本与摘要、续传/过期/隔离、删除引用保护 | PostgreSQL存包二进制、长效bucket key到客户端 |
| OCI Registry | repository+digest；push/head/read manifest；受限凭据 | 外部Registry；Build推送；Capability保存引用 | 远端manifest与平台匹配、共享digest引用、权限 | Cloud再造Registry |
| Fabric Provider | typed capability/preflight/execute/observe；resource profile版本 | Fabric adapter | 可用能力、资源/存储/Secret/readiness事实、相同输入读回 | 偷换provider、TKE字段进入客户DTO |
| Gateway Adapter | authenticate/authorize billing principal/charge/read/refund/key/usage | Sub2API权威，Integration适配 | 原单幂等、授权、金额精度、未知结果读回 | 新钱包、余额双写、retry造成多扣 |

Runtime/WebUI目录必须记录具体可验证contract，而不只给“兼容=true”。当前OPL App适配器可实现发布者现行约定，但路径仅属于该适配器。第三方Runtime需提供相同能力事实，不能以启发式探测冒充准入。

## 5. 服务之间的通信规则

### 5.1 同步调用与用户授权

内部唯一wire规格为contracts/internal.proto。所有业务服务使用各自mTLS服务身份；浏览器不能声明内部caller、issuer或权限。服务身份回答“谁在调用”，CloudIdentity授权回答“这个用户/已接受业务义务是否允许此动作”，两者不能互相替代。

CloudIdentity是Gateway Integration内已有的Cloud身份/租户子模块，不新增身份微服务、不新建密码库、不引入通用权限框架。认证身份来自Sub2API；Tenant/成员授权事实仍由CloudIdentity持有。

具体执行：

1. BFF登录后取得服务端session上下文；内部请求带CallContext.actor/tenant/request/action/resource及session/context reference。mTLS caller从连接提取，不能相信body中的自报caller。
2. 接收Owner调用CloudIdentity的typed AuthorizeAction，明确audience=本Owner、动作、资源、当前请求hash、交互session或accepted-operation grant。CloudIdentity查自己持久的成员/授权版本/Tenant/session状态，返回typed允许或拒绝、权限版本和依据。
3. Owner同时检查自身资源归属、生命周期和版本前置。上游“允许”不替代下游对本域对象的校验。
4. 首次命令通过后，业务Owner持久Operation和确切输入，再为该已接受义务绑定受限operation grant。grant只能完成该operation的列明动作/资源/钱包原单，不能创建另一个Workspace或换Tenant。后台worker不借用浏览器session做永久授权。
5. 后续阶段验证该grant及Owner的原意图/epoch。用户退出只撤销交互session，不丢已接受付款/资源义务；Tenant停用/删除禁止新增购买/采购，已有动作只允许必要读回、释放、按原单结算和证据收尾。不能用过期session绕过停用。
6. 成员权限/停用变化递增授权版本，后续交互写入重新核验；跨域授权决定不被无限期缓存。明确拒绝返回稳定错误，不自动改用平台管理员权限。

签发、查询、验证、撤销、grant字段以及服务/动作allowlist都在proto对应消息和02 CloudIdentity表中实现；不再留下“authorization_context_id由研发自行解释”的空洞。

操作查询采用/api/v2/operations/{owner}/{operationId}，保留原operationId。BFF按有限OperationOwner枚举路由，没有全局注册表或遍历多个服务猜归属。Ledger同步append/read receipt，不为统一形式创建闲置Operation服务。

### 5.2 Domain互通矩阵：只保留有调用者的边

| 调用者→Owner | 何时/业务链 | 传递的权威输入 | 接收方写入/结果 | 不允许代替的证据 |
|---|---|---|---|---|
| BFF→CloudIdentity/Gateway | F01登录、任意新命令授权 | 个人身份、session或受限grant、action/resource/audience | session/授权结果；Gateway认证仍是外部权威 | 仅带tenantId或service token不是用户授权 |
| BFF→Capability | F02–F04/F06/F03 | 分组、包版本、发布空间、完整发布描述 | 元数据/上传会话/目录版本 | 前端声称已上传不等于实际对象确认 |
| Build→Capability | F04/F05构建前 | Package/WebUI选择、固定Runtime策略、三类target claims | BuildInputSnapshot、claim身份/绑定/释放结果 | CapabilityVersion尚不存在时不能用假版本claim |
| Build→Storage/Registry adapter | 构建输入/输出 | 精确object version/repository+digest+platform、受限凭据引用 | 实际字节/manifest读回 | tag、ETag或push接受不等于指定制品存在 |
| Build→Capability Inbox | 构建产物确认后 | 版本事件+Build owner的完整artifact readback引用 | 唯一CapabilityVersion | 不由Build直接写Capability数据库 |
| Workspace→Capability | F07/F08/F10/F16准入与引用 | 版本、完整DeploymentDescriptor identity、claim、namespace可见性 | 固定版本/数据/执行契约 | 不能只拿一个digest再猜启动参数 |
| Workspace→Catalog | F07/F08/F11/F12报价/接受 | 选定套餐、purpose、周期、准入事实、原义务 | 不可变报价；AcceptQuote绑定唯一Operation | Catalog不扣款，Quote不是容量预留 |
| Workspace→Gateway | 购买/续费/退款/Key | 租户钱包映射、原Code、金额、原单、受限grant | 确定/拒绝/未知观察、Key Secret引用 | 余额变化不是该原单成功证据 |
| Workspace→Runtime Control | F08/F09/F10/F16 | 完整描述、预留实例身份、资源/Secret/config引用、generation/epoch | 实例意图、部署/更新/配置结果 | Runtime不接管Workspace业务权益 |
| Runtime Control/Workspace→Fabric | 资源/运行/切流量 | typed资源命令或描述、exact targets、epoch、expected route generation | provider动作/实际readback/路由CAS结果 | DB提交不等于外部router原子切换 |
| 各Owner→Ledger | 每条有证据义务的阶段 | typed receipt、原操作/输入输出摘要、前置证据 | append-only receipt及精确回读 | 不填假Workspace ID或写旁路账本 |
| CloudIdentity→Workspace | F15暂停/删除 | 原Tenant操作、目标Workspace集合、受限cleanup grant | 各子Operation的独立状态 | 改Tenant行不能宣称CVM/CBS已删除 |

上述边的具体RPC、消息字段、错误和事件schema都是契约文件的一部分。并非每个Domain任意调用所有服务；BFF不替服务互相查库，Runtime不直接改钱包，Ledger不调provider执行动作。

### 5.3 可靠事件

1. producer在同一数据库事务写业务状态+Outbox记录；事件ID/aggregateVersion/payloadHash固定。
2. dispatcher读取未确认记录，调用目标owner的typed Inbox；传输可以重复。
3. consumer在同一事务插入唯一(producer,eventId) Inbox、验证payloadHash/版本、写本域结果与可能的新Outbox。
4. consumer提交成功再ack；producer收到ack再标投递成功。ack丢失重复投递不重复业务动作。
5. 消费者只接收其订阅事件，因此aggregateVersion允许合法间隙，不要求n+1连续。不可变事实按eventId和业务身份去重；可变投影用aggregateVersion单调CAS防陈旧覆盖；依赖动作必须验证明确前置事实/receipt，未满足保留原事件不执行。投递失败保留原事件和可观察状态，不靠缺版本或扫描业务表“猜一个事件”补救。
6. 每条事件限定消费者，不能把整个应用状态广播。事件不含Key、密码、客户包内容、完整provider响应或外部凭据。

使用PostgreSQL Outbox+gRPC Inbox满足当前明确链路；没有NATS/全局event bus服务依赖。事件schema和消费者清单见contracts/events.json，跨模块版本变更双方一起更新。

## 6. 跨域引用与删除不变量

- Capability对版本行加锁检查ready，然后创建不可自动过期的reference claim（claimId/consumer/consumerResourceId/digest/状态）；Workspace/Build通过自己的Operation记录取得的claim。
- Claim不是分布式事务锁：消费者失败必须凭本域终态/实际资源读回显式释放。超时不自动释放，也不使用最终一致的引用计数判定可删。
- 删除版本：在同一Capability事务检查无活跃claim、将版本设deleting并冻结新claim。客户删除首先是目录tombstone，不自动清物理OCI或Package历史。
- 物理OCI删除另需按Registry repository+digest串行检查所有共享版本/Runtime/WebUI/Build引用，由受授权管理员流程执行。一个版本无引用不代表共享digest无引用。
- Package归档不级联删除版本、Build、Workspace历史；Workspace删除不删除源Package/OCI。
- 同域FK可RESTRICT；跨域仅不透明引用，通过owner查询/claim/事件保证义务，不建跨库FK或跨服务SQL事务。

## 7. 真实外部接口的交付边界

新租户钱包委托、报价接受、Runtime config reload、Build Receipt无Workspace类型等能力要么由当前owner接口证实，要么在明确的owner开发项中补齐；在能力未具备前相关命令返回可解释的准入错误，不能把“接口写在本文”当作owner已提供。

这不是兜底：缺失的必要能力必须在对应功能验收前完成。不得借用管理员无限权限、二次钱包、假收据、假ready或客户手动执行CLI代替产品功能。

## 8. 结构收敛，不再扩大架构

- 不引入全局Saga服务、全局锁、万能插件Host、独立权限平台或第二event bus。
- typed grant、引用claim、路由CAS都留在有真实写入权的现有Owner，解决的是已发现的具体竞争/授权断点。
- Runtime构建扩展包裹现有WorkspaceApplicationRevision，不另起端口/探针/挂载/Secret的平行应用格式；当前源码validator仍要参与准入。
- 新功能先做一个真实caller到终态的闭环，再提取下一个Owner；服务独立是目标边界，不是先建一堆空仓库的理由。

## 9. D17套餐变更的唯一Owner

| 事实/动作 | 唯一Owner | 不允许的越界 |
|---|---|---|
| 当前/目标套餐、原账期、PlanChange计划与执行状态、预约取消、下期worker | Workspace | BFF不能用计时器执行降配；Fabric不决定客户当期费用 |
| 原/新报价快照、T/S/E、补差ceil及下期目标价格、固定政策版本 | Resource Catalog | 不接收客户端自报价格，不在Wallet复制定价 |
| 原补差扣款、确定失败补偿、成功后未用区间退款、余额/交易读回 | Gateway/Sub2API | Workspace/Ledger不能另记可花费余额；任何退款必须绑定原单 |
| 允许的资源转换、中断/可恢复性、真实规格/文件系统/挂载读回 | Fabric | 不把provider接受RequestId当新规格已生效；不能假缩容 |
| 应用资源限制、重启/reload/健康与当前运行事实 | Runtime Control | 不修改原账期或选择客户收费政策 |
| 原报价/原单/计划变更/实际资源结果与补偿的不可变证据 | Ledger | 不发起资源变更，不编排客户计划 |
| 立即升级/下期预约/取消入口、原到期日、实际补差与独立结果 | Console/BFF | 不计算业务价格、不因Operation受理就显示已升级 |

PlanChange是Workspace内的业务实体，不是新增微服务或通用工作流系统。每个state、期边界触发、原单与失败终点详见13与06，不能每个Owner各自起一套升降配规则。
