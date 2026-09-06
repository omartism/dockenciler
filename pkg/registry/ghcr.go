package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// GHCRProvider implements the Registry interface for the GitHub Container
// Registry (ghcr.io) using the Docker Registry HTTP API v2.
//
// Public images can be accessed anonymously — the provider obtains a bearer
// token from ghcr.io/token for registry API calls. When credentials are
// configured (GitHub username + personal access token with read:packages
// scope), they are sent as HTTP Basic auth on the token request, enabling
// access to private repositories, and returned for Docker daemon pulls.
//
// Supported image reference formats:
//   - ghcr.io/owner/repo:tag            → ("ghcr.io", "owner/repo", "tag")
//   - ghcr.io/owner/repo                → ("ghcr.io", "owner/repo", "latest")
//   - ghcr.io/owner/repo@sha256:abc...  → ("ghcr.io", "owner/repo", "sha256:abc...")
type GHCRProvider struct {
	httpClient *http.Client
	cfg        GHCRConfig
	mu         sync.Mutex
	tokenCache map[string]tokenCacheEntry // keyed by repoPath (e.g. "owner/repo")
	baseURL    string                     // Optional override for testing; empty means "https://ghcr.io"
	authURL    string                     // Optional override for testing; empty means "https://ghcr.io/token"
	cachedHost string                     // Host cached from most recent GetLatestDigest call
}

// GHCRConfig is the subset of config needed by the provider.
// Username and Password are optional; when empty, anonymous access is used
// (public images only). For private images set Username to the GitHub
// username and Password to a personal access token with read:packages scope.
type GHCRConfig struct {
	Username string // GitHub username (leave empty for anonymous access)
	Password string // Personal access token with read:packages scope
}

// NewGHCRProvider creates a new GHCRProvider.
// httpClient may be nil (uses http.DefaultClient with 30s timeout).
// cfg may be zero-valued (GHCRConfig{}) for anonymous access.
func NewGHCRProvider(httpClient *http.Client, cfg GHCRConfig) *GHCRProvider {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &GHCRProvider{
		httpClient: httpClient,
		cfg:        cfg,
	}
}

// setBaseURLForTest overrides the base URL used for registry requests.
// Used only in tests.
func (p *GHCRProvider) setBaseURLForTest(u string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.baseURL = u
}

// setAuthURLForTest overrides the token endpoint URL used for auth.
// Used only in tests.
func (p *GHCRProvider) setAuthURLForTest(u string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.authURL = u
}

// registryBase returns the base URL for registry API requests.
// If a test override is set via setBaseURLForTest, that value is used instead.
func (p *GHCRProvider) registryBase() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.baseURL != "" {
		return p.baseURL
	}
	return "https://ghcr.io"
}

// --------------------------------------------------------------------------
// Registry interface implementation
// --------------------------------------------------------------------------

// InvalidateCache clears all cached bearer tokens. Safe to call concurrently.
func (p *GHCRProvider) InvalidateCache() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tokenCache = nil
	p.cachedHost = ""
}

// HasCredentials reports whether the provider has GHCR credentials configured.
func (p *GHCRProvider) HasCredentials() bool {
	return p.cfg.Username != ""
}

// GetAuth returns Docker auth credentials. When credentials are configured,
// they are returned for Docker daemon pulls. For anonymous access, empty
// credentials are returned and the Docker daemon handles anonymous pulls.
func (p *GHCRProvider) GetAuth(_ context.Context) (Auth, error) {
	p.mu.Lock()
	host := p.cachedHost
	p.mu.Unlock()
	if host == "" {
		host = "ghcr.io"
	}

	return Auth{
		RegistryHost: host,
		Username:     p.cfg.Username,
		Password:     p.cfg.Password,
	}, nil
}

// GetLatestDigest retrieves the digest of the image at the given tag.
//
// Uses the Docker Registry HTTP API v2:
//
//	HEAD https://ghcr.io/v2/<owner>/<repo>/manifests/<ref>
//
// The response header Docker-Content-Digest contains the digest.
func (p *GHCRProvider) GetLatestDigest(ctx context.Context, imageRef string, criteria Criteria) (string, error) {
	host, repoPath, ref, err := ghcrParseRef(imageRef)
	if err != nil {
		return "", fmt.Errorf("failed to parse GHCR image reference: %w", err)
	}
	p.mu.Lock()
	p.cachedHost = host
	p.mu.Unlock()

	target := ref
	if criteria.Digest != "" {
		return criteria.Digest, nil
	}
	if criteria.Version != "" {
		target = criteria.Version
	} else if criteria.Regex != "" {
		return p.getLatestDigestByRegex(ctx, repoPath, criteria.Regex)
	}

	if target == "" {
		target = "latest"
	}

	return p.headManifest(ctx, repoPath, target)
}

// getLatestDigestByRegex lists tags and returns the digest of the first matching tag
// (alphabetically sorted descending, so the highest semver-like tag wins — best effort).
func (p *GHCRProvider) getLatestDigestByRegex(ctx context.Context, repoPath, pattern string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex %q: %w", pattern, err)
	}

	tags, err := p.listTags(ctx, repoPath)
	if err != nil {
		return "", err
	}

	var matching []string
	for _, t := range tags {
		if re.MatchString(t) {
			matching = append(matching, t)
		}
	}
	if len(matching) == 0 {
		return "", fmt.Errorf("no tags matching %q found in ghcr.io/%s", pattern, repoPath)
	}

	sort.Strings(matching)
	// Sort descending — highest first.
	sort.Sort(sort.Reverse(sort.StringSlice(matching)))

	return p.headManifest(ctx, repoPath, matching[0])
}

