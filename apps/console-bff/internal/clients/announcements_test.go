package clients

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
)

func TestRetainedOwnerConfigurationIsExplicit(t *testing.T) {
	for _, config := range []Config{
		{ControlPlaneURL: "http://127.0.0.1:8080"},
		{ControlPlaneToken: strings.Repeat("a", 32)},
		{ControlPlaneURL: "https://user:secret@example.test", ControlPlaneToken: strings.Repeat("a", 32)},
		{ControlPlaneURL: "https://example.test?token=a", ControlPlaneToken: strings.Repeat("a", 32)},
	} {
		if c, err := Dial(config); err == nil {
			c.Close()
			t.Fatalf("partial/credential URL configuration accepted")
		}
	}
}

// TestRetainedOwnerRedirectDoesNotForwardCredentials keeps the retained
// owner's own boundary: a redirect never carries this process's credential to
// another origin.
func TestRetainedOwnerRedirectDoesNotForwardCredentials(t *testing.T) {
	var reached bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))
	defer other.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	reader, err := newControlPlaneReader(origin.URL, strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reader.requestRaw(context.Background(), "reference", http.MethodGet, "/internal/cloud/legacy-billing/receipts", "", 1<<20); status.Code(err) != codes.Unavailable || reached {
		t.Fatalf("redirect=%v reached=%v", err, reached)
	}
}

// stubAnnouncementOwner is the CloudIdentity announcement owner the BFF
// reaches over its typed client.
type stubAnnouncementOwner struct {
	api.TenantProductServiceClient
	page       *api.AnnouncementPage
	read       *api.AnnouncementRead
	listCalls  int
	readCalls  int
	lastList   *api.ListAnnouncementsRpcRequest
	lastRead   *api.MarkAnnouncementReadRpcRequest
	operatorID int
}

func (s *stubAnnouncementOwner) ListAnnouncements(_ context.Context, request *api.ListAnnouncementsRpcRequest, _ ...grpc.CallOption) (*api.AnnouncementPage, error) {
	s.listCalls++
	s.lastList = request
	return s.page, nil
}

func (s *stubAnnouncementOwner) MarkAnnouncementRead(_ context.Context, request *api.MarkAnnouncementReadRpcRequest, _ ...grpc.CallOption) (*api.AnnouncementRead, error) {
	s.readCalls++
	s.lastRead = request
	return s.read, nil
}

func (s *stubAnnouncementOwner) ListOperatorAnnouncements(_ context.Context, request *api.ListAnnouncementsRpcRequest, _ ...grpc.CallOption) (*api.AnnouncementPage, error) {
	s.operatorID++
	s.lastList = request
	return s.page, nil
}

// TestAnnouncementClientReachesTheCloudIdentityOwner proves the announcement
// client calls the owner's typed surface and forwards the caller's own call
// context, never a Control Plane bridge path.
func TestAnnouncementClientReachesTheCloudIdentityOwner(t *testing.T) {
	owner := &stubAnnouncementOwner{
		page: &api.AnnouncementPage{Items: []*api.Announcement{{Id: "notice"}}, Total: 1, Page: 1, PageSize: 20},
		read: &api.AnnouncementRead{AnnouncementId: "notice", ReadAt: "2026-07-19T01:00:00Z"},
	}
	c := &Clients{tenant: owner}
	call := &api.CallContext{ActorId: "actor-1", SessionId: proto.String("session-1")}

	page, err := c.ListAnnouncements(context.Background(), call, 1, 20)
	if err != nil || page.GetTotal() != 1 || len(page.GetItems()) != 1 {
		t.Fatalf("list=%v err=%v", page, err)
	}
	if owner.listCalls != 1 || owner.lastList.GetPage() != 1 || owner.lastList.GetPageSize() != 20 || owner.lastList.GetContext() != call {
		t.Fatalf("the owner did not receive the call: %+v", owner.lastList)
	}

	receipt, err := c.MarkAnnouncementRead(context.Background(), call, "notice")
	if err != nil || receipt.GetAnnouncementId() != "notice" {
		t.Fatalf("receipt=%v err=%v", receipt, err)
	}
	if owner.readCalls != 1 || owner.lastRead.GetAnnouncementId() != "notice" {
		t.Fatalf("the owner did not receive the receipt: %+v", owner.lastRead)
	}

	if _, err = c.ListOperatorAnnouncements(context.Background(), call, 1, 20); err != nil || owner.operatorID != 1 {
		t.Fatalf("operator list=%d err=%v", owner.operatorID, err)
	}
}

// TestAnnouncementClientRefusesAnUnconfiguredOwner proves the client reports
// the missing owner instead of answering with an empty page.
func TestAnnouncementClientRefusesAnUnconfiguredOwner(t *testing.T) {
	c := &Clients{}
	call := &api.CallContext{ActorId: "actor-1"}
	if _, err := c.ListAnnouncements(context.Background(), call, 1, 20); !errors.Is(err, ErrUpstreamUnconfigured) {
		t.Fatalf("list err=%v", err)
	}
	if _, err := c.MarkAnnouncementRead(context.Background(), call, "notice"); !errors.Is(err, ErrUpstreamUnconfigured) {
		t.Fatalf("receipt err=%v", err)
	}
	if _, err := c.ListOperatorAnnouncements(context.Background(), call, 1, 20); !errors.Is(err, ErrUpstreamUnconfigured) {
		t.Fatalf("operator list err=%v", err)
	}
}
