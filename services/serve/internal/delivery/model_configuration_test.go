package delivery

// This file proves the frozen model configuration interface is resolved from Serve's
// own deployment row and the exact immutable release identity that row names, on
// Serve's real isolated database and migrations. It never re-derives which product
// combination a runtime is, and it never accepts the interface from a caller.

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"reflect"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
	"opl-cloud/services/serve/internal/tkeapply"
	"opl-cloud/services/serve/migrations"
)

// deploymentArtifactDigest is the immutable artifact the fixture deployment row
// froze, which the admitted CapabilityVersion must confirm exactly.
const deploymentArtifactDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

// modelConfigurationDatabase provisions Serve's real isolated database and installs
// its real migration entrypoint.
func modelConfigurationDatabase(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	dsn := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)
	h, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{
		AdminDSN: dsn, Owner: "serve", Database: "opl_serve",
		SchemaOwnerRole: "opl_serve_owner", WriterRole: "opl_serve_writer", RuntimeRole: "opl_serve_runtime",
	})
	if err != nil {
		t.Fatalf("provision isolated serve database: %v", err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	source, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	if err = h.Install(ctx, h.OwnerDSN, h.DatabaseName(), source); err != nil {
		t.Fatalf("install serve migrations: %v", err)
	}
	db, err := h.Open(ctx, h.RuntimeDSN, h.DatabaseName())
	if err != nil {
		t.Fatalf("open serve database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// unresolvableAuthorization is the authorizer of a test that resolves an interface
// and performs no authorized action.
func unresolvableAuthorization(context.Context, *api.CallContext, api.AuthorizationActionEnum, *api.AuthorizationResource, ownerservice.ResourceScope) error {
	return status.Error(codes.Unavailable, "this test authorizes nothing")
}

// capabilityNamingRelease serves one admitted CapabilityVersion that names the
// approved Runtime Release it was built against, through the real product client
// surface.
type capabilityNamingRelease struct {
	api.CapabilityProductServiceClient
	version *api.CapabilityVersion
}

func (f *capabilityNamingRelease) GetCapabilityVersion(context.Context, *api.GetCapabilityVersionRpcRequest, ...grpc.CallOption) (*api.CapabilityVersion, error) {
	return proto.Clone(f.version).(*api.CapabilityVersion), nil
}

// releaseCatalog serves one approved Runtime Release through the real Runtime
// Control product client surface.
type releaseCatalog struct {
	api.RuntimeControlProductServiceClient
	release *api.RuntimeVersion
}

func (f *releaseCatalog) ListRuntimeVersions(context.Context, *api.ListRuntimeVersionsRpcRequest, ...grpc.CallOption) (*api.RuntimeVersionPage, error) {
	return &api.RuntimeVersionPage{Items: []*api.RuntimeVersion{proto.Clone(f.release).(*api.RuntimeVersion)}}, nil
}

// declaredModelConfiguration is the one interface this Cloud executes.
func declaredModelConfiguration() *api.ModelConfigurationContract {
	return &api.ModelConfigurationContract{
		Protocol:     api.ModelConfigurationContractProtocolEnum_MODEL_CONFIGURATION_CONTRACT_PROTOCOL_ENUM_OPL_MODEL_CONFIG_V1,
		PortName:     "control",
		ApplyPath:    "/control/models",
		ReadbackPath: "/control/models",
		RequestFields: []api.ModelConfigurationContractRequestFieldsEnum{
			api.ModelConfigurationContractRequestFieldsEnum_MODEL_CONFIGURATION_CONTRACT_REQUEST_FIELDS_ENUM_VERSION,
			api.ModelConfigurationContractRequestFieldsEnum_MODEL_CONFIGURATION_CONTRACT_REQUEST_FIELDS_ENUM_SELECTIONS,
		},
		ReadbackFields: []api.ModelConfigurationContractReadbackFieldsEnum{
			api.ModelConfigurationContractReadbackFieldsEnum_MODEL_CONFIGURATION_CONTRACT_READBACK_FIELDS_ENUM_APPLIEDVERSION,
			api.ModelConfigurationContractReadbackFieldsEnum_MODEL_CONFIGURATION_CONTRACT_READBACK_FIELDS_ENUM_SELECTIONS,
		},
		AuthorizationSecretInputName: "control_token",
	}
}

func approvedReleaseWith(contract *api.ModelConfigurationContract) *api.RuntimeVersion {
	return &api.RuntimeVersion{
		Id: "rv-agent", Status: api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_APPROVED,
		PublisherContract: &api.RuntimePublisherContract{
			Image:                       &api.ArtifactReference{Repository: "registry.test/runtime", Digest: "sha256:2222222222222222222222222222222222222222222222222222222222222222"},
			ApplicationRevisionTemplate: &api.WorkspaceApplicationRevision{SchemaVersion: 1, ApplicationId: "opl-app", Version: "1", Platform: "linux/amd64", Image: "registry.test/runtime@sha256:2222222222222222222222222222222222222222222222222222222222222222"},
			ModelConfiguration:          contract,
		},
	}
}

// insertDeployment writes the one Serve-owned row the resolver reads. A NULL
// runtime_version_id is a built Agent; a non-empty one is the default OPL App.
func insertDeployment(t *testing.T, db *sql.DB, id, kind, capabilityVersionID, runtimeVersionID string) {
	t.Helper()
	var capability, release any
	if capabilityVersionID != "" {
		capability = capabilityVersionID
	}
	if runtimeVersionID != "" {
		release = runtimeVersionID
	}
	if _, err := db.Exec(`INSERT INTO serve.agent_deployments(id,workspace_id,capability_version_id,application_kind,runtime_version_id,artifact_digest,reference_claim_id,runtime_instance_id,operation_id,status,data_compatibility,execution_epoch) VALUES($1,'ws-models',$2,$3,$4,$5,'claim','rti-models','op-models','queued','{}',1)`, id, capability, kind, release, deploymentArtifactDigest); err != nil {
		t.Fatal(err)
	}
}

// TestServeResolvesTheModelConfigurationFromTheFrozenRelease proves the interface
// comes from the exact release identity the deployment froze: directly for the
// default App, and through the admitted Agent's own Runtime Release, refusing an
// Agent that names none.
func TestServeResolvesTheModelConfigurationFromTheFrozenRelease(t *testing.T) {
	db := modelConfigurationDatabase(t)
	ctx := context.Background()
	service, err := New(db, unresolvableAuthorization)
	if err != nil {
		t.Fatal(err)
	}
	command := &api.RuntimeDeployCommand{DeploymentId: "dep-agent"}

	insertDeployment(t, db, "dep-agent", "agent", "cv_1", "")
	service.Capability = &capabilityNamingRelease{version: &api.CapabilityVersion{Id: "cv_1", RuntimeVersionId: proto.String(""), Artifact: &api.ArtifactReference{Digest: deploymentArtifactDigest}}}
	if _, err = service.frozenModelConfiguration(ctx, nil, command); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("an Agent naming no release err=%v want a failed precondition", err)
	}

	service.Capability = &capabilityNamingRelease{version: &api.CapabilityVersion{Id: "cv_1", RuntimeVersionId: proto.String("rv-agent"), Artifact: &api.ArtifactReference{Digest: deploymentArtifactDigest}}}
	service.RuntimeReleases = &releaseCatalog{release: approvedReleaseWith(declaredModelConfiguration())}
	resolved, err := service.frozenModelConfiguration(ctx, nil, command)
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.Declared {
		t.Fatal("the declared interface was not resolved for the admitted Agent")
	}
	if !reflect.DeepEqual(resolved.Contract, declaredContract()) {
		t.Fatalf("contract=%+v want %+v", resolved.Contract, declaredContract())
	}

	// The default App names the release itself, with no CapabilityVersion involved.
	if _, err = db.Exec(`UPDATE serve.agent_deployments SET application_kind='opl_app', capability_version_id='', runtime_version_id='rv-agent' WHERE id='dep-agent'`); err != nil {
		t.Fatal(err)
	}
	service.Capability = nil
	if resolved, err = service.frozenModelConfiguration(ctx, nil, command); err != nil || !resolved.Declared {
		t.Fatalf("default App resolution=%+v err=%v", resolved, err)
	}

	// A release that declares no interface leaves the capability absent rather than
	// supplying a default.
	service.RuntimeReleases = &releaseCatalog{release: approvedReleaseWith(nil)}
	if resolved, err = service.frozenModelConfiguration(ctx, nil, command); err != nil || resolved.Declared {
		t.Fatalf("undeclared release=%+v err=%v", resolved, err)
	}
}

// TestServeResolvesNoModelConfigurationForAnUnknownDeployment proves an unknown
// deployment is refused instead of answered with an invented interface.
func TestServeResolvesNoModelConfigurationForAnUnknownDeployment(t *testing.T) {
	db := modelConfigurationDatabase(t)
	service, err := New(db, unresolvableAuthorization)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.frozenModelConfiguration(context.Background(), nil, &api.RuntimeDeployCommand{DeploymentId: "dep-missing"}); status.Code(err) != codes.NotFound {
		t.Fatalf("err=%v want not found", err)
	}
}

// declaredContract is the exact projection of the declared interface above: the
// protocol's wire name, the declared port and paths, the contract's own field names
// and the declared control credential input.
func declaredContract() tkeapply.ModelConfigurationContract {
	return tkeapply.ModelConfigurationContract{
		Protocol: "opl-model-config/v1", PortName: "control",
		ApplyPath: "/control/models", ReadbackPath: "/control/models",
		RequestFields: []string{"version", "selections"}, ReadbackFields: []string{"appliedVersion", "selections"},
		AuthorizationSecretInputName: "control_token",
	}
}
