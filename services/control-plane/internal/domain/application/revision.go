// Package application validates and hashes retained immutable application
// revisions for historical readback and accepted-operation recovery.
package application

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	contracts "opl-cloud/packages/contracts/go"
)

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
