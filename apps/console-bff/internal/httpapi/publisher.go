package httpapi

import (
	"crypto/subtle"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"opl-cloud/apps/console-bff/internal/clients"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
)

// NewPublisherHandler exposes the same authenticated route implementation used
// by the process, with explicit typed owner dependencies and no BFF persistence.
func NewPublisherHandler(capability api.CapabilityProductServiceClient, build api.BuildProductServiceClient, identity IdentityReader) http.Handler {
	return (&Server{identity: identity, capability: capability, build: build}).Handler()
}

type publisherCall func(*http.Request, *api.CallContext, proto.Message) (proto.Message, error)

func (s *Server) publisherRoute(mux *http.ServeMux, pattern string, owner owneridentity.Owner, action api.AuthorizationActionEnum, kind api.AuthorizationResourceKind, resourcePath string, body func() proto.Message, invoke publisherCall) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		caller, err := RequireSession(r.Context(), s.identity, r)
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		ctx := WithCaller(r.Context(), caller, r.Header.Get(requestIDHeader))
		call := clients.CallContext(ctx)
		var input proto.Message
		if body != nil {
			origin, originErr := url.Parse(r.Header.Get("Origin"))
			if caller.Session.GetCsrfToken() == "" || subtle.ConstantTimeCompare([]byte(caller.Session.CsrfToken), []byte(r.Header.Get("x-opl-csrf"))) != 1 || r.Header.Get("Sec-Fetch-Site") == "cross-site" || (r.Header.Get("Origin") != "" && (originErr != nil || origin.Host != r.Host || (origin.Scheme != "https" && origin.Scheme != "http"))) {
				writeError(w, 403, "csrf_required", "same-origin session CSRF token required")
				return
			}
			call.IdempotencyKey = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
			if call.IdempotencyKey == "" || len(call.IdempotencyKey) > 256 {
				writeError(w, 400, "idempotency_key_required", "Idempotency-Key is required")
				return
			}
			media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || media != "application/json" {
				writeError(w, 415, "json_required", "application/json required")
				return
			}
			raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
			input = body()
			if err != nil || publicjson.Unmarshal(raw, input) != nil {
				writeError(w, 400, "invalid_request", "invalid or oversized request body")
				return
			}
		}
		resource := &api.AuthorizationResource{Kind: kind}
		if resourcePath != "" {
			resource.Id = proto.String(r.PathValue(resourcePath))
		}
		if input, ok := input.(*api.CreateBuildRequest); ok {
			resource.Id = proto.String(input.PackageVersionId)
		}
		if err := RequireAuthorizedAction(ctx, s.identity, caller, owner, action, resource, call.RequestId); err != nil {
			s.writeIdentityError(w, err)
			return
		}
		if (owner == owneridentity.Build && s.build == nil) || (owner == owneridentity.Capability && s.capability == nil) {
			writeError(w, 503, "owner_unconfigured", "publisher owner unavailable")
			return
		}
		result, err := invoke(r.WithContext(ctx), call, input)
		if err != nil {
			code := 502
			switch status.Code(err) {
			case codes.InvalidArgument:
				code = 400
			case codes.Unauthenticated:
				code = 401
			case codes.PermissionDenied:
				code = 403
			case codes.NotFound:
				code = 404
			case codes.AlreadyExists, codes.Aborted:
				code = 409
			case codes.FailedPrecondition:
				code = 422
			case codes.ResourceExhausted:
				code = 429
			case codes.Unavailable, codes.DeadlineExceeded:
				code = 503
			}
			writeError(w, code, "owner_request_failed", "publisher request could not be completed")
			return
		}
		raw, err := publicjson.Marshal(result)
		if err != nil {
			writeError(w, 502, "invalid_owner_response", "publisher owner returned an invalid response")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		code := 200
		if body != nil {
			code = 201
			if action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEBUILD {
				code = 202
			}
			if action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_COMPLETEUPLOAD {
				code = 202
			}
		}
		w.WriteHeader(code)
		_, _ = w.Write(raw)
	})
}

