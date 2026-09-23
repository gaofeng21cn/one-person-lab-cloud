# 继承基线：ledger 的实际 API / DB

> 上游与fork共同基线 `50520e27a6b9a630eefdc2df7da3e5ec498d28a0`。API由Go AST的真实注册函数路径提取；DB为临时空PostgreSQL执行真实loader后的readback，不是生产数据库/历史数据迁移结果。fork `7f5d05fe9b855d8af216caad714cf7fc85014d3c` 保留此模块业务代码。

## 覆盖与判读

11 个已挂载路由pattern（含健康/静态/代理/明确404入口，不等同业务API数量）；6 张实际表、64 列（含迁移journal）。

请求/响应中的map和helper不做猜测式OpenAPI转换。每条附实际handler源码及其调用点；命名JSON类型完整字段见[旧DTO目录](legacy-wire-types.md)，动态投影以源handler/decoder为准。租户校验、middleware和数据库状态影响响应，不能从字段存在推导授权。

## API注册清单

| pattern | 注册函数 | 源码 |
| --- | --- | --- |
| `GET /healthz` | `NewServerWithAuth` | `services/ledger/internal/http/server.go:24` |
| `GET /ledger/evidence-index` | `registerEvidenceIndexRoutes` | `services/ledger/internal/http/server.go:222` |
| `GET /ledger/evidence-index/export` | `registerEvidenceIndexRoutes` | `services/ledger/internal/http/server.go:239` |
| `GET /ledger/receipts` | `NewServerWithAuth` | `services/ledger/internal/http/server.go:62` |
| `GET /ledger/receipts/{id}` | `NewServerWithAuth` | `services/ledger/internal/http/server.go:98` |
| `GET /readyz` | `NewServerWithAuth` | `services/ledger/internal/http/server.go:27` |
| `POST /ledger/evidence-index` | `registerEvidenceIndexRoutes` | `services/ledger/internal/http/server.go:198` |
| `POST /ledger/receipts` | `NewServerWithAuth` | `services/ledger/internal/http/server.go:35` |
| `POST /ledger/receipts/{id}/privacy-delete` | `NewServerWithAuth` | `services/ledger/internal/http/server.go:137` |
| `POST /ledger/receipts/{id}/retention` | `NewServerWithAuth` | `services/ledger/internal/http/server.go:110` |
| `POST /ledger/reconciliation` | `NewServerWithAuth` | `services/ledger/internal/http/server.go:164` |

## 每条API的实际字段处理与交互


### 1. GET /healthz

注册：`services/ledger/internal/http/server.go:24`；`NewServerWithAuth`。

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

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`writeJSON` @ `services/ledger/internal/http/server.go:409`

### 2. GET /ledger/evidence-index

注册：`services/ledger/internal/http/server.go:222`；`registerEvidenceIndexRoutes`。

实际调用：`Context`, `Error`, `Is`, `ListEvidenceIndex`, `evidenceIndexQueryFromRequest`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	query, err := evidenceIndexQueryFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, ledger.ErrInvalidEvidenceIndexQuery.Error())
		return
	}
	result, err := store.ListEvidenceIndex(r.Context(), query)
	if errors.Is(err, ledger.ErrInvalidEvidenceIndexQuery) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "evidence index query failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, result)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`evidenceIndexQueryFromRequest` @ `services/ledger/internal/http/server.go:262`; `writeJSON` @ `services/ledger/internal/http/server.go:409`; `writeError` @ `services/ledger/internal/http/server.go:415`; `ListEvidenceIndex` @ `services/ledger/internal/ledger/memory_store.go:94`; `ListEvidenceIndex` @ `services/ledger/internal/ledger/postgres_store.go:220`

### 3. GET /ledger/evidence-index/export

注册：`services/ledger/internal/http/server.go:239`；`registerEvidenceIndexRoutes`。

