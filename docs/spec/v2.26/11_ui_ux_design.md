# 11 UI/UX交接设计

> 这是新方案的目标交互与视觉交接，不是现有产品已实现声明。可点击结构原型：`ui-prototype/index.html`。原型顶部持续标注“交互原型，示例数据”，网络被CSP禁用；示例金额不建立实际价格或收费政策。

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
│  ├─ 列表 → 部署向导
│  └─ 详情：概览 / 模型配置 / 部署历史 / 账单周期 / 危险操作
│      └─ 历史裸资源：部署到已有资源（adopt，不重购）
├─ OPL Gateway：服务信息 / 用量 / API密钥
├─ 费用：订阅与周期 / 交易记录
├─ 设置：个人账户 / 成员 / 分组
└─ 平台管理（独立权限）
   ├─ Tenant管理：访问、子Workspace动作、托管与恢复
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
[你好/当前空间]                                  [部署智能体]
[钱包当前状态]       [本月实际模型用量]          [Workspace概况]
[当前可用Workspace与Open]                       [下一步]
[待处理的原Operation与查看入口]                [消息]
```

只能聚合现有getWallet/listUsage/listWorkspaces/既有操作读回。独立来源失败不互相遮蔽：钱包不可读时保留工作区和当前可用入口，不显示0余额。

### 3.3 智能体与构建

卡片先名称、官方/私有来源、确切版本、是否可部署，再用途和详情。上传主按钮位于右上；“构建记录”是同一任务域子页。构建详情左侧当前阶段及结果，右侧固定输入；日志默认收起。成功是推送+Capability登记都被确认，不是主进程退出0。

### 3.4 部署向导

```text
1 选择Agent → 2 套餐/模型/续费 → 3 报价/条款 → 4 原Operation
[主表单：只编辑本步]                          [当前选择摘要]
[上一步 / 保存草稿返回]                        [版本/资源/费用状态]
[当前不可跳过的确认]                [下一步 / 明确提交一次]
```

- 上一步保留非敏感选择，不保持旧报价有效。任何影响报价/契约的变更都清除quote和确认checkbox。
- 新收费周期仅1个月；续费模式默认manual但请求显式携带。选择automatic后单独未勾选同意控件；不能把普通“确认部署”视为自动续费授权。
- 草稿按Tenant、actor和create/adopt分别隔离；不存密码、Key、临时签名URL。恢复先重读目录、原状态与新报价。
- 202之后切到原Operation查看，不自动跳转，不再发create。外部unknown不重购、不反向退款。
- D17已确认，规则以13的workspace-plan-change-v1为准；原型数值仍是固定示例，不是实际Catalog定价。

### 3.5 工作区详情

主区先“当前选中应用是否可用”和Open，再资源条件、配置生效版本、套餐和完整周期。更新、调整、续费不挤在同一个危险操作菜单。删除有自己的分区和独立确认，不与Open等权并排。

模型设置显示目标版本与已应用版本。保存成功但reload未确认时保留“配置应用中”；旧版本指针不证明旧应用仍可访问。

### 3.6 平台控制面

目录准入首先选择发布者空间和不可变制品，技术契约可结构化编辑或展开JSON，两种编辑使用同一schema，不接受用户绕过未知字段校验。第三方Runtime的Namespace是Registry准入前缀，不是客户Agent分组。

Tenant页面把访问状态与子Workspace恢复/删除状态并列：重新启用suspended和恢复deleted绝不共用含糊的“恢复”按钮。资格页面分别显示源码、Candidate、Instance、正式发布，不合成总绿灯。

## 4. 跨页交互规则

| 交互 | 确定行为 |
|---|---|
| 未提交表单离开 | 非敏感草稿允许保留；清楚说明未提交。危险确认和Key值不保留 |
| 刷新已受理操作 | 只根据owner+operationId恢复GET，不再发业务写入 |
| 报价过期/输入变化 | 清除原quote与两个同意控件，重新报价后重新确认 |
| 异步轮询 | 202与非终态GET的Retry-After秒值必须和pollAfterSeconds相同；一次只有一个在途GET；终态两者缺省并停轮询 |
| 429/503 | 使用明确Retry-After，不转WebSocket、不固定2秒兜底；未知写入结果仍保留原幂等身份 |
| 返回/后退 | 回到原任务列表或上一步，不自动新增命令；hash原型和生产history行为等价 |
| 表单失败 | 摘要说明、字段错误绑定控件；首次出错聚焦摘要/字段，保留可修正的非敏感输入 |
| 危险动作 | 显示确切对象、影响、原政策，名称输入与未勾选确认；未受理可取消，已受理不假取消 |
| Secret | 显式单次模态展示；关闭/离页/退出从DOM和内存清除，不持久化 |
| 目录下架 | tombstone只撤去目录可用性，不声称物理Registry字节已删除 |
| Tenant重新启用 | 恢复访问后只Resume原Tenant停用所暂停、仍付费且资源存在的子环境；skipped显式原因，不重购/续费 |
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

## 6. 17功能到可点击原型的对应

| F | 原型入口 | 可走查的动作 |
|---|---|---|
| F01 | #login、#settings、#admin-tenants | 示例登录/退出、邀请/角色/移除、Tenant开通与账单绑定确认 |
| F02 | #agents、#settings | 搜索/范围、分组、官方/私有标签、创建/归档确认 |
| F03 | #admin-catalog、#admin-pricing | 发布者空间、Runtime/WebUI契约、默认策略、资源/组合价表单 |
| F04 | #upload | 三步上传、使用示例包、独立验证与构建确认 |
| F05 | #build | 当前阶段、日志展开、失败与成功fixture切换 |
| F06 | #agent-detail | 版本来源、部署入口、归档与引用阻止下架 |
| F07 | #deploy | 四步、返回、报价失效/重新检查、手动/自动续费确认 |
| F08 | #deploy结果步 | 原操作阶段、unknown、显式推进示例，不联网 |
| F09 | #workspaces、#workspace | 列表搜索、Open准入说明、模型目标/应用版本 |
| F10 | #workspace部署历史 | 版本更新与回滚兼容确认 |
| F11 | #workspace → 套餐变更 | 半期20→40补10，原E不变；下期降配预约、取消、边界授权/目标付款和真实生效分开 |
| F12 | #billing、#workspace账单 | 完整周期、续费确认、续费模式及原单状态 |
| F13 | #workspace危险操作、#billing | 名称+影响确认、资源删除/退款分栏 |
| F14 | #gateway | 用量/Key子页、显式示例Secret、撤销权限边界 |
| F15 | #admin-tenants | active/suspended/deleted切换、reenable与restore分离、子项原因 |
| F16 | #adopt | 原资源采用Agent，不出现购买/扣费步骤；独立草稿 |
| F17 | #admin-operations、#admin-qualification | 原操作核对、审计与四层证据分开 |

此覆盖说明原型里能够审阅哪些交互，不等于所有后端动作已实现。真实API/字段/权限仍以03和`contracts/ui_inventory.json`为准；每F的业务流程、草稿、错误和移动行为见04。

## 7. 本轮验证范围

采用本机隔离agent-browser会话检查离线文件。验证项目为：页面路由可达、按钮/对话框/步骤可点击、create/adopt草稿隔离、报价过期阻止提交、危险确认校验、模态关闭、普通/平台权限切换、无横向页面溢出和无HTTP网络请求。

实际结果已记录在checks/ui_prototype_verification.json；不得用原型通过替代React实现、真实Owner合同调用、扣费资源动作或Instance资格验收。

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

本轮只回归受影响F11及关联续费/删除、窄屏；此前无关页面的测试保留其原哈希证据，不宣称在新字节上重新执行。新截图plan-upgrade.png/plan-downgrade.png及补充状态图、可重放脚本和当前sourceHashes记录在checks/ui_prototype_verification.json。

## 10. D17当前回归与金额精度

本轮按最终13政策只回归F11及相关F12/F13，不重新设计其余页面。40项固定时钟交互/数值断言、390px和320px下18项受影响布局检查通过。原始基线仍绑定旧字节，不能把旧截图称为新版本全量验收。

付款确认使用API返回的USDMicros十进制字符串，BigInt显示至少2位、最多6位小数：19354839微美元必须显示US$19.354839，不能仅显示19.35却扣取更高精确金额。31天示例20→50、剩20/31周期的只读核对也包含在升级报价页。真实UI不从JavaScript Date重建计费基准或金额；API的UnixMilli来源和金融快照摘要仅作技术溯源。

当前截图：ui-prototype/screenshots/plan-upgrade.png、plan-downgrade.png、plan-failure.png、plan-boundary-mobile.png。分别覆盖精确升级报价、下期预约未生效、已确定失败与不可逆资源/退款分开、原E边界手动等待目标价付款。

可重放脚本、逐项结果、当前HTML/API/UI清单/本设计/13政策哈希和截图哈希都保存在checks/ui_prototype_verification.json；remainingBusinessDecision为空。D17用户决定已accepted，仍不表示真实采购、扣退款、provider执行或Instance资格已经通过。
