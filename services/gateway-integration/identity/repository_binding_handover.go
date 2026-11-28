package identity

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// repositoryBindingHandoverSelect reads the frozen historical destinations
// together with the ownership facts that decide whether a destination can be
// attributed. The historical table lives in the Tenant database and keeps only
// SELECT for the runtime writer since migration 0004; the Gateway owner holds
// the only live destination mapping.
const repositoryBindingHandoverSelect = `SELECT b.tenant_id,b.registry_host,b.registry_namespace,b.repository,b.status,t.status,
 (SELECT count(*) FROM tenant.tenant_members o WHERE o.tenant_id=b.tenant_id AND o.role='owner' AND o.revoked_at IS NULL),
 (SELECT min(o.actor_id) FROM tenant.tenant_members o WHERE o.tenant_id=b.tenant_id AND o.role='owner' AND o.revoked_at IS NULL)
 FROM tenant.tenant_repository_bindings b JOIN tenant.tenants t ON t.id=b.tenant_id
 ORDER BY b.tenant_id`

// RepositoryBindingHandoverSummary reports what one handover pass did with the
// frozen historical destination table. It is operator evidence about history and
// the input of the startup gate: a residual row is a destination the owner does
// not hold, so the service refuses to serve while one exists.
type RepositoryBindingHandoverSummary struct {
	// Scanned counts every historical row the pass read.
	Scanned int
	// Moved counts rows whose destination the pass placed in the Gateway owner.
	Moved int
	// Already counts rows whose destination the Gateway owner already held.
	Already int
	// Released counts retired historical rows, which the pass did not move.
	Released int
	// ReleasedUnheld counts the released rows whose destination the Gateway
	// owner does not hold. A released row still records a destination, and the
	// retired runtime guard is gone, so such a destination is one any new
	// actor could take. It stays a release in the receipt and is a residual as
	// well.
	ReleasedUnheld int
	// Unattributable counts rows with no single registered owner.
	Unattributable int
	// Conflicting counts rows the Gateway owner cannot serve as recorded.
	Conflicting int
	// Failed counts rows the pass could not complete for another reason.
	Failed int
	// The reference lists name the rows an operator still has to resolve.
	// UnattributableRefs and ConflictingRefs are conclusions this pass reached;
	// FailedRefs are rows whose own move failed; ReleasedUnheldRefs are the
	// released destinations nobody holds.
	UnattributableRefs []string
	ConflictingRefs    []string
	FailedRefs         []string
	ReleasedUnheldRefs []string
	// MovedRefs is the receipt of what this pass moved. It names a Tenant, the
	// owner actor and the OCI destination. A destination is a deployment fact,
	// not a credential: no key, password or customer data is recorded here.
	MovedRefs []string
}

// handoverResidualRefLimit bounds how many rows one gate message names. The
// full lists stay in the summary and in the startup log; the message tells an
// operator how many rows need a decision and shows the first ones.
const handoverResidualRefLimit = 5

// residualRows counts the historical rows this pass could not attribute or
// serve. Every one of them is a destination the owner does not hold: a
// released row whose destination nobody holds is as unowned as an ambiguous
// one, and the retired runtime guard no longer protects either.
func (s RepositoryBindingHandoverSummary) residualRows() int {
	return s.Unattributable + s.Conflicting + s.Failed + s.ReleasedUnheld
}

// ResidualError reports the rows an operator still has to resolve, or nil when
// the handover left nothing behind.
//
// A residual is not a cosmetic report. The runtime fallback read is retired, so
// a historical destination nobody owns is held by nobody: the frozen table
// keeps it as provenance and the owner's unique index knows nothing about it,
// so a new actor could take that destination and then reach another Tenant's
// OCI artifacts. The service therefore refuses to start while a residual
// exists, instead of trusting the deployment order to have run the handover
// before the first login.
func (s RepositoryBindingHandoverSummary) ResidualError() error {
	if s.residualRows() == 0 {
		return nil
	}
	counts := fmt.Sprintf("%d unattributable, %d conflicting, %d failed, %d released without a holder", s.Unattributable, s.Conflicting, s.Failed, s.ReleasedUnheld)
	rows := "rows"
	if s.residualRows() == 1 {
		rows = "row"
	}
	return fmt.Errorf("repository binding handover left %d unresolved historical destination %s (%s); the Gateway owner refuses to serve while a destination is unowned: unattributable=[%s] conflicting=[%s] failed=[%s] released_unheld=[%s]",
		s.residualRows(), rows, counts,
		handoverResidualRefs(s.UnattributableRefs), handoverResidualRefs(s.ConflictingRefs),
		handoverResidualRefs(s.FailedRefs), handoverResidualRefs(s.ReleasedUnheldRefs))
}

