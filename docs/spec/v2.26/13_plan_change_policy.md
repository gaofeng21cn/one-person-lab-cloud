# 13 套餐升级、降配与结算闭环（D17已确认）

> 用户本轮明确决定：“中途升级立即生效、保留原到期日，按剩余已付周期补差；降配下个账期生效，不退当期费用。”
> 产品规则Owner为Workspace产品/订阅；报价计算Owner为Resource Catalog；资金权威为Gateway/Sub2API；资源实际执行/读回Owner为Fabric；Runtime Control负责应用配置/恢复；Ledger持证。没有新增服务、钱包、资金预占系统或工作流引擎。
> 本文是本次F11及关联F12/F13的唯一政策说明；API/DB/proto/原型与数值案例必须投影同一规则。规格定稿不表示代码已实现或已通过Instance资格。

这里的“升级/降配”指**资源套餐**，不指Agent镜像版本更新。F10更新Agent版本仍不重复购买工作区；若新版本确实需要更多资源，必须另经本F11明确报价和客户确认，不能静默扩容扣款。

## 1. 生效不是“点按钮即成功”

- **升级**：不等待下一账期，确认后立即进入执行队列；新资源规格、挂载/文件系统、应用资源限制和健康读回成功后才写appliedAt、切换active计划并显示“升级成功”。可能需要短暂中断，报价确认页必须显示实际执行计划的影响；不承诺零停机。
- **降配**：当前周期继续享受原套餐。创建下一账期计划，目标生效边界为原paidThrough；不提前降低当前资源，不退当前费用。
- **到期日**：升级前后currentPeriodStart/paidThrough保持原值，不从升级时刻重新获得一个月。
- **立即处理与实际生效分开**：requestedAt/acceptedAt表示受理；plannedEffectiveAt表示计划；appliedAt只能来自确认执行后的结果。generic Operation成功不能代替PlanChange.status=applied。

## 2. 哪些组合属于升级或降配

只允许Catalog/Fabric共同批准的同provider、同资源能力类别内的可比较转换。CPU、内存、持久盘容量等可比维度全部不减且至少一项增加为升级；全部不增且至少一项减少为降配。不同机型族/性能等级必须在明确批准的transition中，不能根据SKU名字或单纯价格猜测。

- 同配置是no-op，拒绝创建收费操作。
- 同时升一项、降另一项的混合变更不由系统猜方向；当前命令拒绝并要求分成明确的转换。
- 不支持原盘缩容的provider（包括当前Tencent CBS能力）禁止选择更小存储套餐；“下周期降配”不代表磁盘突然支持缩容，也不允许只降价却保留大盘并声称完成。
- 不满足当前Runtime最低资源要求、不支持持久数据安全或route/资源执行读回的目标不报价。
- 如果存在尚未开始且已经接受/确认付款的其它下一期账单，新升降配请求明确拒绝并提示账单锁定，不能暗改已付价格。本计划绑定的提前续费除外；具体并发规则见第6节。

一个Workspace同时最多一个未完成套餐变更或下期计划。scheduled计划不占用provider执行锁，不阻止不改变资源/财务基础的正常模型配置；应用更新必须仍满足已接受的目标资源约束，不兼容时先显式取消可取消计划，不能静默让已接受计划失效。已有计划时必须先显式取消；新请求不得悄悄覆盖旧计划。相同幂等键重放返回原PlanChange。

## 3. 升级补差的精确计算

使用当前**已接受的套餐月价**Pold和目标批准套餐月价Pnew，两者均为该套餐计算/存储/产品月费的已接受合计，不含Gateway Token用量费用。不用最新目录价格改写已购原价。S/E为当前已付周期起止；T为服务器生成此次有效报价的计费基准时刻，确认页显示T和报价有效期。

```text
unit = USDMicros；时间统一UTC整数毫秒
D = E - S                         # 必须 > 0
R = E - T                         # 必须 0 < R <= D，S <= T < E
Delta = max(Pnew - Pold, 0)
upgradeCharge = ceil(Delta * R / D)
```

