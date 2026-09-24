package publisherjson

import (
	"github.com/dlclark/regexp2"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"time"
)

// NewSchemaCompiler supports JSON Schema's ECMA-262 patterns, including the
// path-traversal lookaheads in the admitted publisher and Package schemas.
func NewSchemaCompiler() *jsonschema.Compiler {
	c := jsonschema.NewCompiler()
	c.UseRegexpEngine(func(pattern string) (jsonschema.Regexp, error) {
		re, err := regexp2.Compile(pattern, regexp2.ECMAScript)
		if err != nil {
			return nil, err
		}
		re.MatchTimeout = 100 * time.Millisecond
		return boundedRegexp{re}, nil
	})
	return c
}

type boundedRegexp struct{ *regexp2.Regexp }

func (r boundedRegexp) MatchString(text string) bool {
	matched, err := r.Regexp.MatchString(text)
	return err == nil && matched
}
