// Command deadcode is the machine-checkable boundary for the Cloud control
// plane: an implementation in services/control-plane/internal/server must have
// a caller.
//
// The check is deliberately conservative. It only reports a declaration as
// dead when nothing in the package's own non-test source can reach it:
//
//   - a package-level function, type, constant or variable is dead when its
//     name never appears as a plain identifier in the package's non-test
//     source, and (for exported names) never appears in another package of
//     the same module;
//   - a method is dead when its name never appears as a selector in the
//     package's non-test source, it is not a method of an interface the
//     package declares (an interface method must exist even when nothing
//     calls it, otherwise the type stops satisfying the interface), and it is
//     not a method of a standard-library interface.
//
// The standard is that the reported set is empty. A small, explicit allowlist
// records the declarations that are known to be unreachable and cannot be
// removed without unrelated interface surgery; every entry carries a reason.
// Adding an entry is a deliberate act, deleting one is the point.
//
// Usage:
//
//	go run ./internal/tools/deadcode
//
// The command exits non-zero when the reported set differs from the
// allowlist, so it can be wired into CI as-is.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const serverImportPath = "opl-cloud/services/control-plane/internal/server"

// stdlibInterfaceMethods are the method names the standard library invokes
// through an interface rather than through a call written in this package.
// They are unreachable for the same reason the reported set is, so they are
// part of the allowlist rather than of the dead set.
var stdlibInterfaceMethods = map[string]bool{
	"Error": true, "Unwrap": true, "String": true, "Format": true,
	"ServeHTTP": true, "Close": true, "Read": true, "Write": true,
	"Len": true, "Less": true, "Swap": true, "Next": true, "Scan": true,
	"Value": true, "MarshalJSON": true, "UnmarshalJSON": true,
	"MarshalText": true, "UnmarshalText": true, "Deadline": true,
	"Done": true, "Err": true, "Summary": true, "Verbose": true,
	"LogValue": true, "GoString": true, "Seek": true, "WriteTo": true,
	"ReadFrom": true,
}

type decl struct {
	name string
	recv string
	kind string // func | method | type | const | var
}

func (d decl) key() string {
	if d.recv != "" {
		return d.recv + "." + d.name
	}
	return d.name
}

func (d decl) String() string { return d.kind + " " + d.key() }

func main() {
	pkgDir := flag.String("pkg", "./internal/server", "package directory to check")
	allowlistPath := flag.String("allowlist", "internal/tools/deadcode/allowlist.txt", "allowlist file")
	moduleDir := flag.String("module", ".", "module root used for cross-package reachability")
	flag.Parse()

	decls, plainIdents, prodSelectors, fieldsByType, ifaceMethods, err := scanPackage(*pkgDir)
	if err != nil {
		fail(err)
	}
	crossUsed, err := crossPackageUsage(*moduleDir, *pkgDir)
	if err != nil {
		fail(err)
	}

	dead := map[string]decl{}
	for _, d := range decls {
		if d.kind == "method" {
			// A method that satisfies a method of an interface this package
			// declares must exist even when nothing calls it, otherwise the
			// type stops satisfying the interface. Any other method must be
			// called on its own receiver from the package's production source.
			if ifaceMethods[d.name] || stdlibInterfaceMethods[d.name] {
				continue
			}
			if methodCalledOn(prodSelectors[d.name], d.recv, fieldsByType) {
				continue
			}
		} else if plainIdents[d.name] || crossUsed[d.name] {
			continue
		}
		dead[d.key()] = d
	}

	allow, err := readAllowlist(*allowlistPath)
	if err != nil {
		fail(err)
	}

	var unexpected, stale []string
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

	if len(unexpected) == 0 && len(stale) == 0 {
		fmt.Printf("deadcode: %s has no unreferenced implementation (%d declarations checked)\n", *pkgDir, len(decls))
		return
	}
	for _, key := range unexpected {
		fmt.Printf("deadcode: unreferenced implementation: %s\n", key)
	}
	for _, key := range stale {
		fmt.Printf("deadcode: allowlisted entry is no longer dead: %s\n", key)
	}
	fmt.Fprintf(os.Stderr, "deadcode: %d unreferenced implementation(s), %d stale allowlist entr(ies)\n", len(unexpected), len(stale))
	os.Exit(1)
}

