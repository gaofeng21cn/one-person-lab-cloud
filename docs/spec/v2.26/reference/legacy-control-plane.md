# 继承基线：control-plane 的实际 API / DB

> 上游与fork共同基线 `50520e27a6b9a630eefdc2df7da3e5ec498d28a0`。API由Go AST的真实注册函数路径提取；DB为临时空PostgreSQL执行真实loader后的readback，不是生产数据库/历史数据迁移结果。fork `7f5d05fe9b855d8af216caad714cf7fc85014d3c` 保留此模块业务代码。

## 覆盖与判读

97 个已挂载路由pattern（含健康/静态/代理/明确404入口，不等同业务API数量）；22 张实际表、290 列（含迁移journal）。

请求/响应中的map和helper不做猜测式OpenAPI转换。每条附实际handler源码及其调用点；命名JSON类型完整字段见[旧DTO目录](legacy-wire-types.md)，动态投影以源handler/decoder为准。租户校验、middleware和数据库状态影响响应，不能从字段存在推导授权。

## API注册清单

| pattern | 注册函数 | 源码 |
| --- | --- | --- |
| `/` | `registerCoreRoutes` | `services/control-plane/internal/server/routes_core.go:38` |
| `/api/` | `registerCoreRoutes` | `services/control-plane/internal/server/routes_core.go:11` |
| `/api/state` | `registerStateRoutes` | `services/control-plane/internal/server/routes_state.go:11` |
| `/w/` | `registerCoreRoutes` | `services/control-plane/internal/server/routes_core.go:10` |
| `/ws` | `registerCoreRoutes` | `services/control-plane/internal/server/routes_core.go:12` |
| `DELETE /api/gateway/keys/{keyId}` | `registerGatewayRoutes` | `services/control-plane/internal/server/routes_gateway.go:103` |
| `DELETE /api/workspaces/{workspaceId}` | `registerWorkspaceRoutes` | `services/control-plane/internal/server/routes_workspace.go:60` |
| `GET /api/announcements` | `registerAnnouncementRoutes` | `services/control-plane/internal/server/routes_announcements.go:53` |
| `GET /api/auth/me` | `registerAuthRoutes` | `services/control-plane/internal/server/routes_auth.go:47` |
| `GET /api/billing/receipts` | `registerBillingRoutes` | `services/control-plane/internal/server/routes_billing.go:20` |
| `GET /api/billing/receipts/{id}` | `registerBillingRoutes` | `services/control-plane/internal/server/routes_billing.go:64` |
| `GET /api/billing/workspace-settlements` | `registerBillingRoutes` | `services/control-plane/internal/server/routes_billing.go:103` |
| `GET /api/gateway/balance-history` | `registerGatewayRoutes` | `services/control-plane/internal/server/routes_gateway.go:118` |
| `GET /api/gateway/endpoint` | `registerGatewayRoutes` | `services/control-plane/internal/server/routes_gateway.go:20` |
| `GET /api/gateway/groups` | `registerGatewayRoutes` | `services/control-plane/internal/server/routes_gateway.go:29` |
| `GET /api/gateway/keys` | `registerGatewayRoutes` | `services/control-plane/internal/server/routes_gateway.go:69` |
| `GET /api/gateway/keys/{keyId}` | `registerGatewayRoutes` | `services/control-plane/internal/server/routes_gateway.go:94` |
| `GET /api/gateway/keys/{keyId}/usage` | `registerGatewayRoutes` | `services/control-plane/internal/server/routes_gateway.go:109` |
| `GET /api/gateway/keys/{keyId}/usage-summary` | `registerGatewayRoutes` | `services/control-plane/internal/server/routes_gateway.go:112` |
| `GET /api/gateway/usage-summary` | `registerGatewayRoutes` | `services/control-plane/internal/server/routes_gateway.go:115` |
| `GET /api/gateway/wallet` | `registerGatewayRoutes` | `services/control-plane/internal/server/routes_gateway.go:54` |
| `GET /api/healthz` | `registerCoreRoutes` | `services/control-plane/internal/server/routes_core.go:13` |
| `GET /api/management/state` | `registerStateRoutes` | `services/control-plane/internal/server/routes_state.go:44` |
| `GET /api/operator/account-reconciliation` | `registerAcceptanceBAccountReconcileRoute` | `services/control-plane/internal/server/account_reconcile.go:64` |
| `GET /api/operator/accounts` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:421` |
| `GET /api/operator/announcements` | `registerAnnouncementRoutes` | `services/control-plane/internal/server/routes_announcements.go:59` |
| `GET /api/operator/application-data-materials/{applicationId}/{version}` | `registerApplicationDataMaterialRoutes` | `services/control-plane/internal/server/workspace_application_data_admission.go:89` |
| `GET /api/operator/application-deployments/{operationId}` | `registerApplicationDeploymentRoutes` | `services/control-plane/internal/server/workspace_application_deployment.go:576` |
| `GET /api/operator/application-revisions/{applicationId}/{version}` | `registerApplicationRevisionRoutes` | `services/control-plane/internal/server/workspace_application_admission.go:93` |
| `GET /api/operator/archive` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:604` |
| `GET /api/operator/health` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:486` |
| `GET /api/operator/overview` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:434` |
| `GET /api/operator/reconciliation` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:474` |
| `GET /api/operator/registry/repositories` | `registerWorkspaceRegistryCatalogRoutesWithCatalog` | `services/control-plane/internal/server/workspace_registry_catalog.go:138` |
| `GET /api/operator/registry/tags/{namespace}/{repository}` | `registerWorkspaceRegistryCatalogRoutesWithCatalog` | `services/control-plane/internal/server/workspace_registry_catalog.go:150` |
| `GET /api/operator/runtime-observations` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:442` |
| `GET /api/operator/wallet-adjustments/{operationId}` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:415` |
| `GET /api/operator/workspace-launches/{operationId}/canonical-facts-repair-preview` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:189` |
| `GET /api/operator/workspace-launches/{operationId}/disposable-reset-preview` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:136` |
| `GET /api/operator/workspace-launches/{operationId}/recovery` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:40` |
| `GET /api/operator/workspace-launches/{operationId}/resume-approval-candidates` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:260` |
| `GET /api/operator/workspace-launches/{operationId}/resume-authorizations/{authorizationId}` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:384` |
| `GET /api/operator/workspace-launches/{operationId}/stage-observation` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:116` |
| `GET /api/operator/workspace-runtime-image-policy` | `registerWorkspaceRuntimeImageReplacementRoutes` | `services/control-plane/internal/server/workspace_runtime_image_replacement.go:40` |
| `GET /api/operator/workspaces` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:450` |
| `GET /api/operator/workspaces/{workspaceId}` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:462` |
| `GET /api/operator/workspaces/{workspaceId}/runtime-image-replacements/preview` | `registerWorkspaceRuntimeImageReplacementRoutes` | `services/control-plane/internal/server/workspace_runtime_image_replacement.go:46` |
| `GET /api/operator/workspaces/{workspaceId}/runtime-image-replacements/{operationId}` | `registerWorkspaceRuntimeImageReplacementRoutes` | `services/control-plane/internal/server/workspace_runtime_image_replacement.go:52` |
| `GET /api/pricing/catalog` | `registerStateRoutes` | `services/control-plane/internal/server/routes_state.go:12` |
| `GET /api/production/readiness` | `registerCoreRoutes` | `services/control-plane/internal/server/routes_core.go:24` |
| `GET /api/runtime/readiness` | `registerCoreRoutes` | `services/control-plane/internal/server/routes_core.go:16` |
| `GET /api/workspace-launches` | `registerWorkspaceLaunchRoutes` | `services/control-plane/internal/server/routes_workspace_launch.go:309` |
| `GET /api/workspace-launches/{id}` | `registerWorkspaceLaunchRoutes` | `services/control-plane/internal/server/routes_workspace_launch.go:331` |
| `GET /api/workspaces` | `registerWorkspaceRoutes` | `services/control-plane/internal/server/routes_workspace.go:17` |
| `GET /api/workspaces/{workspaceId}/deletion` | `registerWorkspaceRoutes` | `services/control-plane/internal/server/routes_workspace.go:59` |
| `GET /api/workspaces/{workspaceId}/gateway-budget` | `registerWorkspaceRoutes` | `services/control-plane/internal/server/routes_workspace.go:243` |
| `GET /api/workspaces/{workspaceId}/renewal` | `registerWorkspaceRoutes` | `services/control-plane/internal/server/routes_workspace.go:249` |
| `GET /api/workspaces/{workspaceId}/runtime-status` | `registerWorkspaceRoutes` | `services/control-plane/internal/server/routes_workspace.go:63` |
| `PATCH /api/gateway/keys/{keyId}` | `registerGatewayRoutes` | `services/control-plane/internal/server/routes_gateway.go:100` |
| `PATCH /api/workspaces/{workspaceId}/gateway-budget` | `registerWorkspaceRoutes` | `services/control-plane/internal/server/routes_workspace.go:246` |
| `POST /api/announcements/{announcementId}/read` | `registerAnnouncementRoutes` | `services/control-plane/internal/server/routes_announcements.go:56` |
| `POST /api/auth/login` | `registerAuthRoutes` | `services/control-plane/internal/server/routes_auth.go:17` |
| `POST /api/auth/logout` | `registerAuthRoutes` | `services/control-plane/internal/server/routes_auth.go:93` |
| `POST /api/billing/reconciliation` | `registerBillingRoutes` | `services/control-plane/internal/server/routes_billing.go:137` |
| `POST /api/gateway/keys` | `registerGatewayRoutes` | `services/control-plane/internal/server/routes_gateway.go:97` |
| `POST /api/gateway/keys/{keyId}/reveal` | `registerGatewayRoutes` | `services/control-plane/internal/server/routes_gateway.go:106` |
| `POST /api/operator/accounts` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:489` |
| `POST /api/operator/accounts/{accountId}/disable` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:569` |
| `POST /api/operator/accounts/{accountId}/wallet-adjustments` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:412` |
| `POST /api/operator/accounts/{accountId}/workspace-purchase-eligibility` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:522` |
| `POST /api/operator/announcements` | `registerAnnouncementRoutes` | `services/control-plane/internal/server/routes_announcements.go:62` |
| `POST /api/operator/announcements/{announcementId}/publish` | `registerAnnouncementRoutes` | `services/control-plane/internal/server/routes_announcements.go:68` |
| `POST /api/operator/announcements/{announcementId}/withdraw` | `registerAnnouncementRoutes` | `services/control-plane/internal/server/routes_announcements.go:71` |
| `POST /api/operator/application-data-materials` | `registerApplicationDataMaterialRoutes` | `services/control-plane/internal/server/workspace_application_data_admission.go:53` |
| `POST /api/operator/application-deployments` | `registerApplicationDeploymentRoutes` | `services/control-plane/internal/server/workspace_application_deployment.go:349` |
| `POST /api/operator/application-deployments/{operationID}/retry` | `registerWorkspaceApplicationRecoveryRoutes` | `services/control-plane/internal/server/workspace_application_recovery.go:147` |
| `POST /api/operator/application-revisions` | `registerApplicationRevisionRoutes` | `services/control-plane/internal/server/workspace_application_admission.go:55` |
| `POST /api/operator/billing-reviews/{resourceType}/{id}/resolve` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:612` |
| `POST /api/operator/provider-acceptance` | `registerProviderAcceptanceRoutes` | `services/control-plane/internal/server/routes_provider_acceptance.go:60` |
| `POST /api/operator/registry/resolve` | `registerWorkspaceRegistryCatalogRoutesWithCatalog` | `services/control-plane/internal/server/workspace_registry_catalog.go:165` |
| `POST /api/operator/wallet-adjustments/{operationId}/recover` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:418` |
| `POST /api/operator/workspace-image-release-activations` | `registerWorkspaceRuntimeImageReplacementRoutes` | `services/control-plane/internal/server/workspace_runtime_image_replacement.go:43` |
| `POST /api/operator/workspace-launches/{operationId}/canonical-facts-repair` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:150` |
| `POST /api/operator/workspace-launches/{operationId}/recover` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:58` |
| `POST /api/operator/workspace-launches/{operationId}/repair-runtime` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:199` |
| `POST /api/operator/workspace-launches/{operationId}/resume` | `registerAdminRoutes` | `services/control-plane/internal/server/routes_admin.go:296` |
| `POST /api/operator/workspaces/{workspaceId}/runtime-gateway-network/recover` | `registerWorkspaceRuntimeGatewayNetworkRecoveryRoutes` | `services/control-plane/internal/server/workspace_runtime_gateway_network_recovery.go:33` |
| `POST /api/operator/workspaces/{workspaceId}/runtime-image-replacements` | `registerWorkspaceRuntimeImageReplacementRoutes` | `services/control-plane/internal/server/workspace_runtime_image_replacement.go:49` |
| `POST /api/pricing/preview` | `registerStateRoutes` | `services/control-plane/internal/server/routes_state.go:24` |
| `POST /api/workspace-launches` | `registerWorkspaceLaunchRoutes` | `services/control-plane/internal/server/routes_workspace_launch.go:15` |
| `POST /api/workspace-launches/{id}/resume` | `registerWorkspaceLaunchRoutes` | `services/control-plane/internal/server/routes_workspace_launch.go:353` |
| `POST /api/workspaces/{workspaceID}/application-installation/resume` | `registerWorkspaceApplicationRecoveryRoutes` | `services/control-plane/internal/server/workspace_application_recovery.go:163` |
| `POST /api/workspaces/{workspaceId}/auto-renew` | `registerWorkspaceRoutes` | `services/control-plane/internal/server/routes_workspace.go:275` |
| `POST /api/workspaces/{workspaceId}/runtime-credentials/reveal` | `registerWorkspaceRoutes` | `services/control-plane/internal/server/routes_workspace.go:149` |
| `POST /api/workspaces/{workspaceId}/runtime-credentials/rotate` | `registerWorkspaceRoutes` | `services/control-plane/internal/server/routes_workspace.go:174` |
| `POST /api/workspaces/{workspaceId}/workspace-key/rotate` | `registerWorkspaceRoutes` | `services/control-plane/internal/server/routes_workspace.go:240` |
| `PUT /api/operator/announcements/{announcementId}` | `registerAnnouncementRoutes` | `services/control-plane/internal/server/routes_announcements.go:65` |

## 每条API的实际字段处理与交互


### 1. /

注册：`services/control-plane/internal/server/routes_core.go:38`；`registerCoreRoutes`。

实际调用：`consoleStatic`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) { app.consoleStatic(w, r, service) }
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`consoleStatic` @ `services/control-plane/internal/server/server.go:165`

### 2. /api/

注册：`services/control-plane/internal/server/routes_core.go:11`；`registerCoreRoutes`。

实际调用：`proxyWorkspaceRoot`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) { app.proxyWorkspaceRoot(w, r, service) }
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`proxyWorkspaceRoot` @ `services/control-plane/internal/server/workspace_gateway.go:1072`

### 3. /api/state

注册：`services/control-plane/internal/server/routes_state.go:11`；`registerStateRoutes`。

实际调用：`NotFound`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }
```

</details>

### 4. /w/

注册：`services/control-plane/internal/server/routes_core.go:10`；`registerCoreRoutes`。

实际调用：`proxyWorkspace`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) { app.proxyWorkspace(w, r, service) }
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`proxyWorkspace` @ `services/control-plane/internal/server/workspace_gateway.go:1062`

### 5. /ws

注册：`services/control-plane/internal/server/routes_core.go:12`；`registerCoreRoutes`。

实际调用：`proxyWorkspaceRoot`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) { app.proxyWorkspaceRoot(w, r, service) }
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`proxyWorkspaceRoot` @ `services/control-plane/internal/server/workspace_gateway.go:1072`

### 6. DELETE /api/gateway/keys/{keyId}

注册：`services/control-plane/internal/server/routes_gateway.go:103`；`registerGatewayRoutes`。

实际调用：`deleteGatewayKey`, `protected`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	app.deleteGatewayKey(w, r, service)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`deleteGatewayKey` @ `services/control-plane/internal/server/routes_gateway.go:483`; `protected` @ `services/control-plane/internal/server/server.go:277`

### 7. DELETE /api/workspaces/{workspaceId}

注册：`services/control-plane/internal/server/routes_workspace.go:60`；`registerWorkspaceRoutes`。

实际调用：`deleteWorkspace`, `protected`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	app.deleteWorkspace(w, r, service)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`protected` @ `services/control-plane/internal/server/server.go:277`; `deleteWorkspace` @ `services/control-plane/internal/server/workspace_delete.go:156`

### 8. GET /api/announcements

注册：`services/control-plane/internal/server/routes_announcements.go:53`；`registerAnnouncementRoutes`。

实际调用：`listActiveAnnouncements`, `protected`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	app.listActiveAnnouncements(w, r)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`listActiveAnnouncements` @ `services/control-plane/internal/server/routes_announcements.go:76`; `protected` @ `services/control-plane/internal/server/server.go:277`

### 9. GET /api/auth/me

注册：`services/control-plane/internal/server/routes_auth.go:47`；`registerAuthRoutes`。

实际调用：`Context`, `Cookie`, `FormatInt`, `Get`, `Header`, `Set`, `Sub2APIUser`, `Sub2APIUserWithCredential`, `customerOwned`, `findUserByID`, `normalizeEmail`, `protected`, `sessionLookupKey`, `sessionUserContext`, `stringValue`, `sub2APIUserID`, `writeError`, `writeSourceEnvelope`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	user, ok := app.sessionUserContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "not_authenticated")
		return
	}
	userID, err := app.sub2APIUserID(r.Context(), stringValue(user["accountId"]))
	if err != nil {
		writeSourceEnvelope(w, http.StatusInternalServerError, "sub2api", "unavailable", nil)
		return
	}
	var identity clients.Sub2APIIdentity
	if app.deployment.customerOwned() {
		cookie, cookieErr := r.Cookie(sessionCookieName)
		if cookieErr != nil {
			writeError(w, http.StatusUnauthorized, "reauthentication_required")
			return
		}
		credential, credentialOK := app.sessionCredentials.Get(sessionLookupKey(cookie.Value))
		if !credentialOK {
			writeError(w, http.StatusUnauthorized, "reauthentication_required")
			return
		}
		identity, err = service.Sub2APIUserWithCredential(r.Context(), credential, userID, stringValue(user["email"]))
	} else {
		identity, err = service.Sub2APIUser(r.Context(), userID)
	}
	if err != nil {
		writeSourceEnvelope(w, http.StatusBadGateway, "sub2api", "unavailable", nil)
		return
	}
	mappedUser, err := app.findUserByID(r.Context(), stringValue(user["id"]))
	if err != nil {
		writeSourceEnvelope(w, http.StatusInternalServerError, "sub2api", "unavailable", nil)
		return
	}
	if mappedUser == nil || stringValue(mappedUser["accountId"]) != stringValue(user["accountId"]) || normalizeEmail(stringValue(mappedUser["email"])) != identity.Email {
		writeSourceEnvelope(w, http.StatusBadGateway, "sub2api", "unavailable", nil)
		return
	}
	writeSourceEnvelope(w, http.StatusOK, "sub2api", "available", map[string]any{
		"consoleUserId":	stringValue(user["id"]), "accountId": stringValue(user["accountId"]), "role": stringValue(user["role"]),
		"sub2apiUserId":	strconv.FormatInt(identity.ID, 10), "email": identity.Email, "status": identity.Status,
	})
})
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeSourceEnvelope(w, http.StatusBadGateway, "sub2api", "unavailable", nil)`
- `writeSourceEnvelope(w, http.StatusInternalServerError, "sub2api", "unavailable", nil)`
- `writeSourceEnvelope(w, http.StatusOK, "sub2api", "available", map[string]any{<br>	"consoleUserId":	stringValue(user["id"]), "accountId": stringValue(user["accountId"]), "role": stringValue(user["role"]),<br>	"sub2apiUserId":	strconv.FormatInt(identity.ID, 10), "email": identity.Email, "status": identity.Status,<br>})`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Sub2APIUser` @ `services/control-plane/internal/controlplane/service.go:180`; `Sub2APIUserWithCredential` @ `services/control-plane/internal/controlplane/service.go:196`; `stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `sessionLookupKey` @ `services/control-plane/internal/server/auth.go:38`; `sessionUserContext` @ `services/control-plane/internal/server/auth_accounts.go:435`; `findUserByID` @ `services/control-plane/internal/server/auth_accounts.go:472`; `customerOwned` @ `services/control-plane/internal/server/deployment_profile.go:57`; `sub2APIUserID` @ `services/control-plane/internal/server/monthly_billing.go:71`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `Get` @ `services/control-plane/internal/server/session_credential_vault.go:38`; `writeSourceEnvelope` @ `services/control-plane/internal/server/source_envelope.go:10`; `normalizeEmail` @ `services/control-plane/internal/server/table_store.go:339`

### 10. GET /api/billing/receipts

注册：`services/control-plane/internal/server/routes_billing.go:20`；`registerBillingRoutes`。

实际调用：`Atoi`, `BillingReceipts`, `Context`, `Get`, `HasPrefix`, `Query`, `TrimSpace`, `append`, `len`, `make`, `projectCustomerBillingReceipt`, `protected`, `sessionUserContext`, `stringValue`, `writeError`, `writeSourceEnvelope`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	user, ok := app.sessionUserContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "not_authenticated")
		return
	}
	accountID := stringValue(user["accountId"])
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(w, http.StatusBadRequest, "invalid_billing_receipt_limit")
			return
		}
		limit = parsed
	}
	page, err := service.BillingReceipts(r.Context(), clients.ReceiptQuery{AccountID: accountID, TypePrefix: "billing.", IncludeType: "gateway.wallet_adjustment.v1", IncludeExecutionKind: "business_refund", Cursor: r.URL.Query().Get("cursor"), Limit: limit})
	if err != nil {
		writeSourceEnvelope(w, http.StatusBadGateway, "ledger", "unavailable", nil)
		return
	}
	receipts := make([]any, 0, len(page.Receipts))
	for _, receipt := range page.Receipts {
		if receipt.AccountID != accountID {
			writeSourceEnvelope(w, http.StatusBadGateway, "ledger", "unavailable", nil)
			return
		}
		if !strings.HasPrefix(receipt.Type, "billing.") && (receipt.Type != "gateway.wallet_adjustment.v1" || receipt.Execution["kind"] != "business_refund") {
			writeSourceEnvelope(w, http.StatusBadGateway, "ledger", "unavailable", nil)
			return
		}
		projected, ok := app.projectCustomerBillingReceipt(r.Context(), receipt)
		if !ok {
			writeSourceEnvelope(w, http.StatusBadGateway, "ledger", "unavailable", nil)
			return
		}
		receipts = append(receipts, projected)
	}
	status := "available"
	if len(receipts) == 0 {
		status = "empty"
	}
	writeSourceEnvelope(w, http.StatusOK, "ledger", status, map[string]any{"receipts": receipts, "nextCursor": page.NextCursor, "hasMore": page.HasMore})
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `cursor` | `string` | query |
| `limit` | `string` | query |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeSourceEnvelope(w, http.StatusBadGateway, "ledger", "unavailable", nil)`
- `writeSourceEnvelope(w, http.StatusOK, "ledger", status, map[string]any{"receipts": receipts, "nextCursor": page.NextCursor, "hasMore": page.HasMore})`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`BillingReceipts` @ `services/control-plane/internal/controlplane/service.go:356`; `stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `sessionUserContext` @ `services/control-plane/internal/server/auth_accounts.go:435`; `projectCustomerBillingReceipt` @ `services/control-plane/internal/server/routes_billing.go:188`; `projectCustomerBillingReceipt` @ `services/control-plane/internal/server/routes_billing.go:336`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `Get` @ `services/control-plane/internal/server/session_credential_vault.go:38`; `writeSourceEnvelope` @ `services/control-plane/internal/server/source_envelope.go:10`

### 11. GET /api/billing/receipts/{id}

注册：`services/control-plane/internal/server/routes_billing.go:64`；`registerBillingRoutes`。

实际调用：`BillingReceiptForAccount`, `Context`, `PathValue`, `TrimSpace`, `projectCustomerBillingReceipt`, `projectWorkspaceCreatedReceipt`, `projectWorkspaceDeletedReceipt`, `protected`, `sessionUserContext`, `stringValue`, `writeError`, `writeSourceEnvelope`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	user, ok := app.sessionUserContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "not_authenticated")
		return
	}
	accountID := stringValue(user["accountId"])
	receiptID := strings.TrimSpace(r.PathValue("id"))
	receipt, err := service.BillingReceiptForAccount(r.Context(), accountID, "", receiptID)
	if err != nil {
		writeSourceEnvelope(w, http.StatusBadGateway, "ledger", "unavailable", nil)
		return
	}
	if receipt.ReceiptID != receiptID {
		writeSourceEnvelope(w, http.StatusBadGateway, "ledger", "unavailable", nil)
		return
	}
	if receipt.AccountID != accountID {
		writeError(w, http.StatusNotFound, "billing_receipt_not_found")
		return
	}
	var projected map[string]any
	var projectedOK bool
	switch receipt.Type {
	case "workspace.created":
		projected, projectedOK = projectWorkspaceCreatedReceipt(receipt)
	case "workspace.deleted.v1":

		projected, projectedOK = projectWorkspaceDeletedReceipt(receipt)
	default:
		projected, projectedOK = app.projectCustomerBillingReceipt(r.Context(), receipt)
	}
	if !projectedOK {
		writeSourceEnvelope(w, http.StatusBadGateway, "ledger", "unavailable", nil)
		return
	}
	writeSourceEnvelope(w, http.StatusOK, "ledger", "available", projected)
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `id` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeSourceEnvelope(w, http.StatusBadGateway, "ledger", "unavailable", nil)`
- `writeSourceEnvelope(w, http.StatusOK, "ledger", "available", projected)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`BillingReceiptForAccount` @ `services/control-plane/internal/controlplane/service.go:346`; `stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `sessionUserContext` @ `services/control-plane/internal/server/auth_accounts.go:435`; `projectCustomerBillingReceipt` @ `services/control-plane/internal/server/routes_billing.go:188`; `projectWorkspaceCreatedReceipt` @ `services/control-plane/internal/server/routes_billing.go:271`; `projectWorkspaceDeletedReceipt` @ `services/control-plane/internal/server/routes_billing.go:292`; `projectCustomerBillingReceipt` @ `services/control-plane/internal/server/routes_billing.go:336`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `writeSourceEnvelope` @ `services/control-plane/internal/server/source_envelope.go:10`

### 12. GET /api/billing/workspace-settlements

注册：`services/control-plane/internal/server/routes_billing.go:103`；`registerBillingRoutes`。

实际调用：`Context`, `Format`, `Now`, `append`, `len`, `make`, `netUSDMicros`, `projectWorkspaceSettlementTrend`, `protected`, `sessionUserContext`, `stringValue`, `writeError`, `writeSourceEnvelope`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	user, ok := app.sessionUserContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "not_authenticated")
		return
	}
	accountID := stringValue(user["accountId"])
	trend, err := app.projectWorkspaceSettlementTrend(r.Context(), service, accountID, time.Now())
	if err != nil {
		writeSourceEnvelope(w, http.StatusBadGateway, "control_plane", "unavailable", nil)
		return
	}
	days := make([]any, 0, len(trend.days))
	for _, day := range trend.days {
		days = append(days, map[string]any{
			"date":	day.date, "chargedUsdMicros": day.chargedUSDMicros, "refundedUsdMicros": day.refundedUSDMicros,
			"netUsdMicros":	day.chargedUSDMicros - day.refundedUSDMicros, "chargeCount": day.chargeCount, "refundCount": day.refundCount,
		})
	}
	status := "available"

	if trend.settledCount == 0 && trend.unconfirmedCount == 0 && trend.inFlightCount == 0 && trend.unattributedCount == 0 {
		status = "empty"
	}
	writeSourceEnvelope(w, http.StatusOK, "control_plane", status, map[string]any{
		"timezone":	workspaceSettlementTrendTimezone, "asOf": trend.asOf.Format(time.RFC3339Nano),
		"windowStart":	trend.windowStart.Format(time.RFC3339), "windowEnd": trend.windowEnd.Format(time.RFC3339),
		"days":	days, "chargedUsdMicros": trend.chargedUSDMicros, "refundedUsdMicros": trend.refundedUSDMicros, "netUsdMicros": trend.netUSDMicros(),
		"settledCount":	trend.settledCount, "inFlightCount": trend.inFlightCount, "unconfirmedCount": trend.unconfirmedCount,
		"unattributedCount":	trend.unattributedCount, "outOfWindowCount": trend.outOfWindowCount,
		"complete":	trend.unconfirmedCount == 0,
	})
})
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeSourceEnvelope(w, http.StatusBadGateway, "control_plane", "unavailable", nil)`
- `writeSourceEnvelope(w, http.StatusOK, "control_plane", status, map[string]any{<br>	"timezone":	workspaceSettlementTrendTimezone, "asOf": trend.asOf.Format(time.RFC3339Nano),<br>	"windowStart":	trend.windowStart.Format(time.RFC3339), "windowEnd": trend.windowEnd.Format(time.RFC3339),<br>	"days":	days, "chargedUsdMicros": trend.chargedUSDMicros, "refundedUsdMicros": trend.refundedUSDMicros, "netUsdMicros": trend.netUSDMicros(),<br>	"settledCount":	trend.settledCount, "inFlightCount": trend.inFlightCount, "unconfirmedCount": trend.unconfirmedCount,<br>	"unattributedCount":	trend.unattributedCount, "outOfWindowCount": trend.outOfWindowCount,<br>	"complete":	trend.unconfirmedCount == 0,<br>})`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `sessionUserContext` @ `services/control-plane/internal/server/auth_accounts.go:435`; `projectWorkspaceSettlementTrend` @ `services/control-plane/internal/server/billing_settlement_trend.go:225`; `netUSDMicros` @ `services/control-plane/internal/server/billing_settlement_trend.go:85`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `writeSourceEnvelope` @ `services/control-plane/internal/server/source_envelope.go:10`

### 13. GET /api/gateway/balance-history

注册：`services/control-plane/internal/server/routes_gateway.go:118`；`registerGatewayRoutes`。

