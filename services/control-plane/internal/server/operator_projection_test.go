package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

var operatorProjectionTime = time.Date(2026, 7, 19, 4, 5, 6, 0, time.UTC)

type operatorProjectionSub2API struct {
	*testSub2APIClient
	users              []clients.Sub2APIUser
	userUsage          map[int64]clients.Sub2APIBatchUserUsage
	keyUsage           map[int64]clients.Sub2APIBatchKeyUsage
	adminUsersErr      error
	adminUserErrs      map[int64]error
	batchUsersErr      error
	batchKeysErr       error
	adminUsersCalls    int
	adminUserQueries   []clients.Sub2APIUserPageQuery
	adminUserCalls     int
	exactUserIDs       []int64
	userReadActive     int
	userReadMax        int
	userReadStarted    chan int64
	userReadRelease    <-chan struct{}
	userReadMu         sync.Mutex
	batchUsersCalls    int
	batchKeysCalls     int
	singleUserCalls    int
	requestedUserIDs   [][]int64
	requestedAPIKeyIDs [][]int64
	keyCounts          map[int64]int
	keyCountErrs       map[int64]error
	keyCountCalls      []int64
	keyCountMu         sync.Mutex
}

type boundedOperatorPageSub2API struct {
	*operatorProjectionSub2API
	mu              sync.Mutex
	active          int
	maxActive       int
	userActive      int
	userMax         int
	keyActive       int
	keyMax          int
	deadlines       []time.Time
	missingDeadline bool
	started         chan string
	release         <-chan struct{}
}

func (c *boundedOperatorPageSub2API) begin(ctx context.Context, kind string) (func(), error) {
	deadline, ok := ctx.Deadline()
	c.mu.Lock()
	c.missingDeadline = c.missingDeadline || !ok
	if ok {
		c.deadlines = append(c.deadlines, deadline)
	}
	c.active++
	if c.active > c.maxActive {
		c.maxActive = c.active
	}
	if kind == "user" {
		c.userActive++
		if c.userActive > c.userMax {
			c.userMax = c.userActive
		}
	} else {
		c.keyActive++
		if c.keyActive > c.keyMax {
			c.keyMax = c.keyActive
		}
	}
	c.mu.Unlock()
	c.started <- kind
	select {
	case <-c.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.active--
		if kind == "user" {
			c.userActive--
		} else {
			c.keyActive--
		}
	}, nil
}

func (c *boundedOperatorPageSub2API) AdminUser(ctx context.Context, userID int64) (clients.Sub2APIUser, error) {
	done, err := c.begin(ctx, "user")
	if err != nil {
		return clients.Sub2APIUser{}, err
	}
	defer done()
	return c.operatorProjectionSub2API.AdminUser(ctx, userID)
}

func (c *boundedOperatorPageSub2API) AdminUserKeyCount(ctx context.Context, userID int64) (int, error) {
	done, err := c.begin(ctx, "key")
	if err != nil {
		return 0, err
	}
	defer done()
	return c.operatorProjectionSub2API.AdminUserKeyCount(ctx, userID)
}

func (c *operatorProjectionSub2API) AdminUserKeyCount(_ context.Context, userID int64) (int, error) {
	c.keyCountMu.Lock()
	defer c.keyCountMu.Unlock()
	c.keyCountCalls = append(c.keyCountCalls, userID)
	return c.keyCounts[userID], c.keyCountErrs[userID]
}

func (c *operatorProjectionSub2API) AdminUsers(_ context.Context, query clients.Sub2APIUserPageQuery) (clients.Sub2APIUserPage, error) {
	c.adminUsersCalls++
	c.adminUserQueries = append(c.adminUserQueries, query)
	if c.adminUsersErr != nil {
		return clients.Sub2APIUserPage{}, c.adminUsersErr
	}
	users := c.users
	if search := normalizeEmail(query.Search); search != "" {
		users = nil
		for _, user := range c.users {
			if strings.Contains(user.Email, search) {
				users = append(users, user)
			}
		}
	}
	pages := (len(users) + query.PageSize - 1) / query.PageSize
	if pages == 0 {
		pages = 1
	}
	start := (query.Page - 1) * query.PageSize
	end := start + query.PageSize
	if start > len(users) {
		start = len(users)
	}
	if end > len(users) {
		end = len(users)
	}
	return clients.Sub2APIUserPage{Items: users[start:end], Total: int64(len(users)), Page: query.Page, PageSize: query.PageSize, Pages: pages}, nil
}

func (c *operatorProjectionSub2API) AdminUser(ctx context.Context, id int64) (clients.Sub2APIUser, error) {
	c.userReadMu.Lock()
	c.adminUserCalls++
	c.exactUserIDs = append(c.exactUserIDs, id)
	c.userReadActive++
	if c.userReadActive > c.userReadMax {
		c.userReadMax = c.userReadActive
	}
	var user clients.Sub2APIUser
	for _, candidate := range c.users {
		if candidate.ID == id {
			user = candidate
			break
		}
	}
	err := c.adminUserErrs[id]
	started, release := c.userReadStarted, c.userReadRelease
	c.userReadMu.Unlock()
	defer func() {
		c.userReadMu.Lock()
		c.userReadActive--
		c.userReadMu.Unlock()
	}()
	if started != nil {
		select {
		case started <- id:
		case <-ctx.Done():
			return clients.Sub2APIUser{}, ctx.Err()
		}
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return clients.Sub2APIUser{}, ctx.Err()
		}
	}
	if err != nil {
		return clients.Sub2APIUser{}, err
	}
	if user.ID == 0 {
		return clients.Sub2APIUser{}, errors.New("user unavailable")
	}
	return user, nil
}

func (c *operatorProjectionSub2API) BatchUsersUsage(_ context.Context, ids []int64) (map[int64]clients.Sub2APIBatchUserUsage, error) {
	c.batchUsersCalls++
	c.requestedUserIDs = append(c.requestedUserIDs, append([]int64(nil), ids...))
	if c.batchUsersErr != nil {
		return nil, c.batchUsersErr
	}
	result := make(map[int64]clients.Sub2APIBatchUserUsage, len(ids))
	for _, id := range ids {
		result[id] = c.userUsage[id]
	}
	return result, nil
}

func (c *operatorProjectionSub2API) BatchKeysUsage(_ context.Context, ids []int64) (map[int64]clients.Sub2APIBatchKeyUsage, error) {
	c.batchKeysCalls++
	c.requestedAPIKeyIDs = append(c.requestedAPIKeyIDs, append([]int64(nil), ids...))
	if c.batchKeysErr != nil {
		return nil, c.batchKeysErr
	}
	result := make(map[int64]clients.Sub2APIBatchKeyUsage, len(ids))
	for _, id := range ids {
		result[id] = c.keyUsage[id]
	}
	return result, nil
}

func (c *operatorProjectionSub2API) User(ctx context.Context, id int64) (clients.Sub2APIIdentity, error) {
	c.singleUserCalls++
	return c.testSub2APIClient.User(ctx, id)
}

type operatorProjectionLedger struct {
	fakeLedgerClient
	receipts map[string]clients.Receipt
	err      error
}

type operatorProjectionNoOperationsFabric struct{ fakeFabricClient }

type operatorProjectionFactsFabric struct {
	fakeFabricClient
	facts  map[string]clients.ProviderFact
	inputs []clients.ProviderFactsBatchInput
}

type operatorWorkspacePagingStore struct {
	*memoryTableStore
	accountLists []string
	accountPages []tablePageQuery
	userLists    int
	computeLists []string
	storageLists []string
	attachLists  []string
	pages        []struct {
		accountID string
		query     tablePageQuery
	}
	getAccountIDs    []string
	getUserIDs       []string
	getComputeIDs    []string
	getStorageIDs    []string
	getAttachmentIDs []string
	getIDs           []string
}

type operatorOverviewAggregateStore struct {
	*memoryTableStore
	pageAccountCalls int
	aggregateCalls   int
}

