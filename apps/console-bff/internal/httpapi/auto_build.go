package httpapi

import (
	"context"
	"net/http"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"opl-cloud/apps/console-bff/internal/clients"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// autoBuildFailure reports that the upload completed but the automatic build
// did not start. The caller has to learn which installation fact is missing:
// the package version is uploaded, and a silent skip would leave it unbuilt
// with nothing to act on.
type autoBuildFailure struct {
	status  int
	code    string
	message string
}

func (e *autoBuildFailure) Error() string { return e.message }

// autoBuildKey is the idempotency key of the automatic build of one package
// version. The Build owner keeps its own record under this key, so a replayed
// upload resolves the same build job instead of allocating a second one.
func autoBuildKey(packageVersionID string) string {
	return "auto-build:" + packageVersionID
}

// startAutomaticBuild starts the build of the package version the caller just
// uploaded. The Build owner resolves the runtime and WebUI versions itself,
// so the BFF reads the installation's effective build policy and hands that
// pair over.
//
// A missing or unusable policy is an explicit failure, never a silent skip:
// the package version is uploaded, and the caller has to know that nothing is
// building it.
func (s *Server) startAutomaticBuild(ctx context.Context, call *api.CallContext, packageVersionID string) error {
	if strings.TrimSpace(packageVersionID) == "" {
		return &autoBuildFailure{status: http.StatusBadRequest, code: "BUILD_NOT_STARTED", message: "the upload did not name the package version to build"}
	}
	if s.runtimeControl == nil || s.build == nil {
		return &autoBuildFailure{status: http.StatusServiceUnavailable, code: "AUTO_BUILD_UNAVAILABLE", message: "this installation cannot start builds"}
	}
	policy, err := s.runtimeControl.GetBuildRuntimePolicy(ctx, &api.GetBuildRuntimePolicyRpcRequest{Context: call})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return &autoBuildFailure{status: http.StatusNotFound, code: "BUILD_POLICY_MISSING", message: "no effective build policy is set for this installation"}
		}
		return &autoBuildFailure{status: http.StatusServiceUnavailable, code: "BUILD_POLICY_UNAVAILABLE", message: "the effective build policy could not be read"}
	}
	if policy.GetRuntimeVersionId() == "" {
		return &autoBuildFailure{status: http.StatusConflict, code: "BUILD_POLICY_INCOMPLETE", message: "the effective build policy names no runtime version"}
	}
	if policy.GetDefaultWebuiVersionId() == "" {
		return &autoBuildFailure{status: http.StatusConflict, code: "BUILD_POLICY_INCOMPLETE", message: "the effective build policy names no default WebUI version"}
	}

	// The upload was authorized for the Capability owner. The Build owner
	// authorizes the actor itself, so the automatic build carries its own
	// decision, and its own idempotency key: one build per package version.
	buildCall := proto.Clone(call).(*api.CallContext)
	buildCall.AuthorizationContextId = ""
	buildCall.IdempotencyKey = autoBuildKey(packageVersionID)
	buildCtx := clients.WithCallContext(ctx, buildCall)
	resource := &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, Id: proto.String(packageVersionID)}
	if err := RequireAuthorizedAction(buildCtx, s.identity, callerFromCall(call), owneridentity.Build, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEBUILD, resource, call.GetRequestId()); err != nil {
		return &autoBuildFailure{status: http.StatusForbidden, code: "BUILD_NOT_AUTHORIZED", message: "this caller may not build the uploaded package version"}
	}

	_, err = s.build.CreateBuild(ctx, &api.CreateBuildRpcRequest{
		Context: buildCall,
		Body: &api.CreateBuildRequest{
			PackageVersionId: packageVersionID,
			RuntimeVersionId: policy.GetRuntimeVersionId(),
			WebuiVersionId:   policy.GetDefaultWebuiVersionId(),
		},
	})
	if err != nil {
		return &autoBuildFailure{status: ownerCallStatus(err), code: "BUILD_NOT_STARTED", message: buildFailureMessage(err)}
	}
	return nil
}

// buildFailureMessage names the owner's own reason when it is a product fact
// the caller can act on, such as a runtime or WebUI version that is not
// approved. A transport failure keeps the public message.
func buildFailureMessage(err error) string {
	switch status.Code(err) {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.NotFound, codes.AlreadyExists, codes.PermissionDenied:
		if detail := strings.TrimSpace(status.Convert(err).Message()); detail != "" {
			return detail
		}
	}
	return "the build could not be started"
}

// callerFromCall rebuilds the browser caller from the validated call context.
// The BFF forwards only the identity CloudIdentity resolved, so the call
// context carries the actor, the session and the scope the session carried.
func callerFromCall(call *api.CallContext) Caller {
	if call == nil {
		return Caller{}
	}
	session := &api.Session{ActorId: call.GetActorId()}
	if tenant := call.GetScope().GetTenant().GetTenantId(); tenant != "" {
		session.TenantId = proto.String(tenant)
	}
	return Caller{Session: session, SessionID: call.GetSessionId()}
}
