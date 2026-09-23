# 继承基线：fabric 的实际 API / DB

> 上游与fork共同基线 `50520e27a6b9a630eefdc2df7da3e5ec498d28a0`。API由Go AST的真实注册函数路径提取；DB为临时空PostgreSQL执行真实loader后的readback，不是生产数据库/历史数据迁移结果。fork `7f5d05fe9b855d8af216caad714cf7fc85014d3c` 保留此模块业务代码。

## 覆盖与判读

63 个已挂载路由pattern（含健康/静态/代理/明确404入口，不等同业务API数量）；5 张实际表、58 列（含迁移journal）。

请求/响应中的map和helper不做猜测式OpenAPI转换。每条附实际handler源码及其调用点；命名JSON类型完整字段见[旧DTO目录](legacy-wire-types.md)，动态投影以源handler/decoder为准。租户校验、middleware和数据库状态影响响应，不能从字段存在推导授权。

## API注册清单

| pattern | 注册函数 | 源码 |
| --- | --- | --- |
| `GET /fabric/catalog` | `newFabricMux` | `services/fabric/internal/http/server.go:157` |
| `GET /fabric/compute-allocations/{id}` | `newFabricMux` | `services/fabric/internal/http/server.go:415` |
| `GET /fabric/compute-allocations/{id}/destroy-status` | `newFabricMux` | `services/fabric/internal/http/server.go:423` |
| `GET /fabric/compute-pool-head` | `newFabricMux` | `services/fabric/internal/http/server.go:177` |
| `GET /fabric/compute-pool-head/terminalization` | `newFabricMux` | `services/fabric/internal/http/server.go:194` |
| `GET /fabric/jobs/{id}` | `newFabricMux` | `services/fabric/internal/http/server.go:353` |
| `GET /fabric/machine-ownerships/{resourceId}` | `newFabricMux` | `services/fabric/internal/http/server.go:334` |
| `GET /fabric/monthly-preflight-report` | `newFabricMux` | `services/fabric/internal/http/server.go:239` |
| `GET /fabric/operations` | `newFabricMux` | `services/fabric/internal/http/server.go:299` |
| `GET /fabric/readiness` | `newFabricMux` | `services/fabric/internal/http/server.go:149` |
| `GET /fabric/runtime-health-summary` | `newFabricMux` | `services/fabric/internal/http/server.go:291` |
| `GET /fabric/runtime-observations` | `newFabricMux` | `services/fabric/internal/http/server.go:283` |
| `GET /fabric/storage-volumes/{id}` | `newFabricMux` | `services/fabric/internal/http/server.go:458` |
| `GET /fabric/workspace-runtimes/{workspaceId}/delete-observation` | `newFabricMux` | `services/fabric/internal/http/server.go:685` |
| `GET /fabric/workspace-runtimes/{workspaceId}/gateway-secret` | `newFabricMux` | `services/fabric/internal/http/server.go:724` |
| `GET /fabric/workspace-runtimes/{workspaceId}/gateway-secret/observation` | `newFabricMux` | `services/fabric/internal/http/server.go:728` |
| `GET /fabric/workspace-runtimes/{workspaceId}/observation` | `newFabricMux` | `services/fabric/internal/http/server.go:681` |
| `GET /fabric/workspace-runtimes/{workspaceId}/status` | `newFabricMux` | `services/fabric/internal/http/server.go:677` |
| `GET /healthz` | `newFabricMux` | `services/fabric/internal/http/server.go:49` |
| `POST /fabric/compute-allocations` | `newFabricMux` | `services/fabric/internal/http/server.go:407` |
| `POST /fabric/compute-allocations/{id}/destroy` | `newFabricMux` | `services/fabric/internal/http/server.go:433` |
| `POST /fabric/compute-allocations/{id}/renew` | `newFabricMux` | `services/fabric/internal/http/server.go:441` |
| `POST /fabric/compute-claim-recovery/identity-evidence` | `newFabricMux` | `services/fabric/internal/http/server.go:256` |
| `POST /fabric/compute-pool-head/terminalization` | `newFabricMux` | `services/fabric/internal/http/server.go:217` |
| `POST /fabric/gateway-secrets` | `newFabricMux` | `services/fabric/internal/http/server.go:732` |
| `POST /fabric/jobs` | `newFabricMux` | `services/fabric/internal/http/server.go:345` |
| `POST /fabric/jobs/{id}/cancel` | `newFabricMux` | `services/fabric/internal/http/server.go:357` |
| `POST /fabric/jobs/{id}/claim` | `newFabricMux` | `services/fabric/internal/http/server.go:366` |
| `POST /fabric/jobs/{id}/complete` | `newFabricMux` | `services/fabric/internal/http/server.go:382` |
| `POST /fabric/jobs/{id}/fail` | `newFabricMux` | `services/fabric/internal/http/server.go:390` |
| `POST /fabric/jobs/{id}/heartbeat` | `newFabricMux` | `services/fabric/internal/http/server.go:374` |
| `POST /fabric/jobs/{id}/retry` | `newFabricMux` | `services/fabric/internal/http/server.go:398` |
| `POST /fabric/monthly-preflight` | `newFabricMux` | `services/fabric/internal/http/server.go:160` |
| `POST /fabric/provider-facts/batch` | `newFabricMux` | `services/fabric/internal/http/server.go:270` |
| `POST /fabric/storage-attachments` | `newFabricMux` | `services/fabric/internal/http/server.go:495` |
| `POST /fabric/storage-attachments/{id}/detach` | `newFabricMux` | `services/fabric/internal/http/server.go:518` |
| `POST /fabric/storage-volumes` | `newFabricMux` | `services/fabric/internal/http/server.go:450` |
| `POST /fabric/storage-volumes/{id}/destroy` | `newFabricMux` | `services/fabric/internal/http/server.go:479` |
| `POST /fabric/storage-volumes/{id}/renew` | `newFabricMux` | `services/fabric/internal/http/server.go:470` |
| `POST /fabric/workspace-application-runtimes` | `newFabricMux` | `services/fabric/internal/http/server.go:595` |
| `POST /fabric/workspace-application-runtimes/{workspaceId}/credentials` | `newFabricMux` | `services/fabric/internal/http/server.go:631` |
| `POST /fabric/workspace-application-runtimes/{workspaceId}/gateway-secret-cleanup` | `newFabricMux` | `services/fabric/internal/http/server.go:559` |
| `POST /fabric/workspace-application-runtimes/{workspaceId}/lifecycle` | `newFabricMux` | `services/fabric/internal/http/server.go:631` |
| `POST /fabric/workspace-application-runtimes/{workspaceId}/lifecycle-readback` | `newFabricMux` | `services/fabric/internal/http/server.go:631` |
| `POST /fabric/workspace-application-runtimes/{workspaceId}/preflight` | `newFabricMux` | `services/fabric/internal/http/server.go:577` |
| `POST /fabric/workspace-application-runtimes/{workspaceId}/readback` | `newFabricMux` | `services/fabric/internal/http/server.go:613` |
| `POST /fabric/workspace-launches/closeout` | `newFabricMux` | `services/fabric/internal/http/server.go:103` |
| `POST /fabric/workspace-launches/closeout/freeze` | `newFabricMux` | `services/fabric/internal/http/server.go:103` |
| `POST /fabric/workspace-launches/closeout/read` | `newFabricMux` | `services/fabric/internal/http/server.go:103` |
| `POST /fabric/workspace-launches/preflight` | `newFabricMux` | `services/fabric/internal/http/server.go:52` |
| `POST /fabric/workspace-launches/preflight/read` | `newFabricMux` | `services/fabric/internal/http/server.go:61` |
| `POST /fabric/workspace-launches/stages/ensure` | `newFabricMux` | `services/fabric/internal/http/server.go:88` |
| `POST /fabric/workspace-launches/stages/observe` | `newFabricMux` | `services/fabric/internal/http/server.go:79` |
| `POST /fabric/workspace-launches/stages/read` | `newFabricMux` | `services/fabric/internal/http/server.go:70` |
| `POST /fabric/workspace-runtimes` | `newFabricMux` | `services/fabric/internal/http/server.go:526` |
| `POST /fabric/workspace-runtimes/power` | `newFabricMux` | `services/fabric/internal/http/server.go:129` |
| `POST /fabric/workspace-runtimes/power/read` | `newFabricMux` | `services/fabric/internal/http/server.go:129` |
| `POST /fabric/workspace-runtimes/{workspaceId}/credentials/reveal` | `newFabricMux` | `services/fabric/internal/http/server.go:689` |
| `POST /fabric/workspace-runtimes/{workspaceId}/destroy` | `newFabricMux` | `services/fabric/internal/http/server.go:668` |
| `POST /fabric/workspace-runtimes/{workspaceId}/gateway-network/recover` | `newFabricMux` | `services/fabric/internal/http/server.go:656` |
| `POST /fabric/workspace-runtimes/{workspaceId}/gateway-secret` | `newFabricMux` | `services/fabric/internal/http/server.go:712` |
| `POST /fabric/workspace-runtimes/{workspaceId}/image-replacements` | `newFabricMux` | `services/fabric/internal/http/server.go:546` |
| `POST /fabric/workspace-runtimes/{workspaceId}/repair` | `newFabricMux` | `services/fabric/internal/http/server.go:534` |

## 每条API的实际字段处理与交互


### 1. GET /fabric/catalog

注册：`services/fabric/internal/http/server.go:157`；`newFabricMux`。

实际调用：`Catalog`, `Context`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, service.Catalog(r.Context()))
}
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, service.Catalog(r.Context()))`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Catalog` @ `services/fabric/internal/fabric/service.go:119`; `writeJSON` @ `services/fabric/internal/http/server.go:1172`

### 2. GET /fabric/compute-allocations/{id}

注册：`services/fabric/internal/http/server.go:415`；`newFabricMux`。

实际调用：`Context`, `GetComputeAllocation`, `PathValue`, `TrimSpace`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	allocation, ok := service.GetComputeAllocation(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if !ok {
		writeError(w, http.StatusNotFound, "compute_allocation_not_found")
		return
	}
	writeJSON(w, http.StatusOK, allocation)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `id` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, allocation)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`GetComputeAllocation` @ `services/fabric/internal/fabric/compute_allocation.go:512`; `writeJSON` @ `services/fabric/internal/http/server.go:1172`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 3. GET /fabric/compute-allocations/{id}/destroy-status

注册：`services/fabric/internal/http/server.go:423`；`newFabricMux`。

实际调用：`Context`, `Error`, `PathValue`, `ReadComputeDestroyStatus`, `TrimSpace`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	allocation, err := service.ReadComputeDestroyStatus(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, allocation)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `id` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, allocation)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`ReadComputeDestroyStatus` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:3724`; `Error` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:47`; `ReadComputeDestroyStatus` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:867`; `ReadComputeDestroyStatus` @ `services/fabric/internal/fabric/compute_allocation.go:466`; `ReadComputeDestroyStatus` @ `services/fabric/internal/fabric/local_docker_provider.go:592`; `ReadComputeDestroyStatus` @ `services/fabric/internal/fabric/tencent_provider_compute.go:1149`; `writeJSON` @ `services/fabric/internal/http/server.go:1172`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 4. GET /fabric/compute-pool-head

注册：`services/fabric/internal/http/server.go:177`；`newFabricMux`。

实际调用：`Context`, `Error`, `Is`, `ReadComputePoolHead`, `exactQueryValue`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	nodePoolID, ok := exactQueryValue(r, "nodePoolId")
	if !ok {
		writeError(w, http.StatusBadRequest, fabric.ErrInvalidMonthlyPreflight.Error())
		return
	}
	result, err := service.ReadComputePoolHead(r.Context(), nodePoolID)
	if errors.Is(err, fabric.ErrInvalidMonthlyPreflight) {
		writeError(w, http.StatusBadRequest, fabric.ErrInvalidMonthlyPreflight.Error())
		return
	}
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, result)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, result)`
- `writeJSON(w, http.StatusServiceUnavailable, result)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:47`; `ReadComputePoolHead` @ `services/fabric/internal/fabric/tencent_operator_pool_head.go:17`; `exactQueryValue` @ `services/fabric/internal/http/server.go:1097`; `writeJSON` @ `services/fabric/internal/http/server.go:1172`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 5. GET /fabric/compute-pool-head/terminalization

