package registry

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Router is a Registry that delegates to one single-registry provider per
// image, selected by the image's host. It lets one dockenciler instance watch
// mixed-registry hosts (e.g. GHCR app images alongside Docker Hub infra
// images) with each provider handling only its own references.
//
// Routing rules (first match wins):
//   - ghcr.io → GHCR provider
//   - docker.io, index.docker.io, registry-1.docker.io, or no host
//     (library/…, user/…) → Docker Hub provider
//   - hosts containing ".dkr.ecr." → ECR provider
//   - gcr.io, *.gcr.io, *.pkg.dev → GCR provider
//
// A nil provider means that registry type is not configured: images routing
// to it fail with ErrUnsupportedImage (the reconciler skips those), the same
// as images from an unknown host.
//
// Auth is registry-specific, so use GetAuthFor with the image being pulled.
// The legacy GetAuth delegates to the provider behind the most recent
// GetLatestDigest call — only correct under the reconciler's sequential
// per-container flow (digest → auth → pull); prefer GetAuthFor.
type Router struct {
	mu        sync.Mutex
	ghcr      *GHCRProvider
	dockerhub *DockerHubProvider
	ecr       *ECRProvider
	gcr       *GCRProvider
	last      Registry // provider behind the most recent digest lookup
}

// NewRouter creates a Router. Any provider may be nil (that registry type is
// then unconfigured and its images are skipped via ErrUnsupportedImage).
func NewRouter(ghcr *GHCRProvider, dockerhub *DockerHubProvider, ecr *ECRProvider, gcr *GCRProvider) *Router {
	return &Router{ghcr: ghcr, dockerhub: dockerhub, ecr: ecr, gcr: gcr}
}

// route selects the provider for an image reference.
func (r *Router) route(imageRef string) (Registry, error) {
	host := registryHost(imageRef)
	switch {
	case host == "ghcr.io":
		if r.ghcr == nil {
			return nil, fmt.Errorf("%w: no ghcr provider configured for %q", ErrUnsupportedImage, imageRef)
		}
		return r.ghcr, nil
	case host == "" || host == "docker.io" || host == "index.docker.io" || host == "registry-1.docker.io":
		if r.dockerhub == nil {
			return nil, fmt.Errorf("%w: no dockerhub provider configured for %q", ErrUnsupportedImage, imageRef)
		}
		return r.dockerhub, nil
	case strings.Contains(host, ".dkr.ecr."):
		if r.ecr == nil {
			return nil, fmt.Errorf("%w: no ecr provider configured for %q", ErrUnsupportedImage, imageRef)
		}
		return r.ecr, nil
	case host == "gcr.io" || strings.HasSuffix(host, ".gcr.io") || strings.HasSuffix(host, ".pkg.dev"):
		if r.gcr == nil {
			return nil, fmt.Errorf("%w: no gcr provider configured for %q", ErrUnsupportedImage, imageRef)
		}
		return r.gcr, nil
	default:
		return nil, fmt.Errorf("%w: no provider for host %q (%q)", ErrUnsupportedImage, host, imageRef)
	}
}

// registryHost extracts the registry host from an image reference, or ""
// when the reference carries no explicit host (a Docker Hub image).
func registryHost(imageRef string) string {
	rest := imageRef
	if idx := strings.Index(rest, "@"); idx != -1 {
		rest = rest[:idx]
	}
	// A tag colon only counts past the last slash; a colon in the first
	// segment means host:port, which still contains a host.
	firstSlash := strings.Index(rest, "/")
	if firstSlash == -1 {
		return ""
	}
	candidate := rest[:firstSlash]
	if strings.Contains(candidate, ".") || strings.Contains(candidate, ":") {
		return candidate
	}
	return ""
}

// GetLatestDigest routes to the image's provider and remembers it for GetAuth.
func (r *Router) GetLatestDigest(ctx context.Context, imageRef string, criteria Criteria) (string, error) {
	p, err := r.route(imageRef)
	if err != nil {
		return "", err
	}
	digest, err := p.GetLatestDigest(ctx, imageRef, criteria)
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	r.last = p
	r.mu.Unlock()
	return digest, nil
}

// GetImageVersion routes to the image's provider.
func (r *Router) GetImageVersion(ctx context.Context, imageRef string) (string, error) {
	p, err := r.route(imageRef)
	if err != nil {
		return "", err
	}
	return p.GetImageVersion(ctx, imageRef)
}

// GetAuthFor returns credentials from the image's provider. Prefer over
// GetAuth whenever the image being pulled is known.
func (r *Router) GetAuthFor(ctx context.Context, imageRef string) (Auth, error) {
	p, err := r.route(imageRef)
	if err != nil {
		return Auth{}, err
	}
	return p.GetAuth(ctx)
}

// GetAuth delegates to the provider behind the most recent GetLatestDigest
// call. Only correct under a sequential digest → auth → pull flow; prefer
// GetAuthFor.
func (r *Router) GetAuth(ctx context.Context) (Auth, error) {
	r.mu.Lock()
	p := r.last
	r.mu.Unlock()
	if p == nil {
		return Auth{}, fmt.Errorf("no registry provider selected yet")
	}
	return p.GetAuth(ctx)
}

// InvalidateCache clears every configured provider's cache.
func (r *Router) InvalidateCache() {
	if r.ghcr != nil {
		r.ghcr.InvalidateCache()
	}
	if r.dockerhub != nil {
		r.dockerhub.InvalidateCache()
	}
	if r.ecr != nil {
		r.ecr.InvalidateCache()
	}
	if r.gcr != nil {
		r.gcr.InvalidateCache()
	}
	r.mu.Lock()
	r.last = nil
	r.mu.Unlock()
}
