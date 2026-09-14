package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	controlplaneent "opl-cloud/services/control-plane/ent"
	"opl-cloud/services/control-plane/ent/runtimeoperation"
	"opl-cloud/services/control-plane/ent/workspace"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

const (
	workspaceApplicationDeploymentRuntimePhase      = "runtime"
	workspaceApplicationDeploymentPredecessorPhase  = "predecessor_suspending"
	workspaceApplicationDeploymentRetiringPhase     = "retiring"
	workspaceApplicationDeploymentActivatingPhase   = "activating"
	workspaceApplicationDeploymentReceiptPhase      = "receipt"
	workspaceApplicationDeploymentActivePhase       = "active"
	workspaceApplicationDeploymentManualReviewPhase = "manual_review"

	workspaceApplicationDeploymentWorkerInterval = 30 * time.Second
)

var (
	errWorkspaceApplicationActivationConflict = errors.New("workspace_application_activation_conflict")
	errWorkspaceApplicationRevisionGone       = errors.New("workspace_application_revision_not_admitted")
)

type workspaceApplicationActivationMutation struct {
	WorkspaceID     string
	ExpectedBinding string
	ExpectedVersion int64
	NextBinding     string
	NextVersion     int64
	Intent          workspaceApplicationDeploymentIntent
}

