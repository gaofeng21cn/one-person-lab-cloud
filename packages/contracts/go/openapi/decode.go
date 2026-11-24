package openapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// DecodeStrict decodes a single JSON object into a generated OpenAPI contract
// type while rejecting unknown fields and trailing tokens.
func DecodeStrict[T any](raw []byte) (T, error) {
	var out T
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, err
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err != nil {
			return out, err
		}
		return out, errors.New("openapi payload contains trailing JSON tokens")
	}
	return out, nil
}

// NewError builds a contract-typed OpenAPI Error response envelope.
func NewError(code ErrorCode, message, requestID string) Error {
	return Error{
		Code:      code,
		Message:   message,
		RequestId: requestID,
	}
}
