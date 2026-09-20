package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/domain/application"
)

var (
	// errWorkspaceApplicationImageRequired reports a deployment description
	// without the image it targets.
	errWorkspaceApplicationImageRequired = errors.New("workspace_application_image_required")
	// errWorkspaceApplicationImageReferenceInvalid reports an image reference
	// that cannot be read back into host, repository and pinned digest.
	errWorkspaceApplicationImageReferenceInvalid = errors.New("workspace_application_image_reference_invalid")
	// errWorkspaceApplicationImageFactsUnavailable reports an image whose
	// declared runtime facts this installation cannot read: it is not on the
	// approved registry, it carries no manifest for the requested platform, or
	// its config cannot be read. The platform refuses instead of substituting
	// defaults an operator did not state.
	errWorkspaceApplicationImageFactsUnavailable = errors.New("workspace_application_image_facts_unavailable")
	// errWorkspaceApplicationRevisionInvalid reports a description that is not a
	// valid application revision even before the platform completes it.
	errWorkspaceApplicationRevisionInvalid = errors.New("invalid_application_revision")
	// errWorkspaceApplicationDeploymentInvalid reports a deployment command that
	// names neither a Workspace nor, after completion, what to deploy.
	errWorkspaceApplicationDeploymentInvalid = errors.New("invalid_application_deployment")
	// errWorkspaceApplicationImageNotApproved reports an image this installation
	// did not approve for deployment: another registry host, or a repository
	// outside the declared set.
	errWorkspaceApplicationImageNotApproved = errors.New("workspace_application_image_not_approved")
	// errWorkspaceApplicationEntryPortRequired reports a publishing deployment
	// whose entry port neither the operator nor the image declares. A published
	// application without an entry is not a successful deployment.
	errWorkspaceApplicationEntryPortRequired = errors.New("workspace_application_entry_port_required")
)

// workspaceApplicationImageFactsReader reads what an image declares about
// itself. The Control Plane registry catalog is the only implementation: the
// facts come from the same approved registry the deployment pulls from.
type workspaceApplicationImageFactsReader interface {
	imageFacts(ctx context.Context, host, namespace, repository, digest, platform string) (clients.WorkspaceRegistryImageFacts, error)
}

// completeWorkspaceApplicationDeploymentRevision turns an operator's "deploy
// this image" description into the immutable revision the deployment chain
// consumes.
//
// Two facts are the platform's to establish, so an operator never types them:
//
//   - The application identity. One application is one repository in this
//     Workspace's application slot, so a new digest of the same repository is a
//     new version that keeps the Workspace's data bindings and entry, while a
//     different repository is a different application with its own data
//     directory and origin.
//   - The run requirements the image itself declares: its TCP ports, the paths
//     it marks as data, and the process identity it expects. They are read from
//     the digest-pinned image config, never invented. Run facts the description
//     states are kept as stated.
//
// A description that already states its own identity is not completed: it is a
// publisher's immutable statement and is validated exactly as written.
//
// The version is derived from the completed description's content, so an exact
// replay resolves to the version already admitted and a real change is a new
// one. The description is validated as a whole afterwards, so a derived
// revision obeys exactly the same contract as a hand-written one.
func (app *controlPlaneServer) completeWorkspaceApplicationDeploymentRevision(ctx context.Context, reader workspaceApplicationImageFactsReader, workspaceID string, revision contracts.WorkspaceApplicationRevision) (contracts.WorkspaceApplicationRevision, error) {
	if strings.TrimSpace(revision.Image) == "" {
		return revision, errWorkspaceApplicationImageRequired
	}
	// A description that states its own identity is its publisher's immutable
	// statement: it is used exactly as written and never completed from the
	// image. Only an identity-less description is the platform's to complete,
	// which is the operator path of selecting an image and deploying it.
	if revision.ApplicationID != "" && revision.Version != "" {
		if err := contracts.ValidateWorkspaceApplicationRevision(revision); err != nil {
			return revision, errWorkspaceApplicationRevisionInvalid
		}
		return revision, nil
	}
	host, namespace, repository, digest, err := contracts.SplitWorkspaceImageReference(revision.Image)
	if err != nil {
		return revision, errWorkspaceApplicationImageReferenceInvalid
	}
	if revision.ApplicationID == "" {
		revision.ApplicationID = workspaceApplicationDerivedApplicationID(namespace, repository)
	}
	// Validate everything except the version before reading the registry, so a
	// description that is wrong on its own terms never becomes a registry
	// request.
	probe := revision
	if probe.Version == "" {
		probe.Version = workspaceApplicationDerivedVersionPlaceholder
	}
	if err := contracts.ValidateWorkspaceApplicationRevision(probe); err != nil {
		return revision, errWorkspaceApplicationRevisionInvalid
	}
	if workspaceApplicationNeedsImageFacts(revision) {
		declared, err := app.readWorkspaceApplicationImageFacts(ctx, reader, host, namespace, repository, digest, revision.Platform)
		if err != nil {
			return revision, err
		}
		revision = workspaceApplicationRevisionWithImageFacts(revision, declared)
	}
	if revision.ExposurePolicy != "cloud_private" {
		if _, published := contracts.WorkspaceApplicationEntryPort(revision); !published {
			return revision, errWorkspaceApplicationEntryPortRequired
		}
	}
	if revision.Version == "" {
		content, err := application.ContentDigest(revision)
		if err != nil {
			return revision, errWorkspaceApplicationRevisionInvalid
		}
		revision.Version = workspaceApplicationDerivedVersion(content)
	}
	if err := contracts.ValidateWorkspaceApplicationRevision(revision); err != nil {
		return revision, errWorkspaceApplicationRevisionInvalid
	}
	return revision, nil
}

