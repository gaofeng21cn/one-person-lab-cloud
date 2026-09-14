package contracts

import (
	"strings"
	"testing"
)

func TestValidateWorkspaceRegistryNamespace(t *testing.T) {
	for _, namespace := range WorkspaceRegistryCatalogNamespaces {
		if err := ValidateWorkspaceRegistryNamespace(namespace); err != nil {
			t.Fatalf("cataloged namespace %q must validate: %v", namespace, err)
		}
	}
	for _, namespace := range []string{"", "library", "oplcloudx", "oplcloud ", "../escape", "Oplcloud", "opl/cloud"} {
		if err := ValidateWorkspaceRegistryNamespace(namespace); err == nil {
			t.Fatalf("namespace %q must not validate", namespace)
		}
	}
}

func TestValidateWorkspaceRegistryRepository(t *testing.T) {
	if err := ValidateWorkspaceRegistryRepository("oplcloud", "one-person-lab-app"); err != nil {
		t.Fatalf("valid repository rejected: %v", err)
	}
	if err := ValidateWorkspaceRegistryRepository("oplcloud", "chaokang_agent_ibd"); err != nil {
		t.Fatalf("valid underscore repository rejected: %v", err)
	}
	for _, repository := range []string{"", "OnePerson", "a/b@c", "repo@sha256:abc", "../escape", "-leading"} {
		if err := ValidateWorkspaceRegistryRepository("oplcloud", repository); err == nil {
			t.Fatalf("repository %q must not validate", repository)
		}
	}
	if err := ValidateWorkspaceRegistryRepository("library", "app"); err == nil {
		t.Fatal("namespace outside the catalog must not validate")
	}
}

func TestWorkspaceRegistryEndpoint(t *testing.T) {
	endpoint, err := WorkspaceRegistryEndpoint("uswccr.ccs.tencentyun.com")
	if err != nil || endpoint != "https://uswccr.ccs.tencentyun.com" {
		t.Fatalf("plain host endpoint = %q, err = %v", endpoint, err)
	}
	if endpoint, err = WorkspaceRegistryEndpoint("registry.example:8443"); err != nil || endpoint != "https://registry.example:8443" {
		t.Fatalf("host with port endpoint = %q, err = %v", endpoint, err)
	}
	for _, host := range []string{"", "https://registry.example", "registry.example/path", "registry.example?q=1", "registry", "registry.example:99999"} {
		if endpoint, err := WorkspaceRegistryEndpoint(host); err == nil {
			t.Fatalf("host %q must not validate, endpoint %q", host, endpoint)
		}
	}
}

func TestWorkspaceRegistryImageReference(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	reference, err := WorkspaceRegistryImageReference("uswccr.ccs.tencentyun.com", "oplcloud", "one-person-lab-app", digest)
	if err != nil || reference != "uswccr.ccs.tencentyun.com/oplcloud/one-person-lab-app@"+digest {
		t.Fatalf("reference = %q, err = %v", reference, err)
	}
	if _, err := WorkspaceRegistryImageReference("uswccr.ccs.tencentyun.com", "oplcloud", "one-person-lab-app", "latest"); err == nil {
		t.Fatal("tag reference must not validate as digest-pinned")
	}
	if _, err := WorkspaceRegistryImageReference("uswccr.ccs.tencentyun.com", "library", "app", digest); err == nil {
		t.Fatal("outside-namespace reference must not validate")
	}
	if !ValidWorkspaceImageReference(reference) {
		t.Fatalf("assembled reference %q must satisfy the workspace image contract", reference)
	}
}
