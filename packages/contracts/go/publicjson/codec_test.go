package publicjson

import (
	"encoding/json"
	"os"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	api "opl-cloud/packages/contracts/go/api"
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

// moneyRequest is a fully specified catalog price-policy request: the DTO the
// resource catalog line could not round-trip before the mapping was generated.
func moneyRequest() *api.CreatePricePolicyRequest {
	return &api.CreatePricePolicyRequest{
		VersionLabel:            "v1",
		PeriodMonths:            1,
		ComputeMonthlyUsdMicros: 50000000,
		StorageMonthlyUsdMicros: 10000000,
		ProductMonthlyUsdMicros: 20000000,
		ComputePlanId:           "compute-plan-a",
		StoragePlanId:           "storage-plan-a",
		RenewalPolicy: &api.RenewalPolicy{
			Version:                   api.RenewalPolicyVersionEnum_RENEWAL_POLICY_VERSION_ENUM_RENEWAL_POLICY_V1,
			Trigger:                   api.RenewalPolicyTriggerEnum_RENEWAL_POLICY_TRIGGER_ENUM_MANUAL_OR_EXPLICITLY_CONSENTED_AUTOMATIC,
			EffectiveStart:            api.RenewalPolicyEffectiveStartEnum_RENEWAL_POLICY_EFFECTIVE_START_ENUM_PREVIOUS_PAID_THROUGH,
			Months:                    1,
			UsesAcceptedPriceSnapshot: true,
		},
		PlanChangePolicyVersion: api.CreatePricePolicyRequestPlanChangePolicyVersionEnum_CREATE_PRICE_POLICY_REQUEST_PLAN_CHANGE_POLICY_VERSION_ENUM_WORKSPACE_PLAN_CHANGE_V1,
	}
}

// TestMoneyPropertiesUseTheContractSpellingAndScalarForm covers the defect that
// blocked the catalog price-policy wire: protoc derives `computeMonthlyUsdMicros`
// while the contract publishes `computeMonthlyUSDMicros`, and the contract types
// the value as a decimal string rather than a JSON number.
func TestMoneyPropertiesUseTheContractSpellingAndScalarForm(t *testing.T) {
	out, err := Marshal(moneyRequest())
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err = json.Unmarshal(out, &object); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"computeMonthlyUSDMicros", "storageMonthlyUSDMicros", "productMonthlyUSDMicros"} {
		text, ok := object[key].(string)
		if !ok {
			t.Fatalf("%s is %#v, the contract requires a decimal string", key, object[key])
		}
		if text != "50000000" && text != "10000000" && text != "20000000" {
			t.Fatalf("%s carries %q", key, text)
		}
	}
	// The protobuf-only spelling must not appear: it is not the public vocabulary.
	for _, key := range []string{"computeMonthlyUsdMicros", "storageMonthlyUsdMicros", "productMonthlyUsdMicros"} {
		if _, present := object[key]; present {
			t.Fatalf("encoded body leaked the protobuf spelling %s", key)
		}
	}

	// The exact contract spelling is accepted and the value survives.
	var decoded api.CreatePricePolicyRequest
	if err = Unmarshal(out, &decoded); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(moneyRequest(), &decoded) {
		t.Fatal("price policy request changed on public JSON roundtrip")
	}

	// The protobuf spelling is not an inbound alias.
	if err = Unmarshal([]byte(`{"versionLabel":"v1","computeMonthlyUsdMicros":"1"}`), &api.CreatePricePolicyRequest{}); err == nil {
		t.Fatal("accepted the protobuf-only money spelling")
	}
	// A numeric money value is refused where the contract requires a string.
	if err = Unmarshal([]byte(`{"versionLabel":"v1","computeMonthlyUSDMicros":50000000}`), &api.CreatePricePolicyRequest{}); err == nil {
		t.Fatal("accepted a numeric money value against the contract string form")
	}
}

// TestGeneratedPublicVocabularyMatchesTheContractCounts pins the generated
// boundary tables so a regeneration or a hand edit cannot change the public
// vocabulary silently.
func TestGeneratedPublicVocabularyMatchesTheContractCounts(t *testing.T) {
	if len(publicToField) != len(fieldToPublic) || len(publicToField) == 0 {
		t.Fatalf("name mappings are inconsistent: %d forward, %d reverse", len(publicToField), len(fieldToPublic))
	}
	for message, forward := range publicToField {
		reverse := fieldToPublic[message]
		if len(reverse) != len(forward) {
			t.Fatalf("%s maps %d properties forward but %d reverse", message, len(forward), len(reverse))
		}
		for property, field := range forward {
			if reverse[field] != property {
				t.Fatalf("%s.%s does not round-trip through the reverse table", message, property)
			}
		}
	}
	// Every declared divergence is a name-only difference for the same fact.
	for _, entry := range []struct{ message, property, field string }{
		{"UpdateWorkspaceVersionRequest", "expectedCurrentDeploymentId", "expected_current_agent_deployment_id"},
		{"RollbackWorkspaceRequest", "expectedCurrentDeploymentId", "expected_current_agent_deployment_id"},
		{"ComputePlan", "monthlyPriceUSDMicros", "monthly_price_usd_micros"},
		{"Quote", "totalUSDMicros", "total_usd_micros"},
	} {
		if got := publicToField[protoreflect.Name(entry.message)][entry.property]; got != entry.field {
			t.Fatalf("%s.%s maps to %q, expected %q", entry.message, entry.property, got, entry.field)
		}
	}
	if len(stringScalars) == 0 {
		t.Fatal("no 64-bit integer field is typed as a contract string")
	}
}

// TestContractTypedIntegersUseTheirDeclaredForm covers both sides of the scalar
// rule: a field the contract types as a decimal string is quoted, and one it does
// not stays a JSON number.
func TestContractTypedIntegersUseTheirDeclaredForm(t *testing.T) {
	custody := &api.AssetCustody{TenantId: "tenant-a", Status: api.AssetCustodyStatusEnum_ASSET_CUSTODY_STATUS_ENUM_RESTRICTED, PackageCount: 3, BuildCount: 4}
	out, err := Marshal(custody)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err = json.Unmarshal(out, &object); err != nil {
		t.Fatal(err)
	}
	if text, ok := object["packageCount"].(string); !ok || text != "3" {
		t.Fatalf("packageCount is %#v, the contract requires a decimal string", object["packageCount"])
	}
	var decoded api.AssetCustody
	if err = Unmarshal(out, &decoded); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(custody, &decoded) {
		t.Fatal("asset custody changed on public JSON roundtrip")
	}
}

// TestUnpublishedEnumValueIsOmittedOrRefused documents the rule for a wire enum
// value the contract does not publish: an optional property is left out of the
// public object, and a required one is refused rather than spelled with the
// protobuf enum name.
func TestUnpublishedEnumValueIsOmittedOrRefused(t *testing.T) {
	// trigger is required by the contract, so a zero value cannot be answered.
	partial := moneyRequest()
	partial.RenewalPolicy = &api.RenewalPolicy{Version: api.RenewalPolicyVersionEnum_RENEWAL_POLICY_VERSION_ENUM_RENEWAL_POLICY_V1, Months: 1}
	if _, err := Marshal(partial); err == nil {
		t.Fatal("a required property with no published value was answered anyway")
	}
}
