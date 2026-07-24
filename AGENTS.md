# One Person Lab Cloud

本仓持有 OPL Cloud 的公共产品入口、目标架构、规划合同和白皮书源码；它不是已部署 Cloud runtime。

- `docs/roadmap.md` 是当前 gap/next-step owner，`docs/architecture.md` 持有技术边界；不要在其它文档建立第二份 current status。
- 文档、规划合同、生成站点或本地 build 不证明 Gateway、Workspace、Serve、Console、Fabric 或 Ledger 已部署、可用或 production ready。
- Framework、App、Package 与 domain truth 留在各自 owner；本仓只引用，不复制实现状态、账单、安全、receipt 或 release currentness。
- 默认验证运行 `node --experimental-strip-types scripts/build-opl-cloud-whitepaper.ts` 和 `git diff --check`；公开白皮书必须走批准 workflow 并以 exact-byte receipt/readback 完成。

<!-- CODEGRAPH_START -->
## CodeGraph

- 本仓库使用本地 `.codegraph/` 索引；该目录不得纳入 Git。
- 定义、调用、影响范围和代码路径等结构检索优先使用 CodeGraph；字面文本检索使用 `rg`。
- 索引缺失或过期时运行 `codegraph init .` 或 `codegraph sync .`。
<!-- CODEGRAPH_END -->
