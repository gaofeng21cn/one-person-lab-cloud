<p align="center">
  <img src="assets/branding/opl-cloud-logo.png" alt="OPL Cloud 标志" width="132" />
</p>

<p align="center">
  <a href="./README.md">English</a> | <a href="./README.zh-CN.md"><strong>中文</strong></a>
</p>

<h1 align="center">OPL Cloud</h1>

<p align="center"><strong>One Person Lab 的云端前沿 AI 基础设施</strong></p>
<p align="center">AI 能力入口 · 在线工作空间 · 智能体服务发布 · 云端控制台 · 资源底座 · 证据账本</p>

<!--
Owner: `one-person-lab-cloud`
Purpose: `public_cloud_entry`
State: `active_public_entry`
Machine boundary: 人读产品与架构入口。App、Framework、Gateway 服务、Workspace 运行环境、账单、任务、回执和发布状态的机器真相，仍以对应仓库、服务、合同、运行输出和负责人回执为准。
-->

<p align="center">
  <img src="assets/branding/opl-cloud-overview-v2.png" alt="OPL Cloud 从本地工作到在线协作和按需发布的连续用户旅程" width="100%" />
</p>

## 为什么需要 OPL Cloud

One Person Lab 面向复杂知识工作：科研、基金、汇报、书籍、智能体和其他需要多轮推进、审查、修订、文件与证据的项目。

OPL Cloud 把这些工作从本地工作台扩展到在线使用、协作和远端资源调用：

- 用户通过一个稳定的 OPL 入口使用前沿 AI 能力。
- 用户既可以使用本机 OPL App，也可以使用云端 OPL Workspace，工作台模型保持一致。
- 本机 App 和云端 Workspace 都可以调用统一的文献、数据、工具、计算和证据能力。
- 智能体开发者可以把一个经过验证的 Agent 精确版本发布为 API、嵌入组件或 OPL 托管界面，不必另建服务栈。
- 一个用户账号只对应一个主 OPL Workspace；Console 管理该账号的账单、权限、额度、批准和纳管资源策略。
- 远端任务在批准过的计算、存储、软件环境和外部系统上运行。
- 重要结果保留回执、来源、审查结果和继续入口。

**OPL Cloud 是支撑这些工作流的云端基础设施层。**

## 公开角色边界

本仓库是 OPL Cloud **目标产品架构与实现族**的公开导航面，覆盖 OPL Gateway、
OPL Workspace、OPL Serve、OPL Console、OPL Fabric 和 OPL Ledger。它负责说明
这组产品与平台能力为什么这样设计、如何协作，以及它们怎样与 OPL App、OPL
Framework、MAS 和领域系统保持清楚边界；它不表示文档中的每项服务均已交付。

Workspace 身份规则固定为 `1 个用户账号 -> 1 个主 OPL Workspace`。协作只共享
引用、产物、获批资源和策略，不建立另一套多租户 Web 产品。详见
[Workspace 身份与外部 SaaS 边界](docs/workspace-identity-and-external-saas-boundary.md)。

OPL Cloud 不是 OPL Framework 的替代品。任务执行、App 行为、服务状态、
Gateway API、Workspace 运行环境、Console 治理、Fabric 资源、Ledger 回执、
账单、发布状态和 owner acceptance 的 runtime truth，仍归对应的 Framework、
App、服务实现、合同、运行输出和负责人回执。

对外看，OPL Cloud 的用户可见产品是 OPL Gateway、OPL Workspace、OPL Serve 和
OPL Console；对内看，它提供 OPL Fabric 与 OPL Ledger 两类可复用平台能力。本机
OPL App 是本地工作面，OPL Workspace 是云端工作面，OPL Serve 把一个精确 Agent
版本发布给外部使用者。OPL Console 只负责账号权限、计费、资源策略和批准；Agent
与能力包的安装、版本、lock、更新和修复统一归 OPL Packages，各个 Cloud 表面不
维护第二份 package registry truth。

## 产品总览

