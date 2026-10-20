package publicjson

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
)

// sessionWithUnnameablePermission is the production shape from #988: a session
// whose stored permission list contains a value the current contract cannot
// name. The named permissions remain valid and must survive the projection.
func sessionWithUnnameablePermission() *api.Session {
	return &api.Session{
		ActorId:     "actor-988",
		DisplayName: "Actor 988",
		TenantId:    proto.String("tenant-988"),
		Permissions: []api.AuthorizationActionEnum{
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWALLET,
			api.AuthorizationActionEnum(999),
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETSESSION,
		},
		CsrfToken: "csrf-988",
		ExpiresAt: timestamppb.New(time.Unix(1800000000, 0)),
	}
}

// An authorization action stored in a session can outlive the contract that
// named it: CloudIdentity returns the enum number, protojson renders it as a
// number, and the generated shape table has no public text for it. That must
// drop the one unnamed element, not the whole session body.
func TestSessionProjectionKeepsNamedPermissionsAndDropsUnnameableOnes(t *testing.T) {
	body, _, err := MarshalWithOmissions(sessionWithUnnameablePermission())
	if err != nil {
		t.Fatalf("session with a permission the contract cannot name must still project: %v", err)
	}
	var projected struct {
		ActorId     string   `json:"actorId"`
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal(body, &projected); err != nil {
		t.Fatal(err)
	}
	if projected.ActorId != "actor-988" {
		t.Fatalf("projected actor = %q, want actor-988", projected.ActorId)
	}
	want := []string{"getWallet", "getSession"}
	if !reflect.DeepEqual(projected.Permissions, want) {
		t.Fatalf("projected permissions = %v, want %v", projected.Permissions, want)
	}
}

// Leniency is an explicit choice at the call site, not a property of the codec.
// Every other caller keeps the loud failure, so a contract gap surfaces where it
// was produced instead of quietly shrinking a response.
func TestStrictMarshalStillRejectsAnUnnameablePermission(t *testing.T) {
	body, err := Marshal(sessionWithUnnameablePermission())
	if err == nil {
		t.Fatalf("strict projection must reject an unnameable permission, got body %s", body)
	}
	if !strings.Contains(err.Error(), "invalid permissions enum") {
		t.Fatalf("strict projection error = %v, want invalid permissions enum", err)
	}
}

// The dropped value must be visible to the caller so the contract gap can be
// recorded. Omitting it silently would hide which stored action is stale.
func TestSessionProjectionReportsTheDroppedPermissionValue(t *testing.T) {
	_, omissions, err := MarshalWithOmissions(sessionWithUnnameablePermission())
	if err != nil {
		t.Fatal(err)
	}
	want := []Omission{{Message: "opl.cloud.api.Session", Field: "permissions", Value: 999}}
	if !reflect.DeepEqual(omissions, want) {
		t.Fatalf("omissions = %+v, want %+v", omissions, want)
	}
}

// A permission added to the contract without a public name would reach a
// customer login as an unnamed value. The field is read from Session itself, so
// this check follows the contract if Session.permissions ever changes. The
// proto3 zero value is the absence of a permission rather than a permission, so
// the contract publishes no text for it and the projection drops it like any
// other unnameable value.
// actionsPendingPublicName lists actions that already route and authorize but
// whose public name has not landed yet. It is deliberately narrow: a value
// outside this map fails the test, so the gap cannot quietly grow.
var actionsPendingPublicName = map[protoreflect.EnumNumber]string{
	154: "#990 getUsageSummary",
	157: "#990 getWorkspaceFinancialReceipt",
}

func TestSessionPermissionEnumValuesHavePublicNames(t *testing.T) {
	field := (&api.Session{}).ProtoReflect().Descriptor().Fields().ByName("permissions")
	if field == nil || !field.IsList() || field.Enum() == nil {
		t.Fatal("Session.permissions must be a repeated enum")
	}
	enum := field.Enum()
	public := enumNames[enum.FullName()]
	if len(public) == 0 {
		t.Fatalf("generated shape table has no entries for %s", enum.FullName())
	}
	for i := 0; i < enum.Values().Len(); i++ {
		value := enum.Values().Get(i)
		if value.Number() == 0 {
			continue
		}
		if _, named := public[value.Number()]; !named {
			if issue, pending := actionsPendingPublicName[value.Number()]; pending {
				t.Logf("%s value %s (%d) is a known contract gap tracked by %s", enum.FullName(), value.Name(), value.Number(), issue)
				continue
			}
			t.Errorf("%s value %s (%d) has no public name in the generated shape table", enum.FullName(), value.Name(), value.Number())
		}
	}
}
