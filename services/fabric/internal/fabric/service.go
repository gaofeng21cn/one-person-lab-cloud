package fabric

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

const (
	storageProvisionTimeout          = 10 * time.Minute
	computeAllocationPollInterval    = 10 * time.Second
	computeAllocationPollWindow      = 10 * time.Minute
	computeAllocationAttemptTimeout  = 30 * time.Second
	computeAllocationFinalizeTimeout = 2 * time.Minute
	providerFactsBatchTimeout        = 5 * time.Second
	providerFactsBatchWorkerCount    = 8
	readinessProviderTimeout         = 5 * time.Second
	readinessSuccessTTL              = 5 * time.Second
	runtimeHealthSummaryTimeout      = 5 * time.Second
)

type readinessRefresh struct {
	done   chan struct{}
	result map[string]any
	err    error
}

type Service struct {
	providerDescriptor               providerDescriptorReader
	computeProvider                  computeProvider
	storageProvider                  storageProvider
	attachmentProvider               attachmentProvider
	secretProvider                   secretProvider
	runtimeProvider                  runtimeMutationProvider
	workspaceImagePolicy             workspaceImagePolicy
	workspaceLaunchPlans             providerPlanResolver
	monthlyPreflightProvider         monthlyPreflightProvider
	providerReadiness                providerReadiness
	optionalProviders                optionalProviderPorts
	mu                               sync.Mutex
	jobMu                            sync.Mutex
	readinessMu                      sync.Mutex
	readinessCached                  bool
	readinessResult                  map[string]any
	readinessExpiresAt               time.Time
	readinessRefresh                 *readinessRefresh
	readinessTTL                     time.Duration
	readinessTimeout                 time.Duration
	computes                         map[string]ComputeAllocation
	volumes                          map[string]StorageVolume
	attachments                      map[string]StorageAttachment
	destroying                       map[string]bool
	reconciling                      map[string]bool
	operationJournal                 OperationJournalStore
	operationHistory                 OperationHistoryStore
	resourceOperations               ResourceOperationStore
	runtimeOperationQueries          RuntimeOperationQueryStore
	runtimeRead                      *workspaceRuntimeReadEngine
	computeClaims                    ComputeClaimStore
	workspaceLaunchPreflights        WorkspaceLaunchPreflightStore
	launchStages                     *launchStageEngine
	providerMutations                ProviderMutationStore
	runtimeOperations                RuntimeOperationStore
	machineOwnership                 MachineOwnershipStore
	computePool                      ComputePoolStore
	jobStore                         JobStore
	resourceLocks                    ResourceLockStore
	now                              func() time.Time
	computeAllocationPollInterval    time.Duration
	computeAllocationPollWindow      time.Duration
	computeAllocationAttemptTimeout  time.Duration
	computeAllocationFinalizeTimeout time.Duration
}

func NewService(provider Provider) *Service {
	return NewServiceWithOperationStore(provider, NewMemoryOperationStore())
}

func NewServiceWithOperationStore(provider Provider, operations OperationStore) *Service {
	if operations == nil {
		operations = NewMemoryOperationStore()
	}
	ports := operationStoreCapabilityPorts{store: operations}
	computes, volumes, attachments, _ := replayResourceState(context.Background(), ports)
	service := &Service{
		providerDescriptor: provider, computeProvider: provider, storageProvider: provider, attachmentProvider: provider,
		secretProvider: provider, runtimeProvider: provider, workspaceImagePolicy: provider, workspaceLaunchPlans: provider,
		monthlyPreflightProvider: provider, providerReadiness: provider, optionalProviders: optionalProviderPortsFrom(provider),
		computes: computes, volumes: volumes, attachments: attachments,
		destroying: map[string]bool{}, reconciling: map[string]bool{},
		operationJournal: ports, operationHistory: ports, resourceOperations: ports, runtimeOperationQueries: ports,
		computeClaims: ports, workspaceLaunchPreflights: ports, providerMutations: providerMutationStorePort(operations),
		runtimeOperations: operations, machineOwnership: operations, computePool: operations, jobStore: operations, resourceLocks: operations,
		now:                           func() time.Time { return time.Now().UTC() },
		readinessTTL:                  readinessSuccessTTL,
		readinessTimeout:              readinessProviderTimeout,
		computeAllocationPollInterval: computeAllocationPollInterval, computeAllocationPollWindow: computeAllocationPollWindow,
		computeAllocationAttemptTimeout: computeAllocationAttemptTimeout, computeAllocationFinalizeTimeout: computeAllocationFinalizeTimeout,
	}
	service.launchStages = newLaunchStageEngine(
		ports,
		service.optionalProviders.workspaceLaunch,
		service.providerDescriptor,
		service.workspaceImagePolicy,
		service.optionalProviders.workspaceLaunchRuntimeImageRevision,
		service.providerMutations,
		service.machineOwnership,
		func() time.Time { return service.now() },
	)
	service.runtimeRead = newWorkspaceRuntimeReadEngine(provider, ports)
	return service
}

