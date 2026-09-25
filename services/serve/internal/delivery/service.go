// Package delivery owns the Serve product read surface over Serve's own
// database.
//
// Serve is the sole owner of per-Workspace Agent delivery state: the current
// selected Deployment, the Agent runtime instance with its readiness/access
// facts, and the access binding. This package reads exactly those owner-local
// rows and never substitutes another owner's fact for Serve's own delivery
// truth:
//
//   - a Fabric resource being ready is NOT application readiness; only Serve's
//     persisted runtime-instance observation proves the Agent is available;
//   - the Workspace does not store a deployment pointer or access copy here;
//   - an access URL is reported only for the current Agent whose own runtime
//     instance is ready, never composed from history.
//
// The delivery write path (Reserve/Deploy/route switching) is not implemented
// in this package. It still needs cross-owner capabilities that current source
// does not provide; see docs/status.md and docs/roadmap.md.
package delivery

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

// AuthorizeFunc is the thin adapter to the live CloudIdentity typed authority.
// It stores no decisions; the owner fails closed when it is absent or refuses.
type AuthorizeFunc func(context.Context, *api.CallContext, api.AuthorizationActionEnum, *api.AuthorizationResource, ownerservice.ResourceScope) error

// Service implements the ServeProductService read surface over Serve's own
// store.
type Service struct {
	api.UnimplementedServeProductServiceServer
	DB        *sql.DB
	Store     *ownerstore.Store
	Authorize AuthorizeFunc
}

// New binds the read surface to Serve's own database and the live authorizer.
func New(db *sql.DB, authorize AuthorizeFunc) (*Service, error) {
	if db == nil || authorize == nil {
		return nil, errors.New("serve database and live authorization are required")
	}
	store, err := ownerstore.New(db, "serve")
	if err != nil {
		return nil, err
	}
	return &Service{DB: db, Store: store, Authorize: authorize}, nil
}

// Register exposes the Serve product surface behind the shared identity
// interceptor.
func (s *Service) Register(server *ownerservice.Server) error {
	return server.RegisterGroup("ServeProductService", func(g *grpc.Server) {
		api.RegisterServeProductServiceServer(g, s)
	})
}

// Configure is the Serve owner process's product wiring: it opens the live
// CloudIdentity authorizer, binds the read surface to Serve's own database and
// registers the product group. It is the single wiring path the process and its
// tests both use, so readiness and the served surface cannot drift from what a
// test proves.
//
// A process without its own database has nothing to read and reports
// handlers-not-implemented, exactly like an owner with no product group.
func Configure(server *ownerservice.Server, database *ownerservice.Database, config ownerservice.Config) error {
	if database == nil {
		return ownerservice.ErrHandlersNotImplemented
	}
	authorizer, conn, err := ownerservice.AuthorizerFromConfig(config)
	if err != nil {
		return err
	}
	if conn != nil {
		if err := server.TrackCloser(conn); err != nil {
			return err
		}
	}
	service, err := New(database.DB(), authorizer.Authorize)
	if err != nil {
		return err
	}
	return service.Register(server)
}

func limit(n int32) int {
	if n <= 0 {
		return 50
	}
	if n > 100 {
		return 100
	}
	return int(n)
}

func upper(v string) string { return strings.ToUpper(strings.TrimSpace(v)) }

func dbError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return status.Error(codes.NotFound, "resource not found")
	}
	return status.Error(codes.Internal, "serve persistence failed")
}

// workspaceTenant resolves the owning tenant of a Workspace from Serve's own
// records: the Operation that created the delivery carries the tenant. A
// Workspace Serve has never delivered has no tenant here, so a tenant-scoped
// caller cannot claim it.
func (s *Service) workspaceTenant(ctx context.Context, workspaceID string) (string, bool, error) {
	var tenant sql.NullString
	err := s.DB.QueryRowContext(ctx, `
		SELECT o.tenant_id
		FROM serve.agent_deployments d
		JOIN serve.operations o ON o.id = d.operation_id
		WHERE d.workspace_id = $1 AND o.tenant_id IS NOT NULL
		ORDER BY d.created_at DESC, d.id DESC
		LIMIT 1`, workspaceID).Scan(&tenant)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return tenant.String, true, nil
}