// workspaceApplicationDeploymentWorkerEnabled follows the launch worker
// convention: the recovery scan is explicitly enabled per deployment.
func workspaceApplicationDeploymentWorkerEnabled() bool {
	value := strings.TrimSpace(os.Getenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED"))
	return value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes")
}

func (app *controlPlaneServer) startWorkspaceApplicationDeploymentWorker(ctx context.Context, service *controlplane.Service, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			if err := app.runWorkspaceApplicationDeploymentsOnce(ctx, service); err != nil {
				log.Printf("workspace application deployment recovery failed: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (app *controlPlaneServer) runWorkspaceApplicationDeploymentsOnce(ctx context.Context, service *controlplane.Service) error {
	defaultErr := app.runWorkspaceDefaultApplicationsOnce(ctx, service)
	operations, err := queryRuntimeOperations(ctx, app.tables, runtimeOperationQuery{
		Action: workspaceApplicationDeploymentAction, Statuses: []string{"pending", "running"},
	})
	if err != nil {
		return err
	}
	var errs []error
	if defaultErr != nil {
		errs = append(errs, defaultErr)
	}
	for _, row := range operations {
		if err := app.runWorkspaceApplicationDeployment(ctx, service, stringValue(row["id"])); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// runWorkspaceApplicationDeployment advances the deployment intent by one
// phase: runtime creation on Fabric, the atomic binding activation against
// the reserved workspace version, then the deployment receipt. Every step is
// resumable: the durable intent and the workspace CAS own the truth, so a
// lost response never repeats a provider mutation or skips an activation.
func (app *controlPlaneServer) runWorkspaceApplicationDeployment(ctx context.Context, service *controlplane.Service, operationID string) error {
	unlock := app.lockResource("workspace-application-deployment", operationID)
	defer unlock()
	row, found, err := app.tables.GetRuntimeOperation(ctx, operationID)
	if err != nil || !found {
		return err
	}
	intent, err := decodeWorkspaceApplicationDeploymentIntent(row)
	if err != nil {
		return err
	}
	for range 4 {
		switch intent.Phase {
		case workspaceApplicationDeploymentIntentPhase:
			if intent.Version == 2 {
				input, err := app.workspaceApplicationRuntimeInput(ctx, intent)
				if err == nil && intent.DataLayout == "legacy_application" {
					sourceRow, found, readErr := app.tables.GetRuntimeOperation(ctx, strings.TrimSuffix(intent.DataSourceRuntimeOperationID, ":runtime"))
					if readErr != nil {
						err = readErr
					} else if !found {
						err = errWorkspaceApplicationBindingUnknown
					} else {
						source, decodeErr := decodeWorkspaceApplicationDeploymentIntent(sourceRow)
						if decodeErr != nil {
							err = decodeErr
						} else {
							historical, inputErr := app.workspaceApplicationRuntimeInput(ctx, source)
							if inputErr != nil {
								err = inputErr
							} else {
								_, err = service.ReadWorkspaceApplicationRuntime(ctx, historical)
							}
						}
					}
				}
				if err == nil {
					err = service.PreflightWorkspaceApplicationRuntime(ctx, input)
				}
				if err != nil {
					return app.markWorkspaceApplicationManualReview(ctx, row, intent, err.Error())
				}
			}
			intent.Phase = workspaceApplicationDeploymentRuntimePhase
			if intent.Version == 2 && (intent.PreviousDeploymentID != "" || intent.LegacyPredecessor != nil) {
				intent.Phase = workspaceApplicationDeploymentPredecessorPhase
			}
			if err := app.persistWorkspaceApplicationDeployment(ctx, row, intent, "pending"); err != nil {
				return err
			}
		case workspaceApplicationDeploymentPredecessorPhase:
			return app.advanceWorkspaceApplicationPredecessor(ctx, service, row, &intent, "suspended")
		case workspaceApplicationDeploymentRetiringPhase:
			return app.advanceWorkspaceApplicationPredecessor(ctx, service, row, &intent, "absent")
		case workspaceApplicationDeploymentRuntimePhase:
			return app.advanceWorkspaceApplicationRuntime(ctx, service, row, &intent)
		case workspaceApplicationDeploymentActivatingPhase:
			return app.advanceWorkspaceApplicationActivation(ctx, row, &intent)
		case workspaceApplicationDeploymentReceiptPhase:
			return app.advanceWorkspaceApplicationReceipt(ctx, service, row, &intent)
		case workspaceApplicationDeploymentActivePhase, workspaceApplicationDeploymentManualReviewPhase:
			return nil
		default:
			return errors.New("workspace_application_deployment_phase_invalid")
		}
	}
	return nil
}

func (app *controlPlaneServer) persistWorkspaceApplicationDeployment(ctx context.Context, row map[string]any, intent workspaceApplicationDeploymentIntent, status string) error {
	encoded, err := json.Marshal(intent)
	if err != nil {
		return err
	}
	next := cloneMap(row)
	next["result"], next["status"] = string(encoded), status
	if err := app.tables.PersistWorkspaceApplicationOperation(ctx, stringValue(row["result"]), next); err != nil {
		return err
	}
	row["result"], row["status"] = next["result"], next["status"]
	return nil
}

func (app *controlPlaneServer) markWorkspaceApplicationManualReview(ctx context.Context, row map[string]any, intent workspaceApplicationDeploymentIntent, reason string) error {
	if intent.Phase != workspaceApplicationDeploymentManualReviewPhase {
		intent.FailurePhase = intent.Phase
	}
	intent.Phase = workspaceApplicationDeploymentManualReviewPhase
	intent.LastError = reason
	return app.persistWorkspaceApplicationDeployment(ctx, row, intent, "manual_review")
}

func (app *controlPlaneServer) advanceWorkspaceApplicationRuntime(ctx context.Context, service *controlplane.Service, row map[string]any, intent *workspaceApplicationDeploymentIntent) error {
	input, err := app.workspaceApplicationRuntimeInput(ctx, *intent)
	if err != nil {
		return app.markWorkspaceApplicationManualReview(ctx, row, *intent, err.Error())
	}
	current, found, err := app.tables.GetWorkspace(ctx, intent.WorkspaceID)
	if err != nil {
		return err
	}
	if !found || !workspaceApplicationEntitlementOpen(current, time.Now()) || stringValue(current["applicationBinding"]) != intent.CurrentBinding || int64(numberField(current, "applicationBindingVersion", 0)) != intent.ExpectedWorkspaceVersion || intent.Version == 2 && stringValue(current["reservedApplicationDeploymentId"]) != intent.OperationID {
		return app.markWorkspaceApplicationManualReview(ctx, row, *intent, errWorkspaceApplicationActivationConflict.Error())
	}
	revision := input.Revision
	observation, ensureErr := service.EnsureWorkspaceApplicationRuntime(ctx, input, intent.OperationID+":runtime")
	intent.RuntimeObservation = &observation
	if ensureErr != nil {
		var upstream *clients.FabricHTTPError
		if errors.As(ensureErr, &upstream) && (upstream.StatusCode == http.StatusBadRequest || upstream.StatusCode == http.StatusUnauthorized || upstream.StatusCode == http.StatusForbidden) {
			return app.markWorkspaceApplicationManualReview(ctx, row, *intent, ensureErr.Error())
		}
		if strings.Contains(ensureErr.Error(), "component_conflict") || errors.Is(ensureErr, errWorkspaceApplicationRevisionGone) {
			return app.markWorkspaceApplicationManualReview(ctx, row, *intent, ensureErr.Error())
		}
		// Transient provider failure: record it and let the next tick retry.
		intent.LastError = ensureErr.Error()
		if err := app.persistWorkspaceApplicationDeployment(ctx, row, *intent, "running"); err != nil {
			return err
		}
		return ensureErr
	}
	if err := contracts.ValidateWorkspaceApplicationRuntimeObservation(revision, observation); err != nil {
		return app.markWorkspaceApplicationManualReview(ctx, row, *intent, err.Error())
	}
	if observation.WorkspaceID != intent.WorkspaceID ||
		observation.Status == "ready" && observation.RuntimeID == "" || intent.Version == 2 && observation.RuntimeID != contracts.WorkspaceApplicationRuntimeID(input.RuntimeOperationID) {
		return app.markWorkspaceApplicationManualReview(ctx, row, *intent, "workspace_application_runtime_observation_mismatch")
	}
	if observation.Status == "failed" {
		return app.markWorkspaceApplicationManualReview(ctx, row, *intent, "workspace_application_runtime_failed")
	}
	intent.LastError = ""
	if observation.Status != "ready" {
		// Components are still converging on the provider; the claim stays
		// started and the next tick re-ensures.
		return app.persistWorkspaceApplicationDeployment(ctx, row, *intent, "running")
	}
	intent.Phase = workspaceApplicationDeploymentActivatingPhase
	return app.persistWorkspaceApplicationDeployment(ctx, row, *intent, "running")
}

func (app *controlPlaneServer) advanceWorkspaceApplicationActivation(ctx context.Context, row map[string]any, intent *workspaceApplicationDeploymentIntent) error {
	// The reserved binding and version were captured at intent creation; the
	// activation commits the deployed revision reference against exactly that
	// reservation or fails into manual review.
	next := *intent
	next.Phase = workspaceApplicationDeploymentReceiptPhase
	if intent.Version == 2 && (intent.PreviousDeploymentID != "" || intent.LegacyPredecessor != nil) {
		next.Phase = workspaceApplicationDeploymentRetiringPhase
	}
	next.ActivationAt = time.Now().UTC().Format(time.RFC3339Nano)
	next.LastError = ""
	mutation := workspaceApplicationActivationMutation{
		WorkspaceID:     intent.WorkspaceID,
		ExpectedBinding: intent.CurrentBinding,
		ExpectedVersion: intent.ExpectedWorkspaceVersion,
		NextBinding:     intent.ApplicationID + "@" + intent.TargetRevision,
		NextVersion:     intent.ExpectedWorkspaceVersion + 1,
		Intent:          next,
	}
	if err := app.tables.ApplyWorkspaceApplicationActivation(ctx, mutation); err != nil {
		if errors.Is(err, errWorkspaceApplicationActivationConflict) {
			// The reserved version no longer matches: another operation moved
			// the binding. Human decision, never an automatic overwrite.
			return app.markWorkspaceApplicationManualReview(ctx, row, *intent, err.Error())
		}
		return err
	}
	return nil
}

func workspaceApplicationResourcesMatch(workspace map[string]any, intent workspaceApplicationDeploymentIntent) bool {
	return stringValue(workspace["id"]) == intent.WorkspaceID &&
		firstNonEmpty(stringValue(workspace["accountId"]), stringValue(workspace["ownerAccountId"])) == intent.AccountID &&
		firstNonEmpty(stringValue(workspace["state"]), stringValue(workspace["status"])) == "running" &&
		firstNonEmpty(stringValue(workspace["currentComputeAllocationId"]), stringValue(workspace["computeAllocationId"])) == intent.ComputeID &&
		stringValue(workspace["storageId"]) == intent.StorageID &&
		firstNonEmpty(stringValue(workspace["currentAttachmentId"]), stringValue(workspace["attachmentId"])) == intent.AttachmentID
}

func (app *controlPlaneServer) advanceWorkspaceApplicationReceipt(ctx context.Context, service *controlplane.Service, row map[string]any, intent *workspaceApplicationDeploymentIntent) error {
	binding := intent.ApplicationID + "@" + intent.TargetRevision
	receipt, err := service.RecordMonthlyReceipt(ctx, clients.ReceiptInput{
		Type: "workspace.application_deployed.v1", Status: "completed", Surface: "control_plane",
		AccountID: intent.AccountID, WorkspaceID: intent.WorkspaceID, RequestID: intent.OperationID,
		Execution: map[string]any{"operationId": intent.OperationID, "applicationId": intent.ApplicationID,
			"targetRevision": intent.TargetRevision, "runtimeId": applicationRuntimeIDFromObservation(intent),
			"binding": binding, "configurationDigest": intent.ConfigurationDigest},
		Owner: map[string]any{"accountId": intent.AccountID, "workspaceId": intent.WorkspaceID},
	}, intent.OperationID+":deployment-receipt")
	if err != nil {
		// Receipt failures only retry the evidence write; the activation stays.
		return err
	}
	intent.ReceiptID = receipt.ReceiptID
	intent.Phase = workspaceApplicationDeploymentActivePhase
	return app.persistWorkspaceApplicationDeployment(ctx, row, *intent, "succeeded")
}

func applicationRuntimeIDFromObservation(intent *workspaceApplicationDeploymentIntent) string {
	if intent.RuntimeObservation != nil {
		return intent.RuntimeObservation.RuntimeID
	}
	return ""
}

// ApplyWorkspaceApplicationActivation commits the binding and the receipt-phase
// operation together. A lost commit response resumes evidence recording, while
// a failed phase write rolls the binding back in the same transaction.
func (s *postgresEntStateStore) ApplyWorkspaceApplicationActivation(ctx context.Context, mutation workspaceApplicationActivationMutation) error {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	entity, err := tx.Workspace.Query().Where(workspace.IDEQ(mutation.WorkspaceID), lockRowForUpdate).Only(ctx)
	if controlplaneent.IsNotFound(err) {
		return errWorkspaceApplicationWorkspaceGone
	}
	if err != nil {
		return err
	}
	operation, err := tx.RuntimeOperation.Query().Where(runtimeoperation.IDEQ(mutation.Intent.OperationID), lockRowForUpdate).Only(ctx)
	if err != nil {
		return err
	}
	current, err := decodeWorkspaceApplicationDeploymentIntent(recordFromEnt(operation, runtimeOpEntFields))
	if err != nil || current.RequestHash != mutation.Intent.RequestHash {
		return errWorkspaceApplicationActivationConflict
	}
	if current.Phase == workspaceApplicationDeploymentReceiptPhase || current.Phase == workspaceApplicationDeploymentRetiringPhase || current.Phase == workspaceApplicationDeploymentActivePhase {
		return nil
	}
	if current.Phase != workspaceApplicationDeploymentActivatingPhase || !workspaceApplicationResourcesMatch(recordFromEnt(entity, workspaceEntFields), mutation.Intent) {
		return errWorkspaceApplicationActivationConflict
	}
	if entity.ApplicationBinding != mutation.ExpectedBinding || entity.ApplicationBindingVersion != mutation.ExpectedVersion || !workspaceApplicationEntitlementOpen(recordFromEnt(entity, workspaceEntFields), time.Now()) || mutation.Intent.Version == 2 && entity.ReservedApplicationDeploymentID != mutation.Intent.OperationID {
		return errWorkspaceApplicationActivationConflict
	}
	if err := tx.Workspace.UpdateOneID(mutation.WorkspaceID).
		SetApplicationBinding(mutation.NextBinding).
		SetApplicationBindingVersion(mutation.NextVersion).
		SetCurrentApplicationDeploymentID(mutation.Intent.OperationID).
		Exec(ctx); err != nil {
		return err
	}
	if mutation.Intent.WorkspaceAPIKeyID > 0 {
		if err := tx.Workspace.UpdateOneID(mutation.WorkspaceID).SetWorkspaceAPIKeyID(mutation.Intent.WorkspaceAPIKeyID).Exec(ctx); err != nil {
			return err
		}
	}
	encoded, err := json.Marshal(mutation.Intent)
	if err != nil {
		return err
	}
	if err := tx.RuntimeOperation.UpdateOneID(operation.ID).SetResult(string(encoded)).SetStatus("running").Exec(ctx); err != nil {
		return err
	}
	return tx.Commit()
}

func (app *controlPlaneServer) advanceWorkspaceApplicationPredecessor(ctx context.Context, service *controlplane.Service, row map[string]any, intent *workspaceApplicationDeploymentIntent, desired string) error {
	current, found, err := app.tables.GetWorkspace(ctx, intent.WorkspaceID)
	if err != nil {
		return err
	}
	if !found || !workspaceApplicationEntitlementOpen(current, time.Now()) || stringValue(current["reservedApplicationDeploymentId"]) != intent.OperationID {
		return app.markWorkspaceApplicationManualReview(ctx, row, *intent, errWorkspaceApplicationActivationConflict.Error())
	}
	input := contracts.WorkspaceApplicationRuntimeLifecycleInput{AccountID: intent.AccountID, WorkspaceID: intent.WorkspaceID, DesiredState: desired, IdempotencyKey: intent.OperationID + ":predecessor:" + desired}
	if intent.LegacyPredecessor != nil {
		input.RuntimeID, input.RuntimeOperationID, input.LegacyRuntime = intent.LegacyPredecessor.RuntimeID, intent.LegacyPredecessor.RuntimeOperationID, true
	} else {
		priorRow, found, err := app.tables.GetRuntimeOperation(ctx, intent.PreviousDeploymentID)
		if err != nil {
			return err
		}
		if !found {
			return errWorkspaceApplicationBindingUnknown
		}
		prior, err := decodeWorkspaceApplicationDeploymentIntent(priorRow)
		if err != nil || !workspaceApplicationOwnedResourcesMatch(current, prior) || prior.RuntimeObservation == nil {
			return errWorkspaceApplicationBindingUnknown
		}
		input.RuntimeID, input.RuntimeOperationID = prior.RuntimeObservation.RuntimeID, prior.OperationID+":runtime"
		if prior.Version == 1 {
			historical, err := app.workspaceApplicationRuntimeInput(ctx, prior)
			if err != nil {
				return err
			}
			observation, err := service.ReadWorkspaceApplicationRuntime(ctx, historical)
			if err != nil {
				return err
			}
			input.HistoricalApplicationRuntime = true
			input.RuntimeID = contracts.WorkspaceApplicationHistoricalRuntimeID(prior.WorkspaceID)
			if observation.RuntimeID != input.RuntimeID || observation.WorkspaceID != prior.WorkspaceID {
				return errWorkspaceApplicationBindingUnknown
			}
		}
	}
	result, err := service.SetWorkspaceApplicationRuntimeLifecycle(ctx, input, input.IdempotencyKey)
	intent.PredecessorObservation = &result
	if err != nil {
		intent.LastError = err.Error()
		_ = app.persistWorkspaceApplicationDeployment(ctx, row, *intent, "running")
		return err
	}
	if result.RuntimeID != input.RuntimeID || result.WorkspaceID != input.WorkspaceID {
		return errors.New("workspace_application_predecessor_identity_mismatch")
	}
	if result.State != desired {
		return app.persistWorkspaceApplicationDeployment(ctx, row, *intent, "running")
	}
	intent.LastError = ""
	if desired == "suspended" {
		intent.Phase = workspaceApplicationDeploymentRuntimePhase
	} else {
		intent.Phase = workspaceApplicationDeploymentReceiptPhase
	}
	return app.persistWorkspaceApplicationDeployment(ctx, row, *intent, "running")
}
