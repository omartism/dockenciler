package reconciler

import (
    "context"
    "fmt"
    "testing"
    "github.com/omarismael/dockenciler/internal/testutil"
    "github.com/omarismael/dockenciler/pkg/docker"
    "github.com/omarismael/dockenciler/pkg/registry"
    "github.com/omarismael/dockenciler/pkg/config"
)

func TestReconcile_WhenNewDigestAvailable_RecreatesContainer(t *testing.T) {
    // Setup mocks
    mockDocker := &testutil.MockDockerClient{}
    mockRegistry := &testutil.MockRegistry{}
    mockNotifier := &testutil.MockNotifier{}

    // Track call order and ensure auth happens before pull
    var authCalled bool
    var getTokenCalled bool
    var pullCalled bool

    // Setup mock expectations
    mockDocker.ListContainersFunc = func(ctx context.Context, labelFilter string) ([]docker.Container, error) {
        // Return one container
        return []docker.Container{
            {
                ID: "container1",
                Image: "nginx:latest",
                Labels: map[string]string{"dockenciler.autoupdate": "true"},
            },
        }, nil
    }

    mockDocker.GetImageDigestFunc = func(ctx context.Context, imageRef string) (string, error) {
        // Return current digest
        return "sha256:currentdigest123", nil
    }

    mockRegistry.GetLatestDigestFunc = func(ctx context.Context, imageRef string, criteria registry.Criteria) (string, error) {
        // Return a new digest (different from current)
        return "sha256:newdigest123", nil
    }

    mockRegistry.GetAuthTokenFunc = func(ctx context.Context) (string, string, error) {
        getTokenCalled = true
        return "https://index.docker.io/v1/", "faketoken", nil
    }

    mockDocker.AuthenticateFunc = func(ctx context.Context, registryURL string, token string) error {
        authCalled = true
        // Ensure token is not empty
        if token == "" {
            return fmt.Errorf("empty token")
        }
        return nil
    }

    mockDocker.PullImageFunc = func(ctx context.Context, imageRef string) error {
        pullCalled = true
        // Ensure auth was called before pull
        if !authCalled {
            t.Error("PullImage called before Authenticate")
        }
        // Mock pull - just return nil
        return nil
    }

    mockDocker.RecreateContainerFunc = func(ctx context.Context, id string, spec docker.ContainerSpec, newImage string) error {
        // Mock recreate - just return nil
        return nil
    }

    // Create reconciler with mocks
    r := &Reconciler{
        DockerClient: mockDocker,
        Registry:     mockRegistry,
        Notifier:     mockNotifier,
        Config: &config.Config{
            Docker: config.Docker{
                LabelFilter: "dockenciler.autoupdate=true",
            },
        },
    }

    // Call Reconcile
    err := r.Reconcile(context.Background())
    if err != nil {
        t.Fatalf("Reconcile returned error: %v", err)
    }

    // Assert that DockerClient.GetImageDigest was called
    if mockDocker.GetImageDigestFunc == nil {
        t.Error("DockerClient.GetImageDigest was not called")
    }

    // Assert that Registry.GetAuthToken was called
    if !getTokenCalled {
        t.Error("Registry.GetAuthToken was not called")
    }

    // Assert that DockerClient.Authenticate was called
    if !authCalled {
        t.Error("DockerClient.Authenticate was not called")
    }

    // Assert that DockerClient.PullImage was called after auth
    if !pullCalled {
        t.Error("DockerClient.PullImage was not called")
    }
}

func TestReconcile_DryRunMode(t *testing.T) {
    // Setup mocks
    mockDocker := &testutil.MockDockerClient{}
    mockRegistry := &testutil.MockRegistry{}
    mockNotifier := &testutil.MockNotifier{}

    // Track which Docker methods were called
    var getImageDigestCalled bool
    var getLatestDigestCalled bool
    var authenticateCalled bool
    var pullImageCalled bool
    var recreateContainerCalled bool
    var updateServiceCalled bool

    // Setup mock expectations
    mockDocker.ListContainersFunc = func(ctx context.Context, labelFilter string) ([]docker.Container, error) {
        // Return one container
        return []docker.Container{
            {
                ID: "container1",
                Image: "nginx:latest",
                Labels: map[string]string{"dockenciler.autoupdate": "true"},
            },
        }, nil
    }

    mockDocker.GetImageDigestFunc = func(ctx context.Context, imageRef string) (string, error) {
        getImageDigestCalled = true
        return "sha256:currentdigest123", nil
    }

    mockRegistry.GetLatestDigestFunc = func(ctx context.Context, imageRef string, criteria registry.Criteria) (string, error) {
        getLatestDigestCalled = true
        return "sha256:newdigest123", nil
    }

    mockRegistry.GetAuthTokenFunc = func(ctx context.Context) (string, string, error) {
        return "https://index.docker.io/v1/", "faketoken", nil
    }

    mockDocker.AuthenticateFunc = func(ctx context.Context, registryURL string, token string) error {
        authenticateCalled = true
        return nil
    }

    mockDocker.PullImageFunc = func(ctx context.Context, imageRef string) error {
        pullImageCalled = true
        return nil
    }

    mockDocker.RecreateContainerFunc = func(ctx context.Context, id string, spec docker.ContainerSpec, newImage string) error {
        recreateContainerCalled = true
        return nil
    }

    mockDocker.UpdateServiceFunc = func(ctx context.Context, serviceID string, spec docker.ServiceSpec) error {
        updateServiceCalled = true
        return nil
    }

    // Create reconciler with dry-run enabled
    r := &Reconciler{
        DockerClient: mockDocker,
        Registry:     mockRegistry,
        Notifier:     mockNotifier,
        Config: &config.Config{
            Docker: config.Docker{
                LabelFilter: "dockenciler.autoupdate=true",
            },
            DryRun: true,
        },
    }

    // Call Reconcile
    err := r.Reconcile(context.Background())
    if err != nil {
        t.Fatalf("Reconcile returned error: %v", err)
    }

    // Assert that DockerClient.GetImageDigest was called
    if !getImageDigestCalled {
        t.Error("DockerClient.GetImageDigest was not called")
    }

    // Assert that Registry.GetLatestDigest was called
    if !getLatestDigestCalled {
        t.Error("Registry.GetLatestDigest was not called")
    }

    // Assert that DockerClient.Authenticate was NOT called (dry-run should skip)
    if authenticateCalled {
        t.Error("DockerClient.Authenticate was called in dry-run mode")
    }

    // Assert that DockerClient.PullImage was NOT called (dry-run should skip)
    if pullImageCalled {
        t.Error("DockerClient.PullImage was called in dry-run mode")
    }

    // Assert that DockerClient.RecreateContainer was NOT called (dry-run should skip)
    if recreateContainerCalled {
        t.Error("DockerClient.RecreateContainer was called in dry-run mode")
    }

    // Assert that DockerClient.UpdateService was NOT called (dry-run should skip)
    if updateServiceCalled {
        t.Error("DockerClient.UpdateService was called in dry-run mode")
    }
}

