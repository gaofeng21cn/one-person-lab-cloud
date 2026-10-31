package migrations_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"testing/fstest"

	"github.com/lib/pq"
	"opl-cloud/services/internal/ownerstore"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
	"opl-cloud/services/runtime-control/migrations"
)

func TestOptionalPackageFormatsMigrationPreservesHistoryAndRetries(t *testing.T) {
	ctx := context.Background()
	h, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{
		AdminDSN: ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip),
		Owner:    "runtime_control", Database: "opl_runtime_control",
		SchemaOwnerRole: "opl_runtime_control_owner", WriterRole: "opl_runtime_control_writer", RuntimeRole: "opl_runtime_control_runtime",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close(context.Background()) })
	source, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	original, err := source.ReadFile("0001_opl_runtime_control.sql")
	if err != nil {
		t.Fatal(err)
	}
	initialSource, err := ownerstore.SourceFromFS(fstest.MapFS{"0001_opl_runtime_control.sql": &fstest.MapFile{Data: original}})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Install(ctx, h.OwnerDSN, h.DatabaseName(), initialSource); err != nil {
		t.Fatal(err)
	}
	db, err := h.Open(ctx, h.OwnerDSN, h.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := ownerstore.New(db, "runtime_control")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile("../../../docs/spec/target/contracts/publisher-contract.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var document struct{ Examples []json.RawMessage }
	if err := json.Unmarshal(schema, &document); err != nil {
		t.Fatal(err)
	}

	for _, mode := range []string{"nonempty", "empty"} {
		var declaration map[string]json.RawMessage
		if err := json.Unmarshal(document.Examples[0], &declaration); err != nil {
			t.Fatal(err)
		}
		formats := []string{}
		if mode == "nonempty" {
			if err := json.Unmarshal(declaration["packageFormatVersions"], &formats); err != nil {
				t.Fatal(err)
			}
		} else {
			for _, key := range []string{"modelConfiguration", "buildRecipe", "packageFormatVersions", "packageFormatContracts"} {
				delete(declaration, key)
			}
			if mode == "empty" {
				declaration["packageFormatVersions"] = json.RawMessage(`[]`)
			}
		}
		raw, err := json.Marshal(declaration)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		hash := hex.EncodeToString(sum[:])
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, `INSERT INTO runtime_control.runtime_releases
			(id,name,version_label,artifact_repository,artifact_digest,approved_by,runtime_abi_version,package_format_versions,admission_receipt_id,publisher_namespace_id,publisher_contract_digest,publisher_contract,publisher_contract_object_ref)
			VALUES ($1,'retained-runtime',$1,$2::jsonb #>> '{image,repository}',$2::jsonb #>> '{image,digest}','retained-actor',$2::jsonb->>'runtimeAbiVersion',$3,'retained-receipt',$2::jsonb->>'publisherNamespaceId',$4,$2::jsonb,$5)`,
			mode, raw, pq.Array(formats), "sha256:"+hash, "runtime-contract:"+mode+"@sha256:"+hash); err != nil {
			t.Fatal(err)
		}
		response, err := json.Marshal(map[string]any{"id": mode, "publisherContract": json.RawMessage(raw), "publisherContractDigest": "sha256:" + hash})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.RecordIdempotency(ctx, tx, ownerstore.IdempotencyInput{
			ID: "retained-" + mode, TenantScope: "platform", ActorScope: "retained-actor", OperationName: "RegisterRuntimeVersion", IdempotencyKey: mode,
			RequestSHA256: hash, ResourceID: mode, ResponseStatus: 200, ResponseBody: response,
		}); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}

	snapshot := func() []byte {
		t.Helper()
		var raw []byte
		if err := db.QueryRowContext(ctx, `SELECT jsonb_build_object(
			'releases',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM runtime_control.runtime_releases r),
			'idempotency',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM runtime_control.idempotency_records i))`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	before := snapshot()
	forward, err := source.ReadFile("0002_optional_package_formats.sql")
	if err != nil {
		t.Fatal(err)
	}
	// Crash window: the SQL transaction commits, but its journal INSERT never ran.
	if _, err := db.ExecContext(ctx, string(forward)); err != nil {
		t.Fatal(err)
	}
	var recorded int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM opl_schema_migrations WHERE service='runtime_control' AND version='0002_optional_package_formats'`).Scan(&recorded); err != nil || recorded != 0 {
		t.Fatalf("expected unjournaled SQL commit, recorded=%d error=%v", recorded, err)
	}
	if !bytes.Equal(before, snapshot()) {
		t.Fatal("SQL migration changed retained releases or idempotency records")
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := h.Install(ctx, h.OwnerDSN, h.DatabaseName(), source); err != nil {
			t.Fatalf("migration retry %d: %v", attempt, err)
		}
		if !bytes.Equal(before, snapshot()) {
			t.Fatalf("migration retry %d changed retained releases or idempotency records", attempt)
		}
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM opl_schema_migrations WHERE service='runtime_control' AND version='0002_optional_package_formats'`).Scan(&recorded); err != nil || recorded != 1 {
		t.Fatalf("expected one migration receipt, recorded=%d error=%v", recorded, err)
	}

	runtime, err := h.Open(ctx, h.RuntimeDSN, h.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close() })
	for _, tc := range []struct {
		name    string
		value   string
		formats []string
		code    pq.ErrorCode
	}{
		{"omitted_empty", "", []string{}, ""},
		{"explicit_empty", `[]`, []string{}, ""},
		{"equal_nonempty", `["a","b"]`, []string{"a", "b"}, ""},
		{"json_null", `null`, []string{}, "23514"},
		{"wrong_json_type", `"a"`, []string{"a"}, "23514"},
		{"omitted_nonempty", "", []string{"a"}, "23514"},
		{"empty_nonempty", `[]`, []string{"a"}, "23514"},
		{"nonempty_empty", `["a"]`, []string{}, "23514"},
		{"different_nonempty", `["a"]`, []string{"b"}, "23514"},
		{"different_order", `["a","b"]`, []string{"b", "a"}, "23514"},
		{"sql_null", "", nil, "23502"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := runtime.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			_, err = tx.ExecContext(ctx, `UPDATE runtime_control.runtime_releases SET
				publisher_contract=CASE WHEN $1='' THEN publisher_contract-'packageFormatVersions' ELSE jsonb_set(publisher_contract,'{packageFormatVersions}',NULLIF($1,'')::jsonb) END,
				package_format_versions=$2 WHERE id='empty'`, tc.value, pq.Array(tc.formats))
			if tc.code == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var pgErr *pq.Error
			if !errors.As(err, &pgErr) || pgErr.Code != tc.code {
				t.Fatalf("constraint error=%v, want SQLSTATE %s", err, tc.code)
			}
			if tc.code == "23514" && pgErr.Constraint != "runtime_releases_package_formats_match" {
				t.Fatalf("wrong constraint rejected the row: %s", pgErr.Constraint)
			}
		})
	}
}
