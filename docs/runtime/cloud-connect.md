# Cloud Connect 本地凭证助手

Cloud 用户通过 Console 的 Agent connections 页面创建 `opl-cloud-mcp` 程序凭证，
下载一次性 JSON 文件后，在自己的终端执行：

```sh
python3 scripts/cloud-connect.py import \
  --credential-file /path/to/download.json \
  --endpoint https://cloud.example.com
```

`--endpoint` 是签发凭证的 Cloud origin。凭证文件由浏览器同源、带 CSRF 的
`POST /api/v2/auth/agent-credentials` 返回；撤销仍在同一 Console 完成。导入不验证
远端可用性，首次调用由 CloudIdentity 核验签发状态、有效期与当前权限。
导入成功只输出 opaque `cloud_ref`、绑定修订和显示元数据，不输出凭证或本地路径。
完成导入后，用户可删除浏览器下载的原始文件。

助手支持 macOS/Linux，要求 Python 3，使用标准库，无额外依赖。凭证保存在 Cloud 自有的
`~/.config/opl-cloud/connect`：目录权限 `0700`，文件权限 `0600`。
安装配置可使用 `OPL_CLOUD_CONNECT_HOME` 选择同权限的独立目录。
此目录属于当前操作系统用户，不能作为该用户下不同进程之间的安全沙箱。
同一 Cloud origin 和凭证 ID 再次导入时保留 `cloud_ref`，生成新的 `revision`；
旧引用随即失效，需要重新导入 Framework 连接并重新获得相应授权。

在可信 Host 启动环境中指定助手的绝对路径，然后通过 Framework 导入已有引用：

```sh
export OPL_CLOUD_CONNECT_BINARY=/absolute/path/one-person-lab-cloud/scripts/cloud-connect.py
opl connect cloud list --json
opl connect cloud import --cloud-ref cloud:00000000-0000-0000-0000-000000000000 --json
```

将示例引用替换为导入结果。Framework 只持有 opaque 引用，凭证文件和 Bearer 值
不进入 Framework、工具参数或模型上下文。Framework 负责 consumer capability grant；
CloudIdentity 继续决定 Cloud 侧可访问对象。两层权限均须有效。

## Provider 接口

Framework 启动 `<helper> connect`，通过 stdin 发送一条不超过 64 KiB 的 JSON。
`schema_version` 固定为 `opl-cloud-connect.v1`；所有层级拒绝未声明字段。

| operation | 参数 | 成功 result |
| --- | --- | --- |
| `list` | 无 | `{connections: [metadata]}` |
| `inspect` | `credential_handle: {opaque, revision}` | `{connection: metadata}` |
| `invoke` | 同一 handle、`capability`、`input` | 原 MCP `structuredContent` |

metadata 包含 `cloud_ref`、`revision`、`credential_id`、`label`、`tenant_id`、
`audience`、`expires_at`、`endpoint`、`status`、`status_code` 和 `capabilities`。
`list`、`inspect` 只读本地元数据；`ready` 表示本地有效期尚未结束，不能替代远端
认证成功证据。过期时显示 `attention_needed` / `CREDENTIAL_EXPIRED`，能力列表为空。

允许的能力直接采用 Cloud MCP 工具名：

- `cloud_list_compute_plans`、`cloud_list_storage_plans`、`cloud_list_workspaces`：
  可选 `cursor`（不超过 4096 字符）、`limit`（1–100 的整数）。
- `cloud_get_quote`、`cloud_get_workspace`、`cloud_get_workspace_operation`：
  必需 `id`（非空，不超过 256 字符），可选 `expectedRevision`（不超过 256 字符）。

助手只向存储绑定的 origin 下 `/api/v2/mcp` 发送 `initialize`、
`notifications/initialized`、`tools/call`，协议版本为 `2025-03-26`。
它不接受模型选择的 URL、path、header、actor 或 Bearer，不跟随重定向，不使用
浏览器 cookie，也不复制 Cloud REST 业务逻辑。HTTPS 使用系统信任证书；仅
loopback 允许 HTTP，供隔离测试。单次 HTTP 超时 4 秒，响应上限 1 MiB，无自动重试。

成功 envelope 为 `{ok: true, disposition: "completed", result: ...}`。
失败退出非零，输出 `{ok: false, disposition: ..., error: {code: ...}}`；
不输出远端错误正文、stderr、凭证或内部文件路径。

| disposition | 适用情况 | 典型 code |
| --- | --- | --- |
| `not_dispatched` | 本地参数、凭证、绑定或连接建立失败 | `INVALID_INPUT`、`CREDENTIAL_EXPIRED`、`STALE_CREDENTIAL_HANDLE`、`REMOTE_UNREACHABLE` |
| `provider_reported` | 远端明确拒绝或报告工具失败 | `REMOTE_UNAUTHORIZED`、`REMOTE_FORBIDDEN`、`REMOTE_REDIRECT_REJECTED`、`REMOTE_HTTP_ERROR`、`REMOTE_MCP_ERROR`、`REMOTE_TOOL_ERROR` |
| `unknown_after_dispatch` | 请求发出后失联、无效或超大响应 | `REMOTE_RESPONSE_UNKNOWN`、`REMOTE_RESPONSE_INVALID` |

凭证撤销由 CloudIdentity 在每次 MCP 请求时核验。响应丢失后，调用者保留未知状态，
恢复连接后读取原对象；不将未知结果转换为成功或隐式重发。

## 验证

```sh
node --test tests/tools/cloud-connect.test.ts
```

测试以真实子进程和 loopback HTTP 服务验证权限、协议、原始 owner 内容传递、
过期与旧绑定拒绝、输入边界、远端拒绝、响应丢失和无重试。生产环境可用性仍需
已部署的 CloudIdentity 签发能力及用户在目标环境的实际凭证，不由本地测试推断。
