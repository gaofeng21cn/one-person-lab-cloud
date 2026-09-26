# Cloud WebUI artifact manifest 与 receipt 设计

状态：**Draft / blocked / not admitted**（2026-09-26）

本文件只定义 Cloud WebUI 独立 artifact 的文档边界，不声明已经构建、发布或可部署。它配套：

- `docs/evidence/source-checks/fixtures/cloud-webui-artifact-manifest-draft.json`
- `docs/evidence/source-checks/2026-09-26-cloud-webui-artifact-blocked-receipt.json`
- `docs/implementation/issue-draft-cloud-webui-356.md`

## 1. 载体与输入

Cloud WebUI 的目标载体是独立的 multi-arch OCI image：

- repository（待最终确认）：`ghcr.io/gaofeng21cn/one-person-lab-cloud-webui`
- platforms：`linux/amd64`、`linux/arm64`
- container port：`3000`
- health endpoint：`/healthz`
- artifact identity：只接受 immutable `sha256:<64 hex>` digest；不接受 `latest`、`stable` 或任何可变 tag
- compatibility：必须声明并匹配 Runtime ABI 与 Package format；缺任一真实 upstream identity 时拒绝 Build

此轮没有修改 `apps/console-ui/**`、Cloud BFF、Dockerfile、CI 或任何源码，因此没有产生上述 OCI bytes，也没有可严谨记录的 artifact digest。manifest draft 使用 `null` 表示“未产生”，而不是使用零值或推导值。

## 2. Manifest 与 receipt 的职责

### Draft manifest

Draft manifest 仅保存计划中的字段、当前 Cloud source provenance、contract 引用、预期网络形状和阻断项。它不是 `opl-cloud-webui-artifact/v1` 的 admitted object，不能注册到 Capability、不能作为 Build input，也不能作为发布或部署依据。

真实 artifact 产出后，必须以构建系统的实际输出替换 draft，并至少绑定：

1. source commit SHA 与 tree；
2. 构建上下文及其 SHA256；
3. index digest 与每个平台 digest；
4. port、health/readiness、compatibility；
5. artifact manifest 本身的 SHA256；
6. 构建日志及不泄露 API key 的证据。

### Blocked receipt

Receipt 记录本轮实际完成的文档设计、已核实的前置证据和未完成的运行验证。它明确区分：

- 已存在的 Cloud contract / intake receipt；
- 观察到但未接纳的 upstream `:stable` WebUI image；
- 尚未构建的 Cloud-owned image；
- 因缺少真实 Runtime Release、qualified AgentVersion 和 artifact bytes 而未执行的 Build、readiness、Workspace/Fabric/Serve 与 BFF readback。

Receipt 不生成 artifact digest，不生成 Build digest，不把 fixture 的零 digest 升格为真实证据。

## 3. W13/W14/W16/W26 边界

- **W13**：只承载同源 session/CSRF/Origin 与 typed Owner read/write 路由；BFF 不写业务表，不判断 artifact/build/deploy 成功。
- **W14**：展示 Package、Runtime、WebUI 目录和 Build progress/log；三者必须展示 owner 返回的 exact refs，UI 不自行计算 digest 或判定成功。
- **W16**：读取 Workspace/Quote/Deploy/Access/Model/Secret ref 的 owner readback；UI 不写 deployment/current selection，API key 只经安全表单/Secret ref。
- **W26**：以真实 BFF → Owner → Local provider/runtime → readback 验收整链；fixture、静态构建通过或字段存在均不替代真实链。

任何共享 BFF router、shared proto/generated、owner writer、数据库迁移或公共 docs 段落仍需其 owner 串行整合，本轮不触碰。

## 4. 解阻条件

1. OMA/Foundry 产生 qualified AgentVersion，并提供可绑定的 Package content digest；
2. OPL App/Framework 发布 approved server/headless Runtime Release OCI、ABI、entrypoint、readiness/access、model 与 Secret contract；
3. Cloud WebUI 以真实构建产出 digest-pinned multi-arch image，并实际验证 `3000`、`/healthz` 与兼容声明；
4. W09 Build 以三者 exact refs 产生 immutable OCI、DeploymentDescriptor 与 Build receipt；
5. 执行并记录 artifact smoke/readiness、浏览器场景、`npm run verify:local:full` 和真实 owner readback。

在第 1–3 项完成前，Cloud integration 必须保持 fail-closed。
