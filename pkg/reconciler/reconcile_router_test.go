package reconciler

import (
	"context"
	"testing"

	"github.com/omarismael/dockenciler/internal/testutil"
	"github.com/omarismael/dockenciler/pkg/docker"
	"github.com/omarismael/dockenciler/pkg/registry"
)

// authForRegistry wraps the shared mock with a per-image auth source, like
// the multi-registry Router's GetAuthFor.
type authForRegistry struct {
	*testutil.MockRegistry
	authFor     func(ctx context.Context, ref string) (registry.Auth, error)
	authForRef  string
	legacyCalls int
}

func (r *authForRegistry) GetAuthFor(ctx context.Context, ref string) (registry.Auth, error) {
	r.authForRef = ref
	return r.authFor(ctx, ref)
}

// The reconciler must pull per-image credentials from GetAuthFor when the
// registry provides it, so one instance can update across registries.
func TestReconcile_PrefersGetAuthForOverLegacyGetAuth(t *testing.T) {
	mockDocker := &testutil.MockDockerClient{}
	inner := &testutil.MockRegistry{}
	reg := &authForRegistry{MockRegistry: inner}
	reg.authFor = func(ctx context.Context, ref string) (registry.Auth, error) {
		return registry.Auth{Username: "router-user", Password: "router-pass", RegistryHost: "ghcr.io"}, nil
	}
	inner.GetAuthFunc = func(ctx context.Context) (registry.Auth, error) {
		reg.legacyCalls++
		return registry.Auth{}, nil
	}

	var authedUser string
	mockDocker.ListContainersFunc = func(ctx context.Context, labelFilter string) ([]docker.Container, error) {
		return []docker.Container{{
			ID:     "c1",
			Image:  "ghcr.io/owner/repo:staging",
			Labels: map[string]string{"dockenciler.autoupdate": "true"},
		}}, nil
	}
	mockDocker.GetImageDigestFunc = func(ctx context.Context, imageRef string) (string, error) {
		return "sha256:old", nil
	}
	inner.GetLatestDigestFunc = func(ctx context.Context, imageRef string, criteria registry.Criteria) (string, error) {
		return "sha256:new", nil
	}
	mockDocker.AuthenticateFunc = func(ctx context.Context, username, password, registryHost string) error {
		authedUser = username
		return nil
	}
	mockDocker.PullImageFunc = func(ctx context.Context, imageRef string) error { return nil }
	mockDocker.InspectContainerFunc = func(ctx context.Context, id string) (docker.ContainerSpec, error) {
		return docker.ContainerSpec{Name: id}, nil
	}
	mockDocker.RecreateContainerFunc = func(ctx context.Context, id string, spec docker.ContainerSpec, newImage string) error {
		return nil
	}

	r := testReconciler(mockDocker, nil)
	r.Registry = reg
	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if reg.authForRef != "ghcr.io/owner/repo:staging" {
		t.Errorf("Expected GetAuthFor with image ref, got %q", reg.authForRef)
	}
	if reg.legacyCalls != 0 {
		t.Errorf("Expected legacy GetAuth unused, called %d times", reg.legacyCalls)
	}
	if authedUser != "router-user" {
		t.Errorf("Expected daemon auth as router-user, got %q", authedUser)
	}
}