func (s *operatorOverviewAggregateStore) PageAccounts(context.Context, tablePageQuery) (tablePage, error) {
	s.pageAccountCalls++
	return tablePage{}, errors.New("overview_must_not_page_accounts")
}

func (s *operatorOverviewAggregateStore) CountAccountStatuses(ctx context.Context) (map[string]int, error) {
	s.aggregateCalls++
	accounts, err := s.memoryTableStore.ListAccounts(ctx, "")
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, account := range accounts {
		counts[stringValue(account["status"])]++
	}
	return counts, nil
}

func (s *operatorOverviewAggregateStore) CountWorkspaces(context.Context) (int, error) {
	return 0, nil
}

func (s *operatorWorkspacePagingStore) ListWorkspaces(ctx context.Context, accountID string) ([]map[string]any, error) {
	return s.memoryTableStore.ListWorkspaces(ctx, accountID)
}

func (s *operatorWorkspacePagingStore) ListAccounts(ctx context.Context, accountID string) ([]map[string]any, error) {
	s.accountLists = append(s.accountLists, accountID)
	return s.memoryTableStore.ListAccounts(ctx, accountID)
}

func (s *operatorWorkspacePagingStore) PageAccounts(ctx context.Context, query tablePageQuery) (tablePage, error) {
	s.accountPages = append(s.accountPages, query)
	return s.memoryTableStore.PageAccounts(ctx, query)
}

func (s *operatorWorkspacePagingStore) ListUsers(ctx context.Context, includeDeleted bool) ([]map[string]any, error) {
	s.userLists++
	return s.memoryTableStore.ListUsers(ctx, includeDeleted)
}

func (s *operatorWorkspacePagingStore) ListComputes(ctx context.Context, accountID string) ([]map[string]any, error) {
	s.computeLists = append(s.computeLists, accountID)
	return s.memoryTableStore.ListComputes(ctx, accountID)
}

func (s *operatorWorkspacePagingStore) ListStorages(ctx context.Context, accountID string) ([]map[string]any, error) {
	s.storageLists = append(s.storageLists, accountID)
	return s.memoryTableStore.ListStorages(ctx, accountID)
}

func (s *operatorWorkspacePagingStore) ListAttachments(ctx context.Context, accountID string) ([]map[string]any, error) {
	s.attachLists = append(s.attachLists, accountID)
	return s.memoryTableStore.ListAttachments(ctx, accountID)
}

func (s *operatorWorkspacePagingStore) GetAccount(ctx context.Context, id string) (map[string]any, bool, error) {
	s.getAccountIDs = append(s.getAccountIDs, id)
	rows, err := s.memoryTableStore.ListAccounts(ctx, id)
	return findRecord(rows, id), len(rows) == 1, err
}

func (s *operatorWorkspacePagingStore) GetUser(ctx context.Context, id string) (map[string]any, bool, error) {
	s.getUserIDs = append(s.getUserIDs, id)
	rows, err := s.memoryTableStore.ListUsers(ctx, true)
	row := findRecord(rows, id)
	return row, row != nil, err
}

func (s *operatorWorkspacePagingStore) GetCompute(ctx context.Context, id string) (map[string]any, bool, error) {
	s.getComputeIDs = append(s.getComputeIDs, id)
	rows, err := s.memoryTableStore.ListComputes(ctx, "")
	row := findRecord(rows, id)
	return row, row != nil, err
}

func (s *operatorWorkspacePagingStore) GetStorage(ctx context.Context, id string) (map[string]any, bool, error) {
	s.getStorageIDs = append(s.getStorageIDs, id)
	rows, err := s.memoryTableStore.ListStorages(ctx, "")
	row := findRecord(rows, id)
	return row, row != nil, err
}

func (s *operatorWorkspacePagingStore) GetAttachment(ctx context.Context, id string) (map[string]any, bool, error) {
	s.getAttachmentIDs = append(s.getAttachmentIDs, id)
	rows, err := s.memoryTableStore.ListAttachments(ctx, "")
	row := findRecord(rows, id)
	return row, row != nil, err
}

func (s *operatorWorkspacePagingStore) PageWorkspaces(ctx context.Context, accountID string, query tablePageQuery) (tablePage, error) {
	s.pages = append(s.pages, struct {
		accountID string
		query     tablePageQuery
	}{accountID: accountID, query: query})
	return s.memoryTableStore.PageWorkspaces(ctx, accountID, query)
}

func (s *operatorWorkspacePagingStore) GetWorkspace(ctx context.Context, id string) (map[string]any, bool, error) {
	s.getIDs = append(s.getIDs, id)
	rows, err := s.memoryTableStore.ListWorkspaces(ctx, "")
	row := findRecord(rows, id)
	return row, row != nil, err
}

func (f *operatorProjectionFactsFabric) ProviderFactsBatch(_ context.Context, input clients.ProviderFactsBatchInput) (clients.ProviderFactsBatch, error) {
	f.inputs = append(f.inputs, input)
	result := clients.ProviderFactsBatch{Items: make([]clients.ProviderFact, 0, len(input.Items))}
	for _, item := range input.Items {
		fact, ok := f.facts[item.ResourceType+":"+item.ResourceID]
		if !ok {
			fact = clients.ProviderFact{
				AccountID: item.AccountID, WorkspaceID: item.WorkspaceID, ResourceType: item.ResourceType, ResourceID: item.ResourceID,
			}
		}
		result.Items = append(result.Items, fact)
	}
	return result, nil
}

func decodeOperatorEnvelope(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode operator envelope: %v: %s", err, response.Body.String())
	}
	if envelope["available"] != true {
		t.Fatalf("operator envelope = %#v", envelope)
	}
	if _, err := time.Parse(time.RFC3339Nano, stringValue(envelope["fetchedAt"])); err != nil {
		t.Fatalf("operator fetchedAt = %#v: %v", envelope["fetchedAt"], err)
	}
	return envelope
}

func (l *operatorProjectionLedger) Receipt(_ context.Context, id string) (clients.Receipt, error) {
	if l.err != nil {
		return clients.Receipt{}, l.err
	}
	receipt, ok := l.receipts[id]
	if !ok {
		return clients.Receipt{}, errors.New("receipt unavailable")
	}
	return receipt, nil
}

func newOperatorProjectionClient(users ...clients.Sub2APIUser) *operatorProjectionSub2API {
	return &operatorProjectionSub2API{
		testSub2APIClient: &testSub2APIClient{balance: 1_000_000_000, charges: map[string]int64{}},
		users:             users,
		adminUserErrs:     map[int64]error{},
		userUsage:         map[int64]clients.Sub2APIBatchUserUsage{},
		keyUsage:          map[int64]clients.Sub2APIBatchKeyUsage{},
		keyCounts:         map[int64]int{},
		keyCountErrs:      map[int64]error{},
	}
}

func seedOperatorProjectionAccount(t *testing.T, store controlPlaneTableStore, accountID, userID, email string, remoteID int64) {
	t.Helper()
	mustStore(t, store.CreateProvisionedAccount(context.Background(),
		map[string]any{"id": accountID, "ownerUserId": userID, "sub2apiUserId": remoteID, "status": "active", "workspacePurchaseEnabled": true, "updatedAt": operatorProjectionTime.Add(-2 * time.Hour).Format(time.RFC3339)},
		map[string]any{"id": userID, "email": email, "accountId": accountID, "role": "owner", "status": "active"},
	))
}

func operatorProjectionUser(id int64, email, status string, balance int64) clients.Sub2APIUser {
	return clients.Sub2APIUser{ID: id, Email: email, Status: status, BalanceUSDMicros: balance, CreatedAt: operatorProjectionTime.Add(-time.Hour), UpdatedAt: operatorProjectionTime}
}

func operatorAccountItem(items []any, accountID string) map[string]any {
	for _, raw := range items {
		if item := raw.(map[string]any); item["accountId"] == accountID {
			return item
		}
	}
	return nil
}

