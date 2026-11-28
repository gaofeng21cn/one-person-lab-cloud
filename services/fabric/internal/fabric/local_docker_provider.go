package fabric

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/moby/moby/api/types/network"
	mobyclient "github.com/moby/moby/client"

	contracts "opl-cloud/packages/contracts/go"
)

const localDockerProviderProfileEnv = "OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON"

type LocalDockerProviderConfig struct {
	GatewaySecretRoot            string
	HostStorageRoot              string
	RuntimeHost                  string
	PublishHost                  string
	ApplicationProbeImage        string
	AllowUnboundedSwap           bool
	RuntimeGatewayContainer      string
	ConfigureRuntimeGateway      bool
	StorageQuotaBackend          localDockerProjectQuota
	WorkloadOwnership            localDockerWorkloadOwnership
	TrustedWorkspaceImageSources []string
	ProviderProfileJSON          []byte
	Getenv                       func(string) string
	LookupEnv                    func(string) (string, bool)
}

type localDockerProviderProfile struct {
	SchemaVersion     int                         `json:"schemaVersion"`
	Packages          []localDockerPackageProfile `json:"packages"`
	ProfileID         string                      `json:"profileId,omitempty"`
	CapabilityVersion string                      `json:"capabilityVersion,omitempty"`
	Region            string                      `json:"region,omitempty"`
	// AccountBindings bind an authorized tenant to the provider account that
	// owns and tags its resources. They mirror the Tencent profile shape; a
	// tenant without a binding resolves the installation's default account.
	AccountBindings []localDockerAccountBinding `json:"accountBindings,omitempty"`
	// DefaultAccountID is the provider account this installation runs its
	// resources in when no binding names the tenant. It is a deployment fact
	// carried by the same profile, never a value compiled into the code, so a
	// new tenant never needs a profile edit. A binding that names the tenant
	// always wins over it, and a profile that declares neither refuses the
	// tenant.
	DefaultAccountID string `json:"defaultAccountId,omitempty"`
}

type localDockerAccountBinding struct {
	TenantID  string `json:"tenantId"`
	AccountID string `json:"accountId"`
}

type localDockerPackageProfile struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Available bool                   `json:"available"`
	Compute   ComputePlan            `json:"compute"`
	Storage   localDockerStoragePlan `json:"storage"`
}

type localDockerStoragePlan struct {
	ID          string `json:"id,omitempty"`
	SizeGB      int    `json:"sizeGb"`
	QuotaPolicy string `json:"quotaPolicy"`
}

type localDockerPlanSpec struct {
	Compute ComputePlan            `json:"compute"`
	Storage localDockerStoragePlan `json:"storage"`
}

type LocalDockerProvider struct {
	docker                            localDockerClient
	gatewaySecretRoot                 string
	gatewaySecretRootErr              error
	hostStorageRoot                   string
	hostStorageRootErr                error
	runtimeHost                       string
	publishHost                       string
	applicationProbeImage             string
	allowUnboundedSwap                bool
	runtimeGatewayContainer           string
	configureRuntimeGatewayEnabled    bool
	trustedWorkspaceImageRepositories map[string]struct{}
	trustedWorkspaceImageReferences   map[string]struct{}
	now                               func() time.Time
	storageQuota                      localDockerProjectQuota
	workloadOwnership                 localDockerWorkloadOwnership
	profile                           localDockerProviderProfile
	plans                             map[string]localDockerPackageProfile
	profileErr                        error
	getenv                            func(string) string
}

func (p *LocalDockerProvider) env(key string) string {
	if p != nil && p.getenv != nil {
		return p.getenv(key)
	}
	return os.Getenv(key)
}

func NewLocalDockerProvider() *LocalDockerProvider {
	return NewLocalDockerProviderWithEnv(os.Getenv, os.LookupEnv)
}

func NewLocalDockerProviderWithEnv(getenv func(string) string, lookupEnv func(string) (string, bool)) *LocalDockerProvider {
	if getenv == nil {
		getenv = os.Getenv
	}
	if lookupEnv == nil {
		lookupEnv = func(key string) (string, bool) {
			value := getenv(key)
			return value, value != ""
		}
	}
	trustedSources := []string{}
	if raw, configured := lookupEnv("OPL_FABRIC_LOCAL_DOCKER_TRUSTED_WORKSPACE_IMAGES"); configured {
		trustedSources = strings.Split(raw, ",")
	}
	runtimeHost := firstNonEmpty(strings.TrimSpace(getenv("OPL_FABRIC_LOCAL_DOCKER_HOST")), "127.0.0.1")
	return newLocalDockerProvider(LocalDockerProviderConfig{
		GatewaySecretRoot:            strings.TrimSpace(getenv("OPL_FABRIC_LOCAL_DOCKER_SECRET_ROOT")),
		HostStorageRoot:              strings.TrimSpace(getenv("OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT")),
		RuntimeHost:                  runtimeHost,
		PublishHost:                  firstNonEmpty(strings.TrimSpace(getenv("OPL_FABRIC_LOCAL_DOCKER_PUBLISH_HOST")), runtimeHost),
		ApplicationProbeImage:        strings.TrimSpace(getenv("OPL_FABRIC_LOCAL_DOCKER_PROBE_IMAGE")),
		AllowUnboundedSwap:           getenv("OPL_FABRIC_LOCAL_DOCKER_ALLOW_UNBOUNDED_SWAP") == "1",
		RuntimeGatewayContainer:      strings.TrimSpace(getenv("OPL_FABRIC_LOCAL_DOCKER_GATEWAY_CONTAINER")),
		ConfigureRuntimeGateway:      true,
		TrustedWorkspaceImageSources: trustedSources,
		ProviderProfileJSON:          []byte(strings.TrimSpace(getenv(localDockerProviderProfileEnv))),
		Getenv:                       getenv,
		LookupEnv:                    lookupEnv,
	}, nil)
}

