// Extract exact Go AST declarations and registered HTTP handlers; not inferred OpenAPI.
package main

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Field struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Tag  string `json:"tag,omitempty"`
}
type Type struct {
	Alias  string  `json:"alias,omitempty"`
	Name   string  `json:"name"`
	Source string  `json:"source"`
	Fields []Field `json:"fields"`
}
type Route struct {
	Inputs       []Field  `json:"inputs"`
	Outputs      []string `json:"outputs"`
	Pattern      string   `json:"pattern"`
	Registration string   `json:"registration"`
	Source       string   `json:"source"`
	Handler      string   `json:"handler"`
	Calls        []string `json:"calls"`
}
type Function struct {
	Name   string   `json:"name"`
	Source string   `json:"source"`
	Code   string   `json:"code"`
	Calls  []string `json:"calls"`
}
type Module struct {
	Routes     []Route    `json:"routes"`
	Types      []Type     `json:"types"`
	Functions  []Function `json:"functions"`
	Unresolved []string   `json:"unresolved"`
}

func main() {
	root := os.Args[1]
	fs := token.NewFileSet()
	out := map[string]*Module{}
	for _, module := range []string{"control-plane", "fabric", "ledger", "contracts"} {
		m := &Module{Routes: []Route{}, Types: []Type{}, Functions: []Function{}, Unresolved: []string{}}
		out[module] = m
		funcs := map[string][]Function{}
		routeCandidates := []Route{}
		constants := map[string][]string{}
		walkRoot := filepath.Join(root, "services", module)
		if module == "contracts" {
			walkRoot = filepath.Join(root, "packages/contracts/go")
		}
		filepath.Walk(walkRoot, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				panic(err)
			}
			if info.IsDir() {
				if info.Name() == "ent" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, e := parser.ParseFile(fs, path, nil, 0)
			if e != nil {
				panic(e)
			}
			rel, _ := filepath.Rel(root, path)
			text := func(n ast.Node) string { var b bytes.Buffer; printer.Fprint(&b, fs, n); return b.String() }
			source := func(n ast.Node) string { return rel + ":" + strconv.Itoa(fs.Position(n.Pos()).Line) }
			calls := func(n ast.Node) []string {
				set := map[string]bool{}
				ast.Inspect(n, func(n ast.Node) bool {
					c, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					switch v := c.Fun.(type) {
					case *ast.Ident:
						set[v.Name] = true
					case *ast.SelectorExpr:
						set[v.Sel.Name] = true
					}
					return true
				})
				a := []string{}
				for k := range set {
					a = append(a, k)
				}
				sort.Strings(a)
				return a
			}
			for _, decl := range f.Decls {
				if d, ok := decl.(*ast.GenDecl); ok && d.Tok == token.CONST {
					for _, sp := range d.Specs {
						if v, ok := sp.(*ast.ValueSpec); ok {
							for i, n := range v.Names {
								if i < len(v.Values) {
									if lit, ok := v.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
										val, _ := strconv.Unquote(lit.Value)
										constants[n.Name] = []string{val}
									}
								}
							}
						}
					}
				}
			}
			var eval func(ast.Expr, map[string][]string) []string
			eval = func(e ast.Expr, env map[string][]string) []string {
				switch v := e.(type) {
				case *ast.BasicLit:
					if v.Kind == token.STRING {
						a, _ := strconv.Unquote(v.Value)
						return []string{a}
					}
				case *ast.Ident:
					if a, ok := env[v.Name]; ok {
						return a
					}
					return constants[v.Name]
				case *ast.BinaryExpr:
					if v.Op == token.ADD {
						a, b := eval(v.X, env), eval(v.Y, env)
						out := []string{}
						for _, x := range a {
							for _, y := range b {
								out = append(out, x+y)
							}
						}
						return out
					}
				case *ast.CompositeLit:
					a := []string{}
					for _, x := range v.Elts {
						e, ok := x.(ast.Expr)
						if !ok {
							return nil
						}
						a = append(a, eval(e, env)...)
					}
					return a
				}
				return nil
			}
			for _, decl := range f.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					if d.Body == nil {
						continue
					}
					fn := Function{d.Name.Name, source(d), text(d), calls(d.Body)}
					funcs[d.Name.Name] = append(funcs[d.Name.Name], fn)
					ranges := []*ast.RangeStmt{}
					ast.Inspect(d.Body, func(n ast.Node) bool {
						if r, ok := n.(*ast.RangeStmt); ok {
							ranges = append(ranges, r)
						}
						return true
					})
					ast.Inspect(d.Body, func(n ast.Node) bool {
						c, ok := n.(*ast.CallExpr)
						if !ok {
							return true
						}
						sel, ok := c.Fun.(*ast.SelectorExpr)
						if !ok || (sel.Sel.Name != "HandleFunc" && sel.Sel.Name != "Handle") || len(c.Args) < 2 {
							return true
						}
						env := map[string][]string{}
						for _, r := range ranges {
							if r.Pos() <= c.Pos() && c.End() <= r.End() {
								if id, ok := r.Value.(*ast.Ident); ok {
									env[id.Name] = eval(r.X, env)
								}
							}
						}
						patterns := eval(c.Args[0], env)
						if len(patterns) == 0 {
							m.Unresolved = append(m.Unresolved, source(c)+" dynamic route "+text(c.Args[0]))
							return true
						}
						inputs := []Field{}
						outputs := []string{}
						ast.Inspect(c.Args[1], func(n ast.Node) bool {
							switch v := n.(type) {
							case *ast.ValueSpec:
								if v.Type != nil {
									for _, id := range v.Names {
										if id.Name == "input" || id.Name == "query" {
											inputs = append(inputs, Field{Name: id.Name, Type: text(v.Type), Tag: "named variable; exact decoder in handler"})
										}
									}
								}
							case *ast.IndexExpr:
								if id, ok := v.X.(*ast.Ident); ok && id.Name == "input" {
									if lit, ok := v.Index.(*ast.BasicLit); ok && lit.Kind == token.STRING {
										field, _ := strconv.Unquote(lit.Value)
										inputs = append(inputs, Field{Name: field, Type: "map value; validator in handler", Tag: "body input access"})
									}
								}
							case *ast.CallExpr:
								if fn, ok := v.Fun.(*ast.SelectorExpr); ok && len(v.Args) > 0 {
									kind := ""
									if fn.Sel.Name == "PathValue" {
										kind = "path"
									}
									if text(fn.X) == "r.Header" && fn.Sel.Name == "Get" {
										kind = "header"
									}
									if text(fn.X) == "r.URL.Query()" && fn.Sel.Name == "Get" {
										kind = "query"
									}
									if kind != "" {
										if lit, ok := v.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
											field, _ := strconv.Unquote(lit.Value)
											inputs = append(inputs, Field{Name: field, Type: "string", Tag: kind})
										}
									}
								}
								if fn, ok := v.Fun.(*ast.Ident); ok {
									switch fn.Name {
									case "writeJSON", "writeSourceEnvelope", "writeResult", "writeWorkspaceLaunchResult":
										outputs = append(outputs, text(v))
									}
								}
							}
							return true
						})
						for _, pattern := range patterns {
							routeCandidates = append(routeCandidates, Route{Pattern: pattern, Registration: d.Name.Name, Source: source(c), Handler: text(c.Args[1]), Calls: calls(c.Args[1]), Inputs: inputs, Outputs: outputs})
						}
						return true
					})
				case *ast.GenDecl:
					for _, sp := range d.Specs {
						ts, ok := sp.(*ast.TypeSpec)
						if !ok {
							continue
						}
						st, ok := ts.Type.(*ast.StructType)
						if !ok {
							if ts.Assign.IsValid() {
								m.Types = append(m.Types, Type{Name: ts.Name.Name, Source: source(ts), Alias: text(ts.Type), Fields: []Field{}})
							}
							continue
						}
						typ := Type{Name: ts.Name.Name, Source: source(ts), Fields: []Field{}}
						hasJSON := false
						for _, f := range st.Fields.List {
							tag := ""
							if f.Tag != nil {
								tag, _ = strconv.Unquote(f.Tag.Value)
								hasJSON = hasJSON || strings.Contains(tag, "json:")
							}
							names := []string{}
							for _, n := range f.Names {
								names = append(names, n.Name)
							}
							typ.Fields = append(typ.Fields, Field{strings.Join(names, ","), text(f.Type), tag})
						}
						if hasJSON {
							m.Types = append(m.Types, typ)
						}
					}
				}
			}
			return nil
		})
		// Restrict to constructors' statically reachable same-package route-registration functions.
		seeds := []string{"NewPersistentServer"}
		if module != "control-plane" {
			seeds = []string{"NewServerWithAuth"}
		}
		reached := map[string]bool{}
		queue := seeds
		for len(queue) > 0 {
			n := queue[0]
			queue = queue[1:]
			if reached[n] {
				continue
			}
			reached[n] = true
			for _, f := range funcs[n] {
				for _, c := range f.Calls {
					if _, ok := funcs[c]; ok {
						queue = append(queue, c)
					}
				}
			}
		}
		for _, r := range routeCandidates {
			if reached[r.Registration] {
				m.Routes = append(m.Routes, r)
			} else {
				m.Unresolved = append(m.Unresolved, r.Source+" unproven registration "+r.Pattern)
			}
		}
		// Keep reachable helper source for map-based DTOs and named handler delegations.
		helpers := map[string]bool{}
		for _, r := range m.Routes {
			for _, c := range r.Calls {
				helpers[c] = true
			}
		}
		for n := range helpers {
			for _, fn := range funcs[n] {
				m.Functions = append(m.Functions, fn)
			}
		}
		sort.Slice(m.Routes, func(i, j int) bool { return m.Routes[i].Pattern < m.Routes[j].Pattern })
		sort.Slice(m.Types, func(i, j int) bool { return m.Types[i].Source < m.Types[j].Source })
		sort.Slice(m.Functions, func(i, j int) bool { return m.Functions[i].Source < m.Functions[j].Source })
	}
	e := json.NewEncoder(os.Stdout)
	e.SetIndent("", "  ")
	if err := e.Encode(out); err != nil {
		panic(err)
	}
}
