package fabric

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/api/types/volume"
	mobyclient "github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// stubDockerClient provides fail-closed defaults for every localDockerClient
// method so unit test fakes embed it and override only the calls they expect.
type stubDockerClient struct{}

func (stubDockerClient) Info(context.Context) (system.Info, error) {
	return system.Info{}, fmt.Errorf("unexpected docker API call: Info")
}

func (stubDockerClient) NetworkList(context.Context, mobyclient.NetworkListOptions) ([]network.Summary, error) {
	return nil, fmt.Errorf("unexpected docker API call: NetworkList")
}

func (stubDockerClient) NetworkInspect(context.Context, string, mobyclient.NetworkInspectOptions) (network.Inspect, error) {
	return network.Inspect{}, fmt.Errorf("unexpected docker API call: NetworkInspect")
}

func (stubDockerClient) NetworkCreate(context.Context, string, mobyclient.NetworkCreateOptions) (mobyclient.NetworkCreateResult, error) {
	return mobyclient.NetworkCreateResult{}, fmt.Errorf("unexpected docker API call: NetworkCreate")
}

func (stubDockerClient) NetworkConnect(context.Context, string, string, *network.EndpointSettings) error {
	return fmt.Errorf("unexpected docker API call: NetworkConnect")
}

func (stubDockerClient) NetworkDisconnect(context.Context, string, string, bool) error {
	return fmt.Errorf("unexpected docker API call: NetworkDisconnect")
}

func (stubDockerClient) NetworkRemove(context.Context, string) error {
	return fmt.Errorf("unexpected docker API call: NetworkRemove")
}

func (stubDockerClient) ContainerList(context.Context, mobyclient.ContainerListOptions) ([]container.Summary, error) {
	return nil, fmt.Errorf("unexpected docker API call: ContainerList")
}

func (stubDockerClient) ContainerInspect(context.Context, string) (container.InspectResponse, error) {
	return container.InspectResponse{}, fmt.Errorf("unexpected docker API call: ContainerInspect")
}

func (stubDockerClient) ContainerCreate(context.Context, *container.Config, *container.HostConfig, *network.NetworkingConfig, *ocispec.Platform, string) (mobyclient.ContainerCreateResult, error) {
	return mobyclient.ContainerCreateResult{}, fmt.Errorf("unexpected docker API call: ContainerCreate")
}

func (stubDockerClient) ContainerStart(context.Context, string, mobyclient.ContainerStartOptions) error {
	return fmt.Errorf("unexpected docker API call: ContainerStart")
}

func (stubDockerClient) ContainerStop(context.Context, string, mobyclient.ContainerStopOptions) error {
	return fmt.Errorf("unexpected docker API call: ContainerStop")
}

func (stubDockerClient) ContainerRemove(context.Context, string, mobyclient.ContainerRemoveOptions) error {
	return fmt.Errorf("unexpected docker API call: ContainerRemove")
}

func (stubDockerClient) ContainerWait(context.Context, string, container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	errCh := make(chan error, 1)
	errCh <- fmt.Errorf("unexpected docker API call: ContainerWait")
	return nil, errCh
}

func (stubDockerClient) ContainerLogs(context.Context, string, mobyclient.ContainerLogsOptions) (io.ReadCloser, error) {
	return nil, fmt.Errorf("unexpected docker API call: ContainerLogs")
}

func (stubDockerClient) CopyFromContainer(context.Context, string, string) (io.ReadCloser, container.PathStat, error) {
	return nil, container.PathStat{}, fmt.Errorf("unexpected docker API call: CopyFromContainer")
}

func (stubDockerClient) ContainerExec(context.Context, string, []string, []byte) ([]byte, error) {
	return nil, fmt.Errorf("unexpected docker API call: ContainerExec")
}

func (stubDockerClient) PullImage(context.Context, string, ocispec.Platform) error {
	return fmt.Errorf("unexpected docker API call: PullImage")
}

func (stubDockerClient) ImageInspect(context.Context, string, *ocispec.Platform) (image.InspectResponse, error) {
	return image.InspectResponse{}, fmt.Errorf("unexpected docker API call: ImageInspect")
}

func (stubDockerClient) ImageRemove(context.Context, string, mobyclient.ImageRemoveOptions) ([]image.DeleteResponse, error) {
	return nil, fmt.Errorf("unexpected docker API call: ImageRemove")
}

func (stubDockerClient) VolumeInspect(context.Context, string) (volume.Volume, error) {
	return volume.Volume{}, fmt.Errorf("unexpected docker API call: VolumeInspect")
}

