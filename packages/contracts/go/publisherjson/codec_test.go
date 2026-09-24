package publisherjson

import (
	"encoding/json"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"os"
	"testing"
)

func TestPublisherExamplesPreservePublicJSON(t *testing.T) {
	raw, err := os.ReadFile("../../../../docs/spec/target/contracts/publisher-contract.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct{ Examples []json.RawMessage }
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, example := range doc.Examples {
		var head struct{ Kind string }
		json.Unmarshal(example, &head)
		var m proto.Message
		if head.Kind == "runtime" {
			m = &api.RuntimePublisherContract{}
		} else {
			m = &api.WebuiPublisherContract{}
		}
		if err = Unmarshal(example, m); err != nil {
			t.Fatal(err)
		}
		out, err := Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		again := proto.Clone(m)
		proto.Reset(again)
		if err = Unmarshal(out, again); err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(m, again) {
			t.Fatal("publisher contract changed on public JSON roundtrip")
		}

	}
}

func TestPublisherJSONRejectsWrongWireVocabulary(t *testing.T) {
	for _, data := range []string{
		`{"schemaVersion":"RUNTIME_PUBLISHER_CONTRACT_SCHEMA_VERSION_ENUM_OPL_PUBLISHER_CONTRACT_V1"}`,
		`{"schemaVersion":1}`,
		`{"unknownProperty":true}`,
		`{"image":[]}`,
		`{"kind":"runtime"} {}`,
		`{"applicationAccess":{"mode":"unsupported"}}`,
	} {
		if err := Unmarshal([]byte(data), &api.RuntimePublisherContract{}); err == nil {
			t.Fatalf("accepted invalid public JSON: %s", data)
		}
	}
}

func TestPublisherPathPatternRejectsTraversal(t *testing.T) {
	c := NewSchemaCompiler()
	if err := c.AddResource("path.json", map[string]any{"type": "string", "pattern": `^(?!/)(?!.*(?:^|/)\.\.?(/|$))[A-Za-z0-9_./-]+$`}); err != nil {
		t.Fatal(err)
	}
	schema, err := c.Compile("path.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../escape", "src/../escape", "/absolute", "src/./file"} {
		if schema.Validate(path) == nil {
			t.Fatalf("accepted unsafe path %q", path)
		}
	}
	if err := schema.Validate("src/package"); err != nil {
		t.Fatal(err)
	}
}