func (app *controlPlaneServer) readWorkspaceApplicationImageFacts(ctx context.Context, reader workspaceApplicationImageFactsReader, host, namespace, repository, digest, platform string) (clients.WorkspaceRegistryImageFacts, error) {
	if reader == nil {
		return clients.WorkspaceRegistryImageFacts{}, errWorkspaceRegistryUnconfigured
	}
	declared, err := reader.imageFacts(ctx, host, namespace, repository, digest, platform)
	if err != nil {
		return clients.WorkspaceRegistryImageFacts{}, err
	}
	return declared, nil
}

// workspaceApplicationNeedsImageFacts reports whether the description leaves a
// run fact to the image. A description that states everything it needs is
// deployed as stated and never touches the registry.
func workspaceApplicationNeedsImageFacts(revision contracts.WorkspaceApplicationRevision) bool {
	if len(revision.Ports) == 0 && revision.EntryPort == "" {
		return true
	}
	if len(revision.PersistentMounts) == 0 || revision.Execution.UserID == nil {
		return true
	}
	return false
}

// workspaceApplicationRevisionWithImageFacts fills only the run facts the
// description left open. Stated values always win.
func workspaceApplicationRevisionWithImageFacts(revision contracts.WorkspaceApplicationRevision, declared clients.WorkspaceRegistryImageFacts) contracts.WorkspaceApplicationRevision {
	if len(revision.Ports) == 0 && revision.EntryPort == "" && len(declared.Ports) == 1 {
		revision.Ports = []contracts.WorkspaceApplicationPort{{Name: workspaceApplicationDerivedEntryPortName, Port: declared.Ports[0], Protocol: "TCP"}}
		revision.EntryPort = workspaceApplicationDerivedEntryPortName
	}
	if len(revision.PersistentMounts) == 0 {
		mounts := make([]contracts.WorkspaceApplicationMount, 0, len(declared.Volumes))
		for _, path := range declared.Volumes {
			mounts = append(mounts, contracts.WorkspaceApplicationMount{Name: workspaceApplicationDerivedMountName(path), MountPath: path})
		}
		if len(mounts) > 0 {
			revision.PersistentMounts = mounts
		}
	}
	if revision.Execution.UserID == nil {
		if userID, groupID, ok := workspaceApplicationImageProcessIdentity(declared.User); ok {
			revision.Execution.UserID, revision.Execution.GroupID = &userID, &groupID
		}
	}
	return revision
}

