package registry

import "context"

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