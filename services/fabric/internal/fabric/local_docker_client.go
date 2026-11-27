package fabric

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/netip"
	"strings"
	"sync"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/api/types/volume"
	mobyclient "github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// localDockerClient is the narrow typed interface around the official Moby Go
// SDK (github.com/moby/moby/client). Production uses mobyDockerClient backed by
// mobyclient.New(mobyclient.FromEnv, mobyclient.WithAPIVersionNegotiation());
// tests inject typed stubs without spawning docker CLI processes or parsing CLI
// text output.
type localDockerClient interface {
	Info(ctx context.Context) (system.Info, error)
	NetworkList(ctx context.Context, options mobyclient.NetworkListOptions) ([]network.Summary, error)
	NetworkInspect(ctx context.Context, networkID string, options mobyclient.NetworkInspectOptions) (network.Inspect, error)
	NetworkCreate(ctx context.Context, name string, options mobyclient.NetworkCreateOptions) (mobyclient.NetworkCreateResult, error)
	NetworkConnect(ctx context.Context, networkID, containerID string, config *network.EndpointSettings) error
	NetworkDisconnect(ctx context.Context, networkID, containerID string, force bool) error
	NetworkRemove(ctx context.Context, networkID string) error
	ContainerList(ctx context.Context, options mobyclient.ContainerListOptions) ([]container.Summary, error)
	ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error)
	ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (mobyclient.ContainerCreateResult, error)
	ContainerStart(ctx context.Context, containerID string, options mobyclient.ContainerStartOptions) error
	ContainerStop(ctx context.Context, containerID string, options mobyclient.ContainerStopOptions) error
	ContainerRemove(ctx context.Context, containerID string, options mobyclient.ContainerRemoveOptions) error
	ContainerWait(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error)
	ContainerLogs(ctx context.Context, containerID string, options mobyclient.ContainerLogsOptions) (io.ReadCloser, error)
	CopyFromContainer(ctx context.Context, containerID, srcPath string) (io.ReadCloser, container.PathStat, error)
	ContainerExec(ctx context.Context, containerID string, cmd []string, stdin []byte) ([]byte, error)
	PullImage(ctx context.Context, refStr string, platform ocispec.Platform) error
	ImageInspect(ctx context.Context, imageID string, platform *ocispec.Platform) (image.InspectResponse, error)
	ImageRemove(ctx context.Context, imageID string, options mobyclient.ImageRemoveOptions) ([]image.DeleteResponse, error)
	VolumeInspect(ctx context.Context, volumeID string) (volume.Volume, error)
}

type mobyDockerClient struct {
	once    sync.Once
	api     *mobyclient.Client
	initErr error
}

func newMobyDockerClient() *mobyDockerClient {
	return &mobyDockerClient{}
}

func newMobyDockerClientFromAPI(api *mobyclient.Client) *mobyDockerClient {
	c := &mobyDockerClient{api: api}
	c.once.Do(func() {
		if c.api == nil {
			c.initErr = fmt.Errorf("docker_client_unavailable")
		}
	})
	return c
}

func (c *mobyDockerClient) client() (*mobyclient.Client, error) {
	c.once.Do(func() {
		cli, err := mobyclient.New(mobyclient.FromEnv, mobyclient.WithAPIVersionNegotiation())
		if err != nil {
			c.initErr = fmt.Errorf("docker_client_init_failed: %w", err)
			return
		}
		c.api = cli
	})
	if c.initErr != nil {
		return nil, c.initErr
	}
	return c.api, nil
}

func (c *mobyDockerClient) Info(ctx context.Context) (system.Info, error) {
	api, err := c.client()
	if err != nil {
		return system.Info{}, err
	}
	res, err := api.Info(ctx, mobyclient.InfoOptions{})
	if err != nil {
		return system.Info{}, err
	}
	return res.Info, nil
}

func (c *mobyDockerClient) NetworkList(ctx context.Context, options mobyclient.NetworkListOptions) ([]network.Summary, error) {
	api, err := c.client()
	if err != nil {
		return nil, err
	}
	res, err := api.NetworkList(ctx, options)
	if err != nil {
		return nil, err
	}
	return res.Items, nil
}

func (c *mobyDockerClient) NetworkInspect(ctx context.Context, networkID string, options mobyclient.NetworkInspectOptions) (network.Inspect, error) {
	api, err := c.client()
	if err != nil {
		return network.Inspect{}, err
	}
	res, err := api.NetworkInspect(ctx, networkID, options)
	if err != nil {
		return network.Inspect{}, err
	}
	return res.Network, nil
}

func (c *mobyDockerClient) NetworkCreate(ctx context.Context, name string, options mobyclient.NetworkCreateOptions) (mobyclient.NetworkCreateResult, error) {
	api, err := c.client()
	if err != nil {
		return mobyclient.NetworkCreateResult{}, err
	}
	return api.NetworkCreate(ctx, name, options)
}