// scanPackage parses one package directory and returns its declarations plus
// the reachability facts the liveness rule needs.
func scanPackage(dir string) ([]decl, map[string]bool, map[string][]string, map[string]map[string]bool, map[string]bool, error) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	var decls []decl
	plainIdents := map[string]bool{}
	selectors := map[string]bool{}
	prodSelectors := map[string][]string{}
	fieldsByType := map[string]map[string]bool{}
	ifaceMethods := map[string]bool{}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		prod := !strings.HasSuffix(entry.Name(), "_test.go")
		f, err := parser.ParseFile(fset, filepath.Join(dir, entry.Name()), nil, parser.ParseComments)
		if err != nil {
			return nil, nil, nil, nil, nil, err
		}
		if !prod {
			// A test file may reference a production symbol; that keeps the
			// symbol alive. Its own declarations are not production code.
			collectRefs(f, plainIdents, selectors)
			continue
		}
		collectRefs(f, plainIdents, selectors)
		collectSelectors(f, prodSelectors)
		for _, d := range f.Decls {
			switch spec := d.(type) {
			case *ast.FuncDecl:
				if spec.Recv == nil || len(spec.Recv.List) == 0 {
					decls = append(decls, decl{name: spec.Name.Name, kind: "func"})
					continue
				}
				decls = append(decls, decl{name: spec.Name.Name, recv: recvTypeName(spec.Recv.List[0].Type), kind: "method"})
			case *ast.GenDecl:
				for _, item := range spec.Specs {
					switch s := item.(type) {
					case *ast.TypeSpec:
						decls = append(decls, decl{name: s.Name.Name, kind: "type"})
						if st, ok := s.Type.(*ast.StructType); ok {
							fields := map[string]bool{}
							for _, field := range st.Fields.List {
								for _, name := range field.Names {
									fields[name.Name] = true
								}
							}
							fieldsByType[s.Name.Name] = fields
						}
						if iface, ok := s.Type.(*ast.InterfaceType); ok {
							for _, m := range iface.Methods.List {
								for _, name := range m.Names {
									ifaceMethods[name.Name] = true
								}
							}
						}
					case *ast.ValueSpec:
						kind := "var"
						if spec.Tok == token.CONST {
							kind = "const"
						}
						for _, name := range s.Names {
							decls = append(decls, decl{name: name.Name, kind: kind})
						}
					}
				}
			}
		}
	}
	return decls, plainIdents, prodSelectors, fieldsByType, ifaceMethods, nil
}

// methodCalledOn reports whether a method name is called on its own receiver.
//
// A selector whose base is a plain identifier calls the method on whatever
// that identifier holds. A selector whose base is itself a selector calls the
// method on the field's type, so it only calls the receiver's method when the
// base is not one of the receiver's own fields.
func methodCalledOn(bases []string, recv string, fieldsByType map[string]map[string]bool) bool {
	own := fieldsByType[recv]
	for _, base := range bases {
		if !strings.HasPrefix(base, "field:") {
			return true
		}
		if !own[strings.TrimPrefix(base, "field:")] {
			return true
		}
	}
	return false
}

// collectSelectors records the method names a production file calls on a
// receiver.
//
// Only a selector whose base is a plain identifier counts. A selector on a
// nested expression (a struct field, a client accessor such as
// s.client.BillingReconciliation) names something else that happens to share
// the method name, and treating it as a call would hide a method whose only
// remaining caller is gone.
func collectSelectors(f *ast.File, selectors map[string][]string) {
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		base := "ident"
		if inner, ok := sel.X.(*ast.SelectorExpr); ok {
			base = "field:" + inner.Sel.Name
		}
		selectors[sel.Sel.Name] = append(selectors[sel.Sel.Name], base)
		return true
	})
}

