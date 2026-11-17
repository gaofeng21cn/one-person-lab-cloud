package composition

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestConfiguredPeerUsesTheTargetEffectiveListener(t *testing.T) {
	for _, address := range []struct{ listener, origin string }{
		{":19111", "http://127.0.0.1:19111"},
		{"0.0.0.0:19112", "http://127.0.0.1:19112"},
		{"[::]:19113", "http://[::1]:19113"},
		{"127.0.0.2:19114", "http://127.0.0.2:19114"},
	} {
		t.Run(address.listener, func(t *testing.T) {
			topology, err := Load(contractPath(t))
			if err != nil {
				t.Fatal(err)
			}
			installation := environment{"FABRIC_URL": "http://fabric:8082", "FABRIC_ADDR": address.listener,
				"OPL_FABRIC_SERVICE_TOKEN": "caller-token", "OPL_GRPC_INSECURE_LOCAL": "1"}
			plan, err := NewPlan(topology, installation.getenv)
			if err != nil {
				t.Fatal(err)
			}
			projected, err := ChildEnvironment(stepFor(t, plan, "control-plane").Process,
				[]string{"FABRIC_URL=http://fabric:8082", "OPL_FABRIC_SERVICE_TOKEN=caller-token", "UNRELATED_OWNER_SECRET=private"}, installation.getenv)
			if err != nil {
				t.Fatal(err)
			}
			got := strings.Join(projected, "\n")
			if !strings.Contains(got, "FABRIC_URL="+address.origin) || !strings.Contains(got, "OPL_FABRIC_SERVICE_TOKEN=caller-token") {
				t.Fatalf("peer origin or caller credential was lost: %v", projected)
			}
			if strings.Contains(got, "UNRELATED_OWNER_SECRET") {
				t.Fatal("peer resolution widened the child environment")
			}
		})
	}
}

func TestConfiguredPeerRejectsMissingFactsAndKeepsOptionalPeersAbsent(t *testing.T) {
	for _, change := range []struct {
		name string
		edit func(*Topology)
	}{
		{"missing target", func(topology *Topology) { topology.PeerAddresses.Entries[0].Target = "" }},
		{"unknown target", func(topology *Topology) { topology.PeerAddresses.Entries[0].Target = "missing" }},
		{"missing target listener", func(topology *Topology) { topology.PeerAddresses.Entries[0].TargetAddressEnv = "MISSING_LISTENER" }},
		{"unknown protocol", func(topology *Topology) { topology.PeerAddresses.Entries[0].OriginScheme = "file" }},
	} {
		t.Run(change.name, func(t *testing.T) {
			topology, err := Load(contractPath(t))
			if err != nil {
				t.Fatal(err)
			}
			change.edit(&topology)
			if _, err := NewPlan(topology, environment{"FABRIC_URL": "http://fabric:8082"}.getenv); err == nil {
				t.Fatal("a configured peer with missing or unknown target facts must not start")
			}
		})
	}
	for _, value := range []string{"", " \t"} {
		topology, _ := Load(contractPath(t))
		plan, err := NewPlan(topology, environment{"FABRIC_URL": value}.getenv)
		if err != nil {
			t.Fatal(err)
		}
		if len(stepFor(t, plan, "control-plane").Process.PeerEnvironment) != 0 {
			t.Fatal("an absent optional peer must not acquire a synthetic origin")
		}
	}
}

// The real ExecStarter child consumes the projected origin and credential and
// reaches a local HTTP peer. This would dial fabric.invalid before projection.
func TestExecStarterChildReachesConfiguredPeer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" || r.Header.Get("Authorization") != "Bearer caller-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, "peer-ready")
	}))
	defer server.Close()
	topology, err := Load(contractPath(t))
	if err != nil {
		t.Fatal(err)
	}
	installation := environment{"FABRIC_URL": "http://fabric.invalid:8082", "FABRIC_ADDR": strings.TrimPrefix(server.URL, "http://"),
		"OPL_FABRIC_SERVICE_TOKEN": "caller-token", "OPL_GRPC_INSECURE_LOCAL": "1", "OPL_TEST_PEER_CHILD": "1"}
	plan, err := NewPlan(topology, installation.getenv)
	if err != nil {
		t.Fatal(err)
	}
	process := stepFor(t, plan, "control-plane").Process
	process.Binary, err = os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	process.DaemonArgs = []string{"-test.run=^TestPeerHTTPChild$"}
	process.AllowedEnv = append(process.AllowedEnv, "OPL_TEST_PEER_CHILD")
	parent := []string{}
	for name, value := range installation {
		parent = append(parent, name+"="+value)
	}
	var output bytes.Buffer
	starter := ExecStarter{Getenv: installation.getenv, Environ: func() []string { return parent }, Stdout: &output, Stderr: &output}
	child, err := starter.Start(context.Background(), process)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-child.Exited():
		if err := child.Err(); err != nil {
			t.Fatalf("HTTP peer child failed: %v; %s", err, output.String())
		}
	case <-time.After(10 * time.Second):
		_ = child.Signal(os.Kill)
		t.Fatal("HTTP peer child did not exit")
	}
	if !strings.Contains(output.String(), "peer-ready") {
		t.Fatalf("the child did not consume the local peer: %s", output.String())
	}
}

func TestPeerHTTPChild(t *testing.T) {
	if os.Getenv("OPL_TEST_PEER_CHILD") != "1" {
		return
	}
	request, err := http.NewRequest(http.MethodGet, os.Getenv("FABRIC_URL")+"/readyz", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+os.Getenv("OPL_FABRIC_SERVICE_TOKEN"))
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("peer returned %d", response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Print(string(body))
}
