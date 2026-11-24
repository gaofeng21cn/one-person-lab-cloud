// Command deadcode enforces the machine-checkable dead-code boundary for the
// Cloud control plane using Go's official Rapid Type Analysis (RTA) call-graph
// analyzer (golang.org/x/tools/cmd/deadcode).
//
// It runs `go tool deadcode -json -test` anchored to the target package, keeps
// only production declarations (non-*_test.go files; test executables are
// included via -test so test callers keep test helpers in production files
// alive, while unused helpers in *_test.go files are ignored), and compares the
// unreachable set against the explicit allowlist.
//
// Usage:
//
//	go run ./internal/tools/deadcode
//
// Exits non-zero when any unexpected unreachable production function exists or
// when any allowlist entry is stale.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

const defaultServerImportPath = "opl-cloud/services/control-plane/internal/server"

type deadcodePackage struct {
	Name  string             `json:"Name"`
	Path  string             `json:"Path"`
	Funcs []deadcodeFunction `json:"Funcs"`
}

type deadcodeFunction struct {
	Name      string           `json:"Name"`
	Position  deadcodePosition `json:"Position"`
	Generated bool             `json:"Generated"`
	Marker    bool             `json:"Marker"`
}

type deadcodePosition struct {
	File string `json:"File"`
	Line int    `json:"Line"`
	Col  int    `json:"Col"`
}

func main() {
	pkgFilter := flag.String("pkg", defaultServerImportPath, "package import path (or directory) to enforce")
	allowlistPath := flag.String("allowlist", "internal/tools/deadcode/allowlist.txt", "allowlist file")
	targetPattern := flag.String("pattern", "./...", "package pattern passed to golang.org/x/tools/cmd/deadcode")
	flag.Parse()

	importPath, err := resolveImportPath(*pkgFilter)
	if err != nil {
		fail(err)
	}
	raw, err := runOfficialDeadcode(importPath, *targetPattern)
	if err != nil {
		fail(err)
	}
	dead, err := parseProductionDeadcode(raw, importPath)
	if err != nil {
		fail(err)
	}
	allow, err := readAllowlist(*allowlistPath)
	if err != nil {
		fail(err)
	}
	unexpected, stale := diffDeadcode(dead, allow)
	if len(unexpected) == 0 && len(stale) == 0 {
		fmt.Printf("deadcode: %s has no unreachable production functions (verified by golang.org/x/tools/cmd/deadcode)\n", importPath)
		return
	}
	for _, key := range unexpected {
		fmt.Printf("deadcode: unreachable production function: %s (%s)\n", key, dead[key])
	}
	for _, key := range stale {
		fmt.Printf("deadcode: allowlisted entry is no longer unreachable: %s\n", key)
	}
	fmt.Fprintf(os.Stderr, "deadcode: %d unreachable production function(s), %d stale allowlist entr(ies)\n", len(unexpected), len(stale))
	os.Exit(1)
}

func resolveImportPath(pkg string) (string, error) {
	trimmed := strings.TrimSpace(pkg)
	if trimmed == "" {
		return "", fmt.Errorf("empty -pkg value")
	}
	if !strings.HasPrefix(trimmed, ".") && !strings.HasPrefix(trimmed, "/") {
		return trimmed, nil
	}
	out, err := exec.Command("go", "list", "-f", "{{.ImportPath}}", trimmed).Output()
	if err != nil {
		return "", fmt.Errorf("resolve package %q: %w", trimmed, err)
	}
	resolved := strings.TrimSpace(string(out))
	if resolved == "" {
		return "", fmt.Errorf("resolve package %q: empty import path", trimmed)
	}
	return resolved, nil
}

func runOfficialDeadcode(importPath, pattern string) ([]byte, error) {
	filter := "^" + regexp.QuoteMeta(importPath) + "$"
	cmd := exec.Command("go", "tool", "deadcode", "-json", "-test", "-filter="+filter, pattern)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("go tool deadcode failed: %w: %s", err, msg)
		}
		return nil, fmt.Errorf("go tool deadcode failed: %w", err)
	}
	return stdout.Bytes(), nil
}

func parseProductionDeadcode(raw []byte, importPath string) (map[string]string, error) {
	dead := map[string]string{}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return dead, nil
	}
	var packages []deadcodePackage
	if err := json.Unmarshal(trimmed, &packages); err != nil {
		return nil, fmt.Errorf("decode deadcode json: %w", err)
	}
	for _, pkg := range packages {
		if pkg.Path != importPath {
			continue
		}
		for _, fn := range pkg.Funcs {
			if fn.Generated || fn.Marker || strings.HasSuffix(fn.Position.File, "_test.go") {
				continue
			}
			dead[fn.Name] = fmt.Sprintf("%s:%d", fn.Position.File, fn.Position.Line)
		}
	}
	return dead, nil
}

func diffDeadcode(dead map[string]string, allow map[string]bool) (unexpected, stale []string) {
	for key := range dead {
		if !allow[key] {
			unexpected = append(unexpected, key)
		}
	}
	for key := range allow {
		if _, ok := dead[key]; !ok {
			stale = append(stale, key)
		}
	}
	sort.Strings(unexpected)
	sort.Strings(stale)
	return unexpected, stale
}

// readAllowlist loads the explicit allowlist. Every line is one symbol key;
// a trailing "#" comment documents why the entry exists.
func readAllowlist(path string) (map[string]bool, error) {
	allow := map[string]bool{}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return allow, nil
		}
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if i := strings.Index(line, "\t"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line != "" {
			allow[line] = true
		}
	}
	return allow, scanner.Err()
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "deadcode:", err)
	os.Exit(2)
}
