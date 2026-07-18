package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// GetAuth tests
// ---------------------------------------------------------------------------

func TestDockerHubProvider_GetAuth(t *testing.T) {
	p := NewDockerHubProvider(nil, DockerHubConfig{})

	auth, err := p.GetAuth(context.Background())
	if err != nil {
		t.Fatalf("GetAuth returned error: %v", err)
	}
	if auth.Username != "" {
		t.Errorf("Expected empty username for anonymous access, got %q", auth.Username)
	}
	if auth.Password != "" {
		t.Errorf("Expected empty password for anonymous access, got %q", auth.Password)
	}
	// Default host when none has been cached via GetLatestDigest.
	if auth.RegistryHost != "registry-1.docker.io" {
		t.Errorf("Expected default RegistryHost 'registry-1.docker.io', got %q", auth.RegistryHost)
	}
}

func TestDockerHubProvider_GetAuth_WithHost(t *testing.T) {
	// Setup a server that handles both token and manifest requests.
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/token") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"token":      "fake-token",
				"expires_in": 300,
			})
			return
		}
		if strings.Contains(r.URL.Path, "/manifests/") {
			w.Header().Set("Docker-Content-Digest", "sha256:test")
			w.WriteHeader(http.StatusOK)
			return
		}
	}))
	defer server.Close()
	p := NewDockerHubProvider(server.Client(), DockerHubConfig{})
	p.setBaseURLForTest(server.URL)
	p.setAuthURLForTest(server.URL)

	// Simulate a GetLatestDigest call with a standard Docker Hub image.
	_, _ = p.GetLatestDigest(context.Background(), "myuser/myimage:tag", Criteria{})

	auth, err := p.GetAuth(context.Background())
	if err != nil {
		t.Fatalf("GetAuth returned error: %v", err)
	}
	if auth.RegistryHost != "registry-1.docker.io" {
		t.Errorf("Expected RegistryHost 'registry-1.docker.io', got %q", auth.RegistryHost)
	}
}

func TestDockerHubProvider_InvalidateCache(t *testing.T) {
	p := NewDockerHubProvider(nil, DockerHubConfig{})

	// Set a cached token manually.
	p.mu.Lock()
	p.tokenCache = map[string]tokenCacheEntry{
		"library/test": {token: "some-token", expiry: time.Now().Add(1 * time.Hour)},
	}
	p.mu.Unlock()

	p.InvalidateCache()

	p.mu.Lock()
	if len(p.tokenCache) != 0 {
		t.Errorf("Expected empty token cache after invalidate, got %d entries", len(p.tokenCache))
	}
	p.mu.Unlock()
}

// ---------------------------------------------------------------------------
// GetLatestDigest tests
// ---------------------------------------------------------------------------

func TestDockerHubProvider_GetLatestDigest_ByTag(t *testing.T) {
	expectedDigest := "sha256:abc123def456"
	var gotAuth, gotAccept, gotPath, gotMethod string

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Token endpoint.
		if strings.HasPrefix(r.URL.Path, "/token") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"token":      "fake-token",
				"expires_in": 300,
			})
			return
		}
		// Manifest endpoint.
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")

		w.Header().Set("Docker-Content-Digest", expectedDigest)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := NewDockerHubProvider(server.Client(), DockerHubConfig{})
	p.setBaseURLForTest(server.URL)
	p.setAuthURLForTest(server.URL)

	digest, err := p.GetLatestDigest(context.Background(), "postgres:18-alpine", Criteria{})
	if err != nil {
		t.Fatalf("GetLatestDigest: %v", err)
	}
	if digest != expectedDigest {
		t.Errorf("Expected digest %q, got %q", expectedDigest, digest)
	}
	if gotPath != "/v2/library/postgres/manifests/18-alpine" {
		t.Errorf("Expected path /v2/library/postgres/manifests/18-alpine, got %q", gotPath)
	}
	if gotMethod != http.MethodHead {
		t.Errorf("Expected HEAD method, got %s", gotMethod)
	}
	if gotAuth != "Bearer fake-token" {
		t.Errorf("Expected Authorization 'Bearer fake-token', got %q", gotAuth)
	}
	if !strings.Contains(gotAccept, "application/vnd.docker.distribution.manifest.v2+json") {
		t.Errorf("Expected Accept header to include manifest v2, got %q", gotAccept)
	}
}

