package fabric

type optionalProviderPorts struct {
	runtimeRepair                       runtimeRepairProvider
	runtimeGatewaySecrets               runtimeGatewaySecretProvider
	gatewaySecretReadback               gatewaySecretReadbackProvider
	providerFacts                       providerFactsReader
	storageAttachmentReadback           storageAttachmentReadbackProvider
	computeDestroyStatus                computeDestroyStatusReader
	computeDestroyFinalizer             computeDestroyAbsenceFinalizer
	storageVolumeStatus                 storageVolumeStatusReader
	runtimeHealth                       runtimeHealthSummaryProvider
	workspaceLaunch                     workspaceLaunchProvider
	workspaceLaunchRuntimeImageRevision workspaceLaunchRuntimeImageRevisionProvider
	workspaceRuntimeDeleteObservation   workspaceRuntimeDeleteObservationProvider
	monthlyPreflightReports             monthlyPreflightReportProvider
	monthlyProviderTruth                monthlyProviderTruthProvider
}

func optionalProviderPortsFrom(provider Provider) optionalProviderPorts {
	ports := optionalProviderPorts{}
	ports.runtimeRepair, _ = provider.(runtimeRepairProvider)
	ports.runtimeGatewaySecrets, _ = provider.(runtimeGatewaySecretProvider)
	ports.gatewaySecretReadback, _ = provider.(gatewaySecretReadbackProvider)
	ports.providerFacts, _ = provider.(providerFactsReader)
	ports.storageAttachmentReadback, _ = provider.(storageAttachmentReadbackProvider)
	ports.computeDestroyStatus, _ = provider.(computeDestroyStatusReader)
	ports.computeDestroyFinalizer, _ = provider.(computeDestroyAbsenceFinalizer)
	ports.storageVolumeStatus, _ = provider.(storageVolumeStatusReader)
	ports.runtimeHealth, _ = provider.(runtimeHealthSummaryProvider)
	ports.workspaceLaunch, _ = provider.(workspaceLaunchProvider)
	ports.workspaceLaunchRuntimeImageRevision, _ = provider.(workspaceLaunchRuntimeImageRevisionProvider)
	ports.workspaceRuntimeDeleteObservation, _ = provider.(workspaceRuntimeDeleteObservationProvider)
	ports.monthlyPreflightReports, _ = provider.(monthlyPreflightReportProvider)
	ports.monthlyProviderTruth, _ = provider.(monthlyProviderTruthProvider)
	return ports
}
