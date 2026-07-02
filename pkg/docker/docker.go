package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/api/types/strslice"
	"github.com/docker/docker/api/types/swarm"
)

type Container struct {
	ID     string
	Image  string
	Labels map[string]string
}

type ContainerSpec struct {
	NetworkMode  string
	Links        []string
	RestartPolicy string
	CapAdd       []string
	CapDrop      []string
	SecurityOpt  []string
	Tmpfs        []string
	Devices      []string
	PidMode      string
	IpcMode      string
	User         string
	WorkingDir   string
	Env          []string
	Mounts       []string
	PortBindings []string
	Healthcheck  string
	Entrypoint   []string
	Cmd          []string
}

type ServiceSpec struct {
	Name        string
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
	Authenticate(ctx context.Context, registry, token string) error
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
	client DockerAPIClient
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
		All:     true,
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

	if inspected.HostConfig != nil {
		spec.NetworkMode = string(inspected.HostConfig.NetworkMode)
		if inspected.HostConfig.RestartPolicy.Name != "" {
			spec.RestartPolicy = string(inspected.HostConfig.RestartPolicy.Name)
		}
		spec.CapAdd = inspected.HostConfig.CapAdd
		spec.CapDrop = inspected.HostConfig.CapDrop
		spec.SecurityOpt = inspected.HostConfig.SecurityOpt
		spec.Tmpfs = mapToStringSlice(inspected.HostConfig.Tmpfs)
		spec.Devices = deviceMappingsToStrings(inspected.HostConfig.Devices)
		spec.PidMode = string(inspected.HostConfig.PidMode)
		spec.IpcMode = string(inspected.HostConfig.IpcMode)
		spec.Mounts = mountsToStrings(inspected.HostConfig.Mounts)
		spec.PortBindings = portBindingsToStrings(inspected.HostConfig.PortBindings)
	}

	if inspected.Config != nil {
		spec.Healthcheck = healthcheckToString(inspected.Config.Healthcheck)
	}

	return spec, nil
}

func (d *DockerClientImpl) PullImage(ctx context.Context, imageRef string) error {
	_, err := d.client.ImagePull(ctx, imageRef, image.PullOptions{})
	return err
}

func (d *DockerClientImpl) RecreateContainer(ctx context.Context, id string, spec ContainerSpec, newImage string) error {
	// Check if the container is managed by a swarm service
	inspected, err := d.client.ContainerInspect(ctx, id)
	if err != nil {
		return err
	}
	if serviceName, ok := inspected.Config.Labels["com.docker.swarm.service.name"]; ok {
		return fmt.Errorf("%w: %s", ErrContainerManagedBySwarm, serviceName)
	}

	// Pull the new image if needed
	if err := d.PullImage(ctx, newImage); err != nil {
		return err
	}

	// Stop and remove the existing container
	if err := d.client.ContainerRemove(ctx, id, container.RemoveOptions{
		Force: true,
	}); err != nil {
		return err
	}

	// Create and start the new container
	containerConfig := &container.Config{
		Image:        newImage,
		Env:          spec.Env,
		WorkingDir:   spec.WorkingDir,
		User:         spec.User,
		Entrypoint:   strslice.StrSlice(spec.Entrypoint),
		Cmd:          strslice.StrSlice(spec.Cmd),
	}
	if spec.Healthcheck != "" {
		var hc *container.HealthConfig
		if err := json.Unmarshal([]byte(spec.Healthcheck), &hc); err != nil {
			return fmt.Errorf("failed to parse healthcheck: %w", err)
		}
		containerConfig.Healthcheck = hc
	}

	hostConfig := &container.HostConfig{}
	if spec.NetworkMode != "" {
		hostConfig.NetworkMode = container.NetworkMode(spec.NetworkMode)
	}
	if spec.RestartPolicy != "" {
		hostConfig.RestartPolicy = container.RestartPolicy{
			Name: container.RestartPolicyMode(spec.RestartPolicy),
		}
	}
	hostConfig.CapAdd = spec.CapAdd
	hostConfig.CapDrop = spec.CapDrop
	hostConfig.SecurityOpt = spec.SecurityOpt
	hostConfig.Tmpfs = stringSliceToMap(spec.Tmpfs)
	hostConfig.Devices = parseDeviceMappings(spec.Devices)
	hostConfig.PidMode = container.PidMode(spec.PidMode)
	hostConfig.IpcMode = container.IpcMode(spec.IpcMode)
	hostConfig.Mounts = parseMounts(spec.Mounts)
	hostConfig.PortBindings = parsePortBindings(spec.PortBindings)

	resp, err := d.client.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, "")
	if err != nil {
		return err
	}

	if err := d.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return err
	}

	return nil
}

func (d *DockerClientImpl) UpdateService(ctx context.Context, serviceID string, spec ServiceSpec) error {
	// First, inspect the service to get its current version
	service, _, err := d.client.ServiceInspectWithRaw(ctx, serviceID, types.ServiceInspectOptions{})
	if err != nil {
		return err
	}

	// Convert our ServiceSpec to swarm.ServiceSpec
	swarmSpec := toSwarmServiceSpec(spec)

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

func (d *DockerClientImpl) Authenticate(ctx context.Context, registryURL, token string) error {
	// Strip https:// prefix from registry URL if present (Docker SDK expects bare hostname)
	serverAddress := strings.TrimPrefix(registryURL, "https://")
	serverAddress = strings.TrimPrefix(serverAddress, "http://")

	// ECR auth uses "AWS" as username and the authorization token as password
	// ServerAddress must be set so Docker daemon knows which registry to authenticate with
	authConfig := registry.AuthConfig{
		Username:      "AWS",
		Password:      token,
		ServerAddress: serverAddress,
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
		Mode: swarm.ServiceMode{
			Replicated: &swarm.ReplicatedService{
				Replicas: ptrToUint64(1), // Default to 1 replica
			},
		},
	}
}

func ptrToUint64(i uint64) *uint64 {
	return &i
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
		result = append(result, fmt.Sprintf("%s:%s:%s", m.Source, m.Target, string(m.Type)))
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
			result = append(result, fmt.Sprintf("%s:%s", port.Port(), b.HostIP+":"+b.HostPort))
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
	for _, m := range s {
		parts := strings.SplitN(m, ":", 3)
		if len(parts) != 3 {
			continue
		}
		result = append(result, mount.Mount{
			Source: parts[0],
			Target: parts[1],
			Type:   mount.Type(parts[2]),
		})
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
		if len(parts) >= 2 {
			port := nat.Port(parts[0])
			result[port] = []nat.PortBinding{{HostPort: parts[1]}}
		}
	}
	return result
}