func TestDockerHubProvider_GetLatestDigest_ByDigest(t *testing.T) {
	p := NewDockerHubProvider(nil, DockerHubConfig{})

	digest, err := p.GetLatestDigest(context.Background(), "postgres:18-alpine", Criteria{
		Digest: "sha256:direct-digest",
	})
	if err != nil {
		t.Fatalf("GetLatestDigest: %v", err)
	}
	if digest != "sha256:direct-digest" {
		t.Errorf("Expected digest 'sha256:direct-digest', got %q", digest)
	}
}

func TestDockerHubProvider_GetLatestDigest_ByVersion(t *testing.T) {
	expectedDigest := "sha256:version-match"

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/token") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"token":      "fake-token",
				"expires_in": 300,
			})
			return
		}
		if r.URL.Path == "/v2/library/postgres/manifests/16-alpine" {
			w.Header().Set("Docker-Content-Digest", expectedDigest)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := NewDockerHubProvider(server.Client(), DockerHubConfig{})
	p.setBaseURLForTest(server.URL)
	p.setAuthURLForTest(server.URL)

	digest, err := p.GetLatestDigest(context.Background(), "postgres:18-alpine", Criteria{
		Version: "16-alpine",
	})
	if err != nil {
		t.Fatalf("GetLatestDigest: %v", err)
	}
	if digest != expectedDigest {
		t.Errorf("Expected digest %q, got %q", expectedDigest, digest)
	}
}

func TestDockerHubProvider_GetLatestDigest_ByRegex(t *testing.T) {
	var reqCount int

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		switch {
		case strings.HasPrefix(r.URL.Path, "/token"):
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"token":      "fake-token",
				"expires_in": 300,
			})
			return
		case r.URL.Path == "/v2/library/postgres/tags/list" && r.Method == http.MethodGet:
			resp := struct {
				Name string   `json:"name"`
				Tags []string `json:"tags"`
			}{
				Name: "library/postgres",
				Tags: []string{"15-alpine", "16-alpine", "17-alpine", "18-alpine", "latest"},
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/v2/library/postgres/manifests/18-alpine" && r.Method == http.MethodHead:
			w.Header().Set("Docker-Content-Digest", "sha256:regex-match")
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	p := NewDockerHubProvider(server.Client(), DockerHubConfig{})
	p.setBaseURLForTest(server.URL)
	p.setAuthURLForTest(server.URL)

	digest, err := p.GetLatestDigest(context.Background(), "postgres:18-alpine", Criteria{
		Regex: `^\d+-alpine$`,
	})
	if err != nil {
		t.Fatalf("GetLatestDigest: %v", err)
	}
	if digest != "sha256:regex-match" {
		t.Errorf("Expected digest 'sha256:regex-match', got %q", digest)
	}
	if reqCount < 2 {
		t.Errorf("Expected at least 2 requests (token + tags list + manifest head), got %d", reqCount)
	}
}

