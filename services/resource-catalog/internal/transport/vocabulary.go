package transport

import (
	"strings"

	v226 "opl-cloud/packages/contracts/go/v226"
)

// The database stores plain text, deliberately without duplicating a second
// enum. The canonical values are the v2.26 OpenAPI vocabulary (snake_case), and
// the protobuf enums are the upper-snake spelling of the same words with their
// enum prefix. These helpers translate between the two without defining a third
// vocabulary.

func stageFromText(text string) v226.OperationStageEnum {
	if text == "" {
		return v226.OperationStageEnum_OPERATION_STAGE_ENUM_UNSPECIFIED
	}
	value, ok := v226.OperationStageEnum_value["OPERATION_STAGE_ENUM_"+strings.ToUpper(text)]
	if !ok {
		return v226.OperationStageEnum_OPERATION_STAGE_ENUM_UNSPECIFIED
	}
	return v226.OperationStageEnum(value)
}

func kindFromText(text string) v226.OperationKindEnum {
	if text == "" {
		return v226.OperationKindEnum_OPERATION_KIND_ENUM_UNSPECIFIED
	}
	value, ok := v226.OperationKindEnum_value["OPERATION_KIND_ENUM_"+strings.ToUpper(text)]
	if !ok {
		return v226.OperationKindEnum_OPERATION_KIND_ENUM_UNSPECIFIED
	}
	return v226.OperationKindEnum(value)
}

func statusFromText(text string) v226.OperationStatusEnum {
	if text == "" {
		return v226.OperationStatusEnum_OPERATION_STATUS_ENUM_UNSPECIFIED
	}
	value, ok := v226.OperationStatusEnum_value["OPERATION_STATUS_ENUM_"+strings.ToUpper(text)]
	if !ok {
		return v226.OperationStatusEnum_OPERATION_STATUS_ENUM_UNSPECIFIED
	}
	return v226.OperationStatusEnum(value)
}

func observationFromText(text string) *v226.OperationObservationResultEnum {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	value, ok := v226.OperationObservationResultEnum_value["OPERATION_OBSERVATION_RESULT_ENUM_"+strings.ToUpper(text)]
	if !ok {
		return nil
	}
	result := v226.OperationObservationResultEnum(value)
	return &result
}

func errorCodeFromText(text string) *v226.ErrorCodeEnum {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	value, ok := v226.ErrorCodeEnum_value["ERROR_CODE_ENUM_"+strings.ToUpper(text)]
	if !ok {
		return nil
	}
	code := v226.ErrorCodeEnum(value)
	return &code
}
