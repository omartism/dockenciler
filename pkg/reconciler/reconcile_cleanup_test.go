package reconciler

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/omarismael/dockenciler/internal/testutil"
	"github.com/omarismael/dockenciler/pkg/docker"
	"github.com/omarismael/dockenciler/pkg/registry"
)

// cleanupHarness wires an update-path reconciler with cleanup enabled and
// records every RemoveImage call. tagDigest is what the local tag holds before
// the pull, postPullDigest what it resolves to afterwards, latestDigest what
// the registry serves.
func cleanupHarness(container docker.Container, tagDigest, postPullDigest, latestDigest string) (*Reconciler, *testutil.MockDockerClient, *[]string) {
	mockDocker := &testutil.MockDockerClient{}
	mockRegistry := &testutil.MockRegistry{}
	stubUpdatePath(mockDocker, mockRegistry)

	mockDocker.ListContainersFunc = func(ctx context.Context, _ string) ([]docker.Container, error) {
		return []docker.Container{container}, nil
	}
	digestReads := 0
	mockDocker.GetImageDigestFunc = func(ctx context.Context, _ string) (string, error) {
		digestReads++
		if digestReads == 1 {
			return tagDigest, nil
		}
		return postPullDigest, nil
	}
	mockRegistry.GetLatestDigestFunc = func(ctx context.Context, _ string, _ registry.Criteria) (string, error) {
		return latestDigest, nil
	}

	removed := &[]string{}
	mockDocker.RemoveImageFunc = func(ctx context.Context, imageID string) error {
		*removed = append(*removed, imageID)
		return nil
	}

	r := testReconciler(mockDocker, mockRegistry)
	r.Config.Docker.CleanupOldImages = true
	return r, mockDocker, removed
}

func autoupdateContainer(imageID string) docker.Container {
	return docker.Container{
		ID:      "c1",
		Image:   "ghcr.io/owner/repo:staging",
		ImageID: imageID,
		Labels:  map[string]string{"dockenciler.autoupdate": "true"},
	}
}

// An update leaves two images behind: the one the old container ran and the one
// the tag pointed at before the pull. Both must go once the replacement runs.
func TestReconcile_CleanupRemovesRunningAndPreviouslyTaggedImages(t *testing.T) {
	r, _, removed := cleanupHarness(
		autoupdateContainer("sha256:running"),
		"sha256:tagged",
		"sha256:pulled",
		"sha256:published",
	)

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	want := []string{"sha256:running", "sha256:tagged"}
	slices.Sort(*removed)
	if !slices.Equal(*removed, want) {
		t.Errorf("Expected superseded images %v removed, got %v", want, *removed)
	}
}

// When the local tag already moved ahead of the container, only the running
// image is superseded — the freshly pulled one must never be removed.
func TestReconcile_CleanupKeepsTheNewlyPulledImage(t *testing.T) {
	r, _, removed := cleanupHarness(
		autoupdateContainer("sha256:running"),
		"sha256:pulled",
		"sha256:pulled",
		"sha256:published",
	)

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	if !slices.Equal(*removed, []string{"sha256:running"}) {
		t.Errorf("Expected only the running image removed, got %v", *removed)
	}
}

// The registry digest and the local image ID are different kinds of digest for
// multi-platform images, so a container can be updated on every tick while its
// image never changes. Cleanup must then remove nothing: the image the new
// container runs is the one local tag lookup reports, not the registry's.
func TestReconcile_CleanupKeepsImageWhenRegistryDigestNeverMatches(t *testing.T) {
	r, _, removed := cleanupHarness(
		autoupdateContainer("sha256:local"),
		"sha256:local",
		"sha256:local",
		"sha256:published",
	)

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	if len(*removed) != 0 {
		t.Errorf("Expected no removals while the image is unchanged, got %v", *removed)
	}
}

func TestReconcile_CleanupDisabledKeepsImages(t *testing.T) {
	r, _, removed := cleanupHarness(
		autoupdateContainer("sha256:running"),
		"sha256:tagged",
		"sha256:pulled",
		"sha256:published",
	)
	r.Config.Docker.CleanupOldImages = false

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	if len(*removed) != 0 {
		t.Errorf("Expected no removals with cleanup disabled, got %v", *removed)
	}
}

// A swarm rollout is asynchronous and its previous image may still be needed by
// other tasks, so the rollback target must survive.
func TestReconcile_SwarmRolloutKeepsPreviousImage(t *testing.T) {
	r, mockDocker, removed := cleanupHarness(
		autoupdateContainer("sha256:running"),
		"sha256:tagged",
		"sha256:pulled",
		"sha256:published",
	)
	mockDocker.IsSwarmModeFunc = func(ctx context.Context) (bool, error) { return true, nil }
	mockDocker.GetServiceIDFunc = func(ctx context.Context, _ string) (string, error) { return "svc1", nil }
	var updated bool
	mockDocker.UpdateServiceFunc = func(ctx context.Context, _ string, _ docker.ServiceSpec) error {
		updated = true
		return nil
	}

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if !updated {
		t.Fatal("Expected a service update, got none")
	}
	if len(*removed) != 0 {
		t.Errorf("Expected no removals after a swarm rollout, got %v", *removed)
	}
}

// Cleanup is best-effort: a daemon refusal must not turn a successful update
// into a failed reconciliation.
func TestReconcile_RemovalFailureDoesNotFailUpdate(t *testing.T) {
	r, mockDocker, _ := cleanupHarness(
		autoupdateContainer("sha256:running"),
		"sha256:tagged",
		"sha256:pulled",
		"sha256:published",
	)
	var recreated bool
	mockDocker.RecreateContainerFunc = func(ctx context.Context, _ string, _ docker.ContainerSpec, _ string) error {
		recreated = true
		return nil
	}
	mockDocker.RemoveImageFunc = func(ctx context.Context, _ string) error {
		return errors.New("conflict: image is being used by container")
	}

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if !recreated {
		t.Error("Expected the container to be recreated, got none")
	}
}