func TestDockerHubProvider_GetLatestDigest_ByRegex_NoMatch(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/token") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"token":      "fake-token",
				"expires_in": 300,
			})
			return
		}
		if r.URL.Path == "/v2/library/postgres/tags/list" && r.Method == http.MethodGet {
			resp := struct {
				Name string   `json:"name"`
				Tags []string `json:"tags"`
			}{
				Name: "library/postgres",
				Tags: []string{"abc", "def"},
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(resp)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	p := NewDockerHubProvider(server.Client(), DockerHubConfig{})
	p.setBaseURLForTest(server.URL)
	p.setAuthURLForTest(server.URL)

	_, err := p.GetLatestDigest(context.Background(), "postgres:18-alpine", Criteria{
		Regex: `^v\d+\.\d+\.\d+$`,
	})
	if err == nil {
		t.Fatal("Expected error for no matching tags, got nil")
	}
	if !strings.Contains(err.Error(), "no tags matching") {
		t.Errorf("Expected error containing 'no tags matching', got %q", err.Error())
	}
}

func TestDockerHubProvider_GetLatestDigest_NotFound(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/token") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"token":      "fake-token",
				"expires_in": 300,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := NewDockerHubProvider(server.Client(), DockerHubConfig{})
	p.setBaseURLForTest(server.URL)
	p.setAuthURLForTest(server.URL)

	_, err := p.GetLatestDigest(context.Background(), "postgres:nonexistent", Criteria{})
	if err == nil {
		t.Fatal("Expected error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "registry returned status 404") {
		t.Errorf("Expected error containing 'registry returned status 404', got %q", err.Error())
	}
}

func TestDockerHubProvider_GetLatestDigest_MissingDigestHeader(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/token") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"token":      "fake-token",
				"expires_in": 300,
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := NewDockerHubProvider(server.Client(), DockerHubConfig{})
	p.setBaseURLForTest(server.URL)
	p.setAuthURLForTest(server.URL)

	_, err := p.GetLatestDigest(context.Background(), "postgres:18-alpine", Criteria{})
	if err == nil {
		t.Fatal("Expected error for missing digest header, got nil")
	}
	if !strings.Contains(err.Error(), "missing Docker-Content-Digest") {
		t.Errorf("Expected error containing 'missing Docker-Content-Digest', got %q", err.Error())
	}
}

func TestDockerHubProvider_GetLatestDigest_EmptyImageRef(t *testing.T) {
	p := NewDockerHubProvider(nil, DockerHubConfig{})

	_, err := p.GetLatestDigest(context.Background(), "", Criteria{})
	if err == nil {
		t.Fatal("Expected error for empty image reference, got nil")
	}
}

func TestDockerHubProvider_GetLatestDigest_NoTag(t *testing.T) {
	expectedDigest := "sha256:latest-digest"

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/token") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"token":      "fake-token",
				"expires_in": 300,
			})
			return
		}
		if r.URL.Path == "/v2/library/postgres/manifests/latest" {
			w.Header().Set("Docker-Content-Digest", expectedDigest)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := NewDockerHubProvider(server.Client(), DockerHubConfig{})
	p.setBaseURLForTest(server.URL)
	p.setAuthURLForTest(server.URL)

	digest, err := p.GetLatestDigest(context.Background(), "postgres", Criteria{})
	if err != nil {
		t.Fatalf("GetLatestDigest: %v", err)
	}
	if digest != expectedDigest {
		t.Errorf("Expected digest %q, got %q", expectedDigest, digest)
	}
}

// ---------------------------------------------------------------------------
// GetImageVersion tests
// ---------------------------------------------------------------------------

func TestDockerHubProvider_GetImageVersion(t *testing.T) {
	p := NewDockerHubProvider(nil, DockerHubConfig{})

	version, err := p.GetImageVersion(context.Background(), "postgres:18-alpine")
	if err != nil {
		t.Fatalf("GetImageVersion: %v", err)
	}
	if version != "18-alpine" {
		t.Errorf("Expected version '18-alpine', got %q", version)
	}
}

func TestDockerHubProvider_GetImageVersion_NoTag(t *testing.T) {
	p := NewDockerHubProvider(nil, DockerHubConfig{})

	version, err := p.GetImageVersion(context.Background(), "postgres")
	if err != nil {
		t.Fatalf("GetImageVersion: %v", err)
	}
	if version != "latest" {
		t.Errorf("Expected version 'latest', got %q", version)
	}
}

