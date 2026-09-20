package server

import (
	"context"
	"errors"
	"strings"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/controlplane"
)

var errProviderConsistencyFailure = errors.New("provider_consistency_failure")

func normalizeFabricReadiness(readiness contracts.FabricReadiness, configured fabricProvider) (contracts.FabricReadiness, bool) {
	if strings.TrimSpace(readiness.Provider) == string(configured) {
		return readiness, false
	}
	readiness.Ready = false
	readiness.ServiceReady = false
	for _, check := range readiness.FailedChecks {
		if check == errProviderConsistencyFailure.Error() {
			return readiness, true
		}
	}
	readiness.FailedChecks = append(readiness.FailedChecks, errProviderConsistencyFailure.Error())
	return readiness, true
}

func (app *controlPlaneServer) fabricReadiness(ctx context.Context, service *controlplane.Service) (contracts.FabricReadiness, error) {
	readiness, err := service.RuntimeReadiness(ctx)
	if err != nil {
		return contracts.FabricReadiness{}, err
	}
	readiness, mismatch := normalizeFabricReadiness(readiness, app.deployment.FabricProvider)
	if mismatch {
		return readiness, errProviderConsistencyFailure
	}
	return readiness, nil
}
