# OPL Cloud SSOT 审计执行摘要

**审计日期:** 2026-10-13
**分支:** `fix/workspace-launch-deployment-config`
**审计深度:** 7 层全栈扫描

---

## 🎯 核心结论

**OPL Cloud 的 SSOT 架构是健康的。**

- ✅ **0 个真实的 SSOT 冲突**（无意外的多源真相）
- ✅ **审计时记录的 Proto 双轨制已收敛**为 `packages/contracts/proto/` 单一源
- 🟡 **1 个仍待改进的架构模式**（端口号分散定义）
- 🟢 **5 个设计性差异**（符合预期的多环境/多模式配置）

---

## 📊 审计覆盖范围

| 扫描层级 | 内容 | 文件数 | 结果 |
|---------|------|-------|------|
| **L1 配置管理** | 环境变量、Docker Compose | 27 个文件 | ✅ 设计合理 |
| **L2 类型系统** | TypeScript/Go 类型定义 | 120+ 文件 | ✅ 完美的单一源 |
| **L3 接口合约** | Protocol Buffers | 1 个 canonical proto 源 | ✅ 已收敛（审计时为 2 个） |
| **L4 网络端口** | 服务端口和 URL | 50+ 位置 | 🟡 可优化为常量包 |
| **L5 身份定义** | Owner 身份常量 | 1 个权威源 | ✅ 完美实践 |
| **L6 Schema 版本** | 领域 schema 版本 | 30+ 位置 | ✅ 领域分离合理 |
| **L7 部署配置** | Compose overlay 模式 | 9 个 overlay | ✅ 标准模式 |

---

## 🟡 架构模式：已收敛 1 个，仍待改进 1 个

### 1. Protocol Buffers 曾有的双轨制（已收敛）

**当前事实:**
```
packages/contracts/proto/internal.proto ← 唯一可编辑协议源
packages/contracts/proto/events.json   ← 唯一可编辑事件源
```

目标规范 reader、Go bindings、public JSON 与 event identity 都直接读取上述
canonical 文件。原 `docs/spec/target/contracts/internal.proto` 与
`docs/spec/target/contracts/events.json` 是字节一致快照，现已删除；默认
`npm run verify:generated-contracts` 会拒绝任一 retired 路径被重新检入或由生成器重建。

**历史判定:** 2026-10-13 审计时两条路径确实并存，当时按“文档-实现双轨制”
处理。该历史结论不再描述当前 SSOT；不要把 `docs/spec/target/contracts/`
当作协议副本目录。

---

### 2. 端口号分散定义

**现状:** 端口 3000 在 6 个位置被定义：
```
services/control-plane/internal/server/app_state.go:23
services/fabric/internal/fabric/local_docker_runtime.go
services/fabric/internal/fabric/tencent_provider.go
tools/package-runtime-webui-intake.ts:1
apps/console-ui/src/app/cloud-webui-model.ts
services/control-plane/internal/server/workspace_default_application.go
```

**判定:** 值是一致的，但缺少集中定义。

**建议:** 创建 `packages/contracts/go/ports/ports.go` 统一管理端口常量。

---

## ✅ 5 个正确的 SSOT 实践

### 1. Owner 身份定义
**权威源:** `packages/contracts/go/owneridentity/owneridentity.go`
**消费方式:** 所有服务通过 import 使用，无重复定义
**评价:** 🏆 教科书级别的 SSOT 实践

### 2. TypeScript 类型系统
**权威源:** `apps/console-ui/src/api/dtos.ts` (1318 行)
**消费方式:** 所有前端组件导入使用
**评价:** ✅ 完美的集中类型定义

### 3. Docker Compose Overlay
**基础:** `compose.yaml`
**Overlay:** 8 个按需组合的 overlay 文件
**评价:** ✅ 标准的 Docker Compose 多环境模式

### 4. 环境变量分层
**开发:** `.env.example` (35 行, HTTP 模式)
**生产:** `deploy/portable/opl-cloud.env.example` (360 行, Owner Topology 模式)
**评价:** ✅ 设计性差异，符合开发/生产分离原则

