# 跨Owner字段对齐修复执行计划

**Status**: EXECUTION READY  
**Baseline**: 05c7b168 (2026-10-13)  
**Owner**: RenDeHuang  
**Purpose**: 修复14个跨owner字段断点，按业务影响排序，避免重复已知错误

---

## 执行原则（不可再犯）

### 已知病症清单

1. **跳过创建链直接修删除** ❌
   - **症状**: 没验证创建流是否work就去修删除
   - **后果**: R02/R01/R03创建断点被遗漏，用户付费后拿不到资源
   - **本次**: Sprint 1必须先修创建链，验收通过才能进Sprint 2

2. **测试fake通过当成真实接通** ❌
   - **症状**: 单元测试用fake能返回，真实handler拒绝Unimplemented/InvalidArgument
   - **后果**: R06/R07上线后才发现删除完全不work
   - **本次**: 每个修复必须包含真实PG组合测试，不能只跑单元测试

3. **改接口不迁移所有caller** ❌
   - **症状**: Serve handover改了，Workspace旧model adapter还在用旧接口
   - **后果**: R10两端都拒绝，模型更新完全失败
   - **本次**: 每次接口变更必须grep所有caller，同PR迁移，删除旧接口

4. **字段传了但consumer不消费** ❌
   - **症状**: Gateway收到obligationId但未落库/未进fingerprint
   - **后果**: R03/R04同key不同obligation无法区分，幂等性失效
   - **本次**: 每个跨owner字段必须追踪到consumer实际消费+落库+readback

5. **恢复逻辑写了但不驱动** ❌
   - **症状**: Fabric pending delete存进DB，read不驱动delete
   - **后果**: R08 crash后删除不会重试
   - **本次**: 每个异步操作必须有crash恢复测试，证明restart后能收口

6. **名称不区分导致混淆** ❌
   - **症状**: retire/stop混用、owned value/opaque ref不区分
   - **后果**: 代码意图不清晰，reviewer看不出问题
   - **本次**: 强制区分retire≠stop、ownedValue≠opaqueRef、readiness≠requested、cachedProof≠liveProof

---

## 优先级矩阵

| Sprint | 问题ID | 业务影响 | 必要性 | 技术复杂度 | 预计工期 |
|---|---|---|---|---|---|
| **Sprint 1** | R02 | 收费错误 | 10/10 | 中 | 2天 |
| | R01 | 价格不一致 | 9/10 | 低 | 1天 |
| | R03 | 幂等性失效 | 10/10 | 中 | 2天 |
| **Sprint 2** | R06 | 删除不work | 10/10 | 高 | 3天 |
| | R07 | 退款断裂 | 10/10 | 中 | 2天 |
| | R08 | 恢复缺失 | 8/10 | 中 | 2天 |
| | R04 | 退款字段丢失 | 9/10 | 低 | 1天 |
| **Sprint 3** | R09 | 初始模型不apply | 7/10 | 中 | 2天 |
| | R10 | 模型更新被拒 | 8/10 | 高 | 3天 |
| | R11 | 权限错误 | 8/10 | 中 | 2天 |
| | R12 | Version覆盖 | 6/10 | 低 | 1天 |
| **Sprint 4** | R05 | Proof validation | 5/10 | 低 | 1天 |
| | R13 | 字段投影差异 | 4/10 | 低 | 1天 |
| | R14 | Name长度冲突 | 3/10 | 低 | 1天 |

---

## Sprint 1: 修创建链 - 保证收费正确 (1-2周)

### 目标
用户付费后必须拿到正确的资源，quote/source/obligation三个维度不能有gap。

### R02 - Source validation时序问题

**断点**: Workspace在资金/资源阶段后才检查source ready/approved（recovery.go:225, runtime.go:26）

**影响**: 无效/撤销source可能先被接受，钱已扣但部署失败

