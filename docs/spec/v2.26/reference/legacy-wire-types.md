# 继承实现的 JSON 类型字段

> 从共同基线 `50520e27a6b9a630eefdc2df7da3e5ec498d28a0` 的非生成Go源码AST提取。原始JSON tag完整保留；omitempty不是业务可选/授权声明。嵌入结构、别名和map的细节按真实类型/decoder，不新增平行schema。


## contracts


### RuntimeObservation

`packages/contracts/go/fabric_observation.go:44`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ObjectRef` | `string` | `json:"objectRef"` |
| `AccountID` | `string` | `json:"accountId,omitempty"` |
| `WorkspaceID` | `string` | `json:"workspaceId,omitempty"` |
| `RuntimeID` | `string` | `json:"runtimeId,omitempty"` |
| `Ownership` | `RuntimeOwnership` | `json:"ownership"` |
| `DesiredState` | `ResourceObservedState` | `json:"desiredState"` |
| `ObservedState` | `ResourceObservedState` | `json:"observedState"` |
| `ReasonCode` | `string` | `json:"reasonCode,omitempty"` |

### ResourceObservation

`packages/contracts/go/fabric_observation.go:5`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Available` | `bool` | `json:"available"` |
| `State` | `ResourceObservedState` | `json:"state"` |
| `ObservedAt` | `string` | `json:"observedAt"` |
| `ReasonCode` | `string` | `json:"reasonCode,omitempty"` |
| `ProviderID` | `string` | `json:"providerId,omitempty"` |
| `PackageOrSpec` | `string` | `json:"packageOrSpec,omitempty"` |
| `Zone` | `string` | `json:"zone,omitempty"` |
| `CreatedAt` | `string` | `json:"createdAt,omitempty"` |
| `ExpiresAt` | `string` | `json:"expiresAt,omitempty"` |
| `ComputeRuntimeBinding` | `*WorkspaceComputeRuntimeBinding` | `json:"computeRuntimeBinding,omitempty"` |

### RuntimeObservations

`packages/contracts/go/fabric_observation.go:57`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ObservedAt` | `string` | `json:"observedAt"` |
| `Items` | `[]RuntimeObservation` | `json:"items"` |

### FabricReadiness

`packages/contracts/go/fabric_observation.go:75`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Provider` | `string` | `json:"provider,omitempty"` |
| `Ready` | `bool` | `json:"ready"` |
| `ServiceReady` | `bool` | `json:"serviceReady"` |
| `CloudImagesReady` | `bool` | `json:"cloudImagesReady"` |
| `WorkspaceImagesReady` | `bool` | `json:"workspaceImagesReady"` |
| `WorkspaceImageStatus` | `WorkspaceImageReadinessStatus` | `json:"workspaceImageStatus,omitempty"` |
| `ImmutableImagesReady` | `bool` | `json:"immutableImagesReady"` |
| `MissingEnv` | `[]string` | `json:"missingEnv"` |
| `MissingTools` | `[]string` | `json:"missingTools"` |
| `FailedChecks` | `[]string` | `json:"failedChecks"` |

### ReceiptLookupScope

`packages/contracts/go/receipt.go:9`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `RequestID` | `string` | `json:"requestId"` |
| `Type` | `string` | `json:"type"` |
| `TypePrefix` | `string` | `json:"typePrefix"` |
| `IncludeType` | `string` | `json:"includeType"` |
| `IncludeExecutionKind` | `string` | `json:"includeExecutionKind"` |

### WorkspaceApplicationCredential

`packages/contracts/go/workspace_application.go:113`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Name` | `string` | `json:"name"` |
| `Kind` | `string` | `json:"kind"` |
| `Target` | `string` | `json:"target,omitempty"` |
| `Env` | `string` | `json:"env,omitempty"` |
| `Username` | `string` | `json:"username,omitempty"` |

### WorkspaceApplicationPort

`packages/contracts/go/workspace_application.go:151`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Name` | `string` | `json:"name"` |
| `Port` | `int` | `json:"port"` |
| `Protocol` | `string` | `json:"protocol"` |

### WorkspaceApplicationHealthCheck

`packages/contracts/go/workspace_application.go:168`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Port` | `int` | `json:"port"` |
| `Path` | `string` | `json:"path"` |
| `InitialDelaySeconds` | `int` | `json:"initialDelaySeconds,omitempty"` |

### WorkspaceApplicationMount

`packages/contracts/go/workspace_application.go:174`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Name` | `string` | `json:"name"` |
| `MountPath` | `string` | `json:"mountPath"` |
| `ReadOnly` | `bool` | `json:"readOnly,omitempty"` |
| `Mode` | `*uint32` | `json:"mode,omitempty"` |
| `UserID` | `*int64` | `json:"userId,omitempty"` |
| `GroupID` | `*int64` | `json:"groupId,omitempty"` |
| `SizeBytes` | `int64` | `json:"sizeBytes,omitempty"` |
| `Executable` | `bool` | `json:"executable,omitempty"` |

### WorkspaceApplicationSecretInput

`packages/contracts/go/workspace_application.go:186`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Name` | `string` | `json:"name"` |
| `Target` | `string` | `json:"target,omitempty"` |
| `Env` | `string` | `json:"env,omitempty"` |

### WorkspaceApplicationConfigInput

`packages/contracts/go/workspace_application.go:193`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Name` | `string` | `json:"name"` |
| `Target` | `string` | `json:"target"` |

### WorkspaceApplicationDependency

`packages/contracts/go/workspace_application.go:198`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Execution` | `WorkspaceApplicationExecution` | `json:"execution,omitzero"` |
| `DependsOn` | `[]string` | `json:"dependsOn,omitempty"` |
| `Name` | `string` | `json:"name"` |
| `Image` | `string` | `json:"image"` |
| `Ports` | `[]WorkspaceApplicationDependencyPort` | `json:"ports,omitempty"` |
| `HealthChecks` | `[]WorkspaceApplicationDependencyHealthCheck` | `json:"healthChecks,omitempty"` |
| `PersistentMounts` | `[]WorkspaceApplicationDependencyMount` | `json:"persistentMounts,omitempty"` |
| `ScratchMounts` | `[]WorkspaceApplicationDependencyMount` | `json:"scratchMounts,omitempty"` |
| `Command` | `WorkspaceApplicationDependencyCommand` | `json:"command,omitempty"` |
| `SecretInputs` | `[]WorkspaceApplicationSecretInput` | `json:"secretInputs,omitempty"` |
| `ConfigInputs` | `[]WorkspaceApplicationConfigInput` | `json:"configInputs,omitempty"` |
| `Compute` | `WorkspaceApplicationCompute` | `json:"compute,omitzero"` |

### WorkspaceApplicationDeployment

`packages/contracts/go/workspace_application.go:216`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `OperationID` | `string` | `json:"operationId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ApplicationID` | `string` | `json:"applicationId"` |
| `TargetRevision` | `string` | `json:"targetRevision"` |
| `PreviousApplicationID` | `string` | `json:"previousApplicationId,omitempty"` |
| `PreviousRevision` | `string` | `json:"previousRevision,omitempty"` |
| `ConfigurationDigest` | `string` | `json:"configurationDigest"` |
| `SecretBindingVersions` | `[]string` | `json:"secretBindingVersions,omitempty"` |
| `DataBindingIDs` | `[]string` | `json:"dataBindingIds,omitempty"` |
| `ExpectedWorkspaceVersion` | `int64` | `json:"expectedWorkspaceVersion"` |
| `IdempotencyKey` | `string` | `json:"idempotencyKey"` |

### WorkspaceApplicationRevision

`packages/contracts/go/workspace_application.go:23`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Execution` | `WorkspaceApplicationExecution` | `json:"execution,omitzero"` |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `ApplicationID` | `string` | `json:"applicationId"` |
| `Version` | `string` | `json:"version"` |
| `Platform` | `string` | `json:"platform"` |
| `Image` | `string` | `json:"image"` |
| `Credentials` | `[]WorkspaceApplicationCredential` | `json:"credentials,omitempty"` |
| `Entrypoint` | `[]string` | `json:"entrypoint,omitempty"` |
| `Ports` | `[]WorkspaceApplicationPort` | `json:"ports,omitempty"` |
| `EntryPort` | `string` | `json:"entryPort,omitempty"` |
| `HealthChecks` | `[]WorkspaceApplicationHealthCheck` | `json:"healthChecks,omitempty"` |
| `PersistentMounts` | `[]WorkspaceApplicationMount` | `json:"persistentMounts,omitempty"` |
| `ScratchMounts` | `[]WorkspaceApplicationMount` | `json:"scratchMounts,omitempty"` |
| `SecretInputs` | `[]WorkspaceApplicationSecretInput` | `json:"secretInputs,omitempty"` |
| `ConfigInputs` | `[]WorkspaceApplicationConfigInput` | `json:"configInputs,omitempty"` |
| `Dependencies` | `[]WorkspaceApplicationDependency` | `json:"dependencies,omitempty"` |
| `ExposurePolicy` | `string` | `json:"exposurePolicy"` |
| `Compute` | `WorkspaceApplicationCompute` | `json:"compute,omitzero"` |

### WorkspaceApplicationCompute

`packages/contracts/go/workspace_application.go:55`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `CPURequestMilli` | `int64` | `json:"cpuRequestMilli,omitempty"` |
| `CPULimitMilli` | `int64` | `json:"cpuLimitMilli,omitempty"` |
| `MemoryRequestBytes` | `int64` | `json:"memoryRequestBytes,omitempty"` |
| `MemoryLimitBytes` | `int64` | `json:"memoryLimitBytes,omitempty"` |

### WorkspaceApplicationDataArtifact

`packages/contracts/go/workspace_application_data.go:17`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Name` | `string` | `json:"name"` |
| `SHA256` | `string` | `json:"sha256"` |
| `SizeBytes` | `int64` | `json:"sizeBytes"` |

### WorkspaceApplicationRestoreTool

`packages/contracts/go/workspace_application_data.go:26`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Image` | `string` | `json:"image"` |

### WorkspaceApplicationDataMaterial

`packages/contracts/go/workspace_application_data.go:33`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `ApplicationID` | `string` | `json:"applicationId"` |
| `Version` | `string` | `json:"version"` |
| `Artifacts` | `[]WorkspaceApplicationDataArtifact` | `json:"artifacts"` |
| `RestoreTool` | `WorkspaceApplicationRestoreTool` | `json:"restoreTool"` |
| `RestoreArguments` | `[]string` | `json:"restoreArguments,omitempty"` |

### WorkspaceApplicationDependencyPort

`packages/contracts/go/workspace_application_dependency.go:11`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Name` | `string` | `json:"name"` |
| `Port` | `int` | `json:"port"` |
| `Protocol` | `string` | `json:"protocol"` |

### WorkspaceApplicationDependencyHealthCheck

`packages/contracts/go/workspace_application_dependency.go:20`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Command` | `[]string` | `json:"command,omitempty"` |
| `Type` | `string` | `json:"type"` |
| `Port` | `int` | `json:"port"` |
| `Path` | `string` | `json:"path,omitempty"` |
| `InitialDelaySeconds` | `int` | `json:"initialDelaySeconds,omitempty"` |

### WorkspaceApplicationDependencyMount

`packages/contracts/go/workspace_application_dependency.go:31`

别名 → `WorkspaceApplicationMount`
| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |

### WorkspaceApplicationDependencyCommand

`packages/contracts/go/workspace_application_dependency.go:35`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Entrypoint` | `[]string` | `json:"entrypoint,omitempty"` |
| `Args` | `[]string` | `json:"args,omitempty"` |
| `Env` | `map[string]string` | `json:"env,omitempty"` |

### WorkspaceApplicationExecution

`packages/contracts/go/workspace_application_execution.go:11`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `UserID` | `*int64` | `json:"userId,omitempty"` |
| `GroupID` | `*int64` | `json:"groupId,omitempty"` |
| `Init` | `bool` | `json:"init,omitempty"` |
| `SeccompProfile` | `string` | `json:"seccompProfile,omitempty"` |

### WorkspaceApplicationRuntimeComponentState

`packages/contracts/go/workspace_application_runtime.go:16`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Name` | `string` | `json:"name"` |
| `Role` | `string` | `json:"role"` |
| `Image` | `string` | `json:"image"` |
| `State` | `string` | `json:"state"` |
| `Ports` | `[]int` | `json:"ports,omitempty"` |
| `LastError` | `string` | `json:"lastError,omitempty"` |
| `ReadyCheck` | `string` | `json:"readyCheck,omitempty"` |
| `CheckNames` | `[]string` | `json:"checkNames,omitempty"` |

### WorkspaceApplicationRuntimeConfiguration

`packages/contracts/go/workspace_application_runtime.go:185`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Environment` | `map[string]string` | `json:"environment,omitempty"` |
| `Files` | `map[string]string` | `json:"files,omitempty"` |
| `CredentialVersion` | `string` | `json:"credentialVersion,omitempty"` |
| `CredentialSourceRuntimeOperationID` | `string` | `json:"credentialSourceRuntimeOperationId,omitempty"` |

### WorkspaceApplicationRuntimeSecretBinding

`packages/contracts/go/workspace_application_runtime.go:194`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Name` | `string` | `json:"name"` |
| `SecretRef` | `string` | `json:"secretRef"` |
| `Version` | `string` | `json:"version"` |
| `Key` | `string` | `json:"key"` |

### WorkspaceApplicationRuntimeInput

`packages/contracts/go/workspace_application_runtime.go:203`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ComputeID` | `string` | `json:"computeId"` |
| `VolumeID` | `string` | `json:"volumeId"` |
| `AttachmentID` | `string` | `json:"attachmentId"` |
| `AttachmentOperationID` | `string` | `json:"attachmentOperationId"` |
| `RuntimeOperationID` | `string` | `json:"runtimeOperationId"` |
| `Revision` | `WorkspaceApplicationRevision` | `json:"revision"` |
| `Configuration` | `WorkspaceApplicationRuntimeConfiguration` | `json:"configuration"` |
| `SecretBindings` | `[]WorkspaceApplicationRuntimeSecretBinding` | `json:"secretBindings,omitempty"` |
| `DataSourceRuntimeOperationID` | `string` | `json:"dataSourceRuntimeOperationId,omitempty"` |
| `DataLayout` | `string` | `json:"dataLayout,omitempty"` |
| `DataBindingID` | `string` | `json:"dataBindingId"` |
| `ConfigurationDigest` | `string` | `json:"configurationDigest"` |
| `IdempotencyKey` | `string` | `json:"-"` |
| `OperationID` | `string` | `json:"-"` |

### WorkspaceApplicationRuntimeLifecycleInput

`packages/contracts/go/workspace_application_runtime.go:223`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `HistoricalApplicationRuntime` | `bool` | `json:"historicalApplicationRuntime,omitempty"` |
| `LegacyRuntime` | `bool` | `json:"legacyRuntime,omitempty"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `RuntimeID` | `string` | `json:"runtimeId"` |
| `RuntimeOperationID` | `string` | `json:"runtimeOperationId"` |
| `DesiredState` | `string` | `json:"desiredState"` |
| `IdempotencyKey` | `string` | `json:"-"` |

### WorkspaceApplicationRuntimeImageRetirement

`packages/contracts/go/workspace_application_runtime.go:234`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Image` | `string` | `json:"image"` |
| `State` | `string` | `json:"state"` |

### WorkspaceApplicationRuntimeLifecycleResult

`packages/contracts/go/workspace_application_runtime.go:239`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `RuntimeID` | `string` | `json:"runtimeId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `State` | `string` | `json:"state"` |
| `Observation` | `WorkspaceApplicationRuntimeObservation` | `json:"observation"` |
| `ImageRetirement` | `[]WorkspaceApplicationRuntimeImageRetirement` | `json:"imageRetirement,omitempty"` |

### WorkspaceApplicationRuntimeCredentials

`packages/contracts/go/workspace_application_runtime.go:247`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `RuntimeID` | `string` | `json:"runtimeId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `WebUIUsername` | `string` | `json:"webuiUsername"` |
| `WebUIPassword` | `string` | `json:"webuiPassword"` |

### WorkspaceApplicationRuntimeObservation

`packages/contracts/go/workspace_application_runtime.go:31`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `RuntimeID` | `string` | `json:"runtimeId"` |
| `Status` | `string` | `json:"status"` |
| `Entry` | `*WorkspaceApplicationEntry` | `json:"entry,omitempty"` |
| `Components` | `[]WorkspaceApplicationRuntimeComponentState` | `json:"components"` |

### WorkspaceApplicationGatewaySecretCleanupInput

`packages/contracts/go/workspace_application_runtime.go:424`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `SecretRef` | `string` | `json:"secretRef"` |
| `IdempotencyKey` | `string` | `json:"-"` |

### WorkspaceApplicationEntry

`packages/contracts/go/workspace_application_runtime.go:52`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ServiceName` | `string` | `json:"serviceName,omitempty"` |
| `Port` | `int` | `json:"port,omitempty"` |
| `URL` | `string` | `json:"url,omitempty"` |

### WorkspaceDeleteStageEvidence

`packages/contracts/go/workspace_delete.go:290`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Stage` | `string` | `json:"stage"` |
| `Result` | `string` | `json:"result"` |
| `ReasonCode` | `string` | `json:"reasonCode,omitempty"` |
| `EvidenceKind` | `string` | `json:"evidenceKind"` |
| `ResourceID` | `string` | `json:"resourceId"` |
| `ProviderResourceID` | `string` | `json:"providerResourceId,omitempty"` |
| `ObservedAt` | `string` | `json:"observedAt"` |
| `ReadbackID` | `string` | `json:"readbackId,omitempty"` |
| `ReadAttempts` | `int` | `json:"readAttempts,omitempty"` |
| `MutationAttempts` | `int` | `json:"mutationAttempts,omitempty"` |

### WorkspaceDeleteStageEvidenceDigest

`packages/contracts/go/workspace_delete.go:429`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Stage` | `string` | `json:"stage"` |
| `Result` | `string` | `json:"result"` |
| `EvidenceKind` | `string` | `json:"evidenceKind"` |
| `ObservedAt` | `string` | `json:"observedAt"` |
| `ReasonCode` | `string` | `json:"reasonCode,omitempty"` |
| `EvidenceRef` | `string` | `json:"evidenceRef"` |

### WorkspaceImageRelease

`packages/contracts/go/workspace_image_release.go:18`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Version` | `string` | `json:"version"` |
| `Image` | `string` | `json:"image"` |

### WorkspaceImageReleaseCatalog

`packages/contracts/go/workspace_image_release.go:23`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Releases` | `[]WorkspaceImageRelease` | `json:"releases"` |

### WorkspaceImageReleaseActivationRequest

`packages/contracts/go/workspace_image_release.go:28`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ReleaseVersion` | `string` | `json:"releaseVersion"` |
| `ExpectedRevision` | `int` | `json:"expectedRevision"` |
| `Reason` | `string` | `json:"reason"` |

### WorkspaceRuntimeImageReplacementPreview

`packages/contracts/go/workspace_image_release.go:34`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `WorkspaceStatus` | `string` | `json:"workspaceStatus"` |
| `RuntimeID` | `string` | `json:"runtimeId"` |
| `RuntimeStatus` | `string` | `json:"runtimeStatus"` |
| `CurrentImageDigest` | `string` | `json:"currentImageDigest"` |
| `TargetImageDigest` | `string` | `json:"targetImageDigest"` |
| `CanReplace` | `bool` | `json:"canReplace"` |

### WorkspaceComputeRuntimeBinding

`packages/contracts/go/workspace_image_release.go:51`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Status` | `WorkspaceComputeRuntimeBindingStatus` | `json:"status"` |

### WorkspaceLaunchCloseoutResult

`packages/contracts/go/workspace_launch_closeout.go:21`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Binding` | `WorkspaceLaunchCloseoutInput` | `json:"binding"` |
| `Frozen` | `bool` | `json:"frozen"` |
| `State` | `string` | `json:"state"` |
| `Reason` | `string` | `json:"reason"` |
| `Resources` | `[]WorkspaceLaunchCloseoutResource` | `json:"resources"` |

### WorkspaceLaunchCloseoutResource

`packages/contracts/go/workspace_launch_closeout.go:30`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Stage` | `string` | `json:"stage"` |
| `OperationID` | `string` | `json:"operationId"` |
| `ResourceID` | `string` | `json:"resourceId"` |
| `State` | `string` | `json:"state"` |

### WorkspaceLaunchCloseoutInput

`packages/contracts/go/workspace_launch_closeout.go:5`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `LaunchOperationID` | `string` | `json:"launchOperationId"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ProviderProfileRef` | `string` | `json:"providerProfileRef"` |
| `ProviderBindingRef` | `string` | `json:"providerBindingRef"` |
| `SpecDigest` | `string` | `json:"specDigest"` |
| `IdempotencyKey` | `string` | `json:"idempotencyKey"` |

### WorkspaceLaunchCloseoutReceiptExecution

`packages/contracts/go/workspace_launch_closeout_receipt.go:14`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `OperationID` | `string` | `json:"operationId"` |
| `AuthorizationID` | `string` | `json:"authorizationId"` |
| `AuthorizedAt` | `string` | `json:"authorizedAt"` |
| `Reason` | `string` | `json:"reason"` |
| `Outcome` | `string` | `json:"outcome"` |
| `ChargeConfirmation` | `*WorkspaceLaunchCloseoutCharge` | `json:"chargeConfirmation,omitempty"` |
| `RefundedUSDMicros` | `int64` | `json:"refundedUsdMicros"` |
| `RefundOperationID` | `string` | `json:"refundOperationId"` |
| `FrozenAt` | `string` | `json:"frozenAt"` |
| `KeyRevokedAt` | `string` | `json:"keyRevokedAt,omitempty"` |
| `ResourcesAbsentAt` | `string` | `json:"resourcesAbsentAt"` |
| `CompletedAt` | `string` | `json:"completedAt"` |

### WorkspaceLaunchCloseoutReceiptCost

`packages/contracts/go/workspace_launch_closeout_receipt.go:30`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Currency` | `string` | `json:"currency"` |
| `PriceVersion` | `string` | `json:"priceVersion"` |
| `ChargeUSDMicros` | `int64` | `json:"chargeUsdMicros"` |
| `PeriodStart` | `string` | `json:"periodStart,omitempty"` |
| `PaidThrough` | `string` | `json:"paidThrough,omitempty"` |

### WorkspaceLaunchCloseoutCharge

`packages/contracts/go/workspace_launch_closeout_receipt.go:7`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Code` | `string` | `json:"code"` |
| `UserID` | `int64` | `json:"userId"` |
| `ChargeUSDMicros` | `int64` | `json:"chargeUsdMicros"` |
| `Status` | `string` | `json:"status"` |

### WorkspaceLaunchResumeAuthorization

`packages/contracts/go/workspace_launch_resume.go:10`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AuthorizationID` | `string` | `json:"authorizationId"` |
| `LaunchVersion` | `int` | `json:"launchVersion"` |
| `AuthorizedStage` | `Stage` | `json:"authorizedStage"` |
| `AuthorizedBy` | `string` | `json:"authorizedBy"` |
| `AuthorizedAt` | `string` | `json:"authorizedAt"` |
| `Reason` | `string` | `json:"reason"` |
| `MutationBudget` | `int` | `json:"mutationBudget"` |
| `IdempotentReplayBudget` | `int` | `json:"idempotentReplayBudget,omitempty"` |
| `AuthoritativeReadBudget` | `int` | `json:"authoritativeReadBudget,omitempty"` |
| `ReadbacksAtAuthorization` | `int` | `json:"readbacksAtAuthorization,omitempty"` |
| `ReplacementWorkspaceImageDigest` | `string` | `json:"replacementWorkspaceImageDigest,omitempty"` |
| `AcceptanceBResumeExisting` | `*WorkspaceLaunchAcceptanceBResumeExistingBinding` | `json:"acceptanceBResumeExisting,omitempty"` |

### WorkspaceLaunchResumeAuthorizationReadback

`packages/contracts/go/workspace_launch_resume.go:25`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `OperationID` | `string` | `json:"operationId"` |
| `OperationVersion` | `int` | `json:"operationVersion"` |
| `AuthorizationID` | `string` | `json:"authorizationId"` |
| `AuthorizationVersion` | `int` | `json:"authorizationVersion"` |
| `AuthorizedStage` | `Stage` | `json:"authorizedStage"` |
| `AuthorizedBy` | `string` | `json:"authorizedBy"` |
| `Status` | `string` | `json:"status"` |
| `ConsumedAt` | `string` | `json:"consumedAt"` |
| `SingleUse` | `bool` | `json:"singleUse"` |
| `Attempt` | `WorkspaceLaunchResumeAttemptReadback` | `json:"attempt"` |
| `Convergence` | `WorkspaceLaunchResumeConvergenceReadback` | `json:"convergence"` |
| `AcceptanceBResumeExisting` | `*WorkspaceLaunchAcceptanceBResumeExistingBinding` | `json:"acceptanceBResumeExisting"` |

### WorkspaceLaunchResumeAttemptReadback

`packages/contracts/go/workspace_launch_resume.go:41`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Attempted` | `int` | `json:"attempted"` |
| `Confirmed` | `int` | `json:"confirmed"` |
| `Unknown` | `int` | `json:"unknown"` |
| `Max` | `int` | `json:"max"` |
| `Status` | `string` | `json:"status"` |
| `IdempotencyKey` | `string` | `json:"idempotencyKey"` |
| `PendingReadbacks` | `int` | `json:"pendingReadbacks"` |
| `MaxPendingReadbacks` | `int` | `json:"maxPendingReadbacks"` |

### WorkspaceLaunchResumeAuthorizationProjection

`packages/contracts/go/workspace_launch_resume.go:5`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ResumeAuthorization` | `*WorkspaceLaunchResumeAuthorization` | `json:"resumeAuthorization"` |
| `ResumeAuthorizationReadback` | `*WorkspaceLaunchResumeAuthorizationReadback` | `json:"resumeAuthorizationReadback"` |

### WorkspaceLaunchResumeConvergenceReadback

`packages/contracts/go/workspace_launch_resume.go:52`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `OperationStatus` | `LaunchStatus` | `json:"operationStatus"` |
| `Stage` | `Stage` | `json:"stage"` |
| `Version` | `int` | `json:"version"` |

### WorkspaceLaunchAcceptanceBResumeExistingBinding

`packages/contracts/go/workspace_launch_resume.go:58`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `ApprovalID` | `string` | `json:"approvalId"` |
| `ApprovalSHA256` | `string` | `json:"approvalSha256"` |
| `CanonicalCloudSHA` | `string` | `json:"canonicalCloudSha"` |
| `CanonicalCloudTree` | `string` | `json:"canonicalCloudTree"` |
| `DeployedCloudImageDigest` | `string` | `json:"deployedCloudImageDigest"` |
| `AuthoritativeState` | `StageState` | `json:"authoritativeState"` |
| `IdentityDigests` | `WorkspaceLaunchAcceptanceBIdentityDigests` | `json:"identityDigests"` |

### WorkspaceLaunchAcceptanceBIdentityDigests

`packages/contracts/go/workspace_launch_resume.go:69`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `OperationIdentitySHA256` | `string` | `json:"operationIdentitySha256"` |
| `AccountIdentitySHA256` | `string` | `json:"accountIdentitySha256"` |
| `WorkspaceIdentitySHA256` | `string` | `json:"workspaceIdentitySha256"` |
| `QuoteIdentitySHA256` | `string` | `json:"quoteIdentitySha256"` |
| `KeyIdentitySHA256` | `string` | `json:"keyIdentitySha256"` |
| `DebitIdentitySHA256` | `string` | `json:"debitIdentitySha256"` |
| `ProviderIdentitySHA256` | `string` | `json:"providerIdentitySha256"` |

### WorkspaceRegistryRepository

`packages/contracts/go/workspace_registry_catalog.go:67`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Namespace` | `string` | `json:"namespace"` |
| `Repository` | `string` | `json:"repository"` |

### WorkspaceRegistryTag

`packages/contracts/go/workspace_registry_catalog.go:74`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Tag` | `string` | `json:"tag"` |

### WorkspaceRegistryImageResolution

`packages/contracts/go/workspace_registry_catalog.go:78`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Reference` | `string` | `json:"reference"` |
| `Digest` | `string` | `json:"digest"` |

### WorkspaceApplicationRetirementReceipt

`packages/contracts/go/workspace_resource_receipt.go:22`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `CurrentDeploymentID` | `string` | `json:"currentDeploymentId,omitempty"` |
| `Runtimes` | `[]WorkspaceApplicationRuntimeRetirementReceipt` | `json:"runtimes,omitempty"` |
| `Secrets` | `[]WorkspaceApplicationSecretRetirementReceipt` | `json:"secrets,omitempty"` |
| `RetainedGatewayKeyIDs` | `[]int64` | `json:"retainedGatewayKeyIds,omitempty"` |

### WorkspaceApplicationRuntimeRetirementReceipt

`packages/contracts/go/workspace_resource_receipt.go:29`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `RuntimeID` | `string` | `json:"runtimeId"` |
| `RuntimeOperationID` | `string` | `json:"runtimeOperationId"` |
| `State` | `string` | `json:"state"` |
| `ImageRetirement` | `[]WorkspaceApplicationRuntimeImageRetirement` | `json:"imageRetirement,omitempty"` |

### WorkspaceApplicationSecretRetirementReceipt

`packages/contracts/go/workspace_resource_receipt.go:36`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SecretRef` | `string` | `json:"secretRef"` |
| `Ownership` | `string` | `json:"ownership"` |
| `State` | `string` | `json:"state"` |

### WorkspaceResourceReceiptExecution

`packages/contracts/go/workspace_resource_receipt.go:6`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `OperationID` | `string` | `json:"operationId"` |
| `ResourceType` | `string` | `json:"resourceType"` |
| `ResourceID` | `string` | `json:"resourceId"` |
| `ComputeAllocationID` | `string` | `json:"computeAllocationId"` |
| `StorageID` | `string` | `json:"storageId"` |
| `AttachmentID` | `string` | `json:"attachmentId"` |
| `ProvisioningMode` | `WorkspaceProvisioningMode` | `json:"provisioningMode"` |

### WorkspaceRuntimePowerResult