实际调用：`Context`, `Error`, `ExportEvidenceIndex`, `Is`, `evidenceIndexQueryFromRequest`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	query, err := evidenceIndexQueryFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, ledger.ErrInvalidEvidenceIndexQuery.Error())
		return
	}
	result, err := store.ExportEvidenceIndex(r.Context(), query)
	if errors.Is(err, ledger.ErrInvalidEvidenceIndexQuery) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, ledger.ErrEvidenceIndexExportTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "evidence index export failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, result)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`evidenceIndexQueryFromRequest` @ `services/ledger/internal/http/server.go:262`; `writeJSON` @ `services/ledger/internal/http/server.go:409`; `writeError` @ `services/ledger/internal/http/server.go:415`; `ExportEvidenceIndex` @ `services/ledger/internal/ledger/memory_store.go:127`; `ExportEvidenceIndex` @ `services/ledger/internal/ledger/postgres_store.go:275`

### 4. GET /ledger/receipts

注册：`services/ledger/internal/http/server.go:62`；`NewServerWithAuth`。

实际调用：`Atoi`, `Context`, `Error`, `Get`, `Is`, `ListReceipts`, `Query`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	query := ledger.ReceiptQuery{
		AccountID:		values.Get("accountId"),
		OrganizationID:		values.Get("organizationId"),
		WorkspaceID:		values.Get("workspaceId"),
		RequestID:		values.Get("requestId"),
		ProjectID:		values.Get("projectId"),
		TaskID:			values.Get("taskId"),
		JobID:			values.Get("jobId"),
		Type:			values.Get("type"),
		TypePrefix:		values.Get("typePrefix"),
		IncludeType:		values.Get("includeType"),
		IncludeExecutionKind:	values.Get("includeExecutionKind"),
		Status:			values.Get("status"),
		Cursor:			values.Get("cursor"),
	}
	if rawLimit := values.Get("limit"); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil || limit < 1 || limit > ledger.MaxReceiptPageSize {
			writeError(w, http.StatusBadRequest, ledger.ErrInvalidReceiptQuery.Error())
			return
		}
		query.Limit = limit
	}
	result, err := store.ListReceipts(r.Context(), query)
	if errors.Is(err, ledger.ErrInvalidReceiptQuery) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "receipt list failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, result)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`writeJSON` @ `services/ledger/internal/http/server.go:409`; `writeError` @ `services/ledger/internal/http/server.go:415`; `ListReceipts` @ `services/ledger/internal/ledger/memory_store.go:279`; `ListReceipts` @ `services/ledger/internal/ledger/postgres_store.go:296`

### 5. GET /ledger/receipts/{id}

注册：`services/ledger/internal/http/server.go:98`；`NewServerWithAuth`。

实际调用：`Context`, `Error`, `Is`, `PathValue`, `Receipt`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	result, err := store.Receipt(r.Context(), r.PathValue("id"))
	if errors.Is(err, ledger.ErrReceiptNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "receipt query failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `id` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, result)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`writeJSON` @ `services/ledger/internal/http/server.go:409`; `writeError` @ `services/ledger/internal/http/server.go:415`; `Receipt` @ `services/ledger/internal/ledger/memory_store.go:183`; `Receipt` @ `services/ledger/internal/ledger/postgres_store.go:379`

### 6. GET /readyz

注册：`services/ledger/internal/http/server.go:27`；`NewServerWithAuth`。

实际调用：`Context`, `Ready`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	readiness, ok := store.(ledger.ReadinessStore)
	if !ok || readiness.Ready(r.Context()) != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})`
- `writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`writeJSON` @ `services/ledger/internal/http/server.go:409`; `Ready` @ `services/ledger/internal/ledger/memory_store.go:26`; `Ready` @ `services/ledger/internal/ledger/postgres_store.go:81`

### 7. POST /ledger/evidence-index

注册：`services/ledger/internal/http/server.go:198`；`registerEvidenceIndexRoutes`。

