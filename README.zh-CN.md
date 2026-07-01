# OPL Cloud

OPL Cloud 是 One Person Lab 的云端前沿 AI 基础设施。

它统一承载前沿 AI 能力接入、在线 OPL 工作空间、受控计算、用量计量与可复现证据链，为科研探索、智能体开发和自动化工作流提供云端能力基座。

## 产品矩阵

| 产品 | 定位 | 状态 |
| --- | --- | --- |
| OPL Gateway | 前沿 AI 能力入口，负责 API 接入、Token 管理、模型接入与用量计量 | 已可用 |
| OPL Console | 云端管理控制台，负责账号、账单、工作空间、权限与运维管理 | 开发中 |
| OPL Workspace | 在线 OPL App 工作空间，提供独立 URL、账号、密码、计算与存储配置 | 开发中 |

OPL Cloud 不是第二个 OPL App。OPL App 仍然是 local-first 的用户工作空间；OPL Cloud 提供远端控制面、计算面、AI 能力入口与证据链能力。

## 当前范围

- OPL Gateway 是第一个已可用的 Cloud 组件。
- OPL Console 和 OPL Workspace 正在开发中。
- Gateway 的公开脚本和接入材料可先作为 integration assets 被引用，不强行拆成独立 public repo。
- 本仓库是 OPL Cloud 的公开产品与架构入口。

## 设计原则

- Cloud 是控制面，不是另一个聊天应用。
- 敏感数据默认留在用户或机构控制的环境中。
- 远端执行应遵循计划、批准、执行、回收、receipt。
- 关键产物应保留可审计、可复现、可继续的证据链。
- 能力包应围绕 OPL domain 精选，不做泛化插件市场。

## 文档

- [产品矩阵](docs/product-matrix.md)
- [架构](docs/architecture.md)
- [OPL Gateway](docs/opl-gateway.md)
- [OPL Console](docs/opl-console.md)
- [OPL Workspace](docs/opl-workspace.md)
- [Research Provenance](docs/research-provenance.md)
- [Roadmap](docs/roadmap.md)