func TestOperatorProjectionUsesBatchAPIs(t *testing.T) {
	store := newMemoryTableStore()
	seedOperatorProjectionAccount(t, store, "acct-alpha", "usr-alpha", "alpha@example.com", 41)
	seedOperatorProjectionAccount(t, store, "acct-beta", "usr-beta", "beta@example.com", 42)
	for _, workspace := range []map[string]any{
		{"id": "ws-alpha", "ownerAccountId": "acct-alpha", "ownerUserId": "usr-alpha", "accountId": "acct-alpha", "state": "active", "createdAt": operatorProjectionTime.Add(-time.Hour).Format(time.RFC3339), "updatedAt": operatorProjectionTime.Format(time.RFC3339), "workspaceApiKeyId": int64(7)},
		{"id": "ws-beta", "ownerAccountId": "acct-beta", "ownerUserId": "usr-beta", "accountId": "acct-beta", "state": "active", "createdAt": operatorProjectionTime.Add(-time.Hour).Format(time.RFC3339), "updatedAt": operatorProjectionTime.Format(time.RFC3339), "workspaceApiKeyId": int64(9)},
	} {
		mustStore(t, store.SaveWorkspace(context.Background(), workspace))
	}
	client := newOperatorProjectionClient(
		operatorProjectionUser(41, "alpha@example.com", "active", 10_000_000),
		operatorProjectionUser(42, "beta@example.com", "disabled", 20_000_000),
	)
	client.userUsage[41] = clients.Sub2APIBatchUserUsage{UserID: 41, TodayActualCostUSDMicros: 1, TotalActualCostUSDMicros: 100}
	client.userUsage[42] = clients.Sub2APIBatchUserUsage{UserID: 42, TodayActualCostUSDMicros: 2, TotalActualCostUSDMicros: 200}
	client.keyCounts[41] = 2
	client.keyCounts[42] = 1
	client.keyUsage[7] = clients.Sub2APIBatchKeyUsage{APIKeyID: 7, TotalActualCostUSDMicros: 70}
	client.keyUsage[9] = clients.Sub2APIBatchKeyUsage{APIKeyID: 9, TotalActualCostUSDMicros: 90}
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, &fakeFabricClient{}, client), store)
	if err != nil {
		t.Fatal(err)
	}
	operator := reservedOperatorSessionForTest(t, server)

	workspaces := requestWithSession(t, server, operator, http.MethodGet, "/api/operator/workspaces?page=1&pageSize=20", "")
	if workspaces.Code != http.StatusOK {
		t.Fatalf("operator workspaces = %d: %s", workspaces.Code, workspaces.Body.String())
	}
	workspaceData := mapField(decodeOperatorEnvelope(t, workspaces), "data")
	workspaceItems := workspaceData["items"].([]any)
	keyUsage := mapField(workspaceItems[0].(map[string]any), "workspaceKeyUsage")
	if mapField(keyUsage, "data")["totalActualCostUsdMicros"] != float64(70) {
		t.Fatalf("workspace key usage = %#v", workspaceItems[0])
	}
	// The operator account page is retired with the account bridges, so the
	// remaining workspace projection is the only surface here: it reads the
	// workspace key usage through the batch API and no per-item user read.
	if client.adminUsersCalls != 0 || client.adminUserCalls != 0 || client.batchUsersCalls != 0 || client.batchKeysCalls != 1 || client.singleUserCalls != 0 || len(client.keyCountCalls) != 0 {
		t.Fatalf("projection calls userPages=%d exactUsers=%d batchUsers=%d batchKeys=%d identityUsers=%d keyCounts=%#v", client.adminUsersCalls, client.adminUserCalls, client.batchUsersCalls, client.batchKeysCalls, client.singleUserCalls, client.keyCountCalls)
	}
}

func TestOperatorOverviewUsesControlPlaneAggregatesInsteadOfCurrentPageProjection(t *testing.T) {
	store := &operatorOverviewAggregateStore{memoryTableStore: newMemoryTableStore()}
	seedOperatorProjectionAccount(t, store, "acct-alpha", "usr-alpha", "alpha@example.com", 41)
	seedOperatorProjectionAccount(t, store, "acct-beta", "usr-beta", "beta@example.com", 42)
	account, found, err := store.GetAccount(context.Background(), "acct-beta")
	if err != nil || !found {
		t.Fatalf("beta account = %#v found=%t err=%v", account, found, err)
	}
	account["status"] = "disabled"
	mustStore(t, store.SaveAccount(context.Background(), account))

	fabric := &runtimeHealthSummaryFabric{observations: operatorRuntimeFixtureObservations(17)}
	data, err := (&controlPlaneServer{tables: store}).operatorOverview(context.Background(), controlplane.NewService(fakeLedgerClient{}, fabric, newOperatorProjectionClient(operatorProjectionUser(1, "admin@opl.local", "active", 0), operatorProjectionUser(41, "alpha@example.com", "active", 2), operatorProjectionUser(42, "beta@example.com", "disabled", 3))))
	if err != nil {
		t.Fatal(err)
	}
	accounts := mapField(data, "accounts")
	if accounts["available"] != true || mapField(accounts, "data")["total"] != 3 || mapField(accounts, "data")["active"] != 2 || mapField(accounts, "data")["disabled"] != 1 {
		t.Fatalf("overview accounts = %#v", accounts)
	}
	if store.pageAccountCalls != 0 || store.aggregateCalls != 1 {
		t.Fatalf("overview must not derive totals from paged accounts: page=%d aggregate=%d", store.pageAccountCalls, store.aggregateCalls)
	}
	if wallet := mapField(data, "wallet"); wallet["available"] != true || mapField(wallet, "data")["usdMicros"] != "5" {
		t.Fatalf("complete OPL wallet must be available: %#v", wallet)
	}
	resources := mapField(data, "resources")
	if resources["available"] != true || mapField(resources, "data")["total"] != float64(17) {
		t.Fatalf("overview Fabric resources = %#v", resources)
	}
	fabric.mu.Lock()
	statusCalls, summaryCalls := fabric.calls, fabric.observationCalls
	fabric.mu.Unlock()
	if statusCalls != 0 || summaryCalls != 1 {
		t.Fatalf("overview Runtime reads status=%d summary=%d", statusCalls, summaryCalls)
	}
}

