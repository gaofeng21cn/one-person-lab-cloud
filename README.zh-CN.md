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
- 团队在一个控制台里管理账号、账单、权限、工作空间、连接器和资源策略。
- 远端任务在批准过的计算、存储、软件环境和外部系统上运行。
- 重要结果保留回执、来源、审查结果和继续入口。

**OPL Cloud 是支撑这些工作流的云端基础设施层。**

它把 OPL App 和 OPL Workspace 视为等价的用户工作面：OPL App 是本机工作台，OPL Workspace 是云端 Docker/WebUI 工作台。两者都可以使用 OPL Gateway、OPL Fabric 和 OPL Ledger 能力。OPL Console 负责纳入 OPL Cloud 托管或组织管理的资源、权限、生命周期和计费。

## 产品总览

| 层级 | 品牌 | 定位 | 露出方式 |
| --- | --- | --- | --- |
| AI 接入 | **OPL Gateway** | 模型、密钥、路由、供应商策略和用量计量 | 产品 |
| 用户工作台 | **OPL App / OPL Workspace** | 本机 OPL App 与云端 Docker/WebUI OPL App，共同承担项目工作、任务会话、产物预览和结果交付 | 产品 |
| 管理端 | **OPL Console** | 组织、用户、账单、额度、工作空间生命周期和资源策略 | 产品 |
| 资源底座 | **OPL Fabric** | 计算、存储、软件环境、连接器和执行适配器 | 平台层 |
| 证据记录 | **OPL Ledger** | 任务回执、产物来源、审查入口、审计记录和继续引用 | 平台层 |

## 核心亮点

**OPL Gateway 保持顶层产品地位**<br/>
Gateway 是直接面向用户的 AI 入口、计量入口和计费入口，也是 OPL Cloud 最先可用的能力基座。

**OPL App 和 OPL Workspace 是等价的用户工作面**<br/>
OPL App 是本机工作台，OPL Workspace 是云端 Docker/WebUI 工作台。用户从任一工作面打开项目、发起任务、查看运行状态、预览产物、接收审查反馈和收取最终交付物。

**OPL Console 管理被纳管的资源**<br/>
Console 负责 OPL Cloud 托管或组织纳管的账号、账单、权限、工作空间生命周期、连接器审批、环境策略和资源额度。用户自己提供的本机、SSH 或高性能计算资源，也可以使用标准 Fabric 流程；当这些资源被组织纳入统一策略时，再进入 Console 管理。

**OPL Fabric 负责资源底座**<br/>
Fabric 承载计算池、存储库、环境目录、连接器注册表和执行适配器。普通用户看到的是“标准计算”“GPU 加速”“私有数据桶”“机构高性能计算”等产品化选项。

**OPL Ledger 负责可信记录**<br/>
Ledger 记录计划、批准、命令或代码、软件环境、输入引用、输出引用、审查结果、负责人和继续入口，让远端工作具备审查、复现和接力基础。

## OPL Fabric

OPL Fabric 是 OPL Cloud 背后的资源与连接底座。

```text
OPL Fabric
├─ OPL Connect        数据库、文献源、工具、接口、机构内部系统
├─ OPL Compute        Docker、虚拟机、GPU、SSH、高性能计算、托管工作节点
├─ OPL Environments   可复现的软件环境和运行模板
├─ OPL Agent Registry 已批准的智能体包、版本和资源要求
└─ Storage Vault      工作空间卷、私有存储桶、机构存储引用
```

这些能力共同保证 OPL App 和 OPL Workspace 都能连接资料、使用工具、获得计算资源，并在合适的软件环境中运行任务。

## OPL Ledger

OPL Ledger 是远端工作和结果交付的证据层。

每个重要的 App 操作、Workspace 操作或云端纳管任务都应留下回执：

```text
计划 → 批准 → 执行 → 产物 → 审查 → 回执 → 继续入口
```

Ledger 记录发生了什么、使用了哪些输入和环境、产生了哪些输出、经过哪些检查，以及后续如何复查或继续。

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