func TestDockerHubProvider_GetImageVersion_EmptyRef(t *testing.T) {
	p := NewDockerHubProvider(nil, DockerHubConfig{})

	_, err := p.GetImageVersion(context.Background(), "")
	if err == nil {
		t.Fatal("Expected error for empty image reference, got nil")
	}
}

func TestDockerHubProvider_GetImageVersion_WithDigest(t *testing.T) {
	p := NewDockerHubProvider(nil, DockerHubConfig{})

	version, err := p.GetImageVersion(context.Background(), "postgres@sha256:abcd1234")
	if err != nil {
		t.Fatalf("GetImageVersion: %v", err)
	}
	if version != "sha256:abcd1234" {
		t.Errorf("Expected version 'sha256:abcd1234', got %q", version)
	}
}

// ---------------------------------------------------------------------------
// dockerHubParseRef tests
// ---------------------------------------------------------------------------

func TestDockerHubParseRef(t *testing.T) {
	tests := []struct {
		name         string
		ref          string
		wantHost     string
		wantRepoPath string
		wantRef      string
		wantErr      bool
		errContains  string
	}{
		{
			name:         "simple tag",
			ref:          "postgres:18-alpine",
			wantHost:     "registry-1.docker.io",
			wantRepoPath: "library/postgres",
			wantRef:      "18-alpine",
		},
		{
			name:         "no tag defaults to latest",
			ref:          "postgres",
			wantHost:     "registry-1.docker.io",
			wantRepoPath: "library/postgres",
			wantRef:      "latest",
		},
		{
			name:         "library prefix",
			ref:          "library/postgres:18-alpine",
			wantHost:     "registry-1.docker.io",
			wantRepoPath: "library/postgres",
			wantRef:      "18-alpine",
		},
		{
			name:         "user image",
			ref:          "myuser/myimage:tag",
			wantHost:     "registry-1.docker.io",
			wantRepoPath: "myuser/myimage",
			wantRef:      "tag",
		},
		{
			name:         "explicit docker.io host",
			ref:          "docker.io/library/postgres:18-alpine",
			wantHost:     "docker.io",
			wantRepoPath: "library/postgres",
			wantRef:      "18-alpine",
		},
		{
			name:         "explicit docker.io host with user image",
			ref:          "docker.io/myuser/myimage:tag",
			wantHost:     "docker.io",
			wantRepoPath: "myuser/myimage",
			wantRef:      "tag",
		},
		{
			name:         "explicit index.docker.io host",
			ref:          "index.docker.io/library/postgres:18-alpine",
			wantHost:     "docker.io",
			wantRepoPath: "library/postgres",
			wantRef:      "18-alpine",
		},
		{
			name:         "explicit registry-1.docker.io host",
			ref:          "registry-1.docker.io/library/postgres:18-alpine",
			wantHost:     "registry-1.docker.io",
			wantRepoPath: "library/postgres",
			wantRef:      "18-alpine",
		},
		{
			name:         "digest pin",
			ref:          "postgres@sha256:abcd1234",
			wantHost:     "registry-1.docker.io",
			wantRepoPath: "library/postgres",
			wantRef:      "sha256:abcd1234",
		},
		{
			name:        "empty reference",
			ref:         "",
			wantErr:     true,
			errContains: "empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, repoPath, ref, err := dockerHubParseRef(tt.ref)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if host != tt.wantHost {
				t.Errorf("host: got %q, want %q", host, tt.wantHost)
			}
			if repoPath != tt.wantRepoPath {
				t.Errorf("repoPath: got %q, want %q", repoPath, tt.wantRepoPath)
			}
			if ref != tt.wantRef {
				t.Errorf("ref: got %q, want %q", ref, tt.wantRef)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Token caching concurrency test
// ---------------------------------------------------------------------------

func TestDockerHubProvider_TokenCachingConcurrent(t *testing.T) {
	var mu sync.Mutex
	var tokenFetchCount int

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/token") {
			mu.Lock()
			tokenFetchCount++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"token":      "concurrent-token",
				"expires_in": 300,
			})
			return
		}
		if strings.Contains(r.URL.Path, "/manifests/") {
			w.Header().Set("Docker-Content-Digest", "sha256:concurrent-test")
			w.WriteHeader(http.StatusOK)
			return
		}
	}))
	defer server.Close()

	p := NewDockerHubProvider(server.Client(), DockerHubConfig{})
	p.setBaseURLForTest(server.URL)
	p.setAuthURLForTest(server.URL)

	// Invalidate to ensure fresh fetch.
	p.InvalidateCache()

	const goroutines = 10
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.GetLatestDigest(context.Background(), "postgres:18-alpine", Criteria{})
			if err != nil {
				t.Errorf("concurrent GetLatestDigest: %v", err)
			}
		}()
	}
	wg.Wait()

	// Only one goroutine should have hit the token endpoint (others got cached token).
	mu.Lock()
	count := tokenFetchCount
	mu.Unlock()
	if count != 1 {
		t.Errorf("expected token endpoint to be called exactly once, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// Credential support tests
// ---------------------------------------------------------------------------

func TestDockerHubProvider_GetAuth_WithCredentials(t *testing.T) {
	p := NewDockerHubProvider(nil, DockerHubConfig{
		Username: "dockeruser",
		Password: "dockerpass",
	})

	auth, err := p.GetAuth(context.Background())
	if err != nil {
		t.Fatalf("GetAuth returned error: %v", err)
	}
	if auth.Username != "dockeruser" {
		t.Errorf("Expected username 'dockeruser', got %q", auth.Username)
	}
	if auth.Password != "dockerpass" {
		t.Errorf("Expected password 'dockerpass', got %q", auth.Password)
	}
	if auth.RegistryHost != "registry-1.docker.io" {
		t.Errorf("Expected default RegistryHost 'registry-1.docker.io', got %q", auth.RegistryHost)
	}
}

func TestDockerHubProvider_fetchToken_Authenticated(t *testing.T) {
	var gotBasicAuth bool

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/token") {
			// Check that Basic auth header was sent.
			user, pass, ok := r.BasicAuth()
			if !ok {
				t.Error("Expected Basic auth header on token request")
			} else {
				gotBasicAuth = true
				if user != "myuser" {
					t.Errorf("Expected Basic auth user 'myuser', got %q", user)
				}
				if pass != "mypat" {
					t.Errorf("Expected Basic auth password 'mypat', got %q", pass)
				}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"token":      "authenticated-token",
				"expires_in": 300,
			})
			return
		}
		if strings.Contains(r.URL.Path, "/manifests/") {
			w.Header().Set("Docker-Content-Digest", "sha256:auth-test")
			w.WriteHeader(http.StatusOK)
			return
		}
	}))
	defer server.Close()

	p := NewDockerHubProvider(server.Client(), DockerHubConfig{
		Username: "myuser",
		Password: "mypat",
	})
	p.setBaseURLForTest(server.URL)
	p.setAuthURLForTest(server.URL)

	// Trigger a token fetch via GetLatestDigest.
	digest, err := p.GetLatestDigest(context.Background(), "myuser/myimage:tag", Criteria{})
	if err != nil {
		t.Fatalf("GetLatestDigest: %v", err)
	}
	if digest != "sha256:auth-test" {
		t.Errorf("Expected digest 'sha256:auth-test', got %q", digest)
	}
	if !gotBasicAuth {
		t.Error("Token request did not include Basic auth")
	}
}