func (c *mobyDockerClient) NetworkConnect(ctx context.Context, networkID, containerID string, config *network.EndpointSettings) error {
	api, err := c.client()
	if err != nil {
		return err
	}
	_, err = api.NetworkConnect(ctx, networkID, mobyclient.NetworkConnectOptions{
		Container:      containerID,
		EndpointConfig: config,
	})
	return err
}

func (c *mobyDockerClient) NetworkDisconnect(ctx context.Context, networkID, containerID string, force bool) error {
	api, err := c.client()
	if err != nil {
		return err
	}
	_, err = api.NetworkDisconnect(ctx, networkID, mobyclient.NetworkDisconnectOptions{
		Container: containerID,
		Force:     force,
	})
	return err
}

func (c *mobyDockerClient) NetworkRemove(ctx context.Context, networkID string) error {
	api, err := c.client()
	if err != nil {
		return err
	}
	_, err = api.NetworkRemove(ctx, networkID, mobyclient.NetworkRemoveOptions{})
	return err
}

func (c *mobyDockerClient) ContainerList(ctx context.Context, options mobyclient.ContainerListOptions) ([]container.Summary, error) {
	api, err := c.client()
	if err != nil {
		return nil, err
	}
	res, err := api.ContainerList(ctx, options)
	if err != nil {
		return nil, err
	}
	return res.Items, nil
}

func (c *mobyDockerClient) ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error) {
	api, err := c.client()
	if err != nil {
		return container.InspectResponse{}, err
	}
	res, err := api.ContainerInspect(ctx, containerID, mobyclient.ContainerInspectOptions{})
	if err != nil {
		return container.InspectResponse{}, err
	}
	return res.Container, nil
}

func (c *mobyDockerClient) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (mobyclient.ContainerCreateResult, error) {
	api, err := c.client()
	if err != nil {
		return mobyclient.ContainerCreateResult{}, err
	}
	return api.ContainerCreate(ctx, mobyclient.ContainerCreateOptions{
		Config:           config,
		HostConfig:       hostConfig,
		NetworkingConfig: networkingConfig,
		Platform:         platform,
		Name:             containerName,
	})
}

func (c *mobyDockerClient) ContainerStart(ctx context.Context, containerID string, options mobyclient.ContainerStartOptions) error {
	api, err := c.client()
	if err != nil {
		return err
	}
	_, err = api.ContainerStart(ctx, containerID, options)
	return err
}

func (c *mobyDockerClient) ContainerStop(ctx context.Context, containerID string, options mobyclient.ContainerStopOptions) error {
	api, err := c.client()
	if err != nil {
		return err
	}
	_, err = api.ContainerStop(ctx, containerID, options)
	return err
}

func (c *mobyDockerClient) ContainerRemove(ctx context.Context, containerID string, options mobyclient.ContainerRemoveOptions) error {
	api, err := c.client()
	if err != nil {
		return err
	}
	_, err = api.ContainerRemove(ctx, containerID, options)
	return err
}

func (c *mobyDockerClient) ContainerWait(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	api, err := c.client()
	if err != nil {
		errCh := make(chan error, 1)
		errCh <- err
		return nil, errCh
	}
	res := api.ContainerWait(ctx, containerID, mobyclient.ContainerWaitOptions{Condition: condition})
	return res.Result, res.Error
}

func (c *mobyDockerClient) ContainerLogs(ctx context.Context, containerID string, options mobyclient.ContainerLogsOptions) (io.ReadCloser, error) {
	api, err := c.client()
	if err != nil {
		return nil, err
	}
	return api.ContainerLogs(ctx, containerID, options)
}

func (c *mobyDockerClient) CopyFromContainer(ctx context.Context, containerID, srcPath string) (io.ReadCloser, container.PathStat, error) {
	api, err := c.client()
	if err != nil {
		return nil, container.PathStat{}, err
	}
	res, err := api.CopyFromContainer(ctx, containerID, mobyclient.CopyFromContainerOptions{SourcePath: srcPath})
	if err != nil {
		return nil, container.PathStat{}, err
	}
	return res.Content, res.Stat, nil
}

