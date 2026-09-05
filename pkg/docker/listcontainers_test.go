package docker

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
)

// notFoundError emulates daemon "No such image" errors: errdefs.IsNotFound
// matches on the NotFound() marker method.
type notFoundError struct{ msg string }

func (e notFoundError) Error() string { return e.msg }
func (e notFoundError) NotFound()     {}

// The list API decays Container.Image to a raw digest once the tag the
// container was created from moves. ListContainers must prefer the
// create-time reference so providers can parse it, and record the running
// digest for the reconciler's comparison.
func TestListContainers_PrefersCreateTimeRef(t *testing.T) {
	m := &mockDockerClient{}
	m.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]types.Container, error) {
		return []types.Container{
			{
				ID:     "c1",
				Image:  "sha256:oldrunningdigest",
				Labels: map[string]string{"dockenciler.autoupdate": "true"},
				State:  "running",
			},
		}, nil
	}
	m.ContainerInspectFunc = func(ctx context.Context, containerID string) (types.ContainerJSON, error) {
		return types.ContainerJSON{
			ContainerJSONBase: &types.ContainerJSONBase{Name: "/c1", Image: "sha256:oldrunningdigest"},
			Config:            &container.Config{Image: "ghcr.io/owner/repo:staging"},
		}, nil
	}

	d := &DockerClientImpl{client: m}
	containers, err := d.ListContainers(context.Background(), "dockenciler.autoupdate=true")
	if err != nil {
		t.Fatalf("ListContainers returned error: %v", err)
	}
	if len(containers) != 1 {
		t.Fatalf("Expected 1 container, got %d", len(containers))
	}
	if containers[0].Image != "ghcr.io/owner/repo:staging" {
		t.Errorf("Expected create-time ref, got %q", containers[0].Image)
	}
	if containers[0].ImageID != "sha256:oldrunningdigest" {
		t.Errorf("Expected running digest recorded, got %q", containers[0].ImageID)
	}
}

func TestListContainers_InspectFailureKeepsListValues(t *testing.T) {
	m := &mockDockerClient{}
	m.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]types.Container, error) {
		return []types.Container{
			{ID: "c1", Image: "ghcr.io/owner/repo:staging", State: "running"},
		}, nil
	}
	m.ContainerInspectFunc = func(ctx context.Context, containerID string) (types.ContainerJSON, error) {
		return types.ContainerJSON{}, fmt.Errorf("no such container")
	}

	d := &DockerClientImpl{client: m}
	containers, err := d.ListContainers(context.Background(), "dockenciler.autoupdate=true")
	if err != nil {
		t.Fatalf("ListContainers returned error: %v", err)
	}
	if len(containers) != 1 {
		t.Fatalf("Expected 1 container, got %d", len(containers))
	}
	if containers[0].Image != "ghcr.io/owner/repo:staging" {
		t.Errorf("Expected list value kept, got %q", containers[0].Image)
	}
	if containers[0].ImageID != "" {
		t.Errorf("Expected empty ImageID, got %q", containers[0].ImageID)
	}
}

func TestGetImageDigest_NotFoundSentinel(t *testing.T) {
	m := &mockDockerClient{}
	m.ImageInspectWithRawFunc = func(ctx context.Context, ref string) (types.ImageInspect, []byte, error) {
		return types.ImageInspect{}, nil, notFoundError{msg: "Error: No such image: " + ref}
	}

	d := &DockerClientImpl{client: m}
	digest, err := d.GetImageDigest(context.Background(), "ghcr.io/owner/repo:staging")
	if err == nil {
		t.Fatal("Expected error for missing image, got none")
	}
	if !errors.Is(err, ErrImageNotFound) {
		t.Fatalf("Expected ErrImageNotFound, got: %v", err)
	}
	if digest != "" {
		t.Errorf("Expected empty digest, got %q", digest)
	}
}
