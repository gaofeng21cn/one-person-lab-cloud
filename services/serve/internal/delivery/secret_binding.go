package delivery

// This file owns Serve's half of the approved managed-key handover. Workspace
// coordinates the Gateway key and hands Serve the opaque handover input (key
// binding, fingerprint, Secret delivery reference and publisher slot). Serve alone
// asks Fabric to bind or replace the Secret, validates the provider-confirmed
// readback, completes the frozen command with the binding Fabric returned, and
// lets the delivery step persist that command before the application is ever
// executed. Serve never mints or revokes a Gateway key, and no raw Key value
// enters this process's state.
//
// The replacement path names the exact predecessor Serve itself recorded: the
// confirmed binding of the latest confirmed reload for the runtime, or the launch
// binding frozen in the original start command. A runtime that declares the
// Gateway credential but has no recorded confirmed binding is refused instead of
// issuing a second initial bind.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
)

// ReasonSecretBindingUnconfirmed reports that Fabric did not confirm the exact
// Secret binding this delivery requires. Serve never proceeds to execution with a
// binding another owner did not confirm.
const ReasonSecretBindingUnconfirmed = "secret_binding_unconfirmed"

// declaredGatewayCredential decodes the frozen application revision of one deploy
// command and reports the Gateway credential it declares, when any.
func declaredGatewayCredential(r *api.RuntimeDeployCommand) (contracts.WorkspaceApplicationCredential, bool, error) {
	revision, err := deploymentRevision(r)
	if err != nil {
		return contracts.WorkspaceApplicationCredential{}, false, err
	}
	credential, declared := contracts.WorkspaceApplicationDeclaredCredential(revision, contracts.WorkspaceApplicationCredentialGatewayKey)
	return credential, declared, nil
}

// managedKeyHandoverInput validates the opaque handover Workspace produces for one
// runtime: Gateway's key binding identity, its fingerprint, the approved Secret
// delivery reference and the publisher-declared slot. The confirmed Fabric binding
// identity is never supplied by the caller; Serve obtains it from Fabric alone.
func managedKeyHandoverInput(binding *api.RuntimeManagedKeyBinding, credential contracts.WorkspaceApplicationCredential, workspaceID string) (*api.RuntimeManagedKeyBinding, error) {
	if binding == nil {
		return nil, status.Errorf(codes.FailedPrecondition, "%s: a runtime that declares a Gateway credential requires the Workspace managed-key handover", ReasonManagedKeyUnavailable)
	}
	if strings.TrimSpace(binding.GetSecretBindingId()) != "" || strings.TrimSpace(binding.GetSecretVersion()) != "" {
		return nil, status.Errorf(codes.FailedPrecondition, "%s: the caller may not supply a Fabric-confirmed Secret binding; Serve obtains it from Fabric", ReasonManagedKeyUnavailable)
	}
	if strings.TrimSpace(binding.GetKeyBindingId()) == "" || strings.TrimSpace(binding.GetFingerprint()) == "" ||
		strings.TrimSpace(binding.GetSecretDeliveryReference()) == "" || strings.TrimSpace(binding.GetTargetSlot()) == "" {
		return nil, status.Errorf(codes.FailedPrecondition, "%s: the managed-key handover requires the Gateway key binding, fingerprint, Secret delivery reference and target slot", ReasonManagedKeyUnavailable)
	}
	if binding.GetTargetSlot() != credential.Name || binding.GetSecretDeliveryReference() != contracts.WorkspaceGatewaySecretRef(workspaceID) {
		return nil, status.Errorf(codes.FailedPrecondition, "%s: the managed-key handover does not name this Workspace's Gateway Secret delivery", ReasonManagedKeyUnavailable)
	}
	return binding, nil
}

// secretHandoverCall derives the owner call for one Fabric binding step from the
// original command's own context. The idempotency key names the same step of the
// same original command on every retry, so Fabric replays one durable readback
// instead of replacing a second binding.
func secretHandoverCall(call *api.CallContext, step string) (*api.CallContext, error) {
	out := nextOwnerCall(call)
	if strings.TrimSpace(out.GetIdempotencyKey()) == "" {
		return nil, status.Errorf(codes.InvalidArgument, "%s: the original command identity is required for the Secret handover", ReasonManagedKeyUnavailable)
	}
	out.IdempotencyKey = out.GetIdempotencyKey() + ":" + step
	return out, nil
}

