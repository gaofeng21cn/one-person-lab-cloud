package main

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
)

const oldImage = "registry.example/workspace@sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const newImage = "registry.example/workspace@sha256:" + "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

type runtimeFixture struct {
	corruptAfterRemoval bool
	cri.UnimplementedImageServiceServer
	cri.UnimplementedRuntimeServiceServer
	images              []*cri.Image
	containers          []*cri.Container
	removals            int
	listCalls           int
	lostResponse        bool
	failContainers      bool
	protectOnSecondRead bool
	removedRequest      string
}

func (f *runtimeFixture) ListImages(context.Context, *cri.ListImagesRequest) (*cri.ListImagesResponse, error) {
	f.listCalls++
	if f.corruptAfterRemoval && f.removals > 0 {
		return &cri.ListImagesResponse{Images: []*cri.Image{nil}}, nil
	}
	return &cri.ListImagesResponse{Images: f.images}, nil
}
func (f *runtimeFixture) ListContainers(context.Context, *cri.ListContainersRequest) (*cri.ListContainersResponse, error) {
	if f.failContainers {
		return nil, errors.New("fixture_unavailable")
	}
	if f.protectOnSecondRead && f.listCalls >= 2 {
		return &cri.ListContainersResponse{Containers: []*cri.Container{{Id: "stopped", Image: &cri.ImageSpec{Image: oldImage}, ImageRef: "sha256:old", State: cri.ContainerState_CONTAINER_EXITED}}}, nil
	}
	return &cri.ListContainersResponse{Containers: f.containers}, nil
}
func (f *runtimeFixture) RemoveImage(_ context.Context, in *cri.RemoveImageRequest) (*cri.RemoveImageResponse, error) {
	f.removals++
	f.removedRequest = in.Image.Image
	f.images = nil
	if f.lostResponse {
		return nil, errors.New("fixture_response_lost")
	}
	return &cri.RemoveImageResponse{}, nil
}
func (f *runtimeFixture) ImageFsInfo(context.Context, *cri.ImageFsInfoRequest) (*cri.ImageFsInfoResponse, error) {
	// Shared layers: deleting an image need not release bytes immediately.
	return &cri.ImageFsInfoResponse{ImageFilesystems: []*cri.FilesystemUsage{{FsId: &cri.FilesystemIdentifier{Mountpoint: "/var/lib/containerd"}, UsedBytes: &cri.UInt64Value{Value: 2048}}}}, nil
}
func connectFixture(t *testing.T, fixture *runtimeFixture) (cri.ImageServiceClient, cri.RuntimeServiceClient) {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	cri.RegisterImageServiceServer(server, fixture)
	cri.RegisterRuntimeServiceServer(server, fixture)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///fixture", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return cri.NewImageServiceClient(conn), cri.NewRuntimeServiceClient(conn)
}
func TestRetirementBusinessChain(t *testing.T) {
	for _, tc := range []struct {
		name      string
		apply     bool
		configure func(*runtimeFixture, *request)
		want      string
		writes    int
	}{
		{name: "invalid post-removal inventory never proves absence", apply: true, want: statusFailed, writes: 1, configure: func(f *runtimeFixture, _ *request) { f.corruptAfterRemoval = true }},
		{name: "preview never removes", want: statusEligible},
		{name: "unused old version removed with shared layers", apply: true, want: statusRemoved, writes: 1},
		{name: "response lost reads back without second removal", apply: true, want: statusRemoved, writes: 1, configure: func(f *runtimeFixture, _ *request) { f.lostResponse = true }},
		{name: "already removed is idempotent", apply: true, want: statusAbsent, configure: func(f *runtimeFixture, _ *request) { f.images = nil }},
		{name: "pinned runtime image retained", apply: true, want: statusProtected, configure: func(f *runtimeFixture, _ *request) { f.images[0].Pinned = true }},
		{name: "rollback catalog alias retained", apply: true, want: statusProtected, configure: func(f *runtimeFixture, _ *request) {
			f.images[0].RepoDigests = append(f.images[0].RepoDigests, newImage)
		}},
		{name: "running workspace retained", apply: true, want: statusProtected, configure: func(f *runtimeFixture, _ *request) {
			f.containers = []*cri.Container{{Id: "running", Image: &cri.ImageSpec{Image: oldImage}, ImageRef: "sha256:old", State: cri.ContainerState_CONTAINER_RUNNING}}
		}},
		{name: "expired stopped workspace retained", apply: true, want: statusProtected, configure: func(f *runtimeFixture, _ *request) {
			f.containers = []*cri.Container{{Id: "stopped", Image: &cri.ImageSpec{Image: oldImage}, ImageRef: "sha256:old", State: cri.ContainerState_CONTAINER_EXITED}}
		}},
		{name: "new reference before removal retained", apply: true, want: statusProtected, configure: func(f *runtimeFixture, _ *request) { f.protectOnSecondRead = true }},
		{name: "unreadable containers prevent deletion", apply: true, want: statusFailed, configure: func(f *runtimeFixture, _ *request) { f.failContainers = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &runtimeFixture{images: []*cri.Image{{Id: "sha256:old", RepoDigests: []string{oldImage}}}}
			input := request{Image: oldImage, ProtectedImages: []string{newImage}, Apply: tc.apply}
			if tc.configure != nil {
				tc.configure(f, &input)
			}
			images, runtime := connectFixture(t, f)
			got := retire(context.Background(), input, images, runtime)
			if got.Status != tc.want || f.removals != tc.writes {
				t.Fatalf("outcome=%+v removals=%d", got, f.removals)
			}
			if f.removals > 0 && f.removedRequest != oldImage {
				t.Fatal("removal must use exact repository digest")
			}
			if got.Status == statusRemoved && (!got.ImageAbsent || *got.ImageFSUsedBytesBefore != *got.ImageFSUsedBytesAfter) {
				t.Fatalf("incorrect shared layer readback: %+v", got)
			}
			if got.Status == statusFailed && got.ErrorCode == "none" {
				t.Fatal("missing failure evidence")
			}
		})
	}
}
func TestInputRejectsMutableOrUnboundedAuthority(t *testing.T) {
	for _, raw := range []string{`{"image":"repo:latest","protectedImages":["safe"]}`, `{"image":"` + oldImage + `","protectedImages":[]}`, `{"image":"` + oldImage + `","protectedImages":["safe"],"prune":true}`, `{"image":"` + oldImage + `","protectedImages":["safe"]} {}`} {
		if _, err := decodeRequest(strings.NewReader(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
