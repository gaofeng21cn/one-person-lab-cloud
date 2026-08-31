<p align="center">
  <img src="assets/branding/opl-cloud-logo.png" alt="OPL Cloud 标志" width="132" />
</p>

<p align="center">
  <a href="./README.md">English</a> | <a href="./README.zh-CN.md"><strong>中文</strong></a>
</p>

<h1 align="center">OPL Cloud</h1>

<p align="center"><strong>让 One Person Lab 的复杂工作在云端持续推进</strong></p>
<p align="center">AI 接入 · 在线工作空间 · 智能体服务 · 受控资源 · 连续证据</p>

<!--
Owner: `one-person-lab-cloud`
Purpose: `public_cloud_entry`
State: `active_public_entry`
Machine boundary: 面向读者的产品与架构入口。本仓库同时持有 Cloud 实现，但文档、源码、测试或构建本身都不能证明部署状态、账单事实、发布状态、领域结论或负责人验收。
-->

<p align="center">
  <img src="assets/branding/opl-cloud-overview-v2.png" alt="OPL Cloud 将本地项目、在线延续、私有数据、远程计算、协作审阅和服务交付连接为一条工作链" width="100%" />
</p>

## 为什么需要 OPL Cloud

科研、基金申请、汇报、书籍和智能体开发，很少能在一次会话或一台设备上完成。工作往往从本机开始，随后需要私有数据、远程算力和人工审阅，最后还可能成为可供他人调用的服务。如果这些环节分散在互不相通的工具里，项目状态、权限、成本和证据很快就会彼此脱节。

OPL Cloud 把这些环节留在同一条 OPL 工作链中：

- 将本机 OPL App 项目延续到云端 OPL Workspace；
- 在不转移原负责人权限的前提下，使用获准的模型、数据源、软件环境、存储和算力；
- 将经过验证的精确智能体版本发布为 API、嵌入组件或托管界面；
- 让审批、用量、来源、审阅和后续入口始终与原项目相连；
- 把专业判断留给相应的领域智能体和人类负责人。

OPL Cloud 是 OPL 产品体系中的第四层。`OPL Base` 提供唯一的框架宿主，`OPL App` 提供本地工作台，`OPL Packages` 提供可安装能力，OPL Cloud 则补上在线工作空间、账户治理、托管资源、协作和智能体服务。OPL Cloud 只引用这些产品各自维护的权威信息，不替代 OPL Base，不代替各仓库发布软件包，也不创建第二个 Cordis 宿主。

## 产品模型

| 用户需求 | 产品模块 | 主要职责 |
| --- | --- | --- |
| AI 接入与用量 | **OPL Gateway** | 模型接入、路由、服务商策略和用量信号 |
| 在线项目工作 | **OPL Workspace** | 让每个账户拥有零个或多个相互独立的云端工作空间 |
| 智能体对外服务 | **OPL Serve** | 管理精确服务、不可变版本、部署、API、嵌入组件和托管界面 |
| 账户治理 | **OPL Console** | 管理账户策略、审批、额度、账单和受管资源策略 |
| 数据、工具与算力 | **OPL Fabric** | 提供连接、计算、存储、运行环境和执行适配器 |
| 证据连续性 | **OPL Ledger** | 保存回执、来源、审阅记录和继续工作的引用 |

各软件包仓库维护自己的稳定身份、能力、入口和精确发布版本；配置好的原生载体负责实际安装、更新、移除以及最新的可调用状态。OPL Framework 只负责发现、载体委托、软件包状态汇总和通用执行语义；OPL Runway 负责调用与会话的执行生命周期；领域智能体继续负责专业策略、质量结论、产物和交付。OPL Cloud 使用这些权威信息，但不复制另一套事实。

## 当前聚焦

第一阶段只打通一条克制而完整的纵向路径：用精简的 Console 管理必要的工作空间、余额和用量；通过
`Console -> Control Plane -> 工作空间启动器/适配器 -> 本地 Docker`
真实创建和管理 OPL App/WebUI 工作空间；通过 Sub2API 读取和结算 OPL Gateway 的权威账目，不另建钱包。面向公众的自助注册、支付充值和更细致的界面优化仍在后续计划中。

当前源码已经包含适用于 Linux 主机的 `local-docker` 工作空间适配器。它要求工作空间存储位于启用项目配额的独立 ext4/XFS 挂载点上；主机条件不满足时会明确失败，不会假装配额已经生效。