**修复方案**:
```
1. 在Workspace接受order时（acceptance阶段）检查source当前有效性
2. 在Reserve/Deploy时再次验证（double-check，防止中间被撤销）
3. 如果source在资金后、资源前失效，返回typed错误并停止，不继续采购
```

**验收标准**:
- [ ] services/workspace/internal/launch/recovery.go 在funding前增加source validation
- [ ] services/workspace/internal/launch/application_source.go 补validation函数
- [ ] 单元测试: source invalid时reject，不进funding
- [ ] 集成测试: 真实Catalog → Workspace，source撤销后返回明确错误
- [ ] 时序图注释: acceptance → sourceCheck → funding → resourceCheck → deploy

**涉及文件**:
- services/workspace/internal/launch/recovery.go
- services/workspace/internal/launch/application_source.go
- services/workspace/internal/launch/recovery_test.go
- services/resource-catalog/internal/quotes/quotes.go (读取source状态)

**依赖**: 无（可先开工）

---

### R01 - UI quote竞态问题

**断点**: use-workspace-launch-controller.ts:348 迟到的quote response覆盖当前selection，确认页显示新套餐但提交的quoteId是旧套餐

**影响**: 用户看到价格A但付了价格B的钱

**修复方案**:
```typescript
// use-workspace-launch-controller.ts
// 每个quote request带generation ID
// response到达时检查generation，丢弃过期response

let currentGeneration = 0;

async function fetchQuote(selection: Selection) {
  const generation = ++currentGeneration;
  const quote = await api.getQuote(selection);
  
  if (generation !== currentGeneration) {
    // 这是过期response，丢弃
    return;
  }
  
  setQuote(quote);
}
```

**验收标准**:
- [ ] 每个quote request带generation/timestamp
- [ ] 迟到response被丢弃，不覆盖当前state
- [ ] 单元测试: 模拟慢response，验证被丢弃
- [ ] 回归测试: 快速切换compute plan，最终quote与selection一致

**涉及文件**:
- apps/console-ui/src/app/use-workspace-launch-controller.ts
- apps/console-ui/src/api/workspaces-api.ts
- apps/console-ui/src/components/workspaces/WorkspaceLaunchPage.tsx

**依赖**: 无（与R02并行，不同owner）

---

### R03 - Gateway obligationId未消费

**断点**: services/gateway-integration/identity/coordination.go:75,143,684 obligationId映射到内部struct后未落库/未进fingerprint

**影响**: 同key只改obligationId无法区分，幂等性失效

**修复方案**:
```go
// coordination.go
type DebitOperation struct {
    WorkspaceID      string
    ObligationID     string // 新增：必须消费
    QuoteAcceptanceID string // 新增：必须消费
    Amount           int64
    // ...
}

// coordination_store.go
// DB schema新增列: obligation_id, quote_acceptance_id
// fingerprint计算包含这两个字段

func (s *Store) SaveDebit(ctx context.Context, op *DebitOperation) error {
    // 插入时obligation_id/quote_acceptance_id不能为空
    // 幂等检查: 同workspace_id + obligation_id拒绝重复
}
```

**验收标准**:
- [ ] coordination.go DebitOperation补字段
- [ ] coordination_store.go 新增DB列 + migration
- [ ] coordination_postgres_test.go 真实PG测试: 同workspace不同obligation可区分
- [ ] Fingerprint单元测试: 只改obligationId导致hash不同
- [ ] 历史记录迁移: 已有pending/confirmed记录如何处理（可标记unknown，不伪造值）

**涉及文件**:
- services/gateway-integration/identity/coordination.go
- services/gateway-integration/internal/gateway_store.go
- services/gateway-integration/internal/coordination_postgres_test.go
- services/gateway-integration/migrations/ (新增)

**依赖**: 无（与R01/R02并行）

---

### Sprint 1 验收门槛

**必须全部通过才能进Sprint 2**:

1. **单元测试全绿** - 每个修复的focused tests pass
2. **真实PG组合测试** - Workspace → Gateway真实handler，不是fake
3. **变异测试** - 单字段变异能检测出问题（如R03只改obligationId）
4. **时序测试** - R02证明source在funding前验证
5. **竞态测试** - R01证明迟到response被丢弃

