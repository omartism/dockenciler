package docker

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/errdefs"
	"github.com/stretchr/testify/require"
	"gotest.tools/v3/assert"
)

func TestRemoveImage(t *testing.T) {
	tests := []struct {
		name      string
		imageID   string
		removeErr error
		wantCalls int
		wantErr   bool
	}{
		{
			name:    "empty image id never reaches the daemon",
			imageID: "",
		},
		{
			name:      "removes the image by id",
			imageID:   "sha256:superseded",
			wantCalls: 1,
		},
		{
			name:      "an image the daemon already dropped is not an error",
			imageID:   "sha256:gone",
			removeErr: errdefs.NotFound(errors.New("No such image: sha256:gone")),
			wantCalls: 1,
		},
		{
			name:      "a daemon refusal is reported to the caller",
			imageID:   "sha256:shared",
			removeErr: errors.New("conflict: unable to delete sha256:shared (must be forced) - image is being used by container"),
			wantCalls: 1,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				calls   int
				options image.RemoveOptions
			)
			mockClient := &mockDockerClient{
				ImageRemoveFunc: func(_ context.Context, _ string, opts image.RemoveOptions) ([]image.DeleteResponse, error) {
					calls++
					options = opts
					return nil, tt.removeErr
				},
			}

			err := (&DockerClientImpl{client: mockClient}).RemoveImage(context.Background(), tt.imageID)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantCalls, calls)
			// Safety invariant: never force. The daemon refuses while any
			// container still references the image, and that refusal is what
			// keeps a shared image from being torn out from under it.
			assert.Equal(t, false, options.Force)
		})
	}
}
