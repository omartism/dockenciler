package docker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/api/types/strslice"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/opencontainers/image-spec/specs-go/v1"
)

type Container struct {
	ID     string
	Image  string
	Labels map[string]string
	State  string
}

type ContainerSpec struct {
	Name                     string
	Hostname                 string
	Domainname               string
	ExposedPorts             []string
	Volumes                  []string
	StopSignal               string
	StopTimeout              *int
	NetworkMode              string
	Links                    []string
	Labels                   map[string]string
	RestartPolicy            string
	RestartMaximumRetryCount int
	CapAdd                   []string
	CapDrop                  []string
	SecurityOpt              []string
	Tmpfs                    []string
	Devices                  []string
	PidMode                  string
	IpcMode                  string
	UTSMode                  string
	UsernsMode               string
	CgroupnsMode             string
	Privileged               bool
	PublishAllPorts          bool
	ReadonlyRootfs           bool
	Binds                    []string
	VolumesFrom              []string
	ExtraHosts               []string
	DNS                      []string
	DNSOptions               []string
	DNSSearch                []string
	GroupAdd                 []string
	CgroupParent             string
	OomScoreAdj              int
	ShmSize                  int64
	Runtime                  string
	Sysctls                  []string
	LogType                  string
	LogConfig                map[string]string
	StorageOpt               map[string]string
	MaskedPaths              []string
	ReadonlyPaths            []string
	Init                     *bool
	User                     string
	WorkingDir               string
	Env                      []string
	Mounts                   []string
	PortBindings             []string
	Healthcheck              string
	Entrypoint               []string
	Cmd                      []string
	Networks                 map[string]*network.EndpointSettings
}

type ServiceSpec struct {
	Name         string
	TaskTemplate struct {
		ContainerSpec struct {
			Image string
		}
	}
}

var ErrContainerManagedBySwarm = fmt.Errorf("container is managed by a swarm service")

type DockerClient interface {
	ListContainers(ctx context.Context, labelFilter string) ([]Container, error)
	InspectContainer(ctx context.Context, id string) (ContainerSpec, error)
	PullImage(ctx context.Context, imageRef string) error
	RecreateContainer(ctx context.Context, id string, spec ContainerSpec, newImage string) error
	UpdateService(ctx context.Context, serviceID string, spec ServiceSpec) error
	IsSwarmMode(ctx context.Context) (bool, error)
	Authenticate(ctx context.Context, username, password, registryHost string) error
	GetImageDigest(ctx context.Context, imageRef string) (string, error)
	GetServiceID(ctx context.Context, containerID string) (string, error)
}

// DockerAPIClient defines the Docker client interface for dependency injection
// This interface matches the Docker SDK v28.x API signatures
// Note: The actual docker.Client implements this interface
// The Platform parameter uses *v1.Platform from image-spec (not *swarm.Platform)

type DockerAPIClient interface {
	ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error)
	ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)
	ImagePull(ctx context.Context, ref string, options image.PullOptions) (io.ReadCloser, error)
	ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
	ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *v1.Platform, containerName string) (container.CreateResponse, error)
	ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
	ServiceInspectWithRaw(ctx context.Context, serviceID string, options types.ServiceInspectOptions) (swarm.Service, []byte, error)
	ServiceUpdate(ctx context.Context, serviceID string, version swarm.Version, spec swarm.ServiceSpec, options types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error)
	Info(ctx context.Context) (system.Info, error)
	RegistryLogin(ctx context.Context, authConfig registry.AuthConfig) (registry.AuthenticateOKBody, error)
	ImageInspectWithRaw(ctx context.Context, ref string) (types.ImageInspect, []byte, error)
}

type DockerClientImpl struct {
	client         DockerAPIClient
	mu             sync.RWMutex
	lastAuthConfig *registry.AuthConfig
}

func NewDockerClient() (*DockerClientImpl, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &DockerClientImpl{client: cli}, nil
}