**如果任何一项失败**: 停止，修复，重新验收，不能带着failing test进下一个sprint。

---

## Sprint 2: 修删除链 - 能退款 (2-3周)

### 目标
用户能删除workspace，Serve retire → Fabric资源回收 → Ledger记账 → Gateway退款，整条链打通。

### R06 - Serve Retire未实现

**断点**: services/serve/internal/service.go:50 Retire返回Unimplemented

**影响**: 删除调用失败，无法释放资源

**修复方案**:
```go
// service.go
func (s *Service) Retire(ctx context.Context, req *servepb.RetireRequest) (*servepb.RetireResponse, error) {
    // 1. 验证请求者身份（Workspace）
    // 2. 停止runtime（调用TKE delete）
    // 3. 解绑Secret
    // 4. 标记deployment为retired
    // 5. 返回retirement confirmation
    
    // 注意: Retire ≠ Stop
    // Stop是暂停，Retire是永久删除
}
```

**验收标准**:
- [ ] service.go 实现完整Retire逻辑
- [ ] retire_test.go 单元测试: 验证状态转换
- [ ] 真实handler测试: Workspace → Serve Retire，返回confirmation
- [ ] Crash恢复测试: Retire到一半crash，restart后能继续
- [ ] Readback测试: Retire后Serve.GetDeployment返回retired状态

**涉及文件**:
- services/serve/internal/service.go
- services/serve/internal/deployment_store.go
- services/serve/internal/retire_test.go
- services/serve/internal/tke_adapter.go (调用TKE delete)

**依赖**: Sprint 1完成（创建链work后才能测删除）

---

### R07 - Ledger不接受删除receipt

**断点**: services/ledger/internal/eventconsumer/coordination.go:29,153 只接受funding/local kinds，PROVIDER_ACTION被拒

**影响**: 删除事件无法记账，退款流程断裂

**修复方案**:
```go
// coordination.go
const (
    ReceiptKindFunding = "FUNDING"
    ReceiptKindLocal = "LOCAL"
    ReceiptKindProviderAction = "PROVIDER_ACTION" // 新增
)

func (c *Coordinator) ProcessReceipt(ctx context.Context, receipt *Receipt) error {
    switch receipt.Kind {
    case ReceiptKindFunding:
        return c.processFunding(ctx, receipt)
    case ReceiptKindLocal:
        return c.processLocal(ctx, receipt)
    case ReceiptKindProviderAction: // 新增处理
        return c.processProviderAction(ctx, receipt)
    default:
        return status.Errorf(codes.InvalidArgument, "unsupported receipt kind: %s", receipt.Kind)
    }
}
```

**验收标准**:
- [ ] coordination.go 支持PROVIDER_ACTION kind
- [ ] coordination_test.go 单元测试: 删除receipt能处理
- [ ] 真实handler测试: Workspace deletion → Ledger append成功
- [ ] Readback测试: Ledger.GetProof能读取删除proof
- [ ] Reference read测试: 其他服务能通过obligationId查删除proof

**涉及文件**:
- services/ledger/internal/eventconsumer/coordination.go
- services/ledger/internal/eventconsumer/coordination_test.go
- services/ledger/internal/proof_store.go

**依赖**: R06完成（需要Serve先能retire）

---

### R08 - Fabric pending delete未恢复

**断点**: services/workspace/internal/launch/deletion.go:638,670 pending delete存进DB但只read，Fabric read不驱动delete

**影响**: Crash后删除不会重试

