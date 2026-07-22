package docker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/api/types/strslice"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/go-connections/nat"
	"github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"gotest.tools/v3/assert"
)

type mockDockerClient struct {
	InfoFunc                func(context.Context) (system.Info, error)
	ServiceUpdateFunc       func(context.Context, string, swarm.Version, swarm.ServiceSpec, types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error)
	ContainerInspectFunc    func(context.Context, string) (types.ContainerJSON, error)
	ServiceInspectWithRawFunc func(context.Context, string, types.ServiceInspectOptions) (swarm.Service, []byte, error)
	ContainerListFunc      func(context.Context, container.ListOptions) ([]types.Container, error)
	ImagePullFunc          func(context.Context, string, image.PullOptions) (io.ReadCloser, error)
	ContainerRemoveFunc    func(context.Context, string, container.RemoveOptions) error
	ContainerCreateFunc    func(context.Context, *container.Config, *container.HostConfig, *network.NetworkingConfig, *v1.Platform, string) (container.CreateResponse, error)
	ContainerStartFunc     func(context.Context, string, container.StartOptions) error
	RegistryLoginFunc      func(context.Context, registry.AuthConfig) (registry.AuthenticateOKBody, error)
	ImageInspectWithRawFunc func(context.Context, string) (types.ImageInspect, []byte, error)
}

func (m *mockDockerClient) Info(ctx context.Context) (system.Info, error) {
	if m.InfoFunc != nil {
		return m.InfoFunc(ctx)
	}
	return system.Info{}, nil
}

func (m *mockDockerClient) ServiceUpdate(ctx context.Context, serviceID string, version swarm.Version, spec swarm.ServiceSpec, options types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error) {
	if m.ServiceUpdateFunc != nil {
		return m.ServiceUpdateFunc(ctx, serviceID, version, spec, options)
	}
	return swarm.ServiceUpdateResponse{}, nil
}

func (m *mockDockerClient) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	if m.ContainerInspectFunc != nil {
		return m.ContainerInspectFunc(ctx, containerID)
	}
	return types.ContainerJSON{}, nil
}

func (m *mockDockerClient) ServiceInspectWithRaw(ctx context.Context, serviceID string, options types.ServiceInspectOptions) (swarm.Service, []byte, error) {
	if m.ServiceInspectWithRawFunc != nil {
		return m.ServiceInspectWithRawFunc(ctx, serviceID, options)
	}
	return swarm.Service{}, nil, nil
}

func (m *mockDockerClient) ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error) {
	if m.ContainerListFunc != nil {
		return m.ContainerListFunc(ctx, options)
	}
	return nil, nil
}

func (m *mockDockerClient) ImagePull(ctx context.Context, ref string, options image.PullOptions) (io.ReadCloser, error) {
	if m.ImagePullFunc != nil {
		return m.ImagePullFunc(ctx, ref, options)
	}
	return nil, nil
}

func (m *mockDockerClient) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	if m.ContainerRemoveFunc != nil {
		return m.ContainerRemoveFunc(ctx, containerID, options)
	}
	return nil
}

func (m *mockDockerClient) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *v1.Platform, containerName string) (container.CreateResponse, error) {
	if m.ContainerCreateFunc != nil {
		return m.ContainerCreateFunc(ctx, config, hostConfig, networkingConfig, platform, containerName)
	}
	return container.CreateResponse{}, nil
}

func (m *mockDockerClient) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	if m.ContainerStartFunc != nil {
		return m.ContainerStartFunc(ctx, containerID, options)
	}
	return nil
}

func (m *mockDockerClient) RegistryLogin(ctx context.Context, authConfig registry.AuthConfig) (registry.AuthenticateOKBody, error) {
	if m.RegistryLoginFunc != nil {
		return m.RegistryLoginFunc(ctx, authConfig)
	}
	return registry.AuthenticateOKBody{}, nil
}

