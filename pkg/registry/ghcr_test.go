package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ghcrTestServer serves the GHCR token endpoint and Registry v2 API.
// Token requests are counted and the Authorization header recorded;
// manifest responses carry the given digest; tags/list returns the given tags.
type ghcrTestServer struct {
	server       *httptest.Server
	digest       string
	tags         []string
	tokenHits    atomic.Int64
	tokenAuth    []string
	manifestPath string
}

func newGHCRTestServer(digest string, tags []string) *ghcrTestServer {
	g := &ghcrTestServer{digest: digest, tags: tags}
	g.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/token") {
			g.tokenHits.Add(1)
			g.tokenAuth = append(g.tokenAuth, r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"token":      "fake-ghcr-token",
				"expires_in": 300,
			})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/tags/list") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"name": "owner/repo",
				"tags": g.tags,
			})
			return
		}
		if strings.Contains(r.URL.Path, "/manifests/") {
			g.manifestPath = r.URL.Path
			if r.Method != http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			if r.Header.Get("Authorization") != "Bearer fake-ghcr-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Docker-Content-Digest", g.digest)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	return g
}

func (g *ghcrTestServer) close() { g.server.Close() }

func (g *ghcrTestServer) provider(cfg GHCRConfig) *GHCRProvider {
	p := NewGHCRProvider(g.server.Client(), cfg)
	p.setBaseURLForTest(g.server.URL)
	p.setAuthURLForTest(g.server.URL + "/token")
	return p
}

// ---------------------------------------------------------------------------
// Reference parsing tests
// ---------------------------------------------------------------------------

