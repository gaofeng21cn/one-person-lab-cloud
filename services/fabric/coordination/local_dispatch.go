package coordination

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/fabric/internal/fabric"
)

// LocalResourceDispatcher is the existing Fabric adapter's bounded local-only
// seam. A result is returned only after actual provider readback.
type LocalResourceDispatcher interface {
	EnsureLocal(context.Context, LocalResourceIntent) (*LocalResourceResult, error)
}
type LocalResourceIntent struct {
	TenantID, WorkspaceID, OperationID, ResourceSetID, ComputeID, StorageID string
	Plan                                                                    *api.ResourcePlanSnapshot
}
type LocalResourceResult struct {
	Binding    *api.ResourceExecutionBinding `json:"binding"`
	Compute    fabric.ComputeAllocation      `json:"compute"`
	Storage    fabric.StorageVolume          `json:"storage"`
	Attachment fabric.StorageAttachment      `json:"attachment"`
	ObservedAt time.Time                     `json:"observedAt"`
}

type localDispatcher struct {
	service  *fabric.Service
	provider *fabric.LocalDockerProvider
}

// NewLocalDispatcher uses the exact same service and provider as Fabric's HTTP
// runtime routes, preserving their existing operation store and resource maps.
func NewLocalDispatcher(service *fabric.Service, provider *fabric.LocalDockerProvider) LocalResourceDispatcher {
	if service == nil || provider == nil {
		return nil
	}
	return &localDispatcher{service: service, provider: provider}
}

func (d *localDispatcher) EnsureLocal(ctx context.Context, in LocalResourceIntent) (*LocalResourceResult, error) {
	bound, err := d.provider.ResolveAcceptedResourcePlan(in.TenantID, in.Plan)
	if err != nil {
		return nil, err
	}
	// The quota preflight must pass before even the free Docker network is
	// dispatched, so an unsupported host cannot leave a partial allocation.
	if _, err = d.service.MonthlyPreflight(ctx, fabric.MonthlyPreflightInput{ResourceType: "storage", PackageID: bound.PackageID, SizeGB: int(in.Plan.CapacityGib), Zone: "local"}); err != nil {
		return nil, err
	}
	compute, err := d.service.CreateComputeAllocation(ctx, fabric.ComputeAllocationInput{ID: in.ComputeID, AccountID: bound.AccountID, WorkspaceID: in.WorkspaceID, PackageID: bound.PackageID, NodePoolID: bound.NodePoolID, IdempotencyKey: in.OperationID + ":compute"})
	if err != nil {
		return nil, err
	}
	// Compute uses the provider's existing asynchronous operation. Returning
	// pending lets the next original-order reconcile observe it without a second
	// create or a new workflow worker.
	compute, ok := d.service.GetComputeAllocation(ctx, compute.ID)
	if !ok || compute.Status != "running" {
		return nil, fmt.Errorf("local_compute_pending")
	}
	compute, err = d.provider.ReadComputeAllocation(ctx, compute)
	if err != nil {
		return nil, err
	}
	if compute.ID != in.ComputeID || compute.AccountID != bound.AccountID || compute.WorkspaceID != in.WorkspaceID || compute.Status != "running" || compute.ProviderResourceID == "" {
		return nil, fmt.Errorf("local_compute_readback_mismatch")
	}
	storage, err := d.service.CreateStorageVolume(ctx, fabric.StorageVolumeInput{ID: in.StorageID, AccountID: bound.AccountID, WorkspaceID: in.WorkspaceID, ComputeID: compute.ID, Zone: compute.Zone, SizeGB: int(in.Plan.CapacityGib), IdempotencyKey: in.OperationID + ":storage"})
	if err != nil {
		return nil, err
	}
	storage, err = d.provider.ReadStorageVolume(ctx, storage)
	if err != nil {
		return nil, err
	}
	if storage.ID != in.StorageID || storage.AccountID != bound.AccountID || storage.WorkspaceID != in.WorkspaceID || storage.Status != "ready" || storage.ProviderResourceID == "" {
		return nil, fmt.Errorf("local_storage_readback_mismatch")
	}
	attachment, err := d.service.CreateStorageAttachment(ctx, fabric.StorageAttachmentInput{WorkspaceID: in.WorkspaceID, ComputeID: compute.ID, VolumeID: storage.ID, IdempotencyKey: in.OperationID + ":attachment"})
	if err != nil {
		return nil, err
	}
	attachment, err = d.provider.ReadStorageAttachment(ctx, attachment, compute, storage)
	if err != nil {
		return nil, err
	}
	if attachment.ID == "" || attachment.OperationID != in.OperationID+":attachment" || attachment.WorkspaceID != in.WorkspaceID || attachment.ComputeID != compute.ID || attachment.VolumeID != storage.ID || attachment.Status != "attached" || attachment.ProviderAttachmentID == "" {
		return nil, fmt.Errorf("local_attachment_readback_mismatch")
	}
	return &LocalResourceResult{Binding: &api.ResourceExecutionBinding{ComputeAllocationId: compute.ID, StorageVolumeId: storage.ID, DataAttachmentId: attachment.ID, DataAttachmentOperationId: attachment.OperationID, AccountId: bound.AccountID}, Compute: compute, Storage: storage, Attachment: attachment, ObservedAt: time.Now().UTC()}, nil
}

