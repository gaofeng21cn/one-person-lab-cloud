package server

import (
	"context"
	"errors"
	"testing"
)

type unavailableWorkspaceKeyRotationStore struct {
	controlPlaneTableStore
	operationID string
}

func (s unavailableWorkspaceKeyRotationStore) GetRuntimeOperation(ctx context.Context, id string) (map[string]any, bool, error) {
	if id == s.operationID {
		return nil, false, errors.New("operation read unavailable")
	}
	return s.controlPlaneTableStore.GetRuntimeOperation(ctx, id)
}

func assertWorkspaceKeyRotationNoExternalWrites(t *testing.T, fixture workspaceKeyRotationFixture) {
	t.Helper()
	if fixture.client.createWrites != 0 || fixture.client.updateWrites != 0 || fixture.client.deleteWrites != 0 ||
		len(fixture.fabric.gatewaySecretInputs) != 0 || len(fixture.fabric.bindings) != 0 || len(fixture.fabric.runtimeInputs) != 0 || len(fixture.ledger.receipts) != 0 {
		t.Fatal("rejected rotation crossed a Gateway, Secret, Runtime or Ledger mutation boundary")
	}
}
