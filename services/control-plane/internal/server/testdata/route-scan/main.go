// This test helper inventories the net/http registration arguments of the
// scanned files with Go's parser, so a route inventory cannot be fooled by a
// comment, a response body string or a route-shaped constant.
//
// Unlike http-surface-scan.go, this scanner keeps the HTTP method next to the
// resource and resolves the computed patterns the Cloud owner actually
// registers: a package constant, a constant concatenated with a literal
// prefix, and a range variable over a slice of route descriptors. A pattern
// this scanner cannot resolve is reported as unresolved rather than dropped,
// because a dropped route would look like a route nobody registered.
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
)

type route struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	File   string `json:"file"`
	Line   int    `json:"line"`
}

type unresolved struct {
	Expression string `json:"expression"`
	File       string `json:"file"`
	Line       int    `json:"line"`
}

type inventory struct {
	Routes     []route     `json:"routes"`
	Unresolved []unresolved `json:"unresolved"`
}

var httpMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true,
	"HEAD": true, "OPTIONS": true, "CONNECT": true, "TRACE": true,
}

func main() {
	var filenames []string
	if err := json.NewDecoder(os.Stdin).Decode(&filenames); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	result := inventory{Routes: []route{}, Unresolved: []unresolved{}}
	for _, filename := range filenames {
		scan(filename, &result)
	}
	sort.Slice(result.Routes, func(i, j int) bool {
		if result.Routes[i].Path != result.Routes[j].Path {
			return result.Routes[i].Path < result.Routes[j].Path
		}
		return result.Routes[i].Method < result.Routes[j].Method
	})
	sort.Slice(result.Unresolved, func(i, j int) bool {
		if result.Unresolved[i].File != result.Unresolved[j].File {
			return result.Unresolved[i].File < result.Unresolved[j].File
		}
		return result.Unresolved[i].Line < result.Unresolved[j].Line
	})
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func scan(filename string, result *inventory) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, nil, parser.SkipObjectResolution)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	constants := packageStringConstants(file)
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (selector.Sel.Name != "HandleFunc" && selector.Sel.Name != "Handle") {
			return true
		}
		position := fset.Position(call.Args[0].Pos())
		patterns, resolved := resolvePatterns(call.Args[0], constants, file)
		if !resolved {
			result.Unresolved = append(result.Unresolved, unresolved{
				Expression: expressionText(call.Args[0]),
				File:       position.Filename,
				Line:       position.Line,
			})
			return true
		}
		for _, pattern := range patterns {
			method, resource := splitPattern(pattern)
			result.Routes = append(result.Routes, route{
				Method: method,
				Path:   resource,
				File:   position.Filename,
				Line:   position.Line,
			})
		}
		return true
	})
}

// splitPattern separates the optional HTTP method from the resource. A pattern
// without a method registers every method on that resource.
func splitPattern(pattern string) (string, string) {
	method, resource, found := strings.Cut(pattern, " ")
	if !found || !httpMethods[method] {
		return "", pattern
	}
	return method, resource
}

// packageStringConstants reads the package level string constants, so a route
// written as a named constant is inventoried with the value it registers.
func packageStringConstants(file *ast.File) map[string]string {
	constants := map[string]string{}
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.CONST {
			continue
		}
		for _, spec := range general.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for index, name := range value.Names {
				if index >= len(value.Values) {
					continue
				}
				literal, ok := value.Values[index].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				unquoted, err := strconv.Unquote(literal.Value)
				if err != nil {
					continue
				}
				constants[name.Name] = unquoted
			}
		}
	}
	return constants
}

