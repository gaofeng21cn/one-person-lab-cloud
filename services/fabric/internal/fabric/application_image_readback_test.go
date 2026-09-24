package fabric

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type imageReadbackRunner struct {
	output []byte
	err    error
	calls  [][]string
}

func (r *imageReadbackRunner) Run(_ context.Context, _ []byte, args ...string) ([]byte, error) {
	r.calls = append(r.calls, args)
	return r.output, r.err
}
func TestApplicationImageReadbackDistinguishesAbsenceFromUnknown(t *testing.T) {
	ref := "registry.example/app@sha256:" + strings.Repeat("a", 64)
	other := "registry.example/app@sha256:" + strings.Repeat("b", 64)
	for _, tc := range []struct {
		name             string
		output           []byte
		err              error
		present, wantErr bool
	}{
		{"digest only", mustJSON([]dockerApplicationImageInspect{{ID: "sha256:" + strings.Repeat("c", 64), RepoDigests: []string{ref}}}), nil, true, false},
		{"confirmed absent", []byte("[]\nError response from daemon: No such image: " + ref + "\n"), errors.New("exit 1"), false, false},
		{"daemon unavailable", nil, errors.New("connection refused"), false, true},
		{"another image missing", []byte("Error response from daemon: No such image: " + other), errors.New("exit 1"), false, true},
		{"wrong digest", mustJSON([]dockerApplicationImageInspect{{ID: "image", RepoDigests: []string{other}}}), nil, false, true},
		{"empty inventory", []byte(`[]`), nil, false, true},
		{"malformed inventory", []byte(`{`), nil, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &imageReadbackRunner{output: tc.output, err: tc.err}
			p := &LocalDockerProvider{runner: runner}
			present, err := p.applicationImagePresent(context.Background(), ref)
			if present != tc.present || (err != nil) != tc.wantErr {
				t.Fatalf("present=%v err=%v", present, err)
			}
			if !reflect.DeepEqual(runner.calls, [][]string{{"image", "inspect", ref}}) {
				t.Fatalf("unexpected mutation/read: %v", runner.calls)
			}
		})
	}
}