func TestOperatorWorkspaceListAndDetailUseBoundedStoreReads(t *testing.T) {
	store := &operatorWorkspacePagingStore{memoryTableStore: newMemoryTableStore()}
	seedOperatorProjectionAccount(t, store, "acct-alpha", "usr-alpha", "alpha@example.com", 41)
	for _, id := range []string{"ws-alpha", "ws-beta"} {
		mustStore(t, store.SaveWorkspace(context.Background(), map[string]any{
			"id": id, "ownerAccountId": "acct-alpha", "ownerUserId": "usr-alpha", "accountId": "acct-alpha", "state": "active",
			"createdAt": "2026-07-18T00:00:00Z", "updatedAt": "2026-07-19T00:00:00Z", "currentComputeAllocationId": "compute-" + id,
			"storageId": "storage-" + id, "currentAttachmentId": "attachment-" + id,
		}))
		mustStore(t, store.SaveCompute(context.Background(), map[string]any{"id": "compute-" + id, "accountId": "acct-alpha", "workspaceId": id}))
		mustStore(t, store.SaveStorage(context.Background(), map[string]any{"id": "storage-" + id, "accountId": "acct-alpha", "workspaceId": id}))
		mustStore(t, store.SaveAttachment(context.Background(), map[string]any{"id": "attachment-" + id, "accountId": "acct-alpha", "workspaceId": id}))
	}
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, &fakeFabricClient{}, newOperatorProjectionClient(operatorProjectionUser(41, "alpha@example.com", "active", 1_000_000))), store)
	if err != nil {
		t.Fatal(err)
	}
	operator := reservedOperatorSessionForTest(t, server)
	store.accountLists = nil
	store.userLists = 0
	store.computeLists = nil
	store.storageLists = nil
	store.attachLists = nil
	store.pages = nil
	store.getAccountIDs = nil
	store.getUserIDs = nil
	store.getComputeIDs = nil
	store.getStorageIDs = nil
	store.getAttachmentIDs = nil
	store.getIDs = nil
	list := requestWithSession(t, server, operator, http.MethodGet, "/api/operator/workspaces?page=2&pageSize=1", "")
	if list.Code != http.StatusOK {
		t.Fatalf("operator workspace page=%d body=%s", list.Code, list.Body.String())
	}
	data := mapField(decodeOperatorEnvelope(t, list), "data")
	items := data["items"].([]any)
	if data["total"] != float64(2) || len(items) != 1 || len(store.pages) != 1 || store.pages[0].accountID != "" || store.pages[0].query != (tablePageQuery{Offset: 1, Limit: 1}) {
		t.Fatalf("operator workspace pagination data=%#v pages=%#v", data, store.pages)
	}
	if len(store.accountLists) != 0 || store.userLists != 0 || len(store.computeLists) != 0 || len(store.storageLists) != 0 || len(store.attachLists) != 0 ||
		!reflect.DeepEqual(store.getAccountIDs, []string{"acct-alpha"}) || !reflect.DeepEqual(store.getUserIDs, []string{"usr-admin", "usr-alpha"}) ||
		!reflect.DeepEqual(store.getComputeIDs, []string{"compute-ws-beta"}) || !reflect.DeepEqual(store.getStorageIDs, []string{"storage-ws-beta"}) ||
		!reflect.DeepEqual(store.getAttachmentIDs, []string{"attachment-ws-beta"}) {
		t.Fatalf("operator current-page facts scanned tables: accounts=%#v users=%d computes=%#v storages=%#v attachments=%#v gets=%#v/%#v/%#v/%#v/%#v", store.accountLists, store.userLists, store.computeLists, store.storageLists, store.attachLists, store.getAccountIDs, store.getUserIDs, store.getComputeIDs, store.getStorageIDs, store.getAttachmentIDs)
	}
	detail := requestWithSession(t, server, operator, http.MethodGet, "/api/operator/workspaces/ws-beta", "")
	if detail.Code != http.StatusOK || !reflect.DeepEqual(store.getIDs, []string{"ws-beta"}) {
		t.Fatalf("operator detail=%d body=%s get=%#v", detail.Code, detail.Body.String(), store.getIDs)
	}
}

func TestOperatorOverviewRejectsMoneyAggregationOverflow(t *testing.T) {
	store := newMemoryTableStore()
	seedOperatorProjectionAccount(t, store, "acct-alpha", "usr-alpha", "alpha@example.com", 41)
	seedOperatorProjectionAccount(t, store, "acct-beta", "usr-beta", "beta@example.com", 42)
	client := newOperatorProjectionClient(
		operatorProjectionUser(1, "admin@opl.local", "active", 0),
		operatorProjectionUser(41, "alpha@example.com", "active", math.MaxInt64),
		operatorProjectionUser(42, "beta@example.com", "active", 1),
	)
	client.userUsage[1] = clients.Sub2APIBatchUserUsage{UserID: 1}
	client.userUsage[41] = clients.Sub2APIBatchUserUsage{UserID: 41, TodayActualCostUSDMicros: math.MaxInt64, TotalActualCostUSDMicros: math.MaxInt64}
	client.userUsage[42] = clients.Sub2APIBatchUserUsage{UserID: 42, TodayActualCostUSDMicros: 1, TotalActualCostUSDMicros: 1}
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, &fakeFabricClient{}, client), store)
	if err != nil {
		t.Fatal(err)
	}
	response := requestWithSession(t, server, reservedOperatorSessionForTest(t, server), http.MethodGet, "/api/operator/overview", "")
	if response.Code != http.StatusOK {
		t.Fatalf("operator overview = %d: %s", response.Code, response.Body.String())
	}
	data := mapField(decodeOperatorEnvelope(t, response), "data")
	if mapField(data, "keys")["available"] != true || client.adminUserCalls != 3 {
		t.Fatal("overflow test did not reach complete validated aggregation")
	}
	for _, field := range []string{"wallet", "usage"} {
		envelope := mapField(data, field)
		if envelope["status"] != "unavailable" || envelope["available"] != false {
			t.Fatalf("overflowed %s must be unavailable: %#v", field, envelope)
		}
		if _, exists := envelope["data"]; exists {
			t.Fatalf("overflowed %s exposed data: %#v", field, envelope)
		}
	}
}

func TestOperatorProjectionReadSurfaces(t *testing.T) {
	store := newMemoryTableStore()
	seedOperatorProjectionAccount(t, store, "acct-alpha", "usr-alpha", "alpha@example.com", 41)
	mustStore(t, store.SaveWorkspace(context.Background(), map[string]any{
		"id": "ws-alpha", "ownerAccountId": "acct-alpha", "ownerUserId": "usr-alpha", "accountId": "acct-alpha", "state": "active",
		"createdAt": "2026-07-18T00:00:00Z", "updatedAt": "2026-07-19T00:00:00Z",
	}))
	client := newOperatorProjectionClient(operatorProjectionUser(41, "alpha@example.com", "active", 1_000_000))
	client.userUsage[41] = clients.Sub2APIBatchUserUsage{UserID: 41, TotalActualCostUSDMicros: 100}
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, &fakeFabricClient{}, client), store)
	if err != nil {
		t.Fatal(err)
	}
	operator := reservedOperatorSessionForTest(t, server)
	for _, path := range []string{"/api/operator/overview", "/api/operator/reconciliation", "/api/operator/health"} {
		response := requestWithSession(t, server, operator, http.MethodGet, path, "")
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", path, response.Code, response.Body.String())
		}
		envelope := decodeOperatorEnvelope(t, response)
		if _, ok := envelope["data"].(map[string]any); !ok {
			t.Fatalf("GET %s data = %#v", path, envelope)
		}
	}
	// The retained account page is retired with the account bridges: Control
	// Plane serves no operator account list at all, so the route is gone.
	retired := requestWithSession(t, server, operator, http.MethodGet, "/api/operator/accounts?page=1&pageSize=20", "")
	if retired.Code != http.StatusNotFound {
		t.Fatalf("retired operator accounts route = %d: %s", retired.Code, retired.Body.String())
	}
}

