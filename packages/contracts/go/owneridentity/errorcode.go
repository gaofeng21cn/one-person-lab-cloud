package owneridentity

import (
	"strings"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"
	api "opl-cloud/packages/contracts/go/api"
)

// ErrorDomain namespaces the ErrorInfo reason this boundary attaches to a typed
// owner failure. The reason is the canonical ErrorCodeEnum name so the public
// Console code and the owner's decision have one spelling.
const ErrorDomain = "opl.cloud"

// WithErrorCode attaches the owner's canonical ErrorCodeEnum to a gRPC status so a
// caller maps the business rejection without parsing a human message. An
// unspecified code or a non-status error is returned unchanged.
func WithErrorCode(err error, code api.ErrorCodeEnum) error {
	if err == nil || code == api.ErrorCodeEnum_ERROR_CODE_ENUM_UNSPECIFIED {
		return err
	}
	reason := strings.TrimPrefix(code.String(), "ERROR_CODE_ENUM_")
	if reason == "" || reason == code.String() {
		return err
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	withDetails, detailErr := st.WithDetails(&errdetails.ErrorInfo{Reason: reason, Domain: ErrorDomain})
	if detailErr != nil {
		return err
	}
	return withDetails.Err()
}

// ErrorCode reads the canonical ErrorCodeEnum an owner attached with
// WithErrorCode. A status without a typed reason reports not-found rather than
// guessing a business code from the transport status.
func ErrorCode(err error) (api.ErrorCodeEnum, bool) {
	st, ok := status.FromError(err)
	if !ok {
		return api.ErrorCodeEnum_ERROR_CODE_ENUM_UNSPECIFIED, false
	}
	for _, detail := range st.Details() {
		info, ok := detail.(*errdetails.ErrorInfo)
		if !ok || info.GetDomain() != ErrorDomain {
			continue
		}
		value, ok := api.ErrorCodeEnum_value["ERROR_CODE_ENUM_"+info.GetReason()]
		if !ok {
			continue
		}
		return api.ErrorCodeEnum(value), true
	}
	return api.ErrorCodeEnum_ERROR_CODE_ENUM_UNSPECIFIED, false
}
