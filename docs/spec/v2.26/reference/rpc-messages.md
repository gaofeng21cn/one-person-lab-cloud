# v2.26 RPC 字段目录（派生参考）

> 来源当前checkout的 `contracts/internal.proto`，通过 protoc descriptor 提取，不是新契约；修改请回到原协议。业务必填/校验不由proto3标量零值自动保证。

172 个 RPC，391 个顶层消息。
## RPC 方法

| RPC | 请求 | 响应 |
| --- | --- | --- |
| `TenantProductService.GetLoginContext` | [GetLoginContextRpcRequest](rpc-messages.md#getlogincontextrpcrequest) | [LoginContext](rpc-messages.md#logincontext) |
| `TenantProductService.Login` | [LoginRpcRequest](rpc-messages.md#loginrpcrequest) | [Session](rpc-messages.md#session) |
| `TenantProductService.GetSession` | [GetSessionRpcRequest](rpc-messages.md#getsessionrpcrequest) | [Session](rpc-messages.md#session) |
| `TenantProductService.Logout` | [LogoutRpcRequest](rpc-messages.md#logoutrpcrequest) | [Empty](rpc-messages.md#empty) |
| `TenantProductService.GetTenant` | [GetTenantRpcRequest](rpc-messages.md#gettenantrpcrequest) | [Tenant](rpc-messages.md#tenant) |
| `TenantProductService.ListMembers` | [ListMembersRpcRequest](rpc-messages.md#listmembersrpcrequest) | [MemberPage](rpc-messages.md#memberpage) |
| `TenantProductService.ListInvitations` | [ListInvitationsRpcRequest](rpc-messages.md#listinvitationsrpcrequest) | [InvitationPage](rpc-messages.md#invitationpage) |
| `TenantProductService.InviteMember` | [InviteMemberRpcRequest](rpc-messages.md#invitememberrpcrequest) | [Invitation](rpc-messages.md#invitation) |
| `TenantProductService.AcceptInvitation` | [AcceptInvitationRpcRequest](rpc-messages.md#acceptinvitationrpcrequest) | [Member](rpc-messages.md#member) |
| `TenantProductService.RevokeInvitation` | [RevokeInvitationRpcRequest](rpc-messages.md#revokeinvitationrpcrequest) | [Invitation](rpc-messages.md#invitation) |
| `TenantProductService.UpdateMemberRole` | [UpdateMemberRoleRpcRequest](rpc-messages.md#updatememberrolerpcrequest) | [Member](rpc-messages.md#member) |
| `TenantProductService.RemoveMember` | [RemoveMemberRpcRequest](rpc-messages.md#removememberrpcrequest) | [Empty](rpc-messages.md#empty) |
| `TenantProductService.ListTenants` | [ListTenantsRpcRequest](rpc-messages.md#listtenantsrpcrequest) | [TenantPage](rpc-messages.md#tenantpage) |
| `TenantProductService.CreateTenant` | [CreateTenantRpcRequest](rpc-messages.md#createtenantrpcrequest) | [Operation](rpc-messages.md#operation) |
| `TenantProductService.GetAdminTenant` | [GetAdminTenantRpcRequest](rpc-messages.md#getadmintenantrpcrequest) | [Tenant](rpc-messages.md#tenant) |
| `TenantProductService.DeleteTenant` | [DeleteTenantRpcRequest](rpc-messages.md#deletetenantrpcrequest) | [Operation](rpc-messages.md#operation) |
| `TenantProductService.BindTenantWallet` | [BindTenantWalletRpcRequest](rpc-messages.md#bindtenantwalletrpcrequest) | [Operation](rpc-messages.md#operation) |
| `TenantProductService.SuspendTenant` | [SuspendTenantRpcRequest](rpc-messages.md#suspendtenantrpcrequest) | [Operation](rpc-messages.md#operation) |
| `TenantProductService.RestoreTenant` | [RestoreTenantRpcRequest](rpc-messages.md#restoretenantrpcrequest) | [Operation](rpc-messages.md#operation) |
| `TenantProductService.GetTenantAssetCustody` | [GetTenantAssetCustodyRpcRequest](rpc-messages.md#gettenantassetcustodyrpcrequest) | [AssetCustody](rpc-messages.md#assetcustody) |
| `TenantProductService.ListAuditEvents` | [ListAuditEventsRpcRequest](rpc-messages.md#listauditeventsrpcrequest) | [AuditEventPage](rpc-messages.md#auditeventpage) |
| `TenantProductService.ReenableTenant` | [ReenableTenantRpcRequest](rpc-messages.md#reenabletenantrpcrequest) | [Operation](rpc-messages.md#operation) |
| `TenantProductService.GetTenantLifecycleOperation` | [GetTenantLifecycleOperationRpcRequest](rpc-messages.md#gettenantlifecycleoperationrpcrequest) | [TenantLifecycleProgress](rpc-messages.md#tenantlifecycleprogress) |
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
| `BuildProductService.CreateBuild` | [CreateBuildRpcRequest](rpc-messages.md#createbuildrpcrequest) | [BuildJob](rpc-messages.md#buildjob) |
| `BuildProductService.ListBuilds` | [ListBuildsRpcRequest](rpc-messages.md#listbuildsrpcrequest) | [BuildJobPage](rpc-messages.md#buildjobpage) |
| `BuildProductService.GetBuild` | [GetBuildRpcRequest](rpc-messages.md#getbuildrpcrequest) | [BuildJob](rpc-messages.md#buildjob) |
| `BuildProductService.ListBuildLogs` | [ListBuildLogsRpcRequest](rpc-messages.md#listbuildlogsrpcrequest) | [BuildLogPage](rpc-messages.md#buildlogpage) |
| `BuildProductService.RetryBuild` | [RetryBuildRpcRequest](rpc-messages.md#retrybuildrpcrequest) | [BuildJob](rpc-messages.md#buildjob) |
| `ResourceCatalogProductService.CreateQuote` | [CreateQuoteRpcRequest](rpc-messages.md#createquoterpcrequest) | [Quote](rpc-messages.md#quote) |
| `ResourceCatalogProductService.GetQuote` | [GetQuoteRpcRequest](rpc-messages.md#getquoterpcrequest) | [Quote](rpc-messages.md#quote) |
| `ResourceCatalogProductService.ListComputePlans` | [ListComputePlansRpcRequest](rpc-messages.md#listcomputeplansrpcrequest) | [ComputePlanPage](rpc-messages.md#computeplanpage) |
| `ResourceCatalogProductService.ListStoragePlans` | [ListStoragePlansRpcRequest](rpc-messages.md#liststorageplansrpcrequest) | [StoragePlanPage](rpc-messages.md#storageplanpage) |
| `ResourceCatalogProductService.CreateComputePlan` | [CreateComputePlanRpcRequest](rpc-messages.md#createcomputeplanrpcrequest) | [ComputePlan](rpc-messages.md#computeplan) |
| `ResourceCatalogProductService.SetComputePlanAvailability` | [SetComputePlanAvailabilityRpcRequest](rpc-messages.md#setcomputeplanavailabilityrpcrequest) | [ComputePlan](rpc-messages.md#computeplan) |
| `ResourceCatalogProductService.CreateStoragePlan` | [CreateStoragePlanRpcRequest](rpc-messages.md#createstorageplanrpcrequest) | [StoragePlan](rpc-messages.md#storageplan) |
| `ResourceCatalogProductService.SetStoragePlanAvailability` | [SetStoragePlanAvailabilityRpcRequest](rpc-messages.md#setstorageplanavailabilityrpcrequest) | [StoragePlan](rpc-messages.md#storageplan) |
| `ResourceCatalogProductService.ListPricePolicyVersions` | [ListPricePolicyVersionsRpcRequest](rpc-messages.md#listpricepolicyversionsrpcrequest) | [PricePolicyVersionPage](rpc-messages.md#pricepolicyversionpage) |
| `ResourceCatalogProductService.CreatePricePolicyVersion` | [CreatePricePolicyVersionRpcRequest](rpc-messages.md#createpricepolicyversionrpcrequest) | [PricePolicyVersion](rpc-messages.md#pricepolicyversion) |
| `ResourceCatalogProductService.ListRefundPolicyVersions` | [ListRefundPolicyVersionsRpcRequest](rpc-messages.md#listrefundpolicyversionsrpcrequest) | [RefundPolicyVersionPage](rpc-messages.md#refundpolicyversionpage) |
| `ResourceCatalogProductService.CreateRefundPolicyVersion` | [CreateRefundPolicyVersionRpcRequest](rpc-messages.md#createrefundpolicyversionrpcrequest) | [RefundPolicyVersion](rpc-messages.md#refundpolicyversion) |
| `ResourceCatalogProductService.ListRetentionPolicyVersions` | [ListRetentionPolicyVersionsRpcRequest](rpc-messages.md#listretentionpolicyversionsrpcrequest) | [RetentionPolicyVersionPage](rpc-messages.md#retentionpolicyversionpage) |
| `ResourceCatalogProductService.CreateRetentionPolicyVersion` | [CreateRetentionPolicyVersionRpcRequest](rpc-messages.md#createretentionpolicyversionrpcrequest) | [RetentionPolicyVersion](rpc-messages.md#retentionpolicyversion) |
| `WorkspaceProductService.CreateWorkspace` | [CreateWorkspaceRpcRequest](rpc-messages.md#createworkspacerpcrequest) | [Operation](rpc-messages.md#operation) |
| `WorkspaceProductService.ListWorkspaces` | [ListWorkspacesRpcRequest](rpc-messages.md#listworkspacesrpcrequest) | [WorkspacePage](rpc-messages.md#workspacepage) |
| `WorkspaceProductService.GetWorkspace` | [GetWorkspaceRpcRequest](rpc-messages.md#getworkspacerpcrequest) | [Workspace](rpc-messages.md#workspace) |
| `WorkspaceProductService.DeleteWorkspace` | [DeleteWorkspaceRpcRequest](rpc-messages.md#deleteworkspacerpcrequest) | [Operation](rpc-messages.md#operation) |
| `WorkspaceProductService.GetWorkspaceAccess` | [GetWorkspaceAccessRpcRequest](rpc-messages.md#getworkspaceaccessrpcrequest) | [WorkspaceAccess](rpc-messages.md#workspaceaccess) |
| `WorkspaceProductService.GetWorkspaceModels` | [GetWorkspaceModelsRpcRequest](rpc-messages.md#getworkspacemodelsrpcrequest) | [ModelConfiguration](rpc-messages.md#modelconfiguration) |
| `WorkspaceProductService.UpdateWorkspaceModels` | [UpdateWorkspaceModelsRpcRequest](rpc-messages.md#updateworkspacemodelsrpcrequest) | [Operation](rpc-messages.md#operation) |
| `WorkspaceProductService.ListDeployments` | [ListDeploymentsRpcRequest](rpc-messages.md#listdeploymentsrpcrequest) | [DeploymentPage](rpc-messages.md#deploymentpage) |
| `WorkspaceProductService.GetDeployment` | [GetDeploymentRpcRequest](rpc-messages.md#getdeploymentrpcrequest) | [Deployment](rpc-messages.md#deployment) |
| `WorkspaceProductService.UpdateWorkspaceVersion` | [UpdateWorkspaceVersionRpcRequest](rpc-messages.md#updateworkspaceversionrpcrequest) | [Operation](rpc-messages.md#operation) |
| `WorkspaceProductService.RollbackWorkspace` | [RollbackWorkspaceRpcRequest](rpc-messages.md#rollbackworkspacerpcrequest) | [Operation](rpc-messages.md#operation) |
| `WorkspaceProductService.ResizeWorkspace` | [ResizeWorkspaceRpcRequest](rpc-messages.md#resizeworkspacerpcrequest) | [Operation](rpc-messages.md#operation) |
| `WorkspaceProductService.RenewWorkspace` | [RenewWorkspaceRpcRequest](rpc-messages.md#renewworkspacerpcrequest) | [Operation](rpc-messages.md#operation) |
| `WorkspaceProductService.GetSubscription` | [GetSubscriptionRpcRequest](rpc-messages.md#getsubscriptionrpcrequest) | [Subscription](rpc-messages.md#subscription) |
| `WorkspaceProductService.GetWorkspaceDeletion` | [GetWorkspaceDeletionRpcRequest](rpc-messages.md#getworkspacedeletionrpcrequest) | [WorkspaceDeletion](rpc-messages.md#workspacedeletion) |
| `WorkspaceProductService.GetOperation` | [GetOperationRpcRequest](rpc-messages.md#getoperationrpcrequest) | [Operation](rpc-messages.md#operation) |
| `WorkspaceProductService.ListAdminOperations` | [ListAdminOperationsRpcRequest](rpc-messages.md#listadminoperationsrpcrequest) | [AdminOperationPage](rpc-messages.md#adminoperationpage) |
| `WorkspaceProductService.ReconcileOperation` | [ReconcileOperationRpcRequest](rpc-messages.md#reconcileoperationrpcrequest) | [Operation](rpc-messages.md#operation) |
| `WorkspaceProductService.AdoptWorkspace` | [AdoptWorkspaceRpcRequest](rpc-messages.md#adoptworkspacerpcrequest) | [Operation](rpc-messages.md#operation) |
| `WorkspaceProductService.UpdateRenewalSettings` | [UpdateRenewalSettingsRpcRequest](rpc-messages.md#updaterenewalsettingsrpcrequest) | [Operation](rpc-messages.md#operation) |
| `WorkspaceProductService.RevealWorkspaceApplicationCredentials` | [RevealWorkspaceApplicationCredentialsRpcRequest](rpc-messages.md#revealworkspaceapplicationcredentialsrpcrequest) | [WorkspaceApplicationCredentials](rpc-messages.md#workspaceapplicationcredentials) |
| `WorkspaceProductService.ListPlanChanges` | [ListPlanChangesRpcRequest](rpc-messages.md#listplanchangesrpcrequest) | [PlanChangePage](rpc-messages.md#planchangepage) |
| `WorkspaceProductService.GetPlanChange` | [GetPlanChangeRpcRequest](rpc-messages.md#getplanchangerpcrequest) | [PlanChange](rpc-messages.md#planchange) |
| `WorkspaceProductService.CancelPlanChange` | [CancelPlanChangeRpcRequest](rpc-messages.md#cancelplanchangerpcrequest) | [Operation](rpc-messages.md#operation) |
| `GatewayProductService.ListWorkspaceTransactions` | [ListWorkspaceTransactionsRpcRequest](rpc-messages.md#listworkspacetransactionsrpcrequest) | [WalletOperationPage](rpc-messages.md#walletoperationpage) |
| `GatewayProductService.GetWallet` | [GetWalletRpcRequest](rpc-messages.md#getwalletrpcrequest) | [Wallet](rpc-messages.md#wallet) |
| `GatewayProductService.ListUsage` | [ListUsageRpcRequest](rpc-messages.md#listusagerpcrequest) | [UsagePage](rpc-messages.md#usagepage) |
| `GatewayProductService.ListGatewayKeys` | [ListGatewayKeysRpcRequest](rpc-messages.md#listgatewaykeysrpcrequest) | [GatewayKeyPage](rpc-messages.md#gatewaykeypage) |
| `GatewayProductService.CreateGatewayKey` | [CreateGatewayKeyRpcRequest](rpc-messages.md#creategatewaykeyrpcrequest) | [GatewayKeySecret](rpc-messages.md#gatewaykeysecret) |
| `GatewayProductService.RevealGatewayKey` | [RevealGatewayKeyRpcRequest](rpc-messages.md#revealgatewaykeyrpcrequest) | [GatewayKeySecret](rpc-messages.md#gatewaykeysecret) |
| `GatewayProductService.RevokeGatewayKey` | [RevokeGatewayKeyRpcRequest](rpc-messages.md#revokegatewaykeyrpcrequest) | [Operation](rpc-messages.md#operation) |
| `GatewayProductService.ListRechargeRecords` | [ListRechargeRecordsRpcRequest](rpc-messages.md#listrechargerecordsrpcrequest) | [WalletOperationPage](rpc-messages.md#walletoperationpage) |
| `GatewayProductService.ListModels` | [ListModelsRpcRequest](rpc-messages.md#listmodelsrpcrequest) | [ModelPage](rpc-messages.md#modelpage) |
| `LedgerProductService.ListReceipts` | [ListReceiptsRpcRequest](rpc-messages.md#listreceiptsrpcrequest) | [ReceiptPage](rpc-messages.md#receiptpage) |
| `LedgerProductService.GetReceipt` | [GetReceiptRpcRequest](rpc-messages.md#getreceiptrpcrequest) | [Receipt](rpc-messages.md#receipt) |
| `LedgerProductService.ListQualifications` | [ListQualificationsRpcRequest](rpc-messages.md#listqualificationsrpcrequest) | [QualificationPage](rpc-messages.md#qualificationpage) |
| `ClaimUsageReadback.ReadClaimUsage` | [ReadClaimUsageRequest](rpc-messages.md#readclaimusagerequest) | [ClaimUsageEvidence](rpc-messages.md#claimusageevidence) |
| `CapabilityCoordination.ResolveBuildInput` | [BuildInputRequest](rpc-messages.md#buildinputrequest) | [BuildInputSnapshot](rpc-messages.md#buildinputsnapshot) |
| `CapabilityCoordination.ResolvePublisherContract` | [ResolvePublisherContractRequest](rpc-messages.md#resolvepublishercontractrequest) | [ResolvedPublisherContract](rpc-messages.md#resolvedpublishercontract) |
| `CapabilityCoordination.AcquireReference` | [ReferenceClaimRequest](rpc-messages.md#referenceclaimrequest) | [ReferenceClaim](rpc-messages.md#referenceclaim) |
| `CapabilityCoordination.BindReference` | [BindReferenceRequest](rpc-messages.md#bindreferencerequest) | [ReferenceClaim](rpc-messages.md#referenceclaim) |
| `CapabilityCoordination.ReleaseReference` | [ReleaseReferenceRequest](rpc-messages.md#releasereferencerequest) | [ReferenceClaim](rpc-messages.md#referenceclaim) |
| `BuildCoordination.ReadArtifact` | [ReadBuildArtifactRequest](rpc-messages.md#readbuildartifactrequest) | [BuildArtifactReadback](rpc-messages.md#buildartifactreadback) |
| `OwnerOperations.Read` | [OwnerOperationRequest](rpc-messages.md#owneroperationrequest) | [Operation](rpc-messages.md#operation) |
| `OwnerOperations.Reconcile` | [ReconcileOperationRpcRequest](rpc-messages.md#reconcileoperationrpcrequest) | [Operation](rpc-messages.md#operation) |
| `OwnerCommitReadback.ReadOwnerCommit` | [ReadOwnerCommitRequest](rpc-messages.md#readownercommitrequest) | [OwnerCommitEvidence](rpc-messages.md#ownercommitevidence) |
| `WorkspaceAuthorizationReadback.ReadRenewalConsent` | [ReadRenewalConsentRequest](rpc-messages.md#readrenewalconsentrequest) | [RenewalConsentReadback](rpc-messages.md#renewalconsentreadback) |
| `CloudIdentityAuthorization.AuthorizeAction` | [AuthorizationRequest](rpc-messages.md#authorizationrequest) | [AuthorizationDecision](rpc-messages.md#authorizationdecision) |
| `CloudIdentityAuthorization.GetAuthorizationContext` | [GetAuthorizationContextRequest](rpc-messages.md#getauthorizationcontextrequest) | [AuthorizationDecision](rpc-messages.md#authorizationdecision) |
| `CloudIdentityAuthorization.IssueAcceptedOperationGrant` | [AcceptedOperationGrantRequest](rpc-messages.md#acceptedoperationgrantrequest) | [AcceptedOperationGrant](rpc-messages.md#acceptedoperationgrant) |
| `CatalogCoordination.AcceptQuote` | [AcceptQuoteRequest](rpc-messages.md#acceptquoterequest) | [QuoteAcceptance](rpc-messages.md#quoteacceptance) |
| `WorkspaceAdmission.CheckAdmission` | [AdmissionRequest](rpc-messages.md#admissionrequest) | [AdmissionResult](rpc-messages.md#admissionresult) |
| `GatewayCoordination.BindWallet` | [WalletBindingCommand](rpc-messages.md#walletbindingcommand) | [WalletBindingReadback](rpc-messages.md#walletbindingreadback) |
| `GatewayCoordination.Debit` | [WalletDebitCommand](rpc-messages.md#walletdebitcommand) | [WalletOperation](rpc-messages.md#walletoperation) |
| `GatewayCoordination.Refund` | [WalletRefundCommand](rpc-messages.md#walletrefundcommand) | [WalletOperation](rpc-messages.md#walletoperation) |
| `GatewayCoordination.ReadWalletAction` | [WalletReadbackRequest](rpc-messages.md#walletreadbackrequest) | [WalletOperation](rpc-messages.md#walletoperation) |
| `GatewayCoordination.CreateManagedKey` | [ManagedKeyCommand](rpc-messages.md#managedkeycommand) | [ManagedKeyBinding](rpc-messages.md#managedkeybinding) |
| `GatewayCoordination.RevokeManagedKey` | [ManagedKeyRevoke](rpc-messages.md#managedkeyrevoke) | [Operation](rpc-messages.md#operation) |
| `FabricCoordination.AdmitResources` | [ResourceAdmissionRequest](rpc-messages.md#resourceadmissionrequest) | [AdmissionResult](rpc-messages.md#admissionresult) |
| `FabricCoordination.EnsureResources` | [EnsureResourcesCommand](rpc-messages.md#ensureresourcescommand) | [Operation](rpc-messages.md#operation) |
| `FabricCoordination.ResizeResources` | [ResizeResourcesCommand](rpc-messages.md#resizeresourcescommand) | [Operation](rpc-messages.md#operation) |
| `FabricCoordination.RenewResources` | [RenewResourcesCommand](rpc-messages.md#renewresourcescommand) | [Operation](rpc-messages.md#operation) |
| `FabricCoordination.SuspendResources` | [MutateResourcesCommand](rpc-messages.md#mutateresourcescommand) | [Operation](rpc-messages.md#operation) |
| `FabricCoordination.ResumeResources` | [MutateResourcesCommand](rpc-messages.md#mutateresourcescommand) | [Operation](rpc-messages.md#operation) |
| `FabricCoordination.DeleteResources` | [MutateResourcesCommand](rpc-messages.md#mutateresourcescommand) | [Operation](rpc-messages.md#operation) |
| `FabricCoordination.ReadResources` | [ResourceReadbackRequest](rpc-messages.md#resourcereadbackrequest) | [ResourceReadback](rpc-messages.md#resourcereadback) |
| `FabricCoordination.BindSecret` | [SecretBindingCommand](rpc-messages.md#secretbindingcommand) | [SecretBindingReadback](rpc-messages.md#secretbindingreadback) |
| `RuntimeCoordination.Reserve` | [RuntimeReservationCommand](rpc-messages.md#runtimereservationcommand) | [RuntimeReservation](rpc-messages.md#runtimereservation) |
| `RuntimeCoordination.Deploy` | [RuntimeDeployCommand](rpc-messages.md#runtimedeploycommand) | [RuntimeReadback](rpc-messages.md#runtimereadback) |
| `RuntimeCoordination.ReloadModels` | [RuntimeReloadCommand](rpc-messages.md#runtimereloadcommand) | [Operation](rpc-messages.md#operation) |
| `RuntimeCoordination.ReadRuntime` | [RuntimeReadbackRequest](rpc-messages.md#runtimereadbackrequest) | [RuntimeReadback](rpc-messages.md#runtimereadback) |
| `RuntimeCoordination.Retire` | [RuntimeStopCommand](rpc-messages.md#runtimestopcommand) | [Operation](rpc-messages.md#operation) |
| `FabricRuntimeExecution.ReadApplicationCredentials` | [ReadApplicationCredentialsRequest](rpc-messages.md#readapplicationcredentialsrequest) | [WorkspaceApplicationCredentials](rpc-messages.md#workspaceapplicationcredentials) |
| `FabricRuntimeExecution.StartRuntime` | [RuntimeDeployCommand](rpc-messages.md#runtimedeploycommand) | [RuntimeReadback](rpc-messages.md#runtimereadback) |
| `FabricRuntimeExecution.StopRuntime` | [RuntimeStopCommand](rpc-messages.md#runtimestopcommand) | [Operation](rpc-messages.md#operation) |
| `FabricRuntimeExecution.ReloadRuntime` | [RuntimeReloadCommand](rpc-messages.md#runtimereloadcommand) | [Operation](rpc-messages.md#operation) |
| `FabricRuntimeExecution.ObserveRuntime` | [RuntimeReadbackRequest](rpc-messages.md#runtimereadbackrequest) | [RuntimeReadback](rpc-messages.md#runtimereadback) |
| `FabricRouteExecution.FenceRouteEpoch` | [FenceRouteEpochCommand](rpc-messages.md#fencerouteepochcommand) | [RouteReadback](rpc-messages.md#routereadback) |
| `FabricRouteExecution.ActivateRoute` | [RouteActivateCommand](rpc-messages.md#routeactivatecommand) | [RouteReadback](rpc-messages.md#routereadback) |
| `FabricRouteExecution.ObserveRoute` | [RouteObserveRequest](rpc-messages.md#routeobserverequest) | [RouteReadback](rpc-messages.md#routereadback) |
| `FabricRouteExecution.RollbackRoute` | [RouteRollbackCommand](rpc-messages.md#routerollbackcommand) | [RouteReadback](rpc-messages.md#routereadback) |
| `TenantWorkspaceCoordination.SuspendTenantWorkspaces` | [TenantWorkspaceLifecycleCommand](rpc-messages.md#tenantworkspacelifecyclecommand) | [TenantWorkspaceLifecycleReadback](rpc-messages.md#tenantworkspacelifecyclereadback) |
| `TenantWorkspaceCoordination.DeleteTenantWorkspaces` | [TenantWorkspaceLifecycleCommand](rpc-messages.md#tenantworkspacelifecyclecommand) | [TenantWorkspaceLifecycleReadback](rpc-messages.md#tenantworkspacelifecyclereadback) |
| `TenantWorkspaceCoordination.ResumeTenantWorkspaces` | [ResumeTenantWorkspacesRequest](rpc-messages.md#resumetenantworkspacesrequest) | [TenantWorkspaceLifecycleReadback](rpc-messages.md#tenantworkspacelifecyclereadback) |
| `LedgerCoordination.AppendReceipt` | [AppendReceiptRequest](rpc-messages.md#appendreceiptrequest) | [Receipt](rpc-messages.md#receipt) |
| `LedgerCoordination.ReadReceiptByReference` | [GetReceiptByReferenceRequest](rpc-messages.md#getreceiptbyreferencerequest) | [Receipt](rpc-messages.md#receipt) |
| `WorkspacePlanChangeReadback.ReadSubscriptionPlanState` | [ReadSubscriptionPlanStateRequest](rpc-messages.md#readsubscriptionplanstaterequest) | [SubscriptionPlanState](rpc-messages.md#subscriptionplanstate) |
| `WorkspacePlanChangeReadback.ReadPlanChange` | [ReadPlanChangeRequest](rpc-messages.md#readplanchangerequest) | [PlanChange](rpc-messages.md#planchange) |
| `WorkspacePlanChangeReadback.ReadNextPeriodObligation` | [ReadNextPeriodObligationRequest](rpc-messages.md#readnextperiodobligationrequest) | [NextPeriodObligation](rpc-messages.md#nextperiodobligation) |
| `WorkspacePlanChangeReadback.ReadPlanChangeFailure` | [ReadPlanChangeFailureRequest](rpc-messages.md#readplanchangefailurerequest) | [PlanChangeEvidence](rpc-messages.md#planchangeevidence) |
| `FabricPlanTransitionReadback.ReadApprovedPlanTransition` | [PlanTransitionRequest](rpc-messages.md#plantransitionrequest) | [ApprovedPlanTransition](rpc-messages.md#approvedplantransition) |
| `FabricPlanTransitionReadback.ReadExecutionPlan` | [ReadProviderExecutionPlanRequest](rpc-messages.md#readproviderexecutionplanrequest) | [ProviderPlanChangeExecutionPlan](rpc-messages.md#providerplanchangeexecutionplan) |
| `GatewayPlanChangeSettlement.DebitSupplement` | [PlanChangeSupplementChargeCommand](rpc-messages.md#planchangesupplementchargecommand) | [WalletOperation](rpc-messages.md#walletoperation) |
| `GatewayPlanChangeSettlement.DebitScheduledPeriod` | [ScheduledPeriodChargeCommand](rpc-messages.md#scheduledperiodchargecommand) | [WalletOperation](rpc-messages.md#walletoperation) |
| `GatewayPlanChangeSettlement.RefundFailure` | [PlanChangeFailureRefundCommand](rpc-messages.md#planchangefailurerefundcommand) | [WalletOperation](rpc-messages.md#walletoperation) |
| `GatewayPlanChangeSettlement.RefundSupplementOnDeletion` | [SupplementDeletionRefundCommand](rpc-messages.md#supplementdeletionrefundcommand) | [WalletOperation](rpc-messages.md#walletoperation) |
| `RuntimePlanChangeControl.RestoreAfterResourceChange` | [RestorePlanChangeRuntimeCommand](rpc-messages.md#restoreplanchangeruntimecommand) | [PlanChangeRuntimeReadback](rpc-messages.md#planchangeruntimereadback) |
| `LedgerPlanChangeEvidence.AppendPlanChangeReceipt` | [AppendPlanChangeReceiptRequest](rpc-messages.md#appendplanchangereceiptrequest) | [Receipt](rpc-messages.md#receipt) |
| `LedgerPlanChangeEvidence.AppendPlanChangeRefundReceipt` | [AppendPlanChangeRefundReceiptRequest](rpc-messages.md#appendplanchangerefundreceiptrequest) | [Receipt](rpc-messages.md#receipt) |
| `DomainInbox.Deliver` | [DeliverEventRequest](rpc-messages.md#delivereventrequest) | [InboxAck](rpc-messages.md#inboxack) |

## CallContext

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `request_id #1` | `string` | 标量/message；语义必填见规格 | `requestId` |
| `idempotency_key #2` | `string` | 标量/message；语义必填见规格 | `idempotencyKey` |
| `authorization_context_id #3` | `string` | 标量/message；语义必填见规格 | `authorizationContextId` |
| `actor_id #4` | `string` | 标量/message；语义必填见规格 | `actorId` |
| `session_id #5` | `string` | proto3 optional | `sessionId` |
| `scope #6` | [AuthorizationScope](rpc-messages.md#authorizationscope) | 标量/message；语义必填见规格 | `scope` |
| `accepted_operation_grant_id #7` | `string` | proto3 optional | `acceptedOperationGrantId` |
| `deadline_at #8` | `Timestamp` | 标量/message；语义必填见规格 | `deadlineAt` |

## FieldError

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `field #1` | `string` | 标量/message；语义必填见规格 | `field` |
| `code #2` | `ErrorCodeEnum` | 标量/message；语义必填见规格 | `code` |
| `message #3` | `string` | 标量/message；语义必填见规格 | `message` |

## Error

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `code #1` | `ErrorCodeEnum` | 标量/message；语义必填见规格 | `code` |
| `message #2` | `string` | 标量/message；语义必填见规格 | `message` |
| `request_id #3` | `string` | 标量/message；语义必填见规格 | `requestId` |
| `field_errors #4` | [FieldError](rpc-messages.md#fielderror) | repeated | `fieldErrors` |

## Operation

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `operation_id #1` | `string` | 标量/message；语义必填见规格 | `operationId` |
| `owner #2` | `OperationOwnerEnum` | 标量/message；语义必填见规格 | `owner` |
| `kind #3` | `OperationKindEnum` | 标量/message；语义必填见规格 | `kind` |
| `resource_id #4` | `string` | 标量/message；语义必填见规格 | `resourceId` |
| `status #5` | `OperationStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `stage #6` | `OperationStageEnum` | 标量/message；语义必填见规格 | `stage` |
| `observation_result #7` | `OperationObservationResultEnum` | proto3 optional | `observationResult` |
| `error_code #8` | `ErrorCodeEnum` | proto3 optional | `errorCode` |
| `request_id #9` | `string` | 标量/message；语义必填见规格 | `requestId` |
| `created_at #10` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |
| `updated_at #11` | `Timestamp` | 标量/message；语义必填见规格 | `updatedAt` |
| `poll_after_seconds #12` | `int32` | proto3 optional | `pollAfterSeconds` |

## LoginContext

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `csrf_token #1` | `string` | 标量/message；语义必填见规格 | `csrfToken` |
| `expires_at #2` | `Timestamp` | 标量/message；语义必填见规格 | `expiresAt` |

## LoginRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `username #1` | `string` | 标量/message；语义必填见规格 | `username` |
| `password #2` | `string` | 标量/message；语义必填见规格 | `password` |

## Session

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `actor_id #1` | `string` | 标量/message；语义必填见规格 | `actorId` |
| `display_name #2` | `string` | 标量/message；语义必填见规格 | `displayName` |
| `tenant_id #3` | `string` | proto3 optional | `tenantId` |
| `tenant_name #4` | `string` | proto3 optional | `tenantName` |
| `role #5` | `TenantRoleEnum` | proto3 optional | `role` |
| `permissions #6` | `AuthorizationActionEnum` | repeated | `permissions` |
| `csrf_token #7` | `string` | 标量/message；语义必填见规格 | `csrfToken` |
| `expires_at #8` | `Timestamp` | 标量/message；语义必填见规格 | `expiresAt` |

## Tenant

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `name #2` | `string` | 标量/message；语义必填见规格 | `name` |
| `status #3` | `TenantStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `billing_sub2api_user_id #4` | `string` | proto3 optional | `billingSub2apiUserId` |
| `restore_until #5` | `Timestamp` | 标量/message；语义必填见规格 | `restoreUntil` |
| `asset_custody_status #6` | `TenantAssetCustodyStatusEnum` | 标量/message；语义必填见规格 | `assetCustodyStatus` |
| `created_at #7` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |
| `updated_at #8` | `Timestamp` | 标量/message；语义必填见规格 | `updatedAt` |

## CreateTenantRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `name #1` | `string` | 标量/message；语义必填见规格 | `name` |
| `billing_sub2api_user_id #2` | `string` | 标量/message；语义必填见规格 | `billingSub2apiUserId` |
| `owner_gateway_subject_id #3` | `string` | 标量/message；语义必填见规格 | `ownerGatewaySubjectId` |

## BindTenantWalletRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `billing_sub2api_user_id #1` | `string` | 标量/message；语义必填见规格 | `billingSub2apiUserId` |
| `expected_binding_version #2` | `int64` | 标量/message；语义必填见规格 | `expectedBindingVersion` |

## Member

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `actor_id #2` | `string` | 标量/message；语义必填见规格 | `actorId` |
| `display_name #3` | `string` | 标量/message；语义必填见规格 | `displayName` |
| `role #4` | `TenantRoleEnum` | 标量/message；语义必填见规格 | `role` |
| `status #5` | `MemberStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `created_at #6` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |

## Invitation

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `invitee_gateway_subject_id #2` | `string` | 标量/message；语义必填见规格 | `inviteeGatewaySubjectId` |
| `role #3` | `InvitationRoleEnum` | 标量/message；语义必填见规格 | `role` |
| `status #4` | `InvitationStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `expires_at #5` | `Timestamp` | 标量/message；语义必填见规格 | `expiresAt` |
| `created_at #6` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |

## InviteMemberRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `invitee_gateway_subject_id #1` | `string` | 标量/message；语义必填见规格 | `inviteeGatewaySubjectId` |
| `role #2` | `InviteMemberRequestRoleEnum` | 标量/message；语义必填见规格 | `role` |

## UpdateMemberRoleRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `role #1` | `TenantRoleEnum` | 标量/message；语义必填见规格 | `role` |

## TenantActionRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `reason #1` | `string` | 标量/message；语义必填见规格 | `reason` |

## DeleteTenantRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `confirmation_name #1` | `string` | 标量/message；语义必填见规格 | `confirmationName` |
| `reason #2` | `string` | 标量/message；语义必填见规格 | `reason` |

## AssetCustody

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `tenant_id #1` | `string` | 标量/message；语义必填见规格 | `tenantId` |
| `status #2` | `AssetCustodyStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `package_count #3` | `int64` | 标量/message；语义必填见规格 | `packageCount` |
| `build_count #4` | `int64` | 标量/message；语义必填见规格 | `buildCount` |
| `restore_until #5` | `Timestamp` | 标量/message；语义必填见规格 | `restoreUntil` |
| `workspace_deletion_operation_ids #6` | `string` | repeated | `workspaceDeletionOperationIds` |

## Namespace

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `name #2` | `string` | 标量/message；语义必填见规格 | `name` |
| `is_default #3` | `bool` | 标量/message；语义必填见规格 | `isDefault` |
| `status #4` | `NamespaceStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `created_at #5` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |

## NamespaceWriteRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `name #1` | `string` | 标量/message；语义必填见规格 | `name` |

## Package

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `namespace_id #2` | `string` | 标量/message；语义必填见规格 | `namespaceId` |
| `name #3` | `string` | 标量/message；语义必填见规格 | `name` |
| `description #4` | `string` | 标量/message；语义必填见规格 | `description` |
| `visibility #5` | `PackageVisibilityEnum` | 标量/message；语义必填见规格 | `visibility` |
| `status #6` | `PackageStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `latest_ready_version_id #7` | `string` | proto3 optional | `latestReadyVersionId` |
| `created_at #8` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |
| `updated_at #9` | `Timestamp` | 标量/message；语义必填见规格 | `updatedAt` |

## CreatePackageRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `namespace_id #1` | `string` | 标量/message；语义必填见规格 | `namespaceId` |
| `name #2` | `string` | 标量/message；语义必填见规格 | `name` |
| `description #3` | `string` | 标量/message；语义必填见规格 | `description` |

## UpdatePackageRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `namespace_id #1` | `string` | 标量/message；语义必填见规格 | `namespaceId` |
| `name #2` | `string` | 标量/message；语义必填见规格 | `name` |
| `description #3` | `string` | 标量/message；语义必填见规格 | `description` |

## PublishPackageRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `admission_receipt_id #1` | `string` | 标量/message；语义必填见规格 | `admissionReceiptId` |

## PackageVersion

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `package_id #2` | `string` | 标量/message；语义必填见规格 | `packageId` |
| `version_label #3` | `string` | 标量/message；语义必填见规格 | `versionLabel` |
| `status #4` | `PackageVersionStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `sha256 #5` | `string` | 标量/message；语义必填见规格 | `sha256` |
| `size_bytes #6` | `int64` | 标量/message；语义必填见规格 | `sizeBytes` |
| `created_at #7` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |

## CreateUploadRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `version_label #1` | `string` | 标量/message；语义必填见规格 | `versionLabel` |
| `file_name #2` | `string` | 标量/message；语义必填见规格 | `fileName` |
| `size_bytes #3` | `int64` | 标量/message；语义必填见规格 | `sizeBytes` |
| `sha256 #4` | `string` | 标量/message；语义必填见规格 | `sha256` |

## UploadPart

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `part_number #1` | `int32` | 标量/message；语义必填见规格 | `partNumber` |
| `etag #2` | `string` | 标量/message；语义必填见规格 | `etag` |
| `size_bytes #3` | `int64` | 标量/message；语义必填见规格 | `sizeBytes` |
| `sha256 #4` | `string` | 标量/message；语义必填见规格 | `sha256` |

## UploadSession

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `package_version_id #2` | `string` | 标量/message；语义必填见规格 | `packageVersionId` |
| `status #3` | `UploadSessionStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `size_bytes #4` | `int64` | 标量/message；语义必填见规格 | `sizeBytes` |
| `sha256 #5` | `string` | 标量/message；语义必填见规格 | `sha256` |
| `part_size_bytes #6` | `int64` | 标量/message；语义必填见规格 | `partSizeBytes` |
| `completed_parts #7` | [UploadPart](rpc-messages.md#uploadpart) | repeated | `completedParts` |
| `expires_at #8` | `Timestamp` | 标量/message；语义必填见规格 | `expiresAt` |

## CreateUploadPartRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `part_number #1` | `int32` | 标量/message；语义必填见规格 | `partNumber` |
| `size_bytes #2` | `int64` | 标量/message；语义必填见规格 | `sizeBytes` |
| `sha256 #3` | `string` | 标量/message；语义必填见规格 | `sha256` |

## UploadPartAuthorization

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `upload_id #1` | `string` | 标量/message；语义必填见规格 | `uploadId` |
| `part_number #2` | `int32` | 标量/message；语义必填见规格 | `partNumber` |
| `method #3` | `UploadPartAuthorizationMethodEnum` | 标量/message；语义必填见规格 | `method` |
| `url #4` | `string` | 标量/message；语义必填见规格 | `url` |
| `content_type #5` | `string` | 标量/message；语义必填见规格 | `contentType` |
| `required_checksum_header_name #6` | `string` | 标量/message；语义必填见规格 | `requiredChecksumHeaderName` |
| `required_checksum_header_value #7` | `string` | 标量/message；语义必填见规格 | `requiredChecksumHeaderValue` |
| `expires_at #8` | `Timestamp` | 标量/message；语义必填见规格 | `expiresAt` |

## CompleteUploadRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `parts #1` | [UploadPart](rpc-messages.md#uploadpart) | repeated | `parts` |

## ModelRequirement

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `slot #1` | `string` | 标量/message；语义必填见规格 | `slot` |
| `required #2` | `bool` | 标量/message；语义必填见规格 | `required` |
| `capability #3` | `ModelRequirementCapabilityEnum` | 标量/message；语义必填见规格 | `capability` |
| `allowed_model_ids #4` | `string` | repeated | `allowedModelIds` |

## DataCompatibility

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `data_schema_version #1` | `string` | 标量/message；语义必填见规格 | `dataSchemaVersion` |
| `compatible_from_versions #2` | `string` | repeated | `compatibleFromVersions` |
| `rollback_safe #3` | `bool` | 标量/message；语义必填见规格 | `rollbackSafe` |
| `migration_required #4` | `bool` | 标量/message；语义必填见规格 | `migrationRequired` |
| `migration_receipt_id #5` | `string` | proto3 optional | `migrationReceiptId` |

## CapabilityVersion

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `package_id #2` | `string` | proto3 optional | `packageId` |
| `package_version_id #3` | `string` | proto3 optional | `packageVersionId` |
| `build_job_id #4` | `string` | proto3 optional | `buildJobId` |
| `version_label #5` | `string` | 标量/message；语义必填见规格 | `versionLabel` |
| `runtime_version_id #6` | `string` | proto3 optional | `runtimeVersionId` |
| `webui_version_id #7` | `string` | proto3 optional | `webuiVersionId` |
| `artifact_digest #8` | `string` | 标量/message；语义必填见规格 | `artifactDigest` |
| `status #9` | `CapabilityVersionStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `model_requirements #10` | [ModelRequirement](rpc-messages.md#modelrequirement) | repeated | `modelRequirements` |
| `data_compatibility #11` | [DataCompatibility](rpc-messages.md#datacompatibility) | 标量/message；语义必填见规格 | `dataCompatibility` |
| `reference_count #12` | `int64` | 标量/message；语义必填见规格 | `referenceCount` |
| `created_at #13` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |
| `provenance #14` | `CapabilityVersionProvenanceEnum` | 标量/message；语义必填见规格 | `provenance` |
| `legacy_application_revision_id #15` | `string` | proto3 optional | `legacyApplicationRevisionId` |
| `artifact #16` | [ArtifactReference](rpc-messages.md#artifactreference) | 标量/message；语义必填见规格 | `artifact` |
| `deployment_descriptor #17` | [DeploymentDescriptor](rpc-messages.md#deploymentdescriptor) | 标量/message；语义必填见规格 | `deploymentDescriptor` |
| `deployment_descriptor_digest #18` | `string` | 标量/message；语义必填见规格 | `deploymentDescriptorDigest` |
| `deployment_descriptor_object_ref #19` | `string` | 标量/message；语义必填见规格 | `deploymentDescriptorObjectRef` |

## BuildJob

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `operation_id #2` | `string` | 标量/message；语义必填见规格 | `operationId` |
| `package_version_id #3` | `string` | 标量/message；语义必填见规格 | `packageVersionId` |
| `runtime_version_id #4` | `string` | 标量/message；语义必填见规格 | `runtimeVersionId` |
| `webui_version_id #5` | `string` | 标量/message；语义必填见规格 | `webuiVersionId` |
| `status #6` | `BuildJobStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `stage #7` | `string` | 标量/message；语义必填见规格 | `stage` |
| `artifact_digest #8` | `string` | proto3 optional | `artifactDigest` |
| `result_capability_version_id #9` | `string` | proto3 optional | `resultCapabilityVersionId` |
| `retry_of_build_job_id #10` | `string` | proto3 optional | `retryOfBuildJobId` |
| `error_code #11` | `ErrorCodeEnum` | proto3 optional | `errorCode` |
| `created_at #12` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |
| `updated_at #13` | `Timestamp` | 标量/message；语义必填见规格 | `updatedAt` |
| `retry_allowed #14` | `bool` | 标量/message；语义必填见规格 | `retryAllowed` |
| `input_claim_ids #15` | `string` | repeated | `inputClaimIds` |

## CreateBuildRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `package_version_id #1` | `string` | 标量/message；语义必填见规格 | `packageVersionId` |
| `webui_version_id #2` | `string` | 标量/message；语义必填见规格 | `webuiVersionId` |

## BuildLog

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `build_job_id #2` | `string` | 标量/message；语义必填见规格 | `buildJobId` |
| `sequence #3` | `int64` | 标量/message；语义必填见规格 | `sequence` |
| `stage #4` | `string` | 标量/message；语义必填见规格 | `stage` |
| `level #5` | `BuildLogLevelEnum` | 标量/message；语义必填见规格 | `level` |
| `message #6` | `string` | 标量/message；语义必填见规格 | `message` |
| `created_at #7` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |

## RuntimeVersion

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `name #2` | `string` | 标量/message；语义必填见规格 | `name` |
| `version_label #3` | `string` | 标量/message；语义必填见规格 | `versionLabel` |
| `artifact_digest #4` | `string` | 标量/message；语义必填见规格 | `artifactDigest` |
| `status #5` | `RuntimeVersionStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `runtime_abi_version #6` | `string` | 标量/message；语义必填见规格 | `runtimeAbiVersion` |
| `package_format_versions #7` | `string` | repeated | `packageFormatVersions` |
| `default_for_new_builds #8` | `bool` | 标量/message；语义必填见规格 | `defaultForNewBuilds` |
| `admission_receipt_id #9` | `string` | 标量/message；语义必填见规格 | `admissionReceiptId` |
| `created_at #10` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |
| `publisher_namespace_id #11` | `string` | 标量/message；语义必填见规格 | `publisherNamespaceId` |
| `publisher_contract_digest #12` | `string` | 标量/message；语义必填见规格 | `publisherContractDigest` |
| `publisher_contract #13` | [RuntimePublisherContract](rpc-messages.md#runtimepublishercontract) | 标量/message；语义必填见规格 | `publisherContract` |
| `publisher_contract_object_ref #14` | `string` | 标量/message；语义必填见规格 | `publisherContractObjectRef` |

## WebuiVersion

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `name #2` | `string` | 标量/message；语义必填见规格 | `name` |
| `version_label #3` | `string` | 标量/message；语义必填见规格 | `versionLabel` |
| `artifact_digest #4` | `string` | 标量/message；语义必填见规格 | `artifactDigest` |
| `status #5` | `WebuiVersionStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `runtime_abi_versions #6` | `string` | repeated | `runtimeAbiVersions` |
| `ui_protocol_version #7` | `string` | 标量/message；语义必填见规格 | `uiProtocolVersion` |
| `admission_receipt_id #8` | `string` | 标量/message；语义必填见规格 | `admissionReceiptId` |
| `created_at #9` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |
| `publisher_namespace_id #10` | `string` | 标量/message；语义必填见规格 | `publisherNamespaceId` |
| `publisher_contract_digest #11` | `string` | 标量/message；语义必填见规格 | `publisherContractDigest` |
| `publisher_contract #12` | [WebuiPublisherContract](rpc-messages.md#webuipublishercontract) | 标量/message；语义必填见规格 | `publisherContract` |
| `publisher_contract_object_ref #13` | `string` | 标量/message；语义必填见规格 | `publisherContractObjectRef` |

## RegisterRuntimeVersionRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `name #1` | `string` | 标量/message；语义必填见规格 | `name` |
| `version_label #2` | `string` | 标量/message；语义必填见规格 | `versionLabel` |
| `publisher_namespace_id #3` | `string` | 标量/message；语义必填见规格 | `publisherNamespaceId` |
| `publisher_contract #4` | [RuntimePublisherContract](rpc-messages.md#runtimepublishercontract) | 标量/message；语义必填见规格 | `publisherContract` |
| `admission_receipt_id #5` | `string` | 标量/message；语义必填见规格 | `admissionReceiptId` |

## RegisterWebuiVersionRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `name #1` | `string` | 标量/message；语义必填见规格 | `name` |
| `version_label #2` | `string` | 标量/message；语义必填见规格 | `versionLabel` |
| `publisher_namespace_id #3` | `string` | 标量/message；语义必填见规格 | `publisherNamespaceId` |
| `publisher_contract #4` | [WebuiPublisherContract](rpc-messages.md#webuipublishercontract) | 标量/message；语义必填见规格 | `publisherContract` |
| `admission_receipt_id #5` | `string` | 标量/message；语义必填见规格 | `admissionReceiptId` |

## CatalogStatusRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `status #1` | `CatalogStatusRequestStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `reason #2` | `string` | 标量/message；语义必填见规格 | `reason` |

## ComputePlan

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `name #2` | `string` | 标量/message；语义必填见规格 | `name` |
| `vcpus #3` | `int32` | 标量/message；语义必填见规格 | `vcpus` |
| `memory_mi_b #4` | `int32` | 标量/message；语义必填见规格 | `memoryMiB` |
| `availability #5` | `ComputePlanAvailabilityEnum` | 标量/message；语义必填见规格 | `availability` |
| `billing_mode #6` | `ComputePlanBillingModeEnum` | 标量/message；语义必填见规格 | `billingMode` |
| `provider_capability_version #7` | `string` | 标量/message；语义必填见规格 | `providerCapabilityVersion` |
| `monthly_price_usd_micros #8` | `int64` | proto3 optional | `monthlyPriceUsdMicros` |
| `price_policy_version_id #9` | `string` | proto3 optional | `pricePolicyVersionId` |
| `valid_from #10` | `Timestamp` | 标量/message；语义必填见规格 | `validFrom` |
| `valid_until #11` | `Timestamp` | 标量/message；语义必填见规格 | `validUntil` |
| `created_at #12` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |

## StoragePlan

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `name #2` | `string` | 标量/message；语义必填见规格 | `name` |
| `capacity_gi_b #3` | `int32` | 标量/message；语义必填见规格 | `capacityGiB` |
| `availability #4` | `StoragePlanAvailabilityEnum` | 标量/message；语义必填见规格 | `availability` |
| `billing_mode #5` | `StoragePlanBillingModeEnum` | 标量/message；语义必填见规格 | `billingMode` |
| `shrink_supported #6` | `bool` | 标量/message；语义必填见规格 | `shrinkSupported` |
| `monthly_price_usd_micros #7` | `int64` | proto3 optional | `monthlyPriceUsdMicros` |
| `price_policy_version_id #8` | `string` | proto3 optional | `pricePolicyVersionId` |
| `valid_from #9` | `Timestamp` | 标量/message；语义必填见规格 | `validFrom` |
| `valid_until #10` | `Timestamp` | 标量/message；语义必填见规格 | `validUntil` |
| `created_at #11` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |

## CreateComputePlanRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `name #1` | `string` | 标量/message；语义必填见规格 | `name` |
| `vcpus #2` | `int32` | 标量/message；语义必填见规格 | `vcpus` |
| `memory_mi_b #3` | `int32` | 标量/message；语义必填见规格 | `memoryMiB` |
| `provider_profile_id #4` | `string` | 标量/message；语义必填见规格 | `providerProfileId` |
| `provider_sku_id #5` | `string` | 标量/message；语义必填见规格 | `providerSkuId` |
| `provider_capability_version #6` | `string` | 标量/message；语义必填见规格 | `providerCapabilityVersion` |
| `valid_from #7` | `Timestamp` | 标量/message；语义必填见规格 | `validFrom` |
| `valid_until #8` | `Timestamp` | 标量/message；语义必填见规格 | `validUntil` |

## CreateStoragePlanRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `name #1` | `string` | 标量/message；语义必填见规格 | `name` |
| `capacity_gi_b #2` | `int32` | 标量/message；语义必填见规格 | `capacityGiB` |
| `provider_profile_id #3` | `string` | 标量/message；语义必填见规格 | `providerProfileId` |
| `provider_sku_id #4` | `string` | 标量/message；语义必填见规格 | `providerSkuId` |
| `shrink_supported #5` | `bool` | 标量/message；语义必填见规格 | `shrinkSupported` |
| `valid_from #6` | `Timestamp` | 标量/message；语义必填见规格 | `validFrom` |
| `valid_until #7` | `Timestamp` | 标量/message；语义必填见规格 | `validUntil` |

## PlanAvailabilityRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `availability #1` | `PlanAvailabilityRequestAvailabilityEnum` | 标量/message；语义必填见规格 | `availability` |
| `reason #2` | `string` | 标量/message；语义必填见规格 | `reason` |

## PricePolicyVersion

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `version_label #2` | `string` | 标量/message；语义必填见规格 | `versionLabel` |
| `currency #3` | `PricePolicyVersionCurrencyEnum` | 标量/message；语义必填见规格 | `currency` |
| `period_months #4` | `int32` | 标量/message；语义必填见规格 | `periodMonths` |
| `compute_monthly_usd_micros #5` | `int64` | 标量/message；语义必填见规格 | `computeMonthlyUsdMicros` |
| `storage_monthly_usd_micros #6` | `int64` | 标量/message；语义必填见规格 | `storageMonthlyUsdMicros` |
| `product_monthly_usd_micros #7` | `int64` | 标量/message；语义必填见规格 | `productMonthlyUsdMicros` |
| `valid_from #8` | `Timestamp` | 标量/message；语义必填见规格 | `validFrom` |
| `valid_until #9` | `Timestamp` | 标量/message；语义必填见规格 | `validUntil` |
| `created_at #10` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |
| `compute_plan_id #11` | `string` | 标量/message；语义必填见规格 | `computePlanId` |
| `storage_plan_id #12` | `string` | 标量/message；语义必填见规格 | `storagePlanId` |
| `renewal_policy #13` | [RenewalPolicy](rpc-messages.md#renewalpolicy) | 标量/message；语义必填见规格 | `renewalPolicy` |
| `plan_change_policy_version #14` | `PricePolicyVersionPlanChangePolicyVersionEnum` | 标量/message；语义必填见规格 | `planChangePolicyVersion` |
| `plan_change_policy #15` | [PlanChangePolicy](rpc-messages.md#planchangepolicy) | 标量/message；语义必填见规格 | `planChangePolicy` |

## CreatePricePolicyRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `version_label #1` | `string` | 标量/message；语义必填见规格 | `versionLabel` |
| `period_months #2` | `int32` | 标量/message；语义必填见规格 | `periodMonths` |
| `compute_monthly_usd_micros #3` | `int64` | 标量/message；语义必填见规格 | `computeMonthlyUsdMicros` |
| `storage_monthly_usd_micros #4` | `int64` | 标量/message；语义必填见规格 | `storageMonthlyUsdMicros` |
| `product_monthly_usd_micros #5` | `int64` | 标量/message；语义必填见规格 | `productMonthlyUsdMicros` |
| `valid_from #6` | `Timestamp` | 标量/message；语义必填见规格 | `validFrom` |
| `valid_until #7` | `Timestamp` | 标量/message；语义必填见规格 | `validUntil` |
| `compute_plan_id #8` | `string` | 标量/message；语义必填见规格 | `computePlanId` |
| `storage_plan_id #9` | `string` | 标量/message；语义必填见规格 | `storagePlanId` |
| `renewal_policy #10` | [RenewalPolicy](rpc-messages.md#renewalpolicy) | 标量/message；语义必填见规格 | `renewalPolicy` |
| `plan_change_policy_version #11` | `CreatePricePolicyRequestPlanChangePolicyVersionEnum` | 标量/message；语义必填见规格 | `planChangePolicyVersion` |

## RefundPolicyVersion

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `version_label #2` | `string` | 标量/message；语义必填见规格 | `versionLabel` |
| `algorithm #3` | `RefundPolicyVersionAlgorithmEnum` | 标量/message；语义必填见规格 | `algorithm` |
| `retention_policy_version_id #4` | `string` | 标量/message；语义必填见规格 | `retentionPolicyVersionId` |
| `customer_terms #5` | `string` | 标量/message；语义必填见规格 | `customerTerms` |
| `valid_from #6` | `Timestamp` | 标量/message；语义必填见规格 | `validFrom` |
| `valid_until #7` | `Timestamp` | 标量/message；语义必填见规格 | `validUntil` |
| `created_at #8` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |

## CreateRefundPolicyRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `version_label #1` | `string` | 标量/message；语义必填见规格 | `versionLabel` |
| `algorithm #2` | `CreateRefundPolicyRequestAlgorithmEnum` | 标量/message；语义必填见规格 | `algorithm` |
| `retention_policy_version_id #3` | `string` | 标量/message；语义必填见规格 | `retentionPolicyVersionId` |
| `customer_terms #4` | `string` | 标量/message；语义必填见规格 | `customerTerms` |
| `valid_from #5` | `Timestamp` | 标量/message；语义必填见规格 | `validFrom` |
| `valid_until #6` | `Timestamp` | 标量/message；语义必填见规格 | `validUntil` |

## RetentionPolicyVersion

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `version_label #2` | `string` | 标量/message；语义必填见规格 | `versionLabel` |
| `workspace_data_disposition #3` | `RetentionPolicyVersionWorkspaceDataDispositionEnum` | 标量/message；语义必填见规格 | `workspaceDataDisposition` |
| `package_history_disposition #4` | `RetentionPolicyVersionPackageHistoryDispositionEnum` | 标量/message；语义必填见规格 | `packageHistoryDisposition` |
| `build_history_disposition #5` | `RetentionPolicyVersionBuildHistoryDispositionEnum` | 标量/message；语义必填见规格 | `buildHistoryDisposition` |
| `tenant_restore_days #6` | `int32` | 标量/message；语义必填见规格 | `tenantRestoreDays` |
| `customer_terms #7` | `string` | 标量/message；语义必填见规格 | `customerTerms` |
| `created_at #8` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |

## CreateRetentionPolicyRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `version_label #1` | `string` | 标量/message；语义必填见规格 | `versionLabel` |
| `customer_terms #2` | `string` | 标量/message；语义必填见规格 | `customerTerms` |

## Model

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `name #2` | `string` | 标量/message；语义必填见规格 | `name` |
| `capabilities #3` | `string` | repeated | `capabilities` |
| `available #4` | `bool` | 标量/message；语义必填见规格 | `available` |
| `input_price_per_million_tokens_usd_micros #5` | `int64` | 标量/message；语义必填见规格 | `inputPricePerMillionTokensUsdMicros` |
| `output_price_per_million_tokens_usd_micros #6` | `int64` | 标量/message；语义必填见规格 | `outputPricePerMillionTokensUsdMicros` |
| `price_source #7` | `ModelPriceSourceEnum` | 标量/message；语义必填见规格 | `priceSource` |
| `fetched_at #8` | `Timestamp` | 标量/message；语义必填见规格 | `fetchedAt` |

## ModelSelection

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `slot #1` | `string` | 标量/message；语义必填见规格 | `slot` |
| `model_id #2` | `string` | 标量/message；语义必填见规格 | `modelId` |

## QuoteRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `purpose #1` | `QuoteRequestPurposeEnum` | 标量/message；语义必填见规格 | `purpose` |
| `workspace_id #2` | `string` | proto3 optional | `workspaceId` |
| `capability_version_id #3` | `string` | proto3 optional | `capabilityVersionId` |
| `compute_plan_id #4` | `string` | 标量/message；语义必填见规格 | `computePlanId` |
| `storage_plan_id #5` | `string` | 标量/message；语义必填见规格 | `storagePlanId` |
| `model_selections #6` | [ModelSelection](rpc-messages.md#modelselection) | repeated | `modelSelections` |
| `period_months #7` | `int32` | 标量/message；语义必填见规格 | `periodMonths` |
| `scheduled_plan_change_id #8` | `string` | proto3 optional | `scheduledPlanChangeId` |

## QuoteLine

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `kind #1` | `QuoteLineKindEnum` | 标量/message；语义必填见规格 | `kind` |
| `description #2` | `string` | 标量/message；语义必填见规格 | `description` |
| `quantity #3` | `int32` | 标量/message；语义必填见规格 | `quantity` |
| `amount_usd_micros #4` | `int64` | 标量/message；语义必填见规格 | `amountUsdMicros` |
| `credit_source #5` | [CreditSource](rpc-messages.md#creditsource) | 标量/message；语义必填见规格 | `creditSource` |

## Quote

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `purpose #2` | `QuotePurposeEnum` | 标量/message；语义必填见规格 | `purpose` |
| `workspace_id #3` | `string` | proto3 optional | `workspaceId` |
| `capability_version_id #4` | `string` | proto3 optional | `capabilityVersionId` |
| `compute_plan_id #5` | `string` | 标量/message；语义必填见规格 | `computePlanId` |
| `storage_plan_id #6` | `string` | 标量/message；语义必填见规格 | `storagePlanId` |
| `model_selections #7` | [ModelSelection](rpc-messages.md#modelselection) | repeated | `modelSelections` |
| `period_months #8` | `int32` | 标量/message；语义必填见规格 | `periodMonths` |
| `period_start #9` | `Timestamp` | 标量/message；语义必填见规格 | `periodStart` |
| `period_end #10` | `Timestamp` | 标量/message；语义必填见规格 | `periodEnd` |
| `price_policy_version_id #11` | `string` | 标量/message；语义必填见规格 | `pricePolicyVersionId` |
| `refund_policy_version_id #12` | `string` | 标量/message；语义必填见规格 | `refundPolicyVersionId` |
| `retention_policy_version_id #13` | `string` | 标量/message；语义必填见规格 | `retentionPolicyVersionId` |
| `refund_terms #14` | `string` | 标量/message；语义必填见规格 | `refundTerms` |
| `retention_terms #15` | `string` | 标量/message；语义必填见规格 | `retentionTerms` |
| `expected_interruption #16` | `string` | 标量/message；语义必填见规格 | `expectedInterruption` |
| `line_items #17` | [QuoteLine](rpc-messages.md#quoteline) | repeated | `lineItems` |
| `total_usd_micros #18` | `int64` | 标量/message；语义必填见规格 | `totalUsdMicros` |
| `status #19` | `QuoteStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `expires_at #20` | `Timestamp` | 标量/message；语义必填见规格 | `expiresAt` |
| `created_at #21` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |
| `source_subscription_version #22` | `int64` | proto3 optional | `sourceSubscriptionVersion` |
| `plan_change_calculation #23` | [PlanChangeCalculation](rpc-messages.md#planchangecalculation) | 标量/message；语义必填见规格 | `planChangeCalculation` |
| `scheduled_plan_change_id #24` | `string` | proto3 optional | `scheduledPlanChangeId` |
| `runtime_readback_requirement #25` | `QuoteRuntimeReadbackRequirementEnum` | 标量/message；语义必填见规格 | `runtimeReadbackRequirement` |

## Workspace

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `name #2` | `string` | 标量/message；语义必填见规格 | `name` |
| `capability_version_id #3` | `string` | proto3 optional | `capabilityVersionId` |
| `compute_plan_id #4` | `string` | 标量/message；语义必填见规格 | `computePlanId` |
| `storage_plan_id #5` | `string` | 标量/message；语义必填见规格 | `storagePlanId` |
| `active_deployment_id #6` | `string` | proto3 optional | `activeDeploymentId` |
| `status #7` | `WorkspaceStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `resource_readiness #8` | `WorkspaceResourceReadinessEnum` | 标量/message；语义必填见规格 | `resourceReadiness` |
| `application_availability #9` | `WorkspaceApplicationAvailabilityEnum` | 标量/message；语义必填见规格 | `applicationAvailability` |
| `model_configuration_version #10` | `int64` | 标量/message；语义必填见规格 | `modelConfigurationVersion` |
| `current_period_end #11` | `Timestamp` | 标量/message；语义必填见规格 | `currentPeriodEnd` |
| `access_url #12` | `string` | proto3 optional | `accessUrl` |
| `created_at #13` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |
| `updated_at #14` | `Timestamp` | 标量/message；语义必填见规格 | `updatedAt` |
| `delivery_model #15` | `WorkspaceDeliveryModelEnum` | 标量/message；语义必填见规格 | `deliveryModel` |
| `version #16` | `int64` | 标量/message；语义必填见规格 | `version` |

## CreateWorkspaceRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `name #1` | `string` | 标量/message；语义必填见规格 | `name` |
| `quote_id #2` | `string` | 标量/message；语义必填见规格 | `quoteId` |
| `renewal_mode #3` | `CreateWorkspaceRequestRenewalModeEnum` | 标量/message；语义必填见规格 | `renewalMode` |
| `automatic_renewal_consent #4` | `bool` | proto3 optional | `automaticRenewalConsent` |

## WorkspaceAccess

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `workspace_id #1` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `url #2` | `string` | 标量/message；语义必填见规格 | `url` |
| `authentication_mode #3` | `WorkspaceAccessAuthenticationModeEnum` | 标量/message；语义必填见规格 | `authenticationMode` |
| `expires_at #4` | `Timestamp` | 标量/message；语义必填见规格 | `expiresAt` |
| `application_credentials_available #5` | `bool` | proto3 optional | `applicationCredentialsAvailable` |

## ModelConfiguration

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `workspace_id #1` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `version #2` | `int64` | 标量/message；语义必填见规格 | `version` |
| `selections #3` | [ModelSelection](rpc-messages.md#modelselection) | repeated | `selections` |
| `status #4` | `ModelConfigurationStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `applied_version #5` | `int64` | proto3 optional | `appliedVersion` |
| `operation_id #6` | `string` | proto3 optional | `operationId` |
| `updated_at #7` | `Timestamp` | 标量/message；语义必填见规格 | `updatedAt` |

## UpdateWorkspaceModelsRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `expected_version #1` | `int64` | 标量/message；语义必填见规格 | `expectedVersion` |
| `selections #2` | [ModelSelection](rpc-messages.md#modelselection) | repeated | `selections` |

## Deployment

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `capability_version_id #3` | `string` | 标量/message；语义必填见规格 | `capabilityVersionId` |
| `runtime_instance_id #4` | `string` | proto3 optional | `runtimeInstanceId` |
| `previous_deployment_id #5` | `string` | proto3 optional | `previousDeploymentId` |
| `status #6` | `DeploymentStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `data_compatibility #7` | [DataCompatibility](rpc-messages.md#datacompatibility) | 标量/message；语义必填见规格 | `dataCompatibility` |
| `created_at #8` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |
| `updated_at #9` | `Timestamp` | 标量/message；语义必填见规格 | `updatedAt` |

## UpdateWorkspaceVersionRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `capability_version_id #1` | `string` | 标量/message；语义必填见规格 | `capabilityVersionId` |
| `expected_active_deployment_id #2` | `string` | 标量/message；语义必填见规格 | `expectedActiveDeploymentId` |

## RollbackWorkspaceRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `target_deployment_id #1` | `string` | 标量/message；语义必填见规格 | `targetDeploymentId` |
| `expected_active_deployment_id #2` | `string` | 标量/message；语义必填见规格 | `expectedActiveDeploymentId` |

## ApplyQuoteRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `quote_id #1` | `string` | 标量/message；语义必填见规格 | `quoteId` |

## DeleteWorkspaceRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `confirmation_name #1` | `string` | 标量/message；语义必填见规格 | `confirmationName` |
| `acknowledge_data_destruction #2` | `bool` | 标量/message；语义必填见规格 | `acknowledgeDataDestruction` |

## WorkspaceDeletion

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `workspace_id #1` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `operation_id #2` | `string` | 标量/message；语义必填见规格 | `operationId` |
| `resource_deletion_status #3` | `WorkspaceDeletionResourceDeletionStatusEnum` | 标量/message；语义必填见规格 | `resourceDeletionStatus` |
| `data_deletion_status #4` | `WorkspaceDeletionDataDeletionStatusEnum` | 标量/message；语义必填见规格 | `dataDeletionStatus` |
| `refund_operation_id #5` | `string` | proto3 optional | `refundOperationId` |
| `refund_status #6` | `WorkspaceDeletionRefundStatusEnum` | 标量/message；语义必填见规格 | `refundStatus` |
| `updated_at #7` | `Timestamp` | 标量/message；语义必填见规格 | `updatedAt` |

## Subscription

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `current_period_start #3` | `Timestamp` | 标量/message；语义必填见规格 | `currentPeriodStart` |
| `current_period_end #4` | `Timestamp` | 标量/message；语义必填见规格 | `currentPeriodEnd` |
| `period_months #5` | `int32` | 标量/message；语义必填见规格 | `periodMonths` |
| `status #6` | `SubscriptionStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `accepted_quote_id #7` | `string` | proto3 optional | `acceptedQuoteId` |
| `renewal_operation_id #8` | `string` | proto3 optional | `renewalOperationId` |
| `created_at #9` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |
| `provenance #10` | `SubscriptionProvenanceEnum` | 标量/message；语义必填见规格 | `provenance` |
| `legacy_purchase_id #11` | `string` | proto3 optional | `legacyPurchaseId` |
| `refund_policy_version_id #12` | `string` | proto3 optional | `refundPolicyVersionId` |
| `retention_policy_version_id #13` | `string` | proto3 optional | `retentionPolicyVersionId` |
| `refund_terms #14` | `string` | proto3 optional | `refundTerms` |
| `retention_terms #15` | `string` | proto3 optional | `retentionTerms` |
| `renewal_mode #16` | `SubscriptionRenewalModeEnum` | 标量/message；语义必填见规格 | `renewalMode` |
| `renewal_consent_id #17` | `string` | proto3 optional | `renewalConsentId` |
| `renewal_settings_version #18` | `int64` | 标量/message；语义必填见规格 | `renewalSettingsVersion` |
| `version #19` | `int64` | 标量/message；语义必填见规格 | `version` |
| `current_price_policy_version_id #20` | `string` | proto3 optional | `currentPricePolicyVersionId` |
| `current_monthly_usd_micros #21` | `int64` | proto3 optional | `currentMonthlyUsdMicros` |

## WalletOperation

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `workspace_id #2` | `string` | proto3 optional | `workspaceId` |
| `kind #3` | `WalletOperationKindEnum` | 标量/message；语义必填见规格 | `kind` |
| `amount_usd_micros #4` | `int64` | 标量/message；语义必填见规格 | `amountUsdMicros` |
| `status #5` | `WalletOperationStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `external_reference #6` | `string` | proto3 optional | `externalReference` |
| `receipt_id #7` | `string` | proto3 optional | `receiptId` |
| `error_code #8` | `ErrorCodeEnum` | proto3 optional | `errorCode` |
| `created_at #9` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |
| `updated_at #10` | `Timestamp` | 标量/message；语义必填见规格 | `updatedAt` |
| `purpose #11` | `WalletOperationPurposeEnum` | proto3 optional | `purpose` |
| `plan_change_id #12` | `string` | proto3 optional | `planChangeId` |
| `original_charge_operation_id #13` | `string` | proto3 optional | `originalChargeOperationId` |
| `coverage_start #14` | `Timestamp` | 标量/message；语义必填见规格 | `coverageStart` |
| `coverage_end #15` | `Timestamp` | 标量/message；语义必填见规格 | `coverageEnd` |
| `coverage_start_milliseconds #16` | `int64` | proto3 optional | `coverageStartMilliseconds` |
| `coverage_end_milliseconds #17` | `int64` | proto3 optional | `coverageEndMilliseconds` |

## Wallet

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `source #1` | `WalletSourceEnum` | 标量/message；语义必填见规格 | `source` |
| `status #2` | `WalletStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `balance_usd_micros #3` | `int64` | 标量/message；语义必填见规格 | `balanceUsdMicros` |
| `currency #4` | `WalletCurrencyEnum` | 标量/message；语义必填见规格 | `currency` |
| `fetched_at #5` | `Timestamp` | 标量/message；语义必填见规格 | `fetchedAt` |

## Usage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `model_id #2` | `string` | 标量/message；语义必填见规格 | `modelId` |
| `period_start #3` | `Timestamp` | 标量/message；语义必填见规格 | `periodStart` |
| `period_end #4` | `Timestamp` | 标量/message；语义必填见规格 | `periodEnd` |
| `input_tokens #5` | `int64` | 标量/message；语义必填见规格 | `inputTokens` |
| `output_tokens #6` | `int64` | 标量/message；语义必填见规格 | `outputTokens` |
| `cost_usd_micros #7` | `int64` | 标量/message；语义必填见规格 | `costUsdMicros` |
| `source #8` | `UsageSourceEnum` | 标量/message；语义必填见规格 | `source` |
| `created_at #9` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |

## GatewayKey

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `name #2` | `string` | 标量/message；语义必填见规格 | `name` |
| `fingerprint #3` | `string` | 标量/message；语义必填见规格 | `fingerprint` |
| `purpose #4` | `GatewayKeyPurposeEnum` | 标量/message；语义必填见规格 | `purpose` |
| `workspace_id #5` | `string` | proto3 optional | `workspaceId` |
| `status #6` | `GatewayKeyStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `model_ids #7` | `string` | repeated | `modelIds` |
| `created_at #8` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |
| `expires_at #9` | `Timestamp` | 标量/message；语义必填见规格 | `expiresAt` |

## CreateGatewayKeyRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `name #1` | `string` | 标量/message；语义必填见规格 | `name` |
| `model_ids #2` | `string` | repeated | `modelIds` |
| `expires_at #3` | `Timestamp` | 标量/message；语义必填见规格 | `expiresAt` |

## GatewayKeySecret

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `key #1` | [GatewayKey](rpc-messages.md#gatewaykey) | 标量/message；语义必填见规格 | `key` |
| `secret #2` | `string` | 标量/message；语义必填见规格 | `secret` |

## AuditEvent

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `actor_id #2` | `string` | 标量/message；语义必填见规格 | `actorId` |
| `action #3` | `string` | 标量/message；语义必填见规格 | `action` |
| `resource_id #4` | `string` | 标量/message；语义必填见规格 | `resourceId` |
| `outcome #5` | `AuditEventOutcomeEnum` | 标量/message；语义必填见规格 | `outcome` |
| `request_id #6` | `string` | 标量/message；语义必填见规格 | `requestId` |
| `created_at #7` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |

## Receipt

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `kind #2` | `ReceiptKindEnum` | 标量/message；语义必填见规格 | `kind` |
| `owner #3` | `OwnerEnum` | 标量/message；语义必填见规格 | `owner` |
| `source_sha #4` | `string` | proto3 optional | `sourceSha` |
| `artifact_digest #5` | `string` | proto3 optional | `artifactDigest` |
| `operation_id #6` | `string` | proto3 optional | `operationId` |
| `workflow_run_id #7` | `string` | proto3 optional | `workflowRunId` |
| `outcome #8` | `ReceiptOutcomeEnum` | 标量/message；语义必填见规格 | `outcome` |
| `evidence_summary #9` | `string` | 标量/message；语义必填见规格 | `evidenceSummary` |
| `created_at #10` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |

## ReconcileOperationRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `reason #1` | `string` | 标量/message；语义必填见规格 | `reason` |

## AdminOperation

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `operation #1` | [Operation](rpc-messages.md#operation) | 标量/message；语义必填见规格 | `operation` |
| `tenant_id #2` | `string` | 标量/message；语义必填见规格 | `tenantId` |

## Qualification

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `candidate_source_sha #2` | `string` | 标量/message；语义必填见规格 | `candidateSourceSha` |
| `artifact_digest #3` | `string` | 标量/message；语义必填见规格 | `artifactDigest` |
| `local_receipt_id #4` | `string` | proto3 optional | `localReceiptId` |
| `instance_receipt_id #5` | `string` | proto3 optional | `instanceReceiptId` |
| `status #6` | `QualificationStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `created_at #7` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |

## MemberPage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [Member](rpc-messages.md#member) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## InvitationPage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [Invitation](rpc-messages.md#invitation) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## NamespacePage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [Namespace](rpc-messages.md#namespace) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## PackagePage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [Package](rpc-messages.md#package) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## PackageVersionPage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [PackageVersion](rpc-messages.md#packageversion) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## CapabilityVersionPage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [CapabilityVersion](rpc-messages.md#capabilityversion) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## BuildJobPage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [BuildJob](rpc-messages.md#buildjob) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## BuildLogPage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [BuildLog](rpc-messages.md#buildlog) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## RuntimeVersionPage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [RuntimeVersion](rpc-messages.md#runtimeversion) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## WebuiVersionPage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [WebuiVersion](rpc-messages.md#webuiversion) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## ComputePlanPage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [ComputePlan](rpc-messages.md#computeplan) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## StoragePlanPage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [StoragePlan](rpc-messages.md#storageplan) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## PricePolicyVersionPage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [PricePolicyVersion](rpc-messages.md#pricepolicyversion) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## RefundPolicyVersionPage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [RefundPolicyVersion](rpc-messages.md#refundpolicyversion) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## RetentionPolicyVersionPage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [RetentionPolicyVersion](rpc-messages.md#retentionpolicyversion) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## ModelPage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [Model](rpc-messages.md#model) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## WorkspacePage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [Workspace](rpc-messages.md#workspace) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## DeploymentPage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [Deployment](rpc-messages.md#deployment) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## WalletOperationPage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [WalletOperation](rpc-messages.md#walletoperation) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## UsagePage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [Usage](rpc-messages.md#usage) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## GatewayKeyPage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [GatewayKey](rpc-messages.md#gatewaykey) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## TenantPage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [Tenant](rpc-messages.md#tenant) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## AuditEventPage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [AuditEvent](rpc-messages.md#auditevent) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## ReceiptPage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [Receipt](rpc-messages.md#receipt) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## AdminOperationPage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [AdminOperation](rpc-messages.md#adminoperation) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## QualificationPage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [Qualification](rpc-messages.md#qualification) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## AdoptWorkspaceRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `capability_version_id #1` | `string` | 标量/message；语义必填见规格 | `capabilityVersionId` |
| `expected_workspace_version #2` | `int64` | 标量/message；语义必填见规格 | `expectedWorkspaceVersion` |
| `model_selections #3` | [ModelSelection](rpc-messages.md#modelselection) | repeated | `modelSelections` |

## BuildRuntimePolicy

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `runtime_version_id #2` | `string` | 标量/message；语义必填见规格 | `runtimeVersionId` |
| `default_webui_version_id #3` | `string` | proto3 optional | `defaultWebuiVersionId` |
| `policy_version #4` | `string` | 标量/message；语义必填见规格 | `policyVersion` |
| `effective_at #5` | `Timestamp` | 标量/message；语义必填见规格 | `effectiveAt` |
| `created_at #6` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |

## SetBuildRuntimePolicyRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `runtime_version_id #1` | `string` | 标量/message；语义必填见规格 | `runtimeVersionId` |
| `default_webui_version_id #2` | `string` | proto3 optional | `defaultWebuiVersionId` |
| `expected_policy_version_id #3` | `string` | proto3 optional | `expectedPolicyVersionId` |

## ImagePlatform

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `os #1` | `ImagePlatformOsEnum` | 标量/message；语义必填见规格 | `os` |
| `architecture #2` | `ImagePlatformArchitectureEnum` | 标量/message；语义必填见规格 | `architecture` |
| `variant #3` | `string` | proto3 optional | `variant` |

## ArtifactReference

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `repository #1` | `string` | 标量/message；语义必填见规格 | `repository` |
| `digest #2` | `string` | 标量/message；语义必填见规格 | `digest` |
| `platform #3` | [ImagePlatform](rpc-messages.md#imageplatform) | 标量/message；语义必填见规格 | `platform` |

## RecipeArtifact

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `repository #1` | `string` | 标量/message；语义必填见规格 | `repository` |
| `digest #2` | `string` | 标量/message；语义必填见规格 | `digest` |
| `media_type #3` | `RecipeArtifactMediaTypeEnum` | 标量/message；语义必填见规格 | `mediaType` |
| `dockerfile_path #4` | `string` | 标量/message；语义必填见规格 | `dockerfilePath` |

## PackageBuildInput

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context_name #1` | `PackageBuildInputContextNameEnum` | 标量/message；语义必填见规格 | `contextName` |
| `format_version #2` | `string` | 标量/message；语义必填见规格 | `formatVersion` |
| `source_root #3` | `string` | 标量/message；语义必填见规格 | `sourceRoot` |
| `target_path #4` | `string` | 标量/message；语义必填见规格 | `targetPath` |
| `uid #5` | `int32` | 标量/message；语义必填见规格 | `uid` |
| `gid #6` | `int32` | 标量/message；语义必填见规格 | `gid` |

## WebuiBuildInput

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context_name #1` | `WebuiBuildInputContextNameEnum` | 标量/message；语义必填见规格 | `contextName` |
| `source_path #2` | `string` | 标量/message；语义必填见规格 | `sourcePath` |
| `target_path #3` | `string` | 标量/message；语义必填见规格 | `targetPath` |
| `uid #4` | `int32` | 标量/message；语义必填见规格 | `uid` |
| `gid #5` | `int32` | 标量/message；语义必填见规格 | `gid` |

## BuildRecipeContract

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `version #1` | `BuildRecipeContractVersionEnum` | 标量/message；语义必填见规格 | `version` |
| `frontend #2` | [ArtifactReference](rpc-messages.md#artifactreference) | 标量/message；语义必填见规格 | `frontend` |
| `recipe #3` | [RecipeArtifact](rpc-messages.md#recipeartifact) | 标量/message；语义必填见规格 | `recipe` |
| `runtime_context_name #4` | `BuildRecipeContractRuntimeContextNameEnum` | 标量/message；语义必填见规格 | `runtimeContextName` |
| `package_input #5` | [PackageBuildInput](rpc-messages.md#packagebuildinput) | 标量/message；语义必填见规格 | `packageInput` |
| `webui_input #6` | [WebuiBuildInput](rpc-messages.md#webuibuildinput) | 标量/message；语义必填见规格 | `webuiInput` |
| `network_policy #7` | `BuildRecipeContractNetworkPolicyEnum` | 标量/message；语义必填见规格 | `networkPolicy` |
| `output_platform #8` | [ImagePlatform](rpc-messages.md#imageplatform) | 标量/message；语义必填见规格 | `outputPlatform` |
| `output_image_command #9` | [BuildRecipeContractOutputImageCommand](rpc-messages.md#buildrecipecontractoutputimagecommand) | 标量/message；语义必填见规格 | `outputImageCommand` |

## ModelConfigurationContract

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `protocol #1` | `ModelConfigurationContractProtocolEnum` | 标量/message；语义必填见规格 | `protocol` |
| `port_name #2` | `string` | 标量/message；语义必填见规格 | `portName` |
| `apply_path #3` | `string` | 标量/message；语义必填见规格 | `applyPath` |
| `readback_path #4` | `string` | 标量/message；语义必填见规格 | `readbackPath` |
| `request_fields #5` | `ModelConfigurationContractRequestFieldsEnum` | repeated | `requestFields` |
| `readback_fields #6` | `ModelConfigurationContractReadbackFieldsEnum` | repeated | `readbackFields` |
| `authorization_secret_input_name #7` | `string` | 标量/message；语义必填见规格 | `authorizationSecretInputName` |

## ApplicationAccessContract

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `application_owned_access_contract #1` | [ApplicationOwnedAccessContract](rpc-messages.md#applicationownedaccesscontract) | oneof value | `applicationOwnedAccessContract` |
| `cloud_private_access_contract #2` | [CloudPrivateAccessContract](rpc-messages.md#cloudprivateaccesscontract) | oneof value | `cloudPrivateAccessContract` |
| `anonymous_access_contract #3` | [AnonymousAccessContract](rpc-messages.md#anonymousaccesscontract) | oneof value | `anonymousAccessContract` |

## DataUpgradeContract

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `mode #1` | `DataUpgradeContractModeEnum` | 标量/message；语义必填见规格 | `mode` |
| `compatible_from_schema_versions #2` | `string` | repeated | `compatibleFromSchemaVersions` |
| `migration_artifact #3` | [ArtifactReference](rpc-messages.md#artifactreference) | 标量/message；语义必填见规格 | `migrationArtifact` |
| `backup_required #4` | `bool` | 标量/message；语义必填见规格 | `backupRequired` |
| `backup_format_version #5` | `string` | proto3 optional | `backupFormatVersion` |

## DataRollbackContract

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `safe #1` | `bool` | 标量/message；语义必填见规格 | `safe` |
| `compatible_schema_versions #2` | `string` | repeated | `compatibleSchemaVersions` |

## DataContract

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `schema_version #1` | `string` | 标量/message；语义必填见规格 | `schemaVersion` |
| `upgrade #2` | [DataUpgradeContract](rpc-messages.md#dataupgradecontract) | 标量/message；语义必填见规格 | `upgrade` |
| `rollback #3` | [DataRollbackContract](rpc-messages.md#datarollbackcontract) | 标量/message；语义必填见规格 | `rollback` |
| `mount_policies #4` | [DataMountPolicy](rpc-messages.md#datamountpolicy) | repeated | `mountPolicies` |

## RuntimePublisherContract

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `schema_version #1` | `RuntimePublisherContractSchemaVersionEnum` | 标量/message；语义必填见规格 | `schemaVersion` |
| `publisher_namespace_id #2` | `string` | 标量/message；语义必填见规格 | `publisherNamespaceId` |
| `image #3` | [ArtifactReference](rpc-messages.md#artifactreference) | 标量/message；语义必填见规格 | `image` |
| `kind #4` | `RuntimePublisherContractKindEnum` | 标量/message；语义必填见规格 | `kind` |
| `runtime_abi_version #5` | `string` | 标量/message；语义必填见规格 | `runtimeAbiVersion` |
| `package_format_versions #6` | `string` | repeated | `packageFormatVersions` |
| `build_recipe #7` | [BuildRecipeContract](rpc-messages.md#buildrecipecontract) | 标量/message；语义必填见规格 | `buildRecipe` |
| `model_configuration #8` | [ModelConfigurationContract](rpc-messages.md#modelconfigurationcontract) | 标量/message；语义必填见规格 | `modelConfiguration` |
| `application_access #9` | [ApplicationAccessContract](rpc-messages.md#applicationaccesscontract) | 标量/message；语义必填见规格 | `applicationAccess` |
| `data #10` | [DataContract](rpc-messages.md#datacontract) | 标量/message；语义必填见规格 | `data` |
| `application_revision_template #11` | [WorkspaceApplicationRevision](rpc-messages.md#workspaceapplicationrevision) | 标量/message；语义必填见规格 | `applicationRevisionTemplate` |
| `package_format_contracts #12` | [PackageFormatContractReference](rpc-messages.md#packageformatcontractreference) | repeated | `packageFormatContracts` |

## WebuiPublisherContract

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `schema_version #1` | `WebuiPublisherContractSchemaVersionEnum` | 标量/message；语义必填见规格 | `schemaVersion` |
| `publisher_namespace_id #2` | `string` | 标量/message；语义必填见规格 | `publisherNamespaceId` |
| `image #3` | [ArtifactReference](rpc-messages.md#artifactreference) | 标量/message；语义必填见规格 | `image` |
| `kind #4` | `WebuiPublisherContractKindEnum` | 标量/message；语义必填见规格 | `kind` |
| `runtime_abi_versions #5` | `string` | repeated | `runtimeAbiVersions` |
| `ui_protocol_version #6` | `WebuiPublisherContractUiProtocolVersionEnum` | 标量/message；语义必填见规格 | `uiProtocolVersion` |
| `integration_mode #7` | `WebuiPublisherContractIntegrationModeEnum` | 标量/message；语义必填见规格 | `integrationMode` |
| `static_root #8` | `string` | 标量/message；语义必填见规格 | `staticRoot` |
| `entry_file #9` | `string` | 标量/message；语义必填见规格 | `entryFile` |
| `asset_base_path #10` | `string` | 标量/message；语义必填见规格 | `assetBasePath` |
| `api_base_path #11` | `string` | 标量/message；语义必填见规格 | `apiBasePath` |
| `authentication_protocol #12` | `WebuiPublisherContractAuthenticationProtocolEnum` | 标量/message；语义必填见规格 | `authenticationProtocol` |
| `supports_sse #13` | `bool` | 标量/message；语义必填见规格 | `supportsSse` |
| `supports_websocket #14` | `bool` | 标量/message；语义必填见规格 | `supportsWebsocket` |

## PublisherContract

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `runtime_publisher_contract #1` | [RuntimePublisherContract](rpc-messages.md#runtimepublishercontract) | oneof value | `runtimePublisherContract` |
| `webui_publisher_contract #2` | [WebuiPublisherContract](rpc-messages.md#webuipublishercontract) | oneof value | `webuiPublisherContract` |

## PublisherContractReference

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `publisher_namespace_id #1` | `string` | 标量/message；语义必填见规格 | `publisherNamespaceId` |
| `version_id #2` | `string` | 标量/message；语义必填见规格 | `versionId` |
| `kind #3` | `PublisherContractReferenceKindEnum` | 标量/message；语义必填见规格 | `kind` |
| `descriptor_digest #4` | `string` | 标量/message；语义必填见规格 | `descriptorDigest` |
| `descriptor_object_ref #5` | `string` | 标量/message；语义必填见规格 | `descriptorObjectRef` |

## DeploymentDescriptor

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `schema_version #1` | `DeploymentDescriptorSchemaVersionEnum` | 标量/message；语义必填见规格 | `schemaVersion` |
| `artifact #2` | [ArtifactReference](rpc-messages.md#artifactreference) | 标量/message；语义必填见规格 | `artifact` |
| `runtime_contract #3` | [RuntimePublisherContract](rpc-messages.md#runtimepublishercontract) | 标量/message；语义必填见规格 | `runtimeContract` |
| `runtime_contract_reference #4` | [PublisherContractReference](rpc-messages.md#publishercontractreference) | 标量/message；语义必填见规格 | `runtimeContractReference` |
| `webui_contract #5` | [WebuiPublisherContract](rpc-messages.md#webuipublishercontract) | 标量/message；语义必填见规格 | `webuiContract` |
| `webui_contract_reference #6` | [PublisherContractReference](rpc-messages.md#publishercontractreference) | 标量/message；语义必填见规格 | `webuiContractReference` |
| `package_version_id #7` | `string` | proto3 optional | `packageVersionId` |
| `build_input_digest #8` | `string` | proto3 optional | `buildInputDigest` |
| `provenance #9` | `DeploymentDescriptorProvenanceEnum` | 标量/message；语义必填见规格 | `provenance` |
| `legacy_application_revision_id #10` | `string` | proto3 optional | `legacyApplicationRevisionId` |
| `application_revision #11` | [WorkspaceApplicationRevision](rpc-messages.md#workspaceapplicationrevision) | 标量/message；语义必填见规格 | `applicationRevision` |

## PublisherNamespace

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `name #2` | `string` | 标量/message；语义必填见规格 | `name` |
| `kind #3` | `PublisherNamespaceKindEnum` | 标量/message；语义必填见规格 | `kind` |
| `registry_id #4` | `string` | 标量/message；语义必填见规格 | `registryId` |
| `repository_prefix #5` | `string` | 标量/message；语义必填见规格 | `repositoryPrefix` |
| `admission_receipt_id #6` | `string` | 标量/message；语义必填见规格 | `admissionReceiptId` |
| `status #7` | `PublisherNamespaceStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `created_at #8` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |

## CreatePublisherNamespaceRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `name #1` | `string` | 标量/message；语义必填见规格 | `name` |
| `kind #2` | `CreatePublisherNamespaceRequestKindEnum` | 标量/message；语义必填见规格 | `kind` |
| `registry_id #3` | `string` | 标量/message；语义必填见规格 | `registryId` |
| `repository_prefix #4` | `string` | 标量/message；语义必填见规格 | `repositoryPrefix` |
| `admission_receipt_id #5` | `string` | 标量/message；语义必填见规格 | `admissionReceiptId` |

## RevokePublisherNamespaceRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `reason #1` | `string` | 标量/message；语义必填见规格 | `reason` |

## PublisherNamespacePage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [PublisherNamespace](rpc-messages.md#publishernamespace) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## ReenableTenantRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `reason #1` | `string` | 标量/message；语义必填见规格 | `reason` |

## RenewalPolicy

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `version #1` | `RenewalPolicyVersionEnum` | 标量/message；语义必填见规格 | `version` |
| `trigger #2` | `RenewalPolicyTriggerEnum` | 标量/message；语义必填见规格 | `trigger` |
| `effective_start #3` | `RenewalPolicyEffectiveStartEnum` | 标量/message；语义必填见规格 | `effectiveStart` |
| `months #4` | `int32` | 标量/message；语义必填见规格 | `months` |
| `uses_accepted_price_snapshot #5` | `bool` | 标量/message；语义必填见规格 | `usesAcceptedPriceSnapshot` |

## WorkspaceApplicationExecution

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `user_id #1` | `int32` | proto3 optional | `userId` |
| `group_id #2` | `int32` | proto3 optional | `groupId` |
| `init #3` | `bool` | proto3 optional | `init` |
| `seccomp_profile #4` | `string` | proto3 optional | `seccompProfile` |

## WorkspaceApplicationCompute

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `cpu_request_milli #1` | `int32` | proto3 optional | `cpuRequestMilli` |
| `cpu_limit_milli #2` | `int32` | proto3 optional | `cpuLimitMilli` |
| `memory_request_bytes #3` | `int64` | proto3 optional | `memoryRequestBytes` |
| `memory_limit_bytes #4` | `int64` | proto3 optional | `memoryLimitBytes` |

## WorkspaceApplicationCredential

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `name #1` | `string` | 标量/message；语义必填见规格 | `name` |
| `kind #2` | `WorkspaceApplicationCredentialKindEnum` | 标量/message；语义必填见规格 | `kind` |
| `target #3` | `string` | 标量/message；语义必填见规格 | `target` |
| `env #4` | `string` | proto3 optional | `env` |
| `username #5` | `string` | proto3 optional | `username` |

## WorkspaceApplicationPort

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `name #1` | `string` | 标量/message；语义必填见规格 | `name` |
| `port #2` | `int32` | 标量/message；语义必填见规格 | `port` |
| `protocol #3` | `WorkspaceApplicationPortProtocolEnum` | 标量/message；语义必填见规格 | `protocol` |

## WorkspaceApplicationHealthCheck

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `port #1` | `int32` | 标量/message；语义必填见规格 | `port` |
| `path #2` | `string` | 标量/message；语义必填见规格 | `path` |
| `initial_delay_seconds #3` | `int32` | proto3 optional | `initialDelaySeconds` |

## WorkspaceApplicationMount

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `name #1` | `string` | 标量/message；语义必填见规格 | `name` |
| `mount_path #2` | `string` | 标量/message；语义必填见规格 | `mountPath` |
| `read_only #3` | `bool` | proto3 optional | `readOnly` |
| `mode #4` | `int32` | proto3 optional | `mode` |
| `user_id #5` | `int32` | proto3 optional | `userId` |
| `group_id #6` | `int32` | proto3 optional | `groupId` |
| `size_bytes #7` | `int64` | proto3 optional | `sizeBytes` |
| `executable #8` | `bool` | proto3 optional | `executable` |

## WorkspaceApplicationSecretInput

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `name #1` | `string` | 标量/message；语义必填见规格 | `name` |
| `target #2` | `string` | proto3 optional | `target` |
| `env #3` | `string` | proto3 optional | `env` |

## WorkspaceApplicationConfigInput

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `name #1` | `string` | 标量/message；语义必填见规格 | `name` |
| `target #2` | `string` | 标量/message；语义必填见规格 | `target` |

## WorkspaceApplicationDependencyCommand

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `entrypoint #1` | `string` | repeated | `entrypoint` |
| `args #2` | `string` | repeated | `args` |
| `env #3` | `EnvEntry` | repeated | `env` |

## WorkspaceApplicationDependencyHealthCheck

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `type #1` | `WorkspaceApplicationDependencyHealthCheckTypeEnum` | 标量/message；语义必填见规格 | `type` |
| `port #2` | `int32` | 标量/message；语义必填见规格 | `port` |
| `path #3` | `string` | proto3 optional | `path` |
| `command #4` | `string` | repeated | `command` |
| `initial_delay_seconds #5` | `int32` | proto3 optional | `initialDelaySeconds` |

## WorkspaceApplicationDependency

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `name #1` | `string` | 标量/message；语义必填见规格 | `name` |
| `image #2` | `string` | 标量/message；语义必填见规格 | `image` |
| `execution #3` | [WorkspaceApplicationExecution](rpc-messages.md#workspaceapplicationexecution) | 标量/message；语义必填见规格 | `execution` |
| `depends_on #4` | `string` | repeated | `dependsOn` |
| `ports #5` | [WorkspaceApplicationPort](rpc-messages.md#workspaceapplicationport) | repeated | `ports` |
| `health_checks #6` | [WorkspaceApplicationDependencyHealthCheck](rpc-messages.md#workspaceapplicationdependencyhealthcheck) | repeated | `healthChecks` |
| `persistent_mounts #7` | [WorkspaceApplicationMount](rpc-messages.md#workspaceapplicationmount) | repeated | `persistentMounts` |
| `scratch_mounts #8` | [WorkspaceApplicationMount](rpc-messages.md#workspaceapplicationmount) | repeated | `scratchMounts` |
| `command #9` | [WorkspaceApplicationDependencyCommand](rpc-messages.md#workspaceapplicationdependencycommand) | 标量/message；语义必填见规格 | `command` |
| `secret_inputs #10` | [WorkspaceApplicationSecretInput](rpc-messages.md#workspaceapplicationsecretinput) | repeated | `secretInputs` |
| `config_inputs #11` | [WorkspaceApplicationConfigInput](rpc-messages.md#workspaceapplicationconfiginput) | repeated | `configInputs` |
| `compute #12` | [WorkspaceApplicationCompute](rpc-messages.md#workspaceapplicationcompute) | 标量/message；语义必填见规格 | `compute` |

## WorkspaceApplicationRevision

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `schema_version #1` | `int32` | 标量/message；语义必填见规格 | `schemaVersion` |
| `application_id #2` | `string` | 标量/message；语义必填见规格 | `applicationId` |
| `version #3` | `string` | 标量/message；语义必填见规格 | `version` |
| `platform #4` | `string` | 标量/message；语义必填见规格 | `platform` |
| `image #5` | `string` | 标量/message；语义必填见规格 | `image` |
| `execution #6` | [WorkspaceApplicationExecution](rpc-messages.md#workspaceapplicationexecution) | 标量/message；语义必填见规格 | `execution` |
| `credentials #7` | [WorkspaceApplicationCredential](rpc-messages.md#workspaceapplicationcredential) | repeated | `credentials` |
| `entrypoint #8` | `string` | repeated | `entrypoint` |
| `ports #9` | [WorkspaceApplicationPort](rpc-messages.md#workspaceapplicationport) | repeated | `ports` |
| `entry_port #10` | `string` | proto3 optional | `entryPort` |
| `health_checks #11` | [WorkspaceApplicationHealthCheck](rpc-messages.md#workspaceapplicationhealthcheck) | repeated | `healthChecks` |
| `persistent_mounts #12` | [WorkspaceApplicationMount](rpc-messages.md#workspaceapplicationmount) | repeated | `persistentMounts` |
| `scratch_mounts #13` | [WorkspaceApplicationMount](rpc-messages.md#workspaceapplicationmount) | repeated | `scratchMounts` |
| `secret_inputs #14` | [WorkspaceApplicationSecretInput](rpc-messages.md#workspaceapplicationsecretinput) | repeated | `secretInputs` |
| `config_inputs #15` | [WorkspaceApplicationConfigInput](rpc-messages.md#workspaceapplicationconfiginput) | repeated | `configInputs` |
| `dependencies #16` | [WorkspaceApplicationDependency](rpc-messages.md#workspaceapplicationdependency) | repeated | `dependencies` |
| `exposure_policy #17` | `WorkspaceApplicationRevisionExposurePolicyEnum` | 标量/message；语义必填见规格 | `exposurePolicy` |
| `compute #18` | [WorkspaceApplicationCompute](rpc-messages.md#workspaceapplicationcompute) | 标量/message；语义必填见规格 | `compute` |

## DataMountPolicy

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `mount_name #1` | `string` | 标量/message；语义必填见规格 | `mountName` |
| `concurrent_writers_supported #2` | `bool` | 标量/message；语义必填见规格 | `concurrentWritersSupported` |

## ApplicationOwnedAccessContract

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `mode #1` | `ApplicationOwnedAccessContractModeEnum` | 标量/message；语义必填见规格 | `mode` |
| `login_path #2` | `string` | 标量/message；语义必填见规格 | `loginPath` |
| `logout_path #3` | `string` | 标量/message；语义必填见规格 | `logoutPath` |
| `username_credential_name #4` | `string` | proto3 optional | `usernameCredentialName` |

## CloudPrivateAccessContract

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `mode #1` | `CloudPrivateAccessContractModeEnum` | 标量/message；语义必填见规格 | `mode` |
| `entry_contract #2` | `CloudPrivateAccessContractEntryContractEnum` | 标量/message；语义必填见规格 | `entryContract` |
| `admission_receipt_id #3` | `string` | 标量/message；语义必填见规格 | `admissionReceiptId` |

## AnonymousAccessContract

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `mode #1` | `AnonymousAccessContractModeEnum` | 标量/message；语义必填见规格 | `mode` |

## CreditSource

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `original_wallet_operation_id #1` | `string` | 标量/message；语义必填见规格 | `originalWalletOperationId` |
| `original_subscription_period_id #2` | `string` | 标量/message；语义必填见规格 | `originalSubscriptionPeriodId` |
| `policy_version_id #3` | `string` | 标量/message；语义必填见规格 | `policyVersionId` |
| `credit_receipt_id #4` | `string` | 标量/message；语义必填见规格 | `creditReceiptId` |
| `amount_usd_micros #5` | `int64` | 标量/message；语义必填见规格 | `amountUsdMicros` |

## TenantWorkspaceAction

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `workspace_id #1` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `action #2` | `TenantWorkspaceActionActionEnum` | 标量/message；语义必填见规格 | `action` |
| `operation_id #3` | `string` | 标量/message；语义必填见规格 | `operationId` |
| `operation_owner #4` | `TenantWorkspaceActionOperationOwnerEnum` | 标量/message；语义必填见规格 | `operationOwner` |
| `status #5` | `TenantWorkspaceActionStatusEnum` | 标量/message；语义必填见规格 | `status` |

## TenantWorkspaceSkip

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `workspace_id #1` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `reason #2` | `TenantWorkspaceSkipReasonEnum` | 标量/message；语义必填见规格 | `reason` |

## TenantLifecycleProgress

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `tenant_id #1` | `string` | 标量/message；语义必填见规格 | `tenantId` |
| `operation #2` | [Operation](rpc-messages.md#operation) | 标量/message；语义必填见规格 | `operation` |
| `access_status #3` | `TenantLifecycleProgressAccessStatusEnum` | 标量/message；语义必填见规格 | `accessStatus` |
| `workspace_actions #4` | [TenantWorkspaceAction](rpc-messages.md#tenantworkspaceaction) | repeated | `workspaceActions` |
| `skipped #5` | [TenantWorkspaceSkip](rpc-messages.md#tenantworkspaceskip) | repeated | `skipped` |

## BuildRecipeContractOutputImageCommand

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `entrypoint #1` | `string` | repeated | `entrypoint` |
| `cmd #2` | `string` | repeated | `cmd` |

## UpdateRenewalSettingsRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `renewal_mode #1` | `UpdateRenewalSettingsRequestRenewalModeEnum` | 标量/message；语义必填见规格 | `renewalMode` |
| `expected_renewal_settings_version #2` | `int64` | 标量/message；语义必填见规格 | `expectedRenewalSettingsVersion` |
| `automatic_renewal_consent #3` | `bool` | proto3 optional | `automaticRenewalConsent` |

## WorkspaceApplicationEntry

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `service_name #1` | `string` | proto3 optional | `serviceName` |
| `port #2` | `int32` | proto3 optional | `port` |
| `url #3` | `string` | proto3 optional | `url` |

## PackageFormatContractReference

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `owner #1` | `PackageFormatContractReferenceOwnerEnum` | 标量/message；语义必填见规格 | `owner` |
| `format_version #2` | `string` | 标量/message；语义必填见规格 | `formatVersion` |
| `schema_object_ref #3` | `string` | 标量/message；语义必填见规格 | `schemaObjectRef` |
| `schema_digest #4` | `string` | 标量/message；语义必填见规格 | `schemaDigest` |
| `validator_artifact #5` | [ArtifactReference](rpc-messages.md#artifactreference) | 标量/message；语义必填见规格 | `validatorArtifact` |
| `admission_receipt_id #6` | `string` | 标量/message；语义必填见规格 | `admissionReceiptId` |

## WorkspaceApplicationCredentials

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `workspace_id #1` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `runtime_instance_id #2` | `string` | 标量/message；语义必填见规格 | `runtimeInstanceId` |
| `username #3` | `string` | 标量/message；语义必填见规格 | `username` |
| `password #4` | `string` | 标量/message；语义必填见规格 | `password` |

## UpgradePlanRules

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `kind #1` | `UpgradePlanRulesKindEnum` | 标量/message；语义必填见规格 | `kind` |
| `effective_when #2` | `UpgradePlanRulesEffectiveWhenEnum` | 标量/message；语义必填见规格 | `effectiveWhen` |
| `preserve_paid_period #3` | `bool` | 标量/message；语义必填见规格 | `preservePaidPeriod` |
| `old_price_source #4` | `UpgradePlanRulesOldPriceSourceEnum` | 标量/message；语义必填见规格 | `oldPriceSource` |
| `charge_clock #5` | `UpgradePlanRulesChargeClockEnum` | 标量/message；语义必填见规格 | `chargeClock` |
| `time_unit #6` | `UpgradePlanRulesTimeUnitEnum` | 标量/message；语义必填见规格 | `timeUnit` |
| `charge_rounding #7` | `UpgradePlanRulesChargeRoundingEnum` | 标量/message；语义必填见规格 | `chargeRounding` |
| `zero_charge #8` | `UpgradePlanRulesZeroChargeEnum` | 标量/message；语义必填见规格 | `zeroCharge` |
| `known_failure_compensation #9` | `UpgradePlanRulesKnownFailureCompensationEnum` | 标量/message；语义必填见规格 | `knownFailureCompensation` |
| `unknown_outcome #10` | `UpgradePlanRulesUnknownOutcomeEnum` | 标量/message；语义必填见规格 | `unknownOutcome` |
| `irreversible_residual_cost_owner #11` | `UpgradePlanRulesIrreversibleResidualCostOwnerEnum` | 标量/message；语义必填见规格 | `irreversibleResidualCostOwner` |
| `supplement_delete_refund #12` | `UpgradePlanRulesSupplementDeleteRefundEnum` | 标量/message；语义必填见规格 | `supplementDeleteRefund` |

## DowngradePlanRules

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `kind #1` | `DowngradePlanRulesKindEnum` | 标量/message；语义必填见规格 | `kind` |
| `planned_boundary #2` | `DowngradePlanRulesPlannedBoundaryEnum` | 标量/message；语义必填见规格 | `plannedBoundary` |
| `current_period_refund #3` | `DowngradePlanRulesCurrentPeriodRefundEnum` | 标量/message；语义必填见规格 | `currentPeriodRefund` |
| `next_period_price #4` | `DowngradePlanRulesNextPeriodPriceEnum` | 标量/message；语义必填见规格 | `nextPeriodPrice` |
| `requires_confirmed_next_period_payment #5` | `bool` | 标量/message；语义必填见规格 | `requiresConfirmedNextPeriodPayment` |
| `cancel_before #6` | `DowngradePlanRulesCancelBeforeEnum` | 标量/message；语义必填见规格 | `cancelBefore` |
| `early_paid_change #7` | `DowngradePlanRulesEarlyPaidChangeEnum` | 标量/message；语义必填见规格 | `earlyPaidChange` |
| `manual_unpaid_boundary #8` | `DowngradePlanRulesManualUnpaidBoundaryEnum` | 标量/message；语义必填见规格 | `manualUnpaidBoundary` |
| `known_failure_compensation #9` | `DowngradePlanRulesKnownFailureCompensationEnum` | 标量/message；语义必填见规格 | `knownFailureCompensation` |
| `fallback #10` | `DowngradePlanRulesFallbackEnum` | 标量/message；语义必填见规格 | `fallback` |

## PlanChangePolicy

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `version #1` | `PlanChangePolicyVersionEnum` | 标量/message；语义必填见规格 | `version` |
| `approval_status #2` | `PlanChangePolicyApprovalStatusEnum` | 标量/message；语义必填见规格 | `approvalStatus` |
| `upgrade #3` | [UpgradePlanRules](rpc-messages.md#upgradeplanrules) | 标量/message；语义必填见规格 | `upgrade` |
| `downgrade #4` | [DowngradePlanRules](rpc-messages.md#downgradeplanrules) | 标量/message；语义必填见规格 | `downgrade` |
| `classification #5` | `PlanChangePolicyClassificationEnum` | 标量/message；语义必填见规格 | `classification` |
| `mixed_or_incomparable_transition #6` | `PlanChangePolicyMixedOrIncomparableTransitionEnum` | 标量/message；语义必填见规格 | `mixedOrIncomparableTransition` |
| `no_op_transition #7` | `PlanChangePolicyNoOpTransitionEnum` | 标量/message；语义必填见规格 | `noOpTransition` |
| `storage_shrink #8` | `PlanChangePolicyStorageShrinkEnum` | 标量/message；语义必填见规格 | `storageShrink` |
| `concurrency #9` | `PlanChangePolicyConcurrencyEnum` | 标量/message；语义必填见规格 | `concurrency` |
| `cancel_and_replace #10` | `PlanChangePolicyCancelAndReplaceEnum` | 标量/message；语义必填见规格 | `cancelAndReplace` |
| `base_refund_policy #11` | `PlanChangePolicyBaseRefundPolicyEnum` | 标量/message；语义必填见规格 | `baseRefundPolicy` |
| `provider_execution_plan #12` | `PlanChangePolicyProviderExecutionPlanEnum` | 标量/message；语义必填见规格 | `providerExecutionPlan` |

## UpgradeProration

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `period_milliseconds #1` | `int64` | 标量/message；语义必填见规格 | `periodMilliseconds` |
| `remaining_milliseconds #2` | `int64` | 标量/message；语义必填见规格 | `remainingMilliseconds` |
| `price_delta_usd_micros #3` | `int64` | 标量/message；语义必填见规格 | `priceDeltaUsdMicros` |
| `rounding #4` | `UpgradeProrationRoundingEnum` | 标量/message；语义必填见规格 | `rounding` |

## NextPeriodPlanQuote

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `period_start #1` | `Timestamp` | 标量/message；语义必填见规格 | `periodStart` |
| `period_end #2` | `Timestamp` | 标量/message；语义必填见规格 | `periodEnd` |
| `total_usd_micros #3` | `int64` | 标量/message；语义必填见规格 | `totalUsdMicros` |
| `price_policy_version_id #4` | `string` | 标量/message；语义必填见规格 | `pricePolicyVersionId` |

## PlanChangeCalculation

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `policy_version #1` | `PlanChangeCalculationPolicyVersionEnum` | 标量/message；语义必填见规格 | `policyVersion` |
| `kind #2` | `PlanChangeCalculationKindEnum` | 标量/message；语义必填见规格 | `kind` |
| `transition_id #3` | `string` | 标量/message；语义必填见规格 | `transitionId` |
| `source_subscription_id #4` | `string` | 标量/message；语义必填见规格 | `sourceSubscriptionId` |
| `source_subscription_version #5` | `int64` | 标量/message；语义必填见规格 | `sourceSubscriptionVersion` |
| `source_period_id #6` | `string` | 标量/message；语义必填见规格 | `sourcePeriodId` |
| `source_compute_plan_id #7` | `string` | 标量/message；语义必填见规格 | `sourceComputePlanId` |
| `source_storage_plan_id #8` | `string` | 标量/message；语义必填见规格 | `sourceStoragePlanId` |
| `target_compute_plan_id #9` | `string` | 标量/message；语义必填见规格 | `targetComputePlanId` |
| `target_storage_plan_id #10` | `string` | 标量/message；语义必填见规格 | `targetStoragePlanId` |
| `source_price_policy_version_id #11` | `string` | 标量/message；语义必填见规格 | `sourcePricePolicyVersionId` |
| `target_price_policy_version_id #12` | `string` | 标量/message；语义必填见规格 | `targetPricePolicyVersionId` |
| `source_monthly_usd_micros #13` | `int64` | 标量/message；语义必填见规格 | `sourceMonthlyUsdMicros` |
| `target_monthly_usd_micros #14` | `int64` | 标量/message；语义必填见规格 | `targetMonthlyUsdMicros` |
| `quote_at #15` | `Timestamp` | 标量/message；语义必填见规格 | `quoteAt` |
| `period_start #16` | `Timestamp` | 标量/message；语义必填见规格 | `periodStart` |
| `period_end #17` | `Timestamp` | 标量/message；语义必填见规格 | `periodEnd` |
| `charge_usd_micros #18` | `int64` | 标量/message；语义必填见规格 | `chargeUsdMicros` |
| `planned_effective_at #19` | `Timestamp` | 标量/message；语义必填见规格 | `plannedEffectiveAt` |
| `upgrade_proration #20` | [UpgradeProration](rpc-messages.md#upgradeproration) | 标量/message；语义必填见规格 | `upgradeProration` |
| `next_period #21` | [NextPeriodPlanQuote](rpc-messages.md#nextperiodplanquote) | 标量/message；语义必填见规格 | `nextPeriod` |
| `execution_plan_id #22` | `string` | 标量/message；语义必填见规格 | `executionPlanId` |
| `execution_plan_digest #23` | `string` | 标量/message；语义必填见规格 | `executionPlanDigest` |
| `quote_at_milliseconds #24` | `int64` | 标量/message；语义必填见规格 | `quoteAtMilliseconds` |
| `period_start_milliseconds #25` | `int64` | 标量/message；语义必填见规格 | `periodStartMilliseconds` |
| `period_end_milliseconds #26` | `int64` | 标量/message；语义必填见规格 | `periodEndMilliseconds` |
| `source_financial_snapshot_digest #27` | `string` | 标量/message；语义必填见规格 | `sourceFinancialSnapshotDigest` |

## PlanChange

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `kind #3` | `PlanChangeKindEnum` | 标量/message；语义必填见规格 | `kind` |
| `status #4` | `PlanChangeStatusEnum` | 标量/message；语义必填见规格 | `status` |
| `source_compute_plan_id #5` | `string` | 标量/message；语义必填见规格 | `sourceComputePlanId` |
| `source_storage_plan_id #6` | `string` | 标量/message；语义必填见规格 | `sourceStoragePlanId` |
| `target_compute_plan_id #7` | `string` | 标量/message；语义必填见规格 | `targetComputePlanId` |
| `target_storage_plan_id #8` | `string` | 标量/message；语义必填见规格 | `targetStoragePlanId` |
| `source_price_policy_version_id #9` | `string` | 标量/message；语义必填见规格 | `sourcePricePolicyVersionId` |
| `target_price_policy_version_id #10` | `string` | 标量/message；语义必填见规格 | `targetPricePolicyVersionId` |
| `source_subscription_id #11` | `string` | 标量/message；语义必填见规格 | `sourceSubscriptionId` |
| `source_subscription_version #12` | `int64` | 标量/message；语义必填见规格 | `sourceSubscriptionVersion` |
| `source_period_id #13` | `string` | 标量/message；语义必填见规格 | `sourcePeriodId` |
| `source_monthly_usd_micros #14` | `int64` | 标量/message；语义必填见规格 | `sourceMonthlyUsdMicros` |
| `target_monthly_usd_micros #15` | `int64` | 标量/message；语义必填见规格 | `targetMonthlyUsdMicros` |
| `quote_id #16` | `string` | 标量/message；语义必填见规格 | `quoteId` |
| `policy_version #17` | `PlanChangePolicyVersionEnum` | 标量/message；语义必填见规格 | `policyVersion` |
| `quote_at #18` | `Timestamp` | 标量/message；语义必填见规格 | `quoteAt` |
| `period_start #19` | `Timestamp` | 标量/message；语义必填见规格 | `periodStart` |
| `period_end #20` | `Timestamp` | 标量/message；语义必填见规格 | `periodEnd` |
| `charge_usd_micros #21` | `int64` | 标量/message；语义必填见规格 | `chargeUsdMicros` |
| `planned_effective_at #22` | `Timestamp` | 标量/message；语义必填见规格 | `plannedEffectiveAt` |
| `applied_at #23` | `Timestamp` | 标量/message；语义必填见规格 | `appliedAt` |
| `operation_id #24` | `string` | 标量/message；语义必填见规格 | `operationId` |
| `execution_operation_id #25` | `string` | proto3 optional | `executionOperationId` |
| `cancellation_operation_id #26` | `string` | proto3 optional | `cancellationOperationId` |
| `charge_operation_id #27` | `string` | proto3 optional | `chargeOperationId` |
| `charge_status #28` | `PlanChangeChargeStatusEnum` | 标量/message；语义必填见规格 | `chargeStatus` |
| `next_period_start #29` | `Timestamp` | 标量/message；语义必填见规格 | `nextPeriodStart` |
| `next_period_end #30` | `Timestamp` | 标量/message；语义必填见规格 | `nextPeriodEnd` |
| `next_period_charge_usd_micros #31` | `int64` | proto3 optional | `nextPeriodChargeUsdMicros` |
| `next_period_obligation_id #32` | `string` | proto3 optional | `nextPeriodObligationId` |
| `next_period_charge_operation_id #33` | `string` | proto3 optional | `nextPeriodChargeOperationId` |
| `next_period_charge_status #34` | `PlanChangeNextPeriodChargeStatusEnum` | proto3 optional | `nextPeriodChargeStatus` |
| `refund_operation_ids #35` | `string` | repeated | `refundOperationIds` |
| `schedule_version #36` | `int64` | 标量/message；语义必填见规格 | `scheduleVersion` |
| `observation_result #37` | `PlanChangeObservationResultEnum` | 标量/message；语义必填见规格 | `observationResult` |
| `error_code #38` | `ErrorCodeEnum` | proto3 optional | `errorCode` |
| `cancellable #39` | `bool` | 标量/message；语义必填见规格 | `cancellable` |
| `created_at #40` | `Timestamp` | 标量/message；语义必填见规格 | `createdAt` |
| `updated_at #41` | `Timestamp` | 标量/message；语义必填见规格 | `updatedAt` |
| `delivery_outcome #42` | `PlanChangeDeliveryOutcomeEnum` | 标量/message；语义必填见规格 | `deliveryOutcome` |
| `resource_outcome #43` | `PlanChangeResourceOutcomeEnum` | 标量/message；语义必填见规格 | `resourceOutcome` |
| `runtime_readback_requirement #44` | `PlanChangeRuntimeReadbackRequirementEnum` | 标量/message；语义必填见规格 | `runtimeReadbackRequirement` |
| `current_requirement_validation #45` | `PlanChangeCurrentRequirementValidationEnum` | 标量/message；语义必填见规格 | `currentRequirementValidation` |
| `risk_code #46` | `ErrorCodeEnum` | proto3 optional | `riskCode` |
| `last_validated_at #47` | `Timestamp` | 标量/message；语义必填见规格 | `lastValidatedAt` |
| `execution_plan_id #48` | `string` | 标量/message；语义必填见规格 | `executionPlanId` |
| `execution_plan_digest #49` | `string` | 标量/message；语义必填见规格 | `executionPlanDigest` |
| `quote_at_milliseconds #50` | `int64` | 标量/message；语义必填见规格 | `quoteAtMilliseconds` |
| `period_start_milliseconds #51` | `int64` | 标量/message；语义必填见规格 | `periodStartMilliseconds` |
| `period_end_milliseconds #52` | `int64` | 标量/message；语义必填见规格 | `periodEndMilliseconds` |
| `source_financial_snapshot_digest #53` | `string` | 标量/message；语义必填见规格 | `sourceFinancialSnapshotDigest` |

## PlanChangePage

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `items #1` | [PlanChange](rpc-messages.md#planchange) | repeated | `items` |
| `next_cursor #2` | `string` | proto3 optional | `nextCursor` |

## CancelPlanChangeRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `expected_schedule_version #1` | `int64` | 标量/message；语义必填见规格 | `expectedScheduleVersion` |
| `reason #2` | `string` | 标量/message；语义必填见规格 | `reason` |

## PlanChangeEvidence

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `schema_version #1` | `int32` | 标量/message；语义必填见规格 | `schemaVersion` |
| `plan_change_id #2` | `string` | 标量/message；语义必填见规格 | `planChangeId` |
| `workspace_id #3` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `kind #4` | `PlanChangeEvidenceKindEnum` | 标量/message；语义必填见规格 | `kind` |
| `policy_version #5` | `PlanChangeEvidencePolicyVersionEnum` | 标量/message；语义必填见规格 | `policyVersion` |
| `quote_id #6` | `string` | 标量/message；语义必填见规格 | `quoteId` |
| `source_subscription_id #7` | `string` | 标量/message；语义必填见规格 | `sourceSubscriptionId` |
| `source_subscription_version #8` | `int64` | 标量/message；语义必填见规格 | `sourceSubscriptionVersion` |
| `source_period_id #9` | `string` | 标量/message；语义必填见规格 | `sourcePeriodId` |
| `source_compute_plan_id #10` | `string` | 标量/message；语义必填见规格 | `sourceComputePlanId` |
| `source_storage_plan_id #11` | `string` | 标量/message；语义必填见规格 | `sourceStoragePlanId` |
| `target_compute_plan_id #12` | `string` | 标量/message；语义必填见规格 | `targetComputePlanId` |
| `target_storage_plan_id #13` | `string` | 标量/message；语义必填见规格 | `targetStoragePlanId` |
| `source_price_policy_version_id #14` | `string` | 标量/message；语义必填见规格 | `sourcePricePolicyVersionId` |
| `target_price_policy_version_id #15` | `string` | 标量/message；语义必填见规格 | `targetPricePolicyVersionId` |
| `quote_at #16` | `Timestamp` | 标量/message；语义必填见规格 | `quoteAt` |
| `period_start #17` | `Timestamp` | 标量/message；语义必填见规格 | `periodStart` |
| `period_end #18` | `Timestamp` | 标量/message；语义必填见规格 | `periodEnd` |
| `charge_usd_micros #19` | `int64` | 标量/message；语义必填见规格 | `chargeUsdMicros` |
| `charge_operation_id #20` | `string` | proto3 optional | `chargeOperationId` |
| `next_period_obligation_id #21` | `string` | proto3 optional | `nextPeriodObligationId` |
| `execution_epoch #22` | `int64` | proto3 optional | `executionEpoch` |
| `resource_observation_receipt_id #23` | `string` | proto3 optional | `resourceObservationReceiptId` |
| `runtime_readiness_receipt_id #24` | `string` | proto3 optional | `runtimeReadinessReceiptId` |
| `failure_receipt_id #25` | `string` | proto3 optional | `failureReceiptId` |
| `applied_at #26` | `Timestamp` | 标量/message；语义必填见规格 | `appliedAt` |
| `outcome #27` | `PlanChangeEvidenceOutcomeEnum` | 标量/message；语义必填见规格 | `outcome` |
| `retained_irreversible_resources #28` | `bool` | 标量/message；语义必填见规格 | `retainedIrreversibleResources` |
| `delivery_outcome #29` | `PlanChangeEvidenceDeliveryOutcomeEnum` | 标量/message；语义必填见规格 | `deliveryOutcome` |
| `resource_outcome #30` | `PlanChangeEvidenceResourceOutcomeEnum` | 标量/message；语义必填见规格 | `resourceOutcome` |
| `runtime_readback_requirement #31` | `PlanChangeEvidenceRuntimeReadbackRequirementEnum` | 标量/message；语义必填见规格 | `runtimeReadbackRequirement` |
| `quote_at_milliseconds #32` | `int64` | 标量/message；语义必填见规格 | `quoteAtMilliseconds` |
| `period_start_milliseconds #33` | `int64` | 标量/message；语义必填见规格 | `periodStartMilliseconds` |
| `period_end_milliseconds #34` | `int64` | 标量/message；语义必填见规格 | `periodEndMilliseconds` |
| `source_financial_snapshot_digest #35` | `string` | 标量/message；语义必填见规格 | `sourceFinancialSnapshotDigest` |

## SupplementalRefundEvidence

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `schema_version #1` | `int32` | 标量/message；语义必填见规格 | `schemaVersion` |
| `purpose #2` | `SupplementalRefundEvidencePurposeEnum` | 标量/message；语义必填见规格 | `purpose` |
| `workspace_id #3` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `plan_change_id #4` | `string` | 标量/message；语义必填见规格 | `planChangeId` |
| `original_charge_operation_id #5` | `string` | 标量/message；语义必填见规格 | `originalChargeOperationId` |
| `original_charge_receipt_id #6` | `string` | 标量/message；语义必填见规格 | `originalChargeReceiptId` |
| `original_confirmed_usd_micros #7` | `int64` | 标量/message；语义必填见规格 | `originalConfirmedUsdMicros` |
| `requested_refund_usd_micros #8` | `int64` | 标量/message；语义必填见规格 | `requestedRefundUsdMicros` |
| `policy_version #9` | `SupplementalRefundEvidencePolicyVersionEnum` | 标量/message；语义必填见规格 | `policyVersion` |
| `coverage_start #10` | `Timestamp` | 标量/message；语义必填见规格 | `coverageStart` |
| `coverage_end #11` | `Timestamp` | 标量/message；语义必填见规格 | `coverageEnd` |
| `delete_confirmed_at #12` | `Timestamp` | 标量/message；语义必填见规格 | `deleteConfirmedAt` |
| `failure_receipt_id #13` | `string` | proto3 optional | `failureReceiptId` |
| `deletion_receipt_id #14` | `string` | proto3 optional | `deletionReceiptId` |
| `fence_receipt_id #15` | `string` | proto3 optional | `fenceReceiptId` |
| `coverage_start_milliseconds #16` | `int64` | 标量/message；语义必填见规格 | `coverageStartMilliseconds` |
| `delete_confirmed_at_milliseconds #17` | `int64` | proto3 optional | `deleteConfirmedAtMilliseconds` |
| `coverage_end_milliseconds #18` | `int64` | 标量/message；语义必填见规格 | `coverageEndMilliseconds` |

## GetLoginContextRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |

## LoginRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [LoginRequest](rpc-messages.md#loginrequest) | 标量/message；语义必填见规格 | `body` |

## GetSessionRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |

## LogoutRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |

## GetTenantRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |

## ListMembersRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `query_cursor #2` | `string` | proto3 optional | `queryCursor` |
| `query_limit #3` | `int32` | proto3 optional | `queryLimit` |

## ListInvitationsRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `query_cursor #2` | `string` | proto3 optional | `queryCursor` |
| `query_limit #3` | `int32` | proto3 optional | `queryLimit` |

## InviteMemberRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [InviteMemberRequest](rpc-messages.md#invitememberrequest) | 标量/message；语义必填见规格 | `body` |

## AcceptInvitationRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `invitation_id #2` | `string` | 标量/message；语义必填见规格 | `invitationId` |

## RevokeInvitationRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `invitation_id #2` | `string` | 标量/message；语义必填见规格 | `invitationId` |

## UpdateMemberRoleRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [UpdateMemberRoleRequest](rpc-messages.md#updatememberrolerequest) | 标量/message；语义必填见规格 | `body` |
| `member_id #3` | `string` | 标量/message；语义必填见规格 | `memberId` |

## RemoveMemberRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `member_id #2` | `string` | 标量/message；语义必填见规格 | `memberId` |

## ListNamespacesRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `query_cursor #2` | `string` | proto3 optional | `queryCursor` |
| `query_limit #3` | `int32` | proto3 optional | `queryLimit` |

## CreateNamespaceRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [NamespaceWriteRequest](rpc-messages.md#namespacewriterequest) | 标量/message；语义必填见规格 | `body` |

## UpdateNamespaceRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [NamespaceWriteRequest](rpc-messages.md#namespacewriterequest) | 标量/message；语义必填见规格 | `body` |
| `namespace_id #3` | `string` | 标量/message；语义必填见规格 | `namespaceId` |

## ArchiveNamespaceRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `namespace_id #2` | `string` | 标量/message；语义必填见规格 | `namespaceId` |

## ListPackagesRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `query_cursor #2` | `string` | proto3 optional | `queryCursor` |
| `query_limit #3` | `int32` | proto3 optional | `queryLimit` |
| `query_visibility #4` | `ListPackagesRpcRequestVisibilityEnum` | proto3 optional | `queryVisibility` |
| `query_namespace_id #5` | `string` | proto3 optional | `queryNamespaceId` |
| `query_status #6` | `ListPackagesRpcRequestStatusEnum` | proto3 optional | `queryStatus` |
| `query_search #7` | `string` | proto3 optional | `querySearch` |

## CreatePackageRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [CreatePackageRequest](rpc-messages.md#createpackagerequest) | 标量/message；语义必填见规格 | `body` |

## GetPackageRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `package_id #2` | `string` | 标量/message；语义必填见规格 | `packageId` |

## UpdatePackageRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [UpdatePackageRequest](rpc-messages.md#updatepackagerequest) | 标量/message；语义必填见规格 | `body` |
| `package_id #3` | `string` | 标量/message；语义必填见规格 | `packageId` |

## ArchivePackageRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `package_id #2` | `string` | 标量/message；语义必填见规格 | `packageId` |

## CreateUploadRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [CreateUploadRequest](rpc-messages.md#createuploadrequest) | 标量/message；语义必填见规格 | `body` |
| `package_id #3` | `string` | 标量/message；语义必填见规格 | `packageId` |

## GetUploadRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `upload_id #2` | `string` | 标量/message；语义必填见规格 | `uploadId` |

## CreateUploadPartRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [CreateUploadPartRequest](rpc-messages.md#createuploadpartrequest) | 标量/message；语义必填见规格 | `body` |
| `upload_id #3` | `string` | 标量/message；语义必填见规格 | `uploadId` |

## CompleteUploadRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [CompleteUploadRequest](rpc-messages.md#completeuploadrequest) | 标量/message；语义必填见规格 | `body` |
| `upload_id #3` | `string` | 标量/message；语义必填见规格 | `uploadId` |

## ListPackageVersionsRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `package_id #2` | `string` | 标量/message；语义必填见规格 | `packageId` |
| `query_cursor #3` | `string` | proto3 optional | `queryCursor` |
| `query_limit #4` | `int32` | proto3 optional | `queryLimit` |

## GetPackageVersionRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `package_version_id #2` | `string` | 标量/message；语义必填见规格 | `packageVersionId` |

## CreateBuildRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [CreateBuildRequest](rpc-messages.md#createbuildrequest) | 标量/message；语义必填见规格 | `body` |

## ListBuildsRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `query_cursor #2` | `string` | proto3 optional | `queryCursor` |
| `query_limit #3` | `int32` | proto3 optional | `queryLimit` |
| `query_package_version_id #4` | `string` | proto3 optional | `queryPackageVersionId` |

## GetBuildRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `build_id #2` | `string` | 标量/message；语义必填见规格 | `buildId` |

## ListBuildLogsRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `build_id #2` | `string` | 标量/message；语义必填见规格 | `buildId` |
| `query_cursor #3` | `string` | proto3 optional | `queryCursor` |
| `query_limit #4` | `int32` | proto3 optional | `queryLimit` |

## RetryBuildRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `build_id #2` | `string` | 标量/message；语义必填见规格 | `buildId` |

## ListCapabilityVersionsRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `query_cursor #2` | `string` | proto3 optional | `queryCursor` |
| `query_limit #3` | `int32` | proto3 optional | `queryLimit` |
| `query_package_id #4` | `string` | proto3 optional | `queryPackageId` |
| `query_status #5` | `ListCapabilityVersionsRpcRequestStatusEnum` | proto3 optional | `queryStatus` |

## GetCapabilityVersionRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `capability_version_id #2` | `string` | 标量/message；语义必填见规格 | `capabilityVersionId` |

## DeleteCapabilityVersionRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `capability_version_id #2` | `string` | 标量/message；语义必填见规格 | `capabilityVersionId` |

## PublishOfficialPackageRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [PublishPackageRequest](rpc-messages.md#publishpackagerequest) | 标量/message；语义必填见规格 | `body` |
| `package_id #3` | `string` | 标量/message；语义必填见规格 | `packageId` |

## CreateQuoteRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [QuoteRequest](rpc-messages.md#quoterequest) | 标量/message；语义必填见规格 | `body` |

## GetQuoteRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `quote_id #2` | `string` | 标量/message；语义必填见规格 | `quoteId` |

## CreateWorkspaceRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [CreateWorkspaceRequest](rpc-messages.md#createworkspacerequest) | 标量/message；语义必填见规格 | `body` |

## ListWorkspacesRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `query_cursor #2` | `string` | proto3 optional | `queryCursor` |
| `query_limit #3` | `int32` | proto3 optional | `queryLimit` |

## GetWorkspaceRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |

## DeleteWorkspaceRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [DeleteWorkspaceRequest](rpc-messages.md#deleteworkspacerequest) | 标量/message；语义必填见规格 | `body` |
| `workspace_id #3` | `string` | 标量/message；语义必填见规格 | `workspaceId` |

## GetWorkspaceAccessRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |

## GetWorkspaceModelsRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |

## UpdateWorkspaceModelsRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [UpdateWorkspaceModelsRequest](rpc-messages.md#updateworkspacemodelsrequest) | 标量/message；语义必填见规格 | `body` |
| `workspace_id #3` | `string` | 标量/message；语义必填见规格 | `workspaceId` |

## ListDeploymentsRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `query_cursor #3` | `string` | proto3 optional | `queryCursor` |
| `query_limit #4` | `int32` | proto3 optional | `queryLimit` |

## GetDeploymentRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `deployment_id #3` | `string` | 标量/message；语义必填见规格 | `deploymentId` |

## UpdateWorkspaceVersionRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [UpdateWorkspaceVersionRequest](rpc-messages.md#updateworkspaceversionrequest) | 标量/message；语义必填见规格 | `body` |
| `workspace_id #3` | `string` | 标量/message；语义必填见规格 | `workspaceId` |

## RollbackWorkspaceRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [RollbackWorkspaceRequest](rpc-messages.md#rollbackworkspacerequest) | 标量/message；语义必填见规格 | `body` |
| `workspace_id #3` | `string` | 标量/message；语义必填见规格 | `workspaceId` |

## ResizeWorkspaceRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [ApplyQuoteRequest](rpc-messages.md#applyquoterequest) | 标量/message；语义必填见规格 | `body` |
| `workspace_id #3` | `string` | 标量/message；语义必填见规格 | `workspaceId` |

## RenewWorkspaceRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [ApplyQuoteRequest](rpc-messages.md#applyquoterequest) | 标量/message；语义必填见规格 | `body` |
| `workspace_id #3` | `string` | 标量/message；语义必填见规格 | `workspaceId` |

## GetSubscriptionRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |

## GetWorkspaceDeletionRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |

## ListWorkspaceTransactionsRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `query_cursor #3` | `string` | proto3 optional | `queryCursor` |
| `query_limit #4` | `int32` | proto3 optional | `queryLimit` |

## GetOperationRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `owner #2` | `OperationOwnerEnum` | 标量/message；语义必填见规格 | `owner` |
| `operation_id #3` | `string` | 标量/message；语义必填见规格 | `operationId` |

## GetWalletRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |

## ListUsageRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `query_cursor #2` | `string` | proto3 optional | `queryCursor` |
| `query_limit #3` | `int32` | proto3 optional | `queryLimit` |
| `query_from #4` | `Timestamp` | 标量/message；语义必填见规格 | `queryFrom` |
| `query_until #5` | `Timestamp` | 标量/message；语义必填见规格 | `queryUntil` |
| `query_model_id #6` | `string` | proto3 optional | `queryModelId` |

## ListGatewayKeysRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `query_cursor #2` | `string` | proto3 optional | `queryCursor` |
| `query_limit #3` | `int32` | proto3 optional | `queryLimit` |

## CreateGatewayKeyRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [CreateGatewayKeyRequest](rpc-messages.md#creategatewaykeyrequest) | 标量/message；语义必填见规格 | `body` |

## RevealGatewayKeyRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `key_id #2` | `string` | 标量/message；语义必填见规格 | `keyId` |

## RevokeGatewayKeyRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `key_id #2` | `string` | 标量/message；语义必填见规格 | `keyId` |

## ListRechargeRecordsRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `query_cursor #2` | `string` | proto3 optional | `queryCursor` |
| `query_limit #3` | `int32` | proto3 optional | `queryLimit` |
| `query_tenant_id #4` | `string` | proto3 optional | `queryTenantId` |

## ListTenantsRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `query_cursor #2` | `string` | proto3 optional | `queryCursor` |
| `query_limit #3` | `int32` | proto3 optional | `queryLimit` |

## CreateTenantRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [CreateTenantRequest](rpc-messages.md#createtenantrequest) | 标量/message；语义必填见规格 | `body` |

## GetAdminTenantRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `tenant_id #2` | `string` | 标量/message；语义必填见规格 | `tenantId` |

## DeleteTenantRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [DeleteTenantRequest](rpc-messages.md#deletetenantrequest) | 标量/message；语义必填见规格 | `body` |
| `tenant_id #3` | `string` | 标量/message；语义必填见规格 | `tenantId` |

## BindTenantWalletRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [BindTenantWalletRequest](rpc-messages.md#bindtenantwalletrequest) | 标量/message；语义必填见规格 | `body` |
| `tenant_id #3` | `string` | 标量/message；语义必填见规格 | `tenantId` |

## SuspendTenantRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [TenantActionRequest](rpc-messages.md#tenantactionrequest) | 标量/message；语义必填见规格 | `body` |
| `tenant_id #3` | `string` | 标量/message；语义必填见规格 | `tenantId` |

## RestoreTenantRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [TenantActionRequest](rpc-messages.md#tenantactionrequest) | 标量/message；语义必填见规格 | `body` |
| `tenant_id #3` | `string` | 标量/message；语义必填见规格 | `tenantId` |

## GetTenantAssetCustodyRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `tenant_id #2` | `string` | 标量/message；语义必填见规格 | `tenantId` |

## ListAdminOperationsRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `query_cursor #2` | `string` | proto3 optional | `queryCursor` |
| `query_limit #3` | `int32` | proto3 optional | `queryLimit` |
| `query_owner #4` | `OperationOwnerEnum` | 标量/message；语义必填见规格 | `queryOwner` |
| `query_tenant_id #5` | `string` | proto3 optional | `queryTenantId` |

## ReconcileOperationRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [ReconcileOperationRequest](rpc-messages.md#reconcileoperationrequest) | 标量/message；语义必填见规格 | `body` |
| `owner #3` | `OperationOwnerEnum` | 标量/message；语义必填见规格 | `owner` |
| `operation_id #4` | `string` | 标量/message；语义必填见规格 | `operationId` |

## ListAuditEventsRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `query_cursor #2` | `string` | proto3 optional | `queryCursor` |
| `query_limit #3` | `int32` | proto3 optional | `queryLimit` |
| `query_tenant_id #4` | `string` | proto3 optional | `queryTenantId` |

## ListReceiptsRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `query_cursor #2` | `string` | proto3 optional | `queryCursor` |
| `query_limit #3` | `int32` | proto3 optional | `queryLimit` |
| `query_artifact_digest #4` | `string` | proto3 optional | `queryArtifactDigest` |

## GetReceiptRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `receipt_id #2` | `string` | 标量/message；语义必填见规格 | `receiptId` |

## ListQualificationsRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `query_cursor #2` | `string` | proto3 optional | `queryCursor` |
| `query_limit #3` | `int32` | proto3 optional | `queryLimit` |
| `query_artifact_digest #4` | `string` | proto3 optional | `queryArtifactDigest` |

## ListRuntimeVersionsRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `query_cursor #2` | `string` | proto3 optional | `queryCursor` |
| `query_limit #3` | `int32` | proto3 optional | `queryLimit` |

## ListWebuiVersionsRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `query_cursor #2` | `string` | proto3 optional | `queryCursor` |
| `query_limit #3` | `int32` | proto3 optional | `queryLimit` |

## ListComputePlansRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `query_cursor #2` | `string` | proto3 optional | `queryCursor` |
| `query_limit #3` | `int32` | proto3 optional | `queryLimit` |
| `query_storage_plan_id #4` | `string` | proto3 optional | `queryStoragePlanId` |

## ListStoragePlansRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `query_cursor #2` | `string` | proto3 optional | `queryCursor` |
| `query_limit #3` | `int32` | proto3 optional | `queryLimit` |
| `query_compute_plan_id #4` | `string` | proto3 optional | `queryComputePlanId` |

## ListModelsRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `query_cursor #2` | `string` | proto3 optional | `queryCursor` |
| `query_limit #3` | `int32` | proto3 optional | `queryLimit` |

## RegisterRuntimeVersionRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [RegisterRuntimeVersionRequest](rpc-messages.md#registerruntimeversionrequest) | 标量/message；语义必填见规格 | `body` |

## SetRuntimeVersionStatusRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [CatalogStatusRequest](rpc-messages.md#catalogstatusrequest) | 标量/message；语义必填见规格 | `body` |
| `version_id #3` | `string` | 标量/message；语义必填见规格 | `versionId` |

## RegisterWebuiVersionRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [RegisterWebuiVersionRequest](rpc-messages.md#registerwebuiversionrequest) | 标量/message；语义必填见规格 | `body` |

## SetWebuiVersionStatusRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [CatalogStatusRequest](rpc-messages.md#catalogstatusrequest) | 标量/message；语义必填见规格 | `body` |
| `version_id #3` | `string` | 标量/message；语义必填见规格 | `versionId` |

## CreateComputePlanRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [CreateComputePlanRequest](rpc-messages.md#createcomputeplanrequest) | 标量/message；语义必填见规格 | `body` |

## SetComputePlanAvailabilityRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [PlanAvailabilityRequest](rpc-messages.md#planavailabilityrequest) | 标量/message；语义必填见规格 | `body` |
| `plan_id #3` | `string` | 标量/message；语义必填见规格 | `planId` |

## CreateStoragePlanRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [CreateStoragePlanRequest](rpc-messages.md#createstorageplanrequest) | 标量/message；语义必填见规格 | `body` |

## SetStoragePlanAvailabilityRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [PlanAvailabilityRequest](rpc-messages.md#planavailabilityrequest) | 标量/message；语义必填见规格 | `body` |
| `plan_id #3` | `string` | 标量/message；语义必填见规格 | `planId` |

## ListPricePolicyVersionsRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `query_cursor #2` | `string` | proto3 optional | `queryCursor` |
| `query_limit #3` | `int32` | proto3 optional | `queryLimit` |

## CreatePricePolicyVersionRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [CreatePricePolicyRequest](rpc-messages.md#createpricepolicyrequest) | 标量/message；语义必填见规格 | `body` |

## ListRefundPolicyVersionsRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `query_cursor #2` | `string` | proto3 optional | `queryCursor` |
| `query_limit #3` | `int32` | proto3 optional | `queryLimit` |

## CreateRefundPolicyVersionRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [CreateRefundPolicyRequest](rpc-messages.md#createrefundpolicyrequest) | 标量/message；语义必填见规格 | `body` |

## ListRetentionPolicyVersionsRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `query_cursor #2` | `string` | proto3 optional | `queryCursor` |
| `query_limit #3` | `int32` | proto3 optional | `queryLimit` |

## CreateRetentionPolicyVersionRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [CreateRetentionPolicyRequest](rpc-messages.md#createretentionpolicyrequest) | 标量/message；语义必填见规格 | `body` |

## AdoptWorkspaceRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [AdoptWorkspaceRequest](rpc-messages.md#adoptworkspacerequest) | 标量/message；语义必填见规格 | `body` |
| `workspace_id #3` | `string` | 标量/message；语义必填见规格 | `workspaceId` |

## GetBuildRuntimePolicyRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |

## SetBuildRuntimePolicyRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [SetBuildRuntimePolicyRequest](rpc-messages.md#setbuildruntimepolicyrequest) | 标量/message；语义必填见规格 | `body` |

## ListPublisherNamespacesRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `query_cursor #2` | `string` | proto3 optional | `queryCursor` |
| `query_limit #3` | `int32` | proto3 optional | `queryLimit` |

## CreatePublisherNamespaceRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [CreatePublisherNamespaceRequest](rpc-messages.md#createpublishernamespacerequest) | 标量/message；语义必填见规格 | `body` |

## RevokePublisherNamespaceRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [RevokePublisherNamespaceRequest](rpc-messages.md#revokepublishernamespacerequest) | 标量/message；语义必填见规格 | `body` |
| `publisher_namespace_id #3` | `string` | 标量/message；语义必填见规格 | `publisherNamespaceId` |

## ReenableTenantRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [ReenableTenantRequest](rpc-messages.md#reenabletenantrequest) | 标量/message；语义必填见规格 | `body` |
| `tenant_id #3` | `string` | 标量/message；语义必填见规格 | `tenantId` |

## GetTenantLifecycleOperationRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `tenant_id #2` | `string` | 标量/message；语义必填见规格 | `tenantId` |
| `operation_id #3` | `string` | 标量/message；语义必填见规格 | `operationId` |

## UpdateRenewalSettingsRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [UpdateRenewalSettingsRequest](rpc-messages.md#updaterenewalsettingsrequest) | 标量/message；语义必填见规格 | `body` |
| `workspace_id #3` | `string` | 标量/message；语义必填见规格 | `workspaceId` |

## RevealWorkspaceApplicationCredentialsRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |

## ListPlanChangesRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `query_cursor #3` | `string` | proto3 optional | `queryCursor` |
| `query_limit #4` | `int32` | proto3 optional | `queryLimit` |

## GetPlanChangeRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `plan_change_id #3` | `string` | 标量/message；语义必填见规格 | `planChangeId` |

## CancelPlanChangeRpcRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `body #2` | [CancelPlanChangeRequest](rpc-messages.md#cancelplanchangerequest) | 标量/message；语义必填见规格 | `body` |
| `workspace_id #3` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `plan_change_id #4` | `string` | 标量/message；语义必填见规格 | `planChangeId` |

## OwnerOperationRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `operation_id #2` | `string` | 标量/message；语义必填见规格 | `operationId` |
| `owner #3` | `OperationOwnerEnum` | 标量/message；语义必填见规格 | `owner` |

## SourceObjectReference

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `storage_object_id #1` | `string` | 标量/message；语义必填见规格 | `storageObjectId` |
| `version_id #2` | `string` | 标量/message；语义必填见规格 | `versionId` |
| `sha256 #3` | `string` | 标量/message；语义必填见规格 | `sha256` |
| `size_bytes #4` | `int64` | 标量/message；语义必填见规格 | `sizeBytes` |

## BuildInputRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `package_version_id #2` | `string` | 标量/message；语义必填见规格 | `packageVersionId` |
| `webui_version_id #3` | `string` | 标量/message；语义必填见规格 | `webuiVersionId` |

## BuildInputSnapshot

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `package_id #1` | `string` | 标量/message；语义必填见规格 | `packageId` |
| `package_version_id #2` | `string` | 标量/message；语义必填见规格 | `packageVersionId` |
| `package_object #3` | [SourceObjectReference](rpc-messages.md#sourceobjectreference) | 标量/message；语义必填见规格 | `packageObject` |
| `runtime_version_id #4` | `string` | 标量/message；语义必填见规格 | `runtimeVersionId` |
| `runtime_artifact #5` | [ArtifactReference](rpc-messages.md#artifactreference) | 标量/message；语义必填见规格 | `runtimeArtifact` |
| `webui_version_id #6` | `string` | 标量/message；语义必填见规格 | `webuiVersionId` |
| `webui_artifact #7` | [ArtifactReference](rpc-messages.md#artifactreference) | 标量/message；语义必填见规格 | `webuiArtifact` |
| `runtime_contract #8` | [RuntimePublisherContract](rpc-messages.md#runtimepublishercontract) | 标量/message；语义必填见规格 | `runtimeContract` |
| `webui_contract #9` | [WebuiPublisherContract](rpc-messages.md#webuipublishercontract) | 标量/message；语义必填见规格 | `webuiContract` |
| `snapshot_digest #10` | `string` | 标量/message；语义必填见规格 | `snapshotDigest` |
| `runtime_contract_reference #11` | [PublisherContractReference](rpc-messages.md#publishercontractreference) | 标量/message；语义必填见规格 | `runtimeContractReference` |
| `webui_contract_reference #12` | [PublisherContractReference](rpc-messages.md#publishercontractreference) | 标量/message；语义必填见规格 | `webuiContractReference` |
| `package_claim_id #13` | `string` | 标量/message；语义必填见规格 | `packageClaimId` |
| `runtime_claim_id #14` | `string` | 标量/message；语义必填见规格 | `runtimeClaimId` |
| `webui_claim_id #15` | `string` | 标量/message；语义必填见规格 | `webuiClaimId` |

## ReferenceTarget

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `package_version_id #1` | `string` | oneof target | `packageVersionId` |
| `runtime_version_id #2` | `string` | oneof target | `runtimeVersionId` |
| `webui_version_id #3` | `string` | oneof target | `webuiVersionId` |
| `capability_version_id #4` | `string` | oneof target | `capabilityVersionId` |

## ReferenceClaimRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `target #2` | [ReferenceTarget](rpc-messages.md#referencetarget) | 标量/message；语义必填见规格 | `target` |
| `claimant_owner #3` | `OwnerEnum` | 标量/message；语义必填见规格 | `claimantOwner` |
| `claimant_resource_id #4` | `string` | 标量/message；语义必填见规格 | `claimantResourceId` |

## OwnerCommitEvidence

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `owner #1` | `OwnerEnum` | 标量/message；语义必填见规格 | `owner` |
| `operation_id #2` | `string` | 标量/message；语义必填见规格 | `operationId` |
| `resource_id #3` | `string` | 标量/message；语义必填见规格 | `resourceId` |
| `accepted_input_digest #4` | `string` | 标量/message；语义必填见规格 | `acceptedInputDigest` |
| `committed_version #5` | `int64` | 标量/message；语义必填见规格 | `committedVersion` |
| `accepted_at #6` | `Timestamp` | 标量/message；语义必填见规格 | `acceptedAt` |

## BindReferenceRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `claim_id #2` | `string` | 标量/message；语义必填见规格 | `claimId` |
| `owner_commit_evidence #3` | [OwnerCommitEvidence](rpc-messages.md#ownercommitevidence) | 标量/message；语义必填见规格 | `ownerCommitEvidence` |

## ReleaseEvidence

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `owner #1` | `OwnerEnum` | 标量/message；语义必填见规格 | `owner` |
| `operation_id #2` | `string` | 标量/message；语义必填见规格 | `operationId` |
| `resource_id #3` | `string` | 标量/message；语义必填见规格 | `resourceId` |
| `terminal_status #4` | `TerminalOperationStatus` | 标量/message；语义必填见规格 | `terminalStatus` |
| `terminal_receipt_id #5` | `string` | 标量/message；语义必填见规格 | `terminalReceiptId` |
| `confirmed_resource_absence_receipt_id #6` | `string` | proto3 optional | `confirmedResourceAbsenceReceiptId` |

## ReleaseReferenceRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `claim_id #2` | `string` | 标量/message；语义必填见规格 | `claimId` |
| `release_evidence #3` | [ReleaseEvidence](rpc-messages.md#releaseevidence) | 标量/message；语义必填见规格 | `releaseEvidence` |

## ReferenceClaim

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `target #2` | [ReferenceTarget](rpc-messages.md#referencetarget) | 标量/message；语义必填见规格 | `target` |
| `claimant_owner #3` | `OwnerEnum` | 标量/message；语义必填见规格 | `claimantOwner` |
| `claimant_resource_id #4` | `string` | 标量/message；语义必填见规格 | `claimantResourceId` |
| `state #5` | `ReferenceClaimState` | 标量/message；语义必填见规格 | `state` |
| `bound_operation_id #6` | `string` | proto3 optional | `boundOperationId` |
| `bound_input_digest #7` | `string` | proto3 optional | `boundInputDigest` |
| `acquired_at #8` | `Timestamp` | 标量/message；语义必填见规格 | `acquiredAt` |
| `released_at #9` | `Timestamp` | proto3 optional | `releasedAt` |

## ReadClaimUsageRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `claim_id #2` | `string` | 标量/message；语义必填见规格 | `claimId` |
| `claimant_resource_id #3` | `string` | 标量/message；语义必填见规格 | `claimantResourceId` |
| `claimant_operation_id #4` | `string` | 标量/message；语义必填见规格 | `claimantOperationId` |

## ClaimUsageEvidence

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `claim_id #1` | `string` | 标量/message；语义必填见规格 | `claimId` |
| `owner #2` | `OwnerEnum` | 标量/message；语义必填见规格 | `owner` |
| `resource_id #3` | `string` | 标量/message；语义必填见规格 | `resourceId` |
| `operation_id #4` | `string` | 标量/message；语义必填见规格 | `operationId` |
| `accepted_input_digest #5` | `string` | 标量/message；语义必填见规格 | `acceptedInputDigest` |
| `operation_status #6` | `OperationStatusEnum` | 标量/message；语义必填见规格 | `operationStatus` |
| `actively_required #7` | `bool` | 标量/message；语义必填见规格 | `activelyRequired` |
| `committed_version #8` | `int64` | 标量/message；语义必填见规格 | `committedVersion` |
| `terminal_receipt_id #9` | `string` | proto3 optional | `terminalReceiptId` |
| `confirmed_absence_receipt_id #10` | `string` | proto3 optional | `confirmedAbsenceReceiptId` |
| `outcome #11` | `Observation` | 标量/message；语义必填见规格 | `outcome` |
| `observed_at #12` | `Timestamp` | 标量/message；语义必填见规格 | `observedAt` |
| `deployment_descriptor_digest #13` | `string` | 标量/message；语义必填见规格 | `deploymentDescriptorDigest` |
| `execution_epoch #14` | `int64` | 标量/message；语义必填见规格 | `executionEpoch` |
| `deployment_descriptor_object_ref #15` | `string` | 标量/message；语义必填见规格 | `deploymentDescriptorObjectRef` |

## ResolvePublisherContractRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `reference #2` | [PublisherContractReference](rpc-messages.md#publishercontractreference) | 标量/message；语义必填见规格 | `reference` |

## ResolvedPublisherContract

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `reference #1` | [PublisherContractReference](rpc-messages.md#publishercontractreference) | 标量/message；语义必填见规格 | `reference` |
| `contract #2` | [PublisherContract](rpc-messages.md#publishercontract) | 标量/message；语义必填见规格 | `contract` |
| `image #3` | [ArtifactReference](rpc-messages.md#artifactreference) | 标量/message；语义必填见规格 | `image` |
| `outcome #4` | `Observation` | 标量/message；语义必填见规格 | `outcome` |

## ReadBuildArtifactRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `build_job_id #2` | `string` | 标量/message；语义必填见规格 | `buildJobId` |

## BuildArtifactReadback

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `build_job_id #1` | `string` | 标量/message；语义必填见规格 | `buildJobId` |
| `input #2` | [BuildInputSnapshot](rpc-messages.md#buildinputsnapshot) | 标量/message；语义必填见规格 | `input` |
| `artifact #3` | [ArtifactReference](rpc-messages.md#artifactreference) | 标量/message；语义必填见规格 | `artifact` |
| `artifact_receipt_id #4` | `string` | 标量/message；语义必填见规格 | `artifactReceiptId` |
| `version_label #5` | `string` | 标量/message；语义必填见规格 | `versionLabel` |
| `model_requirements #6` | [ModelRequirement](rpc-messages.md#modelrequirement) | repeated | `modelRequirements` |
| `data_compatibility #7` | [DataCompatibility](rpc-messages.md#datacompatibility) | 标量/message；语义必填见规格 | `dataCompatibility` |
| `outcome #8` | `Observation` | 标量/message；语义必填见规格 | `outcome` |
| `deployment_descriptor #9` | [DeploymentDescriptor](rpc-messages.md#deploymentdescriptor) | 标量/message；语义必填见规格 | `deploymentDescriptor` |
| `deployment_descriptor_digest #10` | `string` | 标量/message；语义必填见规格 | `deploymentDescriptorDigest` |
| `deployment_descriptor_object_ref #11` | `string` | 标量/message；语义必填见规格 | `deploymentDescriptorObjectRef` |

## PlatformScope

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |

## TenantScope

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `tenant_id #1` | `string` | 标量/message；语义必填见规格 | `tenantId` |

## AuthorizationScope

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `platform #1` | [PlatformScope](rpc-messages.md#platformscope) | oneof scope | `platform` |
| `tenant #2` | [TenantScope](rpc-messages.md#tenantscope) | oneof scope | `tenant` |

## AuthorizationResource

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `kind #1` | `AuthorizationResourceKind` | 标量/message；语义必填见规格 | `kind` |
| `id #2` | `string` | proto3 optional | `id` |

## AuthorizationRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `scope #1` | [AuthorizationScope](rpc-messages.md#authorizationscope) | 标量/message；语义必填见规格 | `scope` |
| `actor_id #2` | `string` | 标量/message；语义必填见规格 | `actorId` |
| `session_id #3` | `string` | proto3 optional | `sessionId` |
| `authorization_context_id #4` | `string` | proto3 optional | `authorizationContextId` |
| `accepted_operation_grant_id #5` | `string` | proto3 optional | `acceptedOperationGrantId` |
| `audience_owner #6` | `OwnerEnum` | 标量/message；语义必填见规格 | `audienceOwner` |
| `action #7` | `AuthorizationActionEnum` | 标量/message；语义必填见规格 | `action` |
| `resource #8` | [AuthorizationResource](rpc-messages.md#authorizationresource) | 标量/message；语义必填见规格 | `resource` |
| `expected_permission_version #9` | `int64` | proto3 optional | `expectedPermissionVersion` |
| `request_id #10` | `string` | 标量/message；语义必填见规格 | `requestId` |

## AuthorizationDecision

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `result #1` | `AuthorizationResult` | 标量/message；语义必填见规格 | `result` |
| `issuer #2` | `AuthorizationIssuer` | 标量/message；语义必填见规格 | `issuer` |
| `authorization_context_id #3` | `string` | proto3 optional | `authorizationContextId` |
| `scope #4` | [AuthorizationScope](rpc-messages.md#authorizationscope) | 标量/message；语义必填见规格 | `scope` |
| `actor_id #5` | `string` | 标量/message；语义必填见规格 | `actorId` |
| `session_id #6` | `string` | proto3 optional | `sessionId` |
| `audience_owner #7` | `OwnerEnum` | 标量/message；语义必填见规格 | `audienceOwner` |
| `action #8` | `AuthorizationActionEnum` | 标量/message；语义必填见规格 | `action` |
| `resource #9` | [AuthorizationResource](rpc-messages.md#authorizationresource) | 标量/message；语义必填见规格 | `resource` |
| `permission_version #10` | `int64` | 标量/message；语义必填见规格 | `permissionVersion` |
| `issued_at #11` | `Timestamp` | 标量/message；语义必填见规格 | `issuedAt` |
| `expires_at #12` | `Timestamp` | 标量/message；语义必填见规格 | `expiresAt` |
| `denial_code #13` | `ErrorCodeEnum` | proto3 optional | `denialCode` |
| `accepted_operation_grant_id #14` | `string` | proto3 optional | `acceptedOperationGrantId` |

## GetAuthorizationContextRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `authorization_context_id #1` | `string` | 标量/message；语义必填见规格 | `authorizationContextId` |
| `expected_audience_owner #2` | `OwnerEnum` | 标量/message；语义必填见规格 | `expectedAudienceOwner` |
| `expected_action #3` | `AuthorizationActionEnum` | 标量/message；语义必填见规格 | `expectedAction` |
| `expected_resource #4` | [AuthorizationResource](rpc-messages.md#authorizationresource) | 标量/message；语义必填见规格 | `expectedResource` |
| `request_id #5` | `string` | 标量/message；语义必填见规格 | `requestId` |

## AcceptedOperationGrantRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `authorization_context_id #1` | `string` | 标量/message；语义必填见规格 | `authorizationContextId` |
| `owner_commit_evidence #2` | [OwnerCommitEvidence](rpc-messages.md#ownercommitevidence) | 标量/message；语义必填见规格 | `ownerCommitEvidence` |
| `allowed_actions #3` | `AuthorizationActionEnum` | repeated | `allowedActions` |
| `renewal_consent_id #4` | `string` | proto3 optional | `renewalConsentId` |
| `subscription_period_id #5` | `string` | proto3 optional | `subscriptionPeriodId` |

## AcceptedOperationGrant

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `scope #2` | [AuthorizationScope](rpc-messages.md#authorizationscope) | 标量/message；语义必填见规格 | `scope` |
| `actor_id #3` | `string` | 标量/message；语义必填见规格 | `actorId` |
| `accepted_operation_owner #4` | `OwnerEnum` | 标量/message；语义必填见规格 | `acceptedOperationOwner` |
| `accepted_operation_id #5` | `string` | 标量/message；语义必填见规格 | `acceptedOperationId` |
| `accepted_action #6` | `AuthorizationActionEnum` | 标量/message；语义必填见规格 | `acceptedAction` |
| `resource_id #7` | `string` | 标量/message；语义必填见规格 | `resourceId` |
| `accepted_permission_version #8` | `int64` | 标量/message；语义必填见规格 | `acceptedPermissionVersion` |
| `allowed_actions #9` | `AuthorizationActionEnum` | repeated | `allowedActions` |
| `mode #10` | `AcceptedGrantMode` | 标量/message；语义必填见规格 | `mode` |
| `issued_at #11` | `Timestamp` | 标量/message；语义必填见规格 | `issuedAt` |
| `expires_at #12` | `Timestamp` | proto3 optional | `expiresAt` |
| `revoked_at #13` | `Timestamp` | proto3 optional | `revokedAt` |
| `obligation_completed_at #14` | `Timestamp` | proto3 optional | `obligationCompletedAt` |

## ReadOwnerCommitRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `owner #1` | `OwnerEnum` | 标量/message；语义必填见规格 | `owner` |
| `operation_id #2` | `string` | 标量/message；语义必填见规格 | `operationId` |
| `resource_id #3` | `string` | 标量/message；语义必填见规格 | `resourceId` |

## ReadRenewalConsentRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `subscription_id #2` | `string` | 标量/message；语义必填见规格 | `subscriptionId` |
| `consent_id #3` | `string` | 标量/message；语义必填见规格 | `consentId` |
| `subscription_period_id #4` | `string` | 标量/message；语义必填见规格 | `subscriptionPeriodId` |

## RenewalConsentReadback

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `subscription_id #1` | `string` | 标量/message；语义必填见规格 | `subscriptionId` |
| `consent_id #2` | `string` | 标量/message；语义必填见规格 | `consentId` |
| `subscription_period_id #3` | `string` | 标量/message；语义必填见规格 | `subscriptionPeriodId` |
| `active #4` | `bool` | 标量/message；语义必填见规格 | `active` |
| `settings_version #5` | `int64` | 标量/message；语义必填见规格 | `settingsVersion` |
| `accepted_policy_version_id #6` | `string` | 标量/message；语义必填见规格 | `acceptedPolicyVersionId` |
| `actor_id #7` | `string` | 标量/message；语义必填见规格 | `actorId` |
| `accepted_at #8` | `Timestamp` | 标量/message；语义必填见规格 | `acceptedAt` |
| `outcome #9` | `Observation` | 标量/message；语义必填见规格 | `outcome` |

## AdmissionRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `capability_version_id #3` | `string` | 标量/message；语义必填见规格 | `capabilityVersionId` |
| `compute_plan_id #4` | `string` | 标量/message；语义必填见规格 | `computePlanId` |
| `storage_plan_id #5` | `string` | 标量/message；语义必填见规格 | `storagePlanId` |
| `model_selections #6` | [ModelSelection](rpc-messages.md#modelselection) | repeated | `modelSelections` |
| `purpose #7` | `string` | 标量/message；语义必填见规格 | `purpose` |

## AdmissionResult

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `outcome #1` | `Observation` | 标量/message；语义必填见规格 | `outcome` |
| `admission_id #2` | `string` | 标量/message；语义必填见规格 | `admissionId` |
| `capability_snapshot_digest #3` | `string` | 标量/message；语义必填见规格 | `capabilitySnapshotDigest` |
| `provider_capability_version #4` | `string` | 标量/message；语义必填见规格 | `providerCapabilityVersion` |
| `policy_version_id #5` | `string` | 标量/message；语义必填见规格 | `policyVersionId` |
| `error_code #6` | `string` | 标量/message；语义必填见规格 | `errorCode` |
| `expires_at #7` | `Timestamp` | 标量/message；语义必填见规格 | `expiresAt` |
| `expected_interruption #8` | `string` | 标量/message；语义必填见规格 | `expectedInterruption` |

## AcceptQuoteRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `quote_id #2` | `string` | 标量/message；语义必填见规格 | `quoteId` |
| `workspace_id #3` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `obligation_id #4` | `string` | 标量/message；语义必填见规格 | `obligationId` |
| `admission_id #5` | `string` | 标量/message；语义必填见规格 | `admissionId` |
| `plan_change_id #6` | `string` | proto3 optional | `planChangeId` |
| `source_subscription_version #7` | `int64` | proto3 optional | `sourceSubscriptionVersion` |
| `subscription_period_obligation_id #8` | `string` | proto3 optional | `subscriptionPeriodObligationId` |

## QuoteAcceptance

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `quote #1` | [Quote](rpc-messages.md#quote) | 标量/message；语义必填见规格 | `quote` |
| `obligation_id #2` | `string` | 标量/message；语义必填见规格 | `obligationId` |
| `acceptance_id #3` | `string` | 标量/message；语义必填见规格 | `acceptanceId` |
| `snapshot_digest #4` | `string` | 标量/message；语义必填见规格 | `snapshotDigest` |
| `source_financial_snapshot_bytes #5` | `bytes` | proto3 optional | `sourceFinancialSnapshotBytes` |
| `source_financial_snapshot_digest #6` | `string` | proto3 optional | `sourceFinancialSnapshotDigest` |

## WalletBindingCommand

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `target_tenant_id #2` | `string` | 标量/message；语义必填见规格 | `targetTenantId` |
| `billing_sub2api_user_id #3` | `string` | 标量/message；语义必填见规格 | `billingSub2apiUserId` |
| `expected_binding_version #4` | `int64` | 标量/message；语义必填见规格 | `expectedBindingVersion` |
| `authorization_receipt_id #5` | `string` | 标量/message；语义必填见规格 | `authorizationReceiptId` |

## WalletBindingReadback

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `tenant_id #1` | `string` | 标量/message；语义必填见规格 | `tenantId` |
| `billing_sub2api_user_id #2` | `string` | 标量/message；语义必填见规格 | `billingSub2apiUserId` |
| `binding_version #3` | `int64` | 标量/message；语义必填见规格 | `bindingVersion` |
| `outcome #4` | `Observation` | 标量/message；语义必填见规格 | `outcome` |
| `authorization_receipt_id #5` | `string` | 标量/message；语义必填见规格 | `authorizationReceiptId` |

## WalletDebitCommand

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `obligation_id #3` | `string` | 标量/message；语义必填见规格 | `obligationId` |
| `quote_acceptance_id #4` | `string` | 标量/message；语义必填见规格 | `quoteAcceptanceId` |
| `amount_usd_micros #5` | `int64` | 标量/message；语义必填见规格 | `amountUsdMicros` |
| `currency #6` | `string` | 标量/message；语义必填见规格 | `currency` |
| `subscription_period_id #7` | `string` | 标量/message；语义必填见规格 | `subscriptionPeriodId` |

## WalletRefundCommand

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `original_wallet_operation_id #3` | `string` | 标量/message；语义必填见规格 | `originalWalletOperationId` |
| `original_external_payment_reference #4` | `string` | 标量/message；语义必填见规格 | `originalExternalPaymentReference` |
| `amount_usd_micros #5` | `int64` | 标量/message；语义必填见规格 | `amountUsdMicros` |
| `refund_policy_version_id #6` | `string` | 标量/message；语义必填见规格 | `refundPolicyVersionId` |
| `confirmed_deletion_receipt_id #7` | `string` | 标量/message；语义必填见规格 | `confirmedDeletionReceiptId` |
| `subscription_period_id #8` | `string` | 标量/message；语义必填见规格 | `subscriptionPeriodId` |

## WalletReadbackRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `wallet_operation_id #2` | `string` | 标量/message；语义必填见规格 | `walletOperationId` |
| `original_idempotency_key #3` | `string` | 标量/message；语义必填见规格 | `originalIdempotencyKey` |

## ManagedKeyCommand

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `model_ids #3` | `string` | repeated | `modelIds` |
| `target_runtime_instance_id #4` | `string` | 标量/message；语义必填见规格 | `targetRuntimeInstanceId` |

## ManagedKeyBinding

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `key_binding_id #1` | `string` | 标量/message；语义必填见规格 | `keyBindingId` |
| `fingerprint #2` | `string` | 标量/message；语义必填见规格 | `fingerprint` |
| `secret_delivery_reference #3` | `string` | 标量/message；语义必填见规格 | `secretDeliveryReference` |
| `workspace_id #4` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `target_runtime_instance_id #5` | `string` | 标量/message；语义必填见规格 | `targetRuntimeInstanceId` |
| `expires_at #6` | `Timestamp` | 标量/message；语义必填见规格 | `expiresAt` |

## ManagedKeyRevoke

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `key_binding_id #2` | `string` | 标量/message；语义必填见规格 | `keyBindingId` |
| `workspace_id #3` | `string` | 标量/message；语义必填见规格 | `workspaceId` |

## ResourcePlanSnapshot

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `compute_plan_id #1` | `string` | 标量/message；语义必填见规格 | `computePlanId` |
| `storage_plan_id #2` | `string` | 标量/message；语义必填见规格 | `storagePlanId` |
| `provider_profile_id #3` | `string` | 标量/message；语义必填见规格 | `providerProfileId` |
| `provider_compute_sku_id #4` | `string` | 标量/message；语义必填见规格 | `providerComputeSkuId` |
| `provider_storage_sku_id #5` | `string` | 标量/message；语义必填见规格 | `providerStorageSkuId` |
| `vcpus #6` | `int32` | 标量/message；语义必填见规格 | `vcpus` |
| `memory_mib #7` | `int32` | 标量/message；语义必填见规格 | `memoryMib` |
| `capacity_gib #8` | `int32` | 标量/message；语义必填见规格 | `capacityGib` |
| `prepaid_months #9` | `int32` | 标量/message；语义必填见规格 | `prepaidMonths` |
| `provider_capability_version #10` | `string` | 标量/message；语义必填见规格 | `providerCapabilityVersion` |
| `conditional_route_revision_supported #11` | `bool` | 标量/message；语义必填见规格 | `conditionalRouteRevisionSupported` |

## ResourceAdmissionRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `plan #2` | [ResourcePlanSnapshot](rpc-messages.md#resourceplansnapshot) | 标量/message；语义必填见规格 | `plan` |
| `workspace_id #3` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `existing_resource_set_id #4` | `string` | 标量/message；语义必填见规格 | `existingResourceSetId` |
| `purpose #5` | `string` | 标量/message；语义必填见规格 | `purpose` |

## EnsureResourcesCommand

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `obligation_id #3` | `string` | 标量/message；语义必填见规格 | `obligationId` |
| `plan #4` | [ResourcePlanSnapshot](rpc-messages.md#resourceplansnapshot) | 标量/message；语义必填见规格 | `plan` |
| `confirmed_charge_receipt_id #5` | `string` | 标量/message；语义必填见规格 | `confirmedChargeReceiptId` |
| `instance_authorization_reference #6` | `string` | 标量/message；语义必填见规格 | `instanceAuthorizationReference` |

## MutateResourcesCommand

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `resource_set_id #3` | `string` | 标量/message；语义必填见规格 | `resourceSetId` |
| `expected_resource_version #4` | `string` | 标量/message；语义必填见规格 | `expectedResourceVersion` |
| `instance_authorization_reference #5` | `string` | 标量/message；语义必填见规格 | `instanceAuthorizationReference` |

## ResizeResourcesCommand

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `resource_set_id #3` | `string` | 标量/message；语义必填见规格 | `resourceSetId` |
| `expected_resource_version #4` | `string` | 标量/message；语义必填见规格 | `expectedResourceVersion` |
| `target #5` | [ResourcePlanSnapshot](rpc-messages.md#resourceplansnapshot) | 标量/message；语义必填见规格 | `target` |
| `quote_acceptance_id #6` | `string` | 标量/message；语义必填见规格 | `quoteAcceptanceId` |
| `funding_evidence #7` | [PlanChangeFundingEvidence](rpc-messages.md#planchangefundingevidence) | 标量/message；语义必填见规格 | `fundingEvidence` |
| `instance_authorization_reference #8` | `string` | 标量/message；语义必填见规格 | `instanceAuthorizationReference` |
| `plan_change_id #9` | `string` | 标量/message；语义必填见规格 | `planChangeId` |
| `transition_id #10` | `string` | 标量/message；语义必填见规格 | `transitionId` |
| `execution_epoch #11` | `int64` | 标量/message；语义必填见规格 | `executionEpoch` |
| `execution_plan #12` | [ProviderPlanChangeExecutionPlanReference](rpc-messages.md#providerplanchangeexecutionplanreference) | 标量/message；语义必填见规格 | `executionPlan` |

## RenewResourcesCommand

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `resource_set_id #3` | `string` | 标量/message；语义必填见规格 | `resourceSetId` |
| `subscription_period_id #4` | `string` | 标量/message；语义必填见规格 | `subscriptionPeriodId` |
| `prepaid_months #5` | `int32` | 标量/message；语义必填见规格 | `prepaidMonths` |
| `confirmed_charge_receipt_id #6` | `string` | 标量/message；语义必填见规格 | `confirmedChargeReceiptId` |
| `instance_authorization_reference #7` | `string` | 标量/message；语义必填见规格 | `instanceAuthorizationReference` |

## ResourceReadbackRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `resource_set_id #2` | `string` | 标量/message；语义必填见规格 | `resourceSetId` |
| `resource_action_id #3` | `string` | 标量/message；语义必填见规格 | `resourceActionId` |

## ResourceFact

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `kind #2` | `string` | 标量/message；语义必填见规格 | `kind` |
| `opaque_provider_reference #3` | `string` | 标量/message；语义必填见规格 | `opaqueProviderReference` |
| `state #4` | `string` | 标量/message；语义必填见规格 | `state` |
| `receipt_id #5` | `string` | 标量/message；语义必填见规格 | `receiptId` |

## ResourceReadback

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `resource_set_id #1` | `string` | 标量/message；语义必填见规格 | `resourceSetId` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `resource_version #3` | `string` | 标量/message；语义必填见规格 | `resourceVersion` |
| `outcome #4` | `Observation` | 标量/message；语义必填见规格 | `outcome` |
| `resources #5` | [ResourceFact](rpc-messages.md#resourcefact) | repeated | `resources` |
| `absence_confirmed #6` | `bool` | 标量/message；语义必填见规格 | `absenceConfirmed` |
| `deletion_receipt_id #7` | `string` | 标量/message；语义必填见规格 | `deletionReceiptId` |
| `observed_at #8` | `Timestamp` | 标量/message；语义必填见规格 | `observedAt` |
| `error_code #9` | `string` | 标量/message；语义必填见规格 | `errorCode` |

## SecretBindingCommand

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `runtime_instance_id #3` | `string` | 标量/message；语义必填见规格 | `runtimeInstanceId` |
| `key_binding_id #4` | `string` | 标量/message；语义必填见规格 | `keyBindingId` |
| `secret_delivery_reference #5` | `string` | 标量/message；语义必填见规格 | `secretDeliveryReference` |
| `target_slot #6` | `string` | 标量/message；语义必填见规格 | `targetSlot` |

## SecretBindingReadback

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `secret_binding_id #1` | `string` | 标量/message；语义必填见规格 | `secretBindingId` |
| `runtime_instance_id #2` | `string` | 标量/message；语义必填见规格 | `runtimeInstanceId` |
| `fingerprint #3` | `string` | 标量/message；语义必填见规格 | `fingerprint` |
| `outcome #4` | `Observation` | 标量/message；语义必填见规格 | `outcome` |
| `receipt_id #5` | `string` | 标量/message；语义必填见规格 | `receiptId` |

## RuntimeReservationCommand

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `deployment_id #3` | `string` | 标量/message；语义必填见规格 | `deploymentId` |
| `capability_version_id #4` | `string` | 标量/message；语义必填见规格 | `capabilityVersionId` |
| `artifact #5` | [ArtifactReference](rpc-messages.md#artifactreference) | 标量/message；语义必填见规格 | `artifact` |
| `deployment_descriptor_digest #6` | `string` | 标量/message；语义必填见规格 | `deploymentDescriptorDigest` |
| `deployment_descriptor_object_ref #7` | `string` | 标量/message；语义必填见规格 | `deploymentDescriptorObjectRef` |

## RuntimeReservation

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `runtime_instance_id #1` | `string` | 标量/message；语义必填见规格 | `runtimeInstanceId` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `deployment_id #3` | `string` | 标量/message；语义必填见规格 | `deploymentId` |
| `artifact #4` | [ArtifactReference](rpc-messages.md#artifactreference) | 标量/message；语义必填见规格 | `artifact` |
| `deployment_descriptor_digest #5` | `string` | 标量/message；语义必填见规格 | `deploymentDescriptorDigest` |
| `deployment_descriptor_object_ref #6` | `string` | 标量/message；语义必填见规格 | `deploymentDescriptorObjectRef` |

## RuntimeDeployCommand

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `deployment_id #3` | `string` | 标量/message；语义必填见规格 | `deploymentId` |
| `capability_version_id #4` | `string` | 标量/message；语义必填见规格 | `capabilityVersionId` |
| `deployment_descriptor #5` | [DeploymentDescriptor](rpc-messages.md#deploymentdescriptor) | 标量/message；语义必填见规格 | `deploymentDescriptor` |
| `resource_set_id #6` | `string` | 标量/message；语义必填见规格 | `resourceSetId` |
| `data_attachment_id #7` | `string` | 标量/message；语义必填见规格 | `dataAttachmentId` |
| `secret_binding_id #8` | `string` | 标量/message；语义必填见规格 | `secretBindingId` |
| `model_configuration_version #9` | `int64` | 标量/message；语义必填见规格 | `modelConfigurationVersion` |
| `model_selections #10` | [ModelSelection](rpc-messages.md#modelselection) | repeated | `modelSelections` |
| `data_compatibility #11` | [DataCompatibility](rpc-messages.md#datacompatibility) | 标量/message；语义必填见规格 | `dataCompatibility` |
| `runtime_instance_id #12` | `string` | 标量/message；语义必填见规格 | `runtimeInstanceId` |
| `deployment_descriptor_digest #13` | `string` | 标量/message；语义必填见规格 | `deploymentDescriptorDigest` |
| `execution_epoch #14` | `int64` | 标量/message；语义必填见规格 | `executionEpoch` |
| `deployment_descriptor_object_ref #15` | `string` | 标量/message；语义必填见规格 | `deploymentDescriptorObjectRef` |

## RuntimeReadbackRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `runtime_instance_id #2` | `string` | 标量/message；语义必填见规格 | `runtimeInstanceId` |
| `deployment_id #3` | `string` | 标量/message；语义必填见规格 | `deploymentId` |

## RuntimeReadback

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `runtime_instance_id #1` | `string` | 标量/message；语义必填见规格 | `runtimeInstanceId` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `deployment_id #3` | `string` | 标量/message；语义必填见规格 | `deploymentId` |
| `state #4` | `RuntimeInstanceState` | 标量/message；语义必填见规格 | `state` |
| `process_ready #5` | `bool` | 标量/message；语义必填见规格 | `processReady` |
| `application_available #6` | `bool` | 标量/message；语义必填见规格 | `applicationAvailable` |
| `credential_injection_verified #7` | `bool` | 标量/message；语义必填见规格 | `credentialInjectionVerified` |
| `artifact #8` | [ArtifactReference](rpc-messages.md#artifactreference) | 标量/message；语义必填见规格 | `artifact` |
| `applied_model_configuration_version #9` | `int64` | 标量/message；语义必填见规格 | `appliedModelConfigurationVersion` |
| `readiness_receipt_id #10` | `string` | 标量/message；语义必填见规格 | `readinessReceiptId` |
| `outcome #11` | `Observation` | 标量/message；语义必填见规格 | `outcome` |
| `observed_at #12` | `Timestamp` | 标量/message；语义必填见规格 | `observedAt` |
| `deployment_descriptor_digest #13` | `string` | 标量/message；语义必填见规格 | `deploymentDescriptorDigest` |
| `execution_epoch #14` | `int64` | 标量/message；语义必填见规格 | `executionEpoch` |
| `deployment_descriptor_object_ref #15` | `string` | 标量/message；语义必填见规格 | `deploymentDescriptorObjectRef` |

## RuntimeReloadCommand

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `runtime_instance_id #2` | `string` | 标量/message；语义必填见规格 | `runtimeInstanceId` |
| `expected_applied_version #3` | `int64` | 标量/message；语义必填见规格 | `expectedAppliedVersion` |
| `target_version #4` | `int64` | 标量/message；语义必填见规格 | `targetVersion` |
| `selections #5` | [ModelSelection](rpc-messages.md#modelselection) | repeated | `selections` |

## RuntimeStopCommand

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `runtime_instance_id #2` | `string` | 标量/message；语义必填见规格 | `runtimeInstanceId` |
| `deployment_id #3` | `string` | 标量/message；语义必填见规格 | `deploymentId` |
| `retained_data_attachment_id #4` | `string` | 标量/message；语义必填见规格 | `retainedDataAttachmentId` |

## ReadApplicationCredentialsRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `runtime_instance_id #3` | `string` | 标量/message；语义必填见规格 | `runtimeInstanceId` |
| `deployment_id #4` | `string` | 标量/message；语义必填见规格 | `deploymentId` |

## ConfirmedRouteAbsence

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `receipt_id #1` | `string` | 标量/message；语义必填见规格 | `receiptId` |
| `observed_at #2` | `Timestamp` | 标量/message；语义必填见规格 | `observedAt` |

## ProviderRevisionPrecondition

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `exact_revision #1` | `string` | oneof condition | `exactRevision` |
| `require_absent #2` | [ConfirmedRouteAbsence](rpc-messages.md#confirmedrouteabsence) | oneof condition | `requireAbsent` |

## FenceRouteEpochCommand

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `operation_id #3` | `string` | 标量/message；语义必填见规格 | `operationId` |
| `execution_epoch #4` | `int64` | 标量/message；语义必填见规格 | `executionEpoch` |
| `expected_route_generation #5` | `int64` | 标量/message；语义必填见规格 | `expectedRouteGeneration` |
| `provider_precondition #6` | [ProviderRevisionPrecondition](rpc-messages.md#providerrevisionprecondition) | 标量/message；语义必填见规格 | `providerPrecondition` |

## RouteActivateCommand

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `operation_id #3` | `string` | 标量/message；语义必填见规格 | `operationId` |
| `execution_epoch #4` | `int64` | 标量/message；语义必填见规格 | `executionEpoch` |
| `expected_route_generation #5` | `int64` | 标量/message；语义必填见规格 | `expectedRouteGeneration` |
| `provider_precondition #6` | [ProviderRevisionPrecondition](rpc-messages.md#providerrevisionprecondition) | 标量/message；语义必填见规格 | `providerPrecondition` |
| `target_execution_resource_id #7` | `string` | 标量/message；语义必填见规格 | `targetExecutionResourceId` |
| `target_runtime_instance_id #8` | `string` | 标量/message；语义必填见规格 | `targetRuntimeInstanceId` |
| `target_deployment_id #9` | `string` | 标量/message；语义必填见规格 | `targetDeploymentId` |
| `confirmed_readiness_receipt_id #10` | `string` | 标量/message；语义必填见规格 | `confirmedReadinessReceiptId` |

## RouteObserveRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `switch_id #3` | `string` | proto3 optional | `switchId` |

## RouteRollbackCommand

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `original_switch_id #3` | `string` | 标量/message；语义必填见规格 | `originalSwitchId` |
| `operation_id #4` | `string` | 标量/message；语义必填见规格 | `operationId` |
| `execution_epoch #5` | `int64` | 标量/message；语义必填见规格 | `executionEpoch` |
| `expected_route_generation #6` | `int64` | 标量/message；语义必填见规格 | `expectedRouteGeneration` |
| `provider_precondition #7` | [ProviderRevisionPrecondition](rpc-messages.md#providerrevisionprecondition) | 标量/message；语义必填见规格 | `providerPrecondition` |
| `target_execution_resource_id #8` | `string` | 标量/message；语义必填见规格 | `targetExecutionResourceId` |
| `target_runtime_instance_id #9` | `string` | 标量/message；语义必填见规格 | `targetRuntimeInstanceId` |
| `target_deployment_id #10` | `string` | 标量/message；语义必填见规格 | `targetDeploymentId` |
| `compatibility_receipt_id #11` | `string` | 标量/message；语义必填见规格 | `compatibilityReceiptId` |

## RouteReadback

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `workspace_id #1` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `switch_id #2` | `string` | 标量/message；语义必填见规格 | `switchId` |
| `observation #3` | `Observation` | 标量/message；语义必填见规格 | `observation` |
| `current_generation #4` | `int64` | 标量/message；语义必填见规格 | `currentGeneration` |
| `accepted_execution_epoch #5` | `int64` | 标量/message；语义必填见规格 | `acceptedExecutionEpoch` |
| `target_execution_resource_id #6` | `string` | proto3 optional | `targetExecutionResourceId` |
| `target_runtime_instance_id #7` | `string` | proto3 optional | `targetRuntimeInstanceId` |
| `target_deployment_id #8` | `string` | proto3 optional | `targetDeploymentId` |
| `provider_revision #9` | `string` | 标量/message；语义必填见规格 | `providerRevision` |
| `provider_command_id #10` | `string` | 标量/message；语义必填见规格 | `providerCommandId` |
| `route_receipt_id #11` | `string` | proto3 optional | `routeReceiptId` |
| `observed_at #12` | `Timestamp` | 标量/message；语义必填见规格 | `observedAt` |
| `error_code #13` | `ErrorCodeEnum` | proto3 optional | `errorCode` |

## TenantWorkspaceLifecycleCommand

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `target_tenant_id #2` | `string` | 标量/message；语义必填见规格 | `targetTenantId` |
| `tenant_operation_id #3` | `string` | 标量/message；语义必填见规格 | `tenantOperationId` |
| `reason #4` | `string` | 标量/message；语义必填见规格 | `reason` |

## TenantWorkspaceLifecycleReadback

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `target_tenant_id #1` | `string` | 标量/message；语义必填见规格 | `targetTenantId` |
| `workspace_actions #2` | [TenantWorkspaceAction](rpc-messages.md#tenantworkspaceaction) | repeated | `workspaceActions` |
| `outcome #3` | `Observation` | 标量/message；语义必填见规格 | `outcome` |
| `skipped #4` | [TenantWorkspaceSkip](rpc-messages.md#tenantworkspaceskip) | repeated | `skipped` |

## ResumeTenantWorkspacesRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `target_tenant_id #2` | `string` | 标量/message；语义必填见规格 | `targetTenantId` |
| `reenable_operation_id #3` | `string` | 标量/message；语义必填见规格 | `reenableOperationId` |
| `original_tenant_suspend_operation_id #4` | `string` | 标量/message；语义必填见规格 | `originalTenantSuspendOperationId` |

## AppendReceiptRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `receipt #2` | [Receipt](rpc-messages.md#receipt) | 标量/message；语义必填见规格 | `receipt` |
| `evidence_digest #3` | `string` | 标量/message；语义必填见规格 | `evidenceDigest` |
| `owner_evidence_reference #4` | `string` | 标量/message；语义必填见规格 | `ownerEvidenceReference` |

## GetReceiptByReferenceRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `owner #2` | `string` | 标量/message；语义必填见规格 | `owner` |
| `owner_evidence_reference #3` | `string` | 标量/message；语义必填见规格 | `ownerEvidenceReference` |

## ReadSubscriptionPlanStateRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |

## SubscriptionPlanState

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `subscription_id #1` | `string` | 标量/message；语义必填见规格 | `subscriptionId` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `subscription_version #3` | `int64` | 标量/message；语义必填见规格 | `subscriptionVersion` |
| `current_period_id #4` | `string` | 标量/message；语义必填见规格 | `currentPeriodId` |
| `period_start #5` | `Timestamp` | 标量/message；语义必填见规格 | `periodStart` |
| `period_end #6` | `Timestamp` | 标量/message；语义必填见规格 | `periodEnd` |
| `billing_anchor_day #7` | `int32` | 标量/message；语义必填见规格 | `billingAnchorDay` |
| `compute_plan_id #8` | `string` | 标量/message；语义必填见规格 | `computePlanId` |
| `storage_plan_id #9` | `string` | 标量/message；语义必填见规格 | `storagePlanId` |
| `accepted_price_policy_version_id #10` | `string` | proto3 optional | `acceptedPricePolicyVersionId` |
| `accepted_monthly_usd_micros #11` | `int64` | proto3 optional | `acceptedMonthlyUsdMicros` |
| `unfinished_plan_change_id #12` | `string` | proto3 optional | `unfinishedPlanChangeId` |
| `next_period_obligation_id #13` | `string` | proto3 optional | `nextPeriodObligationId` |
| `next_period_start #14` | `Timestamp` | 标量/message；语义必填见规格 | `nextPeriodStart` |
| `next_period_end #15` | `Timestamp` | 标量/message；语义必填见规格 | `nextPeriodEnd` |
| `renewal_mode #16` | `SubscriptionRenewalModeEnum` | 标量/message；语义必填见规格 | `renewalMode` |
| `renewal_consent_id #17` | `string` | proto3 optional | `renewalConsentId` |
| `outcome #18` | `Observation` | 标量/message；语义必填见规格 | `outcome` |
| `other_future_committed_obligation_id #19` | `string` | proto3 optional | `otherFutureCommittedObligationId` |
| `runtime_readback_required #20` | `bool` | 标量/message；语义必填见规格 | `runtimeReadbackRequired` |
| `current_application_deployment_id #21` | `string` | proto3 optional | `currentApplicationDeploymentId` |
| `period_start_milliseconds #22` | `int64` | 标量/message；语义必填见规格 | `periodStartMilliseconds` |
| `period_end_milliseconds #23` | `int64` | 标量/message；语义必填见规格 | `periodEndMilliseconds` |
| `source_financial_snapshot_bytes #24` | `bytes` | 标量/message；语义必填见规格 | `sourceFinancialSnapshotBytes` |
| `source_financial_snapshot_digest #25` | `string` | 标量/message；语义必填见规格 | `sourceFinancialSnapshotDigest` |
| `source_financial_snapshot #26` | [SourceFinancialSnapshot](rpc-messages.md#sourcefinancialsnapshot) | 标量/message；语义必填见规格 | `sourceFinancialSnapshot` |

## ReadPlanChangeRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `plan_change_id #3` | `string` | 标量/message；语义必填见规格 | `planChangeId` |

## ReadNextPeriodObligationRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `subscription_id #2` | `string` | 标量/message；语义必填见规格 | `subscriptionId` |
| `period_start #3` | `Timestamp` | 标量/message；语义必填见规格 | `periodStart` |
| `obligation_id #4` | `string` | proto3 optional | `obligationId` |

## NextPeriodObligation

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `subscription_id #3` | `string` | 标量/message；语义必填见规格 | `subscriptionId` |
| `period_start #4` | `Timestamp` | 标量/message；语义必填见规格 | `periodStart` |
| `period_end #5` | `Timestamp` | 标量/message；语义必填见规格 | `periodEnd` |
| `plan_change_id #6` | `string` | proto3 optional | `planChangeId` |
| `target_compute_plan_id #7` | `string` | 标量/message；语义必填见规格 | `targetComputePlanId` |
| `target_storage_plan_id #8` | `string` | 标量/message；语义必填见规格 | `targetStoragePlanId` |
| `accepted_target_price_policy_version_id #9` | `string` | 标量/message；语义必填见规格 | `acceptedTargetPricePolicyVersionId` |
| `amount_usd_micros #10` | `int64` | 标量/message；语义必填见规格 | `amountUsdMicros` |
| `accepted_quote_id #11` | `string` | 标量/message；语义必填见规格 | `acceptedQuoteId` |
| `operation_id #12` | `string` | 标量/message；语义必填见规格 | `operationId` |
| `status #13` | `PeriodObligationStatus` | 标量/message；语义必填见规格 | `status` |
| `wallet_operation_id #14` | `string` | proto3 optional | `walletOperationId` |
| `confirmed_charge_receipt_id #15` | `string` | proto3 optional | `confirmedChargeReceiptId` |
| `zero_amount_receipt_id #16` | `string` | proto3 optional | `zeroAmountReceiptId` |
| `source_subscription_version #17` | `int64` | 标量/message；语义必填见规格 | `sourceSubscriptionVersion` |
| `confirmed_subscription_version #18` | `int64` | proto3 optional | `confirmedSubscriptionVersion` |
| `renewal_consent_id #19` | `string` | proto3 optional | `renewalConsentId` |
| `outcome #20` | `Observation` | 标量/message；语义必填见规格 | `outcome` |
| `version #21` | `int64` | 标量/message；语义必填见规格 | `version` |

## ReadPlanChangeFailureRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `plan_change_id #3` | `string` | 标量/message；语义必填见规格 | `planChangeId` |
| `failure_receipt_id #4` | `string` | 标量/message；语义必填见规格 | `failureReceiptId` |

## PlanTransitionRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `source_compute_plan_id #3` | `string` | 标量/message；语义必填见规格 | `sourceComputePlanId` |
| `source_storage_plan_id #4` | `string` | 标量/message；语义必填见规格 | `sourceStoragePlanId` |
| `target_compute_plan_id #5` | `string` | 标量/message；语义必填见规格 | `targetComputePlanId` |
| `target_storage_plan_id #6` | `string` | 标量/message；语义必填见规格 | `targetStoragePlanId` |
| `resource_set_id #7` | `string` | 标量/message；语义必填见规格 | `resourceSetId` |
| `expected_resource_version #8` | `string` | 标量/message；语义必填见规格 | `expectedResourceVersion` |

## ApprovedPlanTransition

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `kind #3` | `PlanChangeKindEnum` | 标量/message；语义必填见规格 | `kind` |
| `provider_profile_id #4` | `string` | 标量/message；语义必填见规格 | `providerProfileId` |
| `capability_class #5` | `string` | 标量/message；语义必填见规格 | `capabilityClass` |
| `source #6` | [ResourcePlanSnapshot](rpc-messages.md#resourceplansnapshot) | 标量/message；语义必填见规格 | `source` |
| `target #7` | [ResourcePlanSnapshot](rpc-messages.md#resourceplansnapshot) | 标量/message；语义必填见规格 | `target` |
| `provider_capability_version #8` | `string` | 标量/message；语义必填见规格 | `providerCapabilityVersion` |
| `storage_shrink_supported #9` | `bool` | 标量/message；语义必填见规格 | `storageShrinkSupported` |
| `expected_interruption #10` | `string` | 标量/message；语义必填见规格 | `expectedInterruption` |
| `reversibility #11` | `TransitionReversibility` | 标量/message；语义必填见规格 | `reversibility` |
| `admission_receipt_id #12` | `string` | 标量/message；语义必填见规格 | `admissionReceiptId` |
| `observed_at #13` | `Timestamp` | 标量/message；语义必填见规格 | `observedAt` |
| `expires_at #14` | `Timestamp` | 标量/message；语义必填见规格 | `expiresAt` |
| `outcome #15` | `Observation` | 标量/message；语义必填见规格 | `outcome` |
| `execution_plan #16` | [ProviderPlanChangeExecutionPlanReference](rpc-messages.md#providerplanchangeexecutionplanreference) | 标量/message；语义必填见规格 | `executionPlan` |

## ConfirmedPlanChangeCharge

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `wallet_operation_id #1` | `string` | 标量/message；语义必填见规格 | `walletOperationId` |
| `charge_receipt_id #2` | `string` | 标量/message；语义必填见规格 | `chargeReceiptId` |
| `amount_usd_micros #3` | `int64` | 标量/message；语义必填见规格 | `amountUsdMicros` |

## ZeroAmountPlanChangeEvidence

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `zero_amount_receipt_id #1` | `string` | 标量/message；语义必填见规格 | `zeroAmountReceiptId` |

## PlanChangeFundingEvidence

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `confirmed_charge #1` | [ConfirmedPlanChangeCharge](rpc-messages.md#confirmedplanchangecharge) | oneof funding | `confirmedCharge` |
| `zero_amount #2` | [ZeroAmountPlanChangeEvidence](rpc-messages.md#zeroamountplanchangeevidence) | oneof funding | `zeroAmount` |

## PlanChangeSupplementChargeCommand

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `plan_change_id #3` | `string` | 标量/message；语义必填见规格 | `planChangeId` |
| `subscription_period_id #4` | `string` | 标量/message；语义必填见规格 | `subscriptionPeriodId` |
| `quote_acceptance_id #5` | `string` | 标量/message；语义必填见规格 | `quoteAcceptanceId` |
| `original_charge_identity #6` | `string` | 标量/message；语义必填见规格 | `originalChargeIdentity` |
| `amount_usd_micros #7` | `int64` | 标量/message；语义必填见规格 | `amountUsdMicros` |
| `coverage_start #8` | `Timestamp` | 标量/message；语义必填见规格 | `coverageStart` |
| `coverage_end #9` | `Timestamp` | 标量/message；语义必填见规格 | `coverageEnd` |
| `policy_version #10` | `string` | 标量/message；语义必填见规格 | `policyVersion` |
| `source_subscription_version #11` | `int64` | 标量/message；语义必填见规格 | `sourceSubscriptionVersion` |
| `coverage_start_milliseconds #12` | `int64` | 标量/message；语义必填见规格 | `coverageStartMilliseconds` |
| `coverage_end_milliseconds #13` | `int64` | 标量/message；语义必填见规格 | `coverageEndMilliseconds` |
| `source_financial_snapshot_digest #14` | `string` | 标量/message；语义必填见规格 | `sourceFinancialSnapshotDigest` |

## ScheduledPeriodChargeCommand

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `plan_change_id #3` | `string` | 标量/message；语义必填见规格 | `planChangeId` |
| `subscription_id #4` | `string` | 标量/message；语义必填见规格 | `subscriptionId` |
| `period_obligation_id #5` | `string` | 标量/message；语义必填见规格 | `periodObligationId` |
| `period_start #6` | `Timestamp` | 标量/message；语义必填见规格 | `periodStart` |
| `period_end #7` | `Timestamp` | 标量/message；语义必填见规格 | `periodEnd` |
| `accepted_target_quote_id #8` | `string` | 标量/message；语义必填见规格 | `acceptedTargetQuoteId` |
| `target_price_policy_version_id #9` | `string` | 标量/message；语义必填见规格 | `targetPricePolicyVersionId` |
| `amount_usd_micros #10` | `int64` | 标量/message；语义必填见规格 | `amountUsdMicros` |
| `renewal_consent_id #11` | `string` | proto3 optional | `renewalConsentId` |

## PlanChangeFailureRefundCommand

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `evidence #2` | [SupplementalRefundEvidence](rpc-messages.md#supplementalrefundevidence) | 标量/message；语义必填见规格 | `evidence` |
| `original_external_payment_reference #3` | `string` | 标量/message；语义必填见规格 | `originalExternalPaymentReference` |
| `refund_identity #4` | `string` | 标量/message；语义必填见规格 | `refundIdentity` |

## SupplementDeletionRefundCommand

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `evidence #2` | [SupplementalRefundEvidence](rpc-messages.md#supplementalrefundevidence) | 标量/message；语义必填见规格 | `evidence` |
| `original_external_payment_reference #3` | `string` | 标量/message；语义必填见规格 | `originalExternalPaymentReference` |
| `refund_identity #4` | `string` | 标量/message；语义必填见规格 | `refundIdentity` |

## RestorePlanChangeRuntimeCommand

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `plan_change_id #3` | `string` | 标量/message；语义必填见规格 | `planChangeId` |
| `deployment_id #4` | `string` | 标量/message；语义必填见规格 | `deploymentId` |
| `runtime_instance_id #5` | `string` | 标量/message；语义必填见规格 | `runtimeInstanceId` |
| `confirmed_resource_action_id #6` | `string` | 标量/message；语义必填见规格 | `confirmedResourceActionId` |
| `target #7` | [ResourcePlanSnapshot](rpc-messages.md#resourceplansnapshot) | 标量/message；语义必填见规格 | `target` |
| `execution_epoch #8` | `int64` | 标量/message；语义必填见规格 | `executionEpoch` |
| `unchanged_application_descriptor_digest #9` | `string` | 标量/message；语义必填见规格 | `unchangedApplicationDescriptorDigest` |
| `current_workspace_version #10` | `int64` | 标量/message；语义必填见规格 | `currentWorkspaceVersion` |

## PlanChangeRuntimeReadback

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `workspace_id #1` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `plan_change_id #2` | `string` | 标量/message；语义必填见规格 | `planChangeId` |
| `runtime #3` | [RuntimeReadback](rpc-messages.md#runtimereadback) | 标量/message；语义必填见规格 | `runtime` |
| `resources #4` | [ResourceReadback](rpc-messages.md#resourcereadback) | 标量/message；语义必填见规格 | `resources` |
| `target_limits_confirmed #5` | `bool` | 标量/message；语义必填见规格 | `targetLimitsConfirmed` |
| `receipt_id #6` | `string` | 标量/message；语义必填见规格 | `receiptId` |
| `outcome #7` | `Observation` | 标量/message；语义必填见规格 | `outcome` |

## AppendPlanChangeReceiptRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `kind #2` | `ReceiptKindEnum` | 标量/message；语义必填见规格 | `kind` |
| `evidence #3` | [PlanChangeEvidence](rpc-messages.md#planchangeevidence) | 标量/message；语义必填见规格 | `evidence` |
| `accepted_calculation #4` | [PlanChangeCalculation](rpc-messages.md#planchangecalculation) | 标量/message；语义必填见规格 | `acceptedCalculation` |
| `owner_evidence_reference #5` | `string` | 标量/message；语义必填见规格 | `ownerEvidenceReference` |

## AppendPlanChangeRefundReceiptRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `kind #2` | `ReceiptKindEnum` | 标量/message；语义必填见规格 | `kind` |
| `evidence #3` | [SupplementalRefundEvidence](rpc-messages.md#supplementalrefundevidence) | 标量/message；语义必填见规格 | `evidence` |
| `wallet_readback #4` | [WalletOperation](rpc-messages.md#walletoperation) | 标量/message；语义必填见规格 | `walletReadback` |
| `owner_evidence_reference #5` | `string` | 标量/message；语义必填见规格 | `ownerEvidenceReference` |

## ProviderPlanChangeExecutionPlanReference

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `id #1` | `string` | 标量/message；语义必填见规格 | `id` |
| `digest #2` | `string` | 标量/message；语义必填见规格 | `digest` |
| `strategy #3` | `PlanChangeExecutionStrategy` | 标量/message；语义必填见规格 | `strategy` |
| `approval_receipt_id #4` | `string` | 标量/message；语义必填见规格 | `approvalReceiptId` |

## ReadProviderExecutionPlanRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `context #1` | [CallContext](rpc-messages.md#callcontext) | 标量/message；语义必填见规格 | `context` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `reference #3` | [ProviderPlanChangeExecutionPlanReference](rpc-messages.md#providerplanchangeexecutionplanreference) | 标量/message；语义必填见规格 | `reference` |

## ProviderPlanChangeExecutionPlan

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `reference #1` | [ProviderPlanChangeExecutionPlanReference](rpc-messages.md#providerplanchangeexecutionplanreference) | 标量/message；语义必填见规格 | `reference` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `provider_profile_id #3` | `string` | 标量/message；语义必填见规格 | `providerProfileId` |
| `source_resource_set_id #4` | `string` | 标量/message；语义必填见规格 | `sourceResourceSetId` |
| `expected_source_resource_version #5` | `string` | 标量/message；语义必填见规格 | `expectedSourceResourceVersion` |
| `source_compute_resource_id #6` | `string` | 标量/message；语义必填见规格 | `sourceComputeResourceId` |
| `source_storage_resource_id #7` | `string` | 标量/message；语义必填见规格 | `sourceStorageResourceId` |
| `source_attachment_id #8` | `string` | 标量/message；语义必填见规格 | `sourceAttachmentId` |
| `source #9` | [ResourcePlanSnapshot](rpc-messages.md#resourceplansnapshot) | 标量/message；语义必填见规格 | `source` |
| `target #10` | [ResourcePlanSnapshot](rpc-messages.md#resourceplansnapshot) | 标量/message；语义必填见规格 | `target` |
| `target_pool_id #11` | `string` | proto3 optional | `targetPoolId` |
| `storage_action #12` | `PlanChangeStorageAction` | 标量/message；语义必填见规格 | `storageAction` |
| `filesystem_expansion_required #13` | `bool` | 标量/message；语义必填见规格 | `filesystemExpansionRequired` |
| `drain_required #14` | `bool` | 标量/message；语义必填见规格 | `drainRequired` |
| `preserve_storage_identity #15` | `bool` | 标量/message；语义必填见规格 | `preserveStorageIdentity` |
| `approved_at #16` | `Timestamp` | 标量/message；语义必填见规格 | `approvedAt` |

## SourceFinancialSnapshot

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `schema_version #1` | `int32` | 标量/message；语义必填见规格 | `schemaVersion` |
| `subscription_id #2` | `string` | 标量/message；语义必填见规格 | `subscriptionId` |
| `subscription_version #3` | `int64` | 标量/message；语义必填见规格 | `subscriptionVersion` |
| `period_id #4` | `string` | 标量/message；语义必填见规格 | `periodId` |
| `compute_plan_id #5` | `string` | 标量/message；语义必填见规格 | `computePlanId` |
| `storage_plan_id #6` | `string` | 标量/message；语义必填见规格 | `storagePlanId` |
| `accepted_price_policy_version_id #7` | `string` | 标量/message；语义必填见规格 | `acceptedPricePolicyVersionId` |
| `accepted_monthly_usd_micros #8` | `int64` | 标量/message；语义必填见规格 | `acceptedMonthlyUsdMicros` |
| `original_period_start #9` | `string` | 标量/message；语义必填见规格 | `originalPeriodStart` |
| `original_period_end #10` | `string` | 标量/message；语义必填见规格 | `originalPeriodEnd` |
| `period_start_milliseconds #11` | `int64` | 标量/message；语义必填见规格 | `periodStartMilliseconds` |
| `period_end_milliseconds #12` | `int64` | 标量/message；语义必填见规格 | `periodEndMilliseconds` |
| `billing_anchor_day #13` | `int32` | 标量/message；语义必填见规格 | `billingAnchorDay` |
| `source_owner_evidence_reference #14` | `string` | 标量/message；语义必填见规格 | `sourceOwnerEvidenceReference` |

## PackageUploadedEvent

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `package_version_id #1` | `string` | 标量/message；语义必填见规格 | `packageVersionId` |
| `package_id #2` | `string` | 标量/message；语义必填见规格 | `packageId` |
| `sha256 #3` | `string` | 标量/message；语义必填见规格 | `sha256` |
| `size_bytes #4` | `int64` | 标量/message；语义必填见规格 | `sizeBytes` |

## BuildArtifactConfirmedEvent

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `build_job_id #1` | `string` | 标量/message；语义必填见规格 | `buildJobId` |
| `package_version_id #2` | `string` | 标量/message；语义必填见规格 | `packageVersionId` |
| `runtime_version_id #3` | `string` | 标量/message；语义必填见规格 | `runtimeVersionId` |
| `webui_version_id #4` | `string` | 标量/message；语义必填见规格 | `webuiVersionId` |
| `artifact_digest #5` | `string` | 标量/message；语义必填见规格 | `artifactDigest` |
| `artifact_receipt_id #6` | `string` | 标量/message；语义必填见规格 | `artifactReceiptId` |
| `deployment_descriptor_digest #7` | `string` | 标量/message；语义必填见规格 | `deploymentDescriptorDigest` |

## CapabilityVersionRegisteredEvent

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `capability_version_id #1` | `string` | 标量/message；语义必填见规格 | `capabilityVersionId` |
| `build_job_id #2` | `string` | 标量/message；语义必填见规格 | `buildJobId` |
| `artifact_digest #3` | `string` | 标量/message；语义必填见规格 | `artifactDigest` |

## BuildFailedEvent

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `build_job_id #1` | `string` | 标量/message；语义必填见规格 | `buildJobId` |
| `error_code #2` | `string` | 标量/message；语义必填见规格 | `errorCode` |

## WalletOperationObservedEvent

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `wallet_operation_id #1` | `string` | 标量/message；语义必填见规格 | `walletOperationId` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `kind #3` | `string` | 标量/message；语义必填见规格 | `kind` |
| `status #4` | `string` | 标量/message；语义必填见规格 | `status` |
| `amount_usd_micros #5` | `int64` | 标量/message；语义必填见规格 | `amountUsdMicros` |
| `external_reference #6` | `string` | proto3 optional | `externalReference` |
| `receipt_id #7` | `string` | proto3 optional | `receiptId` |
| `purpose #8` | `string` | proto3 optional | `purpose` |
| `plan_change_id #9` | `string` | proto3 optional | `planChangeId` |
| `original_charge_operation_id #10` | `string` | proto3 optional | `originalChargeOperationId` |
| `coverage_start #11` | `Timestamp` | 标量/message；语义必填见规格 | `coverageStart` |
| `coverage_end #12` | `Timestamp` | 标量/message；语义必填见规格 | `coverageEnd` |

## ResourcesObservedEvent

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `resource_set_id #1` | `string` | 标量/message；语义必填见规格 | `resourceSetId` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `resource_action_id #3` | `string` | 标量/message；语义必填见规格 | `resourceActionId` |
| `outcome #4` | `string` | 标量/message；语义必填见规格 | `outcome` |
| `absence_confirmed #5` | `bool` | 标量/message；语义必填见规格 | `absenceConfirmed` |
| `receipt_id #6` | `string` | proto3 optional | `receiptId` |

## RuntimeReadinessObservedEvent

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `runtime_instance_id #1` | `string` | 标量/message；语义必填见规格 | `runtimeInstanceId` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `deployment_id #3` | `string` | 标量/message；语义必填见规格 | `deploymentId` |
| `outcome #4` | `string` | 标量/message；语义必填见规格 | `outcome` |
| `application_available #5` | `bool` | 标量/message；语义必填见规格 | `applicationAvailable` |
| `credential_injection_verified #6` | `bool` | 标量/message；语义必填见规格 | `credentialInjectionVerified` |
| `applied_model_configuration_version #7` | `int64` | 标量/message；语义必填见规格 | `appliedModelConfigurationVersion` |
| `receipt_id #8` | `string` | proto3 optional | `receiptId` |

## WorkspaceStateChangedEvent

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `workspace_id #1` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `operation_id #2` | `string` | 标量/message；语义必填见规格 | `operationId` |
| `status #3` | `string` | 标量/message；语义必填见规格 | `status` |
| `active_deployment_id #4` | `string` | proto3 optional | `activeDeploymentId` |

## WorkspaceDeletionConfirmedEvent

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `workspace_id #1` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `operation_id #2` | `string` | 标量/message；语义必填见规格 | `operationId` |
| `resource_deletion_receipt_id #3` | `string` | 标量/message；语义必填见规格 | `resourceDeletionReceiptId` |
| `data_deletion_receipt_id #4` | `string` | 标量/message；语义必填见规格 | `dataDeletionReceiptId` |
| `deleted_at #5` | `Timestamp` | 标量/message；语义必填见规格 | `deletedAt` |

## TenantAccessRevokedEvent

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `target_tenant_id #1` | `string` | 标量/message；语义必填见规格 | `targetTenantId` |
| `tenant_operation_id #2` | `string` | 标量/message；语义必填见规格 | `tenantOperationId` |
| `status #3` | `string` | 标量/message；语义必填见规格 | `status` |
| `restore_until #4` | `Timestamp` | 标量/message；语义必填见规格 | `restoreUntil` |

## TenantRestoredEvent

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `target_tenant_id #1` | `string` | 标量/message；语义必填见规格 | `targetTenantId` |
| `tenant_operation_id #2` | `string` | 标量/message；语义必填见规格 | `tenantOperationId` |
| `restored_at #3` | `Timestamp` | 标量/message；语义必填见规格 | `restoredAt` |

## ReceiptRecordedEvent

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `receipt_id #1` | `string` | 标量/message；语义必填见规格 | `receiptId` |
| `source_owner #2` | `string` | 标量/message；语义必填见规格 | `sourceOwner` |
| `owner_evidence_reference #3` | `string` | 标量/message；语义必填见规格 | `ownerEvidenceReference` |
| `evidence_digest #4` | `string` | 标量/message；语义必填见规格 | `evidenceDigest` |

## CatalogPolicyChangedEvent

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `policy_version_id #1` | `string` | 标量/message；语义必填见规格 | `policyVersionId` |
| `policy_kind #2` | `string` | 标量/message；语义必填见规格 | `policyKind` |
| `valid_from #3` | `Timestamp` | 标量/message；语义必填见规格 | `validFrom` |

## TenantReenabledEvent

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `target_tenant_id #1` | `string` | 标量/message；语义必填见规格 | `targetTenantId` |
| `tenant_operation_id #2` | `string` | 标量/message；语义必填见规格 | `tenantOperationId` |
| `original_tenant_suspend_operation_id #3` | `string` | 标量/message；语义必填见规格 | `originalTenantSuspendOperationId` |
| `enabled_at #4` | `Timestamp` | 标量/message；语义必填见规格 | `enabledAt` |

## RenewalSettingsChangedEvent

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `workspace_id #1` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `subscription_id #2` | `string` | 标量/message；语义必填见规格 | `subscriptionId` |
| `consent_id #3` | `string` | proto3 optional | `consentId` |
| `renewal_mode #4` | `string` | 标量/message；语义必填见规格 | `renewalMode` |
| `settings_version #5` | `int64` | 标量/message；语义必填见规格 | `settingsVersion` |

## RouteObservedEvent

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `workspace_id #1` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `switch_id #2` | `string` | 标量/message；语义必填见规格 | `switchId` |
| `action_kind #3` | `string` | 标量/message；语义必填见规格 | `actionKind` |
| `route_generation #4` | `int64` | 标量/message；语义必填见规格 | `routeGeneration` |
| `execution_epoch #5` | `int64` | 标量/message；语义必填见规格 | `executionEpoch` |
| `provider_revision #6` | `string` | 标量/message；语义必填见规格 | `providerRevision` |
| `target_execution_resource_id #7` | `string` | proto3 optional | `targetExecutionResourceId` |
| `outcome #8` | `string` | 标量/message；语义必填见规格 | `outcome` |
| `route_receipt_id #9` | `string` | proto3 optional | `routeReceiptId` |
| `route_binding_id #10` | `string` | 标量/message；语义必填见规格 | `routeBindingId` |

## PlanChangeStateChangedEvent

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `plan_change_id #1` | `string` | 标量/message；语义必填见规格 | `planChangeId` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `kind #3` | `string` | 标量/message；语义必填见规格 | `kind` |
| `status #4` | `string` | 标量/message；语义必填见规格 | `status` |
| `schedule_version #5` | `int64` | 标量/message；语义必填见规格 | `scheduleVersion` |
| `delivery_outcome #6` | `string` | 标量/message；语义必填见规格 | `deliveryOutcome` |
| `resource_outcome #7` | `string` | 标量/message；语义必填见规格 | `resourceOutcome` |
| `operation_id #8` | `string` | 标量/message；语义必填见规格 | `operationId` |
| `execution_operation_id #9` | `string` | proto3 optional | `executionOperationId` |
| `applied_at #10` | `Timestamp` | 标量/message；语义必填见规格 | `appliedAt` |
| `policy_version #11` | `string` | 标量/message；语义必填见规格 | `policyVersion` |

## PeriodObligationChangedEvent

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `obligation_id #1` | `string` | 标量/message；语义必填见规格 | `obligationId` |
| `workspace_id #2` | `string` | 标量/message；语义必填见规格 | `workspaceId` |
| `subscription_id #3` | `string` | 标量/message；语义必填见规格 | `subscriptionId` |
| `period_start #4` | `Timestamp` | 标量/message；语义必填见规格 | `periodStart` |
| `period_end #5` | `Timestamp` | 标量/message；语义必填见规格 | `periodEnd` |
| `plan_change_id #6` | `string` | proto3 optional | `planChangeId` |
| `status #7` | `string` | 标量/message；语义必填见规格 | `status` |
| `target_price_policy_version_id #8` | `string` | 标量/message；语义必填见规格 | `targetPricePolicyVersionId` |
| `amount_usd_micros #9` | `int64` | 标量/message；语义必填见规格 | `amountUsdMicros` |
| `wallet_operation_id #10` | `string` | proto3 optional | `walletOperationId` |
| `receipt_id #11` | `string` | proto3 optional | `receiptId` |

## EventEnvelope

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `event_id #1` | `string` | 标量/message；语义必填见规格 | `eventId` |
| `event_type #2` | `string` | 标量/message；语义必填见规格 | `eventType` |
| `schema_version #3` | `int32` | 标量/message；语义必填见规格 | `schemaVersion` |
| `owner #4` | `string` | 标量/message；语义必填见规格 | `owner` |
| `tenant_id #5` | `string` | 标量/message；语义必填见规格 | `tenantId` |
| `aggregate_id #6` | `string` | 标量/message；语义必填见规格 | `aggregateId` |
| `aggregate_version #7` | `int64` | 标量/message；语义必填见规格 | `aggregateVersion` |
| `occurred_at #8` | `Timestamp` | 标量/message；语义必填见规格 | `occurredAt` |
| `request_id #9` | `string` | 标量/message；语义必填见规格 | `requestId` |
| `scope #10` | `string` | 标量/message；语义必填见规格 | `scope` |
| `package_uploaded #20` | [PackageUploadedEvent](rpc-messages.md#packageuploadedevent) | oneof payload | `packageUploaded` |
| `build_artifact_confirmed #21` | [BuildArtifactConfirmedEvent](rpc-messages.md#buildartifactconfirmedevent) | oneof payload | `buildArtifactConfirmed` |
| `capability_version_registered #22` | [CapabilityVersionRegisteredEvent](rpc-messages.md#capabilityversionregisteredevent) | oneof payload | `capabilityVersionRegistered` |
| `build_failed #23` | [BuildFailedEvent](rpc-messages.md#buildfailedevent) | oneof payload | `buildFailed` |
| `wallet_operation_observed #24` | [WalletOperationObservedEvent](rpc-messages.md#walletoperationobservedevent) | oneof payload | `walletOperationObserved` |
| `resources_observed #25` | [ResourcesObservedEvent](rpc-messages.md#resourcesobservedevent) | oneof payload | `resourcesObserved` |
| `runtime_readiness_observed #26` | [RuntimeReadinessObservedEvent](rpc-messages.md#runtimereadinessobservedevent) | oneof payload | `runtimeReadinessObserved` |
| `workspace_state_changed #27` | [WorkspaceStateChangedEvent](rpc-messages.md#workspacestatechangedevent) | oneof payload | `workspaceStateChanged` |
| `workspace_deletion_confirmed #28` | [WorkspaceDeletionConfirmedEvent](rpc-messages.md#workspacedeletionconfirmedevent) | oneof payload | `workspaceDeletionConfirmed` |
| `tenant_access_revoked #29` | [TenantAccessRevokedEvent](rpc-messages.md#tenantaccessrevokedevent) | oneof payload | `tenantAccessRevoked` |
| `tenant_restored #30` | [TenantRestoredEvent](rpc-messages.md#tenantrestoredevent) | oneof payload | `tenantRestored` |
| `receipt_recorded #31` | [ReceiptRecordedEvent](rpc-messages.md#receiptrecordedevent) | oneof payload | `receiptRecorded` |
| `catalog_policy_changed #32` | [CatalogPolicyChangedEvent](rpc-messages.md#catalogpolicychangedevent) | oneof payload | `catalogPolicyChanged` |
| `tenant_reenabled #33` | [TenantReenabledEvent](rpc-messages.md#tenantreenabledevent) | oneof payload | `tenantReenabled` |
| `renewal_settings_changed #34` | [RenewalSettingsChangedEvent](rpc-messages.md#renewalsettingschangedevent) | oneof payload | `renewalSettingsChanged` |
| `route_observed #35` | [RouteObservedEvent](rpc-messages.md#routeobservedevent) | oneof payload | `routeObserved` |
| `plan_change_state_changed #36` | [PlanChangeStateChangedEvent](rpc-messages.md#planchangestatechangedevent) | oneof payload | `planChangeStateChanged` |
| `period_obligation_changed #37` | [PeriodObligationChangedEvent](rpc-messages.md#periodobligationchangedevent) | oneof payload | `periodObligationChanged` |

## DeliverEventRequest

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `event #1` | [EventEnvelope](rpc-messages.md#eventenvelope) | 标量/message；语义必填见规格 | `event` |
| `authenticated_producer #2` | `string` | 标量/message；语义必填见规格 | `authenticatedProducer` |
| `consumer_owner #3` | `OwnerEnum` | 标量/message；语义必填见规格 | `consumerOwner` |

## InboxAck

| 字段 # | wire 类型 | 存在性 / oneof | JSON名 |
| --- | --- | --- | --- |
| `event_id #1` | `string` | 标量/message；语义必填见规格 | `eventId` |
| `consumer #2` | `string` | 标量/message；语义必填见规格 | `consumer` |
| `committed #3` | `bool` | 标量/message；语义必填见规格 | `committed` |
| `duplicate #4` | `bool` | 标量/message；语义必填见规格 | `duplicate` |
| `applied_aggregate_version #5` | `int64` | 标量/message；语义必填见规格 | `appliedAggregateVersion` |
| `rejection_code #6` | `string` | 标量/message；语义必填见规格 | `rejectionCode` |

## 枚举（完整数值）

### Observation

`OBSERVATION_UNSPECIFIED=0`; `OBSERVATION_CONFIRMED=1`; `OBSERVATION_REJECTED=2`; `OBSERVATION_UNKNOWN=3`

### ReferenceClaimState

`REFERENCE_CLAIM_STATE_UNSPECIFIED=0`; `REFERENCE_CLAIM_STATE_ACQUIRED=1`; `REFERENCE_CLAIM_STATE_BOUND=2`; `REFERENCE_CLAIM_STATE_RELEASED=3`

### TerminalOperationStatus

`TERMINAL_OPERATION_STATUS_UNSPECIFIED=0`; `TERMINAL_OPERATION_STATUS_SUCCEEDED=1`; `TERMINAL_OPERATION_STATUS_FAILED=2`; `TERMINAL_OPERATION_STATUS_CANCELLED=3`

### AuthorizationResourceKind

`AUTHORIZATION_RESOURCE_KIND_UNSPECIFIED=0`; `AUTHORIZATION_RESOURCE_KIND_PLATFORM=1`; `AUTHORIZATION_RESOURCE_KIND_TENANT=2`; `AUTHORIZATION_RESOURCE_KIND_PACKAGE=3`; `AUTHORIZATION_RESOURCE_KIND_BUILD=4`; `AUTHORIZATION_RESOURCE_KIND_VERSION=5`; `AUTHORIZATION_RESOURCE_KIND_WORKSPACE=6`; `AUTHORIZATION_RESOURCE_KIND_SUBSCRIPTION=7`; `AUTHORIZATION_RESOURCE_KIND_OPERATION=8`; `AUTHORIZATION_RESOURCE_KIND_KEY=9`; `AUTHORIZATION_RESOURCE_KIND_ROUTE=10`; `AUTHORIZATION_RESOURCE_KIND_RECEIPT=11`; `AUTHORIZATION_RESOURCE_KIND_CATALOG=12`

### AuthorizationResult

`AUTHORIZATION_RESULT_UNSPECIFIED=0`; `AUTHORIZATION_RESULT_ALLOWED=1`; `AUTHORIZATION_RESULT_DENIED=2`

### AuthorizationIssuer

`AUTHORIZATION_ISSUER_UNSPECIFIED=0`; `AUTHORIZATION_ISSUER_CLOUD_IDENTITY=1`

### AcceptedGrantMode

`ACCEPTED_GRANT_MODE_UNSPECIFIED=0`; `ACCEPTED_GRANT_MODE_CONTINUE_ORIGINAL=1`; `ACCEPTED_GRANT_MODE_CLOSEOUT_ONLY=2`; `ACCEPTED_GRANT_MODE_REVOKED=3`

### RuntimeInstanceState

`RUNTIME_INSTANCE_STATE_UNSPECIFIED=0`; `RUNTIME_INSTANCE_STATE_PENDING=1`; `RUNTIME_INSTANCE_STATE_STARTING=2`; `RUNTIME_INSTANCE_STATE_READY=3`; `RUNTIME_INSTANCE_STATE_STOPPED=4`; `RUNTIME_INSTANCE_STATE_FAILED=5`; `RUNTIME_INSTANCE_STATE_TERMINATING=6`; `RUNTIME_INSTANCE_STATE_TERMINATED=7`

### PeriodObligationStatus

`PERIOD_OBLIGATION_STATUS_UNSPECIFIED=0`; `PERIOD_OBLIGATION_STATUS_AWAITING_PAYMENT=1`; `PERIOD_OBLIGATION_STATUS_ACCEPTED=2`; `PERIOD_OBLIGATION_STATUS_CONFIRMED=3`; `PERIOD_OBLIGATION_STATUS_FAILED=4`; `PERIOD_OBLIGATION_STATUS_NEEDS_ATTENTION=5`

### TransitionReversibility

`TRANSITION_REVERSIBILITY_UNSPECIFIED=0`; `TRANSITION_REVERSIBILITY_VERIFIED_RESTORE=1`; `TRANSITION_REVERSIBILITY_IRREVERSIBLE_STORAGE_GROWTH=2`; `TRANSITION_REVERSIBILITY_NO_RESTORE=3`

### PlanChangeExecutionStrategy

`PLAN_CHANGE_EXECUTION_STRATEGY_UNSPECIFIED=0`; `PLAN_CHANGE_EXECUTION_STRATEGY_IN_PLACE_RESIZE=1`; `PLAN_CHANGE_EXECUTION_STRATEGY_CLAIM_TARGET_POOL_AND_REBIND_EXISTING_STORAGE=2`

### PlanChangeStorageAction

`PLAN_CHANGE_STORAGE_ACTION_UNSPECIFIED=0`; `PLAN_CHANGE_STORAGE_ACTION_KEEP=1`; `PLAN_CHANGE_STORAGE_ACTION_EXPAND=2`

### ErrorCodeEnum

`ERROR_CODE_ENUM_UNSPECIFIED=0`; `ERROR_CODE_ENUM_VALIDATION_FAILED=1`; `ERROR_CODE_ENUM_UNAUTHENTICATED=2`; `ERROR_CODE_ENUM_FORBIDDEN=3`; `ERROR_CODE_ENUM_NOT_FOUND=4`; `ERROR_CODE_ENUM_CSRF_INVALID=5`; `ERROR_CODE_ENUM_ORIGIN_REJECTED=6`; `ERROR_CODE_ENUM_IDEMPOTENCY_REQUIRED=7`; `ERROR_CODE_ENUM_IDEMPOTENCY_CONFLICT=8`; `ERROR_CODE_ENUM_VERSION_CONFLICT=9`; `ERROR_CODE_ENUM_LAST_OWNER=10`; `ERROR_CODE_ENUM_INVITATION_INVALID=11`; `ERROR_CODE_ENUM_TENANT_INACTIVE=12`; `ERROR_CODE_ENUM_TENANT_RESTORE_EXPIRED=13`; `ERROR_CODE_ENUM_GATEWAY_UNAVAILABLE=14`; `ERROR_CODE_ENUM_OWNER_CAPABILITY_UNAVAILABLE=15`; `ERROR_CODE_ENUM_WALLET_BINDING_REQUIRED=16`; `ERROR_CODE_ENUM_INSUFFICIENT_BALANCE=17`; `ERROR_CODE_ENUM_QUOTE_EXPIRED=18`; `ERROR_CODE_ENUM_QUOTE_MISMATCH=19`; `ERROR_CODE_ENUM_POLICY_UNCONFIGURED=20`; `ERROR_CODE_ENUM_CAPACITY_UNAVAILABLE=21`; `ERROR_CODE_ENUM_PROVIDER_CAPABILITY_UNSUPPORTED=22`; `ERROR_CODE_ENUM_RUNTIME_REVOKED=23`; `ERROR_CODE_ENUM_WEBUI_INCOMPATIBLE=24`; `ERROR_CODE_ENUM_MODEL_NOT_ALLOWED=25`; `ERROR_CODE_ENUM_PACKAGE_ARCHIVED=26`; `ERROR_CODE_ENUM_UPLOAD_EXPIRED=27`; `ERROR_CODE_ENUM_UPLOAD_PART_MISMATCH=28`; `ERROR_CODE_ENUM_UPLOAD_CHECKSUM_MISMATCH=29`; `ERROR_CODE_ENUM_PACKAGE_INVALID=30`; `ERROR_CODE_ENUM_BUILD_INPUT_REJECTED=31`; `ERROR_CODE_ENUM_BUILD_FAILED=32`; `ERROR_CODE_ENUM_ARTIFACT_REFERENCED=33`; `ERROR_CODE_ENUM_ARTIFACT_UNAVAILABLE=34`; `ERROR_CODE_ENUM_INCOMPATIBLE_VERSION=35`; `ERROR_CODE_ENUM_DATA_MIGRATION_REQUIRED=36`; `ERROR_CODE_ENUM_ROLLBACK_UNSAFE=37`; `ERROR_CODE_ENUM_WORKSPACE_NOT_READY=38`; `ERROR_CODE_ENUM_WORKSPACE_EXPIRED=39`; `ERROR_CODE_ENUM_OPERATION_IN_PROGRESS=40`; `ERROR_CODE_ENUM_EXTERNAL_OUTCOME_UNKNOWN=41`; `ERROR_CODE_ENUM_RESOURCE_DELETE_UNCONFIRMED=42`; `ERROR_CODE_ENUM_REFUND_PENDING=43`; `ERROR_CODE_ENUM_KEY_REVEAL_FORBIDDEN=44`; `ERROR_CODE_ENUM_APP_ACCESS_UNAVAILABLE=45`; `ERROR_CODE_ENUM_RATE_LIMITED=46`; `ERROR_CODE_ENUM_DEPENDENCY_UNAVAILABLE=47`; `ERROR_CODE_ENUM_INSTANCE_AUTHORIZATION_REQUIRED=48`; `ERROR_CODE_ENUM_INTERNAL_ERROR=49`; `ERROR_CODE_ENUM_PUBLISHER_NAMESPACE_MISMATCH=50`; `ERROR_CODE_ENUM_PUBLISHER_CONTRACT_INVALID=51`; `ERROR_CODE_ENUM_REFERENCE_CLAIM_INVALID=52`; `ERROR_CODE_ENUM_AUTHORIZATION_CONTEXT_EXPIRED=53`; `ERROR_CODE_ENUM_AUTHORIZATION_REVOKED=54`; `ERROR_CODE_ENUM_AUTHORIZATION_AUDIENCE_MISMATCH=55`; `ERROR_CODE_ENUM_ROUTE_GENERATION_CONFLICT=56`; `ERROR_CODE_ENUM_STALE_EXECUTION_EPOCH=57`; `ERROR_CODE_ENUM_TENANT_NOT_SUSPENDED=58`; `ERROR_CODE_ENUM_RENEWAL_PERIOD_ELAPSED=59`; `ERROR_CODE_ENUM_PLAN_CHANGE_EXISTS=60`; `ERROR_CODE_ENUM_PLAN_CHANGE_NOT_CANCELLABLE=61`; `ERROR_CODE_ENUM_PLAN_CHANGE_QUOTE_STALE=62`; `ERROR_CODE_ENUM_PLAN_TRANSITION_NOT_SUPPORTED=63`; `ERROR_CODE_ENUM_PLAN_TRANSITION_MIXED=64`; `ERROR_CODE_ENUM_PLAN_CHANGE_NO_OP=65`; `ERROR_CODE_ENUM_STORAGE_SHRINK_UNSUPPORTED=66`; `ERROR_CODE_ENUM_SUBSCRIPTION_VERSION_CONFLICT=67`; `ERROR_CODE_ENUM_PLAN_CHANGE_PAYMENT_REQUIRED=68`; `ERROR_CODE_ENUM_PLAN_CHANGE_PERIOD_ELAPSED=69`; `ERROR_CODE_ENUM_REFUND_ORIGINAL_CHARGE_CONFLICT=70`; `ERROR_CODE_ENUM_FUTURE_PERIOD_COMMITTED=71`; `ERROR_CODE_ENUM_SCHEDULED_PLAN_APPLICATION_CONFLICT=72`

### OwnerEnum

`OWNER_ENUM_UNSPECIFIED=0`; `OWNER_ENUM_TENANT=1`; `OWNER_ENUM_CAPABILITY=2`; `OWNER_ENUM_BUILD=3`; `OWNER_ENUM_WORKSPACE=4`; `OWNER_ENUM_RUNTIME_CONTROL=5`; `OWNER_ENUM_FABRIC=6`; `OWNER_ENUM_GATEWAY=7`; `OWNER_ENUM_RESOURCE_CATALOG=8`; `OWNER_ENUM_LEDGER=9`

### TenantRoleEnum

`TENANT_ROLE_ENUM_UNSPECIFIED=0`; `TENANT_ROLE_ENUM_OWNER=1`; `TENANT_ROLE_ENUM_ADMIN=2`; `TENANT_ROLE_ENUM_MEMBER=3`

### OperationKindEnum

`OPERATION_KIND_ENUM_UNSPECIFIED=0`; `OPERATION_KIND_ENUM_COMPLETE_UPLOAD=1`; `OPERATION_KIND_ENUM_BUILD=2`; `OPERATION_KIND_ENUM_DELETE_CAPABILITY_VERSION=3`; `OPERATION_KIND_ENUM_CREATE_WORKSPACE=4`; `OPERATION_KIND_ENUM_UPDATE_MODELS=5`; `OPERATION_KIND_ENUM_UPDATE_WORKSPACE=6`; `OPERATION_KIND_ENUM_ROLLBACK_WORKSPACE=7`; `OPERATION_KIND_ENUM_RESIZE_WORKSPACE=8`; `OPERATION_KIND_ENUM_RENEW_WORKSPACE=9`; `OPERATION_KIND_ENUM_DELETE_WORKSPACE=10`; `OPERATION_KIND_ENUM_CREATE_TENANT=11`; `OPERATION_KIND_ENUM_BIND_TENANT_WALLET=12`; `OPERATION_KIND_ENUM_SUSPEND_TENANT=13`; `OPERATION_KIND_ENUM_DELETE_TENANT=14`; `OPERATION_KIND_ENUM_RESTORE_TENANT=15`; `OPERATION_KIND_ENUM_REVOKE_KEY=16`; `OPERATION_KIND_ENUM_RECONCILE=17`; `OPERATION_KIND_ENUM_ADOPT_WORKSPACE=18`; `OPERATION_KIND_ENUM_RESOURCE_PROVISION=19`; `OPERATION_KIND_ENUM_RESOURCE_RESIZE=20`; `OPERATION_KIND_ENUM_RESOURCE_RENEW=21`; `OPERATION_KIND_ENUM_RESOURCE_SUSPEND=22`; `OPERATION_KIND_ENUM_RESOURCE_RESUME=23`; `OPERATION_KIND_ENUM_RESOURCE_DELETE=24`; `OPERATION_KIND_ENUM_RUNTIME_DEPLOY=25`; `OPERATION_KIND_ENUM_RUNTIME_RELOAD=26`; `OPERATION_KIND_ENUM_RUNTIME_RETIRE=27`; `OPERATION_KIND_ENUM_REENABLE_TENANT=28`; `OPERATION_KIND_ENUM_MIGRATION=29`; `OPERATION_KIND_ENUM_UPDATE_RENEWAL_SETTINGS=30`; `OPERATION_KIND_ENUM_APPLY_SCHEDULED_PLAN_CHANGE=31`; `OPERATION_KIND_ENUM_CANCEL_PLAN_CHANGE=32`; `OPERATION_KIND_ENUM_COMPENSATE_PLAN_CHANGE=33`; `OPERATION_KIND_ENUM_REFUND_SUPPLEMENT_ON_DELETE=34`

### OperationStageEnum

`OPERATION_STAGE_ENUM_UNSPECIFIED=0`; `OPERATION_STAGE_ENUM_ABSENCE_VERIFICATION=1`; `OPERATION_STAGE_ENUM_ACCESS_ENABLEMENT=2`; `OPERATION_STAGE_ENUM_ACCESS_REVOCATION=3`; `OPERATION_STAGE_ENUM_ACTIVATION=4`; `OPERATION_STAGE_ENUM_ACTUAL_RESOURCE_EVIDENCE=5`; `OPERATION_STAGE_ENUM_ADMISSION=6`; `OPERATION_STAGE_ENUM_ASSET_CUSTODY=7`; `OPERATION_STAGE_ENUM_ATTACHMENT=8`; `OPERATION_STAGE_ENUM_ATTACHMENT_DELETION=9`; `OPERATION_STAGE_ENUM_BUILDING=10`; `OPERATION_STAGE_ENUM_CANCELLATION_GUARD=11`; `OPERATION_STAGE_ENUM_CATALOG_TOMBSTONE=12`; `OPERATION_STAGE_ENUM_COMPATIBILITY=13`; `OPERATION_STAGE_ENUM_COMPUTE=14`; `OPERATION_STAGE_ENUM_COMPUTE_DELETION=15`; `OPERATION_STAGE_ENUM_CONFIGURATION=16`; `OPERATION_STAGE_ENUM_CONSENT_COMMIT=17`; `OPERATION_STAGE_ENUM_CONSENT_VALIDATION=18`; `OPERATION_STAGE_ENUM_COVERAGE_CALCULATION=19`; `OPERATION_STAGE_ENUM_DEBIT=20`; `OPERATION_STAGE_ENUM_DELETION_EVIDENCE=21`; `OPERATION_STAGE_ENUM_FAILURE_FENCE=22`; `OPERATION_STAGE_ENUM_GRANT_REVOCATION=23`; `OPERATION_STAGE_ENUM_IDENTITY_VERIFICATION=24`; `OPERATION_STAGE_ENUM_KEY=25`; `OPERATION_STAGE_ENUM_KEY_REVOCATION=26`; `OPERATION_STAGE_ENUM_MEMBERSHIP=27`; `OPERATION_STAGE_ENUM_OBLIGATION_CHECK=28`; `OPERATION_STAGE_ENUM_ORIGINAL_ACTION_READBACK=29`; `OPERATION_STAGE_ENUM_OWNER_SWITCH=30`; `OPERATION_STAGE_ENUM_PAYMENT_AUTHORIZATION=31`; `OPERATION_STAGE_ENUM_PERIOD_BOUNDARY=32`; `OPERATION_STAGE_ENUM_PERIOD_UPDATE=33`; `OPERATION_STAGE_ENUM_PLAN_CHANGE_COMMIT=34`; `OPERATION_STAGE_ENUM_PLAN_COMMIT=35`; `OPERATION_STAGE_ENUM_PROVIDER_RENEWAL=36`; `OPERATION_STAGE_ENUM_PROVIDER_RESUME=37`; `OPERATION_STAGE_ENUM_PROVIDER_SUSPEND=38`; `OPERATION_STAGE_ENUM_PUSHING=39`; `OPERATION_STAGE_ENUM_QUEUED=40`; `OPERATION_STAGE_ENUM_QUOTE_BINDING=41`; `OPERATION_STAGE_ENUM_READBACK=42`; `OPERATION_STAGE_ENUM_RECEIPT=43`; `OPERATION_STAGE_ENUM_RECONCILIATION=44`; `OPERATION_STAGE_ENUM_REFERENCE_CHECK=45`; `OPERATION_STAGE_ENUM_REFUND=46`; `OPERATION_STAGE_ENUM_REFUND_ORIGINAL_CHARGE=47`; `OPERATION_STAGE_ENUM_REGISTERING=48`; `OPERATION_STAGE_ENUM_RELOAD=49`; `OPERATION_STAGE_ENUM_RESOURCE_PREFLIGHT=50`; `OPERATION_STAGE_ENUM_RESTORE_WINDOW_CHECK=51`; `OPERATION_STAGE_ENUM_RETIREMENT=52`; `OPERATION_STAGE_ENUM_RUNTIME=53`; `OPERATION_STAGE_ENUM_RUNTIME_DELETION=54`; `OPERATION_STAGE_ENUM_SCHEDULE_CANCEL=55`; `OPERATION_STAGE_ENUM_SCHEDULE_COMMIT=56`; `OPERATION_STAGE_ENUM_SECRET_UNBINDING=57`; `OPERATION_STAGE_ENUM_SNAPSHOT_IMPORT=58`; `OPERATION_STAGE_ENUM_SOURCE_VERIFICATION=59`; `OPERATION_STAGE_ENUM_STORAGE=60`; `OPERATION_STAGE_ENUM_STORAGE_DELETION=61`; `OPERATION_STAGE_ENUM_SUCCEEDED=62`; `OPERATION_STAGE_ENUM_SUPPLEMENT_PAYMENT=63`; `OPERATION_STAGE_ENUM_TARGET_PERIOD_PAYMENT=64`; `OPERATION_STAGE_ENUM_TENANT_STATE_CHECK=65`; `OPERATION_STAGE_ENUM_UPLOAD_VERIFICATION=66`; `OPERATION_STAGE_ENUM_VALIDATING=67`; `OPERATION_STAGE_ENUM_VERIFICATION=68`; `OPERATION_STAGE_ENUM_WALLET_BINDING=69`; `OPERATION_STAGE_ENUM_WORKSPACE_DELETION=70`; `OPERATION_STAGE_ENUM_WORKSPACE_RESUMPTION=71`; `OPERATION_STAGE_ENUM_WORKSPACE_SUSPENSION=72`; `OPERATION_STAGE_ENUM_WRITE_BARRIER=73`

### OperationOwnerEnum

`OPERATION_OWNER_ENUM_UNSPECIFIED=0`; `OPERATION_OWNER_ENUM_TENANT=1`; `OPERATION_OWNER_ENUM_CAPABILITY=2`; `OPERATION_OWNER_ENUM_BUILD=3`; `OPERATION_OWNER_ENUM_WORKSPACE=4`; `OPERATION_OWNER_ENUM_RUNTIME_CONTROL=5`; `OPERATION_OWNER_ENUM_FABRIC=6`; `OPERATION_OWNER_ENUM_GATEWAY=7`; `OPERATION_OWNER_ENUM_RESOURCE_CATALOG=8`

### AuthorizationActionEnum

`AUTHORIZATION_ACTION_ENUM_UNSPECIFIED=0`; `AUTHORIZATION_ACTION_ENUM_ACCEPTINVITATION=1`; `AUTHORIZATION_ACTION_ENUM_ACQUIREREFERENCE=2`; `AUTHORIZATION_ACTION_ENUM_ACTIVATEROUTE=3`; `AUTHORIZATION_ACTION_ENUM_ADOPTWORKSPACE=4`; `AUTHORIZATION_ACTION_ENUM_APPENDPLANCHANGEEVIDENCE=5`; `AUTHORIZATION_ACTION_ENUM_APPENDRECEIPT=6`; `AUTHORIZATION_ACTION_ENUM_ARCHIVENAMESPACE=7`; `AUTHORIZATION_ACTION_ENUM_ARCHIVEPACKAGE=8`; `AUTHORIZATION_ACTION_ENUM_BINDMANAGEDSECRET=9`; `AUTHORIZATION_ACTION_ENUM_BINDREFERENCE=10`; `AUTHORIZATION_ACTION_ENUM_BINDTENANTWALLET=11`; `AUTHORIZATION_ACTION_ENUM_CANCELACCEPTEDOBLIGATION=12`; `AUTHORIZATION_ACTION_ENUM_CANCELPLANCHANGE=13`; `AUTHORIZATION_ACTION_ENUM_CHARGEACCEPTEDOBLIGATION=14`; `AUTHORIZATION_ACTION_ENUM_CHARGEPLANCHANGESUPPLEMENT=15`; `AUTHORIZATION_ACTION_ENUM_CLAIMNEXTPERIODOBLIGATION=16`; `AUTHORIZATION_ACTION_ENUM_CLOSEOUTACCEPTEDOBLIGATION=17`; `AUTHORIZATION_ACTION_ENUM_COMPLETEACCEPTEDOBLIGATION=18`; `AUTHORIZATION_ACTION_ENUM_COMPLETEUPLOAD=19`; `AUTHORIZATION_ACTION_ENUM_CREATEBUILD=20`; `AUTHORIZATION_ACTION_ENUM_CREATECOMPUTEPLAN=21`; `AUTHORIZATION_ACTION_ENUM_CREATEGATEWAYKEY=22`; `AUTHORIZATION_ACTION_ENUM_CREATENAMESPACE=23`; `AUTHORIZATION_ACTION_ENUM_CREATEPACKAGE=24`; `AUTHORIZATION_ACTION_ENUM_CREATEPRICEPOLICYVERSION=25`; `AUTHORIZATION_ACTION_ENUM_CREATEPUBLISHERNAMESPACE=26`; `AUTHORIZATION_ACTION_ENUM_CREATEQUOTE=27`; `AUTHORIZATION_ACTION_ENUM_CREATEREFUNDPOLICYVERSION=28`; `AUTHORIZATION_ACTION_ENUM_CREATERETENTIONPOLICYVERSION=29`; `AUTHORIZATION_ACTION_ENUM_CREATESTORAGEPLAN=30`; `AUTHORIZATION_ACTION_ENUM_CREATETENANT=31`; `AUTHORIZATION_ACTION_ENUM_CREATEUPLOAD=32`; `AUTHORIZATION_ACTION_ENUM_CREATEUPLOADPART=33`; `AUTHORIZATION_ACTION_ENUM_CREATEWORKSPACE=34`; `AUTHORIZATION_ACTION_ENUM_DELETECAPABILITYVERSION=35`; `AUTHORIZATION_ACTION_ENUM_DELETETENANT=36`; `AUTHORIZATION_ACTION_ENUM_DELETEWORKSPACE=37`; `AUTHORIZATION_ACTION_ENUM_EXECUTESCHEDULEDPLANCHANGE=38`; `AUTHORIZATION_ACTION_ENUM_FENCEROUTEEPOCH=39`; `AUTHORIZATION_ACTION_ENUM_GETADMINTENANT=40`; `AUTHORIZATION_ACTION_ENUM_GETBUILD=41`; `AUTHORIZATION_ACTION_ENUM_GETBUILDRUNTIMEPOLICY=42`; `AUTHORIZATION_ACTION_ENUM_GETCAPABILITYVERSION=43`; `AUTHORIZATION_ACTION_ENUM_GETDEPLOYMENT=44`; `AUTHORIZATION_ACTION_ENUM_GETLOGINCONTEXT=45`; `AUTHORIZATION_ACTION_ENUM_GETOPERATION=46`; `AUTHORIZATION_ACTION_ENUM_GETPACKAGE=47`; `AUTHORIZATION_ACTION_ENUM_GETPACKAGEVERSION=48`; `AUTHORIZATION_ACTION_ENUM_GETPLANCHANGE=49`; `AUTHORIZATION_ACTION_ENUM_GETQUOTE=50`; `AUTHORIZATION_ACTION_ENUM_GETRECEIPT=51`; `AUTHORIZATION_ACTION_ENUM_GETSESSION=52`; `AUTHORIZATION_ACTION_ENUM_GETSUBSCRIPTION=53`; `AUTHORIZATION_ACTION_ENUM_GETTENANT=54`; `AUTHORIZATION_ACTION_ENUM_GETTENANTASSETCUSTODY=55`; `AUTHORIZATION_ACTION_ENUM_GETTENANTLIFECYCLEOPERATION=56`; `AUTHORIZATION_ACTION_ENUM_GETUPLOAD=57`; `AUTHORIZATION_ACTION_ENUM_GETWALLET=58`; `AUTHORIZATION_ACTION_ENUM_GETWORKSPACE=59`; `AUTHORIZATION_ACTION_ENUM_GETWORKSPACEACCESS=60`; `AUTHORIZATION_ACTION_ENUM_GETWORKSPACEDELETION=61`; `AUTHORIZATION_ACTION_ENUM_GETWORKSPACEMODELS=62`; `AUTHORIZATION_ACTION_ENUM_INVITEMEMBER=63`; `AUTHORIZATION_ACTION_ENUM_ISSUEACCEPTEDOPERATIONGRANT=64`; `AUTHORIZATION_ACTION_ENUM_LISTADMINOPERATIONS=65`; `AUTHORIZATION_ACTION_ENUM_LISTAUDITEVENTS=66`; `AUTHORIZATION_ACTION_ENUM_LISTBUILDLOGS=67`; `AUTHORIZATION_ACTION_ENUM_LISTBUILDS=68`; `AUTHORIZATION_ACTION_ENUM_LISTCAPABILITYVERSIONS=69`; `AUTHORIZATION_ACTION_ENUM_LISTCOMPUTEPLANS=70`; `AUTHORIZATION_ACTION_ENUM_LISTDEPLOYMENTS=71`; `AUTHORIZATION_ACTION_ENUM_LISTGATEWAYKEYS=72`; `AUTHORIZATION_ACTION_ENUM_LISTINVITATIONS=73`; `AUTHORIZATION_ACTION_ENUM_LISTMEMBERS=74`; `AUTHORIZATION_ACTION_ENUM_LISTMODELS=75`; `AUTHORIZATION_ACTION_ENUM_LISTNAMESPACES=76`; `AUTHORIZATION_ACTION_ENUM_LISTPACKAGEVERSIONS=77`; `AUTHORIZATION_ACTION_ENUM_LISTPACKAGES=78`; `AUTHORIZATION_ACTION_ENUM_LISTPLANCHANGES=79`; `AUTHORIZATION_ACTION_ENUM_LISTPRICEPOLICYVERSIONS=80`; `AUTHORIZATION_ACTION_ENUM_LISTPUBLISHERNAMESPACES=81`; `AUTHORIZATION_ACTION_ENUM_LISTQUALIFICATIONS=82`; `AUTHORIZATION_ACTION_ENUM_LISTRECEIPTS=83`; `AUTHORIZATION_ACTION_ENUM_LISTRECHARGERECORDS=84`; `AUTHORIZATION_ACTION_ENUM_LISTREFUNDPOLICYVERSIONS=85`; `AUTHORIZATION_ACTION_ENUM_LISTRETENTIONPOLICYVERSIONS=86`; `AUTHORIZATION_ACTION_ENUM_LISTRUNTIMEVERSIONS=87`; `AUTHORIZATION_ACTION_ENUM_LISTSTORAGEPLANS=88`; `AUTHORIZATION_ACTION_ENUM_LISTTENANTS=89`; `AUTHORIZATION_ACTION_ENUM_LISTUSAGE=90`; `AUTHORIZATION_ACTION_ENUM_LISTWEBUIVERSIONS=91`; `AUTHORIZATION_ACTION_ENUM_LISTWORKSPACETRANSACTIONS=92`; `AUTHORIZATION_ACTION_ENUM_LISTWORKSPACES=93`; `AUTHORIZATION_ACTION_ENUM_LOGIN=94`; `AUTHORIZATION_ACTION_ENUM_LOGOUT=95`; `AUTHORIZATION_ACTION_ENUM_OBSERVERESOURCES=96`; `AUTHORIZATION_ACTION_ENUM_OBSERVEROUTE=97`; `AUTHORIZATION_ACTION_ENUM_PROVISIONACCEPTEDRESOURCES=98`; `AUTHORIZATION_ACTION_ENUM_PUBLISHOFFICIALPACKAGE=99`; `AUTHORIZATION_ACTION_ENUM_READAPPLICATIONCREDENTIALS=100`; `AUTHORIZATION_ACTION_ENUM_READAPPROVEDPLANTRANSITION=101`; `AUTHORIZATION_ACTION_ENUM_READARTIFACT=102`; `AUTHORIZATION_ACTION_ENUM_READAUTHORIZATIONCONTEXT=103`; `AUTHORIZATION_ACTION_ENUM_READCLAIMUSAGE=104`; `AUTHORIZATION_ACTION_ENUM_READNEXTPERIODOBLIGATION=105`; `AUTHORIZATION_ACTION_ENUM_READOWNERCOMMIT=106`; `AUTHORIZATION_ACTION_ENUM_READRENEWALCONSENT=107`; `AUTHORIZATION_ACTION_ENUM_READSUBSCRIPTIONPLANSTATE=108`; `AUTHORIZATION_ACTION_ENUM_READWALLETACTION=109`; `AUTHORIZATION_ACTION_ENUM_RECONCILEOPERATION=110`; `AUTHORIZATION_ACTION_ENUM_REENABLETENANT=111`; `AUTHORIZATION_ACTION_ENUM_REFUNDCONFIRMEDDELETION=112`; `AUTHORIZATION_ACTION_ENUM_REFUNDPLANCHANGECOMPENSATION=113`; `AUTHORIZATION_ACTION_ENUM_REFUNDSUPPLEMENTONDELETION=114`; `AUTHORIZATION_ACTION_ENUM_REGISTERRUNTIMEVERSION=115`; `AUTHORIZATION_ACTION_ENUM_REGISTERWEBUIVERSION=116`; `AUTHORIZATION_ACTION_ENUM_RELEASEREFERENCE=117`; `AUTHORIZATION_ACTION_ENUM_REMOVEMEMBER=118`; `AUTHORIZATION_ACTION_ENUM_RENEWACCEPTEDRESOURCES=119`; `AUTHORIZATION_ACTION_ENUM_RENEWWORKSPACE=120`; `AUTHORIZATION_ACTION_ENUM_RESERVERUNTIME=121`; `AUTHORIZATION_ACTION_ENUM_RESIZEACCEPTEDRESOURCES=122`; `AUTHORIZATION_ACTION_ENUM_RESIZEWORKSPACE=123`; `AUTHORIZATION_ACTION_ENUM_RESOLVEBUILDINPUT=124`; `AUTHORIZATION_ACTION_ENUM_RESOLVEPUBLISHERCONTRACT=125`; `AUTHORIZATION_ACTION_ENUM_RESTORETENANT=126`; `AUTHORIZATION_ACTION_ENUM_RESUMETENANTWORKSPACES=127`; `AUTHORIZATION_ACTION_ENUM_RETIRERUNTIME=128`; `AUTHORIZATION_ACTION_ENUM_RETRYBUILD=129`; `AUTHORIZATION_ACTION_ENUM_REVEALGATEWAYKEY=130`; `AUTHORIZATION_ACTION_ENUM_REVEALWORKSPACEAPPLICATIONCREDENTIALS=131`; `AUTHORIZATION_ACTION_ENUM_REVOKEGATEWAYKEY=132`; `AUTHORIZATION_ACTION_ENUM_REVOKEINVITATION=133`; `AUTHORIZATION_ACTION_ENUM_REVOKEPUBLISHERNAMESPACE=134`; `AUTHORIZATION_ACTION_ENUM_ROLLBACKROUTE=135`; `AUTHORIZATION_ACTION_ENUM_ROLLBACKWORKSPACE=136`; `AUTHORIZATION_ACTION_ENUM_SETBUILDRUNTIMEPOLICY=137`; `AUTHORIZATION_ACTION_ENUM_SETCOMPUTEPLANAVAILABILITY=138`; `AUTHORIZATION_ACTION_ENUM_SETRUNTIMEVERSIONSTATUS=139`; `AUTHORIZATION_ACTION_ENUM_SETSTORAGEPLANAVAILABILITY=140`; `AUTHORIZATION_ACTION_ENUM_SETWEBUIVERSIONSTATUS=141`; `AUTHORIZATION_ACTION_ENUM_SUSPENDTENANT=142`; `AUTHORIZATION_ACTION_ENUM_UPDATEMEMBERROLE=143`; `AUTHORIZATION_ACTION_ENUM_UPDATENAMESPACE=144`; `AUTHORIZATION_ACTION_ENUM_UPDATEPACKAGE=145`; `AUTHORIZATION_ACTION_ENUM_UPDATERENEWALSETTINGS=146`; `AUTHORIZATION_ACTION_ENUM_UPDATEWORKSPACEMODELS=147`; `AUTHORIZATION_ACTION_ENUM_UPDATEWORKSPACEVERSION=148`; `AUTHORIZATION_ACTION_ENUM_READEXECUTIONPLAN=149`; `AUTHORIZATION_ACTION_ENUM_RESTOREAFTERRESOURCECHANGE=150`; `AUTHORIZATION_ACTION_ENUM_DEBITSCHEDULEDPERIOD=151`; `AUTHORIZATION_ACTION_ENUM_READPLANCHANGEFAILURE=152`

### ServiceIdentityEnum

`SERVICE_IDENTITY_ENUM_UNSPECIFIED=0`; `SERVICE_IDENTITY_ENUM_CONSOLE_BFF=1`; `SERVICE_IDENTITY_ENUM_CAPABILITY=2`; `SERVICE_IDENTITY_ENUM_BUILD=3`; `SERVICE_IDENTITY_ENUM_WORKSPACE=4`; `SERVICE_IDENTITY_ENUM_RUNTIME_CONTROL=5`; `SERVICE_IDENTITY_ENUM_FABRIC=6`; `SERVICE_IDENTITY_ENUM_GATEWAY_INTEGRATION=7`; `SERVICE_IDENTITY_ENUM_RESOURCE_CATALOG=8`; `SERVICE_IDENTITY_ENUM_LEDGER=9`

### OperationStatusEnum

`OPERATION_STATUS_ENUM_UNSPECIFIED=0`; `OPERATION_STATUS_ENUM_ACCEPTED=1`; `OPERATION_STATUS_ENUM_RUNNING=2`; `OPERATION_STATUS_ENUM_AWAITING_CONFIRMATION=3`; `OPERATION_STATUS_ENUM_SUCCEEDED=4`; `OPERATION_STATUS_ENUM_FAILED=5`; `OPERATION_STATUS_ENUM_NEEDS_ATTENTION=6`; `OPERATION_STATUS_ENUM_CANCELLED=7`

### OperationObservationResultEnum

`OPERATION_OBSERVATION_RESULT_ENUM_UNSPECIFIED=0`; `OPERATION_OBSERVATION_RESULT_ENUM_CONFIRMED=1`; `OPERATION_OBSERVATION_RESULT_ENUM_REJECTED=2`; `OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN=3`

### TenantStatusEnum

`TENANT_STATUS_ENUM_UNSPECIFIED=0`; `TENANT_STATUS_ENUM_ACTIVE=1`; `TENANT_STATUS_ENUM_SUSPENDED=2`; `TENANT_STATUS_ENUM_DELETING=3`; `TENANT_STATUS_ENUM_DELETED=4`

### TenantAssetCustodyStatusEnum

`TENANT_ASSET_CUSTODY_STATUS_ENUM_UNSPECIFIED=0`; `TENANT_ASSET_CUSTODY_STATUS_ENUM_TENANT_OWNED=1`; `TENANT_ASSET_CUSTODY_STATUS_ENUM_RESTRICTED=2`; `TENANT_ASSET_CUSTODY_STATUS_ENUM_RETAINED=3`

### MemberStatusEnum

`MEMBER_STATUS_ENUM_UNSPECIFIED=0`; `MEMBER_STATUS_ENUM_ACTIVE=1`; `MEMBER_STATUS_ENUM_REVOKED=2`

### InvitationRoleEnum

`INVITATION_ROLE_ENUM_UNSPECIFIED=0`; `INVITATION_ROLE_ENUM_ADMIN=1`; `INVITATION_ROLE_ENUM_MEMBER=2`

### InvitationStatusEnum

`INVITATION_STATUS_ENUM_UNSPECIFIED=0`; `INVITATION_STATUS_ENUM_PENDING=1`; `INVITATION_STATUS_ENUM_ACCEPTED=2`; `INVITATION_STATUS_ENUM_REVOKED=3`; `INVITATION_STATUS_ENUM_EXPIRED=4`

### InviteMemberRequestRoleEnum

`INVITE_MEMBER_REQUEST_ROLE_ENUM_UNSPECIFIED=0`; `INVITE_MEMBER_REQUEST_ROLE_ENUM_ADMIN=1`; `INVITE_MEMBER_REQUEST_ROLE_ENUM_MEMBER=2`

### AssetCustodyStatusEnum

`ASSET_CUSTODY_STATUS_ENUM_UNSPECIFIED=0`; `ASSET_CUSTODY_STATUS_ENUM_RESTRICTED=1`; `ASSET_CUSTODY_STATUS_ENUM_RETAINED=2`

### NamespaceStatusEnum

`NAMESPACE_STATUS_ENUM_UNSPECIFIED=0`; `NAMESPACE_STATUS_ENUM_ACTIVE=1`; `NAMESPACE_STATUS_ENUM_ARCHIVED=2`

### PackageVisibilityEnum

`PACKAGE_VISIBILITY_ENUM_UNSPECIFIED=0`; `PACKAGE_VISIBILITY_ENUM_PRIVATE=1`; `PACKAGE_VISIBILITY_ENUM_OFFICIAL=2`

### PackageStatusEnum

`PACKAGE_STATUS_ENUM_UNSPECIFIED=0`; `PACKAGE_STATUS_ENUM_ACTIVE=1`; `PACKAGE_STATUS_ENUM_ARCHIVED=2`

### PackageVersionStatusEnum

`PACKAGE_VERSION_STATUS_ENUM_UNSPECIFIED=0`; `PACKAGE_VERSION_STATUS_ENUM_UPLOAD_PENDING=1`; `PACKAGE_VERSION_STATUS_ENUM_UPLOADED=2`; `PACKAGE_VERSION_STATUS_ENUM_REJECTED=3`

### UploadSessionStatusEnum

`UPLOAD_SESSION_STATUS_ENUM_UNSPECIFIED=0`; `UPLOAD_SESSION_STATUS_ENUM_UPLOADING=1`; `UPLOAD_SESSION_STATUS_ENUM_COMPLETED=2`; `UPLOAD_SESSION_STATUS_ENUM_EXPIRED=3`

### UploadPartAuthorizationMethodEnum

`UPLOAD_PART_AUTHORIZATION_METHOD_ENUM_UNSPECIFIED=0`; `UPLOAD_PART_AUTHORIZATION_METHOD_ENUM_PUT=1`

### ModelRequirementCapabilityEnum

`MODEL_REQUIREMENT_CAPABILITY_ENUM_UNSPECIFIED=0`; `MODEL_REQUIREMENT_CAPABILITY_ENUM_TEXT=1`; `MODEL_REQUIREMENT_CAPABILITY_ENUM_VISION=2`; `MODEL_REQUIREMENT_CAPABILITY_ENUM_EMBEDDING=3`; `MODEL_REQUIREMENT_CAPABILITY_ENUM_AUDIO=4`

### CapabilityVersionStatusEnum

`CAPABILITY_VERSION_STATUS_ENUM_UNSPECIFIED=0`; `CAPABILITY_VERSION_STATUS_ENUM_READY=1`; `CAPABILITY_VERSION_STATUS_ENUM_DEPRECATED=2`; `CAPABILITY_VERSION_STATUS_ENUM_DELETING=3`; `CAPABILITY_VERSION_STATUS_ENUM_DELETED=4`

### CapabilityVersionProvenanceEnum

`CAPABILITY_VERSION_PROVENANCE_ENUM_UNSPECIFIED=0`; `CAPABILITY_VERSION_PROVENANCE_ENUM_BUILD=1`; `CAPABILITY_VERSION_PROVENANCE_ENUM_LEGACY_APPLICATION=2`

### BuildJobStatusEnum

`BUILD_JOB_STATUS_ENUM_UNSPECIFIED=0`; `BUILD_JOB_STATUS_ENUM_QUEUED=1`; `BUILD_JOB_STATUS_ENUM_VALIDATING=2`; `BUILD_JOB_STATUS_ENUM_BUILDING=3`; `BUILD_JOB_STATUS_ENUM_PUSHING=4`; `BUILD_JOB_STATUS_ENUM_REGISTERING=5`; `BUILD_JOB_STATUS_ENUM_SUCCEEDED=6`; `BUILD_JOB_STATUS_ENUM_FAILED=7`; `BUILD_JOB_STATUS_ENUM_NEEDS_ATTENTION=8`

### BuildLogLevelEnum

`BUILD_LOG_LEVEL_ENUM_UNSPECIFIED=0`; `BUILD_LOG_LEVEL_ENUM_INFO=1`; `BUILD_LOG_LEVEL_ENUM_WARNING=2`; `BUILD_LOG_LEVEL_ENUM_ERROR=3`

### RuntimeVersionStatusEnum

`RUNTIME_VERSION_STATUS_ENUM_UNSPECIFIED=0`; `RUNTIME_VERSION_STATUS_ENUM_APPROVED=1`; `RUNTIME_VERSION_STATUS_ENUM_DEPRECATED=2`; `RUNTIME_VERSION_STATUS_ENUM_REVOKED=3`

### WebuiVersionStatusEnum

`WEBUI_VERSION_STATUS_ENUM_UNSPECIFIED=0`; `WEBUI_VERSION_STATUS_ENUM_APPROVED=1`; `WEBUI_VERSION_STATUS_ENUM_DEPRECATED=2`; `WEBUI_VERSION_STATUS_ENUM_REVOKED=3`

### CatalogStatusRequestStatusEnum

`CATALOG_STATUS_REQUEST_STATUS_ENUM_UNSPECIFIED=0`; `CATALOG_STATUS_REQUEST_STATUS_ENUM_DEPRECATED=1`; `CATALOG_STATUS_REQUEST_STATUS_ENUM_REVOKED=2`

### ComputePlanAvailabilityEnum

`COMPUTE_PLAN_AVAILABILITY_ENUM_UNSPECIFIED=0`; `COMPUTE_PLAN_AVAILABILITY_ENUM_AVAILABLE=1`; `COMPUTE_PLAN_AVAILABILITY_ENUM_UNAVAILABLE=2`; `COMPUTE_PLAN_AVAILABILITY_ENUM_RETIRED=3`

### ComputePlanBillingModeEnum

`COMPUTE_PLAN_BILLING_MODE_ENUM_UNSPECIFIED=0`; `COMPUTE_PLAN_BILLING_MODE_ENUM_PREPAID_MONTHLY=1`; `COMPUTE_PLAN_BILLING_MODE_ENUM_LOCAL_NO_CHARGE=2`

### StoragePlanAvailabilityEnum

`STORAGE_PLAN_AVAILABILITY_ENUM_UNSPECIFIED=0`; `STORAGE_PLAN_AVAILABILITY_ENUM_AVAILABLE=1`; `STORAGE_PLAN_AVAILABILITY_ENUM_UNAVAILABLE=2`; `STORAGE_PLAN_AVAILABILITY_ENUM_RETIRED=3`

### StoragePlanBillingModeEnum

`STORAGE_PLAN_BILLING_MODE_ENUM_UNSPECIFIED=0`; `STORAGE_PLAN_BILLING_MODE_ENUM_PREPAID_MONTHLY=1`; `STORAGE_PLAN_BILLING_MODE_ENUM_LOCAL_NO_CHARGE=2`

### PlanAvailabilityRequestAvailabilityEnum

`PLAN_AVAILABILITY_REQUEST_AVAILABILITY_ENUM_UNSPECIFIED=0`; `PLAN_AVAILABILITY_REQUEST_AVAILABILITY_ENUM_AVAILABLE=1`; `PLAN_AVAILABILITY_REQUEST_AVAILABILITY_ENUM_UNAVAILABLE=2`; `PLAN_AVAILABILITY_REQUEST_AVAILABILITY_ENUM_RETIRED=3`

### PricePolicyVersionCurrencyEnum

`PRICE_POLICY_VERSION_CURRENCY_ENUM_UNSPECIFIED=0`; `PRICE_POLICY_VERSION_CURRENCY_ENUM_USD=1`

### PricePolicyVersionPlanChangePolicyVersionEnum

`PRICE_POLICY_VERSION_PLAN_CHANGE_POLICY_VERSION_ENUM_UNSPECIFIED=0`; `PRICE_POLICY_VERSION_PLAN_CHANGE_POLICY_VERSION_ENUM_WORKSPACE_PLAN_CHANGE_V1=1`

### CreatePricePolicyRequestPlanChangePolicyVersionEnum

`CREATE_PRICE_POLICY_REQUEST_PLAN_CHANGE_POLICY_VERSION_ENUM_UNSPECIFIED=0`; `CREATE_PRICE_POLICY_REQUEST_PLAN_CHANGE_POLICY_VERSION_ENUM_WORKSPACE_PLAN_CHANGE_V1=1`

### RefundPolicyVersionAlgorithmEnum

`REFUND_POLICY_VERSION_ALGORITHM_ENUM_UNSPECIFIED=0`; `REFUND_POLICY_VERSION_ALGORITHM_ENUM_WORKSPACE_DELETE_REFUND_V1=1`

### CreateRefundPolicyRequestAlgorithmEnum

`CREATE_REFUND_POLICY_REQUEST_ALGORITHM_ENUM_UNSPECIFIED=0`; `CREATE_REFUND_POLICY_REQUEST_ALGORITHM_ENUM_WORKSPACE_DELETE_REFUND_V1=1`

### RetentionPolicyVersionWorkspaceDataDispositionEnum

`RETENTION_POLICY_VERSION_WORKSPACE_DATA_DISPOSITION_ENUM_UNSPECIFIED=0`; `RETENTION_POLICY_VERSION_WORKSPACE_DATA_DISPOSITION_ENUM_DESTROY_AFTER_CONFIRMED_DELETION=1`

### RetentionPolicyVersionPackageHistoryDispositionEnum

`RETENTION_POLICY_VERSION_PACKAGE_HISTORY_DISPOSITION_ENUM_UNSPECIFIED=0`; `RETENTION_POLICY_VERSION_PACKAGE_HISTORY_DISPOSITION_ENUM_RETAIN=1`

### RetentionPolicyVersionBuildHistoryDispositionEnum

`RETENTION_POLICY_VERSION_BUILD_HISTORY_DISPOSITION_ENUM_UNSPECIFIED=0`; `RETENTION_POLICY_VERSION_BUILD_HISTORY_DISPOSITION_ENUM_RETAIN=1`

### ModelPriceSourceEnum

`MODEL_PRICE_SOURCE_ENUM_UNSPECIFIED=0`; `MODEL_PRICE_SOURCE_ENUM_GATEWAY=1`

### QuoteRequestPurposeEnum

`QUOTE_REQUEST_PURPOSE_ENUM_UNSPECIFIED=0`; `QUOTE_REQUEST_PURPOSE_ENUM_DEPLOY=1`; `QUOTE_REQUEST_PURPOSE_ENUM_RESIZE=2`; `QUOTE_REQUEST_PURPOSE_ENUM_RENEW=3`

### QuoteLineKindEnum

`QUOTE_LINE_KIND_ENUM_UNSPECIFIED=0`; `QUOTE_LINE_KIND_ENUM_COMPUTE=1`; `QUOTE_LINE_KIND_ENUM_STORAGE=2`; `QUOTE_LINE_KIND_ENUM_PRODUCT=3`; `QUOTE_LINE_KIND_ENUM_ADJUSTMENT_CREDIT=4`

### QuotePurposeEnum

`QUOTE_PURPOSE_ENUM_UNSPECIFIED=0`; `QUOTE_PURPOSE_ENUM_DEPLOY=1`; `QUOTE_PURPOSE_ENUM_RESIZE=2`; `QUOTE_PURPOSE_ENUM_RENEW=3`

### QuoteStatusEnum

`QUOTE_STATUS_ENUM_UNSPECIFIED=0`; `QUOTE_STATUS_ENUM_OFFERED=1`; `QUOTE_STATUS_ENUM_ACCEPTED=2`; `QUOTE_STATUS_ENUM_EXPIRED=3`

### QuoteRuntimeReadbackRequirementEnum

`QUOTE_RUNTIME_READBACK_REQUIREMENT_ENUM_UNSPECIFIED=0`; `QUOTE_RUNTIME_READBACK_REQUIREMENT_ENUM_REQUIRED=1`; `QUOTE_RUNTIME_READBACK_REQUIREMENT_ENUM_NOT_APPLICABLE=2`

### WorkspaceStatusEnum

`WORKSPACE_STATUS_ENUM_UNSPECIFIED=0`; `WORKSPACE_STATUS_ENUM_PROVISIONING=1`; `WORKSPACE_STATUS_ENUM_ACTIVE=2`; `WORKSPACE_STATUS_ENUM_UPDATING=3`; `WORKSPACE_STATUS_ENUM_SUSPENDED=4`; `WORKSPACE_STATUS_ENUM_DELETING=5`; `WORKSPACE_STATUS_ENUM_DELETED=6`; `WORKSPACE_STATUS_ENUM_FAILED=7`; `WORKSPACE_STATUS_ENUM_NEEDS_ATTENTION=8`

### WorkspaceResourceReadinessEnum

`WORKSPACE_RESOURCE_READINESS_ENUM_UNSPECIFIED=0`; `WORKSPACE_RESOURCE_READINESS_ENUM_PENDING=1`; `WORKSPACE_RESOURCE_READINESS_ENUM_READY=2`; `WORKSPACE_RESOURCE_READINESS_ENUM_UNAVAILABLE=3`; `WORKSPACE_RESOURCE_READINESS_ENUM_UNKNOWN=4`

### WorkspaceApplicationAvailabilityEnum

`WORKSPACE_APPLICATION_AVAILABILITY_ENUM_UNSPECIFIED=0`; `WORKSPACE_APPLICATION_AVAILABILITY_ENUM_PENDING=1`; `WORKSPACE_APPLICATION_AVAILABILITY_ENUM_AVAILABLE=2`; `WORKSPACE_APPLICATION_AVAILABILITY_ENUM_UNAVAILABLE=3`; `WORKSPACE_APPLICATION_AVAILABILITY_ENUM_UNKNOWN=4`

### WorkspaceDeliveryModelEnum

`WORKSPACE_DELIVERY_MODEL_ENUM_UNSPECIFIED=0`; `WORKSPACE_DELIVERY_MODEL_ENUM_LEGACY_RESOURCE_ONLY=1`; `WORKSPACE_DELIVERY_MODEL_ENUM_IMPORTED_APPLICATION=2`; `WORKSPACE_DELIVERY_MODEL_ENUM_AGENT_SAAS=3`

### CreateWorkspaceRequestRenewalModeEnum

`CREATE_WORKSPACE_REQUEST_RENEWAL_MODE_ENUM_UNSPECIFIED=0`; `CREATE_WORKSPACE_REQUEST_RENEWAL_MODE_ENUM_MANUAL=1`; `CREATE_WORKSPACE_REQUEST_RENEWAL_MODE_ENUM_AUTOMATIC=2`

### WorkspaceAccessAuthenticationModeEnum

`WORKSPACE_ACCESS_AUTHENTICATION_MODE_ENUM_UNSPECIFIED=0`; `WORKSPACE_ACCESS_AUTHENTICATION_MODE_ENUM_APPLICATION_LOGIN=1`; `WORKSPACE_ACCESS_AUTHENTICATION_MODE_ENUM_CLOUD_PRIVATE=2`; `WORKSPACE_ACCESS_AUTHENTICATION_MODE_ENUM_ANONYMOUS=3`

### ModelConfigurationStatusEnum

`MODEL_CONFIGURATION_STATUS_ENUM_UNSPECIFIED=0`; `MODEL_CONFIGURATION_STATUS_ENUM_PENDING=1`; `MODEL_CONFIGURATION_STATUS_ENUM_APPLIED=2`; `MODEL_CONFIGURATION_STATUS_ENUM_FAILED=3`; `MODEL_CONFIGURATION_STATUS_ENUM_NEEDS_ATTENTION=4`

### DeploymentStatusEnum

`DEPLOYMENT_STATUS_ENUM_UNSPECIFIED=0`; `DEPLOYMENT_STATUS_ENUM_QUEUED=1`; `DEPLOYMENT_STATUS_ENUM_DEPLOYING=2`; `DEPLOYMENT_STATUS_ENUM_VERIFYING=3`; `DEPLOYMENT_STATUS_ENUM_ACTIVE=4`; `DEPLOYMENT_STATUS_ENUM_SUPERSEDED=5`; `DEPLOYMENT_STATUS_ENUM_FAILED=6`; `DEPLOYMENT_STATUS_ENUM_ROLLING_BACK=7`; `DEPLOYMENT_STATUS_ENUM_ROLLED_BACK=8`; `DEPLOYMENT_STATUS_ENUM_NEEDS_ATTENTION=9`

### WorkspaceDeletionResourceDeletionStatusEnum

`WORKSPACE_DELETION_RESOURCE_DELETION_STATUS_ENUM_UNSPECIFIED=0`; `WORKSPACE_DELETION_RESOURCE_DELETION_STATUS_ENUM_PENDING=1`; `WORKSPACE_DELETION_RESOURCE_DELETION_STATUS_ENUM_CONFIRMED=2`; `WORKSPACE_DELETION_RESOURCE_DELETION_STATUS_ENUM_REJECTED=3`; `WORKSPACE_DELETION_RESOURCE_DELETION_STATUS_ENUM_UNKNOWN=4`

### WorkspaceDeletionDataDeletionStatusEnum

`WORKSPACE_DELETION_DATA_DELETION_STATUS_ENUM_UNSPECIFIED=0`; `WORKSPACE_DELETION_DATA_DELETION_STATUS_ENUM_PENDING=1`; `WORKSPACE_DELETION_DATA_DELETION_STATUS_ENUM_CONFIRMED=2`; `WORKSPACE_DELETION_DATA_DELETION_STATUS_ENUM_REJECTED=3`; `WORKSPACE_DELETION_DATA_DELETION_STATUS_ENUM_UNKNOWN=4`

### WorkspaceDeletionRefundStatusEnum

`WORKSPACE_DELETION_REFUND_STATUS_ENUM_UNSPECIFIED=0`; `WORKSPACE_DELETION_REFUND_STATUS_ENUM_NOT_APPLICABLE=1`; `WORKSPACE_DELETION_REFUND_STATUS_ENUM_REQUESTED=2`; `WORKSPACE_DELETION_REFUND_STATUS_ENUM_CONFIRMED=3`; `WORKSPACE_DELETION_REFUND_STATUS_ENUM_REJECTED=4`; `WORKSPACE_DELETION_REFUND_STATUS_ENUM_UNKNOWN=5`

### SubscriptionStatusEnum

`SUBSCRIPTION_STATUS_ENUM_UNSPECIFIED=0`; `SUBSCRIPTION_STATUS_ENUM_PENDING=1`; `SUBSCRIPTION_STATUS_ENUM_ACTIVE=2`; `SUBSCRIPTION_STATUS_ENUM_EXPIRED=3`; `SUBSCRIPTION_STATUS_ENUM_TERMINATED=4`

### SubscriptionProvenanceEnum

`SUBSCRIPTION_PROVENANCE_ENUM_UNSPECIFIED=0`; `SUBSCRIPTION_PROVENANCE_ENUM_QUOTED=1`; `SUBSCRIPTION_PROVENANCE_ENUM_LEGACY_IMPORT=2`

### SubscriptionRenewalModeEnum

`SUBSCRIPTION_RENEWAL_MODE_ENUM_UNSPECIFIED=0`; `SUBSCRIPTION_RENEWAL_MODE_ENUM_MANUAL=1`; `SUBSCRIPTION_RENEWAL_MODE_ENUM_AUTOMATIC=2`

### WalletOperationKindEnum

`WALLET_OPERATION_KIND_ENUM_UNSPECIFIED=0`; `WALLET_OPERATION_KIND_ENUM_CHARGE=1`; `WALLET_OPERATION_KIND_ENUM_REFUND=2`; `WALLET_OPERATION_KIND_ENUM_RECHARGE=3`

### WalletOperationStatusEnum

`WALLET_OPERATION_STATUS_ENUM_UNSPECIFIED=0`; `WALLET_OPERATION_STATUS_ENUM_REQUESTED=1`; `WALLET_OPERATION_STATUS_ENUM_CONFIRMED=2`; `WALLET_OPERATION_STATUS_ENUM_REJECTED=3`; `WALLET_OPERATION_STATUS_ENUM_UNKNOWN=4`

### WalletOperationPurposeEnum

`WALLET_OPERATION_PURPOSE_ENUM_UNSPECIFIED=0`; `WALLET_OPERATION_PURPOSE_ENUM_BASE_PERIOD=1`; `WALLET_OPERATION_PURPOSE_ENUM_UPGRADE_SUPPLEMENT=2`; `WALLET_OPERATION_PURPOSE_ENUM_BASE_PERIOD_DELETE=3`; `WALLET_OPERATION_PURPOSE_ENUM_UPGRADE_FAILURE_FULL=4`; `WALLET_OPERATION_PURPOSE_ENUM_SUPPLEMENT_DELETE_UNUSED=5`; `WALLET_OPERATION_PURPOSE_ENUM_NEXT_PERIOD_PLAN_FAILURE_FULL=6`; `WALLET_OPERATION_PURPOSE_ENUM_RECHARGE=7`

### WalletSourceEnum

`WALLET_SOURCE_ENUM_UNSPECIFIED=0`; `WALLET_SOURCE_ENUM_GATEWAY=1`

### WalletStatusEnum

`WALLET_STATUS_ENUM_UNSPECIFIED=0`; `WALLET_STATUS_ENUM_AVAILABLE=1`

### WalletCurrencyEnum

`WALLET_CURRENCY_ENUM_UNSPECIFIED=0`; `WALLET_CURRENCY_ENUM_USD=1`

### UsageSourceEnum

`USAGE_SOURCE_ENUM_UNSPECIFIED=0`; `USAGE_SOURCE_ENUM_GATEWAY=1`

### GatewayKeyPurposeEnum

`GATEWAY_KEY_PURPOSE_ENUM_UNSPECIFIED=0`; `GATEWAY_KEY_PURPOSE_ENUM_PERSONAL=1`; `GATEWAY_KEY_PURPOSE_ENUM_WORKSPACE_MANAGED=2`

### GatewayKeyStatusEnum

`GATEWAY_KEY_STATUS_ENUM_UNSPECIFIED=0`; `GATEWAY_KEY_STATUS_ENUM_ACTIVE=1`; `GATEWAY_KEY_STATUS_ENUM_REVOKED=2`

### AuditEventOutcomeEnum

`AUDIT_EVENT_OUTCOME_ENUM_UNSPECIFIED=0`; `AUDIT_EVENT_OUTCOME_ENUM_CONFIRMED=1`; `AUDIT_EVENT_OUTCOME_ENUM_REJECTED=2`; `AUDIT_EVENT_OUTCOME_ENUM_UNKNOWN=3`

### ReceiptKindEnum

`RECEIPT_KIND_ENUM_UNSPECIFIED=0`; `RECEIPT_KIND_ENUM_SOURCE_CHECK=1`; `RECEIPT_KIND_ENUM_CANDIDATE=2`; `RECEIPT_KIND_ENUM_QUALIFICATION=3`; `RECEIPT_KIND_ENUM_DEPLOYMENT=4`; `RECEIPT_KIND_ENUM_ROLLBACK=5`; `RECEIPT_KIND_ENUM_RELEASE=6`; `RECEIPT_KIND_ENUM_PROVIDER_ACTION=7`; `RECEIPT_KIND_ENUM_WALLET_ACTION=8`; `RECEIPT_KIND_ENUM_PLAN_CHANGE=9`; `RECEIPT_KIND_ENUM_SUPPLEMENTAL_CHARGE=10`; `RECEIPT_KIND_ENUM_PLAN_CHANGE_FAILURE_COMPENSATION=11`; `RECEIPT_KIND_ENUM_SUPPLEMENTAL_DELETION_REFUND=12`; `RECEIPT_KIND_ENUM_NEXT_PERIOD_PLAN_SETTLEMENT=13`

### ReceiptOutcomeEnum

`RECEIPT_OUTCOME_ENUM_UNSPECIFIED=0`; `RECEIPT_OUTCOME_ENUM_CONFIRMED=1`; `RECEIPT_OUTCOME_ENUM_REJECTED=2`; `RECEIPT_OUTCOME_ENUM_UNKNOWN=3`

### QualificationStatusEnum

`QUALIFICATION_STATUS_ENUM_UNSPECIFIED=0`; `QUALIFICATION_STATUS_ENUM_PENDING=1`; `QUALIFICATION_STATUS_ENUM_QUALIFIED=2`; `QUALIFICATION_STATUS_ENUM_REJECTED=3`

### ImagePlatformOsEnum

`IMAGE_PLATFORM_OS_ENUM_UNSPECIFIED=0`; `IMAGE_PLATFORM_OS_ENUM_LINUX=1`

### ImagePlatformArchitectureEnum

`IMAGE_PLATFORM_ARCHITECTURE_ENUM_UNSPECIFIED=0`; `IMAGE_PLATFORM_ARCHITECTURE_ENUM_AMD64=1`; `IMAGE_PLATFORM_ARCHITECTURE_ENUM_ARM64=2`

### RecipeArtifactMediaTypeEnum

`RECIPE_ARTIFACT_MEDIA_TYPE_ENUM_UNSPECIFIED=0`; `RECIPE_ARTIFACT_MEDIA_TYPE_ENUM_APPLICATION_VND_OPL_BUILD_RECIPE_V1_TAR=1`

### PackageBuildInputContextNameEnum

`PACKAGE_BUILD_INPUT_CONTEXT_NAME_ENUM_UNSPECIFIED=0`; `PACKAGE_BUILD_INPUT_CONTEXT_NAME_ENUM_AGENT_PACKAGE=1`

### WebuiBuildInputContextNameEnum

`WEBUI_BUILD_INPUT_CONTEXT_NAME_ENUM_UNSPECIFIED=0`; `WEBUI_BUILD_INPUT_CONTEXT_NAME_ENUM_WEBUI=1`

### BuildRecipeContractVersionEnum

`BUILD_RECIPE_CONTRACT_VERSION_ENUM_UNSPECIFIED=0`; `BUILD_RECIPE_CONTRACT_VERSION_ENUM_OPL_BUILD_RECIPE_V1=1`

### BuildRecipeContractRuntimeContextNameEnum

`BUILD_RECIPE_CONTRACT_RUNTIME_CONTEXT_NAME_ENUM_UNSPECIFIED=0`; `BUILD_RECIPE_CONTRACT_RUNTIME_CONTEXT_NAME_ENUM_RUNTIME=1`

### BuildRecipeContractNetworkPolicyEnum

`BUILD_RECIPE_CONTRACT_NETWORK_POLICY_ENUM_UNSPECIFIED=0`; `BUILD_RECIPE_CONTRACT_NETWORK_POLICY_ENUM_NONE=1`

### ModelConfigurationContractProtocolEnum

`MODEL_CONFIGURATION_CONTRACT_PROTOCOL_ENUM_UNSPECIFIED=0`; `MODEL_CONFIGURATION_CONTRACT_PROTOCOL_ENUM_OPL_MODEL_CONFIG_V1=1`

### ModelConfigurationContractRequestFieldsEnum

`MODEL_CONFIGURATION_CONTRACT_REQUEST_FIELDS_ENUM_UNSPECIFIED=0`; `MODEL_CONFIGURATION_CONTRACT_REQUEST_FIELDS_ENUM_VERSION=1`; `MODEL_CONFIGURATION_CONTRACT_REQUEST_FIELDS_ENUM_SELECTIONS=2`

### ModelConfigurationContractReadbackFieldsEnum

`MODEL_CONFIGURATION_CONTRACT_READBACK_FIELDS_ENUM_UNSPECIFIED=0`; `MODEL_CONFIGURATION_CONTRACT_READBACK_FIELDS_ENUM_APPLIEDVERSION=1`; `MODEL_CONFIGURATION_CONTRACT_READBACK_FIELDS_ENUM_SELECTIONS=2`

### DataUpgradeContractModeEnum

`DATA_UPGRADE_CONTRACT_MODE_ENUM_UNSPECIFIED=0`; `DATA_UPGRADE_CONTRACT_MODE_ENUM_COMPATIBLE=1`; `DATA_UPGRADE_CONTRACT_MODE_ENUM_PUBLISHER_MIGRATION=2`

### RuntimePublisherContractSchemaVersionEnum

`RUNTIME_PUBLISHER_CONTRACT_SCHEMA_VERSION_ENUM_UNSPECIFIED=0`; `RUNTIME_PUBLISHER_CONTRACT_SCHEMA_VERSION_ENUM_OPL_PUBLISHER_CONTRACT_V1=1`

### RuntimePublisherContractKindEnum

`RUNTIME_PUBLISHER_CONTRACT_KIND_ENUM_UNSPECIFIED=0`; `RUNTIME_PUBLISHER_CONTRACT_KIND_ENUM_RUNTIME=1`

### WebuiPublisherContractSchemaVersionEnum

`WEBUI_PUBLISHER_CONTRACT_SCHEMA_VERSION_ENUM_UNSPECIFIED=0`; `WEBUI_PUBLISHER_CONTRACT_SCHEMA_VERSION_ENUM_OPL_PUBLISHER_CONTRACT_V1=1`

### WebuiPublisherContractKindEnum

`WEBUI_PUBLISHER_CONTRACT_KIND_ENUM_UNSPECIFIED=0`; `WEBUI_PUBLISHER_CONTRACT_KIND_ENUM_WEBUI=1`

### WebuiPublisherContractUiProtocolVersionEnum

`WEBUI_PUBLISHER_CONTRACT_UI_PROTOCOL_VERSION_ENUM_UNSPECIFIED=0`; `WEBUI_PUBLISHER_CONTRACT_UI_PROTOCOL_VERSION_ENUM_OPL_WEBUI_V1=1`

### WebuiPublisherContractIntegrationModeEnum

`WEBUI_PUBLISHER_CONTRACT_INTEGRATION_MODE_ENUM_UNSPECIFIED=0`; `WEBUI_PUBLISHER_CONTRACT_INTEGRATION_MODE_ENUM_BUNDLED_STATIC=1`

### WebuiPublisherContractAuthenticationProtocolEnum

`WEBUI_PUBLISHER_CONTRACT_AUTHENTICATION_PROTOCOL_ENUM_UNSPECIFIED=0`; `WEBUI_PUBLISHER_CONTRACT_AUTHENTICATION_PROTOCOL_ENUM_OPL_APPLICATION_SESSION_V1=1`

### PublisherContractReferenceKindEnum

`PUBLISHER_CONTRACT_REFERENCE_KIND_ENUM_UNSPECIFIED=0`; `PUBLISHER_CONTRACT_REFERENCE_KIND_ENUM_RUNTIME=1`; `PUBLISHER_CONTRACT_REFERENCE_KIND_ENUM_WEBUI=2`

### DeploymentDescriptorSchemaVersionEnum

`DEPLOYMENT_DESCRIPTOR_SCHEMA_VERSION_ENUM_UNSPECIFIED=0`; `DEPLOYMENT_DESCRIPTOR_SCHEMA_VERSION_ENUM_OPL_DEPLOYMENT_DESCRIPTOR_V1=1`

### DeploymentDescriptorProvenanceEnum

`DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_UNSPECIFIED=0`; `DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_BUILD=1`; `DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_LEGACY_APPLICATION=2`

### PublisherNamespaceKindEnum

`PUBLISHER_NAMESPACE_KIND_ENUM_UNSPECIFIED=0`; `PUBLISHER_NAMESPACE_KIND_ENUM_OFFICIAL=1`; `PUBLISHER_NAMESPACE_KIND_ENUM_THIRD_PARTY=2`

### PublisherNamespaceStatusEnum

`PUBLISHER_NAMESPACE_STATUS_ENUM_UNSPECIFIED=0`; `PUBLISHER_NAMESPACE_STATUS_ENUM_APPROVED=1`; `PUBLISHER_NAMESPACE_STATUS_ENUM_REVOKED=2`

### CreatePublisherNamespaceRequestKindEnum

`CREATE_PUBLISHER_NAMESPACE_REQUEST_KIND_ENUM_UNSPECIFIED=0`; `CREATE_PUBLISHER_NAMESPACE_REQUEST_KIND_ENUM_OFFICIAL=1`; `CREATE_PUBLISHER_NAMESPACE_REQUEST_KIND_ENUM_THIRD_PARTY=2`

### RenewalPolicyVersionEnum

`RENEWAL_POLICY_VERSION_ENUM_UNSPECIFIED=0`; `RENEWAL_POLICY_VERSION_ENUM_RENEWAL_POLICY_V1=1`

### RenewalPolicyTriggerEnum

`RENEWAL_POLICY_TRIGGER_ENUM_UNSPECIFIED=0`; `RENEWAL_POLICY_TRIGGER_ENUM_MANUAL_OR_EXPLICITLY_CONSENTED_AUTOMATIC=1`

### RenewalPolicyEffectiveStartEnum

`RENEWAL_POLICY_EFFECTIVE_START_ENUM_UNSPECIFIED=0`; `RENEWAL_POLICY_EFFECTIVE_START_ENUM_PREVIOUS_PAID_THROUGH=1`

### WorkspaceApplicationCredentialKindEnum

`WORKSPACE_APPLICATION_CREDENTIAL_KIND_ENUM_UNSPECIFIED=0`; `WORKSPACE_APPLICATION_CREDENTIAL_KIND_ENUM_WORKSPACE_ADMIN_PASSWORD=1`; `WORKSPACE_APPLICATION_CREDENTIAL_KIND_ENUM_WORKSPACE_SESSION_SECRET=2`; `WORKSPACE_APPLICATION_CREDENTIAL_KIND_ENUM_GATEWAY_KEY=3`

### WorkspaceApplicationPortProtocolEnum

`WORKSPACE_APPLICATION_PORT_PROTOCOL_ENUM_UNSPECIFIED=0`; `WORKSPACE_APPLICATION_PORT_PROTOCOL_ENUM_TCP=1`; `WORKSPACE_APPLICATION_PORT_PROTOCOL_ENUM_UDP=2`

### WorkspaceApplicationDependencyHealthCheckTypeEnum

`WORKSPACE_APPLICATION_DEPENDENCY_HEALTH_CHECK_TYPE_ENUM_UNSPECIFIED=0`; `WORKSPACE_APPLICATION_DEPENDENCY_HEALTH_CHECK_TYPE_ENUM_TCP=1`; `WORKSPACE_APPLICATION_DEPENDENCY_HEALTH_CHECK_TYPE_ENUM_HTTP=2`; `WORKSPACE_APPLICATION_DEPENDENCY_HEALTH_CHECK_TYPE_ENUM_EXEC=3`

### WorkspaceApplicationRevisionExposurePolicyEnum

`WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_UNSPECIFIED=0`; `WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_ANONYMOUS=1`; `WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION=2`; `WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_CLOUD_PRIVATE=3`

### ApplicationOwnedAccessContractModeEnum

`APPLICATION_OWNED_ACCESS_CONTRACT_MODE_ENUM_UNSPECIFIED=0`; `APPLICATION_OWNED_ACCESS_CONTRACT_MODE_ENUM_APPLICATION=1`

### CloudPrivateAccessContractModeEnum

`CLOUD_PRIVATE_ACCESS_CONTRACT_MODE_ENUM_UNSPECIFIED=0`; `CLOUD_PRIVATE_ACCESS_CONTRACT_MODE_ENUM_CLOUD_PRIVATE=1`

### CloudPrivateAccessContractEntryContractEnum

`CLOUD_PRIVATE_ACCESS_CONTRACT_ENTRY_CONTRACT_ENUM_UNSPECIFIED=0`; `CLOUD_PRIVATE_ACCESS_CONTRACT_ENTRY_CONTRACT_ENUM_WORKSPACEAPPLICATIONENTRY=1`

### AnonymousAccessContractModeEnum

`ANONYMOUS_ACCESS_CONTRACT_MODE_ENUM_UNSPECIFIED=0`; `ANONYMOUS_ACCESS_CONTRACT_MODE_ENUM_ANONYMOUS=1`

### TenantWorkspaceActionActionEnum

`TENANT_WORKSPACE_ACTION_ACTION_ENUM_UNSPECIFIED=0`; `TENANT_WORKSPACE_ACTION_ACTION_ENUM_SUSPEND=1`; `TENANT_WORKSPACE_ACTION_ACTION_ENUM_RESUME=2`; `TENANT_WORKSPACE_ACTION_ACTION_ENUM_DELETE=3`

### TenantWorkspaceActionOperationOwnerEnum

`TENANT_WORKSPACE_ACTION_OPERATION_OWNER_ENUM_UNSPECIFIED=0`; `TENANT_WORKSPACE_ACTION_OPERATION_OWNER_ENUM_WORKSPACE=1`

### TenantWorkspaceActionStatusEnum

`TENANT_WORKSPACE_ACTION_STATUS_ENUM_UNSPECIFIED=0`; `TENANT_WORKSPACE_ACTION_STATUS_ENUM_ACCEPTED=1`; `TENANT_WORKSPACE_ACTION_STATUS_ENUM_RUNNING=2`; `TENANT_WORKSPACE_ACTION_STATUS_ENUM_AWAITING_CONFIRMATION=3`; `TENANT_WORKSPACE_ACTION_STATUS_ENUM_SUCCEEDED=4`; `TENANT_WORKSPACE_ACTION_STATUS_ENUM_FAILED=5`; `TENANT_WORKSPACE_ACTION_STATUS_ENUM_NEEDS_ATTENTION=6`; `TENANT_WORKSPACE_ACTION_STATUS_ENUM_CANCELLED=7`

### TenantWorkspaceSkipReasonEnum

`TENANT_WORKSPACE_SKIP_REASON_ENUM_UNSPECIFIED=0`; `TENANT_WORKSPACE_SKIP_REASON_ENUM_NOT_SUSPENDED_BY_TENANT=1`; `TENANT_WORKSPACE_SKIP_REASON_ENUM_PAID_PERIOD_EXPIRED=2`; `TENANT_WORKSPACE_SKIP_REASON_ENUM_ALREADY_DELETED=3`; `TENANT_WORKSPACE_SKIP_REASON_ENUM_RESOURCES_ABSENT=4`; `TENANT_WORKSPACE_SKIP_REASON_ENUM_RESOURCES_UNKNOWN=5`; `TENANT_WORKSPACE_SKIP_REASON_ENUM_OPERATION_IN_PROGRESS=6`

### TenantLifecycleProgressAccessStatusEnum

`TENANT_LIFECYCLE_PROGRESS_ACCESS_STATUS_ENUM_UNSPECIFIED=0`; `TENANT_LIFECYCLE_PROGRESS_ACCESS_STATUS_ENUM_ENABLED=1`; `TENANT_LIFECYCLE_PROGRESS_ACCESS_STATUS_ENUM_REVOKED=2`

### UpdateRenewalSettingsRequestRenewalModeEnum

`UPDATE_RENEWAL_SETTINGS_REQUEST_RENEWAL_MODE_ENUM_UNSPECIFIED=0`; `UPDATE_RENEWAL_SETTINGS_REQUEST_RENEWAL_MODE_ENUM_MANUAL=1`; `UPDATE_RENEWAL_SETTINGS_REQUEST_RENEWAL_MODE_ENUM_AUTOMATIC=2`

### PackageFormatContractReferenceOwnerEnum

`PACKAGE_FORMAT_CONTRACT_REFERENCE_OWNER_ENUM_UNSPECIFIED=0`; `PACKAGE_FORMAT_CONTRACT_REFERENCE_OWNER_ENUM_FRAMEWORK=1`

### UpgradePlanRulesKindEnum

`UPGRADE_PLAN_RULES_KIND_ENUM_UNSPECIFIED=0`; `UPGRADE_PLAN_RULES_KIND_ENUM_UPGRADE_IMMEDIATE=1`

### UpgradePlanRulesEffectiveWhenEnum

`UPGRADE_PLAN_RULES_EFFECTIVE_WHEN_ENUM_UNSPECIFIED=0`; `UPGRADE_PLAN_RULES_EFFECTIVE_WHEN_ENUM_RESOURCE_AND_RUNTIME_READBACK_CONFIRMED=1`

### UpgradePlanRulesOldPriceSourceEnum

`UPGRADE_PLAN_RULES_OLD_PRICE_SOURCE_ENUM_UNSPECIFIED=0`; `UPGRADE_PLAN_RULES_OLD_PRICE_SOURCE_ENUM_ACCEPTED_APPLIED_SUBSCRIPTION_PLAN=1`

### UpgradePlanRulesChargeClockEnum

`UPGRADE_PLAN_RULES_CHARGE_CLOCK_ENUM_UNSPECIFIED=0`; `UPGRADE_PLAN_RULES_CHARGE_CLOCK_ENUM_QUOTE_PRICING_BASIS_AT_FROZEN_ON_ACCEPTANCE=1`

### UpgradePlanRulesTimeUnitEnum

`UPGRADE_PLAN_RULES_TIME_UNIT_ENUM_UNSPECIFIED=0`; `UPGRADE_PLAN_RULES_TIME_UNIT_ENUM_UTC_INTEGER_MILLISECONDS=1`

### UpgradePlanRulesChargeRoundingEnum

`UPGRADE_PLAN_RULES_CHARGE_ROUNDING_ENUM_UNSPECIFIED=0`; `UPGRADE_PLAN_RULES_CHARGE_ROUNDING_ENUM_CEIL_ONCE_USD_MICRO=1`

### UpgradePlanRulesZeroChargeEnum

`UPGRADE_PLAN_RULES_ZERO_CHARGE_ENUM_UNSPECIFIED=0`; `UPGRADE_PLAN_RULES_ZERO_CHARGE_ENUM_RECORD_EVIDENCE_SKIP_GATEWAY=1`

### UpgradePlanRulesKnownFailureCompensationEnum

`UPGRADE_PLAN_RULES_KNOWN_FAILURE_COMPENSATION_ENUM_UNSPECIFIED=0`; `UPGRADE_PLAN_RULES_KNOWN_FAILURE_COMPENSATION_ENUM_FULL_UNREFUNDED_ORIGINAL_SUPPLEMENT=1`

### UpgradePlanRulesUnknownOutcomeEnum

`UPGRADE_PLAN_RULES_UNKNOWN_OUTCOME_ENUM_UNSPECIFIED=0`; `UPGRADE_PLAN_RULES_UNKNOWN_OUTCOME_ENUM_READ_ORIGINAL_ACTION_NO_REFUND=1`

### UpgradePlanRulesIrreversibleResidualCostOwnerEnum

`UPGRADE_PLAN_RULES_IRREVERSIBLE_RESIDUAL_COST_OWNER_ENUM_UNSPECIFIED=0`; `UPGRADE_PLAN_RULES_IRREVERSIBLE_RESIDUAL_COST_OWNER_ENUM_PLATFORM=1`

### UpgradePlanRulesSupplementDeleteRefundEnum

`UPGRADE_PLAN_RULES_SUPPLEMENT_DELETE_REFUND_ENUM_UNSPECIFIED=0`; `UPGRADE_PLAN_RULES_SUPPLEMENT_DELETE_REFUND_ENUM_FLOOR_UNUSED_SUPPLEMENT_COVERAGE=1`

### DowngradePlanRulesKindEnum

`DOWNGRADE_PLAN_RULES_KIND_ENUM_UNSPECIFIED=0`; `DOWNGRADE_PLAN_RULES_KIND_ENUM_DOWNGRADE_NEXT_PERIOD=1`

### DowngradePlanRulesPlannedBoundaryEnum

`DOWNGRADE_PLAN_RULES_PLANNED_BOUNDARY_ENUM_UNSPECIFIED=0`; `DOWNGRADE_PLAN_RULES_PLANNED_BOUNDARY_ENUM_ORIGINAL_PAID_THROUGH=1`

### DowngradePlanRulesCurrentPeriodRefundEnum

`DOWNGRADE_PLAN_RULES_CURRENT_PERIOD_REFUND_ENUM_UNSPECIFIED=0`; `DOWNGRADE_PLAN_RULES_CURRENT_PERIOD_REFUND_ENUM_NONE=1`

### DowngradePlanRulesNextPeriodPriceEnum

`DOWNGRADE_PLAN_RULES_NEXT_PERIOD_PRICE_ENUM_UNSPECIFIED=0`; `DOWNGRADE_PLAN_RULES_NEXT_PERIOD_PRICE_ENUM_ACCEPTED_TARGET_PLAN_QUOTE=1`

### DowngradePlanRulesCancelBeforeEnum

`DOWNGRADE_PLAN_RULES_CANCEL_BEFORE_ENUM_UNSPECIFIED=0`; `DOWNGRADE_PLAN_RULES_CANCEL_BEFORE_ENUM_NEXT_PERIOD_PAYMENT_OBLIGATION_ACCEPTED=1`

### DowngradePlanRulesEarlyPaidChangeEnum

`DOWNGRADE_PLAN_RULES_EARLY_PAID_CHANGE_ENUM_UNSPECIFIED=0`; `DOWNGRADE_PLAN_RULES_EARLY_PAID_CHANGE_ENUM_DO_NOT_REDUCE_RESOURCES_BEFORE_ORIGINAL_PAID_THROUGH=1`

### DowngradePlanRulesManualUnpaidBoundaryEnum

`DOWNGRADE_PLAN_RULES_MANUAL_UNPAID_BOUNDARY_ENUM_UNSPECIFIED=0`; `DOWNGRADE_PLAN_RULES_MANUAL_UNPAID_BOUNDARY_ENUM_AWAITING_PAYMENT_SUSPEND_UNPAID_USAGE=1`

### DowngradePlanRulesKnownFailureCompensationEnum

`DOWNGRADE_PLAN_RULES_KNOWN_FAILURE_COMPENSATION_ENUM_UNSPECIFIED=0`; `DOWNGRADE_PLAN_RULES_KNOWN_FAILURE_COMPENSATION_ENUM_FULL_UNREFUNDED_TARGET_PERIOD_CHARGE=1`

### DowngradePlanRulesFallbackEnum

`DOWNGRADE_PLAN_RULES_FALLBACK_ENUM_UNSPECIFIED=0`; `DOWNGRADE_PLAN_RULES_FALLBACK_ENUM_NONE_NO_OLD_PRICE_RENEWAL_OR_SKU_SUBSTITUTION=1`

### PlanChangePolicyVersionEnum

`PLAN_CHANGE_POLICY_VERSION_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_POLICY_VERSION_ENUM_WORKSPACE_PLAN_CHANGE_V1=1`

### PlanChangePolicyApprovalStatusEnum

`PLAN_CHANGE_POLICY_APPROVAL_STATUS_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_POLICY_APPROVAL_STATUS_ENUM_APPROVED=1`

### PlanChangePolicyClassificationEnum

`PLAN_CHANGE_POLICY_CLASSIFICATION_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_POLICY_CLASSIFICATION_ENUM_APPROVED_COMPARABLE_RESOURCES_NOT_PRICE_OR_SKU_NAME=1`

### PlanChangePolicyMixedOrIncomparableTransitionEnum

`PLAN_CHANGE_POLICY_MIXED_OR_INCOMPARABLE_TRANSITION_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_POLICY_MIXED_OR_INCOMPARABLE_TRANSITION_ENUM_REJECT=1`

### PlanChangePolicyNoOpTransitionEnum

`PLAN_CHANGE_POLICY_NO_OP_TRANSITION_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_POLICY_NO_OP_TRANSITION_ENUM_REJECT=1`

### PlanChangePolicyStorageShrinkEnum

`PLAN_CHANGE_POLICY_STORAGE_SHRINK_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_POLICY_STORAGE_SHRINK_ENUM_REJECT_UNLESS_OWNER_SUPPORTED_TRANSITION=1`

### PlanChangePolicyConcurrencyEnum

`PLAN_CHANGE_POLICY_CONCURRENCY_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_POLICY_CONCURRENCY_ENUM_ONE_UNFINISHED_PLAN_CHANGE_PER_WORKSPACE=1`

### PlanChangePolicyCancelAndReplaceEnum

`PLAN_CHANGE_POLICY_CANCEL_AND_REPLACE_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_POLICY_CANCEL_AND_REPLACE_ENUM_EXPLICIT_CANCEL_THEN_NEW_QUOTE=1`

### PlanChangePolicyBaseRefundPolicyEnum

`PLAN_CHANGE_POLICY_BASE_REFUND_POLICY_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_POLICY_BASE_REFUND_POLICY_ENUM_PRESERVE_ORIGINAL_BASE_ORDER_POLICY=1`

### PlanChangePolicyProviderExecutionPlanEnum

`PLAN_CHANGE_POLICY_PROVIDER_EXECUTION_PLAN_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_POLICY_PROVIDER_EXECUTION_PLAN_ENUM_APPROVED_STRATEGY_FROZEN_BEFORE_QUOTE=1`

### UpgradeProrationRoundingEnum

`UPGRADE_PRORATION_ROUNDING_ENUM_UNSPECIFIED=0`; `UPGRADE_PRORATION_ROUNDING_ENUM_CEIL_ONCE_USD_MICRO=1`

### PlanChangeCalculationPolicyVersionEnum

`PLAN_CHANGE_CALCULATION_POLICY_VERSION_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_CALCULATION_POLICY_VERSION_ENUM_WORKSPACE_PLAN_CHANGE_V1=1`

### PlanChangeCalculationKindEnum

`PLAN_CHANGE_CALCULATION_KIND_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_CALCULATION_KIND_ENUM_UPGRADE_IMMEDIATE=1`; `PLAN_CHANGE_CALCULATION_KIND_ENUM_DOWNGRADE_NEXT_PERIOD=2`

### PlanChangeKindEnum

`PLAN_CHANGE_KIND_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_KIND_ENUM_UPGRADE_IMMEDIATE=1`; `PLAN_CHANGE_KIND_ENUM_DOWNGRADE_NEXT_PERIOD=2`

### PlanChangeStatusEnum

`PLAN_CHANGE_STATUS_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_STATUS_ENUM_REQUESTED=1`; `PLAN_CHANGE_STATUS_ENUM_SCHEDULED=2`; `PLAN_CHANGE_STATUS_ENUM_AWAITING_PAYMENT=3`; `PLAN_CHANGE_STATUS_ENUM_APPLYING=4`; `PLAN_CHANGE_STATUS_ENUM_APPLIED=5`; `PLAN_CHANGE_STATUS_ENUM_FAILED=6`; `PLAN_CHANGE_STATUS_ENUM_NEEDS_ATTENTION=7`; `PLAN_CHANGE_STATUS_ENUM_CANCELLED=8`

### PlanChangeChargeStatusEnum

`PLAN_CHANGE_CHARGE_STATUS_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_CHARGE_STATUS_ENUM_NOT_REQUIRED=1`; `PLAN_CHANGE_CHARGE_STATUS_ENUM_NOT_REQUESTED=2`; `PLAN_CHANGE_CHARGE_STATUS_ENUM_REQUESTED=3`; `PLAN_CHANGE_CHARGE_STATUS_ENUM_CONFIRMED=4`; `PLAN_CHANGE_CHARGE_STATUS_ENUM_REJECTED=5`; `PLAN_CHANGE_CHARGE_STATUS_ENUM_UNKNOWN=6`

### PlanChangeNextPeriodChargeStatusEnum

`PLAN_CHANGE_NEXT_PERIOD_CHARGE_STATUS_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_NEXT_PERIOD_CHARGE_STATUS_ENUM_NOT_REQUIRED=1`; `PLAN_CHANGE_NEXT_PERIOD_CHARGE_STATUS_ENUM_NOT_REQUESTED=2`; `PLAN_CHANGE_NEXT_PERIOD_CHARGE_STATUS_ENUM_REQUESTED=3`; `PLAN_CHANGE_NEXT_PERIOD_CHARGE_STATUS_ENUM_CONFIRMED=4`; `PLAN_CHANGE_NEXT_PERIOD_CHARGE_STATUS_ENUM_REJECTED=5`; `PLAN_CHANGE_NEXT_PERIOD_CHARGE_STATUS_ENUM_UNKNOWN=6`

### PlanChangeObservationResultEnum

`PLAN_CHANGE_OBSERVATION_RESULT_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_OBSERVATION_RESULT_ENUM_CONFIRMED=1`; `PLAN_CHANGE_OBSERVATION_RESULT_ENUM_REJECTED=2`; `PLAN_CHANGE_OBSERVATION_RESULT_ENUM_UNKNOWN=3`

### PlanChangeDeliveryOutcomeEnum

`PLAN_CHANGE_DELIVERY_OUTCOME_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_DELIVERY_OUTCOME_ENUM_PENDING=1`; `PLAN_CHANGE_DELIVERY_OUTCOME_ENUM_APPLIED=2`; `PLAN_CHANGE_DELIVERY_OUTCOME_ENUM_FAILED=3`; `PLAN_CHANGE_DELIVERY_OUTCOME_ENUM_UNKNOWN=4`

### PlanChangeResourceOutcomeEnum

`PLAN_CHANGE_RESOURCE_OUTCOME_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_RESOURCE_OUTCOME_ENUM_UNCHANGED=1`; `PLAN_CHANGE_RESOURCE_OUTCOME_ENUM_TARGET_CONFIRMED=2`; `PLAN_CHANGE_RESOURCE_OUTCOME_ENUM_RESTORED=3`; `PLAN_CHANGE_RESOURCE_OUTCOME_ENUM_IRREVERSIBLE_RESIDUAL=4`; `PLAN_CHANGE_RESOURCE_OUTCOME_ENUM_UNKNOWN=5`

### PlanChangeRuntimeReadbackRequirementEnum

`PLAN_CHANGE_RUNTIME_READBACK_REQUIREMENT_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_RUNTIME_READBACK_REQUIREMENT_ENUM_REQUIRED=1`; `PLAN_CHANGE_RUNTIME_READBACK_REQUIREMENT_ENUM_NOT_APPLICABLE=2`

### PlanChangeCurrentRequirementValidationEnum

`PLAN_CHANGE_CURRENT_REQUIREMENT_VALIDATION_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_CURRENT_REQUIREMENT_VALIDATION_ENUM_VALID=1`; `PLAN_CHANGE_CURRENT_REQUIREMENT_VALIDATION_ENUM_AT_RISK=2`; `PLAN_CHANGE_CURRENT_REQUIREMENT_VALIDATION_ENUM_NOT_CHECKED=3`

### PlanChangeEvidenceKindEnum

`PLAN_CHANGE_EVIDENCE_KIND_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_EVIDENCE_KIND_ENUM_UPGRADE_IMMEDIATE=1`; `PLAN_CHANGE_EVIDENCE_KIND_ENUM_DOWNGRADE_NEXT_PERIOD=2`

### PlanChangeEvidencePolicyVersionEnum

`PLAN_CHANGE_EVIDENCE_POLICY_VERSION_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_EVIDENCE_POLICY_VERSION_ENUM_WORKSPACE_PLAN_CHANGE_V1=1`

### PlanChangeEvidenceOutcomeEnum

`PLAN_CHANGE_EVIDENCE_OUTCOME_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_EVIDENCE_OUTCOME_ENUM_SCHEDULED=1`; `PLAN_CHANGE_EVIDENCE_OUTCOME_ENUM_APPLIED=2`; `PLAN_CHANGE_EVIDENCE_OUTCOME_ENUM_FAILED=3`; `PLAN_CHANGE_EVIDENCE_OUTCOME_ENUM_NEEDS_ATTENTION=4`; `PLAN_CHANGE_EVIDENCE_OUTCOME_ENUM_CANCELLED=5`

### PlanChangeEvidenceDeliveryOutcomeEnum

`PLAN_CHANGE_EVIDENCE_DELIVERY_OUTCOME_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_EVIDENCE_DELIVERY_OUTCOME_ENUM_PENDING=1`; `PLAN_CHANGE_EVIDENCE_DELIVERY_OUTCOME_ENUM_APPLIED=2`; `PLAN_CHANGE_EVIDENCE_DELIVERY_OUTCOME_ENUM_FAILED=3`; `PLAN_CHANGE_EVIDENCE_DELIVERY_OUTCOME_ENUM_UNKNOWN=4`

### PlanChangeEvidenceResourceOutcomeEnum

`PLAN_CHANGE_EVIDENCE_RESOURCE_OUTCOME_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_EVIDENCE_RESOURCE_OUTCOME_ENUM_UNCHANGED=1`; `PLAN_CHANGE_EVIDENCE_RESOURCE_OUTCOME_ENUM_TARGET_CONFIRMED=2`; `PLAN_CHANGE_EVIDENCE_RESOURCE_OUTCOME_ENUM_RESTORED=3`; `PLAN_CHANGE_EVIDENCE_RESOURCE_OUTCOME_ENUM_IRREVERSIBLE_RESIDUAL=4`; `PLAN_CHANGE_EVIDENCE_RESOURCE_OUTCOME_ENUM_UNKNOWN=5`

### PlanChangeEvidenceRuntimeReadbackRequirementEnum

`PLAN_CHANGE_EVIDENCE_RUNTIME_READBACK_REQUIREMENT_ENUM_UNSPECIFIED=0`; `PLAN_CHANGE_EVIDENCE_RUNTIME_READBACK_REQUIREMENT_ENUM_REQUIRED=1`; `PLAN_CHANGE_EVIDENCE_RUNTIME_READBACK_REQUIREMENT_ENUM_NOT_APPLICABLE=2`

### SupplementalRefundEvidencePurposeEnum

`SUPPLEMENTAL_REFUND_EVIDENCE_PURPOSE_ENUM_UNSPECIFIED=0`; `SUPPLEMENTAL_REFUND_EVIDENCE_PURPOSE_ENUM_UPGRADE_FAILURE_FULL=1`; `SUPPLEMENTAL_REFUND_EVIDENCE_PURPOSE_ENUM_SUPPLEMENT_DELETE_UNUSED=2`; `SUPPLEMENTAL_REFUND_EVIDENCE_PURPOSE_ENUM_NEXT_PERIOD_PLAN_FAILURE_FULL=3`

### SupplementalRefundEvidencePolicyVersionEnum

`SUPPLEMENTAL_REFUND_EVIDENCE_POLICY_VERSION_ENUM_UNSPECIFIED=0`; `SUPPLEMENTAL_REFUND_EVIDENCE_POLICY_VERSION_ENUM_WORKSPACE_PLAN_CHANGE_V1=1`

### ListPackagesRpcRequestVisibilityEnum

`LIST_PACKAGES_RPC_REQUEST_VISIBILITY_ENUM_UNSPECIFIED=0`; `LIST_PACKAGES_RPC_REQUEST_VISIBILITY_ENUM_ALL=1`; `LIST_PACKAGES_RPC_REQUEST_VISIBILITY_ENUM_OFFICIAL=2`; `LIST_PACKAGES_RPC_REQUEST_VISIBILITY_ENUM_PRIVATE=3`

### ListPackagesRpcRequestStatusEnum

`LIST_PACKAGES_RPC_REQUEST_STATUS_ENUM_UNSPECIFIED=0`; `LIST_PACKAGES_RPC_REQUEST_STATUS_ENUM_ACTIVE=1`; `LIST_PACKAGES_RPC_REQUEST_STATUS_ENUM_ARCHIVED=2`

### ListCapabilityVersionsRpcRequestStatusEnum

`LIST_CAPABILITY_VERSIONS_RPC_REQUEST_STATUS_ENUM_UNSPECIFIED=0`; `LIST_CAPABILITY_VERSIONS_RPC_REQUEST_STATUS_ENUM_READY=1`; `LIST_CAPABILITY_VERSIONS_RPC_REQUEST_STATUS_ENUM_DEPRECATED=2`; `LIST_CAPABILITY_VERSIONS_RPC_REQUEST_STATUS_ENUM_DELETING=3`; `LIST_CAPABILITY_VERSIONS_RPC_REQUEST_STATUS_ENUM_DELETED=4`