func newLocalDockerProvider(config LocalDockerProviderConfig, docker localDockerClient) *LocalDockerProvider {
	if docker == nil {
		docker = newMobyDockerClient()
	}
	getenv := config.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	lookupEnv := config.LookupEnv
	if lookupEnv == nil {
		if config.Getenv != nil {
			lookupEnv = func(key string) (string, bool) {
				value := getenv(key)
				return value, value != ""
			}
		} else {
			lookupEnv = os.LookupEnv
		}
	}
	trustedSources := config.TrustedWorkspaceImageSources
	if trustedSources == nil {
		trustedSources = []string{}
		if raw, configured := lookupEnv("OPL_FABRIC_LOCAL_DOCKER_TRUSTED_WORKSPACE_IMAGES"); configured {
			trustedSources = strings.Split(raw, ",")
		}
	}
	trustedRepositories, trustedReferences := localDockerWorkspaceImageTrust(trustedSources)
	secretRoot := strings.TrimSpace(config.GatewaySecretRoot)
	secretRootErr := validateLocalDockerGatewaySecretRoot(secretRoot)
	storageRoot := strings.TrimSpace(config.HostStorageRoot)
	storageRootErr := validateLocalDockerStorageRoot(storageRoot)
	storageQuota := config.StorageQuotaBackend
	if storageQuota == nil {
		storageQuota = newLocalDockerProjectQuota(storageRoot)
	}
	workloadOwnership := config.WorkloadOwnership
	if workloadOwnership == nil {
		workloadOwnership = osLocalDockerWorkloadOwnership{}
	}
	profileJSON := config.ProviderProfileJSON
	if len(bytes.TrimSpace(profileJSON)) == 0 {
		profileJSON = []byte(strings.TrimSpace(getenv(localDockerProviderProfileEnv)))
	}
	profile, plans, profileErr := decodeLocalDockerProviderProfile(profileJSON)
	runtimeHost := firstNonEmpty(strings.TrimSpace(config.RuntimeHost), "127.0.0.1")
	return &LocalDockerProvider{
		docker: docker, gatewaySecretRoot: secretRoot, gatewaySecretRootErr: secretRootErr,
		hostStorageRoot: storageRoot, hostStorageRootErr: storageRootErr,
		storageQuota:                      storageQuota,
		workloadOwnership:                 workloadOwnership,
		runtimeHost:                       runtimeHost,
		publishHost:                       firstNonEmpty(strings.TrimSpace(config.PublishHost), runtimeHost),
		applicationProbeImage:             strings.TrimSpace(config.ApplicationProbeImage),
		allowUnboundedSwap:                config.AllowUnboundedSwap,
		runtimeGatewayContainer:           strings.TrimSpace(config.RuntimeGatewayContainer),
		configureRuntimeGatewayEnabled:    config.ConfigureRuntimeGateway,
		trustedWorkspaceImageRepositories: trustedRepositories, trustedWorkspaceImageReferences: trustedReferences,
		profile: profile, plans: plans, profileErr: profileErr,
		now:    func() time.Time { return time.Now().UTC() },
		getenv: getenv,
	}
}

func decodeLocalDockerProviderProfile(raw []byte) (localDockerProviderProfile, map[string]localDockerPackageProfile, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return localDockerProviderProfile{}, nil, fmt.Errorf("local_docker_provider_profile_required")
	}
	var profile localDockerProviderProfile
	if err := json.Unmarshal(raw, &profile); err != nil || profile.SchemaVersion != 1 || len(profile.Packages) == 0 {
		return localDockerProviderProfile{}, nil, fmt.Errorf("local_docker_provider_profile_invalid")
	}
	plans := make(map[string]localDockerPackageProfile, len(profile.Packages))
	for index, item := range profile.Packages {
		item.ID, item.Name = strings.TrimSpace(item.ID), strings.TrimSpace(item.Name)
		item.Compute.ID, item.Compute.Server, item.Compute.InstanceType = strings.TrimSpace(item.Compute.ID), strings.TrimSpace(item.Compute.Server), strings.TrimSpace(item.Compute.InstanceType)
		if item.ID == "" || item.Name == "" || item.Compute.ID == "" || item.Compute.Server == "" || item.Compute.InstanceType == "" || item.Compute.CPU <= 0 || item.Compute.MemoryGB <= 0 ||
			item.Storage.SizeGB < 10 || item.Storage.SizeGB%10 != 0 || item.Storage.QuotaPolicy != "linux-project" || item.Compute.DiskGB != 0 && item.Compute.DiskGB != item.Storage.SizeGB {
			return localDockerProviderProfile{}, nil, fmt.Errorf("local_docker_provider_profile_invalid")
		}
		if _, exists := plans[item.ID]; exists {
			return localDockerProviderProfile{}, nil, fmt.Errorf("local_docker_provider_profile_duplicate_package")
		}
		item.Compute.DiskGB = item.Storage.SizeGB
		profile.Packages[index] = item
		plans[item.ID] = item
	}
	// The default account is read exactly as declared: an empty value declares
	// no default, and a padded value is a malformed profile rather than a
	// silently trimmed account.
	if strings.TrimSpace(profile.DefaultAccountID) != profile.DefaultAccountID {
		return localDockerProviderProfile{}, nil, fmt.Errorf("local_docker_provider_profile_invalid")
	}
	return profile, plans, nil
}