func (d *DockerClientImpl) ListContainers(ctx context.Context, labelFilter string) ([]Container, error) {
	filterArgs := filters.NewArgs()
	if labelFilter != "" {
		filterArgs.Add("label", labelFilter)
	}

	containers, err := d.client.ContainerList(ctx, container.ListOptions{
		All:     false,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, err
	}

	result := make([]Container, len(containers))
	for i, c := range containers {
		result[i] = Container{
			ID:     c.ID,
			Image:  c.Image,
			Labels: c.Labels,
			State:  c.State,
		}
	}
	return result, nil
}

func (d *DockerClientImpl) InspectContainer(ctx context.Context, id string) (ContainerSpec, error) {
	inspected, err := d.client.ContainerInspect(ctx, id)
	if err != nil {
		return ContainerSpec{}, err
	}

	spec := ContainerSpec{}

	// Container name (trim leading /) - ContainerJSONBase may be nil in minimal mocks
	if inspected.ContainerJSONBase != nil {
		spec.Name = strings.TrimPrefix(inspected.ContainerJSONBase.Name, "/")
	}

	// Config fields
	if inspected.Config != nil {
		spec.Hostname = inspected.Config.Hostname
		spec.Domainname = inspected.Config.Domainname
		spec.User = inspected.Config.User
		spec.WorkingDir = inspected.Config.WorkingDir
		spec.Env = append([]string(nil), inspected.Config.Env...)
		spec.Entrypoint = []string(inspected.Config.Entrypoint)
		spec.Cmd = []string(inspected.Config.Cmd)
		spec.Labels = inspected.Config.Labels
		spec.Healthcheck = healthcheckToString(inspected.Config.Healthcheck)
		spec.ExposedPorts = exposedPortsToStrings(inspected.Config.ExposedPorts)
		spec.Volumes = volumesToStrings(inspected.Config.Volumes)
		spec.StopSignal = inspected.Config.StopSignal
		spec.StopTimeout = inspected.Config.StopTimeout
	}

	// HostConfig is inside ContainerJSONBase
	var hc *container.HostConfig
	if inspected.ContainerJSONBase != nil {
		hc = inspected.ContainerJSONBase.HostConfig
	} else {
		// Trypromotion safely (may still panic if Base nil, so avoid)
		hc = nil
	}
	// Also handle direct promotion for code paths where Base is mocked differently
	if hc == nil && inspected.ContainerJSONBase == nil {
		// Attempt to detect HostConfig via reflection-like fallback? Leave nil for minimal mocks
	}
	if hc != nil {
		spec.NetworkMode = string(hc.NetworkMode)
		spec.Links = append([]string(nil), hc.Links...)
		spec.RestartPolicy = string(hc.RestartPolicy.Name)
		spec.RestartMaximumRetryCount = hc.RestartPolicy.MaximumRetryCount
		spec.CapAdd = []string(hc.CapAdd)
		spec.CapDrop = []string(hc.CapDrop)
		spec.SecurityOpt = append([]string(nil), hc.SecurityOpt...)
		spec.Tmpfs = mapToStringSlice(hc.Tmpfs)
		spec.Devices = deviceMappingsToStrings(hc.Devices)
		spec.PidMode = string(hc.PidMode)
		spec.IpcMode = string(hc.IpcMode)
		spec.UTSMode = string(hc.UTSMode)
		spec.UsernsMode = string(hc.UsernsMode)
		spec.CgroupnsMode = string(hc.CgroupnsMode)
		spec.Privileged = hc.Privileged
		spec.PublishAllPorts = hc.PublishAllPorts
		spec.ReadonlyRootfs = hc.ReadonlyRootfs
		spec.Binds = append([]string(nil), hc.Binds...)
		spec.VolumesFrom = append([]string(nil), hc.VolumesFrom...)
		spec.ExtraHosts = append([]string(nil), hc.ExtraHosts...)
		spec.DNS = append([]string(nil), hc.DNS...)
		spec.DNSOptions = append([]string(nil), hc.DNSOptions...)
		spec.DNSSearch = append([]string(nil), hc.DNSSearch...)
		spec.GroupAdd = append([]string(nil), hc.GroupAdd...)
		spec.CgroupParent = hc.CgroupParent
		spec.OomScoreAdj = hc.OomScoreAdj
		spec.ShmSize = hc.ShmSize
		spec.Runtime = hc.Runtime
		spec.Sysctls = mapToStringSlice(hc.Sysctls)
		spec.LogType = hc.LogConfig.Type
		if hc.LogConfig.Config != nil {
			spec.LogConfig = make(map[string]string, len(hc.LogConfig.Config))
			for k, v := range hc.LogConfig.Config {
				spec.LogConfig[k] = v
			}
		}
		spec.StorageOpt = hc.StorageOpt
		spec.MaskedPaths = append([]string(nil), hc.MaskedPaths...)
		spec.ReadonlyPaths = append([]string(nil), hc.ReadonlyPaths...)
		spec.Init = hc.Init
		spec.Mounts = mountsToStrings(hc.Mounts)
		spec.PortBindings = portBindingsToStrings(hc.PortBindings)
	}
	// NetworkingConfig from NetworkSettings
	if inspected.NetworkSettings != nil && inspected.NetworkSettings.Networks != nil {
		spec.Networks = make(map[string]*network.EndpointSettings, len(inspected.NetworkSettings.Networks))
		for k, v := range inspected.NetworkSettings.Networks {
			// Preserve aliases, links and IPAM config; runtime fields like EndpointID are recreated
			copied := &network.EndpointSettings{}
			if v != nil {
				copied.Aliases = append([]string(nil), v.Aliases...)
				copied.Links = append([]string(nil), v.Links...)
				copied.NetworkID = v.NetworkID
				copied.EndpointID = ""
				copied.Gateway = ""
				copied.IPAddress = ""
				copied.IPPrefixLen = 0
				copied.IPv6Gateway = ""
				copied.GlobalIPv6Address = ""
				copied.MacAddress = v.MacAddress
				copied.DriverOpts = v.DriverOpts
				copied.IPAMConfig = v.IPAMConfig
			}
			spec.Networks[k] = copied
		}
	}

	return spec, nil
}

func (d *DockerClientImpl) PullImage(ctx context.Context, imageRef string) error {
	// Get the stored auth config
	d.mu.RLock()
	authConfig := d.lastAuthConfig
	d.mu.RUnlock()

	// Create pull options with encoded registry auth if available
	pullOptions := image.PullOptions{}
	if authConfig != nil {
		// Encode the auth config as a base64 JSON string
		authBytes, err := json.Marshal(authConfig)
		if err != nil {
			return fmt.Errorf("failed to marshal auth config: %w", err)
		}
		encodedAuth := base64.StdEncoding.EncodeToString(authBytes)
		pullOptions.RegistryAuth = encodedAuth
	}

	_, err := d.client.ImagePull(ctx, imageRef, pullOptions)
	return err
}

func (d *DockerClientImpl) RecreateContainer(ctx context.Context, id string, spec ContainerSpec, newImage string) error {
	// Check if the container is managed by a swarm service
	inspected, err := d.client.ContainerInspect(ctx, id)
	if err != nil {
		return err
	}
	if inspected.Config != nil {
		if serviceName, ok := inspected.Config.Labels["com.docker.swarm.service.name"]; ok {
			return fmt.Errorf("%w: %s", ErrContainerManagedBySwarm, serviceName)
		}
	}

	// Pull the new image if needed
	if err := d.PullImage(ctx, newImage); err != nil {
		return err
	}

	// Determine container name for recreation (preserve original name)
	containerName := spec.Name
	if containerName == "" {
		if inspected.ContainerJSONBase != nil {
			containerName = strings.TrimPrefix(inspected.ContainerJSONBase.Name, "/")
		}
	}
	// Docker will generate a name if empty; empty is fine but prefer preserving

	// Stop and remove the existing container
	if err := d.client.ContainerRemove(ctx, id, container.RemoveOptions{
		Force: true,
	}); err != nil {
		return err
	}

	// Build container Config - prefer inspected data (complete) with image override,
	// fallback to spec when inspected.Config is nil (e.g., in tests with minimal mocks)
	var containerConfig *container.Config
	if inspected.Config != nil {
		cfgCopy := *inspected.Config
		// Deep copy maps/slices that might be mutated elsewhere
		if inspected.Config.Labels != nil {
			cfgCopy.Labels = make(map[string]string, len(inspected.Config.Labels))
			for k, v := range inspected.Config.Labels {
				cfgCopy.Labels[k] = v
			}
		}
		if inspected.Config.Env != nil {
			cfgCopy.Env = append([]string(nil), inspected.Config.Env...)
		}
		if inspected.Config.Entrypoint != nil {
			cfgCopy.Entrypoint = append(strslice.StrSlice(nil), inspected.Config.Entrypoint...)
		}
		if inspected.Config.Cmd != nil {
			cfgCopy.Cmd = append(strslice.StrSlice(nil), inspected.Config.Cmd...)
		}
		if inspected.Config.ExposedPorts != nil {
			cfgCopy.ExposedPorts = make(nat.PortSet, len(inspected.Config.ExposedPorts))
			for k, v := range inspected.Config.ExposedPorts {
				cfgCopy.ExposedPorts[k] = v
			}
		}
		if inspected.Config.Volumes != nil {
			cfgCopy.Volumes = make(map[string]struct{}, len(inspected.Config.Volumes))
			for k, v := range inspected.Config.Volumes {
				cfgCopy.Volumes[k] = v
			}
		}
		cfgCopy.Image = newImage
		// Merge any spec fields that are present but missing in inspected (defensive for minimal mocks)
		if len(cfgCopy.Env) == 0 && len(spec.Env) > 0 {
			cfgCopy.Env = spec.Env
		}
		if len(cfgCopy.Labels) == 0 && len(spec.Labels) > 0 {
			cfgCopy.Labels = spec.Labels
		}
		if cfgCopy.WorkingDir == "" && spec.WorkingDir != "" {
			cfgCopy.WorkingDir = spec.WorkingDir
		}
		if cfgCopy.User == "" && spec.User != "" {
			cfgCopy.User = spec.User
		}
		if len(cfgCopy.Entrypoint) == 0 && len(spec.Entrypoint) > 0 {
			cfgCopy.Entrypoint = strslice.StrSlice(spec.Entrypoint)
		}
		if len(cfgCopy.Cmd) == 0 && len(spec.Cmd) > 0 {
			cfgCopy.Cmd = strslice.StrSlice(spec.Cmd)
		}
		if cfgCopy.Hostname == "" && spec.Hostname != "" {
			cfgCopy.Hostname = spec.Hostname
		}
		if cfgCopy.Domainname == "" && spec.Domainname != "" {
			cfgCopy.Domainname = spec.Domainname
		}
		if cfgCopy.Healthcheck == nil && spec.Healthcheck != "" {
			var hc *container.HealthConfig
			if err := json.Unmarshal([]byte(spec.Healthcheck), &hc); err == nil {
				cfgCopy.Healthcheck = hc
			}
		}
		if len(cfgCopy.ExposedPorts) == 0 && len(spec.ExposedPorts) > 0 {
			cfgCopy.ExposedPorts = parseExposedPorts(spec.ExposedPorts)
		}
		if len(cfgCopy.Volumes) == 0 && len(spec.Volumes) > 0 {
			cfgCopy.Volumes = parseVolumes(spec.Volumes)
		}
		if cfgCopy.StopSignal == "" && spec.StopSignal != "" {
			cfgCopy.StopSignal = spec.StopSignal
		}
		if cfgCopy.StopTimeout == nil && spec.StopTimeout != nil {
			cfgCopy.StopTimeout = spec.StopTimeout
		}
		containerConfig = &cfgCopy
	} else {
		containerConfig = &container.Config{
			Image:        newImage,
			Hostname:     spec.Hostname,
			Domainname:   spec.Domainname,
			User:         spec.User,
			WorkingDir:   spec.WorkingDir,
			Env:          spec.Env,
			Entrypoint:   strslice.StrSlice(spec.Entrypoint),
			Cmd:          strslice.StrSlice(spec.Cmd),
			Labels:       spec.Labels,
			StopSignal:   spec.StopSignal,
			StopTimeout:  spec.StopTimeout,
			Volumes:      parseVolumes(spec.Volumes),
			ExposedPorts: parseExposedPorts(spec.ExposedPorts),
		}
		if spec.Healthcheck != "" {
			var hc *container.HealthConfig
			if err := json.Unmarshal([]byte(spec.Healthcheck), &hc); err != nil {
				return fmt.Errorf("failed to parse healthcheck: %w", err)
			}
			containerConfig.Healthcheck = hc
		}
	}

	// Build HostConfig - prefer inspected HostConfig (from ContainerJSONBase)
	var inspectedHC *container.HostConfig
	if inspected.ContainerJSONBase != nil {
		inspectedHC = inspected.ContainerJSONBase.HostConfig
	}
	var hostConfig *container.HostConfig
	if inspectedHC != nil {
		hcCopy := *inspectedHC
		// Deep-copy slices/maps where needed for safety
		if inspectedHC.Binds != nil {
			hcCopy.Binds = append([]string(nil), inspectedHC.Binds...)
		}
		if inspectedHC.Links != nil {
			hcCopy.Links = append([]string(nil), inspectedHC.Links...)
		}
		if inspectedHC.CapAdd != nil {
			hcCopy.CapAdd = append(strslice.StrSlice(nil), inspectedHC.CapAdd...)
		}
		if inspectedHC.CapDrop != nil {
			hcCopy.CapDrop = append(strslice.StrSlice(nil), inspectedHC.CapDrop...)
		}
		if inspectedHC.GroupAdd != nil {
			hcCopy.GroupAdd = append([]string(nil), inspectedHC.GroupAdd...)
		}
		if inspectedHC.ExtraHosts != nil {
			hcCopy.ExtraHosts = append([]string(nil), inspectedHC.ExtraHosts...)
		}
		if inspectedHC.DNS != nil {
			hcCopy.DNS = append([]string(nil), inspectedHC.DNS...)
		}
		if inspectedHC.DNSOptions != nil {
			hcCopy.DNSOptions = append([]string(nil), inspectedHC.DNSOptions...)
		}
		if inspectedHC.DNSSearch != nil {
			hcCopy.DNSSearch = append([]string(nil), inspectedHC.DNSSearch...)
		}
		if inspectedHC.VolumesFrom != nil {
			hcCopy.VolumesFrom = append([]string(nil), inspectedHC.VolumesFrom...)
		}
		if inspectedHC.SecurityOpt != nil {
			hcCopy.SecurityOpt = append([]string(nil), inspectedHC.SecurityOpt...)
		}
		if inspectedHC.MaskedPaths != nil {
			hcCopy.MaskedPaths = append([]string(nil), inspectedHC.MaskedPaths...)
		}
		if inspectedHC.ReadonlyPaths != nil {
			hcCopy.ReadonlyPaths = append([]string(nil), inspectedHC.ReadonlyPaths...)
		}
		// Merge spec fallback where inspected is zero-value but spec has data (minimal mock handling)
		if len(hcCopy.Binds) == 0 && len(spec.Binds) > 0 {
			hcCopy.Binds = spec.Binds
		}
		if len(hcCopy.Links) == 0 && len(spec.Links) > 0 {
			hcCopy.Links = spec.Links
		}
		if len(hcCopy.CapAdd) == 0 && len(spec.CapAdd) > 0 {
			hcCopy.CapAdd = strslice.StrSlice(spec.CapAdd)
		}
		if len(hcCopy.CapDrop) == 0 && len(spec.CapDrop) > 0 {
			hcCopy.CapDrop = strslice.StrSlice(spec.CapDrop)
		}
		if string(hcCopy.RestartPolicy.Name) == "" && spec.RestartPolicy != "" {
			hcCopy.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyMode(spec.RestartPolicy), MaximumRetryCount: spec.RestartMaximumRetryCount}
		}
		if len(hcCopy.Mounts) == 0 && len(spec.Mounts) > 0 {
			hcCopy.Mounts = parseMounts(spec.Mounts)
		}
		if len(hcCopy.PortBindings) == 0 && len(spec.PortBindings) > 0 {
			hcCopy.PortBindings = parsePortBindings(spec.PortBindings)
		}
		if len(hcCopy.Tmpfs) == 0 && len(spec.Tmpfs) > 0 {
			hcCopy.Tmpfs = stringSliceToMap(spec.Tmpfs)
		}
		if len(hcCopy.Devices) == 0 && len(spec.Devices) > 0 {
			hcCopy.Devices = parseDeviceMappings(spec.Devices)
		}
		// Sysctls etc.
		if hcCopy.ExtraHosts == nil && len(spec.ExtraHosts) > 0 {
			hcCopy.ExtraHosts = spec.ExtraHosts
		}
		hostConfig = &hcCopy
	} else {
		hostConfig = &container.HostConfig{
			Binds:           spec.Binds,
			Links:           spec.Links,
			SecurityOpt:     spec.SecurityOpt,
			Tmpfs:           stringSliceToMap(spec.Tmpfs),
			PidMode:         container.PidMode(spec.PidMode),
			IpcMode:         container.IpcMode(spec.IpcMode),
			UTSMode:         container.UTSMode(spec.UTSMode),
			UsernsMode:      container.UsernsMode(spec.UsernsMode),
			CgroupnsMode:    container.CgroupnsMode(spec.CgroupnsMode),
			Privileged:      spec.Privileged,
			PublishAllPorts: spec.PublishAllPorts,
			ReadonlyRootfs:  spec.ReadonlyRootfs,
			VolumesFrom:     spec.VolumesFrom,
			ExtraHosts:      spec.ExtraHosts,
			DNS:             spec.DNS,
			DNSOptions:      spec.DNSOptions,
			DNSSearch:       spec.DNSSearch,
			GroupAdd:        spec.GroupAdd,
			OomScoreAdj:     spec.OomScoreAdj,
			ShmSize:         spec.ShmSize,
			Runtime:         spec.Runtime,
			Sysctls:         stringSliceToMap(spec.Sysctls),
			LogConfig:       container.LogConfig{Type: spec.LogType, Config: spec.LogConfig},
			StorageOpt:      spec.StorageOpt,
			MaskedPaths:     spec.MaskedPaths,
			ReadonlyPaths:   spec.ReadonlyPaths,
			Init:            spec.Init,
			Mounts:          parseMounts(spec.Mounts),
			PortBindings:    parsePortBindings(spec.PortBindings),
		}
		hostConfig.CapAdd = strslice.StrSlice(spec.CapAdd)
		hostConfig.CapDrop = strslice.StrSlice(spec.CapDrop)
		hostConfig.Devices = parseDeviceMappings(spec.Devices)
		hostConfig.CgroupParent = spec.CgroupParent
		if spec.NetworkMode != "" {
			hostConfig.NetworkMode = container.NetworkMode(spec.NetworkMode)
		}
		if spec.RestartPolicy != "" {
			hostConfig.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyMode(spec.RestartPolicy), MaximumRetryCount: spec.RestartMaximumRetryCount}
		}
	}

	// Build NetworkingConfig from inspected NetworkSettings or spec.Networks fallback
	var networkingConfig *network.NetworkingConfig
	if inspected.NetworkSettings != nil && inspected.NetworkSettings.Networks != nil && len(inspected.NetworkSettings.Networks) > 0 {
		networkingConfig = &network.NetworkingConfig{
			EndpointsConfig: make(map[string]*network.EndpointSettings, len(inspected.NetworkSettings.Networks)),
		}
		for name, ep := range inspected.NetworkSettings.Networks {
			if ep == nil {
				continue
			}
			// Preserve user-configurable fields; runtime allocations will be recreated
			networkingConfig.EndpointsConfig[name] = &network.EndpointSettings{
				Aliases:    append([]string(nil), ep.Aliases...),
				Links:      append([]string(nil), ep.Links...),
				NetworkID:  ep.NetworkID,
				MacAddress: ep.MacAddress,
				DriverOpts: ep.DriverOpts,
				IPAMConfig: ep.IPAMConfig,
			}
		}
	} else if len(spec.Networks) > 0 {
		networkingConfig = &network.NetworkingConfig{EndpointsConfig: spec.Networks}
	}

	resp, err := d.client.ContainerCreate(ctx, containerConfig, hostConfig, networkingConfig, nil, containerName)
	if err != nil {
		return err
	}

	if err := d.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return err
	}

	return nil
}

