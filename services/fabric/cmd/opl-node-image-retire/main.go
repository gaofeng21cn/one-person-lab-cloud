// opl-node-image-retire removes one unused image through CRI, never host files,
// containers, snapshots or volumes. Instance supplies current protected refs.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
)

const socketPath = "/run/opl-cri/containerd.sock"
const (
	statusFailed    = "failed"
	statusEligible  = "eligible"
	statusAbsent    = "already_absent"
	statusRemoved   = "removed"
	statusProtected = "protected"
)

var imagePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.:/_-]*@sha256:[a-f0-9]{64}$`)

type request struct {
	Image           string   `json:"image"`
	ProtectedImages []string `json:"protectedImages"`
	Apply           bool     `json:"apply"`
}
type result struct {
	Status                 string  `json:"status"`
	ErrorCode              string  `json:"errorCode"`
	ImageDigest            string  `json:"imageDigest"`
	RemovalAttempted       bool    `json:"removalAttempted"`
	ImageAbsent            bool    `json:"imageAbsent"`
	ImageFSUsedBytesBefore *uint64 `json:"imageFsUsedBytesBefore"`
	ImageFSUsedBytesAfter  *uint64 `json:"imageFsUsedBytesAfter"`
}

func decodeRequest(r io.Reader) (request, error) {
	var value request
	dec := json.NewDecoder(io.LimitReader(r, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&value); err != nil {
		return value, errors.New("node_image_retirement_input_invalid")
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return value, errors.New("node_image_retirement_input_invalid")
	}
	if !imagePattern.MatchString(value.Image) || len(value.ProtectedImages) == 0 {
		return value, errors.New("node_image_retirement_input_invalid")
	}
	for _, image := range value.ProtectedImages {
		if image == "" || image != strings.TrimSpace(image) {
			return value, errors.New("node_image_retirement_input_invalid")
		}
	}
	return value, nil
}

func imageMatches(image *cri.Image, ref string) bool {
	return image != nil && (image.Id == ref || strings.TrimPrefix(image.Id, "sha256:") == ref || slices.Contains(image.RepoDigests, ref) || slices.Contains(image.RepoTags, ref))
}

// List all containers, including exited containers, because a stopped Workspace
// must retain its image. Resolve image IDs and every alias before any removal.
func eligible(ctx context.Context, images cri.ImageServiceClient, runtime cri.RuntimeServiceClient, input request) (*cri.Image, bool, error) {
	inventory, err := images.ListImages(ctx, &cri.ListImagesRequest{})
	if err != nil || inventory == nil {
		return nil, false, errors.New("node_image_inventory_unavailable")
	}
	var target *cri.Image
	for _, image := range inventory.Images {
		if image == nil || image.Id == "" {
			return nil, false, errors.New("node_image_inventory_invalid")
		}
		if slices.Contains(image.RepoDigests, input.Image) {
			if target != nil {
				return nil, false, errors.New("node_image_inventory_conflict")
			}
			target = image
		}
	}
	if target == nil {
		return nil, false, nil
	}
	if target.Pinned {
		return target, true, nil
	}
	for _, ref := range input.ProtectedImages {
		if imageMatches(target, ref) {
			return target, true, nil
		}
	}
	containers, err := runtime.ListContainers(ctx, &cri.ListContainersRequest{})
	if err != nil || containers == nil {
		return nil, false, errors.New("node_container_inventory_unavailable")
	}
	for _, container := range containers.Containers {
		if container == nil || container.Image == nil || container.ImageRef == "" {
			return nil, false, errors.New("node_container_inventory_invalid")
		}
		if imageMatches(target, container.ImageRef) || imageMatches(target, container.Image.Image) {
			return target, true, nil
		}
	}
	return target, false, nil
}

func imageFSUsage(ctx context.Context, images cri.ImageServiceClient) (*uint64, error) {
	info, err := images.ImageFsInfo(ctx, &cri.ImageFsInfoRequest{})
	if err != nil || info == nil || len(info.ImageFilesystems) == 0 {
		return nil, errors.New("node_image_filesystem_readback_unavailable")
	}
	var total uint64
	seen := map[string]bool{}
	for _, fs := range info.ImageFilesystems {
		if fs == nil || fs.FsId == nil || fs.FsId.Mountpoint == "" || fs.UsedBytes == nil || seen[fs.FsId.Mountpoint] {
			return nil, errors.New("node_image_filesystem_readback_invalid")
		}
		seen[fs.FsId.Mountpoint] = true
		if ^uint64(0)-total < fs.UsedBytes.Value {
			return nil, errors.New("node_image_filesystem_readback_invalid")
		}
		total += fs.UsedBytes.Value
	}
	return &total, nil
}

func retire(ctx context.Context, input request, images cri.ImageServiceClient, runtime cri.RuntimeServiceClient) result {
	outcome := result{Status: statusFailed, ErrorCode: "none", ImageDigest: strings.Split(input.Image, "@")[1]}
	fail := func(err error) result { outcome.ErrorCode = err.Error(); return outcome }
	before, err := imageFSUsage(ctx, images)
	if err != nil {
		return fail(err)
	}
	outcome.ImageFSUsedBytesBefore = before
	target, protected, err := eligible(ctx, images, runtime, input)
	if err != nil {
		return fail(err)
	}
	if target == nil {
		outcome.Status, outcome.ImageAbsent = statusAbsent, true
	} else if protected {
		outcome.Status = statusProtected
	} else if !input.Apply {
		outcome.Status = statusEligible
	} else {
		// Fresh eligibility immediately before RemoveImage. RemoveImage is an exact
		// image reference operation, not prune; it cannot stop a running container.
		target, protected, err = eligible(ctx, images, runtime, input)
		if err != nil {
			return fail(err)
		}
		if protected {
			outcome.Status = statusProtected
		} else if target == nil {
			outcome.Status, outcome.ImageAbsent = statusAbsent, true
		} else {
			outcome.RemovalAttempted = true
			_, removeErr := images.RemoveImage(ctx, &cri.RemoveImageRequest{Image: &cri.ImageSpec{Image: input.Image}})
			// A lost RPC response is never permission to remove again. Read back only.
			readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			for {
				inventory, readErr := images.ListImages(readCtx, &cri.ListImagesRequest{})
				if readErr == nil && inventory != nil {
					exists := false
					for _, image := range inventory.Images {
						if image == nil || image.Id == "" {
							return fail(errors.New("node_image_inventory_invalid"))
						}
						if imageMatches(image, input.Image) {
							exists = true
						}
					}
					if !exists {
						outcome.Status, outcome.ImageAbsent = statusRemoved, true
						break
					}
				}
				if readCtx.Err() != nil {
					return fail(errors.New("node_image_removal_readback_unknown"))
				}
				if removeErr != nil && readErr == nil {
					return fail(errors.New("node_image_removal_unconfirmed"))
				}
				select {
				case <-readCtx.Done():
					return fail(errors.New("node_image_removal_readback_unknown"))
				case <-time.After(250 * time.Millisecond):
				}
			}
		}
	}
	after, err := imageFSUsage(ctx, images)
	if err != nil {
		outcome.Status = statusFailed
		return fail(err)
	}
	outcome.ImageFSUsedBytesAfter = after
	return outcome
}

func main() {
	input, err := decodeRequest(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	conn, err := grpc.NewClient("passthrough:///cri", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}))
	if err != nil {
		fmt.Fprintln(os.Stderr, "node_cri_unavailable")
		os.Exit(1)
	}
	defer conn.Close()
	outcome := retire(ctx, input, cri.NewImageServiceClient(conn), cri.NewRuntimeServiceClient(conn))
	if err := json.NewEncoder(os.Stdout).Encode(outcome); err != nil {
		os.Exit(1)
	}
	if outcome.Status == statusFailed {
		os.Exit(1)
	}
}