注册：`services/fabric/internal/http/server.go:194`；`newFabricMux`。

实际调用：`Context`, `Error`, `Get`, `Query`, `ReadComputePoolHeadTerminalization`, `ReadComputePoolHeadTerminalizationResult`, `exactQueryValue`, `len`, `writeComputePoolHeadTerminalizationResult`, `writeError`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	if len(values) == 1 {
		nodePoolID, ok := exactQueryValue(r, "nodePoolId")
		if !ok {
			writeError(w, http.StatusBadRequest, fabric.ErrInvalidComputePoolHeadTerminalization.Error())
			return
		}
		result, err := service.ReadComputePoolHeadTerminalization(r.Context(), nodePoolID)
		writeComputePoolHeadTerminalizationResult(w, result, err)
		return
	}
	if len(values) != 3 || len(values["nodePoolId"]) != 1 || len(values["approvalId"]) != 1 || len(values["approvalDigest"]) != 1 {
		writeError(w, http.StatusBadRequest, fabric.ErrInvalidComputePoolHeadTerminalization.Error())
		return
	}
	input := fabric.ComputePoolHeadTerminalizationInput{
		NodePoolID:	values.Get("nodePoolId"), ApprovalID: values.Get("approvalId"), ApprovalDigest: values.Get("approvalDigest"),
		IdempotencyKey:	values.Get("approvalId"),
	}
	result, err := service.ReadComputePoolHeadTerminalizationResult(r.Context(), input)
	writeComputePoolHeadTerminalizationResult(w, result, err)
}
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:47`; `Get` @ `services/fabric/internal/fabric/operation_capability_ports.go:108`; `Get` @ `services/fabric/internal/fabric/operation_store.go:1229`; `Get` @ `services/fabric/internal/fabric/operation_store.go:175`; `Get` @ `services/fabric/internal/fabric/provider_mutation_store_port.go:30`; `ReadComputePoolHeadTerminalization` @ `services/fabric/internal/fabric/tencent_operator_pool_head.go:109`; `ReadComputePoolHeadTerminalizationResult` @ `services/fabric/internal/fabric/tencent_operator_pool_head.go:240`; `exactQueryValue` @ `services/fabric/internal/http/server.go:1097`; `writeComputePoolHeadTerminalizationResult` @ `services/fabric/internal/http/server.go:1130`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 6. GET /fabric/jobs/{id}

注册：`services/fabric/internal/http/server.go:353`；`newFabricMux`。

实际调用：`Context`, `Job`, `PathValue`, `TrimSpace`, `writeJobResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	job, err := service.Job(r.Context(), strings.TrimSpace(r.PathValue("id")))
	writeJobResult(w, http.StatusOK, job, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `id` | `string` | path |

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Job` @ `services/fabric/internal/fabric/jobs.go:56`; `writeJobResult` @ `services/fabric/internal/http/server.go:1155`

### 7. GET /fabric/machine-ownerships/{resourceId}

注册：`services/fabric/internal/http/server.go:334`；`newFabricMux`。

实际调用：`Context`, `Error`, `Is`, `MachineOwnership`, `PathValue`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	ownership, err := service.MachineOwnership(r.Context(), r.PathValue("resourceId"))
	switch {
	case errors.Is(err, fabric.ErrMachineOwnershipNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "machine ownership query failed")
	default:
		writeJSON(w, http.StatusOK, ownership)
	}
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `resourceId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, ownership)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:47`; `MachineOwnership` @ `services/fabric/internal/fabric/operation_store.go:148`; `MachineOwnership` @ `services/fabric/internal/fabric/operation_store.go:1494`; `MachineOwnership` @ `services/fabric/internal/fabric/tencent_operator_identity_readback.go:233`; `writeJSON` @ `services/fabric/internal/http/server.go:1172`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 8. GET /fabric/monthly-preflight-report

注册：`services/fabric/internal/http/server.go:239`；`newFabricMux`。

实际调用：`Context`, `Error`, `Get`, `Is`, `MonthlyPreflightReport`, `Query`, `len`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	if len(values) != 1 || len(values["zone"]) != 1 {
		writeError(w, http.StatusBadRequest, fabric.ErrInvalidMonthlyPreflight.Error())
		return
	}
	result, err := service.MonthlyPreflightReport(r.Context(), fabric.MonthlyPreflightReportInput{Zone: values.Get("zone")})
	if errors.Is(err, fabric.ErrInvalidMonthlyPreflight) {
		writeError(w, http.StatusBadRequest, fabric.ErrInvalidMonthlyPreflight.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, fabric.ErrMonthlyPreflightUnavailable.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, result)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:47`; `Get` @ `services/fabric/internal/fabric/operation_capability_ports.go:108`; `Get` @ `services/fabric/internal/fabric/operation_store.go:1229`; `Get` @ `services/fabric/internal/fabric/operation_store.go:175`; `Get` @ `services/fabric/internal/fabric/provider_mutation_store_port.go:30`; `MonthlyPreflightReport` @ `services/fabric/internal/fabric/tencent_operator_monthly_preflight.go:14`; `MonthlyPreflightReport` @ `services/fabric/internal/fabric/tencent_provider.go:562`; `writeJSON` @ `services/fabric/internal/http/server.go:1172`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 9. GET /fabric/operations

注册：`services/fabric/internal/http/server.go:299`；`newFabricMux`。

实际调用：`Atoi`, `Context`, `Error`, `Is`, `ListOperationsPage`, `ParseQuery`, `len`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(query["limit"]) > 1 || len(query["cursor"]) > 1 {
		writeError(w, http.StatusBadRequest, fabric.ErrInvalidOperationPage.Error())
		return
	}
	for key := range query {
		if key != "limit" && key != "cursor" {
			writeError(w, http.StatusBadRequest, fabric.ErrInvalidOperationPage.Error())
			return
		}
	}
	limit := fabric.MaxFabricOperationPageSize
	if rawLimit, ok := query["limit"]; ok {
		limit, err = strconv.Atoi(rawLimit[0])
		if err != nil {
			writeError(w, http.StatusBadRequest, fabric.ErrInvalidOperationPage.Error())
			return
		}
	}
	cursor := ""
	if rawCursor, ok := query["cursor"]; ok {
		cursor = rawCursor[0]
	}
	page, err := service.ListOperationsPage(r.Context(), cursor, limit)
	if errors.Is(err, fabric.ErrInvalidOperationPage) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, page)
}
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, page)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:47`; `ListOperationsPage` @ `services/fabric/internal/fabric/operation_store.go:2239`; `writeJSON` @ `services/fabric/internal/http/server.go:1172`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 10. GET /fabric/readiness

注册：`services/fabric/internal/http/server.go:149`；`newFabricMux`。

实际调用：`Context`, `Error`, `Readiness`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	readiness, err := service.Readiness(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, readiness)
}
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, readiness)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:47`; `Readiness` @ `services/fabric/internal/fabric/local_docker_provider.go:851`; `Readiness` @ `services/fabric/internal/fabric/service.go:150`; `Readiness` @ `services/fabric/internal/fabric/tencent_provider_runtime.go:730`; `writeJSON` @ `services/fabric/internal/http/server.go:1172`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 11. GET /fabric/runtime-health-summary

注册：`services/fabric/internal/http/server.go:291`；`newFabricMux`。

实际调用：`Context`, `Error`, `RuntimeHealthSummary`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	result, err := service.RuntimeHealthSummary(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, fabric.ErrRuntimeHealthSummaryUnavailable.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, result)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:47`; `RuntimeHealthSummary` @ `services/fabric/internal/fabric/local_docker_runtime.go:1373`; `RuntimeHealthSummary` @ `services/fabric/internal/fabric/provider_facts.go:66`; `RuntimeHealthSummary` @ `services/fabric/internal/fabric/tencent_provider_runtime.go:722`; `writeJSON` @ `services/fabric/internal/http/server.go:1172`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 12. GET /fabric/runtime-observations

注册：`services/fabric/internal/http/server.go:283`；`newFabricMux`。

实际调用：`Context`, `RuntimeObservations`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	result, err := service.RuntimeObservations(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "runtime_observations_unavailable")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, result)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`RuntimeObservations` @ `services/fabric/internal/fabric/runtime_observations.go:28`; `writeJSON` @ `services/fabric/internal/http/server.go:1172`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 13. GET /fabric/storage-volumes/{id}

注册：`services/fabric/internal/http/server.go:458`；`newFabricMux`。

实际调用：`Context`, `Error`, `PathValue`, `ReadStorageVolume`, `TrimSpace`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	volume, err := service.ReadStorageVolume(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if err != nil && volume.ID == "" {
		writeError(w, http.StatusNotFound, "storage_volume_not_found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, volume)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `id` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, volume)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:47`; `ReadStorageVolume` @ `services/fabric/internal/fabric/local_docker_provider.go:697`; `ReadStorageVolume` @ `services/fabric/internal/fabric/storage.go:183`; `ReadStorageVolume` @ `services/fabric/internal/fabric/tencent_provider_storage.go:499`; `writeJSON` @ `services/fabric/internal/http/server.go:1172`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 14. GET /fabric/workspace-runtimes/{workspaceId}/delete-observation

注册：`services/fabric/internal/http/server.go:685`；`newFabricMux`。

实际调用：`Context`, `ObserveWorkspaceRuntimeDelete`, `PathValue`, `TrimSpace`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	observation := service.ObserveWorkspaceRuntimeDelete(r.Context(), strings.TrimSpace(r.PathValue("workspaceId")))
	writeJSON(w, http.StatusOK, observation)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `workspaceId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, observation)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`ObserveWorkspaceRuntimeDelete` @ `services/fabric/internal/fabric/local_docker_runtime.go:1097`; `ObserveWorkspaceRuntimeDelete` @ `services/fabric/internal/fabric/tencent_provider_runtime.go:331`; `ObserveWorkspaceRuntimeDelete` @ `services/fabric/internal/fabric/workspace_runtime.go:489`; `writeJSON` @ `services/fabric/internal/http/server.go:1172`

### 15. GET /fabric/workspace-runtimes/{workspaceId}/gateway-secret

注册：`services/fabric/internal/http/server.go:724`；`newFabricMux`。

实际调用：`Context`, `PathValue`, `TrimSpace`, `WorkspaceRuntimeGatewaySecret`, `writeResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	binding, err := service.WorkspaceRuntimeGatewaySecret(r.Context(), strings.TrimSpace(r.PathValue("workspaceId")))
	writeResult(w, binding, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `workspaceId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeResult(w, binding, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`WorkspaceRuntimeGatewaySecret` @ `services/fabric/internal/fabric/local_docker_runtime.go:1344`; `WorkspaceRuntimeGatewaySecret` @ `services/fabric/internal/fabric/tencent_provider_runtime.go:658`; `WorkspaceRuntimeGatewaySecret` @ `services/fabric/internal/fabric/workspace_runtime.go:666`; `writeResult` @ `services/fabric/internal/http/server.go:1106`

### 16. GET /fabric/workspace-runtimes/{workspaceId}/gateway-secret/observation

注册：`services/fabric/internal/http/server.go:728`；`newFabricMux`。

实际调用：`Context`, `ObserveWorkspaceRuntimeGatewaySecret`, `PathValue`, `TrimSpace`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	observation := service.ObserveWorkspaceRuntimeGatewaySecret(r.Context(), strings.TrimSpace(r.PathValue("workspaceId")))
	writeJSON(w, http.StatusOK, observation)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `workspaceId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, observation)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`ObserveWorkspaceRuntimeGatewaySecret` @ `services/fabric/internal/fabric/workspace_runtime.go:480`; `writeJSON` @ `services/fabric/internal/http/server.go:1172`

### 17. GET /fabric/workspace-runtimes/{workspaceId}/observation

注册：`services/fabric/internal/http/server.go:681`；`newFabricMux`。