**修复方案**:
```go
// deletion.go (Workspace)
func (w *Worker) RecoverPendingDeletions(ctx context.Context) error {
    pendings := w.store.ListPendingDeletions(ctx)
    for _, pending := range pendings {
        // 重新驱动Fabric delete
        resp, err := w.fabricClient.DeleteResource(ctx, pending.ResourceID)
        if err == nil && resp.Deleted {
            w.store.MarkDeletionComplete(ctx, pending.ID)
        }
    }
}

// Fabric coordination/read.go
func (c *Coordinator) GetResource(ctx context.Context, id string) (*Resource, error) {
    res := c.store.Get(ctx, id)
    
    // 如果pending delete，驱动实际删除
    if res.Status == "PENDING_DELETE" {
        c.executeDelete(ctx, id)
    }
    
    return res, nil
}
```

**验收标准**:
- [ ] Workspace RecoverPendingDeletions在启动时调用
- [ ] Fabric read时检查pending delete并驱动
- [ ] Crash恢复测试: delete到一半kill进程，restart后继续
- [ ] 幂等测试: 重复删除请求不会报错
- [ ] 最终一致性测试: pending delete最终变成deleted

**涉及文件**:
- services/workspace/internal/launch/deletion.go
- services/workspace/internal/launch/recovery.go
- services/fabric/internal/coordination/read.go
- services/fabric/internal/coordination/delete.go

**依赖**: R06/R07完成（删除链上游work后才能测恢复）

---

### R04 - Gateway丢弃subscriptionPeriodId

**断点**: services/workspace/internal/launch/deletion.go:829 传了subscriptionPeriodId，但services/gateway-integration/internal/coordination.go:86 adapter丢弃

**影响**: 退款时无法关联订阅周期

**修复方案**:
```go
// coordination.go (Gateway)
type RefundOperation struct {
    WorkspaceID         string
    SubscriptionPeriodID string // 新增：不能丢弃
    ObligationID        string
    Amount              int64
}

func (a *Adapter) RequestRefund(ctx context.Context, req *WorkspaceRefundRequest) error {
    op := &RefundOperation{
        WorkspaceID:         req.WorkspaceId,
        SubscriptionPeriodID: req.Refund.SubscriptionPeriodId, // 保留
        ObligationID:        req.Refund.ObligationId,
        Amount:              req.Refund.Amount,
    }
    
    return a.coordinator.ProcessRefund(ctx, op)
}
```

**验收标准**:
- [ ] coordination.go RefundOperation补字段
- [ ] Gateway store落库subscriptionPeriodId
- [ ] 真实handler测试: Workspace deletion → Gateway refund，字段传递完整
- [ ] Readback测试: Ledger能通过subscriptionPeriodId查退款记录

**涉及文件**:
- services/workspace/internal/launch/deletion.go
- services/gateway-integration/internal/coordination.go
- services/gateway-integration/internal/gateway_store.go

**依赖**: R07完成（退款依赖Ledger先能记账）

---

### Sprint 2 验收门槛

**必须全部通过**:

1. **完整删除链测试** - Workspace → Serve → Fabric → Ledger → Gateway，真实PG
2. **Crash恢复测试** - 每个环节kill进程，restart后能继续
3. **幂等测试** - 重复删除请求不会重复扣款/报错
4. **字段完整性** - subscriptionPeriodId/obligationId全链路传递
5. **Receipt验证** - Ledger能存储并读取删除proof

---

## Sprint 3: 修模型配置链 (2周)

### R09 - Initial models不apply

**断点**: services/workspace/internal/launch/runtime.go:289 和 services/serve/internal/tke_adapter.go 非空initial model selections传输但不apply

**影响**: 用户配置的初始模型被忽略

**修复方案**:
```go
// Workspace runtime.go
func (w *Worker) StartRuntime(ctx context.Context, req *StartRequest) error {
    // 如果有initial model selections，传给Serve
    if len(req.InitialModels) > 0 {
        deployReq.InitialModelSelections = req.InitialModels
    }
}

// Serve tke_adapter.go
func (a *TKEAdapter) Deploy(ctx context.Context, req *DeployRequest) error {
    // Apply initial models
    if len(req.InitialModelSelections) > 0 {
        for _, model := range req.InitialModelSelections {
            a.applyModel(ctx, model)
        }
    }
}
```

