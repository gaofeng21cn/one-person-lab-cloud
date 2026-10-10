package coordination

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

// deleteResourcesAuthorizationAction is the typed CloudIdentity action Fabric
// requires before it releases an accepted resource set. The contract admits one
// Fabric mutation action for a Workspace's accepted resources —
// PROVISIONACCEPTEDRESOURCES — and has no dedicated release action yet, so the
// release runs under the same accepted-resource-set authority that admitted the
// set. It is deliberately not OBSERVERESOURCES: a read permission is not a write
// permission. When AUTHORIZATION_ACTION_ENUM_DELETEACCEPTEDRESOURCES lands in
// packages/contracts/proto/internal.proto and is admitted for the Fabric
// audience in services/gateway-integration/identity/authorization.go, this
// constant is the one line that changes.
const deleteResourcesAuthorizationAction = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONACCEPTEDRESOURCES

// DeleteResources releases exactly one Workspace's own accepted resource set.
// The command names a resource set and a Workspace; Fabric binds both to its own
// persisted set before anything else, so a set belonging to another Workspace or
// tenant is refused instead of being resolved to another resource.
//
// The release is per kind and in dependency order: the Workspace runtime and its
// owned Secret binding, then the mount binding, then the prepaid volume, then the
// compute allocation. Every kind is confirmed from its owning surface's readback,
// and a pending or unknown handle is reported as pending or unknown — never as
// absent. One delete operation is recorded per resource set, so a repeated call
// (with the same or a new idempotency key) resumes the same operation and never
// dispatches a second provider mutation.
func (s *Service) DeleteResources(ctx context.Context, r *api.MutateResourcesCommand) (*api.Operation, error) {
	if err := peer(ctx, owneridentity.Workspace.Service()); err != nil {
		return nil, err
	}
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	if strings.TrimSpace(r.GetWorkspaceId()) == "" || strings.TrimSpace(r.GetResourceSetId()) == "" || strings.TrimSpace(r.GetContext().GetIdempotencyKey()) == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace, resource set and idempotency key are required")
	}
	tenant := r.GetContext().GetScope().GetTenant().GetTenantId()
	var storedTenant, storedWorkspace string
	var storedVersion time.Time
	if err := s.DB.QueryRowContext(ctx, `SELECT tenant_id,workspace_id,updated_at FROM fabric.resource_sets WHERE id=$1`, r.ResourceSetId).Scan(&storedTenant, &storedWorkspace, &storedVersion); err != nil {
		return nil, persistenceError(err)
	}
	if storedWorkspace != r.WorkspaceId || storedTenant != tenant {
		return nil, status.Error(codes.PermissionDenied, "resource set belongs to another workspace or tenant")
	}
	if expected := strings.TrimSpace(r.GetExpectedResourceVersion()); expected != "" && expected != strconv.FormatInt(storedVersion.UnixNano(), 10) {
		return nil, status.Error(codes.FailedPrecondition, "resource set changed since it was read")
	}
	if err := s.authorizeWorkspace(ctx, r.GetContext(), storedWorkspace, storedTenant, deleteResourcesAuthorizationAction); err != nil {
		return nil, err
	}
	command := proto.Clone(r).(*api.MutateResourcesCommand)
	command.Context = nil
	// The Instance authorization reference is later evidence about this intent,
	// not its identity: exactly like EnsureResources, it never changes which
	// resource set is released and cannot by itself prove that a provider
	// mutation was authorized.
	command.InstanceAuthorizationReference = ""
	body, err := protojson.Marshal(command)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "resource command cannot be encoded")
	}
	normalized, err := (proto.MarshalOptions{Deterministic: true}).Marshal(command)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "resource command cannot be encoded")
	}
	input := ownerstore.IdempotencyInput{ID: "idem_" + uuid.NewString(), TenantScope: tenant, ActorScope: r.Context.GetActorId(), OperationName: "DeleteResources", IdempotencyKey: r.Context.GetIdempotencyKey(), RequestSHA256: ownerstore.HashRequestBody(normalized), ResponseStatus: 202}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, persistenceError(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "fabric:workspace:"+r.WorkspaceId); err != nil {
		return nil, persistenceError(err)
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "fabric:idem:"+tenant+":"+input.ActorScope+":"+input.IdempotencyKey); err != nil {
		return nil, persistenceError(err)
	}
	// Waiting for a competing command must not preserve an authorization that was
	// revoked or expired while queued. Re-check at the actual write boundary.
	if err = s.authorizeWorkspace(ctx, r.GetContext(), storedWorkspace, storedTenant, deleteResourcesAuthorizationAction); err != nil {
		return nil, err
	}
	record, found, err := s.Store.LookupIdempotency(ctx, tx, input)
	if errors.Is(err, ownerstore.ErrIdempotencyConflict) {
		return nil, status.Error(codes.AlreadyExists, "idempotency key has a different resource command")
	}
	if err != nil {
		return nil, persistenceError(err)
	}
	if found {
		if record.ResourceID != r.ResourceSetId {
			return nil, status.Error(codes.AlreadyExists, "idempotency key belongs to another resource set")
		}
		if err = tx.Commit(); err != nil {
			return nil, persistenceError(err)
		}
		return s.resumeDelete(ctx, r, record.OperationID)
	}
	var operationID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM fabric.operations WHERE resource_id=$1 AND kind='resource_delete'`, r.ResourceSetId).Scan(&operationID)
	if err == nil {
		// One release per resource set: a new idempotency key resumes the same
		// operation instead of minting a second one.
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, persistenceError(err)
	} else {
		operationID = "op_" + uuid.NewString()
		if _, err = s.Store.CreateOperation(ctx, tx, ownerstore.OperationInput{ID: operationID, TenantID: tenant, ActorID: r.Context.ActorId, Kind: "resource_delete", ResourceID: r.ResourceSetId, Stage: "runtime_deletion", RequestID: r.Context.RequestId, AcceptedInput: body}); err != nil {
			return nil, persistenceError(err)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE fabric.operations SET status='running',updated_at=now() WHERE id=$1`, operationID); err != nil {
			return nil, persistenceError(err)
		}
	}
	input.ResourceID, input.OperationID, input.ResponseBody = r.ResourceSetId, operationID, []byte(`{}`)
	if err = s.Store.RecordIdempotency(ctx, tx, input); err != nil {
		return nil, persistenceError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, persistenceError(err)
	}
	return s.resumeDelete(ctx, r, operationID)
}