源码能力、公开版本和具体实例是三个不同层次。唯一公开版本 `v0.1.7` 较早，只有基础编排文件、本地工作空间扩展、环境变量示例、发布清单和校验和这 5 个文件；它不包含当前源码中由 10 个文件组成的编排结构和完整工作空间安装路径。请从[当前状态](docs/status.md)了解已经验证的能力，从[安装说明](docs/installation.md)了解公开版本边界，从[路线图](docs/roadmap.md)了解仍未闭合的端到端结果。

## 一条连续的工作链

```text
本机 OPL App 项目
-> 需要时延续到在线 OPL Workspace
-> 使用获准的 OPL Gateway 和 OPL Fabric 能力
-> 将结果与审阅带回工作台
-> 由 OPL Ledger 保留复查和继续工作的线索
-> 服务成熟后，通过 OPL Serve 发布精确的智能体版本
```

每个账户可以拥有零个或多个相互独立的 OPL Workspace。每个工作空间都有自己的稳定身份、访问地址、运行环境、资源绑定、账期、凭据和回执。OPL Cloud 不在产品层设置固定数量上限，但每次创建仍要满足余额、服务商容量、额度和策略要求。一个账户也可以发布多个智能体服务，因为服务是一种独立的部署资源，并不等同于工作空间。

## 仓库边界

`one-person-lab-cloud` 是 OPL Cloud 唯一的产品与实现仓库，负责公开愿景、目标架构、白皮书、路线图、Console、Control Plane、Fabric、Ledger、工作空间交付、机器合同、通用安装资产、GHCR 镜像、GitHub Release 和可复用的服务商适配器。`opl-cloud` 只是 npm 包、镜像、二进制、服务、命名空间、环境变量和运行器标签中的短名称，不代表另一个仓库。

`opl-instance-medopl` 是 `medopl` 实例的唯一负责人，管理域名、Tencent/TKE 选择、启用的 OPL Cloud 套餐、生产环境与密钥、部署流程、镜像固定、回滚和回执。面向客户的版本化价格仍由 OPL Cloud Control Plane 统一维护，实例不能自行改价。实例只使用不可变的 OPL Cloud 源码提交标识和镜像摘要，不复制产品源码或运行时代码。

设计、合同、生成物、测试通过或镜像发布，都不能单独证明某个实例已经部署并可用。能力、健康、安全、账单、发布和验收结论，必须分别读取相应的实现、机器合同、运行结果和负责人回执。[路线图](docs/roadmap.md)只负责记录尚未完成的 OPL Cloud 结果和下一步，不是运行状态看板。

## 从这里开始

- [阅读 OPL Cloud 白皮书](https://gaofeng21cn.github.io/one-person-lab/latest/whitepapers/opl-cloud-whitepaper.html)
- [查看文档索引与权威归属](docs/README.md)
- [了解架构和职责边界](docs/architecture.md)
- [查看当前已经验证的能力](docs/status.md)
- [了解公开版本与候选版本的安装边界](docs/installation.md)
- [查看当前差距和下一步](docs/roadmap.md)
- [了解 Workspace 身份与外部 SaaS 边界](docs/workspace-identity-and-external-saas-boundary.md)

<details>
  <summary><strong>开发与运维</strong></summary>

### 仓库结构

```text
one-person-lab-cloud/
  apps/                Console 用户界面
  assets/              公开品牌资源与用户路径图片
  contracts/           白皮书产物配置
  deploy/              通用安装资产和可复用适配器模板
  docs/                产品、实现架构、规划和历史文档
  packages/contracts/  当前机器合同
  scripts/             白皮书构建和发布请求脚本
  services/            Control Plane、Fabric 和 Ledger
  tools/               本地验证、产品发布和通用验证工具
```

技术文档统一从[文档索引](docs/README.md)进入。产品目标、当前实现、实例配置和外部权威信息必须保持清晰分层，不能再建立第二套 Cloud 当前事实。

### 参与开发

提交拉取请求前请阅读[贡献指南](CONTRIBUTING.md)。`main` 分支由严格的 `validate` 汇总检查和已解决的审阅对话保护；生产与部署结论仍需分别通过对应的授权和证据门槛。

### 最小检查

```bash
node --experimental-strip-types scripts/build-opl-cloud-whitepaper.ts
npm test
npm run typecheck
npm run build
git diff --check
```

白皮书构建只能证明产物已经正确渲染。正式发布还必须经过批准的工作流，并对公开文件逐字节回读；具体边界见[白皮书交付证据](docs/delivery/whitepapers/README.md)。

</details>