func dockerFilterValues(f mobyclient.Filters, key string) []string {
	values := f[key]
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for k, ok := range values {
		if ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func encodeDockerStdoutStream(stdout []byte) io.ReadCloser {
	var buf bytes.Buffer
	var header [8]byte
	header[0] = byte(stdcopy.Stdout)
	binary.BigEndian.PutUint32(header[4:], uint32(len(stdout)))
	buf.Write(header[:])
	buf.Write(stdout)
	return io.NopCloser(&buf)
}

func sdkNetworkSummary(id, name string) network.Summary {
	return network.Summary{
		Network: network.Network{
			ID:   id,
			Name: name,
		},
	}
}

func sdkNetworkInspect(n dockerNetworkInspect) network.Inspect {
	labels := make(map[string]string, len(n.Labels))
	for k, v := range n.Labels {
		labels[k] = v
	}
	return network.Inspect{
		Network: network.Network{
			ID:     n.ID,
			Name:   n.Name,
			Labels: labels,
		},
	}
}

func sdkContainerInspect(c dockerContainerInspect) container.InspectResponse {
	name := c.Name
	if name != "" && !strings.HasPrefix(name, "/") {
		name = "/" + name
	}
	state := &container.State{
		Status:    container.ContainerState(c.State.Status),
		Running:   c.State.Running,
		StartedAt: c.State.StartedAt,
	}
	if c.State.Health != nil {
		state.Health = &container.Health{Status: container.HealthStatus(c.State.Health.Status)}
	}
	hostCfg := &container.HostConfig{
		Init:        c.HostConfig.Init,
		SecurityOpt: append([]string(nil), c.HostConfig.SecurityOpt...),
		Tmpfs:       c.HostConfig.Tmpfs,
		Resources: container.Resources{
			NanoCPUs:   c.HostConfig.NanoCPUs,
			Memory:     c.HostConfig.Memory,
			MemorySwap: c.HostConfig.MemorySwap,
		},
	}
	for _, m := range c.HostConfig.Mounts {
		sm := mount.Mount{
			Type:     mount.Type(m.Type),
			Source:   m.Source,
			Target:   m.Target,
			ReadOnly: m.ReadOnly,
		}
		if m.BindOptions.Propagation != "" {
			sm.BindOptions = &mount.BindOptions{Propagation: mount.Propagation(m.BindOptions.Propagation)}
		}
		if m.TmpfsOptions.Mode != 0 {
			sm.TmpfsOptions = &mount.TmpfsOptions{Mode: osFileMode(m.TmpfsOptions.Mode)}
		}
		hostCfg.Mounts = append(hostCfg.Mounts, sm)
	}
	cfg := &container.Config{
		User:   c.Config.User,
		Image:  c.Config.Image,
		Env:    append([]string(nil), c.Config.Env...),
		Labels: c.Config.Labels,
	}
	if c.Config.Healthcheck != nil {
		cfg.Healthcheck = &container.HealthConfig{
			Test:        append([]string(nil), c.Config.Healthcheck.Test...),
			Interval:    time.Duration(c.Config.Healthcheck.Interval),
			Timeout:     time.Duration(c.Config.Healthcheck.Timeout),
			Retries:     c.Config.Healthcheck.Retries,
			StartPeriod: time.Duration(c.Config.Healthcheck.StartPeriod),
		}
	}
	netSettings := &container.NetworkSettings{}
	if len(c.NetworkSettings.Ports) > 0 {
		netSettings.Ports = network.PortMap{}
		for portStr, bindings := range c.NetworkSettings.Ports {
			p, err := network.ParsePort(portStr)
			if err != nil {
				continue
			}
			out := make([]network.PortBinding, 0, len(bindings))
			for _, b := range bindings {
				out = append(out, network.PortBinding{
					HostIP:   parseDockerHostIP(b.HostIP),
					HostPort: b.HostPort,
				})
			}
			netSettings.Ports[p] = out
		}
	}
	if len(c.NetworkSettings.Networks) > 0 {
		netSettings.Networks = map[string]*network.EndpointSettings{}
		for nName, ep := range c.NetworkSettings.Networks {
			netSettings.Networks[nName] = &network.EndpointSettings{
				NetworkID: ep.NetworkID,
				IPAddress: parseDockerHostIP(ep.IPAddress),
			}
		}
	}
	mounts := make([]container.MountPoint, 0, len(c.Mounts))
	for _, m := range c.Mounts {
		mounts = append(mounts, container.MountPoint{
			Type:        mount.Type(m.Type),
			Name:        m.Name,
			Source:      m.Source,
			Destination: m.Destination,
			RW:          m.RW,
			Propagation: mount.Propagation(m.Propagation),
		})
	}
	return container.InspectResponse{
		ID:              c.ID,
		Name:            name,
		Path:            c.Path,
		Args:            append([]string(nil), c.Args...),
		Image:           c.Image,
		State:           state,
		HostConfig:      hostCfg,
		Config:          cfg,
		NetworkSettings: netSettings,
		Mounts:          mounts,
	}
}

func osFileMode(mode uint32) os.FileMode {
	return os.FileMode(mode)
}