func (s *Server) registerPublisherRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v2/session", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		caller, err := RequireSession(r.Context(), s.identity, r)
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		raw, err := publicjson.Marshal(caller.Session)
		if err != nil {
			writeError(w, 502, "invalid_session", "invalid session response")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	})
	s.publisherRoute(mux, "GET /api/v2/namespaces", owneridentity.Capability, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTNAMESPACES, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, "", nil, func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
		return s.capability.ListNamespaces(r.Context(), &api.ListNamespacesRpcRequest{Context: c, QueryCursor: proto.String(r.URL.Query().Get("cursor"))})
	})
	s.publisherRoute(mux, "POST /api/v2/namespaces", owneridentity.Capability, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATENAMESPACE, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, "", func() proto.Message { return &api.NamespaceWriteRequest{} }, func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
		return s.capability.CreateNamespace(r.Context(), &api.CreateNamespaceRpcRequest{Context: c, Body: body.(*api.NamespaceWriteRequest)})
	})
	s.publisherRoute(mux, "GET /api/v2/packages", owneridentity.Capability, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTPACKAGES, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, "", nil, func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
		return s.capability.ListPackages(r.Context(), &api.ListPackagesRpcRequest{Context: c, QueryCursor: proto.String(r.URL.Query().Get("cursor")), QueryNamespaceId: proto.String(r.URL.Query().Get("namespaceId"))})
	})
	s.publisherRoute(mux, "POST /api/v2/packages", owneridentity.Capability, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEPACKAGE, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, "", func() proto.Message { return &api.CreatePackageRequest{} }, func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
		return s.capability.CreatePackage(r.Context(), &api.CreatePackageRpcRequest{Context: c, Body: body.(*api.CreatePackageRequest)})
	})
	s.publisherRoute(mux, "GET /api/v2/packages/{packageId}", owneridentity.Capability, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETPACKAGE, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PACKAGE, "packageId", nil, func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
		return s.capability.GetPackage(r.Context(), &api.GetPackageRpcRequest{Context: c, PackageId: r.PathValue("packageId")})
	})
	s.publisherRoute(mux, "POST /api/v2/packages/{packageId}/uploads", owneridentity.Capability, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEUPLOAD, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PACKAGE, "packageId", func() proto.Message { return &api.CreateUploadRequest{} }, func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
		return s.capability.CreateUpload(r.Context(), &api.CreateUploadRpcRequest{Context: c, PackageId: r.PathValue("packageId"), Body: body.(*api.CreateUploadRequest)})
	})
	s.publisherRoute(mux, "GET /api/v2/uploads/{uploadId}", owneridentity.Capability, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETUPLOAD, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, "uploadId", nil, func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
		return s.capability.GetUpload(r.Context(), &api.GetUploadRpcRequest{Context: c, UploadId: r.PathValue("uploadId")})
	})
	s.publisherRoute(mux, "POST /api/v2/uploads/{uploadId}/parts", owneridentity.Capability, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEUPLOADPART, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, "uploadId", func() proto.Message { return &api.CreateUploadPartRequest{} }, func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
		return s.capability.CreateUploadPart(r.Context(), &api.CreateUploadPartRpcRequest{Context: c, UploadId: r.PathValue("uploadId"), Body: body.(*api.CreateUploadPartRequest)})
	})
	s.publisherRoute(mux, "POST /api/v2/uploads/{uploadId}/complete", owneridentity.Capability, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_COMPLETEUPLOAD, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, "uploadId", func() proto.Message { return &api.CompleteUploadRequest{} }, func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
		return s.capability.CompleteUpload(r.Context(), &api.CompleteUploadRpcRequest{Context: c, UploadId: r.PathValue("uploadId"), Body: body.(*api.CompleteUploadRequest)})
	})
	s.publisherRoute(mux, "GET /api/v2/catalog/webui-versions", owneridentity.Capability, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTWEBUIVERSIONS, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG, "", nil, func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
		return s.capability.ListWebuiVersions(r.Context(), &api.ListWebuiVersionsRpcRequest{Context: c, QueryCursor: proto.String(r.URL.Query().Get("cursor"))})
	})
	s.publisherRoute(mux, "POST /api/v2/builds", owneridentity.Build, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEBUILD, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, "", func() proto.Message { return &api.CreateBuildRequest{} }, func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
		return s.build.CreateBuild(r.Context(), &api.CreateBuildRpcRequest{Context: c, Body: body.(*api.CreateBuildRequest)})
	})
	s.publisherRoute(mux, "GET /api/v2/builds/{buildId}", owneridentity.Build, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETBUILD, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_BUILD, "buildId", nil, func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
		return s.build.GetBuild(r.Context(), &api.GetBuildRpcRequest{Context: c, BuildId: r.PathValue("buildId")})
	})
	s.publisherRoute(mux, "GET /api/v2/capability-versions/{capabilityVersionId}", owneridentity.Capability, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETCAPABILITYVERSION, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, "capabilityVersionId", nil, func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
		return s.capability.GetCapabilityVersion(r.Context(), &api.GetCapabilityVersionRpcRequest{Context: c, CapabilityVersionId: r.PathValue("capabilityVersionId")})
	})
	s.publisherRoute(mux, "GET /api/v2/package-versions/{packageVersionId}", owneridentity.Capability, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETPACKAGEVERSION, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, "packageVersionId", nil, func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
		return s.capability.GetPackageVersion(r.Context(), &api.GetPackageVersionRpcRequest{Context: c, PackageVersionId: r.PathValue("packageVersionId")})
	})

}
