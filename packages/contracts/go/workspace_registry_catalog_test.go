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

// The approved repository set is declared configuration, so its parser is the
// boundary that decides what an installation may publish. It normalizes order
// and duplicates, and it refuses anything the namespace boundary would refuse.
func TestWorkspaceRegistryDeclaredRepositories(t *testing.T) {
	parsed, err := WorkspaceRegistryDeclaredRepositories(" oplcloud/one-person-lab-app , oplcloud/chaokang_agent_ibd ,oplcloud/one-person-lab-app, ")
	if err != nil {
		t.Fatalf("valid declaration rejected: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("duplicates were not normalized: %+v", parsed)
	}
	if parsed[0].Namespace != "oplcloud" || parsed[0].Repository != "chaokang_agent_ibd" ||
		parsed[1].Namespace != "oplcloud" || parsed[1].Repository != "one-person-lab-app" {
		t.Fatalf("sorted set = %+v", parsed)
	}

	// A nested repository name keeps everything after the first separator.
	nested, err := WorkspaceRegistryDeclaredRepositories("oplcloud/team/app")
	if err != nil || len(nested) != 1 || nested[0].Repository != "team/app" {
		t.Fatalf("nested repository = %+v err=%v", nested, err)
	}

	for _, declared := range []string{
		"",
		"   ",
		",",
		"library/nginx",
		"no-namespace",
		"/leading-slash",
		"oplcloud/",
		"oplcloud/Uppercase",
		"oplcloud/has space",
	} {
		if _, err := WorkspaceRegistryDeclaredRepositories(declared); err == nil {
			t.Fatalf("declaration %q was accepted", declared)
		}
	}
}

func TestSplitWorkspaceImageReference(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	host, namespace, repository, gotDigest, err := SplitWorkspaceImageReference("uswccr.ccs.tencentyun.com/oplcloud/chaokang_agent_ibd@" + digest)
	if err != nil || host != "uswccr.ccs.tencentyun.com" || namespace != "oplcloud" || repository != "chaokang_agent_ibd" || gotDigest != digest {
		t.Fatalf("split = %q %q %q %q err=%v", host, namespace, repository, gotDigest, err)
	}
	// A nested repository keeps everything after the namespace separator.
	if _, _, nested, _, nestedErr := SplitWorkspaceImageReference("registry.example/oplcloud/team/app@" + digest); nestedErr != nil || nested != "team/app" {
		t.Fatalf("nested repository = %q err=%v", nested, nestedErr)
	}
	for _, value := range []string{
		"", "oplcloud/app@" + digest, "uswccr.ccs.tencentyun.com/oplcloud/app", "uswccr.ccs.tencentyun.com/oplcloud/app@latest",
		"uswccr.ccs.tencentyun.com/app@" + digest, "https://uswccr.ccs.tencentyun.com/oplcloud/app@" + digest,
		"uswccr.ccs.tencentyun.com/oplcloud/Uppercase@" + digest,
	} {
		if _, _, _, _, err := SplitWorkspaceImageReference(value); err == nil {
			t.Fatalf("reference %q was accepted", value)
		}
	}
	// Reading a reference is not an installation decision: a namespace outside
	// the catalog parses, and the registry boundary refuses it separately.
	if _, namespace, repository, _, err := SplitWorkspaceImageReference("uswccr.ccs.tencentyun.com/library/app@" + digest); err != nil || namespace != "library" || repository != "app" {
		t.Fatalf("shape parse = %q %q err=%v", namespace, repository, err)
	} else if err := ValidateWorkspaceRegistryRepository(namespace, repository); err == nil {
		t.Fatal("the catalog namespace boundary must reject an outside namespace")
	}
}