| 层级 | 品牌 | 定位 | 露出方式 |
| --- | --- | --- | --- |
| AI 接入 | **OPL Gateway** | 模型、密钥、路由、供应商策略和用量计量 | 产品 |
| 用户工作台 | **OPL Workspace / OPL App 集成** | 云端工作台与本机 OPL App 使用同一套项目工作、任务会话、产物预览和结果交付模型 | 产品 / 本地入口 |
| 智能体服务 | **OPL Serve** | 把精确 Agent 版本发布为 API、嵌入组件或托管 UI | 产品 |
| 管理端 | **OPL Console** | 用户账号、账单、额度、单 Workspace 生命周期和纳管资源策略 | 产品 |
| 资源底座 | **OPL Fabric** | Connect、Compute、Storage、Environments、Gateway/App/Workspace 适配器、连接器和执行适配器 | 平台层 |
| 证据记录 | **OPL Ledger** | 任务回执、产物来源、审查入口、审计记录和继续引用 | 平台层 |

## 模块边界

| 模块 | 定位 | 直接使用方 |
| --- | --- | --- |
| OPL App | 本机工作台，也是 Cloud 能力的一等调用入口 | 用户、MAS、领域智能体 |
| OPL Workspace | 云端工作台，与 OPL App 使用同一套能力模型 | 用户、MAS、领域智能体 |
| OPL Serve | 面向外部 API、嵌入组件、托管 UI、版本、部署和流量的智能体发布产品 | Agent 发布者、外部 App 与网站 |
| OPL Fabric | 通用资源底座，承载连接器、计算、存储、环境和执行适配器 | App、Workspace、Console、MAS、领域智能体 |
| OPL Connect | Fabric 内的连接能力，负责稳定连接、API、规范化 source refs、错误语义和限流语义 | App、Workspace、MAS、领域智能体；需要治理时进入 Console |
| OPL Console | 账号及纳管资源的治理面，负责凭据、额度、审批、账单、审计和生命周期 | 用户、管理员、运维者 |
| OPL Ledger | request、source refs、outputs、review refs 和 continuation entries 的 receipt/provenance 记录层 | App、Workspace、Console、MAS、领域智能体 |
| MAS / ScholarSkills | 领域策略、检索意图、质量判断、证据综合、写作与审阅行为 | MAS 工作流和领域用户 |

Console 治理 OPL Cloud 托管或显式纳管的资源，但不是唯一调用入口。本机
OPL App、云端 OPL Workspace、由 Runway 执行的 Serve 调用、MAS 和获批领域智能体
可以按 capability profile 调用 Fabric 与 Connect。Ledger 记录这些调用的 receipt
与 provenance，不接管连接器实现，也不接管领域判断。

## 核心亮点

**OPL Gateway 保持顶层产品地位**<br/>
Gateway 是直接面向用户的 AI 入口、计量入口和计费入口，也是 OPL Cloud 最先可用的能力基座。

**OPL App 和 OPL Workspace 是等价的用户工作面**<br/>
OPL App 是本机工作台，OPL Workspace 是云端工作台。用户从任一工作面打开项目、发起任务、查看运行状态、预览产物、接收审查反馈和收取最终交付物。

**OPL Serve 把 Agent 精确版本变成对外服务**<br/>
Agent 开发者把精确 OPL Package digest 发布成稳定 Agent Service、不可变 Revision 和受控 Deployment。外部使用者可以调用 API、嵌入组件或可选的托管 UI。公网 Agent Edge 与 Workspace、sandbox 和 provider endpoint 分离，所有客户端使用同一套服务合同。

**OPL Console 管理被纳管的资源**<br/>
Console 是账号与纳管资源治理台，负责账单、权限、用户唯一 Workspace 生命周期、连接器审批、环境策略和资源额度。用户自己提供的本机、SSH、高性能计算、文献连接或数据库，也可以使用标准 Fabric 流程；当这些资源进入显式共享策略时，再进入 Console 管理。Console 只投影策略允许使用的 OPL Package 版本，不持有安装、lock 或修复事实。

**OPL Fabric 负责资源底座**<br/>
Fabric 承载 Connect、Compute、Storage、Environments、Gateway/App/Workspace 适配器和执行适配器。OPL Connect 是 Fabric 中的连接能力，负责把文献库、数据库、工具库、机构系统和外部资源稳定接入 OPL 工作流。普通用户看到的是“文献检索”“标准计算”“GPU 加速”“私有数据桶”“机构高性能计算”等产品化选项；运行需要的 Agent 与能力包只引用 OPL Packages 的当前 manifest 与 lock。

**OPL Ledger 负责可信记录**<br/>
Ledger 记录计划、批准、命令或代码、软件环境、输入引用、输出引用、审查结果、负责人和继续入口，让远端工作具备审查、复现和接力基础。