- 金额JSON用十进制字符串，存储bigint；中间乘除用任意精度整数，最终验证int64范围，禁止浮点。
- 整体只在最后一步向上取整到1微美元；退款向下取整到1微美元。升级Quote只用一条kind=product、说明“本期套餐升级补差”的明细承载整体结果，不能分CPU/存储分别ceil再相加，也不能在UI把它称为新增整月产品费。降配预约本期lineItems为空、total=0，下期目标月价另列；实际next-period renew才按目标套餐明细收费。没有隐形最低消费或额外一月收费。
- 使用实际已付周期长度（包括28/29/30/31天），不是一律除720小时。720小时仅属于已有基础订单的旧删除退款政策，不挪来计算中途升级。
- Quote的T、原/新价格版本、原订阅版本、原计划、目标计划、输入hash及补差金额一经客户接受即固定。原S/E保留原字段与历史字符串；原周期Owner从原始时间/snapshot一次计算UnixMilli，明确传递并保存period_start_ms/period_end_ms/quote_at_ms，不能回写截断后的到期时刻，也不能从PostgreSQL舍入到微秒后的timestamp再次floor重算。补差退款的coverage_start_ms/end_ms同理；毫秒值绑定源快照摘要，不使用任意误差容忍。重试不因时间推进重新计算或创建新扣款。
- 尚未接受的报价过期、原计划/订阅财务基础变化或原到期日已过，拒绝旧报价，返回重新报价；已经接受的PlanChange继续使用原Quote，不因后台执行耗时超过quote有效期重新计价或中止原义务；不从客户body接受价格。
- 资源升级但批准目标价不高于旧价时补差为0，不产生负扣款/当期返差；仍记录零金额计划变更证据，不调用资金扣款。
- 供应商费用与客户补差不是同一个数字。Fabric按批准预算/采购政策做准入，不能把执行中出现的供应商额外费用偷偷加到客户原报价上。

### 数值验收

| 情形 | 输入 | 补差/结果 |
|---|---|---|
| 半周期升级 | 30天周期，旧20美元/月，新40美元/月，剩15天 | 10美元；E不变 |
| 实际31天周期 | 旧20，新50，剩20/31周期 | 19.354839美元，最终一次ceil微美元 |
| 一整周期剩余 | T=S，旧20，新40 | 20美元，不再收新完整40美元 |
| 连续升级 | 前次已成功到40，本次升60且剩1/4周期 | 以40为Pold补5美元，不再以初始20重复计价 |
| 重复点击/丢响应 | 相同原quote/PlanChange/原单 | 同一笔10美元，不生成第二单 |
| 已到期 | T>=E | 不允许中途升级；走原续费/新购规则 |
| 零差价资源升级 | Pnew<=Pold且资源是批准升级 | 补差0，不在当期退差价 |

## 4. 升级实际执行顺序

| 阶段 | Owner | 动作与确认点 |
|---|---|---|
| 选择/报价 | Catalog + Workspace | 检查角色、原已付周期/版本、批准transition、数据/资源能力；保存固定Quote与T |
| 接受 | Workspace | CAS原Workspace版本，写PlanChange、Operation、原计划/周期快照、唯一业务幂等身份；返回可查询ID |
| 报价绑定 | Catalog | AcceptQuote固定到此PlanChange operation；重复接受同身份返回同快照 |
| 补差 | Gateway | 使用PlanChange原始charge code扣一次upgradeCharge；0金额仅记录证据；unknown先原单读回 |
| 执行前确认 | Fabric/Runtime | 原资源identity、provider预付模式、旧/目标配置、数据和中断计划完全一致；获取本Workspace变更epoch |
| 资源调整 | Fabric | 排空/停止/调整计算、扩容及文件系统/挂载验证；按照批准adapter步骤执行，每个外部action持久原身份；本地幂等ID不冒充provider已支持远端幂等，未知结果只做原资源/订单读回 |
| 应用恢复 | Runtime Control/Fabric | 更新应用requests/limits和必要配置，验证正确镜像、挂载、Secret与真实health |
| 提交生效 | Workspace | exact readback通过后CAS sourcePlan→targetPlan，appliedAt记录实际确认时刻，E/S不变；保存补差订单关联 |
| 回执 | Ledger | 原Quote、原/新计划、补差原单、实际资源/应用结果、账期不变证据精确读回 |
| 客户结果 | Console | “升级成功”，并显示新规格、补差、原到期日；不是“等待下一期” |

支付失败不执行资源调整；支付unknown也不执行新副作用。资源/运行unknown保持原业务计划，不伪装成功，也不盲目重复provider请求。

## 5. 升级失败、补偿与不可逆资源事实

