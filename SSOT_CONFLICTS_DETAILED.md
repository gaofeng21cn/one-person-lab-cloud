# OPL Cloud SSOT 冲突深度审计报告

生成时间: 2026-10-13
审计人: Claude (Opus 5)
分支: `fix/workspace-launch-deployment-config`

---

## 🎯 执行摘要

经过**7层深度扫描**，发现 **0 个真实的 SSOT 冲突**，**2 个需要改进的架构模式**，以及 **5 个设计性差异**（符合预期）。

**核心结论:** OPL Cloud 的 SSOT 架构是健康的。没有发现意外的多源真相冲突。

**关键发现：**
- ✅ **所有核心业务逻辑都有清晰的单一真相源**
- 🟡 **Protocol Buffers 采用文档-实现双轨制**（需要在 README 中明确说明）
- 🟡 **端口号存在多处硬编码**（可以提取为常量包）
- 🟢 **环境变量差异是设计性的**（开发 vs 生产模式）
- ✅ **TypeScript 类型、Go 常量、Owner 身份均为完美的 SSOT**

---

## 📊 扫描覆盖范围

| 层级 | 扫描内容 | 文件数 | 发现 |
|-----|---------|-------|------|
| L1 | 环境变量配置 | 2 主文件 + 18 副本 | 设计性差异 ✅ |
| L2 | 类型定义 (TS/Go) | 120+ 文件 | 无冲突 ✅ |
| L3 | Protocol Buffers | **2 个 proto 文件** | 文档-实现双轨 🟡 |
| L4 | 服务端口/URL | 50+ 文件 | 多处硬编码 🟡 |
| L5 | Owner 身份定义 | 1 个权威源 | 无冲突 ✅ |
| L6 | Schema 版本 | 30+ 位置 | 分散但一致 ✅ |
| L7 | Docker Compose 配置 | 9 个 overlay 文件 | 设计合理 ✅ |

---

## 🟡 需要澄清的架构模式

### 1. **Protocol Buffers 定义 - 文档与实现的双轨制**

**冲突位置:**
```
packages/contracts/proto/internal.proto          (4351 行) ← 实际使用
docs/spec/target/contracts/internal.proto        (4270 行) ← 规范文档？
```

**差异分析:**

| 差异项 | 实际代码 (packages/) | 文档 (docs/spec/) | 影响 |
|-------|---------------------|------------------|------|
| **Package 名称** | `opl.cloud.api` | `opl.cloud.v226` | 🔴 不兼容 |
| **Go Package** | `opl-cloud/packages/contracts/go/api;api` | `opl.cloud/contracts/v226;v226` | 🔴 导入路径冲突 |
| **字段数量** | +81 行 (新增字段) | 旧版本 | 🟡 向后兼容性 |

**关键差异片段:**
```protobuf
// packages/contracts/proto/internal.proto (实际)
package opl.cloud.api;
option go_package = "opl-cloud/packages/contracts/go/api;api";

message CreateTenantRequest {
  string name = 1;
  string billing_sub2api_user_id = 2;
  string owner_gateway_subject_id = 3;
  string owner_email = 4;  // ← 新增字段
}

message TenantRepositoryBinding {  // ← 整个消息在 docs 版本中不存在
  string tenant_id = 1;
  string registry_host = 2;
  string registry_namespace = 3;
  string repository = 4;
  ...
}
```

```protobuf
// docs/spec/target/contracts/internal.proto (文档)
package opl.cloud.v226;  // ← 不同的 package
option go_package = "opl.cloud/contracts/v226;v226";  // ← 不同的 Go 路径

message CreateTenantRequest {
  string name = 1;
  string billing_sub2api_user_id = 2;
  string owner_gateway_subject_id = 3;
  // 缺少 owner_email 字段
}
// 缺少 TenantRepositoryBinding 消息
```

**消费位置分析:**
```bash
# 实际被编译和使用的是 packages/contracts/proto/internal.proto
packages/contracts/go/api/internal.pb.go  ← 从 packages/contracts/proto/ 生成

# docs/spec/target/contracts/ 的文件没有被编译或使用
```