**验收标准**:
- [ ] Workspace传递initial models到Serve
- [ ] Serve Start时apply initial models
- [ ] Readback测试: GetModels返回applied models与initial一致
- [ ] 空models测试: 无initial models时不报错（defaultApp场景）

**涉及文件**:
- services/workspace/internal/launch/runtime.go
- services/workspace/internal/launch/models.go
- services/serve/internal/tke_adapter.go
- services/serve/internal/runtime.go

**依赖**: Sprint 1完成（创建链work）

---

### R10 - Model update两端拒绝

**断点**: 
- services/workspace/internal/launch/models_update.go:359,369,449 直接Fabric bind被serve-only拒绝
- Serve也拒绝confirmedBinding输入

**影响**: 模型更新完全失败

**修复方案**:
```
1. Workspace不再直接Fabric bind，改为通过Serve
2. Serve handover完整迁移，不只改接口
3. Predecessor从正确source取（不是旧snapshot）
```

**验收标准**:
- [ ] Workspace → Serve UpdateModel，不直接Fabric
- [ ] Serve接受confirmedBinding
- [ ] Fabric serve-only校验通过
- [ ] Predecessor正确读取
- [ ] 真实handler测试: 模型更新成功

**涉及文件**:
- services/workspace/internal/launch/models_update.go
- services/serve/internal/secret_binding.go
- services/serve/internal/runtime.go
- services/fabric/internal/secret.go

**依赖**: R09完成（初始模型work后才能测更新）

---

### R11 - Model update权限错误

**断点**: services/workspace/internal/launch/models_update.go:78,330 用launch旧grant，update不在recovery范围

**影响**: 权限检查失败，update无法恢复

**修复方案**:
```go
// models_update.go
func (w *Worker) UpdateModels(ctx context.Context, req *UpdateRequest) error {
    // 申请新的update grant，不复用launch grant
    grant, err := w.tenantClient.RequestGrant(ctx, &GrantRequest{
        Action: "update_models",
        Resource: req.WorkspaceID,
        Actor: req.ActorID, // 使用update actor，不是launch actor
    })
}

// recovery.go
func (w *Worker) RecoverOperations(ctx context.Context) error {
    ops := w.store.ListPendingOperations(ctx)
    for _, op := range ops {
        switch op.Type {
        case "launch":
            w.recoverLaunch(ctx, op)
        case "update_models": // 新增
            w.recoverModelUpdate(ctx, op)
        }
    }
}
```

**验收标准**:
- [ ] UpdateModels申请own grant
- [ ] Recovery包含update_models类型
- [ ] Crash恢复测试: update到一半crash，restart后能继续
- [ ] 权限测试: update actor与launch actor不同时能通过

**涉及文件**:
- services/workspace/internal/launch/models_update.go
- services/workspace/internal/launch/recovery.go
- services/tenant/internal/authorization.go

**依赖**: R10完成（更新逻辑work后才能测权限/恢复）

---

### R12 - Model version覆盖

**断点**: services/serve/internal/tke_adapter.go:164 和 runtime.go:212 Observe写0覆盖applied N

**影响**: Applied version丢失

**修复方案**:
```go
// tke_adapter.go
func (a *TKEAdapter) Observe(ctx context.Context, runtimeID string) (*RuntimeState, error) {
    state := a.readRuntimeState(ctx, runtimeID)
    
    // 只写实际观测到的字段，不写0覆盖
    update := &RuntimeUpdate{
        Status: state.Status,
        // 不包含AppliedModelVersion，除非真的读到了
    }
    
    return a.store.UpdateRuntime(ctx, runtimeID, update)
}
```

**验收标准**:
- [ ] Observe不写0覆盖已有version
- [ ] 单元测试: version=N后Observe，version仍然=N
- [ ] Readback测试: AppliedModelVersion持久保留

**涉及文件**:
- services/serve/internal/tke_adapter.go
- services/serve/internal/runtime.go

**依赖**: R09完成（有applied version后才能测覆盖）

