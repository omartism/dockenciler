package reconciler

import (
	"context"
	"testing"

	"github.com/omarismael/dockenciler/internal/testutil"
	"github.com/omarismael/dockenciler/pkg/docker"
	"github.com/omarismael/dockenciler/pkg/registry"
	"github.com/stretchr/testify/require"
)

// A digest-pinned container must be recreated with the freshly pulled tag,
// not the stale create-time pin, or the update resurrects the old image.
func TestReconcile_PinnedContainer_RecreatesWithFreshTag(t *testing.T) {
	mockDocker := &testutil.MockDockerClient{}
	mockRegistry := &testutil.MockRegistry{}
	stubUpdatePath(mockDocker, mockRegistry)

	mockDocker.ListContainersFunc = func(ctx context.Context, labelFilter string) ([]docker.Container, error) {
		return []docker.Container{{
			ID:      "c1",
			Image:   "nginx@sha256:oldpin",
			ImageID: "sha256:oldrunning",
			Labels:  map[string]string{"dockenciler.autoupdate": "true"},
		}}, nil
	}
	mockDocker.GetImageDigestFunc = func(ctx context.Context, imageRef string) (string, error) {
		return "sha256:oldrunning", nil
	}
	mockRegistry.GetLatestDigestFunc = func(ctx context.Context, imageRef string, criteria registry.Criteria) (string, error) {
		return "sha256:newdigest", nil
	}
	var pulledRef, recreatedRef string
	mockDocker.PullImageFunc = func(ctx context.Context, imageRef string) error {
		pulledRef = imageRef
		return nil
	}
	mockDocker.RecreateContainerFunc = func(ctx context.Context, id string, spec docker.ContainerSpec, newImage string) error {
		recreatedRef = newImage
		return nil
	}

	require.NoError(t, testReconciler(mockDocker, mockRegistry).Reconcile(context.Background()))
	require.Equal(t, "nginx", pulledRef)
	require.Equal(t, "nginx", recreatedRef, "recreate must use the freshly pulled tag, not the stale digest pin")
}
