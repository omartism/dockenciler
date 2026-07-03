package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// GCRProvider implements the Registry interface for Google Container Registry
// and Google Artifact Registry.
//
// Supported auth methods:
//   - "adc": Application Default Credentials (recommended).
//     Uses GOOGLE_APPLICATION_CREDENTIALS env var, gcloud ADC, GCE/GKE metadata server,
//     or Workload Identity in that priority order.
//   - "service_account": Service account JSON key file path.
//
// The provider talks to the Docker Registry HTTP API v2 (NOT the Artifact Registry API)
// because that works against gcr.io, *.gcr.io, and *.pkg.dev with a single code path.
type GCRProvider struct {
	httpClient  *http.Client
	tokenSource oauth2.TokenSource
	baseURL     string // Optional override for testing; empty means "https://<host>"
	mu          sync.Mutex
	cachedToken *oauth2.Token
	cachedHost  string
}

// GCRConfig is the subset of config needed by the provider.
type GCRConfig struct {
	AuthMethod        string // "adc" | "service_account"
	ServiceAccountFile string
}

// NewGCRProvider creates a new GCRProvider with the given configuration.
// httpClient may be nil (uses http.DefaultClient with 30s timeout).
func NewGCRProvider(ctx context.Context, cfg GCRConfig, httpClient *http.Client) (*GCRProvider, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	ts, err := buildTokenSource(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build GCR token source: %w", err)
	}

	return &GCRProvider{
		httpClient:  httpClient,
		tokenSource: ts,
		baseURL:     "",
	}, nil
}

// newGCRProviderForTest creates a GCRProvider with a custom token source.
// Used only in tests; the public NewGCRProvider builds a real token source
// from the config.
func newGCRProviderForTest(httpClient *http.Client, ts oauth2.TokenSource) *GCRProvider {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &GCRProvider{
		httpClient:  httpClient,
		tokenSource: ts,
	}
}

// setBaseURLForTest overrides the base URL used for registry requests.
// Used only in tests.
func (p *GCRProvider) setBaseURLForTest(u string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.baseURL = u
}

// registryBase returns the base URL for registry API requests to the given host.
// If a test override is set via setBaseURLForTest, that value is used instead.
func (p *GCRProvider) registryBase(host string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.baseURL != "" {
		return p.baseURL
	}
	return "https://" + host
}

// buildTokenSource constructs the oauth2.TokenSource for the configured method.
func buildTokenSource(ctx context.Context, cfg GCRConfig) (oauth2.TokenSource, error) {
	switch cfg.AuthMethod {
	case "", "adc":
		// Default to Application Default Credentials.
		// Scopes: cloud-platform read is enough for registry pulls.
		return google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
	case "service_account":
		if cfg.ServiceAccountFile == "" {
			return nil, fmt.Errorf("service_account_file is required when auth method is service_account")
		}
		// Validate and clean the path; reject path traversal.
		cleaned := filepath.Clean(cfg.ServiceAccountFile)
		if strings.Contains(cleaned, "..") {
			return nil, fmt.Errorf("invalid service_account_file path")
		}
		data, err := os.ReadFile(cleaned) // #nosec G304 -- path is validated above
		if err != nil {
			return nil, fmt.Errorf("failed to read service account file: %w", err)
		}
		creds, err := google.CredentialsFromJSON(ctx, data, "https://www.googleapis.com/auth/cloud-platform")
		if err != nil {
			return nil, fmt.Errorf("failed to parse service account JSON: %w", err)
		}
		return creds.TokenSource, nil
	default:
		return nil, fmt.Errorf("unsupported gcr auth method: %q (supported: adc, service_account)", cfg.AuthMethod)
	}
}

// InvalidateCache clears the cached token. Safe to call concurrently.
func (p *GCRProvider) InvalidateCache() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cachedToken = nil
	p.cachedHost = ""
}

// GetAuth returns Docker auth credentials for GCR. Username is always
// "oauth2accesstoken" and password is the OAuth access token. RegistryHost is
// derived from the most recent image reference, falling back to "gcr.io".
func (p *GCRProvider) GetAuth(ctx context.Context) (Auth, error) {
	tok, err := p.token(ctx)
	if err != nil {
		return Auth{}, fmt.Errorf("failed to get GCR access token: %w", err)
	}

	p.mu.Lock()
	host := p.cachedHost
	p.mu.Unlock()
	if host == "" {
		host = "gcr.io"
	}

	return Auth{
		Username:     "oauth2accesstoken",
		Password:     tok.AccessToken,
		RegistryHost: host,
	}, nil
}

// token returns a valid OAuth token, using a 5-minute buffer before expiry.
func (p *GCRProvider) token(ctx context.Context) (*oauth2.Token, error) {
	p.mu.Lock()
	cached := p.cachedToken
	p.mu.Unlock()

	if cached != nil && cached.Valid() && cached.Expiry.After(time.Now().Add(5*time.Minute)) {
		return cached, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock.
	if p.cachedToken != nil && p.cachedToken.Valid() && p.cachedToken.Expiry.After(time.Now().Add(5*time.Minute)) {
		return p.cachedToken, nil
	}

	tok, err := p.tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to refresh GCR token: %w", err)
	}
	p.cachedToken = tok
	return tok, nil
}

// rememberHost caches the registry host from the most recent image reference.
func (p *GCRProvider) rememberHost(host string) {
	p.mu.Lock()
	p.cachedHost = host
	p.mu.Unlock()
}