// workspaceApplicationImageProcessIdentity reads the image's declared process
// identity. The image states the user the process runs as; the platform does not
// run an image that names one as another user.
func workspaceApplicationImageProcessIdentity(user string) (int64, int64, bool) {
	user = strings.TrimSpace(user)
	if user == "" {
		return 0, 0, false
	}
	userText, groupText, hasGroup := strings.Cut(user, ":")
	userID, err := strconv.ParseInt(userText, 10, 64)
	if err != nil || userID <= 0 {
		return 0, 0, false
	}
	if !hasGroup {
		return userID, userID, true
	}
	groupID, err := strconv.ParseInt(groupText, 10, 64)
	if err != nil || groupID <= 0 {
		return 0, 0, false
	}
	return userID, groupID, true
}

const (
	// workspaceApplicationDerivedEntryPortName is the port name the platform
	// gives the one TCP port an image declares.
	workspaceApplicationDerivedEntryPortName = "http"
	// workspaceApplicationDerivedVersionPlaceholder stands in for the version
	// while the description is validated without its derived identity.
	workspaceApplicationDerivedVersionPlaceholder = "0"
	// workspaceApplicationImageUserSystem reports an image that runs as root:
	// the revised contract has no non-root identity to carry.
	workspaceApplicationImageUserSystem = "root"
	// workspaceApplicationDerivedRepositoryBudget bounds the readable part of a
	// derived identity so the appended digest suffix always fits one identity.
	workspaceApplicationDerivedRepositoryBudget = 40
)

// workspaceApplicationDerivedApplicationID names the application an image
// belongs to inside a Workspace. The repository is the application, so two
// digests of one repository are two versions of it, and a second repository is
// a second application. The sanitized name stays readable for operators, and
// the appended digest keeps two repositories that sanitize alike apart.
func workspaceApplicationDerivedApplicationID(namespace, repository string) string {
	name := strings.ToLower(repository)
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r == '-' || r == '.' || r == '_' || r == '/':
			return '-'
		default:
			return '-'
		}
	}, name)
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	name = strings.Trim(name, "-.")
	if len(name) > workspaceApplicationDerivedRepositoryBudget {
		name = strings.Trim(name[:workspaceApplicationDerivedRepositoryBudget], "-")
	}
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		name = "app-" + name
	}
	return name + "-" + stableID("workspace-application", namespace, repository)[:8]
}

// workspaceApplicationDerivedVersion names the immutable version of a derived
// description. It is the description's content digest, so the same image and
// the same run facts always resolve to the same version and any real change is
// a new one.
func workspaceApplicationDerivedVersion(content string) string {
	return "d" + content[:16]
}

// workspaceApplicationDerivedMountName names one persistent mount after the
// directory the image declares, so an operator reading the deployment sees the
// image's own path rather than an internal name.
func workspaceApplicationDerivedMountName(path string) string {
	trimmed := strings.Trim(path, "/")
	segment := trimmed
	if index := strings.LastIndex(trimmed, "/"); index >= 0 {
		segment = trimmed[index+1:]
	}
	name := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '-'
		}
	}, segment)
	name = strings.Trim(name, "-")
	if name == "" {
		name = "data"
	}
	if len(name) > 30 {
		name = strings.Trim(name[:30], "-")
	}
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		name = "data-" + name
	}
	return name
}

// writeWorkspaceApplicationDescriptionError maps a refused description onto its
// explicit result. A registry that cannot answer keeps its own registry error
// code, because "this installation cannot read that image" and "that
// description is wrong" are different operator actions.
func writeWorkspaceApplicationDescriptionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errWorkspaceRegistryUnconfigured):
		writeError(w, http.StatusServiceUnavailable, errWorkspaceRegistryUnconfigured.Error())
	case errors.Is(err, errWorkspaceApplicationImageRequired), errors.Is(err, errWorkspaceApplicationImageReferenceInvalid),
		errors.Is(err, errWorkspaceApplicationImageFactsUnavailable), errors.Is(err, errWorkspaceApplicationImageNotApproved),
		errors.Is(err, errWorkspaceApplicationEntryPortRequired), errors.Is(err, errWorkspaceApplicationRevisionInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		var registryErr *clients.RegistryAPIError
		if errors.As(err, &registryErr) {
			writeWorkspaceRegistryError(w, err)
			return
		}
		writeError(w, http.StatusBadRequest, errWorkspaceApplicationRevisionInvalid.Error())
	}
}