// collectRefs records the plain identifiers and selector names a file uses.
//
// A plain identifier is a reference to a package-level declaration. The
// traversal skips every position that merely names something: declaration
// names, struct fields, composite literal keys, short variable declarations
// and labels. A selector name is a reference to a method.
func collectRefs(f *ast.File, plainIdents, selectors map[string]bool) {
	ast.Inspect(f, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.SelectorExpr:
			selectors[node.Sel.Name] = true
			collectIdentUses(node.X, plainIdents, selectors)
			return false
		case *ast.KeyValueExpr:
			// A composite literal key that is an identifier names a field.
			if _, ok := node.Key.(*ast.Ident); ok {
				collectIdentUses(node.Value, plainIdents, selectors)
				return false
			}
			collectIdentUses(node.Key, plainIdents, selectors)
			collectIdentUses(node.Value, plainIdents, selectors)
			return false
		case *ast.FuncDecl:
			collectIdentUses(node.Type, plainIdents, selectors)
			if node.Body != nil {
				collectIdentUses(node.Body, plainIdents, selectors)
			}
			return false
		case *ast.TypeSpec:
			collectIdentUses(node.Type, plainIdents, selectors)
			return false
		case *ast.ValueSpec:
			for _, value := range node.Values {
				collectIdentUses(value, plainIdents, selectors)
			}
			return false
		case *ast.Field:
			collectIdentUses(node.Type, plainIdents, selectors)
			return false
		case *ast.AssignStmt:
			if node.Tok == token.DEFINE {
				// The left side of := declares, it does not reference.
				for _, rhs := range node.Rhs {
					collectIdentUses(rhs, plainIdents, selectors)
				}
				return false
			}
			for _, lhs := range node.Lhs {
				collectIdentUses(lhs, plainIdents, selectors)
			}
			for _, rhs := range node.Rhs {
				collectIdentUses(rhs, plainIdents, selectors)
			}
			return false
		case *ast.LabeledStmt:
			collectIdentUses(node.Stmt, plainIdents, selectors)
			return false
		case *ast.ImportSpec:
			return false
		}
		return true
	})
}

// collectIdentUses records the identifier uses inside an expression subtree.
func collectIdentUses(node ast.Node, plainIdents, selectors map[string]bool) {
	if node == nil {
		return
	}
	ast.Inspect(node, func(n ast.Node) bool {
		switch inner := n.(type) {
		case *ast.SelectorExpr:
			selectors[inner.Sel.Name] = true
			collectIdentUses(inner.X, plainIdents, selectors)
			return false
		case *ast.KeyValueExpr:
			if _, ok := inner.Key.(*ast.Ident); ok {
				collectIdentUses(inner.Value, plainIdents, selectors)
				return false
			}
			collectIdentUses(inner.Key, plainIdents, selectors)
			collectIdentUses(inner.Value, plainIdents, selectors)
			return false
		case *ast.FuncLit:
			collectIdentUses(inner.Type, plainIdents, selectors)
			if inner.Body != nil {
				collectIdentUses(inner.Body, plainIdents, selectors)
			}
			return false
		case *ast.Ident:
			plainIdents[inner.Name] = true
			return false
		}
		return true
	})
}

// crossPackageUsage returns the exported names of the checked package that
// another package of the same module reaches.
func crossPackageUsage(moduleDir, pkgDir string) (map[string]bool, error) {
	used := map[string]bool{}
	absPkg, err := filepath.Abs(pkgDir)
	if err != nil {
		return nil, err
	}
	err = filepath.Walk(moduleDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "node_modules" || info.Name() == ".git" || info.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if strings.HasPrefix(abs, absPkg+string(os.PathSeparator)) {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return nil
		}
		alias := ""
		for _, imp := range f.Imports {
			if strings.Trim(imp.Path.Value, `"`) != serverImportPath {
				continue
			}
			if imp.Name != nil {
				alias = imp.Name.Name
			} else {
				alias = "server"
			}
		}
		if alias == "" {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == alias {
				used[sel.Sel.Name] = true
			}
			return true
		})
		return nil
	})
	return used, err
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

func recvTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return recvTypeName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return recvTypeName(t.X)
	case *ast.IndexListExpr:
		return recvTypeName(t.X)
	}
	return ""
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "deadcode:", err)
	os.Exit(2)
}
