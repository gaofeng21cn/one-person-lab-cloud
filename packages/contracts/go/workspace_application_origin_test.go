package contracts

import (
	"strings"
	"testing"
)

func TestWorkspaceApplicationOriginRoundTrip(t *testing.T) {
	for _, tc := range []struct{ workspace, host string }{
		{"ws-route", "ws-route-62097bae4a90.apps.example"},
		{"workspace_0123456789abcdef0123456789abcdef", "w-0123456789abcdef0123456789abcdef-n960d2f23588c.apps.example"},
	} {
		t.Run(tc.workspace, func(t *testing.T) {
			host, ok := WorkspaceApplicationOriginHost(tc.workspace, "knowledge-app", "apps.example")
			if !ok || host != tc.host {
				t.Fatalf("host=%q ok=%v want=%q", host, ok, tc.host)
			}
			expected, _ := WorkspaceApplicationOriginLabel(tc.workspace, "knowledge-app")
			for _, input := range []string{host, host + ":443", strings.ToUpper(host) + "."} {
				workspace, label, ok := ParseWorkspaceApplicationOriginHost(input, "apps.example")
				if !ok || workspace != tc.workspace || label != expected {
					t.Fatalf("parse %q = %q %q %v", input, workspace, label, ok)
				}
			}
			other, ok := WorkspaceApplicationOriginHost(tc.workspace, "other-app", "apps.example")
			if !ok || other == host {
				t.Fatal("different application reused binding origin")
			}
		})
	}
}

func TestWorkspaceApplicationOriginNativeAndRetainedNamespacesAreDisjoint(t *testing.T) {
	native := "workspace_0123456789abcdef0123456789abcdef"
	retained := "w-0123456789abcdef0123456789abcdef"
	nativeHost, nativeOK := WorkspaceApplicationOriginHost(native, "knowledge-app", "apps.example")
	retainedHost, retainedOK := WorkspaceApplicationOriginHost(retained, "knowledge-app", "apps.example")
	if !nativeOK || !retainedOK || nativeHost == retainedHost {
		t.Fatalf("origins collide: %q %q", nativeHost, retainedHost)
	}
	for host, want := range map[string]string{nativeHost: native, retainedHost: retained} {
		got, _, ok := ParseWorkspaceApplicationOriginHost(host, "apps.example")
		if !ok || got != want {
			t.Fatalf("host %q selected %q want %q", host, got, want)
		}
	}
}

func TestWorkspaceApplicationOriginRejectsUnencodableIdentities(t *testing.T) {
	for _, workspace := range []string{"", "ws_alpha", "WS-ALPHA", " ws-route", "ws-route ", " workspace_0123456789abcdef0123456789abcdef", "workspace_0123456789abcdef0123456789abcdef\t", "workspace_", "workspace_0123456789abcdef0123456789abcde", "workspace_0123456789abcdef0123456789abcdef0", "workspace_0123456789abcdef0123456789abcdeg", "workspace_0123456789ABCDEF0123456789abcdef", "workspace_0123456789abcdef0123456789abcdef/other", strings.Repeat("a", 51)} {
		if host, ok := WorkspaceApplicationOriginHost(workspace, "knowledge-app", "apps.example"); ok {
			t.Errorf("workspace %q produced %q", workspace, host)
		}
	}
	for _, application := range []string{"", "knowledge_app", "Knowledge App", " knowledge-app", "knowledge-app\t"} {
		if host, ok := WorkspaceApplicationOriginHost("workspace_0123456789abcdef0123456789abcdef", application, "apps.example"); ok {
			t.Errorf("application %q produced %q", application, host)
		}
	}
	for _, domain := range []string{"", "apps.example/", "apps.example:443", "apps.example.invalid/other", ".apps.example", strings.Repeat("a", 64) + ".example"} {
		if host, ok := WorkspaceApplicationOriginHost("workspace_0123456789abcdef0123456789abcdef", "knowledge-app", domain); ok {
			t.Errorf("domain %q produced %q", domain, host)
		}
	}
}

func TestWorkspaceApplicationOriginParserRejectsForeignAndMalformedHosts(t *testing.T) {
	valid := "w-0123456789abcdef0123456789abcdef-n960d2f23588c.apps.example"
	for _, host := range []string{"", "apps.example", " " + valid, valid + "\t", "evil." + valid, valid + ".evil", valid + ":0", valid + ":65536", valid + ":bad", valid + ":", "w-0123456789abcdef0123456789abcde-n960d2f23588c.apps.example", "w-0123456789abcdef0123456789abcdeg-n960d2f23588c.apps.example", "workspace_0123456789abcdef0123456789abcdef-n960d2f23588c.apps.example", "ws-route-n960d2f23588c.apps.example", "w-0123456789abcdef0123456789abcdef-x960d2f23588c.apps.example", "w-0123456789abcdef0123456789abcdef-n00000000000.apps.example", "w-0123456789abcdef0123456789abcdef-nzzzzzzzzzzzz.apps.example"} {
		if workspace, label, ok := ParseWorkspaceApplicationOriginHost(host, "apps.example"); ok {
			t.Errorf("host %q selected %q label=%q", host, workspace, label)
		}
	}
}
