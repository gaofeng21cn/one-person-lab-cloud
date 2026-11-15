package contracts

import (
	"encoding/json"
	"strings"
	"testing"
)

const (
	workspaceImageTrustRepository      = "127.0.0.1:5000/qualification/local-qualification"
	workspaceImageTrustOtherRepository = "127.0.0.1:5000/qualification/other-qualification"
)

func workspaceImageTrustReferenceForTest(digit string) string {
	return workspaceImageTrustRepository + "@sha256:" + strings.Repeat(digit, 64)
}

// The activation request must carry the repository as its own field. A
// repository is what a trust decision is made about; hiding it inside the
// version string would make the publisher's own build output unstartable
// without a hand-written catalog entry.
// The route path and the credential header are a shared contract between the
// Control Plane and the providers that read the trust source. They are
// written out literally here so a change to either one is a deliberate,
// visible change rather than a silent drift between two modules.
func TestWorkspaceImageTrustSourceWireContractIsPinned(t *testing.T) {
	if WorkspaceImageTrustSourcePath != "/internal/workspace-image-trust-source" {
		t.Fatalf("trust source path changed: %q", WorkspaceImageTrustSourcePath)
	}
	if WorkspaceImageTrustSourceTokenHeader != "Authorization" {
		t.Fatalf("trust source credential header changed: %q", WorkspaceImageTrustSourceTokenHeader)
	}
	if WorkspaceImageTrustSourceTokenEnv != "OPL_FABRIC_CAPABILITY_KEY" {
		t.Fatalf("trust source credential changed: %q", WorkspaceImageTrustSourceTokenEnv)
	}
	if WorkspaceImageTrustSourceURLEnv != "OPL_CONTROL_PLANE_URL" {
		t.Fatalf("trust source address variable changed: %q", WorkspaceImageTrustSourceURLEnv)
	}
	if WorkspaceImageTrustSourceURLDefault != "http://control-plane:8787" {
		t.Fatalf("trust source address default changed: %q", WorkspaceImageTrustSourceURLDefault)
	}
}

