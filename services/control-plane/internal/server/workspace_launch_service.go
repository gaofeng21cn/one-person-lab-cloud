package server

import (
	"context"
	"errors"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

var errWorkspaceLaunchStageAdapterUnavailable = errors.New("workspace_launch_stage_adapter_unavailable")

type controlPlaneWorkspaceLaunchStageAdapter struct {
	app           *controlPlaneServer
	service       *controlplane.Service
	keyCredential clients.SessionDelegatedCredential
	keyUserID     int64
}

func (a *controlPlaneWorkspaceLaunchStageAdapter) ReadStage(ctx context.Context, operation workspaceLaunchReconcileOperation) (workspaceLaunchStageObservation, error) {
	if a == nil || a.app == nil || a.service == nil {
		return workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}, errWorkspaceLaunchStageAdapterUnavailable
	}
	switch operation.Stage {
	case contracts.StageKey:
		return a.readWorkspaceLaunchKey(ctx, operation)
	case contracts.StageDebit:
		return a.readWorkspaceLaunchDebit(ctx, operation)
	case contracts.StageCompute, contracts.StageStorage, contracts.StageAttachment, contracts.StageSecret, contracts.StageRuntime:
		return a.readWorkspaceLaunchFabricStage(ctx, operation)
	case contracts.StageActivation:
		return a.readWorkspaceLaunchActivation(ctx, operation)
	case contracts.StageReceipt:
		return a.readWorkspaceLaunchReceipt(ctx, operation)
	default:
		return workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}, errInvalidWorkspaceLaunchOperation
	}
}

func (a *controlPlaneWorkspaceLaunchStageAdapter) CanMutateStage(operation workspaceLaunchReconcileOperation) bool {
	if a == nil || a.app == nil || a.service == nil {
		return false
	}
	return operation.Stage != contracts.StageKey || a.workspaceLaunchKeyMutationCredentialValid(operation)
}

func (a *controlPlaneWorkspaceLaunchStageAdapter) CanReplayStage(operation workspaceLaunchReconcileOperation) bool {
	if a == nil || a.app == nil || a.service == nil {
		return false
	}
	switch operation.Stage {
	case contracts.StageKey:
		return a.workspaceLaunchKeyMutationCredentialValid(operation)
	case contracts.StageCompute, contracts.StageStorage, contracts.StageAttachment, contracts.StageSecret, contracts.StageRuntime, contracts.StageActivation, contracts.StageReceipt:
		return true
	default:
		return false
	}
}

func (a *controlPlaneWorkspaceLaunchStageAdapter) MutateStage(ctx context.Context, operation workspaceLaunchReconcileOperation, idempotencyKey string) error {
	if a == nil || a.app == nil || a.service == nil || idempotencyKey == "" {
		return errWorkspaceLaunchStageAdapterUnavailable
	}
	switch operation.Stage {
	case contracts.StageKey:
		return a.mutateWorkspaceLaunchKey(ctx, operation, idempotencyKey)
	case contracts.StageDebit:
		return a.mutateWorkspaceLaunchDebit(ctx, operation)
	case contracts.StageCompute, contracts.StageStorage, contracts.StageAttachment, contracts.StageSecret, contracts.StageRuntime:
		return a.mutateWorkspaceLaunchFabricStage(ctx, operation, idempotencyKey)
	case contracts.StageActivation:
		return a.mutateWorkspaceLaunchActivation(ctx, operation, idempotencyKey)
	case contracts.StageReceipt:
		return a.mutateWorkspaceLaunchReceipt(ctx, operation, idempotencyKey)
	default:
		return errInvalidWorkspaceLaunchOperation
	}
}

func (app *controlPlaneServer) workspaceLaunchReconciler(service *controlplane.Service, credential clients.SessionDelegatedCredential, userID int64) *WorkspaceLaunchReconciler {
	return NewWorkspaceLaunchReconciler(app.tables, &controlPlaneWorkspaceLaunchStageAdapter{
		app: app, service: service, keyCredential: credential, keyUserID: userID,
	})
}

func (app *controlPlaneServer) runWorkspaceLaunchesOnce(ctx context.Context, service *controlplane.Service) error {
	scheduler := newWorkspaceLaunchScheduler(app, service)
	err := scheduler.dispatch(ctx)
	return errors.Join(err, scheduler.collect(true))
}

func (app *controlPlaneServer) runWorkspaceLaunch(ctx context.Context, service *controlplane.Service, operationID string) error {
	_, err := app.workspaceLaunchReconciler(service, clients.SessionDelegatedCredential{}, 0).Reconcile(ctx, operationID)
	return err
}
