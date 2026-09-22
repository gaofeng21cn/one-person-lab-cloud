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
		"upload_verification": v226.OperationStageEnum_OPERATION_STAGE_ENUM_UPLOAD_VERIFICATION,
		"reference_check":     v226.OperationStageEnum_OPERATION_STAGE_ENUM_REFERENCE_CHECK,
		"catalog_tombstone":   v226.OperationStageEnum_OPERATION_STAGE_ENUM_CATALOG_TOMBSTONE,
		"receipt":             v226.OperationStageEnum_OPERATION_STAGE_ENUM_RECEIPT,
		"succeeded":           v226.OperationStageEnum_OPERATION_STAGE_ENUM_SUCCEEDED,
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
	if got := kindFromText("complete_upload"); got != v226.OperationKindEnum_OPERATION_KIND_ENUM_COMPLETE_UPLOAD {
		t.Fatalf("kindFromText(complete_upload) = %v", got)
	}
	if got := kindFromText("delete_capability_version"); got != v226.OperationKindEnum_OPERATION_KIND_ENUM_DELETE_CAPABILITY_VERSION {
		t.Fatalf("kindFromText(delete_capability_version) = %v", got)
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
	code := errorCodeFromText("upload_checksum_mismatch")
	if code == nil || *code != v226.ErrorCodeEnum_ERROR_CODE_ENUM_UPLOAD_CHECKSUM_MISMATCH {
		t.Fatalf("errorCodeFromText(upload_checksum_mismatch) = %v", code)
	}
	if got := errorCodeFromText(""); got != nil {
		t.Fatalf("an absent error code must stay absent, got %v", got)
	}
	if got := errorCodeFromText("not_an_error_code"); got != nil {
		t.Fatalf("an unknown error code must not be guessed, got %v", got)
	}
}