func (d *DockerClientImpl) UpdateService(ctx context.Context, serviceID string, spec ServiceSpec) error {
	// First, inspect the service to get its current version and annotations
	service, _, err := d.client.ServiceInspectWithRaw(ctx, serviceID, types.ServiceInspectOptions{})
	if err != nil {
		return err
	}

	// Convert our ServiceSpec to swarm.ServiceSpec, preserving existing annotations
	// to avoid "renaming services is not supported" error from Docker daemon
	swarmSpec := toSwarmServiceSpec(spec)
	swarmSpec.Annotations = service.Spec.Annotations

	// Perform a rolling update by updating the service
	_, err = d.client.ServiceUpdate(ctx, serviceID, service.Version, swarmSpec, types.ServiceUpdateOptions{})
	return err
}

func (d *DockerClientImpl) IsSwarmMode(ctx context.Context) (bool, error) {
	info, err := d.client.Info(ctx)
	if err != nil {
		return false, err
	}
	return info.Swarm.LocalNodeState == swarm.LocalNodeStateActive, nil
}

func (d *DockerClientImpl) Authenticate(ctx context.Context, username, password, registryHost string) error {
	// Strip https:// prefix from registry host if present (Docker SDK expects bare hostname)
	serverAddress := strings.TrimPrefix(registryHost, "https://")
	serverAddress = strings.TrimPrefix(serverAddress, "http://")

	authConfig := registry.AuthConfig{
		Username:      username,
		Password:      password,
		ServerAddress: serverAddress,
	}

	// Store the auth config for use in PullImage
	d.mu.Lock()
	d.lastAuthConfig = &authConfig
	d.mu.Unlock()

	// Skip RegistryLogin for anonymous access (no credentials).
	// This allows public Docker Hub images to be pulled without authentication.
	if username == "" && password == "" {
		return nil
	}

	_, err := d.client.RegistryLogin(ctx, authConfig)
	return err
}