`packages/contracts/go/workspace_runtime_power.go:20`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Binding` | `WorkspaceRuntimePowerInput` | `json:"binding"` |
| `State` | `string` | `json:"state"` |
| `Reason` | `string` | `json:"reason"` |

### WorkspaceRuntimePowerInput

`packages/contracts/go/workspace_runtime_power.go:4`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `RuntimeID` | `string` | `json:"runtimeId"` |
| `RuntimeOperationID` | `string` | `json:"runtimeOperationId"` |
| `PaidThrough` | `string` | `json:"paidThrough"` |
| `DesiredState` | `string` | `json:"desiredState"` |
| `IdempotencyKey` | `string` | `json:"idempotencyKey"` |
| `SuspensionReason` | `string` | `json:"suspensionReason,omitempty"` |
| `MissingResourceType` | `string` | `json:"missingResourceType,omitempty"` |
| `MissingResourceID` | `string` | `json:"missingResourceId,omitempty"` |

## control-plane


### FabricCatalog

`services/control-plane/internal/clients/fabric.go:100`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Owner` | `string` | `json:"owner"` |
| `WorkspacePackages` | `[]FabricWorkspacePackage` | `json:"workspacePackages"` |
| `StorageClasses` | `[]FabricStorageClass` | `json:"storageClasses"` |
| `IngressDomains` | `[]FabricIngressDomain` | `json:"ingressDomains"` |

### FabricWorkspacePackage

`services/control-plane/internal/clients/fabric.go:108`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id"` |
| `Name` | `string` | `json:"name"` |
| `SizeGB` | `int` | `json:"diskGb"` |
| `Available` | `bool` | `json:"available"` |

### FabricStorageClass

`services/control-plane/internal/clients/fabric.go:115`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id"` |
| `StorageClassName` | `string` | `json:"storageClassName"` |
| `Provider` | `string` | `json:"provider"` |
| `Available` | `bool` | `json:"available"` |

### FabricIngressDomain

`services/control-plane/internal/clients/fabric.go:122`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id"` |
| `Host` | `string` | `json:"host"` |
| `PathPattern` | `string` | `json:"pathPattern"` |
| `Available` | `bool` | `json:"available"` |

### ComputeAllocationInput

`services/control-plane/internal/clients/fabric.go:129`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id,omitempty"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `PackageID` | `string` | `json:"packageId"` |

### MonthlyPreflightInput

`services/control-plane/internal/clients/fabric.go:136`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ResourceType` | `string` | `json:"resourceType"` |
| `PackageID` | `string` | `json:"packageId"` |
| `SizeGB` | `int` | `json:"sizeGb,omitempty"` |
| `Zone` | `string` | `json:"zone,omitempty"` |

### MonthlyPreflight

`services/control-plane/internal/clients/fabric.go:143`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ResourceType` | `string` | `json:"resourceType"` |
| `PackageID` | `string` | `json:"packageId"` |
| `SizeGB` | `int` | `json:"sizeGb"` |
| `Zone` | `string` | `json:"zone"` |
| `Available` | `bool` | `json:"available"` |
| `ChargeType` | `string` | `json:"chargeType"` |
| `PeriodMonths` | `int` | `json:"periodMonths"` |
| `RenewFlag` | `string` | `json:"renewFlag"` |
| `ProviderPriceCNY` | `float64` | `json:"providerPriceCny"` |

### ComputeAllocation

`services/control-plane/internal/clients/fabric.go:155`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `PackageID` | `string` | `json:"packageId"` |
| `Status` | `string` | `json:"status"` |
| `Provider` | `string` | `json:"provider"` |
| `ProviderResourceID` | `string` | `json:"providerResourceId"` |
| `ProviderRequestID` | `string` | `json:"providerRequestId"` |
| `OperationID` | `string` | `json:"operationId,omitempty"` |
| `NodePoolID` | `string` | `json:"nodePoolId,omitempty"` |
| `InstanceID` | `string` | `json:"instanceId,omitempty"` |
| `CVMInstanceID` | `string` | `json:"cvmInstanceId,omitempty"` |
| `InstanceType` | `string` | `json:"instanceType,omitempty"` |
| `Zone` | `string` | `json:"zone,omitempty"` |
| `CVMStatus` | `string` | `json:"cvmStatus,omitempty"` |
| `MachineName` | `string` | `json:"machineName,omitempty"` |
| `DestroyState` | `string` | `json:"destroyState,omitempty"` |
| `ObservedAt` | `string` | `json:"observedAt,omitempty"` |
| `ReadbackID` | `string` | `json:"readbackId,omitempty"` |
| `TKEStatus` | `string` | `json:"tkeStatus,omitempty"` |
| `MachinePresent` | `*bool` | `json:"machinePresent,omitempty"` |
| `ChargeType` | `string` | `json:"chargeType,omitempty"` |
| `RenewFlag` | `string` | `json:"renewFlag,omitempty"` |
| `Deadline` | `string` | `json:"deadline,omitempty"` |
| `ProviderData` | `map[string]string` | `json:"providerData,omitempty"` |
| `CostTags` | `map[string]string` | `json:"costTags,omitempty"` |

### StorageVolumeInput

`services/control-plane/internal/clients/fabric.go:191`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id,omitempty"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ComputeID` | `string` | `json:"computeId"` |
| `Zone` | `string` | `json:"zone"` |
| `SizeGB` | `int` | `json:"sizeGb"` |

### StorageVolume

`services/control-plane/internal/clients/fabric.go:200`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id"` |
| `OperationID` | `string` | `json:"operationId,omitempty"` |
| `AccountID` | `string` | `json:"accountId,omitempty"` |
| `Provider` | `string` | `json:"provider,omitempty"` |
| `ProviderResourceID` | `string` | `json:"providerResourceId,omitempty"` |
| `ProviderRequestID` | `string` | `json:"providerRequestId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `Status` | `string` | `json:"status"` |
| `SizeGB` | `int` | `json:"sizeGb,omitempty"` |
| `CBSStatus` | `string` | `json:"cbsStatus,omitempty"` |
| `BindingPresent` | `*bool` | `json:"bindingPresent,omitempty"` |
| `DestroyState` | `string` | `json:"destroyState,omitempty"` |
| `ObservedAt` | `string` | `json:"observedAt,omitempty"` |
| `ReadbackID` | `string` | `json:"readbackId,omitempty"` |
| `DiskType` | `string` | `json:"diskType,omitempty"` |
| `RenewFlag` | `string` | `json:"renewFlag,omitempty"` |
| `Deadline` | `string` | `json:"deadline,omitempty"` |
| `Zone` | `string` | `json:"zone,omitempty"` |
| `ProviderData` | `map[string]string` | `json:"providerData,omitempty"` |
| `CostTags` | `map[string]string` | `json:"costTags,omitempty"` |

### StorageAttachmentInput

`services/control-plane/internal/clients/fabric.go:230`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ComputeID` | `string` | `json:"computeId"` |
| `VolumeID` | `string` | `json:"volumeId"` |

### StorageAttachment

`services/control-plane/internal/clients/fabric.go:237`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id"` |
| `OperationID` | `string` | `json:"operationId,omitempty"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ComputeID` | `string` | `json:"computeId,omitempty"` |
| `VolumeID` | `string` | `json:"volumeId"` |
| `Status` | `string` | `json:"status"` |
| `Provider` | `string` | `json:"provider,omitempty"` |
| `ProviderAttachmentID` | `string` | `json:"providerAttachmentId,omitempty"` |
| `ProviderRequestID` | `string` | `json:"providerRequestId"` |
| `MountPath` | `string` | `json:"mountPath,omitempty"` |

### WorkspaceRuntimeInput

`services/control-plane/internal/clients/fabric.go:250`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ComputeID` | `string` | `json:"computeId"` |
| `VolumeID` | `string` | `json:"volumeId"` |
| `AttachmentID` | `string` | `json:"attachmentId"` |
| `AttachmentOperationID` | `string` | `json:"attachmentOperationId"` |
| `RuntimeOperationID` | `string` | `json:"runtimeOperationId"` |
| `PreviousRuntimeOperationID` | `string` | `json:"previousRuntimeOperationId,omitempty"` |
| `ImageID` | `string` | `json:"imageId"` |
| `GatewaySecretRef` | `string` | `json:"gatewaySecretRef"` |

### GatewaySecretWriteInput

`services/control-plane/internal/clients/fabric.go:263`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `WorkspaceAPIKeyID` | `int64` | `json:"workspaceApiKeyId"` |
| `Fingerprint` | `string` | `json:"fingerprint"` |
| `GatewayAPIKey` | `string` | `json:"gatewayApiKey"` |

### GatewaySecretWriteResult

`services/control-plane/internal/clients/fabric.go:271`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SecretRef` | `string` | `json:"secretRef"` |
| `Version` | `string` | `json:"version"` |
| `Fingerprint` | `string` | `json:"fingerprint"` |

### WorkspaceRuntimeGatewaySecretInput

`services/control-plane/internal/clients/fabric.go:277`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `WorkspaceAPIKeyID` | `int64` | `json:"workspaceApiKeyId"` |
| `SecretRef` | `string` | `json:"secretRef"` |
| `Fingerprint` | `string` | `json:"fingerprint"` |

### WorkspaceRuntimeGatewaySecretBinding

`services/control-plane/internal/clients/fabric.go:285`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `WorkspaceAPIKeyID` | `int64` | `json:"workspaceApiKeyId"` |
| `SecretRef` | `string` | `json:"secretRef"` |
| `Fingerprint` | `string` | `json:"fingerprint"` |
| `Bound` | `bool` | `json:"bound"` |

### WorkspaceRuntimeObservation

`services/control-plane/internal/clients/fabric.go:303`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `State` | `string` | `json:"state"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `Runtime` | `*WorkspaceRuntime` | `json:"runtime,omitempty"` |

### WorkspaceRuntimeGatewaySecretObservation

`services/control-plane/internal/clients/fabric.go:310`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `State` | `string` | `json:"state"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `Binding` | `*WorkspaceRuntimeGatewaySecretBinding` | `json:"binding,omitempty"` |

### WorkspaceRuntimeDeleteResidual

`services/control-plane/internal/clients/fabric.go:343`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Kind` | `string` | `json:"kind"` |
| `Name` | `string` | `json:"name"` |

### WorkspaceRuntimeDeleteObservation

`services/control-plane/internal/clients/fabric.go:348`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `State` | `string` | `json:"state"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `Residuals` | `[]WorkspaceRuntimeDeleteResidual` | `json:"residuals,omitempty"` |
| `ObservedAt` | `string` | `json:"observedAt,omitempty"` |
| `ReadbackID` | `string` | `json:"readbackId,omitempty"` |

### ProviderFactInput

`services/control-plane/internal/clients/fabric.go:359`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ResourceType` | `string` | `json:"resourceType"` |
| `ResourceID` | `string` | `json:"resourceId"` |

### ProviderFactsBatchInput

`services/control-plane/internal/clients/fabric.go:366`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Items` | `[]ProviderFactInput` | `json:"items"` |

### ProviderResourceFacts

`services/control-plane/internal/clients/fabric.go:370`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `PackageOrSpec` | `string` | `json:"packageOrSpec,omitempty"` |
| `ProviderID` | `string` | `json:"providerId,omitempty"` |
| `Zone` | `string` | `json:"zone,omitempty"` |
| `Status` | `string` | `json:"status,omitempty"` |
| `CreatedAt` | `string` | `json:"createdAt,omitempty"` |
| `ExpiresAt` | `string` | `json:"expiresAt,omitempty"` |
| `LastReadAt` | `string` | `json:"lastReadAt,omitempty"` |
| `ComputeRuntimeBinding` | `*contracts.WorkspaceComputeRuntimeBinding` | `json:"computeRuntimeBinding,omitempty"` |

### ProviderFact

`services/control-plane/internal/clients/fabric.go:381`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Observation` | `*contracts.ResourceObservation` | `json:"observation,omitempty"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ResourceType` | `string` | `json:"resourceType"` |
| `ResourceID` | `string` | `json:"resourceId"` |
| `Available` | `bool` | `json:"available"` |
| `Facts` | `ProviderResourceFacts` | `json:"facts,omitempty"` |
| `ErrorCode` | `string` | `json:"errorCode,omitempty"` |

### ProviderFactsBatch

`services/control-plane/internal/clients/fabric.go:392`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Items` | `[]ProviderFact` | `json:"items"` |

### ProviderResourceMutation

`services/control-plane/internal/clients/fabric.go:396`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id"` |
| `OperationID` | `string` | `json:"operationId,omitempty"` |
| `AccountID` | `string` | `json:"accountId,omitempty"` |
| `WorkspaceID` | `string` | `json:"workspaceId,omitempty"` |
| `Status` | `string` | `json:"status"` |
| `ProviderRequestID` | `string` | `json:"providerRequestId,omitempty"` |

### RuntimeHealthSummary

`services/control-plane/internal/clients/fabric.go:405`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Total` | `int` | `json:"total"` |
| `Ready` | `int` | `json:"ready"` |
| `Unready` | `int` | `json:"unready"` |

### WorkspaceRuntime

`services/control-plane/internal/clients/fabric.go:411`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id"` |
| `OperationID` | `string` | `json:"operationId,omitempty"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `URL` | `string` | `json:"url,omitempty"` |
| `Status` | `string` | `json:"status"` |
| `ServiceName` | `string` | `json:"serviceName"` |
| `ImageID` | `string` | `json:"imageId,omitempty"` |
| `ProviderRequestID` | `string` | `json:"providerRequestId,omitempty"` |
| `Access` | `WorkspaceRuntimeAccess` | `json:"access,omitempty"` |
| `Ready` | `bool` | `json:"ready"` |
| `Checks` | `[]any` | `json:"checks"` |

### WorkspaceRuntimeAccess

`services/control-plane/internal/clients/fabric.go:428`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Username` | `string` | `json:"username,omitempty"` |
| `Password` | `string` | `json:"password,omitempty"` |
| `CredentialStatus` | `string` | `json:"credentialStatus,omitempty"` |
| `CredentialVersion` | `string` | `json:"credentialVersion,omitempty"` |
| `SecretRef` | `string` | `json:"secretRef,omitempty"` |

### fabricCapabilityClaims

`services/control-plane/internal/clients/fabric.go:458`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Version` | `int` | `json:"version"` |
| `Caller` | `string` | `json:"caller"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ResourceKind` | `string` | `json:"resourceKind"` |
| `ResourceID` | `string` | `json:"resourceId"` |
| `Action` | `string` | `json:"action"` |
| `OperationID` | `string` | `json:"operationId"` |
| `ExpiresAt` | `int64` | `json:"expiresAt"` |
| `BodySHA256` | `string` | `json:"bodySha256"` |

### WorkspaceApplicationRuntimeInput

`services/control-plane/internal/clients/fabric_application_runtime.go:14`

别名 → `contracts.WorkspaceApplicationRuntimeInput`
| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |

### WorkspaceApplicationRuntimeLifecycleInput

`services/control-plane/internal/clients/fabric_application_runtime.go:40`

别名 → `contracts.WorkspaceApplicationRuntimeLifecycleInput`
| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |

### WorkspaceApplicationRuntimeLifecycleResult

`services/control-plane/internal/clients/fabric_application_runtime.go:41`

别名 → `contracts.WorkspaceApplicationRuntimeLifecycleResult`
| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |

### WorkspaceApplicationRuntimeCredentials

`services/control-plane/internal/clients/fabric_application_runtime.go:42`

别名 → `contracts.WorkspaceApplicationRuntimeCredentials`
| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |

### WorkspaceApplicationGatewaySecretCleanupInput

`services/control-plane/internal/clients/fabric_application_runtime.go:89`

别名 → `contracts.WorkspaceApplicationGatewaySecretCleanupInput`
| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |

### WorkspaceRuntimeGatewayNetworkRecoveryResult

`services/control-plane/internal/clients/fabric_runtime_gateway_network_recovery.go:19`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `OperationID` | `string` | `json:"operationId"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ComputeID` | `string` | `json:"computeId"` |
| `RuntimeID` | `string` | `json:"runtimeId"` |
| `RuntimeServiceName` | `string` | `json:"runtimeServiceName"` |
| `GatewayContainerID` | `string` | `json:"gatewayContainerId"` |
| `NetworkID` | `string` | `json:"networkId"` |
| `NetworkName` | `string` | `json:"networkName"` |
| `Status` | `string` | `json:"status"` |
| `Runtime` | `WorkspaceRuntime` | `json:"runtime"` |

### WorkspaceRuntimeGatewayNetworkRecoveryInput

`services/control-plane/internal/clients/fabric_runtime_gateway_network_recovery.go:9`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ComputeID` | `string` | `json:"computeId"` |
| `RuntimeID` | `string` | `json:"runtimeId"` |
| `RuntimeOperationID` | `string` | `json:"runtimeOperationId"` |
| `RuntimeServiceName` | `string` | `json:"runtimeServiceName"` |
| `IdempotencyKey` | `string` | `json:"-"` |

### WorkspaceRuntimeImageReplacementInput

`services/control-plane/internal/clients/fabric_runtime_image_replacement.go:12`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `LaunchOperationID` | `string` | `json:"launchOperationId"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ComputeID` | `string` | `json:"computeId"` |
| `StorageID` | `string` | `json:"storageId"` |
| `AttachmentID` | `string` | `json:"attachmentId"` |
| `RuntimeID` | `string` | `json:"runtimeId"` |
| `RuntimeOperationID` | `string` | `json:"runtimeOperationId"` |
| `RuntimeServiceName` | `string` | `json:"runtimeServiceName"` |
| `PreviousImageDigest` | `string` | `json:"previousImageDigest"` |
| `ReplacementImageDigest` | `string` | `json:"replacementImageDigest"` |
| `IdempotencyKey` | `string` | `json:"-"` |

### WorkspaceRuntimeImageReplacementResult

`services/control-plane/internal/clients/fabric_runtime_image_replacement.go:27`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `OperationID` | `string` | `json:"operationId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `RuntimeID` | `string` | `json:"runtimeId"` |
| `PreviousImageDigest` | `string` | `json:"previousImageDigest"` |
| `ReplacementImageDigest` | `string` | `json:"replacementImageDigest"` |
| `Status` | `string` | `json:"status"` |
| `Runtime` | `WorkspaceRuntime` | `json:"runtime"` |

### WorkspaceLaunchGatewayCredential

`services/control-plane/internal/clients/fabric_workspace_launch.go:103`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `KeyID` | `int64` | `json:"keyId"` |
| `Value` | `string` | `json:"value"` |

### WorkspaceLaunchRuntimeImageRevision

`services/control-plane/internal/clients/fabric_workspace_launch.go:108`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `LaunchOperationID` | `string` | `json:"launchOperationId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `RuntimeOperationID` | `string` | `json:"runtimeOperationId"` |
| `AuthorizationDigest` | `string` | `json:"authorizationDigest"` |
| `PreviousImageDigest` | `string` | `json:"previousImageDigest"` |
| `ReplacementImageDigest` | `string` | `json:"replacementImageDigest"` |

### WorkspaceLaunchStageInput

`services/control-plane/internal/clients/fabric_workspace_launch.go:118`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Binding` | `WorkspaceLaunchStageBinding` | `json:"binding"` |
| `ProviderProfileRef` | `string` | `json:"providerProfileRef"` |
| `PreflightBindingRef` | `string` | `json:"providerBindingRef"` |
| `SpecDigest` | `string` | `json:"specDigest"` |
| `PackageID` | `string` | `json:"packageId"` |
| `SizeGB` | `int` | `json:"sizeGb"` |
| `WorkspaceImageDigest` | `string` | `json:"workspaceImageDigest"` |
| `ProvisioningMode` | `string` | `json:"provisioningMode,omitempty"` |
| `Resources` | `WorkspaceLaunchResources` | `json:"resources"` |
| `GatewayCredential` | `*WorkspaceLaunchGatewayCredential` | `json:"gatewayCredential,omitempty"` |
| `RuntimeImageRevision` | `*WorkspaceLaunchRuntimeImageRevision` | `json:"runtimeImageRevision,omitempty"` |

### WorkspaceLaunchStageResult

`services/control-plane/internal/clients/fabric_workspace_launch.go:132`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `State` | `string` | `json:"state"` |
| `Reason` | `string` | `json:"reason"` |
| `Binding` | `WorkspaceLaunchStageBinding` | `json:"binding"` |
| `Resources` | `WorkspaceLaunchResources` | `json:"resources"` |
| `Diagnostic` | `*WorkspaceLaunchStageDiagnostic` | `json:"diagnostic,omitempty"` |

### WorkspaceLaunchStageDiagnostic

`services/control-plane/internal/clients/fabric_workspace_launch.go:141`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Owner` | `string` | `json:"owner"` |
| `BlockReason` | `string` | `json:"blockReason"` |
| `ErrorCode` | `string` | `json:"errorCode,omitempty"` |
| `Retryable` | `bool` | `json:"retryable"` |
| `ObservedAt` | `string` | `json:"observedAt"` |
| `Checks` | `[]WorkspaceLaunchStageCheck` | `json:"checks,omitempty"` |

### WorkspaceLaunchStageCheck

`services/control-plane/internal/clients/fabric_workspace_launch.go:151`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Name` | `string` | `json:"name"` |
| `OK` | `bool` | `json:"ok"` |
| `Details` | `map[string]any` | `json:"details,omitempty"` |

### WorkspaceLaunchPreflightInput

`services/control-plane/internal/clients/fabric_workspace_launch.go:26`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `LaunchOperationID` | `string` | `json:"launchOperationId"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `PackageID` | `string` | `json:"packageId"` |
| `SizeGB` | `int` | `json:"sizeGb"` |
| `WorkspaceImageDigest` | `string` | `json:"workspaceImageDigest"` |
| `ProvisioningMode` | `string` | `json:"provisioningMode,omitempty"` |
| `RequestHash` | `string` | `json:"requestHash"` |

### WorkspaceLaunchPreflight

`services/control-plane/internal/clients/fabric_workspace_launch.go:38`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Available` | `bool` | `json:"available"` |
| `Reason` | `string` | `json:"reason"` |
| `LaunchOperationID` | `string` | `json:"launchOperationId"` |
| `RequestHash` | `string` | `json:"requestHash"` |
| `ProviderProfileRef` | `string` | `json:"providerProfileRef"` |
| `BindingRef` | `string` | `json:"providerBindingRef"` |
| `SpecDigest` | `string` | `json:"specDigest"` |

### WorkspaceLaunchPreflightReadInput

`services/control-plane/internal/clients/fabric_workspace_launch.go:51`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ProviderBindingRef` | `string` | `json:"providerBindingRef"` |

### WorkspaceLaunchPreflightBinding

`services/control-plane/internal/clients/fabric_workspace_launch.go:55`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `LaunchOperationID` | `string` | `json:"launchOperationId"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `PackageID` | `string` | `json:"packageId"` |
| `SizeGB` | `int` | `json:"sizeGb"` |
| `WorkspaceImageDigest` | `string` | `json:"workspaceImageDigest"` |
| `RequestHash` | `string` | `json:"requestHash"` |
| `ProviderProfileRef` | `string` | `json:"providerProfileRef"` |
| `ProviderBindingRef` | `string` | `json:"providerBindingRef"` |
| `SpecDigest` | `string` | `json:"specDigest"` |

### WorkspaceLaunchStageBinding

`services/control-plane/internal/clients/fabric_workspace_launch.go:69`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `LaunchOperationID` | `string` | `json:"launchOperationId"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `Stage` | `string` | `json:"stage"` |
| `Action` | `string` | `json:"action"` |
| `FabricOperationID` | `string` | `json:"fabricOperationId"` |
| `IdempotencyKey` | `string` | `json:"idempotencyKey"` |
| `RequestHash` | `string` | `json:"requestHash"` |
| `ExpectedResourceBinding` | `string` | `json:"expectedResourceBinding"` |

### WorkspaceLaunchResources

`services/control-plane/internal/clients/fabric_workspace_launch.go:82`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ComputeAllocationID` | `string` | `json:"computeAllocationId,omitempty"` |
| `ComputeBindingRef` | `string` | `json:"computeBindingRef,omitempty"` |
| `StorageID` | `string` | `json:"storageId,omitempty"` |
| `StorageBindingRef` | `string` | `json:"storageBindingRef,omitempty"` |
| `AttachmentID` | `string` | `json:"attachmentId,omitempty"` |
| `AttachmentBindingRef` | `string` | `json:"attachmentBindingRef,omitempty"` |
| `GatewaySecretRef` | `string` | `json:"gatewaySecretRef,omitempty"` |
| `GatewaySecretVersion` | `string` | `json:"gatewaySecretVersion,omitempty"` |
| `GatewaySecretFingerprint` | `string` | `json:"gatewaySecretFingerprint,omitempty"` |
| `SecretBindingRef` | `string` | `json:"secretBindingRef,omitempty"` |
| `RuntimeID` | `string` | `json:"runtimeId,omitempty"` |
| `RuntimeServiceName` | `string` | `json:"runtimeServiceName,omitempty"` |
| `RuntimeUsername` | `string` | `json:"runtimeUsername,omitempty"` |
| `RuntimeURL` | `string` | `json:"runtimeUrl,omitempty"` |
| `RuntimeCredentialStatus` | `string` | `json:"runtimeCredentialStatus,omitempty"` |
| `RuntimeCredentialVersion` | `string` | `json:"runtimeCredentialVersion,omitempty"` |
| `RuntimeCredentialSecretRef` | `string` | `json:"runtimeCredentialSecretRef,omitempty"` |
| `RuntimeBindingRef` | `string` | `json:"runtimeBindingRef,omitempty"` |

### ReconciliationInput

`services/control-plane/internal/clients/ledger.go:39`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Report` | `map[string]any` | `json:"report"` |

### ReconciliationResult

`services/control-plane/internal/clients/ledger.go:43`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id"` |
| `Status` | `string` | `json:"status"` |
| `Report` | `map[string]any` | `json:"report"` |
| `BlockNewWorkspaces` | `bool` | `json:"blockNewWorkspaces"` |
| `Reason` | `string` | `json:"reason"` |
| `Replayed` | `bool` | `json:"replayed"` |

### ReceiptInput

`services/control-plane/internal/clients/ledger.go:52`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Type` | `string` | `json:"type"` |
| `Status` | `string` | `json:"status"` |
| `Surface` | `string` | `json:"surface"` |
| `AccountID` | `string` | `json:"accountId,omitempty"` |
| `OrganizationID` | `string` | `json:"organizationId,omitempty"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ProjectID` | `string` | `json:"projectId,omitempty"` |
| `TaskID` | `string` | `json:"taskId,omitempty"` |
| `RequestID` | `string` | `json:"requestId,omitempty"` |
| `ApprovalID` | `string` | `json:"approvalId,omitempty"` |
| `JobID` | `string` | `json:"jobId,omitempty"` |
| `ArtifactID` | `string` | `json:"artifactId,omitempty"` |
| `ReviewID` | `string` | `json:"reviewId,omitempty"` |
| `Actor` | `map[string]any` | `json:"actor,omitempty"` |
| `Plan` | `map[string]any` | `json:"plan,omitempty"` |
| `Execution` | `map[string]any` | `json:"execution,omitempty"` |
| `Environment` | `map[string]any` | `json:"environment,omitempty"` |
| `InputRefs` | `map[string]any` | `json:"inputRefs,omitempty"` |
| `OutputRefs` | `map[string]any` | `json:"outputRefs,omitempty"` |
| `ReviewerChecks` | `map[string]any` | `json:"reviewerChecks,omitempty"` |
| `Cost` | `map[string]any` | `json:"cost,omitempty"` |
| `Owner` | `map[string]any` | `json:"owner,omitempty"` |
| `Continuation` | `map[string]any` | `json:"continuation,omitempty"` |
| `SupersedesReceiptID` | `string` | `json:"supersedesReceiptId,omitempty"` |

### Receipt

`services/control-plane/internal/clients/ledger.go:79`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `(embedded)` | `ReceiptInput` | `` |
| `ReceiptID` | `string` | `json:"receiptId"` |
| `ContinuationID` | `string` | `json:"continuationId"` |
| `CreatedAt` | `string` | `json:"createdAt"` |
| `Replayed` | `bool` | `json:"replayed"` |

### ReceiptPage

`services/control-plane/internal/clients/ledger.go:99`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Lookup` | `*contracts.ReceiptLookupScope` | `json:"lookup,omitempty"` |
| `Receipts` | `[]Receipt` | `json:"receipts"` |
| `NextCursor` | `string` | `json:"nextCursor"` |
| `HasMore` | `bool` | `json:"hasMore"` |

### Sub2APIIdentity

`services/control-plane/internal/clients/sub2api.go:162`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `int64` | `json:"id"` |
| `Email` | `string` | `json:"email"` |
| `Status` | `string` | `json:"status"` |

### sub2APIBalanceHistoryRecord

`services/control-plane/internal/clients/sub2api.go:1824`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Code` | `string` | `json:"code"` |
| `Notes` | `string` | `json:"notes"` |
| `Type` | `string` | `json:"type"` |
| `Value` | `*json.Number` | `json:"value"` |
| `BalanceAppliedValue` | `*json.Number` | `json:"balance_applied_value"` |
| `Status` | `string` | `json:"status"` |
| `UsedBy` | `*int64` | `json:"used_by"` |
| `UsedAt` | `*time.Time` | `json:"used_at"` |
| `CreatedAt` | `*time.Time` | `json:"created_at"` |

### sub2APIBalanceHistoryRecordsPage

`services/control-plane/internal/clients/sub2api.go:1922`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Items` | `[]sub2APIBalanceHistoryRecord` | `json:"items"` |
| `Total` | `int64` | `json:"total"` |
| `Page` | `int` | `json:"page"` |
| `PageSize` | `int` | `json:"page_size"` |
| `Pages` | `int` | `json:"pages"` |

### Sub2APIUserAuthentication