func (c *mobyDockerClient) ContainerExec(ctx context.Context, containerID string, cmd []string, stdin []byte) ([]byte, error) {
	api, err := c.client()
	if err != nil {
		return nil, err
	}
	created, err := api.ExecCreate(ctx, containerID, mobyclient.ExecCreateOptions{
		AttachStdin:  len(stdin) > 0,
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmd,
	})
	if err != nil {
		return nil, err
	}
	attached, err := api.ExecAttach(ctx, created.ID, mobyclient.ExecAttachOptions{})
	if err != nil {
		return nil, err
	}
	defer attached.Close()
	if len(stdin) > 0 {
		go func() {
			_, _ = io.Copy(attached.Conn, bytes.NewReader(stdin))
			_ = attached.CloseWrite()
		}()
	}
	var stdout, stderr bytes.Buffer
	copyDone := make(chan error, 1)
	go func() {
		_, copyErr := stdcopy.StdCopy(&stdout, &stderr, attached.Reader)
		copyDone <- copyErr
	}()
	select {
	case <-ctx.Done():
		attached.Close()
		return nil, ctx.Err()
	case copyErr := <-copyDone:
		if copyErr != nil {
			return stdout.Bytes(), copyErr
		}
	}
	inspected, err := api.ExecInspect(ctx, created.ID, mobyclient.ExecInspectOptions{})
	if err != nil {
		return stdout.Bytes(), err
	}
	if inspected.ExitCode != 0 {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return stdout.Bytes(), fmt.Errorf("docker_exec_failed: exit status %d: %s", inspected.ExitCode, msg)
	}
	return stdout.Bytes(), nil
}

func (c *mobyDockerClient) PullImage(ctx context.Context, refStr string, platform ocispec.Platform) error {
	api, err := c.client()
	if err != nil {
		return err
	}
	pull, err := api.ImagePull(ctx, refStr, mobyclient.ImagePullOptions{Platforms: []ocispec.Platform{platform}})
	if err != nil {
		return err
	}
	return pull.Wait(ctx)
}

func (c *mobyDockerClient) ImageInspect(ctx context.Context, imageID string, platform *ocispec.Platform) (image.InspectResponse, error) {
	api, err := c.client()
	if err != nil {
		return image.InspectResponse{}, err
	}
	var opts []mobyclient.ImageInspectOption
	if platform != nil {
		opts = append(opts, mobyclient.ImageInspectWithPlatform(platform))
	}
	res, err := api.ImageInspect(ctx, imageID, opts...)
	if err != nil {
		return image.InspectResponse{}, err
	}
	return res.InspectResponse, nil
}

func (c *mobyDockerClient) ImageRemove(ctx context.Context, imageID string, options mobyclient.ImageRemoveOptions) ([]image.DeleteResponse, error) {
	api, err := c.client()
	if err != nil {
		return nil, err
	}
	res, err := api.ImageRemove(ctx, imageID, options)
	if err != nil {
		return nil, err
	}
	return res.Items, nil
}

func (c *mobyDockerClient) VolumeInspect(ctx context.Context, volumeID string) (volume.Volume, error) {
	api, err := c.client()
	if err != nil {
		return volume.Volume{}, err
	}
	res, err := api.VolumeInspect(ctx, volumeID, mobyclient.VolumeInspectOptions{})
	if err != nil {
		return volume.Volume{}, err
	}
	return res.Volume, nil
}

func parseOCIPlatform(raw string) (*ocispec.Platform, error) {
	parts := strings.Split(strings.TrimSpace(raw), "/")
	if len(parts) < 2 || len(parts) > 3 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid_oci_platform")
	}
	plat := &ocispec.Platform{OS: parts[0], Architecture: parts[1]}
	if len(parts) == 3 {
		if parts[2] == "" {
			return nil, fmt.Errorf("invalid_oci_platform")
		}
		plat.Variant = parts[2]
	}
	return plat, nil
}

func parseDockerHostIP(raw string) netip.Addr {
	addr, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil {
		return netip.Addr{}
	}
	return addr
}

func readDockerStdoutStream(r io.ReadCloser) ([]byte, error) {
	defer r.Close()
	var stdout bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, io.Discard, r); err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}

func dockerNetworkInspectFromSDK(resp network.Inspect) dockerNetworkInspect {
	labels := make(map[string]string, len(resp.Labels))
	for k, v := range resp.Labels {
		labels[k] = v
	}
	return dockerNetworkInspect{
		ID:     resp.ID,
		Name:   resp.Name,
		Labels: labels,
	}
}