func TestGHCRParseRef(t *testing.T) {
	tests := []struct {
		name     string
		ref      string
		host     string
		repoPath string
		tag      string
		wantErr  bool
	}{
		{"tagged", "ghcr.io/owner/repo:staging", "ghcr.io", "owner/repo", "staging", false},
		{"untagged defaults to latest", "ghcr.io/owner/repo", "ghcr.io", "owner/repo", "latest", false},
		{"digest", "ghcr.io/owner/repo@sha256:abc123", "ghcr.io", "owner/repo", "sha256:abc123", false},
		{"nested path", "ghcr.io/owner/nested/repo:v1.2.3", "ghcr.io", "owner/nested/repo", "v1.2.3", false},
		{"empty", "", "", "", "", true},
		{"missing host prefix", "owner/repo:tag", "", "", "", true},
		{"docker hub ref", "library/postgres:18-alpine", "", "", "", true},
		{"bare name", "postgres", "", "", "", true},
		{"missing repository path", "ghcr.io/owner", "", "", "", true},
		{"missing repository path with tag", "ghcr.io/owner:tag", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, repoPath, ref, err := ghcrParseRef(tt.ref)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Expected error for %q, got none", tt.ref)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error for %q: %v", tt.ref, err)
			}
			if host != tt.host || repoPath != tt.repoPath || ref != tt.tag {
				t.Errorf("ghcrParseRef(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tt.ref, host, repoPath, ref, tt.host, tt.repoPath, tt.tag)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// GetAuth tests
// ---------------------------------------------------------------------------

func TestGHCRProvider_GetAuth(t *testing.T) {
	p := NewGHCRProvider(nil, GHCRConfig{})

	auth, err := p.GetAuth(context.Background())
	if err != nil {
		t.Fatalf("GetAuth returned error: %v", err)
	}
	if auth.Username != "" || auth.Password != "" {
		t.Errorf("Expected empty credentials for anonymous access, got %q/%q", auth.Username, auth.Password)
	}
	if auth.RegistryHost != "ghcr.io" {
		t.Errorf("Expected default RegistryHost 'ghcr.io', got %q", auth.RegistryHost)
	}
}

func TestGHCRProvider_GetAuth_Authenticated(t *testing.T) {
	p := NewGHCRProvider(nil, GHCRConfig{Username: "octocat", Password: "ghp_test"})

	auth, err := p.GetAuth(context.Background())
	if err != nil {
		t.Fatalf("GetAuth returned error: %v", err)
	}
	if auth.Username != "octocat" || auth.Password != "ghp_test" {
		t.Errorf("Expected configured credentials, got %q/%q", auth.Username, auth.Password)
	}
	if auth.RegistryHost != "ghcr.io" {
		t.Errorf("Expected RegistryHost 'ghcr.io', got %q", auth.RegistryHost)
	}
}

func TestGHCRProvider_HasCredentials(t *testing.T) {
	if NewGHCRProvider(nil, GHCRConfig{}).HasCredentials() {
		t.Error("Expected HasCredentials=false for anonymous config")
	}
	if !NewGHCRProvider(nil, GHCRConfig{Username: "octocat", Password: "x"}).HasCredentials() {
		t.Error("Expected HasCredentials=true when username is set")
	}
}

// ---------------------------------------------------------------------------
// GetLatestDigest tests
// ---------------------------------------------------------------------------

func TestGHCRProvider_GetLatestDigest_ByTag(t *testing.T) {
	g := newGHCRTestServer("sha256:abc123", nil)
	defer g.close()
	p := g.provider(GHCRConfig{})

	digest, err := p.GetLatestDigest(context.Background(), "ghcr.io/owner/repo:staging", Criteria{})
	if err != nil {
		t.Fatalf("GetLatestDigest returned error: %v", err)
	}
	if digest != "sha256:abc123" {
		t.Errorf("Expected digest 'sha256:abc123', got %q", digest)
	}
	if want := "/v2/owner/repo/manifests/staging"; g.manifestPath != want {
		t.Errorf("Expected manifest path %q, got %q", want, g.manifestPath)
	}
}

func TestGHCRProvider_GetLatestDigest_TokenReuse(t *testing.T) {
	g := newGHCRTestServer("sha256:abc123", nil)
	defer g.close()
	p := g.provider(GHCRConfig{})

	for i := 0; i < 2; i++ {
		if _, err := p.GetLatestDigest(context.Background(), "ghcr.io/owner/repo:staging", Criteria{}); err != nil {
			t.Fatalf("GetLatestDigest returned error: %v", err)
		}
	}
	if got := g.tokenHits.Load(); got != 1 {
		t.Errorf("Expected 1 token request for 2 digest calls, got %d", got)
	}
}

func TestGHCRProvider_TokenBasicAuth(t *testing.T) {
	g := newGHCRTestServer("sha256:abc123", nil)
	defer g.close()
	p := g.provider(GHCRConfig{Username: "octocat", Password: "ghp_test"})

	if _, err := p.GetLatestDigest(context.Background(), "ghcr.io/owner/repo:staging", Criteria{}); err != nil {
		t.Fatalf("GetLatestDigest returned error: %v", err)
	}
	if len(g.tokenAuth) != 1 {
		t.Fatalf("Expected 1 token request, got %d", len(g.tokenAuth))
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("octocat:ghp_test"))
	if g.tokenAuth[0] != want {
		t.Errorf("Expected token request Basic auth, got %q", g.tokenAuth[0])
	}
}

func TestGHCRProvider_TokenAnonymous(t *testing.T) {
	g := newGHCRTestServer("sha256:abc123", nil)
	defer g.close()
	p := g.provider(GHCRConfig{})

	if _, err := p.GetLatestDigest(context.Background(), "ghcr.io/owner/repo:staging", Criteria{}); err != nil {
		t.Fatalf("GetLatestDigest returned error: %v", err)
	}
	if len(g.tokenAuth) != 1 {
		t.Fatalf("Expected 1 token request, got %d", len(g.tokenAuth))
	}
	if g.tokenAuth[0] != "" {
		t.Errorf("Expected no Authorization header on anonymous token request, got %q", g.tokenAuth[0])
	}
}

func TestGHCRProvider_GetLatestDigest_VersionCriteria(t *testing.T) {
	g := newGHCRTestServer("sha256:abc123", nil)
	defer g.close()
	p := g.provider(GHCRConfig{})

	digest, err := p.GetLatestDigest(context.Background(), "ghcr.io/owner/repo:old", Criteria{Version: "staging"})
	if err != nil {
		t.Fatalf("GetLatestDigest returned error: %v", err)
	}
	if digest != "sha256:abc123" {
		t.Errorf("Expected digest 'sha256:abc123', got %q", digest)
	}
	if want := "/v2/owner/repo/manifests/staging"; g.manifestPath != want {
		t.Errorf("Expected criteria version to win: path %q, got %q", want, g.manifestPath)
	}
}

func TestGHCRProvider_GetLatestDigest_DigestCriteria(t *testing.T) {
	g := newGHCRTestServer("sha256:abc123", nil)
	defer g.close()
	p := g.provider(GHCRConfig{})

	digest, err := p.GetLatestDigest(context.Background(), "ghcr.io/owner/repo:staging", Criteria{Digest: "sha256:pinned"})
	if err != nil {
		t.Fatalf("GetLatestDigest returned error: %v", err)
	}
	if digest != "sha256:pinned" {
		t.Errorf("Expected pinned digest, got %q", digest)
	}
	if got := g.tokenHits.Load(); got != 0 {
		t.Errorf("Expected no registry calls for digest criteria, got %d token hits", got)
	}
}

func TestGHCRProvider_GetLatestDigest_Regex(t *testing.T) {
	g := newGHCRTestServer("sha256:abc123", []string{"staging", "v1.2.3", "v1.2.4", "latest"})
	defer g.close()
	p := g.provider(GHCRConfig{})

	digest, err := p.GetLatestDigest(context.Background(), "ghcr.io/owner/repo:staging", Criteria{Regex: `^v\d+\.\d+\.\d+$`})
	if err != nil {
		t.Fatalf("GetLatestDigest returned error: %v", err)
	}
	if digest != "sha256:abc123" {
		t.Errorf("Expected digest 'sha256:abc123', got %q", digest)
	}
	if want := "/v2/owner/repo/manifests/v1.2.4"; g.manifestPath != want {
		t.Errorf("Expected highest matching tag: path %q, got %q", want, g.manifestPath)
	}
}

func TestGHCRProvider_GetLatestDigest_RegexNoMatch(t *testing.T) {
	g := newGHCRTestServer("sha256:abc123", []string{"staging", "latest"})
	defer g.close()
	p := g.provider(GHCRConfig{})

	if _, err := p.GetLatestDigest(context.Background(), "ghcr.io/owner/repo:staging", Criteria{Regex: `^v\d+`}); err == nil {
		t.Error("Expected error when no tags match, got none")
	}
}

func TestGHCRProvider_GetLatestDigest_InvalidRef(t *testing.T) {
	p := NewGHCRProvider(nil, GHCRConfig{})

	for _, ref := range []string{"", "owner/repo:tag", "postgres"} {
		if _, err := p.GetLatestDigest(context.Background(), ref, Criteria{}); err == nil {
			t.Errorf("Expected error for %q, got none", ref)
		}
	}
}

func TestGHCRProvider_GetLatestDigest_TokenFailure(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	p := NewGHCRProvider(server.Client(), GHCRConfig{})
	p.setBaseURLForTest(server.URL)
	p.setAuthURLForTest(server.URL + "/token")

	if _, err := p.GetLatestDigest(context.Background(), "ghcr.io/owner/repo:staging", Criteria{}); err == nil {
		t.Error("Expected error on token endpoint failure, got none")
	}
}

func TestGHCRProvider_GetLatestDigest_ManifestFailure(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/token") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"token": "t", "expires_in": 300})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	p := NewGHCRProvider(server.Client(), GHCRConfig{})
	p.setBaseURLForTest(server.URL)
	p.setAuthURLForTest(server.URL + "/token")

	if _, err := p.GetLatestDigest(context.Background(), "ghcr.io/owner/repo:staging", Criteria{}); err == nil {
		t.Error("Expected error on manifest failure, got none")
	}
}

func TestGHCRProvider_GetLatestDigest_MissingDigestHeader(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/token") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"token": "t", "expires_in": 300})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	p := NewGHCRProvider(server.Client(), GHCRConfig{})
	p.setBaseURLForTest(server.URL)
	p.setAuthURLForTest(server.URL + "/token")

	if _, err := p.GetLatestDigest(context.Background(), "ghcr.io/owner/repo:staging", Criteria{}); err == nil {
		t.Error("Expected error on missing digest header, got none")
	}
}

func TestGHCRProvider_GetLatestDigest_InvalidRegex(t *testing.T) {
	p := NewGHCRProvider(nil, GHCRConfig{})

	if _, err := p.GetLatestDigest(context.Background(), "ghcr.io/owner/repo:staging", Criteria{Regex: "["}); err == nil {
		t.Error("Expected error for invalid regex, got none")
	}
}

// ---------------------------------------------------------------------------
// GetImageVersion / InvalidateCache tests
// ---------------------------------------------------------------------------

func TestGHCRProvider_GetImageVersion(t *testing.T) {
	p := NewGHCRProvider(nil, GHCRConfig{})

	version, err := p.GetImageVersion(context.Background(), "ghcr.io/owner/repo:staging")
	if err != nil {
		t.Fatalf("GetImageVersion returned error: %v", err)
	}
	if version != "staging" {
		t.Errorf("Expected version 'staging', got %q", version)
	}

	version, err = p.GetImageVersion(context.Background(), "ghcr.io/owner/repo")
	if err != nil {
		t.Fatalf("GetImageVersion returned error: %v", err)
	}
	if version != "latest" {
		t.Errorf("Expected version 'latest', got %q", version)
	}

	if _, err := p.GetImageVersion(context.Background(), "owner/repo:tag"); err == nil {
		t.Error("Expected error for non-GHCR ref, got none")
	}
}

func TestGHCRProvider_InvalidateCache(t *testing.T) {
	g := newGHCRTestServer("sha256:abc123", nil)
	defer g.close()
	p := g.provider(GHCRConfig{})

	if _, err := p.GetLatestDigest(context.Background(), "ghcr.io/owner/repo:staging", Criteria{}); err != nil {
		t.Fatalf("GetLatestDigest returned error: %v", err)
	}
	p.InvalidateCache()

	if _, err := p.GetLatestDigest(context.Background(), "ghcr.io/owner/repo:staging", Criteria{}); err != nil {
		t.Fatalf("GetLatestDigest returned error: %v", err)
	}
	if got := g.tokenHits.Load(); got != 2 {
		t.Errorf("Expected token refetch after invalidate (2 hits), got %d", got)
	}
}

func TestGHCRProvider_InvalidateCache_ClearsEntries(t *testing.T) {
	p := NewGHCRProvider(nil, GHCRConfig{})

	p.mu.Lock()
	p.tokenCache = map[string]tokenCacheEntry{
		"owner/repo": {token: "some-token", expiry: time.Now().Add(1 * time.Hour)},
	}
	p.mu.Unlock()

	p.InvalidateCache()

	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.tokenCache) != 0 {
		t.Errorf("Expected empty token cache after invalidate, got %d entries", len(p.tokenCache))
	}
	if p.cachedHost != "" {
		t.Errorf("Expected empty cached host after invalidate, got %q", p.cachedHost)
	}
}

// ---------------------------------------------------------------------------
// Docker Hub guard test: foreign hosts must fail fast with a ghcr hint
// ---------------------------------------------------------------------------

func TestDockerHubProvider_RejectsGHCRImages(t *testing.T) {
	p := NewDockerHubProvider(nil, DockerHubConfig{})

	_, err := p.GetLatestDigest(context.Background(), "ghcr.io/owner/repo:staging", Criteria{})
	if err == nil {
		t.Fatal("Expected error for ghcr.io image under dockerhub type, got none")
	}
	if !strings.Contains(err.Error(), `"ghcr"`) {
		t.Errorf("Expected error to suggest the ghcr registry type, got: %v", err)
	}
}
