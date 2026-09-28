//go:build livebuild

package catalog

import (
	"os"
	"testing"
)

func TestNativeOMACandidateTransportValidatesExactManifestAndContent(t *testing.T) {
	path := os.Getenv("OPL_OMA_CANDIDATE_TRANSPORT")
	if path == "" {
		t.Skip("OPL_OMA_CANDIDATE_TRANSPORT is not set")
	}
	if _, err := testCandidateObjects(t).validateArchive(path); err != nil {
		t.Fatal(err)
	}
}
