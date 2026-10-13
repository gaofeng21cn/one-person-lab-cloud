# SSOT (Single Source of Truth) 审计报告

生成时间: 2026-10-13
分支: `fix/workspace-launch-deployment-config`

---

## 📋 执行概要

本报告系统性扫描了 OPL Cloud 项目的单一真相源（SSOT）情况，识别冲突、消费位置、开发标准和业务流程。

---

## 🚨 发现的 SSOT 冲突

### 1. 环境变量配置 - **严重冲突**

**冲突的真相源:**
- `.env.example` (开发环境，35行)
- `deploy/portable/opl-cloud.env.example` (生产环境，360行)

**冲突详情:**

| 环境变量 | 开发环境 (.env.example) | 生产环境 (deploy/portable/) | 冲突状态 |
|---------|------------------------|---------------------------|---------|
| `NODE_ENV` | `development` | `production` | ✅ 预期差异 |
| `DATABASE_URL` | 空 | 必需 (per-service) | ✅ 预期差异 |
| `OPL_WORKSPACE_IMAGE` | ❌ 不存在 | ✅ 必需 | 🔴 **缺失** |
| `OPL_WORKSPACE_IMAGE_RELEASES_JSON` | ❌ 不存在 | ✅ 必需 | 🔴 **缺失** |
| `OPL_CLOUD_OWNER_TOPOLOGY` | ❌ 不存在 | `1` (190+ 行配置) | 🔴 **架构差异** |
| `OPL_SUB2API_BASE_URL` | ✅ 定义 | ✅ 定义 | ✅ 一致 |
| `LEDGER_URL` | `http://127.0.0.1:8081` | ❌ 不存在 (使用 gRPC) | 🟡 **协议差异** |
| `FABRIC_URL` | `http://127.0.0.1:8082` | ❌ 不存在 (使用 gRPC) | 🟡 **协议差异** |

**影响分析:**
- 开发环境使用简化的 monolithic HTTP 架构
- 生产环境使用 Owner Topology (分布式 gRPC 微服务架构)
- **这不是配置冲突，而是两种不同的部署模式**

**消费位置:**
```
services/control-plane/internal/server/app_state.go
services/control-plane/internal/server/deployment_profile.go
services/fabric/internal/fabric/tencent_provider.go
services/serve/internal/delivery/routeorigin.go
tools/staging-env.ts
tools/local-workspace-qualification.ts
```

---

### 2. 类型定义 - 轻微重复

**PlanId 类型定义:**

```typescript
// apps/console-ui/src/api/dtos.ts:122
export type PlanId = "basic" | "pro";

// 消费位置：
// - apps/console-ui/src/app/console-controller-types.ts
// - apps/console-ui/src/components/workspaces/WorkspaceLaunchPage.tsx
```

**状态:** ✅ **单一定义，多处引用 - 正确的 SSOT 模式**

---

### 3. 配置结构体 - Go 服务层

**Config 结构体分布:**

```
services/internal/ownerservice/config.go          # Owner 配置基类
services/build/cmd/server/main.go                 # Build 服务配置
services/capability/cmd/server/main.go            # Capability 配置
services/control-plane/cmd/control-plane/main.go  # Control Plane 配置
```

**状态:** ✅ **每个服务有独立配置，通过 `ownerservice.LoadConfig` 统一加载逻辑**

---

## 📍 SSOT 消费位置映射

### 环境变量消费统计

```bash
# Go 服务中的环境变量读取
os.Getenv 使用位置: 20+ 文件
主要消费者:
  - services/control-plane/cmd/control-plane/main.go
  - services/internal/ownerservice/config.go
  - services/fabric/internal/fabric/tencent_provider.go
  - services/build/cmd/server/main.go
```

### 关键配置流向

```
环境变量 (.env / opl-cloud.env)
    ↓
ownerservice.LoadConfig() / 各服务 main.go
    ↓
各 Owner 的 Config 结构体
    ↓
运行时服务实例
```

---

## 📖 开发标准文档

### SSOT 权威文档 (CONTRIBUTING.md)

| 主题 | 权威文档 |
|-----|---------|
| 产品目标和权限边界 | `docs/architecture.md` |
| 当前实现边界 | `docs/implementation-architecture.md`, `docs/invariants.md` |
| 开放的结果和优先级 | `docs/roadmap.md` |
| 持久化决策 | `docs/decisions.md` |
| Medopl 实例配置、生产部署 | `opl-instance-medopl` (独立仓库) |

**关键原则:**
> "Issues, pull requests, discussions, and project boards are proposals or work surfaces. They do not replace these owners."