`services/control-plane/internal/clients/sub2api.go:213`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Identity` | `Sub2APIIdentity` | `json:"-"` |
| `AccessToken` | `string` | `json:"-"` |

### sub2APIKeyPayload

`services/control-plane/internal/clients/sub2api.go:303`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `int64` | `json:"id"` |
| `UserID` | `int64` | `json:"user_id"` |
| `Name` | `string` | `json:"name"` |
| `Key` | `string` | `json:"key"` |
| `GroupID` | `*int64` | `json:"group_id"` |
| `Status` | `string` | `json:"status"` |
| `IPWhitelist` | `[]string` | `json:"ip_whitelist"` |
| `IPBlacklist` | `[]string` | `json:"ip_blacklist"` |
| `Quota` | `*json.Number` | `json:"quota"` |
| `QuotaUsed` | `*json.Number` | `json:"quota_used"` |
| `RateLimit5h` | `*json.Number` | `json:"rate_limit_5h"` |
| `RateLimit1d` | `*json.Number` | `json:"rate_limit_1d"` |
| `RateLimit7d` | `*json.Number` | `json:"rate_limit_7d"` |
| `Usage5h` | `*json.Number` | `json:"usage_5h"` |
| `Usage1d` | `*json.Number` | `json:"usage_1d"` |
| `Usage7d` | `*json.Number` | `json:"usage_7d"` |
| `LastUsedAt` | `*time.Time` | `json:"last_used_at"` |
| `LastUsedIP` | `*string` | `json:"last_used_ip"` |
| `ExpiresAt` | `*time.Time` | `json:"expires_at"` |
| `CreatedAt` | `time.Time` | `json:"created_at"` |
| `UpdatedAt` | `time.Time` | `json:"updated_at"` |
| `CurrentConcurrency` | `int` | `json:"current_concurrency"` |

### Sub2APIUsageRecord

`services/control-plane/internal/clients/sub2api.go:336`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `UserID` | `int64` | `json:"user_id"` |
| `APIKeyID` | `int64` | `json:"api_key_id"` |
| `RequestID` | `string` | `json:"request_id"` |
| `CreatedAt` | `time.Time` | `json:"created_at"` |
| `Model` | `string` | `json:"model"` |
| `InboundEndpoint` | `string` | `json:"inbound_endpoint"` |
| `RequestType` | `string` | `json:"request_type"` |
| `InputTokens` | `int64` | `json:"input_tokens"` |
| `OutputTokens` | `int64` | `json:"output_tokens"` |
| `CacheCreationTokens` | `int64` | `json:"cache_creation_tokens"` |
| `CacheReadTokens` | `int64` | `json:"cache_read_tokens"` |
| `ActualCostUSDMicros` | `int64` | `json:"actual_cost_usd_micros"` |
| `DurationMS` | `*int64` | `json:"duration_ms"` |
| `FirstTokenMS` | `*int64` | `json:"first_token_ms"` |

### Sub2APIUsageStats

`services/control-plane/internal/clients/sub2api.go:367`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `TotalRequests` | `int64` | `json:"total_requests"` |
| `TotalInputTokens` | `int64` | `json:"total_input_tokens"` |
| `TotalOutputTokens` | `int64` | `json:"total_output_tokens"` |
| `TotalTokens` | `int64` | `json:"total_tokens"` |
| `TotalActualCostUSDMicros` | `int64` | `json:"total_actual_cost_usd_micros"` |

### Sub2APIBalanceHistoryEntry

`services/control-plane/internal/clients/sub2api.go:375`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Code` | `string` | `json:"code"` |
| `Type` | `string` | `json:"type"` |
| `ValueUSDMicros` | `int64` | `json:"value_usd_micros"` |
| `Status` | `string` | `json:"status"` |
| `UsedBy` | `*int64` | `json:"used_by"` |
| `UsedAt` | `*time.Time` | `json:"used_at"` |
| `CreatedAt` | `time.Time` | `json:"created_at"` |

### CreateWorkspaceInput

`services/control-plane/internal/controlplane/service.go:28`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `AccountID` | `string` | `json:"accountId"` |
| `Sub2APIUserID` | `int64` | `json:"-"` |
| `WorkspaceAPIKeyID` | `int64` | `json:"workspaceApiKeyId"` |
| `WorkspaceAPIKeyName` | `string` | `json:"-"` |
| `OwnerID` | `string` | `json:"ownerId"` |
| `Name` | `string` | `json:"name"` |
| `PackageID` | `string` | `json:"packageId"` |
| `AttachmentID` | `string` | `json:"attachmentId"` |
| `AttachmentOperationID` | `string` | `json:"attachmentOperationId"` |
| `RuntimeOperationID` | `string` | `json:"runtimeOperationId"` |
| `ComputeID` | `string` | `json:"computeAllocationId"` |
| `VolumeID` | `string` | `json:"storageId"` |
| `GatewaySecretRef` | `string` | `json:"-"` |
| `WorkspaceImageID` | `string` | `json:"-"` |

### ReconciliationInput

`services/control-plane/internal/controlplane/service.go:59`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Report` | `map[string]any` | `json:"report"` |

### StorageAttachmentInput

`services/control-plane/internal/controlplane/service.go:63`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ComputeID` | `string` | `json:"computeId"` |
| `VolumeID` | `string` | `json:"volumeId"` |

### WorkspaceProjection

`services/control-plane/internal/domain/workspace.go:3`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id"` |
| `AccountID` | `string` | `json:"accountId"` |
| `OwnerID` | `string` | `json:"ownerId"` |
| `Name` | `string` | `json:"name"` |
| `PackageID` | `string` | `json:"packageId"` |
| `Provider` | `string` | `json:"provider"` |
| `URL` | `string` | `json:"url"` |
| `Status` | `string` | `json:"status"` |
| `ComputeID` | `string` | `json:"computeAllocationId"` |
| `VolumeID` | `string` | `json:"storageId"` |
| `AttachmentID` | `string` | `json:"attachmentId"` |
| `RuntimeID` | `string` | `json:"runtimeId"` |
| `RuntimeServiceName` | `string` | `json:"runtimeServiceName,omitempty"` |
| `WorkspaceAPIKeyID` | `int64` | `json:"workspaceApiKeyId,omitempty"` |
| `RuntimeReady` | `bool` | `json:"runtimeReady"` |
| `RuntimeUsername` | `string` | `json:"runtimeUsername,omitempty"` |
| `CredentialStatus` | `string` | `json:"credentialStatus,omitempty"` |
| `CredentialVersion` | `string` | `json:"credentialVersion,omitempty"` |
| `CredentialSecretRef` | `string` | `json:"credentialSecretRef,omitempty"` |
| `ReceiptID` | `string` | `json:"receiptId"` |
| `ApplicationBinding` | `string` | `json:"applicationBinding"` |
| `ApplicationBindingVersion` | `int64` | `json:"applicationBindingVersion"` |

### acceptanceBAccountReconcileData

`services/control-plane/internal/server/account_reconcile.go:31`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `OperationMode` | `string` | `json:"operationMode"` |
| `Status` | `string` | `json:"status"` |
| `CustomerIdentitySHA256` | `string` | `json:"customerIdentitySha256"` |
| `AccountProvisionIdentitySHA256` | `string` | `json:"accountProvisionIdentitySha256"` |
| `WalletAdjustmentIdentitySHA256` | `string` | `json:"walletAdjustmentIdentitySha256"` |
| `ApprovalIdentitySHA256` | `string` | `json:"approvalIdentitySha256"` |
| `WorkspaceDebitIdentitySHA256` | `string` | `json:"workspaceDebitIdentitySha256"` |
| `LocalGraph` | `string` | `json:"localGraph"` |
| `RemoteIdentity` | `string` | `json:"remoteIdentity"` |
| `CustomerLogin` | `string` | `json:"customerLogin"` |
| `Wallet` | `string` | `json:"wallet"` |
| `WalletUSDMicros` | `string` | `json:"walletUsdMicros,omitempty"` |
| `WalletAdjustment` | `string` | `json:"walletAdjustment"` |
| `ApprovalState` | `string` | `json:"approvalState"` |
| `WorkspaceLaunchState` | `string` | `json:"workspaceLaunchState"` |
| `WorkspaceState` | `string` | `json:"workspaceState"` |
| `WorkspaceKeyState` | `string` | `json:"workspaceKeyState"` |
| `WorkspaceReceiptState` | `string` | `json:"workspaceReceiptState"` |
| `WorkspaceDebitState` | `string` | `json:"workspaceDebitState"` |
| `WorkspaceCount` | `int` | `json:"workspaceCount"` |
| `LaunchCount` | `int` | `json:"launchCount"` |
| `KeyCount` | `int` | `json:"keyCount"` |
| `ReceiptCount` | `int` | `json:"receiptCount"` |
| `ReadbackError` | `string` | `json:"readbackError,omitempty"` |

### controlPlaneRecord

`services/control-plane/internal/server/ent_state_store.go:37`

别名 → `map[string]any`
| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |

### controlPlaneRecordSet

`services/control-plane/internal/server/ent_state_store.go:38`

别名 → `map[string]controlPlaneRecord`
| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |

### workspaceBillingState

`services/control-plane/internal/server/ent_state_store_workspace.go:60`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ResourceBillingEnabled` | `*bool` | `json:"resourceBillingEnabled,omitempty"` |
| `AutoRenew` | `bool` | `json:"autoRenew"` |
| `AuthorizedBy` | `string` | `json:"authorizedBy"` |
| `AuthorizedAt` | `string` | `json:"authorizedAt"` |
| `PackageID` | `string` | `json:"packageId"` |
| `StorageGB` | `int64` | `json:"storageGb"` |
| `PriceVersion` | `string` | `json:"priceVersion"` |
| `Currency` | `string` | `json:"currency"` |
| `BillingUnit` | `string` | `json:"billingUnit"` |
| `ComputeUSDMicros` | `int64` | `json:"computeUsdMicros"` |
| `StorageUSDMicros` | `int64` | `json:"storageUsdMicros"` |
| `TotalUSDMicros` | `int64` | `json:"totalUsdMicros"` |
| `PeriodStart` | `string` | `json:"periodStart"` |
| `PaidThrough` | `string` | `json:"paidThrough"` |
| `NextRenewalAt` | `string` | `json:"nextRenewalAt"` |
| `BillingAnchorDay` | `int64` | `json:"billingAnchorDay"` |
| `RenewalStatus` | `string` | `json:"renewalStatus"` |
| `ComputeAllocationID` | `string` | `json:"computeAllocationId"` |
| `StorageID` | `string` | `json:"storageId"` |
| `ManualReviewReason` | `string` | `json:"-"` |

### operatorRuntimeObservation

`services/control-plane/internal/server/operator_runtime_observations.go:14`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ObjectRef` | `string` | `json:"objectRef,omitempty"` |
| `WorkspaceID` | `string` | `json:"workspaceId,omitempty"` |
| `RuntimeID` | `string` | `json:"runtimeId,omitempty"` |
| `BusinessState` | `string` | `json:"businessState,omitempty"` |
| `DesiredState` | `contracts.ResourceObservedState` | `json:"desiredState,omitempty"` |
| `ObservedState` | `contracts.ResourceObservedState` | `json:"observedState,omitempty"` |
| `Ownership` | `contracts.RuntimeOwnership` | `json:"ownership,omitempty"` |
| `Status` | `string` | `json:"status"` |
| `ReasonCode` | `string` | `json:"reasonCode,omitempty"` |
| `DeleteStage` | `string` | `json:"deleteStage,omitempty"` |
| `DeletePageState` | `string` | `json:"deletePageState,omitempty"` |
| `DeleteReasonCode` | `string` | `json:"deleteReasonCode,omitempty"` |
| `DeleteLastReadbackAt` | `string` | `json:"deleteLastReadbackAt,omitempty"` |
| `DeleteNextRetryAt` | `string` | `json:"deleteNextRetryAt,omitempty"` |
| `DeleteReceiptID` | `string` | `json:"deleteReceiptId,omitempty"` |

### operatorRuntimeObservations

`services/control-plane/internal/server/operator_runtime_observations.go:35`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ObservedAt` | `string` | `json:"observedAt"` |
| `OwnershipScope` | `string` | `json:"ownershipScope"` |
| `Ready` | `bool` | `json:"ready"` |
| `BusinessTotal` | `int` | `json:"businessTotal"` |
| `ObservedTotal` | `int` | `json:"observedTotal"` |
| `RunningCount` | `int` | `json:"runningCount"` |
| `SuspendedCount` | `int` | `json:"suspendedCount"` |
| `PendingCount` | `int` | `json:"pendingCount"` |
| `AttentionCount` | `int` | `json:"attentionCount"` |
| `UnmatchedCount` | `int` | `json:"unmatchedCount"` |
| `Items` | `[]operatorRuntimeObservation` | `json:"items"` |

### workspaceLaunchRecoveryDTO

`services/control-plane/internal/server/routes_admin.go:1371`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Closeout` | `*workspaceLaunchCloseoutDTO` | `json:"closeout,omitempty"` |
| `OperationID` | `string` | `json:"operationId"` |
| `LaunchVersion` | `int` | `json:"launchVersion"` |
| `Status` | `contracts.LaunchStatus` | `json:"status"` |
| `Stage` | `contracts.Stage` | `json:"stage"` |
| `AllowedActions` | `[]string` | `json:"allowedActions"` |

### AnnouncementDTO

`services/control-plane/internal/server/routes_announcements.go:15`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id"` |
| `Title` | `string` | `json:"title"` |
| `Body` | `string` | `json:"body"` |
| `Status` | `string` | `json:"status"` |
| `StartsAt` | `string` | `json:"startsAt,omitempty"` |
| `EndsAt` | `string` | `json:"endsAt,omitempty"` |
| `PublishedAt` | `string` | `json:"publishedAt,omitempty"` |
| `CreatedAt` | `string` | `json:"createdAt"` |
| `UpdatedAt` | `string` | `json:"updatedAt"` |
| `Read` | `bool` | `json:"read"` |

### AnnouncementPageDTO

`services/control-plane/internal/server/routes_announcements.go:28`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Items` | `[]AnnouncementDTO` | `json:"items"` |
| `Total` | `int` | `json:"total"` |
| `Page` | `int` | `json:"page"` |
| `PageSize` | `int` | `json:"pageSize"` |

### AnnouncementReadDTO

`services/control-plane/internal/server/routes_announcements.go:35`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AnnouncementID` | `string` | `json:"announcementId"` |
| `ReadAt` | `string` | `json:"readAt"` |

### AnnouncementDraftRequest

`services/control-plane/internal/server/routes_announcements.go:40`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Title` | `string` | `json:"title"` |
| `Body` | `string` | `json:"body"` |
| `StartsAt` | `string` | `json:"startsAt"` |
| `EndsAt` | `string` | `json:"endsAt"` |

### AnnouncementScheduleRequest

`services/control-plane/internal/server/routes_announcements.go:47`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `StartsAt` | `string` | `json:"startsAt"` |
| `EndsAt` | `string` | `json:"endsAt"` |

### createGatewayKeyRequest

`services/control-plane/internal/server/routes_gateway.go:152`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Name` | `string` | `json:"name"` |
| `GroupID` | `string` | `json:"groupId"` |
| `IPWhitelist` | `[]string` | `json:"ipWhitelist,omitempty"` |
| `IPBlacklist` | `[]string` | `json:"ipBlacklist,omitempty"` |
| `QuotaUSDMicros` | `int64` | `json:"quotaUsdMicros"` |
| `ExpiresInDays` | `*int` | `json:"expiresInDays,omitempty"` |
| `RateLimit5hUSDMicros` | `int64` | `json:"rateLimit5hUsdMicros,omitempty"` |
| `RateLimit1dUSDMicros` | `int64` | `json:"rateLimit1dUsdMicros,omitempty"` |
| `RateLimit7dUSDMicros` | `int64` | `json:"rateLimit7dUsdMicros,omitempty"` |

### updateGatewayKeyRequest

`services/control-plane/internal/server/routes_gateway.go:164`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Name` | `*string` | `json:"name,omitempty"` |
| `GroupID` | `*string` | `json:"groupId,omitempty"` |
| `IPWhitelist` | `*[]string` | `json:"ipWhitelist,omitempty"` |
| `IPBlacklist` | `*[]string` | `json:"ipBlacklist,omitempty"` |
| `QuotaUSDMicros` | `*int64` | `json:"quotaUsdMicros,omitempty"` |
| `ExpiresAt` | `*string` | `json:"expiresAt,omitempty"` |
| `RateLimit5hUSDMicros` | `*int64` | `json:"rateLimit5hUsdMicros,omitempty"` |
| `RateLimit1dUSDMicros` | `*int64` | `json:"rateLimit1dUsdMicros,omitempty"` |
| `RateLimit7dUSDMicros` | `*int64` | `json:"rateLimit7dUsdMicros,omitempty"` |
| `ResetQuota` | `*bool` | `json:"resetQuota,omitempty"` |
| `ResetRateLimitUsage` | `*bool` | `json:"resetRateLimitUsage,omitempty"` |
| `Enabled` | `*bool` | `json:"enabled,omitempty"` |

### gatewayKeyCommandEvidence

`services/control-plane/internal/server/routes_gateway.go:179`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `int64` | `json:"id"` |
| `Name` | `string` | `json:"name,omitempty"` |
| `GroupID` | `int64` | `json:"groupId,omitempty"` |
| `Status` | `string` | `json:"status"` |
| `IPWhitelist` | `[]string` | `json:"ipWhitelist,omitempty"` |
| `IPBlacklist` | `[]string` | `json:"ipBlacklist,omitempty"` |
| `QuotaUSDMicros` | `int64` | `json:"quotaUsdMicros,omitempty"` |
| `RateLimit5hUSDMicros` | `int64` | `json:"rateLimit5hUsdMicros,omitempty"` |
| `RateLimit1dUSDMicros` | `int64` | `json:"rateLimit1dUsdMicros,omitempty"` |
| `RateLimit7dUSDMicros` | `int64` | `json:"rateLimit7dUsdMicros,omitempty"` |
| `ExpiresAt` | `string` | `json:"expiresAt,omitempty"` |

### gatewayKeyCommandResult

`services/control-plane/internal/server/routes_gateway.go:193`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `RequestHash` | `string` | `json:"requestHash"` |
| `KeyID` | `int64` | `json:"keyId,omitempty"` |
| `TargetStatus` | `string` | `json:"targetStatus"` |
| `Readback` | `*gatewayKeyCommandEvidence` | `json:"readback,omitempty"` |

### SessionDelegatedCredential

`services/control-plane/internal/server/session_credential_vault.go:13`

别名 → `clients.SessionDelegatedCredential`
| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |

### workspaceCreateOperationResult

`services/control-plane/internal/server/table_store.go:40`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `RequestHash` | `string` | `json:"requestHash"` |
| `LeaseExpiresAt` | `*time.Time` | `json:"leaseExpiresAt,omitempty"` |
| `Workspace` | `domain.WorkspaceProjection` | `json:"workspace"` |
| `AcceptedBillingState` | `map[string]any` | `json:"acceptedBillingState,omitempty"` |

### walletAdjustmentRequest

`services/control-plane/internal/server/wallet_adjustment.go:29`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Kind` | `string` | `json:"kind"` |
| `AmountUSD` | `string` | `json:"amountUsd"` |
| `Reason` | `string` | `json:"reason"` |
| `RelatedOperationID` | `string` | `json:"relatedOperationId,omitempty"` |
| `ConfirmationAccountID` | `string` | `json:"confirmationAccountId"` |

### walletAdjustmentRecoveryRequest

`services/control-plane/internal/server/wallet_adjustment.go:37`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AccountID` | `string` | `json:"accountId"` |
| `EvidenceRef` | `string` | `json:"evidenceRef"` |

### walletAdjustmentUpstreamFailure

`services/control-plane/internal/server/wallet_adjustment.go:42`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Phase` | `string` | `json:"phase"` |
| `HTTPStatus` | `int` | `json:"httpStatus,omitempty"` |
| `ErrorCode` | `string` | `json:"errorCode"` |
| `RequestID` | `string` | `json:"requestId,omitempty"` |

### walletAdjustmentOperation

`services/control-plane/internal/server/wallet_adjustment.go:64`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `PersistedResult` | `string` | `json:"-"` |
| `PersistedStatus` | `string` | `json:"-"` |
| `RequestHash` | `string` | `json:"requestHash"` |
| `Phase` | `string` | `json:"phase"` |
| `AccountID` | `string` | `json:"accountId"` |
| `Sub2APIUserID` | `int64` | `json:"sub2apiUserId"` |
| `Kind` | `string` | `json:"kind"` |
| `AmountUSDMicros` | `int64` | `json:"amountUsdMicros"` |
| `AmountUSD` | `string` | `json:"amountUsd"` |
| `Reason` | `string` | `json:"reason"` |
| `RelatedOperationID` | `string` | `json:"relatedOperationId,omitempty"` |
| `ActorUserID` | `string` | `json:"actorUserId"` |
| `CanonicalRedeemCode` | `string` | `json:"canonicalRedeemCode,omitempty"` |
| `RedeemCodeVersion` | `string` | `json:"redeemCodeVersion,omitempty"` |
| `LegacySupersession` | `string` | `json:"legacySupersessionStatus,omitempty"` |
| `AdjustmentAttempted` | `bool` | `json:"adjustmentAttempted,omitempty"` |
| `BeforeBalanceKnown` | `bool` | `json:"beforeBalanceKnown,omitempty"` |
| `BeforeBalanceMicros` | `int64` | `json:"beforeBalanceUsdMicros,omitempty"` |
| `BeforeBalanceReadAt` | `string` | `json:"beforeBalanceReadAt,omitempty"` |
| `AfterBalanceKnown` | `bool` | `json:"afterBalanceKnown,omitempty"` |
| `AfterBalanceMicros` | `int64` | `json:"afterBalanceUsdMicros,omitempty"` |
| `AfterBalanceReadAt` | `string` | `json:"afterBalanceReadAt,omitempty"` |
| `BalanceHistoryRef` | `string` | `json:"balanceHistoryRef,omitempty"` |
| `BalanceHistoryUsedAt` | `string` | `json:"balanceHistoryUsedAt,omitempty"` |
| `ReceiptID` | `string` | `json:"receiptId,omitempty"` |
| `ErrorCode` | `string` | `json:"errorCode,omitempty"` |
| `RecoveryRequestHash` | `string` | `json:"recoveryRequestHash,omitempty"` |
| `RecoveryAttempted` | `bool` | `json:"recoveryAttempted,omitempty"` |
| `RecoveryEvidenceRef` | `string` | `json:"recoveryEvidenceRef,omitempty"` |
| `RecoveryActorUserID` | `string` | `json:"recoveryActorUserId,omitempty"` |
| `RecoveryAuthorizedAt` | `string` | `json:"recoveryAuthorizedAt,omitempty"` |
| `UpstreamFailure` | `*walletAdjustmentUpstreamFailure` | `json:"upstreamFailure,omitempty"` |
| `CreatedAt` | `string` | `json:"createdAt"` |
| `UpdatedAt` | `string` | `json:"updatedAt"` |
| `Status` | `string` | `json:"-"` |

### walletRefundChargeFacts

`services/control-plane/internal/server/wallet_adjustment.go:654`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AccountID` | `string` | `json:"accountId"` |
| `Sub2APIUserID` | `int64` | `json:"sub2apiUserId"` |
| `RedeemCode` | `string` | `json:"sub2apiRedeemCode"` |
| `TotalChargeUSDMicros` | `int64` | `json:"totalChargeUsdMicros"` |
| `TotalUSDMicros` | `int64` | `json:"totalUsdMicros"` |
| `Phase` | `string` | `json:"phase"` |
| `ChargeConfirmation` | `*walletRefundCharge` | `json:"chargeConfirmation"` |

### walletRefundCharge

`services/control-plane/internal/server/wallet_adjustment.go:664`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Code` | `string` | `json:"code"` |
| `UserID` | `int64` | `json:"userId"` |
| `AmountUSDMicros` | `int64` | `json:"chargeUsdMicros"` |
| `Status` | `string` | `json:"status"` |

### workspaceApplicationCapabilities

`services/control-plane/internal/server/workspace_application_access.go:14`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Credentials` | `bool` | `json:"credentials"` |
| `Gateway` | `bool` | `json:"gateway"` |

### workspaceCurrentApplication

`services/control-plane/internal/server/workspace_application_access.go:19`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `OperationID` | `string` | `json:"operationId"` |
| `ApplicationID` | `string` | `json:"applicationId"` |
| `Revision` | `string` | `json:"revision"` |
| `Status` | `string` | `json:"status"` |
| `EntryURL` | `string` | `json:"entryUrl"` |
| `Capabilities` | `workspaceApplicationCapabilities` | `json:"capabilities"` |

### workspaceApplicationInstallationProjection

`services/control-plane/internal/server/workspace_application_access.go:28`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `OperationID` | `string` | `json:"operationId"` |
| `ApplicationID` | `string` | `json:"applicationId"` |
| `Revision` | `string` | `json:"revision"` |
| `Status` | `string` | `json:"status"` |
| `CanResume` | `bool` | `json:"canResume"` |
| `CanRetry` | `bool` | `json:"canRetry"` |

### workspaceApplicationDeploymentIntent

`services/control-plane/internal/server/workspace_application_deployment.go:38`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Version` | `int` | `json:"version"` |
| `RequestHash` | `string` | `json:"requestHash"` |
| `OperationID` | `string` | `json:"operationId"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ComputeID` | `string` | `json:"computeId"` |
| `StorageID` | `string` | `json:"storageId"` |
| `AttachmentID` | `string` | `json:"attachmentId"` |
| `ApplicationID` | `string` | `json:"applicationId"` |
| `TargetRevision` | `string` | `json:"targetRevision"` |
| `RevisionDigest` | `string` | `json:"revisionDigest"` |
| `ConfigurationDigest` | `string` | `json:"configurationDigest"` |
| `ClientConfigurationDigest` | `string` | `json:"clientConfigurationDigest,omitempty"` |
| `SecretBindingVersions` | `[]string` | `json:"secretBindingVersions,omitempty"` |
| `DataBindingIDs` | `[]string` | `json:"dataBindingIds,omitempty"` |
| `ExpectedWorkspaceVersion` | `int64` | `json:"expectedWorkspaceVersion"` |
| `CurrentBinding` | `string` | `json:"currentBinding"` |
| `Configuration` | `contracts.WorkspaceApplicationRuntimeConfiguration` | `json:"configuration,omitempty"` |
| `SecretBindings` | `[]contracts.WorkspaceApplicationRuntimeSecretBinding` | `json:"secretBindings,omitempty"` |
| `DataSourceRuntimeOperationID` | `string` | `json:"dataSourceRuntimeOperationId,omitempty"` |
| `DataLayout` | `string` | `json:"dataLayout,omitempty"` |
| `DataBindingID` | `string` | `json:"dataBindingId,omitempty"` |
| `PreviousDeploymentID` | `string` | `json:"previousDeploymentId,omitempty"` |
| `LegacyPredecessor` | `*contracts.WorkspaceRuntimePowerInput` | `json:"legacyPredecessor,omitempty"` |
| `PredecessorObservation` | `*contracts.WorkspaceApplicationRuntimeLifecycleResult` | `json:"predecessorObservation,omitempty"` |
| `WorkspaceAPIKeyID` | `int64` | `json:"workspaceApiKeyId,omitempty"` |
| `OriginOperationID` | `string` | `json:"originOperationId,omitempty"` |
| `Phase` | `string` | `json:"phase"` |
| `FailurePhase` | `string` | `json:"failurePhase,omitempty"` |
| `CreatedAt` | `string` | `json:"createdAt"` |
| `RuntimeObservation` | `*contracts.WorkspaceApplicationRuntimeObservation` | `json:"runtimeObservation,omitempty"` |
| `ActivationAt` | `string` | `json:"activationAt,omitempty"` |
| `ReceiptID` | `string` | `json:"receiptId,omitempty"` |
| `LastError` | `string` | `json:"lastError,omitempty"` |

### workspaceApplicationLifecycleOperation

`services/control-plane/internal/server/workspace_application_lifecycle.go:15`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Runtimes` | `[]workspaceApplicationLifecycleRuntime` | `json:"runtimes"` |

### workspaceApplicationLifecycleRuntime

`services/control-plane/internal/server/workspace_application_lifecycle.go:19`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Input` | `contracts.WorkspaceApplicationRuntimeLifecycleInput` | `json:"input"` |
| `IdempotencyKey` | `string` | `json:"idempotencyKey"` |
| `Result` | `contracts.WorkspaceApplicationRuntimeLifecycleResult` | `json:"result"` |

### workspaceApplicationSecretCleanup

`services/control-plane/internal/server/workspace_application_secret_cleanup.go:13`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Secrets` | `[]workspaceApplicationSecretRetirement` | `json:"secrets"` |
| `RetainedGatewayKeyIDs` | `[]int64` | `json:"retainedGatewayKeyIds,omitempty"` |

### workspaceApplicationSecretRetirement

`services/control-plane/internal/server/workspace_application_secret_cleanup.go:18`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SecretRef` | `string` | `json:"secretRef"` |
| `Ownership` | `string` | `json:"ownership"` |
| `State` | `string` | `json:"state"` |

### workspaceDefaultApplicationRequest

`services/control-plane/internal/server/workspace_default_application.go:21`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `OperationID` | `string` | `json:"operationId"` |
| `LaunchOperationID` | `string` | `json:"launchOperationId"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `OwnerUserID` | `string` | `json:"ownerUserId"` |
| `Sub2APIUserID` | `int64` | `json:"sub2apiUserId"` |
| `WorkspaceKeyGroupID` | `int64` | `json:"workspaceKeyGroupId"` |
| `Revision` | `contracts.WorkspaceApplicationRevision` | `json:"revision"` |
| `Phase` | `string` | `json:"phase"` |
| `GatewaySecret` | `*clients.GatewaySecretWriteResult` | `json:"gatewaySecret,omitempty"` |
| `WorkspaceAPIKeyID` | `int64` | `json:"workspaceApiKeyId,omitempty"` |
| `DeploymentID` | `string` | `json:"deploymentId,omitempty"` |
| `LastError` | `string` | `json:"lastError,omitempty"` |

### workspaceDeleteReplayAuthorization

`services/control-plane/internal/server/workspace_delete.go:131`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion,omitempty"` |
| `AuthorizationID` | `string` | `json:"authorizationId,omitempty"` |
| `IdempotencyKey` | `string` | `json:"idempotencyKey,omitempty"` |
| `State` | `string` | `json:"state,omitempty"` |
| `LeaseGeneration` | `int` | `json:"leaseGeneration,omitempty"` |
| `LeaseExpiresAt` | `string` | `json:"leaseExpiresAt,omitempty"` |
| `DispatchStartedAt` | `string` | `json:"dispatchStartedAt,omitempty"` |
| `ConsumedAt` | `string` | `json:"consumedAt,omitempty"` |

### workspaceDeleteOperation