// Ensure anonymous fetchToken still works when no credentials are configured.
func TestDockerHubProvider_fetchToken_Anonymous(t *testing.T) {
	var gotBasicAuth bool

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/token") {
			_, _, ok := r.BasicAuth()
			gotBasicAuth = ok
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"token":      "anonymous-token",
				"expires_in": 300,
			})
			return
		}
		if strings.Contains(r.URL.Path, "/manifests/") {
			w.Header().Set("Docker-Content-Digest", "sha256:anon-test")
			w.WriteHeader(http.StatusOK)
			return
		}
	}))
	defer server.Close()

	p := NewDockerHubProvider(server.Client(), DockerHubConfig{})
	p.setBaseURLForTest(server.URL)
	p.setAuthURLForTest(server.URL)

	_, err := p.GetLatestDigest(context.Background(), "postgres:18-alpine", Criteria{})
	if err != nil {
		t.Fatalf("GetLatestDigest: %v", err)
	}
	if gotBasicAuth {
		t.Error("Anonymous fetchToken should not send Basic auth")
	}
}

// ---------------------------------------------------------------------------
// Docker CLI config.json credential resolution tests
// ---------------------------------------------------------------------------

func TestReadDockerConfigAuth(t *testing.T) {
	tests := []struct {
		name          string
		setupFile     func(t *testing.T) string // returns file path, cleans up temp dir
		wantUsername  string
		wantPassword  string
		wantErr       bool
		errContains   string
	}{
		{
			name: "valid config with index.docker.io host",
			setupFile: func(t *testing.T) string {
				dir := t.TempDir()
				auth := base64.StdEncoding.EncodeToString([]byte("myuser:mypass"))
				data := `{"auths":{"https://index.docker.io/v1/":{"auth":"` + auth + `"}}}`
				path := filepath.Join(dir, "config.json")
				if err := os.WriteFile(path, []byte(data), 0644); err != nil {
					t.Fatal(err)
				}
				return path
			},
			wantUsername: "myuser",
			wantPassword: "mypass",
		},
		{
			name: "valid config with registry-1.docker.io host",
			setupFile: func(t *testing.T) string {
				dir := t.TempDir()
				auth := base64.StdEncoding.EncodeToString([]byte("dockeruser:dockerpat"))
				data := `{"auths":{"registry-1.docker.io":{"auth":"` + auth + `"}}}`
				path := filepath.Join(dir, "config.json")
				if err := os.WriteFile(path, []byte(data), 0644); err != nil {
					t.Fatal(err)
				}
				return path
			},
			wantUsername: "dockeruser",
			wantPassword: "dockerpat",
		},
		{
			name: "empty auths map",
			setupFile: func(t *testing.T) string {
				dir := t.TempDir()
				data := `{"auths":{}}`
				path := filepath.Join(dir, "config.json")
				if err := os.WriteFile(path, []byte(data), 0644); err != nil {
					t.Fatal(err)
				}
				return path
			},
			wantErr:     true,
			errContains: "no Docker Hub credentials found",
		},
		{
			name: "non-existent file",
			setupFile: func(t *testing.T) string {
				return "/nonexistent/config.json"
			},
			wantErr:     true,
			errContains: "could not read docker config",
		},
		{
			name: "malformed JSON",
			setupFile: func(t *testing.T) string {
				dir := t.TempDir()
				data := `{invalid json`
				path := filepath.Join(dir, "config.json")
				if err := os.WriteFile(path, []byte(data), 0644); err != nil {
					t.Fatal(err)
				}
				return path
			},
			wantErr:     true,
			errContains: "could not parse docker config",
		},
		{
			name: "auth field with invalid base64",
			setupFile: func(t *testing.T) string {
				dir := t.TempDir()
				data := `{"auths":{"https://index.docker.io/v1/":{"auth":"not-base64!!!"}}}`
				path := filepath.Join(dir, "config.json")
				if err := os.WriteFile(path, []byte(data), 0644); err != nil {
					t.Fatal(err)
				}
				return path
			},
			wantErr:     true,
			errContains: "no Docker Hub credentials found",
		},
		{
			name: "auth field with no colon separator",
			setupFile: func(t *testing.T) string {
				dir := t.TempDir()
				auth := base64.StdEncoding.EncodeToString([]byte("usernameonly"))
				data := `{"auths":{"https://index.docker.io/v1/":{"auth":"` + auth + `"}}}`
				path := filepath.Join(dir, "config.json")
				if err := os.WriteFile(path, []byte(data), 0644); err != nil {
					t.Fatal(err)
				}
				return path
			},
			wantErr:     true,
			errContains: "no Docker Hub credentials found",
		},
		{
			name: "one valid and one invalid entry returns valid",
			setupFile: func(t *testing.T) string {
				dir := t.TempDir()
				validAuth := base64.StdEncoding.EncodeToString([]byte("gooduser:goodpass"))
				invalidAuth := base64.StdEncoding.EncodeToString([]byte("baduser"))
				data := `{"auths":{"https://index.docker.io/v1/":{"auth":"` + validAuth + `"},"other.registry.io":{"auth":"` + invalidAuth + `"}}}`
				path := filepath.Join(dir, "config.json")
				if err := os.WriteFile(path, []byte(data), 0644); err != nil {
					t.Fatal(err)
				}
				return path
			},
			wantUsername: "gooduser",
			wantPassword: "goodpass",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.setupFile(t)
			username, password, err := readDockerConfigAuth(path)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if username != tt.wantUsername {
				t.Errorf("username: got %q, want %q", username, tt.wantUsername)
			}
			if password != tt.wantPassword {
				t.Errorf("password: got %q, want %q", password, tt.wantPassword)
			}
		})
	}
}