实际调用：`Context`, `ObserveWorkspaceRuntime`, `PathValue`, `TrimSpace`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	observation := service.ObserveWorkspaceRuntime(r.Context(), strings.TrimSpace(r.PathValue("workspaceId")))
	writeJSON(w, http.StatusOK, observation)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `workspaceId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, observation)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`ObserveWorkspaceRuntime` @ `services/fabric/internal/fabric/workspace_runtime.go:476`; `writeJSON` @ `services/fabric/internal/http/server.go:1172`

### 18. GET /fabric/workspace-runtimes/{workspaceId}/status

注册：`services/fabric/internal/http/server.go:677`；`newFabricMux`。

实际调用：`Context`, `PathValue`, `TrimSpace`, `WorkspaceRuntimeStatus`, `writeResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	runtime, err := service.WorkspaceRuntimeStatus(r.Context(), strings.TrimSpace(r.PathValue("workspaceId")))
	writeResult(w, runtime, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `workspaceId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeResult(w, runtime, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`WorkspaceRuntimeStatus` @ `services/fabric/internal/fabric/local_docker_runtime.go:1045`; `WorkspaceRuntimeStatus` @ `services/fabric/internal/fabric/tencent_provider_runtime.go:437`; `WorkspaceRuntimeStatus` @ `services/fabric/internal/fabric/workspace_runtime.go:446`; `writeResult` @ `services/fabric/internal/http/server.go:1106`

### 19. GET /healthz

注册：`services/fabric/internal/http/server.go:49`；`newFabricMux`。

实际调用：`writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`writeJSON` @ `services/fabric/internal/http/server.go:1172`

### 20. POST /fabric/compute-allocations

注册：`services/fabric/internal/http/server.go:407`；`newFabricMux`。

实际调用：`Context`, `CreateComputeAllocation`, `decodeWrite`, `writeComputeAllocationResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.ComputeAllocationInput
	if !decodeWrite(w, r, &input.IdempotencyKey, &input) {
		return
	}
	allocation, err := service.CreateComputeAllocation(r.Context(), input)
	writeComputeAllocationResult(w, allocation, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `input` | `fabric.ComputeAllocationInput` | named variable; exact decoder in handler |

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`CreateComputeAllocation` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:2501`; `CreateComputeAllocation` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:832`; `CreateComputeAllocation` @ `services/fabric/internal/fabric/compute_allocation.go:85`; `CreateComputeAllocation` @ `services/fabric/internal/fabric/local_docker_provider.go:462`; `CreateComputeAllocation` @ `services/fabric/internal/fabric/tencent_provider_compute.go:73`; `decodeWrite` @ `services/fabric/internal/http/server.go:1054`; `writeComputeAllocationResult` @ `services/fabric/internal/http/server.go:1122`

### 21. POST /fabric/compute-allocations/{id}/destroy

注册：`services/fabric/internal/http/server.go:433`；`newFabricMux`。

实际调用：`Context`, `DestroyComputeAllocation`, `Get`, `PathValue`, `TrimSpace`, `writeError`, `writeResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Idempotency-Key") == "" {
		writeError(w, http.StatusBadRequest, "missing Idempotency-Key")
		return
	}
	allocation, err := service.DestroyComputeAllocation(r.Context(), strings.TrimSpace(r.PathValue("id")))
	writeResult(w, allocation, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `Idempotency-Key` | `string` | header |
| `id` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeResult(w, allocation, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`DestroyComputeAllocation` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:3525`; `DestroyComputeAllocation` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:849`; `DestroyComputeAllocation` @ `services/fabric/internal/fabric/compute_allocation.go:675`; `DestroyComputeAllocation` @ `services/fabric/internal/fabric/local_docker_provider.go:613`; `Get` @ `services/fabric/internal/fabric/operation_capability_ports.go:108`; `Get` @ `services/fabric/internal/fabric/operation_store.go:1229`; `Get` @ `services/fabric/internal/fabric/operation_store.go:175`; `Get` @ `services/fabric/internal/fabric/provider_mutation_store_port.go:30`; `DestroyComputeAllocation` @ `services/fabric/internal/fabric/tencent_provider_compute.go:958`; `writeResult` @ `services/fabric/internal/http/server.go:1106`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 22. POST /fabric/compute-allocations/{id}/renew

注册：`services/fabric/internal/http/server.go:441`；`newFabricMux`。

实际调用：`Context`, `Get`, `PathValue`, `RenewComputeAllocation`, `TrimSpace`, `writeError`, `writeResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "missing Idempotency-Key")
		return
	}
	allocation, err := service.RenewComputeAllocation(r.Context(), strings.TrimSpace(r.PathValue("id")), key)
	writeResult(w, allocation, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `Idempotency-Key` | `string` | header |
| `id` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeResult(w, allocation, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`RenewComputeAllocation` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:2353`; `RenewComputeAllocation` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:896`; `RenewComputeAllocation` @ `services/fabric/internal/fabric/compute_allocation.go:613`; `RenewComputeAllocation` @ `services/fabric/internal/fabric/local_docker_provider.go:604`; `Get` @ `services/fabric/internal/fabric/operation_capability_ports.go:108`; `Get` @ `services/fabric/internal/fabric/operation_store.go:1229`; `Get` @ `services/fabric/internal/fabric/operation_store.go:175`; `Get` @ `services/fabric/internal/fabric/provider_mutation_store_port.go:30`; `RenewComputeAllocation` @ `services/fabric/internal/fabric/tencent_provider_compute.go:912`; `writeResult` @ `services/fabric/internal/http/server.go:1106`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 23. POST /fabric/compute-claim-recovery/identity-evidence

注册：`services/fabric/internal/http/server.go:256`；`newFabricMux`。

实际调用：`ComputeClaimRecoveryIdentityEvidence`, `Context`, `Decode`, `Error`, `NewDecoder`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.ComputeClaimRecoveryClaimInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	input.IdempotencyKey = input.LaunchOperationID + ":compute"
	evidence, err := service.ComputeClaimRecoveryIdentityEvidence(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, evidence)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `input` | `fabric.ComputeClaimRecoveryClaimInput` | named variable; exact decoder in handler |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, evidence)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:47`; `ComputeClaimRecoveryIdentityEvidence` @ `services/fabric/internal/fabric/tencent_operator_identity_evidence.go:43`; `writeJSON` @ `services/fabric/internal/http/server.go:1172`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 24. POST /fabric/compute-pool-head/terminalization

注册：`services/fabric/internal/http/server.go:217`；`newFabricMux`。

实际调用：`Context`, `Decode`, `DisallowUnknownFields`, `Get`, `Is`, `NewDecoder`, `TerminalizeComputePoolHead`, `TrimSpace`, `writeComputePoolHeadTerminalizationResult`, `writeError`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" || idempotencyKey != strings.TrimSpace(idempotencyKey) {
		writeError(w, http.StatusBadRequest, "missing Idempotency-Key")
		return
	}
	var input fabric.ComputePoolHeadTerminalizationInput
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	input.IdempotencyKey = idempotencyKey
	result, err := service.TerminalizeComputePoolHead(r.Context(), input)
	writeComputePoolHeadTerminalizationResult(w, result, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `Idempotency-Key` | `string` | header |
| `input` | `fabric.ComputePoolHeadTerminalizationInput` | named variable; exact decoder in handler |

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Get` @ `services/fabric/internal/fabric/operation_capability_ports.go:108`; `Get` @ `services/fabric/internal/fabric/operation_store.go:1229`; `Get` @ `services/fabric/internal/fabric/operation_store.go:175`; `Get` @ `services/fabric/internal/fabric/provider_mutation_store_port.go:30`; `TerminalizeComputePoolHead` @ `services/fabric/internal/fabric/tencent_operator_pool_head.go:208`; `writeComputePoolHeadTerminalizationResult` @ `services/fabric/internal/http/server.go:1130`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 25. POST /fabric/gateway-secrets

注册：`services/fabric/internal/http/server.go:732`；`newFabricMux`。

实际调用：`Context`, `UpsertGatewaySecret`, `decodeWrite`, `writeResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.GatewaySecretInput
	if !decodeWrite(w, r, &input.IdempotencyKey, &input) {
		return
	}
	secret, err := service.UpsertGatewaySecret(r.Context(), input)
	writeResult(w, secret, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `input` | `fabric.GatewaySecretInput` | named variable; exact decoder in handler |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeResult(w, secret, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`UpsertGatewaySecret` @ `services/fabric/internal/fabric/local_docker_runtime.go:86`; `UpsertGatewaySecret` @ `services/fabric/internal/fabric/tencent_provider.go:811`; `UpsertGatewaySecret` @ `services/fabric/internal/fabric/workspace_runtime.go:569`; `decodeWrite` @ `services/fabric/internal/http/server.go:1054`; `writeResult` @ `services/fabric/internal/http/server.go:1106`

### 26. POST /fabric/jobs

注册：`services/fabric/internal/http/server.go:345`；`newFabricMux`。

实际调用：`Context`, `CreateJob`, `decodeWrite`, `writeJobResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.JobInput
	if !decodeWrite(w, r, &input.IdempotencyKey, &input) {
		return
	}
	job, err := service.CreateJob(r.Context(), input)
	writeJobResult(w, http.StatusAccepted, job, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `input` | `fabric.JobInput` | named variable; exact decoder in handler |

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`CreateJob` @ `services/fabric/internal/fabric/jobs.go:12`; `decodeWrite` @ `services/fabric/internal/http/server.go:1054`; `writeJobResult` @ `services/fabric/internal/http/server.go:1155`

### 27. POST /fabric/jobs/{id}/cancel

注册：`services/fabric/internal/http/server.go:357`；`newFabricMux`。

实际调用：`CancelJob`, `Context`, `Get`, `PathValue`, `TrimSpace`, `writeError`, `writeJobResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "missing Idempotency-Key")
		return
	}
	job, err := service.CancelJob(r.Context(), strings.TrimSpace(r.PathValue("id")), idempotencyKey)
	writeJobResult(w, http.StatusAccepted, job, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `Idempotency-Key` | `string` | header |
| `id` | `string` | path |

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`CancelJob` @ `services/fabric/internal/fabric/jobs.go:83`; `Get` @ `services/fabric/internal/fabric/operation_capability_ports.go:108`; `Get` @ `services/fabric/internal/fabric/operation_store.go:1229`; `Get` @ `services/fabric/internal/fabric/operation_store.go:175`; `Get` @ `services/fabric/internal/fabric/provider_mutation_store_port.go:30`; `writeJobResult` @ `services/fabric/internal/http/server.go:1155`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 28. POST /fabric/jobs/{id}/claim

注册：`services/fabric/internal/http/server.go:366`；`newFabricMux`。

实际调用：`ClaimJob`, `Context`, `PathValue`, `TrimSpace`, `decodeWrite`, `writeJobResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.JobClaimInput
	if !decodeWrite(w, r, &input.IdempotencyKey, &input) {
		return
	}
	job, err := service.ClaimJob(r.Context(), strings.TrimSpace(r.PathValue("id")), input)
	writeJobResult(w, http.StatusAccepted, job, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `id` | `string` | path |
| `input` | `fabric.JobClaimInput` | named variable; exact decoder in handler |

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`ClaimJob` @ `services/fabric/internal/fabric/jobs.go:113`; `decodeWrite` @ `services/fabric/internal/http/server.go:1054`; `writeJobResult` @ `services/fabric/internal/http/server.go:1155`

### 29. POST /fabric/jobs/{id}/complete

注册：`services/fabric/internal/http/server.go:382`；`newFabricMux`。

实际调用：`CompleteJob`, `Context`, `PathValue`, `TrimSpace`, `decodeWrite`, `writeJobResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.JobCompleteInput
	if !decodeWrite(w, r, &input.IdempotencyKey, &input) {
		return
	}
	job, err := service.CompleteJob(r.Context(), strings.TrimSpace(r.PathValue("id")), input)
	writeJobResult(w, http.StatusAccepted, job, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `id` | `string` | path |
| `input` | `fabric.JobCompleteInput` | named variable; exact decoder in handler |

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`CompleteJob` @ `services/fabric/internal/fabric/jobs.go:185`; `decodeWrite` @ `services/fabric/internal/http/server.go:1054`; `writeJobResult` @ `services/fabric/internal/http/server.go:1155`

### 30. POST /fabric/jobs/{id}/fail

注册：`services/fabric/internal/http/server.go:390`；`newFabricMux`。

实际调用：`Context`, `FailJob`, `PathValue`, `TrimSpace`, `decodeWrite`, `writeJobResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.JobFailInput
	if !decodeWrite(w, r, &input.IdempotencyKey, &input) {
		return
	}
	job, err := service.FailJob(r.Context(), strings.TrimSpace(r.PathValue("id")), input)
	writeJobResult(w, http.StatusAccepted, job, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `id` | `string` | path |
| `input` | `fabric.JobFailInput` | named variable; exact decoder in handler |

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`FailJob` @ `services/fabric/internal/fabric/jobs.go:213`; `decodeWrite` @ `services/fabric/internal/http/server.go:1054`; `writeJobResult` @ `services/fabric/internal/http/server.go:1155`

### 31. POST /fabric/jobs/{id}/heartbeat

注册：`services/fabric/internal/http/server.go:374`；`newFabricMux`。

实际调用：`Context`, `HeartbeatJob`, `PathValue`, `TrimSpace`, `decodeWrite`, `writeJobResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.JobHeartbeatInput
	if !decodeWrite(w, r, &input.IdempotencyKey, &input) {
		return
	}
	job, err := service.HeartbeatJob(r.Context(), strings.TrimSpace(r.PathValue("id")), input)
	writeJobResult(w, http.StatusAccepted, job, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `id` | `string` | path |
| `input` | `fabric.JobHeartbeatInput` | named variable; exact decoder in handler |

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`HeartbeatJob` @ `services/fabric/internal/fabric/jobs.go:148`; `decodeWrite` @ `services/fabric/internal/http/server.go:1054`; `writeJobResult` @ `services/fabric/internal/http/server.go:1155`

### 32. POST /fabric/jobs/{id}/retry

注册：`services/fabric/internal/http/server.go:398`；`newFabricMux`。

实际调用：`Context`, `Get`, `PathValue`, `RetryJob`, `TrimSpace`, `writeError`, `writeJobResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "missing Idempotency-Key")
		return
	}
	job, err := service.RetryJob(r.Context(), strings.TrimSpace(r.PathValue("id")), idempotencyKey)
	writeJobResult(w, http.StatusAccepted, job, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `Idempotency-Key` | `string` | header |
| `id` | `string` | path |

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`RetryJob` @ `services/fabric/internal/fabric/jobs.go:236`; `Get` @ `services/fabric/internal/fabric/operation_capability_ports.go:108`; `Get` @ `services/fabric/internal/fabric/operation_store.go:1229`; `Get` @ `services/fabric/internal/fabric/operation_store.go:175`; `Get` @ `services/fabric/internal/fabric/provider_mutation_store_port.go:30`; `writeJobResult` @ `services/fabric/internal/http/server.go:1155`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 33. POST /fabric/monthly-preflight

注册：`services/fabric/internal/http/server.go:160`；`newFabricMux`。

实际调用：`Context`, `Decode`, `Error`, `Is`, `MonthlyPreflight`, `NewDecoder`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.MonthlyPreflightInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	result, err := service.MonthlyPreflight(r.Context(), input)
	if errors.Is(err, fabric.ErrInvalidMonthlyPreflight) {
		writeError(w, http.StatusBadRequest, fabric.ErrInvalidMonthlyPreflight.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, fabric.ErrMonthlyPreflightUnavailable.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `input` | `fabric.MonthlyPreflightInput` | named variable; exact decoder in handler |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, result)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:47`; `MonthlyPreflight` @ `services/fabric/internal/fabric/local_docker_provider.go:309`; `MonthlyPreflight` @ `services/fabric/internal/fabric/service.go:123`; `MonthlyPreflight` @ `services/fabric/internal/fabric/tencent_provider.go:418`; `writeJSON` @ `services/fabric/internal/http/server.go:1172`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 34. POST /fabric/provider-facts/batch

注册：`services/fabric/internal/http/server.go:270`；`newFabricMux`。

实际调用：`Context`, `Decode`, `Error`, `NewDecoder`, `ProviderFactsBatch`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.ProviderFactsBatchInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	result, err := service.ProviderFactsBatch(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `input` | `fabric.ProviderFactsBatchInput` | named variable; exact decoder in handler |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, result)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:47`; `ProviderFactsBatch` @ `services/fabric/internal/fabric/provider_facts.go:13`; `writeJSON` @ `services/fabric/internal/http/server.go:1172`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 35. POST /fabric/storage-attachments

注册：`services/fabric/internal/http/server.go:495`；`newFabricMux`。

实际调用：`Context`, `CreateStorageAttachment`, `GetComputeAllocation`, `GetStorageVolume`, `decodeWrite`, `writeError`, `writeResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input struct {
		AccountID	string	`json:"accountId"`
		WorkspaceID	string	`json:"workspaceId"`
		ComputeID	string	`json:"computeId"`
		VolumeID	string	`json:"volumeId"`
		IdempotencyKey	string	`json:"-"`
	}
	if !decodeWrite(w, r, &input.IdempotencyKey, &input) {
		return
	}
	compute, computeOK := service.GetComputeAllocation(r.Context(), input.ComputeID)
	volume, volumeOK := service.GetStorageVolume(r.Context(), input.VolumeID)
	if !computeOK || !volumeOK || compute.AccountID != input.AccountID || volume.AccountID != input.AccountID ||
		compute.WorkspaceID != input.WorkspaceID || volume.WorkspaceID != input.WorkspaceID {
		writeError(w, http.StatusBadRequest, "storage_attachment_source_identity_mismatch")
		return
	}
	attachment, err := service.CreateStorageAttachment(r.Context(), fabric.StorageAttachmentInput{
		WorkspaceID:	input.WorkspaceID, ComputeID: input.ComputeID, VolumeID: input.VolumeID, IdempotencyKey: input.IdempotencyKey,
	})
	writeResult(w, attachment, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `input` | `struct {<br>	AccountID	string	`json:"accountId"`<br>	WorkspaceID	string	`json:"workspaceId"`<br>	ComputeID	string	`json:"computeId"`<br>	VolumeID	string	`json:"volumeId"`<br>	IdempotencyKey	string	`json:"-"`<br>}` | named variable; exact decoder in handler |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeResult(w, attachment, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`GetComputeAllocation` @ `services/fabric/internal/fabric/compute_allocation.go:512`; `CreateStorageAttachment` @ `services/fabric/internal/fabric/local_docker_provider.go:801`; `GetStorageVolume` @ `services/fabric/internal/fabric/storage.go:176`; `CreateStorageAttachment` @ `services/fabric/internal/fabric/storage.go:576`; `CreateStorageAttachment` @ `services/fabric/internal/fabric/tencent_provider.go:1086`; `decodeWrite` @ `services/fabric/internal/http/server.go:1054`; `writeResult` @ `services/fabric/internal/http/server.go:1106`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 36. POST /fabric/storage-attachments/{id}/detach

注册：`services/fabric/internal/http/server.go:518`；`newFabricMux`。

实际调用：`Context`, `DetachStorageAttachment`, `Get`, `PathValue`, `TrimSpace`, `writeError`, `writeResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Idempotency-Key") == "" {
		writeError(w, http.StatusBadRequest, "missing Idempotency-Key")
		return
	}
	attachment, err := service.DetachStorageAttachment(r.Context(), strings.TrimSpace(r.PathValue("id")))
	writeResult(w, attachment, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `Idempotency-Key` | `string` | header |
| `id` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeResult(w, attachment, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`DetachStorageAttachment` @ `services/fabric/internal/fabric/local_docker_provider.go:845`; `Get` @ `services/fabric/internal/fabric/operation_capability_ports.go:108`; `Get` @ `services/fabric/internal/fabric/operation_store.go:1229`; `Get` @ `services/fabric/internal/fabric/operation_store.go:175`; `Get` @ `services/fabric/internal/fabric/provider_mutation_store_port.go:30`; `DetachStorageAttachment` @ `services/fabric/internal/fabric/storage.go:670`; `DetachStorageAttachment` @ `services/fabric/internal/fabric/tencent_provider.go:1179`; `writeResult` @ `services/fabric/internal/http/server.go:1106`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 37. POST /fabric/storage-volumes

注册：`services/fabric/internal/http/server.go:450`；`newFabricMux`。

实际调用：`Context`, `CreateStorageVolume`, `decodeWrite`, `writeResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.StorageVolumeInput
	if !decodeWrite(w, r, &input.IdempotencyKey, &input) {
		return
	}
	volume, err := service.CreateStorageVolume(r.Context(), input)
	writeResult(w, volume, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `input` | `fabric.StorageVolumeInput` | named variable; exact decoder in handler |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeResult(w, volume, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`CreateStorageVolume` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:1826`; `CreateStorageVolume` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:876`; `CreateStorageVolume` @ `services/fabric/internal/fabric/local_docker_provider.go:654`; `CreateStorageVolume` @ `services/fabric/internal/fabric/storage.go:74`; `CreateStorageVolume` @ `services/fabric/internal/fabric/tencent_provider_storage.go:31`; `decodeWrite` @ `services/fabric/internal/http/server.go:1054`; `writeResult` @ `services/fabric/internal/http/server.go:1106`

### 38. POST /fabric/storage-volumes/{id}/destroy

注册：`services/fabric/internal/http/server.go:479`；`newFabricMux`。

实际调用：`Context`, `DestroyStorageVolume`, `Get`, `Is`, `PathValue`, `TrimSpace`, `writeError`, `writeJSON`, `writeResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Idempotency-Key") == "" {
		writeError(w, http.StatusBadRequest, "missing Idempotency-Key")
		return
	}
	volume, err := service.DestroyStorageVolume(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if errors.Is(err, fabric.ErrWorkspaceLaunchPending) && volume.DestroyState != "" {

		writeJSON(w, http.StatusAccepted, volume)
		return
	}
	writeResult(w, volume, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `Idempotency-Key` | `string` | header |
| `id` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusAccepted, volume)`
- `writeResult(w, volume, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`DestroyStorageVolume` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:2153`; `DestroyStorageVolume` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:888`; `DestroyStorageVolume` @ `services/fabric/internal/fabric/local_docker_provider.go:757`; `Get` @ `services/fabric/internal/fabric/operation_capability_ports.go:108`; `Get` @ `services/fabric/internal/fabric/operation_store.go:1229`; `Get` @ `services/fabric/internal/fabric/operation_store.go:175`; `Get` @ `services/fabric/internal/fabric/provider_mutation_store_port.go:30`; `DestroyStorageVolume` @ `services/fabric/internal/fabric/storage.go:222`; `DestroyStorageVolume` @ `services/fabric/internal/fabric/tencent_provider_storage.go:579`; `writeResult` @ `services/fabric/internal/http/server.go:1106`; `writeJSON` @ `services/fabric/internal/http/server.go:1172`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 39. POST /fabric/storage-volumes/{id}/renew

注册：`services/fabric/internal/http/server.go:470`；`newFabricMux`。

实际调用：`Context`, `Get`, `PathValue`, `RenewStorageVolume`, `TrimSpace`, `writeError`, `writeResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "missing Idempotency-Key")
		return
	}
	volume, err := service.RenewStorageVolume(r.Context(), strings.TrimSpace(r.PathValue("id")), key)
	writeResult(w, volume, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `Idempotency-Key` | `string` | header |
| `id` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeResult(w, volume, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`RenewStorageVolume` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:2052`; `RenewStorageVolume` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:892`; `RenewStorageVolume` @ `services/fabric/internal/fabric/local_docker_provider.go:748`; `Get` @ `services/fabric/internal/fabric/operation_capability_ports.go:108`; `Get` @ `services/fabric/internal/fabric/operation_store.go:1229`; `Get` @ `services/fabric/internal/fabric/operation_store.go:175`; `Get` @ `services/fabric/internal/fabric/provider_mutation_store_port.go:30`; `RenewStorageVolume` @ `services/fabric/internal/fabric/storage.go:514`; `RenewStorageVolume` @ `services/fabric/internal/fabric/tencent_provider_storage.go:784`; `writeResult` @ `services/fabric/internal/http/server.go:1106`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 40. POST /fabric/workspace-application-runtimes

注册：`services/fabric/internal/http/server.go:595`；`newFabricMux`。

实际调用：`Context`, `CreateWorkspaceApplicationRuntime`, `Error`, `Is`, `decodeWrite`, `writeError`, `writeJSON`, `writeResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.WorkspaceApplicationRuntimeInput
	if !decodeWrite(w, r, &input.IdempotencyKey, &input) {
		return
	}
	observation, err := service.CreateWorkspaceApplicationRuntime(r.Context(), input)
	if errors.Is(err, fabric.ErrWorkspaceApplicationRuntimeInputInvalid) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, fabric.ErrWorkspaceLaunchPending) {

		writeJSON(w, http.StatusAccepted, observation)
		return
	}
	writeResult(w, observation, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `input` | `fabric.WorkspaceApplicationRuntimeInput` | named variable; exact decoder in handler |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusAccepted, observation)`
- `writeResult(w, observation, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:47`; `CreateWorkspaceApplicationRuntime` @ `services/fabric/internal/fabric/workspace_application_runtime.go:81`; `decodeWrite` @ `services/fabric/internal/http/server.go:1054`; `writeResult` @ `services/fabric/internal/http/server.go:1106`; `writeJSON` @ `services/fabric/internal/http/server.go:1172`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 41. POST /fabric/workspace-application-runtimes/{workspaceId}/credentials

注册：`services/fabric/internal/http/server.go:631`；`newFabricMux`。

实际调用：`Context`, `Header`, `PathValue`, `ReadWorkspaceApplicationRuntimeCredentials`, `ReadWorkspaceApplicationRuntimeLifecycle`, `Set`, `SetWorkspaceApplicationRuntimeLifecycle`, `TrimSpace`, `decodeWrite`, `writeError`, `writeResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.WorkspaceApplicationRuntimeLifecycleInput
	if !decodeWrite(w, r, &input.IdempotencyKey, &input) {
		return
	}
	if input.WorkspaceID != strings.TrimSpace(r.PathValue("workspaceId")) {
		writeError(w, http.StatusBadRequest, "workspace_application_runtime_identity_required")
		return
	}
	if endpoint == "credentials" {
		w.Header().Set("Cache-Control", "no-store")
		result, err := service.ReadWorkspaceApplicationRuntimeCredentials(r.Context(), input)
		writeResult(w, result, err)
		return
	}
	var result fabric.WorkspaceApplicationRuntimeLifecycleResult
	var err error
	if endpoint == "lifecycle" {
		result, err = service.SetWorkspaceApplicationRuntimeLifecycle(r.Context(), input)
	} else {
		result, err = service.ReadWorkspaceApplicationRuntimeLifecycle(r.Context(), input)
	}
	writeResult(w, result, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `input` | `fabric.WorkspaceApplicationRuntimeLifecycleInput` | named variable; exact decoder in handler |
| `workspaceId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeResult(w, result, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`ReadWorkspaceApplicationRuntimeCredentials` @ `services/fabric/internal/fabric/local_docker_application_configuration.go:119`; `ReadWorkspaceApplicationRuntimeLifecycle` @ `services/fabric/internal/fabric/local_docker_application_lifecycle.go:12`; `SetWorkspaceApplicationRuntimeLifecycle` @ `services/fabric/internal/fabric/local_docker_application_lifecycle.go:37`; `ReadWorkspaceApplicationRuntimeCredentials` @ `services/fabric/internal/fabric/tencent_provider_application_configuration.go:434`; `ReadWorkspaceApplicationRuntimeLifecycle` @ `services/fabric/internal/fabric/tencent_provider_application_lifecycle.go:11`; `SetWorkspaceApplicationRuntimeLifecycle` @ `services/fabric/internal/fabric/tencent_provider_application_lifecycle.go:57`; `ReadWorkspaceApplicationRuntimeCredentials` @ `services/fabric/internal/fabric/workspace_application_lifecycle.go:231`; `SetWorkspaceApplicationRuntimeLifecycle` @ `services/fabric/internal/fabric/workspace_application_lifecycle.go:62`; `ReadWorkspaceApplicationRuntimeLifecycle` @ `services/fabric/internal/fabric/workspace_application_lifecycle.go:65`; `decodeWrite` @ `services/fabric/internal/http/server.go:1054`; `writeResult` @ `services/fabric/internal/http/server.go:1106`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 42. POST /fabric/workspace-application-runtimes/{workspaceId}/gateway-secret-cleanup

注册：`services/fabric/internal/http/server.go:559`；`newFabricMux`。

实际调用：`Context`, `PathValue`, `RemoveWorkspaceApplicationGatewaySecret`, `TrimSpace`, `decodeWrite`, `writeError`, `writeJSON`, `writeResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.WorkspaceApplicationGatewaySecretCleanupInput
	if !decodeWrite(w, r, &input.IdempotencyKey, &input) {
		return
	}
	if input.WorkspaceID != strings.TrimSpace(r.PathValue("workspaceId")) {
		writeError(w, http.StatusBadRequest, "workspace_application_runtime_identity_required")
		return
	}
	err := service.RemoveWorkspaceApplicationGatewaySecret(r.Context(), input)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		State string `json:"state"`
	}{"absent"})
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `input` | `fabric.WorkspaceApplicationGatewaySecretCleanupInput` | named variable; exact decoder in handler |
| `workspaceId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, struct {<br>	State string `json:"state"`<br>}{"absent"})`
- `writeResult(w, nil, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`RemoveWorkspaceApplicationGatewaySecret` @ `services/fabric/internal/fabric/workspace_application_secret_cleanup.go:109`; `RemoveWorkspaceApplicationGatewaySecret` @ `services/fabric/internal/fabric/workspace_application_secret_cleanup.go:15`; `RemoveWorkspaceApplicationGatewaySecret` @ `services/fabric/internal/fabric/workspace_application_secret_cleanup.go:151`; `decodeWrite` @ `services/fabric/internal/http/server.go:1054`; `writeResult` @ `services/fabric/internal/http/server.go:1106`; `writeJSON` @ `services/fabric/internal/http/server.go:1172`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 43. POST /fabric/workspace-application-runtimes/{workspaceId}/lifecycle

注册：`services/fabric/internal/http/server.go:631`；`newFabricMux`。

实际调用：`Context`, `Header`, `PathValue`, `ReadWorkspaceApplicationRuntimeCredentials`, `ReadWorkspaceApplicationRuntimeLifecycle`, `Set`, `SetWorkspaceApplicationRuntimeLifecycle`, `TrimSpace`, `decodeWrite`, `writeError`, `writeResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.WorkspaceApplicationRuntimeLifecycleInput
	if !decodeWrite(w, r, &input.IdempotencyKey, &input) {
		return
	}
	if input.WorkspaceID != strings.TrimSpace(r.PathValue("workspaceId")) {
		writeError(w, http.StatusBadRequest, "workspace_application_runtime_identity_required")
		return
	}
	if endpoint == "credentials" {
		w.Header().Set("Cache-Control", "no-store")
		result, err := service.ReadWorkspaceApplicationRuntimeCredentials(r.Context(), input)
		writeResult(w, result, err)
		return
	}
	var result fabric.WorkspaceApplicationRuntimeLifecycleResult
	var err error
	if endpoint == "lifecycle" {
		result, err = service.SetWorkspaceApplicationRuntimeLifecycle(r.Context(), input)
	} else {
		result, err = service.ReadWorkspaceApplicationRuntimeLifecycle(r.Context(), input)
	}
	writeResult(w, result, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `input` | `fabric.WorkspaceApplicationRuntimeLifecycleInput` | named variable; exact decoder in handler |
| `workspaceId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeResult(w, result, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`ReadWorkspaceApplicationRuntimeCredentials` @ `services/fabric/internal/fabric/local_docker_application_configuration.go:119`; `ReadWorkspaceApplicationRuntimeLifecycle` @ `services/fabric/internal/fabric/local_docker_application_lifecycle.go:12`; `SetWorkspaceApplicationRuntimeLifecycle` @ `services/fabric/internal/fabric/local_docker_application_lifecycle.go:37`; `ReadWorkspaceApplicationRuntimeCredentials` @ `services/fabric/internal/fabric/tencent_provider_application_configuration.go:434`; `ReadWorkspaceApplicationRuntimeLifecycle` @ `services/fabric/internal/fabric/tencent_provider_application_lifecycle.go:11`; `SetWorkspaceApplicationRuntimeLifecycle` @ `services/fabric/internal/fabric/tencent_provider_application_lifecycle.go:57`; `ReadWorkspaceApplicationRuntimeCredentials` @ `services/fabric/internal/fabric/workspace_application_lifecycle.go:231`; `SetWorkspaceApplicationRuntimeLifecycle` @ `services/fabric/internal/fabric/workspace_application_lifecycle.go:62`; `ReadWorkspaceApplicationRuntimeLifecycle` @ `services/fabric/internal/fabric/workspace_application_lifecycle.go:65`; `decodeWrite` @ `services/fabric/internal/http/server.go:1054`; `writeResult` @ `services/fabric/internal/http/server.go:1106`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 44. POST /fabric/workspace-application-runtimes/{workspaceId}/lifecycle-readback

注册：`services/fabric/internal/http/server.go:631`；`newFabricMux`。

实际调用：`Context`, `Header`, `PathValue`, `ReadWorkspaceApplicationRuntimeCredentials`, `ReadWorkspaceApplicationRuntimeLifecycle`, `Set`, `SetWorkspaceApplicationRuntimeLifecycle`, `TrimSpace`, `decodeWrite`, `writeError`, `writeResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.WorkspaceApplicationRuntimeLifecycleInput
	if !decodeWrite(w, r, &input.IdempotencyKey, &input) {
		return
	}
	if input.WorkspaceID != strings.TrimSpace(r.PathValue("workspaceId")) {
		writeError(w, http.StatusBadRequest, "workspace_application_runtime_identity_required")
		return
	}
	if endpoint == "credentials" {
		w.Header().Set("Cache-Control", "no-store")
		result, err := service.ReadWorkspaceApplicationRuntimeCredentials(r.Context(), input)
		writeResult(w, result, err)
		return
	}
	var result fabric.WorkspaceApplicationRuntimeLifecycleResult
	var err error
	if endpoint == "lifecycle" {
		result, err = service.SetWorkspaceApplicationRuntimeLifecycle(r.Context(), input)
	} else {
		result, err = service.ReadWorkspaceApplicationRuntimeLifecycle(r.Context(), input)
	}
	writeResult(w, result, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `input` | `fabric.WorkspaceApplicationRuntimeLifecycleInput` | named variable; exact decoder in handler |
| `workspaceId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeResult(w, result, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`ReadWorkspaceApplicationRuntimeCredentials` @ `services/fabric/internal/fabric/local_docker_application_configuration.go:119`; `ReadWorkspaceApplicationRuntimeLifecycle` @ `services/fabric/internal/fabric/local_docker_application_lifecycle.go:12`; `SetWorkspaceApplicationRuntimeLifecycle` @ `services/fabric/internal/fabric/local_docker_application_lifecycle.go:37`; `ReadWorkspaceApplicationRuntimeCredentials` @ `services/fabric/internal/fabric/tencent_provider_application_configuration.go:434`; `ReadWorkspaceApplicationRuntimeLifecycle` @ `services/fabric/internal/fabric/tencent_provider_application_lifecycle.go:11`; `SetWorkspaceApplicationRuntimeLifecycle` @ `services/fabric/internal/fabric/tencent_provider_application_lifecycle.go:57`; `ReadWorkspaceApplicationRuntimeCredentials` @ `services/fabric/internal/fabric/workspace_application_lifecycle.go:231`; `SetWorkspaceApplicationRuntimeLifecycle` @ `services/fabric/internal/fabric/workspace_application_lifecycle.go:62`; `ReadWorkspaceApplicationRuntimeLifecycle` @ `services/fabric/internal/fabric/workspace_application_lifecycle.go:65`; `decodeWrite` @ `services/fabric/internal/http/server.go:1054`; `writeResult` @ `services/fabric/internal/http/server.go:1106`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 45. POST /fabric/workspace-application-runtimes/{workspaceId}/preflight

注册：`services/fabric/internal/http/server.go:577`；`newFabricMux`。

实际调用：`Context`, `Error`, `PathValue`, `PreflightWorkspaceApplicationRuntime`, `TrimSpace`, `decodeWrite`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.WorkspaceApplicationRuntimeInput
	if !decodeWrite(w, r, &input.IdempotencyKey, &input) {
		return
	}
	if input.WorkspaceID != strings.TrimSpace(r.PathValue("workspaceId")) {
		writeError(w, http.StatusBadRequest, "workspace_application_runtime_identity_required")
		return
	}
	err := service.PreflightWorkspaceApplicationRuntime(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Admitted bool `json:"admitted"`
	}{true})
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `input` | `fabric.WorkspaceApplicationRuntimeInput` | named variable; exact decoder in handler |
| `workspaceId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, struct {<br>	Admitted bool `json:"admitted"`<br>}{true})`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:47`; `PreflightWorkspaceApplicationRuntime` @ `services/fabric/internal/fabric/workspace_application_preflight.go:16`; `PreflightWorkspaceApplicationRuntime` @ `services/fabric/internal/fabric/workspace_application_preflight.go:39`; `PreflightWorkspaceApplicationRuntime` @ `services/fabric/internal/fabric/workspace_application_preflight.go:70`; `decodeWrite` @ `services/fabric/internal/http/server.go:1054`; `writeJSON` @ `services/fabric/internal/http/server.go:1172`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 46. POST /fabric/workspace-application-runtimes/{workspaceId}/readback

注册：`services/fabric/internal/http/server.go:613`；`newFabricMux`。

实际调用：`Context`, `Is`, `PathValue`, `TrimSpace`, `WorkspaceApplicationRuntimeReadback`, `decodeWrite`, `writeError`, `writeJSON`, `writeResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.WorkspaceApplicationRuntimeInput
	if !decodeWrite(w, r, &input.IdempotencyKey, &input) {
		return
	}
	if input.WorkspaceID != strings.TrimSpace(r.PathValue("workspaceId")) {
		writeError(w, http.StatusBadRequest, "workspace_application_runtime_identity_required")
		return
	}
	observation, err := service.WorkspaceApplicationRuntimeReadback(r.Context(), input)
	if errors.Is(err, fabric.ErrWorkspaceLaunchPending) {
		writeJSON(w, http.StatusAccepted, observation)
		return
	}
	writeResult(w, observation, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `input` | `fabric.WorkspaceApplicationRuntimeInput` | named variable; exact decoder in handler |
| `workspaceId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusAccepted, observation)`
- `writeResult(w, observation, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`WorkspaceApplicationRuntimeReadback` @ `services/fabric/internal/fabric/workspace_application_runtime.go:300`; `decodeWrite` @ `services/fabric/internal/http/server.go:1054`; `writeResult` @ `services/fabric/internal/http/server.go:1106`; `writeJSON` @ `services/fabric/internal/http/server.go:1172`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 47. POST /fabric/workspace-launches/closeout

注册：`services/fabric/internal/http/server.go:103`；`newFabricMux`。

实际调用：`CloseoutWorkspaceLaunch`, `Context`, `Decode`, `FreezeWorkspaceLaunch`, `Get`, `NewDecoder`, `ReadWorkspaceLaunchCloseout`, `writeError`, `writeWorkspaceLaunchResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.WorkspaceLaunchCloseoutInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	var result fabric.WorkspaceLaunchCloseoutResult
	var err error
	switch r.URL.Path {
	case "/fabric/workspace-launches/closeout/read":
		result, err = service.ReadWorkspaceLaunchCloseout(r.Context(), input)
	default:
		if input.IdempotencyKey == "" || r.Header.Get("Idempotency-Key") != input.IdempotencyKey {
			writeError(w, http.StatusBadRequest, "workspace launch closeout idempotency key mismatch")
			return
		}
		if r.URL.Path == "/fabric/workspace-launches/closeout/freeze" {
			result, err = service.FreezeWorkspaceLaunch(r.Context(), input)
		} else {
			result, err = service.CloseoutWorkspaceLaunch(r.Context(), input)
		}
	}
	writeWorkspaceLaunchResult(w, result, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `Idempotency-Key` | `string` | header |
| `input` | `fabric.WorkspaceLaunchCloseoutInput` | named variable; exact decoder in handler |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeWorkspaceLaunchResult(w, result, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Get` @ `services/fabric/internal/fabric/operation_capability_ports.go:108`; `Get` @ `services/fabric/internal/fabric/operation_store.go:1229`; `Get` @ `services/fabric/internal/fabric/operation_store.go:175`; `Get` @ `services/fabric/internal/fabric/provider_mutation_store_port.go:30`; `ReadWorkspaceLaunchCloseout` @ `services/fabric/internal/fabric/workspace_launch_closeout.go:60`; `FreezeWorkspaceLaunch` @ `services/fabric/internal/fabric/workspace_launch_closeout.go:70`; `CloseoutWorkspaceLaunch` @ `services/fabric/internal/fabric/workspace_launch_closeout.go:74`; `writeWorkspaceLaunchResult` @ `services/fabric/internal/http/server.go:1039`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 48. POST /fabric/workspace-launches/closeout/freeze

注册：`services/fabric/internal/http/server.go:103`；`newFabricMux`。

实际调用：`CloseoutWorkspaceLaunch`, `Context`, `Decode`, `FreezeWorkspaceLaunch`, `Get`, `NewDecoder`, `ReadWorkspaceLaunchCloseout`, `writeError`, `writeWorkspaceLaunchResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.WorkspaceLaunchCloseoutInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	var result fabric.WorkspaceLaunchCloseoutResult
	var err error
	switch r.URL.Path {
	case "/fabric/workspace-launches/closeout/read":
		result, err = service.ReadWorkspaceLaunchCloseout(r.Context(), input)
	default:
		if input.IdempotencyKey == "" || r.Header.Get("Idempotency-Key") != input.IdempotencyKey {
			writeError(w, http.StatusBadRequest, "workspace launch closeout idempotency key mismatch")
			return
		}
		if r.URL.Path == "/fabric/workspace-launches/closeout/freeze" {
			result, err = service.FreezeWorkspaceLaunch(r.Context(), input)
		} else {
			result, err = service.CloseoutWorkspaceLaunch(r.Context(), input)
		}
	}
	writeWorkspaceLaunchResult(w, result, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `Idempotency-Key` | `string` | header |
| `input` | `fabric.WorkspaceLaunchCloseoutInput` | named variable; exact decoder in handler |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeWorkspaceLaunchResult(w, result, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Get` @ `services/fabric/internal/fabric/operation_capability_ports.go:108`; `Get` @ `services/fabric/internal/fabric/operation_store.go:1229`; `Get` @ `services/fabric/internal/fabric/operation_store.go:175`; `Get` @ `services/fabric/internal/fabric/provider_mutation_store_port.go:30`; `ReadWorkspaceLaunchCloseout` @ `services/fabric/internal/fabric/workspace_launch_closeout.go:60`; `FreezeWorkspaceLaunch` @ `services/fabric/internal/fabric/workspace_launch_closeout.go:70`; `CloseoutWorkspaceLaunch` @ `services/fabric/internal/fabric/workspace_launch_closeout.go:74`; `writeWorkspaceLaunchResult` @ `services/fabric/internal/http/server.go:1039`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 49. POST /fabric/workspace-launches/closeout/read

注册：`services/fabric/internal/http/server.go:103`；`newFabricMux`。

实际调用：`CloseoutWorkspaceLaunch`, `Context`, `Decode`, `FreezeWorkspaceLaunch`, `Get`, `NewDecoder`, `ReadWorkspaceLaunchCloseout`, `writeError`, `writeWorkspaceLaunchResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.WorkspaceLaunchCloseoutInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	var result fabric.WorkspaceLaunchCloseoutResult
	var err error
	switch r.URL.Path {
	case "/fabric/workspace-launches/closeout/read":
		result, err = service.ReadWorkspaceLaunchCloseout(r.Context(), input)
	default:
		if input.IdempotencyKey == "" || r.Header.Get("Idempotency-Key") != input.IdempotencyKey {
			writeError(w, http.StatusBadRequest, "workspace launch closeout idempotency key mismatch")
			return
		}
		if r.URL.Path == "/fabric/workspace-launches/closeout/freeze" {
			result, err = service.FreezeWorkspaceLaunch(r.Context(), input)
		} else {
			result, err = service.CloseoutWorkspaceLaunch(r.Context(), input)
		}
	}
	writeWorkspaceLaunchResult(w, result, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `Idempotency-Key` | `string` | header |
| `input` | `fabric.WorkspaceLaunchCloseoutInput` | named variable; exact decoder in handler |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeWorkspaceLaunchResult(w, result, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Get` @ `services/fabric/internal/fabric/operation_capability_ports.go:108`; `Get` @ `services/fabric/internal/fabric/operation_store.go:1229`; `Get` @ `services/fabric/internal/fabric/operation_store.go:175`; `Get` @ `services/fabric/internal/fabric/provider_mutation_store_port.go:30`; `ReadWorkspaceLaunchCloseout` @ `services/fabric/internal/fabric/workspace_launch_closeout.go:60`; `FreezeWorkspaceLaunch` @ `services/fabric/internal/fabric/workspace_launch_closeout.go:70`; `CloseoutWorkspaceLaunch` @ `services/fabric/internal/fabric/workspace_launch_closeout.go:74`; `writeWorkspaceLaunchResult` @ `services/fabric/internal/http/server.go:1039`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 50. POST /fabric/workspace-launches/preflight

注册：`services/fabric/internal/http/server.go:52`；`newFabricMux`。

实际调用：`Context`, `Decode`, `NewDecoder`, `PreflightWorkspaceLaunch`, `writeError`, `writeWorkspaceLaunchResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.WorkspaceLaunchPreflightInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	result, err := service.PreflightWorkspaceLaunch(r.Context(), input)
	writeWorkspaceLaunchResult(w, result, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `input` | `fabric.WorkspaceLaunchPreflightInput` | named variable; exact decoder in handler |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeWorkspaceLaunchResult(w, result, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`PreflightWorkspaceLaunch` @ `services/fabric/internal/fabric/workspace_launch_stage.go:240`; `writeWorkspaceLaunchResult` @ `services/fabric/internal/http/server.go:1039`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 51. POST /fabric/workspace-launches/preflight/read

注册：`services/fabric/internal/http/server.go:61`；`newFabricMux`。

实际调用：`Context`, `Decode`, `NewDecoder`, `ReadWorkspaceLaunchPreflight`, `writeError`, `writeWorkspaceLaunchResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.WorkspaceLaunchPreflightReadInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	result, err := service.ReadWorkspaceLaunchPreflight(r.Context(), input)
	writeWorkspaceLaunchResult(w, result, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `input` | `fabric.WorkspaceLaunchPreflightReadInput` | named variable; exact decoder in handler |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeWorkspaceLaunchResult(w, result, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`ReadWorkspaceLaunchPreflight` @ `services/fabric/internal/fabric/workspace_launch_stage.go:353`; `writeWorkspaceLaunchResult` @ `services/fabric/internal/http/server.go:1039`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 52. POST /fabric/workspace-launches/stages/ensure

注册：`services/fabric/internal/http/server.go:88`；`newFabricMux`。

实际调用：`Context`, `Decode`, `EnsureWorkspaceLaunchStage`, `Get`, `NewDecoder`, `writeError`, `writeWorkspaceLaunchResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.WorkspaceLaunchStageInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if input.Binding.IdempotencyKey == "" || r.Header.Get("Idempotency-Key") != input.Binding.IdempotencyKey {
		writeError(w, http.StatusBadRequest, "workspace launch stage idempotency key mismatch")
		return
	}
	result, err := service.EnsureWorkspaceLaunchStage(r.Context(), input)
	writeWorkspaceLaunchResult(w, result, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `Idempotency-Key` | `string` | header |
| `input` | `fabric.WorkspaceLaunchStageInput` | named variable; exact decoder in handler |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeWorkspaceLaunchResult(w, result, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`EnsureWorkspaceLaunchStage` @ `services/fabric/internal/fabric/local_docker_workspace_launch.go:36`; `Get` @ `services/fabric/internal/fabric/operation_capability_ports.go:108`; `Get` @ `services/fabric/internal/fabric/operation_store.go:1229`; `Get` @ `services/fabric/internal/fabric/operation_store.go:175`; `Get` @ `services/fabric/internal/fabric/provider_mutation_store_port.go:30`; `EnsureWorkspaceLaunchStage` @ `services/fabric/internal/fabric/tencent_workspace_launch.go:44`; `EnsureWorkspaceLaunchStage` @ `services/fabric/internal/fabric/workspace_launch_stage.go:745`; `EnsureWorkspaceLaunchStage` @ `services/fabric/internal/fabric/workspace_launch_stage_engine.go:212`; `writeWorkspaceLaunchResult` @ `services/fabric/internal/http/server.go:1039`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 53. POST /fabric/workspace-launches/stages/observe

注册：`services/fabric/internal/http/server.go:79`；`newFabricMux`。

实际调用：`Context`, `Decode`, `NewDecoder`, `ObserveWorkspaceLaunchStage`, `writeError`, `writeWorkspaceLaunchResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.WorkspaceLaunchStageInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	result, err := service.ObserveWorkspaceLaunchStage(r.Context(), input)
	writeWorkspaceLaunchResult(w, result, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `input` | `fabric.WorkspaceLaunchStageInput` | named variable; exact decoder in handler |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeWorkspaceLaunchResult(w, result, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`ObserveWorkspaceLaunchStage` @ `services/fabric/internal/fabric/workspace_launch_stage.go:777`; `ObserveWorkspaceLaunchStage` @ `services/fabric/internal/fabric/workspace_launch_stage_engine.go:388`; `writeWorkspaceLaunchResult` @ `services/fabric/internal/http/server.go:1039`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 54. POST /fabric/workspace-launches/stages/read

注册：`services/fabric/internal/http/server.go:70`；`newFabricMux`。

实际调用：`Context`, `Decode`, `NewDecoder`, `ReadWorkspaceLaunchStage`, `writeError`, `writeWorkspaceLaunchResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.WorkspaceLaunchStageInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	result, err := service.ReadWorkspaceLaunchStage(r.Context(), input)
	writeWorkspaceLaunchResult(w, result, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `input` | `fabric.WorkspaceLaunchStageInput` | named variable; exact decoder in handler |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeWorkspaceLaunchResult(w, result, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`ReadWorkspaceLaunchStage` @ `services/fabric/internal/fabric/local_docker_workspace_launch.go:161`; `ReadWorkspaceLaunchStage` @ `services/fabric/internal/fabric/tencent_workspace_launch.go:216`; `ReadWorkspaceLaunchStage` @ `services/fabric/internal/fabric/workspace_launch_stage.go:761`; `ReadWorkspaceLaunchStage` @ `services/fabric/internal/fabric/workspace_launch_stage_engine.go:384`; `writeWorkspaceLaunchResult` @ `services/fabric/internal/http/server.go:1039`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 55. POST /fabric/workspace-runtimes

注册：`services/fabric/internal/http/server.go:526`；`newFabricMux`。

实际调用：`Context`, `CreateWorkspaceRuntime`, `decodeWrite`, `writeResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.WorkspaceRuntimeInput
	if !decodeWrite(w, r, &input.IdempotencyKey, &input) {
		return
	}
	runtime, err := service.CreateWorkspaceRuntime(r.Context(), input)
	writeResult(w, runtime, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `input` | `fabric.WorkspaceRuntimeInput` | named variable; exact decoder in handler |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeResult(w, runtime, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`CreateWorkspaceRuntime` @ `services/fabric/internal/fabric/local_docker_runtime.go:777`; `CreateWorkspaceRuntime` @ `services/fabric/internal/fabric/tencent_provider_runtime.go:23`; `CreateWorkspaceRuntime` @ `services/fabric/internal/fabric/workspace_runtime.go:74`; `decodeWrite` @ `services/fabric/internal/http/server.go:1054`; `writeResult` @ `services/fabric/internal/http/server.go:1106`

### 56. POST /fabric/workspace-runtimes/power

注册：`services/fabric/internal/http/server.go:129`；`newFabricMux`。

实际调用：`Context`, `Decode`, `Get`, `NewDecoder`, `ReadWorkspaceRuntimePower`, `SetWorkspaceRuntimePower`, `writeError`, `writeWorkspaceLaunchResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.WorkspaceRuntimePowerInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	var result fabric.WorkspaceRuntimePowerResult
	var err error
	if r.URL.Path == "/fabric/workspace-runtimes/power/read" {
		result, err = service.ReadWorkspaceRuntimePower(r.Context(), input)
	} else {
		if input.IdempotencyKey == "" || r.Header.Get("Idempotency-Key") != input.IdempotencyKey {
			writeError(w, http.StatusBadRequest, "workspace runtime power idempotency key mismatch")
			return
		}
		result, err = service.SetWorkspaceRuntimePower(r.Context(), input)
	}
	writeWorkspaceLaunchResult(w, result, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `Idempotency-Key` | `string` | header |
| `input` | `fabric.WorkspaceRuntimePowerInput` | named variable; exact decoder in handler |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeWorkspaceLaunchResult(w, result, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`ReadWorkspaceRuntimePower` @ `services/fabric/internal/fabric/local_docker_runtime_power.go:38`; `SetWorkspaceRuntimePower` @ `services/fabric/internal/fabric/local_docker_runtime_power.go:43`; `Get` @ `services/fabric/internal/fabric/operation_capability_ports.go:108`; `Get` @ `services/fabric/internal/fabric/operation_store.go:1229`; `Get` @ `services/fabric/internal/fabric/operation_store.go:175`; `Get` @ `services/fabric/internal/fabric/provider_mutation_store_port.go:30`; `ReadWorkspaceRuntimePower` @ `services/fabric/internal/fabric/tencent_runtime_power.go:64`; `SetWorkspaceRuntimePower` @ `services/fabric/internal/fabric/tencent_runtime_power.go:69`; `ReadWorkspaceRuntimePower` @ `services/fabric/internal/fabric/workspace_runtime_power.go:51`; `SetWorkspaceRuntimePower` @ `services/fabric/internal/fabric/workspace_runtime_power.go:55`; `writeWorkspaceLaunchResult` @ `services/fabric/internal/http/server.go:1039`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 57. POST /fabric/workspace-runtimes/power/read

注册：`services/fabric/internal/http/server.go:129`；`newFabricMux`。

实际调用：`Context`, `Decode`, `Get`, `NewDecoder`, `ReadWorkspaceRuntimePower`, `SetWorkspaceRuntimePower`, `writeError`, `writeWorkspaceLaunchResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.WorkspaceRuntimePowerInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	var result fabric.WorkspaceRuntimePowerResult
	var err error
	if r.URL.Path == "/fabric/workspace-runtimes/power/read" {
		result, err = service.ReadWorkspaceRuntimePower(r.Context(), input)
	} else {
		if input.IdempotencyKey == "" || r.Header.Get("Idempotency-Key") != input.IdempotencyKey {
			writeError(w, http.StatusBadRequest, "workspace runtime power idempotency key mismatch")
			return
		}
		result, err = service.SetWorkspaceRuntimePower(r.Context(), input)
	}
	writeWorkspaceLaunchResult(w, result, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `Idempotency-Key` | `string` | header |
| `input` | `fabric.WorkspaceRuntimePowerInput` | named variable; exact decoder in handler |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeWorkspaceLaunchResult(w, result, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`ReadWorkspaceRuntimePower` @ `services/fabric/internal/fabric/local_docker_runtime_power.go:38`; `SetWorkspaceRuntimePower` @ `services/fabric/internal/fabric/local_docker_runtime_power.go:43`; `Get` @ `services/fabric/internal/fabric/operation_capability_ports.go:108`; `Get` @ `services/fabric/internal/fabric/operation_store.go:1229`; `Get` @ `services/fabric/internal/fabric/operation_store.go:175`; `Get` @ `services/fabric/internal/fabric/provider_mutation_store_port.go:30`; `ReadWorkspaceRuntimePower` @ `services/fabric/internal/fabric/tencent_runtime_power.go:64`; `SetWorkspaceRuntimePower` @ `services/fabric/internal/fabric/tencent_runtime_power.go:69`; `ReadWorkspaceRuntimePower` @ `services/fabric/internal/fabric/workspace_runtime_power.go:51`; `SetWorkspaceRuntimePower` @ `services/fabric/internal/fabric/workspace_runtime_power.go:55`; `writeWorkspaceLaunchResult` @ `services/fabric/internal/http/server.go:1039`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 58. POST /fabric/workspace-runtimes/{workspaceId}/credentials/reveal

注册：`services/fabric/internal/http/server.go:689`；`newFabricMux`。

实际调用：`Context`, `Decode`, `Get`, `NewDecoder`, `PathValue`, `TrimSpace`, `WorkspaceRuntimeCredentials`, `writeError`, `writeResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input struct {
		AccountID	string	`json:"accountId"`
		WorkspaceID	string	`json:"workspaceId"`
	}
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "missing Idempotency-Key")
		return
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	workspaceID := strings.TrimSpace(r.PathValue("workspaceId"))
	accountID := strings.TrimSpace(input.AccountID)
	if accountID == "" || input.WorkspaceID != workspaceID {
		writeError(w, http.StatusBadRequest, "workspace_runtime_credential_input_required")
		return
	}
	runtime, err := service.WorkspaceRuntimeCredentials(r.Context(), accountID, workspaceID)
	writeResult(w, runtime, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `Idempotency-Key` | `string` | header |
| `input` | `struct {<br>	AccountID	string	`json:"accountId"`<br>	WorkspaceID	string	`json:"workspaceId"`<br>}` | named variable; exact decoder in handler |
| `workspaceId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeResult(w, runtime, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Get` @ `services/fabric/internal/fabric/operation_capability_ports.go:108`; `Get` @ `services/fabric/internal/fabric/operation_store.go:1229`; `Get` @ `services/fabric/internal/fabric/operation_store.go:175`; `Get` @ `services/fabric/internal/fabric/provider_mutation_store_port.go:30`; `WorkspaceRuntimeCredentials` @ `services/fabric/internal/fabric/workspace_runtime.go:552`; `writeResult` @ `services/fabric/internal/http/server.go:1106`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 59. POST /fabric/workspace-runtimes/{workspaceId}/destroy

注册：`services/fabric/internal/http/server.go:668`；`newFabricMux`。

实际调用：`Context`, `DestroyWorkspaceRuntime`, `Get`, `PathValue`, `TrimSpace`, `writeError`, `writeResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "missing Idempotency-Key")
		return
	}
	runtime, err := service.DestroyWorkspaceRuntime(r.Context(), strings.TrimSpace(r.PathValue("workspaceId")), key)
	writeResult(w, runtime, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `Idempotency-Key` | `string` | header |
| `workspaceId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeResult(w, runtime, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`DestroyWorkspaceRuntime` @ `services/fabric/internal/fabric/local_docker_runtime.go:1246`; `Get` @ `services/fabric/internal/fabric/operation_capability_ports.go:108`; `Get` @ `services/fabric/internal/fabric/operation_store.go:1229`; `Get` @ `services/fabric/internal/fabric/operation_store.go:175`; `Get` @ `services/fabric/internal/fabric/provider_mutation_store_port.go:30`; `DestroyWorkspaceRuntime` @ `services/fabric/internal/fabric/tencent_provider_runtime.go:258`; `DestroyWorkspaceRuntime` @ `services/fabric/internal/fabric/workspace_runtime.go:323`; `writeResult` @ `services/fabric/internal/http/server.go:1106`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 60. POST /fabric/workspace-runtimes/{workspaceId}/gateway-network/recover

注册：`services/fabric/internal/http/server.go:656`；`newFabricMux`。

实际调用：`Context`, `PathValue`, `RecoverWorkspaceRuntimeGatewayNetwork`, `TrimSpace`, `decodeWrite`, `writeError`, `writeResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.WorkspaceRuntimeGatewayNetworkRecoveryInput
	if !decodeWrite(w, r, &input.IdempotencyKey, &input) {
		return
	}
	if input.WorkspaceID != strings.TrimSpace(r.PathValue("workspaceId")) {
		writeError(w, http.StatusBadRequest, "workspace_runtime_gateway_network_recovery_input_invalid")
		return
	}
	result, err := service.RecoverWorkspaceRuntimeGatewayNetwork(r.Context(), input)
	writeResult(w, result, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `input` | `fabric.WorkspaceRuntimeGatewayNetworkRecoveryInput` | named variable; exact decoder in handler |
| `workspaceId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeResult(w, result, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`RecoverWorkspaceRuntimeGatewayNetwork` @ `services/fabric/internal/fabric/local_docker_runtime.go:382`; `RecoverWorkspaceRuntimeGatewayNetwork` @ `services/fabric/internal/fabric/workspace_runtime_gateway_network_recovery.go:26`; `decodeWrite` @ `services/fabric/internal/http/server.go:1054`; `writeResult` @ `services/fabric/internal/http/server.go:1106`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 61. POST /fabric/workspace-runtimes/{workspaceId}/gateway-secret

注册：`services/fabric/internal/http/server.go:712`；`newFabricMux`。

实际调用：`BindWorkspaceRuntimeGatewaySecret`, `Context`, `PathValue`, `TrimSpace`, `decodeWrite`, `writeError`, `writeResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.WorkspaceRuntimeGatewaySecretInput
	if !decodeWrite(w, r, &input.IdempotencyKey, &input) {
		return
	}
	if input.WorkspaceID != strings.TrimSpace(r.PathValue("workspaceId")) {
		writeError(w, http.StatusBadRequest, "workspace_runtime_gateway_secret_input_required")
		return
	}
	binding, err := service.BindWorkspaceRuntimeGatewaySecret(r.Context(), input)
	writeResult(w, binding, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `input` | `fabric.WorkspaceRuntimeGatewaySecretInput` | named variable; exact decoder in handler |
| `workspaceId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeResult(w, binding, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`BindWorkspaceRuntimeGatewaySecret` @ `services/fabric/internal/fabric/local_docker_runtime.go:1323`; `BindWorkspaceRuntimeGatewaySecret` @ `services/fabric/internal/fabric/tencent_provider_runtime.go:628`; `BindWorkspaceRuntimeGatewaySecret` @ `services/fabric/internal/fabric/workspace_runtime.go:655`; `decodeWrite` @ `services/fabric/internal/http/server.go:1054`; `writeResult` @ `services/fabric/internal/http/server.go:1106`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 62. POST /fabric/workspace-runtimes/{workspaceId}/image-replacements

注册：`services/fabric/internal/http/server.go:546`；`newFabricMux`。

实际调用：`Context`, `Error`, `PathValue`, `ReplaceWorkspaceRuntimeImage`, `TrimSpace`, `decodeWrite`, `writeError`, `writeResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.WorkspaceRuntimeImageReplacementInput
	if !decodeWrite(w, r, &input.IdempotencyKey, &input) {
		return
	}
	if input.WorkspaceID != strings.TrimSpace(r.PathValue("workspaceId")) {
		writeError(w, http.StatusBadRequest, fabric.ErrWorkspaceRuntimeImageReplacementInputInvalid.Error())
		return
	}
	result, err := service.ReplaceWorkspaceRuntimeImage(r.Context(), input)
	writeResult(w, result, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `input` | `fabric.WorkspaceRuntimeImageReplacementInput` | named variable; exact decoder in handler |
| `workspaceId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeResult(w, result, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/fabric/cmd/opl-tencent-provisioner/main.go:47`; `ReplaceWorkspaceRuntimeImage` @ `services/fabric/internal/fabric/tencent_provider_runtime.go:31`; `ReplaceWorkspaceRuntimeImage` @ `services/fabric/internal/fabric/workspace_runtime_image_replacement.go:34`; `decodeWrite` @ `services/fabric/internal/http/server.go:1054`; `writeResult` @ `services/fabric/internal/http/server.go:1106`; `writeError` @ `services/fabric/internal/http/server.go:1178`

### 63. POST /fabric/workspace-runtimes/{workspaceId}/repair

注册：`services/fabric/internal/http/server.go:534`；`newFabricMux`。

实际调用：`Context`, `PathValue`, `RepairWorkspaceRuntime`, `TrimSpace`, `decodeWrite`, `writeError`, `writeResult`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	var input fabric.WorkspaceRuntimeInput
	if !decodeWrite(w, r, &input.IdempotencyKey, &input) {
		return
	}
	if input.WorkspaceID != strings.TrimSpace(r.PathValue("workspaceId")) {
		writeError(w, http.StatusBadRequest, "workspace_runtime_repair_identity_required")
		return
	}
	runtime, err := service.RepairWorkspaceRuntime(r.Context(), input)
	writeResult(w, runtime, err)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `input` | `fabric.WorkspaceRuntimeInput` | named variable; exact decoder in handler |
| `workspaceId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeResult(w, runtime, err)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`RepairWorkspaceRuntime` @ `services/fabric/internal/fabric/local_docker_runtime.go:964`; `RepairWorkspaceRuntime` @ `services/fabric/internal/fabric/workspace_runtime.go:169`; `decodeWrite` @ `services/fabric/internal/http/server.go:1054`; `writeResult` @ `services/fabric/internal/http/server.go:1106`; `writeError` @ `services/fabric/internal/http/server.go:1178`

## 数据库实际字段与约束


### fabric_content_transfer_chunks

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `text` | NO | `—` | — |
| `transfer_id` | `text` | NO | `—` | — |
| `chunk_index` | `int8` | NO | `—` | 64 |
| `digest` | `text` | NO | `—` | — |
| `body` | `bytea` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `now()` | — |

约束：
- `fabric_content_transfer_chunks_pkey` `PRIMARY KEY (id)`

索引：
- `CREATE UNIQUE INDEX content_transfer_chunks_transfer_index ON public.fabric_content_transfer_chunks USING btree (transfer_id, chunk_index)`
- `CREATE UNIQUE INDEX fabric_content_transfer_chunks_pkey ON public.fabric_content_transfer_chunks USING btree (id)`

### fabric_content_transfers

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `text` | NO | `—` | — |
| `organization_id` | `text` | NO | `—` | — |
| `workspace_id` | `text` | NO | `—` | — |
| `project_id` | `text` | NO | `—` | — |
| `path` | `text` | NO | `—` | — |
| `digest` | `text` | NO | `—` | — |
| `size` | `int8` | NO | `—` | 64 |
| `chunk_size` | `int8` | NO | `—` | 64 |
| `chunk_count` | `int8` | NO | `—` | 64 |
| `status` | `text` | NO | `—` | — |
| `idempotency_key` | `text` | NO | `—` | — |
| `request_hash` | `text` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `—` | — |
| `completed_at` | `timestamptz` | YES | `—` | — |

约束：
- `fabric_content_transfers_idempotency_key_key` `UNIQUE (idempotency_key)`
- `fabric_content_transfers_pkey` `PRIMARY KEY (id)`

索引：
- `CREATE INDEX content_transfers_digest ON public.fabric_content_transfers USING btree (digest)`
- `CREATE INDEX content_transfers_workspace_path ON public.fabric_content_transfers USING btree (workspace_id, path)`
- `CREATE UNIQUE INDEX fabric_content_transfers_idempotency_key_key ON public.fabric_content_transfers USING btree (idempotency_key)`
- `CREATE UNIQUE INDEX fabric_content_transfers_pkey ON public.fabric_content_transfers USING btree (id)`

### fabric_operations

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `text` | NO | `—` | — |
| `operation_id` | `text` | NO | `—` | — |
| `caller_service` | `text` | NO | `—` | — |
| `action` | `text` | NO | `—` | — |
| `resource_kind` | `text` | NO | `—` | — |
| `resource_id` | `text` | NO | `—` | — |
| `account_id` | `text` | NO | `''::text` | — |
| `workspace_id` | `text` | NO | `''::text` | — |
| `provider` | `text` | NO | `''::text` | — |
| `provider_request_id` | `text` | NO | `''::text` | — |
| `idempotency_key` | `text` | NO | `''::text` | — |
| `request_hash` | `text` | NO | `''::text` | — |
| `redacted_provider_payload` | `text` | NO | `'{}'::text` | — |
| `status` | `text` | NO | `—` | — |
| `error_code` | `text` | NO | `''::text` | — |
| `retryable` | `bool` | NO | `false` | — |
| `started_at` | `timestamptz` | NO | `—` | — |
| `finished_at` | `timestamptz` | YES | `—` | — |
| `created_at` | `timestamptz` | NO | `now()` | — |
| `compute_pool_key` | `text` | NO | `''::text` | — |
| `compute_pool_lease_owner` | `text` | NO | `''::text` | — |
| `compute_pool_lease_expires_at` | `timestamptz` | YES | `—` | — |

约束：
- `fabric_operations_pkey` `PRIMARY KEY (id)`

索引：
- `CREATE UNIQUE INDEX fabric_operations_compute_claim_idx ON public.fabric_operations USING btree (action, idempotency_key) WHERE ((action = 'create_compute_allocation'::text) AND (idempotency_key <> ''::text) AND (id ~~ 'fop_compute_claim_%'::text))`
- `CREATE INDEX fabric_operations_compute_pool_head_idx ON public.fabric_operations USING btree (compute_pool_key, created_at, id) WHERE ((action = ANY (ARRAY['create_compute_allocation'::text, 'ensure_compute_allocation'::text])) AND (status = ANY (ARRAY['started'::text, 'claim_pending'::text])) AND (compute_pool_key <> ''::text))`
- `CREATE INDEX fabric_operations_created_idx ON public.fabric_operations USING btree (created_at)`
- `CREATE INDEX fabric_operations_operation_id_idx ON public.fabric_operations USING btree (operation_id)`
- `CREATE UNIQUE INDEX fabric_operations_pkey ON public.fabric_operations USING btree (id)`
- `CREATE INDEX fabric_operations_resource_idx ON public.fabric_operations USING btree (resource_kind, resource_id)`
- `CREATE UNIQUE INDEX fabric_operations_runtime_claim_idx ON public.fabric_operations USING btree (action, idempotency_key) WHERE ((action = 'create_workspace_runtime'::text) AND (idempotency_key <> ''::text) AND (id ~~ 'fop_runtime_claim_%'::text))`
- `CREATE INDEX fabric_operations_workspace_idx ON public.fabric_operations USING btree (workspace_id)`

### machine_ownerships

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `text` | NO | `—` | — |
| `resource_id` | `text` | NO | `—` | — |
| `account_id` | `text` | NO | `—` | — |
| `workspace_id` | `text` | NO | `''::text` | — |
| `package_id` | `text` | NO | `—` | — |
| `node_pool_id` | `text` | NO | `—` | — |
| `machine_id` | `text` | NO | `—` | — |
| `instance_id` | `text` | YES | `—` | — |
| `node_name` | `text` | NO | `''::text` | — |
| `status` | `text` | NO | `—` | — |
| `provider_request_id` | `text` | NO | `''::text` | — |
| `claimed_at` | `timestamptz` | NO | `—` | — |
| `released_at` | `timestamptz` | YES | `—` | — |

约束：
- `machine_ownerships_instance_id_key` `UNIQUE (instance_id)`
- `machine_ownerships_machine_id_key` `UNIQUE (machine_id)`
- `machine_ownerships_pkey` `PRIMARY KEY (id)`
- `machine_ownerships_resource_id_key` `UNIQUE (resource_id)`

索引：
- `CREATE UNIQUE INDEX machine_ownerships_instance_id_key ON public.machine_ownerships USING btree (instance_id)`
- `CREATE UNIQUE INDEX machine_ownerships_machine_id_key ON public.machine_ownerships USING btree (machine_id)`
- `CREATE UNIQUE INDEX machine_ownerships_pkey ON public.machine_ownerships USING btree (id)`
- `CREATE INDEX machine_ownerships_pool_status_idx ON public.machine_ownerships USING btree (node_pool_id, status)`
- `CREATE UNIQUE INDEX machine_ownerships_resource_id_key ON public.machine_ownerships USING btree (resource_id)`

### opl_schema_migrations

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `service` | `text` | NO | `—` | — |
| `version` | `text` | NO | `—` | — |
| `applied_at` | `timestamptz` | NO | `CURRENT_TIMESTAMP` | — |

约束：
- `opl_schema_migrations_pkey` `PRIMARY KEY (service, version)`

索引：
- `CREATE UNIQUE INDEX opl_schema_migrations_pkey ON public.opl_schema_migrations USING btree (service, version)`

## 未自动提升为完整性的项目

- 空库安装结果不证明已有客户数据的迁移、归档表在特定旧版本下的全部形状。
- Go AST清单不是网络扫描；未登录、越权、retired guard、静态/应用host分流仍由真实入口决定。
- 关联对象和JSON文本必须使用其真实typed decoder；不把HTTP DTO等同数据库实体。