`services/control-plane/internal/server/workspace_delete.go:36`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `OperationID` | `string` | `json:"operationId"` |
| `RequestHash` | `string` | `json:"requestHash"` |
| `AccountID` | `string` | `json:"accountId"` |
| `OwnerUserID` | `string` | `json:"ownerUserId"` |
| `Sub2APIUserID` | `int64` | `json:"sub2apiUserId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ResourceType` | `string` | `json:"resourceType"` |
| `ResourceID` | `string` | `json:"resourceId"` |
| `LaunchOperationID` | `string` | `json:"launchOperationId"` |
| `LaunchReceiptID` | `string` | `json:"launchReceiptId"` |
| `RuntimeID` | `string` | `json:"runtimeId"` |
| `RuntimeServiceName` | `string` | `json:"runtimeServiceName"` |
| `ComputeID` | `string` | `json:"computeId"` |
| `StorageID` | `string` | `json:"storageId"` |
| `AttachmentID` | `string` | `json:"attachmentId"` |
| `WorkspaceAPIKeyID` | `int64` | `json:"workspaceApiKeyId"` |
| `GatewaySecretRef` | `string` | `json:"gatewaySecretRef"` |
| `GatewayFingerprint` | `string` | `json:"gatewayFingerprint"` |
| `DeletionReceiptID` | `string` | `json:"deletionReceiptId,omitempty"` |
| `Phase` | `string` | `json:"phase"` |
| `Status` | `string` | `json:"status"` |
| `RuntimeStatus` | `string` | `json:"runtimeStatus,omitempty"` |
| `SecretStatus` | `string` | `json:"secretStatus,omitempty"` |
| `AttachmentStatus` | `string` | `json:"attachmentStatus,omitempty"` |
| `StorageStatus` | `string` | `json:"storageStatus,omitempty"` |
| `ComputeStatus` | `string` | `json:"computeStatus,omitempty"` |
| `ComputeReadbacks` | `int` | `json:"computeReadbacks,omitempty"` |
| `MaxComputeReadbacks` | `int` | `json:"maxComputeReadbacks,omitempty"` |
| `ComputeReadbackNotBefore` | `string` | `json:"computeReadbackNotBefore,omitempty"` |
| `KeyStatus` | `string` | `json:"keyStatus,omitempty"` |
| `KeyDeleteAttempted` | `bool` | `json:"keyDeleteAttempted,omitempty"` |
| `KeyDeleteReplay` | `workspaceDeleteReplayAuthorization` | `json:"keyDeleteReplay,omitempty"` |
| `ProvisioningMode` | `string` | `json:"provisioningMode,omitempty"` |
| `CurrentApplicationDeploymentID` | `string` | `json:"currentApplicationDeploymentId,omitempty"` |
| `LaunchFulfilledAt` | `string` | `json:"launchFulfilledAt,omitempty"` |
| `StorageProviderResourceID` | `string` | `json:"storageProviderResourceId,omitempty"` |
| `ComputeMachineName` | `string` | `json:"computeMachineName,omitempty"` |
| `ComputeCVMInstanceID` | `string` | `json:"computeCvmInstanceId,omitempty"` |
| `DeletedAt` | `string` | `json:"deletedAt,omitempty"` |
| `StageEvidence` | `[]contracts.WorkspaceDeleteStageEvidence` | `json:"stageEvidence,omitempty"` |
| `ApplicationCleanup` | `*workspaceApplicationLifecycleOperation` | `json:"applicationCleanup,omitempty"` |
| `ApplicationSecrets` | `*workspaceApplicationSecretCleanup` | `json:"applicationSecrets,omitempty"` |
| `LastErrorCode` | `string` | `json:"lastErrorCode,omitempty"` |
| `CreatedAt` | `string` | `json:"createdAt"` |

### workspaceDeleteLegacyOperation

`services/control-plane/internal/server/workspace_delete.go:90`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `OperationID` | `string` | `json:"operationId"` |
| `RequestHash` | `string` | `json:"requestHash"` |
| `AccountID` | `string` | `json:"accountId"` |
| `OwnerUserID` | `string` | `json:"ownerUserId"` |
| `Sub2APIUserID` | `int64` | `json:"sub2apiUserId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `LaunchOperationID` | `string` | `json:"launchOperationId"` |
| `RuntimeID` | `string` | `json:"runtimeId"` |
| `ComputeID` | `string` | `json:"computeId"` |
| `StorageID` | `string` | `json:"storageId"` |
| `AttachmentID` | `string` | `json:"attachmentId"` |
| `WorkspaceAPIKeyID` | `int64` | `json:"workspaceApiKeyId"` |
| `GatewaySecretRef` | `string` | `json:"gatewaySecretRef"` |
| `GatewayFingerprint` | `string` | `json:"gatewayFingerprint"` |
| `DebitCode` | `string` | `json:"debitCode"` |
| `PurchaseReceiptID` | `string` | `json:"purchaseReceiptId"` |
| `PurchaseReceipt` | `clients.ReceiptInput` | `json:"purchaseReceipt"` |
| `RefundCode` | `string` | `json:"refundCode"` |
| `RefundReceiptID` | `string` | `json:"refundReceiptId,omitempty"` |
| `TotalUSDMicros` | `int64` | `json:"totalUsdMicros"` |
| `Phase` | `string` | `json:"phase"` |
| `Status` | `string` | `json:"status"` |
| `RuntimeStatus` | `string` | `json:"runtimeStatus,omitempty"` |
| `SecretStatus` | `string` | `json:"secretStatus,omitempty"` |
| `AttachmentStatus` | `string` | `json:"attachmentStatus,omitempty"` |
| `StorageStatus` | `string` | `json:"storageStatus,omitempty"` |
| `ComputeStatus` | `string` | `json:"computeStatus,omitempty"` |
| `ComputeReadbacks` | `int` | `json:"computeReadbacks,omitempty"` |
| `MaxComputeReadbacks` | `int` | `json:"maxComputeReadbacks,omitempty"` |
| `ComputeReadbackNotBefore` | `string` | `json:"computeReadbackNotBefore,omitempty"` |
| `KeyStatus` | `string` | `json:"keyStatus,omitempty"` |
| `KeyDeleteAttempted` | `bool` | `json:"keyDeleteAttempted,omitempty"` |
| `RefundAttempted` | `bool` | `json:"refundAttempted,omitempty"` |
| `KeyDeleteReplay` | `workspaceDeleteReplayAuthorization` | `json:"keyDeleteReplay,omitempty"` |
| `RefundReplay` | `workspaceDeleteReplayAuthorization` | `json:"refundReplay,omitempty"` |
| `RefundConfirmation` | `map[string]any` | `json:"refundConfirmation,omitempty"` |
| `LastErrorCode` | `string` | `json:"lastErrorCode,omitempty"` |
| `CreatedAt` | `string` | `json:"createdAt"` |

### workspaceDeleteRefundOperation

`services/control-plane/internal/server/workspace_delete_refund.go:46`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `DeleteOperationID` | `string` | `json:"deleteOperationId"` |
| `LaunchOperationID` | `string` | `json:"launchOperationId"` |
| `PolicyVersion` | `string` | `json:"policyVersion"` |
| `Status` | `string` | `json:"status"` |
| `ReasonCode` | `string` | `json:"reasonCode,omitempty"` |
| `OriginalChargeCode` | `string` | `json:"originalChargeCode,omitempty"` |
| `OriginalChargeUSDMicros` | `int64` | `json:"originalChargeUsdMicros,omitempty"` |
| `RefundUSDMicros` | `int64` | `json:"refundUsdMicros,omitempty"` |
| `UsedHours` | `int64` | `json:"usedHours,omitempty"` |
| `RefundHours` | `int64` | `json:"refundHours,omitempty"` |
| `WalletAdjustmentOperationID` | `string` | `json:"walletAdjustmentOperationId,omitempty"` |
| `RefundOrderOperationID` | `string` | `json:"refundOrderOperationId,omitempty"` |
| `RefundReceiptID` | `string` | `json:"refundReceiptId,omitempty"` |
| `DeletedAt` | `string` | `json:"deletedAt,omitempty"` |
| `ResourceFulfilledAt` | `string` | `json:"resourceFulfilledAt,omitempty"` |
| `ReadbackID` | `string` | `json:"readbackId,omitempty"` |
| `ReadbackProvider` | `string` | `json:"readbackProvider,omitempty"` |
| `ReadbackObservedAt` | `string` | `json:"readbackObservedAt,omitempty"` |
| `CreatedAt` | `string` | `json:"createdAt"` |
| `UpdatedAt` | `string` | `json:"updatedAt"` |

### workspaceDeleteRefundOrderFacts

`services/control-plane/internal/server/workspace_delete_refund.go:465`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `PaidThrough` | `string` | `json:"paidThrough"` |
| `RenewedThrough` | `string` | `json:"renewedThrough"` |

### workspaceDeletionStatusDTO

`services/control-plane/internal/server/workspace_delete_worker.go:146`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `OperationID` | `string` | `json:"operationId"` |
| `Status` | `string` | `json:"status"` |
| `Stage` | `string` | `json:"stage,omitempty"` |
| `Phase` | `string` | `json:"phase"` |
| `PageState` | `string` | `json:"pageState,omitempty"` |
| `ReasonCode` | `string` | `json:"reasonCode,omitempty"` |
| `LastReadbackAt` | `string` | `json:"lastReadbackAt,omitempty"` |
| `NextRetryAt` | `string` | `json:"nextRetryAt,omitempty"` |
| `ReceiptID` | `string` | `json:"receiptId,omitempty"` |
| `RefundStatus` | `string` | `json:"refundStatus,omitempty"` |
| `RefundReasonCode` | `string` | `json:"refundReasonCode,omitempty"` |
| `RefundOperationID` | `string` | `json:"refundOperationId,omitempty"` |
| `RefundReceiptID` | `string` | `json:"refundReceiptId,omitempty"` |
| `RefundUSDMicros` | `int64` | `json:"refundUsdMicros,omitempty"` |
| `OriginalChargeUSDMicros` | `int64` | `json:"originalChargeUsdMicros,omitempty"` |
| `RefundPolicyVersion` | `string` | `json:"refundPolicyVersion,omitempty"` |

### workspaceKeyRotationOperation

`services/control-plane/internal/server/workspace_gateway.go:313`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `RequestHash` | `string` | `json:"requestHash"` |
| `Phase` | `string` | `json:"phase"` |
| `OldKeyID` | `int64` | `json:"oldKeyId"` |
| `NewKeyID` | `int64` | `json:"newKeyId,omitempty"` |
| `ReplacementName` | `string` | `json:"replacementName"` |
| `RetiredName` | `string` | `json:"retiredName"` |
| `ReplacementCreateStarted` | `bool` | `json:"replacementCreateStarted,omitempty"` |
| `ReplacementGroupID` | `int64` | `json:"replacementGroupId,omitempty"` |
| `BudgetSnapshotCaptured` | `bool` | `json:"budgetSnapshotCaptured,omitempty"` |
| `OldQuotaUSDMicros` | `int64` | `json:"oldQuotaUsdMicros,omitempty"` |
| `OldQuotaUsedUSDMicros` | `int64` | `json:"oldQuotaUsedUsdMicros,omitempty"` |
| `ReplacementQuotaUSDMicros` | `int64` | `json:"replacementQuotaUsdMicros,omitempty"` |
| `RateLimit5hUSDMicros` | `int64` | `json:"rateLimit5hUsdMicros,omitempty"` |
| `RateLimit1dUSDMicros` | `int64` | `json:"rateLimit1dUsdMicros,omitempty"` |
| `RateLimit7dUSDMicros` | `int64` | `json:"rateLimit7dUsdMicros,omitempty"` |
| `Usage5hUSDMicros` | `int64` | `json:"usage5hUsdMicros,omitempty"` |
| `Usage1dUSDMicros` | `int64` | `json:"usage1dUsdMicros,omitempty"` |
| `Usage7dUSDMicros` | `int64` | `json:"usage7dUsdMicros,omitempty"` |
| `BudgetCapturedAt` | `string` | `json:"budgetCapturedAt,omitempty"` |
| `SecretRef` | `string` | `json:"secretRef,omitempty"` |
| `SecretVersion` | `string` | `json:"secretVersion,omitempty"` |
| `ApplicationDeploymentID` | `string` | `json:"applicationDeploymentId,omitempty"` |
| `Fingerprint` | `string` | `json:"fingerprint,omitempty"` |
| `RuntimeID` | `string` | `json:"runtimeId,omitempty"` |
| `ReceiptID` | `string` | `json:"receiptId,omitempty"` |
| `CompletedAt` | `string` | `json:"completedAt,omitempty"` |
| `AuditEvent` | `map[string]any` | `json:"auditEvent"` |

### workspaceGatewayBudgetRequest

`services/control-plane/internal/server/workspace_gateway_budget.go:24`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `QuotaUSDMicros` | `*int64` | `json:"quotaUsdMicros,omitempty"` |
| `RateLimit5hUSDMicros` | `*int64` | `json:"rateLimit5hUsdMicros,omitempty"` |
| `RateLimit1dUSDMicros` | `*int64` | `json:"rateLimit1dUsdMicros,omitempty"` |
| `RateLimit7dUSDMicros` | `*int64` | `json:"rateLimit7dUsdMicros,omitempty"` |
| `Enabled` | `*bool` | `json:"enabled,omitempty"` |
| `ResetQuota` | `*bool` | `json:"resetQuota,omitempty"` |
| `ResetRateLimitUsage` | `*bool` | `json:"resetRateLimitUsage,omitempty"` |

### workspaceGatewayBudgetOperation

`services/control-plane/internal/server/workspace_gateway_budget.go:34`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `RequestHash` | `string` | `json:"requestHash"` |
| `KeyID` | `int64` | `json:"keyId"` |
| `Request` | `workspaceGatewayBudgetRequest` | `json:"request"` |
| `AuditEvent` | `map[string]any` | `json:"auditEvent"` |

### workspaceImageReleasePolicy

`services/control-plane/internal/server/workspace_image_release_policy.go:28`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Revision` | `int` | `json:"revision"` |
| `ActiveVersion` | `string` | `json:"activeVersion"` |
| `ActiveImage` | `string` | `json:"activeImage"` |
| `UpdatedAt` | `string` | `json:"updatedAt,omitempty"` |
| `UpdatedBy` | `string` | `json:"updatedBy,omitempty"` |
| `Reason` | `string` | `json:"reason,omitempty"` |

### workspaceImageReleaseDTO

`services/control-plane/internal/server/workspace_image_release_policy.go:38`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Version` | `string` | `json:"version"` |
| `Image` | `string` | `json:"image"` |
| `Digest` | `string` | `json:"digest"` |

### workspaceImageReleasePolicyDTO

`services/control-plane/internal/server/workspace_image_release_policy.go:44`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Revision` | `int` | `json:"revision"` |
| `Active` | `workspaceImageReleaseDTO` | `json:"active"` |
| `InstalledDefault` | `workspaceImageReleaseDTO` | `json:"installedDefault"` |
| `Releases` | `[]workspaceImageReleaseDTO` | `json:"releases"` |
| `Source` | `string` | `json:"source"` |
| `UpdatedAt` | `string` | `json:"updatedAt,omitempty"` |
| `UpdatedBy` | `string` | `json:"updatedBy,omitempty"` |
| `Reason` | `string` | `json:"reason,omitempty"` |

### productionAcceptanceBResumeExistingPrepareRequest

`services/control-plane/internal/server/workspace_launch_admission.go:133`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ApprovalID` | `string` | `json:"approvalId"` |
| `AuthorizationID` | `string` | `json:"authorizationId"` |
| `ReasonSHA256` | `string` | `json:"reasonSha256"` |
| `Release` | `struct {<br>	CanonicalCloudSHA		string	`json:"canonicalCloudSha"`<br>	CanonicalCloudTree		string	`json:"canonicalCloudTree"`<br>	DeployedCloudImageDigest	string	`json:"deployedCloudImageDigest"`<br>}` | `json:"release"` |

### workspaceLaunchAcceptanceBResumeExistingBinding

`services/control-plane/internal/server/workspace_launch_admission.go:144`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `ApprovalID` | `string` | `json:"approvalId"` |
| `ApprovalSHA256` | `string` | `json:"approvalSha256"` |
| `CanonicalCloudSHA` | `string` | `json:"canonicalCloudSha"` |
| `CanonicalCloudTree` | `string` | `json:"canonicalCloudTree"` |
| `DeployedCloudImageDigest` | `string` | `json:"deployedCloudImageDigest"` |
| `AuthoritativeState` | `string` | `json:"authoritativeState"` |
| `IdentityDigests` | `workspaceLaunchAcceptanceBIdentityDigestSet` | `json:"identityDigests"` |

### productionAcceptanceBApproval

`services/control-plane/internal/server/workspace_launch_admission.go:56`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `OperationMode` | `string` | `json:"operationMode"` |
| `ApprovalID` | `string` | `json:"approvalId"` |
| `ExpiresAt` | `string` | `json:"expiresAt"` |
| `Confirmation` | `string` | `json:"confirmation"` |
| `Release` | `struct {<br>	MergedMainSHA		string	`json:"mergedMainSha"`<br>	CloudImageDigest	string	`json:"cloudImageDigest"`<br>	WorkspaceImageDigest	string	`json:"workspaceImageDigest"`<br>}` | `json:"release"` |
| `Customer` | `struct {<br>	Email		string	`json:"email"`<br>	AccountID	string	`json:"accountId"`<br>}` | `json:"customer"` |
| `Launch` | `struct {<br>	IdempotencyKey	string	`json:"idempotencyKey"`<br>	OperationID	string	`json:"operationId"`<br>	WorkspaceID	string	`json:"workspaceId"`<br>	Name		string	`json:"name"`<br>	PackageID	string	`json:"packageId"`<br>	SizeGB		int	`json:"sizeGb"`<br>	AutoRenew	bool	`json:"autoRenew"`<br>}` | `json:"launch"` |
| `Expected` | `struct {<br>	NodePoolID		string	`json:"nodePoolId"`<br>	ResolvedInstanceType	string	`json:"resolvedInstanceType"`<br>}` | `json:"expected"` |
| `AllowedWrites` | `[]string` | `json:"allowedWrites"` |
| `ForbiddenWrites` | `[]string` | `json:"forbiddenWrites"` |

### workspaceLaunchAcceptanceBIdentityDigestSet

`services/control-plane/internal/server/workspace_launch_admission.go:88`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AccountIdentitySHA256` | `string` | `json:"accountIdentitySha256"` |
| `OperationIdentitySHA256` | `string` | `json:"operationIdentitySha256"` |
| `WorkspaceIdentitySHA256` | `string` | `json:"workspaceIdentitySha256"` |
| `KeyIdentitySHA256` | `string` | `json:"keyIdentitySha256"` |
| `DebitIdentitySHA256` | `string` | `json:"debitIdentitySha256"` |
| `QuoteIdentitySHA256` | `string` | `json:"quoteIdentitySha256"` |
| `ProviderIdentitySHA256` | `string` | `json:"providerIdentitySha256"` |

### productionAcceptanceBResumeExistingApproval

`services/control-plane/internal/server/workspace_launch_admission.go:98`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `OperationMode` | `string` | `json:"operationMode"` |
| `ApprovalID` | `string` | `json:"approvalId"` |
| `ExpiresAt` | `string` | `json:"expiresAt"` |
| `Release` | `struct {<br>	CanonicalCloudSHA		string	`json:"canonicalCloudSha"`<br>	CanonicalCloudTree		string	`json:"canonicalCloudTree"`<br>	DeployedCloudImageDigest	string	`json:"deployedCloudImageDigest"`<br>}` | `json:"release"` |
| `Authorization` | `struct {<br>	AuthorizationID		string	`json:"authorizationId"`<br>	OperationID		string	`json:"operationId"`<br>	LaunchVersion		int	`json:"launchVersion"`<br>	AuthorizedStage		string	`json:"authorizedStage"`<br>	ReasonSHA256		string	`json:"reasonSha256"`<br>	MutationBudget		int	`json:"mutationBudget"`<br>	IdempotentReplayBudget	int	`json:"idempotentReplayBudget"`<br>	AuthoritativeReadBudget	int	`json:"authoritativeReadBudget"`<br>}` | `json:"authorization"` |
| `Reconciliation` | `struct {<br>	OperationStatus		string	`json:"operationStatus"`<br>	AuthoritativeStageState	string	`json:"authoritativeStageState"`<br>	Attempt			struct {<br>		Attempted		int	`json:"attempted"`<br>		Confirmed		int	`json:"confirmed"`<br>		Unknown			int	`json:"unknown"`<br>		Max			int	`json:"max"`<br>		Status			string	`json:"status"`<br>		IdempotencyKeySHA256	string	`json:"idempotencyKeySha256"`<br>	}	`json:"attempt"`<br>}` | `json:"reconciliation"` |
| `IdentityDigests` | `workspaceLaunchAcceptanceBIdentityDigestSet` | `json:"identityDigests"` |

### workspaceLaunchBillingHistory

`services/control-plane/internal/server/workspace_launch_billing_history.go:11`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `(embedded)` | `walletRefundChargeFacts` | `` |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `OwnerUserID` | `string` | `json:"ownerUserId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `PackageID` | `string` | `json:"packageId"` |
| `StorageGB` | `int64` | `json:"sizeGb"` |
| `PriceVersion` | `string` | `json:"priceVersion"` |
| `PeriodStart` | `string` | `json:"periodStart"` |
| `PaidThrough` | `string` | `json:"paidThrough"` |
| `ComputeID` | `string` | `json:"computeAllocationId"` |
| `StorageID` | `string` | `json:"storageId"` |
| `AttachmentID` | `string` | `json:"attachmentId"` |
| `WorkspaceAPIKeyID` | `int64` | `json:"workspaceApiKeyId"` |
| `WorkspaceKeyFingerprint` | `string` | `json:"workspaceKeyFingerprint"` |
| `RuntimeID` | `string` | `json:"runtimeId"` |
| `RuntimeServiceName` | `string` | `json:"runtimeServiceName"` |
| `ChargeAttempted` | `bool` | `json:"chargeAttempted"` |
| `ReceiptID` | `string` | `json:"receiptId"` |
| `RefundCode` | `string` | `json:"sub2apiRefundCode"` |
| `RefundAttempted` | `bool` | `json:"refundAttempted"` |
| `RefundConfirmation` | `map[string]any` | `json:"refundConfirmation"` |
| `RefundReason` | `string` | `json:"refundReason"` |
| `RefundReceiptID` | `string` | `json:"refundReceiptId"` |

### workspaceLaunchCloseout

`services/control-plane/internal/server/workspace_launch_closeout.go:19`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AuthorizationID` | `string` | `json:"authorizationId"` |
| `LaunchVersion` | `int` | `json:"launchVersion"` |
| `AuthorizedBy` | `string` | `json:"authorizedBy"` |
| `AuthorizedAt` | `string` | `json:"authorizedAt"` |
| `Reason` | `string` | `json:"reason"` |
| `Phase` | `string` | `json:"phase"` |
| `FrozenAt` | `string` | `json:"frozenAt,omitempty"` |
| `KeyRevokedAt` | `string` | `json:"keyRevokedAt,omitempty"` |
| `ResourcesAbsentAt` | `string` | `json:"resourcesAbsentAt,omitempty"` |
| `DebitState` | `string` | `json:"debitState,omitempty"` |
| `RefundOperationID` | `string` | `json:"refundOperationId,omitempty"` |
| `RefundedUSDMicros` | `int64` | `json:"refundedUsdMicros"` |
| `ReceiptRequestedAt` | `string` | `json:"receiptRequestedAt,omitempty"` |
| `ReceiptID` | `string` | `json:"receiptId,omitempty"` |
| `CompletedAt` | `string` | `json:"completedAt,omitempty"` |
| `ErrorCode` | `string` | `json:"errorCode,omitempty"` |

### workspaceLaunchCloseoutDTO

`services/control-plane/internal/server/workspace_launch_closeout.go:38`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `PendingConfirmation` | `bool` | `json:"pendingConfirmation,omitempty"` |
| `Status` | `string` | `json:"status"` |
| `RefundedUSDMicros` | `int64` | `json:"refundedUsdMicros"` |
| `ReceiptID` | `string` | `json:"receiptId,omitempty"` |

### workspaceLaunchDisposableOwnerObservation

`services/control-plane/internal/server/workspace_launch_disposable_reset.go:47`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `State` | `workspaceLaunchDisposableOwnerState` | `json:"state"` |
| `Count` | `int` | `json:"count"` |
| `AmountUSDMicros` | `int64` | `json:"amountUsdMicros"` |
| `IdentityDigests` | `[]string` | `json:"identityDigests"` |

### workspaceLaunchDisposableResetPreview

`services/control-plane/internal/server/workspace_launch_disposable_reset.go:74`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Eligible` | `bool` | `json:"eligible"` |
| `OperationIdentityDigest` | `string` | `json:"operationIdentityDigest"` |
| `AccountIdentityDigest` | `string` | `json:"accountIdentityDigest"` |
| `WorkspaceIdentityDigest` | `string` | `json:"workspaceIdentityDigest"` |
| `OperationVersion` | `int` | `json:"operationVersion"` |
| `Stage` | `string` | `json:"stage"` |
| `Status` | `string` | `json:"status"` |
| `OwnerStates` | `map[string]workspaceLaunchDisposableOwnerState` | `json:"ownerStates"` |
| `OwnerObservations` | `map[string]workspaceLaunchDisposableOwnerObservation` | `json:"ownerObservations"` |
| `Blockers` | `[]string` | `json:"blockers"` |
| `PlanSteps` | `[]string` | `json:"planSteps"` |
| `ResetPlanDigest` | `string` | `json:"resetPlanDigest"` |
| `MutationBudget` | `int` | `json:"mutationBudget"` |

### workspaceLaunchStageAttempt

`services/control-plane/internal/server/workspace_launch_reconciler.go:184`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Attempted` | `int` | `json:"attempted"` |
| `Confirmed` | `int` | `json:"confirmed"` |
| `Unknown` | `int` | `json:"unknown"` |
| `Max` | `int` | `json:"max"` |
| `Status` | `string` | `json:"status,omitempty"` |
| `IdempotencyKey` | `string` | `json:"idempotencyKey,omitempty"` |
| `PendingReadbacks` | `int` | `json:"pendingReadbacks,omitempty"` |
| `MaxPendingReadbacks` | `int` | `json:"maxPendingReadbacks,omitempty"` |
| `PendingDeadlineAt` | `string` | `json:"pendingDeadlineAt,omitempty"` |
| `DispatchLeaseExpiresAt` | `string` | `json:"dispatchLeaseExpiresAt,omitempty"` |

### workspaceLaunchStageObservation

`services/control-plane/internal/server/workspace_launch_reconciler.go:197`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `State` | `contracts.StageState` | `json:"state"` |
| `Facts` | `map[string]any` | `json:"facts,omitempty"` |
| `Diagnostic` | `*clients.WorkspaceLaunchStageDiagnostic` | `json:"diagnostic,omitempty"` |

### workspaceLaunchResumeAuthorization

`services/control-plane/internal/server/workspace_launch_reconciler.go:209`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AuthorizationID` | `string` | `json:"authorizationId"` |
| `LaunchVersion` | `int` | `json:"launchVersion"` |
| `AuthorizedStage` | `contracts.Stage` | `json:"authorizedStage"` |
| `AuthorizedBy` | `string` | `json:"authorizedBy"` |
| `AuthorizedAt` | `string` | `json:"authorizedAt"` |
| `Reason` | `string` | `json:"reason"` |
| `MutationBudget` | `int` | `json:"mutationBudget"` |
| `IdempotentReplayBudget` | `int` | `json:"idempotentReplayBudget,omitempty"` |
| `AuthoritativeReadBudget` | `int` | `json:"authoritativeReadBudget,omitempty"` |
| `ReadbacksAtAuthorization` | `int` | `json:"readbacksAtAuthorization,omitempty"` |
| `ReplacementWorkspaceImageDigest` | `string` | `json:"replacementWorkspaceImageDigest,omitempty"` |
| `AcceptanceBResumeExisting` | `*workspaceLaunchAcceptanceBResumeExistingBinding` | `json:"acceptanceBResumeExisting,omitempty"` |

### workspaceLaunchResultCheck

`services/control-plane/internal/server/workspace_launch_reconciler.go:224`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Authorization` | `workspaceLaunchResumeAuthorization` | `json:"authorization"` |
| `ConsumedAt` | `string` | `json:"consumedAt"` |
| `State` | `contracts.StageState` | `json:"state"` |
| `ErrorCode` | `string` | `json:"errorCode"` |

### workspaceLaunchConsumedResumeAuthorization

`services/control-plane/internal/server/workspace_launch_reconciler.go:231`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Authorization` | `workspaceLaunchResumeAuthorization` | `json:"authorization"` |
| `ConsumedAt` | `string` | `json:"consumedAt"` |

### workspaceLaunchIdempotentReplayClaim

`services/control-plane/internal/server/workspace_launch_reconciler.go:236`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AuthorizationID` | `string` | `json:"authorizationId"` |
| `Stage` | `contracts.Stage` | `json:"stage"` |
| `IdempotencyKey` | `string` | `json:"idempotencyKey"` |
| `Status` | `string` | `json:"status"` |
| `LeaseExpiresAt` | `string` | `json:"leaseExpiresAt,omitempty"` |
| `CompletedAt` | `string` | `json:"completedAt,omitempty"` |

### workspaceLaunchFreshContinuationAuthorization

`services/control-plane/internal/server/workspace_launch_reconciler.go:245`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `AuthorizationID` | `string` | `json:"authorizationId"` |
| `AuthorizationClass` | `string` | `json:"authorizationClass"` |
| `AccountID` | `string` | `json:"accountId"` |
| `OperationID` | `string` | `json:"operationId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `Stage` | `contracts.Stage` | `json:"stage"` |
| `IdempotencyKey` | `string` | `json:"idempotencyKey"` |
| `Attempt` | `int` | `json:"attempt"` |
| `OperationVersion` | `int` | `json:"operationVersion"` |
| `MutationBudget` | `int` | `json:"mutationBudget"` |
| `IdempotentReplayBudget` | `int` | `json:"idempotentReplayBudget"` |
| `AuthoritativeReadBudget` | `int` | `json:"authoritativeReadBudget"` |
| `ReadbacksAtAuthorization` | `int` | `json:"readbacksAtAuthorization"` |
| `Status` | `string` | `json:"status"` |
| `ConsumedAt` | `string` | `json:"consumedAt,omitempty"` |

### workspaceLaunchContinuationReadClaim

`services/control-plane/internal/server/workspace_launch_reconciler.go:264`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `AuthorizationID` | `string` | `json:"authorizationId"` |
| `Stage` | `contracts.Stage` | `json:"stage"` |
| `IdempotencyKey` | `string` | `json:"idempotencyKey"` |
| `Readback` | `int` | `json:"readback"` |
| `Status` | `string` | `json:"status"` |
| `LeaseExpiresAt` | `string` | `json:"leaseExpiresAt,omitempty"` |
| `CompletedAt` | `string` | `json:"completedAt,omitempty"` |

### workspaceLaunchRuntimeRepair

`services/control-plane/internal/server/workspace_launch_reconciler.go:275`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AuthorizationID` | `string` | `json:"authorizationId"` |
| `LaunchVersion` | `int` | `json:"launchVersion"` |
| `AuthorizedBy` | `string` | `json:"authorizedBy"` |
| `AuthorizedAt` | `string` | `json:"authorizedAt"` |
| `Reason` | `string` | `json:"reason"` |
| `ImageDigest` | `string` | `json:"imageDigest"` |

