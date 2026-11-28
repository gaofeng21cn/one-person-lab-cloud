package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"opl-cloud/apps/console-bff/internal/clients"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

type packageEntitlementInput struct {
	ConfirmationAccountID *string `json:"confirmationAccountId"`
	PackageID             *string `json:"packageId"`
	Enabled               *bool   `json:"enabled"`
	Reason                *string `json:"reason"`
	ExpiresAt             *string `json:"expiresAt"`
	ClearExpiresAt        bool    `json:"clearExpiresAt"`
}

func (s *Server) registerSoftwareDeliveryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v2/operator/accounts/{actorId}/package-entitlements", s.operatorPackageEntitlementHandler)
	mux.HandleFunc("GET /api/v2/operator/software-packages", s.operatorSoftwarePackagesHandler)
	mux.HandleFunc("GET /api/v2/software/entitlements", s.callerSoftwareEntitlementsHandler)
	mux.HandleFunc("POST /api/v2/software/packages/{packageId}/download", s.callerSoftwarePackageDownloadHandler)
}

func isSoftwarePackageDownloadPath(urlPath string) bool {
	if !strings.HasPrefix(urlPath, "/api/v2/software/packages/") || !strings.HasSuffix(urlPath, "/download") {
		return false
	}
	pkg := strings.TrimSuffix(strings.TrimPrefix(urlPath, "/api/v2/software/packages/"), "/download")
	_, ok := validSoftwarePackageID(pkg)
	return ok
}

func allowsGatewayBearerPath(urlPath string) bool {
	return urlPath == "/api/v2/mcp" ||
		urlPath == "/api/v2/client/service-grant" ||
		urlPath == "/api/v2/link-relay/entitlement" ||
		urlPath == "/api/v2/software/entitlements" ||
		isSoftwarePackageDownloadPath(urlPath)
}

