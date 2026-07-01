package testutil

import (
    "context"

    "github.com/omarismael/dockenciler/pkg/registry"
    "github.com/omarismael/dockenciler/pkg/docker"
    "github.com/omarismael/dockenciler/pkg/notifier"
)

// MockRegistry is a mock implementation of the Registry interface
// using function fields for flexible testing
type MockRegistry struct {
    GetLatestDigestFunc func(ctx context.Context, imageRef string, criteria registry.Criteria) (string, error)
    GetImageVersionFunc func(ctx context.Context, imageRef string) (string, error)
    GetAuthTokenFunc    func(ctx context.Context) (string, string, error) // returns registryURL and token
}

func (m *MockRegistry) GetLatestDigest(ctx context.Context, imageRef string, criteria registry.Criteria) (string, error) {
    if m.GetLatestDigestFunc != nil {
        return m.GetLatestDigestFunc(ctx, imageRef, criteria)
    }
    return "", nil
}

func (m *MockRegistry) GetImageVersion(ctx context.Context, imageRef string) (string, error) {
    if m.GetImageVersionFunc != nil {
        return m.GetImageVersionFunc(ctx, imageRef)
    }
    return "", nil
}

func (m *MockRegistry) GetAuthToken(ctx context.Context) (string, string, error) {
    if m.GetAuthTokenFunc != nil {
        return m.GetAuthTokenFunc(ctx)
    }
    return "", "", nil
}

// MockDockerClient is a mock implementation of the DockerClient interface
// using function fields for flexible testing
type MockDockerClient struct {
    ListContainersFunc    func(ctx context.Context, labelFilter string) ([]docker.Container, error)
    InspectContainerFunc func(ctx context.Context, id string) (docker.ContainerSpec, error)
    PullImageFunc        func(ctx context.Context, imageRef string) error
    RecreateContainerFunc func(ctx context.Context, id string, spec docker.ContainerSpec, newImage string) error
    UpdateServiceFunc    func(ctx context.Context, serviceID string, spec docker.ServiceSpec) error
    GetImageDigestFunc   func(ctx context.Context, imageRef string) (string, error)
    AuthenticateFunc     func(ctx context.Context, registryURL string, token string) error
    IsSwarmModeFunc      func(ctx context.Context) (bool, error)
    GetServiceIDFunc     func(ctx context.Context, containerID string) (string, error)
}

func (m *MockDockerClient) ListContainers(ctx context.Context, labelFilter string) ([]docker.Container, error) {
    if m.ListContainersFunc != nil {
        return m.ListContainersFunc(ctx, labelFilter)
    }
    return nil, nil
}

func (m *MockDockerClient) InspectContainer(ctx context.Context, id string) (docker.ContainerSpec, error) {
    if m.InspectContainerFunc != nil {
        return m.InspectContainerFunc(ctx, id)
    }
    return docker.ContainerSpec{}, nil
}

func (m *MockDockerClient) PullImage(ctx context.Context, imageRef string) error {
    if m.PullImageFunc != nil {
        return m.PullImageFunc(ctx, imageRef)
    }
    return nil
}

func (m *MockDockerClient) RecreateContainer(ctx context.Context, id string, spec docker.ContainerSpec, newImage string) error {
    if m.RecreateContainerFunc != nil {
        return m.RecreateContainerFunc(ctx, id, spec, newImage)
    }
    return nil
}

func (m *MockDockerClient) UpdateService(ctx context.Context, serviceID string, spec docker.ServiceSpec) error {
    if m.UpdateServiceFunc != nil {
        return m.UpdateServiceFunc(ctx, serviceID, spec)
    }
    return nil
}

func (m *MockDockerClient) GetImageDigest(ctx context.Context, imageRef string) (string, error) {
    if m.GetImageDigestFunc != nil {
        return m.GetImageDigestFunc(ctx, imageRef)
    }
    return "", nil
}

func (m *MockDockerClient) Authenticate(ctx context.Context, registryURL string, token string) error {
    if m.AuthenticateFunc != nil {
        return m.AuthenticateFunc(ctx, registryURL, token)
    }
    return nil
}

func (m *MockDockerClient) IsSwarmMode(ctx context.Context) (bool, error) {
    if m.IsSwarmModeFunc != nil {
        return m.IsSwarmModeFunc(ctx)
    }
    return false, nil
}

func (m *MockDockerClient) GetServiceID(ctx context.Context, containerID string) (string, error) {
    if m.GetServiceIDFunc != nil {
        return m.GetServiceIDFunc(ctx, containerID)
    }
    return "", nil
}

// MockNotifier is a mock implementation of the Notifier interface
// using function fields for flexible testing
type MockNotifier struct {
    NotifyFunc func(ctx context.Context, n notifier.Notification) error
}

func (m *MockNotifier) Notify(ctx context.Context, n notifier.Notification) error {
    if m.NotifyFunc != nil {
        return m.NotifyFunc(ctx, n)
    }
    return nil
}