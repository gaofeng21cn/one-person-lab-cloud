<p align="center">
  <img src="assets/branding/opl-cloud-logo.png" alt="OPL Cloud 标志" width="132" />
</p>

<p align="center">
  <a href="./README.md">English</a> | <a href="./README.zh-CN.md"><strong>中文</strong></a>
</p>

<h1 align="center">OPL Cloud</h1>

<p align="center"><strong>One Person Lab 的云端前沿 AI 基础设施</strong></p>
<p align="center">AI 能力入口 · 在线工作空间 · 云端控制台 · 资源底座 · 证据账本</p>

<!--
Owner: `one-person-lab-cloud`
Purpose: `public_cloud_entry`
State: `active_public_entry`
Machine boundary: 人读产品与架构入口。App、Framework、Gateway 服务、Workspace 运行环境、账单、任务、回执和发布状态的机器真相，仍以对应仓库、服务、合同、运行输出和负责人回执为准。
-->

<p align="center">
  <img src="assets/branding/opl-cloud-overview.png" alt="OPL Cloud 产品关系图" width="100%" />
</p>

## 为什么需要 OPL Cloud

One Person Lab 面向复杂知识工作：科研、基金、汇报、书籍、智能体和其他需要多轮推进、审查、修订、文件与证据的项目。

OPL Cloud 把这些工作从本地工作台扩展到在线使用、团队协作和远端资源调用：

- 用户通过一个稳定的 OPL 入口使用前沿 AI 能力。
- 用户既可以使用本机 OPL App，也可以使用云端 OPL Workspace，工作台模型保持一致。
- 本机 App 和云端 Workspace 都可以调用统一的文献、数据、工具、计算和证据能力。
- 团队在一个控制台里管理账号、账单、权限、工作空间和资源策略。
- 远端任务在批准过的计算、存储、软件环境和外部系统上运行。
- 重要结果保留回执、来源、审查结果和继续入口。

**OPL Cloud 是支撑这些工作流的云端基础设施层。**

## 公开角色边界

本仓库是 OPL Cloud 产品实现族的公开导航面，覆盖 OPL Gateway、OPL Workspace、
OPL Console、OPL Fabric 和 OPL Ledger。它负责说明这组产品与平台能力的边界，
以及它们如何与 OPL App、OPL Framework、MAS 和领域系统协作。

OPL Cloud 不是 OPL Framework 的替代品。任务执行、App 行为、服务状态、
Gateway API、Workspace 运行环境、Console 治理、Fabric 资源、Ledger 回执、
账单、发布状态和 owner acceptance 的 runtime truth，仍归对应的 Framework、
App、服务实现、合同、运行输出和负责人回执。

对外看，OPL Cloud 的用户可见产品是 OPL Gateway、OPL Workspace 和 OPL Console；对内看，它提供 OPL Fabric 与 OPL Ledger 两类可复用平台能力。本机 OPL App 是本地工作面，OPL Workspace 是云端 Docker/WebUI 工作面，两者都可以直接调用 OPL Gateway、OPL Fabric 和 OPL Ledger。OPL Console 负责组织、权限、计费、资源策略和生命周期管理，但不是所有 Fabric 能力的唯一入口。

## 产品总览

| 层级 | 品牌 | 定位 | 露出方式 |
| --- | --- | --- | --- |
| AI 接入 | **OPL Gateway** | 模型、密钥、路由、供应商策略和用量计量 | 产品 |
| 用户工作台 | **OPL Workspace / OPL App 集成** | 云端 Docker/WebUI OPL App 与本机 OPL App 使用同一套项目工作、任务会话、产物预览和结果交付模型 | 产品 / 本地入口 |
| 管理端 | **OPL Console** | 组织、用户、账单、额度、工作空间生命周期和资源策略 | 产品 |
| 资源底座 | **OPL Fabric** | Connect、Compute、Storage、Environments、Gateway/App/Workspace 适配器、连接器和执行适配器 | 平台层 |
| 证据记录 | **OPL Ledger** | 任务回执、产物来源、审查入口、审计记录和继续引用 | 平台层 |

## 模块边界

