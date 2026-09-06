package registry

import (
	"context"
	"errors"
	"testing"
)

func testRouter() *Router {
	return NewRouter(
		NewGHCRProvider(nil, GHCRConfig{Username: "gh-user", Password: "gh-pat"}),
		NewDockerHubProvider(nil, DockerHubConfig{Username: "hub-user", Password: "hub-pat"}),
		NewECRProvider(&mockECRClient{}),
		newGCRProviderForTest(nil, newFakeTokenSource()),
	)
}

func TestRouter_RoutesByHost(t *testing.T) {
	r := testRouter()
	tests := []struct {
		ref      string
		username string
	}{
		{"ghcr.io/owner/repo:staging", "gh-user"},
		{"ghcr.io/owner/repo@sha256:abc", "gh-user"},
		{"postgres:18-alpine", "hub-user"},
		{"library/redis:7-alpine", "hub-user"},
		{"myuser/myimage:tag", "hub-user"},
		{"docker.io/library/redis:7-alpine", "hub-user"},
		{"registry-1.docker.io/library/redis:7-alpine", "hub-user"},
		{"gcr.io/proj/repo:v1", "oauth2accesstoken"},
		{"us-docker.pkg.dev/proj/repo:v1", "oauth2accesstoken"},
	}
	for _, tt := range tests {
		auth, err := r.GetAuthFor(context.Background(), tt.ref)
		if err != nil {
			t.Errorf("GetAuthFor(%q) returned error: %v", tt.ref, err)
			continue
		}
		if auth.Username != tt.username {
			t.Errorf("GetAuthFor(%q) username = %q, want %q", tt.ref, auth.Username, tt.username)
		}
	}
}

func TestRouter_ECRRouteReachesECR(t *testing.T) {
	r := testRouter()
	_, err := r.GetLatestDigest(context.Background(), "123456789012.dkr.ecr.us-east-1.amazonaws.com/myrepo:tag", Criteria{})
	if err == nil {
		t.Fatal("Expected ECR mock error, got none")
	}
	if errors.Is(err, ErrUnsupportedImage) {
		t.Fatalf("Expected routing to ECR provider, got unsupported: %v", err)
	}
}

func TestRouter_UnknownHostUnsupported(t *testing.T) {
	r := testRouter()
	for _, ref := range []string{"quay.io/org/img:v1", "localhost:5000/img:tag"} {
		if _, err := r.GetLatestDigest(context.Background(), ref, Criteria{}); !errors.Is(err, ErrUnsupportedImage) {
			t.Errorf("GetLatestDigest(%q): expected ErrUnsupportedImage, got %v", ref, err)
		}
		if _, err := r.GetAuthFor(context.Background(), ref); !errors.Is(err, ErrUnsupportedImage) {
			t.Errorf("GetAuthFor(%q): expected ErrUnsupportedImage, got %v", ref, err)
		}
	}
}

func TestRouter_MissingProviderUnsupported(t *testing.T) {
	r := NewRouter(nil, nil, nil, nil)
	for _, ref := range []string{"ghcr.io/o/r:t", "postgres:18", "gcr.io/p/r:v1"} {
		if _, err := r.GetLatestDigest(context.Background(), ref, Criteria{}); !errors.Is(err, ErrUnsupportedImage) {
			t.Errorf("GetLatestDigest(%q): expected ErrUnsupportedImage, got %v", ref, err)
		}
	}
}

func TestRouter_GetAuthWithoutLookupFails(t *testing.T) {
	r := testRouter()
	if _, err := r.GetAuth(context.Background()); err == nil {
		t.Error("Expected error from legacy GetAuth with no prior lookup, got none")
	}
}

func TestRouter_InvalidateCacheClearsAll(t *testing.T) {
	r := testRouter()
	r.InvalidateCache()
	if _, err := r.GetAuth(context.Background()); err == nil {
		t.Error("Expected error from legacy GetAuth after invalidate, got none")
	}
}

func TestRouter_GetImageVersionRoutes(t *testing.T) {
	r := testRouter()
	v, err := r.GetImageVersion(context.Background(), "ghcr.io/o/r:staging")
	if err != nil || v != "staging" {
		t.Errorf("GetImageVersion(ghcr) = %q, %v; want staging, nil", v, err)
	}
	v, err = r.GetImageVersion(context.Background(), "postgres:18-alpine")
	if err != nil || v != "18-alpine" {
		t.Errorf("GetImageVersion(hub) = %q, %v; want 18-alpine, nil", v, err)
	}
}

func TestRegistryHost(t *testing.T) {
	tests := []struct {
		ref  string
		host string
	}{
		{"postgres:18-alpine", ""},
		{"library/redis:7-alpine", ""},
		{"myuser/myimage:tag", ""},
		{"ghcr.io/owner/repo:staging", "ghcr.io"},
		{"ghcr.io/owner/repo@sha256:abc123", "ghcr.io"},
		{"docker.io/library/redis:7-alpine", "docker.io"},
		{"index.docker.io/library/redis:7-alpine", "index.docker.io"},
		{"registry-1.docker.io/library/redis:7-alpine", "registry-1.docker.io"},
		{"123456789012.dkr.ecr.us-east-1.amazonaws.com/myrepo:tag", "123456789012.dkr.ecr.us-east-1.amazonaws.com"},
		{"gcr.io/proj/repo:v1", "gcr.io"},
		{"us-docker.pkg.dev/proj/repo:v1", "us-docker.pkg.dev"},
		{"quay.io/org/img:v1", "quay.io"},
		{"localhost:5000/img:tag", "localhost:5000"},
	}
	for _, tt := range tests {
		if got := registryHost(tt.ref); got != tt.host {
			t.Errorf("registryHost(%q) = %q, want %q", tt.ref, got, tt.host)
		}
	}
}