实际调用：`Context`, `Format`, `FormatInt`, `GatewayBalanceHistoryPage`, `Header`, `Set`, `UTC`, `append`, `gatewayBalanceHistoryPagination`, `gatewaySub2APIUserID`, `len`, `make`, `protected`, `writeGatewaySourceError`, `writeSourceEnvelope`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	userID, ok := app.gatewaySub2APIUserID(w, r)
	if !ok {
		return
	}
	pageNumber, pageSize, ok := gatewayBalanceHistoryPagination(w, r)
	if !ok {
		return
	}
	history, err := service.GatewayBalanceHistoryPage(r.Context(), userID, clients.Sub2APIBalanceHistoryPageQuery{Page: pageNumber, PageSize: pageSize})
	if err != nil {
		writeGatewaySourceError(w, err)
		return
	}
	items := make([]any, 0, len(history.Items))
	for _, entry := range history.Items {
		var usedAt any
		if entry.UsedAt != nil {
			usedAt = entry.UsedAt.UTC().Format(time.RFC3339Nano)
		}
		items = append(items, map[string]any{
			"type":	entry.Type, "valueUsdMicros": strconv.FormatInt(entry.ValueUSDMicros, 10), "status": entry.Status,
			"usedAt":	usedAt, "createdAt": entry.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	status := "available"
	if len(items) == 0 {
		status = "empty"
	}
	writeSourceEnvelope(w, http.StatusOK, "sub2api", status, map[string]any{"items": items, "total": history.Total, "page": history.Page, "pageSize": history.PageSize, "pages": history.Pages})
})
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeSourceEnvelope(w, http.StatusOK, "sub2api", status, map[string]any{"items": items, "total": history.Total, "page": history.Page, "pageSize": history.PageSize, "pages": history.Pages})`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`GatewayBalanceHistoryPage` @ `services/control-plane/internal/controlplane/service.go:315`; `gatewaySub2APIUserID` @ `services/control-plane/internal/server/routes_gateway.go:1005`; `gatewayBalanceHistoryPagination` @ `services/control-plane/internal/server/routes_gateway.go:890`; `writeGatewaySourceError` @ `services/control-plane/internal/server/routes_gateway.go:997`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeSourceEnvelope` @ `services/control-plane/internal/server/source_envelope.go:10`

### 14. GET /api/gateway/endpoint

注册：`services/control-plane/internal/server/routes_gateway.go:20`；`registerGatewayRoutes`。

实际调用：`GatewayPublicEndpoint`, `Header`, `Set`, `protected`, `writeSourceEnvelope`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	endpoint, err := service.GatewayPublicEndpoint()
	if err != nil {
		writeSourceEnvelope(w, http.StatusInternalServerError, "sub2api", "unavailable", nil)
		return
	}
	writeSourceEnvelope(w, http.StatusOK, "sub2api", "available", map[string]any{"baseUrl": endpoint})
})
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeSourceEnvelope(w, http.StatusInternalServerError, "sub2api", "unavailable", nil)`
- `writeSourceEnvelope(w, http.StatusOK, "sub2api", "available", map[string]any{"baseUrl": endpoint})`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`GatewayPublicEndpoint` @ `services/control-plane/internal/controlplane/service.go:132`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeSourceEnvelope` @ `services/control-plane/internal/server/source_envelope.go:10`

### 15. GET /api/gateway/groups

注册：`services/control-plane/internal/server/routes_gateway.go:29`；`registerGatewayRoutes`。

实际调用：`Context`, `FormatInt`, `GatewayUserGroups`, `Header`, `Set`, `append`, `gatewayUserContext`, `len`, `make`, `protected`, `writeGatewaySourceError`, `writeSourceEnvelope`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	_, userID, credential, ok := app.gatewayUserContext(w, r)
	if !ok {
		return
	}
	groups, err := service.GatewayUserGroups(r.Context(), credential, userID)
	if err != nil {
		writeGatewaySourceError(w, err)
		return
	}
	items := make([]any, 0, len(groups))
	for _, group := range groups {
		items = append(items, map[string]any{
			"id":	strconv.FormatInt(group.ID, 10), "name": group.Name, "description": group.Description,
			"platform":	group.Platform, "rateMultiplier": group.RateMultiplier,
			"subscriptionType":	group.SubscriptionType, "status": group.Status,
		})
	}
	status := "available"
	if len(items) == 0 {
		status = "empty"
	}
	writeSourceEnvelope(w, http.StatusOK, "sub2api", status, map[string]any{"items": items, "total": len(items)})
})
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeSourceEnvelope(w, http.StatusOK, "sub2api", status, map[string]any{"items": items, "total": len(items)})`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`GatewayUserGroups` @ `services/control-plane/internal/controlplane/service.go:124`; `gatewayUserContext` @ `services/control-plane/internal/server/routes_gateway.go:795`; `writeGatewaySourceError` @ `services/control-plane/internal/server/routes_gateway.go:997`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeSourceEnvelope` @ `services/control-plane/internal/server/source_envelope.go:10`

### 16. GET /api/gateway/keys

注册：`services/control-plane/internal/server/routes_gateway.go:69`；`registerGatewayRoutes`。

实际调用：`Context`, `GatewayUserKeyPage`, `Header`, `Set`, `append`, `gatewayKeyPageQuery`, `gatewayKeySummary`, `gatewayUserContext`, `len`, `make`, `protected`, `writeGatewaySourceError`, `writeSourceEnvelope`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	_, userID, credential, ok := app.gatewayUserContext(w, r)
	if !ok {
		return
	}
	query, ok := gatewayKeyPageQuery(w, r)
	if !ok {
		return
	}
	page, err := service.GatewayUserKeyPage(r.Context(), credential, userID, query)
	if err != nil {
		writeGatewaySourceError(w, err)
		return
	}
	items := make([]any, 0, len(page.Items))
	for _, key := range page.Items {
		items = append(items, gatewayKeySummary(key))
	}
	status := "available"
	if len(items) == 0 {
		status = "empty"
	}
	writeSourceEnvelope(w, http.StatusOK, "sub2api", status, map[string]any{"items": items, "total": page.Total, "page": page.Page, "pageSize": page.PageSize, "pages": page.Pages})
})
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeSourceEnvelope(w, http.StatusOK, "sub2api", status, map[string]any{"items": items, "total": page.Total, "page": page.Page, "pageSize": page.PageSize, "pages": page.Pages})`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`GatewayUserKeyPage` @ `services/control-plane/internal/controlplane/service.go:116`; `gatewayKeySummary` @ `services/control-plane/internal/server/routes_gateway.go:658`; `gatewayUserContext` @ `services/control-plane/internal/server/routes_gateway.go:795`; `gatewayKeyPageQuery` @ `services/control-plane/internal/server/routes_gateway.go:833`; `writeGatewaySourceError` @ `services/control-plane/internal/server/routes_gateway.go:997`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeSourceEnvelope` @ `services/control-plane/internal/server/source_envelope.go:10`

### 17. GET /api/gateway/keys/{keyId}

注册：`services/control-plane/internal/server/routes_gateway.go:94`；`registerGatewayRoutes`。

实际调用：`gatewayKey`, `protected`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	app.gatewayKey(w, r, service)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`gatewayKey` @ `services/control-plane/internal/server/routes_gateway.go:200`; `protected` @ `services/control-plane/internal/server/server.go:277`

### 18. GET /api/gateway/keys/{keyId}/usage

注册：`services/control-plane/internal/server/routes_gateway.go:109`；`registerGatewayRoutes`。

实际调用：`gatewayKeyUsage`, `protected`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	app.gatewayKeyUsage(w, r, service)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`gatewayKeyUsage` @ `services/control-plane/internal/server/routes_gateway.go:595`; `protected` @ `services/control-plane/internal/server/server.go:277`

### 19. GET /api/gateway/keys/{keyId}/usage-summary

注册：`services/control-plane/internal/server/routes_gateway.go:112`；`registerGatewayRoutes`。

实际调用：`gatewayKeyUsageSummary`, `protected`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	app.gatewayKeyUsageSummary(w, r, service)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`gatewayKeyUsageSummary` @ `services/control-plane/internal/server/routes_gateway.go:620`; `protected` @ `services/control-plane/internal/server/server.go:277`

### 20. GET /api/gateway/usage-summary

注册：`services/control-plane/internal/server/routes_gateway.go:115`；`registerGatewayRoutes`。

实际调用：`gatewayAccountUsageSummary`, `protected`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	app.gatewayAccountUsageSummary(w, r, service)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`gatewayAccountUsageSummary` @ `services/control-plane/internal/server/routes_gateway.go:641`; `protected` @ `services/control-plane/internal/server/server.go:277`

### 21. GET /api/gateway/wallet

注册：`services/control-plane/internal/server/routes_gateway.go:54`；`registerGatewayRoutes`。

实际调用：`Context`, `FormatInt`, `Header`, `Set`, `Sub2APIBalance`, `gatewaySub2APIUserID`, `protected`, `writeGatewaySourceError`, `writeSourceEnvelope`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	userID, ok := app.gatewaySub2APIUserID(w, r)
	if !ok {
		return
	}
	balance, err := service.Sub2APIBalance(r.Context(), userID)
	if err != nil {
		writeGatewaySourceError(w, err)
		return
	}
	writeSourceEnvelope(w, http.StatusOK, "sub2api", "available", map[string]any{
		"userId":	strconv.FormatInt(balance.UserID, 10), "currency": "USD", "usdMicros": strconv.FormatInt(balance.USDMicros, 10), "status": balance.Status,
	})
})
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeSourceEnvelope(w, http.StatusOK, "sub2api", "available", map[string]any{<br>	"userId":	strconv.FormatInt(balance.UserID, 10), "currency": "USD", "usdMicros": strconv.FormatInt(balance.USDMicros, 10), "status": balance.Status,<br>})`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Sub2APIBalance` @ `services/control-plane/internal/controlplane/monthly_billing.go:22`; `gatewaySub2APIUserID` @ `services/control-plane/internal/server/routes_gateway.go:1005`; `writeGatewaySourceError` @ `services/control-plane/internal/server/routes_gateway.go:997`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeSourceEnvelope` @ `services/control-plane/internal/server/source_envelope.go:10`

### 22. GET /api/healthz

注册：`services/control-plane/internal/server/routes_core.go:13`；`registerCoreRoutes`。

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

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`writeJSON` @ `services/control-plane/internal/server/server.go:335`

### 23. GET /api/management/state

注册：`services/control-plane/internal/server/routes_state.go:44`；`registerStateRoutes`。

实际调用：`Get`, `Query`, `fabricComputePools`, `managementState`, `protected`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	computePools, ok := fabricComputePools(w, r, service)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, app.managementState(r.URL.Query().Get("includeDeleted") == "true", computePools))
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `includeDeleted` | `string` | query |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, app.managementState(r.URL.Query().Get("includeDeleted") == "true", computePools))`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`managementState` @ `services/control-plane/internal/server/admin_ops.go:34`; `protected` @ `services/control-plane/internal/server/server.go:277`; `fabricComputePools` @ `services/control-plane/internal/server/server.go:326`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `Get` @ `services/control-plane/internal/server/session_credential_vault.go:38`

### 24. GET /api/operator/account-reconciliation

注册：`services/control-plane/internal/server/account_reconcile.go:64`；`registerAcceptanceBAccountReconcileRoute`。

实际调用：`Context`, `Get`, `Is`, `canonicalEmail`, `protected`, `reconcileAcceptanceBAccount`, `writeError`, `writeSourceEnvelope`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	email, err := canonicalEmail(r.Header.Get(acceptanceBAccountReconcileHeader))
	if err != nil {
		writeError(w, http.StatusBadRequest, "acceptance_b_account_reconcile_email_invalid")
		return
	}
	data, err := app.reconcileAcceptanceBAccount(r.Context(), service, email)
	if err != nil {
		if errors.Is(err, errAcceptanceBAccountReconcileUnknown) {

			writeSourceEnvelope(w, http.StatusOK, "control-plane+sub2api+ledger", "available", data)
			return
		}
		writeSourceEnvelope(w, http.StatusOK, "control-plane+sub2api+ledger", "available", data)
		return
	}
	writeSourceEnvelope(w, http.StatusOK, "control-plane+sub2api+ledger", "available", data)
})
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeSourceEnvelope(w, http.StatusOK, "control-plane+sub2api+ledger", "available", data)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`reconcileAcceptanceBAccount` @ `services/control-plane/internal/server/account_reconcile.go:134`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `Get` @ `services/control-plane/internal/server/session_credential_vault.go:38`; `writeSourceEnvelope` @ `services/control-plane/internal/server/source_envelope.go:10`; `canonicalEmail` @ `services/control-plane/internal/server/table_store.go:341`

### 25. GET /api/operator/accounts

注册：`services/control-plane/internal/server/routes_admin.go:421`；`registerAdminRoutes`。

实际调用：`Context`, `operatorAccountPage`, `operatorPagination`, `protected`, `writeSourceEnvelope`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	page, pageSize, ok := operatorPagination(w, r)
	if !ok {
		return
	}
	data, status, err := app.operatorAccountPage(r.Context(), service, page, pageSize)
	if err != nil {
		writeSourceEnvelope(w, http.StatusBadGateway, "control-plane+sub2api", "unavailable", nil)
		return
	}
	writeSourceEnvelope(w, http.StatusOK, "control-plane+sub2api", status, data)
})
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeSourceEnvelope(w, http.StatusBadGateway, "control-plane+sub2api", "unavailable", nil)`
- `writeSourceEnvelope(w, http.StatusOK, "control-plane+sub2api", status, data)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`operatorPagination` @ `services/control-plane/internal/server/routes_admin.go:675`; `operatorAccountPage` @ `services/control-plane/internal/server/routes_admin.go:748`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeSourceEnvelope` @ `services/control-plane/internal/server/source_envelope.go:10`

### 26. GET /api/operator/announcements

注册：`services/control-plane/internal/server/routes_announcements.go:59`；`registerAnnouncementRoutes`。

实际调用：`listOperatorAnnouncements`, `protected`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	app.listOperatorAnnouncements(w, r)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`listOperatorAnnouncements` @ `services/control-plane/internal/server/routes_announcements.go:110`; `protected` @ `services/control-plane/internal/server/server.go:277`

### 27. GET /api/operator/application-data-materials/{applicationId}/{version}

注册：`services/control-plane/internal/server/workspace_application_data_admission.go:89`；`registerApplicationDataMaterialRoutes`。

实际调用：`AdmittedApplicationDataMaterial`, `Context`, `PathValue`, `protected`, `string`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	row, found, err := app.tables.AdmittedApplicationDataMaterial(r.Context(), r.PathValue("applicationId"), r.PathValue("version"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "workspace_application_data_material_not_found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"decision": string(application.AdmissionIdentical), "dataMaterial": row})
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `applicationId` | `string` | path |
| `version` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, map[string]any{"decision": string(application.AdmissionIdentical), "dataMaterial": row})`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`AdmittedApplicationDataMaterial` @ `services/control-plane/internal/server/application_data_material_store.go:43`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`

### 28. GET /api/operator/application-deployments/{operationId}

注册：`services/control-plane/internal/server/workspace_application_deployment.go:576`；`registerApplicationDeploymentRoutes`。

实际调用：`Context`, `GetRuntimeOperation`, `PathValue`, `decodeWorkspaceApplicationDeploymentIntent`, `protected`, `stringValue`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	row, found, err := app.tables.GetRuntimeOperation(r.Context(), r.PathValue("operationId"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if !found || stringValue(row["action"]) != workspaceApplicationDeploymentAction {
		writeError(w, http.StatusNotFound, "workspace_application_deployment_not_found")
		return
	}
	intent, err := decodeWorkspaceApplicationDeploymentIntent(row)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "workspace_application_deployment_intent_invalid")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": stringValue(row["status"]), "intent": intent})
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `operationId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, map[string]any{"status": stringValue(row["status"]), "intent": intent})`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `GetRuntimeOperation` @ `services/control-plane/internal/server/ent_state_store.go:694`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `decodeWorkspaceApplicationDeploymentIntent` @ `services/control-plane/internal/server/workspace_application_deployment.go:120`

### 29. GET /api/operator/application-revisions/{applicationId}/{version}

注册：`services/control-plane/internal/server/workspace_application_admission.go:93`；`registerApplicationRevisionRoutes`。

实际调用：`AdmittedApplicationRevision`, `Context`, `PathValue`, `protected`, `string`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	row, found, err := app.tables.AdmittedApplicationRevision(r.Context(), r.PathValue("applicationId"), r.PathValue("version"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "workspace_application_revision_not_found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"decision": string(application.AdmissionIdentical), "revision": row})
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `applicationId` | `string` | path |
| `version` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, map[string]any{"decision": string(application.AdmissionIdentical), "revision": row})`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`AdmittedApplicationRevision` @ `services/control-plane/internal/server/application_revision_store.go:46`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`

### 30. GET /api/operator/archive

注册：`services/control-plane/internal/server/routes_admin.go:604`；`registerAdminRoutes`。

实际调用：`Context`, `archiveState`, `protected`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	result, err := app.archiveState(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "archive_state_failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
})
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, result)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`archiveState` @ `services/control-plane/internal/server/admin_ops.go:164`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`

### 31. GET /api/operator/health

注册：`services/control-plane/internal/server/routes_admin.go:486`；`registerAdminRoutes`。

实际调用：`Context`, `operatorHealth`, `protected`, `writeSourceEnvelope`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	writeSourceEnvelope(w, http.StatusOK, "control-plane", "available", app.operatorHealth(r.Context(), service))
})
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeSourceEnvelope(w, http.StatusOK, "control-plane", "available", app.operatorHealth(r.Context(), service))`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`operatorHealth` @ `services/control-plane/internal/server/routes_admin.go:1479`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeSourceEnvelope` @ `services/control-plane/internal/server/source_envelope.go:10`

### 32. GET /api/operator/overview

注册：`services/control-plane/internal/server/routes_admin.go:434`；`registerAdminRoutes`。

实际调用：`Context`, `operatorOverview`, `protected`, `writeSourceEnvelope`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	data, err := app.operatorOverview(r.Context(), service)
	if err != nil {
		writeSourceEnvelope(w, http.StatusBadGateway, "control-plane", "unavailable", nil)
		return
	}
	writeSourceEnvelope(w, http.StatusOK, "control-plane", "available", data)
})
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeSourceEnvelope(w, http.StatusBadGateway, "control-plane", "unavailable", nil)`
- `writeSourceEnvelope(w, http.StatusOK, "control-plane", "available", data)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`operatorOverview` @ `services/control-plane/internal/server/routes_admin.go:1336`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeSourceEnvelope` @ `services/control-plane/internal/server/source_envelope.go:10`

### 33. GET /api/operator/reconciliation

注册：`services/control-plane/internal/server/routes_admin.go:474`；`registerAdminRoutes`。

实际调用：`Context`, `operatorPagination`, `operatorReconciliationPage`, `protected`, `writeSourceEnvelope`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	page, pageSize, ok := operatorPagination(w, r)
	if !ok {
		return
	}
	data, status, err := app.operatorReconciliationPage(r.Context(), page, pageSize)
	if err != nil {
		writeSourceEnvelope(w, http.StatusInternalServerError, "control-plane", "unavailable", nil)
		return
	}
	writeSourceEnvelope(w, http.StatusOK, "control-plane", status, data)
})
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeSourceEnvelope(w, http.StatusInternalServerError, "control-plane", "unavailable", nil)`
- `writeSourceEnvelope(w, http.StatusOK, "control-plane", status, data)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`operatorReconciliationPage` @ `services/control-plane/internal/server/routes_admin.go:1398`; `operatorPagination` @ `services/control-plane/internal/server/routes_admin.go:675`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeSourceEnvelope` @ `services/control-plane/internal/server/source_envelope.go:10`

### 34. GET /api/operator/registry/repositories

注册：`services/control-plane/internal/server/workspace_registry_catalog.go:138`；`registerWorkspaceRegistryCatalogRoutesWithCatalog`。

实际调用：`Get`, `Query`, `TrimSpace`, `protected`, `repositories`, `requireWorkspaceRegistryCatalog`, `writeJSON`, `writeWorkspaceRegistryError`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	if !requireWorkspaceRegistryCatalog(w, catalog) {
		return
	}
	namespace := strings.TrimSpace(r.URL.Query().Get("namespace"))
	response, err := catalog.repositories(namespace)
	if err != nil {
		writeWorkspaceRegistryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `namespace` | `string` | query |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, response)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `Get` @ `services/control-plane/internal/server/session_credential_vault.go:38`; `requireWorkspaceRegistryCatalog` @ `services/control-plane/internal/server/workspace_registry_catalog.go:197`; `writeWorkspaceRegistryError` @ `services/control-plane/internal/server/workspace_registry_catalog.go:218`; `repositories` @ `services/control-plane/internal/server/workspace_registry_catalog.go:97`

### 35. GET /api/operator/registry/tags/{namespace}/{repository}

注册：`services/control-plane/internal/server/workspace_registry_catalog.go:150`；`registerWorkspaceRegistryCatalogRoutesWithCatalog`。

实际调用：`Context`, `protected`, `requireWorkspaceRegistryCatalog`, `tags`, `workspaceRegistryPathParams`, `writeJSON`, `writeWorkspaceRegistryError`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	if !requireWorkspaceRegistryCatalog(w, catalog) {
		return
	}
	namespace, repository, ok := workspaceRegistryPathParams(w, r)
	if !ok {
		return
	}
	tags, err := catalog.tags(r.Context(), namespace, repository)
	if err != nil {
		writeWorkspaceRegistryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"namespace": namespace, "repository": repository, "tags": tags})
})
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, map[string]any{"namespace": namespace, "repository": repository, "tags": tags})`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `tags` @ `services/control-plane/internal/server/workspace_registry_catalog.go:116`; `requireWorkspaceRegistryCatalog` @ `services/control-plane/internal/server/workspace_registry_catalog.go:197`; `workspaceRegistryPathParams` @ `services/control-plane/internal/server/workspace_registry_catalog.go:205`; `writeWorkspaceRegistryError` @ `services/control-plane/internal/server/workspace_registry_catalog.go:218`

### 36. GET /api/operator/runtime-observations

注册：`services/control-plane/internal/server/routes_admin.go:442`；`registerAdminRoutes`。

实际调用：`Context`, `Now`, `UTC`, `operatorRuntimeObservations`, `protected`, `writeSourceEnvelope`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	data, err := app.operatorRuntimeObservations(r.Context(), service, time.Now().UTC())
	if err != nil {
		writeSourceEnvelope(w, http.StatusBadGateway, "control-plane+fabric", "unavailable", nil)
		return
	}
	writeSourceEnvelope(w, http.StatusOK, "control-plane+fabric", "available", data)
})
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeSourceEnvelope(w, http.StatusBadGateway, "control-plane+fabric", "unavailable", nil)`
- `writeSourceEnvelope(w, http.StatusOK, "control-plane+fabric", "available", data)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`operatorRuntimeObservations` @ `services/control-plane/internal/server/operator_runtime_observations.go:49`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeSourceEnvelope` @ `services/control-plane/internal/server/source_envelope.go:10`

### 37. GET /api/operator/wallet-adjustments/{operationId}

注册：`services/control-plane/internal/server/routes_admin.go:415`；`registerAdminRoutes`。

实际调用：`getWalletAdjustment`, `protected`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	app.getWalletAdjustment(w, r)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`protected` @ `services/control-plane/internal/server/server.go:277`; `getWalletAdjustment` @ `services/control-plane/internal/server/wallet_adjustment.go:176`

### 38. GET /api/operator/workspace-launches/{operationId}/canonical-facts-repair-preview

注册：`services/control-plane/internal/server/routes_admin.go:189`；`registerAdminRoutes`。

实际调用：`Context`, `Error`, `Header`, `PathValue`, `Set`, `TrimSpace`, `previewWorkspaceLaunchCanonicalFactRepair`, `protected`, `workspaceLaunchCanonicalFactRepairPreviewResponse`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	operationID := strings.TrimSpace(r.PathValue("operationId"))
	preview, err := app.previewWorkspaceLaunchCanonicalFactRepair(r.Context(), service, operationID)
	if err != nil {
		writeError(w, http.StatusConflict, errWorkspaceLaunchCanonicalFactRepairNotEligible.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, workspaceLaunchCanonicalFactRepairPreviewResponse(preview))
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `operationId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, workspaceLaunchCanonicalFactRepairPreviewResponse(preview))`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/control-plane/internal/clients/fabric.go:96`; `Error` @ `services/control-plane/internal/clients/sub2api.go:432`; `Error` @ `services/control-plane/internal/clients/sub2api.go:446`; `Error` @ `services/control-plane/internal/clients/workspace_registry.go:84`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `workspaceLaunchCanonicalFactRepairPreviewResponse` @ `services/control-plane/internal/server/workspace_launch_canonical_fact_repair.go:107`; `previewWorkspaceLaunchCanonicalFactRepair` @ `services/control-plane/internal/server/workspace_launch_canonical_fact_repair.go:80`; `Error` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:75`

### 39. GET /api/operator/workspace-launches/{operationId}/disposable-reset-preview

注册：`services/control-plane/internal/server/routes_admin.go:136`；`registerAdminRoutes`。

实际调用：`Context`, `Error`, `Header`, `PathValue`, `Set`, `TrimSpace`, `previewWorkspaceLaunchDisposableReset`, `protected`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	operationID := strings.TrimSpace(r.PathValue("operationId"))
	preview, err := app.previewWorkspaceLaunchDisposableReset(r.Context(), service, operationID)
	w.Header().Set("Cache-Control", "no-store")
	if err != nil {
		if preview.SchemaVersion == 1 {
			writeJSON(w, http.StatusOK, preview)
			return
		}
		writeError(w, http.StatusConflict, errWorkspaceLaunchDisposableResetNotEligible.Error())
		return
	}
	writeJSON(w, http.StatusOK, preview)
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `operationId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, preview)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/control-plane/internal/clients/fabric.go:96`; `Error` @ `services/control-plane/internal/clients/sub2api.go:432`; `Error` @ `services/control-plane/internal/clients/sub2api.go:446`; `Error` @ `services/control-plane/internal/clients/workspace_registry.go:84`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `previewWorkspaceLaunchDisposableReset` @ `services/control-plane/internal/server/workspace_launch_disposable_reset.go:231`; `Error` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:75`

### 40. GET /api/operator/workspace-launches/{operationId}/recovery

注册：`services/control-plane/internal/server/routes_admin.go:40`；`registerAdminRoutes`。

实际调用：`Context`, `Error`, `GetRuntimeOperation`, `Header`, `PathValue`, `Set`, `TrimSpace`, `decodeWorkspaceLaunchReconcileOperation`, `protected`, `workspaceLaunchRecovery`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	row, found, err := app.tables.GetRuntimeOperation(r.Context(), strings.TrimSpace(r.PathValue("operationId")))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "workspace_launch_not_found")
		return
	}
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		writeError(w, http.StatusConflict, errInvalidWorkspaceLaunchOperation.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, app.workspaceLaunchRecovery(r.Context(), service, operation))
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `operationId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, app.workspaceLaunchRecovery(r.Context(), service, operation))`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/control-plane/internal/clients/fabric.go:96`; `Error` @ `services/control-plane/internal/clients/sub2api.go:432`; `Error` @ `services/control-plane/internal/clients/sub2api.go:446`; `Error` @ `services/control-plane/internal/clients/workspace_registry.go:84`; `GetRuntimeOperation` @ `services/control-plane/internal/server/ent_state_store.go:694`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `workspaceLaunchRecovery` @ `services/control-plane/internal/server/workspace_launch_closeout.go:187`; `decodeWorkspaceLaunchReconcileOperation` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:2250`; `Error` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:75`

### 41. GET /api/operator/workspace-launches/{operationId}/resume-approval-candidates

注册：`services/control-plane/internal/server/routes_admin.go:260`；`registerAdminRoutes`。

实际调用：`Context`, `Error`, `Getenv`, `Header`, `Is`, `Now`, `PathValue`, `Set`, `TrimSpace`, `Values`, `len`, `prepareProductionAcceptanceBResumeExisting`, `productionAcceptanceBResumeExistingPrepareRequestValid`, `protected`, `secureHeaderMatches`, `singleHeaderValue`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	capabilities := r.Header.Values(productionAcceptanceBCapability)
	if len(capabilities) != 1 || !secureHeaderMatches(strings.TrimSpace(capabilities[0]), strings.TrimSpace(os.Getenv("OPL_INTERNAL_SERVICE_TOKEN"))) {
		writeError(w, http.StatusUnauthorized, "acceptance_b_capability_invalid")
		return
	}
	operationID := strings.TrimSpace(r.PathValue("operationId"))
	approvalID, approvalOK := singleHeaderValue(r.Header, productionAcceptanceBApprovalID)
	authorizationID, authorizationOK := singleHeaderValue(r.Header, productionAcceptanceBResumeAuthorizationID)
	reasonSHA256, reasonOK := singleHeaderValue(r.Header, productionAcceptanceBResumeReasonSHA256)
	releaseSHA, releaseSHAOK := singleHeaderValue(r.Header, productionAcceptanceBResumeReleaseSHA)
	releaseTree, releaseTreeOK := singleHeaderValue(r.Header, productionAcceptanceBResumeReleaseTree)
	imageDigest, imageOK := singleHeaderValue(r.Header, productionAcceptanceBResumeImageDigest)
	if operationID == "" || !approvalOK || !authorizationOK || !reasonOK || !releaseSHAOK || !releaseTreeOK || !imageOK {
		writeError(w, http.StatusBadRequest, errInvalidBillingReview.Error())
		return
	}
	var request productionAcceptanceBResumeExistingPrepareRequest
	request.ApprovalID, request.AuthorizationID, request.ReasonSHA256 = approvalID, authorizationID, reasonSHA256
	request.Release.CanonicalCloudSHA, request.Release.CanonicalCloudTree, request.Release.DeployedCloudImageDigest = releaseSHA, releaseTree, imageDigest
	if !productionAcceptanceBResumeExistingPrepareRequestValid(request) {
		writeError(w, http.StatusBadRequest, errInvalidBillingReview.Error())
		return
	}
	approval, err := app.prepareProductionAcceptanceBResumeExisting(r.Context(), service, operationID, request, time.Now())
	if err != nil {
		if errors.Is(err, errBillingReviewNotFound) {
			writeError(w, http.StatusNotFound, "workspace_launch_not_found")
		} else {
			writeError(w, http.StatusConflict, errWorkspaceLaunchGrantConflict.Error())
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, approval)
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `operationId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, approval)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/control-plane/internal/clients/fabric.go:96`; `Error` @ `services/control-plane/internal/clients/sub2api.go:432`; `Error` @ `services/control-plane/internal/clients/sub2api.go:446`; `Error` @ `services/control-plane/internal/clients/workspace_registry.go:84`; `singleHeaderValue` @ `services/control-plane/internal/server/routes_admin.go:654`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `productionAcceptanceBResumeExistingPrepareRequestValid` @ `services/control-plane/internal/server/workspace_launch_admission.go:373`; `secureHeaderMatches` @ `services/control-plane/internal/server/workspace_launch_admission.go:555`; `Error` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:75`; `prepareProductionAcceptanceBResumeExisting` @ `services/control-plane/internal/server/workspace_launch_service.go:175`; `prepareProductionAcceptanceBResumeExisting` @ `services/control-plane/internal/server/workspace_launch_service.go:186`

### 42. GET /api/operator/workspace-launches/{operationId}/resume-authorizations/{authorizationId}

注册：`services/control-plane/internal/server/routes_admin.go:384`；`registerAdminRoutes`。

实际调用：`Context`, `GetRuntimeOperation`, `PathValue`, `TrimSpace`, `decodeWorkspaceLaunchReconcileOperation`, `protected`, `validBillingReviewOpaqueID`, `workspaceLaunchResumeAuthorizationReadback`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	operationID := strings.TrimSpace(r.PathValue("operationId"))
	authorizationID := strings.TrimSpace(r.PathValue("authorizationId"))
	if operationID == "" || !validBillingReviewOpaqueID(authorizationID) {
		writeError(w, http.StatusBadRequest, "invalid_resume_authorization")
		return
	}
	row, found, err := app.tables.GetRuntimeOperation(r.Context(), operationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "workspace_launch_not_found")
		return
	}
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	readback, found := workspaceLaunchResumeAuthorizationReadback(operation, authorizationID)
	if !found {
		writeError(w, http.StatusNotFound, "resume_authorization_not_found")
		return
	}
	writeJSON(w, http.StatusOK, readback)
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `authorizationId` | `string` | path |
| `operationId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, readback)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`GetRuntimeOperation` @ `services/control-plane/internal/server/ent_state_store.go:694`; `validBillingReviewOpaqueID` @ `services/control-plane/internal/server/routes_admin.go:1545`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `workspaceLaunchResumeAuthorizationReadback` @ `services/control-plane/internal/server/workspace_launch.go:192`; `decodeWorkspaceLaunchReconcileOperation` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:2250`

### 43. GET /api/operator/workspace-launches/{operationId}/stage-observation

注册：`services/control-plane/internal/server/routes_admin.go:116`；`registerAdminRoutes`。

实际调用：`Context`, `Getenv`, `Header`, `PathValue`, `Set`, `TrimSpace`, `Values`, `len`, `observeWorkspaceLaunchStage`, `protected`, `secureHeaderMatches`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	capabilities := r.Header.Values(productionAcceptanceBCapability)
	if len(capabilities) != 1 || !secureHeaderMatches(strings.TrimSpace(capabilities[0]), strings.TrimSpace(os.Getenv("OPL_INTERNAL_SERVICE_TOKEN"))) {
		writeError(w, http.StatusUnauthorized, "workspace_launch_stage_diagnostic_capability_invalid")
		return
	}
	operationID := strings.TrimSpace(r.PathValue("operationId"))
	adapter := &controlPlaneWorkspaceLaunchStageAdapter{app: app, service: service}
	diagnostic, found, err := observeWorkspaceLaunchStage(r.Context(), app.tables, adapter, operationID)
	w.Header().Set("Cache-Control", "no-store")
	if !found {
		writeError(w, http.StatusNotFound, "workspace_launch_not_found")
		return
	}
	if err != nil {
		writeError(w, http.StatusConflict, "workspace_launch_stage_diagnostic_not_available")
		return
	}
	writeJSON(w, http.StatusOK, diagnostic)
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `operationId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, diagnostic)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `secureHeaderMatches` @ `services/control-plane/internal/server/workspace_launch_admission.go:555`; `observeWorkspaceLaunchStage` @ `services/control-plane/internal/server/workspace_launch_stage_diagnostic.go:52`