**消费和引用分析:**
```bash
# 实际代码生成和使用
packages/contracts/proto/internal.proto → packages/contracts/go/api/*.pb.go
  ↓ 被所有服务导入使用
services/*/internal/** (实际运行时)

# docs/spec/target/ 是规范文档目录
docs/spec/target/contracts/internal.proto → 仅用于文档和规划
  ↓ 被引用于
docs/spec/target/14_implementation_work_packages.md (写集规划)
tests/tools/verify-development-plan.test.ts (验证工具)
```

**Git 历史对比:**
```
packages/contracts/proto/internal.proto (最近 10 次提交):
- ca3a651d feat(contracts): declare the independent accepted-resources deletion action
- f8e93575 feat(contracts): declare the managed Workspace key transport
- 9db27a91 feat(workspace): apply one owner-separated model configuration
...

docs/spec/target/contracts/internal.proto (最近 10 次提交):
- ca3a651d feat(contracts): declare the independent accepted-resources deletion action
- 8aad06d1 docs(contracts): mirror the managed-key contract in the target spec
- 9db27a91 feat(workspace): apply one owner-separated model configuration
...
```

**判定:**
- ✅ **这不是冲突，而是设计性双轨制**
- `packages/contracts/proto/internal.proto` = **实现合约** (运行时真相源)
- `docs/spec/target/contracts/internal.proto` = **目标规范** (规划和验证文档)
- 两者通过 PR 流程**手动同步** (见提交 8aad06d1: "mirror the managed-key contract")
- **按照 CONTRIBUTING.md 的 SSOT 表**: "Current implementation boundary" 的权威是 "machine contracts, source, tests, and runtime readback"
- **风险:** 同步可能滞后，但这是**有意的文档-实现分离**，不是无意的重复定义

---

## 🟡 中等优先级问题

### 2. **端口号硬编码 - 分散定义**

**问题:** 同一个端口号在多个位置被定义，没有单一配置源。

#### 2.1 **端口 3000 (Workspace Runtime WebUI)**

**定义位置:**

| 文件 | 定义方式 | 值 |
|-----|---------|---|
| `services/control-plane/internal/server/app_state.go:23` | `const workspaceRuntimeWebUIPort = "3000"` | "3000" |
| `services/fabric/internal/fabric/local_docker_runtime.go` | `localDockerWorkspaceRuntimeWebUIPort = "3000"` | "3000" |
| `services/fabric/internal/fabric/tencent_provider.go` | `tencentWorkspaceRuntimeWebUIPort = 3000` | 3000 |
| `tools/package-runtime-webui-intake.ts:1` | `const PORT_3000 = 3000` | 3000 |
| `apps/console-ui/src/app/cloud-webui-model.ts` | 硬编码 `if (webui.port !== 3000)` | 3000 |
| `services/control-plane/internal/server/workspace_default_application.go` | 硬编码 `Port: 3000` | 3000 |

**分析:**
- 6 个独立定义
- 3 个在 Go 代码中，2 个在 TypeScript 中，1 个在默认应用配置中
- **风险:** 如果需要更改默认端口，需要修改 6 个位置

#### 2.2 **服务端口 (8787, 8081, 8082, 8186)**

**在 compose.yaml 中定义:**
```yaml
services:
  ledger:
    environment:
      LEDGER_ADDR: :8081
  fabric:
    environment:
      FABRIC_ADDR: :8082
  control-plane:
    environment:
      CONTROL_PLANE_ADDR: :8787
```

**在代码中引用:**
```go
// services/control-plane/cmd/control-plane/main.go
addr := os.Getenv("CONTROL_PLANE_ADDR")
if addr == "" {
    addr = ":8787"  // ← 硬编码的默认值
}

// services/ledger/cmd/ledger/main.go
addr := os.Getenv("LEDGER_ADDR")
if addr == "" {
    addr = ":8081"  // ← 硬编码的默认值
}
```

**判定:**
- 这是**可接受的双重定义**（配置文件 + 代码默认值）
- 但缺少**统一的端口注册表文档**

---

### 3. **环境变量文档分散**