实际调用：`Context`, `Error`, `Get`, `Is`, `RecordEvidenceIndex`, `decodeJSONBody`, `writeError`, `writeJSON`, `writeJSONBodyError`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "missing Idempotency-Key")
		return
	}
	var input ledger.EvidenceIndexInput
	if err := decodeJSONBody(r, &input); err != nil {
		writeJSONBodyError(w, err)
		return
	}
	input.IdempotencyKey = idempotencyKey
	result, err := store.RecordEvidenceIndex(r.Context(), input)
	switch {
	case errors.Is(err, ledger.ErrInvalidEvidenceIndexInput):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ledger.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, "evidence index write failed")
	default:
		writeJSON(w, http.StatusCreated, result)
	}
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `Idempotency-Key` | `string` | header |
| `input` | `ledger.EvidenceIndexInput` | named variable; exact decoder in handler |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusCreated, result)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`decodeJSONBody` @ `services/ledger/internal/http/server.go:288`; `writeJSONBodyError` @ `services/ledger/internal/http/server.go:311`; `writeJSON` @ `services/ledger/internal/http/server.go:409`; `writeError` @ `services/ledger/internal/http/server.go:415`; `RecordEvidenceIndex` @ `services/ledger/internal/ledger/memory_store.go:54`; `RecordEvidenceIndex` @ `services/ledger/internal/ledger/postgres_store.go:161`

### 8. POST /ledger/receipts

注册：`services/ledger/internal/http/server.go:35`；`NewServerWithAuth`。

实际调用：`Context`, `Error`, `Get`, `Is`, `RecordReceipt`, `decodeJSONBody`, `writeError`, `writeJSON`, `writeJSONBodyError`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "missing Idempotency-Key")
		return
	}
	var input ledger.ReceiptInput
	if err := decodeJSONBody(r, &input); err != nil {
		writeJSONBodyError(w, err)
		return
	}
	input.IdempotencyKey = idempotencyKey
	result, err := store.RecordReceipt(r.Context(), input)
	if errors.Is(err, ledger.ErrInvalidReceiptInput) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, ledger.ErrIdempotencyConflict) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "receipt failed")
		return
	}
	writeJSON(w, http.StatusCreated, result)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `Idempotency-Key` | `string` | header |
| `input` | `ledger.ReceiptInput` | named variable; exact decoder in handler |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusCreated, result)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`decodeJSONBody` @ `services/ledger/internal/http/server.go:288`; `writeJSONBodyError` @ `services/ledger/internal/http/server.go:311`; `writeJSON` @ `services/ledger/internal/http/server.go:409`; `writeError` @ `services/ledger/internal/http/server.go:415`; `RecordReceipt` @ `services/ledger/internal/ledger/memory_store.go:148`; `RecordReceipt` @ `services/ledger/internal/ledger/postgres_store.go:104`

### 9. POST /ledger/receipts/{id}/privacy-delete

注册：`services/ledger/internal/http/server.go:137`；`NewServerWithAuth`。

实际调用：`Context`, `Error`, `Get`, `Is`, `PathValue`, `PrivacyDeleteReceipt`, `decodeJSONBody`, `writeError`, `writeJSON`, `writeJSONBodyError`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "missing Idempotency-Key")
		return
	}
	var input ledger.ReceiptPrivacyDeleteInput
	if err := decodeJSONBody(r, &input); err != nil {
		writeJSONBodyError(w, err)
		return
	}
	input.ReceiptID = r.PathValue("id")
	input.IdempotencyKey = idempotencyKey
	result, err := store.PrivacyDeleteReceipt(r.Context(), input)
	switch {
	case errors.Is(err, ledger.ErrInvalidReceiptRetentionInput):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ledger.ErrReceiptNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ledger.ErrIdempotencyConflict), errors.Is(err, ledger.ErrReceiptRetentionActive), errors.Is(err, ledger.ErrReceiptLegalHold):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, "receipt privacy delete failed")
	default:
		writeJSON(w, http.StatusOK, result)
	}
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `Idempotency-Key` | `string` | header |
| `id` | `string` | path |
| `input` | `ledger.ReceiptPrivacyDeleteInput` | named variable; exact decoder in handler |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, result)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`decodeJSONBody` @ `services/ledger/internal/http/server.go:288`; `writeJSONBodyError` @ `services/ledger/internal/http/server.go:311`; `writeJSON` @ `services/ledger/internal/http/server.go:409`; `writeError` @ `services/ledger/internal/http/server.go:415`; `PrivacyDeleteReceipt` @ `services/ledger/internal/ledger/memory_store.go:234`; `PrivacyDeleteReceipt` @ `services/ledger/internal/ledger/postgres_store.go:411`

