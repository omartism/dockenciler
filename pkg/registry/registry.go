package registry

import "context"

type Criteria struct {
    Version string
    Regex   string
    Digest  string
}

type Registry interface {
	GetLatestDigest(ctx context.Context, imageRef string, criteria Criteria) (string, error)
	GetImageVersion(ctx context.Context, imageRef string) (string, error)
	GetAuthToken(ctx context.Context) (string, string, error) // returns registry URL, token, error
}