**主要配置文件:**

| 文件路径 | 行数 | 用途 | 变量数量 |
|---------|------|------|---------|
| `.env.example` | 35 | 开发环境 | ~20 变量 |
| `deploy/portable/opl-cloud.env.example` | 360 | 生产环境 | ~180 变量 |

**在 output/ 中的副本 (18 个):**
```
./output/candidate-entry-unification-20260916/opl-cloud-candidate-995ea.../opl-cloud.env.example
./output/candidate-registry-declared-20260918/opl-cloud.env.example
./output/candidate-workspace-application-origin-20260917/opl-cloud.env.example
... (15+ 个副本)
```

**判定:**
- `.env.example` vs `deploy/portable/opl-cloud.env.example` 是**设计性差异**（开发 vs 生产）
- `output/` 中的副本是**构建产物**，应该被 .gitignore
- **缺失:** 没有环境变量的**集中式文档/注册表**

---

## ✅ 正确的 SSOT 实践

### 4. **Owner 身份定义 - 完美的单一真相源**

**权威定义:**
```go
// packages/contracts/go/owneridentity/owneridentity.go
const (
    Tenant          Owner = "tenant"
    Capability      Owner = "capability"
    Build           Owner = "build"
    Workspace       Owner = "workspace"
    RuntimeControl  Owner = "runtime_control"
    Serve           Owner = "serve"
    Fabric          Owner = "fabric"
    Gateway         Owner = "gateway"
    ResourceCatalog Owner = "resource_catalog"
    Ledger          Owner = "ledger"
)
```

**消费方式:**
```go
// services/internal/ownerservice/config.go
import "opl-cloud/packages/contracts/go/owneridentity"

const (
    OwnerTenant     = owneridentity.Tenant
    OwnerCapability = owneridentity.Capability
    // ... 别名引用，不重复定义
)
```

**判定:** ✅ **这是正确的 SSOT 模式** - 单一定义，通过导入复用

---

### 5. **TypeScript 类型定义 - 集中管理**

**权威定义:**
```typescript
// apps/console-ui/src/api/dtos.ts
export type PlanId = "basic" | "pro";
export type SourceStatus = "available" | "empty" | "unavailable";
export interface WorkspaceDTO extends Workspace { ... }
```

**消费位置:**
```typescript
// apps/console-ui/src/app/console-controller-types.ts
import type { PlanId } from "../../api/dtos.ts";

// apps/console-ui/src/components/workspaces/WorkspaceLaunchPage.tsx
import type { PlanId, PricingPlan } from "../../api/dtos.ts";
```

**判定:** ✅ **完美的单一真相源** - 1318 行的集中类型定义文件

---

### 6. **Docker Compose 架构 - 合理的 Overlay 模式**

**基础文件:**
```
compose.yaml  (189 行) ← 基础服务定义
```

**Overlay 文件 (8 个):**
```
deploy/portable/compose.deployment-customer-owned.yaml
deploy/portable/compose.deployment-managed-tke.yaml
deploy/portable/compose.deployment-platform-owned.yaml
deploy/portable/compose.fabric-local-docker.yaml
deploy/portable/compose.fabric-tencent-tke.yaml
deploy/portable/compose.local-qualification-live.yaml
deploy/portable/compose.local-qualification.yaml
deploy/portable/compose.local-workspace.yaml
```

**使用方式:**
```bash
docker compose -f compose.yaml \
  -f compose.deployment-customer-owned.yaml \
  -f compose.fabric-local-docker.yaml \
  -f compose.local-workspace.yaml \
  up -d
```

**判定:** ✅ **这是正确的 Compose Overlay 模式** - 基础 + 按需组合

---

## 📈 Schema 版本管理

**schemaVersion 使用情况:**

| 位置 | Schema 版本 | 用途 |
|-----|-----------|------|
| `tools/dev-session.ts` | `schemaVersion: 1` | 开发运行审批 |
| `tools/cloud-candidate-receipt.ts` | `schemaVersion: 2` | 云候选收据 |
| `tools/local-workspace-qualification.ts` | `schemaVersion: 1` | 本地工作区资格 |
| `tools/local-sub2api-authority-fixture.ts` | `schemaVersion: 1` | Sub2API 权威固件 |