### 44. GET /api/operator/workspace-runtime-image-policy

注册：`services/control-plane/internal/server/workspace_runtime_image_replacement.go:40`；`registerWorkspaceRuntimeImageReplacementRoutes`。

实际调用：`getWorkspaceImageReleasePolicy`, `protected`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	app.getWorkspaceImageReleasePolicy(w, r)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`protected` @ `services/control-plane/internal/server/server.go:277`; `getWorkspaceImageReleasePolicy` @ `services/control-plane/internal/server/workspace_image_release_policy.go:215`

### 45. GET /api/operator/workspaces

注册：`services/control-plane/internal/server/routes_admin.go:450`；`registerAdminRoutes`。

实际调用：`Context`, `operatorPagination`, `operatorWorkspacePage`, `protected`, `writeSourceEnvelope`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	page, pageSize, ok := operatorPagination(w, r)
	if !ok {
		return
	}
	data, status, err := app.operatorWorkspacePage(r.Context(), service, page, pageSize)
	if err != nil {
		writeSourceEnvelope(w, http.StatusInternalServerError, "control-plane+fabric+sub2api", "unavailable", nil)
		return
	}
	writeSourceEnvelope(w, http.StatusOK, "control-plane+fabric+sub2api", status, data)
})
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeSourceEnvelope(w, http.StatusInternalServerError, "control-plane+fabric+sub2api", "unavailable", nil)`
- `writeSourceEnvelope(w, http.StatusOK, "control-plane+fabric+sub2api", status, data)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`operatorPagination` @ `services/control-plane/internal/server/routes_admin.go:675`; `operatorWorkspacePage` @ `services/control-plane/internal/server/routes_admin.go:974`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeSourceEnvelope` @ `services/control-plane/internal/server/source_envelope.go:10`

### 46. GET /api/operator/workspaces/{workspaceId}

注册：`services/control-plane/internal/server/routes_admin.go:462`；`registerAdminRoutes`。

实际调用：`Context`, `PathValue`, `TrimSpace`, `operatorWorkspaceDetail`, `protected`, `writeError`, `writeSourceEnvelope`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	data, found, err := app.operatorWorkspaceDetail(r.Context(), service, strings.TrimSpace(r.PathValue("workspaceId")))
	if err != nil {
		writeSourceEnvelope(w, http.StatusInternalServerError, "control-plane+fabric+ledger", "unavailable", nil)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "workspace_not_found")
		return
	}
	writeSourceEnvelope(w, http.StatusOK, "control-plane+fabric+ledger", "available", data)
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `workspaceId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeSourceEnvelope(w, http.StatusInternalServerError, "control-plane+fabric+ledger", "unavailable", nil)`
- `writeSourceEnvelope(w, http.StatusOK, "control-plane+fabric+ledger", "available", data)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`operatorWorkspaceDetail` @ `services/control-plane/internal/server/routes_admin.go:995`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `writeSourceEnvelope` @ `services/control-plane/internal/server/source_envelope.go:10`

### 47. GET /api/operator/workspaces/{workspaceId}/runtime-image-replacements/preview

注册：`services/control-plane/internal/server/workspace_runtime_image_replacement.go:46`；`registerWorkspaceRuntimeImageReplacementRoutes`。

实际调用：`previewWorkspaceRuntimeImageReplacement`, `protected`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	app.previewWorkspaceRuntimeImageReplacement(w, r, service)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`protected` @ `services/control-plane/internal/server/server.go:277`; `previewWorkspaceRuntimeImageReplacement` @ `services/control-plane/internal/server/workspace_runtime_image_replacement.go:57`

### 48. GET /api/operator/workspaces/{workspaceId}/runtime-image-replacements/{operationId}

注册：`services/control-plane/internal/server/workspace_runtime_image_replacement.go:52`；`registerWorkspaceRuntimeImageReplacementRoutes`。

实际调用：`getWorkspaceRuntimeImageReplacement`, `protected`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	app.getWorkspaceRuntimeImageReplacement(w, r)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`protected` @ `services/control-plane/internal/server/server.go:277`; `getWorkspaceRuntimeImageReplacement` @ `services/control-plane/internal/server/workspace_runtime_image_replacement.go:213`

### 49. GET /api/pricing/catalog

注册：`services/control-plane/internal/server/routes_state.go:12`；`registerStateRoutes`。

实际调用：`Context`, `fabricComputePools`, `pricingCatalogResponse`, `protected`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	computePools, ok := fabricComputePools(w, r, service)
	if !ok {
		return
	}
	catalog, err := app.pricingCatalogResponse(r.Context(), computePools)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "pricing_catalog_unavailable")
		return
	}
	writeJSON(w, http.StatusOK, catalog)
})
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, catalog)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`pricingCatalogResponse` @ `services/control-plane/internal/server/pricing.go:64`; `pricingCatalogResponse` @ `services/control-plane/internal/server/pricing.go:66`; `protected` @ `services/control-plane/internal/server/server.go:277`; `fabricComputePools` @ `services/control-plane/internal/server/server.go:326`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`

### 50. GET /api/production/readiness

注册：`services/control-plane/internal/server/routes_core.go:24`；`registerCoreRoutes`。

实际调用：`Context`, `RuntimeReadiness`, `writeJSON`, `writeUpstreamError`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	readiness, err := service.RuntimeReadiness(r.Context())
	if err != nil {
		writeUpstreamError(w)
		return
	}
	cloudImagesReady := readiness.CloudImagesReady
	workspaceImagesReady := readiness.WorkspaceImagesReady
	immutableImagesReady := readiness.ImmutableImagesReady
	writeJSON(w, http.StatusOK, map[string]any{
		"provider":	readiness.Provider, "ready": readiness.Ready && cloudImagesReady && workspaceImagesReady && immutableImagesReady,
		"cloudImagesReady":	cloudImagesReady, "workspaceImagesReady": workspaceImagesReady, "immutableImagesReady": immutableImagesReady, "checks": []any{},
	})
}
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, map[string]any{<br>	"provider":	readiness.Provider, "ready": readiness.Ready && cloudImagesReady && workspaceImagesReady && immutableImagesReady,<br>	"cloudImagesReady":	cloudImagesReady, "workspaceImagesReady": workspaceImagesReady, "immutableImagesReady": immutableImagesReady, "checks": []any{},<br>})`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`RuntimeReadiness` @ `services/control-plane/internal/controlplane/service.go:391`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeUpstreamError` @ `services/control-plane/internal/server/server.go:345`

### 51. GET /api/runtime/readiness

注册：`services/control-plane/internal/server/routes_core.go:16`；`registerCoreRoutes`。

实际调用：`Context`, `RuntimeReadiness`, `writeJSON`, `writeUpstreamError`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	readiness, err := service.RuntimeReadiness(r.Context())
	if err != nil {
		writeUpstreamError(w)
		return
	}
	writeJSON(w, http.StatusOK, readiness)
}
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, readiness)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`RuntimeReadiness` @ `services/control-plane/internal/controlplane/service.go:391`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeUpstreamError` @ `services/control-plane/internal/server/server.go:345`

### 52. GET /api/workspace-launches

注册：`services/control-plane/internal/server/routes_workspace_launch.go:309`；`registerWorkspaceLaunchRoutes`。

实际调用：`Context`, `append`, `len`, `make`, `protected`, `queryRuntimeOperations`, `scopedAccountID`, `workspaceLaunchResponse`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	accountID, ok := app.scopedAccountID(w, r, nil)
	if !ok {
		return
	}
	operations, err := queryRuntimeOperations(r.Context(), app.tables, runtimeOperationQuery{AccountID: accountID, Action: workspaceLaunchAction})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	rows := make([]any, 0, len(operations))
	for _, operation := range operations {
		body, err := workspaceLaunchResponse(operation)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		rows = append(rows, body)
	}
	writeJSON(w, http.StatusOK, rows)
})
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, rows)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `scopedAccountID` @ `services/control-plane/internal/server/server.go:428`; `queryRuntimeOperations` @ `services/control-plane/internal/server/table_store.go:87`; `workspaceLaunchResponse` @ `services/control-plane/internal/server/workspace_launch.go:135`

### 53. GET /api/workspace-launches/{id}

注册：`services/control-plane/internal/server/routes_workspace_launch.go:331`；`registerWorkspaceLaunchRoutes`。

实际调用：`Context`, `GetRuntimeOperation`, `PathValue`, `protected`, `scopedAccountID`, `stringValue`, `workspaceLaunchResponse`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	accountID, ok := app.scopedAccountID(w, r, nil)
	if !ok {
		return
	}
	operation, found, err := app.tables.GetRuntimeOperation(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if found && stringValue(operation["accountId"]) == accountID && stringValue(operation["action"]) == workspaceLaunchAction {
		body, err := workspaceLaunchResponse(operation)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		writeJSON(w, http.StatusOK, body)
		return
	}
	writeError(w, http.StatusNotFound, "workspace_launch_not_found")
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `id` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, body)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `GetRuntimeOperation` @ `services/control-plane/internal/server/ent_state_store.go:694`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `scopedAccountID` @ `services/control-plane/internal/server/server.go:428`; `workspaceLaunchResponse` @ `services/control-plane/internal/server/workspace_launch.go:135`

### 54. GET /api/workspaces

注册：`services/control-plane/internal/server/routes_workspace.go:17`；`registerWorkspaceRoutes`。

实际调用：`Context`, `PageWorkspaces`, `append`, `len`, `make`, `operatorPagination`, `projectWorkspaceApplicationInstallation`, `projectWorkspaceCurrentApplication`, `protected`, `readWorkspaceCurrentApplication`, `sessionUserContext`, `stringValue`, `workspaceSourceProjection`, `writeError`, `writeSourceEnvelope`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	page, pageSize, ok := operatorPagination(w, r)
	if !ok {
		return
	}
	user, ok := app.sessionUserContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "not_authenticated")
		return
	}
	workspacePage, err := app.tables.PageWorkspaces(r.Context(), stringValue(user["accountId"]), tablePageQuery{Offset: (page - 1) * pageSize, Limit: pageSize})
	if err != nil {
		writeSourceEnvelope(w, http.StatusInternalServerError, "control-plane", "unavailable", nil)
		return
	}
	items := make([]any, 0, len(workspacePage.Items))
	for _, row := range workspacePage.Items {
		item, ok := workspaceSourceProjection(row)
		if !ok {
			writeSourceEnvelope(w, http.StatusInternalServerError, "control-plane", "unavailable", nil)
			return
		}
		if stringValue(row["currentApplicationDeploymentId"]) != "" {
			current, _, readErr := app.readWorkspaceCurrentApplication(r.Context(), service, row)
			if current == nil || readErr != nil && current.OperationID == "" {
				writeSourceEnvelope(w, http.StatusInternalServerError, "control-plane", "unavailable", nil)
				return
			}
			projectWorkspaceCurrentApplication(row, item, current)
		}
		if err := app.projectWorkspaceApplicationInstallation(r.Context(), row, item); err != nil {
			writeSourceEnvelope(w, http.StatusInternalServerError, "control-plane", "unavailable", nil)
			return
		}
		items = append(items, item)
	}
	status := "available"
	if len(items) == 0 {
		status = "empty"
	}
	writeSourceEnvelope(w, http.StatusOK, "control-plane", status, map[string]any{"items": items, "total": workspacePage.Total, "page": page, "pageSize": pageSize})
})
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeSourceEnvelope(w, http.StatusInternalServerError, "control-plane", "unavailable", nil)`
- `writeSourceEnvelope(w, http.StatusOK, "control-plane", status, map[string]any{"items": items, "total": workspacePage.Total, "page": page, "pageSize": pageSize})`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `sessionUserContext` @ `services/control-plane/internal/server/auth_accounts.go:435`; `PageWorkspaces` @ `services/control-plane/internal/server/ent_state_store_workspace.go:467`; `operatorPagination` @ `services/control-plane/internal/server/routes_admin.go:675`; `workspaceSourceProjection` @ `services/control-plane/internal/server/routes_workspace.go:448`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `writeSourceEnvelope` @ `services/control-plane/internal/server/source_envelope.go:10`; `projectWorkspaceCurrentApplication` @ `services/control-plane/internal/server/workspace_application_access.go:142`; `projectWorkspaceApplicationInstallation` @ `services/control-plane/internal/server/workspace_application_access.go:37`; `readWorkspaceCurrentApplication` @ `services/control-plane/internal/server/workspace_application_access.go:93`

### 55. GET /api/workspaces/{workspaceId}/deletion

注册：`services/control-plane/internal/server/routes_workspace.go:59`；`registerWorkspaceRoutes`。

实际调用：`protected`, `workspaceDeletionStatus`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) { app.workspaceDeletionStatus(w, r) })
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`protected` @ `services/control-plane/internal/server/server.go:277`; `workspaceDeletionStatus` @ `services/control-plane/internal/server/workspace_delete_worker.go:176`

### 56. GET /api/workspaces/{workspaceId}/gateway-budget

注册：`services/control-plane/internal/server/routes_workspace.go:243`；`registerWorkspaceRoutes`。

实际调用：`protected`, `workspaceGatewayBudget`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	app.workspaceGatewayBudget(w, r, service)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`protected` @ `services/control-plane/internal/server/server.go:277`; `workspaceGatewayBudget` @ `services/control-plane/internal/server/workspace_gateway_budget.go:41`

### 57. GET /api/workspaces/{workspaceId}/renewal

注册：`services/control-plane/internal/server/routes_workspace.go:249`；`registerWorkspaceRoutes`。

实际调用：`Context`, `Header`, `Now`, `PathValue`, `Set`, `UTC`, `canAccessResource`, `getWorkspace`, `protected`, `queryRuntimeOperations`, `stringValue`, `workspaceAutoRenewResponse`, `workspaceRenewalRecoveryState`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	workspace, ok := app.getWorkspace(r.PathValue("workspaceId"))
	if !ok {
		writeError(w, http.StatusNotFound, "workspace_not_found")
		return
	}
	if !app.canAccessResource(r, workspace) {
		writeError(w, http.StatusForbidden, "account_scope_forbidden")
		return
	}
	operations, err := queryRuntimeOperations(r.Context(), app.tables, runtimeOperationQuery{WorkspaceID: stringValue(workspace["id"])})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	now := time.Now().UTC()
	response, err := workspaceAutoRenewResponse(workspace, operations, workspace["autoRenew"] == true, now)
	if err != nil {
		writeError(w, http.StatusConflict, "workspace_billing_state_invalid")
		return
	}
	response["renewalStatus"] = stringValue(workspace["renewalStatus"])
	response["recovery"] = app.workspaceRenewalRecoveryState(r.Context(), service, workspace, operations, now)
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, response)
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `workspaceId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, response)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `canAccessResource` @ `services/control-plane/internal/server/resource_facts.go:138`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `queryRuntimeOperations` @ `services/control-plane/internal/server/table_store.go:87`; `getWorkspace` @ `services/control-plane/internal/server/workspace_gateway.go:308`; `workspaceRenewalRecoveryState` @ `services/control-plane/internal/server/workspace_renewal.go:1484`; `workspaceAutoRenewResponse` @ `services/control-plane/internal/server/workspace_renewal.go:156`

### 58. GET /api/workspaces/{workspaceId}/runtime-status

注册：`services/control-plane/internal/server/routes_workspace.go:63`；`registerWorkspaceRoutes`。

