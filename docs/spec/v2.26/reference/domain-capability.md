# Capability：API、字段与交互

> 源码快照 `7f5d05fe9b855d8af216caad714cf7fc85014d3c`；这是已定义目标的派生索引，不是业务实现完成声明。回到[总览](../15_domain_alignment.md)。

## 1. 边界与继承

模块：`services/capability`。当前：**新持久化骨架；ProductService/Coordination尚未注册**。

Namespace、Package及其版本、批准Runtime/WebUI目录、唯一可部署CapabilityVersion、引用claims。不执行Build、不拥有Registry内容权威。

**继承 / 提取 / 新增：** 旧 application_revisions/data_materials 的typed publisher契约复用；legacy_application导入与新Build来源区分，不伪造构建历史。

**旧事实：** control_plane_application_revisions；control_plane_application_data_materials；现有contracts Go validators。

**事务边界：** 同一版本的注册、引用保护、唯一约束及事件在本库提交；Build绝不能直接插入capability_versions。

**业务顺序：** 上传→对象digest确认→PackageVersion uploaded；客户显式请求Build；收到Build产物事件后向Build读回精确制品/descriptor→创建唯一ready版本→注册完成事件。

**失败 / unknown：** 未获精确制品/claim证据不ready；归档不清历史；删除需引用保护。

## 2. 客户 REST 与后端 Owner

30 个规格REST操作；浏览器仅经BFF。表中的请求/响应为目标契约，不代表该RPC已挂载。字段展开见DTO目录。


### listNamespaces

`GET /api/v2/namespaces`

权限：`member`；F：`F02`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |

Body：无独立命名body，见该操作schema；Response：[NamespacePage](rest-schemas.md#namespacepage)

响应顶层字段：`items`: array<Namespace>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`capability.namespaces`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### createNamespace

`POST /api/v2/namespaces`

权限：`admin, owner`；F：`F02`；主要成功状态：`201`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[NamespaceWriteRequest](rest-schemas.md#namespacewriterequest)；Response：[Namespace](rest-schemas.md#namespace)

请求顶层字段：`name`: string（必填）

响应顶层字段：`id`: OpaqueId（必填）；`name`: string（必填）；`isDefault`: boolean（必填）；`status`: string（必填）；`createdAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`capability.namespaces`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### updateNamespace

`PUT /api/v2/namespaces/{namespaceId}`

权限：`admin, owner`；F：`F02`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `namespaceId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[NamespaceWriteRequest](rest-schemas.md#namespacewriterequest)；Response：[Namespace](rest-schemas.md#namespace)

请求顶层字段：`name`: string（必填）

响应顶层字段：`id`: OpaqueId（必填）；`name`: string（必填）；`isDefault`: boolean（必填）；`status`: string（必填）；`createdAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`capability.namespaces`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### archiveNamespace

`POST /api/v2/namespaces/{namespaceId}/archive`

权限：`admin, owner`；F：`F02`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `namespaceId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：无独立命名body，见该操作schema；Response：[Namespace](rest-schemas.md#namespace)

响应顶层字段：`id`: OpaqueId（必填）；`name`: string（必填）；`isDefault`: boolean（必填）；`status`: string（必填）；`createdAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`capability.namespaces`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listPackages

`GET /api/v2/packages`

权限：`member`；F：`F02, F06`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |
| query | `visibility` | `string` | 否 |
| query | `namespaceId` | `OpaqueId` | 否 |
| query | `status` | `string` | 否 |
| query | `search` | `string` | 否 |

Body：无独立命名body，见该操作schema；Response：[PackagePage](rest-schemas.md#packagepage)

响应顶层字段：`items`: array<Package>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`capability.packages`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### createPackage

`POST /api/v2/packages`

权限：`admin, owner`；F：`F02, F04`；主要成功状态：`201`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[CreatePackageRequest](rest-schemas.md#createpackagerequest)；Response：[Package](rest-schemas.md#package)

请求顶层字段：`namespaceId`: OpaqueId（必填）；`name`: string（必填）；`description`: string（必填）

响应顶层字段：`id`: OpaqueId（必填）；`namespaceId`: OpaqueId（必填）；`name`: string（必填）；`description`: string（必填）；`visibility`: string（必填）；`status`: string（必填）；`latestReadyVersionId`: OpaqueId（可选）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`capability.packages`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### getPackage

`GET /api/v2/packages/{packageId}`

权限：`member`；F：`F02, F06`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `packageId` | `OpaqueId` | 是 |

Body：无独立命名body，见该操作schema；Response：[Package](rest-schemas.md#package)

响应顶层字段：`id`: OpaqueId（必填）；`namespaceId`: OpaqueId（必填）；`name`: string（必填）；`description`: string（必填）；`visibility`: string（必填）；`status`: string（必填）；`latestReadyVersionId`: OpaqueId（可选）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`capability.packages`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### updatePackage

`PUT /api/v2/packages/{packageId}`

权限：`admin, owner`；F：`F02`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `packageId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[UpdatePackageRequest](rest-schemas.md#updatepackagerequest)；Response：[Package](rest-schemas.md#package)

请求顶层字段：`namespaceId`: OpaqueId（必填）；`name`: string（必填）；`description`: string（必填）

响应顶层字段：`id`: OpaqueId（必填）；`namespaceId`: OpaqueId（必填）；`name`: string（必填）；`description`: string（必填）；`visibility`: string（必填）；`status`: string（必填）；`latestReadyVersionId`: OpaqueId（可选）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`capability.packages`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### archivePackage

`POST /api/v2/packages/{packageId}/archive`

权限：`admin, owner`；F：`F06`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `packageId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：无独立命名body，见该操作schema；Response：[Package](rest-schemas.md#package)

响应顶层字段：`id`: OpaqueId（必填）；`namespaceId`: OpaqueId（必填）；`name`: string（必填）；`description`: string（必填）；`visibility`: string（必填）；`status`: string（必填）；`latestReadyVersionId`: OpaqueId（可选）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`capability.packages`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### createUpload

`POST /api/v2/packages/{packageId}/uploads`

权限：`admin, owner`；F：`F04`；主要成功状态：`201`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `packageId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[CreateUploadRequest](rest-schemas.md#createuploadrequest)；Response：[UploadSession](rest-schemas.md#uploadsession)

请求顶层字段：`versionLabel`: string（必填）；`fileName`: string（必填）；`sizeBytes`: NonnegativeInt64（必填）；`sha256`: Digest（必填）

响应顶层字段：`id`: OpaqueId（必填）；`packageVersionId`: OpaqueId（必填）；`status`: string（必填）；`sizeBytes`: NonnegativeInt64（必填）；`sha256`: Digest（必填）；`partSizeBytes`: NonnegativeInt64（必填）；`completedParts`: array<UploadPart>（必填）；`expiresAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`capability.package_versions`, `capability.upload_sessions`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### getUpload

`GET /api/v2/uploads/{uploadId}`

权限：`admin, owner`；F：`F04`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `uploadId` | `OpaqueId` | 是 |

Body：无独立命名body，见该操作schema；Response：[UploadSession](rest-schemas.md#uploadsession)

响应顶层字段：`id`: OpaqueId（必填）；`packageVersionId`: OpaqueId（必填）；`status`: string（必填）；`sizeBytes`: NonnegativeInt64（必填）；`sha256`: Digest（必填）；`partSizeBytes`: NonnegativeInt64（必填）；`completedParts`: array<UploadPart>（必填）；`expiresAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`capability.upload_sessions`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### createUploadPart

`POST /api/v2/uploads/{uploadId}/parts`

权限：`admin, owner`；F：`F04`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `uploadId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[CreateUploadPartRequest](rest-schemas.md#createuploadpartrequest)；Response：[UploadPartAuthorization](rest-schemas.md#uploadpartauthorization)

请求顶层字段：`partNumber`: integer/int32（必填）；`sizeBytes`: NonnegativeInt64（必填）；`sha256`: Digest（必填）

响应顶层字段：`uploadId`: OpaqueId（必填）；`partNumber`: integer/int32（必填）；`method`: string（必填）；`url`: string/uri（必填）；`contentType`: string（必填）；`requiredChecksumHeaderName`: string（必填）；`requiredChecksumHeaderValue`: string（必填）；`expiresAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`capability.upload_sessions`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### completeUpload

`POST /api/v2/uploads/{uploadId}/complete`

权限：`admin, owner`；F：`F04`；主要成功状态：`202`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `uploadId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[CompleteUploadRequest](rest-schemas.md#completeuploadrequest)；Response：[Operation](rest-schemas.md#operation)

请求顶层字段：`parts`: array<UploadPart>（必填）

响应顶层字段：`operationId`: OpaqueId（必填）；`owner`: OperationOwner（必填）；`kind`: OperationKind（必填）；`resourceId`: OpaqueId（必填）；`status`: string（必填）；`stage`: OperationStage（必填）；`observationResult`: string（可选）；`errorCode`: ErrorCode（可选）；`requestId`: OpaqueId（必填）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）；`pollAfterSeconds`: integer（可选）

涉及表（规格声明，不自动等于每次都写）：`capability.upload_sessions`, `capability.package_versions`, `capability.operations`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listPackageVersions

`GET /api/v2/packages/{packageId}/versions`

权限：`member`；F：`F04`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `packageId` | `OpaqueId` | 是 |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |

Body：无独立命名body，见该操作schema；Response：[PackageVersionPage](rest-schemas.md#packageversionpage)

响应顶层字段：`items`: array<PackageVersion>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`capability.package_versions`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### getPackageVersion

`GET /api/v2/package-versions/{packageVersionId}`

权限：`member`；F：`F04`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `packageVersionId` | `OpaqueId` | 是 |

Body：无独立命名body，见该操作schema；Response：[PackageVersion](rest-schemas.md#packageversion)

响应顶层字段：`id`: OpaqueId（必填）；`packageId`: OpaqueId（必填）；`versionLabel`: string（必填）；`status`: string（必填）；`sha256`: Digest（必填）；`sizeBytes`: NonnegativeInt64（必填）；`createdAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`capability.package_versions`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listCapabilityVersions

`GET /api/v2/capability-versions`

权限：`member`；F：`F06, F07`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |
| query | `packageId` | `OpaqueId` | 否 |
| query | `status` | `string` | 否 |

Body：无独立命名body，见该操作schema；Response：[CapabilityVersionPage](rest-schemas.md#capabilityversionpage)

响应顶层字段：`items`: array<CapabilityVersion>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`capability.capability_versions`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### getCapabilityVersion

`GET /api/v2/capability-versions/{capabilityVersionId}`

权限：`member`；F：`F06, F10`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `capabilityVersionId` | `OpaqueId` | 是 |

Body：无独立命名body，见该操作schema；Response：[CapabilityVersion](rest-schemas.md#capabilityversion)

响应顶层字段：`id`: OpaqueId（必填）；`packageId`: OpaqueId（可选）；`packageVersionId`: OpaqueId（可选）；`buildJobId`: OpaqueId（可选）；`versionLabel`: string（必填）；`runtimeVersionId`: OpaqueId（可选）；`webuiVersionId`: OpaqueId（可选）；`artifactDigest`: Digest（必填）；`status`: string（必填）；`modelRequirements`: array<ModelRequirement>（必填）；`dataCompatibility`: DataCompatibility（必填）；`referenceCount`: NonnegativeInt64（必填）；`createdAt`: string/date-time（必填）；`provenance`: string（必填）；`legacyApplicationRevisionId`: OpaqueId（可选）；`artifact`: ArtifactReference（必填）；`deploymentDescriptor`: DeploymentDescriptor（必填）；`deploymentDescriptorDigest`: Digest（必填）；`deploymentDescriptorObjectRef`: OpaqueId（必填）

涉及表（规格声明，不自动等于每次都写）：`capability.capability_versions`, `capability.reference_claims`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### deleteCapabilityVersion

`DELETE /api/v2/capability-versions/{capabilityVersionId}`

权限：`admin, owner`；F：`F06`；主要成功状态：`202`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `capabilityVersionId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：无独立命名body，见该操作schema；Response：[Operation](rest-schemas.md#operation)

响应顶层字段：`operationId`: OpaqueId（必填）；`owner`: OperationOwner（必填）；`kind`: OperationKind（必填）；`resourceId`: OpaqueId（必填）；`status`: string（必填）；`stage`: OperationStage（必填）；`observationResult`: string（可选）；`errorCode`: ErrorCode（可选）；`requestId`: OpaqueId（必填）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）；`pollAfterSeconds`: integer（可选）

涉及表（规格声明，不自动等于每次都写）：`capability.capability_versions`, `capability.reference_claims`, `capability.operations`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### publishOfficialPackage

`POST /api/v2/admin/packages/{packageId}/publish`

权限：`platform_admin`；F：`F03`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `packageId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[PublishPackageRequest](rest-schemas.md#publishpackagerequest)；Response：[Package](rest-schemas.md#package)

请求顶层字段：`admissionReceiptId`: OpaqueId（必填）

响应顶层字段：`id`: OpaqueId（必填）；`namespaceId`: OpaqueId（必填）；`name`: string（必填）；`description`: string（必填）；`visibility`: string（必填）；`status`: string（必填）；`latestReadyVersionId`: OpaqueId（可选）；`createdAt`: string/date-time（必填）；`updatedAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`capability.packages`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listRuntimeVersions

`GET /api/v2/catalog/runtime-versions`

权限：`member`；F：`F03, F07`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |

Body：无独立命名body，见该操作schema；Response：[RuntimeVersionPage](rest-schemas.md#runtimeversionpage)

响应顶层字段：`items`: array<RuntimeVersion>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`capability.runtime_versions`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listWebuiVersions

`GET /api/v2/catalog/webui-versions`

权限：`member`；F：`F03, F07`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |

Body：无独立命名body，见该操作schema；Response：[WebuiVersionPage](rest-schemas.md#webuiversionpage)

响应顶层字段：`items`: array<WebuiVersion>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`capability.webui_versions`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### registerRuntimeVersion

`POST /api/v2/admin/catalog/runtime-versions`

权限：`platform_admin`；F：`F03`；主要成功状态：`201`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[RegisterRuntimeVersionRequest](rest-schemas.md#registerruntimeversionrequest)；Response：[RuntimeVersion](rest-schemas.md#runtimeversion)

请求顶层字段：`name`: string（必填）；`versionLabel`: string（必填）；`publisherNamespaceId`: OpaqueId（必填）；`publisherContract`: RuntimePublisherContract（必填）；`admissionReceiptId`: OpaqueId（必填）

响应顶层字段：`id`: OpaqueId（必填）；`name`: string（必填）；`versionLabel`: string（必填）；`artifactDigest`: Digest（必填）；`status`: string（必填）；`runtimeAbiVersion`: string（必填）；`packageFormatVersions`: array<string>（必填）；`defaultForNewBuilds`: boolean（必填）；`admissionReceiptId`: OpaqueId（必填）；`createdAt`: string/date-time（必填）；`publisherNamespaceId`: OpaqueId（必填）；`publisherContractDigest`: Digest（必填）；`publisherContract`: RuntimePublisherContract（必填）；`publisherContractObjectRef`: OpaqueId（必填）

涉及表（规格声明，不自动等于每次都写）：`capability.runtime_versions`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### setRuntimeVersionStatus

`PUT /api/v2/admin/catalog/runtime-versions/{versionId}/status`

权限：`platform_admin`；F：`F03`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `versionId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[CatalogStatusRequest](rest-schemas.md#catalogstatusrequest)；Response：[RuntimeVersion](rest-schemas.md#runtimeversion)

请求顶层字段：`status`: string（必填）；`reason`: string（必填）

响应顶层字段：`id`: OpaqueId（必填）；`name`: string（必填）；`versionLabel`: string（必填）；`artifactDigest`: Digest（必填）；`status`: string（必填）；`runtimeAbiVersion`: string（必填）；`packageFormatVersions`: array<string>（必填）；`defaultForNewBuilds`: boolean（必填）；`admissionReceiptId`: OpaqueId（必填）；`createdAt`: string/date-time（必填）；`publisherNamespaceId`: OpaqueId（必填）；`publisherContractDigest`: Digest（必填）；`publisherContract`: RuntimePublisherContract（必填）；`publisherContractObjectRef`: OpaqueId（必填）

涉及表（规格声明，不自动等于每次都写）：`capability.runtime_versions`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### registerWebuiVersion

`POST /api/v2/admin/catalog/webui-versions`

权限：`platform_admin`；F：`F03`；主要成功状态：`201`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[RegisterWebuiVersionRequest](rest-schemas.md#registerwebuiversionrequest)；Response：[WebuiVersion](rest-schemas.md#webuiversion)

请求顶层字段：`name`: string（必填）；`versionLabel`: string（必填）；`publisherNamespaceId`: OpaqueId（必填）；`publisherContract`: WebuiPublisherContract（必填）；`admissionReceiptId`: OpaqueId（必填）

响应顶层字段：`id`: OpaqueId（必填）；`name`: string（必填）；`versionLabel`: string（必填）；`artifactDigest`: Digest（必填）；`status`: string（必填）；`runtimeAbiVersions`: array<string>（必填）；`uiProtocolVersion`: string（必填）；`admissionReceiptId`: OpaqueId（必填）；`createdAt`: string/date-time（必填）；`publisherNamespaceId`: OpaqueId（必填）；`publisherContractDigest`: Digest（必填）；`publisherContract`: WebuiPublisherContract（必填）；`publisherContractObjectRef`: OpaqueId（必填）

涉及表（规格声明，不自动等于每次都写）：`capability.webui_versions`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### setWebuiVersionStatus

`PUT /api/v2/admin/catalog/webui-versions/{versionId}/status`

权限：`platform_admin`；F：`F03`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `versionId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[CatalogStatusRequest](rest-schemas.md#catalogstatusrequest)；Response：[WebuiVersion](rest-schemas.md#webuiversion)

请求顶层字段：`status`: string（必填）；`reason`: string（必填）

响应顶层字段：`id`: OpaqueId（必填）；`name`: string（必填）；`versionLabel`: string（必填）；`artifactDigest`: Digest（必填）；`status`: string（必填）；`runtimeAbiVersions`: array<string>（必填）；`uiProtocolVersion`: string（必填）；`admissionReceiptId`: OpaqueId（必填）；`createdAt`: string/date-time（必填）；`publisherNamespaceId`: OpaqueId（必填）；`publisherContractDigest`: Digest（必填）；`publisherContract`: WebuiPublisherContract（必填）；`publisherContractObjectRef`: OpaqueId（必填）

涉及表（规格声明，不自动等于每次都写）：`capability.webui_versions`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### getBuildRuntimePolicy

`GET /api/v2/admin/catalog/build-policy`

权限：`platform_admin`；F：`F03`；主要成功状态：`200`。

Body：无独立命名body，见该操作schema；Response：[BuildRuntimePolicy](rest-schemas.md#buildruntimepolicy)

响应顶层字段：`id`: OpaqueId（必填）；`runtimeVersionId`: OpaqueId（必填）；`defaultWebuiVersionId`: OpaqueId（可选）；`policyVersion`: string（必填）；`effectiveAt`: string/date-time（必填）；`createdAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`capability.catalog_policies`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### setBuildRuntimePolicy

`PUT /api/v2/admin/catalog/build-policy`

权限：`platform_admin`；F：`F03`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[SetBuildRuntimePolicyRequest](rest-schemas.md#setbuildruntimepolicyrequest)；Response：[BuildRuntimePolicy](rest-schemas.md#buildruntimepolicy)

请求顶层字段：`runtimeVersionId`: OpaqueId（必填）；`defaultWebuiVersionId`: OpaqueId（可选）；`expectedPolicyVersionId`: OpaqueId（可选）

响应顶层字段：`id`: OpaqueId（必填）；`runtimeVersionId`: OpaqueId（必填）；`defaultWebuiVersionId`: OpaqueId（可选）；`policyVersion`: string（必填）；`effectiveAt`: string/date-time（必填）；`createdAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`capability.catalog_policies`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### listPublisherNamespaces

`GET /api/v2/admin/catalog/publisher-namespaces`

权限：`platform_admin`；F：`F03`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| query | `cursor` | `OpaqueId` | 否 |
| query | `limit` | `integer` | 否 |

Body：无独立命名body，见该操作schema；Response：[PublisherNamespacePage](rest-schemas.md#publishernamespacepage)

响应顶层字段：`items`: array<PublisherNamespace>（必填）；`nextCursor`: OpaqueId（可选）

涉及表（规格声明，不自动等于每次都写）：`capability.publisher_namespaces`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### createPublisherNamespace

`POST /api/v2/admin/catalog/publisher-namespaces`

权限：`platform_admin`；F：`F03`；主要成功状态：`201`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[CreatePublisherNamespaceRequest](rest-schemas.md#createpublishernamespacerequest)；Response：[PublisherNamespace](rest-schemas.md#publishernamespace)

请求顶层字段：`name`: string（必填）；`kind`: string（必填）；`registryId`: OpaqueId（必填）；`repositoryPrefix`: string（必填）；`admissionReceiptId`: OpaqueId（必填）

响应顶层字段：`id`: OpaqueId（必填）；`name`: string（必填）；`kind`: string（必填）；`registryId`: OpaqueId（必填）；`repositoryPrefix`: string（必填）；`admissionReceiptId`: OpaqueId（必填）；`status`: string（必填）；`createdAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`capability.publisher_namespaces`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

### revokePublisherNamespace

`POST /api/v2/admin/catalog/publisher-namespaces/{publisherNamespaceId}/revoke`

权限：`platform_admin`；F：`F03`；主要成功状态：`200`。
| 入参位置 | 字段 | 类型 | 必填 |
| --- | --- | --- | --- |
| path | `publisherNamespaceId` | `OpaqueId` | 是 |
| header | `Idempotency-Key` | `string` | 是 |
| header | `X-CSRF-Token` | `string` | 是 |

Body：[RevokePublisherNamespaceRequest](rest-schemas.md#revokepublishernamespacerequest)；Response：[PublisherNamespace](rest-schemas.md#publishernamespace)

请求顶层字段：`reason`: string（必填）

响应顶层字段：`id`: OpaqueId（必填）；`name`: string（必填）；`kind`: string（必填）；`registryId`: OpaqueId（必填）；`repositoryPrefix`: string（必填）；`admissionReceiptId`: OpaqueId（必填）；`status`: string（必填）；`createdAt`: string/date-time（必填）

涉及表（规格声明，不自动等于每次都写）：`capability.publisher_namespaces`
错误响应：`400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`, `503`

## 3. 本域数据库全字段

`opl_capability`：16 张表，204 列。字段权威：[02](../02_database_schema_complete.md)、[SQL](../contracts/schema.sql)；以下从db_inventory派生。**表不是自动等同DDD聚合根**；事务边界见第1节。


### capability.namespaces

官方空间无零UUID伪Tenant；私有空间经Tenant owner授权

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Namespace/properties/id |
| `tenant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#capability.namespaces.tenant_id |
| `name` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Namespace/properties/name |
| `kind` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.namespaces.kind |
| `description` | `text` | 是 | `—` | 02_database_schema_complete.md#capability.namespaces.description |
| `status` | `text` | 否 | `'active'` | 03_api_contract_complete.yaml#/components/schemas/Namespace/properties/status |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/Namespace/properties/createdAt |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#capability.namespaces.updated_at |

约束：
- `PRIMARY KEY (id)`
- `CHECK (kind IN ('official','tenant_default','tenant_custom'))`
- `CHECK (status IN ('active','archived'))`
- `CHECK ((kind = 'official') = (tenant_id IS NULL))`

索引：
- `{"name": "namespaces_tenant_name", "columns": "tenant_id, name", "unique": true, "where": "tenant_id IS NOT NULL"}`
- `{"name": "namespaces_official_name", "columns": "name", "unique": true, "where": "tenant_id IS NULL"}`
- `{"name": "namespaces_default", "columns": "tenant_id", "unique": true, "where": "kind = 'tenant_default' AND status = 'active'"}`

### capability.packages

visibility与namespace.kind同事务校验；不存最新对象或Build状态

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Package/properties/id |
| `namespace_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Package/properties/namespaceId |
| `name` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Package/properties/name |
| `description` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/Package/properties/description |
| `visibility` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/Package/properties/visibility |
| `status` | `text` | 否 | `'active'` | 03_api_contract_complete.yaml#/components/schemas/Package/properties/status |
| `created_by` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.packages.created_by |
| `archived_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#capability.packages.archived_at |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/Package/properties/createdAt |
| `updated_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/Package/properties/updatedAt |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (namespace_id) REFERENCES capability.namespaces (id) ON DELETE RESTRICT`
- `CHECK (visibility IN ('official','private'))`
- `CHECK (status IN ('active','archived'))`
- `UNIQUE (namespace_id, name)`
- `CHECK ((status = 'archived') = (archived_at IS NOT NULL))`

索引：
- `{"name": "packages_namespace_list", "columns": "namespace_id, created_at DESC, id DESC", "unique": false, "where": null}`

### capability.runtime_versions

Strict PublisherContract schema; repository/digest must match contract and admitted namespace; descriptor is propagated unchanged into Build and execution

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/id |
| `name` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/name |
| `version_label` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/versionLabel |
| `artifact_repository` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.runtime_versions.artifact_repository |
| `artifact_digest` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/artifactDigest |
| `status` | `text` | 否 | `'approved'` | 03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/status |
| `approved_by` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.runtime_versions.approved_by |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/createdAt |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#capability.runtime_versions.updated_at |
| `runtime_abi_version` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/runtimeAbiVersion |
| `package_format_versions` | `text[]` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/packageFormatVersions |
| `admission_receipt_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/admissionReceiptId |
| `publisher_namespace_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/publisherNamespaceId |
| `publisher_contract_digest` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/publisherContractDigest |
| `publisher_contract` | `jsonb` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/publisherContract |
| `publisher_contract_object_ref` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/RuntimeVersion/properties/publisherContractObjectRef |

约束：
- `PRIMARY KEY (id)`
- `CHECK (status IN ('approved','deprecated','revoked'))`
- `UNIQUE (name, version_label)`
- `CHECK (artifact_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK (publisher_contract_digest ~ '^sha256:[0-9a-f]{64}$')`
- `FOREIGN KEY (publisher_namespace_id) REFERENCES capability.publisher_namespaces (id) ON DELETE RESTRICT`
- `CHECK ((jsonb_typeof(publisher_contract) = 'object') IS TRUE)`
- `CHECK ((publisher_contract->>'schemaVersion' = 'opl-publisher-contract/v1') IS TRUE)`
- `CHECK ((publisher_contract->>'kind' = 'runtime') IS TRUE)`
- `CHECK ((publisher_contract->>'publisherNamespaceId' = publisher_namespace_id) IS TRUE)`
- `CHECK ((publisher_contract #>> '{image,repository}' = artifact_repository) IS TRUE)`
- `CHECK ((publisher_contract #>> '{image,digest}' = artifact_digest) IS TRUE)`
- `CHECK ((publisher_contract #>> '{image,platform,os}' = 'linux') IS TRUE)`
- `CHECK ((publisher_contract #>> '{image,platform,architecture}' IN ('amd64','arm64')) IS TRUE)`
- `CHECK ((publisher_contract #>> '{applicationRevisionTemplate,image}' = artifact_repository \|\| '@' \|\| artifact_digest) IS TRUE)`
- `CHECK ((publisher_contract #>> '{applicationRevisionTemplate,platform}' = (publisher_contract #>> '{image,platform,os}') \|\| '/' \|\| (publisher_contract #>> '{image,platform,architecture}') \|\| CASE WHEN publisher_contract #>> '{image,platform,variant}' IS NULL THEN '' ELSE '/' \|\| (publisher_contract #>> '{image,platform,variant}') END) IS TRUE)`
- `CHECK ((publisher_contract->>'runtimeAbiVersion' = runtime_abi_version) IS TRUE)`
- `CHECK ((publisher_contract->'packageFormatVersions' = to_jsonb(package_format_versions)) IS TRUE)`

索引：
- `{"name": "runtime_versions_status", "columns": "status, created_at DESC, id DESC", "unique": false, "where": null}`
- `{"name": "runtime_versions_publisher", "columns": "publisher_namespace_id", "unique": false, "where": null}`

### capability.webui_versions

Strict PublisherContract schema; repository/digest must match contract and admitted namespace; descriptor is propagated unchanged into Build and execution

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/id |
| `name` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/name |
| `version_label` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/versionLabel |
| `artifact_repository` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.webui_versions.artifact_repository |
| `artifact_digest` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/artifactDigest |
| `status` | `text` | 否 | `'approved'` | 03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/status |
| `approved_by` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.webui_versions.approved_by |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/createdAt |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#capability.webui_versions.updated_at |
| `runtime_abi_versions` | `text[]` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/runtimeAbiVersions |
| `ui_protocol_version` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/uiProtocolVersion |
| `admission_receipt_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/admissionReceiptId |
| `publisher_namespace_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/publisherNamespaceId |
| `publisher_contract_digest` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/publisherContractDigest |
| `publisher_contract` | `jsonb` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/publisherContract |
| `publisher_contract_object_ref` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/WebuiVersion/properties/publisherContractObjectRef |

约束：
- `PRIMARY KEY (id)`
- `CHECK (status IN ('approved','deprecated','revoked'))`
- `UNIQUE (name, version_label)`
- `CHECK (artifact_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK (publisher_contract_digest ~ '^sha256:[0-9a-f]{64}$')`
- `FOREIGN KEY (publisher_namespace_id) REFERENCES capability.publisher_namespaces (id) ON DELETE RESTRICT`
- `CHECK ((jsonb_typeof(publisher_contract) = 'object') IS TRUE)`
- `CHECK ((publisher_contract->>'schemaVersion' = 'opl-publisher-contract/v1') IS TRUE)`
- `CHECK ((publisher_contract->>'kind' = 'webui') IS TRUE)`
- `CHECK ((publisher_contract->>'publisherNamespaceId' = publisher_namespace_id) IS TRUE)`
- `CHECK ((publisher_contract #>> '{image,repository}' = artifact_repository) IS TRUE)`
- `CHECK ((publisher_contract #>> '{image,digest}' = artifact_digest) IS TRUE)`
- `CHECK ((publisher_contract #>> '{image,platform,os}' = 'linux') IS TRUE)`
- `CHECK ((publisher_contract #>> '{image,platform,architecture}' IN ('amd64','arm64')) IS TRUE)`
- `CHECK ((publisher_contract->'runtimeAbiVersions' = to_jsonb(runtime_abi_versions)) IS TRUE)`
- `CHECK ((publisher_contract->>'uiProtocolVersion' = ui_protocol_version) IS TRUE)`

索引：
- `{"name": "webui_versions_status", "columns": "status, created_at DESC, id DESC", "unique": false, "where": null}`
- `{"name": "webui_versions_publisher", "columns": "publisher_namespace_id", "unique": false, "where": null}`

### capability.catalog_policies

按当前生效不可变策略选择默认Runtime/WebUI，不多处写is_default

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/BuildRuntimePolicy/properties/id |
| `runtime_version_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/BuildRuntimePolicy/properties/runtimeVersionId |
| `default_webui_version_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/BuildRuntimePolicy/properties/defaultWebuiVersionId |
| `policy_version` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/BuildRuntimePolicy/properties/policyVersion |
| `published_by` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.catalog_policies.published_by |
| `effective_at` | `timestamptz` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/BuildRuntimePolicy/properties/effectiveAt |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/BuildRuntimePolicy/properties/createdAt |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (runtime_version_id) REFERENCES capability.runtime_versions (id) ON DELETE RESTRICT`
- `FOREIGN KEY (default_webui_version_id) REFERENCES capability.webui_versions (id) ON DELETE RESTRICT`
- `UNIQUE (policy_version)`

索引：
- `{"name": "catalog_policies_effective", "columns": "effective_at DESC, id DESC", "unique": false, "where": null}`

### capability.package_versions

上传申请冻结sha256/size，实测相符才uploaded；构建状态只在Build

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PackageVersion/properties/id |
| `package_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PackageVersion/properties/packageId |
| `version_label` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PackageVersion/properties/versionLabel |
| `status` | `text` | 否 | `'upload_pending'` | 03_api_contract_complete.yaml#/components/schemas/PackageVersion/properties/status |
| `sha256` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PackageVersion/properties/sha256; 03_api_contract_complete.yaml#/components/schemas/UploadSession/properties/sha256 |
| `size_bytes` | `bigint` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PackageVersion/properties/sizeBytes; 03_api_contract_complete.yaml#/components/schemas/UploadSession/properties/sizeBytes |
| `object_ref` | `text` | 是 | `—` | 02_database_schema_complete.md#capability.package_versions.object_ref |
| `manifest` | `jsonb` | 是 | `—` | 02_database_schema_complete.md#capability.package_versions.manifest |
| `validation_error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#capability.package_versions.validation_error_code |
| `created_by` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.package_versions.created_by |
| `verified_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#capability.package_versions.verified_at |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/PackageVersion/properties/createdAt |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#capability.package_versions.updated_at |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (package_id) REFERENCES capability.packages (id) ON DELETE RESTRICT`
- `CHECK (status IN ('upload_pending','uploaded','rejected'))`
- `UNIQUE (package_id, version_label)`
- `UNIQUE (id, package_id)`
- `CHECK (sha256 ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK (size_bytes > 0)`
- `CHECK (status <> 'uploaded' OR (object_ref IS NOT NULL AND verified_at IS NOT NULL AND manifest IS NOT NULL))`

索引：
- `{"name": "package_versions_list", "columns": "package_id, created_at DESC, id DESC", "unique": false, "where": null}`

### capability.upload_sessions

Storage multipart直传凭据由Capability签发；sizeBytes/sha256从PackageVersion、completedParts从已确认upload_chunks读回，不双写数组

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/UploadSession/properties/id |
| `package_version_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/UploadSession/properties/packageVersionId |
| `status` | `text` | 否 | `'uploading'` | 03_api_contract_complete.yaml#/components/schemas/UploadSession/properties/status |
| `part_size_bytes` | `bigint` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/UploadSession/properties/partSizeBytes |
| `object_ref` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.upload_sessions.object_ref |
| `provider_upload_ref` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.upload_sessions.provider_upload_ref |
| `expires_at` | `timestamptz` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/UploadSession/properties/expiresAt |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#capability.upload_sessions.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#capability.upload_sessions.updated_at |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (package_version_id) REFERENCES capability.package_versions (id) ON DELETE RESTRICT`
- `CHECK (status IN ('uploading','completed','expired'))`
- `CHECK (part_size_bytes > 0)`

索引：
- `{"name": "upload_sessions_active", "columns": "package_version_id", "unique": true, "where": "status = 'uploading'"}`

### capability.upload_chunks

同session/partNumber固定sha256与size；确认后保存etag；unknown读取同Provider part，重签URL不创建第二分片

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.upload_chunks.id |
| `upload_session_id` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.upload_chunks.upload_session_id |
| `size_bytes` | `bigint` | 否 | `—` | 02_database_schema_complete.md#capability.upload_chunks.size_bytes |
| `sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.upload_chunks.sha256 |
| `provider_part_ref` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.upload_chunks.provider_part_ref |
| `observation_result` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.upload_chunks.observation_result |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#capability.upload_chunks.created_at |
| `part_number` | `integer` | 否 | `—` | 02_database_schema_complete.md#capability.upload_chunks.part_number |
| `etag` | `text` | 是 | `—` | 02_database_schema_complete.md#capability.upload_chunks.etag |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (upload_session_id) REFERENCES capability.upload_sessions (id) ON DELETE RESTRICT`
- `CHECK (sha256 ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`
- `UNIQUE (upload_session_id, part_number)`
- `CHECK (part_number > 0)`
- `CHECK (size_bytes > 0)`
- `CHECK (observation_result <> 'confirmed' OR etag IS NOT NULL)`

索引：
- `{"name": "upload_chunks_session", "columns": "upload_session_id", "unique": false, "where": null}`

### capability.capability_versions

build引用五项齐全才ready；legacy_application保留旧exact revision/digest且五个构建引用全空，不伪造Package/Build；deleted仅墓碑

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/id |
| `package_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/packageId |
| `package_version_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/packageVersionId |
| `build_job_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/buildJobId |
| `version_label` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/versionLabel |
| `runtime_version_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/runtimeVersionId |
| `webui_version_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/webuiVersionId |
| `artifact_repository` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.capability_versions.artifact_repository |
| `artifact_digest` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/artifactDigest |
| `status` | `text` | 否 | `'ready'` | 03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/status |
| `model_requirements` | `jsonb` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/modelRequirements |
| `data_compatibility` | `jsonb` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/dataCompatibility |
| `provenance_evidence` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#capability.capability_versions.provenance_evidence |
| `deletion_operation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#capability.capability_versions.deletion_operation_id |
| `deleted_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#capability.capability_versions.deleted_at |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/createdAt |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#capability.capability_versions.updated_at |
| `provenance` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/provenance |
| `legacy_application_revision_id` | `text` | 是 | `—` | 03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/legacyApplicationRevisionId |
| `deployment_descriptor` | `jsonb` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/deploymentDescriptor |
| `deployment_descriptor_digest` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/deploymentDescriptorDigest |
| `deployment_descriptor_object_ref` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/CapabilityVersion/properties/deploymentDescriptorObjectRef |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (package_id) REFERENCES capability.packages (id) ON DELETE RESTRICT`
- `FOREIGN KEY (package_version_id, package_id) REFERENCES capability.package_versions (id, package_id) ON DELETE RESTRICT`
- `FOREIGN KEY (runtime_version_id) REFERENCES capability.runtime_versions (id) ON DELETE RESTRICT`
- `FOREIGN KEY (webui_version_id) REFERENCES capability.webui_versions (id) ON DELETE RESTRICT`
- `CHECK (status IN ('ready','deprecated','deleting','deleted'))`
- `UNIQUE (build_job_id)`
- `CHECK (artifact_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK ((status = 'deleted') = (deleted_at IS NOT NULL))`
- `FOREIGN KEY (deletion_operation_id) REFERENCES capability.operations (id) ON DELETE RESTRICT`
- `CHECK (provenance IN ('build','legacy_application'))`
- `CHECK ((provenance = 'build' AND num_nonnulls(package_id, package_version_id, build_job_id, runtime_version_id, webui_version_id) = 5 AND legacy_application_revision_id IS NULL) OR (provenance = 'legacy_application' AND num_nonnulls(package_id, package_version_id, build_job_id, runtime_version_id, webui_version_id) = 0 AND legacy_application_revision_id IS NOT NULL))`
- `CHECK (deployment_descriptor_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK ((deployment_descriptor->>'schemaVersion' = 'opl-deployment-descriptor/v1') IS TRUE)`
- `CHECK ((deployment_descriptor #>> '{artifact,repository}' = artifact_repository) IS TRUE)`
- `CHECK ((deployment_descriptor #>> '{artifact,digest}' = artifact_digest) IS TRUE)`
- `CHECK ((deployment_descriptor #>> '{applicationRevision,image}' = artifact_repository \|\| '@' \|\| artifact_digest) IS TRUE)`
- `CHECK ((deployment_descriptor->>'provenance' = provenance) IS TRUE)`
- `CHECK (provenance <> 'legacy_application' OR ((deployment_descriptor->>'legacyApplicationRevisionId' = legacy_application_revision_id AND NOT (deployment_descriptor ?\| ARRAY['packageVersionId','buildInputDigest','runtimeContract','runtimeContractReference','webuiContract','webuiContractReference'])) IS TRUE))`
- `CHECK (provenance <> 'build' OR ((deployment_descriptor->>'packageVersionId' = package_version_id AND deployment_descriptor #>> '{runtimeContractReference,versionId}' = runtime_version_id AND deployment_descriptor #>> '{webuiContractReference,versionId}' = webui_version_id) IS TRUE))`

索引：
- `{"name": "capability_versions_list", "columns": "package_id, created_at DESC, id DESC", "unique": false, "where": null}`
- `{"name": "capability_versions_artifact", "columns": "artifact_repository, artifact_digest", "unique": false, "where": null}`
- `{"name": "capability_versions_input", "columns": "package_version_id", "unique": false, "where": null}`
- `{"name": "capability_versions_legacy", "columns": "legacy_application_revision_id", "unique": true, "where": "legacy_application_revision_id IS NOT NULL"}`

### capability.reference_claims

Four-way ReferenceTarget maps to exactly one local FK; Bind records original operation/input digest; Release requires typed owner terminal receipt and confirmed readback

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.reference_claims.id |
| `target_type` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.reference_claims.target_type |
| `package_version_id` | `text` | 是 | `—` | 02_database_schema_complete.md#capability.reference_claims.package_version_id |
| `capability_version_id` | `text` | 是 | `—` | 02_database_schema_complete.md#capability.reference_claims.capability_version_id |
| `runtime_version_id` | `text` | 是 | `—` | 02_database_schema_complete.md#capability.reference_claims.runtime_version_id |
| `webui_version_id` | `text` | 是 | `—` | 02_database_schema_complete.md#capability.reference_claims.webui_version_id |
| `claimant_owner` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.reference_claims.claimant_owner |
| `claimant_resource_id` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.reference_claims.claimant_resource_id |
| `purpose` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.reference_claims.purpose |
| `request_id` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.reference_claims.request_id |
| `bound_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#capability.reference_claims.bound_at |
| `released_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#capability.reference_claims.released_at |
| `release_evidence_ref` | `text` | 是 | `—` | 02_database_schema_complete.md#capability.reference_claims.release_evidence_ref |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#capability.reference_claims.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#capability.reference_claims.updated_at |
| `bound_operation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#capability.reference_claims.bound_operation_id |
| `bound_input_digest` | `text` | 是 | `—` | 02_database_schema_complete.md#capability.reference_claims.bound_input_digest |
| `release_evidence` | `jsonb` | 是 | `—` | 02_database_schema_complete.md#capability.reference_claims.release_evidence |

约束：
- `PRIMARY KEY (id)`
- `CHECK (target_type IN ('package_version','capability_version','runtime_version','webui_version'))`
- `CHECK (num_nonnulls(package_version_id, capability_version_id, runtime_version_id, webui_version_id) = 1)`
- `CHECK (released_at IS NULL OR release_evidence_ref IS NOT NULL)`
- `FOREIGN KEY (package_version_id) REFERENCES capability.package_versions (id) ON DELETE RESTRICT`
- `CHECK ((target_type = 'package_version') = (package_version_id IS NOT NULL))`
- `FOREIGN KEY (capability_version_id) REFERENCES capability.capability_versions (id) ON DELETE RESTRICT`
- `CHECK ((target_type = 'capability_version') = (capability_version_id IS NOT NULL))`
- `FOREIGN KEY (runtime_version_id) REFERENCES capability.runtime_versions (id) ON DELETE RESTRICT`
- `CHECK ((target_type = 'runtime_version') = (runtime_version_id IS NOT NULL))`
- `FOREIGN KEY (webui_version_id) REFERENCES capability.webui_versions (id) ON DELETE RESTRICT`
- `CHECK ((target_type = 'webui_version') = (webui_version_id IS NOT NULL))`
- `CHECK ((bound_at IS NULL AND bound_operation_id IS NULL AND bound_input_digest IS NULL) OR (bound_at IS NOT NULL AND bound_operation_id IS NOT NULL AND bound_input_digest IS NOT NULL))`
- `CHECK (bound_input_digest IS NULL OR bound_input_digest ~ '^sha256:[0-9a-f]{64}$')`
- `CHECK ((released_at IS NULL) = (release_evidence IS NULL))`

索引：
- `{"name": "reference_claims_package_version", "columns": "package_version_id, claimant_owner, claimant_resource_id, purpose", "unique": true, "where": "released_at IS NULL AND package_version_id IS NOT NULL"}`
- `{"name": "reference_claims_capability_version", "columns": "capability_version_id, claimant_owner, claimant_resource_id, purpose", "unique": true, "where": "released_at IS NULL AND capability_version_id IS NOT NULL"}`
- `{"name": "reference_claims_runtime_version", "columns": "runtime_version_id, claimant_owner, claimant_resource_id, purpose", "unique": true, "where": "released_at IS NULL AND runtime_version_id IS NOT NULL"}`
- `{"name": "reference_claims_webui_version", "columns": "webui_version_id, claimant_owner, claimant_resource_id, purpose", "unique": true, "where": "released_at IS NULL AND webui_version_id IS NOT NULL"}`

### capability.outbox_events

本Owner状态事务同写immutable事件；revision只在本Owner聚合锁内分配

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.outbox_events.id |
| `event_type` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.outbox_events.event_type |
| `schema_version` | `integer` | 否 | `—` | 02_database_schema_complete.md#capability.outbox_events.schema_version |
| `aggregate_type` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.outbox_events.aggregate_type |
| `aggregate_id` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.outbox_events.aggregate_id |
| `aggregate_revision` | `bigint` | 否 | `—` | 02_database_schema_complete.md#capability.outbox_events.aggregate_revision |
| `tenant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#capability.outbox_events.tenant_id |
| `correlation_id` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.outbox_events.correlation_id |
| `causation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#capability.outbox_events.causation_id |
| `payload` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#capability.outbox_events.payload |
| `payload_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.outbox_events.payload_sha256 |
| `occurred_at` | `timestamptz` | 否 | `—` | 02_database_schema_complete.md#capability.outbox_events.occurred_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#capability.outbox_events.created_at |

约束：
- `PRIMARY KEY (id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`
- `UNIQUE (aggregate_type, aggregate_id, aggregate_revision, event_type)`

索引：
- `{"name": "outbox_events_aggregate", "columns": "aggregate_type, aggregate_id, aggregate_revision", "unique": false, "where": null}`

### capability.outbox_deliveries

各consumer独立ACK，禁止单个ACK丢其他消费者事件；lease仅本库

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.outbox_deliveries.id |
| `event_id` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.outbox_deliveries.event_id |
| `consumer_owner` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.outbox_deliveries.consumer_owner |
| `attempt_count` | `integer` | 否 | `0` | 02_database_schema_complete.md#capability.outbox_deliveries.attempt_count |
| `next_attempt_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#capability.outbox_deliveries.next_attempt_at |
| `acknowledged_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#capability.outbox_deliveries.acknowledged_at |
| `last_error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#capability.outbox_deliveries.last_error_code |
| `lease_token` | `text` | 是 | `—` | 02_database_schema_complete.md#capability.outbox_deliveries.lease_token |
| `lease_until` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#capability.outbox_deliveries.lease_until |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#capability.outbox_deliveries.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#capability.outbox_deliveries.updated_at |

约束：
- `PRIMARY KEY (id)`
- `FOREIGN KEY (event_id) REFERENCES capability.outbox_events (id) ON DELETE RESTRICT`
- `UNIQUE (event_id, consumer_owner)`
- `CHECK (attempt_count >= 0)`
- `CHECK ((lease_token IS NULL) = (lease_until IS NULL))`

索引：
- `{"name": "outbox_deliveries_pending", "columns": "next_attempt_at, id", "unique": false, "where": "acknowledged_at IS NULL"}`

### capability.inbox_events

去重hash/业务状态/processed_at/后续Outbox同本库事务；乱序不能覆盖新版本

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.inbox_events.id |
| `source_owner` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.inbox_events.source_owner |
| `source_event_id` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.inbox_events.source_event_id |
| `event_type` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.inbox_events.event_type |
| `schema_version` | `integer` | 否 | `—` | 02_database_schema_complete.md#capability.inbox_events.schema_version |
| `aggregate_type` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.inbox_events.aggregate_type |
| `aggregate_id` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.inbox_events.aggregate_id |
| `aggregate_revision` | `bigint` | 否 | `—` | 02_database_schema_complete.md#capability.inbox_events.aggregate_revision |
| `payload_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.inbox_events.payload_sha256 |
| `payload` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#capability.inbox_events.payload |
| `received_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#capability.inbox_events.received_at |
| `processed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#capability.inbox_events.processed_at |
| `result_resource_id` | `text` | 是 | `—` | 02_database_schema_complete.md#capability.inbox_events.result_resource_id |
| `error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#capability.inbox_events.error_code |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (source_owner, source_event_id)`
- `CHECK (schema_version > 0 AND aggregate_revision >= 0)`
- `CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')`

索引：
- `{"name": "inbox_events_pending", "columns": "received_at, id", "unique": false, "where": "processed_at IS NULL"}`
- `{"name": "inbox_events_aggregate", "columns": "source_owner, aggregate_type, aggregate_id, aggregate_revision", "unique": false, "where": null}`

### capability.idempotency_records

命令创建同事务；operation_name是API操作名不是新Operation ID；安全响应无Key明文；义务存续无TTL清理

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.idempotency_records.id |
| `tenant_scope` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.idempotency_records.tenant_scope |
| `actor_scope` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.idempotency_records.actor_scope |
| `operation_name` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.idempotency_records.operation_name |
| `idempotency_key` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.idempotency_records.idempotency_key |
| `request_sha256` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.idempotency_records.request_sha256 |
| `resource_id` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.idempotency_records.resource_id |
| `operation_id` | `text` | 是 | `—` | 02_database_schema_complete.md#capability.idempotency_records.operation_id |
| `response_status` | `integer` | 否 | `—` | 02_database_schema_complete.md#capability.idempotency_records.response_status |
| `response_body` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#capability.idempotency_records.response_body |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#capability.idempotency_records.created_at |

约束：
- `PRIMARY KEY (id)`
- `UNIQUE (tenant_scope, actor_scope, operation_name, idempotency_key)`
- `CHECK (request_sha256 ~ '^[0-9a-f]{64}$')`
- `CHECK (response_status BETWEEN 100 AND 599)`

索引：
- `{"name": "idempotency_records_resource", "columns": "resource_id", "unique": false, "where": null}`

### capability.operations

目标Owner持异步Operation；BFF路由无中央writer；Build创建201回Job；只有Workspace配Saga

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.operations.id |
| `tenant_id` | `text` | 是 | `—` | 02_database_schema_complete.md#capability.operations.tenant_id |
| `actor_id` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.operations.actor_id |
| `kind` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.operations.kind |
| `resource_id` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.operations.resource_id |
| `status` | `text` | 否 | `'accepted'` | 02_database_schema_complete.md#capability.operations.status |
| `stage` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.operations.stage |
| `error_code` | `text` | 是 | `—` | 02_database_schema_complete.md#capability.operations.error_code |
| `observation_result` | `text` | 是 | `—` | 02_database_schema_complete.md#capability.operations.observation_result |
| `request_id` | `text` | 否 | `—` | 02_database_schema_complete.md#capability.operations.request_id |
| `accepted_input` | `jsonb` | 否 | `—` | 02_database_schema_complete.md#capability.operations.accepted_input |
| `result` | `jsonb` | 是 | `—` | 02_database_schema_complete.md#capability.operations.result |
| `worker_lease_token` | `text` | 是 | `—` | 02_database_schema_complete.md#capability.operations.worker_lease_token |
| `worker_lease_until` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#capability.operations.worker_lease_until |
| `started_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#capability.operations.started_at |
| `completed_at` | `timestamptz` | 是 | `—` | 02_database_schema_complete.md#capability.operations.completed_at |
| `created_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#capability.operations.created_at |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#capability.operations.updated_at |

约束：
- `PRIMARY KEY (id)`
- `CHECK (status IN ('accepted','running','awaiting_confirmation','succeeded','failed','needs_attention','cancelled'))`
- `CHECK (observation_result IN ('confirmed','rejected','unknown'))`
- `CHECK ((worker_lease_token IS NULL) = (worker_lease_until IS NULL))`
- `CHECK (status <> 'succeeded' OR completed_at IS NOT NULL)`

索引：
- `{"name": "operations_resource", "columns": "resource_id, created_at DESC, id DESC", "unique": false, "where": null}`
- `{"name": "operations_tenant_list", "columns": "tenant_id, created_at DESC, id DESC", "unique": false, "where": null}`
- `{"name": "operations_recovery", "columns": "status, updated_at", "unique": false, "where": null}`

### capability.publisher_namespaces

Publisher Registry namespaces are distinct from Tenant Agent groups; third-party publishers use separately admitted prefixes

| 列 | SQL类型 | 可NULL | 默认值 | API血缘 / 原始来源 |
| --- | --- | --- | --- | --- |
| `id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PublisherNamespace/properties/id |
| `name` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PublisherNamespace/properties/name |
| `kind` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PublisherNamespace/properties/kind |
| `registry_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PublisherNamespace/properties/registryId |
| `repository_prefix` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PublisherNamespace/properties/repositoryPrefix |
| `admission_receipt_id` | `text` | 否 | `—` | 03_api_contract_complete.yaml#/components/schemas/PublisherNamespace/properties/admissionReceiptId |
| `status` | `text` | 否 | `'approved'` | 03_api_contract_complete.yaml#/components/schemas/PublisherNamespace/properties/status |
| `created_at` | `timestamptz` | 否 | `now()` | 03_api_contract_complete.yaml#/components/schemas/PublisherNamespace/properties/createdAt |
| `updated_at` | `timestamptz` | 否 | `now()` | 02_database_schema_complete.md#capability.publisher_namespaces.updated_at |

约束：
- `PRIMARY KEY (id)`
- `CHECK (kind IN ('official','third_party'))`
- `CHECK (status IN ('approved','revoked'))`
- `UNIQUE (registry_id, repository_prefix)`
- `UNIQUE (name)`
- `CHECK (repository_prefix <> '' AND repository_prefix NOT LIKE '%..%' AND repository_prefix NOT LIKE '%@%' AND repository_prefix NOT LIKE '%://%')`

索引：
- `{"name": "publisher_namespaces_catalog", "columns": "kind, status, created_at DESC, id DESC", "unique": false, "where": null}`

### 本Owner应实现的RPC方法全集（目标，不是挂载证据）

共享OwnerOperations/CommitReadback/Inbox分别由本域实现，不形成中央业务服务；通用Operation REST由BFF按显式Owner路由；本域只负责自己的Operation与授权。

| RPC | 请求 | 响应 |
| --- | --- | --- |
| `CapabilityProductService.ListNamespaces` | [ListNamespacesRpcRequest](rpc-messages.md#listnamespacesrpcrequest) | [NamespacePage](rpc-messages.md#namespacepage) |
| `CapabilityProductService.CreateNamespace` | [CreateNamespaceRpcRequest](rpc-messages.md#createnamespacerpcrequest) | [Namespace](rpc-messages.md#namespace) |
| `CapabilityProductService.UpdateNamespace` | [UpdateNamespaceRpcRequest](rpc-messages.md#updatenamespacerpcrequest) | [Namespace](rpc-messages.md#namespace) |
| `CapabilityProductService.ArchiveNamespace` | [ArchiveNamespaceRpcRequest](rpc-messages.md#archivenamespacerpcrequest) | [Namespace](rpc-messages.md#namespace) |
| `CapabilityProductService.ListPackages` | [ListPackagesRpcRequest](rpc-messages.md#listpackagesrpcrequest) | [PackagePage](rpc-messages.md#packagepage) |
| `CapabilityProductService.CreatePackage` | [CreatePackageRpcRequest](rpc-messages.md#createpackagerpcrequest) | [Package](rpc-messages.md#package) |
| `CapabilityProductService.GetPackage` | [GetPackageRpcRequest](rpc-messages.md#getpackagerpcrequest) | [Package](rpc-messages.md#package) |
| `CapabilityProductService.UpdatePackage` | [UpdatePackageRpcRequest](rpc-messages.md#updatepackagerpcrequest) | [Package](rpc-messages.md#package) |
| `CapabilityProductService.ArchivePackage` | [ArchivePackageRpcRequest](rpc-messages.md#archivepackagerpcrequest) | [Package](rpc-messages.md#package) |
| `CapabilityProductService.CreateUpload` | [CreateUploadRpcRequest](rpc-messages.md#createuploadrpcrequest) | [UploadSession](rpc-messages.md#uploadsession) |
| `CapabilityProductService.GetUpload` | [GetUploadRpcRequest](rpc-messages.md#getuploadrpcrequest) | [UploadSession](rpc-messages.md#uploadsession) |
| `CapabilityProductService.CreateUploadPart` | [CreateUploadPartRpcRequest](rpc-messages.md#createuploadpartrpcrequest) | [UploadPartAuthorization](rpc-messages.md#uploadpartauthorization) |
| `CapabilityProductService.CompleteUpload` | [CompleteUploadRpcRequest](rpc-messages.md#completeuploadrpcrequest) | [Operation](rpc-messages.md#operation) |
| `CapabilityProductService.ListPackageVersions` | [ListPackageVersionsRpcRequest](rpc-messages.md#listpackageversionsrpcrequest) | [PackageVersionPage](rpc-messages.md#packageversionpage) |
| `CapabilityProductService.GetPackageVersion` | [GetPackageVersionRpcRequest](rpc-messages.md#getpackageversionrpcrequest) | [PackageVersion](rpc-messages.md#packageversion) |
| `CapabilityProductService.ListCapabilityVersions` | [ListCapabilityVersionsRpcRequest](rpc-messages.md#listcapabilityversionsrpcrequest) | [CapabilityVersionPage](rpc-messages.md#capabilityversionpage) |
| `CapabilityProductService.GetCapabilityVersion` | [GetCapabilityVersionRpcRequest](rpc-messages.md#getcapabilityversionrpcrequest) | [CapabilityVersion](rpc-messages.md#capabilityversion) |
| `CapabilityProductService.DeleteCapabilityVersion` | [DeleteCapabilityVersionRpcRequest](rpc-messages.md#deletecapabilityversionrpcrequest) | [Operation](rpc-messages.md#operation) |
| `CapabilityProductService.PublishOfficialPackage` | [PublishOfficialPackageRpcRequest](rpc-messages.md#publishofficialpackagerpcrequest) | [Package](rpc-messages.md#package) |
| `CapabilityProductService.ListRuntimeVersions` | [ListRuntimeVersionsRpcRequest](rpc-messages.md#listruntimeversionsrpcrequest) | [RuntimeVersionPage](rpc-messages.md#runtimeversionpage) |
| `CapabilityProductService.ListWebuiVersions` | [ListWebuiVersionsRpcRequest](rpc-messages.md#listwebuiversionsrpcrequest) | [WebuiVersionPage](rpc-messages.md#webuiversionpage) |
| `CapabilityProductService.RegisterRuntimeVersion` | [RegisterRuntimeVersionRpcRequest](rpc-messages.md#registerruntimeversionrpcrequest) | [RuntimeVersion](rpc-messages.md#runtimeversion) |
| `CapabilityProductService.SetRuntimeVersionStatus` | [SetRuntimeVersionStatusRpcRequest](rpc-messages.md#setruntimeversionstatusrpcrequest) | [RuntimeVersion](rpc-messages.md#runtimeversion) |
| `CapabilityProductService.RegisterWebuiVersion` | [RegisterWebuiVersionRpcRequest](rpc-messages.md#registerwebuiversionrpcrequest) | [WebuiVersion](rpc-messages.md#webuiversion) |
| `CapabilityProductService.SetWebuiVersionStatus` | [SetWebuiVersionStatusRpcRequest](rpc-messages.md#setwebuiversionstatusrpcrequest) | [WebuiVersion](rpc-messages.md#webuiversion) |
| `CapabilityProductService.GetBuildRuntimePolicy` | [GetBuildRuntimePolicyRpcRequest](rpc-messages.md#getbuildruntimepolicyrpcrequest) | [BuildRuntimePolicy](rpc-messages.md#buildruntimepolicy) |
| `CapabilityProductService.SetBuildRuntimePolicy` | [SetBuildRuntimePolicyRpcRequest](rpc-messages.md#setbuildruntimepolicyrpcrequest) | [BuildRuntimePolicy](rpc-messages.md#buildruntimepolicy) |
| `CapabilityProductService.ListPublisherNamespaces` | [ListPublisherNamespacesRpcRequest](rpc-messages.md#listpublishernamespacesrpcrequest) | [PublisherNamespacePage](rpc-messages.md#publishernamespacepage) |
| `CapabilityProductService.CreatePublisherNamespace` | [CreatePublisherNamespaceRpcRequest](rpc-messages.md#createpublishernamespacerpcrequest) | [PublisherNamespace](rpc-messages.md#publishernamespace) |
| `CapabilityProductService.RevokePublisherNamespace` | [RevokePublisherNamespaceRpcRequest](rpc-messages.md#revokepublishernamespacerpcrequest) | [PublisherNamespace](rpc-messages.md#publishernamespace) |
| `CapabilityCoordination.ResolveBuildInput` | [BuildInputRequest](rpc-messages.md#buildinputrequest) | [BuildInputSnapshot](rpc-messages.md#buildinputsnapshot) |
| `CapabilityCoordination.ResolvePublisherContract` | [ResolvePublisherContractRequest](rpc-messages.md#resolvepublishercontractrequest) | [ResolvedPublisherContract](rpc-messages.md#resolvedpublishercontract) |
| `CapabilityCoordination.AcquireReference` | [ReferenceClaimRequest](rpc-messages.md#referenceclaimrequest) | [ReferenceClaim](rpc-messages.md#referenceclaim) |
| `CapabilityCoordination.BindReference` | [BindReferenceRequest](rpc-messages.md#bindreferencerequest) | [ReferenceClaim](rpc-messages.md#referenceclaim) |
| `CapabilityCoordination.ReleaseReference` | [ReleaseReferenceRequest](rpc-messages.md#releasereferencerequest) | [ReferenceClaim](rpc-messages.md#referenceclaim) |
| `OwnerOperations.Read` | [OwnerOperationRequest](rpc-messages.md#owneroperationrequest) | [Operation](rpc-messages.md#operation) |
| `OwnerOperations.Reconcile` | [ReconcileOperationRpcRequest](rpc-messages.md#reconcileoperationrpcrequest) | [Operation](rpc-messages.md#operation) |
| `OwnerCommitReadback.ReadOwnerCommit` | [ReadOwnerCommitRequest](rpc-messages.md#readownercommitrequest) | [OwnerCommitEvidence](rpc-messages.md#ownercommitevidence) |
| `DomainInbox.Deliver` | [DeliverEventRequest](rpc-messages.md#delivereventrequest) | [InboxAck](rpc-messages.md#inboxack) |

## 4. 跨域调用：调用者 → 拥有方 → 字段 → 结果

以下只列`domain_flows.json`声明的业务边；共享通道/尚无业务边的RPC不能推断成已经实现。


### F02.1 capability → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

### F02.2 bff → capability / CapabilityProductService.CreateNamespace

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [CreateNamespaceRpcRequest](rpc-messages.md#createnamespacerpcrequest)：context#1: CallContext；body#2: NamespaceWriteRequest

返回 [Namespace](rpc-messages.md#namespace)：id#1: string；name#2: string；is_default#3: bool；status#4: NamespaceStatusEnum；created_at#5: Timestamp

接收方写入：`capability.namespaces`

完成证据：tenant+name唯一、正确权限和状态

失败/未知：冲突不创建第二组；不跨Tenant读写

### F03.1 capability → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

### F03.2 bff → capability / CapabilityProductService.CreatePublisherNamespace

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [CreatePublisherNamespaceRpcRequest](rpc-messages.md#createpublishernamespacerpcrequest)：context#1: CallContext；body#2: CreatePublisherNamespaceRequest

返回 [PublisherNamespace](rpc-messages.md#publishernamespace)：id#1: string；name#2: string；kind#3: PublisherNamespaceKindEnum；registry_id#4: string；repository_prefix#5: string；admission_receipt_id#6: string；status#7: PublisherNamespaceStatusEnum；created_at#8: Timestamp

接收方写入：`capability.publisher_namespaces`

完成证据：官方/第三方种类与Registry prefix确认

失败/未知：错误prefix/归属拒绝，不放进默认官方空间

### F03.3 bff → capability / CapabilityProductService.RegisterRuntimeVersion

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [RegisterRuntimeVersionRpcRequest](rpc-messages.md#registerruntimeversionrpcrequest)：context#1: CallContext；body#2: RegisterRuntimeVersionRequest

返回 [RuntimeVersion](rpc-messages.md#runtimeversion)：id#1: string；name#2: string；version_label#3: string；artifact_digest#4: string；status#5: RuntimeVersionStatusEnum；runtime_abi_version#6: string；package_format_versions#7: repeated string；default_for_new_builds#8: bool；admission_receipt_id#9: string；created_at#10: Timestamp；publisher_namespace_id#11: string；publisher_contract_digest#12: string；publisher_contract#13: RuntimePublisherContract；publisher_contract_object_ref#14: string

接收方写入：`capability.runtime_versions`

完成证据：完整PublisherContract schema+canonical Go validator+Registry读回通过

失败/未知：缺repository/platform/完整revision拒绝

### F04.1 capability → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

### F04.2 bff → capability / CapabilityProductService.CreateUpload

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [CreateUploadRpcRequest](rpc-messages.md#createuploadrpcrequest)：context#1: CallContext；body#2: CreateUploadRequest；package_id#3: string

返回 [UploadSession](rpc-messages.md#uploadsession)：id#1: string；package_version_id#2: string；status#3: UploadSessionStatusEnum；size_bytes#4: int64；sha256#5: string；part_size_bytes#6: int64；completed_parts#7: repeated UploadPart；expires_at#8: Timestamp

接收方写入：`capability.package_versions`, `capability.upload_sessions`

完成证据：固定包版本及uploadId/受限对象地址

失败/未知：同键返回同身份；过期续签同对象

### F04.3 bff → capability / CapabilityProductService.CompleteUpload

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [CompleteUploadRpcRequest](rpc-messages.md#completeuploadrpcrequest)：context#1: CallContext；body#2: CompleteUploadRequest；upload_id#3: string

返回 [Operation](rpc-messages.md#operation)：operation_id#1: string；owner#2: OperationOwnerEnum；kind#3: OperationKindEnum；resource_id#4: string；status#5: OperationStatusEnum；stage#6: OperationStageEnum；observation_result#7: optional OperationObservationResultEnum；error_code#8: optional ErrorCodeEnum；request_id#9: string；created_at#10: Timestamp；updated_at#11: Timestamp；poll_after_seconds#12: optional int32

接收方写入：`capability.upload_chunks`, `capability.package_versions`, `capability.operations`

完成证据：Storage精确对象version、字节sha256和长度通过

失败/未知：校验失败不得标uploaded，不信ETag=SHA256

### F05.1 build → capability / CapabilityCoordination.ResolveBuildInput

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [BuildInputRequest](rpc-messages.md#buildinputrequest)：context#1: CallContext；package_version_id#2: string；webui_version_id#3: string

返回 [BuildInputSnapshot](rpc-messages.md#buildinputsnapshot)：package_id#1: string；package_version_id#2: string；package_object#3: SourceObjectReference；runtime_version_id#4: string；runtime_artifact#5: ArtifactReference；webui_version_id#6: string；webui_artifact#7: ArtifactReference；runtime_contract#8: RuntimePublisherContract；webui_contract#9: WebuiPublisherContract；snapshot_digest#10: string；runtime_contract_reference#11: PublisherContractReference；webui_contract_reference#12: PublisherContractReference；package_claim_id#13: string；runtime_claim_id#14: string；webui_claim_id#15: string

接收方写入：

完成证据：Package/WebUI/Runtime策略和完整发布描述冻结

失败/未知：读失败还未建业务副作用，不能选latest替代

### F05.3 build → capability / CapabilityCoordination.AcquireReference

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [ReferenceClaimRequest](rpc-messages.md#referenceclaimrequest)：context#1: CallContext；target#2: ReferenceTarget；claimant_owner#3: OwnerEnum；claimant_resource_id#4: string

返回 [ReferenceClaim](rpc-messages.md#referenceclaim)：id#1: string；target#2: ReferenceTarget；claimant_owner#3: OwnerEnum；claimant_resource_id#4: string；state#5: ReferenceClaimState；bound_operation_id#6: optional string；bound_input_digest#7: optional string；acquired_at#8: Timestamp；released_at#9: optional Timestamp

接收方写入：`capability.reference_claims`

完成证据：PackageVersion/RuntimeVersion/WebuiVersion三target均绑定本job

失败/未知：任一拒绝则不读字节/不build；保留已取得claim待确定收尾

### F05.4 build → capability / CapabilityCoordination.BindReference

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [BindReferenceRequest](rpc-messages.md#bindreferencerequest)：context#1: CallContext；claim_id#2: string；owner_commit_evidence#3: OwnerCommitEvidence

返回 [ReferenceClaim](rpc-messages.md#referenceclaim)：id#1: string；target#2: ReferenceTarget；claimant_owner#3: OwnerEnum；claimant_resource_id#4: string；state#5: ReferenceClaimState；bound_operation_id#6: optional string；bound_input_digest#7: optional string；acquired_at#8: Timestamp；released_at#9: optional Timestamp

接收方写入：`capability.reference_claims`

完成证据：Build本域保存claim IDs后的OwnerCommitEvidence可读回

失败/未知：Bind丢响应查原claim，不自动TTL释放

### F05.5 capability → build / BuildCoordination.ReadArtifact

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [ReadBuildArtifactRequest](rpc-messages.md#readbuildartifactrequest)：context#1: CallContext；build_job_id#2: string

返回 [BuildArtifactReadback](rpc-messages.md#buildartifactreadback)：build_job_id#1: string；input#2: BuildInputSnapshot；artifact#3: ArtifactReference；artifact_receipt_id#4: string；version_label#5: string；model_requirements#6: repeated ModelRequirement；data_compatibility#7: DataCompatibility；outcome#8: Observation；deployment_descriptor#9: DeploymentDescriptor；deployment_descriptor_digest#10: string；deployment_descriptor_object_ref#11: string

接收方写入：

完成证据：repository+digest+platform+DeploymentDescriptor与远端制品相同

失败/未知：不是push接受就注册，unknown继续读原产物

### F05.6 build → capability / DomainInbox.Deliver

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [DeliverEventRequest](rpc-messages.md#delivereventrequest)：event#1: EventEnvelope；authenticated_producer#2: string；consumer_owner#3: OwnerEnum

返回 [InboxAck](rpc-messages.md#inboxack)：event_id#1: string；consumer#2: string；committed#3: bool；duplicate#4: bool；applied_aggregate_version#5: int64；rejection_code#6: string

接收方写入：`capability.capability_versions`

完成证据：Inbox事务去重后唯一版本，事件与Build真实readback一致

失败/未知：同eventId重复ACK，乱序不覆盖新事实

### F06.1 capability → tenant / CloudIdentityAuthorization.AuthorizeAction

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [AuthorizationRequest](rpc-messages.md#authorizationrequest)：scope#1: AuthorizationScope；actor_id#2: string；session_id#3: optional string；authorization_context_id#4: optional string；accepted_operation_grant_id#5: optional string；audience_owner#6: OwnerEnum；action#7: AuthorizationActionEnum；resource#8: AuthorizationResource；expected_permission_version#9: optional int64；request_id#10: string

返回 [AuthorizationDecision](rpc-messages.md#authorizationdecision)：result#1: AuthorizationResult；issuer#2: AuthorizationIssuer；authorization_context_id#3: optional string；scope#4: AuthorizationScope；actor_id#5: string；session_id#6: optional string；audience_owner#7: OwnerEnum；action#8: AuthorizationActionEnum；resource#9: AuthorizationResource；permission_version#10: int64；issued_at#11: Timestamp；expires_at#12: Timestamp；denial_code#13: optional ErrorCodeEnum；accepted_operation_grant_id#14: optional string

接收方写入：

完成证据：session/grant/action/resource/audience/权限版本确切一致

失败/未知：deny不改用管理员；unknown不发后续副作用

### F06.2 bff → capability / CapabilityProductService.DeleteCapabilityVersion

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [DeleteCapabilityVersionRpcRequest](rpc-messages.md#deletecapabilityversionrpcrequest)：context#1: CallContext；capability_version_id#2: string

返回 [Operation](rpc-messages.md#operation)：operation_id#1: string；owner#2: OperationOwnerEnum；kind#3: OperationKindEnum；resource_id#4: string；status#5: OperationStatusEnum；stage#6: OperationStageEnum；observation_result#7: optional OperationObservationResultEnum；error_code#8: optional ErrorCodeEnum；request_id#9: string；created_at#10: Timestamp；updated_at#11: Timestamp；poll_after_seconds#12: optional int32

接收方写入：`capability.capability_versions`, `capability.reference_claims`

完成证据：本域锁定版本并确认无活跃claim；只做目录下架

失败/未知：仍使用返回冲突；不建议删Workspace绕过

### F06.3 consumer_owner → capability / CapabilityCoordination.ReleaseReference

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [ReleaseReferenceRequest](rpc-messages.md#releasereferencerequest)：context#1: CallContext；claim_id#2: string；release_evidence#3: ReleaseEvidence

返回 [ReferenceClaim](rpc-messages.md#referenceclaim)：id#1: string；target#2: ReferenceTarget；claimant_owner#3: OwnerEnum；claimant_resource_id#4: string；state#5: ReferenceClaimState；bound_operation_id#6: optional string；bound_input_digest#7: optional string；acquired_at#8: Timestamp；released_at#9: optional Timestamp

接收方写入：`capability.reference_claims`

完成证据：原claim owner真实usage/终态证据

失败/未知：未知不释放，历史元数据不级联

### F10.2 workspace → capability / CapabilityCoordination.ResolvePublisherContract

按当前入口/阶段与06/13状态机前置执行；不是无条件调用

请求 [ResolvePublisherContractRequest](rpc-messages.md#resolvepublishercontractrequest)：context#1: CallContext；reference#2: PublisherContractReference

返回 [ResolvedPublisherContract](rpc-messages.md#resolvedpublishercontract)：reference#1: PublisherContractReference；contract#2: PublisherContract；image#3: ArtifactReference；outcome#4: Observation

接收方写入：

完成证据：目标版本完整契约与数据兼容

失败/未知：不支持安全回滚的迁移拒绝，不猜semver

## 5. 事件：谁生产、谁消费、哪些字段

aggregate_type由事件精确版本的x-aggregate-identity.type派生；aggregateId须与其idPayloadField一致。revision由生产者聚合事务内分配；consumer_owner显式选择本域Inbox。字段与实现状态不得混同。


### package.uploaded.v1

`capability` → `ledger`

聚合类型：`package_version`；ID来源：`payload.packageVersionId`（须等于aggregateId）

Envelope字段：`eventId`, `eventType`, `schemaVersion`, `owner`, `tenantId`, `aggregateId`, `aggregateVersion`, `occurredAt`, `requestId`, `payload`, `scope`

| payload字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `packageVersionId` | `string` | 是 | minLength=1 |
| `packageId` | `string` | 是 | minLength=1 |
| `sha256` | `string` | 是 | pattern="^sha256:[0-9a-f]{64}$" |
| `sizeBytes` | `string` | 是 | pattern="^(?:0\|[1-9][0-9]{0,17}\|[1-8][0-9]{18}\|9[0-1][0-9]{17}\|92[0-1][0-9]{16}\|922[0-2][0-9]{15}\|9223[0-2][0-9]{14}\|92233[0-6][0-9]{13}\|922337[0-1][0-9]{12}\|92233720[0-2][0-9]{10}\|922337203[0-5][0-9]{9}\|9223372036[0-7][0-9]{8}\|92233720368[0-4][0-9]{7}\|922337203685[0-3][0-9]{6}\|9223372036854[0-6][0-9]{5}\|92233720368547[0-6][0-9]{4}\|922337203685477[0-4][0-9]{3}\|9223372036854775[0-7][0-9]{2}\|922337203685477580[0-6]\|9223372036854775807)$" |

Package上传实际校验通过
仅审计，不自动创建Build；客户必须显式createBuild。

### build.artifact_confirmed.v1

`build` → `capability`, `ledger`

聚合类型：`build_job`；ID来源：`payload.buildJobId`（须等于aggregateId）

Envelope字段：`eventId`, `eventType`, `schemaVersion`, `owner`, `tenantId`, `aggregateId`, `aggregateVersion`, `occurredAt`, `requestId`, `payload`, `scope`

| payload字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `buildJobId` | `string` | 是 | minLength=1 |
| `packageVersionId` | `string` | 是 | minLength=1 |
| `runtimeVersionId` | `string` | 是 | minLength=1 |
| `webuiVersionId` | `string` | 是 | minLength=1 |
| `artifactDigest` | `string` | 是 | pattern="^sha256:[0-9a-f]{64}$" |
| `artifactReceiptId` | `string` | 是 | minLength=1 |
| `deploymentDescriptorDigest` | `string` | 是 | pattern="^sha256:[0-9a-f]{64}$" |

制品已push并读回，Capability唯一writer登记version


### capability.version_registered.v1

`capability` → `build`, `ledger`

聚合类型：`capability_version`；ID来源：`payload.capabilityVersionId`（须等于aggregateId）

Envelope字段：`eventId`, `eventType`, `schemaVersion`, `owner`, `tenantId`, `aggregateId`, `aggregateVersion`, `occurredAt`, `requestId`, `payload`, `scope`

| payload字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `capabilityVersionId` | `string` | 是 | minLength=1 |
| `buildJobId` | `string` | 是 | minLength=1 |
| `artifactDigest` | `string` | 是 | pattern="^sha256:[0-9a-f]{64}$" |

Capability ready已提交，Build据此确认succeeded


### tenant.access_revoked.v1

`tenant` → `capability`, `build`, `workspace`, `gateway`, `ledger`

聚合类型：`tenant`；ID来源：`payload.targetTenantId`（须等于aggregateId）

Envelope字段：`eventId`, `eventType`, `schemaVersion`, `owner`, `tenantId`, `aggregateId`, `aggregateVersion`, `occurredAt`, `requestId`, `payload`, `scope`

| payload字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `targetTenantId` | `string` | 是 | minLength=1 |
| `tenantOperationId` | `string` | 是 | minLength=1 |
| `status` | `string` | 是 | enum=["suspended","deleting","deleted"]; minLength=1 |
| `restoreUntil` | `string/date-time` | 否 |  |

Tenant访问立即撤销，消费者清除访问准入


### tenant.restored.v1

`tenant` → `capability`, `build`, `gateway`, `ledger`

聚合类型：`tenant`；ID来源：`payload.targetTenantId`（须等于aggregateId）

Envelope字段：`eventId`, `eventType`, `schemaVersion`, `owner`, `tenantId`, `aggregateId`, `aggregateVersion`, `occurredAt`, `requestId`, `payload`, `scope`

| payload字段 | 类型 | 必填 | 约束 |
| --- | --- | --- | --- |
| `targetTenantId` | `string` | 是 | minLength=1 |
| `tenantOperationId` | `string` | 是 | minLength=1 |
| `restoredAt` | `string/date-time` | 是 |  |

恢复身份和保留制品权限，不复活资源