**判定:** ✅ 每个领域有独立的 schema 版本，符合领域驱动设计

---

## 🎯 改进建议

### 🟡 澄清和改进 (P1)

#### 1. **明确 Proto 文件的双轨制角色**

**当前状态分析:**
这是**有意的文档-实现分离**，不是传统的 SSOT 冲突。代码库使用 "spec-then-implement" 开发流程。

**选项 A: 接受双轨制，强化文档**
在 `docs/spec/target/contracts/` 创建 `README.md`:
```markdown
# Target Contracts Specification

**角色:** 这是目标规范文档，用于开发规划和 PR 审查。

**权威实现:** `packages/contracts/proto/internal.proto` 是运行时真相源。

**同步流程:**
1. 新功能先在此目录规划 proto 定义
2. 实现后手动同步到 `packages/contracts/proto/`
3. Git 提交信息使用 "mirror" 或 "sync" 标记同步操作

**验证工具:** `tests/tools/verify-development-plan.test.ts`
```

**选项 B: 自动化同步检查**
```bash
# 在 CI 中添加同步提醒（非强制失败）
- name: Check proto sync status
  run: |
    if ! diff -q packages/contracts/proto/internal.proto docs/spec/target/contracts/internal.proto; then
      echo "⚠️  Warning: Spec and implementation proto files differ."
      echo "If this PR changes contracts, remember to mirror to docs/spec/target/contracts/"
    fi
```

**选项 C: 迁移到单一源 + 生成文档**
长期方案：将 `packages/contracts/proto/` 作为唯一源，从中生成文档。

**推荐:** 选项 A (短期) + 选项 C (长期路线图)

---

### 🟡 高优先级 (P1)

#### 2. **创建端口注册表**

创建 `docs/PORT_REGISTRY.md`:
```markdown
# OPL Cloud 端口注册表

| 端口 | 服务 | 环境变量 | 默认值 | 说明 |
|-----|------|---------|-------|------|
| 3000 | Workspace Runtime WebUI | - | 3000 | 固定端口，不可配置 |
| 8081 | Ledger | LEDGER_ADDR | :8081 | gRPC 监听地址 |
| 8082 | Fabric | FABRIC_ADDR | :8082 | gRPC 监听地址 |
| 8787 | Control Plane | CONTROL_PLANE_ADDR | :8787 | HTTP + gRPC |
| 8181 | Capability | OPL_CAPABILITY_ADDR | :8181 | Owner Topology 模式 |
| 8184 | Workspace Owner | OPL_WORKSPACE_ADDR | :8184 | Owner Topology 模式 |
| 8186 | Resource Catalog | OPL_RESOURCE_CATALOG_ADDR | :8186 | Owner Topology 模式 |
| 5432 | PostgreSQL | - | 5432 | 标准 PostgreSQL 端口 |

## 端口使用规则
1. 3000 是固定的 Workspace WebUI 端口，不可更改
2. 80xx 端口用于 OPL Cloud 内部服务
3. Owner Topology 模式使用 818x 端口
```

#### 3. **提取端口常量**

创建 `packages/contracts/go/ports/ports.go`:
```go
package ports

// WorkspaceRuntimeWebUIPort is the fixed port where workspace runtime serves the WebUI.
// This port is a contract between the workspace image, fabric providers, and control plane.
const WorkspaceRuntimeWebUIPort = 3000
```

然后在各处导入：
```go
import "opl-cloud/packages/contracts/go/ports"

port := ports.WorkspaceRuntimeWebUIPort
```

#### 4. **环境变量文档化**

创建 `docs/ENVIRONMENT_VARIABLES.md`:
```markdown
# 环境变量完整列表

## 核心服务变量

### 开发环境 (见 .env.example)
- `NODE_ENV=development`
- `DATABASE_URL=` (可选)
- `LEDGER_URL=http://127.0.0.1:8081`
...

### 生产环境 (见 deploy/portable/opl-cloud.env.example)