### 10. POST /ledger/receipts/{id}/retention

注册：`services/ledger/internal/http/server.go:110`；`NewServerWithAuth`。

实际调用：`Context`, `Error`, `Get`, `Is`, `PathValue`, `UpdateReceiptRetention`, `decodeJSONBody`, `writeError`, `writeJSON`, `writeJSONBodyError`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "missing Idempotency-Key")
		return
	}
	var input ledger.ReceiptRetentionInput
	if err := decodeJSONBody(r, &input); err != nil {
		writeJSONBodyError(w, err)
		return
	}
	input.ReceiptID = r.PathValue("id")
	input.IdempotencyKey = idempotencyKey
	result, err := store.UpdateReceiptRetention(r.Context(), input)
	switch {
	case errors.Is(err, ledger.ErrInvalidReceiptRetentionInput):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ledger.ErrReceiptNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ledger.ErrIdempotencyConflict), errors.Is(err, ledger.ErrReceiptRetentionShortening), errors.Is(err, ledger.ErrReceiptLegalHold):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, "receipt retention update failed")
	default:
		writeJSON(w, http.StatusOK, result)
	}
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `Idempotency-Key` | `string` | header |
| `id` | `string` | path |
| `input` | `ledger.ReceiptRetentionInput` | named variable; exact decoder in handler |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, result)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`decodeJSONBody` @ `services/ledger/internal/http/server.go:288`; `writeJSONBodyError` @ `services/ledger/internal/http/server.go:311`; `writeJSON` @ `services/ledger/internal/http/server.go:409`; `writeError` @ `services/ledger/internal/http/server.go:415`; `UpdateReceiptRetention` @ `services/ledger/internal/ledger/memory_store.go:193`; `UpdateReceiptRetention` @ `services/ledger/internal/ledger/postgres_store.go:387`

### 11. POST /ledger/reconciliation

注册：`services/ledger/internal/http/server.go:164`；`NewServerWithAuth`。

实际调用：`Context`, `Error`, `Get`, `Is`, `RecordReconciliation`, `decodeJSONBody`, `writeError`, `writeJSON`, `writeJSONBodyError`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "missing Idempotency-Key")
		return
	}
	var input ledger.ReconciliationInput
	if err := decodeJSONBody(r, &input); err != nil {
		writeJSONBodyError(w, err)
		return
	}
	input.IdempotencyKey = idempotencyKey
	result, err := store.RecordReconciliation(r.Context(), input)
	if errors.Is(err, ledger.ErrInvalidReconciliationInput) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, ledger.ErrIdempotencyConflict) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reconciliation failed")
		return
	}
	writeJSON(w, http.StatusCreated, result)
}
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `Idempotency-Key` | `string` | header |
| `input` | `ledger.ReconciliationInput` | named variable; exact decoder in handler |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusCreated, result)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`decodeJSONBody` @ `services/ledger/internal/http/server.go:288`; `writeJSONBodyError` @ `services/ledger/internal/http/server.go:311`; `writeJSON` @ `services/ledger/internal/http/server.go:409`; `writeError` @ `services/ledger/internal/http/server.go:415`; `RecordReconciliation` @ `services/ledger/internal/ledger/memory_store.go:328`; `RecordReconciliation` @ `services/ledger/internal/ledger/postgres_store.go:510`

## 数据库实际字段与约束


