package publicjson

import (
	"bytes"
	"math"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
)

// This is the actual BFF serialization boundary: even values outside JavaScript's
// safe integer range must keep their exact decimal form through the public DTO.
func TestPersonalGatewayReadbackPreservesExactMoneyAndRedactedVocabulary(t *testing.T) {
	key := &api.PersonalGatewayKey{
		Id: "42", Name: "Personal", Status: api.PersonalGatewayKeyStatusEnum_PERSONAL_GATEWAY_KEY_STATUS_ENUM_DISABLED,
		Kind:           api.PersonalGatewayKeyKindEnum_PERSONAL_GATEWAY_KEY_KIND_ENUM_GENERAL,
		QuotaUsdMicros: math.MaxInt64, RateLimit5HUsdMicros: 9007199254740993,
	}
	raw, err := Marshal(key)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"quotaUSDMicros":"9223372036854775807"`, `"rateLimit5hUSDMicros":"9007199254740993"`, `"quotaUsedUSDMicros":"0"`, `"status":"disabled"`, `"kind":"general"`, `"manageable":false`, `"deletable":false`} {
		if !bytes.Contains(raw, []byte(expected)) {
			t.Fatalf("missing %s in %s", expected, raw)
		}
	}
	roundtrip := &api.PersonalGatewayKey{}
	if err := Unmarshal(raw, roundtrip); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(key, roundtrip) {
		t.Fatal("personal Key readback lost exact fields")
	}
	for _, secretField := range []string{"key", "secret", "accessToken"} {
		poisoned := append([]byte{}, raw[:len(raw)-1]...)
		poisoned = append(poisoned, []byte(`,"`+secretField+`":"must-not-cross-read-boundary"}`)...)
		if Unmarshal(poisoned, &api.PersonalGatewayKey{}) == nil {
			t.Fatalf("accepted secret field %s", secretField)
		}
	}
}

func TestPersonalGatewayUsageReadbackPreservesLargeTokenAndDurationValues(t *testing.T) {
	usage := &api.PersonalGatewayKeyUsage{CreatedAt: timestamppb.New(time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)), ApiKeyId: "42", RequestId: "request-1", Model: "model-1", InboundEndpoint: "/v1/messages", RequestType: "sync", InputTokens: 9007199254740993, ActualCostUsdMicros: 1234567, DurationMs: proto.Int64(0)}
	raw, err := Marshal(usage)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"inputTokens":"9007199254740993"`, `"actualCostUSDMicros":"1234567"`, `"durationMs":"0"`} {
		if !bytes.Contains(raw, []byte(expected)) {
			t.Fatalf("missing %s in %s", expected, raw)
		}
	}
	copy := &api.PersonalGatewayKeyUsage{}
	if err := Unmarshal(raw, copy); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(usage, copy) {
		t.Fatal("usage readback lost exact values")
	}
}