func (s *Service) resume(ctx context.Context, r *api.EnsureResourcesCommand, operationID string) (*api.Operation, error) {
	current, err := s.operation(ctx, operationID)
	if err != nil {
		return nil, err
	}
	if current.GetStatus() == api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || s.Dispatcher == nil || s.Ledger == nil || r.GetConfirmedChargeReceiptId() == "" || r.GetPlan().GetProvider() != "local-docker" || r.Plan.BillingMode != "LOCAL_NO_CHARGE" || r.QuoteAcceptance.Quote.TotalUsdMicros != 0 {
		return current, nil
	}
	call := proto.Clone(r.Context).(*api.CallContext)
	call.AuthorizationContextId = ""
	evidence, err := s.Ledger.ReadLocalNoChargeReceipt(ctx, &api.GetReceiptByReferenceRequest{Context: call, Owner: "workspace", OwnerEvidenceReference: r.ObligationId})
	if err != nil {
		return current, nil
	}
	receipt, commit := evidence.GetReceipt(), evidence.GetOwnerCommitEvidence()
	if receipt.GetId() != r.ConfirmedChargeReceiptId || receipt.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE || receipt.GetKind() != api.ReceiptKindEnum_RECEIPT_KIND_ENUM_LOCAL_NO_CHARGE || receipt.GetOutcome() != api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED || receipt.GetOperationId() != r.ObligationId || !proto.Equal(evidence.GetQuoteAcceptance(), r.QuoteAcceptance) || commit.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE || commit.GetOperationId() != r.ObligationId || commit.GetResourceId() != r.WorkspaceId || commit.GetActorId() != r.Context.ActorId || !proto.Equal(commit.GetScope(), r.Context.Scope) || commit.GetCommittedVersion() <= 0 || evidence.GetEvidenceDigest() != r.QuoteAcceptance.SnapshotDigest {
		return current, nil
	}
	// A transaction-scoped workspace lock serializes dispatch as well as intent
	// creation. The provider still owns its durable mutation claims; a lost target
	// commit replays those same resource IDs and original keys.
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, persistenceError(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "fabric:workspace:"+r.WorkspaceId); err != nil {
		return nil, persistenceError(err)
	}
	var existingStatus string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM fabric.operations WHERE id=$1 FOR UPDATE`, operationID).Scan(&existingStatus); err != nil {
		return nil, persistenceError(err)
	}
	if existingStatus == "succeeded" {
		if err = tx.Commit(); err != nil {
			return nil, persistenceError(err)
		}
		return s.operation(ctx, operationID)
	}
	intent := LocalResourceIntent{TenantID: r.Context.Scope.GetTenant().TenantId, WorkspaceID: r.WorkspaceId, OperationID: operationID, ResourceSetID: current.ResourceId, Plan: r.Plan}
	if err = tx.QueryRowContext(ctx, `SELECT id FROM fabric.resources WHERE resource_set_id=$1 AND kind='compute'`, current.ResourceId).Scan(&intent.ComputeID); err != nil {
		return nil, persistenceError(err)
	}
	if err = tx.QueryRowContext(ctx, `SELECT id FROM fabric.resources WHERE resource_set_id=$1 AND kind='storage'`, current.ResourceId).Scan(&intent.StorageID); err != nil {
		return nil, persistenceError(err)
	}
	if err = s.authorizeWorkspace(ctx, r.Context, r.WorkspaceId, intent.TenantID, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONACCEPTEDRESOURCES); err != nil {
		return nil, err
	}
	result, dispatchErr := s.Dispatcher.EnsureLocal(ctx, intent)
	if dispatchErr != nil {
		if _, err = tx.ExecContext(ctx, `UPDATE fabric.operations SET status='awaiting_confirmation',stage='resource_preflight',error_code='DEPENDENCY_UNAVAILABLE',observation_result='unknown',updated_at=now() WHERE id=$1`, operationID); err != nil {
			return nil, persistenceError(err)
		}
		if err = tx.Commit(); err != nil {
			return nil, persistenceError(err)
		}
		return s.operation(ctx, operationID)
	}
	if err = validateLocalResult(intent, result); err != nil {
		return nil, err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	proof, err := protojson.Marshal(evidence)
	if err != nil {
		return nil, err
	}
	for _, resource := range []struct {
		id, provider string
		value        any
	}{{intent.ComputeID, result.Compute.ProviderResourceID, result.Compute}, {intent.StorageID, result.Storage.ProviderResourceID, result.Storage}} {
		observed, e := json.Marshal(resource.value)
		if e != nil {
			return nil, e
		}
		if _, err = tx.ExecContext(ctx, `UPDATE fabric.resources SET provider_resource_ref=$2,observed_specification=$3,observation_result='confirmed',observed_at=$4,updated_at=now() WHERE id=$1`, resource.id, resource.provider, observed, result.ObservedAt); err != nil {
			return nil, persistenceError(err)
		}
	}
	// The provider's data-attachment preparation is preserved in the operation
	// result. Target fabric.attachments requires a real execution resource, which
	// does not exist until Serve creates its runtime; a compute network is not one.
	if _, err = tx.ExecContext(ctx, `UPDATE fabric.resource_sets SET observation_result='confirmed',observed_at=$2,updated_at=now() WHERE id=$1`, current.ResourceId, result.ObservedAt); err != nil {
		return nil, persistenceError(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE fabric.resource_actions SET observation_result='confirmed',authorization_receipt_ref=$2,provider_request_ref=$3,evidence_ref=$4,error_code=NULL,approved_input=approved_input || jsonb_build_object('localNoChargeEvidence',$5::jsonb),updated_at=now() WHERE command_id=$1`, operationID, receipt.Id, result.Attachment.ProviderRequestID, operationID, proof); err != nil {
		return nil, persistenceError(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE fabric.operations SET status='succeeded',stage='actual_resource_evidence',observation_result='confirmed',error_code=NULL,result=$2,completed_at=now(),updated_at=now() WHERE id=$1`, operationID, data); err != nil {
		return nil, persistenceError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, persistenceError(err)
	}
	return s.operation(ctx, operationID)
}

func validateLocalResult(in LocalResourceIntent, r *LocalResourceResult) error {
	if r == nil || r.Binding == nil || r.Binding.ComputeAllocationId != in.ComputeID || r.Binding.StorageVolumeId != in.StorageID || r.Binding.AccountId == "" || r.Binding.DataAttachmentId == "" || r.Binding.DataAttachmentOperationId == "" || r.ObservedAt.IsZero() || r.Compute.ID != in.ComputeID || r.Compute.WorkspaceID != in.WorkspaceID || r.Compute.AccountID != r.Binding.AccountId || r.Compute.Provider != "local-docker" || r.Compute.ProviderResourceID == "" || r.Storage.ID != in.StorageID || r.Storage.WorkspaceID != in.WorkspaceID || r.Storage.AccountID != r.Binding.AccountId || r.Storage.Provider != "local-docker" || r.Storage.ProviderResourceID == "" || r.Attachment.ID != r.Binding.DataAttachmentId || r.Attachment.OperationID != r.Binding.DataAttachmentOperationId || r.Attachment.WorkspaceID != in.WorkspaceID || r.Attachment.ComputeID != in.ComputeID || r.Attachment.VolumeID != in.StorageID || r.Attachment.ProviderAttachmentID == "" {
		return errors.New("local resource readback differs from the original intent")
	}
	return nil
}

// ReadLocalResult reads only a confirmed original operation's provider evidence.
func readLocalResult(ctx context.Context, tx *sql.Tx, setID string) (*LocalResourceResult, error) {
	var data []byte
	if err := tx.QueryRowContext(ctx, `SELECT result FROM fabric.operations WHERE resource_id=$1 AND kind='resource_provision' AND status='succeeded' AND observation_result='confirmed'`, setID).Scan(&data); err != nil {
		return nil, err
	}
	var result LocalResourceResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
