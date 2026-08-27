package contracts

import (
	"strings"
	"testing"
)

func workspaceImageForTest(digit string) string {
	return "registry.example/opl/workspace@sha256:" + strings.Repeat(digit, 64)
}

func TestDecodeWorkspaceImageReleaseCatalogFallsBackToInstalledImage(t *testing.T) {
	installed := workspaceImageForTest("a")
	catalog, err := DecodeWorkspaceImageReleaseCatalog("", installed)
	if err != nil || catalog.SchemaVersion != 1 || len(catalog.Releases) != 1 || catalog.Releases[0].Version != "installed" || catalog.Releases[0].Image != installed {
		t.Fatalf("catalog=%#v err=%v", catalog, err)
	}
}

func TestDecodeWorkspaceImageReleaseCatalogAcceptsApprovedImmutableVersions(t *testing.T) {
	stable, rollback := workspaceImageForTest("b"), workspaceImageForTest("a")
	raw := `{"schemaVersion":1,"releases":[{"version":"26.8.26","image":"` + stable + `"},{"version":"26.8.4","image":"` + rollback + `"}]}`
	catalog, err := DecodeWorkspaceImageReleaseCatalog(raw, stable)
	if err != nil || !catalog.ContainsImage(stable) || !catalog.ContainsImage(rollback) {
		t.Fatalf("catalog=%#v err=%v", catalog, err)
	}
	if release, found := catalog.ReleaseForImage(rollback); !found || release.Version != "26.8.4" || WorkspaceImageDigest(release.Image) == "" {
		t.Fatalf("rollback release=%#v found=%v", release, found)
	}
}

func TestDecodeWorkspaceImageReleaseCatalogRejectsUnprotectedShapes(t *testing.T) {
	installed, other := workspaceImageForTest("b"), workspaceImageForTest("a")
	for name, raw := range map[string]string{
		"tag":               `{"schemaVersion":1,"releases":[{"version":"latest","image":"registry.example/opl/workspace:latest"}]}`,
		"missing installed": `{"schemaVersion":1,"releases":[{"version":"old","image":"` + other + `"}]}`,
		"duplicate version": `{"schemaVersion":1,"releases":[{"version":"same","image":"` + installed + `"},{"version":"same","image":"` + other + `"}]}`,
		"unknown field":     `{"schemaVersion":1,"releases":[{"version":"current","image":"` + installed + `","mutable":true}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if catalog, err := DecodeWorkspaceImageReleaseCatalog(raw, installed); err == nil {
				t.Fatalf("catalog unexpectedly accepted: %#v", catalog)
			}
		})
	}
}