func (s *Server) operatorPackageEntitlementHandler(w http.ResponseWriter, r *http.Request) {
	publisherRequestID(r)
	w.Header().Set("Cache-Control", "private, no-store")
	caller, err := RequireSession(r.Context(), s.identity, r)
	if err != nil {
		writePublisherIdentityError(w, r, err)
		return
	}
	caller.Session = proto.Clone(caller.Session).(*api.Session)
	caller.Session.TenantId = nil
	ctx := WithCaller(r.Context(), caller, r.Header.Get(requestIDHeader))
	call := clients.CallContext(ctx)
	if !requirePublisherWriteGuards(w, r, caller, call) {
		return
	}
	actorID := strings.TrimSpace(r.PathValue("actorId"))
	parsedID, parseErr := strconv.ParseInt(actorID, 10, 64)
	if parseErr != nil || parsedID <= 0 || actorID != r.PathValue("actorId") || strconv.FormatInt(parsedID, 10) != actorID {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "a valid target actor id is required")
		return
	}
	if r.URL.RawQuery != "" {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "query parameters are not accepted")
		return
	}
	raw, readErr := io.ReadAll(io.LimitReader(r.Body, operatorMutationMaxBodyBytes+1))
	if readErr != nil || len(raw) == 0 || len(raw) > operatorMutationMaxBodyBytes {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	var input packageEntitlementInput
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if input.ConfirmationAccountID == nil || strings.TrimSpace(*input.ConfirmationAccountID) != actorID || input.PackageID == nil || input.Enabled == nil || input.Reason == nil {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "confirmationAccountId, packageId, enabled and reason are required")
		return
	}
	pkgID, pkgOK := validSoftwarePackageID(*input.PackageID)
	if !pkgOK {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "a valid packageId is required")
		return
	}
	reason := strings.TrimSpace(*input.Reason)
	if reason == "" || utf8.RuneCountInString(reason) > operatorMutationMaxReasonRunes {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "reason must be between 1 and 200 characters")
		return
	}
	if input.ClearExpiresAt && input.ExpiresAt != nil {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "clearExpiresAt and expiresAt cannot both be set")
		return
	}
	var expiresTS *timestamppb.Timestamp
	if !input.ClearExpiresAt && input.ExpiresAt != nil {
		parsedTime, timeErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(*input.ExpiresAt))
		if timeErr != nil {
			writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "expiresAt must be a valid RFC3339 timestamp")
			return
		}
		expiresTS = timestamppb.New(parsedTime.UTC())
	}
	if s.tenant == nil {
		writePublisherError(w, r, http.StatusServiceUnavailable, "owner_unconfigured", "tenant owner is not configured")
		return
	}
	requestID := r.Header.Get(requestIDHeader)
	if err := RequireAuthorizedAction(ctx, s.identity, caller, owneridentity.Tenant,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETPACKAGEENTITLEMENT,
		&api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM, Id: proto.String(actorID)},
		requestID); err != nil {
		writePublisherIdentityError(w, r, err)
		return
	}
	resp, err := s.tenant.SetPackageEntitlement(ctx, &api.SetPackageEntitlementRpcRequest{
		Context:       call,
		TargetActorId: actorID,
		Body: &api.SetPackageEntitlementRequest{
			ConfirmationAccountId: actorID,
			PackageId:             pkgID,
			Enabled:               *input.Enabled,
			Reason:                reason,
			ExpiresAt:             expiresTS,
			ClearExpiresAt:        input.ClearExpiresAt,
		},
	})
	if err != nil {
		writeOwnerCallFailure(w, r, err)
		return
	}
	out := map[string]any{
		"operationId": resp.GetOperationId(),
		"accountId":   resp.GetAccountId(),
		"packageId":   resp.GetPackageId(),
		"status":      resp.GetStatus(),
		"enabled":     resp.GetEnabled(),
	}
	if resp.GetExpiresAt() != nil {
		out["expiresAt"] = resp.GetExpiresAt().AsTime().UTC().Format(time.RFC3339Nano)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) operatorSoftwarePackagesHandler(w http.ResponseWriter, r *http.Request) {
	publisherRequestID(r)
	w.Header().Set("Cache-Control", "private, no-store")
	if r.URL.RawQuery != "" {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "query parameters are not accepted")
		return
	}
	caller, err := RequireSession(r.Context(), s.identity, r)
	if err != nil {
		writePublisherIdentityError(w, r, err)
		return
	}
	caller.Session = proto.Clone(caller.Session).(*api.Session)
	caller.Session.TenantId = nil
	ctx := WithCaller(r.Context(), caller, r.Header.Get(requestIDHeader))
	if err := RequireAuthorizedAction(ctx, s.identity, caller, owneridentity.Tenant,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORSOFTWAREPACKAGES,
		&api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM},
		r.Header.Get(requestIDHeader)); err != nil {
		writePublisherIdentityError(w, r, err)
		return
	}
	configured, entries, catalogErr := s.loadSoftwareCatalog()
	if catalogErr != nil {
		writePublisherError(w, r, http.StatusServiceUnavailable, "software_catalog_invalid", "software catalog configuration is invalid")
		return
	}
	items := make([]any, 0, len(entries))
	for _, entry := range entries {
		items = append(items, entry.publicMetadata())
	}
	writeJSON(w, http.StatusOK, operatorSource("console-bff", map[string]any{
		"configured": configured,
		"items":      items,
	}, "", ""))
}

func (s *Server) resolveCallerSoftwareRPC(w http.ResponseWriter, r *http.Request, requireCookieCSRF bool) (*api.ListCallerPackageEntitlementsRpcRequest, bool) {
	if r.URL.RawQuery != "" {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "query parameters are not accepted")
		return nil, false
	}
	hasAuthHeader := len(r.Header.Values("Authorization")) != 0
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" || (r.Header.Get("Origin") != "" && !s.sameOrigin(r)) {
		writePublisherError(w, r, http.StatusForbidden, "origin_rejected", "cross-site request rejected")
		return nil, false
	}
	if s.tenant == nil {
		writeOwnerCallFailure(w, r, status.Error(codes.Unavailable, "Tenant owner is not configured"))
		return nil, false
	}
	if hasAuthHeader {
		token, err := singleGatewayBearer(r)
		if err != nil {
			writePublisherIdentityError(w, r, err)
			return nil, false
		}
		return &api.ListCallerPackageEntitlementsRpcRequest{BearerToken: token}, true
	}
	caller, err := RequireSession(r.Context(), s.identity, r)
	if err != nil {
		writePublisherIdentityError(w, r, err)
		return nil, false
	}
	if requireCookieCSRF {
		if caller.Session.GetCsrfToken() == "" || subtle.ConstantTimeCompare([]byte(caller.Session.GetCsrfToken()), []byte(r.Header.Get("X-CSRF-Token"))) != 1 || !s.sameOrigin(r) {
			writePublisherError(w, r, http.StatusForbidden, "csrf_required", "same-origin session CSRF required")
			return nil, false
		}
	}
	ctx := WithCaller(r.Context(), caller, r.Header.Get(requestIDHeader))
	return &api.ListCallerPackageEntitlementsRpcRequest{Context: clients.CallContext(ctx)}, true
}