func (m *mockDockerClient) ImageInspectWithRaw(ctx context.Context, ref string) (types.ImageInspect, []byte, error) {
	if m.ImageInspectWithRawFunc != nil {
		return m.ImageInspectWithRawFunc(ctx, ref)
	}
	return types.ImageInspect{}, nil, nil
}

func TestIsSwarmMode(t *testing.T) {
	tests := []struct {
		name          string
		info          system.Info
		wantIsSwarm   bool
		wantError     bool
	}{
		{
			name: "swarm active",
			info: system.Info{
				Swarm: swarm.Info{
					LocalNodeState: swarm.LocalNodeStateActive,
				},
			},
			wantIsSwarm: true,
			wantError:   false,
		},
		{
			name: "swarm inactive",
			info: system.Info{
				Swarm: swarm.Info{
					LocalNodeState: swarm.LocalNodeStateInactive,
				},
			},
			wantIsSwarm: false,
			wantError:   false,
		},
{
			name: "error",
			info: system.Info{},
			wantIsSwarm: false,
			wantError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockDockerClient{
				InfoFunc: func(ctx context.Context) (system.Info, error) {
					if tt.wantError {
						return system.Info{}, errors.New("error")
					}
					return tt.info, nil
				},
			}
			dockerClient := &DockerClientImpl{client: mockClient}

			isSwarm, err := dockerClient.IsSwarmMode(context.Background())
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantIsSwarm, isSwarm)
			}
		})
	}
}