func TestWorkspaceImageReleaseActivationRequestCarriesRepositorySeparately(t *testing.T) {
	request := WorkspaceImageReleaseActivationRequest{
		ReleaseVersion:    "customer-agent-2026.11.15",
		ReleaseRepository: workspaceImageTrustRepository,
		ExpectedRevision:  1,
		Reason:            "customer agent build succeeded",
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["releaseRepository"] != workspaceImageTrustRepository {
		t.Fatalf("releaseRepository did not survive the wire: %#v", decoded)
	}
	if decoded["releaseVersion"] != request.ReleaseVersion {
		t.Fatalf("releaseVersion changed meaning: %#v", decoded)
	}
	var roundTripped WorkspaceImageReleaseActivationRequest
	if err := json.Unmarshal(encoded, &roundTripped); err != nil {
		t.Fatal(err)
	}
	if roundTripped.ReleaseRepository != workspaceImageTrustRepository {
		t.Fatalf("releaseRepository round trip lost the repository: %#v", roundTripped)
	}
}

func TestValidWorkspaceImageRepositoryAcceptsOnlyRepositoryShapes(t *testing.T) {
	for _, value := range []string{
		workspaceImageTrustRepository,
		workspaceImageTrustOtherRepository,
		"registry.example/opl/workspace",
		"ghcr.io/gaofeng21cn/opl-cloud",
	} {
		if !ValidWorkspaceImageRepository(value) {
			t.Fatalf("repository rejected: %q", value)
		}
	}
	for _, value := range []string{
		"",
		"   ",
		"registry.example",
		"registry.example/OPL/Workspace",
		"registry.example/opl/workspace@sha256:" + strings.Repeat("a", 64),
		"registry.example/opl:0/workspace",
		"registry.example/opl/workspace space",
	} {
		if ValidWorkspaceImageRepository(value) {
			t.Fatalf("repository accepted an unsafe value: %q", value)
		}
	}
}

func TestWorkspaceImageRepositoryExtractsTheRepositoryPart(t *testing.T) {
	reference := workspaceImageTrustReferenceForTest("a")
	if got := WorkspaceImageRepository(reference); got != workspaceImageTrustRepository {
		t.Fatalf("repository=%q want %q", got, workspaceImageTrustRepository)
	}
	if got := WorkspaceImageRepository("registry.example/opl/workspace:latest"); got != "" {
		t.Fatalf("a mutable reference produced a repository: %q", got)
	}
}

func TestDecodeWorkspaceImageTrustSourceRejectsUnprotectedShapes(t *testing.T) {
	reference := workspaceImageTrustReferenceForTest("b")
	for name, raw := range map[string]string{
		"malformed":          `{`,
		"unknown field":      `{"schemaVersion":1,"revision":2,"repositories":["` + workspaceImageTrustRepository + `"],"trusted":true}`,
		"wrong schema":       `{"schemaVersion":2,"revision":2,"repositories":[]}`,
		"revision zero":      `{"schemaVersion":1,"revision":0,"repositories":[]}`,
		"invalid repository": `{"schemaVersion":1,"revision":2,"repositories":["registry.example"]}`,
		"duplicate repo":     `{"schemaVersion":1,"revision":2,"repositories":["` + workspaceImageTrustRepository + `","` + workspaceImageTrustRepository + `"]}`,
		"invalid reference":  `{"schemaVersion":1,"revision":2,"references":["registry.example/opl/workspace:latest"]}`,
		"duplicate reference": `{"schemaVersion":1,"revision":2,"references":["` + reference + `","` + reference + `"]}`,
		"trailing content":   `{"schemaVersion":1,"revision":2,"repositories":[]} {"extra":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			if source, err := DecodeWorkspaceImageTrustSource(raw); err == nil {
				t.Fatalf("trust source unexpectedly accepted: %#v", source)
			}
		})
	}
}

func TestDecodeWorkspaceImageTrustSourceReadsAnEmptyInstallation(t *testing.T) {
	source, err := DecodeWorkspaceImageTrustSource("")
	if err != nil {
		t.Fatal(err)
	}
	if source.SchemaVersion != 1 || source.Revision != 1 || len(source.Repositories) != 0 {
		t.Fatalf("empty installation was not the default: %#v", source)
	}
	if source.TrustsImage(workspaceImageTrustReferenceForTest("c")) {
		t.Fatal("an installation that trusts nothing admitted an image")
	}
}

func TestWorkspaceImageTrustSourceTrustsRepositoryAndReference(t *testing.T) {
	source := WorkspaceImageTrustSource{
		SchemaVersion: 1,
		Revision:      2,
		Repositories:  []string{workspaceImageTrustRepository},
		References:    []string{"registry.example/opl/workspace@sha256:" + strings.Repeat("d", 64)},
	}
	if !source.TrustsRepository(workspaceImageTrustRepository) {
		t.Fatal("the registered repository was not trusted")
	}
	if source.TrustsRepository(workspaceImageTrustOtherRepository) {
		t.Fatal("an unregistered repository was trusted")
	}
	if !source.TrustsReference("registry.example/opl/workspace@sha256:" + strings.Repeat("d", 64)) {
		t.Fatal("the registered reference was not trusted")
	}
	if source.TrustsReference(workspaceImageTrustReferenceForTest("e")) {
		t.Fatal("an unregistered reference was trusted")
	}
	// A repository admits every image published to it: that is what makes the
	// publisher's own build output startable without a catalog edit.
	if !source.TrustsImage(workspaceImageTrustReferenceForTest("f")) {
		t.Fatal("an image published to a trusted repository was rejected")
	}
	if source.TrustsImage(workspaceImageTrustOtherRepository+"@sha256:"+strings.Repeat("a", 64)) {
		t.Fatal("an image outside every trusted repository was admitted")
	}
	if source.TrustsImage("") {
		t.Fatal("an empty reference was admitted")
	}
}

// The catalog keeps its exact image match. A repository-level question must
// never loosen the exact question the catalog already answers.
func TestWorkspaceImageReleaseCatalogContainsRepositoryStaysRepositoryGranular(t *testing.T) {
	installed := workspaceImageTrustReferenceForTest("a")
	other := workspaceImageTrustRepository + "@sha256:" + strings.Repeat("b", 64)
	catalog, err := DecodeWorkspaceImageReleaseCatalog(
		`{"schemaVersion":1,"releases":[{"version":"26.11.15","image":"`+installed+`"}]}`, installed)
	if err != nil {
		t.Fatal(err)
	}
	if !catalog.ContainsRepository(workspaceImageTrustRepository) {
		t.Fatal("the catalog did not report the release repository")
	}
	if catalog.ContainsRepository(workspaceImageTrustOtherRepository) {
		t.Fatal("the catalog reported a repository it does not publish to")
	}
	if !catalog.ContainsImage(installed) {
		t.Fatal("the catalog lost its exact image match")
	}
	if catalog.ContainsImage(other) {
		t.Fatal("a repository-level answer loosened the catalog's exact image match")
	}
}
