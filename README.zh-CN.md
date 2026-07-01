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

OPL Cloud 把这些工作从本地工作台扩展到在线使用和团队协作：

- 用户通过一个稳定的 OPL 入口使用前沿 AI 能力。
- 用户打开在线 OPL App 工作空间，获得独立访问地址和资源套餐。
- 团队在一个控制台里管理账号、账单、权限、工作空间、连接器和资源策略。
- 远端任务在批准过的计算、存储、软件环境和外部系统上运行。
- 重要结果保留回执、来源、审查结果和继续入口。

**OPL Cloud 是支撑这些工作流的云端基础设施层。**

它用 OPL Workspace 承载用户工作台，用 OPL Console 承载管理面，用 OPL Gateway 承载 AI 能力入口，用 OPL Fabric 承载资源底座，用 OPL Ledger 承载证据记录。

## 产品总览

| 层级 | 品牌 | 定位 | 露出方式 |
| --- | --- | --- | --- |
| AI 接入 | **OPL Gateway** | 模型、密钥、路由、供应商策略和用量计量 | 产品 |
| 用户工作台 | **OPL Workspace** | 在线 OPL App、项目工作空间、任务会话、产物预览和结果交付 | 产品 |
| 管理端 | **OPL Console** | 组织、用户、账单、额度、工作空间生命周期和资源策略 | 产品 |
| 资源底座 | **OPL Fabric** | 计算、存储、软件环境、连接器和执行适配器 | 平台层 |
| 证据记录 | **OPL Ledger** | 任务回执、产物来源、审查入口、审计记录和继续引用 | 平台层 |

## 核心亮点

**OPL Gateway 保持顶层产品地位**<br/>
Gateway 在技术上属于资源接入能力，但它已经是直接面向用户的 AI 入口、计量入口和计费入口。它适合作为 OPL Cloud 的顶层产品继续露出。

**OPL Workspace 是用户工作面**<br/>
Workspace 负责打开项目、发起任务、查看运行状态、预览产物、接收审查反馈和收取最终交付物。

**OPL Console 是管理面**<br/>
Console 负责谁能使用、能访问什么、能花多少钱、能启动哪些工作空间、哪些连接器被批准、哪些软件环境可用。

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
├─ OPL Environments   软件栈、依赖锁定、容器镜像、可复现运行清单
└─ Storage Vault      工作空间卷、私有存储桶、机构存储引用
```

`OPL Environments` 是 Compute 和 Fabric 的子能力。它需要作为版本化环境目录维护，但第一版不必作为单独前台产品露出。

## OPL Ledger

OPL Ledger 是远端工作和结果交付的证据层。

每个重要的 Workspace 操作或云端任务都应留下回执：

```text
计划 → 批准 → 执行 → 产物 → 审查 → 回执 → 继续入口
```

Ledger 本身不是文件仓库。它记录发生了什么、使用了哪些输入和环境、产生了哪些输出、经过哪些检查，以及后续如何复查或继续。

## 最小可行版本

第一版可以保持很小：

1. Gateway：AI 接入、密钥管理、模型路由、用量和账单数据。
2. Workspace：一个在线 OPL App 实例，带访问地址、账号、存储目录和基础套餐。
3. Console：用户、套餐、Gateway 用量和 Workspace 生命周期管理。
4. Fabric v0：一条 Docker 或虚拟机计算路径，加一个卷或对象存储路径。
5. Ledger v0：为 Workspace 操作和任务生成回执 JSON。

高性能计算、GPU 工作节点、文献数据库、机构存储和软件环境目录，都可以在第一个真实 Workspace 任务链路跑通后，以 Fabric 适配器的方式逐步加入。

## 文档

- [产品矩阵](docs/product-matrix.md)
- [架构](docs/architecture.md)
- [OPL Gateway](docs/opl-gateway.md)
- [OPL Workspace](docs/opl-workspace.md)
- [OPL Console](docs/opl-console.md)
- [OPL Fabric](docs/opl-fabric.md)
- [OPL Ledger](docs/opl-ledger.md)
- [Research Provenance](docs/research-provenance.md)
- [Roadmap](docs/roadmap.md)

## 技术入口

<details>
  <summary><strong>展开开发者与运维说明</strong></summary>

本仓库当前承载产品与架构文档。Gateway 服务、Console 实现、Workspace 运行环境、Fabric 适配器、Ledger 存储和账单系统由对应负责人界面承载。

涉及可用性、发布、账单、运行状态、安全边界或复现能力的结论，应回到对应服务、仓库、合同、运行读回或负责人回执。本仓库负责解释公开产品边界。

### 仓库结构

```text
one-person-lab-cloud/
  assets/              README 和产品视觉资产
  docs/                Cloud 产品、架构和路线图文档
  README.md            英文公开入口
  README.zh-CN.md      中文公开入口
```

</details>

