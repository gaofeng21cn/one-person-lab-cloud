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
		"admission":         v226.OperationStageEnum_OPERATION_STAGE_ENUM_ADMISSION,
		"quote_binding":     v226.OperationStageEnum_OPERATION_STAGE_ENUM_QUOTE_BINDING,
		"consent_commit":    v226.OperationStageEnum_OPERATION_STAGE_ENUM_CONSENT_COMMIT,
		"catalog_tombstone": v226.OperationStageEnum_OPERATION_STAGE_ENUM_CATALOG_TOMBSTONE,
		"readback":          v226.OperationStageEnum_OPERATION_STAGE_ENUM_READBACK,
		"queued":            v226.OperationStageEnum_OPERATION_STAGE_ENUM_QUEUED,
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
	if got := kindFromText("reconcile"); got != v226.OperationKindEnum_OPERATION_KIND_ENUM_RECONCILE {
		t.Fatalf("kindFromText(reconcile) = %v", got)
	}
	if got := statusFromText("awaiting_confirmation"); got != v226.OperationStatusEnum_OPERATION_STATUS_ENUM_AWAITING_CONFIRMATION {
		t.Fatalf("statusFromText(awaiting_confirmation) = %v", got)
	}
	if got := statusFromText("needs_attention"); got != v226.OperationStatusEnum_OPERATION_STATUS_ENUM_NEEDS_ATTENTION {
		t.Fatalf("statusFromText(needs_attention) = %v", got)
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
	code := errorCodeFromText("quote_expired")
	if code == nil || *code != v226.ErrorCodeEnum_ERROR_CODE_ENUM_QUOTE_EXPIRED {
		t.Fatalf("errorCodeFromText(quote_expired) = %v", code)
	}
	if got := errorCodeFromText(""); got != nil {
		t.Fatalf("an absent error code must stay absent, got %v", got)
	}
	if got := errorCodeFromText("not_an_error_code"); got != nil {
		t.Fatalf("an unknown error code must not be guessed, got %v", got)
	}
}
