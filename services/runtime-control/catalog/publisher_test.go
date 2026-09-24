package catalog

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publisherjson"
)

func TestPublisherAdmissionEncodingMatchesApprovedSchema(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/spec/target/contracts/publisher-contract.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schemaDocument any
	if err = json.Unmarshal(raw, &schemaDocument); err != nil {
		t.Fatal(err)
	}
	compiler := publisherjson.NewSchemaCompiler()
	if err = compiler.AddResource("publisher.json", schemaDocument); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("publisher.json")
	if err != nil {
		t.Fatal(err)
	}
	var examples struct{ Examples []json.RawMessage }
	json.Unmarshal(raw, &examples)
	for _, e := range examples.Examples {
		var head struct{ Kind string }
		json.Unmarshal(e, &head)
		var message proto.Message
		if head.Kind == "runtime" {
			message = &api.RuntimePublisherContract{}
		} else {
			message = &api.WebuiPublisherContract{}
		}
		if err = publisherjson.Unmarshal(e, message); err != nil {
			t.Fatal(err)
		}
		admitted, err := publisherjson.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		var value any
		decoder := json.NewDecoder(bytes.NewReader(admitted))
		decoder.UseNumber()
		if err = decoder.Decode(&value); err != nil {
			t.Fatal(err)
		}
		if err = schema.Validate(value); err != nil {
			t.Fatalf("%s public contract rejected: %v", head.Kind, err)
		}
	}
}