## OPL Fabric

OPL Fabric 是 OPL Cloud 背后的资源与连接底座。

```text
OPL Fabric
├─ OPL Connect        数据库、文献源、工具库、接口和外部资源
├─ OPL Compute        Docker、虚拟机、GPU、SSH、高性能计算、托管工作节点
├─ OPL Environments   可复现的软件环境和运行模板
├─ Gateway adapters   AI 能力接入配置、用量信号、供应商策略引用
├─ Package bindings    OPL Packages manifest/lock 的运行绑定
└─ Storage Vault      工作空间卷、私有存储桶、机构存储引用
```

这些能力共同保证 OPL App 和 OPL Workspace 都能连接资料、使用工具、获得计算资源，并在合适的软件环境中运行任务。

OPL Connect 为共享数据源和外部工具提供稳定访问、API 形态、规范化 source refs、凭据边界、错误、重试和限流语义。具体检索策略、证据质量和写作审阅仍由 MAS 或其他领域 owner 持有；医学专用 PubMed 访问与 normalization 归 MAS adapter，不在 Cloud 层建立兼容入口。任务需要回执时，Ledger 只记录连接器引用和 provenance。

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

每个重要的 App 操作、Workspace 操作、Serve 部署或调用，以及云端纳管任务都可以留下回执：

```text
计划 → 批准 → 执行 → 产物 → 审查 → 回执 → 继续入口
```

Ledger 记录发生了什么、使用了哪些输入和环境、产生了哪些输出、经过哪些检查，以及后续如何复查或继续。Ledger 记录回执和来源，不替代 MAS、MAG、RCA、BookForge 或其他领域 owner 的事实、质量判断和交付权威。

## 核心交付链路

OPL Cloud 的核心交付链路由六个部分组成，并允许把 Agent 继续发布给外部使用者：

1. Gateway：AI 接入、密钥管理、模型路由、用量和账单数据。
2. Workspace：每个用户账号唯一的在线 OPL App 工作台，带访问地址、存储目录和基础套餐。
3. Serve：把精确 Agent Revision 发布为 API、嵌入组件或托管 UI。
4. Console：用户、套餐、Gateway/Serve 用量以及 Workspace/服务治理。
5. Fabric：计算路径、存储路径、软件环境和连接器能力。
6. Ledger：为 App 操作、Workspace 操作、Serve 部署与调用和纳管任务生成可检查的回执。

高性能计算、GPU 工作节点、文献数据库、机构存储和软件环境目录，都可以沿同一套 Fabric 流程进入 OPL Cloud。

## 文档

- [在线阅读 OPL Cloud 白皮书](https://gaofeng21cn.github.io/one-person-lab-cloud/latest/whitepapers/opl-cloud-whitepaper.html)
- [OPL Cloud 白皮书 PDF](https://gaofeng21cn.github.io/one-person-lab-cloud/latest/whitepapers/opl-cloud-whitepaper.pdf)
- [白皮书 Markdown 源文](docs/whitepapers/opl-cloud-whitepaper.md)
- [产品矩阵](docs/product-matrix.md)
- [架构](docs/architecture.md)
- [Workspace 身份与外部 SaaS 边界](docs/workspace-identity-and-external-saas-boundary.md)
- [平台能力缺口](docs/platform-capability-gaps.md)
- [OPL Gateway](docs/opl-gateway.md)
- [OPL Workspace](docs/opl-workspace.md)
- [OPL Serve](docs/opl-serve.md)
- [OPL Console](docs/opl-console.md)
- [OPL Fabric](docs/opl-fabric.md)
- [OPL Connect](docs/opl-connect.md)
- [OPL Ledger](docs/opl-ledger.md)
- [OPL Agent Lifecycle](docs/agent-lifecycle.md)
- [Research Provenance](docs/research-provenance.md)
- [共享执行合同](docs/contracts/shared-execution-contract.md)
- [Agent Service 发布合同](docs/contracts/agent-service-publication-contract.md)
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

本仓库当前承载产品定位、规划、品牌与架构文档。Gateway 服务、Serve 控制面与 Agent Edge、Console 实现、Workspace 运行环境、Runway 执行、Fabric 适配器、Ledger 存储和账单系统由对应实现面承载。

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