func (d *DockerClientImpl) GetImageDigest(ctx context.Context, imageRef string) (string, error) {
	inspect, _, err := d.client.ImageInspectWithRaw(ctx, imageRef)
	if err != nil {
		return "", err
	}
	return inspect.ID, nil
}

func (d *DockerClientImpl) GetServiceID(ctx context.Context, containerID string) (string, error) {
	inspected, err := d.client.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", err
	}
	if serviceID, ok := inspected.Config.Labels["com.docker.swarm.service.id"]; ok {
		return serviceID, nil
	}
	return "", nil
}

// Helper functions

func toSwarmServiceSpec(spec ServiceSpec) swarm.ServiceSpec {
	return swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image: spec.TaskTemplate.ContainerSpec.Image,
			},
		},
	}
}

// Helper functions from the original file

func stringSliceToMap(s []string) map[string]string {
	if s == nil {
		return nil
	}
	result := make(map[string]string)
	for _, item := range s {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result
}

func mapToStringSlice(m map[string]string) []string {
	if m == nil {
		return nil
	}
	result := make([]string, 0, len(m))
	for k, v := range m {
		result = append(result, fmt.Sprintf("%s=%v", k, v))
	}
	return result
}

func deviceMappingsToStrings(devices []container.DeviceMapping) []string {
	if devices == nil {
		return nil
	}
	result := make([]string, 0, len(devices))
	for _, d := range devices {
		result = append(result, fmt.Sprintf("%s:%s:%s", d.PathOnHost, d.PathInContainer, d.CgroupPermissions))
	}
	return result
}

func mountsToStrings(mounts []mount.Mount) []string {
	if mounts == nil {
		return nil
	}
	result := make([]string, 0, len(mounts))
	for _, m := range mounts {
		b, err := json.Marshal(m)
		if err != nil {
			slog.Warn("failed to marshal mount", "error", err)
			continue
		}
		result = append(result, string(b))
	}
	return result
}

func portBindingsToStrings(bindings map[nat.Port][]nat.PortBinding) []string {
	if bindings == nil {
		return nil
	}
	result := make([]string, 0, len(bindings))
	for port, binds := range bindings {
		for _, b := range binds {
			result = append(result, fmt.Sprintf("%s:%s", string(port), b.HostIP+":"+b.HostPort))
		}
	}
	return result
}

func healthcheckToString(hc *container.HealthConfig) string {
	if hc == nil {
		return ""
	}
	b, _ := json.Marshal(hc)
	return string(b)
}

func parseStringSlice(s []string) []string {
	if s == nil {
		return nil
	}
	result := make([]string, len(s))
	copy(result, s)
	return result
}

func parseDeviceMappings(s []string) []container.DeviceMapping {
	if s == nil {
		return nil
	}
	result := make([]container.DeviceMapping, 0, len(s))
	for _, d := range s {
		parts := strings.SplitN(d, ":", 3)
		if len(parts) != 3 {
			continue
		}
		result = append(result, container.DeviceMapping{
			PathOnHost:        parts[0],
			PathInContainer:   parts[1],
			CgroupPermissions: parts[2],
		})
	}
	return result
}

func parseMounts(s []string) []mount.Mount {
	if s == nil {
		return nil
	}
	result := make([]mount.Mount, 0, len(s))
	for _, ms := range s {
		var m mount.Mount
		if err := json.Unmarshal([]byte(ms), &m); err != nil {
			slog.Warn("failed to unmarshal mount", "error", err)
			continue
		}
		result = append(result, m)
	}
	return result
}

func parsePortBindings(s []string) nat.PortMap {
	if s == nil {
		return nil
	}
	result := make(nat.PortMap)
	for _, b := range s {
		parts := strings.Split(b, ":")
		var pb nat.PortBinding
		switch len(parts) {
		case 2:
			// format: "port/proto:hostPort" or "port:hostPort"
			pb = nat.PortBinding{HostPort: parts[1]}
		case 3:
			// format: "port/proto:hostIP:hostPort" or "port:hostIP:hostPort"
			pb = nat.PortBinding{HostIP: parts[1], HostPort: parts[2]}
		default:
			continue
		}
		port := nat.Port(parts[0])
		result[port] = []nat.PortBinding{pb}
	}
	return result
}
func exposedPortsToStrings(ports nat.PortSet) []string {
	if ports == nil {
		return nil
	}
	result := make([]string, 0, len(ports))
	for p := range ports {
		result = append(result, string(p))
	}
	return result
}

func parseExposedPorts(s []string) nat.PortSet {
	if s == nil {
		return nil
	}
	result := make(nat.PortSet, len(s))
	for _, p := range s {
		result[nat.Port(p)] = struct{}{}
	}
	return result
}

func volumesToStrings(volumes map[string]struct{}) []string {
	if volumes == nil {
		return nil
	}
	result := make([]string, 0, len(volumes))
	for v := range volumes {
		result = append(result, v)
	}
	return result
}

func parseVolumes(s []string) map[string]struct{} {
	if s == nil {
		return nil
	}
	result := make(map[string]struct{}, len(s))
	for _, v := range s {
		result[v] = struct{}{}
	}
	return result
}
