package server

import (
	_ "github.com/jackc/pgx/v5/stdlib"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

const (
	capacityHistoryPerWorkspace = 5
	capacitySeedWorkers         = 8
	capacityTestTimeout         = 10 * time.Minute
)

type capacitySQLCounter struct {
	mu      sync.Mutex
	queries []string
}

func (c *capacitySQLCounter) log(values ...any) {
	message := fmt.Sprint(values...)
	if !strings.Contains(message, "query=") {
		return
	}
	c.mu.Lock()
	c.queries = append(c.queries, message)
	c.mu.Unlock()
}

func (c *capacitySQLCounter) reset() {
	c.mu.Lock()
	c.queries = nil
	c.mu.Unlock()
}

func (c *capacitySQLCounter) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.queries...)
}

func seedCapacityAccount(ctx context.Context, store controlPlaneTableStore, now time.Time, index int) error {
	accountID := fmt.Sprintf("acct-capacity-%04d", index)
	userID := fmt.Sprintf("usr-capacity-%04d", index)
	workspaceID := fmt.Sprintf("workspace-capacity-%04d", index)
	email := fmt.Sprintf("capacity-%04d@example.com", index)
	account, user := provisionedAccountRowsFor(accountID, userID, email, int64(10_000+index))
	if err := store.CreateProvisionedAccount(ctx, account, user); err != nil {
		return fmt.Errorf("seed account %d: %w", index, err)
	}
	if err := store.SaveSession(ctx, map[string]any{
		"id": sessionLookupKey(fmt.Sprintf("capacity-session-%04d", index)), "userId": userID,
		"csrf": "capacity-csrf", "expiresAt": now.Add(24 * time.Hour).Format(time.RFC3339),
	}); err != nil {
		return fmt.Errorf("seed session %d: %w", index, err)
	}
	paidThrough := now.Add(90 * 24 * time.Hour)
	workspace := canonicalWorkspaceRenewalRow(false)
	workspace["id"], workspace["name"] = workspaceID, "Capacity Workspace"
	workspace["accountId"], workspace["ownerAccountId"], workspace["ownerUserId"] = accountID, accountID, userID
	workspace["state"], workspace["status"], workspace["customerProduct"] = "running", "running", true
	workspace["currentComputeAllocationId"], workspace["computeAllocationId"] = "compute-"+workspaceID, "compute-"+workspaceID
	workspace["storageId"] = "storage-" + workspaceID
	workspace["autoRenew"], workspace["authorizedBy"], workspace["authorizedAt"] = false, "", ""
	workspace["periodStart"] = paidThrough.AddDate(0, -1, 0).Format(time.RFC3339)
	workspace["paidThrough"] = paidThrough.Format(time.RFC3339)
	workspace["nextRenewalAt"] = paidThrough.Add(-24 * time.Hour).Format(time.RFC3339)
	workspace["billingAnchorDay"] = int64(paidThrough.Day())
	if err := store.SaveWorkspace(ctx, workspace); err != nil {
		return fmt.Errorf("seed Workspace %d: %w", index, err)
	}
	for history := 0; history < capacityHistoryPerWorkspace; history++ {
		historyPaidThrough := paidThrough.AddDate(0, -history-1, 0)
		historyWorkspace := cloneMap(workspace)
		historyWorkspace["periodStart"] = historyPaidThrough.AddDate(0, -1, 0).Format(time.RFC3339)
		historyWorkspace["paidThrough"] = historyPaidThrough.Format(time.RFC3339)
		operationID := fmt.Sprintf("operation-capacity-%04d-%02d", index, history)
		operation, err := newWorkspaceRenewalOperation(historyWorkspace, now.AddDate(0, -history-1, 0))
		if err != nil {
			return fmt.Errorf("build operation %d/%d: %w", index, history, err)
		}
		operation.ID, operation.Status, operation.Phase = operationID, "active", "complete"
		operation.ReceiptID = "receipt-" + operationID
		if err := store.SaveRuntimeOperation(ctx, workspaceRenewalOperationRow(operation)); err != nil {
			return fmt.Errorf("seed operation %d/%d: %w", index, history, err)
		}
	}
	return nil
}