- **确定失败且尚无补差扣款**：直接failed，原计划/账期保留。
- **扣款已确认、变更可继续**：继续同一个PlanChange/原操作，不再次收费；页面显示执行中。
- **结果未知**：先以原provider request/原charge code/原epoch读回，不能按超时推断失败、退款或重购。
- **确定无法完成目标变更**：持久终止意图并fence迟到执行；记录目标计划未激活、实际资源结果和失败回执，再按原补差单全额补偿，不能用工作区删除的720小时比例退款公式处理此次失败。
- **资源可以安全回退**：按批准计划恢复并读回原资源/应用。没有该能力不能声称已经回滚。
- **部分变化不可逆（例如磁盘已扩容）**：不删除数据盘、不假缩容；Fabric保存实际资源，Workspace仍不把未完整交付的目标套餐标已生效。确定失败后的补差按上一条补偿，供应商残余成本由平台执行/预算责任承担，不向客户擅自收部分交付费用。资源与业务计划差异保持明确needs_attention，后续变更被阻止直到Owner安全收敛。
- 补偿原单只允许退剩余未退金额；退款unknown保留未决预留，不再发第二退款。退款确认与资源/运行是否恢复分别显示。

这是显式分布式操作的失败结算规则，不是把任意异常静默降级为另一个套餐。

## 6. 降配计划与下一账期

1. 用户选择目标并看到**下一账期价格、原E、目标规格及影响**。Catalog验证允许的downward transition和未来有效价格版本。
2. Workspace保存PlanChange.kind=downgrade_next_period，status=scheduled，plannedEffectiveAt=E；当前计划、当前账期费用与实际资源不变。此时不扣款、不退款、不调用资源降配。若存在尚未开始且资金已accepted/confirmed的其它下一期账单，禁止再新建升/降配计划，明确提示“下一期账单已锁定，进入该周期后再调整”，不重定价或擅退旧款。本期不扩展多段已预付账期的重定价；本降配计划自己的提前续费不是这个冲突。
3. 用户可以在下期资金义务尚未接受/执行前取消计划，保留审计；开始下期扣款或资源动作后不允许简单删除计划，防止按低价付款后偷偷恢复高配。
4. 到原E，已有Workspace续费worker读取唯一scheduled计划。自动续费必须有有效consent；手动模式没有支付授权时进入awaiting_payment，停止未付费使用，不偷偷开自动续费。
5. 下一期扣款按**已接受的目标套餐报价**走唯一(workspace,nextPeriodStart)资金义务，不先按旧高价扣一笔再补救。提前续费可以确认下一期付款，但必须等E才降低当前资源。
6. 有明确付款与provider准入后，在E或之后执行目标资源调整和运行验证。不能宣称瞬时无中断；资源确认后才applied。新周期仍按原Workspace账期Owner的billingAnchorDay推进：UTC下一自然月，day=min(原anchorDay,目标月最后一天)，保留原UTC时刻；Jan31→Feb28→Mar31，不能因短月永久漂移到28日。PlanChange只消费Owner给出的nextPeriodStart/End，不重造日历算法。
7. 因容量/目录撤销/数据或provider限制无法执行，不自动续旧高价、切其他SKU或制造成功。已扣的下一期目标款项按确定失败证据原单补偿；原周期结束后不能把未经支付的旧权益延长为active。
8. unknown仍按原付款/资源动作读回。用户看到“计划待付款/下期调整中/需要处理”，而不是一个伪造的已降配状态。

### 与其它动作的关系

PlanChange保存sourceSubscriptionVersion作为原证据；执行时按稳定的原period/计划/报价/资源基础校验，并对刚读到的当前Workspace版本做CAS。普通模型更新不篡改金融基础；本PlanChange绑定的提前付款只允许原义务记录的source→confirmed版本演进，不能放宽成任意最新财务版本。

- 同时人工续费/自动续费/边界worker：同一nextPeriodStart只能一个执行者和一个资金原单。
- 删除Workspace：原子取消尚未执行的scheduled计划；已开始者先沿原操作收敛，不能一边resize一边destroy。
- 取消自动续费：不删除已接受的计划，但没有下期付款授权时不会自动扣钱或生效。
- manual进入awaiting_payment但尚无payment accepted、wallet action或resource action时仍可取消；下一期obligation ID/billing key保持不变，以版本CAS和新Quote重新绑定未付款快照，原历史不改。一旦付款accepted，快照冻结，不再重绑定或造第二笔同周期资金单。
- 同期再次调整：先显式取消尚未执行计划，再创建新Quote和计划；不静默覆盖原目标。
- 已到期且超过现有续费窗口：保留现有拒绝规则，不用resize API偷偷重新开始一个月。

