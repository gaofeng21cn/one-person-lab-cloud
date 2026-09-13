package application

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	contracts "opl-cloud/packages/contracts/go"
)

// DataMaterialDigest returns the canonical content digest of one data
// material: same identity and same digest are identical; same identity with a
// different digest is an admission conflict.
func DataMaterialDigest(material contracts.WorkspaceApplicationDataMaterial) (string, error) {
	if err := contracts.ValidateWorkspaceApplicationDataMaterial(material); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(material)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(encoded)), nil
}

// DecideDataMaterialAdmission compares one candidate against the stored data
// material with the same identity. A nil admitted material means the identity
// is new.
func DecideDataMaterialAdmission(admitted *contracts.WorkspaceApplicationDataMaterial, candidate contracts.WorkspaceApplicationDataMaterial) (AdmissionDecision, error) {
	if err := contracts.ValidateWorkspaceApplicationDataMaterial(candidate); err != nil {
		return "", err
	}
	if admitted == nil {
		return AdmissionNew, nil
	}
	if admitted.ApplicationID != candidate.ApplicationID || admitted.Version != candidate.Version {
		return "", errors.New("workspace_application_data_material_identity_mismatch")
	}
	admittedDigest, err := DataMaterialDigest(*admitted)
	if err != nil {
		return "", err
	}
	candidateDigest, err := DataMaterialDigest(candidate)
	if err != nil {
		return "", err
	}
	if admittedDigest == candidateDigest {
		return AdmissionIdentical, nil
	}
	return "", ErrDataMaterialConflict
}