### workspaceLaunchDisposableResetEvidence

`services/control-plane/internal/server/workspace_launch_reconciler.go:284`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `LaunchVersion` | `int` | `json:"launchVersion"` |
| `ResetPlanDigest` | `string` | `json:"resetPlanDigest"` |
| `AuthorityDigest` | `string` | `json:"authorityDigest"` |
| `LedgerReceiptDigest` | `string` | `json:"ledgerReceiptDigest"` |
| `CompletedAt` | `string` | `json:"completedAt"` |
| `MutationScopeMatchedPlan` | `bool` | `json:"mutationScopeMatchedPlan"` |

### workspaceLaunchReconcileOperation

`services/control-plane/internal/server/workspace_launch_reconciler.go:294`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Closeout` | `*workspaceLaunchCloseout` | `json:"closeout,omitempty"` |
| `ID` | `string` | `json:"-"` |
| `Status` | `contracts.LaunchStatus` | `json:"-"` |
| `CreatedAt` | `string` | `json:"-"` |
| `PersistedResult` | `string` | `json:"-"` |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Version` | `int` | `json:"version"` |
| `Stage` | `contracts.Stage` | `json:"stage"` |
| `Attempts` | `map[contracts.Stage]workspaceLaunchStageAttempt` | `json:"attempts"` |
| `Observations` | `map[contracts.Stage]workspaceLaunchStageObservation` | `json:"observations,omitempty"` |
| `ResultChecks` | `[]workspaceLaunchResultCheck` | `json:"resultChecks,omitempty"` |
| `ConsumedResumeAuthorizations` | `[]workspaceLaunchConsumedResumeAuthorization` | `json:"consumedResumeAuthorizations,omitempty"` |
| `ResumeAuthorization` | `*workspaceLaunchResumeAuthorization` | `json:"resumeAuthorization,omitempty"` |
| `ResumeAuthorizationConsumedAt` | `string` | `json:"resumeAuthorizationConsumedAt,omitempty"` |
| `IdempotentReplayClaims` | `map[contracts.Stage]workspaceLaunchIdempotentReplayClaim` | `json:"idempotentReplayClaims,omitempty"` |
| `FreshContinuationAuthorizations` | `map[contracts.Stage]workspaceLaunchFreshContinuationAuthorization` | `json:"freshContinuationAuthorizations,omitempty"` |
| `ContinuationReadClaims` | `map[string]workspaceLaunchContinuationReadClaim` | `json:"continuationReadClaims,omitempty"` |
| `RuntimeRepair` | `*workspaceLaunchRuntimeRepair` | `json:"runtimeRepair,omitempty"` |
| `DisposableReset` | `*workspaceLaunchDisposableResetEvidence` | `json:"disposableReset,omitempty"` |
| `raw` | `map[string]json.RawMessage` | `` |

### workspaceLaunchStageDiagnosticAttempt

`services/control-plane/internal/server/workspace_launch_stage_diagnostic.go:20`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Attempted` | `int` | `json:"attempted"` |
| `Confirmed` | `int` | `json:"confirmed"` |
| `Unknown` | `int` | `json:"unknown"` |
| `Max` | `int` | `json:"max"` |
| `Status` | `string` | `json:"status,omitempty"` |

### workspaceLaunchStageDiagnostic

`services/control-plane/internal/server/workspace_launch_stage_diagnostic.go:28`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `OperationIdentityDigest` | `string` | `json:"operationIdentityDigest"` |
| `OperationVersion` | `int` | `json:"operationVersion"` |
| `OperationStatus` | `string` | `json:"operationStatus"` |
| `Stage` | `string` | `json:"stage"` |
| `State` | `string` | `json:"state"` |
| `ErrorCode` | `string` | `json:"errorCode"` |
| `Owner` | `string` | `json:"owner"` |
| `BlockReason` | `string` | `json:"blockReason"` |
| `Retryable` | `bool` | `json:"retryable"` |
| `ObservedAt` | `string` | `json:"observedAt,omitempty"` |
| `Checks` | `[]clients.WorkspaceLaunchStageCheck` | `json:"checks,omitempty"` |
| `Attempt` | `workspaceLaunchStageDiagnosticAttempt` | `json:"attempt"` |
| `AuthoritativeRead` | `bool` | `json:"authoritativeRead"` |
| `MutationBudget` | `int` | `json:"mutationBudget"` |
| `AutoRecoveryEligible` | `bool` | `json:"autoRecoveryEligible"` |
| `AutoRecoveryBlockReason` | `string` | `json:"autoRecoveryBlockReason"` |

### workspaceRegistryCatalogResponse

`services/control-plane/internal/server/workspace_registry_catalog.go:87`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Host` | `string` | `json:"host"` |
| `Namespaces` | `[]string` | `json:"namespaces"` |
| `Items` | `[]contracts.WorkspaceRegistryRepository` | `json:"items,omitempty"` |

### workspaceRenewalRecovery

`services/control-plane/internal/server/workspace_renewal.go:1479`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `State` | `string` | `json:"state"` |
| `Reason` | `string` | `json:"reason"` |

### workspaceRenewalOperation

`services/control-plane/internal/server/workspace_renewal.go:208`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ExpiryRuntimePower` | `*contracts.WorkspaceRuntimePowerResult` | `json:"expiryRuntimePower,omitempty"` |
| `ResumeRuntimePower` | `*contracts.WorkspaceRuntimePowerResult` | `json:"resumeRuntimePower,omitempty"` |
| `ExpiryApplicationPower` | `*workspaceApplicationLifecycleOperation` | `json:"expiryApplicationPower,omitempty"` |
| `ResumeApplicationPower` | `*workspaceApplicationLifecycleOperation` | `json:"resumeApplicationPower,omitempty"` |
| `ID` | `string` | `json:"-"` |
| `Status` | `string` | `json:"-"` |
| `CreatedAt` | `string` | `json:"-"` |
| `PersistedResult` | `string` | `json:"-"` |
| `RequestHash` | `string` | `json:"requestHash"` |
| `Phase` | `string` | `json:"phase"` |
| `AccountID` | `string` | `json:"accountId"` |
| `OwnerUserID` | `string` | `json:"ownerUserId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `PackageID` | `string` | `json:"packageId"` |
| `StorageGB` | `int64` | `json:"storageGb"` |
| `ComputeID` | `string` | `json:"computeAllocationId"` |
| `StorageID` | `string` | `json:"storageId"` |
| `PriceVersion` | `string` | `json:"priceVersion"` |
| `ComputeUSDMicros` | `int64` | `json:"computeUsdMicros"` |
| `StorageUSDMicros` | `int64` | `json:"storageUsdMicros"` |
| `TotalUSDMicros` | `int64` | `json:"totalUsdMicros"` |
| `PeriodStart` | `string` | `json:"periodStart"` |
| `PaidThrough` | `string` | `json:"paidThrough"` |
| `RenewedThrough` | `string` | `json:"renewedThrough"` |
| `RedeemCode` | `string` | `json:"sub2apiRedeemCode"` |
| `RefundCode` | `string` | `json:"sub2apiRefundCode"` |
| `ComputePreflightConfirmed` | `bool` | `json:"computePreflightConfirmed,omitempty"` |
| `StoragePreflightConfirmed` | `bool` | `json:"storagePreflightConfirmed,omitempty"` |
| `ChargeAttempted` | `bool` | `json:"chargeAttempted,omitempty"` |
| `ChargeConfirmation` | `map[string]any` | `json:"chargeConfirmation,omitempty"` |
| `PreChargeBalanceUSDMicros` | `int64` | `json:"preChargeBalanceUsdMicros,omitempty"` |
| `PostChargeBalanceUSDMicros` | `int64` | `json:"postChargeBalanceUsdMicros,omitempty"` |
| `PostChargeBalanceKnown` | `bool` | `json:"postChargeBalanceKnown,omitempty"` |
| `RefundAttempted` | `bool` | `json:"refundAttempted,omitempty"` |
| `RefundConfirmation` | `map[string]any` | `json:"refundConfirmation,omitempty"` |
| `RefundReason` | `string` | `json:"refundReason,omitempty"` |
| `RefundReceiptID` | `string` | `json:"refundReceiptId,omitempty"` |
| `ComputeRenewal` | `map[string]any` | `json:"computeRenewal,omitempty"` |
| `StorageRenewal` | `map[string]any` | `json:"storageRenewal,omitempty"` |
| `ComputeReadback` | `map[string]any` | `json:"computeReadback,omitempty"` |
| `StorageReadback` | `map[string]any` | `json:"storageReadback,omitempty"` |
| `EntitlementCommitted` | `bool` | `json:"entitlementCommitted,omitempty"` |
| `ReceiptID` | `string` | `json:"receiptId,omitempty"` |
| `ErrorCode` | `string` | `json:"errorCode,omitempty"` |
| `PriorStatus` | `string` | `json:"priorStatus,omitempty"` |
| `PriorErrorCode` | `string` | `json:"priorErrorCode,omitempty"` |
| `ExpiryStatus` | `string` | `json:"expiryStatus,omitempty"` |
| `ExpiryPhase` | `string` | `json:"expiryPhase,omitempty"` |
| `ExpiryErrorCode` | `string` | `json:"expiryErrorCode,omitempty"` |
| `ExpiryReceiptID` | `string` | `json:"expiryReceiptId,omitempty"` |
| `ExpiryPeriodStart` | `string` | `json:"expiryPeriodStart,omitempty"` |
| `ExpiryPaidThrough` | `string` | `json:"expiryPaidThrough,omitempty"` |
| `LeaseToken` | `string` | `json:"leaseToken,omitempty"` |
| `LeaseExpiresAt` | `string` | `json:"leaseExpiresAt,omitempty"` |
| `ReviewResolutionKey` | `string` | `json:"reviewResolutionKey,omitempty"` |
| `ReviewResolutionFingerprint` | `string` | `json:"reviewResolutionFingerprint,omitempty"` |
| `ReviewResolutionDecision` | `string` | `json:"reviewResolutionDecision,omitempty"` |
| `ReviewResolutionEvidenceRef` | `string` | `json:"reviewResolutionEvidenceRef,omitempty"` |
| `ReviewResolutionReviewer` | `string` | `json:"reviewResolutionReviewer,omitempty"` |
| `ReviewResolutionPhase` | `string` | `json:"reviewResolutionPhase,omitempty"` |
| `ReviewResolutionResolvedAt` | `string` | `json:"reviewResolutionResolvedAt,omitempty"` |
| `ReviewResolutionResult` | `map[string]any` | `json:"reviewResolutionResult,omitempty"` |

### workspaceAutoRenewCommandResult

`services/control-plane/internal/server/workspace_renewal.go:42`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `RequestHash` | `string` | `json:"requestHash"` |
| `Response` | `map[string]any` | `json:"response"` |

### workspaceRuntimeGatewayNetworkRecoveryOperation

`services/control-plane/internal/server/workspace_runtime_gateway_network_recovery.go:23`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `RequestHash` | `string` | `json:"requestHash"` |
| `Reason` | `string` | `json:"reason"` |
| `Input` | `clients.WorkspaceRuntimeGatewayNetworkRecoveryInput` | `json:"input"` |
| `Result` | `clients.WorkspaceRuntimeGatewayNetworkRecoveryResult` | `json:"result"` |
| `AuditEvent` | `map[string]any` | `json:"auditEvent"` |
| `ErrorCode` | `string` | `json:"errorCode,omitempty"` |

### workspaceRuntimeImageReplacementRequest

`services/control-plane/internal/server/workspace_runtime_image_replacement.go:25`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ReplacementImageDigest` | `string` | `json:"replacementImageDigest"` |
| `Reason` | `string` | `json:"reason"` |

### workspaceRuntimeImageReplacementOperation

`services/control-plane/internal/server/workspace_runtime_image_replacement.go:30`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `RequestHash` | `string` | `json:"requestHash"` |
| `Reason` | `string` | `json:"reason"` |
| `Input` | `clients.WorkspaceRuntimeImageReplacementInput` | `json:"input"` |
| `Runtime` | `clients.WorkspaceRuntime` | `json:"runtime"` |
| `AuditEvent` | `map[string]any` | `json:"auditEvent"` |
| `ErrorCode` | `string` | `json:"errorCode,omitempty"` |

## fabric


### request

`services/fabric/cmd/opl-node-image-retire/main.go:34`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Image` | `string` | `json:"image"` |
| `ProtectedImages` | `[]string` | `json:"protectedImages"` |
| `Apply` | `bool` | `json:"apply"` |

### result

`services/fabric/cmd/opl-node-image-retire/main.go:39`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Status` | `string` | `json:"status"` |
| `ErrorCode` | `string` | `json:"errorCode"` |
| `ImageDigest` | `string` | `json:"imageDigest"` |
| `RemovalAttempted` | `bool` | `json:"removalAttempted"` |
| `ImageAbsent` | `bool` | `json:"imageAbsent"` |
| `ImageFSUsedBytesBefore` | `*uint64` | `json:"imageFsUsedBytesBefore"` |
| `ImageFSUsedBytesAfter` | `*uint64` | `json:"imageFsUsedBytesAfter"` |

### tencentSKUProviderProfile

`services/fabric/cmd/opl-tencent-provisioner/main.go:108`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Packages` | `[]tencentSKUProfile` | `json:"packages"` |

### Request

`services/fabric/cmd/opl-tencent-provisioner/main.go:159`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Action` | `string` | `json:"action"` |
| `DryRun` | `bool` | `json:"dryRun,omitempty"` |
| `AccountId` | `string` | `json:"accountId,omitempty"` |
| `UserId` | `string` | `json:"userId,omitempty"` |
| `PackageId` | `string` | `json:"packageId,omitempty"` |
| `Region` | `string` | `json:"region,omitempty"` |
| `Zone` | `string` | `json:"zone,omitempty"` |
| `StorageVolumeId` | `string` | `json:"storageVolumeId,omitempty"` |
| `RequiredCapacity` | `int64` | `json:"requiredCapacity,omitempty"` |
| `Tags` | `map[string]string` | `json:"tags,omitempty"` |
| `ComputeTags` | `map[string]string` | `json:"computeTags,omitempty"` |
| `Pool` | `ComputePoolInput` | `json:"pool,omitempty"` |
| `Allocation` | `ComputeAllocationInput` | `json:"allocation,omitempty"` |
| `Storage` | `StorageInput` | `json:"storage,omitempty"` |

### ComputePoolInput

`services/fabric/cmd/opl-tencent-provisioner/main.go:176`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Id` | `string` | `json:"id,omitempty"` |
| `ClusterId` | `string` | `json:"clusterId,omitempty"` |
| `PackageId` | `string` | `json:"packageId,omitempty"` |
| `InstanceType` | `string` | `json:"instanceType,omitempty"` |
| `CPU` | `uint64` | `json:"cpu,omitempty"` |
| `MemoryGB` | `uint64` | `json:"memoryGb,omitempty"` |
| `NodePoolId` | `string` | `json:"nodePoolId,omitempty"` |
| `DesiredNodeLabels` | `map[string]string` | `json:"desiredNodeLabels,omitempty"` |
| `DesiredReplicas` | `int64` | `json:"desiredReplicas,omitempty"` |
| `MaxReplicas` | `int64` | `json:"maxReplicas,omitempty"` |
| `BaselineReplicas` | `int64` | `json:"baselineReplicas,omitempty"` |
| `TargetReplicas` | `int64` | `json:"targetReplicas,omitempty"` |
| `BeforeMachineNames` | `[]string` | `json:"beforeMachineNames,omitempty"` |

### ComputeAllocationInput

`services/fabric/cmd/opl-tencent-provisioner/main.go:192`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Id` | `string` | `json:"id,omitempty"` |
| `InstanceId` | `string` | `json:"instanceId,omitempty"` |
| `MachineType` | `string` | `json:"machineType,omitempty"` |
| `MachineName` | `string` | `json:"machineName,omitempty"` |
| `NodeName` | `string` | `json:"nodeName,omitempty"` |
| `PrivateIp` | `string` | `json:"privateIp,omitempty"` |
| `PublicIp` | `string` | `json:"publicIp,omitempty"` |
| `Deadline` | `string` | `json:"deadline,omitempty"` |

### StorageInput

`services/fabric/cmd/opl-tencent-provisioner/main.go:203`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Id` | `string` | `json:"id,omitempty"` |
| `SizeGB` | `uint64` | `json:"sizeGb,omitempty"` |
| `Zone` | `string` | `json:"zone,omitempty"` |
| `DiskType` | `string` | `json:"diskType,omitempty"` |
| `Deadline` | `string` | `json:"deadline,omitempty"` |
| `ExpectedState` | `string` | `json:"expectedState,omitempty"` |
| `ExpectedProviderResourceId` | `string` | `json:"expectedProviderResourceId,omitempty"` |
| `AllowExistingExactReplay` | `bool` | `json:"allowExistingExactReplay,omitempty"` |

### Response

`services/fabric/cmd/opl-tencent-provisioner/main.go:214`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Observation` | `*contracts.ResourceObservation` | `json:"observation,omitempty"` |
| `Ok` | `bool` | `json:"ok"` |
| `OperationId` | `string` | `json:"operationId,omitempty"` |
| `PoolId` | `string` | `json:"poolId,omitempty"` |
| `NodePoolId` | `string` | `json:"nodePoolId,omitempty"` |
| `InstanceId` | `string` | `json:"instanceId,omitempty"` |
| `NodeName` | `string` | `json:"nodeName,omitempty"` |
| `PrivateIp` | `string` | `json:"privateIp,omitempty"` |
| `MachinePresent` | `*bool` | `json:"machinePresent,omitempty"` |
| `StoragePresent` | `*bool` | `json:"storagePresent,omitempty"` |
| `CVMStatus` | `string` | `json:"cvmStatus,omitempty"` |
| `TKEStatus` | `string` | `json:"tkeStatus,omitempty"` |
| `CBSStatus` | `string` | `json:"cbsStatus,omitempty"` |
| `StorageVolumeId` | `string` | `json:"storageVolumeId,omitempty"` |
| `StorageState` | `string` | `json:"storageState,omitempty"` |
| `PublicIp` | `string` | `json:"publicIp,omitempty"` |
| `Status` | `string` | `json:"status,omitempty"` |
| `ProviderRequestId` | `string` | `json:"providerRequestId,omitempty"` |
| `ProviderRequestIDs` | `map[string]string` | `json:"providerRequestIds,omitempty"` |
| `ProviderPriceCNY` | `float64` | `json:"providerPriceCny,omitempty"` |
| `ProviderData` | `map[string]string` | `json:"providerData,omitempty"` |
| `ErrorCode` | `string` | `json:"errorCode,omitempty"` |
| `Message` | `string` | `json:"message,omitempty"` |
| `Retryable` | `bool` | `json:"retryable,omitempty"` |
| `MissingEnv` | `[]string` | `json:"missingEnv,omitempty"` |
| `Machines` | `[]MachineOutput` | `json:"machines,omitempty"` |
| `InstanceType` | `string` | `json:"instanceType,omitempty"` |
| `InstanceAvailable` | `bool` | `json:"instanceAvailable,omitempty"` |
| `RequiredCapacity` | `int64` | `json:"requiredCapacity,omitempty"` |
| `RemainingQuota` | `uint64` | `json:"remainingQuota,omitempty"` |
| `CurrentReplicas` | `int64` | `json:"currentReplicas,omitempty"` |
| `ReadyReplicas` | `int64` | `json:"readyReplicas,omitempty"` |
| `MaxReplicas` | `int64` | `json:"maxReplicas,omitempty"` |
| `TargetReplicas` | `int64` | `json:"targetReplicas,omitempty"` |
| `MachineType` | `string` | `json:"machineType,omitempty"` |
| `Zones` | `[]string` | `json:"zones,omitempty"` |
| `PreflightStages` | `[]PreflightStage` | `json:"preflightStages,omitempty"` |
| `NodePools` | `[]NodePoolBootstrapResult` | `json:"nodePools,omitempty"` |
| `NodePoolImageGC` | `[]NodePoolImageGCResult` | `json:"nodePoolImageGc,omitempty"` |
| `SKUPackages` | `[]WorkspaceSKUPackage` | `json:"skuPackages,omitempty"` |
| `PrepaidQuotaRemaining` | `uint64` | `json:"prepaidQuotaRemaining,omitempty"` |
| `Subnets` | `[]WorkspaceSubnetFact` | `json:"subnets,omitempty"` |
| `TKEClusterNodeLimit` | `uint64` | `json:"tkeClusterNodeLimit,omitempty"` |
| `TKECurrentNodeCount` | `uint64` | `json:"tkeCurrentNodeCount,omitempty"` |
| `TKEAvailableNodeCapacity` | `uint64` | `json:"tkeAvailableNodeCapacity,omitempty"` |
| `TKECapacity` | `*TKEClusterCapacityFacts` | `json:"tkeCapacity,omitempty"` |
| `ProtectedSystem` | `ProtectedSystemFacts` | `json:"protectedSystem,omitempty"` |
| `NodePoolInventory` | `[]string` | `json:"nodePoolInventoryBeforeMutation,omitempty"` |
| `MutationCount` | `int` | `json:"mutationCount"` |
| `FailureStage` | `string` | `json:"failureStage,omitempty"` |
| `ProviderErrorClass` | `string` | `json:"providerErrorClass,omitempty"` |
| `ProviderIdentityFailure` | `*ProviderIdentityFailure` | `json:"providerIdentityFailure,omitempty"` |
| `MutationEvidence` | `*MutationEvidence` | `json:"mutationEvidence,omitempty"` |

### ProviderIdentityFailure

`services/fabric/cmd/opl-tencent-provisioner/main.go:270`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Predicate` | `string` | `json:"predicate"` |
| `ExpectedDigest` | `string` | `json:"expectedDigest"` |
| `ActualDigest` | `string` | `json:"actualDigest"` |

### MutationEvidence

`services/fabric/cmd/opl-tencent-provisioner/main.go:276`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Attempted` | `int` | `json:"attempted"` |
| `Confirmed` | `int` | `json:"confirmed"` |
| `Unknown` | `int` | `json:"unknown"` |
| `Missing` | `[]string` | `json:"missing,omitempty"` |

### WorkspaceSKUCandidate

`services/fabric/cmd/opl-tencent-provisioner/main.go:283`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `InstanceType` | `string` | `json:"instanceType"` |
| `CPU` | `uint64` | `json:"cpu"` |
| `MemoryGB` | `uint64` | `json:"memoryGb"` |
| `MonthlyPriceCNY` | `float64` | `json:"monthlyPriceCny"` |
| `StockStatus` | `string` | `json:"stockStatus"` |
| `StockCategory` | `string` | `json:"stockCategory,omitempty"` |

### WorkspaceSKUPackage

`services/fabric/cmd/opl-tencent-provisioner/main.go:292`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `PackageID` | `string` | `json:"packageId"` |
| `CPU` | `uint64` | `json:"cpu"` |
| `MemoryGB` | `uint64` | `json:"memoryGb"` |
| `RecommendedInstanceType` | `string` | `json:"recommendedInstanceType"` |
| `RecommendedMonthlyPriceCNY` | `float64` | `json:"recommendedMonthlyPriceCny"` |
| `Candidates` | `[]WorkspaceSKUCandidate` | `json:"candidates"` |

### WorkspaceSubnetFact

`services/fabric/cmd/opl-tencent-provisioner/main.go:301`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SubnetID` | `string` | `json:"subnetId"` |
| `VPCID` | `string` | `json:"vpcId"` |
| `Zone` | `string` | `json:"zone"` |
| `AvailableIPAddresses` | `uint64` | `json:"availableIpAddresses"` |

### TKEClusterCapacityFacts

`services/fabric/cmd/opl-tencent-provisioner/main.go:308`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ClusterLevel` | `string` | `json:"clusterLevel"` |
| `CurrentNodeCount` | `uint64` | `json:"currentNodeCount"` |
| `LevelAttributes` | `[]TKEClusterLevelFact` | `json:"levelAttributes"` |

### TKEClusterLevelFact

`services/fabric/cmd/opl-tencent-provisioner/main.go:314`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Name` | `string` | `json:"name"` |
| `Alias` | `string` | `json:"alias,omitempty"` |
| `NodeCount` | `*uint64` | `json:"nodeCount,omitempty"` |
| `Enable` | `*bool` | `json:"enable,omitempty"` |

### ProtectedSystemFacts

`services/fabric/cmd/opl-tencent-provisioner/main.go:321`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `NodePoolID` | `string` | `json:"nodePoolId,omitempty"` |
| `PoolCheckStatus` | `string` | `json:"poolCheckStatus"` |
| `MachineID` | `string` | `json:"machineId,omitempty"` |
| `MachineCheckStatus` | `string` | `json:"machineCheckStatus"` |
| `NodeName` | `string` | `json:"nodeName,omitempty"` |
| `NodeCheckStatus` | `string` | `json:"nodeCheckStatus"` |
| `MachineType` | `string` | `json:"machineType,omitempty"` |
| `CVMApplicable` | `bool` | `json:"cvmApplicable"` |
| `CVMID` | `string` | `json:"cvmId,omitempty"` |
| `CVMCheckStatus` | `string` | `json:"cvmCheckStatus"` |

### NodePoolBootstrapResult

`services/fabric/cmd/opl-tencent-provisioner/main.go:334`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `PackageID` | `string` | `json:"packageId"` |
| `PoolID` | `string` | `json:"poolId"` |
| `NodePoolID` | `string` | `json:"nodePoolId,omitempty"` |
| `InstanceType` | `string` | `json:"instanceType"` |
| `CPU` | `uint64` | `json:"cpu"` |
| `MemoryGB` | `uint64` | `json:"memoryGb"` |
| `MaxReplicas` | `int64` | `json:"maxReplicas"` |
| `MaxReplicasSource` | `string` | `json:"maxReplicasSource"` |
| `MaxReplicasDecision` | `string` | `json:"maxReplicasDecision"` |
| `MaxReplicasConstraint` | `string` | `json:"maxReplicasConstraint"` |
| `MaxReplicasRecommendation` | `string` | `json:"maxReplicasRecommendation"` |
| `Status` | `string` | `json:"status"` |
| `ErrorCode` | `string` | `json:"errorCode,omitempty"` |
| `TaintsBefore` | `[]NodePoolTaintFact` | `json:"taintsBefore,omitempty"` |
| `TaintsAfter` | `[]NodePoolTaintFact` | `json:"taintsAfter,omitempty"` |
| `EstimatedNodeCount` | `int64` | `json:"estimatedNodeCount"` |
| `AffectedNodeNames` | `[]string` | `json:"affectedNodeNames,omitempty"` |

### NodePoolTaintFact

`services/fabric/cmd/opl-tencent-provisioner/main.go:354`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Key` | `string` | `json:"key"` |
| `Value` | `string` | `json:"value"` |
| `Effect` | `string` | `json:"effect"` |

### NodePoolImageGCResult

`services/fabric/cmd/opl-tencent-provisioner/main.go:360`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `PackageID` | `string` | `json:"packageId"` |
| `NodePoolID` | `string` | `json:"nodePoolId"` |
| `Status` | `string` | `json:"status"` |
| `HighThresholdPercent` | `int` | `json:"highThresholdPercent"` |
| `LowThresholdPercent` | `int` | `json:"lowThresholdPercent"` |
| `UpdateExistingNodes` | `bool` | `json:"updateExistingNodes"` |
| `TaintsBefore` | `[]NodePoolTaintFact` | `json:"taintsBefore"` |
| `TaintsAfter` | `[]NodePoolTaintFact` | `json:"taintsAfter"` |
| `KubeletArgsBefore` | `[]string` | `json:"kubeletArgsBefore"` |
| `KubeletArgsAfter` | `[]string` | `json:"kubeletArgsAfter"` |

### PreflightStage

`services/fabric/cmd/opl-tencent-provisioner/main.go:373`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Stage` | `string` | `json:"stage"` |
| `Status` | `string` | `json:"status"` |
| `ErrorCode` | `string` | `json:"errorCode,omitempty"` |
| `BlockedBy` | `[]string` | `json:"blockedBy"` |
| `DurationMS` | `int64` | `json:"durationMs"` |
| `SafeFacts` | `map[string]any` | `json:"safeFacts"` |

### MachineOutput

`services/fabric/cmd/opl-tencent-provisioner/main.go:382`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `MachineId` | `string` | `json:"machineId"` |
| `InstanceId` | `string` | `json:"instanceId,omitempty"` |
| `NodeName` | `string` | `json:"nodeName,omitempty"` |
| `PrivateIp` | `string` | `json:"privateIp,omitempty"` |
| `PublicIp` | `string` | `json:"publicIp,omitempty"` |
| `InstanceType` | `string` | `json:"instanceType,omitempty"` |
| `Zone` | `string` | `json:"zone,omitempty"` |
| `ChargeType` | `string` | `json:"chargeType,omitempty"` |
| `RenewFlag` | `string` | `json:"renewFlag,omitempty"` |
| `Deadline` | `string` | `json:"deadline,omitempty"` |
| `Ready` | `bool` | `json:"ready"` |

### tagResourceRequest

`services/fabric/cmd/opl-tencent-provisioner/main.go:446`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `(embedded)` | `*tchttp.BaseRequest` | `` |
| `ServiceType` | `*string` | `json:"ServiceType,omitempty" name:"ServiceType"` |
| `ResourceIds` | `[]*string` | `json:"ResourceIds,omitempty" name:"ResourceIds"` |
| `TagKey` | `*string` | `json:"TagKey,omitempty" name:"TagKey"` |
| `TagValue` | `*string` | `json:"TagValue,omitempty" name:"TagValue"` |
| `ResourceRegion` | `*string` | `json:"ResourceRegion,omitempty" name:"ResourceRegion"` |
| `ResourcePrefix` | `*string` | `json:"ResourcePrefix,omitempty" name:"ResourcePrefix"` |

### tagResourceResponse

`services/fabric/cmd/opl-tencent-provisioner/main.go:456`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `(embedded)` | `*tchttp.BaseResponse` | `` |
| `Response` | `*struct {<br>	RequestId *string `json:"RequestId,omitempty" name:"RequestId"`<br>}` | `json:"Response"` |

### getCallerIdentityResponse