// authorize resolves the persisted owner of the target Workspace, enforces that
// the caller's own scope covers it, then defers the live decision to
// CloudIdentity. A caller cannot widen its scope by presenting a different
// workspace id: Serve's persisted tenant decides, not the request.
func (s *Service) authorize(ctx context.Context, call *api.CallContext, action api.AuthorizationActionEnum, workspaceID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if call == nil || workspaceID == "" {
		return status.Error(codes.InvalidArgument, "authenticated call context and workspace are required")
	}
	if err := ownerservice.ValidateCallContext(ctx, call); err != nil {
		return err
	}
	tenant, known, err := s.workspaceTenant(ctx, workspaceID)
	if err != nil {
		return dbError(err)
	}
	callerTenant := call.GetScope().GetTenant().GetTenantId()
	platform := call.GetScope().GetPlatform() != nil
	// Serve's own delivery records are authoritative for the tenant of a Workspace
	// it has delivered; a caller whose scope names a different tenant is refused
	// before the live authority is even consulted.
	if known && !platform && callerTenant != tenant {
		return status.Error(codes.PermissionDenied, "workspace belongs to another tenant")
	}
	// A Workspace Serve has never delivered has no tenant recorded here, so Serve
	// does not invent one: the live CloudIdentity decision remains the authority
	// for that workspace, scoped to the caller's own tenant. Serve only supplies
	// the persisted tenant when it actually knows it.
	scopeTenant := tenant
	if !known && !platform {
		scopeTenant = callerTenant
	}
	resource := &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: &workspaceID}
	return s.Authorize(ctx, call, action, resource, ownerservice.ResourceScope{TenantID: scopeTenant})
}

// ListDeployments returns the Serve-owned delivery attempts for one Workspace.
func (s *Service) ListDeployments(ctx context.Context, r *api.ListDeploymentsRpcRequest) (*api.DeploymentPage, error) {
	if err := s.authorize(ctx, r.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTDEPLOYMENTS, r.GetWorkspaceId()); err != nil {
		return nil, err
	}
	n := limit(r.GetQueryLimit())
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, workspace_id, capability_version_id, runtime_instance_id, previous_deployment_id,
		       status, data_compatibility, created_at, updated_at
		FROM serve.agent_deployments
		WHERE workspace_id = $1 AND ($2 = '' OR id < $2)
		ORDER BY id DESC
		LIMIT $3`, r.GetWorkspaceId(), r.GetQueryCursor(), n+1)
	if err != nil {
		return nil, dbError(err)
	}
	defer rows.Close()
	page := &api.DeploymentPage{}
	for rows.Next() {
		item, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, dbError(err)
	}
	if len(page.Items) > n {
		page.Items = page.Items[:n]
		cursor := page.Items[n-1].GetId()
		page.NextCursor = &cursor
	}
	return page, nil
}

// GetDeployment returns one Serve-owned deployment for one Workspace.
func (s *Service) GetDeployment(ctx context.Context, r *api.GetDeploymentRpcRequest) (*api.Deployment, error) {
	if err := s.authorize(ctx, r.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETDEPLOYMENT, r.GetWorkspaceId()); err != nil {
		return nil, err
	}
	if strings.TrimSpace(r.GetDeploymentId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "deployment id is required")
	}
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, workspace_id, capability_version_id, runtime_instance_id, previous_deployment_id,
		       status, data_compatibility, created_at, updated_at
		FROM serve.agent_deployments
		WHERE workspace_id = $1 AND id = $2`, r.GetWorkspaceId(), r.GetDeploymentId())
	item, err := scanDeployment(row)
	if err != nil {
		return nil, err
	}
	return item, nil
}

// GetWorkspaceAccess reports the current Agent's access facts for one Workspace.
//
// The authentication mode is projected from the deployment descriptor's declared
// exposure policy, and credentials are reported as available only when the
// current Agent's own runtime instance is ready. A pending or failed runtime, or
// a Workspace whose Agent Serve has not activated, reports no application
// credentials: a provisioned resource is never substituted for a ready
// application.
func (s *Service) GetWorkspaceAccess(ctx context.Context, r *api.GetWorkspaceAccessRpcRequest) (*api.WorkspaceAccess, error) {
	if err := s.authorize(ctx, r.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEACCESS, r.GetWorkspaceId()); err != nil {
		return nil, err
	}
	access := &api.WorkspaceAccess{WorkspaceId: r.GetWorkspaceId()}
	mode, ready, url, err := s.currentAgentAccess(ctx, r.GetWorkspaceId())
	if err != nil {
		return nil, err
	}
	access.AuthenticationMode = mode
	if !ready {
		return access, nil
	}
	available := mode == api.WorkspaceAccessAuthenticationModeEnum_WORKSPACE_ACCESS_AUTHENTICATION_MODE_ENUM_APPLICATION_LOGIN
	access.ApplicationCredentialsAvailable = &available
	if url != "" {
		access.Url = url
	}
	return access, nil
}