func TestOperatorHealthProjectsRedactedWorkspaceLaunchDecodeDiagnosticsBesidePilot(t *testing.T) {
	t.Setenv(controlledBasicPilotEnabledEnv, "1")
	t.Setenv(controlledBasicPilotMaxInFlightEnv, "1")
	store := newMemoryTableStore()
	secretOperationID := "workspace-launch-secret-operation"
	secretAccountID := "acct-secret-diagnostic"
	secretWorkspaceID := "ws-secret-diagnostic"
	result := map[string]any{
		"schemaVersion":    2,
		"phase":            "debit_pending",
		"diagnosticSecret": "hidden-result-value",
		"requestHash":      strings.Repeat("a", 64),
		"accountId":        secretAccountID,
		"workspaceId":      secretWorkspaceID,
		"attempts": map[string]any{
			"storage":          map[string]any{"attempted": 1, "confirmed": 0, "unknown": 1, "status": "unknown"},
			"hiddenattemptkey": map[string]any{"status": "hiddenattemptstatus"},
		},
		"observations": map[string]any{
			"storage":              map[string]any{"state": "unknown"},
			"hiddenobservationkey": map[string]any{"state": "hiddenobservationstate"},
		},
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), map[string]any{
		"id": secretOperationID, "operationId": secretOperationID, "accountId": secretAccountID, "workspaceId": secretWorkspaceID,
		"action": workspaceLaunchAction, "status": "hidden-operation-status", "result": string(mustJSON(result)),
		"createdAt": "hidden-created-at", "updatedAt": "2026-08-12T00:01:00Z",
	}))
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, &fakeFabricClient{}, newOperatorProjectionClient()), store)
	if err != nil {
		t.Fatal(err)
	}
	response := requestWithSession(t, server, reservedOperatorSessionForTest(t, server), http.MethodGet, "/api/operator/health", "")
	if response.Code != http.StatusOK {
		t.Fatalf("health status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, secret := range []string{
		secretOperationID, secretAccountID, secretWorkspaceID, "hidden-result-value", "hidden-created-at",
		"hidden-operation-status", "hiddenattemptkey", "hiddenattemptstatus", "hiddenobservationkey", "hiddenobservationstate",
	} {
		if strings.Contains(body, secret) {
			t.Fatalf("health leaked %q: %s", secret, body)
		}
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatal(err)
	}
	health := mapField(envelope, "data")
	pilotEnvelope := mapField(health, "controlledBasicPilot")
	pilot := mapField(pilotEnvelope, "data")
	wantPilotKeys := []string{"accountEligibilityAuthority", "alerts", "availableCapacity", "configured", "disableRequired", "enabled", "failures", "inFlight", "manualReview", "maxInFlight", "stages"}
	gotPilotKeys := make([]string, 0, len(pilot))
	for key := range pilot {
		gotPilotKeys = append(gotPilotKeys, key)
	}
	slices.Sort(gotPilotKeys)
	if !slices.Equal(gotPilotKeys, wantPilotKeys) {
		t.Fatalf("pilot keys=%#v", gotPilotKeys)
	}
	diagnosticEnvelope := mapField(health, "workspaceLaunchOperationDiagnostics")
	diagnostic := mapField(diagnosticEnvelope, "data")
	if diagnostic["schemaVersion"] != float64(1) || diagnostic["failedOperationCount"] != float64(1) {
		t.Fatalf("diagnostic=%#v", diagnostic)
	}
	items := diagnostic["operations"].([]any)
	item := items[0].(map[string]any)
	if item["failureCategory"] != "schema_version_mismatch" || item["schemaVersion"] != float64(2) ||
		item["attemptsKeyCount"] != float64(2) || item["observationsKeyCount"] != float64(2) || item["updatedAt"] != "2026-08-12T00:01:00Z" {
		t.Fatalf("diagnostic item=%#v", item)
	}
	if _, exists := item["status"]; exists {
		t.Fatalf("diagnostic included unknown status: %#v", item)
	}
	if _, exists := item["createdAt"]; exists {
		t.Fatalf("diagnostic included invalid createdAt: %#v", item)
	}
	if digest := stringValue(item["operationIdentityDigest"]); !strings.HasPrefix(digest, "sha256:") || len(digest) != 71 {
		t.Fatalf("identity digest=%q", digest)
	}
}

func TestOperatorReconciliationOnlyProjectsBillingReviewFacts(t *testing.T) {
	store := newMemoryTableStore()
	for _, operation := range []map[string]any{
		{"id": "gateway-key", "operationId": "gateway-key", "action": "gateway.key.create", "status": "manual_review"},
		{"id": "account-provision", "operationId": "account-provision", "action": "account.provision", "status": "manual_review"},
		{"id": "runtime-resume", "operationId": "runtime-resume", "action": "runtime.resume", "status": "started"},
		{"id": "launch-started", "operationId": "launch-started", "action": workspaceLaunchAction, "status": "started"},
		{"id": "launch-succeeded", "operationId": "launch-succeeded", "action": workspaceLaunchAction, "status": "succeeded"},
	} {
		mustStore(t, store.SaveRuntimeOperation(context.Background(), operation))
	}
	applyBillingReconciliationForTest(t, store, billingReconciliationRowForTest("reconcile-ok", "ok", "operator_reconciliation"), "")
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, &fakeFabricClient{}, newOperatorProjectionClient()), store)
	if err != nil {
		t.Fatal(err)
	}
	response := requestWithSession(t, server, reservedOperatorSessionForTest(t, server), http.MethodGet, "/api/operator/reconciliation", "")
	if response.Code != http.StatusOK {
		t.Fatalf("reconciliation status=%d body=%s", response.Code, response.Body.String())
	}
	envelope := decodeOperatorEnvelope(t, response)
	data := mapField(envelope, "data")
	if envelope["status"] != "empty" || data["total"] != float64(0) || len(data["items"].([]any)) != 0 {
		t.Fatalf("non-billing operations leaked into review queue: %#v", envelope)
	}
}

func TestOperatorReconciliationIgnoresHistoricalResourceBillingReviews(t *testing.T) {
	store := newMemoryTableStore()
	mustStore(t, store.SaveCompute(context.Background(), map[string]any{
		"id": "compute-review", "accountId": "acct-alpha", "billingOperationId": "compute-billing", "billingStatus": "manual_review", "lastBillingError": "compute_unknown",
	}))
	mustStore(t, store.SaveStorage(context.Background(), map[string]any{
		"id": "storage-review", "accountId": "acct-alpha", "billingOperationId": "storage-billing", "billingStatus": "manual_review", "lastBillingError": "storage_unknown",
	}))
	renewal := workspaceRenewalOperation{
		ID: "renewal-billing", Status: "manual_review", RequestHash: "renewal-hash", Phase: "provider_review",
		AccountID: "acct-alpha", WorkspaceID: "ws-alpha", PaidThrough: "2026-08-19T00:00:00Z", ErrorCode: "provider_unknown",
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), workspaceRenewalOperationRow(renewal)))
	applyBillingReconciliationForTest(t, store, billingReconciliationRowForTest("reconcile-mismatch", "mismatch", "balance_mismatch"), "")
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, &fakeFabricClient{}, newOperatorProjectionClient()), store)
	if err != nil {
		t.Fatal(err)
	}
	response := requestWithSession(t, server, reservedOperatorSessionForTest(t, server), http.MethodGet, "/api/operator/reconciliation", "")
	if response.Code != http.StatusOK {
		t.Fatalf("reconciliation status=%d body=%s", response.Code, response.Body.String())
	}
	data := mapField(decodeOperatorEnvelope(t, response), "data")
	items := data["items"].([]any)
	if data["total"] != float64(2) || len(items) != 2 {
		t.Fatalf("billing review items=%#v", data)
	}
	byID := map[string]map[string]any{}
	for _, raw := range items {
		item := raw.(map[string]any)
		byID[stringValue(item["id"])] = item
	}
	if byID["compute-review"] != nil || byID["storage-review"] != nil {
		t.Fatalf("historical resource billing reviews leaked into active queue: %#v", byID)
	}
	item := byID["ws-alpha"]
	actions, _ := item["allowedActions"].([]any)
	if item["resourceType"] != "workspace" || item["status"] != "manual_review" || len(actions) != 1 || actions[0] != "resolve_billing_review" {
		t.Fatalf("workspace renewal review=%#v", item)
	}
	if byID["ws-alpha"]["billingOperationId"] != renewal.ID || byID["ws-alpha"]["operationRef"] != renewal.ID {
		t.Fatalf("renewal identity=%#v", byID["ws-alpha"])
	}
	mismatchActions, _ := byID["reconcile-mismatch"]["allowedActions"].([]any)
	if byID["reconcile-mismatch"]["status"] != "mismatch" || len(mismatchActions) != 0 {
		t.Fatalf("mismatch review=%#v", byID["reconcile-mismatch"])
	}
}