`services/fabric/cmd/opl-tencent-provisioner/main.go:467`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `(embedded)` | `*tchttp.BaseResponse` | `` |
| `Response` | `*struct {<br>	Type		*string	`json:"Type,omitempty" name:"Type"`<br>	PrincipalId	*string	`json:"PrincipalId,omitempty" name:"PrincipalId"`<br>	AccountId	*string	`json:"AccountId,omitempty" name:"AccountId"`<br>	UserId		*string	`json:"UserId,omitempty" name:"UserId"`<br>	RequestId	*string	`json:"RequestId,omitempty" name:"RequestId"`<br>}` | `json:"Response"` |

### listAttachedUserPoliciesRequest

`services/fabric/cmd/opl-tencent-provisioner/main.go:478`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `(embedded)` | `*tchttp.BaseRequest` | `` |
| `TargetUin` | `*uint64` | `json:"TargetUin,omitempty" name:"TargetUin"` |
| `Page` | `*uint64` | `json:"Page,omitempty" name:"Page"` |
| `Rp` | `*uint64` | `json:"Rp,omitempty" name:"Rp"` |

### attachedUserPolicy

`services/fabric/cmd/opl-tencent-provisioner/main.go:485`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `PolicyID` | `*uint64` | `json:"PolicyId,omitempty" name:"PolicyId"` |
| `PolicyName` | `*string` | `json:"PolicyName,omitempty" name:"PolicyName"` |
| `PolicyType` | `*string` | `json:"PolicyType,omitempty" name:"PolicyType"` |
| `Deactived` | `*uint64` | `json:"Deactived,omitempty" name:"Deactived"` |

### listAttachedUserPoliciesResponse

`services/fabric/cmd/opl-tencent-provisioner/main.go:492`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `(embedded)` | `*tchttp.BaseResponse` | `` |
| `Response` | `*struct {<br>	TotalNum	*uint64			`json:"TotalNum,omitempty" name:"TotalNum"`<br>	List		[]*attachedUserPolicy	`json:"List,omitempty" name:"List"`<br>	RequestID	*string			`json:"RequestId,omitempty" name:"RequestId"`<br>}` | `json:"Response"` |

### predebitIAMIdentity

`services/fabric/cmd/opl-tencent-provisioner/main.go:501`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Type` | `string` | `json:"type"` |
| `PrincipalID` | `string` | `json:"principalId"` |
| `AccountID` | `string` | `json:"accountId"` |
| `UserID` | `string` | `json:"userId"` |

### predebitIAMAttestation

`services/fabric/cmd/opl-tencent-provisioner/main.go:508`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `ProofMode` | `string` | `json:"proofMode"` |
| `ReleaseSHA` | `string` | `json:"releaseSha"` |
| `Identity` | `predebitIAMIdentity` | `json:"identity"` |
| `RequiredActions` | `[]string` | `json:"requiredActions"` |
| `RequiredPolicies` | `[]string` | `json:"requiredPolicies"` |
| `PolicyDigest` | `string` | `json:"policyDigest"` |

### createAndBindTag

`services/fabric/cmd/opl-tencent-provisioner/main.go:526`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `TagKey` | `*string` | `json:"TagKey,omitempty" name:"TagKey"` |
| `TagValue` | `*string` | `json:"TagValue,omitempty" name:"TagValue"` |

### createAndBindTagRequest

`services/fabric/cmd/opl-tencent-provisioner/main.go:531`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `(embedded)` | `*tchttp.BaseRequest` | `` |
| `ResourceList` | `[]*string` | `json:"ResourceList,omitempty" name:"ResourceList"` |
| `Tags` | `[]*createAndBindTag` | `json:"Tags,omitempty" name:"Tags"` |

### createAndBindTagResponse

`services/fabric/cmd/opl-tencent-provisioner/main.go:537`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `(embedded)` | `*tchttp.BaseResponse` | `` |
| `Response` | `*struct {<br>	RequestId *string `json:"RequestId,omitempty" name:"RequestId"`<br>}` | `json:"Response"` |

### tencentSKUProfile

`services/fabric/cmd/opl-tencent-provisioner/main.go:82`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id"` |
| `Name` | `string` | `json:"name"` |
| `Available` | `bool` | `json:"available"` |
| `Compute` | `struct {<br>	ID		string	`json:"id"`<br>	Server		string	`json:"server"`<br>	CPU		uint64	`json:"cpu"`<br>	MemoryGB	uint64	`json:"memoryGb"`<br>	DiskGB		int	`json:"diskGb"`<br>	InstanceType	string	`json:"instanceType"`<br>}` | `json:"compute"` |
| `NodePoolID` | `string` | `json:"nodePoolId"` |
| `MaxReplicas` | `int64` | `json:"maxReplicas"` |
| `Zone` | `string` | `json:"zone"` |
| `Storage` | `struct {<br>	SizeGB		int	`json:"sizeGb"`<br>	DiskType	string	`json:"diskType"`<br>}` | `json:"storage"` |
| `Billing` | `struct {<br>	ChargeType	string	`json:"chargeType"`<br>	PeriodMonths	int64	`json:"periodMonths"`<br>	RenewFlag	string	`json:"renewFlag"`<br>}` | `json:"billing"` |

### normalLaunchMutationBudget

`services/fabric/internal/fabric/compute_allocation.go:15`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Attempted` | `int` | `json:"attempted"` |
| `Confirmed` | `int` | `json:"confirmed"` |
| `Unknown` | `int` | `json:"unknown"` |
| `Max` | `int` | `json:"max"` |

### WorkspaceLaunchStageBinding

`services/fabric/internal/fabric/launch_stage_binding.go:19`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `LaunchOperationID` | `string` | `json:"launchOperationId"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `Stage` | `string` | `json:"stage"` |
| `Action` | `string` | `json:"action"` |
| `FabricOperationID` | `string` | `json:"fabricOperationId"` |
| `IdempotencyKey` | `string` | `json:"idempotencyKey"` |
| `RequestHash` | `string` | `json:"requestHash"` |
| `ExpectedResourceBinding` | `string` | `json:"expectedResourceBinding"` |

### persistedLaunchStageBinding

`services/fabric/internal/fabric/launch_stage_binding.go:32`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Binding` | `WorkspaceLaunchStageBinding` | `json:"binding"` |
| `Digest` | `string` | `json:"digest"` |

### localDockerApplicationSecretMetadata

`services/fabric/internal/fabric/local_docker_application_secret.go:23`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `SecretRef` | `string` | `json:"secretRef"` |
| `Version` | `string` | `json:"version"` |
| `Keys` | `map[string]string` | `json:"keys"` |

### dockerNetworkInspect

`services/fabric/internal/fabric/local_docker_provider.go:363`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"Id"` |
| `Name` | `string` | `json:"Name"` |
| `Labels` | `map[string]string` | `json:"Labels"` |

### dockerVolumeInspect

`services/fabric/internal/fabric/local_docker_provider.go:369`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Name` | `string` | `json:"Name"` |
| `Labels` | `map[string]string` | `json:"Labels"` |

### dockerObjectInventoryRow

`services/fabric/internal/fabric/local_docker_provider.go:374`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"ID"` |
| `Name` | `string` | `json:"Name"` |
| `Names` | `string` | `json:"Names"` |

### localDockerProviderProfile

`services/fabric/internal/fabric/local_docker_provider.go:58`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Packages` | `[]localDockerPackageProfile` | `json:"packages"` |

### localDockerPackageProfile

`services/fabric/internal/fabric/local_docker_provider.go:63`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id"` |
| `Name` | `string` | `json:"name"` |
| `Available` | `bool` | `json:"available"` |
| `Compute` | `ComputePlan` | `json:"compute"` |
| `Storage` | `localDockerStoragePlan` | `json:"storage"` |

### localDockerStoragePlan

`services/fabric/internal/fabric/local_docker_provider.go:71`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SizeGB` | `int` | `json:"sizeGb"` |
| `QuotaPolicy` | `string` | `json:"quotaPolicy"` |

### localDockerPlanSpec

`services/fabric/internal/fabric/local_docker_provider.go:76`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Compute` | `ComputePlan` | `json:"compute"` |
| `Storage` | `localDockerStoragePlan` | `json:"storage"` |

### dockerContainerInspect

`services/fabric/internal/fabric/local_docker_runtime.go:193`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"Id"` |
| `Name` | `string` | `json:"Name"` |
| `Image` | `string` | `json:"Image"` |
| `Config` | `struct {<br>	User		string			`json:"User"`<br>	Image		string			`json:"Image"`<br>	Env		[]string		`json:"Env"`<br>	Labels		map[string]string	`json:"Labels"`<br>	Healthcheck	*struct {<br>		Test		[]string	`json:"Test"`<br>		Interval	int64		`json:"Interval"`<br>		Timeout		int64		`json:"Timeout"`<br>		Retries		int		`json:"Retries"`<br>		StartPeriod	int64		`json:"StartPeriod"`<br>	}	`json:"Healthcheck"`<br>}` | `json:"Config"` |
| `State` | `struct {<br>	Status		string	`json:"Status"`<br>	Running		bool	`json:"Running"`<br>	StartedAt	string	`json:"StartedAt"`<br>	Health		*struct {<br>		Status string `json:"Status"`<br>	}	`json:"Health"`<br>}` | `json:"State"` |
| `NetworkSettings` | `struct {<br>	Ports	map[string][]struct {<br>		HostIP		string	`json:"HostIp"`<br>		HostPort	string	`json:"HostPort"`<br>	}	`json:"Ports"`<br>	Networks	map[string]dockerEndpointSettings	`json:"Networks"`<br>}` | `json:"NetworkSettings"` |
| `HostConfig` | `struct {<br>	Init		*bool			`json:"Init"`<br>	SecurityOpt	[]string		`json:"SecurityOpt"`<br>	Tmpfs		map[string]string	`json:"Tmpfs"`<br>	NanoCPUs	int64			`json:"NanoCpus"`<br>	Memory		int64			`json:"Memory"`<br>	MemorySwap	int64			`json:"MemorySwap"`<br>	Mounts		[]dockerHostMount	`json:"Mounts"`<br>}` | `json:"HostConfig"` |
| `Mounts` | `[]dockerRuntimeMount` | `json:"Mounts"` |

### dockerEndpointSettings

`services/fabric/internal/fabric/local_docker_runtime.go:237`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `NetworkID` | `string` | `json:"NetworkID"` |
| `IPAddress` | `string` | `json:"IPAddress"` |

### localDockerGatewayMetadata

`services/fabric/internal/fabric/local_docker_runtime.go:43`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `WorkspaceAPIKeyID` | `int64` | `json:"workspaceApiKeyId"` |
| `SecretRef` | `string` | `json:"secretRef"` |
| `Fingerprint` | `string` | `json:"fingerprint"` |
| `Version` | `string` | `json:"version"` |

### dockerHostMount

`services/fabric/internal/fabric/local_docker_runtime.go:463`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Type` | `string` | `json:"Type"` |
| `Source` | `string` | `json:"Source"` |
| `Target` | `string` | `json:"Target"` |
| `ReadOnly` | `bool` | `json:"ReadOnly"` |
| `BindOptions` | `struct {<br>	Propagation string `json:"Propagation"`<br>}` | `json:"BindOptions"` |
| `TmpfsOptions` | `struct {<br>	Mode uint32 `json:"Mode"`<br>}` | `json:"TmpfsOptions"` |

### dockerRuntimeMount

`services/fabric/internal/fabric/local_docker_runtime.go:476`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Type` | `string` | `json:"Type"` |
| `Name` | `string` | `json:"Name"` |
| `Source` | `string` | `json:"Source"` |
| `Destination` | `string` | `json:"Destination"` |
| `RW` | `bool` | `json:"RW"` |
| `Propagation` | `string` | `json:"Propagation"` |

### localDockerRuntimeReservation

`services/fabric/internal/fabric/local_docker_runtime_capacity.go:20`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ResourceID` | `string` | `json:"resourceId"` |
| `PackageID` | `string` | `json:"packageId"` |
| `NanoCPUs` | `uint64` | `json:"nanoCpus"` |
| `MemoryBytes` | `uint64` | `json:"memoryBytes"` |

### localDockerInfoCapacity

`services/fabric/internal/fabric/local_docker_runtime_capacity.go:28`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `NCPU` | `uint64` | `json:"NCPU"` |
| `MemTotal` | `uint64` | `json:"MemTotal"` |

### localDockerStorageMetadata

`services/fabric/internal/fabric/local_docker_storage.go:28`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `StorageID` | `string` | `json:"storageId"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ProjectID` | `uint32` | `json:"projectId"` |
| `SizeGB` | `int` | `json:"sizeGb"` |

### localDockerStorageDeletion

`services/fabric/internal/fabric/local_docker_storage.go:37`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `StorageID` | `string` | `json:"storageId"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `WorkspaceName` | `string` | `json:"workspaceName"` |
| `ProjectID` | `uint32` | `json:"projectId"` |
| `SizeGB` | `int` | `json:"sizeGb"` |

### localDockerWorkspaceLaunchState

`services/fabric/internal/fabric/local_docker_workspace_launch.go:10`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Compute` | `*ComputeAllocation` | `json:"compute,omitempty"` |
| `ComputePlan` | `*ComputeAllocationPreparation` | `json:"computePlan,omitempty"` |
| `Storage` | `*StorageVolume` | `json:"storage,omitempty"` |
| `Attachment` | `*StorageAttachment` | `json:"attachment,omitempty"` |
| `Secret` | `*GatewaySecret` | `json:"secret,omitempty"` |
| `Runtime` | `*WorkspaceRuntime` | `json:"runtime,omitempty"` |

### FabricOperationPage

`services/fabric/internal/fabric/operation_store.go:33`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Operations` | `[]FabricOperation` | `json:"operations"` |
| `NextCursor` | `string` | `json:"nextCursor,omitempty"` |

### fabricOperationCursor

`services/fabric/internal/fabric/operation_store.go:38`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `CreatedAt` | `time.Time` | `json:"createdAt"` |
| `ID` | `string` | `json:"id"` |

### providerMutationBinding

`services/fabric/internal/fabric/provider_mutation.go:35`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Parent` | `WorkspaceLaunchStageBinding` | `json:"parent"` |
| `FabricOperationID` | `string` | `json:"fabricOperationId"` |
| `Action` | `string` | `json:"action"` |
| `ResourceKind` | `string` | `json:"resourceKind"` |
| `ResourceID` | `string` | `json:"resourceId"` |
| `ExpectedResourceBinding` | `string` | `json:"expectedResourceBinding"` |

### providerMutationReplayEpoch

`services/fabric/internal/fabric/provider_mutation.go:52`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `ReplayID` | `string` | `json:"replayId"` |
| `ParentFabricOperationID` | `string` | `json:"parentFabricOperationId"` |
| `ChildOperationID` | `string` | `json:"childOperationId"` |
| `IdempotencyKey` | `string` | `json:"idempotencyKey"` |
| `ReplayClass` | `string` | `json:"replayClass,omitempty"` |
| `AuthorityDigest` | `string` | `json:"authorityDigest,omitempty"` |
| `PreviousImageDigest` | `string` | `json:"previousImageDigest,omitempty"` |
| `ReplacementImageDigest` | `string` | `json:"replacementImageDigest,omitempty"` |
| `State` | `string` | `json:"state"` |
| `LeaseGeneration` | `int` | `json:"leaseGeneration"` |
| `LeaseExpiresAt` | `string` | `json:"leaseExpiresAt"` |
| `DispatchStartedAt` | `string` | `json:"dispatchStartedAt,omitempty"` |
| `CompletedAt` | `string` | `json:"completedAt,omitempty"` |

### persistedProviderMutationBinding

`services/fabric/internal/fabric/provider_mutation.go:69`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Binding` | `providerMutationBinding` | `json:"binding"` |
| `Digest` | `string` | `json:"digest"` |

### persistedProviderMutationState

`services/fabric/internal/fabric/provider_mutation.go:74`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Value` | `json.RawMessage` | `json:"value"` |
| `Digest` | `string` | `json:"digest"` |

### ComputePlan

`services/fabric/internal/fabric/provider_port.go:23`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id"` |
| `Server` | `string` | `json:"server"` |
| `CPU` | `int` | `json:"cpu"` |
| `MemoryGB` | `int` | `json:"memoryGb"` |
| `DiskGB` | `int` | `json:"diskGb"` |
| `InstanceType` | `string` | `json:"instanceType"` |

### plan

`services/fabric/internal/fabric/provider_port.go:97`

别名 → `ComputePlan`
| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |

### ComputeClaimRecoveryInput

`services/fabric/internal/fabric/tencent_compute_claim.go:10`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `LaunchOperationID` | `string` | `json:"launchOperationId"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ComputeAllocationID` | `string` | `json:"computeAllocationId"` |
| `StorageVolumeID` | `string` | `json:"storageVolumeId"` |
| `PackageID` | `string` | `json:"packageId"` |
| `PoolID` | `string` | `json:"poolId"` |
| `NodePoolID` | `string` | `json:"nodePoolId"` |
| `AllowExistingStorageOperation` | `bool` | `json:"allowExistingStorageOperation,omitempty"` |

### ComputeClaimStageBudget

`services/fabric/internal/fabric/tencent_compute_claim.go:121`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Attempted` | `int` | `json:"attempted"` |
| `Confirmed` | `int` | `json:"confirmed"` |
| `Unknown` | `int` | `json:"unknown"` |
| `Max` | `int` | `json:"max"` |

### ComputeClaimIdentityCheck

`services/fabric/internal/fabric/tencent_compute_claim.go:128`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Field` | `string` | `json:"field"` |
| `Matches` | `bool` | `json:"matches"` |
| `Expected` | `string` | `json:"expected,omitempty"` |
| `Actual` | `string` | `json:"actual,omitempty"` |
| `ExpectedDigest` | `string` | `json:"expectedDigest,omitempty"` |
| `ActualDigest` | `string` | `json:"actualDigest,omitempty"` |

### ComputeClaimIdentityEvidence

`services/fabric/internal/fabric/tencent_compute_claim.go:137`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Checks` | `[]ComputeClaimIdentityCheck` | `json:"checks"` |
| `BindingClassification` | `string` | `json:"bindingClassification"` |
| `BindingDigest` | `string` | `json:"bindingDigest"` |
| `MutationLedger` | `string` | `json:"mutationLedger"` |
| `MutationLedgerOutcome` | `string` | `json:"mutationLedgerOutcome"` |
| `MutationLedgerDigest` | `string` | `json:"mutationLedgerDigest"` |
| `MutationEvidence` | `*ComputeClaimEvidence` | `json:"mutationEvidence,omitempty"` |
| `FailureStage` | `string` | `json:"failureStage"` |
| `ProviderErrorClass` | `string` | `json:"providerErrorClass"` |
| `Reconciliation` | `*ComputeClaimReconciliationEvidence` | `json:"reconciliation,omitempty"` |

### ComputeClaimReconciliationEvidence

`services/fabric/internal/fabric/tencent_compute_claim.go:150`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Consumer` | `string` | `json:"consumer"` |
| `Generation` | `string` | `json:"generation"` |
| `ProvenanceSource` | `string` | `json:"provenanceSource,omitempty"` |
| `ProvenanceDigest` | `string` | `json:"provenanceDigest,omitempty"` |
| `State` | `string` | `json:"state"` |
| `ExpectedRequestHashDigest` | `string` | `json:"expectedRequestHashDigest"` |
| `PersistedRequestHashDigest` | `string` | `json:"persistedRequestHashDigest"` |
| `FailureStage` | `string` | `json:"failureStage,omitempty"` |
| `ProviderErrorClass` | `string` | `json:"providerErrorClass,omitempty"` |
| `Node` | `ComputeClaimMutationEvidence` | `json:"node"` |

### ComputeClaimRecoveryProof

`services/fabric/internal/fabric/tencent_compute_claim.go:164`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Eligible` | `bool` | `json:"eligible"` |
| `Reason` | `string` | `json:"reason"` |
| `RecoveryClassification` | `string` | `json:"recoveryClassification,omitempty"` |
| `StorageState` | `string` | `json:"storageState"` |
| `StorageProviderResourceID` | `string` | `json:"storageProviderResourceId,omitempty"` |
| `LaunchOperationID` | `string` | `json:"launchOperationId"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ComputeAllocationID` | `string` | `json:"computeAllocationId"` |
| `StorageVolumeID` | `string` | `json:"storageVolumeId"` |
| `PackageID` | `string` | `json:"packageId"` |
| `PoolID` | `string` | `json:"poolId"` |
| `NodePoolID` | `string` | `json:"nodePoolId"` |
| `MachineName` | `string` | `json:"machineName,omitempty"` |
| `NodeName` | `string` | `json:"nodeName,omitempty"` |
| `CVMInstanceID` | `string` | `json:"cvmInstanceId,omitempty"` |
| `PrivateIP` | `string` | `json:"privateIp,omitempty"` |
| `InstanceType` | `string` | `json:"instanceType,omitempty"` |
| `Zone` | `string` | `json:"zone,omitempty"` |
| `ChargeType` | `string` | `json:"chargeType,omitempty"` |
| `PeriodMonths` | `int` | `json:"periodMonths,omitempty"` |
| `RenewFlag` | `string` | `json:"renewFlag,omitempty"` |
| `Deadline` | `string` | `json:"deadline,omitempty"` |
| `NodeOwnershipState` | `string` | `json:"nodeOwnershipState,omitempty"` |
| `CVMOwnershipState` | `string` | `json:"cvmOwnershipState,omitempty"` |
| `Sub2APIMutationCount` | `int` | `json:"sub2apiMutationCount"` |
| `TencentMutationCount` | `int` | `json:"tencentMutationCount"` |
| `KubernetesMutationCount` | `int` | `json:"kubernetesMutationCount"` |
| `FailureStage` | `string` | `json:"failureStage,omitempty"` |
| `ProviderErrorClass` | `string` | `json:"providerErrorClass,omitempty"` |
| `ProviderIdentityFailure` | `*ComputeClaimProviderIdentityFailure` | `json:"providerIdentityFailure,omitempty"` |
| `Evidence` | `*ComputeClaimEvidence` | `json:"evidence,omitempty"` |
| `IdentityEvidence` | `*ComputeClaimIdentityEvidence` | `json:"identityEvidence,omitempty"` |

### ComputeClaimRecoveryClaimInput

`services/fabric/internal/fabric/tencent_compute_claim.go:24`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `(embedded)` | `ComputeClaimRecoveryInput` | `` |
| `MachineName` | `string` | `json:"machineName"` |
| `NodeName` | `string` | `json:"nodeName"` |
| `CVMInstanceID` | `string` | `json:"cvmInstanceId"` |
| `PrivateIP` | `string` | `json:"privateIp"` |
| `InstanceType` | `string` | `json:"instanceType"` |
| `Zone` | `string` | `json:"zone"` |
| `NodeOnlyContinuation` | `bool` | `json:"nodeOnlyContinuation,omitempty"` |
| `IdempotencyKey` | `string` | `json:"-"` |

### ComputeClaimProviderProof

`services/fabric/internal/fabric/tencent_compute_claim.go:36`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Status` | `string` | `json:"status"` |
| `Reason` | `string` | `json:"reason,omitempty"` |
| `NodeOwnershipState` | `string` | `json:"nodeOwnershipState"` |
| `CVMOwnershipState` | `string` | `json:"cvmOwnershipState"` |
| `MachineName` | `string` | `json:"machineName"` |
| `NodeName` | `string` | `json:"nodeName"` |
| `CVMInstanceID` | `string` | `json:"cvmInstanceId"` |
| `PrivateIP` | `string` | `json:"privateIp"` |
| `InstanceType` | `string` | `json:"instanceType"` |
| `Zone` | `string` | `json:"zone"` |
| `ChargeType` | `string` | `json:"chargeType"` |
| `PeriodMonths` | `int` | `json:"periodMonths"` |
| `RenewFlag` | `string` | `json:"renewFlag"` |
| `Deadline` | `string` | `json:"deadline"` |
| `FailureStage` | `string` | `json:"failureStage,omitempty"` |
| `ProviderErrorClass` | `string` | `json:"providerErrorClass,omitempty"` |
| `ProviderIdentityFailure` | `*ComputeClaimProviderIdentityFailure` | `json:"providerIdentityFailure,omitempty"` |

### ComputeClaimProviderIdentityFailure

`services/fabric/internal/fabric/tencent_compute_claim.go:56`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Predicate` | `string` | `json:"predicate"` |
| `ExpectedDigest` | `string` | `json:"expectedDigest"` |
| `ActualDigest` | `string` | `json:"actualDigest"` |

### ComputeClaimMutationEvidence

`services/fabric/internal/fabric/tencent_compute_claim.go:65`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Attempted` | `int` | `json:"attempted"` |
| `Confirmed` | `int` | `json:"confirmed"` |
| `Unknown` | `int` | `json:"unknown"` |
| `Missing` | `[]string` | `json:"missing,omitempty"` |

### ComputeClaimEvidence

`services/fabric/internal/fabric/tencent_compute_claim.go:72`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `CVM` | `ComputeClaimMutationEvidence` | `json:"cvm"` |
| `Node` | `ComputeClaimMutationEvidence` | `json:"node"` |

### ComputeClaimTerminalEvidence

`services/fabric/internal/fabric/tencent_compute_claim.go:81`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Stage` | `string` | `json:"stage"` |
| `Status` | `string` | `json:"status"` |
| `ErrorCode` | `string` | `json:"errorCode"` |
| `Reason` | `string` | `json:"reason,omitempty"` |
| `ReadbackStatus` | `string` | `json:"readbackStatus"` |
| `AttemptCount` | `int` | `json:"attemptCount"` |
| `Attempted` | `int` | `json:"attempted"` |
| `Confirmed` | `int` | `json:"confirmed"` |
| `Unknown` | `int` | `json:"unknown"` |
| `Max` | `int` | `json:"max"` |
| `StartedAt` | `string` | `json:"startedAt"` |
| `FinishedAt` | `string` | `json:"finishedAt"` |
| `FabricRecordID` | `string` | `json:"fabricRecordId"` |
| `OperationID` | `string` | `json:"operationId"` |
| `IdempotencyKey` | `string` | `json:"idempotencyKey"` |
| `RequestHash` | `string` | `json:"requestHash"` |
| `LaunchOperationID` | `string` | `json:"launchOperationId,omitempty"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ComputeAllocationID` | `string` | `json:"computeAllocationId"` |
| `StorageVolumeID` | `string` | `json:"storageVolumeId,omitempty"` |
| `PackageID` | `string` | `json:"packageId"` |
| `PoolID` | `string` | `json:"poolId,omitempty"` |
| `NodePoolID` | `string` | `json:"nodePoolId"` |
| `MachineName` | `string` | `json:"machineName,omitempty"` |
| `NodeName` | `string` | `json:"nodeName,omitempty"` |
| `CVMInstanceID` | `string` | `json:"cvmInstanceId,omitempty"` |
| `CVMOwnershipState` | `string` | `json:"cvmOwnershipState,omitempty"` |
| `NodeOwnershipState` | `string` | `json:"nodeOwnershipState,omitempty"` |
| `BindingDigest` | `string` | `json:"bindingDigest,omitempty"` |
| `OperatorApprovalID` | `string` | `json:"operatorApprovalId,omitempty"` |
| `OperatorApprovalDigest` | `string` | `json:"operatorApprovalDigest,omitempty"` |
| `OperatorIdempotencyKey` | `string` | `json:"operatorIdempotencyKey,omitempty"` |
| `ManualRecoveryLedgerDigest` | `string` | `json:"manualRecoveryLedgerDigest,omitempty"` |
| `Evidence` | `*ComputeClaimEvidence` | `json:"evidence,omitempty"` |
| `StageBudgets` | `map[string]ComputeClaimStageBudget` | `json:"stageBudgets,omitempty"` |

### computeClaimRecoveryBinding

`services/fabric/internal/fabric/tencent_operator_identity_ledger.go:12`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `LaunchOperationID` | `string` | `json:"launchOperationId"` |
| `IdempotencyKey` | `string` | `json:"idempotencyKey"` |
| `TargetHash` | `string` | `json:"targetHash"` |
| `RequestHash` | `string` | `json:"requestHash"` |

### computeClaimRecoveryReconciliation

`services/fabric/internal/fabric/tencent_operator_identity_ledger.go:26`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Consumer` | `string` | `json:"consumer"` |
| `Generation` | `string` | `json:"generation"` |
| `ProvenanceSource` | `string` | `json:"provenanceSource,omitempty"` |
| `ProvenanceDigest` | `string` | `json:"provenanceDigest,omitempty"` |
| `State` | `string` | `json:"state"` |
| `BindingDigest` | `string` | `json:"bindingDigest"` |
| `ExpectedRequestHashDigest` | `string` | `json:"expectedRequestHashDigest"` |
| `PersistedRequestHashDigest` | `string` | `json:"persistedRequestHashDigest"` |
| `MutationLedgerDigest` | `string` | `json:"mutationLedgerDigest"` |
| `AuthorityDigest` | `string` | `json:"authorityDigest"` |
| `FailureStage` | `string` | `json:"failureStage,omitempty"` |
| `ProviderErrorClass` | `string` | `json:"providerErrorClass,omitempty"` |
| `Node` | `ComputeClaimMutationEvidence` | `json:"node"` |

### computeClaimNodeClientRejectionRecovery

`services/fabric/internal/fabric/tencent_operator_identity_ledger.go:43`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Classification` | `string` | `json:"classification"` |
| `Invocation` | `string` | `json:"invocation"` |
| `RecordedCalls` | `int` | `json:"recordedCalls"` |
| `APIAcceptedMutations` | `int` | `json:"apiAcceptedMutations"` |
| `SourceReconciliationDigest` | `string` | `json:"sourceReconciliationDigest"` |

### computeClaimRecoveryMutationLedger

`services/fabric/internal/fabric/tencent_operator_identity_ledger.go:59`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Generation` | `string` | `json:"generation,omitempty"` |
| `AttemptDigest` | `string` | `json:"attemptDigest,omitempty"` |
| `State` | `string` | `json:"state"` |
| `Reason` | `string` | `json:"reason"` |
| `TencentMutationCount` | `int` | `json:"tencentMutationCount"` |
| `KubernetesMutationCount` | `int` | `json:"kubernetesMutationCount"` |
| `FailureStage` | `string` | `json:"failureStage,omitempty"` |
| `ProviderErrorClass` | `string` | `json:"providerErrorClass,omitempty"` |
| `Evidence` | `ComputeClaimEvidence` | `json:"evidence"` |

### provisionerAllocation