// handoverResidualRefs renders the first rows of one residual list.
func handoverResidualRefs(refs []string) string {
	if len(refs) == 0 {
		return "none"
	}
	shown := refs
	if len(shown) > handoverResidualRefLimit {
		shown = shown[:handoverResidualRefLimit]
	}
	suffix := ""
	if len(refs) > len(shown) {
		suffix = fmt.Sprintf(" (+%d more)", len(refs)-len(shown))
	}
	return strings.Join(shown, "; ") + suffix
}

// repositoryBindingHandoverRow is one historical destination and the
// ownership facts that decide whether it can be attributed.
type repositoryBindingHandoverRow struct {
	TenantID          string
	RegistryHost      string
	RegistryNamespace string
	Repository        string
	Status            string
	TenantStatus      string
	OwnerCount        int
	OwnerActorID      string
}

// handoverMappingState is the Gateway owner's answer about one actor's live
// destination mapping.
type handoverMappingState int

const (
	// handoverMappingAbsent means the owner holds no mapping for the actor.
	handoverMappingAbsent handoverMappingState = iota
	// handoverMappingUsable means the owner holds a usable mapping.
	handoverMappingUsable
	// handoverMappingUnusable means the owner holds a mapping it refuses: it is
	// revoked, or its identity is unverified. It is not an absent mapping.
	handoverMappingUnusable
)

