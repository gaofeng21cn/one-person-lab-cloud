<p align="center">
  <img src="assets/branding/opl-cloud-logo.png" alt="OPL Cloud logo" width="132" />
</p>

<p align="center">
  <a href="./README.md">English</a> | <a href="./README.zh-CN.md"><strong>中文</strong></a>
</p>

<h1 align="center">OPL Cloud</h1>

<p align="center"><strong>让 One Person Lab 的复杂工作在云端连续推进</strong></p>
<p align="center">AI 接入 · 在线工作台 · Agent 服务 · 受控资源 · 证据连续性</p>

<!--
Owner: `one-person-lab-cloud`
Purpose: `public_cloud_entry`
State: `active_public_entry`
Machine boundary: 面向人的产品与架构入口。本仓不持有 Cloud runtime、已部署服务状态、账单真相、发布状态、领域结论或负责人验收。
-->

<p align="center">
  <img src="assets/branding/opl-cloud-overview-v2.png" alt="OPL Cloud 让工作从本机延伸到在线继续、私有数据、远端计算、共同审阅和按需发布" width="100%" />
</p>

## 为什么需要 OPL Cloud

科研、基金、汇报、书籍和 Agent 开发很少在一次会话或一台机器上完成。工作可能从本机开始，随后需要私有数据、远端计算和人工审阅，最后还可能变成可供他人调用的服务。如果这些环节散落在彼此无关的工具中，项目状态、权限、成本和证据就会逐渐脱节。

OPL Cloud 定义如何让这些需求留在同一条 OPL 工作链中：

- 从本机 OPL App 项目自然延续到在线 OPL Workspace；
- 在不转移 owner 权威的前提下，使用获准的模型、数据源、软件环境、存储和计算；
- 把经过验证的精确 Agent Revision 发布为稳定 API、嵌入组件或托管界面；
- 让批准、用量、来源、审阅和继续入口始终连接到原工作；
- 把专业判断留给对应领域 Agent 和人类负责人。

## 产品模型

| 用户需要 | 目标产品面 | 责任边界 |
| --- | --- | --- |
| AI 接入与用量 | **OPL Gateway** | 模型接入、路由、provider 策略和用量信号 |
| 在线项目工作 | **OPL Workspace** | 每个用户账号一个主在线工作台 |
| Agent 对外使用 | **OPL Serve** | 精确 Service、不可变 Revision、Deployment、API、Embed 和 Hosted UI |
| 账号治理 | **OPL Console** | 账号策略、批准、额度、账单和纳管资源策略 |
| 数据、工具与计算 | **OPL Fabric** | Connect、Compute、Storage、Environments 和执行适配器 |
| 证据连续性 | **OPL Ledger** | 回执、来源、审阅和继续引用 |

OPL Framework 继续持有能力包生命周期和通用执行语义。OPL Packages 持有 manifest、digest、安装、lock、更新、回滚和修复；OPL Runway 持有 Invocation 与 Session 执行生命周期；领域 Agent 持有专业策略、质量结论、产物和交付权威。Cloud 各产品面只消费这些引用，不创建竞争真相。

## 一条连续工作链

```text
本机 OPL App 项目
-> 按需进入在线 OPL Workspace
-> 使用获准的 Gateway / Fabric 能力
-> 结果与审阅回到工作台
-> Ledger refs 保留复查和继续线索
-> 按需通过 OPL Serve 发布精确 Agent Revision
```

每个用户账号只有一个主 OPL Workspace。项目与协作位于这个 Workspace 内部或围绕它展开，不创建多份工作台真相。一个账号可以发布多个 Agent Service，因为 Service 是部署资源，不是额外 Workspace。

## 当前仓库边界

本仓持有 OPL Cloud 的公开愿景、目标产品架构、规划合同和白皮书源文，是文档与产品架构聚合仓，不是实现仓。设计存在、合同存在、生成物完成或文档检查通过，都不代表 Gateway、Workspace、Serve、Console、Fabric 或 Ledger 已经部署或 ready。

某项能力当前是否可用、运行是否健康、安全与账单是否成立、能否发布以及 owner 是否验收，必须读取对应实现、机器合同、运行输出和负责人回执。[路线图](docs/roadmap.md) 是 Cloud 剩余差距的唯一当前规划 owner，不是 readiness dashboard。

## 从这里开始

- [在线阅读 OPL Cloud 白皮书](https://gaofeng21cn.github.io/one-person-lab-cloud/latest/whitepapers/opl-cloud-whitepaper.html)
- [文档索引与 owner 映射](docs/README.md)
- [架构与权威边界](docs/architecture.md)
- [当前路线图、差距和下一轮 Agent Prompt](docs/roadmap.md)
- [Workspace 身份与外部 SaaS 边界](docs/workspace-identity-and-external-saas-boundary.md)

<details>
  <summary><strong>开发者与运维细节</strong></summary>

### 仓库结构

```text
one-person-lab-cloud/
  assets/              公开品牌与用户旅程资产
  contracts/           白皮书 artifact Profile
  docs/                产品、架构、规划与 provenance 文档
  scripts/             白皮书构建与发布请求 wrapper
```

技术文档统一从 [docs/README.md](docs/README.md) 进入。本仓不得新增服务代码、runtime state、package state 或复制来的 owner truth。

### 最小检查

```bash
node --experimental-strip-types scripts/build-opl-cloud-whitepaper.ts
git diff --check
```

白皮书构建只证明 artifact 渲染；发布还必须经过批准工作流和公开 exact-byte 回读，边界见 [白皮书交付证据](docs/delivery/whitepapers/README.md)。

</details>