`services/fabric/internal/fabric/tencent_provider.go:1007`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id,omitempty"` |
| `InstanceID` | `string` | `json:"instanceId,omitempty"` |
| `MachineName` | `string` | `json:"machineName,omitempty"` |
| `MachineType` | `string` | `json:"machineType,omitempty"` |
| `NodeName` | `string` | `json:"nodeName,omitempty"` |
| `PrivateIP` | `string` | `json:"privateIp,omitempty"` |
| `PublicIP` | `string` | `json:"publicIp,omitempty"` |
| `Deadline` | `string` | `json:"deadline,omitempty"` |

### provisionerStorage

`services/fabric/internal/fabric/tencent_provider.go:1018`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id,omitempty"` |
| `SizeGB` | `uint64` | `json:"sizeGb,omitempty"` |
| `Zone` | `string` | `json:"zone,omitempty"` |
| `DiskType` | `string` | `json:"diskType,omitempty"` |
| `Deadline` | `string` | `json:"deadline,omitempty"` |
| `ExpectedState` | `string` | `json:"expectedState,omitempty"` |
| `ExpectedProviderResourceID` | `string` | `json:"expectedProviderResourceId,omitempty"` |
| `AllowExistingExactReplay` | `bool` | `json:"allowExistingExactReplay,omitempty"` |

### provisionerResponse

`services/fabric/internal/fabric/tencent_provider.go:1029`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Observation` | `*contracts.ResourceObservation` | `json:"observation,omitempty"` |
| `OK` | `bool` | `json:"ok"` |
| `OperationID` | `string` | `json:"operationId,omitempty"` |
| `PoolID` | `string` | `json:"poolId,omitempty"` |
| `NodePoolID` | `string` | `json:"nodePoolId,omitempty"` |
| `InstanceID` | `string` | `json:"instanceId,omitempty"` |
| `NodeName` | `string` | `json:"nodeName,omitempty"` |
| `PrivateIP` | `string` | `json:"privateIp,omitempty"` |
| `PublicIP` | `string` | `json:"publicIp,omitempty"` |
| `MachinePresent` | `*bool` | `json:"machinePresent,omitempty"` |
| `StoragePresent` | `*bool` | `json:"storagePresent,omitempty"` |
| `StorageVolumeID` | `string` | `json:"storageVolumeId,omitempty"` |
| `StorageState` | `string` | `json:"storageState,omitempty"` |
| `CBSStatus` | `string` | `json:"cbsStatus,omitempty"` |
| `CVMStatus` | `string` | `json:"cvmStatus,omitempty"` |
| `TKEStatus` | `string` | `json:"tkeStatus,omitempty"` |
| `Status` | `string` | `json:"status,omitempty"` |
| `ProviderRequestID` | `string` | `json:"providerRequestId,omitempty"` |
| `ProviderRequestIDs` | `map[string]string` | `json:"providerRequestIds,omitempty"` |
| `ProviderPriceCNY` | `float64` | `json:"providerPriceCny,omitempty"` |
| `ProviderData` | `map[string]string` | `json:"providerData,omitempty"` |
| `ErrorCode` | `string` | `json:"errorCode,omitempty"` |
| `Message` | `string` | `json:"message,omitempty"` |
| `Retryable` | `bool` | `json:"retryable,omitempty"` |
| `MissingEnv` | `[]string` | `json:"missingEnv,omitempty"` |
| `Machines` | `[]provisionerMachine` | `json:"machines,omitempty"` |
| `InstanceType` | `string` | `json:"instanceType,omitempty"` |
| `InstanceAvailable` | `bool` | `json:"instanceAvailable,omitempty"` |
| `RemainingQuota` | `uint64` | `json:"remainingQuota,omitempty"` |
| `Zones` | `[]string` | `json:"zones,omitempty"` |
| `PreflightStages` | `[]MonthlyPreflightStage` | `json:"preflightStages,omitempty"` |
| `CurrentReplicas` | `int64` | `json:"currentReplicas,omitempty"` |
| `ReadyReplicas` | `int64` | `json:"readyReplicas,omitempty"` |
| `MaxReplicas` | `int64` | `json:"maxReplicas,omitempty"` |
| `TargetReplicas` | `int64` | `json:"targetReplicas,omitempty"` |
| `MutationCount` | `int` | `json:"mutationCount"` |
| `FailureStage` | `string` | `json:"failureStage,omitempty"` |
| `ProviderErrorClass` | `string` | `json:"providerErrorClass,omitempty"` |
| `ProviderIdentityFailure` | `*ComputeClaimProviderIdentityFailure` | `json:"providerIdentityFailure,omitempty"` |
| `MutationEvidence` | `*ComputeClaimMutationEvidence` | `json:"mutationEvidence,omitempty"` |

### provisionerMachine

`services/fabric/internal/fabric/tencent_provider.go:1072`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `MachineID` | `string` | `json:"machineId"` |
| `InstanceID` | `string` | `json:"instanceId,omitempty"` |
| `NodeName` | `string` | `json:"nodeName,omitempty"` |
| `PrivateIP` | `string` | `json:"privateIp,omitempty"` |
| `PublicIP` | `string` | `json:"publicIp,omitempty"` |
| `InstanceType` | `string` | `json:"instanceType,omitempty"` |
| `Zone` | `string` | `json:"zone,omitempty"` |
| `ChargeType` | `string` | `json:"chargeType,omitempty"` |
| `RenewFlag` | `string` | `json:"renewFlag,omitempty"` |
| `Deadline` | `string` | `json:"deadline,omitempty"` |
| `Ready` | `bool` | `json:"ready"` |

### tencentStoragePlan

`services/fabric/internal/fabric/tencent_provider.go:45`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SizeGB` | `int` | `json:"sizeGb"` |
| `DiskType` | `string` | `json:"diskType"` |

### tencentBillingPlan

`services/fabric/internal/fabric/tencent_provider.go:50`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ChargeType` | `string` | `json:"chargeType"` |
| `PeriodMonths` | `int64` | `json:"periodMonths"` |
| `RenewFlag` | `string` | `json:"renewFlag"` |

### tencentPackageProfile

`services/fabric/internal/fabric/tencent_provider.go:56`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id"` |
| `Name` | `string` | `json:"name"` |
| `Available` | `bool` | `json:"available"` |
| `Compute` | `ComputePlan` | `json:"compute"` |
| `NodePoolID` | `string` | `json:"nodePoolId"` |
| `MaxReplicas` | `int64` | `json:"maxReplicas"` |
| `Zone` | `string` | `json:"zone"` |
| `Storage` | `tencentStoragePlan` | `json:"storage"` |
| `Billing` | `tencentBillingPlan` | `json:"billing"` |

### tencentProviderProfile

`services/fabric/internal/fabric/tencent_provider.go:68`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Packages` | `[]tencentPackageProfile` | `json:"packages"` |

### tencentWorkspacePlan

`services/fabric/internal/fabric/tencent_provider.go:76`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `PackageID` | `string` | `json:"packageId"` |
| `Compute` | `ComputePlan` | `json:"compute"` |
| `NodePoolID` | `string` | `json:"nodePoolId"` |
| `MaxReplicas` | `int64` | `json:"maxReplicas"` |
| `Region` | `string` | `json:"region"` |
| `Zone` | `string` | `json:"zone"` |
| `Storage` | `tencentStoragePlan` | `json:"storage"` |
| `Billing` | `tencentBillingPlan` | `json:"billing"` |

### provisionerRequest

`services/fabric/internal/fabric/tencent_provider.go:970`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Action` | `string` | `json:"action"` |
| `DryRun` | `bool` | `json:"dryRun,omitempty"` |
| `AccountID` | `string` | `json:"accountId,omitempty"` |
| `UserID` | `string` | `json:"userId,omitempty"` |
| `PackageID` | `string` | `json:"packageId,omitempty"` |
| `Region` | `string` | `json:"region,omitempty"` |
| `Zone` | `string` | `json:"zone,omitempty"` |
| `StorageVolumeID` | `string` | `json:"storageVolumeId,omitempty"` |
| `Tags` | `map[string]string` | `json:"tags,omitempty"` |
| `ComputeTags` | `map[string]string` | `json:"computeTags,omitempty"` |
| `Pool` | `provisionerPool` | `json:"pool,omitempty"` |
| `Allocation` | `provisionerAllocation` | `json:"allocation,omitempty"` |
| `Storage` | `provisionerStorage` | `json:"storage,omitempty"` |

### provisionerPool

`services/fabric/internal/fabric/tencent_provider.go:986`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id,omitempty"` |
| `ClusterID` | `string` | `json:"clusterId,omitempty"` |
| `PackageID` | `string` | `json:"packageId,omitempty"` |
| `InstanceType` | `string` | `json:"instanceType,omitempty"` |
| `CPU` | `uint64` | `json:"cpu,omitempty"` |
| `MemoryGB` | `uint64` | `json:"memoryGb,omitempty"` |
| `NodePoolID` | `string` | `json:"nodePoolId,omitempty"` |
| `Labels` | `map[string]string` | `json:"desiredNodeLabels,omitempty"` |
| `DesiredReplicas` | `int64` | `json:"desiredReplicas,omitempty"` |
| `MaxReplicas` | `int64` | `json:"maxReplicas,omitempty"` |
| `BaselineReplicas` | `int64` | `json:"baselineReplicas,omitempty"` |
| `TargetReplicas` | `int64` | `json:"targetReplicas,omitempty"` |
| `BeforeMachineNames` | `[]string` | `json:"beforeMachineNames,omitempty"` |

### computeClaimNodeTaint

`services/fabric/internal/fabric/tencent_provider_compute.go:552`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Key` | `string` | `json:"key"` |
| `Value` | `string` | `json:"value"` |
| `Effect` | `string` | `json:"effect"` |

### computeClaimNodeDocument

`services/fabric/internal/fabric/tencent_provider_compute.go:558`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Metadata` | `struct {<br>	Name		string			`json:"name"`<br>	ResourceVersion	string			`json:"resourceVersion"`<br>	Labels		map[string]string	`json:"labels"`<br>}` | `json:"metadata"` |
| `Spec` | `struct {<br>	ProviderID	string			`json:"providerID"`<br>	Taints		[]computeClaimNodeTaint	`json:"taints"`<br>}` | `json:"spec"` |
| `Status` | `struct {<br>	Addresses []struct {<br>		Type	string	`json:"type"`<br>		Address	string	`json:"address"`<br>	} `json:"addresses"`<br>}` | `json:"status"` |

### computeClaimMachineDocument

`services/fabric/internal/fabric/tencent_provider_compute.go:576`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Metadata` | `struct {<br>	Name		string	`json:"name"`<br>	ResourceVersion	string	`json:"resourceVersion"`<br>}` | `json:"metadata"` |
| `Spec` | `struct {<br>	Taints []computeClaimNodeTaint `json:"taints"`<br>}` | `json:"spec"` |

### tencentComputeMutationState

`services/fabric/internal/fabric/tencent_provider_compute.go:61`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Allocation` | `ComputeAllocation` | `json:"allocation"` |
| `Plan` | `ComputeAllocationPreparation` | `json:"plan"` |

### tencentCBSCreateMutationState

`services/fabric/internal/fabric/tencent_provider_storage.go:19`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Region` | `string` | `json:"region"` |
| `DiskType` | `string` | `json:"diskType"` |
| `Zone` | `string` | `json:"zone"` |
| `SizeGB` | `int` | `json:"sizeGb"` |
| `OperationID` | `string` | `json:"operationId"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `StorageID` | `string` | `json:"storageId"` |

### tencentWorkspaceLaunchState

`services/fabric/internal/fabric/tencent_workspace_launch.go:11`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Compute` | `*ComputeAllocation` | `json:"compute,omitempty"` |
| `ComputePlan` | `*ComputeAllocationPreparation` | `json:"computePlan,omitempty"` |
| `Ownership` | `*MachineOwnership` | `json:"ownership,omitempty"` |
| `Storage` | `*StorageVolume` | `json:"storage,omitempty"` |
| `Attachment` | `*StorageAttachment` | `json:"attachment,omitempty"` |
| `Secret` | `*GatewaySecret` | `json:"secret,omitempty"` |
| `Runtime` | `*WorkspaceRuntime` | `json:"runtime,omitempty"` |

### MonthlyPreflightReportInput

`services/fabric/internal/fabric/types.go:109`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Zone` | `string` | `json:"zone"` |

### MonthlyPreflightStage

`services/fabric/internal/fabric/types.go:113`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Stage` | `string` | `json:"stage"` |
| `Status` | `string` | `json:"status"` |
| `ErrorCode` | `string` | `json:"errorCode,omitempty"` |
| `BlockedBy` | `[]string` | `json:"blockedBy"` |
| `DurationMS` | `int64` | `json:"durationMs"` |
| `SafeFacts` | `map[string]any` | `json:"safeFacts"` |

### MonthlyPreflightReport

`services/fabric/internal/fabric/types.go:122`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Status` | `string` | `json:"status"` |
| `Zone` | `string` | `json:"zone"` |
| `Items` | `[]MonthlyPreflightStage` | `json:"items"` |
| `Packages` | `[]MonthlyPreflightPackageReport` | `json:"packages"` |
| `Sub2APIMutationCount` | `int` | `json:"sub2apiMutationCount"` |
| `TencentMutationCount` | `int` | `json:"tencentMutationCount"` |
| `KubernetesMutationCount` | `int` | `json:"kubernetesMutationCount"` |

### MonthlyPreflightPackageReport

`services/fabric/internal/fabric/types.go:133`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `PackageID` | `string` | `json:"packageId"` |
| `SizeGB` | `int` | `json:"sizeGb"` |
| `Status` | `string` | `json:"status"` |
| `Items` | `[]MonthlyPreflightStage` | `json:"items"` |

### MonthlyProviderTruth

`services/fabric/internal/fabric/types.go:140`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ComputeState` | `string` | `json:"computeState"` |
| `StorageState` | `string` | `json:"storageState"` |
| `Compute` | `ComputeAllocation` | `json:"compute"` |
| `Storage` | `StorageVolume` | `json:"storage"` |
| `ProviderRequestID` | `string` | `json:"providerRequestId,omitempty"` |
| `ErrorCode` | `string` | `json:"errorCode,omitempty"` |

### WorkspacePackage

`services/fabric/internal/fabric/types.go:149`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id"` |
| `Name` | `string` | `json:"name"` |
| `ComputeProfileID` | `string` | `json:"computeProfileId"` |
| `CPU` | `int` | `json:"cpu"` |
| `MemoryGB` | `int` | `json:"memoryGb"` |
| `DiskGB` | `int` | `json:"diskGb"` |
| `Provider` | `string` | `json:"provider"` |
| `Available` | `bool` | `json:"available"` |

### StorageClass

`services/fabric/internal/fabric/types.go:160`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id"` |
| `StorageClassName` | `string` | `json:"storageClassName"` |
| `Provider` | `string` | `json:"provider"` |
| `Available` | `bool` | `json:"available"` |

### IngressDomain

`services/fabric/internal/fabric/types.go:167`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id"` |
| `Host` | `string` | `json:"host"` |
| `PathPattern` | `string` | `json:"pathPattern"` |
| `Available` | `bool` | `json:"available"` |

### ComputeAllocationInput

`services/fabric/internal/fabric/types.go:174`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id,omitempty"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `PackageID` | `string` | `json:"packageId"` |
| `NodePoolID` | `string` | `json:"nodePoolId,omitempty"` |
| `IdempotencyKey` | `string` | `json:"-"` |
| `OperationID` | `string` | `json:"-"` |
| `DryRun` | `bool` | `json:"dryRun,omitempty"` |

### ComputeAllocation

`services/fabric/internal/fabric/types.go:187`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id"` |
| `OperationID` | `string` | `json:"operationId,omitempty"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `PackageID` | `string` | `json:"packageId"` |
| `Status` | `string` | `json:"status"` |
| `Provider` | `string` | `json:"provider"` |
| `ProviderResourceID` | `string` | `json:"providerResourceId,omitempty"` |
| `ProviderRequestID` | `string` | `json:"providerRequestId"` |
| `PoolID` | `string` | `json:"poolId,omitempty"` |
| `NodePoolID` | `string` | `json:"nodePoolId,omitempty"` |
| `InstanceID` | `string` | `json:"instanceId,omitempty"` |
| `CVMInstanceID` | `string` | `json:"cvmInstanceId,omitempty"` |
| `NodeName` | `string` | `json:"nodeName,omitempty"` |
| `MachineName` | `string` | `json:"machineName,omitempty"` |
| `PrivateIP` | `string` | `json:"privateIp,omitempty"` |
| `PublicIP` | `string` | `json:"publicIp,omitempty"` |
| `InstanceType` | `string` | `json:"instanceType,omitempty"` |
| `Zone` | `string` | `json:"zone,omitempty"` |
| `CVMStatus` | `string` | `json:"cvmStatus,omitempty"` |
| `TKEStatus` | `string` | `json:"tkeStatus,omitempty"` |
| `MachinePresent` | `*bool` | `json:"machinePresent,omitempty"` |
| `DestroyState` | `string` | `json:"destroyState,omitempty"` |
| `ObservedAt` | `string` | `json:"observedAt,omitempty"` |
| `ReadbackID` | `string` | `json:"readbackId,omitempty"` |
| `ChargeType` | `string` | `json:"chargeType,omitempty"` |
| `RenewFlag` | `string` | `json:"renewFlag,omitempty"` |
| `Deadline` | `string` | `json:"deadline,omitempty"` |
| `ServiceName` | `string` | `json:"serviceName,omitempty"` |
| `NodeSelector` | `map[string]any` | `json:"nodeSelector,omitempty"` |
| `ProviderData` | `map[string]string` | `json:"providerData,omitempty"` |
| `CostTags` | `map[string]string` | `json:"costTags,omitempty"` |
| `ClaimTerminalEvidence` | `*ComputeClaimTerminalEvidence` | `json:"claimTerminalEvidence,omitempty"` |
| `CreatedAt` | `time.Time` | `json:"createdAt"` |

### MachineOwnership

`services/fabric/internal/fabric/types.go:231`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id"` |
| `ResourceID` | `string` | `json:"resourceId"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId,omitempty"` |
| `PackageID` | `string` | `json:"packageId"` |
| `NodePoolID` | `string` | `json:"nodePoolId"` |
| `MachineID` | `string` | `json:"machineId"` |
| `InstanceID` | `string` | `json:"instanceId,omitempty"` |
| `NodeName` | `string` | `json:"nodeName,omitempty"` |
| `Status` | `string` | `json:"status"` |
| `ProviderRequestID` | `string` | `json:"providerRequestId,omitempty"` |
| `ClaimedAt` | `time.Time` | `json:"claimedAt"` |
| `ReleasedAt` | `*time.Time` | `json:"releasedAt,omitempty"` |

### ProviderMachine

`services/fabric/internal/fabric/types.go:247`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `MachineID` | `string` | `json:"machineId"` |
| `InstanceID` | `string` | `json:"instanceId,omitempty"` |
| `NodeName` | `string` | `json:"nodeName,omitempty"` |
| `PrivateIP` | `string` | `json:"privateIp,omitempty"` |
| `PublicIP` | `string` | `json:"publicIp,omitempty"` |
| `InstanceType` | `string` | `json:"instanceType,omitempty"` |
| `Zone` | `string` | `json:"zone,omitempty"` |
| `ChargeType` | `string` | `json:"chargeType,omitempty"` |
| `RenewFlag` | `string` | `json:"renewFlag,omitempty"` |
| `Deadline` | `string` | `json:"deadline,omitempty"` |
| `Ready` | `bool` | `json:"ready"` |

### ComputeAllocationPreparation

`services/fabric/internal/fabric/types.go:261`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `PoolID` | `string` | `json:"poolId"` |
| `PackageID` | `string` | `json:"packageId"` |
| `NodePoolID` | `string` | `json:"nodePoolId"` |
| `InstanceType` | `string` | `json:"instanceType"` |
| `Zone` | `string` | `json:"zone,omitempty"` |
| `MaxReplicas` | `int64` | `json:"maxReplicas"` |
| `BaselineReplicas` | `int64` | `json:"baselineReplicas"` |
| `TargetReplicas` | `int64` | `json:"targetReplicas"` |
| `BeforeMachineNames` | `[]string` | `json:"beforeMachineNames"` |
| `ProviderRequestID` | `string` | `json:"providerRequestId,omitempty"` |

### ComputeAllocationExecution

`services/fabric/internal/fabric/types.go:274`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Allocation` | `ComputeAllocation` | `json:"allocation"` |
| `Plan` | `ComputeAllocationPreparation` | `json:"plan"` |
| `DryRun` | `bool` | `json:"dryRun,omitempty"` |

### StorageVolumeInput

`services/fabric/internal/fabric/types.go:280`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id,omitempty"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ComputeID` | `string` | `json:"computeId"` |
| `Zone` | `string` | `json:"zone"` |
| `SizeGB` | `int` | `json:"sizeGb"` |
| `ExpectedRecoveryState` | `string` | `json:"expectedRecoveryState,omitempty"` |
| `ExpectedProviderResourceID` | `string` | `json:"expectedProviderResourceId,omitempty"` |
| `IdempotencyKey` | `string` | `json:"-"` |
| `OperationID` | `string` | `json:"-"` |
| `AllowExistingExactReplay` | `bool` | `json:"-"` |

### StorageVolume

`services/fabric/internal/fabric/types.go:296`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id"` |
| `OperationID` | `string` | `json:"operationId,omitempty"` |
| `AccountID` | `string` | `json:"accountId,omitempty"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `Status` | `string` | `json:"status"` |
| `Provider` | `string` | `json:"provider,omitempty"` |
| `ProviderResourceID` | `string` | `json:"providerResourceId,omitempty"` |
| `ProviderRequestID` | `string` | `json:"providerRequestId"` |
| `SizeGB` | `int` | `json:"sizeGb,omitempty"` |
| `StorageClass` | `string` | `json:"storageClass,omitempty"` |
| `CBSStatus` | `string` | `json:"cbsStatus,omitempty"` |
| `ObservedAt` | `string` | `json:"observedAt,omitempty"` |
| `ReadbackID` | `string` | `json:"readbackId,omitempty"` |
| `DestroyState` | `string` | `json:"destroyState,omitempty"` |
| `BindingPresent` | `*bool` | `json:"bindingPresent,omitempty"` |
| `DiskType` | `string` | `json:"diskType,omitempty"` |
| `RenewFlag` | `string` | `json:"renewFlag,omitempty"` |
| `Deadline` | `string` | `json:"deadline,omitempty"` |
| `Zone` | `string` | `json:"zone,omitempty"` |
| `ProviderData` | `map[string]string` | `json:"providerData,omitempty"` |
| `CostTags` | `map[string]string` | `json:"costTags,omitempty"` |
| `CreatedAt` | `time.Time` | `json:"createdAt"` |

### StorageAttachmentInput

`services/fabric/internal/fabric/types.go:332`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ComputeID` | `string` | `json:"computeId"` |
| `VolumeID` | `string` | `json:"volumeId"` |
| `IdempotencyKey` | `string` | `json:"-"` |
| `OperationID` | `string` | `json:"-"` |

### StorageAttachment

`services/fabric/internal/fabric/types.go:340`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id"` |
| `OperationID` | `string` | `json:"operationId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ComputeID` | `string` | `json:"computeId,omitempty"` |
| `VolumeID` | `string` | `json:"volumeId"` |
| `Status` | `string` | `json:"status"` |
| `Provider` | `string` | `json:"provider,omitempty"` |
| `ProviderAttachmentID` | `string` | `json:"providerAttachmentId,omitempty"` |
| `ProviderRequestID` | `string` | `json:"providerRequestId"` |
| `CostTags` | `map[string]string` | `json:"costTags,omitempty"` |
| `CreatedAt` | `time.Time` | `json:"createdAt"` |

### WorkspaceRuntimeInput

`services/fabric/internal/fabric/types.go:354`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ComputeID` | `string` | `json:"computeId"` |
| `VolumeID` | `string` | `json:"volumeId"` |
| `AttachmentID` | `string` | `json:"attachmentId"` |
| `AttachmentOperationID` | `string` | `json:"attachmentOperationId"` |
| `RuntimeOperationID` | `string` | `json:"runtimeOperationId"` |
| `PreviousRuntimeOperationID` | `string` | `json:"previousRuntimeOperationId,omitempty"` |
| `ImageID` | `string` | `json:"imageId"` |
| `GatewaySecretRef` | `string` | `json:"gatewaySecretRef"` |
| `RuntimeImageRevision` | `*WorkspaceLaunchRuntimeImageRevision` | `json:"-"` |
| `IdempotencyKey` | `string` | `json:"-"` |
| `OperationID` | `string` | `json:"-"` |

### WorkspaceRuntimeImageReplacementInput

`services/fabric/internal/fabric/types.go:372`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `LaunchOperationID` | `string` | `json:"launchOperationId"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ComputeID` | `string` | `json:"computeId"` |
| `StorageID` | `string` | `json:"storageId"` |
| `AttachmentID` | `string` | `json:"attachmentId"` |
| `RuntimeID` | `string` | `json:"runtimeId"` |
| `RuntimeOperationID` | `string` | `json:"runtimeOperationId"` |
| `RuntimeServiceName` | `string` | `json:"runtimeServiceName"` |
| `PreviousImageDigest` | `string` | `json:"previousImageDigest"` |
| `ReplacementImageDigest` | `string` | `json:"replacementImageDigest"` |
| `IdempotencyKey` | `string` | `json:"-"` |
| `OperationID` | `string` | `json:"-"` |

### WorkspaceRuntimeImageReplacementResult

`services/fabric/internal/fabric/types.go:388`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `OperationID` | `string` | `json:"operationId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `RuntimeID` | `string` | `json:"runtimeId"` |
| `PreviousImageDigest` | `string` | `json:"previousImageDigest"` |
| `ReplacementImageDigest` | `string` | `json:"replacementImageDigest"` |
| `Status` | `string` | `json:"status"` |
| `Runtime` | `WorkspaceRuntime` | `json:"runtime"` |

### WorkspaceRuntimeGatewayNetworkRecoveryInput

`services/fabric/internal/fabric/types.go:399`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ComputeID` | `string` | `json:"computeId"` |
| `RuntimeID` | `string` | `json:"runtimeId"` |
| `RuntimeOperationID` | `string` | `json:"runtimeOperationId"` |
| `RuntimeServiceName` | `string` | `json:"runtimeServiceName"` |
| `IdempotencyKey` | `string` | `json:"-"` |

### WorkspaceRuntimeGatewayNetworkRecoveryResult

`services/fabric/internal/fabric/types.go:409`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `OperationID` | `string` | `json:"operationId"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ComputeID` | `string` | `json:"computeId"` |
| `RuntimeID` | `string` | `json:"runtimeId"` |
| `RuntimeServiceName` | `string` | `json:"runtimeServiceName"` |
| `GatewayContainerID` | `string` | `json:"gatewayContainerId"` |
| `NetworkID` | `string` | `json:"networkId"` |
| `NetworkName` | `string` | `json:"networkName"` |
| `Status` | `string` | `json:"status"` |
| `Runtime` | `WorkspaceRuntime` | `json:"runtime"` |

### Catalog

`services/fabric/internal/fabric/types.go:42`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Owner` | `string` | `json:"owner"` |
| `WorkspacePackages` | `[]WorkspacePackage` | `json:"workspacePackages"` |
| `StorageClasses` | `[]StorageClass` | `json:"storageClasses"` |
| `IngressDomains` | `[]IngressDomain` | `json:"ingressDomains"` |

### WorkspaceRuntime

`services/fabric/internal/fabric/types.go:424`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Observation` | `*contracts.ResourceObservation` | `json:"-"` |
| `ID` | `string` | `json:"id"` |
| `OperationID` | `string` | `json:"operationId,omitempty"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `URL` | `string` | `json:"url,omitempty"` |
| `Status` | `string` | `json:"status"` |
| `ServiceName` | `string` | `json:"serviceName,omitempty"` |
| `ImageID` | `string` | `json:"imageId,omitempty"` |
| `ProviderRequestID` | `string` | `json:"providerRequestId"` |
| `Access` | `RuntimeAccess` | `json:"access,omitempty"` |
| `Ready` | `bool` | `json:"ready,omitempty"` |
| `Checks` | `[]Check` | `json:"checks,omitempty"` |
| `CostTags` | `map[string]string` | `json:"costTags,omitempty"` |
| `CreatedAt` | `time.Time` | `json:"createdAt"` |
| `ComputeID` | `string` | `json:"-"` |
| `NodeName` | `string` | `json:"-"` |

### RuntimeAccess

`services/fabric/internal/fabric/types.go:447`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Username` | `string` | `json:"username,omitempty"` |
| `Password` | `string` | `json:"password,omitempty"` |
| `CredentialStatus` | `string` | `json:"credentialStatus,omitempty"` |
| `CredentialVersion` | `string` | `json:"credentialVersion,omitempty"` |
| `SecretRef` | `string` | `json:"secretRef,omitempty"` |
| `UpdatedAt` | `time.Time` | `json:"updatedAt,omitempty"` |

### GatewaySecretInput

`services/fabric/internal/fabric/types.go:456`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `WorkspaceAPIKeyID` | `int64` | `json:"workspaceApiKeyId"` |
| `Fingerprint` | `string` | `json:"fingerprint"` |
| `GatewayAPIKey` | `string` | `json:"gatewayApiKey"` |
| `IdempotencyKey` | `string` | `json:"-"` |

### GatewaySecret

`services/fabric/internal/fabric/types.go:465`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SecretRef` | `string` | `json:"secretRef"` |
| `Version` | `string` | `json:"version"` |
| `Fingerprint` | `string` | `json:"fingerprint"` |

### WorkspaceRuntimeGatewaySecretInput

`services/fabric/internal/fabric/types.go:471`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `WorkspaceAPIKeyID` | `int64` | `json:"workspaceApiKeyId"` |
| `SecretRef` | `string` | `json:"secretRef"` |
| `Fingerprint` | `string` | `json:"fingerprint"` |
| `IdempotencyKey` | `string` | `json:"-"` |

### WorkspaceRuntimeGatewaySecretBinding

`services/fabric/internal/fabric/types.go:479`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `WorkspaceAPIKeyID` | `int64` | `json:"workspaceApiKeyId"` |
| `SecretRef` | `string` | `json:"secretRef"` |
| `Fingerprint` | `string` | `json:"fingerprint"` |
| `Bound` | `bool` | `json:"bound"` |

### WorkspaceRuntimeObservation

`services/fabric/internal/fabric/types.go:497`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `State` | `string` | `json:"state"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `Runtime` | `*WorkspaceRuntime` | `json:"runtime,omitempty"` |

### MonthlyPreflightInput

`services/fabric/internal/fabric/types.go:50`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ResourceType` | `string` | `json:"resourceType"` |
| `PackageID` | `string` | `json:"packageId"` |
| `SizeGB` | `int` | `json:"sizeGb,omitempty"` |
| `Zone` | `string` | `json:"zone"` |