func TestOperatorResourceOwnerFields(t *testing.T) {
	store := newMemoryTableStore()
	seedOperatorProjectionAccount(t, store, "acct-alpha", "usr-alpha", "alpha@example.com", 41)
	mustStore(t, store.SaveWorkspace(context.Background(), map[string]any{
		"id": "ws-alpha", "name": "Alpha", "accountId": "acct-alpha", "ownerAccountId": "acct-alpha", "ownerUserId": "usr-alpha", "state": "active",
		"packageId": "basic", "runtimeId": "runtime-alpha", "currentComputeAllocationId": "compute-alpha", "createdAt": "2026-07-18T00:00:00Z", "updatedAt": "2026-07-19T00:00:00Z", "receiptId": "receipt-workspace",
	}))
	mustStore(t, store.SaveCompute(context.Background(), map[string]any{
		"id": "compute-alpha", "accountId": "acct-alpha", "workspaceId": "ws-alpha", "packageId": "basic", "instanceType": "STALE.INSTANCE",
		"providerResourceId": "ins-stale", "zone": "ap-guangzhou-1", "providerStatus": "stopped", "status": "stopped", "deadline": "2026-07-18T00:00:00Z",
		"lastProviderSyncAt": "2026-07-18T03:00:00Z", "operationId": "op-compute", "lastReceiptId": "receipt-compute", "createdAt": "2026-07-18T00:00:00Z", "updatedAt": "2026-07-19T03:00:00Z",
	}))
	ledger := &operatorProjectionLedger{receipts: map[string]clients.Receipt{
		"receipt-workspace": {ReceiptInput: clients.ReceiptInput{AccountID: "acct-alpha", WorkspaceID: "ws-alpha", Type: "workspace.created", Status: "completed"}, ReceiptID: "receipt-workspace"},
		"receipt-compute":   {ReceiptInput: clients.ReceiptInput{AccountID: "acct-alpha", WorkspaceID: "ws-alpha", Type: "billing.resource_purchased.v1", Status: "completed", Cost: map[string]any{"resourceType": "compute", "resourceId": "compute-alpha"}}, ReceiptID: "receipt-compute"},
	}}
	client := newOperatorProjectionClient(operatorProjectionUser(41, "alpha@example.com", "active", 1_000_000))
	fabric := &operatorProjectionFactsFabric{facts: map[string]clients.ProviderFact{
		"compute:compute-alpha": {
			AccountID: "acct-alpha", WorkspaceID: "ws-alpha", ResourceType: "compute", ResourceID: "compute-alpha", Available: true,
			Facts:       clients.ProviderResourceFacts{PackageOrSpec: "S5.MEDIUM4", ProviderID: "ins-alpha", Zone: "ap-shanghai-2", Status: "running", ExpiresAt: "2026-08-18T00:00:00Z", LastReadAt: "2026-07-19T03:00:00Z"},
			Observation: &contracts.ResourceObservation{Available: true, State: contracts.ResourceObservedRunning, PackageOrSpec: "S5.MEDIUM4", ProviderID: "ins-alpha", Zone: "ap-shanghai-2", ExpiresAt: "2026-08-18T00:00:00Z", ObservedAt: "2026-07-19T03:00:00Z"},
		},
		"runtime:runtime-alpha": {
			AccountID: "acct-alpha", WorkspaceID: "ws-alpha", ResourceType: "runtime", ResourceID: "runtime-alpha", Available: true,
			Observation: &contracts.ResourceObservation{Available: true, State: contracts.ResourceObservedRunning, ProviderID: "runtime-service-alpha", ObservedAt: "2026-07-19T03:01:00Z", ComputeRuntimeBinding: &contracts.WorkspaceComputeRuntimeBinding{Status: contracts.WorkspaceComputeRuntimeBindingMatched}},
			Facts: clients.ProviderResourceFacts{
				ProviderID: "runtime-service-alpha", Status: "running", LastReadAt: "2026-07-19T03:01:00Z",
				ComputeRuntimeBinding: &contracts.WorkspaceComputeRuntimeBinding{Status: contracts.WorkspaceComputeRuntimeBindingMatched},
			},
		},
	}}
	server, err := NewPersistentServer(controlplane.NewService(ledger, fabric, client), store)
	if err != nil {
		t.Fatal(err)
	}
	response := requestWithSession(t, server, reservedOperatorSessionForTest(t, server), http.MethodGet, "/api/operator/workspaces/ws-alpha", "")
	if response.Code != http.StatusOK {
		t.Fatalf("operator workspace detail = %d: %s", response.Code, response.Body.String())
	}
	data := mapField(decodeOperatorEnvelope(t, response), "data")
	resources := data["resources"].([]any)
	if len(resources) != 2 {
		t.Fatalf("resources = %#v", resources)
	}
	var resource, runtime map[string]any
	for _, raw := range resources {
		item := raw.(map[string]any)
		switch mapField(item, "resourceType")["data"] {
		case "compute":
			resource = item
		case "runtime":
			runtime = item
		}
	}
	if resource == nil || runtime == nil {
		t.Fatalf("live resources = %#v", resources)
	}
	want := map[string]any{
		"ownerAccount": "acct-alpha", "ownerUser": "usr-alpha", "workspace": "ws-alpha", "resourceType": "compute", "packageOrSpec": "S5.MEDIUM4",
		"providerId": "ins-alpha", "zone": "ap-shanghai-2", "status": "running", "expiresAt": "2026-08-18T00:00:00Z", "lastReadAt": "2026-07-19T03:00:00Z", "operationRef": "op-compute", "receiptRef": "receipt-compute",
	}
	for field, value := range want {
		envelope := mapField(resource, field)
		if envelope["available"] != true {
			t.Fatalf("%s unavailable: %#v", field, envelope)
		}
		got := envelope["data"]
		if object, ok := got.(map[string]any); ok {
			got = object["id"]
		}
		if got != value {
			t.Fatalf("%s = %#v, want %#v", field, got, value)
		}
	}
	created := mapField(resource, "createdAt")
	if created["status"] != "unavailable" || created["available"] != false {
		t.Fatalf("provider createdAt must stay unavailable: %#v", created)
	}
	if mapField(runtime, "providerId")["data"] != "runtime-service-alpha" || mapField(runtime, "status")["data"] != "running" {
		t.Fatalf("runtime provider facts = %#v", runtime)
	}
	bindingPayload, err := json.Marshal(mapField(runtime, "computeRuntimeBinding")["data"])
	if err != nil {
		t.Fatal(err)
	}
	var binding contracts.WorkspaceComputeRuntimeBinding
	if err := json.Unmarshal(bindingPayload, &binding); err != nil || binding.Status != contracts.WorkspaceComputeRuntimeBindingMatched {
		t.Fatalf("runtime compute binding = %#v err=%v", binding, err)
	}
	if len(fabric.inputs) != 1 || len(fabric.inputs[0].Items) != 2 {
		t.Fatalf("provider fact requests = %#v", fabric.inputs)
	}
}

func TestOperatorWorkspaceProviderFactsOnlyReadCurrentPage(t *testing.T) {
	store := newMemoryTableStore()
	seedOperatorProjectionAccount(t, store, "acct-alpha", "usr-alpha", "alpha@example.com", 41)
	fabric := &operatorProjectionFactsFabric{facts: map[string]clients.ProviderFact{}}
	for index := 0; index < 30; index++ {
		workspaceID := fmt.Sprintf("ws-%02d", index)
		computeID := fmt.Sprintf("compute-%02d", index)
		mustStore(t, store.SaveWorkspace(context.Background(), map[string]any{
			"id": workspaceID, "accountId": "acct-alpha", "ownerAccountId": "acct-alpha", "ownerUserId": "usr-alpha", "state": "active",
			"currentComputeAllocationId": computeID,
			"createdAt":                  "2026-07-18T00:00:00Z", "updatedAt": "2026-07-19T00:00:00Z",
		}))
		mustStore(t, store.SaveCompute(context.Background(), map[string]any{"id": computeID, "accountId": "acct-alpha", "workspaceId": workspaceID}))
		fabric.facts["compute:"+computeID] = clients.ProviderFact{
			AccountID: "acct-alpha", WorkspaceID: workspaceID, ResourceType: "compute", ResourceID: computeID, Available: true,
			Facts: clients.ProviderResourceFacts{ProviderID: "ins-" + computeID, Status: "running", LastReadAt: "2026-07-19T03:00:00Z"},
		}
	}
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, fabric, newOperatorProjectionClient(operatorProjectionUser(41, "alpha@example.com", "active", 1_000_000))), store)
	if err != nil {
		t.Fatal(err)
	}
	response := requestWithSession(t, server, reservedOperatorSessionForTest(t, server), http.MethodGet, "/api/operator/workspaces?page=2&pageSize=10", "")
	if response.Code != http.StatusOK {
		t.Fatalf("operator workspace page = %d: %s", response.Code, response.Body.String())
	}
	if len(fabric.inputs) != 1 || len(fabric.inputs[0].Items) != 10 {
		t.Fatalf("provider fact requests = %#v", fabric.inputs)
	}
	for index, item := range fabric.inputs[0].Items {
		want := fmt.Sprintf("compute-%02d", index+10)
		if item.ResourceID != want || item.WorkspaceID != fmt.Sprintf("ws-%02d", index+10) {
			t.Fatalf("provider fact item %d = %#v, want %s", index, item, want)
		}
	}
}

