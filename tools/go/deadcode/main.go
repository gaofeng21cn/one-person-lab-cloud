// Command deadcode enforces the machine-checkable dead-code boundary for the
// Cloud production packages using Go's official Rapid Type Analysis (RTA)
// call-graph analyzer (golang.org/x/tools/cmd/deadcode).
//
// The boundary is declared, not implied. tools/go/deadcode/targets.txt lists
// the production packages the check covers, one Go import path per line. The
// tool reads that declared list, runs the analyzer over the whole module, keeps
// only production declarations (non-*_test.go files) in the declared packages,
// and compares the unreachable set against the explicit allowlist.
//
// The analyzer's reachability is computed over the whole module, so the tool
// runs from the repository root:
//
//	go run ./tools/go/deadcode
//
// A symbol key is package-qualified, because a bare name can collide across
// packages: a package-level symbol is "<import path>.<Symbol>" and a method is
// "<import path>.<ReceiverType>.<name>".
//
// The declared list must stay equal to the packages the tree actually
// contains. The derivation rule is: every package under opl-cloud/services/...
// and every package under opl-cloud/apps/console-bff/..., except a cmd/
// entry-point package, which is a program root rather than a subject, and
// except opl-cloud/packages/contracts/go/..., which is the generated contract
// client rather than an owner implementation. A package that leaves or joins
// that set without a matching declared entry fails the check, so the boundary
// cannot silently shrink.
//
// Exits non-zero when any unexpected unreachable production function exists,
// when any allowlist entry is stale, when an allowlist entry carries no reason,
// or when the declared target list no longer matches the tree.
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

const (
	modulePath       = "opl-cloud"
	servicesPrefix   = "opl-cloud/services/"
	consoleBFFPrefix = "opl-cloud/apps/console-bff"
	contractsPrefix  = "opl-cloud/packages/contracts/go"
)

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
	targetsPath := flag.String("targets", "tools/go/deadcode/targets.txt", "declared target package list")
	allowlistPath := flag.String("allowlist", "tools/go/deadcode/allowlist.txt", "allowlist file")
	targetPattern := flag.String("pattern", "./...", "package pattern passed to golang.org/x/tools/cmd/deadcode")
	flag.Parse()

	targets, err := readTargets(*targetsPath)
	if err != nil {
		fail(err)
	}
	if err := checkTargetsMatchTree(targets, *targetPattern); err != nil {
		fail(err)
	}
	raw, err := runOfficialDeadcode(targets, *targetPattern)
	if err != nil {
		fail(err)
	}
	dead, err := parseProductionDeadcode(raw, targets)
	if err != nil {
		fail(err)
	}
	allow, err := readAllowlist(*allowlistPath)
	if err != nil {
		fail(err)
	}
	unexpected, stale := diffDeadcode(dead, allow)
	if len(unexpected) == 0 && len(stale) == 0 {
		fmt.Printf("deadcode: %d declared target package(s) covered, no unreachable production function (verified by golang.org/x/tools/cmd/deadcode)\n", len(targets))
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

// targetPackage reports whether an import path is a production package the
// dead-code boundary covers. A cmd/ entry-point package is excluded because it
// is a program root, not a subject, and the generated contract client is
// excluded because it is not an owner implementation.
func targetPackage(importPath string) bool {
	if importPath == contractsPrefix || strings.HasPrefix(importPath, contractsPrefix+"/") {
		return false
	}
	if strings.HasPrefix(importPath, servicesPrefix) {
		remainder := strings.TrimPrefix(importPath, servicesPrefix)
		_, rest, found := strings.Cut(remainder, "/")
		if !found {
			return false
		}
		return !strings.HasPrefix(rest, "cmd/")
	}
	if importPath == consoleBFFPrefix {
		return true
	}
	if strings.HasPrefix(importPath, consoleBFFPrefix+"/") {
		rest := strings.TrimPrefix(importPath, consoleBFFPrefix+"/")
		return !strings.HasPrefix(rest, "cmd/")
	}
	return false
}

// readTargets loads the declared target package list. Every line is one Go
// import path; a trailing "#" comment documents the entry.
func readTargets(path string) ([]string, error) {
	lines, err := readLines(path)
	if err != nil {
		return nil, err
	}
	targets := []string{}
	seen := map[string]bool{}
	for _, line := range lines {
		target := line
		if i := strings.Index(target, "#"); i >= 0 {
			target = strings.TrimSpace(target[:i])
		}
		if target == "" {
			continue
		}
		if !strings.HasPrefix(target, modulePath+"/") {
			return nil, fmt.Errorf("declared target %q is not a package of the %s module", target, modulePath)
		}
		if seen[target] {
			return nil, fmt.Errorf("declared target %q is listed twice", target)
		}
		seen[target] = true
		targets = append(targets, target)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("declared target list %s is empty", path)
	}
	sort.Strings(targets)
	return targets, nil
}

// checkTargetsMatchTree fails when the declared list no longer equals the set
// of covered packages the tree actually contains. The declared list is the
// boundary; without this check a package could leave the boundary unnoticed.
func checkTargetsMatchTree(declared []string, pattern string) error {
	derived, err := deriveTargets(pattern)
	if err != nil {
		return err
	}
	return compareTargets(declared, derived)
}

// compareTargets reports every covered package that is not declared and every
// declared package that is not in the tree.
func compareTargets(declared, derived []string) error {
	declaredSet := map[string]bool{}
	for _, target := range declared {
		declaredSet[target] = true
	}
	derivedSet := map[string]bool{}
	for _, target := range derived {
		derivedSet[target] = true
	}
	missing := []string{}
	for _, target := range derived {
		if !declaredSet[target] {
			missing = append(missing, target)
		}
	}
	extra := []string{}
	for _, target := range declared {
		if !derivedSet[target] {
			extra = append(extra, target)
		}
	}
	if len(missing) == 0 && len(extra) == 0 {
		return nil
	}
	var report strings.Builder
	report.WriteString("declared target list does not match the packages the tree contains")
	for _, target := range missing {
		report.WriteString("\n  covered package is not declared: " + target)
	}
	for _, target := range extra {
		report.WriteString("\n  declared package is not in the tree: " + target)
	}
	return fmt.Errorf("%s", report.String())
}

// deriveTargets lists the packages the boundary covers from the tree itself.
func deriveTargets(pattern string) ([]string, error) {
	cmd := exec.Command("go", "list", "-f", "{{.ImportPath}}", pattern)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("go list failed: %w: %s", err, msg)
		}
		return nil, fmt.Errorf("go list failed: %w", err)
	}
	targets := []string{}
	for _, line := range strings.Split(stdout.String(), "\n") {
		importPath := strings.TrimSpace(line)
		if importPath == "" {
			continue
		}
		if targetPackage(importPath) {
			targets = append(targets, importPath)
		}
	}
	sort.Strings(targets)
	return targets, nil
}

