# 11 UI/UX交接设计

> 这是新方案的目标交互与视觉交接，不是现有产品已实现声明。应用默认与两种产品组合是**2026-09-29已采用、待W01贯通的合同迁移**，产品定义见[12](12_product_spec.md)，实施与验收见[14](14_implementation_work_packages.md)，前端绑定见[04](04_frontend_interaction_spec.md)。03/机器inventory和可点击结构原型`ui-prototype/index.html`尚未因此自动支持新选择，本轮不修改它们。原型顶部持续标注“交互原型，示例数据”，网络被CSP禁用；示例金额不建立实际价格或收费政策。

> 2026-10-09交互细化已采用：[当前业务决定](../../decisions.md#2026-10-09-task-oriented-console-and-customer-agent-version-lifecycle)、[Console交互owner](../../product/console-experience-guide.md)、[Workspace交互owner](../../product/workspace-experience.md)。固定的是任务、权限、状态、确认与验收，不是本文件原型的像素值。旧04/03字段、原型和生成任务清单仍是待迁移投影，不能据它们恢复客户Runtime/WebUI选择或旧Admin-only授权；缺口见[roadmap](../../roadmap.md#task-oriented-console-and-agent-version-lifecycle)。

## 1. 继承什么，改变什么

设计来源：
- `/Users/huangrende/Documents/ChatGPT/one-person-lab-cloud/docs/product/console-experience-guide.md`
- `/Users/huangrende/Documents/ChatGPT/one-person-lab-cloud/apps/console-ui/src/components/ui/tokens.css`
- `/Users/huangrende/Documents/ChatGPT/one-person-lab-cloud/apps/console-ui/src/styles.css`
- 品牌图标：`/Users/huangrende/Documents/ChatGPT/one-person-lab-cloud/public/opl-app-icon.png`；原型以内嵌data URI保留原字节，不新画logo。

沿用OPL Cloud通用产品名、现有中性色/青绿token、系统字体、轻边框卡片和语义状态色。没有新框架、新品牌、炫光背景、无来源趋势图或主观推荐套餐。生产前端仍为React/TypeScript；单文件HTML只是可携带的设计原型，不是技术迁移。

实际CSS token比文档的“blue”概述更具体：`--color-background-primary-solid=#0d9488`，深一级`#115e59`。原型使用已有深一级颜色承载小字号白字主按钮；`#0d9488`用于选择/强调。这样不另造色板，并提高小字对比。

| 用途 | 继承值 / 约束 |
|---|---|
| 主文字 / 次文字 | #142536 / #617487 |
| 页面 / 卡片 / 分隔 | #f7fafc / #ffffff / #d9e3eb |
| 主操作 / hover | #115e59 / #134e4a，均来自已有色阶 |
| 选择 / 柔和高亮 | #0d9488 / #f0fdfa |
| 成功 / 警告 / 错误 | #0c5c58 + #e8f4ed；#8f4a20 + #fff3e0；#a40e26 + #ffebe9 |
| 字体 | Inter、系统sans、中文系统回退；无外部字体请求 |
| 正文 / 次文 / 页面标题 | 14px / 12–13px / 26px；移动标题23px |
| 圆角 / 空间 | 控件8px，卡片10px；4/8/12/16/24/32px分级 |
| 可访问性 | 操作不只靠颜色；焦点环可见；移动触控目标至少44px；支持reduced-motion |

## 2. 导航与页面任务归位

不按Domain数目平铺菜单。主导航固定任务顺序：**概览、智能体、工作区、OPL Gateway、费用**。账户设置属于次导航，平台管理仅platform_admin显示。

```text
OPL Cloud
├─ 概览：已有实体聚合，不新增服务/API
├─ 智能体
│  ├─ 目录 → 详情/版本 → 部署
│  ├─ 上传向导
│  └─ 构建记录 → 构建详情
├─ 工作区
│  ├─ 列表 → 新建工作区：默认OPL App / 自选Agent + 套餐
│  └─ 详情：概览 / 模型配置 / 部署历史 / 账单周期 / 危险操作
│      └─ 历史裸资源：部署到已有资源（adopt，不重购）
├─ OPL Gateway：服务信息 / 用量 / API密钥
├─ 费用：订阅与周期 / 交易记录
├─ 账户菜单：身份 / 安全退出（首版不扩成员/分组管理）
└─ 平台管理（独立权限）
   ├─ 客户管理：查看 / 准入 / 启用 / 停用（不连带停机、删资源或退款）
   ├─ 运行底座与界面：Runtime / WebUI / 发布者空间 / 默认构建策略
   ├─ 资源与价格政策：计算 / 存储 / 组合价格 / 退款 / 保留
   ├─ 操作与审计
   └─ 资格与发布证据
```

原型hash路径用于离线浏览，不规定生产URL必须照抄。生产路由的迁移由现有Console router owner实施；03 `/api/v2`是接口路径，不是菜单路径。

## 3. 桌面布局与信息优先级

### 3.1 统一外壳

左侧228px导航；上方69px页头放当前位置、消息、账户菜单；内容区左右36px、卡间18–24px。页面标题、当前动作与真实状态在首屏，不把原始DTO、源码SHA和大段正则放到普通客户主视图。长列表独立分页，不为装饰填虚构数量。

原型额外有“角色/状态切换”工具条，这是评审工具，不进入生产Console。

### 3.2 概览

```text
[你好/当前空间]                                  [新建工作区]
[钱包当前状态]       [本月实际模型用量]          [Workspace概况]
[当前可用Workspace与Open]                       [下一步]
[待处理的原Operation与查看入口]                [消息]
```

只能聚合现有getWallet/listUsage/listWorkspaces/既有操作读回。独立来源失败不互相遮蔽：钱包不可读时保留工作区和当前可用入口，不显示0余额。

### 3.3 智能体与构建

卡片先名称、官方/私有来源、确切版本、是否可部署，再用途和详情。上传主按钮位于右上；“构建记录”是同一任务域子页。构建详情左侧当前阶段及结果，右侧固定输入；日志默认收起。成功是推送、Capability登记和所需artifact receipt都被确认，不是主进程退出0；主表单只保留标准Package、名称/版本；平台active策略解析获准Runtime/WebUI并冻结输入。精确输入属于折叠技术详情，不是客户选版器。显示同Package的所有构建制品，“有可用更新 → 重新构建 → 确认切换”分开。版本页区分本地清理与云端删除，显示Tenant云端制品用量/50；引用保护和计数来自owner，不由浏览器猜测。

### 3.4 新建工作区：一次确认应用+套餐

```text
1 应用（默认OPL App，可自选Agent）→ 2 套餐/模型/续费 → 3 一次确认应用+报价/条款 → 4 原ownerOperation
[主表单：只编辑本步]                          [当前选择摘要]
[上一步 / 保存草稿返回]                        [版本/资源/费用状态]
[当前不可跳过的确认]                [下一步 / 明确提交一次]
```

默认卡片为“OPL App（自带界面）”，用户不需要先到智能体目录、上传Package或挑选独立WebUI。确认区必须显示已解析的获准RuntimeRelease确切版本；默认是明确的产品选择，不是空值、resource_only或失败fallback。套餐确认后后台自动部署，无第二次安装/应用确认。

| 页面场景 | 选择控件与摘要 | 合同/执行含义（待W01贯通） |
|---|---|---|
| 未选Package、未选独立WebUI | 默认OPL App及native WebUI；获准精确Runtime版本可读 | `WorkspaceApplicationSelection = opl_app(runtimeVersionId)`；直接部署RuntimeRelease不可变OCI，不创建Package/Build |
| 制作Agent，平台固定active独立界面/Runtime | PackageVersion、获准RuntimeRelease、获准独立WebUI，三项确切版本均必需 | CreateBuildRequest的packageVersionId/runtimeVersionId/webuiVersionId；Build产生版本，部署时选择`agent(capabilityVersionId)` |

只允许表内两种组合。客户只传Package是合法上传交互，平台必须补全并冻结已配置active的获准Runtime/WebUI输入；没有完整有效策略则明确拒绝Build，不猜测补包/界面、不忽略意图、不降为默认App。默认App的内置UI不伪装为独立目录版本，缺少其发布契约时不允许默认App准入。

“自选Agent”入口选择已ready的确切CapabilityVersion；冻结Runtime/UI来源放技术详情，不是客户选版器。Admin更新active策略后，客户可从原Package显式重新构建，再确认切换；不能在部署页改写旧产物。新构建解析当前有效策略，已接受Build/重试保持原冻结输入。版本撤销则阻止新的使用并说明原因，不自动改选latest/default。

默认和自选的进度页、Workspace详情、购买及访问体验共用一套路径：Workspace购买与资源权益→Fabric资源→Serve运行/readiness/access→Ledger证据读回；不新增默认部署服务。

- 上一步保留非敏感选择，不保持旧报价有效。任何影响报价/契约的变更都清除quote和确认checkbox。
- 新收费周期仅1个月；续费模式默认manual但请求显式携带。选择automatic后单独未勾选同意控件；不能把普通“确认部署”视为自动续费授权。
- 草稿按Tenant、actor和create/adopt分别隔离；不存密码、Key、临时签名URL。恢复先重读目录、原状态与新报价。
- 202之后留在原ownerOperation（owner+operationId）进度页，不自动打开应用、不再发create。终态后分别读回Workspace、Serve当前Deployment/access、账务与所需receipt；资源ready不等于应用ready，receipt缺失显示待核实。外部unknown不重购、不反向退款。
- D17已确认，规则以13的workspace-plan-change-v1为准；原型数值仍是固定示例，不是实际Catalog定价。

### 3.5 工作区详情

主区先“当前选中应用是否可用”和Open，再资源条件、配置生效版本、套餐和完整周期。应用来源明确区分“OPL App / RuntimeRelease”与“Agent / CapabilityVersion”；默认路径不显示虚构的Package/Build记录，也不标作历史裸资源。更新、调整、续费不挤在同一个危险操作菜单。删除有自己的分区和独立确认，不与Open等权并排。

模型设置显示目标版本与已应用版本。保存成功但reload未确认时保留“配置应用中”；旧版本指针不证明旧应用仍可访问。

### 3.6 平台控制面

目录准入首先选择发布者空间和不可变制品，技术契约可结构化编辑或展开JSON，两种编辑使用同一schema，不接受用户绕过未知字段校验。第三方Runtime的Namespace是Registry准入前缀，不是客户Agent分组。

Tenant页面把访问状态与子Workspace恢复/删除状态并列：重新启用suspended和恢复deleted绝不共用含糊的“恢复”按钮。资格页面分别显示源码、Candidate、Instance、正式发布，不合成总绿灯。

## 4. 跨页交互规则

| 交互 | 确定行为 |
|---|---|
| 未提交表单离开 | 非敏感草稿允许保留；清楚说明未提交。危险确认和Key值不保留 |
| 刷新已受理操作 | 只根据ownerOperation的owner+operationId恢复GET并读回所需receipt，不再发业务写入 |
| 写响应丢失 | 已知operationId则GET；初次响应丢失且ID未知时，仅对证明持久幂等的原接受端点用同key同body取回原身份。换key重做或下游盲重发禁止；无法保证时显示unknown |
| 证据尚未确认 | UI经BFF/所属Owner读取对象授权的receipt结果投影（待W01贯通），不调用platform_admin收据端点；所属Owner恢复证据写入，不让用户再次购买、部署或Build来补证据 |
| 报价过期/输入变化 | 清除原quote与两个同意控件，重新报价后重新确认 |
| 异步轮询 | 202与非终态GET的Retry-After秒值必须和pollAfterSeconds相同；一次只有一个在途GET；终态两者缺省并停轮询 |
| 429/503 | 使用明确Retry-After，不转WebSocket、不固定2秒兜底；未知写入结果仍保留原幂等身份 |
| 返回/后退 | 回到原任务列表或上一步，不自动新增命令；hash原型和生产history行为等价 |
| 表单失败 | 摘要说明、字段错误绑定控件；首次出错聚焦摘要/字段，保留可修正的非敏感输入 |
| 危险动作 | 显示确切对象、影响、原政策，名称输入与未勾选确认；未受理可取消，已受理不假取消 |
| Secret | 显式单次模态展示；关闭/离页/退出从DOM和内存清除，不持久化 |
| 目录下架 | tombstone只撤去目录可用性，不声称物理Registry字节已删除 |
| Tenant重新启用 | 只恢复新Cloud管理命令权限；不自动Resume、采购或续费Workspace。停用未自动暂停应用，欠费到期或历史暂停的恢复须走独立授权操作 |
| Tenant删除恢复 | deletedAt+15天内只恢复身份/保留资产权限，已销毁CVM/CBS不复活 |

## 5. 状态设计与窄屏焦点

所有页面支持loading、empty、error、partial、permission、unknown，而不是只有成功截图：
- loading：区域骨架，不虚构数值；已有其他区域独立可用。
- empty：明确没有记录和有权限的下一步；搜索无结果给清除筛选。
- error：具体源不可读，requestId可复制，重试是重新读取，不重复写。
- partial：在失败区域标出缺失事实，不覆盖有效Workspace或Receipt。
- permission：成员和平台权限分开；不显示越权对象存在性。
- unknown：展示待核实原操作，不把它映射为失败/退款成功。

窄屏≤800px时导航变互斥抽屉，关闭状态inert不可Tab进入；打开焦点进入导航，关闭回菜单按钮。主区单列；列表转卡片；主结果和可执行动作优先于筛选。长版本、名字、完整金额和周期可换行。只有日志/JSON允许自身横向滚动，页面不横向溢出。

原生dialog提供模态焦点限制；Escape关闭未提交确认，焦点回触发按钮。提交后重读页面让焦点落在操作结果标题。步骤切换将焦点移到新标题，错误聚焦具体字段；颜色不是唯一状态标记。

## 6. 17功能到原型入口的对应与待迁移差异

| F | 原型入口 | 可走查的动作 |
|---|---|---|
| F01 | #login、#settings、#admin-tenants | 示例登录/退出、邀请/角色/移除、Tenant开通与账单绑定确认 |
| F02 | #agents、#settings | 搜索/范围、分组、官方/私有标签、创建/归档确认 |
| F03 | #admin-catalog、#admin-pricing | 既有目录/政策表单；待验证Admin active策略固定完整获准输入；新Build用当前策略，已接受Build/重试保留原输入，客户无选版器 |
| F04 | #upload | 既有三步上传；待接平台active的精确Runtime/独立WebUI策略（旧原型选版器不代表新交互）及缺任一输入禁提交 |
| F05 | #build | 既有阶段/日志fixture；待迁移精确输入分支和artifact receipt读回 |
| F06 | #agent-detail | 版本来源、部署入口、归档与引用阻止下架 |
| F07 | #deploy | 既有四步、报价/续费交互；待迁移默认OPL App/自选Agent及一次确认应用+套餐 |
| F08 | #deploy结果步 | 既有原操作/unknown示例；待迁移两分支共用ownerOperation/receipt读回及丢响应不重发，不联网 |
| F09 | #workspaces、#workspace | 既有搜索/Open/模型版本；待迁移OPL App与Agent来源展示和共用访问链 |
| F10 | #workspace部署历史 | 既有版本/回滚确认；opl_app精确RuntimeRelease更新输入待W01贯通 |
| F11 | #workspace → 套餐变更 | 半期20→40补10，原E不变；下期降配预约、取消、边界授权/目标付款和真实生效分开 |
| F12 | #billing、#workspace账单 | 完整周期、续费确认、续费模式及原单状态 |
| F13 | #workspace危险操作、#billing | 名称+影响确认、资源删除/退款分栏 |
| F14 | #gateway | 用量/Key子页、显式示例Secret、撤销权限边界 |
| F15 | #admin-tenants | active/suspended/deleted切换、reenable与restore分离、子项原因 |
| F16 | #adopt | 原资源采用Agent，不出现购买/扣费步骤；独立草稿 |
| F17 | #admin-operations、#admin-qualification | 原操作核对、审计与四层证据分开 |

此表区分既有原型可审阅内容与2026-09-29待迁移交互，不把新规格当成原型已实现。既有API/字段/权限见03和`contracts/ui_inventory.json`；新增选择、快照和原请求幂等回读须按03/14的W01贯通，不能把设计术语当现有可发送DTO。每F的业务流程、草稿、错误和移动行为见04。

## 7. 既有离线原型验证范围（非2026-09-29新交互证据）

此前采用本机隔离agent-browser会话检查离线文件；本轮只修改设计文档，不重跑或覆盖这些记录。既有验证项目为：页面路由可达、按钮/对话框/步骤可点击、create/adopt草稿隔离、报价过期阻止提交、危险确认校验、模态关闭、普通/平台权限切换、无横向页面溢出和无HTTP网络请求。

既有结果记录在checks/ui_prototype_verification.json，效力仅限其原sourceHashes；本设计修改后不能宣称该文件已经绑定本轮字节。不得用原型通过替代React实现、真实Owner合同调用、扣费资源动作或Instance资格验收。

## 8. 上一轮不受影响页面的基线（保留原哈希）

- 桌面1440×1050：42项页面/交互检查通过，覆盖17个复用入口、权限、独立自动续费同意、报价失效、unknown、危险确认、单次Key/应用密码清除和发布者schema示例。
- 窄屏390×844与320×844：40项检查通过，包括所有入口无页面横向溢出、抽屉inert及焦点归还。
- Runtime/WebUI官方与第三方共4个完整只读例子原样来自publisher-contract.schema.json examples，已通过本地Draft202012验证；它们不是实际制品或生产准入证明。
- 10张截图在ui-prototype/screenshots/，关键截图为deploy-quote-desktop.png、deploy-unknown-desktop.png、build-failed-desktop.png、workspace-delete-confirm-desktop.png、adopt-existing-desktop.png和workspaces-mobile.png。
- 可重放DOM事件脚本、测试明细、截图SHA256与源文件哈希在checks/ui_prototype_verification.json。原型无HTTP资源调用，且CSP禁止业务网络。
- D17已由用户确认，政策Owner为13。原型通过仍不证明后端/供应商执行或Instance资格。

## 9. D17已确认后的套餐变更交互

只在既有工作区详情增加“套餐变更”tab和既有“调整套餐”对话框，不新开主导航。当前业务套餐、实际资源、已付周期、计划历史分别显示；列表/详情/取消直接绑定listPlanChanges/getPlanChange/cancelPlanChange。

### 固定可重放示例

- 示例时钟明确为2026-09-16 00:00 UTC，不使用观看者当前时间。S=2026-09-01 00:00 UTC，E=2026-10-01 00:00 UTC，旧20美元/月、新40美元/月，剩15/30天，补差10美元。
- 原型内部BigInt演示一次ceil；生产前端只显示Catalog返回的Quote.planChangeCalculation，不自己决定收费。Quote窗口是固定测试数据，实际expiresAt只取API。
- 确认页显示原/新价、T/S/E、剩余周期、补差、原E保持及中断影响。接受后是requested，不写成功；资源confirmed仍非applied，Runtime恢复确认后才记录实际appliedAt。
- 降配示例只减计算、保持50GiB原盘：当前20美元/月不变，目标下一期12美元/月，E=10-01才允许执行。保存scheduled时total=0、不退款、不动资源。
- scheduled初次Operation的succeeded只说明保存完成，执行用新的executionOperationId；取消用cancellationOperationId。
- 取消需要非空reason与expectedScheduleVersion，只有下期义务未accepted、付款/provider动作未发出时允许。已有计划必须显式cancel再new。
- 到E手动未授权→awaiting_payment，并停止未付款使用；已有有效自动同意按目标价的唯一下一期原单处理。提前付款不提前降资源；重复边界/点击不产生第二原单。

### 失败与收费展示

CBS原盘缩容、混合升降、相同配置no-op、未经批准转换在报价前拒绝。付款unknown只读原单；明确付款拒绝不执行资源调整。目标确定无法交付后先fence迟到动作，再按补差原单全额补偿；若实际磁盘已扩容，继续显示irreversible_residual/needs_attention，不能因退款确认就写资源已回退。

未来正常删除分别呈现基础单与补差单：基础单继续原720小时规则；补差仅按quoteT..E覆盖剩余部分向下取整。不得把半个月补差再次当整月基础单。

灰色“模拟边界/原单确认/资源确认/Runtime确认”按钮仅用于离线fixture，生产不提供手改状态API；示例view state不是可直接发送的API DTO。真实字段、状态、窗口与控制条件由03和13唯一约束。

此前D17轮次只回归受影响F11及关联续费/删除、窄屏；此前无关页面的测试保留其原哈希证据，不宣称在新字节上重新执行。该轮截图plan-upgrade.png/plan-downgrade.png及补充状态图、可重放脚本和当时sourceHashes记录在checks/ui_prototype_verification.json。

## 10. D17既有回归与金额精度

此前D17轮次按最终13政策只回归F11及相关F12/F13，不代表本轮新应用选择已测试。40项固定时钟交互/数值断言、390px和320px下18项受影响布局检查通过。原始基线仍绑定旧字节，不能把旧截图称为新版本全量验收。

付款确认使用API返回的USDMicros十进制字符串，BigInt显示至少2位、最多6位小数：19354839微美元必须显示US$19.354839，不能仅显示19.35却扣取更高精确金额。31天示例20→50、剩20/31周期的只读核对也包含在升级报价页。真实UI不从JavaScript Date重建计费基准或金额；API的UnixMilli来源和金融快照摘要仅作技术溯源。

既有D17截图：ui-prototype/screenshots/plan-upgrade.png、plan-downgrade.png、plan-failure.png、plan-boundary-mobile.png。分别覆盖精确升级报价、下期预约未生效、已确定失败与不可逆资源/退款分开、原E边界手动等待目标价付款。

该轮可重放脚本、逐项结果、当时HTML/API/UI清单/本设计/13政策哈希和截图哈希保存在checks/ui_prototype_verification.json；记录中的remainingBusinessDecision为空不表示本轮W01合同迁移已完成。D17用户决定已accepted，仍不表示真实采购、扣退款、provider执行或Instance资格已经通过。

## 11. 2026-09-29交付验收：以tencent-tke为主

以下是12/14所列后续实现的验收要求，不是本轮已通过的测试记录；不新增服务，不访问生产，不修改既有原型证据或机器清单。

- **默认新建**：在真实React页面不选Agent/独立WebUI，仍能看清获准OPL App版本、native WebUI、套餐与费用，一次确认后后台部署；无Package/Build前置和伪历史。最终打开Serve当前应用，而非仅见资源ready。
- **自选Agent**：客户上传标准Package及名称/版本，平台读取active策略并固定精确Runtime和独立WebUI；三项输入穿过Build、CapabilityVersion、报价和部署。策略更新不覆盖旧Build/重试；无有效组合或不兼容时明确拒绝。上传页不再要求客户选版。
- **异步与丢响应**：提交后返回、刷新、网络中断及后端重启均定位同一ownerOperation/receipt，已知Operation后不重发create；初次响应丢失按04的持久幂等接受端点取回原身份，不再次扣费/采购/构建。unknown、资源已ready但应用不可用、receipt未确认分别展示；Operation终态不代替所需事实和证据。
- **TKE真实终点**：在批准`tencent-tke` profile/套餐下读回精确OCI、资源与持久数据、Serve readiness/access、凭据注入和实际WebUI响应，并与原应用选择及receipt一致。三输入Agent Build产物和默认RuntimeRelease直部署两种组合都要覆盖；Local-Docker通过不替代TKE通过。
- **证据与权限**：Cloud合同/组件/集成检查只证明其层次；Instance验收由`opl-instance-medopl`保护流程提供绑定同一Candidate SHA/digest的receipt和真实读回。普通本地/E2E不得访问生产私网、采购/删除真实CVM/CBS或扣真实费用。新交互桌面/窄屏、焦点、默认与自选分支截图须在实现后独立取证，不挪用旧截图。