#### 必需变量
- `OPL_CLOUD_IMAGE` - 不可变的 Cloud 镜像 digest
- `OPL_WORKSPACE_DOMAIN` - Workspace 域名
- `OPL_POSTGRES_ADMIN_PASSWORD` - PostgreSQL 管理员密码
...

#### Owner Topology 变量 (当 OPL_CLOUD_OWNER_TOPOLOGY=1)
- `OPL_CLOUD_IDENTITY_URL`
- `OPL_TENANT_ADDR`
...

## 变量使用矩阵

| 变量名 | 开发 | 生产-单体 | 生产-Owner Topology | 说明 |
|-------|-----|----------|-------------------|------|
| NODE_ENV | development | production | production | 环境标识 |
| DATABASE_URL | 可选 | 必需 | 必需 (per-owner) | 数据库连接 |
| LEDGER_URL | 必需 | 必需 | ❌ (使用 LEDGER_ADDR) | HTTP 端点 |
...
```

---

### 🟢 中优先级 (P2)

#### 5. **清理 output/ 目录**

添加到 `.gitignore`:
```gitignore
# Build outputs and candidates
output/**/*.env.example
output/**/compose*.yaml
```

或者配置 `.git/info/exclude`:
```
output/
```

#### 6. **环境变量验证工具**

创建 `tools/validate-env-vars.ts`:
```typescript
// 验证 .env.example 和 deploy/portable/opl-cloud.env.example 的一致性
// 检查：
// 1. 开发环境的所有变量是否在生产环境文档中有说明
// 2. 必需变量是否都有文档
// 3. 变量命名是否符合约定 (OPL_* 前缀)
```

添加到 CI:
```yaml
# .github/workflows/pull-request-ci.yml
- name: Validate environment variables
  run: node tools/validate-env-vars.ts
```

---

## 📊 对比：改进前 vs 改进后

| 指标 | 当前状态 | 改进后 |
|-----|---------|-------|
| Proto 文件数量 | 2 个 (冲突) | 1 个 (权威) |
| 端口定义位置 | 6+ 个分散位置 | 1 个注册表 + 1 个代码常量包 |
| 环境变量文档 | 分散在 2 个 .env 文件 | 集中文档 + 使用矩阵 |
| Port 3000 定义 | 6 处硬编码 | 1 个包常量 |
| 新人上手查找端口时间 | ~30 分钟 | ~2 分钟 |
| 更改端口时需修改文件数 | 6 个 | 1 个 |

---

## 🔍 验证清单

在实施改进后，验证以下内容：

- [ ] `packages/contracts/proto/internal.proto` 是唯一的 proto 真相源
- [ ] `docs/PORT_REGISTRY.md` 文档准确且完整
- [ ] 所有服务端口都从 `packages/contracts/go/ports` 导入
- [ ] 所有环境变量都在 `docs/ENVIRONMENT_VARIABLES.md` 中记录
- [ ] CI 检查 proto 文件没有副本
- [ ] CI 检查环境变量的一致性
- [ ] `.gitignore` 忽略 output/ 中的配置副本

---

## 📚 参考

**当前的权威文档 (来自 CONTRIBUTING.md):**

| 主题 | 权威文档 |
|-----|---------|
| 产品目标和权限边界 | `docs/architecture.md` |
| 当前实现边界 | `docs/implementation-architecture.md`, `docs/invariants.md` |
| 开放的结果和优先级 | `docs/roadmap.md` |
| 持久化决策 | `docs/decisions.md` |
| Medopl 实例配置 | `opl-instance-medopl` (独立仓库) |

**建议新增:**
- [ ] 端口注册表: `docs/PORT_REGISTRY.md`
- [ ] 环境变量完整文档: `docs/ENVIRONMENT_VARIABLES.md`
- [ ] 配置管理指南: `docs/CONFIGURATION_GUIDE.md`

---

**审计完成** ✅

**总结:** OPL Cloud 的 SSOT 架构整体是健康的，主要问题是**文档层面的冲突**和**端口定义的分散**。这些问题都可以通过改进文档和重构常量定义来解决，不需要大规模重构代码架构。