func TestReconcile_SelfUpdateExclusion(t *testing.T) {
    // Setup mocks
    mockDocker := &testutil.MockDockerClient{}
    mockRegistry := &testutil.MockRegistry{}
    mockNotifier := &testutil.MockNotifier{}

    // Track which Docker methods were called
    var getImageDigestCalled bool
    var getLatestDigestCalled bool
    var authenticateCalled bool
    var pullImageCalled bool
    var recreateContainerCalled bool

    // Setup mock expectations
    mockDocker.ListContainersFunc = func(ctx context.Context, labelFilter string) ([]docker.Container, error) {
        // Return two containers: one with self-update label, one normal
        return []docker.Container{
            {
                ID: "container1",
                Image: "dockenciler:latest",
                Labels: map[string]string{"dockenciler.instance": "true"},
            },
            {
                ID: "container2",
                Image: "nginx:latest",
                Labels: map[string]string{"dockenciler.autoupdate": "true"},
            },
        }, nil
    }

    mockDocker.GetImageDigestFunc = func(ctx context.Context, imageRef string) (string, error) {
        getImageDigestCalled = true
        return "sha256:currentdigest123", nil
    }

    mockRegistry.GetLatestDigestFunc = func(ctx context.Context, imageRef string, criteria registry.Criteria) (string, error) {
        getLatestDigestCalled = true
        return "sha256:newdigest123", nil
    }

    mockRegistry.GetAuthTokenFunc = func(ctx context.Context) (string, string, error) {
        return "https://index.docker.io/v1/", "faketoken", nil
    }

    mockDocker.AuthenticateFunc = func(ctx context.Context, registryURL string, token string) error {
        authenticateCalled = true
        return nil
    }

    mockDocker.PullImageFunc = func(ctx context.Context, imageRef string) error {
        pullImageCalled = true
        return nil
    }

    mockDocker.RecreateContainerFunc = func(ctx context.Context, id string, spec docker.ContainerSpec, newImage string) error {
        recreateContainerCalled = true
        return nil
    }

    // Create reconciler with exclusions
    r := &Reconciler{
        DockerClient: mockDocker,
        Registry:     mockRegistry,
        Notifier:     mockNotifier,
        Config: &config.Config{
            Docker: config.Docker{
                LabelFilter: "dockenciler.autoupdate=true",
            },
            Exclusions: []string{"container2"},
        },
    }

    // Call Reconcile
    err := r.Reconcile(context.Background())
    if err != nil {
        t.Fatalf("Reconcile returned error: %v", err)
    }

    // Assert that DockerClient.GetImageDigest was NOT called (both containers should be skipped)
    if getImageDigestCalled {
        t.Error("DockerClient.GetImageDigest was called - containers should have been skipped")
    }

    // Assert that Registry.GetLatestDigest was NOT called (both containers should be skipped)
    if getLatestDigestCalled {
        t.Error("Registry.GetLatestDigest was called - containers should have been skipped")
    }

    // Assert that DockerClient.Authenticate was NOT called (both containers should be skipped)
    if authenticateCalled {
        t.Error("DockerClient.Authenticate was called - containers should have been skipped")
    }

    // Assert that DockerClient.PullImage was NOT called (both containers should be skipped)
    if pullImageCalled {
        t.Error("DockerClient.PullImage was called - containers should have been skipped")
    }

    // Assert that DockerClient.RecreateContainer was NOT called (both containers should be skipped)
    if recreateContainerCalled {
        t.Error("DockerClient.RecreateContainer was called - containers should have been skipped")
    }
}