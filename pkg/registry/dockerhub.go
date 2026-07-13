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

// DockerHubProvider implements the Registry interface for Docker Hub
// (registry-1.docker.io) using the Docker Registry HTTP API v2.
//
// Public images require no credentials — the provider obtains an anonymous
// bearer token from auth.docker.io for registry API calls. For private
// repositories, auth is not yet supported.
//
// Supported image reference formats:
//   - postgres:18-alpine          → registry-1.docker.io/library/postgres:18-alpine
//   - library/postgres:18-alpine  → registry-1.docker.io/library/postgres:18-alpine
//   - myuser/myimage:tag          → registry-1.docker.io/myuser/myimage:tag
//   - docker.io/library/postgres:18-alpine → uses docker.io as host, mapped to registry-1.docker.io
type DockerHubProvider struct {
	httpClient  *http.Client
	mu          sync.Mutex
	cachedToken string
	tokenExpiry time.Time
	baseURL     string // Optional override for testing; empty means "https://<host>"
	authURL     string // Optional override for testing; empty means "https://auth.docker.io"
	cachedHost  string // Host cached from most recent GetLatestDigest call
}

// DockerHubConfig is the subset of config needed by the provider.
// Currently empty (public images need no config), but reserved for future
// private registry auth support.
type DockerHubConfig struct{}

// NewDockerHubProvider creates a new DockerHubProvider.
// httpClient may be nil (uses http.DefaultClient with 30s timeout).
func NewDockerHubProvider(httpClient *http.Client) *DockerHubProvider {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &DockerHubProvider{
		httpClient: httpClient,
	}
}

// setBaseURLForTest overrides the base URL used for registry requests.
// Used only in tests.
func (p *DockerHubProvider) setBaseURLForTest(u string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.baseURL = u
}

// setAuthURLForTest overrides the token endpoint URL used for auth.
// Used only in tests.
func (p *DockerHubProvider) setAuthURLForTest(u string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.authURL = u
}

// registryBase returns the base URL for registry API requests.
func (p *DockerHubProvider) registryBase(host string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.baseURL != "" {
		return p.baseURL
	}
	return "https://" + host
}

// --------------------------------------------------------------------------
// Registry interface implementation
// --------------------------------------------------------------------------

// InvalidateCache clears the cached anonymous token. Safe to call concurrently.
func (p *DockerHubProvider) InvalidateCache() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cachedToken = ""
	p.tokenExpiry = time.Time{}
}

// GetAuth returns Docker auth credentials. For Docker Hub public images,
// no credentials are required — the Docker daemon handles anonymous pulls.
func (p *DockerHubProvider) GetAuth(ctx context.Context) (Auth, error) {
	p.mu.Lock()
	host := p.cachedHost
	p.mu.Unlock()
	if host == "" {
		host = "registry-1.docker.io"
	}

	return Auth{
		RegistryHost: host,
	}, nil
}

// GetLatestDigest retrieves the digest of the image at the given tag.
//
// Uses the Docker Registry HTTP API v2:
//
//	HEAD https://registry-1.docker.io/v2/<path>/manifests/<ref>
//
// The response header Docker-Content-Digest contains the digest.
func (p *DockerHubProvider) GetLatestDigest(ctx context.Context, imageRef string, criteria Criteria) (string, error) {
	host, repoPath, ref, err := dockerHubParseRef(imageRef)
	if err != nil {
		return "", fmt.Errorf("failed to parse Docker Hub image reference: %w", err)
	}
	// Map docker.io to registry-1.docker.io for API access.
	if host == "docker.io" {
		host = "registry-1.docker.io"
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
		return p.getLatestDigestByRegex(ctx, host, repoPath, criteria.Regex)
	}

	if target == "" {
		target = "latest"
	}

	return p.headManifest(ctx, host, repoPath, target)
}

// getLatestDigestByRegex lists tags and returns the digest of the first matching tag
// (alphabetically sorted descending, so the highest semver-like tag wins — best effort).
func (p *DockerHubProvider) getLatestDigestByRegex(ctx context.Context, host, repoPath, pattern string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex %q: %w", pattern, err)
	}

	tags, err := p.listTags(ctx, host, repoPath)
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
		return "", fmt.Errorf("no tags matching %q found in %s/%s", pattern, host, repoPath)
	}

	sort.Strings(matching)
	// Sort descending — highest first.
	sort.Sort(sort.Reverse(sort.StringSlice(matching)))

	return p.headManifest(ctx, host, repoPath, matching[0])
}

// GetImageVersion returns the tag of the image reference.
func (p *DockerHubProvider) GetImageVersion(ctx context.Context, imageRef string) (string, error) {
	_, _, ref, err := dockerHubParseRef(imageRef)
	if err != nil {
		return "", fmt.Errorf("failed to parse Docker Hub image reference: %w", err)
	}
	if ref == "" {
		return "latest", nil
	}
	return ref, nil
}

// --------------------------------------------------------------------------
// Anonymous token management
// --------------------------------------------------------------------------

