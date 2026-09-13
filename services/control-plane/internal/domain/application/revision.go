// Package application owns the admission and deployment rules for Workspace
// application revisions: immutable identity, idempotent re-admission,
// conflict detection and the deployment transition checks that reference an
// admitted revision. The rules are pure; persistence, registry access and
// administrator authorization stay outside this package.
package application

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/domain/provisioning"
)

var (
	// ErrRevisionConflict reports two different revision contents admitted
	// under the same application identity.
	ErrRevisionConflict = errors.New("workspace_application_revision_conflict")
	// ErrRevisionNotAdmitted reports a deployment that targets a revision
	// identity without an admitted revision behind it.
	ErrRevisionNotAdmitted = errors.New("workspace_application_revision_not_admitted")
	// ErrDataMaterialConflict reports two different data material contents
	// admitted under the same application identity.
	ErrDataMaterialConflict = errors.New("workspace_application_data_material_conflict")
	// ErrDeploymentTransitionInvalid reports a deployment intent whose
	// previous-application facts do not match the Workspace's current binding.
	ErrDeploymentTransitionInvalid = errors.New("workspace_application_deployment_transition_invalid")
)

// RevisionRef names one admitted revision inside a Workspace binding. Both
// parts exclude the separator, so the reference is unambiguous.
type RevisionRef struct {
	ApplicationID string
	Version       string
}

// RevisionRefOf returns the reference of one revision.
func RevisionRefOf(revision contracts.WorkspaceApplicationRevision) RevisionRef {
	return RevisionRef{ApplicationID: revision.ApplicationID, Version: revision.Version}
}

// String renders the reference as "<applicationID>@<version>", the value the
// Workspace projection carries once a deployment activates.
func (r RevisionRef) String() string {
	return r.ApplicationID + "@" + r.Version
}

// ParseRevisionRef parses a binding value into a reference. Retained binding
// values such as the empty binding and the fixed OPL App are not references.
func ParseRevisionRef(value string) (RevisionRef, error) {
	applicationID, version, found := strings.Cut(value, "@")
	if !found || strings.TrimSpace(applicationID) == "" || strings.TrimSpace(version) == "" ||
		strings.ContainsAny(applicationID, " \t") || strings.ContainsAny(version, " \t@") {
		return RevisionRef{}, errors.New("workspace_application_revision_ref_invalid")
	}
	return RevisionRef{ApplicationID: applicationID, Version: version}, nil
}

// RevisionDigest returns the canonical content digest of one revision. Two
// revisions with the same identity and the same digest are identical; the
// same identity with a different digest is an admission conflict.
func RevisionDigest(revision contracts.WorkspaceApplicationRevision) (string, error) {
	if err := contracts.ValidateWorkspaceApplicationRevision(revision); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(revision)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", sum), nil
}

// AdmissionDecision is the outcome of admitting one revision against the
// already-admitted revision with the same identity, if any.
type AdmissionDecision string

const (
	// AdmissionNew reports an application identity admitted for the first time.
	AdmissionNew AdmissionDecision = "new"
	// AdmissionIdentical reports an exact replay of an admitted revision:
	// the store keeps the original and no new identity is created.
	AdmissionIdentical AdmissionDecision = "identical"
)

// DecideRevisionAdmission compares one candidate against the stored revision
// with the same identity. A nil admitted revision means the identity is new.
func DecideRevisionAdmission(admitted *contracts.WorkspaceApplicationRevision, candidate contracts.WorkspaceApplicationRevision) (AdmissionDecision, error) {
	if err := contracts.ValidateWorkspaceApplicationRevision(candidate); err != nil {
		return "", err
	}
	if admitted == nil {
		return AdmissionNew, nil
	}
	if RevisionRefOf(*admitted) != RevisionRefOf(candidate) {
		return "", errors.New("workspace_application_admission_identity_mismatch")
	}
	admittedDigest, err := RevisionDigest(*admitted)
	if err != nil {
		return "", err
	}
	candidateDigest, err := RevisionDigest(candidate)
	if err != nil {
		return "", err
	}
	if admittedDigest == candidateDigest {
		return AdmissionIdentical, nil
	}
	return "", ErrRevisionConflict
}

// ValidateDeploymentTransition checks one deployment intent against the
// admitted revision it targets and the Workspace's current application
// binding. The binding is the projection's application binding value: the
// empty binding, the retained fixed OPL App value, or a revision reference.
func ValidateDeploymentTransition(deployment contracts.WorkspaceApplicationDeployment, admittedRevision contracts.WorkspaceApplicationRevision, currentBinding string) error {
	if err := contracts.ValidateWorkspaceApplicationDeployment(deployment); err != nil {
		return err
	}
	if target := RevisionRefOf(admittedRevision); deployment.ApplicationID != target.ApplicationID || deployment.TargetRevision != target.Version {
		return ErrRevisionNotAdmitted
	}
	switch currentBinding {
	case provisioning.ApplicationBindingEmpty, "":
		if deployment.PreviousApplicationID != "" || deployment.PreviousRevision != "" {
			return ErrDeploymentTransitionInvalid
		}
	case provisioning.ApplicationBindingOPLApp:
		// Replacing the retained default application names no previous
		// revision, because the fixed OPL App never carried one.
		if deployment.PreviousApplicationID != "" || deployment.PreviousRevision != "" {
			return ErrDeploymentTransitionInvalid
		}
	default:
		previous, err := ParseRevisionRef(currentBinding)
		if err != nil {
			return ErrDeploymentTransitionInvalid
		}
		if deployment.PreviousApplicationID != previous.ApplicationID || deployment.PreviousRevision != previous.Version {
			return ErrDeploymentTransitionInvalid
		}
	}
	return nil
}

// RevisionStore is the read-side port over admitted revisions. The owning
// persistence keeps revisions immutable; implementations return the stored
// revision, its canonical digest and whether the identity was admitted.
type RevisionStore interface {
	AdmittedRevision(ctx context.Context, applicationID, version string) (revision contracts.WorkspaceApplicationRevision, digest string, found bool, err error)
}