func dockerContainerInspectFromSDK(resp container.InspectResponse) dockerContainerInspect {
	var out dockerContainerInspect
	out.ID = resp.ID
	out.Name = strings.TrimPrefix(resp.Name, "/")
	out.Path = resp.Path
	out.Args = append([]string(nil), resp.Args...)
	out.Image = resp.Image
	if resp.State != nil {
		out.State.Status = string(resp.State.Status)
		out.State.Running = resp.State.Running
		out.State.StartedAt = resp.State.StartedAt
		if resp.State.Health != nil {
			out.State.Health = &struct {
				Status string `json:"Status"`
			}{Status: string(resp.State.Health.Status)}
		}
	}
	if resp.HostConfig != nil {
		out.HostConfig.Init = resp.HostConfig.Init
		out.HostConfig.SecurityOpt = append([]string(nil), resp.HostConfig.SecurityOpt...)
		if len(resp.HostConfig.Tmpfs) > 0 {
			out.HostConfig.Tmpfs = make(map[string]string, len(resp.HostConfig.Tmpfs))
			for k, v := range resp.HostConfig.Tmpfs {
				out.HostConfig.Tmpfs[k] = v
			}
		}
		out.HostConfig.NanoCPUs = resp.HostConfig.NanoCPUs
		out.HostConfig.Memory = resp.HostConfig.Memory
		out.HostConfig.MemorySwap = resp.HostConfig.MemorySwap
		for _, m := range resp.HostConfig.Mounts {
			hm := dockerHostMount{
				Type:     string(m.Type),
				Source:   m.Source,
				Target:   m.Target,
				ReadOnly: m.ReadOnly,
			}
			if m.BindOptions != nil {
				hm.BindOptions.Propagation = string(m.BindOptions.Propagation)
			}
			if m.TmpfsOptions != nil {
				hm.TmpfsOptions.Mode = uint32(m.TmpfsOptions.Mode)
			}
			out.HostConfig.Mounts = append(out.HostConfig.Mounts, hm)
		}
	}
	if resp.Config != nil {
		out.Config.User = resp.Config.User
		out.Config.Image = resp.Config.Image
		out.Config.Env = append([]string(nil), resp.Config.Env...)
		if resp.Config.Labels != nil {
			out.Config.Labels = make(map[string]string, len(resp.Config.Labels))
			for k, v := range resp.Config.Labels {
				out.Config.Labels[k] = v
			}
		}
		if resp.Config.Healthcheck != nil {
			out.Config.Healthcheck = &struct {
				Test        []string `json:"Test"`
				Interval    int64    `json:"Interval"`
				Timeout     int64    `json:"Timeout"`
				Retries     int      `json:"Retries"`
				StartPeriod int64    `json:"StartPeriod"`
			}{
				Test:        append([]string(nil), resp.Config.Healthcheck.Test...),
				Interval:    int64(resp.Config.Healthcheck.Interval),
				Timeout:     int64(resp.Config.Healthcheck.Timeout),
				Retries:     resp.Config.Healthcheck.Retries,
				StartPeriod: int64(resp.Config.Healthcheck.StartPeriod),
			}
		}
	}
	if resp.NetworkSettings != nil {
		if len(resp.NetworkSettings.Ports) > 0 {
			out.NetworkSettings.Ports = make(map[string][]struct {
				HostIP   string `json:"HostIp"`
				HostPort string `json:"HostPort"`
			}, len(resp.NetworkSettings.Ports))
			for port, bindings := range resp.NetworkSettings.Ports {
				if bindings == nil {
					continue
				}
				items := make([]struct {
					HostIP   string `json:"HostIp"`
					HostPort string `json:"HostPort"`
				}, 0, len(bindings))
				for _, b := range bindings {
					hostIP := ""
					if b.HostIP.IsValid() {
						hostIP = b.HostIP.String()
					}
					items = append(items, struct {
						HostIP   string `json:"HostIp"`
						HostPort string `json:"HostPort"`
					}{HostIP: hostIP, HostPort: b.HostPort})
				}
				out.NetworkSettings.Ports[port.String()] = items
			}
		}
		if len(resp.NetworkSettings.Networks) > 0 {
			out.NetworkSettings.Networks = make(map[string]dockerEndpointSettings, len(resp.NetworkSettings.Networks))
			for name, ep := range resp.NetworkSettings.Networks {
				if ep == nil {
					continue
				}
				ipAddr := ""
				if ep.IPAddress.IsValid() {
					ipAddr = ep.IPAddress.String()
				}
				out.NetworkSettings.Networks[name] = dockerEndpointSettings{
					NetworkID: ep.NetworkID,
					IPAddress: ipAddr,
				}
			}
		}
	}
	for _, m := range resp.Mounts {
		out.Mounts = append(out.Mounts, dockerRuntimeMount{
			Type:        string(m.Type),
			Name:        m.Name,
			Source:      m.Source,
			Destination: m.Destination,
			RW:          m.RW,
			Propagation: string(m.Propagation),
		})
	}
	return out
}

func containerSummaryName(summary container.Summary) (string, error) {
	if len(summary.Names) != 1 {
		return "", fmt.Errorf("local_docker_inventory_identity_conflict")
	}
	name := strings.TrimPrefix(summary.Names[0], "/")
	if name == "" {
		return "", fmt.Errorf("local_docker_inventory_identity_conflict")
	}
	return name, nil
}