### evidence_index_entries

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `text` | NO | `—` | — |
| `operation_id` | `text` | NO | `—` | — |
| `candidate_sha` | `text` | NO | `—` | — |
| `candidate_tree` | `text` | NO | `—` | — |
| `image_digest` | `text` | NO | `—` | — |
| `receipt_id` | `text` | NO | `—` | — |
| `receipt_type` | `text` | NO | `—` | — |
| `status` | `text` | NO | `—` | — |
| `actor` | `text` | NO | `—` | — |
| `observed_at` | `timestamptz` | NO | `—` | — |
| `identity_digest` | `text` | NO | `—` | — |
| `redacted_link` | `text` | NO | `''::text` | — |
| `idempotency_key` | `text` | NO | `—` | — |
| `request_hash` | `text` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `now()` | — |

约束：
- `evidence_index_entries_idempotency_key_key` `UNIQUE (idempotency_key)`
- `evidence_index_entries_pkey` `PRIMARY KEY (id)`

索引：
- `CREATE INDEX evidence_index_entries_candidate_observed ON public.evidence_index_entries USING btree (candidate_sha, candidate_tree, image_digest, observed_at DESC, id DESC)`
- `CREATE UNIQUE INDEX evidence_index_entries_idempotency_key_key ON public.evidence_index_entries USING btree (idempotency_key)`
- `CREATE INDEX evidence_index_entries_operation_observed ON public.evidence_index_entries USING btree (operation_id, observed_at DESC, id DESC)`
- `CREATE UNIQUE INDEX evidence_index_entries_pkey ON public.evidence_index_entries USING btree (id)`
- `CREATE INDEX evidence_index_entries_receipt_observed ON public.evidence_index_entries USING btree (receipt_id, receipt_type, observed_at DESC, id DESC)`
- `CREATE INDEX evidence_index_entries_status_observed ON public.evidence_index_entries USING btree (status, observed_at DESC, id DESC)`

### evidence_receipts

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `text` | NO | `—` | — |
| `receipt_type` | `text` | NO | `''::text` | — |
| `status` | `text` | NO | `''::text` | — |
| `account_id` | `text` | NO | `''::text` | — |
| `organization_id` | `text` | NO | `''::text` | — |
| `workspace_id` | `text` | NO | `''::text` | — |
| `project_id` | `text` | NO | `''::text` | — |
| `task_id` | `text` | NO | `''::text` | — |
| `job_id` | `text` | NO | `''::text` | — |
| `payload_json` | `text` | NO | `'{}'::text` | — |
| `supersedes_receipt_id` | `text` | NO | `''::text` | — |
| `provider_request_id` | `text` | NO | `''::text` | — |
| `redacted_url` | `text` | NO | `''::text` | — |
| `token_version` | `text` | NO | `''::text` | — |
| `idempotency_key` | `text` | NO | `—` | — |
| `request_hash` | `text` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `now()` | — |
| `artifact_id` | `text` | NO | `''::text` | — |
| `review_id` | `text` | NO | `''::text` | — |

约束：
- `evidence_receipts_idempotency_key_key` `UNIQUE (idempotency_key)`
- `evidence_receipts_pkey` `PRIMARY KEY (id)`

索引：
- `CREATE INDEX evidence_receipts_account_created ON public.evidence_receipts USING btree (account_id, created_at DESC, id DESC)`
- `CREATE INDEX evidence_receipts_account_request_created ON public.evidence_receipts USING btree (account_id, (((payload_json)::jsonb ->> 'requestId'::text)), created_at DESC, id DESC)`
- `CREATE INDEX evidence_receipts_artifact_id ON public.evidence_receipts USING btree (artifact_id)`
- `CREATE UNIQUE INDEX evidence_receipts_idempotency_key_key ON public.evidence_receipts USING btree (idempotency_key)`
- `CREATE INDEX evidence_receipts_job_created ON public.evidence_receipts USING btree (job_id, created_at DESC, id DESC)`
- `CREATE INDEX evidence_receipts_organization_created ON public.evidence_receipts USING btree (organization_id, created_at DESC, id DESC)`
- `CREATE UNIQUE INDEX evidence_receipts_pkey ON public.evidence_receipts USING btree (id)`
- `CREATE INDEX evidence_receipts_project_created ON public.evidence_receipts USING btree (project_id, created_at DESC, id DESC)`
- `CREATE INDEX evidence_receipts_review_id ON public.evidence_receipts USING btree (review_id)`
- `CREATE INDEX evidence_receipts_status_created ON public.evidence_receipts USING btree (status, created_at DESC, id DESC)`
- `CREATE INDEX evidence_receipts_task_created ON public.evidence_receipts USING btree (task_id, created_at DESC, id DESC)`
- `CREATE INDEX evidence_receipts_type_created ON public.evidence_receipts USING btree (receipt_type, created_at DESC, id DESC)`
- `CREATE INDEX evidence_receipts_workspace_created ON public.evidence_receipts USING btree (workspace_id, created_at DESC, id DESC)`

