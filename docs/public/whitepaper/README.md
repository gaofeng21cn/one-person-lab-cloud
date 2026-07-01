# OPL Cloud 白皮书

Owner: `one-person-lab-cloud`
Purpose: `public_cloud_whitepaper_artifact_index`
State: `active_public_support`
Machine boundary: 本目录是用户可读白皮书与派生产物入口。`opl-cloud-whitepaper.md` 是正文源；PDF 和 verification JSON 是从 Markdown 派生的生成物。Cloud 服务可用性、账单、Workspace 生命周期、Fabric 执行、Ledger 回执和发布状态的机器真相仍以对应服务、仓库、合同、运行输出和负责人回执为准。

本目录承接面向用户分享的 `OPL Cloud` 白皮书。它解释 OPL Cloud 的定位、产品分层、App/Workspace 等价工作面、Gateway/Fabric/Console/Ledger 边界，以及为什么 OPL Cloud 要成为复杂知识工作的云端前沿 AI 基础设施。

## 文件

- [OPL Cloud 白皮书 Markdown](./opl-cloud-whitepaper.md)：canonical 正文源，供仓库内阅读和人工维护。
- [OPL Cloud 白皮书 PDF](./opl-cloud-whitepaper.pdf)：用于分享、转发和离线阅读的正式版本。
- [Verification record](./opl-cloud-whitepaper-verification.json)：记录正文源、PDF 写入状态、PDF 页数、渲染页、文本抽取检查和工具链信息。

## 更新流程

1. 修改 `opl-cloud-whitepaper.md`。
2. 运行 `node --experimental-strip-types scripts/build-opl-cloud-whitepaper.ts`。
3. 检查 `opl-cloud-whitepaper-verification.json`，并渲染 PDF 页面确认版式。