func runOfficialDeadcode(targets []string, pattern string) ([]byte, error) {
	quoted := make([]string, 0, len(targets))
	for _, target := range targets {
		quoted = append(quoted, regexp.QuoteMeta(target))
	}
	filter := "^(" + strings.Join(quoted, "|") + ")$"
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

// parseProductionDeadcode keeps the unreachable production declarations of the
// declared target packages. A key is package-qualified: "<import path>.<Symbol>"
// for a package-level symbol and "<import path>.<ReceiverType>.<name>" for a
// method.
func parseProductionDeadcode(raw []byte, targets []string) (map[string]string, error) {
	dead := map[string]string{}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return dead, nil
	}
	var packages []deadcodePackage
	if err := json.Unmarshal(trimmed, &packages); err != nil {
		return nil, fmt.Errorf("decode deadcode json: %w", err)
	}
	declared := map[string]bool{}
	for _, target := range targets {
		declared[target] = true
	}
	for _, pkg := range packages {
		if !declared[pkg.Path] {
			continue
		}
		for _, fn := range pkg.Funcs {
			if fn.Generated || fn.Marker || strings.HasSuffix(fn.Position.File, "_test.go") {
				continue
			}
			dead[pkg.Path+"."+fn.Name] = fmt.Sprintf("%s:%d", fn.Position.File, fn.Position.Line)
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

// readAllowlist loads the explicit allowlist. Every line is one package-
// qualified symbol key followed by a "#" reason stating why the declaration
// cannot simply be deleted. A key without a reason is refused.
func readAllowlist(path string) (map[string]bool, error) {
	allow := map[string]bool{}
	lines, err := readLines(path)
	if err != nil {
		if os.IsNotExist(err) {
			return allow, nil
		}
		return nil, err
	}
	for _, line := range lines {
		key := line
		reason := ""
		if i := strings.Index(key, "#"); i >= 0 {
			reason = strings.TrimSpace(key[i+1:])
			key = strings.TrimSpace(key[:i])
		} else if i := strings.Index(key, "\t"); i >= 0 {
			reason = strings.TrimSpace(key[i+1:])
			key = strings.TrimSpace(key[:i])
		}
		if key == "" {
			continue
		}
		if reason == "" {
			return nil, fmt.Errorf("allowlist entry %q carries no reason why it cannot simply be deleted", key)
		}
		allow[key] = true
	}
	return allow, nil
}

// readLines returns the non-empty, trimmed lines of a file that are not whole
// line comments.
func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	lines := []string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "deadcode:", err)
	os.Exit(2)
}