## 7. 补差订单与之后正常删除的关系

升级补差不是第二次购买工作区，必须是当前subscription period下独立的supplemental charge，记录quoteT..E费用覆盖区间、原计划/新计划和原单。未来续费按最新**已成功生效**套餐月价；没有生效的目标不成为下次收费基准。

若成功升级后再正常删除：基础购买/续费款继续按其原退款政策；补差款按自己的剩余覆盖区间退款，不能把半个月补差再次当成一整月款项计算。补差剩余退款向下取整：

```text
unusedSupplement = floor(confirmedSupplement * max(E-deleteConfirmedAt,0) / (E-quoteT))
```

不得超过该补差原单尚未退金额；删除确认在quoteT之前或覆盖区间不合法必须拒绝。每个原单独立去重、回读及Ledger证据，再聚合展示。该规则随本升级政策版本写入报价/补差单，不能改写历史基础单的720小时规则。

## 8. API、持久化与Owner要求

- Workspace拥有第一类PlanChange，至少记录id、workspace/tenant、kind、status、source/target plan与price version、source subscription period/version、quoteId、policyVersion、quoteT、S/E、chargeUSDMicros、plannedEffectiveAt、appliedAt、operationId、charge/refund references、schedule version、actual outcome与失败原因。
- Catalog拥有typed PlanChangePolicy与完整报价计算来源；不得再用未定义adjustment_rules JSON或pending_user占位。
- Gateway只拥有真实扣退款事实；Workspace不能改余额，Catalog不能扣钱。
- Fabric提供supported transition、预付/中断/可恢复性计划、原请求ID与actual readback；Runtime Control提供应用恢复事实。
- Ledger绑定原单、政策、输入和资源证据。旧钱/旧单保留；新失败补偿和成功后删除补差退款是不同kind/证据条件。
- 前端需要：preview Quote、接受PlanChange、查询/列表、取消未执行计划；升级进度和降配scheduled不是同一种成功文案。
- 两种不同配置只靠API body传planId、然后UPDATE Workspace行，不能满足本规格。
- legacy_resource_only没有应用时，运行验证结果为由Owner绑定快照决定的not_applicable，只验证真实资源；不能伪造Runtime ready。已有应用必须验证其实际运行，客户不能传一个skipRuntime字段跳过。

## 9. 当前实现与provider边界

当前Cloud源码中尚无本v2 PlanChange链和ResizeResources/ResetInstancesType/ResizeDisk的完整adapter调用；本文是开发规格，不是现成功能证明。

Tencent当前API提供CVM实例类型调整和CBS扩容能力，但需要遵守运行状态/实例类型/预付策略与真实询价；CBS原盘不支持缩容。正式实现先在Fabric适配层补这些动作及TKE排空、运行/文件系统验证，不能直接从Console调云API。现有Tencent路径包含pool claim，因此并不假定所有受管CVM都可原地改型：Fabric在报价前固定已批准策略（原地resize，或从目标套餐pool获取Compute并重绑原CBS/应用），记录executionPlan/profile引用。后一种是明确的正常执行策略，不是在失败后临时换路；客户E不变不要求底层CVM ID永远不变，但数据/Key/挂载/应用身份必须确切保留并读回。Local-Docker也必须验证宿主quota/容器limits/持久卷支持，不假冒Tencent资格。

Cloud提供可移植产品；实际实例的采购、收费或销毁测试由Instance保护流程及单独限定授权执行。本次没有执行任何此类动作。

参考的provider原始接口（接口实现前仍按具体profile验证）：
- `https://raw.githubusercontent.com/TencentCloud/tencentcloud-sdk-go/master/tencentcloud/cvm/v20170312/client.go`：官方SDK的ResetInstancesType、InquiryPriceResetInstancesType及DescribeInstancesModification；实际状态/预付约束需按具体API/profile验证。
- `https://www.tencentcloud.com/document/api/362/16310`：ResizeDisk，扩容/后续分区文件系统与不可缩容边界。

## 10. 定稿验收

D17用户决定已确认；必须同时通过：1）精确数值/取整/多次升级/原E不变；2）升级扣款失败/未知/部分失败及补偿；3）降配不提前动资源、不退当期、边界唯一扣目标价；4）取消/删除/续费并发；5）provider不支持/存储缩容拒绝；6）前端显示与PlanChange业务状态一致；7）各Owner单writer和原单证据。只有文档/契约及这些反例一致，才标规格ready_for_implementation；仍不标功能已上线。