func TestOperatorResourceDoesNotLabelControlPlaneSnapshotAsFabric(t *testing.T) {
	store := newMemoryTableStore()
	seedOperatorProjectionAccount(t, store, "acct-alpha", "usr-alpha", "alpha@example.com", 41)
	mustStore(t, store.SaveWorkspace(context.Background(), map[string]any{
		"id": "ws-alpha", "name": "Alpha", "accountId": "acct-alpha", "ownerAccountId": "acct-alpha", "ownerUserId": "usr-alpha", "state": "active",
		"currentComputeAllocationId": "compute-alpha",
		"createdAt":                  "2026-07-18T00:00:00Z", "updatedAt": "2026-07-19T00:00:00Z",
	}))
	mustStore(t, store.SaveCompute(context.Background(), map[string]any{
		"id": "compute-alpha", "accountId": "acct-alpha", "workspaceId": "ws-alpha", "instanceType": "S5.MEDIUM4",
		"providerResourceId": "ins-stale", "zone": "ap-shanghai-2", "providerStatus": "running", "status": "running",
		"deadline": "2026-08-18T00:00:00Z", "lastProviderSyncAt": "2026-07-19T03:00:00Z",
	}))
	fabric := &operatorProjectionFactsFabric{facts: map[string]clients.ProviderFact{
		"compute:compute-alpha": {
			AccountID: "acct-other", WorkspaceID: "ws-alpha", ResourceType: "compute", ResourceID: "compute-alpha", Available: true,
			Facts: clients.ProviderResourceFacts{PackageOrSpec: "S5.MEDIUM4", ProviderID: "ins-live", Status: "running", LastReadAt: "2026-07-19T04:00:00Z"},
		},
	}}
	server, err := NewPersistentServer(controlplane.NewService(&operatorProjectionLedger{receipts: map[string]clients.Receipt{}}, fabric, newOperatorProjectionClient(operatorProjectionUser(41, "alpha@example.com", "active", 1_000_000))), store)
	if err != nil {
		t.Fatal(err)
	}
	response := requestWithSession(t, server, reservedOperatorSessionForTest(t, server), http.MethodGet, "/api/operator/workspaces/ws-alpha", "")
	if response.Code != http.StatusOK {
		t.Fatalf("operator workspace detail = %d: %s", response.Code, response.Body.String())
	}
	resources := mapField(decodeOperatorEnvelope(t, response), "data")["resources"].([]any)
	if len(resources) != 1 {
		t.Fatalf("resources = %#v", resources)
	}
	resource := resources[0].(map[string]any)
	for _, field := range []string{"packageOrSpec", "providerId", "zone", "status", "expiresAt", "lastReadAt"} {
		envelope := mapField(resource, field)
		if envelope["source"] == "fabric" && envelope["available"] == true {
			t.Fatalf("Control Plane snapshot was mislabeled as live Fabric %s: %#v", field, envelope)
		}
	}
}

func TestOperatorOverviewDoesNotCountUnavailableProviderFacts(t *testing.T) {
	store := newMemoryTableStore()
	seedOperatorProjectionAccount(t, store, "acct-alpha", "usr-alpha", "alpha@example.com", 41)
	mustStore(t, store.SaveWorkspace(context.Background(), map[string]any{
		"id": "ws-alpha", "accountId": "acct-alpha", "ownerAccountId": "acct-alpha", "ownerUserId": "usr-alpha", "state": "active",
		"currentComputeAllocationId": "compute-alpha",
		"createdAt":                  "2026-07-18T00:00:00Z", "updatedAt": "2026-07-19T00:00:00Z",
	}))
	mustStore(t, store.SaveCompute(context.Background(), map[string]any{
		"id": "compute-alpha", "accountId": "acct-alpha", "workspaceId": "ws-alpha", "providerResourceId": "ins-stale", "status": "running",
	}))
	client := newOperatorProjectionClient(operatorProjectionUser(41, "alpha@example.com", "active", 1_000_000))
	client.userUsage[41] = clients.Sub2APIBatchUserUsage{UserID: 41}
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, &operatorProjectionNoOperationsFabric{}, client), store)
	if err != nil {
		t.Fatal(err)
	}
	response := requestWithSession(t, server, reservedOperatorSessionForTest(t, server), http.MethodGet, "/api/operator/overview", "")
	if response.Code != http.StatusOK {
		t.Fatalf("operator overview = %d: %s", response.Code, response.Body.String())
	}
	resources := mapField(mapField(decodeOperatorEnvelope(t, response), "data"), "resources")
	if resources["status"] != "unavailable" || resources["available"] != false {
		t.Fatalf("unavailable provider facts were counted as Fabric resources: %#v", resources)
	}
}

func TestOperatorWorkspaceFirstPageKeyUsageStaysOneBatchAtScale(t *testing.T) {
	requestCounts := map[int]int{}
	for _, workspaceCount := range []int{100, 1000} {
		store := newMemoryTableStore()
		seedOperatorProjectionAccount(t, store, "acct-alpha", "usr-alpha", "alpha@example.com", 41)
		client := newOperatorProjectionClient(operatorProjectionUser(41, "alpha@example.com", "active", 1_000_000))
		for index := 1; index <= workspaceCount; index++ {
			workspaceID := fmt.Sprintf("ws-%04d", index)
			keyID := int64(1000 + index)
			mustStore(t, store.SaveWorkspace(context.Background(), map[string]any{
				"id": workspaceID, "ownerAccountId": "acct-alpha", "ownerUserId": "usr-alpha", "accountId": "acct-alpha",
				"state": "active", "createdAt": operatorProjectionTime.Add(-time.Hour).Format(time.RFC3339),
				"updatedAt": operatorProjectionTime.Format(time.RFC3339), "workspaceApiKeyId": keyID,
			}))
			client.keyUsage[keyID] = clients.Sub2APIBatchKeyUsage{APIKeyID: keyID}
		}
		app := &controlPlaneServer{tables: store}
		data, status, err := app.operatorWorkspacePage(context.Background(), controlplane.NewService(fakeLedgerClient{}, &fakeFabricClient{}, client), 1, 20)
		if err != nil || status != "available" || data["total"] != workspaceCount || len(data["items"].([]any)) != 20 {
			t.Fatalf("%d-workspace page=%#v status=%s err=%v", workspaceCount, data, status, err)
		}
		wantIDs := make([]int64, 0, 20)
		for id := int64(1001); id <= 1020; id++ {
			wantIDs = append(wantIDs, id)
		}
		if client.batchKeysCalls != 1 || len(client.requestedAPIKeyIDs) != 1 || !slices.Equal(client.requestedAPIKeyIDs[0], wantIDs) {
			t.Fatalf("%d-workspace key usage calls=%d ids=%#v", workspaceCount, client.batchKeysCalls, client.requestedAPIKeyIDs)
		}
		requestCounts[workspaceCount] = client.batchKeysCalls
	}
	if requestCounts[100] != 1 || requestCounts[1000] != 1 {
		t.Fatalf("workspace key usage request count must stay fixed: %#v", requestCounts)
	}
}