实际调用：`Context`, `Header`, `PathValue`, `Set`, `TrimSpace`, `WorkspaceRuntimeStatus`, `canAccessResource`, `firstNonEmpty`, `lockEntitlementResources`, `protected`, `readWorkspaceCurrentApplication`, `sessionUserContext`, `stringValue`, `unlock`, `workspaceAccessAllowed`, `workspaceCurrentApplicationRuntimeResponse`, `workspaceForSource`, `workspaceLaunchProvisioningMode`, `workspaceRuntimeStatusResponse`, `writeError`, `writeSourceEnvelope`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	workspaceID := strings.TrimSpace(r.PathValue("workspaceId"))
	user, ok := app.sessionUserContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "not_authenticated")
		return
	}
	accountID := stringValue(user["accountId"])
	workspace, ok, err := app.workspaceForSource(r.Context(), accountID, workspaceID)
	if err != nil {
		writeSourceEnvelope(w, http.StatusInternalServerError, "fabric", "unavailable", nil)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "workspace_not_found")
		return
	}
	if !app.canAccessResource(r, workspace) {
		writeError(w, http.StatusForbidden, "account_scope_forbidden")
		return
	}
	if !app.workspaceAccessAllowed(w, r, workspace) {
		return
	}
	unlock := app.lockEntitlementResources(
		firstNonEmpty(stringValue(workspace["currentComputeAllocationId"]), stringValue(workspace["computeAllocationId"])),
		stringValue(workspace["storageId"]),
		firstNonEmpty(stringValue(workspace["currentAttachmentId"]), stringValue(workspace["attachmentId"])),
	)
	defer unlock()
	workspace, ok, err = app.workspaceForSource(r.Context(), accountID, workspaceID)
	if err != nil {
		writeSourceEnvelope(w, http.StatusInternalServerError, "fabric", "unavailable", nil)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "workspace_not_found")
		return
	}
	if !app.canAccessResource(r, workspace) {
		writeError(w, http.StatusForbidden, "account_scope_forbidden")
		return
	}
	if !app.workspaceAccessAllowed(w, r, workspace) {
		return
	}
	switch stringValue(workspace["state"]) {
	case "suspended", "stopped":
		writeError(w, http.StatusConflict, "workspace_suspended")
		return
	case "data_deleted", "unrecoverable", "storage_missing", "destroyed":
		writeError(w, http.StatusGone, "workspace_storage_destroyed")
		return
	}
	if stringValue(workspace["currentApplicationDeploymentId"]) != "" {
		current, observation, err := app.readWorkspaceCurrentApplication(r.Context(), service, workspace)
		if err != nil || current == nil {
			writeSourceEnvelope(w, http.StatusBadGateway, "fabric", "unavailable", nil)
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		writeSourceEnvelope(w, http.StatusOK, "fabric", "available", workspaceCurrentApplicationRuntimeResponse(current, observation))
		return
	}
	if stringValue(workspace["applicationBinding"]) == "empty" {
		if mode, err := app.workspaceLaunchProvisioningMode(r.Context(), workspaceID); err != nil || mode != contracts.WorkspaceProvisioningResourceOnly {
			writeSourceEnvelope(w, http.StatusBadGateway, "control-plane", "unavailable", nil)
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		writeSourceEnvelope(w, http.StatusOK, "control-plane", "available", map[string]any{"workspaceId": workspaceID, "status": "not_found", "ready": false, "currentApplication": nil, "checks": []any{}})
		return
	}
	runtime, err := service.WorkspaceRuntimeStatus(r.Context(), workspaceID)
	if err != nil {
		writeSourceEnvelope(w, http.StatusBadGateway, "fabric", "unavailable", nil)
		return
	}
	body, ok := workspaceRuntimeStatusResponse(runtime, workspaceID)
	if !ok {
		writeSourceEnvelope(w, http.StatusBadGateway, "fabric", "unavailable", nil)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeSourceEnvelope(w, http.StatusOK, "fabric", "available", body)
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `workspaceId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeSourceEnvelope(w, http.StatusBadGateway, "control-plane", "unavailable", nil)`
- `writeSourceEnvelope(w, http.StatusBadGateway, "fabric", "unavailable", nil)`
- `writeSourceEnvelope(w, http.StatusInternalServerError, "fabric", "unavailable", nil)`
- `writeSourceEnvelope(w, http.StatusOK, "control-plane", "available", map[string]any{"workspaceId": workspaceID, "status": "not_found", "ready": false, "currentApplication": nil, "checks": []any{}})`
- `writeSourceEnvelope(w, http.StatusOK, "fabric", "available", body)`
- `writeSourceEnvelope(w, http.StatusOK, "fabric", "available", workspaceCurrentApplicationRuntimeResponse(current, observation))`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`WorkspaceRuntimeStatus` @ `services/control-plane/internal/clients/fabric.go:787`; `WorkspaceRuntimeStatus` @ `services/control-plane/internal/controlplane/service.go:379`; `stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `firstNonEmpty` @ `services/control-plane/internal/server/app_state.go:552`; `lockEntitlementResources` @ `services/control-plane/internal/server/app_state.go:59`; `sessionUserContext` @ `services/control-plane/internal/server/auth_accounts.go:435`; `canAccessResource` @ `services/control-plane/internal/server/resource_facts.go:138`; `workspaceForSource` @ `services/control-plane/internal/server/routes_workspace.go:437`; `workspaceAccessAllowed` @ `services/control-plane/internal/server/routes_workspace.go:549`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `workspaceRuntimeStatusResponse` @ `services/control-plane/internal/server/server.go:635`; `writeSourceEnvelope` @ `services/control-plane/internal/server/source_envelope.go:10`; `workspaceCurrentApplicationRuntimeResponse` @ `services/control-plane/internal/server/workspace_application_access.go:159`; `readWorkspaceCurrentApplication` @ `services/control-plane/internal/server/workspace_application_access.go:93`; `workspaceLaunchProvisioningMode` @ `services/control-plane/internal/server/workspace_launch_provisioning_mode.go:81`

### 59. PATCH /api/gateway/keys/{keyId}

注册：`services/control-plane/internal/server/routes_gateway.go:100`；`registerGatewayRoutes`。

实际调用：`protected`, `updateGatewayKey`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	app.updateGatewayKey(w, r, service)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`updateGatewayKey` @ `services/control-plane/internal/server/routes_gateway.go:304`; `protected` @ `services/control-plane/internal/server/server.go:277`

### 60. PATCH /api/workspaces/{workspaceId}/gateway-budget

注册：`services/control-plane/internal/server/routes_workspace.go:246`；`registerWorkspaceRoutes`。

实际调用：`protected`, `updateWorkspaceGatewayBudget`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	app.updateWorkspaceGatewayBudget(w, r, service)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`protected` @ `services/control-plane/internal/server/server.go:277`; `updateWorkspaceGatewayBudget` @ `services/control-plane/internal/server/workspace_gateway_budget.go:59`

### 61. POST /api/announcements/{announcementId}/read

注册：`services/control-plane/internal/server/routes_announcements.go:56`；`registerAnnouncementRoutes`。

实际调用：`markAnnouncementRead`, `protected`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	app.markAnnouncementRead(w, r)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`markAnnouncementRead` @ `services/control-plane/internal/server/routes_announcements.go:261`; `protected` @ `services/control-plane/internal/server/server.go:277`

### 62. POST /api/auth/login

注册：`services/control-plane/internal/server/routes_auth.go:17`；`registerAuthRoutes`。

实际调用：`Context`, `Header`, `Is`, `Set`, `SetCookie`, `clearLoginFailures`, `decodeJSON`, `limitJSONBody`, `login`, `loginRateLimited`, `recordLoginFailure`, `sessionCookie`, `stringValue`, `validLoginRequest`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
func(w http.ResponseWriter, r *http.Request) {
	if !validLoginRequest(w, r) {
		return
	}
	if !limitJSONBody(w, r) {
		return
	}
	input := decodeJSON(r)
	if app.loginRateLimited(r, input) {
		writeError(w, http.StatusTooManyRequests, "login_rate_limited")
		return
	}
	payload, sessionID, err := app.login(r.Context(), service, input)
	if err != nil {
		switch {
		case errors.Is(err, clients.ErrSub2APIInvalidCredentials), errors.Is(err, errInvalidLocalCredentials):
			app.recordLoginFailure(r, input)
			writeError(w, http.StatusUnauthorized, "invalid_credentials")
		case errors.Is(err, clients.ErrSub2APIAuthRateLimited):
			writeError(w, http.StatusTooManyRequests, "login_rate_limited")
		default:
			writeError(w, http.StatusServiceUnavailable, "authentication_unavailable")
		}
		return
	}
	app.clearLoginFailures(r, input)
	http.SetCookie(w, sessionCookie(sessionID, 12*60*60))
	w.Header().Set("x-opl-csrf-token", stringValue(payload["csrfToken"]))
	writeJSON(w, http.StatusOK, payload)
}
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, payload)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `sessionCookie` @ `services/control-plane/internal/server/auth.go:34`; `login` @ `services/control-plane/internal/server/auth_accounts.go:162`; `loginRateLimited` @ `services/control-plane/internal/server/auth_accounts.go:209`; `recordLoginFailure` @ `services/control-plane/internal/server/auth_accounts.go:221`; `clearLoginFailures` @ `services/control-plane/internal/server/auth_accounts.go:260`; `validLoginRequest` @ `services/control-plane/internal/server/routes_auth.go:103`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `limitJSONBody` @ `services/control-plane/internal/server/server.go:361`; `decodeJSON` @ `services/control-plane/internal/server/server.go:446`

### 63. POST /api/auth/logout

注册：`services/control-plane/internal/server/routes_auth.go:93`；`registerAuthRoutes`。

实际调用：`SetCookie`, `logout`, `protected`, `sessionCookie`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	if err := app.logout(r); err != nil {
		writeError(w, http.StatusInternalServerError, "state_persist_failed")
		return
	}
	http.SetCookie(w, sessionCookie("", -1))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
})
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, map[string]bool{"ok": true})`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`sessionCookie` @ `services/control-plane/internal/server/auth.go:34`; `logout` @ `services/control-plane/internal/server/auth_accounts.go:448`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`

### 64. POST /api/billing/reconciliation

注册：`services/control-plane/internal/server/routes_billing.go:137`；`registerBillingRoutes`。

实际调用：`ApplyBillingReconciliation`, `BillingReconciliation`, `Context`, `Error`, `Get`, `Is`, `RecordReconciliation`, `TrimSpace`, `auditEvent`, `billingReconciliationAuditID`, `billingReconciliationReport`, `confirmed`, `decodeJSON`, `protected`, `reconciliationResponse`, `stringValue`, `writeError`, `writeJSON`, `writeUpstreamError`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	input := decodeJSON(r)
	if !confirmed(input, "confirm") {
		writeError(w, http.StatusBadRequest, "confirmation_required")
		return
	}
	if _, supplied := input["report"]; supplied {
		writeError(w, http.StatusBadRequest, "reconciliation_report_server_computed")
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "missing Idempotency-Key")
		return
	}
	current, found, err := app.tables.BillingReconciliation(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	expectedCurrentGuardID := ""
	if found {
		expectedCurrentGuardID = stringValue(current["id"])
	}
	report, err := app.billingReconciliationReport(r.Context(), service, idempotencyKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	result, err := service.RecordReconciliation(r.Context(), controlplane.ReconciliationInput{Report: report}, idempotencyKey)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	row := reconciliationResponse(result)
	audit := app.auditEvent(r, "billing.reconciliation", "billing_reconciliation", result.ID, "", current, row, "succeeded")
	audit["id"] = billingReconciliationAuditID(result.ID)
	if err := app.tables.ApplyBillingReconciliation(r.Context(), billingReconciliationMutation{
		Row:	row, AuditEvent: audit, ExpectedCurrentGuardID: expectedCurrentGuardID,
	}); err != nil {
		if errors.Is(err, errBillingReconciliationCASConflict) || errors.Is(err, errIdempotencyConflict) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "state_persist_failed")
		return
	}
	writeJSON(w, http.StatusCreated, row)
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `Idempotency-Key` | `string` | header |
| `report` | `map value; validator in handler` | body input access |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusCreated, row)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/control-plane/internal/clients/fabric.go:96`; `RecordReconciliation` @ `services/control-plane/internal/clients/ledger.go:251`; `Error` @ `services/control-plane/internal/clients/sub2api.go:432`; `Error` @ `services/control-plane/internal/clients/sub2api.go:446`; `Error` @ `services/control-plane/internal/clients/workspace_registry.go:84`; `RecordReconciliation` @ `services/control-plane/internal/controlplane/service.go:375`; `auditEvent` @ `services/control-plane/internal/server/admin_ops.go:105`; `stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `billingReconciliationReport` @ `services/control-plane/internal/server/billing_projection.go:35`; `billingReconciliationAuditID` @ `services/control-plane/internal/server/billing_projection.go:465`; `BillingReconciliation` @ `services/control-plane/internal/server/ent_state_store.go:816`; `ApplyBillingReconciliation` @ `services/control-plane/internal/server/ent_state_store_workspace.go:803`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `writeUpstreamError` @ `services/control-plane/internal/server/server.go:345`; `decodeJSON` @ `services/control-plane/internal/server/server.go:446`; `confirmed` @ `services/control-plane/internal/server/server.go:481`; `reconciliationResponse` @ `services/control-plane/internal/server/server.go:622`; `Get` @ `services/control-plane/internal/server/session_credential_vault.go:38`; `Error` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:75`

### 65. POST /api/gateway/keys

注册：`services/control-plane/internal/server/routes_gateway.go:97`；`registerGatewayRoutes`。

实际调用：`createGatewayKey`, `protected`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	app.createGatewayKey(w, r, service)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`createGatewayKey` @ `services/control-plane/internal/server/routes_gateway.go:217`; `protected` @ `services/control-plane/internal/server/server.go:277`

### 66. POST /api/gateway/keys/{keyId}/reveal

注册：`services/control-plane/internal/server/routes_gateway.go:106`；`registerGatewayRoutes`。

实际调用：`protected`, `revealGatewayKey`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	app.revealGatewayKey(w, r, service)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`revealGatewayKey` @ `services/control-plane/internal/server/routes_gateway.go:552`; `protected` @ `services/control-plane/internal/server/server.go:277`

### 67. POST /api/operator/accounts

注册：`services/control-plane/internal/server/routes_admin.go:489`；`registerAdminRoutes`。

实际调用：`Context`, `appendAuditEvent`, `canonicalEmail`, `createUser`, `decodeJSON`, `firstNonEmpty`, `operatorProvisionShapeValid`, `protected`, `requiredMutationKey`, `stableID`, `stringValue`, `writeCreateUserError`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	key, ok := requiredMutationKey(w, r)
	if !ok {
		return
	}
	input := decodeJSON(r)
	if !operatorProvisionShapeValid(input) {
		writeError(w, http.StatusBadRequest, "invalid_provision")
		return
	}
	email, err := canonicalEmail(stringValue(input["email"]))
	if err != nil {
		writeCreateUserError(w, err)
		return
	}
	accountID := "acct-" + stableID("account", email)[:18]
	admission := firstNonEmpty(stringValue(input["admission"]), "full_cloud_customer")
	workspaceEligibility := admission == "full_cloud_customer"
	user, err := app.createUser(r.Context(), service, map[string]any{
		"email":	email, "password": input["password"], "accountId": accountID, "role": "owner",
		"workspacePurchaseEnabled":	workspaceEligibility,
	})
	if err != nil {
		writeCreateUserError(w, err)
		return
	}
	result := map[string]any{"operationId": "account-provision-" + stableID(key, email)[:18], "accountId": accountID, "status": "succeeded", "workspacePurchaseEnabled": workspaceEligibility}
	if err := app.appendAuditEvent(r, "account.provision", "account", accountID, accountID, nil, map[string]any{"userId": user["id"], "email": email, "workspacePurchaseEnabled": workspaceEligibility, "admission": admission}, "succeeded"); err != nil {
		writeError(w, http.StatusInternalServerError, "state_persist_failed")
		return
	}
	writeJSON(w, http.StatusCreated, result)
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `admission` | `map value; validator in handler` | body input access |
| `email` | `map value; validator in handler` | body input access |
| `password` | `map value; validator in handler` | body input access |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusCreated, result)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`appendAuditEvent` @ `services/control-plane/internal/server/admin_ops.go:93`; `stableID` @ `services/control-plane/internal/server/app_state.go:496`; `stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `firstNonEmpty` @ `services/control-plane/internal/server/app_state.go:552`; `createUser` @ `services/control-plane/internal/server/auth_accounts.go:26`; `operatorProvisionShapeValid` @ `services/control-plane/internal/server/routes_admin.go:693`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `writeCreateUserError` @ `services/control-plane/internal/server/server.go:389`; `decodeJSON` @ `services/control-plane/internal/server/server.go:446`; `requiredMutationKey` @ `services/control-plane/internal/server/server.go:486`; `canonicalEmail` @ `services/control-plane/internal/server/table_store.go:341`

### 68. POST /api/operator/accounts/{accountId}/disable

注册：`services/control-plane/internal/server/routes_admin.go:569`；`registerAdminRoutes`。

实际调用：`Context`, `ListAccounts`, `PathValue`, `TrimSpace`, `appendAuditEvent`, `decodeJSON`, `disableUser`, `findRecord`, `operatorDisableShapeValid`, `protected`, `requiredMutationKey`, `sessionUserID`, `stableID`, `stringValue`, `validAccountID`, `withOperatorUserID`, `writeError`, `writeJSON`, `writeUserLifecycleError`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	key, ok := requiredMutationKey(w, r)
	if !ok {
		return
	}
	accountID := strings.TrimSpace(r.PathValue("accountId"))
	input := decodeJSON(r)
	if !validAccountID(accountID) || !operatorDisableShapeValid(input, accountID) {
		writeError(w, http.StatusBadRequest, "invalid_account_disable")
		return
	}
	accounts, err := app.tables.ListAccounts(r.Context(), accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_persist_failed")
		return
	}
	account := findRecord(accounts, accountID)
	if account == nil {
		writeError(w, http.StatusNotFound, "account_not_found")
		return
	}
	withOperatorUserID(input, app.sessionUserID(r))
	input["userId"] = stringValue(account["ownerUserId"])
	user, err := app.disableUser(input)
	if err != nil {
		writeUserLifecycleError(w, err)
		return
	}
	result := map[string]any{"operationId": "account-disable-" + stableID(key, accountID)[:18], "accountId": accountID, "status": "succeeded"}
	if err := app.appendAuditEvent(r, "account.disable", "account", accountID, accountID, nil, map[string]any{"userId": user["id"], "reason": input["reason"]}, "succeeded"); err != nil {
		writeError(w, http.StatusInternalServerError, "state_persist_failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `accountId` | `string` | path |
| `reason` | `map value; validator in handler` | body input access |
| `userId` | `map value; validator in handler` | body input access |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, result)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`findRecord` @ `services/control-plane/internal/server/admin_ops.go:25`; `appendAuditEvent` @ `services/control-plane/internal/server/admin_ops.go:93`; `stableID` @ `services/control-plane/internal/server/app_state.go:496`; `stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `disableUser` @ `services/control-plane/internal/server/auth_accounts.go:108`; `sessionUserID` @ `services/control-plane/internal/server/auth_accounts.go:427`; `ListAccounts` @ `services/control-plane/internal/server/ent_state_store_identity.go:140`; `operatorDisableShapeValid` @ `services/control-plane/internal/server/routes_admin.go:721`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `writeUserLifecycleError` @ `services/control-plane/internal/server/server.go:378`; `withOperatorUserID` @ `services/control-plane/internal/server/server.go:410`; `decodeJSON` @ `services/control-plane/internal/server/server.go:446`; `requiredMutationKey` @ `services/control-plane/internal/server/server.go:486`; `validAccountID` @ `services/control-plane/internal/server/table_store.go:350`

### 69. POST /api/operator/accounts/{accountId}/wallet-adjustments

注册：`services/control-plane/internal/server/routes_admin.go:412`；`registerAdminRoutes`。

实际调用：`createWalletAdjustment`, `protected`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	app.createWalletAdjustment(w, r, service)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`protected` @ `services/control-plane/internal/server/server.go:277`; `createWalletAdjustment` @ `services/control-plane/internal/server/wallet_adjustment.go:102`

### 70. POST /api/operator/accounts/{accountId}/workspace-purchase-eligibility

注册：`services/control-plane/internal/server/routes_admin.go:522`；`registerAdminRoutes`。

实际调用：`ApplyWorkspacePurchaseEligibility`, `Context`, `Error`, `GetAccount`, `Is`, `PathValue`, `TrimSpace`, `auditEvent`, `decodeJSON`, `lockResource`, `protected`, `requiredMutationKey`, `stableID`, `unlock`, `validAccountID`, `workspacePurchaseEligibilityShapeValid`, `workspacePurchaseEnabled`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	key, ok := requiredMutationKey(w, r)
	if !ok {
		return
	}
	accountID := strings.TrimSpace(r.PathValue("accountId"))
	input := decodeJSON(r)
	if !validAccountID(accountID) || !workspacePurchaseEligibilityShapeValid(input, accountID) {
		writeError(w, http.StatusBadRequest, "invalid_workspace_purchase_eligibility")
		return
	}
	unlock := app.lockResource("account", accountID)
	defer unlock()
	account, found, err := app.tables.GetAccount(r.Context(), accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "account_not_found")
		return
	}
	enabled := input["enabled"].(bool)
	before := workspacePurchaseEnabled(account)
	action := "account.workspace_purchase.revoke"
	if enabled {
		action = "account.workspace_purchase.grant"
	}
	after := map[string]any{"workspacePurchaseEnabled": enabled, "reason": input["reason"]}
	audit := app.auditEvent(r, action, "account", accountID, accountID, map[string]any{"workspacePurchaseEnabled": before}, after, "succeeded")
	audit["id"] = "audit-" + stableID("account.workspace_purchase", accountID, key)[:12]
	if _, err := app.tables.ApplyWorkspacePurchaseEligibility(r.Context(), workspacePurchaseEligibilityMutation{
		AccountID:	accountID, Enabled: enabled, AuditEvent: audit,
	}); err != nil {
		if errors.Is(err, errIdempotencyConflict) {
			writeError(w, http.StatusConflict, errIdempotencyConflict.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "state_persist_failed")
		return
	}
	result := map[string]any{
		"operationId":	"workspace-purchase-eligibility-" + stableID(key, accountID)[:18],
		"accountId":	accountID, "status": "succeeded", "workspacePurchaseEnabled": enabled,
	}
	writeJSON(w, http.StatusOK, result)
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `accountId` | `string` | path |
| `enabled` | `map value; validator in handler` | body input access |
| `reason` | `map value; validator in handler` | body input access |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, result)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/control-plane/internal/clients/fabric.go:96`; `Error` @ `services/control-plane/internal/clients/sub2api.go:432`; `Error` @ `services/control-plane/internal/clients/sub2api.go:446`; `Error` @ `services/control-plane/internal/clients/workspace_registry.go:84`; `auditEvent` @ `services/control-plane/internal/server/admin_ops.go:105`; `lockResource` @ `services/control-plane/internal/server/app_state.go:42`; `stableID` @ `services/control-plane/internal/server/app_state.go:496`; `GetAccount` @ `services/control-plane/internal/server/ent_state_store_identity.go:152`; `ApplyWorkspacePurchaseEligibility` @ `services/control-plane/internal/server/ent_state_store_identity.go:230`; `workspacePurchaseEligibilityShapeValid` @ `services/control-plane/internal/server/routes_admin.go:734`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `decodeJSON` @ `services/control-plane/internal/server/server.go:446`; `requiredMutationKey` @ `services/control-plane/internal/server/server.go:486`; `validAccountID` @ `services/control-plane/internal/server/table_store.go:350`; `workspacePurchaseEnabled` @ `services/control-plane/internal/server/workspace_launch_admission.go:198`; `Error` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:75`; `ApplyWorkspacePurchaseEligibility` @ `services/control-plane/migrations/migrations.go:143`

### 71. POST /api/operator/announcements

注册：`services/control-plane/internal/server/routes_announcements.go:62`；`registerAnnouncementRoutes`。

实际调用：`createAnnouncement`, `protected`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	app.createAnnouncement(w, r)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`createAnnouncement` @ `services/control-plane/internal/server/routes_announcements.go:159`; `protected` @ `services/control-plane/internal/server/server.go:277`

### 72. POST /api/operator/announcements/{announcementId}/publish

注册：`services/control-plane/internal/server/routes_announcements.go:68`；`registerAnnouncementRoutes`。

实际调用：`protected`, `publishAnnouncement`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	app.publishAnnouncement(w, r)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`publishAnnouncement` @ `services/control-plane/internal/server/routes_announcements.go:210`; `protected` @ `services/control-plane/internal/server/server.go:277`

### 73. POST /api/operator/announcements/{announcementId}/withdraw

注册：`services/control-plane/internal/server/routes_announcements.go:71`；`registerAnnouncementRoutes`。

实际调用：`protected`, `withdrawAnnouncement`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	app.withdrawAnnouncement(w, r)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`withdrawAnnouncement` @ `services/control-plane/internal/server/routes_announcements.go:243`; `protected` @ `services/control-plane/internal/server/server.go:277`

### 74. POST /api/operator/application-data-materials

注册：`services/control-plane/internal/server/workspace_application_data_admission.go:53`；`registerApplicationDataMaterialRoutes`。

实际调用：`Context`, `Error`, `Is`, `Marshal`, `Unmarshal`, `ValidateWorkspaceApplicationDataMaterial`, `admitApplicationDataMaterial`, `decodeJSON`, `protected`, `requiredMutationKey`, `sessionUserContext`, `string`, `stringValue`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	input := decodeJSON(r)
	if _, ok := requiredMutationKey(w, r); !ok {
		return
	}
	user, ok := app.sessionUserContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "not_authenticated")
		return
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_application_data_material")
		return
	}
	var material contracts.WorkspaceApplicationDataMaterial
	if json.Unmarshal(encoded, &material) != nil {
		writeError(w, http.StatusBadRequest, "invalid_application_data_material")
		return
	}
	if err := contracts.ValidateWorkspaceApplicationDataMaterial(material); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_application_data_material")
		return
	}
	row, decision, err := app.admitApplicationDataMaterial(r.Context(), material, stringValue(user["id"]))
	if err != nil {
		if errors.Is(err, application.ErrDataMaterialConflict) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "state_persist_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"decision": string(decision), "dataMaterial": row})
})
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, map[string]any{"decision": string(decision), "dataMaterial": row})`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/control-plane/internal/clients/fabric.go:96`; `Error` @ `services/control-plane/internal/clients/sub2api.go:432`; `Error` @ `services/control-plane/internal/clients/sub2api.go:446`; `Error` @ `services/control-plane/internal/clients/workspace_registry.go:84`; `stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `sessionUserContext` @ `services/control-plane/internal/server/auth_accounts.go:435`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `decodeJSON` @ `services/control-plane/internal/server/server.go:446`; `requiredMutationKey` @ `services/control-plane/internal/server/server.go:486`; `admitApplicationDataMaterial` @ `services/control-plane/internal/server/workspace_application_data_admission.go:21`; `Error` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:75`

### 75. POST /api/operator/application-deployments

注册：`services/control-plane/internal/server/workspace_application_deployment.go:349`；`registerApplicationDeploymentRoutes`。

实际调用：`AdmittedApplicationRevision`, `Background`, `Context`, `Decode`, `DisallowUnknownFields`, `Error`, `GetRuntimeOperation`, `Is`, `Marshal`, `NewDecoder`, `NewReader`, `Sprintf`, `Sum256`, `ValidateWorkspaceApplicationRevision`, `WorkspaceApplicationConfigurationDigest`, `WorkspaceApplicationRequiresPlatformCredentials`, `admitWorkspaceApplicationRevision`, `createWorkspaceApplicationDeploymentIntent`, `decodeApplicationRevisionPayload`, `decodeJSON`, `decodeWorkspaceApplicationDeploymentIntent`, `len`, `protected`, `requiredMutationKey`, `runWorkspaceApplicationDeployment`, `sessionUserContext`, `stringValue`, `workspaceApplicationDeploymentOperationID`, `workspaceApplicationDeploymentWorkerEnabled`, `workspaceOPLApplicationConfiguration`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	input := decodeJSON(r)
	key, ok := requiredMutationKey(w, r)
	if !ok {
		return
	}
	workspaceID, _ := input["workspaceId"].(string)
	applicationID, _ := input["applicationId"].(string)
	targetRevision, _ := input["targetRevision"].(string)
	var configuration contracts.WorkspaceApplicationRuntimeConfiguration
	rawConfiguration, configErr := json.Marshal(input["configuration"])
	if configErr != nil {
		writeError(w, http.StatusBadRequest, "invalid_application_configuration")
		return
	}
	configurationDecoder := json.NewDecoder(bytes.NewReader(rawConfiguration))
	configurationDecoder.DisallowUnknownFields()
	if configurationDecoder.Decode(&configuration) != nil {
		writeError(w, http.StatusBadRequest, "invalid_application_configuration")
		return
	}

	var requestedBindings []contracts.WorkspaceApplicationRuntimeSecretBinding
	if supplied, exists := input["secretBindings"]; exists {
		encoded, err := json.Marshal(supplied)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_application_secret_bindings")
			return
		}
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&requestedBindings); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_application_secret_bindings")
			return
		}
	}
	if _, supplied := input["configurationDigest"]; supplied {
		writeError(w, http.StatusBadRequest, "client_configuration_digest_forbidden")
		return
	}
	if _, supplied := input["secretBindingVersions"]; supplied {
		writeError(w, http.StatusBadRequest, "client_secret_binding_forbidden")
		return
	}
	if _, supplied := input["dataBindingIds"]; supplied {
		writeError(w, http.StatusBadRequest, "client_data_binding_forbidden")
		return
	}
	if workspaceID == "" || applicationID == "" || targetRevision == "" {
		writeError(w, http.StatusBadRequest, "invalid_application_deployment")
		return
	}
	clientConfiguration, err := json.Marshal(configuration)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_application_configuration")
		return
	}
	clientConfigurationDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(clientConfiguration))
	if len(requestedBindings) > 0 {

		clientConfigurationDigest, err = contracts.WorkspaceApplicationConfigurationDigest(configuration, requestedBindings, workspaceID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_application_secret_bindings")
			return
		}
	}

	priorRow, replayed, err := app.tables.GetRuntimeOperation(r.Context(), workspaceApplicationDeploymentOperationID(workspaceID, key))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if replayed {
		prior, err := decodeWorkspaceApplicationDeploymentIntent(priorRow)
		if err != nil || prior.Version != 2 || prior.ApplicationID != applicationID || prior.TargetRevision != targetRevision || prior.ClientConfigurationDigest != clientConfigurationDigest || prior.OriginOperationID != "" {
			writeError(w, http.StatusConflict, errWorkspaceApplicationIntentConflict.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"intent": prior})
		return
	}
	secretBindings := requestedBindings
	var workspaceAPIKeyID int64

	revisionRow, admitted, revisionErr := app.tables.AdmittedApplicationRevision(r.Context(), applicationID, targetRevision)
	if revisionErr != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if admitted {
		revision, valid := decodeApplicationRevisionPayload(stringValue(revisionRow["payload"]))
		if !valid {
			writeError(w, http.StatusConflict, "workspace_application_revision_invalid")
			return
		}
		if contracts.WorkspaceApplicationRequiresPlatformCredentials(revision) {
			if len(requestedBindings) > 0 {
				writeError(w, http.StatusBadRequest, "workspace_application_owned_configuration_conflict")
				return
			}
			requestedEnvironment := configuration.Environment
			requestedFiles := configuration.Files
			var prepErr error
			configuration, secretBindings, workspaceAPIKeyID, prepErr = app.workspaceOPLApplicationConfiguration(r.Context(), service, workspaceID, applicationID)
			if prepErr != nil {
				writeError(w, http.StatusConflict, prepErr.Error())
				return
			}

			for name, value := range requestedEnvironment {
				if owned, exists := configuration.Environment[name]; exists && owned != value {
					writeError(w, http.StatusBadRequest, "workspace_application_owned_configuration_conflict")
					return
				}
				configuration.Environment[name] = value
			}
			configuration.Files = requestedFiles
		}
	}

	if rawRevision, supplied := input["revision"]; supplied {
		encoded, err := json.Marshal(rawRevision)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_application_revision")
			return
		}
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.DisallowUnknownFields()
		var revision contracts.WorkspaceApplicationRevision
		if err := decoder.Decode(&revision); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_application_revision")
			return
		}
		if revision.ApplicationID != applicationID || revision.Version != targetRevision {
			writeError(w, http.StatusBadRequest, "invalid_application_revision")
			return
		}
		if err := contracts.ValidateWorkspaceApplicationRevision(revision); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_application_revision")
			return
		}
		user, ok := app.sessionUserContext(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "not_authenticated")
			return
		}
		if _, _, err := app.admitWorkspaceApplicationRevision(r.Context(), revision, stringValue(user["id"])); err != nil {

			writeError(w, http.StatusConflict, err.Error())
			return
		}

		admittedRow, admittedNow, err := app.tables.AdmittedApplicationRevision(r.Context(), applicationID, targetRevision)
		if err != nil || !admittedNow {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		admittedRevision, valid := decodeApplicationRevisionPayload(stringValue(admittedRow["payload"]))
		if !valid {
			writeError(w, http.StatusConflict, "workspace_application_revision_invalid")
			return
		}
		if contracts.WorkspaceApplicationRequiresPlatformCredentials(admittedRevision) {
			if len(requestedBindings) > 0 {
				writeError(w, http.StatusBadRequest, "workspace_application_owned_configuration_conflict")
				return
			}
			requestedEnvironment := configuration.Environment
			requestedFiles := configuration.Files
			var prepErr error
			configuration, secretBindings, workspaceAPIKeyID, prepErr = app.workspaceOPLApplicationConfiguration(r.Context(), service, workspaceID, applicationID)
			if prepErr != nil {
				writeError(w, http.StatusConflict, prepErr.Error())
				return
			}
			for name, value := range requestedEnvironment {
				if owned, exists := configuration.Environment[name]; exists && owned != value {
					writeError(w, http.StatusBadRequest, "workspace_application_owned_configuration_conflict")
					return
				}
				configuration.Environment[name] = value
			}
			configuration.Files = requestedFiles
		}
	}
	intent, err := app.createWorkspaceApplicationDeploymentIntent(
		r.Context(), workspaceID, key, applicationID, targetRevision, configuration, secretBindings, workspaceAPIKeyID, "", clientConfigurationDigest,
	)
	if err != nil {
		switch {
		case errors.Is(err, errWorkspaceApplicationConfigurationInvalid):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, errWorkspaceApplicationWorkspaceGone):
			writeError(w, http.StatusNotFound, "workspace_not_found")
		case errors.Is(err, application.ErrRevisionNotAdmitted):
			writeError(w, http.StatusNotFound, "workspace_application_revision_not_found")
		case errors.Is(err, errWorkspaceApplicationResourcesUnready), errors.Is(err, errWorkspaceApplicationBindingUnknown),
			errors.Is(err, application.ErrDeploymentTransitionInvalid):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, errWorkspaceApplicationIntentConflict):
			writeError(w, http.StatusConflict, errWorkspaceApplicationIntentConflict.Error())
		default:
			writeError(w, http.StatusInternalServerError, "state_persist_failed")
		}
		return
	}
	if workspaceApplicationDeploymentWorkerEnabled() && intent.Phase == workspaceApplicationDeploymentIntentPhase {
		go func() {
			_ = app.runWorkspaceApplicationDeployment(context.Background(), service, intent.OperationID)
		}()
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"intent": intent})
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `applicationId` | `map value; validator in handler` | body input access |
| `configuration` | `map value; validator in handler` | body input access |
| `configurationDigest` | `map value; validator in handler` | body input access |
| `dataBindingIds` | `map value; validator in handler` | body input access |
| `revision` | `map value; validator in handler` | body input access |
| `secretBindingVersions` | `map value; validator in handler` | body input access |
| `secretBindings` | `map value; validator in handler` | body input access |
| `targetRevision` | `map value; validator in handler` | body input access |
| `workspaceId` | `map value; validator in handler` | body input access |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusAccepted, map[string]any{"intent": intent})`
- `writeJSON(w, http.StatusAccepted, map[string]any{"intent": prior})`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/control-plane/internal/clients/fabric.go:96`; `Error` @ `services/control-plane/internal/clients/sub2api.go:432`; `Error` @ `services/control-plane/internal/clients/sub2api.go:446`; `Error` @ `services/control-plane/internal/clients/workspace_registry.go:84`; `stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `AdmittedApplicationRevision` @ `services/control-plane/internal/server/application_revision_store.go:46`; `decodeApplicationRevisionPayload` @ `services/control-plane/internal/server/application_revision_store.go:96`; `sessionUserContext` @ `services/control-plane/internal/server/auth_accounts.go:435`; `GetRuntimeOperation` @ `services/control-plane/internal/server/ent_state_store.go:694`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `decodeJSON` @ `services/control-plane/internal/server/server.go:446`; `requiredMutationKey` @ `services/control-plane/internal/server/server.go:486`; `admitWorkspaceApplicationRevision` @ `services/control-plane/internal/server/workspace_application_admission.go:20`; `decodeWorkspaceApplicationDeploymentIntent` @ `services/control-plane/internal/server/workspace_application_deployment.go:120`; `createWorkspaceApplicationDeploymentIntent` @ `services/control-plane/internal/server/workspace_application_deployment.go:141`; `workspaceApplicationDeploymentOperationID` @ `services/control-plane/internal/server/workspace_application_deployment.go:77`; `workspaceApplicationDeploymentWorkerEnabled` @ `services/control-plane/internal/server/workspace_application_deployment_driver.go:49`; `runWorkspaceApplicationDeployment` @ `services/control-plane/internal/server/workspace_application_deployment_driver.go:96`; `workspaceOPLApplicationConfiguration` @ `services/control-plane/internal/server/workspace_default_application.go:272`; `Error` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:75`

### 76. POST /api/operator/application-deployments/{operationID}/retry

注册：`services/control-plane/internal/server/workspace_application_recovery.go:147`；`registerWorkspaceApplicationRecoveryRoutes`。

实际调用：`Context`, `PathValue`, `ResumeWorkspaceApplicationDeployment`, `decodeWorkspaceApplicationDeploymentIntent`, `protected`, `requiredMutationKey`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	if _, ok := requiredMutationKey(w, r); !ok {
		return
	}
	row, err := app.tables.ResumeWorkspaceApplicationDeployment(r.Context(), r.PathValue("operationID"))
	if err != nil {
		writeError(w, http.StatusConflict, "workspace_application_recovery_conflict")
		return
	}
	intent, err := decodeWorkspaceApplicationDeploymentIntent(row)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"intent": intent})
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `operationID` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusAccepted, map[string]any{"intent": intent})`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `requiredMutationKey` @ `services/control-plane/internal/server/server.go:486`; `decodeWorkspaceApplicationDeploymentIntent` @ `services/control-plane/internal/server/workspace_application_deployment.go:120`; `ResumeWorkspaceApplicationDeployment` @ `services/control-plane/internal/server/workspace_application_recovery.go:100`

### 77. POST /api/operator/application-revisions

注册：`services/control-plane/internal/server/workspace_application_admission.go:55`；`registerApplicationRevisionRoutes`。

实际调用：`Context`, `Decode`, `DisallowUnknownFields`, `Error`, `Is`, `Marshal`, `NewDecoder`, `NewReader`, `ValidateWorkspaceApplicationRevision`, `admitWorkspaceApplicationRevision`, `decodeJSON`, `protected`, `requiredMutationKey`, `sessionUserContext`, `string`, `stringValue`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	input := decodeJSON(r)
	if _, ok := requiredMutationKey(w, r); !ok {
		return
	}
	user, ok := app.sessionUserContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "not_authenticated")
		return
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_application_revision")
		return
	}
	var revision contracts.WorkspaceApplicationRevision
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&revision) != nil {
		writeError(w, http.StatusBadRequest, "invalid_application_revision")
		return
	}
	if err := contracts.ValidateWorkspaceApplicationRevision(revision); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_application_revision")
		return
	}
	row, decision, err := app.admitWorkspaceApplicationRevision(r.Context(), revision, stringValue(user["id"]))
	if err != nil {
		if errors.Is(err, application.ErrRevisionConflict) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "state_persist_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"decision": string(decision), "revision": row})
})
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, map[string]any{"decision": string(decision), "revision": row})`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/control-plane/internal/clients/fabric.go:96`; `Error` @ `services/control-plane/internal/clients/sub2api.go:432`; `Error` @ `services/control-plane/internal/clients/sub2api.go:446`; `Error` @ `services/control-plane/internal/clients/workspace_registry.go:84`; `stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `sessionUserContext` @ `services/control-plane/internal/server/auth_accounts.go:435`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `decodeJSON` @ `services/control-plane/internal/server/server.go:446`; `requiredMutationKey` @ `services/control-plane/internal/server/server.go:486`; `admitWorkspaceApplicationRevision` @ `services/control-plane/internal/server/workspace_application_admission.go:20`; `Error` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:75`

### 78. POST /api/operator/billing-reviews/{resourceType}/{id}/resolve

注册：`services/control-plane/internal/server/routes_admin.go:612`；`registerAdminRoutes`。

实际调用：`Context`, `Error`, `Get`, `PathValue`, `TrimSpace`, `appendBillingReviewResolutionAudit`, `billingReviewRequestShapeValid`, `decodeJSON`, `protected`, `resolveWorkspaceRenewalReview`, `sessionUserID`, `stringValue`, `validBillingReviewEvidenceRef`, `validBillingReviewOpaqueID`, `writeBillingReviewResolutionError`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	input := decodeJSON(r)
	if !billingReviewRequestShapeValid(input) {
		writeError(w, http.StatusBadRequest, errInvalidBillingReview.Error())
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeError(w, http.StatusBadRequest, "missing Idempotency-Key")
		return
	}
	if !validBillingReviewOpaqueID(key) {
		writeError(w, http.StatusBadRequest, "invalid_idempotency_key")
		return
	}
	evidenceRef := strings.TrimSpace(stringValue(input["evidenceRef"]))
	if !validBillingReviewEvidenceRef(evidenceRef) {
		writeError(w, http.StatusBadRequest, "invalid_evidence_ref")
		return
	}
	resolution := billingReviewResolutionInput{
		ResourceType:	strings.TrimSpace(r.PathValue("resourceType")), ResourceID: strings.TrimSpace(r.PathValue("id")),
		AccountID:	strings.TrimSpace(stringValue(input["accountId"])), BillingOperationID: strings.TrimSpace(stringValue(input["billingOperationId"])),
		Decision:	strings.TrimSpace(stringValue(input["decision"])), EvidenceRef: evidenceRef, IdempotencyKey: key, Reviewer: app.sessionUserID(r),
	}
	if resolution.ResourceType != "workspace" {
		writeError(w, http.StatusBadRequest, errInvalidBillingReview.Error())
		return
	}
	result, err := app.resolveWorkspaceRenewalReview(r.Context(), service, resolution)
	if err != nil {
		writeBillingReviewResolutionError(w, err)
		return
	}
	if err := app.appendBillingReviewResolutionAudit(r, key, result); err != nil {
		writeError(w, http.StatusInternalServerError, "state_persist_failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `Idempotency-Key` | `string` | header |
| `accountId` | `map value; validator in handler` | body input access |
| `billingOperationId` | `map value; validator in handler` | body input access |
| `decision` | `map value; validator in handler` | body input access |
| `evidenceRef` | `map value; validator in handler` | body input access |
| `id` | `string` | path |
| `resourceType` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, result)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/control-plane/internal/clients/fabric.go:96`; `Error` @ `services/control-plane/internal/clients/sub2api.go:432`; `Error` @ `services/control-plane/internal/clients/sub2api.go:446`; `Error` @ `services/control-plane/internal/clients/workspace_registry.go:84`; `appendBillingReviewResolutionAudit` @ `services/control-plane/internal/server/admin_ops.go:97`; `stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `sessionUserID` @ `services/control-plane/internal/server/auth_accounts.go:427`; `billingReviewRequestShapeValid` @ `services/control-plane/internal/server/routes_admin.go:1528`; `validBillingReviewEvidenceRef` @ `services/control-plane/internal/server/routes_admin.go:1541`; `validBillingReviewOpaqueID` @ `services/control-plane/internal/server/routes_admin.go:1545`; `writeBillingReviewResolutionError` @ `services/control-plane/internal/server/routes_admin.go:1558`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `decodeJSON` @ `services/control-plane/internal/server/server.go:446`; `Get` @ `services/control-plane/internal/server/session_credential_vault.go:38`; `Error` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:75`; `resolveWorkspaceRenewalReview` @ `services/control-plane/internal/server/workspace_renewal.go:633`

