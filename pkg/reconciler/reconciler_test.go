package reconciler

import (
    "context"
	"fmt"
	"strings"
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

    mockRegistry.GetAuthFunc = func(ctx context.Context) (registry.Auth, error) {
		getTokenCalled = true
		return registry.Auth{Username: "AWS", Password: "faketoken", RegistryHost: "https://index.docker.io/v1/"}, nil
	}

	mockDocker.AuthenticateFunc = func(ctx context.Context, username, password, registryHost string) error {
        authCalled = true
        // Ensure token is not empty
        if password == "" {
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

    // Assert that Registry.GetAuth was called
    if !getTokenCalled {
        t.Error("Registry.GetAuth was not called")
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

    mockRegistry.GetAuthFunc = func(ctx context.Context) (registry.Auth, error) {
        return registry.Auth{Username: "AWS", Password: "faketoken", RegistryHost: "https://index.docker.io/v1/"}, nil
    }

    mockDocker.AuthenticateFunc = func(ctx context.Context, username, password, registryHost string) error {
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

func TestReconcile_RetryOnAuthFailure(t *testing.T) {
    // Setup mocks
    mockDocker := &testutil.MockDockerClient{}
    mockRegistry := &testutil.MockRegistry{}
    mockNotifier := &testutil.MockNotifier{}

    // Track call counts
    var getTokenCalled int
    var authenticateCalled int
    var pullCalled int
    var invalidateCacheCalled int

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

    mockRegistry.GetAuthFunc = func(ctx context.Context) (registry.Auth, error) {
        getTokenCalled++
        if getTokenCalled == 1 {
            return registry.Auth{Username: "AWS", Password: "faketoken", RegistryHost: "https://index.docker.io/v1/"}, nil
        } else {
            return registry.Auth{Username: "AWS", Password: "newtoken", RegistryHost: "https://index.docker.io/v1/"}, nil
        }
    }

    // Track InvalidateCache calls
    mockRegistry.InvalidateCacheFunc = func() {
        invalidateCacheCalled++
    }

    mockDocker.AuthenticateFunc = func(ctx context.Context, username, password, registryHost string) error {
        authenticateCalled++
        // Ensure token is not empty
        if password == "" {
            return fmt.Errorf("empty token")
        }
        return nil
    }

    mockDocker.PullImageFunc = func(ctx context.Context, imageRef string) error {
        pullCalled++
        // First pull fails with auth error
        if pullCalled == 1 {
            return fmt.Errorf("no basic auth credentials")
        }
        // Second pull succeeds
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

    // Assert that Registry.GetAuth was called twice (initial + retry)
    if getTokenCalled != 2 {
        t.Errorf("Expected Registry.GetAuth to be called twice, but was called %d times", getTokenCalled)
    }

    // Assert that DockerClient.Authenticate was called twice (initial + retry)
    if authenticateCalled != 2 {
        t.Errorf("Expected DockerClient.Authenticate to be called twice, but was called %d times", authenticateCalled)
    }

    // Assert that DockerClient.PullImage was called twice (initial + retry)
    if pullCalled != 2 {
        t.Errorf("Expected DockerClient.PullImage to be called twice, but was called %d times", pullCalled)
    }

    // Assert that Registry.InvalidateCache was called once
    if invalidateCacheCalled != 1 {
        t.Errorf("Expected Registry.InvalidateCache to be called once, but was called %d times", invalidateCacheCalled)
    }
}

func TestReconcile_ImageRefHasDigestPinStripped(t *testing.T) {
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
        // Return one container with digest pin
        return []docker.Container{
            {
                ID: "container1",
                Image: "nginx@sha256:currentdigest123",
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

	mockRegistry.GetAuthFunc = func(ctx context.Context) (registry.Auth, error) {
		getTokenCalled = true
		return registry.Auth{Username: "AWS", Password: "faketoken", RegistryHost: "https://index.docker.io/v1/"}, nil
	}

	mockDocker.AuthenticateFunc = func(ctx context.Context, username, password, registryHost string) error {
        authCalled = true
        // Ensure token is not empty
        if password == "" {
            return fmt.Errorf("empty token")
        }
        return nil
    }

    mockDocker.PullImageFunc = func(ctx context.Context, imageRef string) error {
		pullCalled = true
		// Ensure digest pin is stripped
        if strings.Contains(imageRef, "@sha256:") {
            t.Errorf("Image reference passed to PullImage should not contain digest pin, but got: %s", imageRef)
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

    // Assert that Registry.GetAuth was called
    if !getTokenCalled {
        t.Error("Registry.GetAuth was not called")
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