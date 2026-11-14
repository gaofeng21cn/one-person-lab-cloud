package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testProbeTimeout = 5 * time.Second

// The Console BFF is a deployment unit rather than a data owner, so the probe
// answers for it over the HTTP surface it serves. These cases keep that
// criterion its own and keep the owner criterion closed to identities the
// contracts package does not accept.
func TestProbeConsoleBFFAnswersOverItsOwnSurface(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Errorf("the Console BFF probe requested %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	out, err := probeConsoleBFF(config{Address: server.URL, Timeout: testProbeTimeout}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("the Console BFF probe failed on a healthy surface: %v", err)
	}
	if out.Status != "ok" || out.Health == nil || out.Health.Status != "ok" {
		t.Fatalf("the Console BFF probe did not report a ready Console: %+v", out)
	}
}

func TestProbeConsoleBFFRefusesAnUnreadySurface(t *testing.T) {
	for name, body := range map[string]string{
		"degraded":  `{"status":"degraded"}`,
		"no status": `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("content-type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()

			out, err := probeConsoleBFF(config{Address: server.URL, Timeout: testProbeTimeout}, func(string) string { return "" })
			if err == nil {
				t.Fatalf("the Console BFF probe accepted an unready surface: %+v", out)
			}
			if out.Status != "failed" {
				t.Fatalf("the Console BFF probe did not report a failure: %+v", out)
			}
		})
	}
}

func TestProbeConsoleBFFRefusesAnUnreachableSurface(t *testing.T) {
	out, err := probeConsoleBFF(config{Address: "http://127.0.0.1:1", Timeout: testProbeTimeout}, func(string) string { return "" })
	if err == nil {
		t.Fatalf("the Console BFF probe accepted an unreachable surface: %+v", out)
	}
	if !strings.HasPrefix(err.Error(), "console_bff_health") {
		t.Fatalf("the Console BFF probe did not name its own criterion: %v", err)
	}
}

// The deployment units keep their own criterion, and an identity that is
// neither an owner nor a deployment unit is still refused before any request
// is made.
func TestProbeFromArgsRoutesEachIdentityToItsOwnCriterion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	if _, err := probeFromArgs([]string{"--owner", "console_bff", "--addr", server.URL, "--timeout", "5s"}, func(string) string { return "" }); err != nil {
		t.Fatalf("the Console BFF identity was not routed to its own criterion: %v", err)
	}
	out, err := probeFromArgs([]string{"--owner", "not_an_owner", "--addr", server.URL, "--timeout", "5s"}, func(string) string { return "" })
	if err == nil || errorCode(err) != "OWNER_INVALID" {
		t.Fatalf("an identity that is neither an owner nor a deployment unit was not refused: %v", err)
	}
	if out.Status != "failed" {
		t.Fatalf("a refused identity did not report a failure: %+v", out)
	}
}