// resolvePatterns turns a registration argument into the concrete route
// patterns it registers.
func resolvePatterns(expression ast.Expr, constants map[string]string, file *ast.File) ([]string, bool) {
	switch typed := expression.(type) {
	case *ast.BasicLit:
		if typed.Kind != token.STRING {
			return nil, false
		}
		unquoted, err := strconv.Unquote(typed.Value)
		if err != nil {
			return nil, false
		}
		return []string{unquoted}, true
	case *ast.Ident:
		value, ok := constants[typed.Name]
		if !ok {
			// A range variable over a slice of strings.
			return rangeStringValues(file, typed.Name)
		}
		return []string{value}, true
	case *ast.BinaryExpr:
		if typed.Op != token.ADD {
			return nil, false
		}
		left, leftOK := resolvePatterns(typed.X, constants, file)
		right, rightOK := resolvePatterns(typed.Y, constants, file)
		if !leftOK || !rightOK {
			return nil, false
		}
		combined := make([]string, 0, len(left)*len(right))
		for _, prefix := range left {
			for _, suffix := range right {
				combined = append(combined, prefix+suffix)
			}
		}
		return combined, true
	case *ast.SelectorExpr:
		// route.path over a slice of route descriptors.
		field := typed.Sel
		receiver, ok := typed.X.(*ast.Ident)
		if !ok {
			return nil, false
		}
		return descriptorFieldValues(file, receiver.Name, field.Name)
	}
	return nil, false
}

// descriptorFieldValues reads the values a range variable takes for one field
// of the route descriptor slice it ranges over. The descriptor is an anonymous
// struct, so the field index is read from the declared field order rather than
// assumed from the literal's position.
func descriptorFieldValues(file *ast.File, variable string, field string) ([]string, bool) {
	values := []string{}
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		loop, ok := node.(*ast.RangeStmt)
		if !ok {
			return true
		}
		if loop.Value == nil || loop.Value.(*ast.Ident).Name != variable {
			return true
		}
		composite, ok := loop.X.(*ast.CompositeLit)
		if !ok {
			return true
		}
		index, ok := descriptorFieldIndex(composite.Type, field)
		if !ok {
			return true
		}
		for _, element := range composite.Elts {
			item, ok := element.(*ast.CompositeLit)
			if !ok {
				continue
			}
			value, ok := descriptorFieldValue(item, index, field)
			if !ok {
				continue
			}
			values = append(values, value)
			found = true
		}
		return true
	})
	return values, found
}

// descriptorFieldIndex finds the position of a named field in the descriptor
// struct type.
func descriptorFieldIndex(typeExpression ast.Expr, field string) (int, bool) {
	structure, ok := typeExpression.(*ast.StructType)
	if !ok {
		array, isArray := typeExpression.(*ast.ArrayType)
		if !isArray {
			return 0, false
		}
		structure, ok = array.Elt.(*ast.StructType)
		if !ok {
			return 0, false
		}
	}
	for index, member := range structure.Fields.List {
		for _, name := range member.Names {
			if name.Name == field {
				return index, true
			}
		}
	}
	return 0, false
}

// descriptorFieldValue reads one field of a descriptor literal, whether the
// literal names the field or positions it.
func descriptorFieldValue(item *ast.CompositeLit, index int, field string) (string, bool) {
	for _, entry := range item.Elts {
		pair, ok := entry.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := pair.Key.(*ast.Ident)
		if !ok || key.Name != field {
			continue
		}
		literal, ok := pair.Value.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return "", false
		}
		unquoted, err := strconv.Unquote(literal.Value)
		if err != nil {
			return "", false
		}
		return unquoted, true
	}
	if index >= len(item.Elts) {
		return "", false
	}
	literal, ok := item.Elts[index].(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	unquoted, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", false
	}
	return unquoted, true
}

// rangeStringValues reads the values a range variable takes over a slice of
// string literals.
func rangeStringValues(file *ast.File, variable string) ([]string, bool) {
	values := []string{}
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		loop, ok := node.(*ast.RangeStmt)
		if !ok {
			return true
		}
		if loop.Value == nil || loop.Value.(*ast.Ident).Name != variable {
			return true
		}
		composite, ok := loop.X.(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, element := range composite.Elts {
			literal, ok := element.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				continue
			}
			unquoted, err := strconv.Unquote(literal.Value)
			if err != nil {
				continue
			}
			values = append(values, unquoted)
			found = true
		}
		return true
	})
	return values, found
}

// expressionText renders an expression back to source so an unresolved
// registration can be reported without guessing.
func expressionText(expression ast.Expr) string {
	switch typed := expression.(type) {
	case *ast.BasicLit:
		return typed.Value
	case *ast.Ident:
		return typed.Name
	case *ast.BinaryExpr:
		return expressionText(typed.X) + " + " + expressionText(typed.Y)
	case *ast.SelectorExpr:
		return expressionText(typed.X) + "." + expressionText(typed.Sel)
	}
	return "expression"
}
