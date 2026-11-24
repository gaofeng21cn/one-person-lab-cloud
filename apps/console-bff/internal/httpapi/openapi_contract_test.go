package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"opl-cloud/packages/contracts/go/openapi"
)

func TestOpenAPIErrorEnvelopeMatchesGeneratedContract(t *testing.T) {
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v2/workspaces", nil)
	req.Header.Set(requestIDHeader, "req-openapi-001")

	writePublisherError(recorder, req, http.StatusForbidden, "csrf_required", "session CSRF token required")

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
	decoded, err := openapi.DecodeStrict[openapi.Error](recorder.Body.Bytes())
	if err != nil {
		t.Fatalf("DecodeStrict[openapi.Error]: %v (body=%s)", err, recorder.Body.String())
	}
	if decoded.Code != openapi.CSRFINVALID || !decoded.Code.Valid() {
		t.Fatalf("decoded error code = %q, want %q", decoded.Code, openapi.CSRFINVALID)
	}
	if decoded.Message != "session CSRF token required" || decoded.RequestId != "req-openapi-001" {
		t.Fatalf("decoded error = %+v", decoded)
	}
}