func TestOperatorCurrentPageUserReadsUseAtMostFourConcurrentRequests(t *testing.T) {
	users := make([]clients.Sub2APIUser, 0, 20)
	local := make([]map[string]any, 0, 20)
	for id := int64(1); id <= 20; id++ {
		users = append(users, operatorProjectionUser(id, fmt.Sprintf("user-%02d@example.com", id), "active", id))
		local = append(local, map[string]any{"remoteId": id})
	}
	started := make(chan int64, 20)
	release := make(chan struct{})
	client := newOperatorProjectionClient(users...)
	client.userReadStarted = started
	client.userReadRelease = release
	app := &controlPlaneServer{}
	done := make(chan map[int64]clients.Sub2APIUser, 1)
	go func() {
		done <- app.operatorCurrentPageUsers(context.Background(), controlplane.NewService(fakeLedgerClient{}, &fakeFabricClient{}, client), local, make(chan struct{}, operatorPageTotalConcurrency))
	}()

	for index := 0; index < 4; index++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for four current-page user reads")
		}
	}
	select {
	case id := <-started:
		t.Fatalf("fifth user read started before concurrency slot released: %d", id)
	case <-time.After(50 * time.Millisecond):
	}
	client.userReadMu.Lock()
	active, maximum := client.userReadActive, client.userReadMax
	client.userReadMu.Unlock()
	if active != 4 || maximum != 4 {
		t.Fatalf("current-page user read concurrency active=%d max=%d", active, maximum)
	}
	close(release)
	select {
	case result := <-done:
		if len(result) != 20 {
			t.Fatalf("current-page exact users = %d", len(result))
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for current-page user reads")
	}
	client.userReadMu.Lock()
	maximum = client.userReadMax
	client.userReadMu.Unlock()
	if maximum != 4 {
		t.Fatalf("current-page user read max concurrency = %d", maximum)
	}
}

func TestOperatorResourceUnavailableFields(t *testing.T) {
	store := newMemoryTableStore()
	seedOperatorProjectionAccount(t, store, "acct-alpha", "usr-alpha", "alpha@example.com", 41)
	mustStore(t, store.SaveWorkspace(context.Background(), map[string]any{
		"id": "ws-alpha", "accountId": "acct-alpha", "ownerAccountId": "acct-alpha", "ownerUserId": "usr-alpha", "state": "active",
		"currentComputeAllocationId": "compute-alpha", "createdAt": "2026-07-18T00:00:00Z", "updatedAt": "2026-07-19T00:00:00Z",
	}))
	mustStore(t, store.SaveCompute(context.Background(), map[string]any{"id": "compute-alpha", "accountId": "acct-alpha", "workspaceId": "ws-alpha", "status": "provisioning"}))
	client := newOperatorProjectionClient(operatorProjectionUser(41, "alpha@example.com", "active", 1_000_000))
	server, err := NewPersistentServer(controlplane.NewService(&operatorProjectionLedger{receipts: map[string]clients.Receipt{}}, &operatorProjectionNoOperationsFabric{}, client), store)
	if err != nil {
		t.Fatal(err)
	}
	response := requestWithSession(t, server, reservedOperatorSessionForTest(t, server), http.MethodGet, "/api/operator/workspaces/ws-alpha", "")
	if response.Code != http.StatusOK {
		t.Fatalf("operator workspace unavailable detail = %d: %s", response.Code, response.Body.String())
	}
	resource := mapField(decodeOperatorEnvelope(t, response), "data")["resources"].([]any)[0].(map[string]any)
	for _, field := range []string{"packageOrSpec", "providerId", "zone", "createdAt", "expiresAt", "lastReadAt", "computeRuntimeBinding", "operationRef", "receiptRef"} {
		envelope := mapField(resource, field)
		if envelope["status"] != "unavailable" || envelope["available"] != false {
			t.Fatalf("%s must be unavailable: %#v", field, envelope)
		}
		if _, exists := envelope["data"]; exists {
			t.Fatalf("%s unavailable data = %#v", field, envelope)
		}
	}
}

func TestOperatorWorkspacePurchaseEligibilityIsAuditedAndIdempotent(t *testing.T) {
	store := newMemoryTableStore()
	seedOperatorProjectionAccount(t, store, "acct-alpha", "usr-alpha", "alpha@example.com", 41)
	client := newOperatorProjectionClient(operatorProjectionUser(41, "alpha@example.com", "active", 1_000_000))
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, &fakeFabricClient{}, client), store)
	if err != nil {
		t.Fatal(err)
	}
	operator := reservedOperatorSessionForTest(t, server)
	revoke := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/accounts/acct-alpha/workspace-purchase-eligibility", `{"confirmationAccountId":"acct-alpha","enabled":false,"reason":"gateway_only_scope"}`, "eligibility-revoke")
	if revoke.Code != http.StatusOK || !strings.Contains(revoke.Body.String(), `"workspacePurchaseEnabled":false`) {
		t.Fatalf("eligibility revoke = %d: %s", revoke.Code, revoke.Body.String())
	}
	replay := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/accounts/acct-alpha/workspace-purchase-eligibility", `{"confirmationAccountId":"acct-alpha","enabled":false,"reason":"gateway_only_scope"}`, "eligibility-revoke")
	if replay.Code != http.StatusOK || !strings.Contains(replay.Body.String(), `"workspacePurchaseEnabled":false`) {
		t.Fatalf("eligibility replay = %d: %s", replay.Code, replay.Body.String())
	}
	conflict := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/accounts/acct-alpha/workspace-purchase-eligibility", `{"confirmationAccountId":"acct-alpha","enabled":true,"reason":"changed_request"}`, "eligibility-revoke")
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), errIdempotencyConflict.Error()) {
		t.Fatalf("eligibility conflict = %d: %s", conflict.Code, conflict.Body.String())
	}
	grant := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/accounts/acct-alpha/workspace-purchase-eligibility", `{"confirmationAccountId":"acct-alpha","enabled":true,"reason":"full_cloud_admission"}`, "eligibility-grant")
	if grant.Code != http.StatusOK || !strings.Contains(grant.Body.String(), `"workspacePurchaseEnabled":true`) {
		t.Fatalf("eligibility grant = %d: %s", grant.Code, grant.Body.String())
	}
	account, found, err := store.GetAccount(context.Background(), "acct-alpha")
	if err != nil || !found || !workspacePurchaseEnabled(account) {
		t.Fatalf("eligibility account readback = %#v found=%v err=%v", account, found, err)
	}
	audits, err := store.ListAuditEvents(context.Background(), "acct-alpha")
	if err != nil || len(audits) != 2 {
		t.Fatalf("eligibility audits = %#v err=%v", audits, err)
	}
}

func TestOperatorAccountDisable(t *testing.T) {
	store := newMemoryTableStore()
	seedOperatorProjectionAccount(t, store, "acct-alpha", "usr-alpha", "alpha@example.com", 41)
	client := newOperatorProjectionClient(operatorProjectionUser(41, "alpha@example.com", "active", 1_000_000))
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, &fakeFabricClient{}, client), store)
	if err != nil {
		t.Fatal(err)
	}
	response := requestWithMutationKeyForTest(t, server, reservedOperatorSessionForTest(t, server), http.MethodPost, "/api/operator/accounts/acct-alpha/disable", `{"confirmationAccountId":"acct-alpha","reason":"pilot_offboarding"}`, "disable-account-once")
	if response.Code != http.StatusOK {
		t.Fatalf("account disable = %d: %s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["accountId"] != "acct-alpha" || body["status"] != "succeeded" || !strings.HasPrefix(stringValue(body["operationId"]), "account-disable-") {
		t.Fatalf("account disable body = %#v", body)
	}
	users, err := store.ListUsers(context.Background(), true)
	if err != nil || stringValue(findRecord(users, "usr-alpha")["status"]) != "disabled" {
		t.Fatalf("disabled user = %#v err=%v", users, err)
	}
	admin := requestWithMutationKeyForTest(t, server, reservedOperatorSessionForTest(t, server), http.MethodPost, "/api/operator/accounts/acct-admin/disable", `{"confirmationAccountId":"acct-admin","reason":"pilot_offboarding"}`, "disable-admin-forbidden")
	if admin.Code != http.StatusBadRequest || !strings.Contains(admin.Body.String(), "last_active_admin") {
		t.Fatalf("admin disable status=%d body=%s", admin.Code, admin.Body.String())
	}
}