---

### Sprint 3 验收门槛

1. **初始模型测试** - 非空initial models被apply
2. **模型更新测试** - UpdateModels成功，version/key/runtime一致
3. **权限测试** - Update用own grant
4. **恢复测试** - Update crash后能继续
5. **Version持久性** - Observe不覆盖applied version

---

## Sprint 4: 修可靠性问题 (1周)

### R05/R13/R14 - 低优先级修复

这些都是边界case，修复方案见field-alignment-review.md。

**R05**: Cached proof validation - funding.go:181  
**R13**: RuntimeReadback字段差异 - write.go:543,622  
**R14**: Name长度冲突 - service.go:152

---

## 并行规则

### 可以并行（不同owner，写集不重叠）

- **Sprint 1**: R01 (UI) 和 R02 (Workspace) 并行
- **Sprint 1**: R03 (Gateway) 独立并行
- **Sprint 2**: R04 (Gateway adapter) 和 R08 (Workspace recovery) 并行

### 必须串行（写集重叠或依赖）

- **Sprint 2**: R06 → R07 → R08（删除链依赖）
- **Sprint 3**: R09 → R10 → R11 → R12（模型链依赖）
- **Sprint 3**: R10 E与A1重叠 services/serve/internal/delivery/runtime_bridge.go，必须串行

### 共享文件串行

- 任何涉及 contracts/ revision变更必须串行
- 任何涉及main分支的修改必须串行
- 任何涉及生产环境的变更必须串行

---

## 验收标准（每个PR必须满足）

### 1. 字段追踪契约

每个跨owner字段修复必须记录：

```markdown
## 字段追踪契约 - [字段名]

- **Producer**: services/workspace/internal/deletion.go:829
- **Wire**: WorkspaceDeleteRequest.refund.subscriptionPeriodId
- **Consumer**: services/gateway/internal/coordination.go:86 (MUST consume)
- **Storage**: gateway_operations表.subscription_period_id列
- **Readback**: services/workspace/internal/deletion.go:850
- **Test**: 真实PG组合测试，验证字段完整传递
```

### 2. 真实组合测试

不能只有单元测试，必须包含：

```go
// 真实PG组合测试示例
func TestDeleteWorkspace_RealPG(t *testing.T) {
    // 1. 启动真实Workspace + Gateway + Ledger服务（独立PG）
    // 2. 创建workspace
    // 3. 删除workspace
    // 4. 验证: Serve retired, Ledger proof存在, Gateway refund确认
    // 5. 验证: subscriptionPeriodId全链路传递
}
```

### 3. Crash恢复测试

每个异步操作必须有：

```go
func TestDeleteWorkspace_CrashRecovery(t *testing.T) {
    // 1. 开始删除
    // 2. 删除到一半kill进程
    // 3. Restart进程
    // 4. 验证: 删除自动继续并完成
}
```

### 4. 命名区分

必须区分：

- `Retire` vs `Stop` (永久删除 vs 暂停)
- `OwnedValue` vs `OpaqueRef` (自己的字段 vs 引用别人的ID)
- `Readiness` vs `Requested` (实际状态 vs 用户请求)
- `CachedProof` vs `LiveProof` (本地缓存 vs 实时查询)

### 5. 接口迁移checklist

改接口时必须：

1. [ ] grep -r "旧接口名" services/ 列出所有caller
2. [ ] 同一个PR迁移所有caller
3. [ ] 删除旧接口
4. [ ] 验收: grep旧接口名返回0结果

---

## 失败模式registry

每个修复必须考虑这些场景：

| 场景 | 必须验证 |
|---|---|
| 网络断开 | accepted operation durable，restart后能继续 |
| Crash | 异步操作能恢复 |
| 重复请求 | 幂等，不会重复扣款/创建资源 |
| 字段drift | Fingerprint包含所有业务身份字段 |
| 依赖服务unavailable | 返回明确错误，不伪造成功 |
| 权限过期 | 续作grant正确，不复用旧grant |