func TestSinglePodCapacity(t *testing.T) {
	if os.Getenv("OPL_CAPACITY_TESTS") != "1" {
		t.Skip("set OPL_CAPACITY_TESTS=1 to run the isolated 1000-user data-scale test")
	}
	t.Setenv("OPL_PROVIDER_RECONCILE_WORKER_ENABLED", "false")
	t.Setenv("OPL_ARCHIVE_RETENTION_WORKER_ENABLED", "false")

	type result struct {
		loginQueries, sessionQueries, operationGetQueries, operationPageQueries int
	}
	results := map[int]result{}
	for _, total := range []int{100, 1000} {
		t.Run(fmt.Sprintf("records_%d", total), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), capacityTestTimeout)
			defer cancel()
			admin := openControlPlaneTestPostgres(t)
			schema := fmt.Sprintf("control_plane_capacity_%d_%d", total, time.Now().UnixNano())
			if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
				_ = admin.Close()
			})
			databaseURL := controlPlaneTestPostgresURL(t, "postgres", schema)
			stateStore, err := newTestPostgresEntStateStore(databaseURL)
			if err != nil {
				t.Fatal(err)
			}
			seedStore := stateStore.(*postgresEntStateStore)
			t.Cleanup(func() { _ = seedStore.Close() })

			now := time.Now().UTC().Truncate(time.Second)
			seedStarted := time.Now()
			jobs := make(chan int, total)
			for index := 1; index <= total; index++ {
				jobs <- index
			}
			close(jobs)
			var workers sync.WaitGroup
			var seedErr error
			var seedErrOnce sync.Once
			for range capacitySeedWorkers {
				workers.Add(1)
				go func() {
					defer workers.Done()
					for index := range jobs {
						if err := seedCapacityAccount(ctx, seedStore, now, index); err != nil {
							seedErrOnce.Do(func() { seedErr = err })
						}
					}
				}()
			}
			workers.Wait()
			if seedErr != nil {
				t.Fatal(seedErr)
			}
			seedDuration := time.Since(seedStarted)

			counter := &capacitySQLCounter{}
			db, err := sql.Open("pgx", databaseURL)
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(controlPlaneMaxOpenDBConnections)
			measuredStore := &postgresEntStateStore{db: db, dialect: "postgres", onQuery: func(q string) { counter.log(q) }}
			t.Cleanup(func() { _ = measuredStore.Close() })
			app, err := newControlPlaneAppWithStore(measuredStore)
			if err != nil {
				t.Fatal(err)
			}
			targetEmail := fmt.Sprintf("capacity-%04d@example.com", total)
			targetAccountID := fmt.Sprintf("acct-capacity-%04d", total)
			targetWorkspaceID := fmt.Sprintf("workspace-capacity-%04d", total)
			targetOperationID := fmt.Sprintf("operation-capacity-%04d-00", total)
			remote := newIdentityTestSub2API()
			remote.identities[targetEmail] = clients.Sub2APIIdentity{ID: int64(10_000 + total), Email: targetEmail, Status: "active"}
			remote.passwords[targetEmail] = "capacity-password"
			fabricCalls := []string{}
			service := controlplane.NewService(fakeLedgerClient{}, &fakeFabricClient{calls: &fabricCalls}, remote)

			counter.reset()
			loginStarted := time.Now()
			_, sessionID, err := app.login(ctx, service, map[string]any{"email": targetEmail, "password": "capacity-password"})
			loginDuration := time.Since(loginStarted)
			if err != nil {
				t.Fatal(err)
			}
			loginQueries := len(counter.snapshot())
			counter.reset()
			sessionStarted := time.Now()
			request := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
			request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionID})
			if _, state := app.session(request); state != sessionAuthenticated {
				t.Fatalf("session state=%v", state)
			}
			sessionDuration := time.Since(sessionStarted)
			sessionQueries := len(counter.snapshot())

			counter.reset()
			operationStarted := time.Now()
			operation, found, err := measuredStore.GetRuntimeOperation(ctx, targetOperationID)
			operationDuration := time.Since(operationStarted)
			if err != nil || !found || stringValue(operation["accountId"]) != targetAccountID {
				t.Fatalf("operation point lookup=%#v found=%t err=%v", operation, found, err)
			}
			operationGetQueries := len(counter.snapshot())
			counter.reset()
			operationPageStarted := time.Now()
			operationPage, err := measuredStore.PageRuntimeOperations(ctx, runtimeOperationQuery{
				AccountID: targetAccountID, WorkspaceID: targetWorkspaceID, Action: "workspace.renewal", Statuses: []string{"active"}, Offset: 0, Limit: 20,
			})
			operationPageDuration := time.Since(operationPageStarted)
			if err != nil || operationPage.Total != capacityHistoryPerWorkspace || len(operationPage.Items) != capacityHistoryPerWorkspace {
				t.Fatalf("operation page=%#v err=%v", operationPage, err)
			}
			operationPageQueries := len(counter.snapshot())

			remote.identityMu.Lock()
			sub2APIRequests := remote.authCalls
			remote.identityMu.Unlock()
			if sub2APIRequests != 1 || len(fabricCalls) != 0 {
				t.Fatalf("capacity external reads sub2api=%d fabric=%#v", sub2APIRequests, fabricCalls)
			}

			var accounts, users, sessions, workspaces, operations int
			if err := admin.QueryRowContext(ctx, fmt.Sprintf(`SELECT
				(SELECT count(*) FROM %s.control_plane_accounts),
				(SELECT count(*) FROM %s.control_plane_users),
				(SELECT count(*) FROM %s.control_plane_sessions),
				(SELECT count(*) FROM %s.control_plane_workspaces),
				(SELECT count(*) FROM %s.control_plane_runtime_operations)`, schema, schema, schema, schema, schema)).Scan(&accounts, &users, &sessions, &workspaces, &operations); err != nil {
				t.Fatal(err)
			}
			if accounts != total || users != total || sessions != total+1 || workspaces != total || operations != total*capacityHistoryPerWorkspace {
				t.Fatalf("seeded facts accounts=%d users=%d sessions=%d workspaces=%d operations=%d", accounts, users, sessions, workspaces, operations)
			}
			results[total] = result{loginQueries, sessionQueries, operationGetQueries, operationPageQueries}
			t.Logf("single_pod_capacity records=%d accounts=%d users=%d sessions=%d workspaces=%d operation_history=%d seed=%s sql_login=%d login=%s sql_auth_me=%d auth_me=%s sql_operation_get=%d operation_get=%s sql_operation_page=%d operation_page=%s sub2api_requests=%d fabric_requests=%d max_downstream_concurrency=1",
				total, accounts, users, sessions, workspaces, operations, seedDuration, loginQueries, loginDuration, sessionQueries, sessionDuration,
				operationGetQueries, operationDuration, operationPageQueries, operationPageDuration, sub2APIRequests, len(fabricCalls))
		})
	}
	if results[100] != results[1000] {
		t.Fatalf("point-query SQL count changed with table size: 100=%#v 1000=%#v", results[100], results[1000])
	}
}
