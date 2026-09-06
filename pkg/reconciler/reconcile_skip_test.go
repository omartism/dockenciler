package reconciler

import (
	"context"
	"fmt"
	"testing"

	"github.com/omarismael/dockenciler/internal/testutil"
	"github.com/omarismael/dockenciler/pkg/docker"
	"github.com/omarismael/dockenciler/pkg/registry"
)

// Images from another registry are another instance's job: skip quietly,
// never fail, and never touch auth/pull/recreate.
func TestReconcile_ForeignImage_SkippedNotFailed(t *testing.T) {
	mockDocker := &testutil.MockDockerClient{}
	mockRegistry := &testutil.MockRegistry{}

	var authed, pulled, recreated bool
	mockDocker.ListContainersFunc = func(ctx context.Context, labelFilter string) ([]docker.Container, error) {
		return []docker.Container{{
			ID:     "c1",
			Image:  "traefik:v3",
			Labels: map[string]string{"dockenciler.autoupdate": "true"},
		}}, nil
	}
	mockDocker.GetImageDigestFunc = func(ctx context.Context, imageRef string) (string, error) {
		return "sha256:local", nil
	}
	mockRegistry.GetLatestDigestFunc = func(ctx context.Context, imageRef string, criteria registry.Criteria) (string, error) {
		return "", fmt.Errorf("parse: %w: %q", registry.ErrUnsupportedImage, imageRef)
	}
	mockRegistry.GetAuthFunc = func(ctx context.Context) (registry.Auth, error) {
		authed = true
		return registry.Auth{}, nil
	}
	mockDocker.PullImageFunc = func(ctx context.Context, imageRef string) error {
		pulled = true
		return nil
	}
	mockDocker.RecreateContainerFunc = func(ctx context.Context, id string, spec docker.ContainerSpec, newImage string) error {
		recreated = true
		return nil
	}

	if err := testReconciler(mockDocker, mockRegistry).Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if authed || pulled || recreated {
		t.Errorf("Expected no auth/pull/recreate for foreign image (auth=%v pull=%v recreate=%v)", authed, pulled, recreated)
	}
}