### 79. POST /api/operator/provider-acceptance

注册：`services/control-plane/internal/server/routes_provider_acceptance.go:60`；`registerProviderAcceptanceRoutes`。

实际调用：`ClaimWorkspaceCreate`, `Context`, `Error`, `Is`, `Now`, `SaveRuntimeOperation`, `Sub2APIWorkspaceKey`, `UTC`, `advanceProviderAcceptance`, `appendAuditEvent`, `decodeJSON`, `delete`, `len`, `lockResource`, `mustJSON`, `numberField`, `providerAcceptanceAttachment`, `providerAcceptanceAttachmentIdentityValid`, `providerAcceptanceComputeCandidates`, `providerAcceptanceComputeID`, `providerAcceptanceComputeIdentityValid`, `providerAcceptanceIdentity`, `providerAcceptanceOperation`, `providerAcceptanceOperationRow`, `providerAcceptanceOperationValid`, `providerAcceptancePreflight`, `providerAcceptanceProtected`, `providerAcceptanceReadFacts`, `providerAcceptanceReadySlot`, `providerAcceptanceResourceInventoryValid`, `providerAcceptanceResponse`, `providerAcceptanceSlotSummary`, `providerAcceptanceStorageCandidates`, `providerAcceptanceStorageID`, `providerAcceptanceStorageIdentityValid`, `providerAcceptanceWorkspace`, `providerAcceptanceWorkspaceCandidateValid`, `providerAcceptanceWorkspaceCandidates`, `providerAcceptanceWorkspaceClaim`, `requiredMutationKey`, `string`, `stringField`, `stringValue`, `unlock`, `writeError`, `writeJSON`, `writeProviderAcceptanceManualReview`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.providerAcceptanceProtected(func(w http.ResponseWriter, r *http.Request) {
	input := decodeJSON(r)
	key, ok := requiredMutationKey(w, r)
	if !ok {
		return
	}
	if stringField(input, "confirmation", "") != providerAcceptanceConfirmation {
		writeError(w, http.StatusBadRequest, "provider_acceptance_confirmation_required")
		return
	}
	slot, exists := providerAcceptanceSlots[stringField(input, "slotId", "")]
	if !exists {
		writeError(w, http.StatusBadRequest, "provider_acceptance_slot_fixed")
		return
	}
	if stringField(input, "accountId", "") != slot.AccountID {
		writeError(w, http.StatusBadRequest, "provider_acceptance_account_fixed")
		return
	}
	if key != slot.Key {
		writeError(w, http.StatusConflict, "provider_acceptance_idempotency_key_fixed")
		return
	}

	unlock := app.lockResource("provider-acceptance", slot.ID)
	defer unlock()

	ownerID, sub2APIUserID, code := app.providerAcceptanceIdentity(r.Context(), slot)
	if code != "" {
		writeError(w, http.StatusConflict, code)
		return
	}
	workspaces, err := app.providerAcceptanceWorkspaceCandidates(r.Context(), slot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	workspace, conflict := providerAcceptanceWorkspace(workspaces, slot)
	if conflict {
		writeError(w, http.StatusConflict, errPrimaryWorkspaceExists.Error())
		return
	}
	operation, operationExists, err := app.providerAcceptanceOperation(r.Context(), slot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	computes, err := app.providerAcceptanceComputeCandidates(r.Context(), slot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	storages, err := app.providerAcceptanceStorageCandidates(r.Context(), slot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	attachment, attachmentCount, err := app.providerAcceptanceAttachment(r.Context(), slot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	identitiesValid := providerAcceptanceResourceInventoryValid(computes, slot, providerAcceptanceComputeID(slot), ownerID) &&
		providerAcceptanceResourceInventoryValid(storages, slot, providerAcceptanceStorageID(slot), ownerID)
	workspaceIdentityValid := workspace == nil || providerAcceptanceWorkspaceCandidateValid(workspace, slot, ownerID)
	attachmentInventoryValid := attachmentCount == 0 || (attachmentCount == 1 && providerAcceptanceAttachmentIdentityValid(attachment, slot))
	emptyInventory := workspace == nil && len(computes) == 0 && len(storages) == 0 && attachmentCount == 0
	completeInventory := providerAcceptanceWorkspaceCandidateValid(workspace, slot, ownerID) && len(computes) == 1 && len(storages) == 1 &&
		providerAcceptanceComputeIdentityValid(computes[0], slot, ownerID) && providerAcceptanceStorageIdentityValid(storages[0], slot, ownerID) &&
		attachmentCount == 1 && providerAcceptanceAttachmentIdentityValid(attachment, slot)
	invalidOperation := operationExists && !providerAcceptanceOperationValid(operation, slot)
	unclaimedAmbiguousInventory := !operationExists && !emptyInventory && !completeInventory
	if !workspaceIdentityValid || !identitiesValid || !attachmentInventoryValid || invalidOperation || unclaimedAmbiguousInventory {
		writeError(w, http.StatusConflict, "provider_acceptance_inventory_ambiguous")
		return
	}
	providerFacts, err := providerAcceptanceReadFacts(r.Context(), service, slot, workspace, computes, storages, attachment)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if operationExists && stringValue(operation["status"]) == "manual_review" {
		summary, err := app.providerAcceptanceSlotSummary(r.Context(), slot, providerFacts)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		writeJSON(w, http.StatusOK, providerAcceptanceResponse("manual_review", stringValue(operation["errorCode"]), summary))
		return
	}
	summary, ready, err := app.providerAcceptanceReadySlot(r.Context(), slot, ownerID, providerFacts, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if ready {
		if !operationExists {
			operation = providerAcceptanceOperationRow("succeeded", slot)
		}
		if !operationExists || stringValue(operation["status"]) == "started" {
			operation["status"] = "succeeded"
			delete(operation, "errorCode")
			operation["result"] = string(mustJSON(providerAcceptanceResponse("reused", "", summary)))
			if err := app.tables.SaveRuntimeOperation(r.Context(), operation); err != nil {
				writeError(w, http.StatusInternalServerError, "state_persist_failed")
				return
			}
		}
		writeJSON(w, http.StatusOK, providerAcceptanceResponse("reused", "", summary))
		return
	}
	if operationExists && stringValue(operation["status"]) == "succeeded" {
		app.writeProviderAcceptanceManualReview(w, r, operation, slot, providerFacts, "provider_acceptance_state_ambiguous")
		return
	}
	approved, _ := input["environmentApproved"].(bool)
	if !approved {
		writeError(w, http.StatusConflict, "provider_acceptance_environment_approval_required")
		return
	}
	if numberField(input, "purchaseBudget", 0) != 1 {
		writeError(w, http.StatusConflict, "provider_acceptance_purchase_budget_invalid")
		return
	}
	maxApprovedProviderCost := numberField(input, "maxApprovedProviderCost", 0)
	if maxApprovedProviderCost <= 0 {
		writeError(w, http.StatusConflict, "provider_acceptance_provider_cost_approval_required")
		return
	}

	workspaceKey, err := service.Sub2APIWorkspaceKey(r.Context(), sub2APIUserID)
	if err != nil || workspaceKey.UserID != sub2APIUserID || workspaceKey.Name != "opl-workspace" || workspaceKey.Status != "active" || workspaceKey.Key == "" {
		writeError(w, http.StatusConflict, "provider_acceptance_gateway_key_required")
		return
	}
	computePreflight, storagePreflight, ok := providerAcceptancePreflight(r.Context(), service, slot)
	if !ok {
		writeError(w, http.StatusConflict, "provider_acceptance_preflight_failed")
		return
	}
	if computePreflight.ProviderPriceCNY+storagePreflight.ProviderPriceCNY > maxApprovedProviderCost {
		writeError(w, http.StatusConflict, "provider_acceptance_provider_cost_exceeds_approval")
		return
	}

	if !operationExists {
		operation = providerAcceptanceOperationRow("started", slot)
		if workspace == nil {
			workspace = providerAcceptanceWorkspaceClaim(ownerID, slot)
			if err := app.tables.ClaimWorkspaceCreate(r.Context(), workspace, operation); err != nil {
				if errors.Is(err, errPrimaryWorkspaceExists) {
					writeError(w, http.StatusConflict, errPrimaryWorkspaceExists.Error())
				} else {
					writeError(w, http.StatusInternalServerError, "state_persist_failed")
				}
				return
			}
		} else if err := app.tables.SaveRuntimeOperation(r.Context(), operation); err != nil {
			writeError(w, http.StatusInternalServerError, "state_persist_failed")
			return
		}
	}

	status, reason, err := app.advanceProviderAcceptance(r.Context(), service, slot, ownerID, sub2APIUserID, computePreflight, storagePreflight, providerFacts)
	if err != nil {
		if errors.Is(err, errProviderAcceptanceStateRead) {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
		} else {
			writeError(w, http.StatusInternalServerError, "state_persist_failed")
		}
		return
	}
	if reason != "" {
		app.writeProviderAcceptanceManualReview(w, r, operation, slot, providerFacts, reason)
		return
	}
	summary, err = app.providerAcceptanceSlotSummary(r.Context(), slot, providerFacts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if status == "ready" {
		operation["status"] = "succeeded"
		operation["result"] = string(mustJSON(providerAcceptanceResponse("ready", "", summary)))
		if err := app.appendAuditEvent(r, "operator.provider_acceptance", "verification_slot", slot.ID, slot.AccountID, nil, summary, "succeeded"); err != nil {
			app.writeProviderAcceptanceManualReview(w, r, operation, slot, providerFacts, "provider_acceptance_audit_failed")
			return
		}
		if err := app.tables.SaveRuntimeOperation(r.Context(), operation); err != nil {
			writeError(w, http.StatusInternalServerError, "state_persist_failed")
			return
		}
	}
	writeJSON(w, http.StatusOK, providerAcceptanceResponse(status, "", summary))
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `environmentApproved` | `map value; validator in handler` | body input access |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, providerAcceptanceResponse("manual_review", stringValue(operation["errorCode"]), summary))`
- `writeJSON(w, http.StatusOK, providerAcceptanceResponse("reused", "", summary))`
- `writeJSON(w, http.StatusOK, providerAcceptanceResponse(status, "", summary))`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/control-plane/internal/clients/fabric.go:96`; `stringField` @ `services/control-plane/internal/clients/ledger.go:361`; `Error` @ `services/control-plane/internal/clients/sub2api.go:432`; `Error` @ `services/control-plane/internal/clients/sub2api.go:446`; `Error` @ `services/control-plane/internal/clients/workspace_registry.go:84`; `Sub2APIWorkspaceKey` @ `services/control-plane/internal/controlplane/service.go:78`; `appendAuditEvent` @ `services/control-plane/internal/server/admin_ops.go:93`; `lockResource` @ `services/control-plane/internal/server/app_state.go:42`; `mustJSON` @ `services/control-plane/internal/server/app_state.go:491`; `stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `SaveRuntimeOperation` @ `services/control-plane/internal/server/ent_state_store.go:742`; `ClaimWorkspaceCreate` @ `services/control-plane/internal/server/ent_state_store_workspace.go:1215`; `providerAcceptanceProtected` @ `services/control-plane/internal/server/routes_provider_acceptance.go:257`; `providerAcceptanceIdentity` @ `services/control-plane/internal/server/routes_provider_acceptance.go:274`; `providerAcceptanceWorkspace` @ `services/control-plane/internal/server/routes_provider_acceptance.go:294`; `providerAcceptanceWorkspaceCandidateValid` @ `services/control-plane/internal/server/routes_provider_acceptance.go:308`; `providerAcceptanceResourceInventoryValid` @ `services/control-plane/internal/server/routes_provider_acceptance.go:317`; `providerAcceptanceWorkspaceClaim` @ `services/control-plane/internal/server/routes_provider_acceptance.go:329`; `providerAcceptanceOperationRow` @ `services/control-plane/internal/server/routes_provider_acceptance.go:339`; `providerAcceptanceOperation` @ `services/control-plane/internal/server/routes_provider_acceptance.go:348`; `providerAcceptanceOperationValid` @ `services/control-plane/internal/server/routes_provider_acceptance.go:356`; `providerAcceptancePreflight` @ `services/control-plane/internal/server/routes_provider_acceptance.go:366`; `providerAcceptanceComputeID` @ `services/control-plane/internal/server/routes_provider_acceptance.go:390`; `providerAcceptanceStorageID` @ `services/control-plane/internal/server/routes_provider_acceptance.go:394`; `providerAcceptanceWorkspaceCandidates` @ `services/control-plane/internal/server/routes_provider_acceptance.go:412`; `providerAcceptanceComputeCandidates` @ `services/control-plane/internal/server/routes_provider_acceptance.go:422`; `providerAcceptanceStorageCandidates` @ `services/control-plane/internal/server/routes_provider_acceptance.go:433`; `advanceProviderAcceptance` @ `services/control-plane/internal/server/routes_provider_acceptance.go:492`; `providerAcceptanceComputeIdentityValid` @ `services/control-plane/internal/server/routes_provider_acceptance.go:675`; `providerAcceptanceStorageIdentityValid` @ `services/control-plane/internal/server/routes_provider_acceptance.go:681`; `providerAcceptanceReadFacts` @ `services/control-plane/internal/server/routes_provider_acceptance.go:694`; `providerAcceptanceAttachment` @ `services/control-plane/internal/server/routes_provider_acceptance.go:752`; `providerAcceptanceAttachmentIdentityValid` @ `services/control-plane/internal/server/routes_provider_acceptance.go:763`; `providerAcceptanceReadySlot` @ `services/control-plane/internal/server/routes_provider_acceptance.go:799`; `providerAcceptanceSlotSummary` @ `services/control-plane/internal/server/routes_provider_acceptance.go:833`; `providerAcceptanceResponse` @ `services/control-plane/internal/server/routes_provider_acceptance.go:873`; `writeProviderAcceptanceManualReview` @ `services/control-plane/internal/server/routes_provider_acceptance.go:881`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `decodeJSON` @ `services/control-plane/internal/server/server.go:446`; `stringField` @ `services/control-plane/internal/server/server.go:454`; `numberField` @ `services/control-plane/internal/server/server.go:461`; `requiredMutationKey` @ `services/control-plane/internal/server/server.go:486`; `Error` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:75`

### 80. POST /api/operator/registry/resolve

注册：`services/control-plane/internal/server/workspace_registry_catalog.go:165`；`registerWorkspaceRegistryCatalogRoutesWithCatalog`。

实际调用：`Context`, `TrimSpace`, `ValidateWorkspaceRegistryRepository`, `decodeJSON`, `protected`, `requireWorkspaceRegistryCatalog`, `resolve`, `stringValue`, `writeError`, `writeJSON`, `writeWorkspaceRegistryError`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	if !requireWorkspaceRegistryCatalog(w, catalog) {
		return
	}
	input := decodeJSON(r)
	namespace, repository, tag := strings.TrimSpace(stringValue(input["namespace"])), strings.TrimSpace(stringValue(input["repository"])), strings.TrimSpace(stringValue(input["tag"]))
	if namespace == "" || repository == "" || tag == "" {
		writeError(w, http.StatusBadRequest, "workspace_registry_request_invalid")
		return
	}
	if err := contracts.ValidateWorkspaceRegistryRepository(namespace, repository); err != nil {
		writeError(w, http.StatusBadRequest, "workspace_registry_request_invalid")
		return
	}
	resolution, err := catalog.resolve(r.Context(), namespace, repository, tag)
	if err != nil {
		writeWorkspaceRegistryError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"host":	catalog.host, "namespace": namespace, "repository": repository, "tag": tag,
		"digest":	resolution.Digest, "reference": resolution.Reference,
	})
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `namespace` | `map value; validator in handler` | body input access |
| `repository` | `map value; validator in handler` | body input access |
| `tag` | `map value; validator in handler` | body input access |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, map[string]any{<br>	"host":	catalog.host, "namespace": namespace, "repository": repository, "tag": tag,<br>	"digest":	resolution.Digest, "reference": resolution.Reference,<br>})`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `decodeJSON` @ `services/control-plane/internal/server/server.go:446`; `resolve` @ `services/control-plane/internal/server/workspace_registry_catalog.go:120`; `requireWorkspaceRegistryCatalog` @ `services/control-plane/internal/server/workspace_registry_catalog.go:197`; `writeWorkspaceRegistryError` @ `services/control-plane/internal/server/workspace_registry_catalog.go:218`

### 81. POST /api/operator/wallet-adjustments/{operationId}/recover

注册：`services/control-plane/internal/server/routes_admin.go:418`；`registerAdminRoutes`。

实际调用：`protected`, `recoverWalletAdjustment`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	app.recoverWalletAdjustment(w, r, service)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`protected` @ `services/control-plane/internal/server/server.go:277`; `recoverWalletAdjustment` @ `services/control-plane/internal/server/wallet_adjustment.go:190`

### 82. POST /api/operator/workspace-image-release-activations

注册：`services/control-plane/internal/server/workspace_runtime_image_replacement.go:43`；`registerWorkspaceRuntimeImageReplacementRoutes`。

实际调用：`activateWorkspaceImageRelease`, `protected`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	app.activateWorkspaceImageRelease(w, r)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`protected` @ `services/control-plane/internal/server/server.go:277`; `activateWorkspaceImageRelease` @ `services/control-plane/internal/server/workspace_image_release_policy.go:224`

### 83. POST /api/operator/workspace-launches/{operationId}/canonical-facts-repair

注册：`services/control-plane/internal/server/routes_admin.go:150`；`registerAdminRoutes`。

实际调用：`Context`, `Error`, `GetRuntimeOperation`, `ListAuditEvents`, `MatchString`, `PathValue`, `TrimSpace`, `Values`, `applyWorkspaceLaunchCanonicalFactRepair`, `auditEvent`, `decodeJSON`, `decodeWorkspaceLaunchReconcileOperation`, `exactWorkspaceComputeClaimKeys`, `int`, `int64`, `len`, `positiveIntegerField`, `protected`, `requiredMutationKey`, `stringFact`, `stringValue`, `uint`, `validBillingReviewOpaqueID`, `workspaceLaunchCanonicalFactRepairApplyResponse`, `workspaceLaunchCanonicalFactRepairAuditID`, `workspaceLaunchCanonicalFactRepairReplayMatches`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	key, ok := requiredMutationKey(w, r)
	if !ok {
		return
	}
	if len(r.Header.Values("Idempotency-Key")) != 1 || !validBillingReviewOpaqueID(key) {
		writeError(w, http.StatusBadRequest, errWorkspaceLaunchCanonicalFactRepairNotEligible.Error())
		return
	}
	input := decodeJSON(r)
	launchVersion, validVersion := positiveIntegerField(input, "launchVersion")
	previewDigest, reason := strings.TrimSpace(stringValue(input["previewDigest"])), strings.TrimSpace(stringValue(input["reason"]))
	if !validVersion || launchVersion > int64(^uint(0)>>1) || !workspaceLaunchRepairDigestPattern.MatchString(previewDigest) || reason == "" ||
		!exactWorkspaceComputeClaimKeys(input, []string{"launchVersion", "previewDigest", "reason"}) {
		writeError(w, http.StatusBadRequest, errWorkspaceLaunchCanonicalFactRepairNotEligible.Error())
		return
	}
	operationID := strings.TrimSpace(r.PathValue("operationId"))
	audit := app.auditEvent(r, "workspace.launch.canonical_fact_repair", "workspace_launch", operationID, "", nil, nil, "succeeded")
	row, found, err := app.tables.GetRuntimeOperation(r.Context(), operationID)
	if err == nil && found {
		if operation, decodeErr := decodeWorkspaceLaunchReconcileOperation(row); decodeErr == nil && operation.Version == int(launchVersion)+1 {
			auditID := workspaceLaunchCanonicalFactRepairAuditID(operationID, key)
			audits, auditErr := app.tables.ListAuditEvents(r.Context(), operation.stringFact("accountId"))
			for _, existing := range audits {
				if auditErr == nil && workspaceLaunchCanonicalFactRepairReplayMatches(operation, existing, audit, int(launchVersion), previewDigest, key, reason) {
					writeJSON(w, http.StatusOK, workspaceLaunchCanonicalFactRepairApplyResponse(operation, auditID))
					return
				}
			}
		}
	}
	repaired, err := app.applyWorkspaceLaunchCanonicalFactRepair(r.Context(), service, operationID, int(launchVersion), previewDigest, key, reason, audit)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, workspaceLaunchCanonicalFactRepairApplyResponse(repaired, workspaceLaunchCanonicalFactRepairAuditID(operationID, key)))
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `operationId` | `string` | path |
| `previewDigest` | `map value; validator in handler` | body input access |
| `reason` | `map value; validator in handler` | body input access |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, workspaceLaunchCanonicalFactRepairApplyResponse(operation, auditID))`
- `writeJSON(w, http.StatusOK, workspaceLaunchCanonicalFactRepairApplyResponse(repaired, workspaceLaunchCanonicalFactRepairAuditID(operationID, key)))`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/control-plane/internal/clients/fabric.go:96`; `Error` @ `services/control-plane/internal/clients/sub2api.go:432`; `Error` @ `services/control-plane/internal/clients/sub2api.go:446`; `Error` @ `services/control-plane/internal/clients/workspace_registry.go:84`; `auditEvent` @ `services/control-plane/internal/server/admin_ops.go:105`; `stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `positiveIntegerField` @ `services/control-plane/internal/server/auth_accounts.go:103`; `ListAuditEvents` @ `services/control-plane/internal/server/ent_state_store.go:504`; `GetRuntimeOperation` @ `services/control-plane/internal/server/ent_state_store.go:694`; `validBillingReviewOpaqueID` @ `services/control-plane/internal/server/routes_admin.go:1545`; `exactWorkspaceComputeClaimKeys` @ `services/control-plane/internal/server/routes_admin.go:663`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `decodeJSON` @ `services/control-plane/internal/server/server.go:446`; `requiredMutationKey` @ `services/control-plane/internal/server/server.go:486`; `workspaceLaunchCanonicalFactRepairApplyResponse` @ `services/control-plane/internal/server/workspace_launch_canonical_fact_repair.go:120`; `workspaceLaunchCanonicalFactRepairReplayMatches` @ `services/control-plane/internal/server/workspace_launch_canonical_fact_repair.go:179`; `workspaceLaunchCanonicalFactRepairAuditID` @ `services/control-plane/internal/server/workspace_launch_canonical_fact_repair.go:46`; `applyWorkspaceLaunchCanonicalFactRepair` @ `services/control-plane/internal/server/workspace_launch_canonical_fact_repair.go:50`; `decodeWorkspaceLaunchReconcileOperation` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:2250`; `stringFact` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:2821`; `Error` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:75`

### 84. POST /api/operator/workspace-launches/{operationId}/recover

注册：`services/control-plane/internal/server/routes_admin.go:58`；`registerAdminRoutes`。

实际调用：`CheckResult`, `Context`, `Error`, `Format`, `GetRuntimeOperation`, `Is`, `Now`, `PathValue`, `TrimSpace`, `UTC`, `Values`, `closeWorkspaceLaunch`, `decodeJSON`, `decodeWorkspaceLaunchReconcileOperation`, `exactWorkspaceComputeClaimKeys`, `int`, `int64`, `len`, `positiveIntegerField`, `protected`, `requiredMutationKey`, `resultCheckByID`, `sessionUserID`, `stringValue`, `uint`, `validBillingReviewOpaqueID`, `workspaceLaunchReconciler`, `workspaceLaunchRecovery`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	key, ok := requiredMutationKey(w, r)
	if !ok {
		return
	}
	input := decodeJSON(r)
	launchVersion, validVersion := positiveIntegerField(input, "launchVersion")
	reason := stringValue(input["reason"])
	if len(r.Header.Values("Idempotency-Key")) != 1 || !validBillingReviewOpaqueID(key) ||
		!exactWorkspaceComputeClaimKeys(input, []string{"action", "launchVersion", "reason"}) ||
		(stringValue(input["action"]) != "check_result" && stringValue(input["action"]) != "close_unfulfilled") || !validVersion || launchVersion > int64(^uint(0)>>1) ||
		reason == "" || reason != strings.TrimSpace(reason) {
		writeError(w, http.StatusBadRequest, errInvalidBillingReview.Error())
		return
	}
	operationID := strings.TrimSpace(r.PathValue("operationId"))
	row, found, err := app.tables.GetRuntimeOperation(r.Context(), operationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "workspace_launch_not_found")
		return
	}
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		writeError(w, http.StatusConflict, errInvalidWorkspaceLaunchOperation.Error())
		return
	}
	authorization := workspaceLaunchResumeAuthorization{
		AuthorizationID:	key, LaunchVersion: int(launchVersion), AuthorizedStage: operation.Stage,
		AuthorizedBy:	app.sessionUserID(r), AuthorizedAt: time.Now().UTC().Format(time.RFC3339), Reason: reason,
		AuthoritativeReadBudget:	workspaceLaunchAuthoritativeReadBudget,
	}
	if existing, _, exists := operation.resultCheckByID(key); exists {
		authorization.AuthorizedStage = existing.AuthorizedStage
		authorization.AuthorizedAt = existing.AuthorizedAt
		authorization.ReadbacksAtAuthorization = existing.ReadbacksAtAuthorization
	}
	var result workspaceLaunchReconcileOperation
	if stringValue(input["action"]) == "close_unfulfilled" {
		result, err = app.closeWorkspaceLaunch(r.Context(), service, operationID, key, app.sessionUserID(r), reason, int(launchVersion))
	} else {
		result, err = app.workspaceLaunchReconciler(service, clients.SessionDelegatedCredential{}, 0).CheckResult(r.Context(), operationID, authorization)
	}
	if err != nil {
		if errors.Is(err, errBillingReviewNotFound) {
			writeError(w, http.StatusNotFound, "workspace_launch_not_found")
		} else if errors.Is(err, errWorkspaceLaunchGrantConflict) || errors.Is(err, errWorkspaceLaunchCASConflict) || errors.Is(err, errInvalidWorkspaceLaunchOperation) {
			writeError(w, http.StatusConflict, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, "state_persist_failed")
		}
		return
	}
	writeJSON(w, http.StatusOK, app.workspaceLaunchRecovery(r.Context(), service, result))
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `action` | `map value; validator in handler` | body input access |
| `operationId` | `string` | path |
| `reason` | `map value; validator in handler` | body input access |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, app.workspaceLaunchRecovery(r.Context(), service, result))`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/control-plane/internal/clients/fabric.go:96`; `Error` @ `services/control-plane/internal/clients/sub2api.go:432`; `Error` @ `services/control-plane/internal/clients/sub2api.go:446`; `Error` @ `services/control-plane/internal/clients/workspace_registry.go:84`; `stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `positiveIntegerField` @ `services/control-plane/internal/server/auth_accounts.go:103`; `sessionUserID` @ `services/control-plane/internal/server/auth_accounts.go:427`; `GetRuntimeOperation` @ `services/control-plane/internal/server/ent_state_store.go:694`; `validBillingReviewOpaqueID` @ `services/control-plane/internal/server/routes_admin.go:1545`; `exactWorkspaceComputeClaimKeys` @ `services/control-plane/internal/server/routes_admin.go:663`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `decodeJSON` @ `services/control-plane/internal/server/server.go:446`; `requiredMutationKey` @ `services/control-plane/internal/server/server.go:486`; `workspaceLaunchRecovery` @ `services/control-plane/internal/server/workspace_launch_closeout.go:187`; `closeWorkspaceLaunch` @ `services/control-plane/internal/server/workspace_launch_closeout.go:200`; `CheckResult` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:1491`; `resultCheckByID` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:1535`; `decodeWorkspaceLaunchReconcileOperation` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:2250`; `Error` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:75`; `workspaceLaunchReconciler` @ `services/control-plane/internal/server/workspace_launch_service.go:93`

### 85. POST /api/operator/workspace-launches/{operationId}/repair-runtime

注册：`services/control-plane/internal/server/routes_admin.go:199`；`registerAdminRoutes`。

实际调用：`Context`, `Error`, `GetRuntimeOperation`, `Is`, `PathValue`, `TrimSpace`, `Values`, `decodeJSON`, `decodeWorkspaceLaunchReconcileOperation`, `exactWorkspaceComputeClaimKeys`, `int`, `int64`, `len`, `positiveIntegerField`, `protected`, `repairWorkspaceLaunchRuntime`, `requiredMutationKey`, `sessionUserID`, `stringValue`, `uint`, `validBillingReviewOpaqueID`, `workspaceImageReferenceWithDigest`, `workspaceLaunchReconcileResponse`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	key, ok := requiredMutationKey(w, r)
	if !ok {
		return
	}
	if len(r.Header.Values("Idempotency-Key")) != 1 || !validBillingReviewOpaqueID(key) {
		writeError(w, http.StatusBadRequest, errInvalidBillingReview.Error())
		return
	}
	input := decodeJSON(r)
	launchVersion, validVersion := positiveIntegerField(input, "launchVersion")
	reason, imageDigest := stringValue(input["reason"]), stringValue(input["imageDigest"])
	if !validVersion || launchVersion > int64(^uint(0)>>1) || reason == "" || reason != strings.TrimSpace(reason) ||
		imageDigest == "" || !workspaceImageReferenceWithDigest(imageDigest) ||
		!exactWorkspaceComputeClaimKeys(input, []string{"launchVersion", "reason", "imageDigest"}) {
		writeError(w, http.StatusBadRequest, errInvalidBillingReview.Error())
		return
	}
	operationID := strings.TrimSpace(r.PathValue("operationId"))
	row, found, err := app.tables.GetRuntimeOperation(r.Context(), operationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "workspace_launch_not_found")
		return
	}
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		writeError(w, http.StatusConflict, errWorkspaceLaunchRepairNotEligible.Error())
		return
	}
	operatorUserID := app.sessionUserID(r)
	exactReplay := operation.RuntimeRepair != nil && operation.RuntimeRepair.AuthorizationID == key &&
		operation.RuntimeRepair.AuthorizedBy == operatorUserID && operation.RuntimeRepair.LaunchVersion == int(launchVersion) && operation.RuntimeRepair.Reason == reason && operation.RuntimeRepair.ImageDigest == imageDigest
	if !exactReplay && operation.Version != int(launchVersion) {
		writeError(w, http.StatusConflict, errWorkspaceLaunchRepairNotEligible.Error())
		return
	}
	repaired, err := app.repairWorkspaceLaunchRuntime(r.Context(), service, operationID, int(launchVersion), key, operatorUserID, reason, imageDigest)
	if err != nil {
		if errors.Is(err, errBillingReviewNotFound) {
			writeError(w, http.StatusNotFound, "workspace_launch_not_found")
		} else {
			writeError(w, http.StatusConflict, err.Error())
		}
		return
	}
	body, err := workspaceLaunchReconcileResponse(repaired, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	repair := map[string]any{"operationId": operationID, "authorizationId": key, "authorizedBy": operatorUserID, "reason": reason, "imageDigest": imageDigest}
	if repaired.RuntimeRepair != nil {
		repair["authorizedAt"] = repaired.RuntimeRepair.AuthorizedAt
	}
	body["repair"] = repair
	writeJSON(w, http.StatusOK, body)
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `imageDigest` | `map value; validator in handler` | body input access |
| `operationId` | `string` | path |
| `reason` | `map value; validator in handler` | body input access |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, body)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/control-plane/internal/clients/fabric.go:96`; `Error` @ `services/control-plane/internal/clients/sub2api.go:432`; `Error` @ `services/control-plane/internal/clients/sub2api.go:446`; `Error` @ `services/control-plane/internal/clients/workspace_registry.go:84`; `stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `positiveIntegerField` @ `services/control-plane/internal/server/auth_accounts.go:103`; `sessionUserID` @ `services/control-plane/internal/server/auth_accounts.go:427`; `GetRuntimeOperation` @ `services/control-plane/internal/server/ent_state_store.go:694`; `validBillingReviewOpaqueID` @ `services/control-plane/internal/server/routes_admin.go:1545`; `exactWorkspaceComputeClaimKeys` @ `services/control-plane/internal/server/routes_admin.go:663`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `decodeJSON` @ `services/control-plane/internal/server/server.go:446`; `requiredMutationKey` @ `services/control-plane/internal/server/server.go:486`; `workspaceLaunchReconcileResponse` @ `services/control-plane/internal/server/workspace_launch.go:143`; `decodeWorkspaceLaunchReconcileOperation` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:2250`; `Error` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:75`; `workspaceImageReferenceWithDigest` @ `services/control-plane/internal/server/workspace_launch_repair.go:180`; `repairWorkspaceLaunchRuntime` @ `services/control-plane/internal/server/workspace_launch_repair.go:82`

### 86. POST /api/operator/workspace-launches/{operationId}/resume

注册：`services/control-plane/internal/server/routes_admin.go:296`；`registerAdminRoutes`。

实际调用：`Context`, `Error`, `Is`, `PathValue`, `Stage`, `TrimSpace`, `Values`, `bindProductionAcceptanceBResumeExisting`, `decodeJSON`, `exactWorkspaceComputeClaimKeys`, `float64`, `int`, `int64`, `len`, `parseProductionAcceptanceBResumeExistingApproval`, `positiveIntegerField`, `productionAcceptanceBResumeExistingRequestMode`, `protected`, `requiredMutationKey`, `resumeWorkspaceLaunch`, `sessionUserID`, `stringValue`, `uint`, `validBillingReviewOpaqueID`, `workspaceImageReferenceWithDigest`, `workspaceLaunchReconcileResponse`, `workspaceLaunchResumeAuthorizationReadback`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	key, ok := requiredMutationKey(w, r)
	if !ok {
		return
	}
	if len(r.Header.Values("Idempotency-Key")) != 1 || !validBillingReviewOpaqueID(key) {
		writeError(w, http.StatusBadRequest, errInvalidBillingReview.Error())
		return
	}
	resumeExistingRequested, validResumeExistingHeaders := productionAcceptanceBResumeExistingRequestMode(r.Header)
	if !validResumeExistingHeaders {
		writeError(w, http.StatusBadRequest, errInvalidBillingReview.Error())
		return
	}
	operationID := strings.TrimSpace(r.PathValue("operationId"))
	input := decodeJSON(r)
	launchVersion, validVersion := positiveIntegerField(input, "launchVersion")
	authorizedStage, reason := stringValue(input["authorizedStage"]), stringValue(input["reason"])
	replacementWorkspaceImageDigest, replacementWorkspaceImageProvided := input["replacementWorkspaceImageDigest"].(string)
	_, replacementWorkspaceImageFieldPresent := input["replacementWorkspaceImageDigest"]
	mutationBudget, validBudget := input["mutationBudget"].(float64)
	idempotentReplayBudget, replayProvided := input["idempotentReplayBudget"].(float64)
	authoritativeReadBudget, readBudgetProvided := input["authoritativeReadBudget"].(float64)
	exactFields := exactWorkspaceComputeClaimKeys(input, []string{"launchVersion", "authorizedStage", "reason", "mutationBudget"}) ||
		exactWorkspaceComputeClaimKeys(input, []string{"launchVersion", "authorizedStage", "reason", "mutationBudget", "idempotentReplayBudget", "authoritativeReadBudget"}) ||
		exactWorkspaceComputeClaimKeys(input, []string{"launchVersion", "authorizedStage", "reason", "mutationBudget", "idempotentReplayBudget", "authoritativeReadBudget", "replacementWorkspaceImageDigest"})
	validAuthoritativeReadBudget := authoritativeReadBudget == workspaceLaunchAuthoritativeReadBudget ||
		authorizedStage == "ensure_compute_allocation" && authoritativeReadBudget > 0 &&
			authoritativeReadBudget <= float64(workspaceLaunchComputeFreshContinuationAdditionalReadBudget)
	validRuntimeImageRevision := !replacementWorkspaceImageFieldPresent || replacementWorkspaceImageProvided && authorizedStage == "runtime" &&
		workspaceImageReferenceWithDigest(replacementWorkspaceImageDigest) && mutationBudget == 0 && replayProvided && readBudgetProvided &&
		idempotentReplayBudget == 1 && authoritativeReadBudget == workspaceLaunchAuthoritativeReadBudget
	if operationID == "" || !exactFields ||
		!validVersion || launchVersion > int64(^uint(0)>>1) || authorizedStage == "" || authorizedStage != strings.TrimSpace(authorizedStage) ||
		reason == "" || reason != strings.TrimSpace(reason) || !validBudget || mutationBudget != 0 && mutationBudget != 1 ||
		replayProvided != readBudgetProvided || replayProvided && (idempotentReplayBudget != 0 && idempotentReplayBudget != 1 || !validAuthoritativeReadBudget) ||
		!validRuntimeImageRevision {
		writeError(w, http.StatusBadRequest, errInvalidBillingReview.Error())
		return
	}
	authorization := workspaceLaunchResumeAuthorization{
		AuthorizationID:	key, LaunchVersion: int(launchVersion), AuthorizedStage: contracts.Stage(authorizedStage),
		AuthorizedBy:	app.sessionUserID(r), Reason: reason, MutationBudget: int(mutationBudget),
		IdempotentReplayBudget:	int(idempotentReplayBudget), AuthoritativeReadBudget: int(authoritativeReadBudget),
		ReplacementWorkspaceImageDigest:	replacementWorkspaceImageDigest,
	}
	if resumeExistingRequested && (!replayProvided || !readBudgetProvided) {
		writeError(w, http.StatusBadRequest, errInvalidBillingReview.Error())
		return
	}
	if resumeExistingRequested {
		approval, configured := parseProductionAcceptanceBResumeExistingApproval()
		if !configured {
			writeError(w, http.StatusConflict, errWorkspaceLaunchGrantConflict.Error())
			return
		}
		var bindErr error
		authorization, bindErr = app.bindProductionAcceptanceBResumeExisting(r.Context(), service, r.Header, operationID, approval, authorization)
		if bindErr != nil {
			if errors.Is(bindErr, errBillingReviewNotFound) {
				writeError(w, http.StatusNotFound, "workspace_launch_not_found")
			} else {
				writeError(w, http.StatusConflict, errWorkspaceLaunchGrantConflict.Error())
			}
			return
		}
	}
	operation, err := app.resumeWorkspaceLaunch(r.Context(), service, operationID, authorization)
	if err != nil {
		if errors.Is(err, errBillingReviewNotFound) {
			writeError(w, http.StatusNotFound, "workspace_launch_not_found")
		} else if errors.Is(err, errWorkspaceLaunchGrantConflict) || errors.Is(err, errWorkspaceLaunchCASConflict) || errors.Is(err, errInvalidWorkspaceLaunchOperation) {
			writeError(w, http.StatusConflict, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, "state_persist_failed")
		}
		return
	}
	body, err := workspaceLaunchReconcileResponse(operation, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if readback, found := workspaceLaunchResumeAuthorizationReadback(operation, key); found {
		body["resumeAuthorizationReadback"] = readback
	}
	writeJSON(w, http.StatusOK, body)
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `authoritativeReadBudget` | `map value; validator in handler` | body input access |
| `authorizedStage` | `map value; validator in handler` | body input access |
| `idempotentReplayBudget` | `map value; validator in handler` | body input access |
| `mutationBudget` | `map value; validator in handler` | body input access |
| `operationId` | `string` | path |
| `reason` | `map value; validator in handler` | body input access |
| `replacementWorkspaceImageDigest` | `map value; validator in handler` | body input access |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, body)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/control-plane/internal/clients/fabric.go:96`; `Error` @ `services/control-plane/internal/clients/sub2api.go:432`; `Error` @ `services/control-plane/internal/clients/sub2api.go:446`; `Error` @ `services/control-plane/internal/clients/workspace_registry.go:84`; `stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `positiveIntegerField` @ `services/control-plane/internal/server/auth_accounts.go:103`; `sessionUserID` @ `services/control-plane/internal/server/auth_accounts.go:427`; `validBillingReviewOpaqueID` @ `services/control-plane/internal/server/routes_admin.go:1545`; `exactWorkspaceComputeClaimKeys` @ `services/control-plane/internal/server/routes_admin.go:663`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `decodeJSON` @ `services/control-plane/internal/server/server.go:446`; `requiredMutationKey` @ `services/control-plane/internal/server/server.go:486`; `workspaceLaunchReconcileResponse` @ `services/control-plane/internal/server/workspace_launch.go:143`; `workspaceLaunchResumeAuthorizationReadback` @ `services/control-plane/internal/server/workspace_launch.go:192`; `parseProductionAcceptanceBResumeExistingApproval` @ `services/control-plane/internal/server/workspace_launch_admission.go:229`; `productionAcceptanceBResumeExistingRequestMode` @ `services/control-plane/internal/server/workspace_launch_admission.go:494`; `Error` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:75`; `workspaceImageReferenceWithDigest` @ `services/control-plane/internal/server/workspace_launch_repair.go:180`; `resumeWorkspaceLaunch` @ `services/control-plane/internal/server/workspace_launch_service.go:103`; `bindProductionAcceptanceBResumeExisting` @ `services/control-plane/internal/server/workspace_launch_service.go:136`

### 87. POST /api/operator/workspaces/{workspaceId}/runtime-gateway-network/recover

注册：`services/control-plane/internal/server/workspace_runtime_gateway_network_recovery.go:33`；`registerWorkspaceRuntimeGatewayNetworkRecoveryRoutes`。

实际调用：`protected`, `recoverWorkspaceRuntimeGatewayNetwork`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	app.recoverWorkspaceRuntimeGatewayNetwork(w, r, service)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`protected` @ `services/control-plane/internal/server/server.go:277`; `recoverWorkspaceRuntimeGatewayNetwork` @ `services/control-plane/internal/server/workspace_runtime_gateway_network_recovery.go:38`

### 88. POST /api/operator/workspaces/{workspaceId}/runtime-image-replacements

注册：`services/control-plane/internal/server/workspace_runtime_image_replacement.go:49`；`registerWorkspaceRuntimeImageReplacementRoutes`。

实际调用：`createWorkspaceRuntimeImageReplacement`, `protected`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	app.createWorkspaceRuntimeImageReplacement(w, r, service)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`protected` @ `services/control-plane/internal/server/server.go:277`; `createWorkspaceRuntimeImageReplacement` @ `services/control-plane/internal/server/workspace_runtime_image_replacement.go:105`

### 89. POST /api/pricing/preview

注册：`services/control-plane/internal/server/routes_state.go:24`；`registerStateRoutes`。

实际调用：`Context`, `decodeJSON`, `fabricComputePools`, `limitJSONBody`, `pricingPreviewResponse`, `protected`, `scopedAccountID`, `writeJSON`, `writePricingError`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	if !limitJSONBody(w, r) {
		return
	}
	input := decodeJSON(r)
	_, ok := app.scopedAccountID(w, r, input)
	if !ok {
		return
	}
	computePools, ok := fabricComputePools(w, r, service)
	if !ok {
		return
	}
	preview, err := app.pricingPreviewResponse(r.Context(), input, computePools)
	if err != nil {
		writePricingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
})
```

</details>

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, preview)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`pricingPreviewResponse` @ `services/control-plane/internal/server/pricing.go:86`; `pricingPreviewResponse` @ `services/control-plane/internal/server/pricing.go:90`; `writePricingError` @ `services/control-plane/internal/server/routes_state.go:53`; `protected` @ `services/control-plane/internal/server/server.go:277`; `fabricComputePools` @ `services/control-plane/internal/server/server.go:326`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `limitJSONBody` @ `services/control-plane/internal/server/server.go:361`; `scopedAccountID` @ `services/control-plane/internal/server/server.go:428`; `decodeJSON` @ `services/control-plane/internal/server/server.go:446`

### 90. POST /api/workspace-launches

注册：`services/control-plane/internal/server/routes_workspace_launch.go:15`；`registerWorkspaceLaunchRoutes`。

实际调用：`Background`, `Context`, `Error`, `GetAccount`, `GetRuntimeOperation`, `Is`, `PreflightWorkspaceLaunch`, `Reconcile`, `Sub2APIBalance`, `TrimSpace`, `WorkspaceProvisioningMode`, `applyResourceBillingQuote`, `boolPtr`, `controlledBasicPilotAdmissionFromEnv`, `createWorkspaceLaunch`, `currentWorkspaceImageReleasePolicy`, `customerOwned`, `customerPricingPreviewDTO`, `decodeJSON`, `decodeWorkspaceDefaultApplication`, `decodeWorkspaceLaunchReconcileOperation`, `defaultOPLApplicationRevision`, `fabricComputePools`, `gatewayUserContext`, `int64`, `int64Fact`, `isWorkspaceLaunchAction`, `lockResource`, `newWorkspaceLaunchDescriptorWithImage`, `numberField`, `parseProductionAcceptanceBApproval`, `prepareDefaultWorkspaceApplication`, `pricingPreviewResponse`, `productionAcceptanceBLaunchApproved`, `protected`, `providerPackageStorageGB`, `queryRuntimeOperations`, `reconciliationBlocksNewWorkspaces`, `rejectNewLaunch`, `requiredMutationKey`, `resourceBillingEnabled`, `respondWorkspaceLaunchContinuation`, `runWorkspaceDefaultApplication`, `runWorkspaceLaunch`, `scopedAccountID`, `sessionUserContext`, `string`, `stringValue`, `unlock`, `unlockAccount`, `workspaceCodexGroupID`, `workspaceDefaultApplicationOperationID`, `workspaceDefaultApplicationRow`, `workspaceLaunchOperationID`, `workspaceLaunchPreflightConfirmed`, `workspaceLaunchReconcileRequestMatches`, `workspaceLaunchReconciler`, `workspaceLaunchResponse`, `workspaceLaunchWorkerEnabled`, `workspacePurchaseEnabled`, `writeError`, `writeJSON`, `writePricingError`, `writeUpstreamError`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	input := decodeJSON(r)
	key, ok := requiredMutationKey(w, r)
	if !ok {
		return
	}
	accountID, ok := app.scopedAccountID(w, r, input)
	if !ok {
		return
	}
	user, ok := app.sessionUserContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "not_authenticated")
		return
	}
	name, validName := input["name"].(string)
	packageID, validPackage := input["packageId"].(string)
	name, packageID = strings.TrimSpace(name), strings.TrimSpace(packageID)
	autoRenew, validAutoRenew := input["autoRenew"].(bool)
	if !validName || !validPackage || name == "" || packageID == "" || !validAutoRenew {
		writeError(w, http.StatusBadRequest, "invalid_pricing_input")
		return
	}
	provisioningMode := ""
	if rawMode, supplied := input["provisioningMode"]; supplied {
		mode, isText := rawMode.(string)
		if !isText || mode != "" && mode != string(contracts.WorkspaceProvisioningFull) && mode != string(contracts.WorkspaceProvisioningResourceOnly) {
			writeError(w, http.StatusBadRequest, "invalid_pricing_input")
			return
		}
		if mode == string(contracts.WorkspaceProvisioningResourceOnly) {
			provisioningMode = mode
		}
	}
	resourceOnly := provisioningMode == string(contracts.WorkspaceProvisioningResourceOnly)
	if _, supplied := input["sizeGb"]; supplied {
		writeError(w, http.StatusBadRequest, "invalid_pricing_input")
		return
	}
	if _, supplied := input["priceVersion"]; supplied {
		writeError(w, http.StatusBadRequest, "client_pricing_forbidden")
		return
	}
	if _, supplied := input["totalChargeUsdMicros"]; supplied {
		writeError(w, http.StatusBadRequest, "client_pricing_forbidden")
		return
	}
	ownerUserID := stringValue(user["id"])
	account, found, err := app.tables.GetAccount(r.Context(), accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if !found || stringValue(account["status"]) != "active" || stringValue(account["ownerUserId"]) != ownerUserID {
		writeError(w, http.StatusForbidden, "account_scope_forbidden")
		return
	}
	if !workspacePurchaseEnabled(account) {
		writeError(w, http.StatusConflict, "workspace_purchase_not_enabled")
		return
	}

	unlock := app.lockResource("workspace-launch", accountID)
	defer unlock()
	operationID := workspaceLaunchOperationID(accountID, key)
	row, found, err := app.tables.GetRuntimeOperation(r.Context(), operationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if found {
		persisted, decodeErr := decodeWorkspaceLaunchReconcileOperation(row)
		matchingMode := contracts.WorkspaceProvisioningMode(provisioningMode)
		defaultRow, defaultFound, defaultErr := app.tables.GetRuntimeOperation(r.Context(), workspaceDefaultApplicationOperationID(operationID))
		if defaultErr != nil {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		if defaultFound {
			defaultRequest, err := decodeWorkspaceDefaultApplication(defaultRow)
			if err != nil || resourceOnly || defaultRequest.AccountID != accountID {
				writeError(w, http.StatusConflict, errIdempotencyConflict.Error())
				return
			}
			matchingMode = contracts.WorkspaceProvisioningResourceOnly
		}

		if decodeErr != nil || !workspaceLaunchReconcileRequestMatches(persisted, accountID, ownerUserID, name, packageID, autoRenew, matchingMode) {
			writeError(w, http.StatusConflict, errIdempotencyConflict.Error())
			return
		}
		if (persisted.Status == contracts.StatusPending && persisted.Stage == contracts.StageKey) || (app.deployment.customerOwned() && persisted.Status == contracts.StatusManualReview && (persisted.Stage == contracts.StageKey || persisted.Stage == contracts.StageSecret) && persisted.Observations[persisted.Stage].State == workspaceLaunchStageUnknown) {
			credentialUser, sub2APIUserID, credential, credentialOK := app.gatewayUserContext(w, r)
			if !credentialOK {
				return
			}
			if stringValue(credentialUser["accountId"]) != accountID || sub2APIUserID != persisted.int64Fact("sub2apiUserId") {
				writeError(w, http.StatusForbidden, "account_scope_forbidden")
				return
			}
			continued, reconcileErr := app.workspaceLaunchReconciler(service, credential, sub2APIUserID).Reconcile(r.Context(), persisted.ID)
			if reconcileErr != nil {
				writeError(w, http.StatusInternalServerError, "state_persist_failed")
				return
			}
			persisted = continued
		}
		if defaultFound {
			_, sub2APIUserID, credential, ok := app.gatewayUserContext(w, r)
			if !ok {
				return
			}
			_ = app.prepareDefaultWorkspaceApplication(r.Context(), service, operationID, credential, sub2APIUserID)
			_ = app.runWorkspaceDefaultApplication(r.Context(), service, workspaceDefaultApplicationOperationID(operationID))
		}
		app.respondWorkspaceLaunchContinuation(w, r, persisted)
		return
	}
	if autoRenew && !app.deployment.resourceBillingEnabled() {
		writeError(w, http.StatusConflict, "autoRenew_unavailable")
		return
	}
	active, err := queryRuntimeOperations(r.Context(), app.tables, runtimeOperationQuery{
		AccountID:	accountID, ExcludedStatuses: []string{"succeeded", "refunded", "failed"},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	for _, candidate := range active {
		if isWorkspaceLaunchAction(stringValue(candidate["action"])) {
			writeError(w, http.StatusConflict, errWorkspaceLaunchInProgress.Error())
			return
		}
	}
	if _, blocked, err := app.reconciliationBlocksNewWorkspaces(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	} else if blocked {
		writeError(w, http.StatusConflict, "billing_reconciliation_blocked")
		return
	}
	computePools, ok := fabricComputePools(w, r, service)
	if !ok {
		return
	}
	storageGB, packageAvailable := providerPackageStorageGB(computePools, packageID)
	if !packageAvailable {
		writePricingError(w, errPackageUnavailable)
		return
	}

	admission := controlledBasicPilotAdmissionFromEnv()
	code := ""
	if !app.deployment.customerOwned() {

		code = admission.rejectNewLaunch(false)
	}
	acceptanceBApproved := false
	if code == "workspace_launch_admission_disabled" {
		approval, configured := parseProductionAcceptanceBApproval()
		if configured && productionAcceptanceBLaunchApproved(r.Header, approval, accountID, stringValue(user["email"]), name, packageID, storageGB, autoRenew, key) {
			code, acceptanceBApproved = "", true
		}
	}
	if code != "" {
		writeError(w, http.StatusConflict, code)
		return
	}
	quote, err := pricingPreviewResponse(map[string]any{"resourceType": "workspace", "packageId": packageID, "sizeGb": storageGB})
	if err != nil {
		writePricingError(w, err)
		return
	}
	quote = customerPricingPreviewDTO(quote)
	quote = app.applyResourceBillingQuote(quote)
	imageDigest := ""
	if !resourceOnly {
		imagePolicy, _, _, policyErr := app.currentWorkspaceImageReleasePolicy(r.Context())
		if policyErr != nil {
			writeError(w, http.StatusServiceUnavailable, errWorkspaceImageReleasePolicyUnavailable.Error())
			return
		}
		imageDigest = imagePolicy.ActiveImage
	}
	descriptor, err := newWorkspaceLaunchDescriptorWithImage(accountID, ownerUserID, name, packageID, storageGB, autoRenew, stringValue(quote["priceVersion"]), key, "", contracts.WorkspaceProvisioningResourceOnly)
	if err != nil {
		writeError(w, http.StatusConflict, "workspace_image_digest_invalid")
		return
	}
	preflightInput := clients.WorkspaceLaunchPreflightInput{
		SchemaVersion:	clients.WorkspaceLaunchFabricSchemaVersion, LaunchOperationID: descriptor.OperationID,
		AccountID:	accountID, WorkspaceID: descriptor.WorkspaceID, PackageID: packageID, SizeGB: storageGB,
		WorkspaceImageDigest:	"", ProvisioningMode: string(contracts.WorkspaceProvisioningResourceOnly), RequestHash: descriptor.RequestHash,
	}
	preflight, err := service.PreflightWorkspaceLaunch(r.Context(), preflightInput)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	if !workspaceLaunchPreflightConfirmed(preflightInput, preflight) {
		writeError(w, http.StatusBadGateway, "fabric_workspace_launch_preflight_invalid")
		return
	}

	unlockAccount := app.lockResource("account", accountID)
	defer unlockAccount()
	credentialUser, sub2APIUserID, credential, ok := app.gatewayUserContext(w, r)
	if !ok {
		return
	}
	if stringValue(credentialUser["accountId"]) != accountID {
		writeError(w, http.StatusForbidden, "account_scope_forbidden")
		return
	}
	workspaceKeyGroupID := int64(0)
	if !resourceOnly {
		workspaceKeyGroupID, err = workspaceCodexGroupID(r.Context(), service, credential, sub2APIUserID)
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
	}
	totalCharge := int64(numberField(quote, "totalChargeUsdMicros", 0))
	preChargeBalance := int64(0)
	if app.deployment.resourceBillingEnabled() {
		balance, balanceErr := service.Sub2APIBalance(r.Context(), sub2APIUserID)
		if balanceErr != nil {
			writeUpstreamError(w, balanceErr)
			return
		}
		if balance.USDMicros < totalCharge {
			writeError(w, http.StatusConflict, errMonthlyInsufficientBalance.Error())
			return
		}
		preChargeBalance = balance.USDMicros
	}
	var defaultOperation map[string]any
	if !resourceOnly {
		defaultOperation, err = workspaceDefaultApplicationRow(workspaceDefaultApplicationRequest{SchemaVersion: 1, OperationID: workspaceDefaultApplicationOperationID(descriptor.OperationID), LaunchOperationID: descriptor.OperationID, AccountID: accountID, WorkspaceID: descriptor.WorkspaceID, OwnerUserID: ownerUserID, Sub2APIUserID: sub2APIUserID, WorkspaceKeyGroupID: workspaceKeyGroupID, Revision: defaultOPLApplicationRevision(imageDigest), Phase: "credentials_required"})
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
	}
	created, err := app.createWorkspaceLaunch(r.Context(), service, credential, sub2APIUserID, workspaceLaunchReconcileCreate{
		Mode:	contracts.WorkspaceProvisioningResourceOnly, DefaultApplicationOperation: defaultOperation,
		OperationID:	descriptor.OperationID, RequestHash: descriptor.RequestHash, AccountID: accountID, OwnerUserID: ownerUserID,
		Sub2APIUserID:	sub2APIUserID, WorkspaceID: descriptor.WorkspaceID,
		Name:	name, PackageID: packageID, StorageGB: storageGB, AutoRenew: autoRenew,
		PriceVersion:	stringValue(quote["priceVersion"]), TotalChargeUSDMicros: totalCharge,
		ProviderProfileRef:	preflight.ProviderProfileRef, PreflightBindingRef: preflight.BindingRef, SpecDigest: preflight.SpecDigest,
		WorkspaceImageDigest:	descriptor.WorkspaceImageDigest, PreChargeBalanceMicros: preChargeBalance, ResourceBillingEnabled: boolPtr(app.deployment.resourceBillingEnabled()),
		AcceptanceBCapacitySlot:	acceptanceBApproved,
	})
	if err != nil {
		switch {
		case errors.Is(err, errBillingReconciliationBlocked):
			writeError(w, http.StatusConflict, errBillingReconciliationBlocked.Error())
		case errors.Is(err, errWorkspaceLaunchCapacityReached):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, errWorkspaceLaunchCASConflict), errors.Is(err, errWorkspaceLaunchInProgress):
			writeError(w, http.StatusConflict, errIdempotencyConflict.Error())
		default:
			writeError(w, http.StatusInternalServerError, "state_persist_failed")
		}
		return
	}
	if defaultOperation != nil {
		_ = app.prepareDefaultWorkspaceApplication(r.Context(), service, created.ID, credential, sub2APIUserID)
		_ = app.runWorkspaceDefaultApplication(r.Context(), service, workspaceDefaultApplicationOperationID(created.ID))
	}
	persistedRow, found, err := app.tables.GetRuntimeOperation(r.Context(), created.ID)
	if err != nil || !found {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	body, err := workspaceLaunchResponse(persistedRow)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if workspaceLaunchWorkerEnabled() && created.Status == contracts.StatusPending {
		go func() {
			unlock := app.lockResource("workspace-launch", accountID)
			defer unlock()
			_ = app.runWorkspaceLaunch(context.Background(), service, created.ID)
		}()
	}
	writeJSON(w, http.StatusAccepted, body)
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `autoRenew` | `map value; validator in handler` | body input access |
| `name` | `map value; validator in handler` | body input access |
| `packageId` | `map value; validator in handler` | body input access |
| `priceVersion` | `map value; validator in handler` | body input access |
| `provisioningMode` | `map value; validator in handler` | body input access |
| `sizeGb` | `map value; validator in handler` | body input access |
| `totalChargeUsdMicros` | `map value; validator in handler` | body input access |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusAccepted, body)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/control-plane/internal/clients/fabric.go:96`; `PreflightWorkspaceLaunch` @ `services/control-plane/internal/clients/fabric_workspace_launch.go:157`; `Error` @ `services/control-plane/internal/clients/sub2api.go:432`; `Error` @ `services/control-plane/internal/clients/sub2api.go:446`; `Error` @ `services/control-plane/internal/clients/workspace_registry.go:84`; `Sub2APIBalance` @ `services/control-plane/internal/controlplane/monthly_billing.go:22`; `PreflightWorkspaceLaunch` @ `services/control-plane/internal/controlplane/workspace_launch.go:12`; `lockResource` @ `services/control-plane/internal/server/app_state.go:42`; `stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `sessionUserContext` @ `services/control-plane/internal/server/auth_accounts.go:435`; `reconciliationBlocksNewWorkspaces` @ `services/control-plane/internal/server/billing_projection.go:397`; `customerOwned` @ `services/control-plane/internal/server/deployment_profile.go:57`; `resourceBillingEnabled` @ `services/control-plane/internal/server/deployment_profile.go:58`; `boolPtr` @ `services/control-plane/internal/server/deployment_profile.go:60`; `GetRuntimeOperation` @ `services/control-plane/internal/server/ent_state_store.go:694`; `GetAccount` @ `services/control-plane/internal/server/ent_state_store_identity.go:152`; `applyResourceBillingQuote` @ `services/control-plane/internal/server/pricing.go:133`; `providerPackageStorageGB` @ `services/control-plane/internal/server/pricing.go:298`; `customerPricingPreviewDTO` @ `services/control-plane/internal/server/pricing.go:323`; `pricingPreviewResponse` @ `services/control-plane/internal/server/pricing.go:86`; `pricingPreviewResponse` @ `services/control-plane/internal/server/pricing.go:90`; `gatewayUserContext` @ `services/control-plane/internal/server/routes_gateway.go:795`; `writePricingError` @ `services/control-plane/internal/server/routes_state.go:53`; `respondWorkspaceLaunchContinuation` @ `services/control-plane/internal/server/routes_workspace_launch.go:400`; `protected` @ `services/control-plane/internal/server/server.go:277`; `fabricComputePools` @ `services/control-plane/internal/server/server.go:326`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `writeUpstreamError` @ `services/control-plane/internal/server/server.go:345`; `scopedAccountID` @ `services/control-plane/internal/server/server.go:428`; `decodeJSON` @ `services/control-plane/internal/server/server.go:446`; `numberField` @ `services/control-plane/internal/server/server.go:461`; `requiredMutationKey` @ `services/control-plane/internal/server/server.go:486`; `queryRuntimeOperations` @ `services/control-plane/internal/server/table_store.go:87`; `prepareDefaultWorkspaceApplication` @ `services/control-plane/internal/server/workspace_default_application.go:101`; `runWorkspaceDefaultApplication` @ `services/control-plane/internal/server/workspace_default_application.go:163`; `workspaceDefaultApplicationOperationID` @ `services/control-plane/internal/server/workspace_default_application.go:38`; `defaultOPLApplicationRevision` @ `services/control-plane/internal/server/workspace_default_application.go:42`; `workspaceDefaultApplicationRow` @ `services/control-plane/internal/server/workspace_default_application.go:58`; `decodeWorkspaceDefaultApplication` @ `services/control-plane/internal/server/workspace_default_application.go:79`; `workspaceCodexGroupID` @ `services/control-plane/internal/server/workspace_gateway.go:874`; `currentWorkspaceImageReleasePolicy` @ `services/control-plane/internal/server/workspace_image_release_policy.go:115`; `isWorkspaceLaunchAction` @ `services/control-plane/internal/server/workspace_launch.go:109`; `workspaceLaunchResponse` @ `services/control-plane/internal/server/workspace_launch.go:135`; `workspaceLaunchReconcileRequestMatches` @ `services/control-plane/internal/server/workspace_launch.go:228`; `workspaceLaunchPreflightConfirmed` @ `services/control-plane/internal/server/workspace_launch.go:237`; `newWorkspaceLaunchDescriptorWithImage` @ `services/control-plane/internal/server/workspace_launch.go:44`; `workspaceLaunchOperationID` @ `services/control-plane/internal/server/workspace_launch.go:96`; `controlledBasicPilotAdmissionFromEnv` @ `services/control-plane/internal/server/workspace_launch_admission.go:161`; `rejectNewLaunch` @ `services/control-plane/internal/server/workspace_launch_admission.go:185`; `workspacePurchaseEnabled` @ `services/control-plane/internal/server/workspace_launch_admission.go:198`; `parseProductionAcceptanceBApproval` @ `services/control-plane/internal/server/workspace_launch_admission.go:214`; `productionAcceptanceBLaunchApproved` @ `services/control-plane/internal/server/workspace_launch_admission.go:591`; `decodeWorkspaceLaunchReconcileOperation` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:2250`; `int64Fact` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:2833`; `Reconcile` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:506`; `Error` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:75`; `runWorkspaceLaunch` @ `services/control-plane/internal/server/workspace_launch_service.go:225`; `workspaceLaunchReconciler` @ `services/control-plane/internal/server/workspace_launch_service.go:93`; `createWorkspaceLaunch` @ `services/control-plane/internal/server/workspace_launch_service.go:99`; `workspaceLaunchWorkerEnabled` @ `services/control-plane/internal/server/workspace_launch_worker.go:130`

### 91. POST /api/workspace-launches/{id}/resume

注册：`services/control-plane/internal/server/routes_workspace_launch.go:353`；`registerWorkspaceLaunchRoutes`。

实际调用：`Context`, `GetRuntimeOperation`, `PathValue`, `Reconcile`, `customerOwned`, `decodeWorkspaceLaunchReconcileOperation`, `gatewayUserContext`, `int64Fact`, `lockResource`, `protected`, `requiredMutationKey`, `respondWorkspaceLaunchContinuation`, `scopedAccountID`, `stringValue`, `unlock`, `workspaceLaunchReconciler`, `writeError`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	if _, ok := requiredMutationKey(w, r); !ok {
		return
	}
	if !app.deployment.customerOwned() {
		writeError(w, http.StatusNotFound, "workspace_launch_not_found")
		return
	}
	accountID, ok := app.scopedAccountID(w, r, nil)
	if !ok {
		return
	}
	unlock := app.lockResource("workspace-launch", accountID)
	defer unlock()
	row, found, err := app.tables.GetRuntimeOperation(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if !found || stringValue(row["accountId"]) != accountID || stringValue(row["action"]) != workspaceLaunchAction {
		writeError(w, http.StatusNotFound, "workspace_launch_not_found")
		return
	}
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	recoverableManualReview := operation.Status == contracts.StatusManualReview && (operation.Stage == contracts.StageKey || operation.Stage == contracts.StageStorage || operation.Stage == contracts.StageAttachment || operation.Stage == contracts.StageSecret || operation.Stage == contracts.StageRuntime || operation.Stage == contracts.StageActivation) && operation.Observations[operation.Stage].State == workspaceLaunchStageUnknown
	recoverablePendingStage := operation.Status == contracts.StatusPending && (operation.Stage == contracts.StageKey || operation.Stage == contracts.StageStorage || operation.Stage == contracts.StageAttachment || operation.Stage == contracts.StageSecret || operation.Stage == contracts.StageRuntime)
	if err != nil || !recoverableManualReview && !recoverablePendingStage {
		writeError(w, http.StatusConflict, "workspace_launch_not_recoverable")
		return
	}
	_, sub2APIUserID, credential, ok := app.gatewayUserContext(w, r)
	if !ok {
		return
	}
	if sub2APIUserID != operation.int64Fact("sub2apiUserId") {
		writeError(w, http.StatusForbidden, "account_scope_forbidden")
		return
	}
	continued, err := app.workspaceLaunchReconciler(service, credential, sub2APIUserID).Reconcile(r.Context(), operation.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_persist_failed")
		return
	}
	app.respondWorkspaceLaunchContinuation(w, r, continued)
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `id` | `string` | path |

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`lockResource` @ `services/control-plane/internal/server/app_state.go:42`; `stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `customerOwned` @ `services/control-plane/internal/server/deployment_profile.go:57`; `GetRuntimeOperation` @ `services/control-plane/internal/server/ent_state_store.go:694`; `gatewayUserContext` @ `services/control-plane/internal/server/routes_gateway.go:795`; `respondWorkspaceLaunchContinuation` @ `services/control-plane/internal/server/routes_workspace_launch.go:400`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `scopedAccountID` @ `services/control-plane/internal/server/server.go:428`; `requiredMutationKey` @ `services/control-plane/internal/server/server.go:486`; `decodeWorkspaceLaunchReconcileOperation` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:2250`; `int64Fact` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:2833`; `Reconcile` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:506`; `workspaceLaunchReconciler` @ `services/control-plane/internal/server/workspace_launch_service.go:93`

### 92. POST /api/workspaces/{workspaceID}/application-installation/resume

注册：`services/control-plane/internal/server/workspace_application_recovery.go:163`；`registerWorkspaceApplicationRecoveryRoutes`。

实际调用：`Context`, `GetRuntimeOperation`, `GetWorkspace`, `Now`, `PathValue`, `ResumeWorkspaceApplicationDeployment`, `decodeWorkspaceDefaultApplication`, `decodeWorkspaceLaunchReconcileOperation`, `firstNonEmpty`, `gatewayUserContext`, `len`, `persistWorkspaceDefaultApplication`, `prepareDefaultWorkspaceApplication`, `projectWorkspaceApplicationInstallation`, `protected`, `queryRuntimeOperations`, `requiredMutationKey`, `runWorkspaceDefaultApplication`, `scopedAccountID`, `sessionUserContext`, `stringFact`, `stringValue`, `workspaceApplicationEntitlementOpen`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	if _, ok := requiredMutationKey(w, r); !ok {
		return
	}
	accountID, ok := app.scopedAccountID(w, r, nil)
	if !ok {
		return
	}
	workspaceID := r.PathValue("workspaceID")
	current, found, err := app.tables.GetWorkspace(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if !found || firstNonEmpty(stringValue(current["accountId"]), stringValue(current["ownerAccountId"])) != accountID {
		writeError(w, http.StatusNotFound, "workspace_not_found")
		return
	}
	rows, err := queryRuntimeOperations(r.Context(), app.tables, runtimeOperationQuery{WorkspaceID: workspaceID, Action: workspaceDefaultApplicationAction})
	if err != nil || len(rows) != 1 {
		writeError(w, http.StatusConflict, "workspace_default_application_unavailable")
		return
	}
	row := rows[0]
	request, err := decodeWorkspaceDefaultApplication(row)
	user, authenticated := app.sessionUserContext(r)
	if err != nil || !authenticated || request.AccountID != accountID || request.WorkspaceID != workspaceID || request.OwnerUserID != stringValue(user["id"]) {
		writeError(w, http.StatusForbidden, "account_scope_forbidden")
		return
	}
	if request.Phase != "deployed" {
		launchRow, found, err := app.tables.GetRuntimeOperation(r.Context(), request.LaunchOperationID)
		launch, decodeErr := decodeWorkspaceLaunchReconcileOperation(launchRow)
		if err != nil || !found || decodeErr != nil || launch.Status != contracts.StatusSucceeded || launch.stringFact("workspaceId") != workspaceID || launch.stringFact("accountId") != accountID || !workspaceApplicationEntitlementOpen(current, time.Now()) {
			writeError(w, http.StatusConflict, "workspace_application_recovery_conflict")
			return
		}
		if request.DeploymentID != "" {
			if _, err := app.tables.ResumeWorkspaceApplicationDeployment(r.Context(), request.DeploymentID); err != nil {
				writeError(w, http.StatusConflict, "workspace_application_recovery_conflict")
				return
			}
			request.Phase, request.LastError = "installing", ""
		} else {
			request.LastError = ""
			if request.Phase != "preparing_credentials" {
				request.Phase = "credentials_required"
			}
			if request.GatewaySecret != nil {
				request.Phase = "waiting_resources"
			}
		}
		if err := app.persistWorkspaceDefaultApplication(r.Context(), row, request); err != nil {
			writeError(w, http.StatusConflict, "workspace_application_recovery_conflict")
			return
		}
		if request.GatewaySecret == nil {
			_, userID, credential, ok := app.gatewayUserContext(w, r)
			if !ok {
				return
			}
			if err := app.prepareDefaultWorkspaceApplication(r.Context(), service, request.LaunchOperationID, credential, userID); err != nil {
				writeError(w, http.StatusConflict, "workspace_default_application_credentials_unconfirmed")
				return
			}
		}
		_ = app.runWorkspaceDefaultApplication(r.Context(), service, request.OperationID)
	}
	current, found, err = app.tables.GetWorkspace(r.Context(), workspaceID)
	projection := map[string]any{"workspaceId": workspaceID, "applicationInstallation": nil}
	if err != nil || !found || app.projectWorkspaceApplicationInstallation(r.Context(), current, projection) != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	writeJSON(w, http.StatusAccepted, projection)
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `workspaceID` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusAccepted, projection)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `firstNonEmpty` @ `services/control-plane/internal/server/app_state.go:552`; `sessionUserContext` @ `services/control-plane/internal/server/auth_accounts.go:435`; `GetRuntimeOperation` @ `services/control-plane/internal/server/ent_state_store.go:694`; `GetWorkspace` @ `services/control-plane/internal/server/ent_state_store_workspace.go:456`; `gatewayUserContext` @ `services/control-plane/internal/server/routes_gateway.go:795`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `scopedAccountID` @ `services/control-plane/internal/server/server.go:428`; `requiredMutationKey` @ `services/control-plane/internal/server/server.go:486`; `queryRuntimeOperations` @ `services/control-plane/internal/server/table_store.go:87`; `projectWorkspaceApplicationInstallation` @ `services/control-plane/internal/server/workspace_application_access.go:37`; `ResumeWorkspaceApplicationDeployment` @ `services/control-plane/internal/server/workspace_application_recovery.go:100`; `workspaceApplicationEntitlementOpen` @ `services/control-plane/internal/server/workspace_application_selection.go:24`; `prepareDefaultWorkspaceApplication` @ `services/control-plane/internal/server/workspace_default_application.go:101`; `runWorkspaceDefaultApplication` @ `services/control-plane/internal/server/workspace_default_application.go:163`; `decodeWorkspaceDefaultApplication` @ `services/control-plane/internal/server/workspace_default_application.go:79`; `persistWorkspaceDefaultApplication` @ `services/control-plane/internal/server/workspace_default_application.go:88`; `decodeWorkspaceLaunchReconcileOperation` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:2250`; `stringFact` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:2821`

### 93. POST /api/workspaces/{workspaceId}/auto-renew

注册：`services/control-plane/internal/server/routes_workspace.go:275`；`registerWorkspaceRoutes`。

实际调用：`ApplyWorkspaceRenewalIntent`, `Before`, `Context`, `Error`, `GetRuntimeOperation`, `Is`, `Now`, `Parse`, `PathValue`, `UTC`, `append`, `auditEvent`, `bindWorkspaceAutoRenewAudit`, `canAccessResource`, `decodeJSON`, `decodeWorkspaceAutoRenewCommand`, `firstNonEmpty`, `getWorkspace`, `planWorkspaceRenewalIntent`, `protected`, `queryRuntimeOperations`, `requiredMutationKey`, `sessionUserContext`, `stringValue`, `workspaceAutoRenewCommandID`, `workspaceAutoRenewRequestHash`, `workspaceAutoRenewResponse`, `workspaceRenewalIntentState`, `workspaceRenewalOperationID`, `workspaceRenewalRecoveryState`, `writeError`, `writeJSON`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	input := decodeJSON(r)
	key, ok := requiredMutationKey(w, r)
	if !ok {
		return
	}
	autoRenew, ok := input["autoRenew"].(bool)
	if !ok {
		writeError(w, http.StatusBadRequest, "autoRenew_required")
		return
	}
	workspaceID := r.PathValue("workspaceId")
	workspace, ok := app.getWorkspace(workspaceID)
	if !ok {
		writeError(w, http.StatusNotFound, "workspace_not_found")
		return
	}
	if !app.canAccessResource(r, workspace) {
		writeError(w, http.StatusForbidden, "account_scope_forbidden")
		return
	}
	user, ok := app.sessionUserContext(r)
	if !ok || firstNonEmpty(stringValue(workspace["ownerUserId"]), stringValue(workspace["ownerId"])) != stringValue(user["id"]) {
		writeError(w, http.StatusForbidden, "workspace_owner_required")
		return
	}
	if autoRenew && workspace["resourceBillingEnabled"] == false {
		writeError(w, http.StatusConflict, "autoRenew_unavailable")
		return
	}
	operationID := workspaceAutoRenewCommandID(workspaceID, key)
	requestHash := workspaceAutoRenewRequestHash(workspaceID, autoRenew)
	for range 3 {
		workspace, ok = app.getWorkspace(workspaceID)
		if !ok || !app.canAccessResource(r, workspace) {
			writeError(w, http.StatusForbidden, "account_scope_forbidden")
			return
		}
		command, found, err := app.tables.GetRuntimeOperation(r.Context(), operationID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		if found {
			result, err := decodeWorkspaceAutoRenewCommand(command)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "state_read_failed")
				return
			}
			if result.RequestHash != requestHash {
				writeError(w, http.StatusConflict, errIdempotencyConflict.Error())
				return
			}
			writeJSON(w, http.StatusOK, result.Response)
			return
		}
		paidThrough, paidThroughErr := time.Parse(time.RFC3339, stringValue(workspace["paidThrough"]))
		expired := paidThroughErr == nil && !time.Now().UTC().Before(paidThrough)
		if workspace["autoRenew"] == autoRenew && !expired {
			paidThrough, parseErr := time.Parse(time.RFC3339, stringValue(workspace["paidThrough"]))
			if parseErr != nil {
				writeError(w, http.StatusConflict, "workspace_billing_state_invalid")
				return
			}
			operation, found, queryErr := app.tables.GetRuntimeOperation(r.Context(), workspaceRenewalOperationID(workspaceID, paidThrough))
			if queryErr != nil {
				writeError(w, http.StatusInternalServerError, "state_read_failed")
				return
			}
			operations := []map[string]any(nil)
			if found {
				operations = append(operations, operation)
			}
			response, responseErr := workspaceAutoRenewResponse(workspace, operations, autoRenew, time.Now().UTC())
			if responseErr != nil {
				writeError(w, http.StatusConflict, "workspace_billing_state_invalid")
				return
			}
			writeJSON(w, http.StatusOK, response)
			return
		}
		operations, err := queryRuntimeOperations(r.Context(), app.tables, runtimeOperationQuery{WorkspaceID: workspaceID})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		if expired && autoRenew {
			recovery := app.workspaceRenewalRecoveryState(r.Context(), service, workspace, operations, time.Now().UTC())
			if recovery.State != "recoverable" && recovery.State != "pending" {
				writeError(w, http.StatusConflict, recovery.Reason)
				return
			}
		}
		update, response, err := planWorkspaceRenewalIntent(workspace, user, operations, autoRenew, key, time.Now().UTC())
		if err != nil {
			writeError(w, http.StatusConflict, "workspace_billing_state_invalid")
			return
		}
		before := workspaceRenewalIntentState(workspace["autoRenew"] == true, stringValue(workspace["authorizedBy"]), stringValue(workspace["authorizedAt"]))
		after := workspaceRenewalIntentState(update.WorkspacePatch.AutoRenew, update.WorkspacePatch.AuthorizedBy, update.WorkspacePatch.AuthorizedAt)
		update.AuditEvent = bindWorkspaceAutoRenewAudit(update.CommandOperation, app.auditEvent(r, "workspace.auto_renew", "workspace", workspaceID, stringValue(workspace["accountId"]), before, after, "succeeded"))
		if err := app.tables.ApplyWorkspaceRenewalIntent(r.Context(), update); errors.Is(err, errWorkspaceRenewalCASConflict) {
			continue
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "state_persist_failed")
			return
		}
		writeJSON(w, http.StatusOK, response)
		return
	}
	writeError(w, http.StatusConflict, errWorkspaceRenewalCASConflict.Error())
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `autoRenew` | `map value; validator in handler` | body input access |
| `workspaceId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, response)`
- `writeJSON(w, http.StatusOK, result.Response)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`Error` @ `services/control-plane/internal/clients/fabric.go:96`; `Error` @ `services/control-plane/internal/clients/sub2api.go:432`; `Error` @ `services/control-plane/internal/clients/sub2api.go:446`; `Error` @ `services/control-plane/internal/clients/workspace_registry.go:84`; `auditEvent` @ `services/control-plane/internal/server/admin_ops.go:105`; `stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `firstNonEmpty` @ `services/control-plane/internal/server/app_state.go:552`; `sessionUserContext` @ `services/control-plane/internal/server/auth_accounts.go:435`; `GetRuntimeOperation` @ `services/control-plane/internal/server/ent_state_store.go:694`; `ApplyWorkspaceRenewalIntent` @ `services/control-plane/internal/server/ent_state_store_workspace.go:625`; `canAccessResource` @ `services/control-plane/internal/server/resource_facts.go:138`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `decodeJSON` @ `services/control-plane/internal/server/server.go:446`; `requiredMutationKey` @ `services/control-plane/internal/server/server.go:486`; `queryRuntimeOperations` @ `services/control-plane/internal/server/table_store.go:87`; `getWorkspace` @ `services/control-plane/internal/server/workspace_gateway.go:308`; `Error` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:75`; `decodeWorkspaceAutoRenewCommand` @ `services/control-plane/internal/server/workspace_renewal.go:109`; `workspaceRenewalRecoveryState` @ `services/control-plane/internal/server/workspace_renewal.go:1484`; `workspaceAutoRenewResponse` @ `services/control-plane/internal/server/workspace_renewal.go:156`; `planWorkspaceRenewalIntent` @ `services/control-plane/internal/server/workspace_renewal.go:188`; `workspaceRenewalOperationID` @ `services/control-plane/internal/server/workspace_renewal.go:331`; `workspaceAutoRenewCommandID` @ `services/control-plane/internal/server/workspace_renewal.go:47`; `workspaceRenewalIntentState` @ `services/control-plane/internal/server/workspace_renewal.go:55`; `bindWorkspaceAutoRenewAudit` @ `services/control-plane/internal/server/workspace_renewal.go:59`; `workspaceAutoRenewRequestHash` @ `services/control-plane/internal/server/workspace_renewal.go:95`

### 94. POST /api/workspaces/{workspaceId}/runtime-credentials/reveal

注册：`services/control-plane/internal/server/routes_workspace.go:149`；`registerWorkspaceRoutes`。

实际调用：`Context`, `Header`, `PathValue`, `RevealWorkspaceRuntimeCredentials`, `Set`, `ownedWorkspaceForCredentialCommand`, `protected`, `requiredMutationKey`, `stringValue`, `workspaceCurrentApplicationCredentials`, `workspaceRuntimeCredentialResponse`, `writeError`, `writeJSON`, `writeUpstreamError`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	workspace, ok := app.ownedWorkspaceForCredentialCommand(w, r, workspaceID)
	if !ok {
		return
	}
	key, ok := requiredMutationKey(w, r)
	if !ok {
		return
	}
	if app.workspaceCurrentApplicationCredentials(w, r, service, workspace) {
		return
	}
	runtime, err := service.RevealWorkspaceRuntimeCredentials(r.Context(), stringValue(workspace["accountId"]), workspaceID, key)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	if !runtime.Ready || runtime.Status == "not_found" || runtime.Access.Password == "" {
		writeError(w, http.StatusConflict, "workspace_credentials_unavailable")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, workspaceRuntimeCredentialResponse(runtime))
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `workspaceId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, workspaceRuntimeCredentialResponse(runtime))`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`RevealWorkspaceRuntimeCredentials` @ `services/control-plane/internal/clients/fabric.go:793`; `RevealWorkspaceRuntimeCredentials` @ `services/control-plane/internal/controlplane/service.go:383`; `stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `ownedWorkspaceForCredentialCommand` @ `services/control-plane/internal/server/routes_workspace.go:535`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `writeUpstreamError` @ `services/control-plane/internal/server/server.go:345`; `requiredMutationKey` @ `services/control-plane/internal/server/server.go:486`; `workspaceRuntimeCredentialResponse` @ `services/control-plane/internal/server/server.go:692`; `workspaceCurrentApplicationCredentials` @ `services/control-plane/internal/server/workspace_application_access.go:177`

### 95. POST /api/workspaces/{workspaceId}/runtime-credentials/rotate

注册：`services/control-plane/internal/server/routes_workspace.go:174`；`registerWorkspaceRoutes`。

实际调用：`Context`, `Header`, `Now`, `PathValue`, `RotateWorkspaceCredential`, `SaveWorkspace`, `Set`, `UTC`, `cloneMap`, `currentWorkspaceGatewaySecretRef`, `delete`, `firstNonEmpty`, `lockResource`, `mapField`, `ownedWorkspaceForCredentialCommand`, `protected`, `requiredMutationKey`, `rotateWorkspaceCurrentApplicationCredentials`, `stringFact`, `stringValue`, `succeededWorkspaceLaunchForAccess`, `unlock`, `workspaceAccessResponse`, `workspaceRuntimeCredentialResponse`, `writeError`, `writeJSON`, `writeUpstreamError`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	workspace, ok := app.ownedWorkspaceForCredentialCommand(w, r, workspaceID)
	if !ok {
		return
	}
	key, ok := requiredMutationKey(w, r)
	if !ok {
		return
	}
	unlock := app.lockResource("runtime-credential", workspaceID)
	defer unlock()
	workspace, ok = app.ownedWorkspaceForCredentialCommand(w, r, workspaceID)
	if !ok {
		return
	}
	if app.rotateWorkspaceCurrentApplicationCredentials(w, r, service, workspace, key) {
		return
	}
	if response, reason := app.workspaceAccessResponse(r.Context(), cloneMap(workspace), time.Now().UTC()); reason != "" || response["openable"] != true {
		writeError(w, http.StatusConflict, "workspace_not_running")
		return
	}
	launch, err := app.succeededWorkspaceLaunchForAccess(r.Context(), workspace)
	if err != nil {
		writeError(w, http.StatusConflict, "workspace_runtime_truth_unavailable")
		return
	}
	accountID := firstNonEmpty(stringValue(workspace["accountId"]), stringValue(workspace["ownerAccountId"]))
	gatewaySecretRef, err := app.currentWorkspaceGatewaySecretRef(r.Context(), workspace)
	if err != nil {
		writeError(w, http.StatusConflict, "workspace_gateway_secret_ref_unavailable")
		return
	}
	runtime, receipt, err := service.RotateWorkspaceCredential(r.Context(), controlplane.RotateWorkspaceCredentialInput{
		WorkspaceID:	workspaceID, AccountID: accountID, GatewaySecretRef: gatewaySecretRef,
		OwnerID:	firstNonEmpty(stringValue(workspace["ownerUserId"]), stringValue(workspace["ownerId"])),
		ComputeID:	launch.stringFact("computeAllocationId"), VolumeID: launch.stringFact("storageId"), AttachmentID: launch.stringFact("attachmentId"),
		AttachmentOperationID:	launch.ID + ":attachment", RuntimeID: launch.stringFact("runtimeId"),
		RuntimeOperationID:	launch.stringFact("runtimeBindingRef"),
	}, key)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	access := cloneMap(mapField(workspace, "access"))
	delete(access, "password")
	access["account"], access["username"] = runtime.Access.Username, runtime.Access.Username
	access["credentialStatus"] = runtime.Access.CredentialStatus
	access["credentialVersion"] = runtime.Access.CredentialVersion
	access["secretRef"] = runtime.Access.SecretRef
	workspace["access"] = access
	workspace["runtimeId"] = firstNonEmpty(runtime.ID, stringValue(workspace["runtimeId"]))
	runtimeProjection := cloneMap(mapField(workspace, "runtime"))
	runtimeProjection["serviceName"] = firstNonEmpty(runtime.ServiceName, stringValue(runtimeProjection["serviceName"]))
	runtimeProjection["status"], runtimeProjection["ready"] = runtime.Status, runtime.Ready
	workspace["runtime"] = runtimeProjection
	if err := app.tables.SaveWorkspace(r.Context(), workspace); err != nil {
		writeError(w, http.StatusInternalServerError, "state_persist_failed")
		return
	}
	body := workspaceRuntimeCredentialResponse(runtime)
	body["receiptId"] = receipt.ReceiptID
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, body)
})
```

</details>

直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：

| 字段/变量 | 类型 | 来源 |
| --- | --- | --- |
| `workspaceId` | `string` | path |

响应构造（含所有可见分支；不是承诺每次返回同一形状）：

- `writeJSON(w, http.StatusOK, body)`

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`RotateWorkspaceCredential` @ `services/control-plane/internal/controlplane/service.go:570`; `lockResource` @ `services/control-plane/internal/server/app_state.go:42`; `stringValue` @ `services/control-plane/internal/server/app_state.go:530`; `firstNonEmpty` @ `services/control-plane/internal/server/app_state.go:552`; `cloneMap` @ `services/control-plane/internal/server/app_state.go:561`; `SaveWorkspace` @ `services/control-plane/internal/server/ent_state_store_workspace.go:523`; `currentWorkspaceGatewaySecretRef` @ `services/control-plane/internal/server/routes_workspace.go:389`; `ownedWorkspaceForCredentialCommand` @ `services/control-plane/internal/server/routes_workspace.go:535`; `protected` @ `services/control-plane/internal/server/server.go:277`; `writeJSON` @ `services/control-plane/internal/server/server.go:335`; `writeError` @ `services/control-plane/internal/server/server.go:341`; `writeUpstreamError` @ `services/control-plane/internal/server/server.go:345`; `mapField` @ `services/control-plane/internal/server/server.go:476`; `requiredMutationKey` @ `services/control-plane/internal/server/server.go:486`; `workspaceRuntimeCredentialResponse` @ `services/control-plane/internal/server/server.go:692`; `rotateWorkspaceCurrentApplicationCredentials` @ `services/control-plane/internal/server/workspace_application_access.go:201`; `succeededWorkspaceLaunchForAccess` @ `services/control-plane/internal/server/workspace_gateway.go:1400`; `workspaceAccessResponse` @ `services/control-plane/internal/server/workspace_gateway.go:44`; `stringFact` @ `services/control-plane/internal/server/workspace_launch_reconciler.go:2821`

### 96. POST /api/workspaces/{workspaceId}/workspace-key/rotate

注册：`services/control-plane/internal/server/routes_workspace.go:240`；`registerWorkspaceRoutes`。

实际调用：`protected`, `rotateWorkspaceGatewayKey`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(false, func(w http.ResponseWriter, r *http.Request) {
	app.rotateWorkspaceGatewayKey(w, r, service)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`protected` @ `services/control-plane/internal/server/server.go:277`; `rotateWorkspaceGatewayKey` @ `services/control-plane/internal/server/workspace_gateway.go:458`

### 97. PUT /api/operator/announcements/{announcementId}

注册：`services/control-plane/internal/server/routes_announcements.go:65`；`registerAnnouncementRoutes`。

实际调用：`protected`, `updateAnnouncement`

<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>

```go
app.protected(true, func(w http.ResponseWriter, r *http.Request) {
	app.updateAnnouncement(w, r)
})
```

</details>

委托定义（须继续遵循该decoder/投影，不按名称猜字段）：`updateAnnouncement` @ `services/control-plane/internal/server/routes_announcements.go:185`; `protected` @ `services/control-plane/internal/server/server.go:277`

## 数据库实际字段与约束


### control_plane_accounts

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `varchar` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `—` | — |
| `updated_at` | `timestamptz` | NO | `—` | — |
| `owner_user_id` | `varchar` | NO | `—` | — |
| `sub2api_user_id` | `int8` | NO | `—` | 64 |
| `name` | `varchar` | NO | `''::character varying` | — |
| `status` | `varchar` | NO | `'active'::character varying` | — |
| `workspace_purchase_enabled` | `bool` | NO | `false` | — |

约束：
- `control_plane_accounts_owner_account_fkey` `FOREIGN KEY (owner_user_id, id) REFERENCES control_plane_users(id, account_id) DEFERRABLE INITIALLY DEFERRED`
- `control_plane_accounts_pkey` `PRIMARY KEY (id)`
- `control_plane_accounts_sub2api_user_id_positive` `CHECK ((sub2api_user_id > 0))`

索引：
- `CREATE UNIQUE INDEX control_plane_accounts_owner_user_id_key ON public.control_plane_accounts USING btree (owner_user_id)`
- `CREATE UNIQUE INDEX control_plane_accounts_owner_user_id_unique ON public.control_plane_accounts USING btree (owner_user_id)`
- `CREATE UNIQUE INDEX control_plane_accounts_pkey ON public.control_plane_accounts USING btree (id)`
- `CREATE UNIQUE INDEX control_plane_accounts_sub2api_user_id_key ON public.control_plane_accounts USING btree (sub2api_user_id)`
- `CREATE UNIQUE INDEX control_plane_accounts_sub2api_user_id_unique ON public.control_plane_accounts USING btree (sub2api_user_id) WHERE (sub2api_user_id > 0)`

### control_plane_admin_audit_events

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `varchar` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `—` | — |
| `updated_at` | `timestamptz` | NO | `—` | — |
| `actor_user_id` | `varchar` | NO | `''::character varying` | — |
| `actor_role` | `varchar` | NO | `''::character varying` | — |
| `actor_account_id` | `varchar` | NO | `''::character varying` | — |
| `target_account_id` | `varchar` | NO | `''::character varying` | — |
| `action` | `varchar` | NO | `''::character varying` | — |
| `resource_kind` | `varchar` | NO | `''::character varying` | — |
| `resource_id` | `varchar` | NO | `''::character varying` | — |
| `ip_address` | `varchar` | NO | `''::character varying` | — |
| `user_agent` | `varchar` | NO | `''::character varying` | — |
| `before_json` | `varchar` | NO | `''::character varying` | — |
| `after_json` | `varchar` | NO | `''::character varying` | — |
| `result` | `varchar` | NO | `''::character varying` | — |

约束：
- `control_plane_admin_audit_events_pkey` `PRIMARY KEY (id)`

索引：
- `CREATE UNIQUE INDEX control_plane_admin_audit_events_pkey ON public.control_plane_admin_audit_events USING btree (id)`

### control_plane_announcement_reads

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `varchar` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `—` | — |
| `updated_at` | `timestamptz` | NO | `—` | — |
| `announcement_id` | `varchar` | NO | `—` | — |
| `user_id` | `varchar` | NO | `—` | — |
| `read_at` | `varchar` | NO | `—` | — |

约束：
- `control_plane_announcement_reads_announcement_fk` `FOREIGN KEY (announcement_id) REFERENCES control_plane_announcements(id)`
- `control_plane_announcement_reads_pkey` `PRIMARY KEY (id)`
- `control_plane_announcement_reads_user_unique` `UNIQUE (announcement_id, user_id)`

索引：
- `CREATE UNIQUE INDEX control_plane_announcement_reads_pkey ON public.control_plane_announcement_reads USING btree (id)`
- `CREATE UNIQUE INDEX control_plane_announcement_reads_user_unique ON public.control_plane_announcement_reads USING btree (announcement_id, user_id)`

### control_plane_announcements

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `varchar` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `—` | — |
| `updated_at` | `timestamptz` | NO | `—` | — |
| `title` | `varchar` | NO | `—` | — |
| `body` | `varchar` | NO | `—` | — |
| `status` | `varchar` | NO | `'draft'::character varying` | — |
| `starts_at` | `varchar` | NO | `''::character varying` | — |
| `ends_at` | `varchar` | NO | `''::character varying` | — |
| `published_at` | `varchar` | NO | `''::character varying` | — |
| `created_by_user_id` | `varchar` | NO | `—` | — |
| `updated_by_user_id` | `varchar` | NO | `—` | — |

约束：
- `control_plane_announcements_ends_at_check` `CHECK ((((ends_at)::text = ''::text) OR ((ends_at)::timestamp with time zone IS NOT NULL)))`
- `control_plane_announcements_pkey` `PRIMARY KEY (id)`
- `control_plane_announcements_schedule_check` `CHECK ((((ends_at)::text = ''::text) OR ((starts_at)::text = ''::text) OR ((ends_at)::timestamp with time zone > (starts_at)::timestamp with time zone)))`
- `control_plane_announcements_starts_at_check` `CHECK ((((starts_at)::text = ''::text) OR ((starts_at)::timestamp with time zone IS NOT NULL)))`
- `control_plane_announcements_status_check` `CHECK (((status)::text = ANY ((ARRAY['draft'::character varying, 'scheduled'::character varying, 'published'::character varying, 'withdrawn'::character varying])::text[])))`

索引：
- `CREATE INDEX control_plane_announcements_active_idx ON public.control_plane_announcements USING btree (status, starts_at, ends_at)`
- `CREATE UNIQUE INDEX control_plane_announcements_pkey ON public.control_plane_announcements USING btree (id)`

### control_plane_application_data_materials

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `varchar` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `—` | — |
| `updated_at` | `timestamptz` | NO | `—` | — |
| `application_id` | `varchar` | NO | `—` | — |
| `version` | `varchar` | NO | `—` | — |
| `digest` | `varchar` | NO | `—` | — |
| `payload` | `varchar` | NO | `—` | — |
| `admitted_by_user_id` | `varchar` | NO | `—` | — |

约束：
- `control_plane_application_data_materials_pkey` `PRIMARY KEY (id)`

索引：
- `CREATE UNIQUE INDEX applicationdatamaterial_application_id_version ON public.control_plane_application_data_materials USING btree (application_id, version)`
- `CREATE INDEX control_plane_application_data_materials_application_idx ON public.control_plane_application_data_materials USING btree (application_id)`
- `CREATE UNIQUE INDEX control_plane_application_data_materials_pkey ON public.control_plane_application_data_materials USING btree (id)`

### control_plane_application_revisions

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `varchar` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `—` | — |
| `updated_at` | `timestamptz` | NO | `—` | — |
| `application_id` | `varchar` | NO | `—` | — |
| `version` | `varchar` | NO | `—` | — |
| `digest` | `varchar` | NO | `—` | — |
| `payload` | `varchar` | NO | `—` | — |
| `admitted_by_user_id` | `varchar` | NO | `—` | — |

约束：
- `control_plane_application_revisions_pkey` `PRIMARY KEY (id)`

索引：
- `CREATE UNIQUE INDEX applicationrevision_application_id_version ON public.control_plane_application_revisions USING btree (application_id, version)`
- `CREATE INDEX control_plane_application_revisions_application_idx ON public.control_plane_application_revisions USING btree (application_id)`
- `CREATE UNIQUE INDEX control_plane_application_revisions_pkey ON public.control_plane_application_revisions USING btree (id)`

### control_plane_archived_admin_audit_events

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `varchar` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `—` | — |
| `updated_at` | `timestamptz` | NO | `—` | — |
| `actor_user_id` | `varchar` | NO | `''::character varying` | — |
| `actor_role` | `varchar` | NO | `''::character varying` | — |
| `actor_account_id` | `varchar` | NO | `''::character varying` | — |
| `target_account_id` | `varchar` | NO | `''::character varying` | — |
| `action` | `varchar` | NO | `''::character varying` | — |
| `resource_kind` | `varchar` | NO | `''::character varying` | — |
| `resource_id` | `varchar` | NO | `''::character varying` | — |
| `ip_address` | `varchar` | NO | `''::character varying` | — |
| `user_agent` | `varchar` | NO | `''::character varying` | — |
| `before_json` | `varchar` | NO | `''::character varying` | — |
| `after_json` | `varchar` | NO | `''::character varying` | — |
| `result` | `varchar` | NO | `''::character varying` | — |

约束：
- `control_plane_archived_admin_audit_events_pkey` `PRIMARY KEY (id)`

索引：
- `CREATE UNIQUE INDEX control_plane_archived_admin_audit_events_pkey ON public.control_plane_archived_admin_audit_events USING btree (id)`

### control_plane_auth_attempts

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `varchar` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `—` | — |
| `updated_at` | `timestamptz` | NO | `—` | — |
| `email` | `varchar` | NO | `''::character varying` | — |
| `status` | `varchar` | NO | `''::character varying` | — |
| `reason` | `varchar` | NO | `''::character varying` | — |
| `ip_address` | `varchar` | NO | `''::character varying` | — |
| `user_agent` | `varchar` | NO | `''::character varying` | — |

约束：
- `control_plane_auth_attempts_pkey` `PRIMARY KEY (id)`

索引：
- `CREATE UNIQUE INDEX control_plane_auth_attempts_pkey ON public.control_plane_auth_attempts USING btree (id)`

### control_plane_billing_reconciliation

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `varchar` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `—` | — |
| `updated_at` | `timestamptz` | NO | `—` | — |
| `status` | `varchar` | NO | `''::character varying` | — |
| `guard_status` | `varchar` | NO | `''::character varying` | — |
| `guard_reason` | `varchar` | NO | `''::character varying` | — |
| `message_author` | `varchar` | NO | `''::character varying` | — |
| `message_text` | `varchar` | NO | `''::character varying` | — |
| `message_created_at` | `varchar` | NO | `''::character varying` | — |
| `guard_block_new_workspaces` | `bool` | NO | `false` | — |
| `reports` | `int8` | NO | `0` | 64 |

约束：
- `control_plane_billing_reconciliation_pkey` `PRIMARY KEY (id)`

索引：
- `CREATE UNIQUE INDEX control_plane_billing_reconciliation_pkey ON public.control_plane_billing_reconciliation USING btree (id)`

### control_plane_compute_allocations

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `varchar` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `—` | — |
| `updated_at` | `timestamptz` | NO | `—` | — |
| `account_id` | `varchar` | NO | `—` | — |
| `owner_user_id` | `varchar` | NO | `''::character varying` | — |
| `workspace_id` | `varchar` | NO | `''::character varying` | — |
| `name` | `varchar` | NO | `''::character varying` | — |
| `package_id` | `varchar` | NO | `''::character varying` | — |
| `provider` | `varchar` | NO | `''::character varying` | — |
| `provider_resource_id` | `varchar` | NO | `''::character varying` | — |
| `provider_request_id` | `varchar` | NO | `''::character varying` | — |
| `operation_id` | `varchar` | NO | `''::character varying` | — |
| `status` | `varchar` | NO | `''::character varying` | — |
| `desired_status` | `varchar` | NO | `''::character varying` | — |
| `provider_status` | `varchar` | NO | `''::character varying` | — |
| `last_provider_sync_at` | `varchar` | NO | `''::character varying` | — |
| `last_provider_sync_error` | `varchar` | NO | `''::character varying` | — |
| `external_deleted_at` | `varchar` | NO | `''::character varying` | — |
| `billing_status` | `varchar` | NO | `''::character varying` | — |
| `pricing_version` | `varchar` | NO | `''::character varying` | — |
| `billing_operation_id` | `varchar` | NO | `''::character varying` | — |
| `billing_state_json` | `varchar` | NO | `'{}'::character varying` | — |
| `evidence_id` | `varchar` | NO | `''::character varying` | — |
| `cvm_instance_id` | `varchar` | NO | `''::character varying` | — |
| `instance_id` | `varchar` | NO | `''::character varying` | — |
| `node_name` | `varchar` | NO | `''::character varying` | — |
| `machine_name` | `varchar` | NO | `''::character varying` | — |
| `cpu` | `float8` | NO | `0` | 53 |
| `memory_gb` | `float8` | NO | `0` | 53 |
| `disk_gb` | `float8` | NO | `0` | 53 |

约束：
- `control_plane_compute_allocations_pkey` `PRIMARY KEY (id)`

索引：
- `CREATE UNIQUE INDEX control_plane_compute_allocations_pkey ON public.control_plane_compute_allocations USING btree (id)`

### control_plane_memberships

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `text` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `—` | — |
| `updated_at` | `timestamptz` | NO | `—` | — |
| `account_id` | `text` | NO | `—` | — |
| `organization_id` | `text` | NO | `''::text` | — |
| `user_id` | `text` | NO | `—` | — |
| `role` | `text` | NO | `'member'::text` | — |
| `status` | `text` | NO | `'active'::text` | — |

约束：
- `control_plane_memberships_account_id_fkey` `FOREIGN KEY (account_id) REFERENCES control_plane_accounts(id) DEFERRABLE INITIALLY DEFERRED`
- `control_plane_memberships_organization_account_fkey` `FOREIGN KEY (organization_id, account_id) REFERENCES control_plane_organizations(id, billing_account_id) DEFERRABLE INITIALLY DEFERRED`
- `control_plane_memberships_owner_role` `CHECK ((role = 'owner'::text))`
- `control_plane_memberships_pkey` `PRIMARY KEY (id)`
- `control_plane_memberships_user_account_fkey` `FOREIGN KEY (user_id, account_id) REFERENCES control_plane_users(id, account_id) DEFERRABLE INITIALLY DEFERRED`

索引：
- `CREATE UNIQUE INDEX control_plane_memberships_account_id_unique ON public.control_plane_memberships USING btree (account_id)`
- `CREATE UNIQUE INDEX control_plane_memberships_organization_id_unique ON public.control_plane_memberships USING btree (organization_id)`
- `CREATE UNIQUE INDEX control_plane_memberships_pkey ON public.control_plane_memberships USING btree (id)`
- `CREATE UNIQUE INDEX control_plane_memberships_user_id_unique ON public.control_plane_memberships USING btree (user_id)`

### control_plane_organizations

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `text` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `—` | — |
| `updated_at` | `timestamptz` | NO | `—` | — |
| `billing_account_id` | `text` | NO | `''::text` | — |
| `name` | `text` | NO | `''::text` | — |
| `status` | `text` | NO | `'active'::text` | — |

约束：
- `control_plane_organizations_billing_account_id_fkey` `FOREIGN KEY (billing_account_id) REFERENCES control_plane_accounts(id) DEFERRABLE INITIALLY DEFERRED`
- `control_plane_organizations_pkey` `PRIMARY KEY (id)`

索引：
- `CREATE UNIQUE INDEX control_plane_organizations_billing_account_id_unique ON public.control_plane_organizations USING btree (billing_account_id)`
- `CREATE UNIQUE INDEX control_plane_organizations_id_billing_account_id_unique ON public.control_plane_organizations USING btree (id, billing_account_id)`
- `CREATE UNIQUE INDEX control_plane_organizations_pkey ON public.control_plane_organizations USING btree (id)`

### control_plane_production_e2e_records

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `varchar` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `—` | — |
| `updated_at` | `timestamptz` | NO | `—` | — |
| `account_id` | `varchar` | NO | `''::character varying` | — |
| `workspace_id` | `varchar` | NO | `''::character varying` | — |
| `status` | `varchar` | NO | `''::character varying` | — |
| `result` | `varchar` | NO | `''::character varying` | — |
| `reason` | `varchar` | NO | `''::character varying` | — |
| `url` | `varchar` | NO | `''::character varying` | — |

约束：
- `control_plane_production_e2e_records_pkey` `PRIMARY KEY (id)`

索引：
- `CREATE UNIQUE INDEX control_plane_production_e2e_records_pkey ON public.control_plane_production_e2e_records USING btree (id)`

### control_plane_project_task_sync_heads

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `varchar` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `—` | — |
| `updated_at` | `timestamptz` | NO | `—` | — |
| `kind` | `varchar` | NO | `—` | — |
| `organization_id` | `varchar` | NO | `—` | — |
| `workspace_id` | `varchar` | NO | `—` | — |
| `project_id` | `varchar` | NO | `''::character varying` | — |
| `local_alias_id` | `varchar` | NO | `''::character varying` | — |
| `version` | `int8` | NO | `1` | 64 |
| `status` | `varchar` | NO | `'active'::character varying` | — |

约束：
- `control_plane_project_task_sync_heads_pkey` `PRIMARY KEY (id)`

索引：
- `CREATE UNIQUE INDEX control_plane_project_task_sync_heads_pkey ON public.control_plane_project_task_sync_heads USING btree (id)`

### control_plane_runtime_operations

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `varchar` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `—` | — |
| `updated_at` | `timestamptz` | NO | `—` | — |
| `operation_id` | `varchar` | NO | `''::character varying` | — |
| `account_id` | `varchar` | NO | `''::character varying` | — |
| `workspace_id` | `varchar` | NO | `''::character varying` | — |
| `period_start` | `varchar` | NO | `''::character varying` | — |
| `resource_id` | `varchar` | NO | `''::character varying` | — |
| `resource_kind` | `varchar` | NO | `''::character varying` | — |
| `action` | `varchar` | NO | `''::character varying` | — |
| `provider` | `varchar` | NO | `''::character varying` | — |
| `provider_request_id` | `varchar` | NO | `''::character varying` | — |
| `status` | `varchar` | NO | `''::character varying` | — |
| `result` | `varchar` | NO | `''::character varying` | — |
| `compute_allocation_id` | `varchar` | NO | `''::character varying` | — |
| `storage_id` | `varchar` | NO | `''::character varying` | — |
| `attachment_id` | `varchar` | NO | `''::character varying` | — |
| `runtime_service_name` | `varchar` | NO | `''::character varying` | — |
| `cvm_instance_id` | `varchar` | NO | `''::character varying` | — |
| `instance_id` | `varchar` | NO | `''::character varying` | — |
| `node_name` | `varchar` | NO | `''::character varying` | — |
| `machine_name` | `varchar` | NO | `''::character varying` | — |

约束：
- `control_plane_runtime_operations_pkey` `PRIMARY KEY (id)`

索引：
- `CREATE INDEX control_plane_runtime_operations_account_action_status_key ON public.control_plane_runtime_operations USING btree (account_id, action, status, created_at, id)`
- `CREATE INDEX control_plane_runtime_operations_action_status_key ON public.control_plane_runtime_operations USING btree (action, status, created_at, id)`
- `CREATE UNIQUE INDEX control_plane_runtime_operations_pkey ON public.control_plane_runtime_operations USING btree (id)`
- `CREATE INDEX control_plane_runtime_operations_workspace_action_status_period ON public.control_plane_runtime_operations USING btree (workspace_id, action, status, period_start, created_at, id)`
- `CREATE INDEX runtimeoperation_account_id_action_status_created_at_id ON public.control_plane_runtime_operations USING btree (account_id, action, status, created_at, id)`
- `CREATE INDEX runtimeoperation_action_status_created_at_id ON public.control_plane_runtime_operations USING btree (action, status, created_at, id)`
- `CREATE INDEX runtimeoperation_workspace_id__ac43725fe158bfaf1da4a9b216fead02 ON public.control_plane_runtime_operations USING btree (workspace_id, action, status, period_start, created_at, id)`

### control_plane_sessions

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `varchar` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `—` | — |
| `updated_at` | `timestamptz` | NO | `—` | — |
| `user_id` | `varchar` | NO | `—` | — |
| `csrf` | `varchar` | NO | `—` | — |
| `expires_at` | `varchar` | NO | `—` | — |

约束：
- `control_plane_sessions_pkey` `PRIMARY KEY (id)`
- `control_plane_sessions_sub2api_id` `CHECK (((id)::text ~~ 'sub2api-sha256:%'::text))`

索引：
- `CREATE UNIQUE INDEX control_plane_sessions_pkey ON public.control_plane_sessions USING btree (id)`
- `CREATE INDEX control_plane_sessions_user_id_key ON public.control_plane_sessions USING btree (user_id, id)`
- `CREATE INDEX session_user_id ON public.control_plane_sessions USING btree (user_id)`

### control_plane_storage_attachments

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `varchar` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `—` | — |
| `updated_at` | `timestamptz` | NO | `—` | — |
| `account_id` | `varchar` | NO | `—` | — |
| `workspace_id` | `varchar` | NO | `''::character varying` | — |
| `compute_allocation_id` | `varchar` | NO | `''::character varying` | — |
| `storage_id` | `varchar` | NO | `''::character varying` | — |
| `volume_id` | `varchar` | NO | `''::character varying` | — |
| `operation_id` | `varchar` | NO | `''::character varying` | — |
| `provider` | `varchar` | NO | `''::character varying` | — |
| `provider_request_id` | `varchar` | NO | `''::character varying` | — |
| `status` | `varchar` | NO | `''::character varying` | — |
| `mount_path` | `varchar` | NO | `''::character varying` | — |

约束：
- `control_plane_storage_attachments_pkey` `PRIMARY KEY (id)`

索引：
- `CREATE UNIQUE INDEX control_plane_storage_attachments_pkey ON public.control_plane_storage_attachments USING btree (id)`

### control_plane_storage_volumes

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `varchar` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `—` | — |
| `updated_at` | `timestamptz` | NO | `—` | — |
| `account_id` | `varchar` | NO | `—` | — |
| `owner_user_id` | `varchar` | NO | `''::character varying` | — |
| `workspace_id` | `varchar` | NO | `''::character varying` | — |
| `name` | `varchar` | NO | `''::character varying` | — |
| `package_id` | `varchar` | NO | `''::character varying` | — |
| `provider` | `varchar` | NO | `''::character varying` | — |
| `provider_resource_id` | `varchar` | NO | `''::character varying` | — |
| `provider_request_id` | `varchar` | NO | `''::character varying` | — |
| `operation_id` | `varchar` | NO | `''::character varying` | — |
| `status` | `varchar` | NO | `''::character varying` | — |
| `desired_status` | `varchar` | NO | `''::character varying` | — |
| `provider_status` | `varchar` | NO | `''::character varying` | — |
| `last_provider_sync_at` | `varchar` | NO | `''::character varying` | — |
| `last_provider_sync_error` | `varchar` | NO | `''::character varying` | — |
| `external_deleted_at` | `varchar` | NO | `''::character varying` | — |
| `billing_status` | `varchar` | NO | `''::character varying` | — |
| `pricing_version` | `varchar` | NO | `''::character varying` | — |
| `billing_operation_id` | `varchar` | NO | `''::character varying` | — |
| `billing_state_json` | `varchar` | NO | `'{}'::character varying` | — |
| `mount_path` | `varchar` | NO | `''::character varying` | — |
| `size_gb` | `float8` | NO | `0` | 53 |

约束：
- `control_plane_storage_volumes_pkey` `PRIMARY KEY (id)`

索引：
- `CREATE UNIQUE INDEX control_plane_storage_volumes_pkey ON public.control_plane_storage_volumes USING btree (id)`

### control_plane_users

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `varchar` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `—` | — |
| `updated_at` | `timestamptz` | NO | `—` | — |
| `account_id` | `varchar` | NO | `—` | — |
| `email` | `varchar` | NO | `—` | — |
| `role` | `varchar` | NO | `'owner'::character varying` | — |
| `status` | `varchar` | NO | `'active'::character varying` | — |
| `password_hash` | `varchar` | NO | `''::character varying` | — |
| `disabled_at` | `varchar` | NO | `''::character varying` | — |
| `disabled_by` | `varchar` | NO | `''::character varying` | — |
| `disabled_reason` | `varchar` | NO | `''::character varying` | — |
| `deleted_at` | `varchar` | NO | `''::character varying` | — |
| `deleted_by` | `varchar` | NO | `''::character varying` | — |
| `delete_reason` | `varchar` | NO | `''::character varying` | — |

约束：
- `control_plane_users_account_id_fkey` `FOREIGN KEY (account_id) REFERENCES control_plane_accounts(id) DEFERRABLE INITIALLY DEFERRED`
- `control_plane_users_customer_owner_role` `CHECK (((((id)::text = 'usr-admin'::text) AND ((account_id)::text = 'acct-admin'::text) AND ((role)::text = 'admin'::text)) OR (((id)::text <> 'usr-admin'::text) AND ((account_id)::text <> 'acct-admin'::text) AND ((role)::text = 'owner'::text))))`
- `control_plane_users_email_canonical` `CHECK ((((email)::text <> ''::text) AND ((email)::text = lower(btrim((email)::text)))))`
- `control_plane_users_password_hash_empty` `CHECK (((password_hash)::text = ''::text))`
- `control_plane_users_pkey` `PRIMARY KEY (id)`

索引：
- `CREATE UNIQUE INDEX control_plane_users_account_id_key ON public.control_plane_users USING btree (account_id)`
- `CREATE UNIQUE INDEX control_plane_users_account_id_unique ON public.control_plane_users USING btree (account_id)`
- `CREATE UNIQUE INDEX control_plane_users_email_key ON public.control_plane_users USING btree (email)`
- `CREATE UNIQUE INDEX control_plane_users_email_normalized_unique ON public.control_plane_users USING btree (lower(btrim((email)::text)))`
- `CREATE UNIQUE INDEX control_plane_users_id_account_id_unique ON public.control_plane_users USING btree (id, account_id)`
- `CREATE UNIQUE INDEX control_plane_users_pkey ON public.control_plane_users USING btree (id)`

### control_plane_workspace_sync_events

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `varchar` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `—` | — |
| `updated_at` | `timestamptz` | NO | `—` | — |
| `operation_id` | `varchar` | NO | `—` | — |
| `workspace_id` | `varchar` | NO | `—` | — |
| `cursor` | `int8` | NO | `—` | 64 |
| `entity_kind` | `varchar` | NO | `—` | — |
| `project_id` | `varchar` | NO | `—` | — |
| `task_id` | `varchar` | NO | `''::character varying` | — |
| `client_id` | `varchar` | NO | `—` | — |
| `actor_user_id` | `varchar` | NO | `—` | — |
| `base_version` | `int8` | NO | `—` | 64 |
| `server_version` | `int8` | NO | `—` | 64 |
| `operation` | `varchar` | NO | `—` | — |
| `status` | `varchar` | NO | `—` | — |
| `payload_json` | `varchar` | NO | `'{}'::character varying` | — |
| `content_digest` | `varchar` | NO | `''::character varying` | — |
| `idempotency_key` | `varchar` | NO | `—` | — |
| `request_hash` | `varchar` | NO | `—` | — |
| `conflict_id` | `varchar` | NO | `''::character varying` | — |
| `occurred_at` | `timestamptz` | NO | `—` | — |

约束：
- `control_plane_workspace_sync_events_pkey` `PRIMARY KEY (id)`

索引：
- `CREATE UNIQUE INDEX control_plane_workspace_sync_events_idempotency_key_key ON public.control_plane_workspace_sync_events USING btree (idempotency_key)`
- `CREATE UNIQUE INDEX control_plane_workspace_sync_events_pkey ON public.control_plane_workspace_sync_events USING btree (id)`
- `CREATE INDEX workspacesyncevent_workspace_i_03f5119414bab2de3cb7797619f3e1b9 ON public.control_plane_workspace_sync_events USING btree (workspace_id, entity_kind, project_id, task_id, cursor)`
- `CREATE INDEX workspacesyncevent_workspace_id_conflict_id ON public.control_plane_workspace_sync_events USING btree (workspace_id, conflict_id)`
- `CREATE UNIQUE INDEX workspacesyncevent_workspace_id_cursor ON public.control_plane_workspace_sync_events USING btree (workspace_id, cursor)`
- `CREATE UNIQUE INDEX workspacesyncevent_workspace_id_operation_id ON public.control_plane_workspace_sync_events USING btree (workspace_id, operation_id)`

### control_plane_workspaces

| 列 | PostgreSQL类型 | 可NULL | 默认值 | 长度/精度 |
| --- | --- | --- | --- | --- |
| `id` | `varchar` | NO | `—` | — |
| `created_at` | `timestamptz` | NO | `—` | — |
| `updated_at` | `timestamptz` | NO | `—` | — |
| `account_id` | `varchar` | NO | `''::character varying` | — |
| `owner_account_id` | `varchar` | NO | `''::character varying` | — |
| `owner_user_id` | `varchar` | NO | `''::character varying` | — |
| `user_id` | `varchar` | NO | `''::character varying` | — |
| `name` | `varchar` | NO | `''::character varying` | — |
| `url` | `varchar` | NO | `''::character varying` | — |
| `state` | `varchar` | NO | `''::character varying` | — |
| `status` | `varchar` | NO | `''::character varying` | — |
| `purchase_receipt_id` | `varchar` | NO | `''::character varying` | — |
| `billing_state_json` | `varchar` | NO | `'{}'::character varying` | — |
| `storage_id` | `varchar` | NO | `''::character varying` | — |
| `current_compute_allocation_id` | `varchar` | NO | `''::character varying` | — |
| `current_attachment_id` | `varchar` | NO | `''::character varying` | — |
| `runtime_id` | `varchar` | NO | `''::character varying` | — |
| `runtime_service_name` | `varchar` | NO | `''::character varying` | — |
| `runtime_service_name_root` | `varchar` | NO | `''::character varying` | — |
| `service_name` | `varchar` | NO | `''::character varying` | — |
| `workspace_api_key_id` | `int8` | YES | `—` | 64 |
| `access_token_status` | `varchar` | NO | `''::character varying` | — |
| `access_account` | `varchar` | NO | `''::character varying` | — |
| `access_username` | `varchar` | NO | `''::character varying` | — |
| `credential_status` | `varchar` | NO | `''::character varying` | — |
| `credential_version` | `varchar` | NO | `''::character varying` | — |
| `credential_secret_ref` | `varchar` | NO | `''::character varying` | — |
| `access_requires_login` | `bool` | NO | `false` | — |
| `verification_slot_id` | `varchar` | NO | `''::character varying` | — |
| `customer_product` | `bool` | NO | `true` | — |
| `application_binding` | `varchar` | NO | `''::character varying` | — |
| `application_binding_version` | `int8` | NO | `0` | 64 |
| `current_application_deployment_id` | `varchar` | NO | `''::character varying` | — |
| `reserved_application_deployment_id` | `varchar` | NO | `''::character varying` | — |

约束：
- `control_plane_workspaces_pkey` `PRIMARY KEY (id)`
- `control_plane_workspaces_workspace_api_key_id_positive` `CHECK (((workspace_api_key_id IS NULL) OR (workspace_api_key_id > 0)))`

索引：
- `CREATE INDEX control_plane_workspaces_account_page_key ON public.control_plane_workspaces USING btree (COALESCE(NULLIF((account_id)::text, ''::text), (owner_account_id)::text), customer_product, id)`
- `CREATE UNIQUE INDEX control_plane_workspaces_pkey ON public.control_plane_workspaces USING btree (id)`
- `CREATE INDEX control_plane_workspaces_renewal_candidates_key ON public.control_plane_workspaces USING btree (customer_product, id)`
- `CREATE INDEX workspace_customer_product_id ON public.control_plane_workspaces USING btree (customer_product, id)`

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
