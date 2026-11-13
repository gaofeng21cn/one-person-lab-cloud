package main

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// The pre-debit IAM attestation shape this provisioner accepts is declared once
// for the source in packages/contracts/tencent-predebit-iam-contract.json. The
// installer reads that declaration out of the exact Candidate source it is
// about to install, so the accepted shape and the produced shape can never
// drift apart. These tests hold the declaration to what this provisioner
// actually enforces, and they are the reason the declaration may not drift
// away from the code.
const predebitIAMContractDeclarationPath = "../../../../packages/contracts/tencent-predebit-iam-contract.json"

type predebitIAMContractDeclaration struct {
	SchemaVersion       int    `json:"schemaVersion"`
	PredebitIAMContract string `json:"predebitIAMContract"`
	Attestation         struct {
		SchemaVersion   int    `json:"schemaVersion"`
		ProofMode       string `json:"proofMode"`
		BindsReleaseSha bool   `json:"bindsReleaseSha"`
	} `json:"attestation"`
	RequiredActions  []string `json:"requiredActions"`
	RequiredPolicies []string `json:"requiredPolicies"`
}

func readPredebitIAMContractDeclaration(t *testing.T) predebitIAMContractDeclaration {
	t.Helper()
	raw, err := os.ReadFile(predebitIAMContractDeclarationPath)
	if err != nil {
		t.Fatal(err)
	}
	var declaration predebitIAMContractDeclaration
	if err := json.Unmarshal(raw, &declaration); err != nil {
		t.Fatal(err)
	}
	return declaration
}

func TestPredebitIAMContractDeclarationMatchesProvisionerEnforcement(t *testing.T) {
	declaration := readPredebitIAMContractDeclaration(t)
	if declaration.SchemaVersion != 1 {
		t.Fatalf("pre-debit IAM contract schemaVersion=%d, want 1", declaration.SchemaVersion)
	}
	if strings.TrimSpace(declaration.PredebitIAMContract) == "" {
		t.Fatal("pre-debit IAM contract declares no contract name")
	}
	if declaration.Attestation.SchemaVersion != predebitIAMAttestationSchemaVersion {
		t.Fatalf("declared attestation schemaVersion=%d, provisioner accepts %d",
			declaration.Attestation.SchemaVersion, predebitIAMAttestationSchemaVersion)
	}
	if declaration.Attestation.ProofMode != predebitIAMProofMode {
		t.Fatalf("declared proofMode=%q, provisioner accepts %q",
			declaration.Attestation.ProofMode, predebitIAMProofMode)
	}
	if !reflect.DeepEqual(declaration.RequiredActions, predebitIAMRequiredActions) {
		t.Fatalf("declared requiredActions=%v, provisioner requires %v",
			declaration.RequiredActions, predebitIAMRequiredActions)
	}
	if !reflect.DeepEqual(declaration.RequiredPolicies, predebitIAMRequiredPolicies) {
		t.Fatalf("declared requiredPolicies=%v, provisioner requires %v",
			declaration.RequiredPolicies, predebitIAMRequiredPolicies)
	}
}

func TestPredebitIAMContractDeclarationProducesAcceptedAttestation(t *testing.T) {
	declaration := readPredebitIAMContractDeclaration(t)
	attestation := predebitIAMAttestation{
		SchemaVersion:    declaration.Attestation.SchemaVersion,
		ProofMode:        declaration.Attestation.ProofMode,
		ReleaseSHA:       "0123456789abcdef0123456789abcdef01234567",
		Identity:         predebitIAMIdentity{Type: "CAMUser", PrincipalID: "principal", AccountID: "100000000001", UserID: "100000000001"},
		RequiredActions:  append([]string(nil), declaration.RequiredActions...),
		RequiredPolicies: append([]string(nil), declaration.RequiredPolicies...),
		PolicyDigest:     "sha256:" + strings.Repeat("a", 64),
	}
	if !validPredebitIAMAttestation(attestation) {
		t.Fatalf("attestation built from the declared contract is rejected: %#v", attestation)
	}

	// The declared release binding has to be a real gate: the provisioner
	// compares the attested release with the running release, so an attestation
	// that carries no release can never satisfy the declared contract.
	if declaration.Attestation.BindsReleaseSha {
		unbound := attestation
		unbound.ReleaseSHA = ""
		if validPredebitIAMAttestation(unbound) {
			t.Fatal("an attestation without a release SHA satisfies a contract that binds the release")
		}
	}

	// The declared policies have to be a real gate too: dropping one must stop
	// the attestation from being accepted, which is what makes the finance
	// policy requirement an enforced fact rather than a note.
	for index := range declaration.RequiredPolicies {
		missing := attestation
		missing.RequiredPolicies = append([]string(nil), declaration.RequiredPolicies[:index]...)
		missing.RequiredPolicies = append(missing.RequiredPolicies, declaration.RequiredPolicies[index+1:]...)
		if validPredebitIAMAttestation(missing) {
			t.Fatalf("an attestation without %q satisfies the declared contract", declaration.RequiredPolicies[index])
		}
	}
}