// HandoverRepositoryBindings moves the historical OCI destinations out of the
// frozen Tenant table and into the Gateway owner's single destination mapping.
//
// The pass is read-only on the historical table: every row stays exactly as it
// was, so a repeated pass changes no history. A destination is attributed only
// to a Tenant that is active and has exactly one registered owner; an ambiguous
// or released row is reported and never guessed. The Gateway owner keeps the
// only write path for a destination: an existing mapping is never replaced, and
// a conflict is reported instead of overwritten.
//
// A row the owner refuses for its own reasons is a conclusion this pass reports
// for an operator; it does not stop the pass. An unavailable owner store is
// different: the pass cannot be trusted to have completed, so it returns the
// error and the owner refuses to start on an incomplete handover.
//
// The pass is idempotent. Running it again over the same history moves
// nothing, creates no row, and reports the destinations it already holds.
//
// The pass reports; the startup gate decides. ResidualError turns the reported
// rows into a refusal to serve, because the runtime fallback read is retired
// and a destination nobody owns is a destination any new actor could take.
func (s *Service) HandoverRepositoryBindings(ctx context.Context) (RepositoryBindingHandoverSummary, error) {
	var summary RepositoryBindingHandoverSummary
	if s == nil || s.DB == nil {
		return summary, fmt.Errorf("tenant database connection required for the repository binding handover")
	}
	if s.GatewayStore == nil {
		return summary, fmt.Errorf("gateway database connection required for the repository binding handover")
	}
	rows, err := s.DB.QueryContext(ctx, repositoryBindingHandoverSelect)
	if err != nil {
		return summary, persistence(err)
	}
	history := []repositoryBindingHandoverRow{}
	for rows.Next() {
		var row repositoryBindingHandoverRow
		if err = rows.Scan(&row.TenantID, &row.RegistryHost, &row.RegistryNamespace, &row.Repository, &row.Status, &row.TenantStatus, &row.OwnerCount, &row.OwnerActorID); err != nil {
			rows.Close()
			return summary, persistence(err)
		}
		history = append(history, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return summary, persistence(err)
	}
	for _, row := range history {
		summary.Scanned++
		if err = s.handoverRepositoryBinding(ctx, &summary, row); err != nil {
			return summary, err
		}
	}
	return summary, nil
}

// handoverRepositoryBinding attributes one historical destination and moves it
// into the Gateway owner when the ownership facts are unambiguous.
func (s *Service) handoverRepositoryBinding(ctx context.Context, summary *RepositoryBindingHandoverSummary, row repositoryBindingHandoverRow) error {
	switch row.Status {
	case "retired":
		// A released reservation was not moved: the owner holds no destination
		// for it. The row still records a destination, and the retired runtime
		// guard no longer protects it, so a destination nobody holds is one any
		// new actor could take and reach another Tenant's OCI artifacts. The row
		// stays a release in the receipt, and it is a residual as well unless
		// the owner already holds that destination.
		summary.Released++
		_, held, err := s.GatewayStore.destinationHolder(ctx, row.RegistryHost, row.RegistryNamespace, row.Repository)
		if err != nil {
			summary.Failed++
			summary.FailedRefs = append(summary.FailedRefs, fmt.Sprintf("tenant=%s destination=%s/%s/%s error=%v", row.TenantID, row.RegistryHost, row.RegistryNamespace, row.Repository, err))
			if status.Code(err) == codes.Unavailable {
				// The owner store is unavailable, so this pass cannot be trusted
				// to have completed. The startup fails on it.
				return err
			}
			return nil
		}
		if held {
			// The owner holds the destination, so no new actor can take it.
			return nil
		}
		summary.ReleasedUnheld++
		summary.ReleasedUnheldRefs = append(summary.ReleasedUnheldRefs, fmt.Sprintf("tenant=%s registered_owners=%d destination=%s/%s/%s reason=destination_unheld", row.TenantID, row.OwnerCount, row.RegistryHost, row.RegistryNamespace, row.Repository))
		return nil
	case "reserved", "active":
	default:
		summary.Failed++
		summary.FailedRefs = append(summary.FailedRefs, fmt.Sprintf("tenant=%s destination=%s/%s/%s status=%s", row.TenantID, row.RegistryHost, row.RegistryNamespace, row.Repository, row.Status))
		return nil
	}
	if row.TenantStatus != "active" {
		summary.Unattributable++
		summary.UnattributableRefs = append(summary.UnattributableRefs, fmt.Sprintf("tenant=%s tenant_status=%s destination=%s/%s/%s", row.TenantID, row.TenantStatus, row.RegistryHost, row.RegistryNamespace, row.Repository))
		return nil
	}
	if row.OwnerCount != 1 || row.OwnerActorID == "" {
		summary.Unattributable++
		summary.UnattributableRefs = append(summary.UnattributableRefs, fmt.Sprintf("tenant=%s registered_owners=%d destination=%s/%s/%s", row.TenantID, row.OwnerCount, row.RegistryHost, row.RegistryNamespace, row.Repository))
		return nil
	}
	owner := row.OwnerActorID
	existing, state, err := s.handoverDestination(ctx, owner)
	if err != nil {
		summary.Failed++
		summary.FailedRefs = append(summary.FailedRefs, fmt.Sprintf("tenant=%s actor=%s destination=%s/%s/%s error=%v", row.TenantID, owner, row.RegistryHost, row.RegistryNamespace, row.Repository, err))
		if status.Code(err) == codes.Unavailable {
			// The owner store is unavailable, so this pass cannot be trusted to
			// have completed. The startup fails on it rather than serving an
			// incomplete handover.
			return err
		}
		return nil
	}
	if state == handoverMappingUnusable && existing.RevokedAt.Valid {
		// The owner holds a revoked mapping and refuses it. It is not an absent
		// mapping: the handover reports it and never writes over it.
		summary.Conflicting++
		summary.ConflictingRefs = append(summary.ConflictingRefs, fmt.Sprintf("tenant=%s actor=%s destination=%s/%s/%s reason=revoked_mapping", row.TenantID, owner, row.RegistryHost, row.RegistryNamespace, row.Repository))
		return nil
	}
	held := state != handoverMappingAbsent && existing.RegistryHost == row.RegistryHost &&
		existing.RegistryNamespace == row.RegistryNamespace && existing.Repository == row.Repository
	// The Gateway owner's own storage action places the destination. It keeps an
	// existing destination and never replaces a different one, so the handover
	// cannot overwrite a live mapping. An unusable identity is not an absent
	// mapping either: the owner's action backfills it instead of inventing one.
	bound, err := s.GatewayStore.bindAuthenticatedUser(ctx, owner, owner, row.RegistryHost, row.RegistryNamespace, row.Repository)
	if err != nil {
		switch status.Code(err) {
		case codes.AlreadyExists, codes.FailedPrecondition:
			// The mapping exists and cannot take this destination: another subject
			// owns the identity, the mapping is revoked, or the destination is
			// already held by a different actor. It is reported, never replaced.
			summary.Conflicting++
			summary.ConflictingRefs = append(summary.ConflictingRefs, fmt.Sprintf("tenant=%s actor=%s destination=%s/%s/%s reason=%s", row.TenantID, owner, row.RegistryHost, row.RegistryNamespace, row.Repository, status.Convert(err).Message()))
			return nil
		default:
			summary.Failed++
			summary.FailedRefs = append(summary.FailedRefs, fmt.Sprintf("tenant=%s actor=%s destination=%s/%s/%s error=%v", row.TenantID, owner, row.RegistryHost, row.RegistryNamespace, row.Repository, err))
			if status.Code(err) == codes.Unavailable {
				return err
			}
			return nil
		}
	}
	if err := bound.usableIdentity(); err != nil {
		// The owner still refuses the mapping it holds for this actor, so the
		// destination is not one the actor can be served. It is reported for an
		// operator instead of counted as moved.
		summary.Conflicting++
		summary.ConflictingRefs = append(summary.ConflictingRefs, fmt.Sprintf("tenant=%s actor=%s destination=%s/%s/%s reason=unusable_mapping", row.TenantID, owner, row.RegistryHost, row.RegistryNamespace, row.Repository))
		return nil
	}
	if bound.RegistryHost != row.RegistryHost || bound.RegistryNamespace != row.RegistryNamespace || bound.Repository != row.Repository {
		// The owner kept a different destination. The handover reports it and
		// leaves the live mapping untouched.
		summary.Conflicting++
		summary.ConflictingRefs = append(summary.ConflictingRefs, fmt.Sprintf("tenant=%s actor=%s destination=%s/%s/%s reason=held_different_destination", row.TenantID, owner, row.RegistryHost, row.RegistryNamespace, row.Repository))
		return nil
	}
	if !bound.DestinationVerifiedAt.Valid {
		// The destination is recorded but its verification is missing, so the
		// owner's own read refuses the actor. The owner's storage action cannot
		// supply that verification, so an operator has to resolve it.
		summary.Conflicting++
		summary.ConflictingRefs = append(summary.ConflictingRefs, fmt.Sprintf("tenant=%s actor=%s destination=%s/%s/%s reason=destination_unverified", row.TenantID, owner, row.RegistryHost, row.RegistryNamespace, row.Repository))
		return nil
	}
	if held {
		summary.Already++
		return nil
	}
	summary.Moved++
	summary.MovedRefs = append(summary.MovedRefs, fmt.Sprintf("tenant=%s actor=%s destination=%s/%s/%s", row.TenantID, owner, row.RegistryHost, row.RegistryNamespace, row.Repository))
	return nil
}

// handoverDestination reads the owner's live destination mapping through the
// owner's own public read. It tells an absent mapping from a mapping the owner
// refuses, so an unusable mapping is never mistaken for an absent one.
func (s *Service) handoverDestination(ctx context.Context, actorID string) (UserExternalBinding, handoverMappingState, error) {
	binding, err := s.GatewayStore.ReadUserExternalBinding(ctx, actorID)
	switch status.Code(err) {
	case codes.OK:
		return binding, handoverMappingUsable, nil
	case codes.NotFound:
		return UserExternalBinding{}, handoverMappingAbsent, nil
	case codes.FailedPrecondition:
		// The mapping exists but the owner refuses it: it is revoked or its
		// identity is unverified.
		return binding, handoverMappingUnusable, nil
	default:
		return UserExternalBinding{}, handoverMappingAbsent, err
	}
}
