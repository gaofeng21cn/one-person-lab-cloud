<p align="center">
  <a href="./README.md">English</a> | <a href="./README.zh-CN.md"><strong>中文</strong></a>
</p>

<h1 align="center">OPL Cloud</h1>

<p align="center"><strong>One Person Lab 的云端前沿 AI 基础设施</strong></p>
<p align="center">前沿 AI 接入 · 在线工作空间 · 受控计算 · 用量计量 · 可复现证据链</p>

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

本地优先仍然是 OPL 的重要基础。云端能力负责把这些工作扩展到在线使用、团队协作和受控计算场景：

- 用户通过一个稳定的 OPL 入口使用前沿 AI 能力。
- 用户可以启动在线 OPL App 工作空间，直接获得独立访问地址、账号、密码、计算配置和存储配置。
- 长时间运行的计算任务形成可检查、可追踪、可交接的回执。
- 团队在一个控制台里管理用量、账单、权限和工作空间生命周期。
- 科研与智能体产物保留清楚的来源、过程和回执，方便审查、复现和后续继续。

**OPL Cloud 是为这些问题设计的云端基础设施层。**

它把远端控制面、AI 能力入口、在线工作空间、受控计算路径和证据链能力组织起来，让 OPL 从单机工作台自然扩展到在线和团队工作流。One Person Lab App 继续作为用户工作台，OPL Cloud 提供它背后的云端能力基座。

## 核心亮点

**OPL Gateway 是 AI 能力基座**<br/>
Gateway 是第一个已可用的 Cloud 组件，提供前沿 AI 统一接入、密钥管理、模型路由、用量可视化和 OPL 下游工作流支持。

**在线 OPL 工作空间**<br/>
OPL Workspace 是规划中的云端工作空间产品。每个工作空间提供独立访问地址、账号、密码、计算配置、存储配置和打包计费。

**面向用户的云端控制台**<br/>
OPL Console 管理账号、组织、账单、权限、Gateway 用量和 Workspace 生命周期。Docker 主机、计算节点和存储位置作为基础设施细节，由平台在后台管理。

**带回执的受控计算**<br/>
远端任务遵循清晰路径：计划、批准、执行、回收产物、留下回执。本地 Docker、远端虚拟机、GPU 工作节点、SSH 和高性能计算执行可以逐步统一到同一套任务合同。

**面向科研和智能体工作流的证据链**<br/>
云端证据链保留输入、代码、命令、环境、负责人、审查结果和继续入口的引用，让关键产物具备审查、复现和接力基础。

**面向专业场景的精选能力包**<br/>
OPL Cloud 支持团队批准过的 MAS、MAG、RCA、BookForge 和未来 Foundry Agent 能力包。用户看到的是专业工作入口，平台在背后管理技能、连接器和运行环境。

## 产品矩阵

| 产品 | 定位 | 状态 |
| --- | --- | --- |
| **OPL Gateway** | 前沿 AI 能力入口，负责接口接入、密钥管理、模型路由与用量计量 | 已可用 |
| **OPL Console** | 云端管理控制台，负责账号、组织、账单、权限、工作空间与运维管理 | 开发中 |
| **OPL Workspace** | 在线 OPL App 工作空间，提供独立访问地址、凭证、计算、存储与计费包 | 开发中 |
| **Evidence Services** | 证据链存储、审查入口、产物回执与策略检查 | 规划中 |

## 产品边界

OPL Cloud 是产品总品牌和架构入口，负责把云端能力、产品关系和路线图公开呈现。各产品和仓库继续持有自己的真实状态与运行证据。

| 仓库或产品 | 负责 |
| --- | --- |
| [`one-person-lab`](https://github.com/gaofeng21cn/one-person-lab) | OPL Framework、运行时合同、命令行、阶段执行、进度和证据接口 |
| [`one-person-lab-app`](https://github.com/gaofeng21cn/one-person-lab-app) | 桌面 App、Docker/WebUI 用户体验、打包、发布资产、界面合同 |
| **OPL Gateway** | AI 能力接入、密钥管理、模型路由、用量计量和接入材料 |
| **OPL Console** | 云端账号、组织、账单、工作空间、权限和运维管理 |
| **OPL Workspace** | 在线 OPL App 运行实例与工作空间生命周期 |

在独立 OPL Gateway 公开仓库出现前，Gateway 的公开脚本和接入材料可以作为接入资产被引用。本仓库把 OPL Cloud 的公开产品叙事、架构和路线图放在一起。

## 当前状态

- OPL Gateway 已可用，定位为 OPL Cloud 的 AI 能力基座。
- OPL Console 和 OPL Workspace 正在开发中。
- 研究证据链、审查入口、任务适配器和团队能力包属于规划中的 Cloud 能力。
- 敏感数据默认留在用户工作空间、机构存储或私有存储桶；Cloud 默认保存引用、元数据、过程记录、回执、用量和策略记录。

## 文档

- [产品矩阵](docs/product-matrix.md)
- [架构](docs/architecture.md)
- [OPL Gateway](docs/opl-gateway.md)
- [OPL Console](docs/opl-console.md)
- [OPL Workspace](docs/opl-workspace.md)
- [Research Provenance](docs/research-provenance.md)
- [Roadmap](docs/roadmap.md)

## 技术入口

<details>
  <summary><strong>展开开发者与运维说明</strong></summary>

本仓库当前承载产品与架构文档。Gateway 服务、Console 实现、Workspace 运行环境和账单系统由对应负责人界面承载。

涉及可用性、发布、账单、运行状态或安全边界的结论，应回到对应服务、仓库、合同、运行读回或负责人回执。本仓库负责解释公开产品边界。

### 仓库结构

```text
one-person-lab-cloud/
  assets/              README 和产品视觉资产
  docs/                Cloud 产品、架构和路线图文档
  README.md            英文公开入口
  README.zh-CN.md      中文公开入口
```

</details>
