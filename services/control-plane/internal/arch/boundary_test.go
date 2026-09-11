package arch

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestDomainImportBoundary keeps the domain layer pure: domain packages may
// import the standard library, the contracts module and sibling domain
// packages only. HTTP, SQL, process execution and this service's server,
// client and service-orchestration packages are forbidden.
func TestDomainImportBoundary(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve the boundary test location")
	}
	root := filepath.Join(filepath.Dir(file), "..", "domain")
	forbidden := map[string]bool{
		"net/http":          true,
		"net/http/httptest": true,
		"database/sql":      true,
		"os/exec":           true,
		"syscall":           true,
		"opl-cloud/services/control-plane/internal/server":       true,
		"opl-cloud/services/control-plane/internal/clients":      true,
		"opl-cloud/services/control-plane/internal/controlplane": true,
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		node, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, importSpec := range node.Imports {
			importPath := strings.Trim(importSpec.Path.Value, `"`)
			if forbidden[importPath] {
				t.Errorf("%s imports forbidden package %q", path, importPath)
				continue
			}
			if !strings.HasPrefix(importPath, "opl-cloud/") {
				continue
			}
			if strings.HasPrefix(importPath, "opl-cloud/packages/contracts/go") ||
				strings.HasPrefix(importPath, "opl-cloud/services/control-plane/internal/domain/") {
				continue
			}
			t.Errorf("%s imports non-domain module package %q", path, importPath)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