func (p *LocalDockerProvider) localDockerPackagePlan(packageID string) (localDockerPackageProfile, bool) {
	plan, ok := p.plans[packageID]
	return plan, ok && plan.Available
}

func decodeLocalDockerPlanEnvelope(raw json.RawMessage, packageID string, sizeGB int) (localDockerPlanSpec, error) {
	var envelope struct {
		PackageID          string              `json:"packageId"`
		ProviderProfileRef string              `json:"providerProfileRef"`
		SchemaVersion      int                 `json:"schemaVersion"`
		Spec               localDockerPlanSpec `json:"spec"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.SchemaVersion != 1 || envelope.ProviderProfileRef != "local-docker" || envelope.PackageID != packageID ||
		envelope.Spec.Storage.SizeGB != sizeGB || envelope.Spec.Storage.QuotaPolicy != "linux-project" || envelope.Spec.Compute.CPU <= 0 || envelope.Spec.Compute.MemoryGB <= 0 ||
		envelope.Spec.Compute.DiskGB != envelope.Spec.Storage.SizeGB {
		return localDockerPlanSpec{}, ErrLaunchStageBindingConflict
	}
	return envelope.Spec, nil
}

func immutableLocalDockerImage(value string) (repository, reference string, ok bool) {
	value = strings.TrimSpace(value)
	digest := ""
	if strings.HasPrefix(value, "sha256:") {
		digest = strings.TrimPrefix(value, "sha256:")
	} else {
		before, after, found := strings.Cut(value, "@sha256:")
		if !found || before == "" || strings.Contains(before, "@") {
			return "", "", false
		}
		repository, digest = before, after
	}
	if len(digest) != 64 || digest != strings.ToLower(digest) || !validDigest(digest) {
		return "", "", false
	}
	return repository, value, true
}

func localDockerWorkspaceImageTrust(sources []string) (map[string]struct{}, map[string]struct{}) {
	repositories, references := map[string]struct{}{}, map[string]struct{}{}
	for _, raw := range sources {
		source := strings.TrimSpace(raw)
		if source == "" {
			continue
		}
		if _, reference, ok := immutableLocalDockerImage(source); ok {
			references[reference] = struct{}{}
			continue
		}
		lastComponent := source[strings.LastIndex(source, "/")+1:]
		if source != strings.ToLower(source) || strings.ContainsAny(source, "@ \t\r\n") || strings.Contains(lastComponent, ":") || strings.HasSuffix(source, "/") {
			continue
		}
		repositories[source] = struct{}{}
	}
	return repositories, references
}

func (p *LocalDockerProvider) Descriptor() ProviderDescriptor {
	plans := make(map[string]ComputePlan, len(p.plans))
	packages := make([]WorkspacePackage, 0, len(p.plans))
	for _, item := range p.plans {
		if item.Available {
			plans[item.ID] = item.Compute
		}
		packages = append(packages, WorkspacePackage{ID: item.ID, Name: item.Name, ComputeProfileID: item.Compute.ID, CPU: item.Compute.CPU, MemoryGB: item.Compute.MemoryGB, DiskGB: item.Storage.SizeGB, Provider: "local-docker", Available: item.Available})
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].ID < packages[j].ID })
	return ProviderDescriptor{
		Name: "local-docker", Plans: plans,
		DefaultComputePoolIDs: func() map[string]string {
			pools := make(map[string]string, len(plans))
			for id := range plans {
				pools[id] = "local-docker"
			}
			return pools
		}(),
		Catalog: Catalog{
			SchemaVersion: 1, Owner: "OPL Fabric",
			WorkspacePackages: packages,
			StorageClasses:    []StorageClass{{ID: "workspace-local", StorageClassName: "host-directory", Provider: "local-docker", Available: true}},
			IngressDomains:    []IngressDomain{{ID: "workspace", Host: "127.0.0.1", PathPattern: "/", Available: true}},
		},
	}
}

func (p *LocalDockerProvider) ResolveWorkspacePlan(_ context.Context, input WorkspaceLaunchPlanInput) (json.RawMessage, error) {
	item, ok := p.localDockerPackagePlan(input.PackageID)
	if !ok || item.Storage.SizeGB != input.SizeGB {
		return nil, ErrProviderPlanUnavailable
	}
	return json.Marshal(map[string]any{
		"compute": item.Compute,
		"storage": item.Storage,
	})
}

func (p *LocalDockerProvider) ValidateWorkspaceImageReference(value string) bool {
	repository, reference, ok := immutableLocalDockerImage(value)
	if !ok {
		return false
	}
	if _, trusted := p.trustedWorkspaceImageReferences[reference]; trusted {
		return true
	}
	_, trusted := p.trustedWorkspaceImageRepositories[repository]
	return repository != "" && trusted
}

func (*LocalDockerProvider) ValidateComputeAllocation(allocation ComputeAllocation, prepared ComputeAllocationPreparation) error {
	if allocation.Provider != "local-docker" || allocation.PoolID != prepared.PoolID || allocation.NodePoolID != prepared.NodePoolID ||
		allocation.PackageID != prepared.PackageID || allocation.InstanceType != prepared.InstanceType ||
		!strings.HasPrefix(allocation.ProviderResourceID, "network/") || allocation.Zone != "local" || allocation.ChargeType != "LOCAL" {
		return fmt.Errorf("compute_provider_readback_mismatch")
	}
	return nil
}

func (p *LocalDockerProvider) MonthlyPreflight(ctx context.Context, input MonthlyPreflightInput) (MonthlyPreflight, error) {
	if p == nil || p.profileErr != nil {
		return MonthlyPreflight{}, ErrProviderPlanUnavailable
	}
	_, ok := p.localDockerPackagePlan(input.PackageID)
	if !ok {
		return MonthlyPreflight{}, ErrProviderPlanUnavailable
	}
	if _, err := p.docker.Info(ctx); err != nil {
		return MonthlyPreflight{}, err
	}
	requestIDs := map[string]string{"nodePool": "local-docker", "subnets": "local-bridge", "availability": "docker-engine", "quota": "local-host"}
	if input.ResourceType == "storage" {
		if err := p.storageQuota.Preflight(p.hostStorageRoot); err != nil {
			return MonthlyPreflight{}, err
		}
		if err := p.prepareStorageRoot(ctx); err != nil {
			return MonthlyPreflight{}, err
		}
		if err := p.preflightStorageCapacity(ctx, input.SizeGB); err != nil {
			return MonthlyPreflight{}, err
		}
		requestIDs = map[string]string{"quota": "local-host", "price": "not-applicable"}
	}
	return MonthlyPreflight{
		ResourceType: input.ResourceType, PackageID: input.PackageID, NodePoolID: "local-docker", SizeGB: input.SizeGB, Zone: input.Zone,
		Available: true, ChargeType: "LOCAL", RenewFlag: "NOT_APPLICABLE", ProviderRequestIDs: requestIDs,
	}, nil
}

func (p *LocalDockerProvider) prepareComputeAllocationWithPlan(ctx context.Context, input ComputeAllocationInput, plan ComputePlan) (ComputeAllocationPreparation, error) {
	if plan.ID == "" || plan.InstanceType == "" || plan.CPU <= 0 || plan.MemoryGB <= 0 {
		return ComputeAllocationPreparation{}, ErrUnsupportedComputePackage
	}
	if input.NodePoolID != "local-docker" {
		return ComputeAllocationPreparation{}, fmt.Errorf("local_docker_compute_pool_mismatch")
	}
	if _, err := p.docker.Info(ctx); err != nil {
		return ComputeAllocationPreparation{}, err
	}
	return ComputeAllocationPreparation{
		PoolID: plan.ID, PackageID: input.PackageID, NodePoolID: input.NodePoolID, InstanceType: plan.InstanceType,
		MaxReplicas: 1024, BaselineReplicas: 0, TargetReplicas: 1, ProviderRequestID: providerRequestID("docker-prepare", input.ID),
	}, nil
}

func (p *LocalDockerProvider) PrepareComputeAllocation(ctx context.Context, input ComputeAllocationInput) (ComputeAllocationPreparation, error) {
	item, ok := p.localDockerPackagePlan(input.PackageID)
	if !ok {
		return ComputeAllocationPreparation{}, ErrUnsupportedComputePackage
	}
	return p.prepareComputeAllocationWithPlan(ctx, input, item.Compute)
}

type dockerNetworkInspect struct {
	ID     string            `json:"Id"`
	Name   string            `json:"Name"`
	Labels map[string]string `json:"Labels"`
}

type dockerVolumeInspect struct {
	Name   string            `json:"Name"`
	Labels map[string]string `json:"Labels"`
}

func matchExactNetworkSummary(summaries []network.Summary, name string) (network.Summary, bool, error) {
	var matched []network.Summary
	for _, summary := range summaries {
		if summary.Name != name {
			return network.Summary{}, false, fmt.Errorf("local_docker_inventory_identity_conflict")
		}
		matched = append(matched, summary)
	}
	if len(matched) == 0 {
		return network.Summary{}, false, nil
	}
	if len(matched) != 1 {
		return network.Summary{}, false, fmt.Errorf("local_docker_inventory_identity_conflict")
	}
	return matched[0], true, nil
}

func (p *LocalDockerProvider) inspectNetwork(ctx context.Context, name string) (dockerNetworkInspect, bool, error) {
	summaries, err := p.docker.NetworkList(ctx, mobyclient.NetworkListOptions{
		Filters: make(mobyclient.Filters).Add("name", "^"+name+"$"),
	})
	if err != nil {
		return dockerNetworkInspect{}, false, err
	}
	summary, exists, err := matchExactNetworkSummary(summaries, name)
	if err != nil || !exists {
		return dockerNetworkInspect{}, false, err
	}
	resp, err := p.docker.NetworkInspect(ctx, firstNonEmpty(summary.ID, name), mobyclient.NetworkInspectOptions{})
	if err != nil {
		return dockerNetworkInspect{}, false, err
	}
	inspected := dockerNetworkInspectFromSDK(resp)
	if inspected.ID == "" || inspected.Name != name || (summary.ID != "" && inspected.ID != summary.ID) {
		return dockerNetworkInspect{}, false, fmt.Errorf("local_docker_network_readback_invalid")
	}
	return inspected, true, nil
}

func localDockerLabels(accountID, workspaceID, resourceID, operationID, kind string) map[string]string {
	return map[string]string{
		"opl.fabric.provider": "local-docker", "opl.fabric.kind": kind, "opl.account.id": accountID,
		"opl.workspace.id": workspaceID, "opl.resource.id": resourceID, "opl.operation.id": operationID,
	}
}

func exactDockerLabels(actual, expected map[string]string) bool {
	for key, value := range expected {
		if value != "" && actual[key] != value {
			return false
		}
	}
	return true
}

func localDockerName(prefix, id string) string {
	return prefix + "-" + stableSuffix(id)[:16]
}

func (p *LocalDockerProvider) CreateComputeAllocation(ctx context.Context, input ComputeAllocationExecution) (ComputeAllocation, error) {
	allocation, prepared := input.Allocation, input.Plan
	name := localDockerName("opl-compute", allocation.ID)
	labels := localDockerLabels(allocation.AccountID, allocation.WorkspaceID, allocation.ID, allocation.OperationID, "compute")
	if input.DryRun {
		return p.localComputeAllocation(allocation, prepared, "dry-run"), nil
	}
	attempt, err := beginProviderMutation(ctx, "local_docker_network_create", "compute_allocation", allocation.ID, name)
	if err != nil {
		return ComputeAllocation{}, err
	}
	readback, exists, err := p.inspectNetwork(ctx, name)
	if err != nil {
		return ComputeAllocation{}, err
	}
	if !exists {
		if attempt != nil && !attempt.Fresh {
			claimed, claimErr := attempt.claimReplay(ctx)
			if claimErr != nil || !claimed {
				return ComputeAllocation{}, firstNonNil(claimErr, ErrWorkspaceLaunchPending)
			}
			readback, exists, err = p.inspectNetwork(ctx, name)
			if err != nil {
				_ = attempt.complete(ctx, "", allocation, err)
				return ComputeAllocation{}, err
			}
		}
		if !exists {
			if dispatchErr := attempt.markReplayDispatch(ctx); dispatchErr != nil {
				return ComputeAllocation{}, dispatchErr
			}
			if _, err := p.docker.NetworkCreate(ctx, name, mobyclient.NetworkCreateOptions{Driver: "bridge", Labels: labels}); err != nil {
				_ = attempt.complete(ctx, "", allocation, err)
				return ComputeAllocation{}, err
			}
			readback, exists, err = p.inspectNetwork(ctx, name)
		}
	}
	if err != nil || !exists || !exactDockerLabels(readback.Labels, labels) {
		readErr := fmt.Errorf("local_docker_compute_readback_mismatch")
		_ = attempt.complete(ctx, "", allocation, readErr)
		return ComputeAllocation{}, readErr
	}
	resource := p.localComputeAllocation(allocation, prepared, readback.ID)
	if completeErr := attempt.complete(ctx, resource.ProviderRequestID, resource, nil); completeErr != nil {
		return ComputeAllocation{}, completeErr
	}
	return resource, nil
}

func (p *LocalDockerProvider) localComputeAllocation(allocation ComputeAllocation, prepared ComputeAllocationPreparation, networkID string) ComputeAllocation {
	deadline := p.now().AddDate(0, 1, 0).Format(time.RFC3339)
	return ComputeAllocation{
		ID: allocation.ID, OperationID: allocation.OperationID, AccountID: allocation.AccountID, WorkspaceID: allocation.WorkspaceID,
		PackageID: allocation.PackageID, Status: "running", Provider: "local-docker", ProviderResourceID: "network/" + networkID,
		ProviderRequestID: providerRequestID("docker-compute", allocation.ID), PoolID: prepared.PoolID, NodePoolID: prepared.NodePoolID,
		InstanceType: prepared.InstanceType, Zone: "local", ChargeType: "LOCAL", RenewFlag: "NOT_APPLICABLE", Deadline: deadline,
		CreatedAt: p.now(),
	}
}

func (p *LocalDockerProvider) TagComputeMachine(ctx context.Context, _ ProviderMachine, ownership MachineOwnership) error {
	readback, exists, err := p.inspectNetwork(ctx, localDockerName("opl-compute", ownership.ResourceID))
	if err != nil || !exists {
		return firstNonNil(err, fmt.Errorf("local_docker_compute_not_found"))
	}
	expected := localDockerLabels(ownership.AccountID, ownership.WorkspaceID, ownership.ResourceID, "", "compute")
	if !exactDockerLabels(readback.Labels, expected) {
		return fmt.Errorf("local_docker_compute_ownership_mismatch")
	}
	return nil
}

func firstNonNil(values ...error) error {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func (p *LocalDockerProvider) ReadComputeAllocation(ctx context.Context, allocation ComputeAllocation) (ComputeAllocation, error) {
	name := localDockerName("opl-compute", allocation.ID)
	readback, exists, err := p.inspectNetwork(ctx, name)
	if err != nil {
		return allocation, err
	}
	if !exists {
		allocation.Status = "external_deleted"
		// This provider owns no TKE machine, CVM or CBS disk: after the local
		// compute and volume are gone those resources do not exist in its scope.
		absent := false
		allocation.MachinePresent = &absent
		allocation.CVMStatus = "NOT_FOUND"
		allocation.TKEStatus = "NOT_FOUND"
		return allocation, fmt.Errorf("local_docker_compute_not_found")
	}
	expected := localDockerLabels(allocation.AccountID, allocation.WorkspaceID, allocation.ID, "", "compute")
	if !exactDockerLabels(readback.Labels, expected) {
		return allocation, fmt.Errorf("local_docker_compute_ownership_mismatch")
	}
	allocation.Status, allocation.Provider, allocation.ProviderResourceID = "running", "local-docker", "network/"+readback.ID
	allocation.ProviderRequestID = providerRequestID("docker-compute-read", allocation.ID)
	allocation.Zone = "local"
	return allocation, nil
}

func (p *LocalDockerProvider) ReadComputeProviderFacts(ctx context.Context, allocation ComputeAllocation) (ProviderResourceFacts, error) {
	readback, err := p.ReadComputeAllocation(ctx, allocation)
	if err != nil {
		if err.Error() == "local_docker_compute_not_found" {
			return ProviderResourceFacts{Observation: observationFromFacts(ProviderResourceFacts{Status: "external_deleted"})}, err
		}
		return ProviderResourceFacts{}, err
	}
	return ProviderResourceFacts{
		PackageOrSpec: readback.InstanceType,
		ProviderID:    readback.ProviderResourceID,
		Zone:          readback.Zone,
		Status:        readback.Status,
		ExpiresAt:     readback.Deadline,
	}, nil
}

// ReadComputeDestroyStatus answers the read-only absence readback for the local
// provider. It owns no TKE machine, CVM or CBS disk, so a missing local compute
// network is the complete absence fact for its scope. A compute network that is
// still present means the deletion this readback attests is unfinished, and the
// readback classifies that outcome so the owning operation keeps retrying by
// reading again instead of recording a terminal conflict.
func (p *LocalDockerProvider) ReadComputeDestroyStatus(ctx context.Context, allocation ComputeAllocation) (ComputeAllocation, error) {
	readback, err := p.ReadComputeAllocation(ctx, allocation)
	if err != nil && readback.Status != "external_deleted" {
		return readback, err
	}
	if readback.Status != "external_deleted" {
		// The retained allocation records whether the network removal was
		// already dispatched. A dispatched removal may have run, so only a
		// read-only reconciliation may retry it; without one the owner may
		// still remove the network itself.
		readback.DestroyState = contracts.WorkspaceDeleteOutcomePendingRetry
		if allocation.Status == "destroying" {
			readback.DestroyState = contracts.WorkspaceDeleteOutcomeUnconfirmedSend
		}
	}
	return readback, nil
}

func (p *LocalDockerProvider) SyncComputeAllocation(ctx context.Context, allocation ComputeAllocation) (ComputeAllocation, error) {
	return p.ReadComputeAllocation(ctx, allocation)
}

func (p *LocalDockerProvider) RenewComputeAllocation(ctx context.Context, allocation ComputeAllocation) (ComputeAllocation, error) {
	readback, err := p.ReadComputeAllocation(ctx, allocation)
	if err != nil {
		return readback, err
	}
	readback.Deadline = p.now().AddDate(0, 1, 0).Format(time.RFC3339)
	return readback, nil
}

func (p *LocalDockerProvider) DestroyComputeAllocation(ctx context.Context, allocation ComputeAllocation) (ComputeAllocation, error) {
	name := localDockerName("opl-compute", allocation.ID)
	network, exists, err := p.inspectNetwork(ctx, name)
	if err != nil {
		return allocation, err
	}
	if exists {
		expected := localDockerLabels(allocation.AccountID, allocation.WorkspaceID, allocation.ID, "", "compute")
		if !exactDockerLabels(network.Labels, expected) {
			return allocation, fmt.Errorf("local_docker_compute_ownership_mismatch")
		}
		if p.runtimeGatewayContainer != "" {
			gateway, gatewayExists, inspectErr := p.inspectContainer(ctx, p.runtimeGatewayContainer)
			if inspectErr != nil || !gatewayExists || gateway.Config.Labels["opl.fabric.local-docker.gateway"] != "control-plane" {
				return allocation, firstNonNil(inspectErr, fmt.Errorf("local_docker_runtime_gateway_identity_mismatch"))
			}
			bound, bindingErr := exactContainerNetworkMembership(gateway, name, network.ID)
			if bindingErr != nil {
				return allocation, bindingErr
			}
			if bound {
				disconnectErr := p.docker.NetworkDisconnect(ctx, name, p.runtimeGatewayContainer, false)
				gateway, gatewayExists, inspectErr = p.inspectContainer(ctx, p.runtimeGatewayContainer)
				if inspectErr != nil || !gatewayExists {
					return allocation, firstNonNil(inspectErr, fmt.Errorf("local_docker_runtime_gateway_identity_mismatch"))
				}
				bound, bindingErr = exactContainerNetworkMembership(gateway, name, network.ID)
				if bindingErr != nil || bound {
					return allocation, firstNonNil(bindingErr, disconnectErr, fmt.Errorf("local_docker_runtime_gateway_network_readback_mismatch"))
				}
			}
		}
		removeErr := p.docker.NetworkRemove(ctx, name)
		if _, stillExists, readErr := p.inspectNetwork(ctx, name); readErr != nil || stillExists {
			return allocation, firstNonNil(readErr, removeErr, fmt.Errorf("local_docker_compute_destroy_readback_mismatch"))
		}
	}
	allocation.Status, allocation.ProviderRequestID = "destroyed", providerRequestID("docker-compute-destroy", allocation.ID)
	return allocation, nil
}

func (p *LocalDockerProvider) CreateStorageVolume(ctx context.Context, input StorageVolumeInput) (StorageVolume, error) {
	paths, err := p.storagePaths(input.WorkspaceID)
	if err != nil {
		return StorageVolume{}, err
	}
	attempt, err := beginProviderMutation(ctx, "local_docker_storage_directory_create", "storage_volume", input.ID, paths.WorkspaceName)
	if err != nil {
		return StorageVolume{}, err
	}
	metadata := localDockerStorageMetadata{SchemaVersion: localDockerStorageMetadataSchemaVersion, StorageID: input.ID, AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, SizeGB: input.SizeGB}
	if attempt != nil && !attempt.Fresh {
		if _, readErr := p.readStorageDirectories(StorageVolume{ID: input.ID, AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, SizeGB: input.SizeGB}); readErr != nil {
			claimed, claimErr := attempt.claimReplay(ctx)
			if claimErr != nil || !claimed {
				return StorageVolume{}, firstNonNil(claimErr, ErrWorkspaceLaunchPending)
			}
			if dispatchErr := attempt.markReplayDispatch(ctx); dispatchErr != nil {
				return StorageVolume{}, dispatchErr
			}
		}
	} else if attempt != nil {
		if dispatchErr := attempt.markReplayDispatch(ctx); dispatchErr != nil {
			return StorageVolume{}, dispatchErr
		}
	}
	if _, err := p.ensureStorageDirectories(ctx, metadata, input.SizeGB); err != nil {
		readErr := fmt.Errorf("local_docker_storage_readback_mismatch: %w", err)
		_ = attempt.complete(ctx, "", StorageVolume{ID: input.ID, AccountID: input.AccountID, WorkspaceID: input.WorkspaceID}, readErr)
		return StorageVolume{}, readErr
	}
	deadline := p.now().AddDate(0, 1, 0).Format(time.RFC3339)
	resource := StorageVolume{
		ID: input.ID, OperationID: input.IdempotencyKey, AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, Status: "ready",
		Provider: "local-docker", ProviderResourceID: "directory/" + paths.WorkspaceName, ProviderRequestID: providerRequestID("docker-storage", input.ID),
		SizeGB: input.SizeGB, StorageClass: "host-directory", DiskType: "local-directory",
		RenewFlag: "NOT_APPLICABLE", Deadline: deadline, Zone: "local", CreatedAt: p.now(),
	}
	if completeErr := attempt.complete(ctx, resource.ProviderRequestID, resource, nil); completeErr != nil {
		return StorageVolume{}, completeErr
	}
	return resource, nil
}

func (p *LocalDockerProvider) ReadStorageVolume(ctx context.Context, volume StorageVolume) (StorageVolume, error) {
	paths, err := p.readStorageDirectories(volume)
	if errors.Is(err, ErrWorkspaceLaunchResourceAbsent) {
		volume.Status = "external_deleted"
		return volume, fmt.Errorf("local_docker_storage_not_found: %w", err)
	}
	if err != nil {
		return volume, fmt.Errorf("local_docker_storage_ownership_mismatch: %w", err)
	}
	volume.Status, volume.Provider, volume.ProviderResourceID = "ready", "local-docker", "directory/"+paths.WorkspaceName
	volume.StorageClass, volume.DiskType = "host-directory", "local-directory"
	volume.ProviderRequestID = providerRequestID("docker-storage-read", volume.ID)
	return volume, nil
}

func (p *LocalDockerProvider) ReadStorageProviderFacts(ctx context.Context, volume StorageVolume) (ProviderResourceFacts, error) {
	readback, err := p.ReadStorageVolume(ctx, volume)
	if err != nil {
		if errors.Is(err, ErrWorkspaceLaunchResourceAbsent) {
			return ProviderResourceFacts{Observation: observationFromFacts(ProviderResourceFacts{Status: "external_deleted"})}, err
		}
		return ProviderResourceFacts{}, err
	}
	return ProviderResourceFacts{
		PackageOrSpec: firstNonEmpty(readback.DiskType, readback.StorageClass),
		ProviderID:    readback.ProviderResourceID,
		Zone:          readback.Zone,
		Status:        readback.Status,
		ExpiresAt:     readback.Deadline,
	}, nil
}

func (p *LocalDockerProvider) ReadStorageVolumeStatus(ctx context.Context, volume StorageVolume) (StorageVolume, error) {
	readback, err := p.ReadStorageVolume(ctx, volume)
	if errors.Is(err, ErrWorkspaceLaunchResourceAbsent) {
		readback.CBSStatus = "NOT_FOUND"
		absent := false
		readback.BindingPresent = &absent
		return readback, nil
	}
	if err == nil {
		present := true
		readback.BindingPresent = &present
	}
	return readback, err
}

func (p *LocalDockerProvider) SyncStorageVolume(ctx context.Context, volume StorageVolume) (StorageVolume, error) {
	return p.ReadStorageVolume(ctx, volume)
}

func (p *LocalDockerProvider) RenewStorageVolume(ctx context.Context, volume StorageVolume) (StorageVolume, error) {
	readback, err := p.ReadStorageVolume(ctx, volume)
	if err != nil {
		return readback, err
	}
	readback.Deadline = p.now().AddDate(0, 1, 0).Format(time.RFC3339)
	return readback, nil
}

func (p *LocalDockerProvider) DestroyStorageVolume(ctx context.Context, volume StorageVolume) (StorageVolume, error) {
	paths, pathErr := p.storagePaths(volume.WorkspaceID)
	if pathErr != nil || volume.ID == "" || volume.AccountID == "" {
		return volume, firstNonNil(pathErr, fmt.Errorf("local_docker_storage_identity_invalid"))
	}
	if err := p.storageQuota.Preflight(p.hostStorageRoot); err != nil {
		return volume, err
	}
	if err := p.withStorageQuotaLock(ctx, func() error {
		root, err := p.openStorageRoot()
		if err != nil {
			return err
		}
		defer root.Close()
		deletionName := localDockerStorageDeletionName(paths.WorkspaceName)
		deletion, err := readLocalDockerStorageDeletion(root, deletionName)
		if err == nil {
			if deletion.StorageID != volume.ID || deletion.AccountID != volume.AccountID || deletion.WorkspaceID != volume.WorkspaceID || deletion.SizeGB != volume.SizeGB {
				return fmt.Errorf("local_docker_storage_destroy_ownership_mismatch")
			}
			return p.resumeStorageDeletionLocked(root, deletion)
		}
		if !errors.Is(err, ErrWorkspaceLaunchResourceAbsent) {
			return err
		}
		metadata, err := readLocalDockerStorageOwnerMetadata(root, paths)
		if errors.Is(err, ErrWorkspaceLaunchResourceAbsent) {
			return nil
		}
		if err != nil || metadata.SchemaVersion != localDockerStorageMetadataSchemaVersion || metadata.ProjectID == 0 || metadata.StorageID != volume.ID || metadata.AccountID != volume.AccountID || metadata.WorkspaceID != volume.WorkspaceID || metadata.SizeGB != volume.SizeGB {
			return firstNonNil(err, fmt.Errorf("local_docker_storage_destroy_ownership_mismatch"))
		}
		deletion = localDockerStorageDeletionFromMetadata(metadata, paths.WorkspaceName)
		if err := writeLocalDockerStorageDeletion(root, deletion); err != nil {
			return err
		}
		return p.resumeStorageDeletionLocked(root, deletion)
	}); err != nil {
		return volume, err
	}
	volume.Status, volume.ProviderRequestID = "destroyed", providerRequestID("docker-storage-destroy", volume.ID)
	return volume, nil
}

func (p *LocalDockerProvider) CreateStorageAttachment(ctx context.Context, input StorageAttachmentInput, compute ComputeAllocation, volume StorageVolume) (StorageAttachment, error) {
	if _, err := p.ReadComputeAllocation(ctx, compute); err != nil {
		return StorageAttachment{}, err
	}
	if _, err := p.ReadStorageVolume(ctx, volume); err != nil {
		return StorageAttachment{}, err
	}
	paths, err := p.storagePaths(volume.WorkspaceID)
	if err != nil {
		return StorageAttachment{}, err
	}
	id := "att_" + stableSuffix(input.IdempotencyKey)[:18]
	return StorageAttachment{
		ID: id, OperationID: input.IdempotencyKey, WorkspaceID: input.WorkspaceID, ComputeID: input.ComputeID, VolumeID: input.VolumeID,
		Status: "attached", Provider: "local-docker", ProviderAttachmentID: "docker/" + localDockerName("opl-compute", compute.ID) + "/" + paths.WorkspaceName,
		ProviderRequestID: providerRequestID("docker-attachment", input.IdempotencyKey), CreatedAt: p.now(),
	}, nil
}

func (p *LocalDockerProvider) ReadStorageAttachment(ctx context.Context, attachment StorageAttachment, compute ComputeAllocation, volume StorageVolume) (StorageAttachment, error) {
	if _, err := p.ReadComputeAllocation(ctx, compute); err != nil {
		return attachment, err
	}
	if _, err := p.ReadStorageVolume(ctx, volume); err != nil {
		return attachment, err
	}
	paths, err := p.storagePaths(volume.WorkspaceID)
	if err != nil {
		return attachment, err
	}
	attachment.Status, attachment.Provider = "attached", "local-docker"
	attachment.ProviderAttachmentID = "docker/" + localDockerName("opl-compute", compute.ID) + "/" + paths.WorkspaceName
	attachment.ProviderRequestID = providerRequestID("docker-attachment-read", attachment.OperationID)
	return attachment, nil
}

func (p *LocalDockerProvider) ReadStorageAttachmentProviderFacts(ctx context.Context, attachment StorageAttachment, compute ComputeAllocation, volume StorageVolume) (ProviderResourceFacts, error) {
	readback, err := p.ReadStorageAttachment(ctx, attachment, compute, volume)
	if err != nil {
		return ProviderResourceFacts{}, err
	}
	return ProviderResourceFacts{PackageOrSpec: "/data", ProviderID: readback.ProviderAttachmentID, Status: readback.Status}, nil
}

func (*LocalDockerProvider) DetachStorageAttachment(_ context.Context, attachment StorageAttachment) (StorageAttachment, error) {
	attachment.Status = "detached"
	attachment.ProviderRequestID = providerRequestID("docker-attachment-detach", attachment.OperationID)
	return attachment, nil
}

func (p *LocalDockerProvider) Readiness(ctx context.Context) (FabricReadiness, error) {
	if p.profileErr != nil {
		return FabricReadiness{}, p.profileErr
	}
	if p.gatewaySecretRootErr != nil {
		return FabricReadiness{}, p.gatewaySecretRootErr
	}
	if p.hostStorageRootErr != nil {
		return FabricReadiness{}, p.hostStorageRootErr
	}
	if err := p.storageQuota.Preflight(p.hostStorageRoot); err != nil {
		return FabricReadiness{}, err
	}
	root, err := p.openStorageRoot()
	if err != nil {
		return FabricReadiness{}, err
	}
	inventoryErr := p.validateStorageMetadataInventoryLocked(root)
	closeErr := root.Close()
	if inventoryErr != nil || closeErr != nil {
		return FabricReadiness{}, firstNonNil(inventoryErr, closeErr)
	}
	_, err = p.docker.Info(ctx)
	if err != nil {
		return FabricReadiness{}, err
	}
	return FabricReadiness{Provider: "local-docker", ServiceReady: true, MissingEnv: []string{}, MissingTools: []string{}, FailedChecks: []string{}}, nil
}
