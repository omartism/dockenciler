package reconciler

import (
	"context"
	"fmt"
	"testing"

	"github.com/omarismael/dockenciler/internal/testutil"
	"github.com/omarismael/dockenciler/pkg/config"
	"github.com/omarismael/dockenciler/pkg/docker"
	"github.com/omarismael/dockenciler/pkg/registry"
)

func testReconciler(mockDocker *testutil.MockDockerClient, mockRegistry *testutil.MockRegistry) *Reconciler {
	return &Reconciler{
		DockerClient: mockDocker,
		Registry:     mockRegistry,
		Notifier:     &testutil.MockNotifier{},
		Config: &config.Config{
			Docker: config.Docker{LabelFilter: "dockenciler.autoupdate=true"},
		},
	}
}

func stubUpdatePath(mockDocker *testutil.MockDockerClient, mockRegistry *testutil.MockRegistry) {
	mockRegistry.GetAuthFunc = func(ctx context.Context) (registry.Auth, error) {
		return registry.Auth{Username: "u", Password: "p", RegistryHost: "ghcr.io"}, nil
	}
	mockDocker.AuthenticateFunc = func(ctx context.Context, username, password, registryHost string) error {
		return nil
	}
	mockDocker.PullImageFunc = func(ctx context.Context, imageRef string) error { return nil }
	mockDocker.InspectContainerFunc = func(ctx context.Context, id string) (docker.ContainerSpec, error) {
		return docker.ContainerSpec{Name: id}, nil
	}
	mockDocker.RecreateContainerFunc = func(ctx context.Context, id string, spec docker.ContainerSpec, newImage string) error {
		return nil
	}
}

// Once a tag moves locally, the container still runs the previous image while
// the local tag already matches the registry. The running digest must force
// the update anyway.
func TestReconcile_TagMovedLocally_UpdatesFromRunningDigest(t *testing.T) {
	mockDocker := &testutil.MockDockerClient{}
	mockRegistry := &testutil.MockRegistry{}
	stubUpdatePath(mockDocker, mockRegistry)

	var recreated bool
	mockDocker.ListContainersFunc = func(ctx context.Context, labelFilter string) ([]docker.Container, error) {
		return []docker.Container{{
			ID:      "c1",
			Image:   "ghcr.io/owner/repo:staging",
			ImageID: "sha256:oldrunning",
			Labels:  map[string]string{"dockenciler.autoupdate": "true"},
		}}, nil
	}
	mockDocker.GetImageDigestFunc = func(ctx context.Context, imageRef string) (string, error) {
		return "sha256:newtag", nil
	}
	mockRegistry.GetLatestDigestFunc = func(ctx context.Context, imageRef string, criteria registry.Criteria) (string, error) {
		return "sha256:newtag", nil
	}
	mockDocker.RecreateContainerFunc = func(ctx context.Context, id string, spec docker.ContainerSpec, newImage string) error {
		recreated = true
		if newImage != "ghcr.io/owner/repo:staging" {
			t.Errorf("Expected recreate with tag ref, got %q", newImage)
		}
		return nil
	}

	if err := testReconciler(mockDocker, mockRegistry).Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if !recreated {
		t.Error("Expected update when running digest lags the local tag, got none")
	}
}

func TestReconcile_RunningMatchesTagAndRegistry_NoUpdate(t *testing.T) {
	mockDocker := &testutil.MockDockerClient{}
	mockRegistry := &testutil.MockRegistry{}

	var recreated bool
	mockDocker.ListContainersFunc = func(ctx context.Context, labelFilter string) ([]docker.Container, error) {
		return []docker.Container{{
			ID:      "c1",
			Image:   "ghcr.io/owner/repo:staging",
			ImageID: "sha256:same",
			Labels:  map[string]string{"dockenciler.autoupdate": "true"},
		}}, nil
	}
	mockDocker.GetImageDigestFunc = func(ctx context.Context, imageRef string) (string, error) {
		return "sha256:same", nil
	}
	mockRegistry.GetLatestDigestFunc = func(ctx context.Context, imageRef string, criteria registry.Criteria) (string, error) {
		return "sha256:same", nil
	}
	mockDocker.RecreateContainerFunc = func(ctx context.Context, id string, spec docker.ContainerSpec, newImage string) error {
		recreated = true
		return nil
	}

	if err := testReconciler(mockDocker, mockRegistry).Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if recreated {
		t.Error("Expected no update when running digest matches, got one")
	}
}

// A pruned local image must be pulled (with fresh auth) before the comparison
// instead of failing the container.
func TestReconcile_PrunedImage_PullsBeforeCompare(t *testing.T) {
	mockDocker := &testutil.MockDockerClient{}
	mockRegistry := &testutil.MockRegistry{}
	stubUpdatePath(mockDocker, mockRegistry)

	var calls int
	var pulledRef string
	mockDocker.ListContainersFunc = func(ctx context.Context, labelFilter string) ([]docker.Container, error) {
		return []docker.Container{{
			ID:      "c1",
			Image:   "ghcr.io/owner/repo:staging",
			ImageID: "sha256:pruned",
			Labels:  map[string]string{"dockenciler.autoupdate": "true"},
		}}, nil
	}
	mockDocker.GetImageDigestFunc = func(ctx context.Context, imageRef string) (string, error) {
		calls++
		if calls == 1 {
			return "", fmt.Errorf("wrapped: %w: %s", docker.ErrImageNotFound, imageRef)
		}
		return "sha256:newtag", nil
	}
	mockDocker.PullImageFunc = func(ctx context.Context, imageRef string) error {
		pulledRef = imageRef
		return nil
	}
	mockRegistry.GetLatestDigestFunc = func(ctx context.Context, imageRef string, criteria registry.Criteria) (string, error) {
		return "sha256:newtag", nil
	}
	var recreated bool
	mockDocker.RecreateContainerFunc = func(ctx context.Context, id string, spec docker.ContainerSpec, newImage string) error {
		recreated = true
		return nil
	}

	if err := testReconciler(mockDocker, mockRegistry).Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if pulledRef != "ghcr.io/owner/repo:staging" {
		t.Errorf("Expected pruned tag pulled before compare, got %q", pulledRef)
	}
	// Running sha256:pruned vs tag sha256:newtag → must converge.
	if !recreated {
		t.Error("Expected update after healing pruned image, got none")
	}
}

func TestReconcile_PrunedImage_PullFailureFailsClean(t *testing.T) {
	mockDocker := &testutil.MockDockerClient{}
	mockRegistry := &testutil.MockRegistry{}
	stubUpdatePath(mockDocker, mockRegistry)

	mockDocker.ListContainersFunc = func(ctx context.Context, labelFilter string) ([]docker.Container, error) {
		return []docker.Container{{
			ID:      "c1",
			Image:   "ghcr.io/owner/repo:staging",
			ImageID: "sha256:pruned",
			Labels:  map[string]string{"dockenciler.autoupdate": "true"},
		}}, nil
	}
	mockDocker.GetImageDigestFunc = func(ctx context.Context, imageRef string) (string, error) {
		return "", fmt.Errorf("wrapped: %w: %s", docker.ErrImageNotFound, imageRef)
	}
	mockDocker.PullImageFunc = func(ctx context.Context, imageRef string) error {
		return fmt.Errorf("pull denied")
	}
	var recreated bool
	mockDocker.RecreateContainerFunc = func(ctx context.Context, id string, spec docker.ContainerSpec, newImage string) error {
		recreated = true
		return nil
	}

	if err := testReconciler(mockDocker, mockRegistry).Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if recreated {
		t.Error("Expected no update when healing pull fails, got one")
	}
}
