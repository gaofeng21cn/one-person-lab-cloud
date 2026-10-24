// This test helper inventories literal net/http registration arguments with
// Go's parser. Response metadata and comments are not route registrations.
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

func main() {
	var filenames []string
	if err := json.NewDecoder(os.Stdin).Decode(&filenames); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	paths := map[string]bool{}
	for _, filename := range filenames {
		file, err := parser.ParseFile(token.NewFileSet(), filename, nil, parser.SkipObjectResolution)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			method, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (method.Sel.Name != "HandleFunc" && method.Sel.Name != "Handle") {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			pattern, err := strconv.Unquote(literal.Value)
			if err != nil {
				return true
			}
			if _, resource, found := strings.Cut(pattern, " "); found {
				pattern = resource
			}
			if strings.HasPrefix(pattern, "/") {
				paths[pattern] = true
			}
			return true
		})
	}
	result := make([]string, 0, len(paths))
	for resource := range paths {
		result = append(result, resource)
	}
	sort.Strings(result)
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
