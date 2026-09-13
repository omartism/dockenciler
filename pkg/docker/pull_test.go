package docker

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/docker/docker/api/types/image"
	"github.com/stretchr/testify/require"
)

// trackCloser records whether the pull body was fully consumed and closed.
type trackCloser struct {
	io.Reader
	closed *atomic.Bool
}

func (t *trackCloser) Close() error {
	t.closed.Store(true)
	return nil
}

func newDockerClientWithPull(fn func(context.Context, string, image.PullOptions) (io.ReadCloser, error)) *DockerClientImpl {
	return &DockerClientImpl{client: &mockDockerClient{ImagePullFunc: fn}}
}

func TestPullImage_DrainsStreamToCompletion(t *testing.T) {
	body := "{\"status\":\"Pulling from library/nginx\"}\n{\"status\":\"Digest: sha256:new\"}\n"
	var closed atomic.Bool
	src := strings.NewReader(body)
	mock := &mockDockerClient{
		ImagePullFunc: func(ctx context.Context, ref string, opts image.PullOptions) (io.ReadCloser, error) {
			if ref != "nginx:latest" {
				t.Errorf("Expected pull ref nginx:latest, got %q", ref)
			}
			return &trackCloser{Reader: src, closed: &closed}, nil
		},
	}
	d := &DockerClientImpl{client: mock}

	require.NoError(t, d.PullImage(context.Background(), "nginx:latest"))
	require.True(t, closed.Load(), "PullImage must close the pull stream")
	require.Zero(t, src.Len(), "PullImage must consume the pull stream so the daemon completes the pull")
}

func TestPullImage_SurfacesInlineStreamError(t *testing.T) {
	body := "{\"status\":\"Pulling\"}\n{\"errorDetail\":{\"message\":\"unauthorized: authentication required\"},\"error\":\"unauthorized: authentication required\"}\n"
	d := newDockerClientWithPull(func(ctx context.Context, ref string, opts image.PullOptions) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(body)), nil
	})

	err := d.PullImage(context.Background(), "nginx:latest")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestPullImage_PropagatesAPIError(t *testing.T) {
	sentinel := errors.New("connection refused")
	d := newDockerClientWithPull(func(ctx context.Context, ref string, opts image.PullOptions) (io.ReadCloser, error) {
		return nil, sentinel
	})

	require.ErrorIs(t, d.PullImage(context.Background(), "nginx:latest"), sentinel)
}

func TestPullImage_NilBodyIsNoOp(t *testing.T) {
	d := newDockerClientWithPull(func(ctx context.Context, ref string, opts image.PullOptions) (io.ReadCloser, error) {
		return nil, nil
	})

	require.NoError(t, d.PullImage(context.Background(), "nginx:latest"))
}
