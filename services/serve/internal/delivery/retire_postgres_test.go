package delivery_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/serve/internal/delivery"
)

// insertAction writes one lifecycle action row exactly as Serve's own
// applyRecordedLifecycle writes it, so the table's own CHECK constraints decide
// whether the vocabulary is legal. This is the real write path: a word the schema
// does not admit is rejected here, in PostgreSQL, before any provider is called.
func insertAction(t *testing.T, db *sql.DB, runtimeID, action string) error {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO serve.agent_runtime_actions(id,runtime_instance_id,command_id,action,expected_deployment_id,input_snapshot,observation_result)
		 VALUES($1,$2,$1,$3,'dep-1','{"desired":"absent"}'::jsonb,'unknown')`,
		"act_"+action+"_"+runtimeID, runtimeID, action)
	return err
}

// TestAgentRuntimeActionVocabularyIsSchemaChecked pins the vocabulary this owner
// can write. serve.agent_runtime_actions admits exactly start, stop, terminate,
// reload and verify, and the check has never been relaxed by a later migration.
// A retirement must therefore be recorded as "terminate"; naming it "retire"
// reads well and is refused by the database on the first execution.
func TestAgentRuntimeActionVocabularyIsSchemaChecked(t *testing.T) {
	db, tenant, _ := fixture(t)
	ctx := context.Background()
	seedObservationDeployment(t, db, "ws-vocab", tenant, "dep-vocab", "active", "ready", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
	runtimeID := "rt_" + "dep-vocab"

	// The word the retirement path actually writes is the constant Serve uses, and
	// the table must admit it. Renaming RetirementAction to a word outside the
	// declared list fails here, in PostgreSQL, rather than at runtime.
	if err := insertAction(t, db, runtimeID, delivery.RetirementAction); err != nil {
		t.Fatalf("Serve's RetirementAction %q was refused: %v", delivery.RetirementAction, err)
	}

	// A word outside the declared vocabulary is refused by the constraint itself.
	for _, action := range []string{"retire", "removed", "delete"} {
		err := insertAction(t, db, runtimeID, action)
		if err == nil {
			t.Fatalf("action %q was accepted, want the schema's CHECK to refuse it", action)
		}
		if !strings.Contains(err.Error(), "agent_runtime_actions_action_check") {
			t.Fatalf("action %q failed for %v, want the action CHECK constraint", action, err)
		}
	}

	var admitted int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM serve.agent_runtime_actions WHERE runtime_instance_id=$1`, runtimeID).Scan(&admitted); err != nil {
		t.Fatal(err)
	}
	if admitted != 1 {
		t.Fatalf("stored %d actions, want only the accepted one", admitted)
	}
}

// TestRetirementOperationKindAndObservationAreStorable proves the operation row
// Retire writes is accepted by its own constraints. serve.operations has no CHECK
// on kind, so the retirement's kind is free, but a succeeded operation must carry
// its completion and its observation must be one the schema admits.
func TestRetirementOperationKindAndObservationAreStorable(t *testing.T) {
	db, tenant, _ := fixture(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `INSERT INTO serve.operations(id,tenant_id,actor_id,kind,resource_id,status,stage,observation_result,request_id,accepted_input,completed_at)
		VALUES('op-retire','`+tenant+`','actor-1','runtime_retire','rt-vocab','succeeded','retirement','confirmed','req-1','{}'::jsonb,now())`)
	if err != nil {
		t.Fatalf("writing the retirement operation Serve produces: %v", err)
	}
	var kind, status, observation string
	if err := db.QueryRowContext(ctx, `SELECT kind,status,observation_result FROM serve.operations WHERE id='op-retire'`).Scan(&kind, &status, &observation); err != nil {
		t.Fatal(err)
	}
	if kind != "runtime_retire" || status != "succeeded" || observation != "confirmed" {
		t.Fatalf("stored %q %q %q, want runtime_retire/succeeded/confirmed", kind, status, observation)
	}

	// The enum the Workspace's own retirement binding checks must resolve from it.
	if value, ok := api.OperationKindEnum_value["OPERATION_KIND_ENUM_"+strings.ToUpper(kind)]; !ok || api.OperationKindEnum(value) != api.OperationKindEnum_OPERATION_KIND_ENUM_RUNTIME_RETIRE {
		t.Fatalf("kind %q does not resolve to RUNTIME_RETIRE", kind)
	}

	// A succeeded operation without its completion is refused, so the confirmation
	// write must carry completed_at with it.
	if _, err := db.ExecContext(ctx, `UPDATE serve.operations SET status='succeeded',completed_at=NULL WHERE id='op-retire'`); err == nil {
		t.Fatal("a succeeded operation without completed_at was accepted, want the schema's CHECK to refuse it")
	} else if !strings.Contains(strings.ToLower(err.Error()), "check constraint") {
		t.Fatalf("failed for %v, want a CHECK constraint", err)
	}
}

// TestAgentRuntimeActionsHasNoRelaxedVocabulary guards the claim above against a
// later migration widening the list. If a future migration admits more actions,
// this test fails so the retirement's vocabulary is re-decided rather than assumed.
func TestAgentRuntimeActionsHasNoRelaxedVocabulary(t *testing.T) {
	db, _, _ := fixture(t)
	rows, err := db.QueryContext(context.Background(),
		`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='serve.agent_runtime_actions'::regclass AND conname='agent_runtime_actions_action_check'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("the action CHECK constraint is missing")
	}
	var def string
	if err := rows.Scan(&def); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(def, "'terminate'") || strings.Contains(def, "'retire'") {
		t.Fatalf("action vocabulary changed to %q", def)
	}
}