| 模块 | 定位 | 直接使用方 |
| --- | --- | --- |
| OPL App | 本机工作台，也是 Cloud 能力的一等调用入口 | 用户、MAS、领域智能体 |
| OPL Workspace | 云端 Docker/WebUI 工作台，与 OPL App 使用同一套能力模型 | 用户、MAS、领域智能体 |
| OPL Fabric | 通用资源底座，承载连接器、计算、存储、环境、智能体包和执行适配器 | App、Workspace、Console、MAS、领域智能体 |
| OPL Connect | Fabric 内的连接能力，负责稳定连接、API、规范化 source refs、错误语义和限流语义 | App、Workspace、MAS、领域智能体；需要治理时进入 Console |
| OPL Console | 组织纳管资源的治理面，负责凭据、额度、审批、账单、审计和生命周期 | 管理员、运维者 |
| OPL Ledger | request、source refs、outputs、review refs 和 continuation entries 的 receipt/provenance 记录层 | App、Workspace、Console、MAS、领域智能体 |
| MAS / ScholarSkills | 领域策略、检索意图、质量判断、证据综合、写作与审阅行为 | MAS 工作流和领域用户 |

Console 治理 OPL Cloud 托管或组织纳管的资源，但不是唯一调用入口。本机
OPL App、云端 OPL Workspace、MAS 和获批领域智能体可以按 capability profile
直接调用 Fabric 与 Connect。Ledger 记录这些调用的 receipt 与 provenance，
不接管连接器实现，也不接管领域判断。

## 核心亮点

**OPL Gateway 保持顶层产品地位**<br/>
Gateway 是直接面向用户的 AI 入口、计量入口和计费入口，也是 OPL Cloud 最先可用的能力基座。

**OPL App 和 OPL Workspace 是等价的用户工作面**<br/>
OPL App 是本机工作台，OPL Workspace 是云端 Docker/WebUI 工作台。用户从任一工作面打开项目、发起任务、查看运行状态、预览产物、接收审查反馈和收取最终交付物。

**OPL Console 管理被纳管的资源**<br/>
Console 是组织治理台，负责 OPL Cloud 托管或组织纳管的账号、账单、权限、工作空间生命周期、连接器审批、环境策略和资源额度。用户自己提供的本机、SSH、高性能计算、文献连接、数据库或大型 skill pack，也可以使用标准 Fabric 流程；当这些资源被组织纳入统一策略时，再进入 Console 管理。

**OPL Fabric 负责资源底座**<br/>
Fabric 承载 Connect、Compute、Storage、Environments、Gateway/App/Workspace 适配器、智能体包注册和执行适配器。OPL Connect 是 Fabric 中的连接能力，负责把 PubMed 等文献库、数据库、工具库、机构系统、外部资源和大型 skill pack 稳定接入 OPL 工作流。普通用户看到的是“文献检索”“标准计算”“GPU 加速”“私有数据桶”“机构高性能计算”“团队 skill pack”等产品化选项。

**OPL Ledger 负责可信记录**<br/>
Ledger 记录计划、批准、命令或代码、软件环境、输入引用、输出引用、审查结果、负责人和继续入口，让远端工作具备审查、复现和接力基础。

## OPL Fabric

OPL Fabric 是 OPL Cloud 背后的资源与连接底座。

```text
OPL Fabric
├─ OPL Connect        数据库、文献源、工具库、接口、资源、大型 skill pack
├─ OPL Compute        Docker、虚拟机、GPU、SSH、高性能计算、托管工作节点
├─ OPL Environments   可复现的软件环境和运行模板
├─ Gateway adapters   AI 能力接入配置、用量信号、供应商策略引用
├─ OPL Agent Registry 已批准的智能体包、版本和资源要求
└─ Storage Vault      工作空间卷、私有存储桶、机构存储引用
```

这些能力共同保证 OPL App 和 OPL Workspace 都能连接资料、使用工具、获得计算资源，并在合适的软件环境中运行任务。

PubMed 只读连接器（PubMed read-only connector）是 OPL Connect 的第一条稳定路径。MAS 或 ScholarSkills 可以先验证检索策略、排序、质量下限、证据使用、写作与审阅行为；当连接能力成为共享平台行为后，OPL Connect 承担稳定访问、API 形态、规范化 source refs、凭据边界、错误、重试和限流语义。MAS、OPL App 和 OPL Workspace 通过 capability profile 调用同一个连接器；任务需要回执时，Ledger 记录连接器引用和 provenance。