func (s *Service) Catalog(_ context.Context) Catalog {
	return s.providerDescriptor.Descriptor().Catalog
}

func (s *Service) MonthlyPreflight(ctx context.Context, input MonthlyPreflightInput) (MonthlyPreflight, error) {
	if (input.ResourceType != "compute" && input.ResourceType != "storage") || strings.TrimSpace(input.PackageID) == "" || input.PackageID != strings.TrimSpace(input.PackageID) || input.Zone == "" || input.Zone != strings.TrimSpace(input.Zone) ||
		(input.ResourceType == "compute" && input.SizeGB != 0) || (input.ResourceType == "storage" && input.SizeGB <= 0) {
		return MonthlyPreflight{}, ErrInvalidMonthlyPreflight
	}
	result, err := s.monthlyPreflightProvider.MonthlyPreflight(ctx, input)
	if err != nil {
		return MonthlyPreflight{}, fmt.Errorf("%w: %v", ErrMonthlyPreflightUnavailable, err)
	}
	requiredRequestIDs := []string{"nodePool", "subnets", "availability", "quota"}
	if input.ResourceType == "storage" {
		requiredRequestIDs = []string{"quota", "price"}
	}
	validRequestIDs := len(result.ProviderRequestIDs) > 0
	for _, key := range requiredRequestIDs {
		validRequestIDs = validRequestIDs && strings.TrimSpace(result.ProviderRequestIDs[key]) != ""
	}
	pricingValid := !s.providerDescriptor.Descriptor().RequiresMonthlyPricing ||
		strings.TrimSpace(result.ChargeType) != "" && result.PeriodMonths > 0 && strings.TrimSpace(result.RenewFlag) != "" && result.ProviderPriceCNY > 0
	if result.ResourceType != input.ResourceType || result.PackageID != input.PackageID || result.SizeGB != input.SizeGB || result.Zone != input.Zone || !result.Available ||
		!pricingValid || math.IsNaN(result.ProviderPriceCNY) || math.IsInf(result.ProviderPriceCNY, 0) || !validRequestIDs ||
		(input.ResourceType == "compute" && strings.TrimSpace(result.NodePoolID) == "") {
		return MonthlyPreflight{}, ErrMonthlyPreflightUnavailable
	}
	return result, nil
}

func (s *Service) Readiness(ctx context.Context) (map[string]any, error) {
	s.readinessMu.Lock()
	if s.readinessCached && s.now().Before(s.readinessExpiresAt) {
		result := s.readinessResult
		s.readinessMu.Unlock()
		return result, nil
	}
	refresh := s.readinessRefresh
	if refresh == nil {
		refresh = &readinessRefresh{done: make(chan struct{})}
		s.readinessRefresh = refresh
		go s.refreshReadiness(refresh)
	}
	s.readinessMu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-refresh.done:
		return refresh.result, refresh.err
	}
}

func (s *Service) refreshReadiness(refresh *readinessRefresh) {
	ctx, cancel := context.WithTimeout(context.Background(), s.readinessTimeout)
	result, err := s.providerReadiness.Readiness(ctx)
	cancel()

	s.readinessMu.Lock()
	refresh.result = result
	refresh.err = err
	if err == nil {
		s.readinessCached = true
		s.readinessResult = result
		s.readinessExpiresAt = s.now().Add(s.readinessTTL)
	}
	if s.readinessRefresh == refresh {
		s.readinessRefresh = nil
	}
	close(refresh.done)
	s.readinessMu.Unlock()
}

func (s *Service) ListOperations(ctx context.Context) ([]FabricOperation, error) {
	return s.operationHistory.List(ctx)
}

func isReadyResourceStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running", "ready", "active":
		return true
	default:
		return false
	}
}

func isRetainedStorageStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "retained", "released":
		return true
	default:
		return false
	}
}
