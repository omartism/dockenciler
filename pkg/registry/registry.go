package registry

import (
	"context"
	"errors"
)

// ErrUnsupportedImage marks an image reference that belongs to a different
// registry than the configured provider (e.g. a Docker Hub image checked by
// the GHCR provider). Providers wrap it with the offending host; the
// reconciler treats it as a skip, not a failure, so mixed-registry hosts
// watched by several single-registry instances stay quiet.
var ErrUnsupportedImage = errors.New("image not supported by this registry provider")

type Criteria struct {
	Version string
	Regex   string
	Digest  string
}

// Auth contains authentication information for a registry.
// Username is the Docker username (e.g., "oauth2accesstoken", "AWS", "_json_key").
// Password is the token, password, or key.
// RegistryHost is the registry hostname (e.g., "gcr.io", "12345.dkr.ecr.us-west-2.amazonaws.com").
// AuthHeader is an optional pre-encoded Authorization header value (e.g., "Bearer <token>")
// for registries that don't use Docker's basic auth scheme.
type Auth struct {
	Username     string
	Password     string
	RegistryHost string
	AuthHeader   string
}

type Registry interface {
	GetLatestDigest(ctx context.Context, imageRef string, criteria Criteria) (string, error)
	GetImageVersion(ctx context.Context, imageRef string) (string, error)
	GetAuth(ctx context.Context) (Auth, error)
	InvalidateCache()
}