### idempotency_keys

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `text` | NO | `—` | — |
| `service` | `text` | NO | `—` | — |
| `idempotency_key` | `text` | NO | `—` | — |
| `request_hash` | `text` | NO | `—` | — |
| `response_ref` | `text` | NO | `''::text` | — |
| `created_at` | `timestamptz` | NO | `now()` | — |

约束：
- `idempotency_keys_pkey` `PRIMARY KEY (id)`

索引：
- `CREATE UNIQUE INDEX idempotency_keys_pkey ON public.idempotency_keys USING btree (id)`

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

### reconciliation_reports

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `text` | NO | `—` | — |
| `status` | `text` | NO | `'ok'::text` | — |
| `report_json` | `text` | NO | `'{}'::text` | — |
| `block_new_workspaces` | `bool` | NO | `false` | — |
| `reason` | `text` | NO | `''::text` | — |
| `idempotency_key` | `text` | NO | `—` | — |
| `request_hash` | `text` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `now()` | — |

约束：
- `reconciliation_reports_idempotency_key_key` `UNIQUE (idempotency_key)`
- `reconciliation_reports_pkey` `PRIMARY KEY (id)`

索引：
- `CREATE UNIQUE INDEX reconciliation_reports_idempotency_key_key ON public.reconciliation_reports USING btree (idempotency_key)`
- `CREATE UNIQUE INDEX reconciliation_reports_pkey ON public.reconciliation_reports USING btree (id)`

### review_policies

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `text` | NO | `—` | — |
| `organization_id` | `text` | NO | `''::text` | — |
| `workspace_id` | `text` | NO | `—` | — |
| `project_id` | `text` | NO | `—` | — |
| `task_id` | `text` | NO | `—` | — |
| `job_id` | `text` | NO | `—` | — |
| `version` | `text` | NO | `—` | — |
| `required_reviewers_json` | `text` | NO | `—` | — |
| `status` | `text` | NO | `—` | — |
| `supersedes_policy_id` | `text` | NO | `''::text` | — |
| `idempotency_key` | `text` | NO | `—` | — |
| `request_hash` | `text` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `now()` | — |

约束：
- `review_policies_idempotency_key_key` `UNIQUE (idempotency_key)`
- `review_policies_pkey` `PRIMARY KEY (id)`
- `review_policies_status_check` `CHECK ((status = ANY (ARRAY['active'::text, 'superseded'::text])))`

索引：
- `CREATE UNIQUE INDEX review_policies_active_scope ON public.review_policies USING btree (organization_id, workspace_id, project_id, task_id, job_id) WHERE (status = 'active'::text)`
- `CREATE UNIQUE INDEX review_policies_idempotency_key_key ON public.review_policies USING btree (idempotency_key)`
- `CREATE UNIQUE INDEX review_policies_pkey ON public.review_policies USING btree (id)`
- `CREATE INDEX review_policies_scope_created ON public.review_policies USING btree (organization_id, workspace_id, project_id, task_id, job_id, created_at DESC)`

## 未自动提升为完整性的项目

- 空库安装结果不证明已有客户数据的迁移、归档表在特定旧版本下的全部形状。
- Go AST清单不是网络扫描；未登录、越权、retired guard、静态/应用host分流仍由真实入口决定。
- 关联对象和JSON文本必须使用其真实typed decoder；不把HTTP DTO等同数据库实体。