func TestDockerHubProvider_ResolveCredentialsFromFile(t *testing.T) {
	dir := t.TempDir()
	auth := base64.StdEncoding.EncodeToString([]byte("fileuser:filepass"))
	data := `{"auths":{"https://index.docker.io/v1/":{"auth":"` + auth + `"}}}`
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	p := NewDockerHubProvider(nil, DockerHubConfig{
		ConfigPath: configPath,
	})

	authInfo, err := p.GetAuth(context.Background())
	if err != nil {
		t.Fatalf("GetAuth returned error: %v", err)
	}
	if authInfo.Username != "fileuser" {
		t.Errorf("Expected username 'fileuser' resolved from config file, got %q", authInfo.Username)
	}
	if authInfo.Password != "filepass" {
		t.Errorf("Expected password 'filepass' resolved from config file, got %q", authInfo.Password)
	}
}

func TestDockerHubProvider_ResolveCredentialsExplicitTakesPriority(t *testing.T) {
	dir := t.TempDir()
	auth := base64.StdEncoding.EncodeToString([]byte("fileuser:filepass"))
	data := `{"auths":{"https://index.docker.io/v1/":{"auth":"` + auth + `"}}}`
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	// Explicit username/password should take priority over config file.
	p := NewDockerHubProvider(nil, DockerHubConfig{
		Username:   "explicituser",
		Password:   "explicitpass",
		ConfigPath: configPath,
	})

	authInfo, err := p.GetAuth(context.Background())
	if err != nil {
		t.Fatalf("GetAuth returned error: %v", err)
	}
	if authInfo.Username != "explicituser" {
		t.Errorf("Expected username 'explicituser' from explicit config, got %q", authInfo.Username)
	}
	if authInfo.Password != "explicitpass" {
		t.Errorf("Expected password 'explicitpass' from explicit config, got %q", authInfo.Password)
	}
}

func TestDockerHubProvider_ResolveCredentials_UnreadableFileFallsBack(t *testing.T) {
	// When ConfigPath points to a non-existent file and no credentials are set,
	// the provider should silently fall through to anonymous access.
	p := NewDockerHubProvider(nil, DockerHubConfig{
		ConfigPath: "/nonexistent/config.json",
	})

	authInfo, err := p.GetAuth(context.Background())
	if err != nil {
		t.Fatalf("GetAuth returned error: %v", err)
	}
	if authInfo.Username != "" {
		t.Errorf("Expected empty username for fallback, got %q", authInfo.Username)
	}
	if authInfo.Password != "" {
		t.Errorf("Expected empty password for fallback, got %q", authInfo.Password)
	}
}