// recordedDeletion is one kind's recorded release evidence.
type recordedDeletion struct {
	confirmed      bool
	resourceID     string
	state          string
	evidenceRef    string
	providerStatus string
	observedAt     time.Time
}

// resumeDelete advances one recorded release operation. Every kind whose owning
// surface has not confirmed absence is released through the dispatcher, which
// replays its own durable dispatch claim, and the release stops at the first kind
// that is not confirmed gone. The same call then re-reads the recorded per-kind
// evidence, so a retry resumes from state instead of re-dispatching.
func (s *Service) resumeDelete(ctx context.Context, r *api.MutateResourcesCommand, operationID string) (*api.Operation, error) {
	current, err := s.operation(ctx, operationID)
	if err != nil {
		return nil, err
	}
	if current.GetStatus() == api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || s.Dispatcher == nil {
		return current, nil
	}
	setID := current.GetResourceId()
	tenant := r.GetContext().GetScope().GetTenant().GetTenantId()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, persistenceError(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "fabric:workspace:"+r.WorkspaceId); err != nil {
		return nil, persistenceError(err)
	}
	var operationStatus, workspace, storedTenant, provider string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM fabric.operations WHERE id=$1 FOR UPDATE`, operationID).Scan(&operationStatus); err != nil {
		return nil, persistenceError(err)
	}
	if operationStatus == "succeeded" {
		if err = tx.Commit(); err != nil {
			return nil, persistenceError(err)
		}
		return s.operation(ctx, operationID)
	}
	if err = tx.QueryRowContext(ctx, `SELECT workspace_id,tenant_id,provider FROM fabric.resource_sets WHERE id=$1`, setID).Scan(&workspace, &storedTenant, &provider); err != nil {
		return nil, persistenceError(err)
	}
	if workspace != r.WorkspaceId || tenant == "" || tenant != storedTenant {
		return nil, status.Error(codes.PermissionDenied, "resource set belongs to another workspace or tenant")
	}
	if provider != s.Dispatcher.Provider() {
		// This process's single provider adapter does not own this set; the
		// operation stays recorded and a deployment with the owning adapter
		// resumes it.
		return current, nil
	}
	if err = s.authorizeWorkspace(ctx, r.GetContext(), workspace, tenant, deleteResourcesAuthorizationAction); err != nil {
		return nil, err
	}
	var computeID, storageID string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM fabric.resources WHERE resource_set_id=$1 AND kind='compute'`, setID).Scan(&computeID); err != nil {
		return nil, persistenceError(err)
	}
	if err = tx.QueryRowContext(ctx, `SELECT id FROM fabric.resources WHERE resource_set_id=$1 AND kind='storage'`, setID).Scan(&storageID); err != nil {
		return nil, persistenceError(err)
	}
	// The provider-confirmed mount identity belongs to the original,
	// provider-confirmed execution evidence; a set that never confirmed one has no
	// attachment identity to release, and it is reported unknown rather than absent.
	var accountID, attachmentID string
	result, resultErr := readResourceResult(ctx, tx, setID)
	if resultErr == nil {
		accountID, attachmentID = result.Binding.GetAccountId(), result.Binding.GetDataAttachmentId()
	} else if !errors.Is(resultErr, sql.ErrNoRows) {
		return nil, persistenceError(resultErr)
	}
	recorded, err := readRecordedDeletions(ctx, tx, setID)
	if err != nil {
		return nil, err
	}
	intent := ResourceDeletionIntent{TenantID: tenant, WorkspaceID: workspace, OperationID: operationID, ResourceSetID: setID, AccountID: accountID, ComputeID: computeID, StorageID: storageID, AttachmentID: attachmentID}
	awaitingStage := ""
	for _, kind := range deleteResourceKinds() {
		if recorded[kind].confirmed {
			continue
		}
		fact, dispatchErr := s.Dispatcher.DeleteResource(ctx, intent, kind)
		fact.Kind = kind
		if dispatchErr != nil {
			// A pending or unreadable provider stays unknown, never absent, and is
			// retried against the same original intent. An errored attempt proves no
			// readback, so it cannot keep an evidence reference or claim absence.
			log.Printf("fabric resource deletion unavailable operation=%s kind=%s: %v", operationID, kind, dispatchErr)
			fact.State, fact.EvidenceRef = DeletionStateUnknown, ""
			if strings.TrimSpace(fact.ProviderStatus) == "" {
				fact.ProviderStatus = "readback_unavailable"
			}
		}
		if err = recordDeletionAttempt(ctx, tx, setID, operationID, kind, fact); err != nil {
			return nil, err
		}
		recorded[kind] = recordedDeletion{confirmed: fact.State == DeletionStateAbsent, state: fact.State, evidenceRef: fact.EvidenceRef, providerStatus: fact.ProviderStatus, observedAt: fact.ObservedAt}
		if fact.State != DeletionStateAbsent {
			awaitingStage = deletionStage(kind)
			break
		}
	}
	if awaitingStage != "" {
		if _, err = tx.ExecContext(ctx, `UPDATE fabric.operations SET status='awaiting_confirmation',stage=$2,error_code='DEPENDENCY_UNAVAILABLE',observation_result='unknown',updated_at=now() WHERE id=$1`, operationID, awaitingStage); err != nil {
			return nil, persistenceError(err)
		}
		if err = recordDeletionObservationTime(ctx, tx, setID, recorded); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, persistenceError(err)
		}
		return s.operation(ctx, operationID)
	}
	// Every kind is gone: the resource rows and the set's Secret bindings carry the
	// owning readback that proved it, and the set is released. A persisted row's
	// provider scope belongs to exactly one owning surface — the set's provider
	// network lives inside the compute allocation, and any Workspace execution
	// resource lives inside the Workspace runtime — so each row is stamped with
	// that owner's readback instead of a separate assumption.
	for _, release := range []struct{ kind, evidenceKind string }{
		{DeletionKindStorage, DeletionKindStorage},
		{DeletionKindCompute, DeletionKindCompute},
		{"network", DeletionKindCompute},
		{"execution", DeletionKindRuntime},
	} {
		if recorded[release.evidenceKind].evidenceRef == "" {
			continue
		}
		if _, err = tx.ExecContext(ctx, `UPDATE fabric.resources SET deletion_evidence_ref=$3,deleted_at=now(),observed_at=COALESCE($4,now()),updated_at=now() WHERE resource_set_id=$1 AND kind=$2 AND deleted_at IS NULL`, setID, release.kind, recorded[release.evidenceKind].evidenceRef, nullableTime(recorded[release.evidenceKind].observedAt)); err != nil {
			return nil, persistenceError(err)
		}
	}
	var unreleased int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM fabric.resources WHERE resource_set_id=$1 AND deleted_at IS NULL`, setID).Scan(&unreleased); err != nil {
		return nil, persistenceError(err)
	}
	if unreleased != 0 {
		// A persisted row without an owning deletion readback cannot be released;
		// the set stays unconfirmed rather than reporting absence it cannot prove.
		return nil, status.Error(codes.Internal, "resource row has no owning deletion readback")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE fabric.secret_bindings SET revoked_at=now(),evidence_ref=$2,updated_at=now() WHERE resource_set_id=$1 AND revoked_at IS NULL`, setID, recorded[DeletionKindSecret].evidenceRef); err != nil {
		return nil, persistenceError(err)
	}
	if err = recordDeletionObservationTime(ctx, tx, setID, recorded); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE fabric.operations SET status='succeeded',stage='absence_verification',error_code=NULL,observation_result='confirmed',completed_at=now(),updated_at=now() WHERE id=$1`, operationID); err != nil {
		return nil, persistenceError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, persistenceError(err)
	}
	return s.operation(ctx, operationID)
}

// readRecordedDeletions reads the per-kind evidence one release operation already
// recorded, so a retry resumes from state instead of re-dispatching.
func readRecordedDeletions(ctx context.Context, tx *sql.Tx, setID string) (map[string]recordedDeletion, error) {
	recorded := map[string]recordedDeletion{}
	rows, err := tx.QueryContext(ctx, `SELECT COALESCE(approved_input->>'deletionKind',''),COALESCE(approved_input->>'resourceId',''),observation_result,COALESCE(error_code,''),COALESCE(evidence_ref,'') FROM fabric.resource_actions WHERE resource_set_id=$1 AND approved_input ? 'deletionKind'`, setID)
	if err != nil {
		return nil, persistenceError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind, resourceID, observation, errorCode, evidence string
		if err = rows.Scan(&kind, &resourceID, &observation, &errorCode, &evidence); err != nil {
			return nil, persistenceError(err)
		}
		if kind == "" {
			continue
		}
		value := recordedDeletion{confirmed: observation == "confirmed", resourceID: resourceID, evidenceRef: evidence}
		switch {
		case value.confirmed:
			value.state = DeletionStateAbsent
		case errorCode == DeletionStatePending:
			value.state = DeletionStatePending
		default:
			value.state = DeletionStateUnknown
		}
		recorded[kind] = value
	}
	if err = rows.Err(); err != nil {
		return nil, persistenceError(err)
	}
	return recorded, nil
}

// recordDeletionAttempt writes one kind's release evidence. The row is keyed by
// the operation and the kind, so the same attempt is updated instead of appended
// and a second release for the same kind cannot be recorded twice.
func recordDeletionAttempt(ctx context.Context, tx *sql.Tx, setID, operationID, kind string, fact ResourceDeletionFact) error {
	approved, err := json.Marshal(struct {
		DeletionKind  string `json:"deletionKind"`
		ResourceSetID string `json:"resourceSetId"`
		ResourceID    string `json:"resourceId,omitempty"`
	}{
		DeletionKind: kind, ResourceSetID: setID, ResourceID: fact.ResourceID,
	})
	if err != nil {
		return persistenceError(err)
	}
	// observation_result only admits confirmed/rejected/unknown, so an unfinished
	// deletion is recorded as unknown with its own absent/pending/unknown state in
	// error_code. The readback reads that state back and never turns it into
	// absence.
	observation, errorCode := "unknown", fact.State
	if fact.State == DeletionStateAbsent {
		observation, errorCode = "confirmed", ""
	}
	var resourceID any
	switch kind {
	case DeletionKindCompute:
		resourceID = fact.ResourceID
	case DeletionKindStorage:
		resourceID = fact.ResourceID
	}
	providerStatus := firstNonBlank(fact.DestroyState, fact.ProviderStatus)
	if _, err = tx.ExecContext(ctx, `INSERT INTO fabric.resource_actions (id,resource_set_id,resource_id,command_id,action,provider_idempotency_key,approved_input,provider_request_ref,observation_result,evidence_ref,error_code)
		VALUES ($1,$2,$3,$4,'delete',$5,$6,$7,$8,$9,$10)
		ON CONFLICT (id) DO UPDATE SET provider_request_ref=EXCLUDED.provider_request_ref,observation_result=EXCLUDED.observation_result,evidence_ref=EXCLUDED.evidence_ref,error_code=EXCLUDED.error_code,updated_at=now()`,
		"raction_"+shortDigest("resource_delete", setID, kind), setID, resourceID, operationID+":"+kind, "delete:"+setID+":"+kind, approved, providerStatus, observation, nullString(fact.EvidenceRef), nullString(errorCode)); err != nil {
		return persistenceError(err)
	}
	return nil
}

// recordDeletionObservationTime stamps the set with the freshest owning-surface
// observation of this attempt, so a readback shows when the release fact was last
// read instead of when the row was written.
func recordDeletionObservationTime(ctx context.Context, tx *sql.Tx, setID string, recorded map[string]recordedDeletion) error {
	latest := time.Time{}
	for _, kind := range deleteResourceKinds() {
		if observed := recorded[kind].observedAt; observed.After(latest) {
			latest = observed
		}
	}
	if latest.IsZero() {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE fabric.resource_sets SET observed_at=$2 WHERE id=$1`, setID, latest); err != nil {
		return persistenceError(err)
	}
	return nil
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
