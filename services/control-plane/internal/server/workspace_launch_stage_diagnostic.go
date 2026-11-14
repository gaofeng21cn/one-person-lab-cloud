package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"opl-cloud/services/control-plane/internal/clients"
)

var workspaceLaunchStageDiagnosticErrorPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,127}$`)

func workspaceLaunchStageReadErrorCode(err error) string {
	if err == nil {
		return "none"
	}
	var fabricErr *clients.FabricHTTPError
	if errors.As(err, &fabricErr) {
		var envelope struct {
			Error string `json:"error"`
		}
		if json.Unmarshal([]byte(fabricErr.Body), &envelope) == nil {
			candidate := strings.TrimSpace(strings.SplitN(envelope.Error, ":", 2)[0])
			if workspaceLaunchStageDiagnosticErrorPattern.MatchString(candidate) {
				return candidate
			}
		}
		return fmt.Sprintf("fabric_http_%d", fabricErr.StatusCode)
	}
	for _, candidate := range []struct {
		err  error
		code string
	}{
		{errWorkspaceLaunchStageAdapterUnavailable, "stage_adapter_unavailable"},
		{errInvalidWorkspaceLaunchOperation, "stage_operation_invalid"},
	} {
		if errors.Is(err, candidate.err) {
			return candidate.code
		}
	}
	return "stage_read_failed"
}
