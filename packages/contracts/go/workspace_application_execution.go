package contracts

import (
	"errors"
	"regexp"
)

// Execution is a component's explicit process requirement, not host authority.
// A custom seccomp digest must resolve to a profile approved by the installation;
// providers reject requirements they cannot represent rather than ignoring them.
type WorkspaceApplicationExecution struct {
	UserID         *int64 `json:"userId,omitempty"`
	GroupID        *int64 `json:"groupId,omitempty"`
	Init           bool   `json:"init,omitempty"`
	SeccompProfile string `json:"seccompProfile,omitempty"`
}

var applicationSeccompDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func ValidateWorkspaceApplicationExecution(execution WorkspaceApplicationExecution) error {
	if execution.UserID != nil && (*execution.UserID <= 0 || *execution.UserID > 2147483647) || execution.GroupID != nil && (*execution.GroupID < 0 || *execution.GroupID > 2147483647) || execution.GroupID != nil && execution.UserID == nil {
		return errors.New("workspace_application_execution_identity_invalid")
	}
	if execution.SeccompProfile != "" && !applicationSeccompDigestPattern.MatchString(execution.SeccompProfile) {
		return errors.New("workspace_application_seccomp_profile_invalid")
	}
	return nil
}