// currentAgentAccess reads the current active deployment and its own runtime
// instance. The access URL and readiness come from Serve's runtime observation;
// a Fabric resource fact never appears here.
func (s *Service) currentAgentAccess(ctx context.Context, workspaceID string) (api.WorkspaceAccessAuthenticationModeEnum, bool, string, error) {
	var descriptorRaw []byte
	var url sql.NullString
	var statusText string
	err := s.DB.QueryRowContext(ctx, `
		SELECT i.deployment_descriptor, i.access_url, i.status
		FROM serve.agent_deployments d
		JOIN serve.agent_runtime_instances i ON i.deployment_id = d.id
		WHERE d.workspace_id = $1 AND d.status = 'active'
		ORDER BY d.created_at DESC, d.id DESC
		LIMIT 1`, workspaceID).Scan(&descriptorRaw, &url, &statusText)
	if errors.Is(err, sql.ErrNoRows) {
		return api.WorkspaceAccessAuthenticationModeEnum_WORKSPACE_ACCESS_AUTHENTICATION_MODE_ENUM_UNSPECIFIED, false, "", nil
	}
	if err != nil {
		return 0, false, "", dbError(err)
	}
	descriptor := &api.DeploymentDescriptor{}
	if err := publicjson.Unmarshal(descriptorRaw, descriptor); err != nil {
		return 0, false, "", status.Error(codes.DataLoss, "invalid persisted deployment descriptor")
	}
	mode := accessMode(descriptor.GetApplicationRevision().GetExposurePolicy())
	observedURL := strings.TrimSpace(url.String)
	ready := statusText == "ready" && url.Valid && observedURL != ""
	return mode, ready, observedURL, nil
}

// accessMode maps the descriptor's declared exposure policy to the access
// vocabulary. It is a projection of the deployment's own declaration, never a
// second policy writer.
func accessMode(policy api.WorkspaceApplicationRevisionExposurePolicyEnum) api.WorkspaceAccessAuthenticationModeEnum {
	switch policy {
	case api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_ANONYMOUS:
		return api.WorkspaceAccessAuthenticationModeEnum_WORKSPACE_ACCESS_AUTHENTICATION_MODE_ENUM_ANONYMOUS
	case api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION:
		return api.WorkspaceAccessAuthenticationModeEnum_WORKSPACE_ACCESS_AUTHENTICATION_MODE_ENUM_APPLICATION_LOGIN
	case api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_CLOUD_PRIVATE:
		return api.WorkspaceAccessAuthenticationModeEnum_WORKSPACE_ACCESS_AUTHENTICATION_MODE_ENUM_CLOUD_PRIVATE
	default:
		return api.WorkspaceAccessAuthenticationModeEnum_WORKSPACE_ACCESS_AUTHENTICATION_MODE_ENUM_UNSPECIFIED
	}
}

// rowScanner is the shared shape sql.Row and sql.Rows both satisfy.
type rowScanner interface{ Scan(dest ...any) error }

func scanDeployment(row rowScanner) (*api.Deployment, error) {
	var (
		out              api.Deployment
		runtimeID        sql.NullString
		previousID       sql.NullString
		rawCompatibility []byte
		statusText       string
		created, updated time.Time
	)
	if err := row.Scan(&out.Id, &out.WorkspaceId, &out.CapabilityVersionId, &runtimeID, &previousID,
		&statusText, &rawCompatibility, &created, &updated); err != nil {
		return nil, dbError(err)
	}
	if runtimeID.Valid {
		out.RuntimeInstanceId = &runtimeID.String
	}
	if previousID.Valid {
		out.PreviousDeploymentId = &previousID.String
	}
	statusValue, ok := api.DeploymentStatusEnum_value["DEPLOYMENT_STATUS_ENUM_"+upper(statusText)]
	if !ok {
		return nil, status.Error(codes.DataLoss, "invalid persisted deployment status")
	}
	out.Status = api.DeploymentStatusEnum(statusValue)
	compatibility := &api.DataCompatibility{}
	if err := publicjson.Unmarshal(rawCompatibility, compatibility); err != nil {
		return nil, status.Error(codes.DataLoss, "invalid persisted data compatibility")
	}
	out.DataCompatibility = compatibility
	out.CreatedAt = timestamppb.New(created.UTC())
	out.UpdatedAt = timestamppb.New(updated.UTC())
	return &out, nil
}