func (s *Server) callerSoftwareEntitlementsHandler(w http.ResponseWriter, r *http.Request) {
	publisherRequestID(r)
	w.Header().Set("Cache-Control", "private, no-store")
	rpcReq, ok := s.resolveCallerSoftwareRPC(w, r, false)
	if !ok {
		return
	}
	resp, err := s.tenant.ListCallerPackageEntitlements(r.Context(), rpcReq)
	if err != nil {
		writeOwnerCallFailure(w, r, err)
		return
	}
	catalogConfigured, catalogEntries, catalogErr := s.loadSoftwareCatalog()
	if catalogErr != nil {
		catalogConfigured = false
		catalogEntries = nil
	}
	byID := make(map[string]softwareCatalogEntry, len(catalogEntries))
	for _, entry := range catalogEntries {
		byID[entry.PackageID] = entry
	}
	items := make([]any, 0, len(resp.GetItems()))
	for _, ent := range resp.GetItems() {
		item := map[string]any{
			"packageId":        ent.GetPackageId(),
			"enabled":          ent.GetEnabled(),
			"reason":           ent.GetReason(),
			"catalogAvailable": false,
		}
		if ent.GetExpiresAt() != nil {
			item["expiresAt"] = ent.GetExpiresAt().AsTime().UTC().Format(time.RFC3339Nano)
		}
		if ent.GetUpdatedAt() != nil {
			item["updatedAt"] = ent.GetUpdatedAt().AsTime().UTC().Format(time.RFC3339Nano)
		}
		if entry, found := byID[ent.GetPackageId()]; found {
			item["catalogAvailable"] = true
			item["displayName"] = entry.DisplayName
			item["summary"] = entry.Summary
			item["version"] = entry.Version
			item["sha256"] = entry.SHA256
			item["sizeBytes"] = entry.SizeBytes
			item["filename"] = entry.Filename
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"actorId":           resp.GetActorId(),
		"email":             resp.GetEmail(),
		"catalogConfigured": catalogConfigured,
		"items":             items,
	})
}

func (s *Server) callerSoftwarePackageDownloadHandler(w http.ResponseWriter, r *http.Request) {
	publisherRequestID(r)
	w.Header().Set("Cache-Control", "private, no-store")
	pkgID, pkgOK := validSoftwarePackageID(r.PathValue("packageId"))
	if !pkgOK {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "a valid packageId is required")
		return
	}
	rpcReq, ok := s.resolveCallerSoftwareRPC(w, r, true)
	if !ok {
		return
	}
	resp, err := s.tenant.ListCallerPackageEntitlements(r.Context(), rpcReq)
	if err != nil {
		writeOwnerCallFailure(w, r, err)
		return
	}
	entitled := false
	for _, item := range resp.GetItems() {
		if item.GetPackageId() == pkgID && item.GetEnabled() {
			entitled = true
			break
		}
	}
	if !entitled {
		writePublisherError(w, r, http.StatusForbidden, "package_entitlement_denied", "caller is not entitled to download this software package")
		return
	}
	_, entry, found, catalogErr := s.findSoftwareCatalogEntry(pkgID)
	if catalogErr != nil {
		writePublisherError(w, r, http.StatusServiceUnavailable, "software_catalog_invalid", "software catalog configuration is invalid")
		return
	}
	if !found {
		writePublisherError(w, r, http.StatusNotFound, "software_package_not_found", "requested software package is not configured")
		return
	}
	blobPath, blobErr := s.ensureSoftwareBlob(r.Context(), entry)
	if blobErr != nil {
		if errors.Is(blobErr, errSoftwareCacheUnconfigured) || errors.Is(blobErr, errSoftwareSourceUnconfigured) {
			writePublisherError(w, r, http.StatusServiceUnavailable, "software_cache_unconfigured", "software delivery cache is not configured")
			return
		}
		writePublisherError(w, r, http.StatusBadGateway, "software_fetch_failed", "unable to retrieve verified software package")
		return
	}
	f, openErr := os.Open(blobPath)
	if openErr != nil {
		writePublisherError(w, r, http.StatusBadGateway, "software_fetch_failed", "unable to open cached software package")
		return
	}
	defer f.Close()
	info, statErr := f.Stat()
	if statErr != nil || !info.Mode().IsRegular() {
		writePublisherError(w, r, http.StatusBadGateway, "software_fetch_failed", "unable to stat cached software package")
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+entry.Filename+`"`)
	w.Header().Set("X-Content-Sha256", entry.SHA256)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", formatContentLengthHeader(info.Size()))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}
