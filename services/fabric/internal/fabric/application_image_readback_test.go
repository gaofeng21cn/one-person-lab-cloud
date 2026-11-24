package fabric

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/image"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type imageReadbackRunner struct {
	stubDockerClient
	response image.InspectResponse
	err      error
	calls    []string
}

func (r *imageReadbackRunner) ImageInspect(_ context.Context, imageID string, _ *ocispec.Platform) (image.InspectResponse, error) {
	r.calls = append(r.calls, imageID)
	return r.response, r.err
}

func TestApplicationImageReadbackDistinguishesAbsenceFromUnknown(t *testing.T) {
	ref := "registry.example/app@sha256:" + strings.Repeat("a", 64)
	other := "registry.example/app@sha256:" + strings.Repeat("b", 64)
	for _, tc := range []struct {
		name             string
		response         image.InspectResponse
		err              error
		present, wantErr bool
	}{
		{"digest only", image.InspectResponse{ID: "sha256:" + strings.Repeat("c", 64), RepoDigests: []string{ref}}, nil, true, false},
		{"confirmed absent", image.InspectResponse{}, fmt.Errorf("%w: No such image: %s", cerrdefs.ErrNotFound, ref), false, false},
		{"daemon unavailable", image.InspectResponse{}, errors.New("connection refused"), false, true},
		{"wrong digest", image.InspectResponse{ID: "image", RepoDigests: []string{other}}, nil, false, true},
		{"empty response", image.InspectResponse{}, nil, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &imageReadbackRunner{response: tc.response, err: tc.err}
			p := &LocalDockerProvider{docker: runner}
			present, err := p.applicationImagePresent(context.Background(), ref)
			if present != tc.present || (err != nil) != tc.wantErr {
				t.Fatalf("present=%v err=%v", present, err)
			}
			if !reflect.DeepEqual(runner.calls, []string{ref}) {
				t.Fatalf("unexpected mutation/read: %v", runner.calls)
			}
		})
	}
}
