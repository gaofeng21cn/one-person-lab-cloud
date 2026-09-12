package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"strings"
	"time"

	controlplaneent "opl-cloud/services/control-plane/ent"
	"opl-cloud/services/control-plane/ent/workspace"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

const (
	workspaceApplicationDeploymentRuntimePhase    = "runtime"
	workspaceApplicationDeploymentActivatingPhase = "activating"
	workspaceApplicationDeploymentReceiptPhase    = "receipt"
	workspaceApplicationDeploymentActivePhase     = "active"

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
	operations, err := queryRuntimeOperations(ctx, app.tables, runtimeOperationQuery{
		Action: workspaceApplicationDeploymentAction, Statuses: []string{"pending"}, Limit: 50,
	})
	if err != nil {
		return err
	}
	var errs []error
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
			intent.Phase = workspaceApplicationDeploymentRuntimePhase
			if err := app.persistWorkspaceApplicationDeployment(ctx, row, intent, "pending"); err != nil {
				return err
			}
		case workspaceApplicationDeploymentRuntimePhase:
			return app.advanceWorkspaceApplicationRuntime(ctx, service, row, &intent)
		case workspaceApplicationDeploymentActivatingPhase:
			return app.advanceWorkspaceApplicationActivation(ctx, row, &intent)
		case workspaceApplicationDeploymentReceiptPhase:
			return app.advanceWorkspaceApplicationReceipt(ctx, service, row, &intent)
		case workspaceApplicationDeploymentActivePhase:
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
	row["result"] = string(encoded)
	row["status"] = status
	return app.tables.SaveRuntimeOperation(ctx, row)
}

func (app *controlPlaneServer) markWorkspaceApplicationManualReview(ctx context.Context, row map[string]any, intent workspaceApplicationDeploymentIntent, reason string) error {
	intent.LastError = reason
	encoded, err := json.Marshal(intent)
	if err != nil {
		return err
	}
	row["result"] = string(encoded)
	row["status"] = "manual_review"
	return app.tables.SaveRuntimeOperation(ctx, row)
}

func (app *controlPlaneServer) advanceWorkspaceApplicationRuntime(ctx context.Context, service *controlplane.Service, row map[string]any, intent *workspaceApplicationDeploymentIntent) error {
	revisionRow, admitted, err := app.tables.AdmittedApplicationRevision(ctx, intent.ApplicationID, intent.TargetRevision)
	if err != nil {
		return err
	}
	if !admitted {
		return app.markWorkspaceApplicationManualReview(ctx, row, *intent, errWorkspaceApplicationRevisionGone.Error())
	}
	revision, ok := decodeApplicationRevisionPayload(stringValue(revisionRow["payload"]))
	if !ok {
		return app.markWorkspaceApplicationManualReview(ctx, row, *intent, errApplicationRevisionPayloadInvalid.Error())
	}
	observation, ensureErr := service.EnsureWorkspaceApplicationRuntime(ctx, clients.WorkspaceApplicationRuntimeInput{
		WorkspaceID: intent.WorkspaceID, ComputeID: intent.ComputeID, VolumeID: intent.StorageID,
		AttachmentID: intent.AttachmentID, AttachmentOperationID: intent.OperationID + ":attachment",
		RuntimeOperationID: intent.OperationID + ":runtime",
		Revision:           revision, ConfigurationDigest: intent.ConfigurationDigest,
	}, intent.OperationID+":runtime")
	intent.RuntimeObservation = &observation
	if ensureErr != nil {
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
	if observation.Status != "ready" {
		// Components are still converging on the provider; the claim stays
		// started and the next tick re-ensures.
		return app.persistWorkspaceApplicationDeployment(ctx, row, *intent, "running")
	}
	intent.Phase = workspaceApplicationDeploymentActivatingPhase
	intent.LastError = ""
	return app.persistWorkspaceApplicationDeployment(ctx, row, *intent, "running")
}

func (app *controlPlaneServer) advanceWorkspaceApplicationActivation(ctx context.Context, row map[string]any, intent *workspaceApplicationDeploymentIntent) error {
	// The reserved binding and version were captured at intent creation; the
	// activation commits the deployed revision reference against exactly that
	// reservation or fails into manual review.
	mutation := workspaceApplicationActivationMutation{
		WorkspaceID:     intent.WorkspaceID,
		ExpectedBinding: intent.CurrentBinding,
		ExpectedVersion: intent.ExpectedWorkspaceVersion,
		NextBinding:     intent.ApplicationID + "@" + intent.TargetRevision,
		NextVersion:     intent.ExpectedWorkspaceVersion + 1,
	}
	if err := app.tables.ApplyWorkspaceApplicationActivation(ctx, mutation); err != nil {
		if errors.Is(err, errWorkspaceApplicationActivationConflict) {
			// The reserved version no longer matches: another operation moved
			// the binding. Human decision, never an automatic overwrite.
			return app.markWorkspaceApplicationManualReview(ctx, row, *intent, err.Error())
		}
		return err
	}
	intent.Phase = workspaceApplicationDeploymentReceiptPhase
	intent.ActivationAt = time.Now().UTC().Format(time.RFC3339Nano)
	intent.LastError = ""
	return app.persistWorkspaceApplicationDeployment(ctx, row, *intent, "running")
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

// ApplyWorkspaceApplicationActivation commits the deployed revision reference
// against the reserved binding version, or fails with a conflict when the
// workspace binding moved after the intent was created.
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
	if entity.ApplicationBinding != mutation.ExpectedBinding || entity.ApplicationBindingVersion != mutation.ExpectedVersion {
		return errWorkspaceApplicationActivationConflict
	}
	if err := tx.Workspace.UpdateOneID(mutation.WorkspaceID).
		SetApplicationBinding(mutation.NextBinding).
		SetApplicationBindingVersion(mutation.NextVersion).
		Exec(ctx); err != nil {
		return err
	}
	return tx.Commit()
}
