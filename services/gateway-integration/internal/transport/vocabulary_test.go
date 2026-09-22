package transport

import (
	"testing"

	v226 "opl-cloud/packages/contracts/go/v226"
)

// The canonical database text is the v2.26 OpenAPI vocabulary (snake_case).
// These mappings must round-trip to the protobuf enum of the same word; they
// must never invent a third spelling.
func TestStageFromTextUsesTheContractVocabulary(t *testing.T) {
	cases := map[string]v226.OperationStageEnum{
		"admission":             v226.OperationStageEnum_OPERATION_STAGE_ENUM_ADMISSION,
		"identity_verification": v226.OperationStageEnum_OPERATION_STAGE_ENUM_IDENTITY_VERIFICATION,
		"key_revocation":        v226.OperationStageEnum_OPERATION_STAGE_ENUM_KEY_REVOCATION,
		"membership":            v226.OperationStageEnum_OPERATION_STAGE_ENUM_MEMBERSHIP,
		"wallet_binding":        v226.OperationStageEnum_OPERATION_STAGE_ENUM_WALLET_BINDING,
		"readback":              v226.OperationStageEnum_OPERATION_STAGE_ENUM_READBACK,
	}
	for text, want := range cases {
		if got := stageFromText(text); got != want {
			t.Fatalf("stageFromText(%q) = %v, want %v", text, got, want)
		}
	}
	if got := stageFromText(""); got != v226.OperationStageEnum_OPERATION_STAGE_ENUM_UNSPECIFIED {
		t.Fatalf("empty stage must be unspecified, got %v", got)
	}
	if got := stageFromText("not_a_stage"); got != v226.OperationStageEnum_OPERATION_STAGE_ENUM_UNSPECIFIED {
		t.Fatalf("unknown stage must not be guessed, got %v", got)
	}
}

func TestKindStatusAndObservationMapping(t *testing.T) {
	if got := kindFromText("create_tenant"); got != v226.OperationKindEnum_OPERATION_KIND_ENUM_CREATE_TENANT {
		t.Fatalf("kindFromText(create_tenant) = %v", got)
	}
	if got := kindFromText("revoke_key"); got != v226.OperationKindEnum_OPERATION_KIND_ENUM_REVOKE_KEY {
		t.Fatalf("kindFromText(revoke_key) = %v", got)
	}
	if got := statusFromText("needs_attention"); got != v226.OperationStatusEnum_OPERATION_STATUS_ENUM_NEEDS_ATTENTION {
		t.Fatalf("statusFromText(needs_attention) = %v", got)
	}
	if got := statusFromText("running"); got != v226.OperationStatusEnum_OPERATION_STATUS_ENUM_RUNNING {
		t.Fatalf("statusFromText(running) = %v", got)
	}

	unknown := observationFromText("unknown")
	if unknown == nil || *unknown != v226.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN {
		t.Fatalf("observationFromText(unknown) = %v", unknown)
	}
	if got := observationFromText(""); got != nil {
		t.Fatalf("an absent observation must stay absent, got %v", got)
	}
	if got := observationFromText("nonsense"); got != nil {
		t.Fatalf("an unknown observation must not be guessed, got %v", got)
	}
}

func TestErrorCodeMappingRejectsUnknownText(t *testing.T) {
	code := errorCodeFromText("gateway_unavailable")
	if code == nil || *code != v226.ErrorCodeEnum_ERROR_CODE_ENUM_GATEWAY_UNAVAILABLE {
		t.Fatalf("errorCodeFromText(gateway_unavailable) = %v", code)
	}
	reveal := errorCodeFromText("key_reveal_forbidden")
	if reveal == nil || *reveal != v226.ErrorCodeEnum_ERROR_CODE_ENUM_KEY_REVEAL_FORBIDDEN {
		t.Fatalf("errorCodeFromText(key_reveal_forbidden) = %v", reveal)
	}
	if got := errorCodeFromText(""); got != nil {
		t.Fatalf("an absent error code must stay absent, got %v", got)
	}
	if got := errorCodeFromText("not_an_error_code"); got != nil {
		t.Fatalf("an unknown error code must not be guessed, got %v", got)
	}
}

// This deployment unit serves exactly two data owners. Every other owner belongs
// to a different unit and must not be resolved to a local store.
func TestOwnerRoutingSelectsOnlyTheTwoOwnersThisUnitServes(t *testing.T) {
	for owner, want := range map[v226.OperationOwnerEnum]ownerRoute{
		v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT:  routeTenant,
		v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_GATEWAY: routeGateway,
	} {
		got, ok := routeFromOperationOwner(owner)
		if !ok || got != want {
			t.Fatalf("routeFromOperationOwner(%v) = %q ok=%v, want %q", owner, got, ok, want)
		}
	}
	for owner, want := range map[v226.OwnerEnum]ownerRoute{
		v226.OwnerEnum_OWNER_ENUM_TENANT:  routeTenant,
		v226.OwnerEnum_OWNER_ENUM_GATEWAY: routeGateway,
	} {
		got, ok := routeFromOwner(owner)
		if !ok || got != want {
			t.Fatalf("routeFromOwner(%v) = %q ok=%v, want %q", owner, got, ok, want)
		}
	}

	for _, owner := range []v226.OperationOwnerEnum{
		v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_UNSPECIFIED,
		v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_CAPABILITY,
		v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_BUILD,
		v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE,
		v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_RUNTIME_CONTROL,
		v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_FABRIC,
		v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_RESOURCE_CATALOG,
	} {
		if route, ok := routeFromOperationOwner(owner); ok {
			t.Fatalf("routeFromOperationOwner(%v) = %q, want rejected", owner, route)
		}
	}
	for _, owner := range []v226.OwnerEnum{
		v226.OwnerEnum_OWNER_ENUM_UNSPECIFIED,
		v226.OwnerEnum_OWNER_ENUM_CAPABILITY,
		v226.OwnerEnum_OWNER_ENUM_BUILD,
		v226.OwnerEnum_OWNER_ENUM_WORKSPACE,
		v226.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL,
		v226.OwnerEnum_OWNER_ENUM_FABRIC,
		v226.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG,
		v226.OwnerEnum_OWNER_ENUM_LEDGER,
	} {
		if route, ok := routeFromOwner(owner); ok {
			t.Fatalf("routeFromOwner(%v) = %q, want rejected", owner, route)
		}
	}
}