// GetImageVersion returns the tag of the image reference.
func (p *GHCRProvider) GetImageVersion(_ context.Context, imageRef string) (string, error) {
	_, _, ref, err := ghcrParseRef(imageRef)
	if err != nil {
		return "", fmt.Errorf("failed to parse GHCR image reference: %w", err)
	}
	if ref == "" {
		return "latest", nil
	}
	return ref, nil
}

// --------------------------------------------------------------------------
// Token management
// --------------------------------------------------------------------------

// token returns a valid bearer token for the given GHCR repository.
// Tokens are cached per-repository and reused until 1 minute before expiry.
func (p *GHCRProvider) token(ctx context.Context, repoPath string) (string, error) {
	p.mu.Lock()
	if entry, ok := p.tokenCache[repoPath]; ok && entry.token != "" && time.Now().Add(time.Minute).Before(entry.expiry) {
		tok := entry.token
		p.mu.Unlock()
		return tok, nil
	}
	p.mu.Unlock()
	return p.fetchToken(ctx, repoPath)
}

var ghcrTokenMu sync.Mutex

// fetchToken obtains a bearer token from the GHCR token endpoint:
//
//	GET https://ghcr.io/token?service=ghcr.io&scope=repository:<repoPath>:pull
//
// Anonymous for public images; HTTP Basic auth (username + PAT) when
// credentials are configured, enabling access to private repositories.
func (p *GHCRProvider) fetchToken(ctx context.Context, repoPath string) (string, error) {
	ghcrTokenMu.Lock()
	defer ghcrTokenMu.Unlock()

	// Double-check after acquiring the fetch lock.
	p.mu.Lock()
	if entry, ok := p.tokenCache[repoPath]; ok && entry.token != "" && time.Now().Add(time.Minute).Before(entry.expiry) {
		p.mu.Unlock()
		return entry.token, nil
	}
	p.mu.Unlock()

	p.mu.Lock()
	authURL := p.authURL
	username := p.cfg.Username
	password := p.cfg.Password
	p.mu.Unlock()
	if authURL == "" {
		authURL = "https://ghcr.io/token"
	}

	scope := fmt.Sprintf("repository:%s:pull", repoPath)
	url := fmt.Sprintf("%s?service=ghcr.io&scope=%s", authURL, scope)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create GHCR token request: %w", err)
	}

	if username != "" {
		req.SetBasicAuth(username, password)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch GHCR token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GHCR token endpoint returned status %d", resp.StatusCode)
	}

	var result struct {
		Token     string `json:"token"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode GHCR token response: %w", err)
	}

	if result.Token == "" {
		return "", fmt.Errorf("GHCR token endpoint returned empty token")
	}

	expiry := time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
	p.mu.Lock()
	if p.tokenCache == nil {
		p.tokenCache = make(map[string]tokenCacheEntry)
	}
	p.tokenCache[repoPath] = tokenCacheEntry{token: result.Token, expiry: expiry}
	p.mu.Unlock()

	return result.Token, nil
}

// --------------------------------------------------------------------------
// Registry API helpers
// --------------------------------------------------------------------------

// headManifest performs a HEAD request against the manifest endpoint and returns
// the Docker-Content-Digest header value.
func (p *GHCRProvider) headManifest(ctx context.Context, repoPath, ref string) (string, error) {
	tok, err := p.token(ctx, repoPath)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/v2/%s/manifests/%s", p.registryBase(), repoPath, ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
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

// listTags returns all tags for a repository via the Docker Registry v2 API.
func (p *GHCRProvider) listTags(ctx context.Context, repoPath string) ([]string, error) {
	tok, err := p.token(ctx, repoPath)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/v2/%s/tags/list", p.registryBase(), repoPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)

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

// --------------------------------------------------------------------------
// Image reference parsing
// --------------------------------------------------------------------------

// ghcrParseRef parses a GHCR image reference.
//
// Examples:
//
//	ghcr.io/owner/repo:tag           → ("ghcr.io", "owner/repo", "tag")
//	ghcr.io/owner/repo               → ("ghcr.io", "owner/repo", "latest")
//	ghcr.io/owner/repo@sha256:abc... → ("ghcr.io", "owner/repo", "sha256:abc...")
func ghcrParseRef(imageRef string) (host, repoPath, ref string, err error) {
	if imageRef == "" {
		return "", "", "", fmt.Errorf("empty image reference")
	}

	rest, ok := strings.CutPrefix(imageRef, "ghcr.io/")
	if !ok {
		return "", "", "", fmt.Errorf("%w: %q is not a GHCR image (must start with ghcr.io/)", ErrUnsupportedImage, imageRef)
	}

	// Separate ref (tag or digest) from the rest.
	// Digest is "@sha256:..."; tag is ":tag" (split on the last colon,
	// which always follows the repository path).
	if atIdx := strings.Index(rest, "@"); atIdx != -1 {
		repoPath = rest[:atIdx]
		ref = rest[atIdx+1:]
	} else if colonIdx := strings.LastIndex(rest, ":"); colonIdx != -1 {
		repoPath = rest[:colonIdx]
		ref = rest[colonIdx+1:]
	} else {
		repoPath = rest
		ref = "latest"
	}

	if repoPath == "" || !strings.Contains(repoPath, "/") {
		return "", "", "", fmt.Errorf("invalid GHCR image reference %q: missing owner/repository path", imageRef)
	}

	return "ghcr.io", repoPath, ref, nil
}