---

## 不在范围内（不要做）

1. ❌ 新架构/新服务/新事件总线
2. ❌ 全仓字段registry
3. ❌ 视觉重设计
4. ❌ 生产部署/真实扣款
5. ❌ 批量删除旧服务
6. ❌ 手改旧receipt的hash/pass状态

---

## 机制文档（Sprint 4后汇总）

修完所有bug后，汇总成3个机制文档：

### 1. docs/field-tracking-contract.md

模板：
```markdown
# 跨Owner字段追踪契约

每个跨owner字段必须声明：
- Producer: 哪个文件、哪一行产生
- Wire: 通过哪个proto/DTO传输
- Consumer: 哪个文件、哪一行消费
- Storage: 落库到哪个表、哪个列
- Readback: 哪个接口读回
- Test: 真实PG组合测试覆盖
```

### 2. docs/integration-test-standard.md

```markdown
# 真实PG组合测试标准

## 不够的测试
- ❌ 单元测试用fake: 不能证明真实handler会接受
- ❌ Mock返回成功: 不能证明实际会work

## 必须的测试
- ✅ 真实handler + 真实PG
- ✅ 跨owner调用（如Workspace → Gateway → Ledger）
- ✅ Crash恢复测试
```

### 3. docs/interface-migration-checklist.md

```markdown
# 接口变更Checklist

改接口时必须：
1. grep所有caller
2. 同PR迁移所有caller
3. 删除旧接口
4. 验收: grep旧接口返回0结果

禁止：
- ❌ 改了接口但caller还在用旧的
- ❌ 新旧接口共存
- ❌ 假设"没人用了"就不迁移
```

---

## 拷打清单（给我的约束）

你可以用这些来"拷打"我：

1. **每个PR必须包含字段追踪契约** - 没写契约不准merge
2. **每个修复必须有真实PG测试** - 只有单元测试不够
3. **接口变更必须grep所有caller** - 漏一个caller就是bug
4. **Crash恢复测试必须有** - 异步操作没恢复测试不准merge
5. **命名必须区分** - retire/stop混用直接打回
6. **不能跳sprint** - Sprint 1没验收通过不准进Sprint 2
7. **不能修范围外的东西** - 看到"新架构"三个字直接拒绝
8. **不能假设"应该work"** - 必须有测试证明

---

## 执行开始

现在开始 **Sprint 1 → R02 source validation时序问题**。

下一步: 读取相关文件，理解当前实现，给出具体修复代码。

## 执行记录 — Sprint 1 → R02

### 2026-10-13 修复完成

**修改文件：**
1. `services/workspace/internal/launch/application_source.go:217-263`
   - 新增 `validateSourceReadiness()` 函数
   - 在资金/资源分配前验证 source 状态
   - 检查 CapabilityVersion 或 RuntimeRelease 当前是否 APPROVED
   - 返回 `FailedPrecondition` 如果 source 无效/已撤销

2. `services/workspace/internal/launch/recovery.go:214-217`
   - 在 `validateAcceptance()` 之后、`ensureResources()` 之前插入验证
   - 调用 `validateSourceReadiness(ctx, op, result.GrantID, accepted)`
   - 失败时通过 `failedCall()` 记录到 "source_validation" phase

**时序修正：**
- **之前**：AcceptQuote → ensureResources → fundingEvidence → ensureResources(with receipt) → resumeRuntime → resolveApplicationSource (TOO LATE)
- **现在**：AcceptQuote → **validateSourceReadiness** → ensureResources → fundingEvidence → ensureResources(with receipt) → resumeRuntime

**验证状态：**
- [x] 代码编译通过 (`go build ./internal/launch`)
- [ ] 单元测试（待编写）
- [ ] 集成测试：真实 Catalog → Workspace，source 撤销场景
- [ ] Crash recovery 测试：validation 后 crash 恢复
- [ ] 字段追踪 contract 文档

**下一步：** 编写测试覆盖
