package identity_test

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"opl-cloud/services/gateway-integration/identity"
)

// handoverHistory is one historical destination row to seed. The historical
// table is frozen for the runtime writer since migration 0004, so the fixture
// seeds it through the Tenant schema owner, the way a pre-existing fact was
// recorded before the freeze.
type handoverHistory struct {
	TenantID     string
	TenantStatus string
	Owners       []string
	Repository   string
	Status       string
}

// handoverSystem holds both owners' databases: the Tenant database that keeps
// the frozen history and the Gateway database that owns the live destination.
type handoverSystem struct {
	service *identity.Service
	tenant  *sql.DB
	gateway *sql.DB
}

// newHandoverSystem serves the real Tenant and Gateway owners over isolated
// PostgreSQL databases and seeds the historical rows before the handover runs.
func newHandoverSystem(t *testing.T, history []handoverHistory, gatewaySeed func(*sql.DB)) *handoverSystem {
	t.Helper()
	service, _, _ := bindingSystemWithHistory(t, func(db *sql.DB) {
		for _, row := range history {
			if _, err := db.ExecContext(t.Context(), `INSERT INTO tenant.tenants(id,name,status) VALUES($1,$2,$3)`, row.TenantID, row.TenantID, row.TenantStatus); err != nil {
				t.Fatal(err)
			}
			for i, owner := range row.Owners {
				if _, err := db.ExecContext(t.Context(), `INSERT INTO tenant.tenant_members(id,tenant_id,actor_id,role) VALUES($1,$2,$3,'owner')`, fmt.Sprintf("member-%s-%d", row.TenantID, i), row.TenantID, owner); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.ExecContext(t.Context(), `INSERT INTO tenant.tenant_repository_bindings(tenant_id,registry_host,registry_namespace,repository,status) VALUES($1,'uswccr.ccs.tencentyun.com','oplcloud',$2,$3)`, row.TenantID, row.Repository, row.Status); err != nil {
				t.Fatal(err)
			}
		}
	})
	gateway := service.GatewayStore.DB()
	if gatewaySeed != nil {
		gatewaySeed(gateway)
	}
	return &handoverSystem{service: service, tenant: service.DB, gateway: gateway}
}

// historicalRow renders the complete historical row, so a test compares every
// stored column rather than the destination alone.
func historicalRow(t *testing.T, db *sql.DB, tenantID string) string {
	t.Helper()
	var raw string
	if err := db.QueryRowContext(t.Context(), `SELECT row_to_json(b)::text FROM tenant.tenant_repository_bindings b WHERE b.tenant_id=$1`, tenantID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

// mappingRow renders the complete Gateway mapping row of one actor.
func mappingRow(t *testing.T, db *sql.DB, actorID string) string {
	t.Helper()
	var raw string
	if err := db.QueryRowContext(t.Context(), `SELECT row_to_json(m)::text FROM gateway.identity_mappings m WHERE m.actor_id=$1`, actorID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func mappingCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.identity_mappings`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// seedMapping plants one live Gateway mapping in the state a test needs: an
// absent, an unusable or a differently-holding destination mapping.
func seedMapping(t *testing.T, db *sql.DB, actorID, host, namespace, repository string, verified, destinationVerified, revoked bool) {
	t.Helper()
	stamp := func(present bool) any {
		if present {
			return time.Now().UTC()
		}
		return nil
	}
	destination := func(value string) any {
		if value == "" {
			return nil
		}
		return value
	}
	if _, err := db.ExecContext(t.Context(), `INSERT INTO gateway.identity_mappings(id,actor_id,sub2api_user_id,external_identity_ref,verified_at,registry_host,registry_namespace,repository,destination_verified_at,revoked_at)
 VALUES($1,$2,$2,$3,$4,$5,$6,$7,$8,$9)`, "mapping-"+actorID, actorID, "sub2api:user:"+actorID, stamp(verified),
		destination(host), destination(namespace), destination(repository), stamp(destinationVerified), stamp(revoked)); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryBindingHandoverMovesAttributableDestination(t *testing.T) {
	system := newHandoverSystem(t, []handoverHistory{
		{TenantID: "tenant-handover-move", TenantStatus: "active", Owners: []string{"201"}, Repository: "historical-path", Status: "reserved"},
	}, nil)
	before := historicalRow(t, system.tenant, "tenant-handover-move")

	summary, err := system.service.HandoverRepositoryBindings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// The destination itself is asserted first: a handover that did not move
	// the historical destination has to fail here.
	var actorID, subjectID, host, namespace, repository string
	var destinationVerified, verified, revoked sql.NullTime
	if err = system.gateway.QueryRowContext(t.Context(), `SELECT actor_id,sub2api_user_id,COALESCE(registry_host,''),COALESCE(registry_namespace,''),COALESCE(repository,''),destination_verified_at,verified_at,revoked_at
 FROM gateway.identity_mappings WHERE actor_id='201'`).Scan(&actorID, &subjectID, &host, &namespace, &repository, &destinationVerified, &verified, &revoked); err != nil {
		t.Fatalf("the historical destination did not reach the Gateway owner: %v", err)
	}
	if actorID != "201" || subjectID != "201" || host != "uswccr.ccs.tencentyun.com" || namespace != "oplcloud" || repository != "historical-path" {
		t.Fatalf("mapping=%s/%s/%s actor=%s subject=%s", host, namespace, repository, actorID, subjectID)
	}
	if !destinationVerified.Valid || !verified.Valid || revoked.Valid {
		t.Fatalf("destination_verified=%v verified=%v revoked=%v", destinationVerified, verified, revoked)
	}
	if summary.Scanned != 1 || summary.Moved != 1 || summary.Already != 0 || summary.Released != 0 || summary.Unattributable != 0 || summary.Conflicting != 0 || summary.Failed != 0 {
		t.Fatalf("summary=%+v", summary)
	}
	// The summary carries the receipt of what the pass moved.
	if len(summary.MovedRefs) != 1 || !strings.Contains(summary.MovedRefs[0], "tenant=tenant-handover-move") ||
		!strings.Contains(summary.MovedRefs[0], "actor=201") || !strings.Contains(summary.MovedRefs[0], "uswccr.ccs.tencentyun.com/oplcloud/historical-path") {
		t.Fatalf("moved refs=%v", summary.MovedRefs)
	}
	// The frozen history keeps every column it had, including its own
	// timestamps: the handover reads it and never writes it.
	if after := historicalRow(t, system.tenant, "tenant-handover-move"); after != before {
		t.Fatalf("historical row changed:\nbefore=%s\nafter=%s", before, after)
	}
}

func TestRepositoryBindingHandoverIsIdempotent(t *testing.T) {
	system := newHandoverSystem(t, []handoverHistory{
		{TenantID: "tenant-handover-again", TenantStatus: "active", Owners: []string{"201"}, Repository: "historical-path", Status: "active"},
	}, nil)

	first, err := system.service.HandoverRepositoryBindings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if first.Moved != 1 {
		t.Fatalf("first pass=%+v", first)
	}
	mappingBefore := mappingRow(t, system.gateway, "201")
	historyBefore := historicalRow(t, system.tenant, "tenant-handover-again")

	second, err := system.service.HandoverRepositoryBindings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if second.Scanned != 1 || second.Moved != 0 || second.Already != 1 || second.Released != 0 || second.Unattributable != 0 || second.Conflicting != 0 || second.Failed != 0 {
		t.Fatalf("second pass=%+v", second)
	}
	if len(second.MovedRefs) != 0 {
		t.Fatalf("the replay moved rows again: %v", second.MovedRefs)
	}
	if after := mappingRow(t, system.gateway, "201"); after != mappingBefore {
		t.Fatalf("mapping changed on replay:\nbefore=%s\nafter=%s", mappingBefore, after)
	}
	if after := historicalRow(t, system.tenant, "tenant-handover-again"); after != historyBefore {
		t.Fatalf("historical row changed on replay:\nbefore=%s\nafter=%s", historyBefore, after)
	}
	if count := mappingCount(t, system.gateway); count != 1 {
		t.Fatalf("replay added a mapping row: %d", count)
	}
}

func TestRepositoryBindingHandoverReportsConflictWithoutOverwrite(t *testing.T) {
	system := newHandoverSystem(t, []handoverHistory{
		{TenantID: "tenant-handover-conflict", TenantStatus: "active", Owners: []string{"201"}, Repository: "historical-path", Status: "reserved"},
	}, func(db *sql.DB) {
		seedMapping(t, db, "201", "other.example", "otherns", "other-repo", true, true, false)
	})
	mappingBefore := mappingRow(t, system.gateway, "201")
	historyBefore := historicalRow(t, system.tenant, "tenant-handover-conflict")

	summary, err := system.service.HandoverRepositoryBindings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Scanned != 1 || summary.Moved != 0 || summary.Already != 0 || summary.Conflicting != 1 || summary.Unattributable != 0 || summary.Failed != 0 {
		t.Fatalf("summary=%+v", summary)
	}
	if len(summary.ConflictingRefs) != 1 {
		t.Fatalf("conflicting refs=%v", summary.ConflictingRefs)
	}
	if !strings.Contains(summary.ConflictingRefs[0], "tenant=tenant-handover-conflict") || !strings.Contains(summary.ConflictingRefs[0], "actor=201") {
		t.Fatalf("conflicting ref=%q", summary.ConflictingRefs[0])
	}
	// The live mapping keeps the destination it already holds. The handover
	// reports the difference and never replaces it.
	if after := mappingRow(t, system.gateway, "201"); after != mappingBefore {
		t.Fatalf("conflicting mapping was overwritten:\nbefore=%s\nafter=%s", mappingBefore, after)
	}
	if after := historicalRow(t, system.tenant, "tenant-handover-conflict"); after != historyBefore {
		t.Fatalf("historical row changed:\nbefore=%s\nafter=%s", historyBefore, after)
	}
}

func TestRepositoryBindingHandoverReportsDestinationHeldByAnotherActor(t *testing.T) {
	system := newHandoverSystem(t, []handoverHistory{
		{TenantID: "tenant-handover-taken", TenantStatus: "active", Owners: []string{"201"}, Repository: "historical-path", Status: "reserved"},
	}, func(db *sql.DB) {
		// Another actor already holds the destination the historical row claims.
		// The owner refuses the second claim, so the handover reports it.
		seedMapping(t, db, "202", "uswccr.ccs.tencentyun.com", "oplcloud", "historical-path", true, true, false)
	})
	mappingBefore := mappingRow(t, system.gateway, "202")

	summary, err := system.service.HandoverRepositoryBindings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Scanned != 1 || summary.Moved != 0 || summary.Already != 0 || summary.Conflicting != 1 || summary.Unattributable != 0 || summary.Failed != 0 {
		t.Fatalf("summary=%+v", summary)
	}
	if len(summary.ConflictingRefs) != 1 {
		t.Fatalf("conflicting refs=%v", summary.ConflictingRefs)
	}
	if after := mappingRow(t, system.gateway, "202"); after != mappingBefore {
		t.Fatalf("the held destination was changed:\nbefore=%s\nafter=%s", mappingBefore, after)
	}
	// The owner of the historical row gets no mapping of its own.
	var ownerMappings int
	if err = system.gateway.QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.identity_mappings WHERE actor_id='201'`).Scan(&ownerMappings); err != nil {
		t.Fatal(err)
	}
	if ownerMappings != 0 {
		t.Fatalf("a contested destination produced %d owner mappings", ownerMappings)
	}
}

func TestRepositoryBindingHandoverReportsUnverifiedDestination(t *testing.T) {
	system := newHandoverSystem(t, []handoverHistory{
		{TenantID: "tenant-handover-unverified", TenantStatus: "active", Owners: []string{"201"}, Repository: "historical-path", Status: "reserved"},
	}, func(db *sql.DB) {
		// The mapping holds the destination but its verification is missing, so
		// the owner's own read refuses the actor.
		seedMapping(t, db, "201", "uswccr.ccs.tencentyun.com", "oplcloud", "historical-path", true, false, false)
	})
	mappingBefore := mappingRow(t, system.gateway, "201")

	summary, err := system.service.HandoverRepositoryBindings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// The row is not counted as already moved: the destination the owner holds
	// is not a destination the owner can serve.
	if summary.Scanned != 1 || summary.Moved != 0 || summary.Already != 0 || summary.Conflicting != 1 || summary.Unattributable != 0 || summary.Failed != 0 {
		t.Fatalf("summary=%+v", summary)
	}
	if len(summary.ConflictingRefs) != 1 || !strings.Contains(summary.ConflictingRefs[0], "reason=destination_unverified") {
		t.Fatalf("conflicting refs=%v", summary.ConflictingRefs)
	}
	if after := mappingRow(t, system.gateway, "201"); after != mappingBefore {
		t.Fatalf("the unverified mapping was changed:\nbefore=%s\nafter=%s", mappingBefore, after)
	}
}

func TestRepositoryBindingHandoverReportsRevokedMapping(t *testing.T) {
	system := newHandoverSystem(t, []handoverHistory{
		{TenantID: "tenant-handover-revoked", TenantStatus: "active", Owners: []string{"201"}, Repository: "historical-path", Status: "reserved"},
	}, func(db *sql.DB) {
		// A revoked mapping exists and the owner refuses it. It is not an absent
		// mapping, so the handover must not write a new one over it.
		seedMapping(t, db, "201", "uswccr.ccs.tencentyun.com", "oplcloud", "historical-path", true, true, true)
	})
	mappingBefore := mappingRow(t, system.gateway, "201")

	summary, err := system.service.HandoverRepositoryBindings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Scanned != 1 || summary.Moved != 0 || summary.Already != 0 || summary.Conflicting != 1 || summary.Unattributable != 0 || summary.Failed != 0 {
		t.Fatalf("summary=%+v", summary)
	}
	if len(summary.ConflictingRefs) != 1 || !strings.Contains(summary.ConflictingRefs[0], "reason=revoked_mapping") {
		t.Fatalf("conflicting refs=%v", summary.ConflictingRefs)
	}
	if after := mappingRow(t, system.gateway, "201"); after != mappingBefore {
		t.Fatalf("the revoked mapping was changed:\nbefore=%s\nafter=%s", mappingBefore, after)
	}
}

func TestRepositoryBindingHandoverRepairsUnverifiedIdentity(t *testing.T) {
	system := newHandoverSystem(t, []handoverHistory{
		{TenantID: "tenant-handover-unverified-identity", TenantStatus: "active", Owners: []string{"201"}, Repository: "historical-path", Status: "reserved"},
	}, func(db *sql.DB) {
		// The mapping exists without a verified identity and without a
		// destination. The owner's own storage action completes it; the handover
		// does not invent a second mapping for the actor.
		seedMapping(t, db, "201", "", "", "", false, false, false)
	})

	summary, err := system.service.HandoverRepositoryBindings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Scanned != 1 || summary.Moved != 1 || summary.Already != 0 || summary.Conflicting != 0 || summary.Unattributable != 0 || summary.Failed != 0 {
		t.Fatalf("summary=%+v", summary)
	}
	if count := mappingCount(t, system.gateway); count != 1 {
		t.Fatalf("the handover produced %d mapping rows for one actor", count)
	}
	var repository string
	var verified, destinationVerified sql.NullTime
	if err = system.gateway.QueryRowContext(t.Context(), `SELECT COALESCE(repository,''),verified_at,destination_verified_at FROM gateway.identity_mappings WHERE actor_id='201'`).Scan(&repository, &verified, &destinationVerified); err != nil {
		t.Fatal(err)
	}
	if repository != "historical-path" || !verified.Valid || !destinationVerified.Valid {
		t.Fatalf("repository=%q verified=%v destination_verified=%v", repository, verified, destinationVerified)
	}
}

func TestRepositoryBindingHandoverRefusesUnattributableHistory(t *testing.T) {
	for _, tc := range []struct {
		name    string
		history handoverHistory
	}{
		{"inactive tenant", handoverHistory{TenantID: "tenant-handover-suspended", TenantStatus: "suspended", Owners: []string{"201"}, Repository: "suspended-path", Status: "reserved"}},
		{"two registered owners", handoverHistory{TenantID: "tenant-handover-two-owners", TenantStatus: "active", Owners: []string{"201", "202"}, Repository: "shared-path", Status: "active"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			system := newHandoverSystem(t, []handoverHistory{tc.history}, nil)
			historyBefore := historicalRow(t, system.tenant, tc.history.TenantID)

			summary, err := system.service.HandoverRepositoryBindings(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if summary.Scanned != 1 || summary.Moved != 0 || summary.Already != 0 || summary.Released != 0 || summary.Unattributable != 1 || summary.Conflicting != 0 || summary.Failed != 0 {
				t.Fatalf("summary=%+v", summary)
			}
			if len(summary.UnattributableRefs) != 1 {
				t.Fatalf("unattributable refs=%v", summary.UnattributableRefs)
			}
			// No actor is invented for an ambiguous owner, so nothing is written.
			if count := mappingCount(t, system.gateway); count != 0 {
				t.Fatalf("unattributable history wrote %d mapping rows", count)
			}
			if after := historicalRow(t, system.tenant, tc.history.TenantID); after != historyBefore {
				t.Fatalf("historical row changed:\nbefore=%s\nafter=%s", historyBefore, after)
			}
		})
	}
}

func TestRepositoryBindingHandoverReportsRetiredHistory(t *testing.T) {
	system := newHandoverSystem(t, []handoverHistory{
		{TenantID: "tenant-handover-retired", TenantStatus: "active", Owners: []string{"201"}, Repository: "retired-path", Status: "retired"},
	}, nil)
	historyBefore := historicalRow(t, system.tenant, "tenant-handover-retired")

	summary, err := system.service.HandoverRepositoryBindings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Scanned != 1 || summary.Moved != 0 || summary.Already != 0 || summary.Released != 1 || summary.Unattributable != 0 || summary.Conflicting != 0 || summary.Failed != 0 {
		t.Fatalf("summary=%+v", summary)
	}
	if count := mappingCount(t, system.gateway); count != 0 {
		t.Fatalf("retired history wrote %d mapping rows", count)
	}
	if after := historicalRow(t, system.tenant, "tenant-handover-retired"); after != historyBefore {
		t.Fatalf("historical row changed:\nbefore=%s\nafter=%s", historyBefore, after)
	}
}