// token returns a valid anonymous bearer token for Docker Hub.
// Tokens are reused until 1 minute before expiry.
func (p *DockerHubProvider) token(ctx context.Context, repoPath string) (string, error) {
	p.mu.Lock()
	if p.cachedToken != "" && time.Now().Add(time.Minute).Before(p.tokenExpiry) {
		tok := p.cachedToken
		p.mu.Unlock()
		return tok, nil
	}
	// Release the lock for the HTTP call, but set a flag so only one
	// goroutine fetches. Double-check pattern.
	p.mu.Unlock()

	// Use a separate lock to serialize token fetches.
	return p.fetchToken(ctx, repoPath)
}

var dockerHubTokenMu sync.Mutex

func (p *DockerHubProvider) fetchToken(ctx context.Context, repoPath string) (string, error) {
	dockerHubTokenMu.Lock()
	defer dockerHubTokenMu.Unlock()

	// Double-check after acquiring the fetch lock.
	p.mu.Lock()
	if p.cachedToken != "" && time.Now().Add(time.Minute).Before(p.tokenExpiry) {
		tok := p.cachedToken
		p.mu.Unlock()
		return tok, nil
	}
	p.mu.Unlock()

	scope := fmt.Sprintf("repository:%s:pull", repoPath)

	authHost := "https://auth.docker.io"
	p.mu.Lock()
	if p.authURL != "" {
		authHost = p.authURL
	}
	p.mu.Unlock()

	url := fmt.Sprintf("%s/token?service=registry.docker.io&scope=%s", authHost, scope)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch Docker Hub token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned status %d", resp.StatusCode)
	}

	var result struct {
		Token     string `json:"token"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}

	if result.Token == "" {
		return "", fmt.Errorf("token endpoint returned empty token")
	}

	expiry := time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
	p.mu.Lock()
	p.cachedToken = result.Token
	p.tokenExpiry = expiry
	p.mu.Unlock()

	return result.Token, nil
}

// --------------------------------------------------------------------------
// Registry API helpers
// --------------------------------------------------------------------------

// headManifest performs a HEAD request against the manifest endpoint and returns
// the Docker-Content-Digest header value.
func (p *DockerHubProvider) headManifest(ctx context.Context, host, repoPath, ref string) (string, error) {
	tok, err := p.token(ctx, repoPath)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/v2/%s/manifests/%s", p.registryBase(host), repoPath, ref)
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
func (p *DockerHubProvider) listTags(ctx context.Context, host, repoPath string) ([]string, error) {
	tok, err := p.token(ctx, repoPath)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/v2/%s/tags/list", p.registryBase(host), repoPath)
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

// dockerHubParseRef parses a Docker Hub image reference.
//
// Examples:
//
//	postgres:18-alpine                    → ("registry-1.docker.io", "library/postgres", "18-alpine")
//	postgres                              → ("registry-1.docker.io", "library/postgres", "latest")
//	library/postgres:18-alpine            → ("registry-1.docker.io", "library/postgres", "18-alpine")
//	myuser/myimage:tag                    → ("registry-1.docker.io", "myuser/myimage", "tag")
//	docker.io/library/postgres:18-alpine  → ("docker.io", "library/postgres", "18-alpine")
//	docker.io/myuser/myimage:tag          → ("docker.io", "myuser/myimage", "tag")
func dockerHubParseRef(imageRef string) (host, repoPath, ref string, err error) {
	if imageRef == "" {
		return "", "", "", fmt.Errorf("empty image reference")
	}

	rest := imageRef

	// Check for explicit registry host (docker.io or index.docker.io).
	if strings.HasPrefix(rest, "docker.io/") {
		host = "docker.io"
		rest = rest[len("docker.io/"):]
	} else if strings.HasPrefix(rest, "index.docker.io/") {
		host = "docker.io"
		rest = rest[len("index.docker.io/"):]
	} else if strings.HasPrefix(rest, "registry-1.docker.io/") {
		host = "registry-1.docker.io"
		rest = rest[len("registry-1.docker.io/"):]
	} else if idx := strings.Index(rest, "/"); idx != -1 {
		// Could be "myuser/image" or "library/image" (no host prefix).
		// If the part before the first slash looks like a host (contains a dot or colon),
		// treat it as a host. Otherwise it's a Docker Hub path without explicit host.
		candidate := rest[:idx]
		if strings.Contains(candidate, ".") || strings.Contains(candidate, ":") {
			host = candidate
			rest = rest[idx+1:]
		} else {
			host = "registry-1.docker.io"
		}
	} else {
		// Single component — Docker Hub image without explicit namespace.
		host = "registry-1.docker.io"
	}

	// Now rest is the remainder after the host (e.g., "library/postgres:18-alpine"
	// or "postgres:18-alpine" or plain "postgres").
	// If it doesn't contain a "/", it's an official library image.
	if !strings.Contains(rest, "/") {
		rest = "library/" + rest
	}

	// Separate ref (tag or digest) from the rest.
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

	if repoPath == "" {
		return "", "", "", fmt.Errorf("invalid image reference %q: missing repository path", imageRef)
	}

	return host, repoPath, ref, nil
}