## Skill-first 协同

OPL Cloud 支持 skill-first 的能力演进路径：MAS、ScholarSkills 和其他领域 owner 维护领域策略、质量判断、写作与审阅行为；OPL Connect 负责稳定连接、API 行为、规范化、source refs、错误和限流语义；OPL Ledger 记录 receipt 与 provenance。

```text
领域 skill 原型
→ 稳定连接行为
→ OPL Fabric 内的 OPL Connect
→ MAS / Workspace / App capability profile 调用
→ normalized refs 进入 MAS evidence workflow
→ 可选 OPL Ledger receipt refs
```

这个路径让 MAS 等领域系统继续持有领域真相，也让成熟连接能力进入 OPL Cloud 的通用平台层。

## OPL Ledger

OPL Ledger 是远端工作和结果交付的证据层。

每个重要的 App 操作、Workspace 操作或云端纳管任务都可以留下回执：

```text
计划 → 批准 → 执行 → 产物 → 审查 → 回执 → 继续入口
```

Ledger 记录发生了什么、使用了哪些输入和环境、产生了哪些输出、经过哪些检查，以及后续如何复查或继续。Ledger 记录回执和来源，不替代 MAS、MAG、RCA、BookForge 或其他领域 owner 的事实、质量判断和交付权威。

## 核心交付链路

OPL Cloud 的核心交付链路由五个部分组成：

1. Gateway：AI 接入、密钥管理、模型路由、用量和账单数据。
2. Workspace：在线 OPL App 实例，带访问地址、账号、存储目录和基础套餐。
3. Console：用户、套餐、Gateway 用量和 Workspace 生命周期管理。
4. Fabric：计算路径、存储路径、软件环境和连接器能力。
5. Ledger：为 App 操作、Workspace 操作和纳管任务生成可检查的回执。

高性能计算、GPU 工作节点、文献数据库、机构存储和软件环境目录，都可以沿同一套 Fabric 流程进入 OPL Cloud。

## 文档

- [OPL Cloud 白皮书 Markdown](docs/public/whitepaper/opl-cloud-whitepaper.md)
- [OPL Cloud 白皮书 PDF](docs/public/whitepaper/opl-cloud-whitepaper.pdf)
- [产品矩阵](docs/product-matrix.md)
- [架构](docs/architecture.md)
- [平台能力缺口](docs/platform-capability-gaps.md)
- [OPL Gateway](docs/opl-gateway.md)
- [OPL Workspace](docs/opl-workspace.md)
- [OPL Console](docs/opl-console.md)
- [OPL Fabric](docs/opl-fabric.md)
- [OPL Connect](docs/opl-connect.md)
- [OPL Ledger](docs/opl-ledger.md)
- [OPL Agent Lifecycle](docs/agent-lifecycle.md)
- [Research Provenance](docs/research-provenance.md)
- [共享执行合同](docs/contracts/shared-execution-contract.md)
- [账本回执结构](docs/contracts/ledger-receipt-schema.md)
- [智能体注册条目](docs/contracts/agent-registry-entry.md)
- [资源归属与计费](docs/contracts/resource-ownership-and-billing.md)
- [Workspace 生命周期](docs/workspace-lifecycle.md)
- [Console 治理与计费](docs/console-governance-and-billing.md)
- [Fabric 适配器合同](docs/fabric-adapter-contract.md)
- [Roadmap](docs/roadmap.md)

## 技术入口

<details>
  <summary><strong>展开开发者与运维说明</strong></summary>

本仓库当前承载产品与架构文档。Gateway 服务、Console 实现、Workspace 运行环境、Fabric 适配器、Ledger 存储和账单系统由对应实现面承载。

涉及可用性、发布、账单、运行状态、安全边界或复现能力的结论，以对应服务、仓库、合同、运行读回和负责人回执为准。

### 仓库结构

```text
one-person-lab-cloud/
  assets/              README 和产品视觉资产
  docs/                Cloud 产品、架构和路线图文档
  README.md            英文公开入口
  README.zh-CN.md      中文公开入口
```

</details>