// GetLatestDigest retrieves the digest of the image at the given tag, or matches
// against criteria.
//
// For GCR/AR, we use the Docker Registry HTTP API v2:
//
//	HEAD https://<host>/v2/<projectPath>/manifests/<ref>
//	Accept: application/vnd.docker.distribution.manifest.v2+json,
//	        application/vnd.oci.image.manifest.v1+json,
//	        application/vnd.docker.distribution.manifest.list.v2+json,
//	        application/vnd.oci.image.index.v1+json
//
// The response header Docker-Content-Digest contains the digest.
func (p *GCRProvider) GetLatestDigest(ctx context.Context, imageRef string, criteria Criteria) (string, error) {
	host, projectPath, ref, err := gcrParseFullRef(imageRef)
	if err != nil {
		return "", fmt.Errorf("failed to parse GCR image reference: %w", err)
	}
	p.rememberHost(host)

	// If criteria specifies a tag, use it; otherwise use ref from imageRef.
	target := ref
	if criteria.Version != "" {
		target = criteria.Version
	} else if criteria.Regex != "" {
		// For regex matching, we need to enumerate tags first.
		return p.getLatestDigestByRegex(ctx, host, projectPath, criteria.Regex)
	} else if criteria.Digest != "" {
		// Already a digest, return as-is.
		return criteria.Digest, nil
	}

	if target == "" {
		target = "latest"
	}

	return p.headManifest(ctx, host, projectPath, target)
}

// getLatestDigestByRegex lists tags and returns the digest of the first matching tag
// (alphabetically sorted descending, so the highest semver wins — best effort).
func (p *GCRProvider) getLatestDigestByRegex(ctx context.Context, host, projectPath, pattern string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex %q: %w", pattern, err)
	}

	tags, err := p.listTags(ctx, host, projectPath)
	if err != nil {
		return "", err
	}

	// Filter matching tags.
	var matching []string
	for _, t := range tags {
		if re.MatchString(t) {
			matching = append(matching, t)
		}
	}
	if len(matching) == 0 {
		return "", fmt.Errorf("no tags matching %q found in %s/%s", pattern, host, projectPath)
	}

	// Sort descending (best-effort semver; falls back to lexicographic).
	sortStringsDesc(matching)

	return p.headManifest(ctx, host, projectPath, matching[0])
}

// GetImageVersion returns the tag of the image. For GCR, this is informational
// and returns the tag from the imageRef (GCR doesn't have a single "latest" concept
// like ECR's image pushed time).
func (p *GCRProvider) GetImageVersion(ctx context.Context, imageRef string) (string, error) {
	_, _, ref, err := gcrParseFullRef(imageRef)
	if err != nil {
		return "", fmt.Errorf("failed to parse GCR image reference: %w", err)
	}
	if ref == "" {
		return "latest", nil
	}
	return ref, nil
}

// listTags returns all tags for a repository via the Docker Registry v2 API.
func (p *GCRProvider) listTags(ctx context.Context, host, projectPath string) ([]string, error) {
	tok, err := p.token(ctx)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/v2/%s/tags/list", p.registryBase(host), projectPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list tags: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list tags: registry returned status %d", resp.StatusCode)
	}

	var result struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode tags response: %w", err)
	}
	return result.Tags, nil
}

// headManifest performs a HEAD request against the manifest endpoint and returns
// the Docker-Content-Digest header value.
func (p *GCRProvider) headManifest(ctx context.Context, host, projectPath, ref string) (string, error) {
	tok, err := p.token(ctx)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/v2/%s/manifests/%s", p.registryBase(host), projectPath, ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("Accept", strings.Join([]string{
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.index.v1+json",
	}, ", "))

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("head manifest: registry returned status %d", resp.StatusCode)
	}

	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return "", fmt.Errorf("registry response missing Docker-Content-Digest header")
	}
	return digest, nil
}

// gcrParseFullRef parses a GCR/AR image reference into its parts.
// Examples:
//
//	gcr.io/project/repo:tag                    → ("gcr.io", "project/repo", "tag")
//	gcr.io/project/repo@sha256:abc             → ("gcr.io", "project/repo", "sha256:abc")
//	us-docker.pkg.dev/proj/sub/repo:tag        → ("us-docker.pkg.dev", "proj/sub/repo", "tag")
//	us-docker.pkg.dev/proj/sub/repo@sha256:abc → ("us-docker.pkg.dev", "proj/sub/repo", "sha256:abc")
//	europe-docker.pkg.dev/p/r:v1               → ("europe-docker.pkg.dev", "p/r", "v1")
func gcrParseFullRef(imageRef string) (host, projectPath, ref string, err error) {
	if imageRef == "" {
		return "", "", "", fmt.Errorf("empty image reference")
	}

	// Strip digest or tag to find host/projectPath boundary.
	// The host is everything up to the first "/".
	firstSlash := strings.Index(imageRef, "/")
	if firstSlash == -1 {
		return "", "", "", fmt.Errorf("invalid image reference %q: missing registry host", imageRef)
	}
	host = imageRef[:firstSlash]
	rest := imageRef[firstSlash+1:]

	// Separate ref (tag or digest) from the rest.
	// Digest is "@sha256:..."; tag is ":tag".
	if atIdx := strings.Index(rest, "@"); atIdx != -1 {
		projectPath = rest[:atIdx]
		ref = rest[atIdx+1:]
	} else if colonIdx := strings.LastIndex(rest, ":"); colonIdx != -1 {
		projectPath = rest[:colonIdx]
		ref = rest[colonIdx+1:]
	} else {
		projectPath = rest
		ref = "latest"
	}

	if projectPath == "" {
		return "", "", "", fmt.Errorf("invalid image reference %q: missing repository path", imageRef)
	}

	return host, projectPath, ref, nil
}

// sortStringsDesc sorts a slice of strings in descending lexicographic order.
func sortStringsDesc(s []string) {
	sort.Slice(s, func(i, j int) bool { return s[i] > s[j] })
}