**冲突解决流程:**
```
1. 通过 Git 历史追溯冲突
2. 检查规范主题所有者
3. 检查实际调用者
4. 检查运行时证据
5. 将每个声明分类为: target / current / evidence / historical / stale / derived / unknown
6. 更新规范所有者
7. 在同一 PR 中删除重复的当前写入者
```

---

## 🔄 业务流程 (从开发标准梳理)

### 1. 开发工作选择流程

```
1. 阅读 docs/roadmap.md 中的排序结果
   ↓
2. 检查是否有现有的 PR 所有者
   ↓
3. 选择一个 gap ID，在 PR 中明确其 owner/write set
   ↓
4. 检查 docs/implementation-architecture.md 的模块清单
   ↓
5. Rebase 到最新的 main
```

### 2. 分支和 PR 流程

```
origin/main
   ↓ 创建短期分支
codex/<objective> 或 fix/<name>
   ↓ 实现 + 测试
Open PR to main + 完成模板
   ↓ 更新到当前 main
解决所有 review 对话
   ↓ CI 检查
npm run verify:local (必需)
   ↓ 合并后
删除分支
```

### 3. 验证流程

**聚焦验证 (推荐日常使用):**
```bash
npm run verify:local:focused -- --base origin/main
npm run verify:local:focused -- --base origin/main --test tests/tools/dev-session.test.ts
```

**完整验证 (CI 级别):**
```bash
npm run verify:local              # 基础门槛，无数据库
npm run verify:local:full         # 使用临时 PostgreSQL 容器
```

**生成字段验证:**
```bash
npm run verify:generated-contracts
npm run verify:development-plan
```

### 4. Scoped Development 流程 (受限工作者模式)

```
信任的主机 (Host)
   ↓ 批准运行
npm run dev:approve -- /absolute/host-approval.json /absolute/host-store
   ↓ 提供上下文
npm run dev:context -- /absolute/host-store run-id
   ↓ 执行并验证
npm run dev:verify -- /absolute/host-store run-id gate-id
   ↓ 导出源检查
node tools/dev-session.ts source-check /absolute/host-store <run-id> <gate-id>
```

### 5. PR 治理记录流程

每个 PR 必须包含治理记录：
- 基础 SHA
- 所有者和阶段记录
- 写入集合
- 收据条目 (source-check / business / instance)
- 终端状态: `merge-ready` / `source-complete` / `blocked`

**机器执行检查:**
```bash
npm run check:pr-body -- --body-file /absolute/pr-body.md --base origin/main
```

---

## 🎯 关键发现总结

### ✅ 正确的 SSOT 实践

1. **类型定义集中化**: `apps/console-ui/src/api/dtos.ts` 是前端类型的单一真相源
2. **配置加载统一**: `services/internal/ownerservice/config.go` 提供统一的配置加载逻辑
3. **文档权威明确**: `CONTRIBUTING.md` 明确定义了每个主题的权威文档
4. **合约集中管理**: `packages/contracts/` 是跨服务合约的单一真相源

### 🟡 需要注意的差异

1. **部署模式差异** (不是冲突):
   - 开发: Monolithic HTTP 架构 + 内存存储
   - 生产: Owner Topology 微服务架构 + PostgreSQL + gRPC

2. **环境变量集合差异** (设计如此):
   - `.env.example`: 开发最小配置
   - `deploy/portable/opl-cloud.env.example`: 生产完整配置

### 🔴 潜在风险点

1. **配置文档同步**: 当添加新环境变量时，需要同时更新两个文件
2. **架构转换**: 开发到生产的架构差异可能导致问题难以在本地重现
3. **配置复杂度**: 生产环境 360 行配置，错误配置风险高

---

## 💡 建议

### 短期 (立即可做)

1. **在两个 .env.example 文件顶部添加交叉引用注释**
2. **创建配置验证工具**: 检查生产配置模板的完整性
3. **文档化架构差异**: 在 DEV_GUIDE.md 中明确说明开发与生产的架构差异

### 中期 (计划中)

1. **配置生成器**: 从单一源生成开发和生产配置
2. **环境变量注册表**: 维护所有环境变量的集中清单
3. **配置验证 CI**: 在 PR 中自动检查新环境变量是否在两处都有文档

### 长期 (架构级)

1. **统一部署模式**: 考虑让开发环境也能选择性启用 Owner Topology
2. **配置即代码**: 使用类型安全的配置系统替代环境变量

---

## 📚 相关文档引用

- **SSOT 所有权**: `CONTRIBUTING.md` 第 7-16 行
- **冲突解决**: `CONTRIBUTING.md` 第 20-24 行
- **开发流程**: `DEV_GUIDE.md` 第 21-68 行
- **PR 治理**: `DEV_GUIDE.md` 第 178-284 行
- **环境配置**: `.env.example`, `deploy/portable/opl-cloud.env.example`

---

**审计完成** ✓
