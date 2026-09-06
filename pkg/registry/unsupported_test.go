package registry

import (
	"context"
	"errors"
	"testing"
)

// Each single-registry provider must reject images from other registries with
// ErrUnsupportedImage (not a generic failure) so the reconciler can skip them
// quietly. This keeps mixed-registry hosts watched by several instances clean.
func TestProviders_RejectForeignImages(t *testing.T) {
	t.Run("ghcr", func(t *testing.T) {
		p := NewGHCRProvider(nil, GHCRConfig{})
		for _, ref := range []string{
			"traefik:v3",
			"postgres:18-alpine",
			"library/redis:7-alpine",
			"docker.io/library/redis:7-alpine",
		} {
			_, err := p.GetLatestDigest(context.Background(), ref, Criteria{})
			if !errors.Is(err, ErrUnsupportedImage) {
				t.Errorf("GetLatestDigest(%q): expected ErrUnsupportedImage, got %v", ref, err)
			}
		}
	})

	t.Run("dockerhub", func(t *testing.T) {
		p := NewDockerHubProvider(nil, DockerHubConfig{})
		for _, ref := range []string{
			"ghcr.io/owner/repo:staging",
			"quay.io/org/img:v1",
		} {
			_, err := p.GetLatestDigest(context.Background(), ref, Criteria{})
			if !errors.Is(err, ErrUnsupportedImage) {
				t.Errorf("GetLatestDigest(%q): expected ErrUnsupportedImage, got %v", ref, err)
			}
		}
	})

	// NOTE: the GCR provider intentionally accepts any hostname (custom
	// registries with GCR auth), so it has no foreign-host guard.
}