### 5. Schema 版本管理
**模式:** 每个领域独立维护 `schemaVersion`
**位置:** development tools, receipts, qualification tools
**评价:** ✅ 符合领域驱动设计

---

## 📋 改进建议优先级

### 🟡 P1 - 澄清文档 (1-2 天)

1. **协议源已收敛（完成）**
   - `packages/contracts/proto/` 是唯一协议源，target reader 直接读取
   - 不再维护 `docs/spec/target/contracts/` 下的 proto/events 副本
   - freshness 由 `tools/verify-generated-contracts.ts` 的 retired-output 检查强制

2. **创建 `docs/PORT_REGISTRY.md`**
   - 记录所有服务端口
   - 端口分配规则
   - 预计工作量: 1 小时

3. **创建 `docs/ENVIRONMENT_VARIABLES.md`**
   - 完整的环境变量列表
   - 开发/生产使用矩阵
   - 预计工作量: 2 小时

### 🟢 P2 - 代码重构 (3-5 天)

4. **提取端口常量包**
   ```go
   // packages/contracts/go/ports/ports.go
   package ports
   const WorkspaceRuntimeWebUIPort = 3000
   ```
   - 替换 6 处硬编码
   - 预计工作量: 半天

5. **Proto 快照回归检查（完成）**
   - 默认 `verify:generated-contracts` 拒绝 retired target 快照重新出现
   - 不再需要 spec/实现双份文件的 mirror/sync 流程

6. **清理 output/ 目录**
   - 添加 .gitignore 规则
   - 预计工作量: 10 分钟

---

## 🔍 验证方法

按照 CONTRIBUTING.md 的 SSOT 表验证：

| 主题 | 权威文档 | 审计结果 |
|-----|---------|---------|
| 产品目标和权限边界 | `docs/architecture.md` | ✅ 单一源 |
| 当前实现边界 | `docs/implementation-architecture.md`, machine contracts | ✅ 清晰 |
| 开放的结果和优先级 | `docs/roadmap.md` | ✅ 单一源 |
| 持久化决策 | `docs/decisions.md` | ✅ 单一源 |
| **合约定义** | `packages/contracts/proto/` | ✅ 运行时权威 |
| **类型系统** | `packages/contracts/go/`, `apps/console-ui/src/api/dtos.ts` | ✅ 集中管理 |
| **配置管理** | `.env.example`, `deploy/portable/opl-cloud.env.example` | ✅ 分层合理 |

---

## 📈 对比分析

### 审计前的担忧
- ❓ 环境变量是否冲突？
- ❓ Proto 文件为何有两份？
- ❓ 端口定义是否一致？

### 审计后的结论
- ✅ 环境变量是设计性差异（开发 vs 生产）
- ✅ Proto/events 已收敛为 `packages/contracts/proto/` 单一源；审计时的双轨制只保留为历史
- ✅ 端口值一致，但可以集中定义

---

## 💡 关键洞察

1. **OPL Cloud 遵循严格的 owner 分离原则**
   - 每个领域有明确的数据和逻辑边界
   - 通过 `packages/contracts/` 共享合约，避免循环依赖

2. **协议契约采用 canonical source + target reader**
   - `docs/spec/target/` 是规划与验证 owner
   - `packages/contracts/proto/` 是 proto/events 的唯一可编辑源
   - target reader 与生成器直接读取 canonical 文件，不再维护字节一致副本

3. **配置分层清晰**
   - 开发环境简化配置（单体 HTTP 模式）
   - 生产环境完整配置（Owner Topology + gRPC）
   - 通过 Docker Compose overlay 组合

4. **TypeScript 和 Go 的类型管理是模范实践**
   - 前端：1318 行的集中 DTO 文件
   - 后端：通过 import 复用，无重复定义

---

## 📚 参考文档

- **详细报告:** `SSOT_CONFLICTS_DETAILED.md` (541 行)
- **贡献指南:** `CONTRIBUTING.md`
- **架构文档:** `docs/architecture.md`
- **实现架构:** `docs/implementation-architecture.md`
- **开发指南:** `DEV_GUIDE.md`

---

**审计完成** ✅
**总体评价:** OPL Cloud 的 SSOT 架构健康，主要需要补充文档说明现有的架构模式。