// confirmManagedSecret asks Fabric to bind (predecessor empty) or replace
// (predecessor named) the exact Secret one command requires, validates the
// provider-confirmed readback and completes the command with the confirmed
// binding. A refusal or an unconfirmed readback returns with the command
// untouched: no predecessor is retired, no version is advanced and no binding is
// recorded from a caller claim.
func (s *Service) confirmManagedSecret(ctx context.Context, r *api.RuntimeDeployCommand, input *api.RuntimeManagedKeyBinding, predecessor string) error {
	if s.Resources == nil {
		return status.Error(codes.Unavailable, "Fabric Secret coordination is not configured")
	}
	step := "bind_managed_secret"
	if predecessor != "" {
		step = "rebind_managed_secret"
	}
	call, err := secretHandoverCall(r.GetContext(), step)
	if err != nil {
		return err
	}
	if predecessor == "" {
		readback, err := s.Resources.BindSecret(ctx, &api.SecretBindingCommand{
			Context: call, WorkspaceId: r.GetWorkspaceId(), RuntimeInstanceId: r.GetRuntimeInstanceId(),
			KeyBindingId: input.GetKeyBindingId(), SecretDeliveryReference: input.GetSecretDeliveryReference(),
			TargetSlot: input.GetTargetSlot(), Fingerprint: input.GetFingerprint(),
		})
		if err != nil {
			return err
		}
		if readback.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED || strings.TrimSpace(readback.GetSecretBindingId()) == "" ||
			strings.TrimSpace(readback.GetVersion()) == "" || readback.GetRuntimeInstanceId() != r.GetRuntimeInstanceId() ||
			readback.GetFingerprint() != input.GetFingerprint() {
			return status.Errorf(codes.FailedPrecondition, "%s: Fabric did not confirm the exact Secret binding", ReasonSecretBindingUnconfirmed)
		}
		r.ManagedKeyBinding = &api.RuntimeManagedKeyBinding{
			KeyBindingId: input.GetKeyBindingId(), SecretDeliveryReference: input.GetSecretDeliveryReference(),
			Fingerprint: readback.GetFingerprint(), TargetSlot: input.GetTargetSlot(),
			SecretBindingId: readback.GetSecretBindingId(), SecretVersion: readback.GetVersion(),
		}
		return nil
	}
	readback, err := s.Resources.RebindSecret(ctx, &api.SecretBindingRebindCommand{
		Context: call, WorkspaceId: r.GetWorkspaceId(), RuntimeInstanceId: r.GetRuntimeInstanceId(),
		ExpectedCurrentSecretBindingId: predecessor, KeyBindingId: input.GetKeyBindingId(),
		SecretDeliveryReference: input.GetSecretDeliveryReference(), TargetSlot: input.GetTargetSlot(),
		Fingerprint: input.GetFingerprint(),
	})
	if err != nil {
		return err
	}
	if readback.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED || strings.TrimSpace(readback.GetSecretBindingId()) == "" ||
		strings.TrimSpace(readback.GetVersion()) == "" || readback.GetRuntimeInstanceId() != r.GetRuntimeInstanceId() ||
		readback.GetFingerprint() != input.GetFingerprint() || readback.GetPreviousSecretBindingId() != predecessor {
		return status.Errorf(codes.FailedPrecondition, "%s: Fabric did not confirm the exact Secret replacement", ReasonSecretBindingUnconfirmed)
	}
	r.ManagedKeyBinding = &api.RuntimeManagedKeyBinding{
		KeyBindingId: input.GetKeyBindingId(), SecretDeliveryReference: input.GetSecretDeliveryReference(),
		Fingerprint: readback.GetFingerprint(), TargetSlot: input.GetTargetSlot(),
		SecretBindingId: readback.GetSecretBindingId(), SecretVersion: readback.GetVersion(),
	}
	return nil
}

// confirmLaunchManagedSecret performs Serve's managed-key handover for one launch
// admission. It proves the delivery is admissible before any external effect,
// then lets Fabric confirm the initial binding. A runtime that declares no Gateway
// credential must present no binding and needs no handover.
func (s *Service) confirmLaunchManagedSecret(ctx context.Context, r *api.RuntimeDeployCommand) error {
	credential, declared, err := declaredGatewayCredential(r)
	if err != nil {
		return err
	}
	if !declared {
		if r.GetManagedKeyBinding() != nil {
			return status.Errorf(codes.FailedPrecondition, "%s: the runtime declares no Gateway credential, so no managed key binding applies", ReasonManagedKeyUnavailable)
		}
		return nil
	}
	input, err := managedKeyHandoverInput(r.GetManagedKeyBinding(), credential, r.GetWorkspaceId())
	if err != nil {
		return err
	}
	// The command must match Serve's reserved delivery before Serve causes any
	// externally visible Secret binding. The write path revalidates under its own
	// lock, so a refusal here leaves nothing behind and a concurrent change is
	// still caught before the frozen command is persisted.
	if err := s.validateDeploy(ctx, r); err != nil {
		return err
	}
	return s.confirmManagedSecret(ctx, r, input, "")
}