func TestUpdateService(t *testing.T) {
	tests := []struct {
		name          string
		serviceID     string
		spec          ServiceSpec
		setupMock     func(*mockDockerClient)
		wantErr       bool
		expectedError string
	}{
		{
			name:      "success",
			serviceID: "service1",
			spec: ServiceSpec{
				TaskTemplate: struct {
					ContainerSpec struct {
						Image string
					}
				}{
					ContainerSpec: struct {
						Image string
					}{
						Image: "new-image",
					},
				},
			},
			setupMock: func(m *mockDockerClient) {
				m.ServiceInspectWithRawFunc = func(ctx context.Context, serviceID string, options types.ServiceInspectOptions) (swarm.Service, []byte, error) {
					return swarm.Service{
						Meta: swarm.Meta{
							Version: swarm.Version{Index: 42},
						},
						Spec: swarm.ServiceSpec{
							Annotations: swarm.Annotations{
								Name: "prod_myna-dashboard",
								Labels: map[string]string{
									"com.docker.stack.namespace": "prod",
								},
							},
						},
					}, nil, nil
				}
				m.ServiceUpdateFunc = func(ctx context.Context, serviceID string, version swarm.Version, spec swarm.ServiceSpec, options types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error) {
					if serviceID != "service1" {
						t.Errorf("expected serviceID service1, got %s", serviceID)
					}
					if spec.TaskTemplate.ContainerSpec.Image != "new-image" {
						t.Errorf("expected image new-image, got %s", spec.TaskTemplate.ContainerSpec.Image)
					}
					if spec.Annotations.Name != "prod_myna-dashboard" {
						t.Errorf("expected annotations name prod_myna-dashboard, got %s", spec.Annotations.Name)
					}
					if spec.Annotations.Labels["com.docker.stack.namespace"] != "prod" {
						t.Errorf("expected label com.docker.stack.namespace=prod, got %v", spec.Annotations.Labels)
					}
					return swarm.ServiceUpdateResponse{}, nil
				}
			},
			wantErr:       false,
			expectedError: "",
		},
		{
			name:      "error from service update",
			serviceID: "service1",
			spec: ServiceSpec{
				TaskTemplate: struct {
					ContainerSpec struct {
						Image string
					}
				}{
					ContainerSpec: struct {
						Image string
					}{
						Image: "new-image",
					},
				},
			},
			setupMock: func(m *mockDockerClient) {
				m.ServiceInspectWithRawFunc = func(ctx context.Context, serviceID string, options types.ServiceInspectOptions) (swarm.Service, []byte, error) {
					return swarm.Service{
						Meta: swarm.Meta{
							Version: swarm.Version{Index: 1},
						},
						Spec: swarm.ServiceSpec{
							Annotations: swarm.Annotations{Name: "test-service"},
						},
					}, nil, nil
				}
				m.ServiceUpdateFunc = func(ctx context.Context, serviceID string, version swarm.Version, spec swarm.ServiceSpec, options types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error) {
					return swarm.ServiceUpdateResponse{}, errors.New("update failed")
				}
			},
			wantErr:       true,
			expectedError: "update failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockDockerClient{}
			if tt.setupMock != nil {
				tt.setupMock(mockClient)
			}
dockerClient := &DockerClientImpl{client: mockClient}

			err := dockerClient.UpdateService(context.Background(), tt.serviceID, tt.spec)
			if tt.wantErr {
				require.Error(t, err)
				if tt.expectedError != "" {
					assert.ErrorContains(t, err, tt.expectedError)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
func TestRecreateContainerSwarmManaged(t *testing.T) {
	tests := []struct {
		name          string
		containerID string
		setupMock	func(*mockDockerClient)
		wantErr		bool
		expectedError	string
	}{
		{
			name: "container is managed by swarm service",
			containerID: "container1",
			setupMock: func(m *mockDockerClient) {
m.ContainerInspectFunc = func(ctx context.Context, containerID string) (types.ContainerJSON, error) {
					return types.ContainerJSON{
						Config: &container.Config{
							Labels: map[string]string{
								"com.docker.swarm.service.name": "myservice",
							},
						},
					}, nil
				}
			},
			wantErr:		true,
			expectedError:	"container is managed by a swarm service",
		},
		{
			name:		"container is not managed by swarm service",
			containerID:	"container2",
			setupMock:	func(m *mockDockerClient) {
				m.ContainerInspectFunc = func(ctx context.Context, containerID string) (types.ContainerJSON, error) {
					return types.ContainerJSON{
						Config: &container.Config{
							Labels: map[string]string{},
						},
					}, nil
				}
			},
			wantErr:	false,
			expectedError:	"",
		},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mockClient := &mockDockerClient{}
				if tt.setupMock != nil {
					tt.setupMock(mockClient)
				}
			dockerClient := &DockerClientImpl{client: mockClient}

				// We need a dummy spec and newImage for the RecreateContainer call
			spec := ContainerSpec{}
			newImage := "image"

			err := dockerClient.RecreateContainer(context.Background(), tt.containerID, spec, newImage)
			if tt.wantErr {
				require.Error(t, err)
				if tt.expectedError != "" {
					assert.ErrorContains(t, err, tt.expectedError)
				}
			} else {
				require.NoError(t, err)
			}
			})
		}
		}

func TestPortBindingRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		input    map[nat.Port][]nat.PortBinding
	}{
		{
			name: "simple port mapping without host IP",
			input: map[nat.Port][]nat.PortBinding{
				"80/tcp": {{HostIP: "", HostPort: "8080"}},
			},
		},
		{
			name: "port mapping with host IP",
			input: map[nat.Port][]nat.PortBinding{
				"80/tcp": {{HostIP: "127.0.0.1", HostPort: "8080"}},
			},
		},
		{
			name: "port mapping without protocol suffix",
			input: map[nat.Port][]nat.PortBinding{
				"443": {{HostIP: "0.0.0.0", HostPort: "443"}},
			},
		},
		{
			name: "multiple port bindings",
			input: map[nat.Port][]nat.PortBinding{
				"80/tcp":  {{HostIP: "127.0.0.1", HostPort: "8080"}},
				"443/tcp": {{HostIP: "", HostPort: "8443"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Serialize
			serialized := portBindingsToStrings(tt.input)
			// Deserialize
			deserialized := parsePortBindings(serialized)

			for port, expectedBindings := range tt.input {
				got, ok := deserialized[port]
				require.True(t, ok, "missing port %s in deserialized", port)
				require.Equal(t, len(expectedBindings), len(got), "binding count mismatch for %s", port)
				for i, expected := range expectedBindings {
					assert.Equal(t, expected.HostIP, got[i].HostIP, "HostIP mismatch for %s", port)
					assert.Equal(t, expected.HostPort, got[i].HostPort, "HostPort mismatch for %s", port)
				}
			}
		})
	}
}

func TestParsePortBindings_Nil(t *testing.T) {
	result := parsePortBindings(nil)
	assert.Assert(t, result == nil)
}

func TestPortBindingsToStrings_Nil(t *testing.T) {
	result := portBindingsToStrings(nil)
	assert.Assert(t, result == nil)
}

func TestInspectContainer(t *testing.T) {
	mockClient := &mockDockerClient{
		ContainerInspectFunc: func(ctx context.Context, id string) (types.ContainerJSON, error) {
			return types.ContainerJSON{
				Config: &container.Config{
					Env:        []string{"FOO=bar", "BAZ=qux"},
					Entrypoint: strslice.StrSlice{"/entry.sh"},
					Cmd:        strslice.StrSlice{"arg1", "arg2"},
					WorkingDir: "/app",
					User:       "1000:1000",
					Labels:     map[string]string{"label1": "value1"},
					Healthcheck: &container.HealthConfig{
						Test: []string{"CMD", "curl", "-f", "localhost"},
					},
				},
				ContainerJSONBase: &container.ContainerJSONBase{
					HostConfig: &container.HostConfig{
						NetworkMode: "bridge",
						Links:       []string{"container1:alias1"},
						RestartPolicy: container.RestartPolicy{
							Name: "always",
						},
						CapAdd:      []string{"SYS_ADMIN"},
						CapDrop:     []string{"ALL"},
						SecurityOpt: []string{"no-new-privileges:true"},
						Tmpfs:       map[string]string{"/tmp": "size=64M"},
						Resources: container.Resources{
							Devices: []container.DeviceMapping{
								{PathOnHost: "/dev/fuse", PathInContainer: "/dev/fuse", CgroupPermissions: "rwm"},
							},
						},
						PidMode: "host",
						IpcMode: "private",
						Mounts: []mount.Mount{
							{
								Type:     mount.TypeBind,
								Source:   "/host/path",
								Target:   "/container/path",
								ReadOnly: true,
								BindOptions: &mount.BindOptions{
									Propagation: mount.PropagationRSlave,
								},
							},
						},
						PortBindings: nat.PortMap{
							"443/tcp": {{HostIP: "127.0.0.1", HostPort: "8443"}},
						},
					},
				},
			}, nil
		},
	}

	d := &DockerClientImpl{client: mockClient}
	spec, err := d.InspectContainer(context.Background(), "test-container")
	require.NoError(t, err)

	// Verify Config fields
	assert.DeepEqual(t, []string{"FOO=bar", "BAZ=qux"}, spec.Env)
	assert.DeepEqual(t, []string{"/entry.sh"}, spec.Entrypoint)
	assert.DeepEqual(t, []string{"arg1", "arg2"}, spec.Cmd)
	assert.Equal(t, "/app", spec.WorkingDir)
	assert.Equal(t, "1000:1000", spec.User)
	assert.DeepEqual(t, map[string]string{"label1": "value1"}, spec.Labels)

	// Verify HostConfig fields
	assert.Equal(t, "bridge", spec.NetworkMode)
	assert.DeepEqual(t, []string{"container1:alias1"}, spec.Links)
	assert.Equal(t, "always", spec.RestartPolicy)
	assert.DeepEqual(t, []string{"SYS_ADMIN"}, spec.CapAdd)
	assert.DeepEqual(t, []string{"ALL"}, spec.CapDrop)
	assert.DeepEqual(t, []string{"no-new-privileges:true"}, spec.SecurityOpt)
	assert.DeepEqual(t, []string{"/tmp=size=64M"}, spec.Tmpfs)
	assert.DeepEqual(t, []string{"/dev/fuse:/dev/fuse:rwm"}, spec.Devices)
	assert.Equal(t, "host", spec.PidMode)
	assert.Equal(t, "private", spec.IpcMode)

	// Verify PortBindings
	require.Equal(t, 1, len(spec.PortBindings))
	assert.Equal(t, "443/tcp:127.0.0.1:8443", spec.PortBindings[0])

	// Verify Mounts - should be valid JSON
	require.Equal(t, 1, len(spec.Mounts))
	var parsedMount mount.Mount
	err = json.Unmarshal([]byte(spec.Mounts[0]), &parsedMount)
	require.NoError(t, err)
	assert.Equal(t, mount.TypeBind, parsedMount.Type)
	assert.Equal(t, "/host/path", parsedMount.Source)
	assert.Equal(t, "/container/path", parsedMount.Target)
	assert.Equal(t, true, parsedMount.ReadOnly)
	require.NotNil(t, parsedMount.BindOptions)
	assert.Equal(t, mount.PropagationRSlave, parsedMount.BindOptions.Propagation)

	// Verify Healthcheck
	require.NotEqual(t, "", spec.Healthcheck)
	var hc container.HealthConfig
	err = json.Unmarshal([]byte(spec.Healthcheck), &hc)
	require.NoError(t, err)
	assert.DeepEqual(t, []string{"CMD", "curl", "-f", "localhost"}, hc.Test)
}

func TestMountRoundTrip(t *testing.T) {
	t.Run("bind mount with ReadOnly and BindOptions", func(t *testing.T) {
		mounts := []mount.Mount{
			{
				Type:     mount.TypeBind,
				Source:   "/host/path",
				Target:   "/container/path",
				ReadOnly: true,
				BindOptions: &mount.BindOptions{
					Propagation: mount.PropagationRSlave,
				},
			},
		}

		strings := mountsToStrings(mounts)
		require.Equal(t, 1, len(strings))

		parsed := parseMounts(strings)
		require.Equal(t, 1, len(parsed))
		assert.Equal(t, mount.TypeBind, parsed[0].Type)
		assert.Equal(t, "/host/path", parsed[0].Source)
		assert.Equal(t, "/container/path", parsed[0].Target)
		assert.Equal(t, true, parsed[0].ReadOnly)
		require.NotNil(t, parsed[0].BindOptions)
		assert.Equal(t, mount.PropagationRSlave, parsed[0].BindOptions.Propagation)
	})

	t.Run("volume mount with VolumeOptions", func(t *testing.T) {
		mounts := []mount.Mount{
			{
				Type:   mount.TypeVolume,
				Source: "myvolume",
				Target: "/container/vol",
				VolumeOptions: &mount.VolumeOptions{
					NoCopy: true,
					Labels: map[string]string{"key": "value"},
				},
			},
		}

		strings := mountsToStrings(mounts)
		require.Equal(t, 1, len(strings))

		parsed := parseMounts(strings)
		require.Equal(t, 1, len(parsed))
		assert.Equal(t, mount.TypeVolume, parsed[0].Type)
		assert.Equal(t, "myvolume", parsed[0].Source)
		assert.Equal(t, "/container/vol", parsed[0].Target)
		require.NotNil(t, parsed[0].VolumeOptions)
		assert.Equal(t, true, parsed[0].VolumeOptions.NoCopy)
		assert.DeepEqual(t, map[string]string{"key": "value"}, parsed[0].VolumeOptions.Labels)
	})

	t.Run("nil mounts returns nil", func(t *testing.T) {
		result := mountsToStrings(nil)
		assert.Assert(t, result == nil)

		result2 := parseMounts(nil)
		assert.Assert(t, result2 == nil)
	})

	t.Run("empty mounts slice round-trips", func(t *testing.T) {
		result := mountsToStrings([]mount.Mount{})
		assert.Equal(t, 0, len(result))

		parsed := parseMounts(result)
		assert.Equal(t, 0, len(parsed))
	})
}