### WorkspaceRuntimeGatewaySecretObservation

`services/fabric/internal/fabric/types.go:504`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `State` | `string` | `json:"state"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `Binding` | `*WorkspaceRuntimeGatewaySecretBinding` | `json:"binding,omitempty"` |

### WorkspaceRuntimeDeleteResidual

`services/fabric/internal/fabric/types.go:515`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Kind` | `string` | `json:"kind"` |
| `Name` | `string` | `json:"name"` |

### WorkspaceRuntimeDeleteObservation

`services/fabric/internal/fabric/types.go:520`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `State` | `string` | `json:"state"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `Residuals` | `[]WorkspaceRuntimeDeleteResidual` | `json:"residuals,omitempty"` |
| `ObservedAt` | `string` | `json:"observedAt,omitempty"` |
| `ReadbackID` | `string` | `json:"readbackId,omitempty"` |

### ProviderFactInput

`services/fabric/internal/fabric/types.go:531`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ResourceType` | `string` | `json:"resourceType"` |
| `ResourceID` | `string` | `json:"resourceId"` |

### ProviderFactsBatchInput

`services/fabric/internal/fabric/types.go:538`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Items` | `[]ProviderFactInput` | `json:"items"` |

### ProviderResourceFacts

`services/fabric/internal/fabric/types.go:542`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Observation` | `*contracts.ResourceObservation` | `json:"-"` |
| `PackageOrSpec` | `string` | `json:"packageOrSpec,omitempty"` |
| `ProviderID` | `string` | `json:"providerId,omitempty"` |
| `Zone` | `string` | `json:"zone,omitempty"` |
| `Status` | `string` | `json:"status,omitempty"` |
| `CreatedAt` | `string` | `json:"createdAt,omitempty"` |
| `ExpiresAt` | `string` | `json:"expiresAt,omitempty"` |
| `LastReadAt` | `string` | `json:"lastReadAt,omitempty"` |
| `ComputeRuntimeBinding` | `*contracts.WorkspaceComputeRuntimeBinding` | `json:"computeRuntimeBinding,omitempty"` |

### ProviderFact

`services/fabric/internal/fabric/types.go:555`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ResourceType` | `string` | `json:"resourceType"` |
| `ResourceID` | `string` | `json:"resourceId"` |
| `Available` | `bool` | `json:"available"` |
| `Facts` | `ProviderResourceFacts` | `json:"facts,omitempty"` |
| `ErrorCode` | `string` | `json:"errorCode,omitempty"` |
| `Observation` | `*contracts.ResourceObservation` | `json:"observation,omitempty"` |

### FabricReadiness

`services/fabric/internal/fabric/types.go:566`

别名 → `contracts.FabricReadiness`
| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |

### ProviderFactsBatch

`services/fabric/internal/fabric/types.go:568`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Items` | `[]ProviderFact` | `json:"items"` |

### MonthlyPreflight

`services/fabric/internal/fabric/types.go:57`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ResourceType` | `string` | `json:"resourceType"` |
| `PackageID` | `string` | `json:"packageId"` |
| `NodePoolID` | `string` | `json:"nodePoolId,omitempty"` |
| `SizeGB` | `int` | `json:"sizeGb,omitempty"` |
| `Zone` | `string` | `json:"zone"` |
| `Available` | `bool` | `json:"available"` |
| `ChargeType` | `string` | `json:"chargeType"` |
| `PeriodMonths` | `int` | `json:"periodMonths"` |
| `RenewFlag` | `string` | `json:"renewFlag"` |
| `ProviderPriceCNY` | `float64` | `json:"providerPriceCny"` |
| `ProviderRequestIDs` | `map[string]string` | `json:"providerRequestIds"` |

### RuntimeHealthSummary

`services/fabric/internal/fabric/types.go:572`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Total` | `int` | `json:"total"` |
| `Ready` | `int` | `json:"ready"` |
| `Unready` | `int` | `json:"unready"` |

### Check

`services/fabric/internal/fabric/types.go:578`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Name` | `string` | `json:"name"` |
| `OK` | `bool` | `json:"ok"` |
| `Details` | `map[string]any` | `json:"details,omitempty"` |

### JobInput

`services/fabric/internal/fabric/types.go:584`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `OrganizationID` | `string` | `json:"organizationId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ProjectID` | `string` | `json:"projectId"` |
| `TaskID` | `string` | `json:"taskId"` |
| `RequestID` | `string` | `json:"requestId"` |
| `ApprovalID` | `string` | `json:"approvalId"` |
| `EnvironmentRef` | `string` | `json:"environmentRef,omitempty"` |
| `IdempotencyKey` | `string` | `json:"-"` |

### JobClaimInput

`services/fabric/internal/fabric/types.go:595`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `RunnerID` | `string` | `json:"runnerId"` |
| `IdempotencyKey` | `string` | `json:"-"` |

### JobHeartbeatInput

`services/fabric/internal/fabric/types.go:600`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `RunnerID` | `string` | `json:"runnerId"` |
| `LeaseToken` | `string` | `json:"leaseToken"` |
| `IdempotencyKey` | `string` | `json:"-"` |

### JobCompleteInput

`services/fabric/internal/fabric/types.go:606`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `RunnerID` | `string` | `json:"runnerId"` |
| `LeaseToken` | `string` | `json:"leaseToken"` |
| `ArtifactIDs` | `[]string` | `json:"artifactIds"` |
| `ReviewIDs` | `[]string` | `json:"reviewIds"` |
| `IdempotencyKey` | `string` | `json:"-"` |

### JobFailInput

`services/fabric/internal/fabric/types.go:614`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `RunnerID` | `string` | `json:"runnerId"` |
| `LeaseToken` | `string` | `json:"leaseToken"` |
| `ErrorCode` | `string` | `json:"errorCode"` |
| `IdempotencyKey` | `string` | `json:"-"` |

### Job

`services/fabric/internal/fabric/types.go:621`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `JobID` | `string` | `json:"jobId"` |
| `OrganizationID` | `string` | `json:"organizationId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ProjectID` | `string` | `json:"projectId"` |
| `TaskID` | `string` | `json:"taskId"` |
| `RequestID` | `string` | `json:"requestId"` |
| `ApprovalID` | `string` | `json:"approvalId"` |
| `EnvironmentRef` | `string` | `json:"environmentRef,omitempty"` |
| `Status` | `string` | `json:"status"` |
| `Attempt` | `int` | `json:"attempt"` |
| `LeaseOwner` | `string` | `json:"leaseOwner,omitempty"` |
| `LeaseExpiresAt` | `*time.Time` | `json:"leaseExpiresAt,omitempty"` |
| `LeaseToken` | `string` | `json:"leaseToken,omitempty"` |
| `ArtifactIDs` | `[]string` | `json:"artifactIds,omitempty"` |
| `ReviewIDs` | `[]string` | `json:"reviewIds,omitempty"` |
| `ErrorCode` | `string` | `json:"errorCode,omitempty"` |
| `CreatedAt` | `time.Time` | `json:"createdAt"` |
| `UpdatedAt` | `time.Time` | `json:"updatedAt"` |
| `Replayed` | `bool` | `json:"replayed,omitempty"` |
| `leaseTokenHash` | `string` | `` |

### FabricOperation

`services/fabric/internal/fabric/types.go:644`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id"` |
| `OperationID` | `string` | `json:"operationId"` |
| `CallerService` | `string` | `json:"callerService"` |
| `Action` | `string` | `json:"action"` |
| `ResourceKind` | `string` | `json:"resourceKind"` |
| `ResourceID` | `string` | `json:"resourceId"` |
| `AccountID` | `string` | `json:"accountId,omitempty"` |
| `WorkspaceID` | `string` | `json:"workspaceId,omitempty"` |
| `Provider` | `string` | `json:"provider,omitempty"` |
| `ProviderRequestID` | `string` | `json:"providerRequestId,omitempty"` |
| `IdempotencyKey` | `string` | `json:"idempotencyKey,omitempty"` |
| `RequestHash` | `string` | `json:"requestHash,omitempty"` |
| `RedactedProviderPayload` | `map[string]any` | `json:"redactedProviderPayload,omitempty"` |
| `Status` | `string` | `json:"status"` |
| `ErrorCode` | `string` | `json:"errorCode,omitempty"` |
| `Retryable` | `bool` | `json:"retryable,omitempty"` |
| `ComputePoolKey` | `string` | `json:"-"` |
| `ComputePoolLeaseOwner` | `string` | `json:"-"` |
| `ComputePoolLeaseExpires` | `*time.Time` | `json:"-"` |
| `StartedAt` | `time.Time` | `json:"startedAt"` |
| `FinishedAt` | `time.Time` | `json:"finishedAt,omitempty"` |
| `CreatedAt` | `time.Time` | `json:"createdAt"` |

### ComputePoolHeadReadback

`services/fabric/internal/fabric/types.go:71`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Status` | `string` | `json:"status"` |
| `ContinuationState` | `string` | `json:"continuationState"` |
| `FailureStage` | `string` | `json:"failureStage"` |
| `ErrorCode` | `string` | `json:"errorCode"` |

### ComputePoolHeadTerminalizationInput

`services/fabric/internal/fabric/types.go:79`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `NodePoolID` | `string` | `json:"nodePoolId"` |
| `ApprovalID` | `string` | `json:"approvalId"` |
| `ApprovalDigest` | `string` | `json:"approvalDigest"` |
| `IdempotencyKey` | `string` | `json:"-"` |

### ComputePoolHeadTerminalizationAuthorization

`services/fabric/internal/fabric/types.go:86`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `NodePoolID` | `string` | `json:"nodePoolId"` |

### ComputePoolHeadTerminalizationReadback

`services/fabric/internal/fabric/types.go:92`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Status` | `string` | `json:"status"` |
| `HeadStatus` | `string` | `json:"headStatus"` |
| `AllocationStatus` | `string` | `json:"allocationStatus"` |
| `OwnershipStatus` | `string` | `json:"ownershipStatus"` |
| `TerminalStatus` | `string` | `json:"terminalStatus,omitempty"` |
| `ApprovalDigest` | `string` | `json:"approvalDigest"` |
| `BindingDigest` | `string` | `json:"bindingDigest"` |
| `ManualRecoveryLedgerDigest` | `string` | `json:"manualRecoveryLedgerDigest"` |
| `AuthorizationScope` | `*ComputePoolHeadTerminalizationAuthorization` | `json:"authorizationScope,omitempty"` |
| `Replayed` | `bool` | `json:"replayed"` |
| `Sub2APIMutationCount` | `int` | `json:"sub2apiMutationCount"` |
| `TencentMutationCount` | `int` | `json:"tencentMutationCount"` |
| `KubernetesMutationCount` | `int` | `json:"kubernetesMutationCount"` |

### historicalWorkspaceApplicationRuntimeInput

`services/fabric/internal/fabric/workspace_application_historical.go:9`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ComputeID` | `string` | `json:"computeId"` |
| `VolumeID` | `string` | `json:"volumeId"` |
| `AttachmentID` | `string` | `json:"attachmentId"` |
| `AttachmentOperationID` | `string` | `json:"attachmentOperationId"` |
| `RuntimeOperationID` | `string` | `json:"runtimeOperationId"` |
| `Revision` | `contracts.WorkspaceApplicationRevision` | `json:"revision"` |
| `ConfigurationDigest` | `string` | `json:"configurationDigest"` |

### WorkspaceApplicationRuntimeLifecycleResult

`services/fabric/internal/fabric/workspace_application_lifecycle.go:10`

别名 → `contracts.WorkspaceApplicationRuntimeLifecycleResult`
| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |

### workspaceApplicationLifecycleRecord

`services/fabric/internal/fabric/workspace_application_lifecycle.go:18`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Input` | `WorkspaceApplicationRuntimeLifecycleInput` | `json:"input"` |
| `Result` | `WorkspaceApplicationRuntimeLifecycleResult` | `json:"result"` |

### WorkspaceApplicationRuntimeLifecycleInput

`services/fabric/internal/fabric/workspace_application_lifecycle.go:9`

别名 → `contracts.WorkspaceApplicationRuntimeLifecycleInput`
| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |

### WorkspaceApplicationRuntimeInput

`services/fabric/internal/fabric/workspace_application_runtime.go:24`

别名 → `contracts.WorkspaceApplicationRuntimeInput`
| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |

### workspaceApplicationRuntimeRecord

`services/fabric/internal/fabric/workspace_application_runtime.go:38`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `RuntimeID` | `string` | `json:"runtimeId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `Observation` | `contracts.WorkspaceApplicationRuntimeObservation` | `json:"observation"` |
| `Input` | `WorkspaceApplicationRuntimeInput` | `json:"input"` |

### WorkspaceApplicationGatewaySecretCleanupInput

`services/fabric/internal/fabric/workspace_application_secret_cleanup.go:10`

别名 → `contracts.WorkspaceApplicationGatewaySecretCleanupInput`
| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |

### WorkspaceLaunchCloseoutInput

`services/fabric/internal/fabric/workspace_launch_closeout.go:14`

别名 → `contracts.WorkspaceLaunchCloseoutInput`
| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |

### WorkspaceLaunchCloseoutResult

`services/fabric/internal/fabric/workspace_launch_closeout.go:15`

别名 → `contracts.WorkspaceLaunchCloseoutResult`
| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |

### WorkspaceLaunchGatewayCredential

`services/fabric/internal/fabric/workspace_launch_stage.go:108`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `KeyID` | `int64` | `json:"keyId"` |
| `Value` | `string` | `json:"value"` |

### WorkspaceLaunchRuntimeImageRevision

`services/fabric/internal/fabric/workspace_launch_stage.go:113`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `LaunchOperationID` | `string` | `json:"launchOperationId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `RuntimeOperationID` | `string` | `json:"runtimeOperationId"` |
| `AuthorizationDigest` | `string` | `json:"authorizationDigest"` |
| `PreviousImageDigest` | `string` | `json:"previousImageDigest"` |
| `ReplacementImageDigest` | `string` | `json:"replacementImageDigest"` |

### WorkspaceLaunchStageInput

`services/fabric/internal/fabric/workspace_launch_stage.go:123`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Binding` | `WorkspaceLaunchStageBinding` | `json:"binding"` |
| `ProviderProfileRef` | `string` | `json:"providerProfileRef"` |
| `ProviderBindingRef` | `string` | `json:"providerBindingRef"` |
| `SpecDigest` | `string` | `json:"specDigest"` |
| `PackageID` | `string` | `json:"packageId"` |
| `SizeGB` | `int` | `json:"sizeGb"` |
| `WorkspaceImageDigest` | `string` | `json:"workspaceImageDigest"` |
| `ProvisioningMode` | `string` | `json:"provisioningMode,omitempty"` |
| `Resources` | `WorkspaceLaunchResources` | `json:"resources"` |
| `GatewayCredential` | `*WorkspaceLaunchGatewayCredential` | `json:"gatewayCredential,omitempty"` |
| `RuntimeImageRevision` | `*WorkspaceLaunchRuntimeImageRevision` | `json:"runtimeImageRevision,omitempty"` |

### WorkspaceLaunchStageResult

`services/fabric/internal/fabric/workspace_launch_stage.go:137`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `State` | `string` | `json:"state"` |
| `Reason` | `string` | `json:"reason"` |
| `Binding` | `WorkspaceLaunchStageBinding` | `json:"binding"` |
| `Resources` | `WorkspaceLaunchResources` | `json:"resources"` |
| `Diagnostic` | `*WorkspaceLaunchStageDiagnostic` | `json:"diagnostic,omitempty"` |

### WorkspaceLaunchStageDiagnostic

`services/fabric/internal/fabric/workspace_launch_stage.go:146`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Owner` | `string` | `json:"owner"` |
| `BlockReason` | `string` | `json:"blockReason"` |
| `ErrorCode` | `string` | `json:"errorCode,omitempty"` |
| `Retryable` | `bool` | `json:"retryable"` |
| `ObservedAt` | `string` | `json:"observedAt"` |
| `Checks` | `[]Check` | `json:"checks,omitempty"` |

### persistedWorkspaceLaunchStageDiagnostic

`services/fabric/internal/fabric/workspace_launch_stage.go:156`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Diagnostic` | `WorkspaceLaunchStageDiagnostic` | `json:"diagnostic"` |
| `Digest` | `string` | `json:"digest"` |

### workspaceLaunchStageRecord

`services/fabric/internal/fabric/workspace_launch_stage.go:161`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `ProviderProfileRef` | `string` | `json:"providerProfileRef"` |
| `ProviderBindingRef` | `string` | `json:"providerBindingRef"` |
| `SpecDigest` | `string` | `json:"specDigest"` |
| `RequestResources` | `WorkspaceLaunchResources` | `json:"requestResources"` |
| `Resources` | `WorkspaceLaunchResources` | `json:"resources"` |
| `GatewayKeyID` | `int64` | `json:"gatewayKeyId,omitempty"` |
| `RuntimeImageRevision` | `*WorkspaceLaunchRuntimeImageRevision` | `json:"runtimeImageRevision,omitempty"` |
| `ProviderState` | `json.RawMessage` | `json:"providerState,omitempty"` |
| `ComputePoolQueued` | `bool` | `json:"computePoolQueued,omitempty"` |

### persistedWorkspaceLaunchStageRecord

`services/fabric/internal/fabric/workspace_launch_stage.go:174`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Record` | `workspaceLaunchStageRecord` | `json:"record"` |
| `Digest` | `string` | `json:"digest"` |

### WorkspaceLaunchPreflightInput

`services/fabric/internal/fabric/workspace_launch_stage.go:35`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `LaunchOperationID` | `string` | `json:"launchOperationId"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `PackageID` | `string` | `json:"packageId"` |
| `SizeGB` | `int` | `json:"sizeGb"` |
| `WorkspaceImageDigest` | `string` | `json:"workspaceImageDigest"` |
| `ProvisioningMode` | `string` | `json:"provisioningMode,omitempty"` |
| `RequestHash` | `string` | `json:"requestHash"` |

### WorkspaceLaunchPreflight

`services/fabric/internal/fabric/workspace_launch_stage.go:47`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Available` | `bool` | `json:"available"` |
| `Reason` | `string` | `json:"reason"` |
| `LaunchOperationID` | `string` | `json:"launchOperationId"` |
| `RequestHash` | `string` | `json:"requestHash"` |
| `ProviderProfileRef` | `string` | `json:"providerProfileRef"` |
| `ProviderBindingRef` | `string` | `json:"providerBindingRef"` |
| `SpecDigest` | `string` | `json:"specDigest"` |

### WorkspaceLaunchPreflightReadInput

`services/fabric/internal/fabric/workspace_launch_stage.go:58`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ProviderBindingRef` | `string` | `json:"providerBindingRef"` |

### WorkspaceLaunchPreflightBinding

`services/fabric/internal/fabric/workspace_launch_stage.go:64`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `LaunchOperationID` | `string` | `json:"launchOperationId"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `PackageID` | `string` | `json:"packageId"` |
| `SizeGB` | `int` | `json:"sizeGb"` |
| `WorkspaceImageDigest` | `string` | `json:"workspaceImageDigest"` |
| `RequestHash` | `string` | `json:"requestHash"` |
| `ProviderProfileRef` | `string` | `json:"providerProfileRef"` |
| `ProviderBindingRef` | `string` | `json:"providerBindingRef"` |
| `SpecDigest` | `string` | `json:"specDigest"` |

### workspaceLaunchPreflightAdmission

`services/fabric/internal/fabric/workspace_launch_stage.go:78`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Input` | `WorkspaceLaunchPreflightInput` | `json:"input"` |
| `ProviderProfileRef` | `string` | `json:"providerProfileRef"` |
| `ProviderBindingRef` | `string` | `json:"providerBindingRef"` |
| `CanonicalProviderPlan` | `json.RawMessage` | `json:"canonicalProviderPlan"` |
| `SpecDigest` | `string` | `json:"specDigest"` |

### WorkspaceLaunchResources

`services/fabric/internal/fabric/workspace_launch_stage.go:87`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ComputeAllocationID` | `string` | `json:"computeAllocationId,omitempty"` |
| `ComputeBindingRef` | `string` | `json:"computeBindingRef,omitempty"` |
| `StorageID` | `string` | `json:"storageId,omitempty"` |
| `StorageBindingRef` | `string` | `json:"storageBindingRef,omitempty"` |
| `AttachmentID` | `string` | `json:"attachmentId,omitempty"` |
| `AttachmentBindingRef` | `string` | `json:"attachmentBindingRef,omitempty"` |
| `GatewaySecretRef` | `string` | `json:"gatewaySecretRef,omitempty"` |
| `GatewaySecretVersion` | `string` | `json:"gatewaySecretVersion,omitempty"` |
| `GatewaySecretFingerprint` | `string` | `json:"gatewaySecretFingerprint,omitempty"` |
| `SecretBindingRef` | `string` | `json:"secretBindingRef,omitempty"` |
| `RuntimeID` | `string` | `json:"runtimeId,omitempty"` |
| `RuntimeServiceName` | `string` | `json:"runtimeServiceName,omitempty"` |
| `RuntimeUsername` | `string` | `json:"runtimeUsername,omitempty"` |
| `RuntimeURL` | `string` | `json:"runtimeUrl,omitempty"` |
| `RuntimeCredentialStatus` | `string` | `json:"runtimeCredentialStatus,omitempty"` |
| `RuntimeCredentialVersion` | `string` | `json:"runtimeCredentialVersion,omitempty"` |
| `RuntimeCredentialSecretRef` | `string` | `json:"runtimeCredentialSecretRef,omitempty"` |
| `RuntimeBindingRef` | `string` | `json:"runtimeBindingRef,omitempty"` |

### workspaceRuntimeRepairBinding

`services/fabric/internal/fabric/workspace_runtime.go:17`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `PreviousRuntimeOperationID` | `string` | `json:"previousRuntimeOperationId"` |
| `ReplacementRuntimeOperationID` | `string` | `json:"replacementRuntimeOperationId"` |
| `ImageID` | `string` | `json:"imageId"` |

### WorkspaceRuntimePowerInput

`services/fabric/internal/fabric/workspace_runtime_power.go:12`

别名 → `contracts.WorkspaceRuntimePowerInput`
| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |

### WorkspaceRuntimePowerResult

`services/fabric/internal/fabric/workspace_runtime_power.go:13`

别名 → `contracts.WorkspaceRuntimePowerResult`
| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |

### fabricCapabilityClaims

`services/fabric/internal/http/server.go:743`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Version` | `int` | `json:"version"` |
| `Caller` | `string` | `json:"caller"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ResourceKind` | `string` | `json:"resourceKind"` |
| `ResourceID` | `string` | `json:"resourceId"` |
| `Action` | `string` | `json:"action"` |
| `OperationID` | `string` | `json:"operationId"` |
| `ExpiresAt` | `int64` | `json:"expiresAt"` |
| `BodySHA256` | `string` | `json:"bodySha256"` |

## ledger


### ledgerCapabilityClaims

`services/ledger/internal/http/capability.go:26`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Version` | `int` | `json:"version"` |
| `Caller` | `string` | `json:"caller"` |
| `AccountID` | `string` | `json:"accountId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ResourceKind` | `string` | `json:"resourceKind"` |
| `ResourceID` | `string` | `json:"resourceId"` |
| `Action` | `string` | `json:"action"` |
| `OperationID` | `string` | `json:"operationId"` |
| `ExpiresAt` | `int64` | `json:"expiresAt"` |
| `BodySHA256` | `string` | `json:"bodySha256"` |

### EvidenceIndexInput

`services/ledger/internal/ledger/evidence_index.go:36`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `OperationID` | `string` | `json:"operationId"` |
| `CandidateSHA` | `string` | `json:"candidateSha"` |
| `CandidateTree` | `string` | `json:"candidateTree"` |
| `ImageDigest` | `string` | `json:"imageDigest"` |
| `ReceiptID` | `string` | `json:"receiptId"` |
| `ReceiptType` | `string` | `json:"receiptType"` |
| `Status` | `string` | `json:"status"` |
| `Actor` | `string` | `json:"actor"` |
| `ObservedAt` | `time.Time` | `json:"observedAt"` |
| `IdentityDigest` | `string` | `json:"identityDigest"` |
| `RedactedLink` | `string` | `json:"redactedLink,omitempty"` |
| `IdempotencyKey` | `string` | `json:"-"` |

### EvidenceIndexEntry

`services/ledger/internal/ledger/evidence_index.go:51`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `(embedded)` | `EvidenceIndexInput` | `` |
| `EvidenceID` | `string` | `json:"evidenceId"` |
| `CreatedAt` | `time.Time` | `json:"createdAt"` |
| `Replayed` | `bool` | `json:"replayed"` |

### EvidenceIndexPage

`services/ledger/internal/ledger/evidence_index.go:70`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Entries` | `[]EvidenceIndexEntry` | `json:"entries"` |
| `NextCursor` | `string` | `json:"nextCursor"` |
| `HasMore` | `bool` | `json:"hasMore"` |

### EvidenceIndexExport

`services/ledger/internal/ledger/evidence_index.go:78`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `SchemaVersion` | `int` | `json:"schemaVersion"` |
| `Entries` | `[]EvidenceIndexEntry` | `json:"entries"` |

### evidenceIndexCursor

`services/ledger/internal/ledger/evidence_index.go:83`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ObservedAt` | `time.Time` | `json:"observedAt"` |
| `EvidenceID` | `string` | `json:"evidenceId"` |

### receiptPayload

`services/ledger/internal/ledger/postgres_store.go:600`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `(embedded)` | `ReceiptInput` | `` |
| `Retention` | `ReceiptRetention` | `json:"retention"` |

### Receipt

`services/ledger/internal/ledger/types.go:105`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `(embedded)` | `ReceiptInput` | `` |
| `ReceiptID` | `string` | `json:"receiptId"` |
| `CreatedAt` | `time.Time` | `json:"createdAt"` |
| `Retention` | `ReceiptRetention` | `json:"retention"` |
| `Replayed` | `bool` | `json:"replayed"` |

### ReceiptPage

`services/ledger/internal/ledger/types.go:135`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Lookup` | `*contracts.ReceiptLookupScope` | `json:"lookup,omitempty"` |
| `Receipts` | `[]Receipt` | `json:"receipts"` |
| `NextCursor` | `string` | `json:"nextCursor"` |
| `HasMore` | `bool` | `json:"hasMore"` |

### receiptCursor

`services/ledger/internal/ledger/types.go:142`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `CreatedAt` | `time.Time` | `json:"createdAt"` |
| `ReceiptID` | `string` | `json:"receiptId"` |

### ReceiptInput

`services/ledger/internal/ledger/types.go:31`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Type` | `string` | `json:"type"` |
| `Status` | `string` | `json:"status"` |
| `Surface` | `string` | `json:"surface"` |
| `AccountID` | `string` | `json:"accountId,omitempty"` |
| `OrganizationID` | `string` | `json:"organizationId"` |
| `WorkspaceID` | `string` | `json:"workspaceId"` |
| `ProjectID` | `string` | `json:"projectId"` |
| `TaskID` | `string` | `json:"taskId"` |
| `RequestID` | `string` | `json:"requestId"` |
| `ApprovalID` | `string` | `json:"approvalId"` |
| `JobID` | `string` | `json:"jobId"` |
| `ArtifactID` | `string` | `json:"artifactId"` |
| `ReviewID` | `string` | `json:"reviewId"` |
| `ContinuationID` | `string` | `json:"continuationId"` |
| `Actor` | `map[string]any` | `json:"actor"` |
| `Plan` | `map[string]any` | `json:"plan"` |
| `Execution` | `map[string]any` | `json:"execution"` |
| `Environment` | `map[string]any` | `json:"environment"` |
| `InputRefs` | `map[string]any` | `json:"inputRefs"` |
| `OutputRefs` | `map[string]any` | `json:"outputRefs"` |
| `ReviewerChecks` | `map[string]any` | `json:"reviewerChecks"` |
| `Cost` | `map[string]any` | `json:"cost"` |
| `Owner` | `map[string]any` | `json:"owner"` |
| `Continuation` | `map[string]any` | `json:"continuation"` |
| `SupersedesReceiptID` | `string` | `json:"supersedesReceiptId"` |
| `IdempotencyKey` | `string` | `json:"-"` |

### ReceiptRetention

`services/ledger/internal/ledger/types.go:60`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `RetainUntil` | `time.Time` | `json:"retainUntil,omitempty"` |
| `LegalHold` | `bool` | `json:"legalHold"` |
| `PrivacyRedaction` | `*PrivacyRedactionEvidence` | `json:"privacyRedaction,omitempty"` |

### ReconciliationInput

`services/ledger/internal/ledger/types.go:724`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `Report` | `map[string]any` | `json:"report"` |
| `IdempotencyKey` | `string` | `json:"-"` |

### ReconciliationResult

`services/ledger/internal/ledger/types.go:729`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ID` | `string` | `json:"id"` |
| `Status` | `string` | `json:"status"` |
| `Report` | `map[string]any` | `json:"report"` |
| `BlockNewWorkspaces` | `bool` | `json:"blockNewWorkspaces"` |
| `Reason` | `string` | `json:"reason"` |
| `CreatedAt` | `time.Time` | `json:"createdAt"` |
| `Replayed` | `bool` | `json:"replayed"` |

### PrivacyRedactionEvidence

`services/ledger/internal/ledger/types.go:80`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `AppliedAt` | `time.Time` | `json:"appliedAt"` |
| `Reason` | `string` | `json:"reason"` |
| `Eligible` | `bool` | `json:"eligible"` |

### ReceiptRetentionInput

`services/ledger/internal/ledger/types.go:86`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ReceiptID` | `string` | `json:"-"` |
| `RetainUntil` | `time.Time` | `json:"retainUntil,omitempty"` |
| `LegalHold` | `bool` | `json:"legalHold"` |
| `IdempotencyKey` | `string` | `json:"-"` |

### ReceiptPrivacyDeleteInput

`services/ledger/internal/ledger/types.go:93`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ReceiptID` | `string` | `json:"-"` |
| `Reason` | `string` | `json:"reason"` |
| `IdempotencyKey` | `string` | `json:"-"` |

### ReceiptRetentionResult

`services/ledger/internal/ledger/types.go:99`


| Go字段 | 类型 | JSON/其他tag |
| --- | --- | --- |
| `ReceiptID` | `string` | `json:"receiptId"` |
| `Retention` | `ReceiptRetention` | `json:"retention"` |
| `Replayed` | `bool` | `json:"replayed"` |