// resolveReloadManagedSecret performs Serve's managed-key handover for one model
// configuration. A command whose durable intent already recorded a confirmed
// binding reuses that exact generation without asking Fabric again, so a replay
// of an already-recorded reload can never name a different original or retire a
// second binding. A first attempt replaces the exact predecessor Serve recorded;
// a runtime whose confirmed binding Serve cannot read is refused rather than
// bound a second time.
func (s *Service) resolveReloadManagedSecret(ctx context.Context, r, launch *api.RuntimeDeployCommand, handover *api.RuntimeManagedKeyBinding, recorded *recordedHandover) error {
	credential, declared, err := declaredGatewayCredential(r)
	if err != nil {
		return err
	}
	if !declared {
		if handover != nil {
			return status.Errorf(codes.FailedPrecondition, "%s: the runtime declares no Gateway credential, so no managed key binding applies", ReasonManagedKeyUnavailable)
		}
		return nil
	}
	input, err := managedKeyHandoverInput(handover, credential, r.GetWorkspaceId())
	if err != nil {
		return err
	}
	if recorded != nil && recorded.SecretBindingID != "" {
		// The durable intent already carries the credential generation Fabric
		// confirmed for this exact command. Reusing it keeps the retry on the same
		// original; a retry that presents a different handover input is refused
		// rather than reinterpreted.
		if recorded.KeyBindingID != input.GetKeyBindingId() || recorded.Fingerprint != input.GetFingerprint() {
			return status.Errorf(codes.AlreadyExists, "%s: the reload input differs from its original command", ReasonManagedKeyUnavailable)
		}
		r.ManagedKeyBinding = &api.RuntimeManagedKeyBinding{
			KeyBindingId: input.GetKeyBindingId(), SecretDeliveryReference: input.GetSecretDeliveryReference(),
			Fingerprint: input.GetFingerprint(), TargetSlot: input.GetTargetSlot(),
			SecretBindingId: recorded.SecretBindingID, SecretVersion: recorded.SecretVersion,
		}
		return nil
	}
	// The predecessor is the confirmed binding Serve recorded in its own rows; the
	// fallback is the launch binding frozen in the original start command, never the
	// caller's unconfirmed handover input.
	predecessor, err := s.currentManagedSecretBinding(ctx, r.GetRuntimeInstanceId(), r.GetDeploymentId(), launch)
	if err != nil {
		return err
	}
	if strings.TrimSpace(predecessor) == "" {
		return status.Errorf(codes.FailedPrecondition, "%s: the runtime has no recorded confirmed Secret binding to replace", ReasonSecretBindingUnconfirmed)
	}
	return s.confirmManagedSecret(ctx, r, input, predecessor)
}

// recordedHandover is the credential generation a durable reload intent already
// recorded: the exact Gateway key binding, its fingerprint and the Fabric binding
// and version confirmed for the command.
type recordedHandover struct {
	KeyBindingID    string
	Fingerprint     string
	SecretBindingID string
	SecretVersion   string
}

// recordedSecretHandover reads the Secret handover identity one already-recorded
// reload action carries. The operation identity is the one Serve derived for the
// original owner Operation (the same identity reloadModelConfiguration keys its
// durable reload intent by), never a caller-chosen value. It is how a retry of the same logical command reuses the
// binding Fabric confirmed for it: re-deriving a predecessor from current state
// would name a different original on a replay and could retire the wrong binding.
func (s *Service) recordedSecretHandover(ctx context.Context, runtimeID, operationID, targetVersion string) (*recordedHandover, error) {
	actionID := stableID("act_", "reload", runtimeID, operationID, targetVersion)
	var raw []byte
	err := s.DB.QueryRowContext(ctx, `SELECT input_snapshot FROM serve.agent_runtime_actions WHERE command_id=$1`, actionID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, dbError(err)
	}
	snapshot := map[string]string{}
	if json.Unmarshal(raw, &snapshot) != nil {
		return nil, status.Error(codes.DataLoss, "stored runtime reload snapshot is invalid")
	}
	return &recordedHandover{
		KeyBindingID:    strings.TrimSpace(snapshot["keyBinding"]),
		Fingerprint:     strings.TrimSpace(snapshot["fingerprint"]),
		SecretBindingID: strings.TrimSpace(snapshot["secretBindingId"]),
		SecretVersion:   strings.TrimSpace(snapshot["secretVersion"]),
	}, nil
}

// currentManagedSecretBinding resolves the confirmed binding Serve recorded for
// one runtime before a new replacement: the replacement of the latest confirmed
// reload when one exists, otherwise the launch binding frozen in the original
// start command. It reads only Serve's own rows; an absent predecessor is
// reported as such rather than derived from another owner's state.
func (s *Service) currentManagedSecretBinding(ctx context.Context, runtimeID, deploymentID string, launch *api.RuntimeDeployCommand) (string, error) {
	var raw []byte
	err := s.DB.QueryRowContext(ctx, `
		SELECT input_snapshot FROM serve.agent_runtime_actions
		WHERE runtime_instance_id=$1 AND expected_deployment_id=$2 AND action='reload' AND observation_result='confirmed'
		ORDER BY created_at DESC, id DESC LIMIT 1`, runtimeID, deploymentID).Scan(&raw)
	if err == nil {
		snapshot := map[string]string{}
		if json.Unmarshal(raw, &snapshot) != nil {
			return "", status.Error(codes.DataLoss, "stored runtime reload snapshot is invalid")
		}
		return strings.TrimSpace(snapshot["secretBindingId"]), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", dbError(err)
	}
	return strings.TrimSpace(launch.GetManagedKeyBinding().GetSecretBindingId()), nil
}
