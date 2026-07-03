package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// fakeTokenSource returns a static token. Implements oauth2.TokenSource.
type fakeTokenSource struct {
	token *oauth2.Token
	err   error
	callN int
}

func (f *fakeTokenSource) Token() (*oauth2.Token, error) {
	f.callN++
	if f.err != nil {
		return nil, f.err
	}
	return f.token, nil
}

// newFakeTokenSource returns a token source that yields a token valid for 1 hour.
func newFakeTokenSource() *fakeTokenSource {
	return &fakeTokenSource{
		token: &oauth2.Token{
			AccessToken: "fake-access-token",
			TokenType:   "Bearer",
			Expiry:      time.Now().Add(1 * time.Hour),
		},
	}
}

// ---------------------------------------------------------------------------
// GetAuth tests
// ---------------------------------------------------------------------------

// TestGCRProvider_GetAuth verifies that GetAuth returns the expected
// oauth2accesstoken username and the bearer access token as the password.
func TestGCRProvider_GetAuth(t *testing.T) {
	ts := newFakeTokenSource()
	p := newGCRProviderForTest(nil, ts)

	auth, err := p.GetAuth(context.Background())
	if err != nil {
		t.Fatalf("GetAuth returned error: %v", err)
	}
	if auth.Username != "oauth2accesstoken" {
		t.Errorf("Expected username 'oauth2accesstoken', got %q", auth.Username)
	}
	if auth.Password != "fake-access-token" {
		t.Errorf("Expected password 'fake-access-token', got %q", auth.Password)
	}
	if auth.RegistryHost == "" {
		t.Error("Expected non-empty RegistryHost")
	}
	if auth.RegistryHost != "gcr.io" {
		t.Errorf("Expected default RegistryHost 'gcr.io', got %q", auth.RegistryHost)
	}

	// First call should have hit the token source (5-min buffer check passes -> uses cached).
	// Actually, the token is 1 hour in future so it should use cached from the get-go.
	// Wait — the first call has no cache, so it will hit tokenSource once.
	if ts.callN != 1 {
		t.Errorf("Expected token source to be called once, was called %d times", ts.callN)
	}

	// Second call should use cached token (still valid).
	_, err = p.GetAuth(context.Background())
	if err != nil {
		t.Fatalf("GetAuth (cached) returned error: %v", err)
	}
	if ts.callN != 1 {
		t.Errorf("Expected token source to still be called once on cache hit, was called %d times", ts.callN)
	}
}

// TestGCRProvider_GetAuth_WithHost verifies that the host from a previous
// GetLatestDigest call is remembered by GetAuth.
func TestGCRProvider_GetAuth_WithHost(t *testing.T) {
	ts := newFakeTokenSource()
	p := newGCRProviderForTest(nil, ts)

	// Simulate remembering a host by calling GetLatestDigest against a TLS server.
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", "sha256:test")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p.setBaseURLForTest(server.URL)

	_, _ = p.GetLatestDigest(context.Background(), "my-custom-host.io/project/repo:v1.0.0", Criteria{})
	// The host should be "my-custom-host.io"
	auth, err := p.GetAuth(context.Background())
	if err != nil {
		t.Fatalf("GetAuth returned error: %v", err)
	}
	if auth.RegistryHost != "my-custom-host.io" {
		t.Errorf("Expected RegistryHost 'my-custom-host.io', got %q", auth.RegistryHost)
	}
}

// TestGCRProvider_InvalidateCache forces the next GetAuth to refresh the token.
func TestGCRProvider_InvalidateCache(t *testing.T) {
	ts := newFakeTokenSource()
	p := newGCRProviderForTest(nil, ts)

	if _, err := p.GetAuth(context.Background()); err != nil {
		t.Fatalf("GetAuth: %v", err)
	}
	if ts.callN != 1 {
		t.Fatalf("expected 1 token call before invalidate, got %d", ts.callN)
	}

	p.InvalidateCache()
	if _, err := p.GetAuth(context.Background()); err != nil {
		t.Fatalf("GetAuth after invalidate: %v", err)
	}
	if ts.callN != 2 {
		t.Errorf("Expected token source to be called twice after InvalidateCache, was called %d times", ts.callN)
	}
}

// TestGCRProvider_GetAuth_TokenSourceError propagates errors from the token source.
func TestGCRProvider_GetAuth_TokenSourceError(t *testing.T) {
	ts := &fakeTokenSource{
		err: fmt.Errorf("token source failure"),
	}
	p := newGCRProviderForTest(nil, ts)

	_, err := p.GetAuth(context.Background())
	if err == nil {
		t.Fatal("Expected error from GetAuth, got nil")
	}
	if !strings.Contains(err.Error(), "token source failure") {
		t.Errorf("Expected error to contain 'token source failure', got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// GetLatestDigest tests
// ---------------------------------------------------------------------------

// TestGCRProvider_GetLatestDigest_ByTag tests the HEAD manifest endpoint with a tag.
func TestGCRProvider_GetLatestDigest_ByTag(t *testing.T) {
	expectedDigest := "sha256:abc123def456"
	var gotAuth, gotAccept, gotPath, gotMethod string

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")

		w.Header().Set("Docker-Content-Digest", expectedDigest)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := newGCRProviderForTest(server.Client(), newFakeTokenSource())
	p.setBaseURLForTest(server.URL)

	digest, err := p.GetLatestDigest(context.Background(), "gcr.io/my-project/my-repo:v1.0.0", Criteria{})
	if err != nil {
		t.Fatalf("GetLatestDigest: %v", err)
	}
	if digest != expectedDigest {
		t.Errorf("Expected digest %q, got %q", expectedDigest, digest)
	}
	if gotPath != "/v2/my-project/my-repo/manifests/v1.0.0" {
		t.Errorf("Expected path /v2/my-project/my-repo/manifests/v1.0.0, got %q", gotPath)
	}
	if gotMethod != http.MethodHead {
		t.Errorf("Expected HEAD method, got %s", gotMethod)
	}
	if gotAuth != "Bearer fake-access-token" {
		t.Errorf("Expected Authorization 'Bearer fake-access-token', got %q", gotAuth)
	}
	if !strings.Contains(gotAccept, "application/vnd.docker.distribution.manifest.v2+json") {
		t.Errorf("Expected Accept header to include manifest v2, got %q", gotAccept)
	}
}

// TestGCRProvider_GetLatestDigest_ByDigest returns the digest from criteria directly.
func TestGCRProvider_GetLatestDigest_ByDigest(t *testing.T) {
	p := newGCRProviderForTest(nil, newFakeTokenSource())

	digest, err := p.GetLatestDigest(context.Background(), "gcr.io/my-project/my-repo:latest", Criteria{
		Digest: "sha256:direct-digest",
	})
	if err != nil {
		t.Fatalf("GetLatestDigest: %v", err)
	}
	if digest != "sha256:direct-digest" {
		t.Errorf("Expected digest 'sha256:direct-digest', got %q", digest)
	}
}

// TestGCRProvider_GetLatestDigest_ByVersion uses criteria.Version as the tag.
func TestGCRProvider_GetLatestDigest_ByVersion(t *testing.T) {
	expectedDigest := "sha256:version-match"

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/my-project/my-repo/manifests/v2.0.0" {
			w.Header().Set("Docker-Content-Digest", expectedDigest)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := newGCRProviderForTest(server.Client(), newFakeTokenSource())
	p.setBaseURLForTest(server.URL)

	digest, err := p.GetLatestDigest(context.Background(), "gcr.io/my-project/my-repo:latest", Criteria{
		Version: "v2.0.0",
	})
	if err != nil {
		t.Fatalf("GetLatestDigest: %v", err)
	}
	if digest != expectedDigest {
		t.Errorf("Expected digest %q, got %q", expectedDigest, digest)
	}
}

// TestGCRProvider_GetLatestDigest_ByRegex enumerates tags, filters by regex,
// and returns the digest of the first match.
func TestGCRProvider_GetLatestDigest_ByRegex(t *testing.T) {
	var reqCount int

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		switch {
		case r.URL.Path == "/v2/my-project/my-repo/tags/list" && r.Method == http.MethodGet:
			// Return tag list.
			resp := struct {
				Name string   `json:"name"`
				Tags []string `json:"tags"`
			}{
				Name: "my-project/my-repo",
				Tags: []string{"v1.0.0", "v2.1.0", "latest", "v1.2.3", "dev"},
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/v2/my-project/my-repo/manifests/v2.1.0" && r.Method == http.MethodHead:
			// Return digest for the highest matching tag (descending: v2.1.0 > v1.2.3 > v1.0.0).
			w.Header().Set("Docker-Content-Digest", "sha256:regex-match-digest")
			w.WriteHeader(http.StatusOK)
		default:
			t.Logf("Unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	p := newGCRProviderForTest(server.Client(), newFakeTokenSource())
	p.setBaseURLForTest(server.URL)

	digest, err := p.GetLatestDigest(context.Background(), "gcr.io/my-project/my-repo:latest", Criteria{
		Regex: `^v\d+\.\d+\.\d+$`,
	})
	if err != nil {
		t.Fatalf("GetLatestDigest: %v", err)
	}
	if digest != "sha256:regex-match-digest" {
		t.Errorf("Expected digest 'sha256:regex-match-digest', got %q", digest)
	}
	if reqCount != 2 {
		t.Errorf("Expected 2 requests (tags list + manifest head), got %d", reqCount)
	}
}

// TestGCRProvider_GetLatestDigest_ByRegex_NoMatch returns an error when no tags match.
func TestGCRProvider_GetLatestDigest_ByRegex_NoMatch(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/my-project/my-repo/tags/list" && r.Method == http.MethodGet {
			resp := struct {
				Name string   `json:"name"`
				Tags []string `json:"tags"`
			}{
				Name: "my-project/my-repo",
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

	p := newGCRProviderForTest(server.Client(), newFakeTokenSource())
	p.setBaseURLForTest(server.URL)

	_, err := p.GetLatestDigest(context.Background(), "gcr.io/my-project/my-repo:latest", Criteria{
		Regex: `^v\d+\.\d+\.\d+$`,
	})
	if err == nil {
		t.Fatal("Expected error for no matching tags, got nil")
	}
	if !strings.Contains(err.Error(), "no tags matching") {
		t.Errorf("Expected error containing 'no tags matching', got %q", err.Error())
	}
}

// TestGCRProvider_GetLatestDigest_ByRegex_InvalidPattern returns an error for bad regex.
func TestGCRProvider_GetLatestDigest_ByRegex_InvalidPattern(t *testing.T) {
	p := newGCRProviderForTest(nil, newFakeTokenSource())

	_, err := p.GetLatestDigest(context.Background(), "gcr.io/my-project/my-repo:latest", Criteria{
		Regex: `[invalid(regex`,
	})
	if err == nil {
		t.Fatal("Expected error for invalid regex, got nil")
	}
	if !strings.Contains(err.Error(), "invalid regex") {
		t.Errorf("Expected error containing 'invalid regex', got %q", err.Error())
	}
}

// TestGCRProvider_GetLatestDigest_NotFound returns an error when the registry
// returns a 404 status.
func TestGCRProvider_GetLatestDigest_NotFound(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := newGCRProviderForTest(server.Client(), newFakeTokenSource())
	p.setBaseURLForTest(server.URL)

	_, err := p.GetLatestDigest(context.Background(), "gcr.io/my-project/my-repo:nonexistent", Criteria{})
	if err == nil {
		t.Fatal("Expected error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "registry returned status 404") {
		t.Errorf("Expected error containing 'registry returned status 404', got %q", err.Error())
	}
}

// TestGCRProvider_GetLatestDigest_MissingDigestHeader returns an error when the
// Docker-Content-Digest header is missing.
func TestGCRProvider_GetLatestDigest_MissingDigestHeader(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 200 but no digest header.
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := newGCRProviderForTest(server.Client(), newFakeTokenSource())
	p.setBaseURLForTest(server.URL)

	_, err := p.GetLatestDigest(context.Background(), "gcr.io/my-project/my-repo:v1.0.0", Criteria{})
	if err == nil {
		t.Fatal("Expected error for missing digest header, got nil")
	}
	if !strings.Contains(err.Error(), "missing Docker-Content-Digest") {
		t.Errorf("Expected error containing 'missing Docker-Content-Digest', got %q", err.Error())
	}
}

// TestGCRProvider_GetLatestDigest_EmptyImageRef returns an error for empty ref.
func TestGCRProvider_GetLatestDigest_EmptyImageRef(t *testing.T) {
	p := newGCRProviderForTest(nil, newFakeTokenSource())

	_, err := p.GetLatestDigest(context.Background(), "", Criteria{})
	if err == nil {
		t.Fatal("Expected error for empty image reference, got nil")
	}
}

// TestGCRProvider_GetLatestDigest_NoTag defaults to "latest".
func TestGCRProvider_GetLatestDigest_NoTag(t *testing.T) {
	expectedDigest := "sha256:latest-digest"

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/my-project/my-repo/manifests/latest" {
			w.Header().Set("Docker-Content-Digest", expectedDigest)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := newGCRProviderForTest(server.Client(), newFakeTokenSource())
	p.setBaseURLForTest(server.URL)

	digest, err := p.GetLatestDigest(context.Background(), "gcr.io/my-project/my-repo", Criteria{})
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

// TestGCRProvider_GetImageVersion returns the tag from imageRef.
func TestGCRProvider_GetImageVersion(t *testing.T) {
	p := newGCRProviderForTest(nil, newFakeTokenSource())

	version, err := p.GetImageVersion(context.Background(), "gcr.io/my-project/my-repo:v1.2.3")
	if err != nil {
		t.Fatalf("GetImageVersion: %v", err)
	}
	if version != "v1.2.3" {
		t.Errorf("Expected version 'v1.2.3', got %q", version)
	}
}

// TestGCRProvider_GetImageVersion_NoTag returns "latest".
func TestGCRProvider_GetImageVersion_NoTag(t *testing.T) {
	p := newGCRProviderForTest(nil, newFakeTokenSource())

	version, err := p.GetImageVersion(context.Background(), "gcr.io/my-project/my-repo")
	if err != nil {
		t.Fatalf("GetImageVersion: %v", err)
	}
	if version != "latest" {
		t.Errorf("Expected version 'latest', got %q", version)
	}
}

// TestGCRProvider_GetImageVersion_EmptyRef returns an error.
func TestGCRProvider_GetImageVersion_EmptyRef(t *testing.T) {
	p := newGCRProviderForTest(nil, newFakeTokenSource())

	_, err := p.GetImageVersion(context.Background(), "")
	if err == nil {
		t.Fatal("Expected error for empty image reference, got nil")
	}
}

// TestGCRProvider_GetImageVersion_WithDigest returns the digest string.
func TestGCRProvider_GetImageVersion_WithDigest(t *testing.T) {
	p := newGCRProviderForTest(nil, newFakeTokenSource())

	version, err := p.GetImageVersion(context.Background(), "gcr.io/my-project/my-repo@sha256:abcd1234")
	if err != nil {
		t.Fatalf("GetImageVersion: %v", err)
	}
	if version != "sha256:abcd1234" {
		t.Errorf("Expected version 'sha256:abcd1234', got %q", version)
	}
}

// ---------------------------------------------------------------------------
// Token caching tests
// ---------------------------------------------------------------------------

// TestGCRProvider_TokenCaching verifies that the token source is only called
// once for multiple GetAuth calls and that InvalidateCache forces a refresh.
func TestGCRProvider_TokenCaching(t *testing.T) {
	ts := newFakeTokenSource()
	p := newGCRProviderForTest(nil, ts)

	// First call — should hit token source.
	if _, err := p.GetAuth(context.Background()); err != nil {
		t.Fatalf("first GetAuth: %v", err)
	}
	if ts.callN != 1 {
		t.Errorf("expected 1 token call, got %d", ts.callN)
	}

	// Second call — should use cache.
	if _, err := p.GetAuth(context.Background()); err != nil {
		t.Fatalf("second GetAuth: %v", err)
	}
	if ts.callN != 1 {
		t.Errorf("expected 1 token call (cached), got %d", ts.callN)
	}

	// Invalidate then call again — should refresh.
	p.InvalidateCache()
	if _, err := p.GetAuth(context.Background()); err != nil {
		t.Fatalf("GetAuth after invalidate: %v", err)
	}
	if ts.callN != 2 {
		t.Errorf("expected 2 token calls after invalidate, got %d", ts.callN)
	}
}

// TestGCRProvider_TokenCaching_ExpiryBuffer verifies that a cached token within
// the 5-minute expiry buffer is refreshed (the token method checks
// Expiry.After(time.Now().Add(5*time.Minute))).
func TestGCRProvider_TokenCaching_ExpiryBuffer(t *testing.T) {
	ts := newFakeTokenSource()
	p := newGCRProviderForTest(nil, ts)

	// Pre-populate the cache with a token that expires in 3 minutes (within the 5-min buffer).
	token := &oauth2.Token{
		AccessToken: "near-expiry-token",
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(3 * time.Minute),
	}
	p.mu.Lock()
	p.cachedToken = token
	p.mu.Unlock()

	// GetAuth should refresh because the cached token is within the buffer.
	auth, err := p.GetAuth(context.Background())
	if err != nil {
		t.Fatalf("GetAuth: %v", err)
	}
	if ts.callN != 1 {
		t.Errorf("expected token source to be called once (buffer refresh), got %d", ts.callN)
	}
	if auth.Password != "fake-access-token" {
		t.Errorf("expected password from new token, got %q", auth.Password)
	}

	// A second call should see the fresh valid token (1 hour from now) and use the cache.
	_, err = p.GetAuth(context.Background())
	if err != nil {
		t.Fatalf("second GetAuth: %v", err)
	}
	if ts.callN != 1 {
		t.Errorf("expected token source to still be called once only (cached), got %d", ts.callN)
	}
}

// ---------------------------------------------------------------------------
// gcrParseFullRef tests
// ---------------------------------------------------------------------------

// TestGCRParseFullRef exercises the gcrParseFullRef function with various inputs.
func TestGCRParseFullRef(t *testing.T) {
	tests := []struct {
		name            string
		ref             string
		wantHost        string
		wantProjectPath string
		wantRef         string
		wantErr         bool
		errContains     string
	}{
		{
			name:            "tag",
			ref:             "gcr.io/project/repo:tag",
			wantHost:        "gcr.io",
			wantProjectPath: "project/repo",
			wantRef:         "tag",
		},
		{
			name:            "digest",
			ref:             "gcr.io/project/repo@sha256:abc",
			wantHost:        "gcr.io",
			wantProjectPath: "project/repo",
			wantRef:         "sha256:abc",
		},
		{
			name:            "artifact registry",
			ref:             "us-docker.pkg.dev/proj/sub/repo:tag",
			wantHost:        "us-docker.pkg.dev",
			wantProjectPath: "proj/sub/repo",
			wantRef:         "tag",
		},
		{
			name:            "artifact registry digest",
			ref:             "us-docker.pkg.dev/proj/sub/repo@sha256:abc",
			wantHost:        "us-docker.pkg.dev",
			wantProjectPath: "proj/sub/repo",
			wantRef:         "sha256:abc",
		},
		{
			name:            "europe location",
			ref:             "europe-docker.pkg.dev/p/r:v1",
			wantHost:        "europe-docker.pkg.dev",
			wantProjectPath: "p/r",
			wantRef:         "v1",
		},
		{
			name:            "no tag defaults to latest",
			ref:             "gcr.io/project/repo",
			wantHost:        "gcr.io",
			wantProjectPath: "project/repo",
			wantRef:         "latest",
		},
		{
			name:            "single project component",
			ref:             "gcr.io/project:tag",
			wantHost:        "gcr.io",
			wantProjectPath: "project",
			wantRef:         "tag",
		},
		{
			name:        "empty reference",
			ref:         "",
			wantErr:     true,
			errContains: "empty",
		},
		{
			name:        "no host",
			ref:         "onlyrepo",
			wantErr:     true,
			errContains: "missing registry host",
		},
		{
			name:        "no project path",
			ref:         "gcr.io/",
			wantErr:     true,
			errContains: "missing repository path",
		},
		{
			name:            "tag with path containing colon inside project",
			ref:             "gcr.io/project/sub:tag",
			wantHost:        "gcr.io",
			wantProjectPath: "project/sub",
			wantRef:         "tag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, projectPath, ref, err := gcrParseFullRef(tt.ref)
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
			if projectPath != tt.wantProjectPath {
				t.Errorf("projectPath: got %q, want %q", projectPath, tt.wantProjectPath)
			}
			if ref != tt.wantRef {
				t.Errorf("ref: got %q, want %q", ref, tt.wantRef)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// listTags specific tests
// ---------------------------------------------------------------------------

// TestGCRProvider_ListTags exercises the listTags path via regex criteria.
func TestGCRProvider_ListTags(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/my-project/my-repo/tags/list" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"name": "my-project/my-repo",
				"tags": []string{"v1", "v2", "v3"},
			})
		case r.URL.Path == "/v2/my-project/my-repo/manifests/v1" && r.Method == http.MethodHead:
			w.Header().Set("Docker-Content-Digest", "sha256:tag-v1-digest")
			w.WriteHeader(http.StatusOK)
		default:
			t.Logf("Unexpected: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	p := newGCRProviderForTest(server.Client(), newFakeTokenSource())
	p.setBaseURLForTest(server.URL)

	digest, err := p.GetLatestDigest(context.Background(), "gcr.io/my-project/my-repo:latest", Criteria{
		Regex: "v1",
	})
	if err != nil {
		t.Fatalf("GetLatestDigest with regex: %v", err)
	}
	if digest != "sha256:tag-v1-digest" {
		t.Errorf("expected digest 'sha256:tag-v1-digest', got %q", digest)
	}
}

// TestGCRProvider_ListTags_RegistryError returns an error when list tags fails.
func TestGCRProvider_ListTags_RegistryError(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := newGCRProviderForTest(server.Client(), newFakeTokenSource())
	p.setBaseURLForTest(server.URL)

	_, err := p.GetLatestDigest(context.Background(), "gcr.io/my-project/my-repo:latest", Criteria{
		Regex: "v1",
	})
	if err == nil {
		t.Fatal("Expected error for 500, got nil")
	}
	if !strings.Contains(err.Error(), "registry returned status 500") {
		t.Errorf("Expected error containing 'registry returned status 500', got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Concurrent safety test for token cache
// ---------------------------------------------------------------------------

// TestGCRProvider_GetAuthConcurrent verifies that concurrent calls to GetAuth
// do not cause multiple token source invocations.
func TestGCRProvider_GetAuthConcurrent(t *testing.T) {
	ts := newFakeTokenSource()
	p := newGCRProviderForTest(nil, ts)

	// Warm the cache.
	if _, err := p.GetAuth(context.Background()); err != nil {
		t.Fatalf("warmup GetAuth: %v", err)
	}

	// Invalidate so the next call will need to fetch again.
	p.InvalidateCache()
	ts.callN = 0 // reset

	const goroutines = 20
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := p.GetAuth(context.Background()); err != nil {
				t.Errorf("concurrent GetAuth: %v", err)
			}
		}()
	}
	wg.Wait()

	// Only one goroutine should have hit the token source (others got the cached token).
	if ts.callN != 1 {
		t.Errorf("expected token source to be called exactly once concurrently, got %d", ts.callN)
	}
}

// ---------------------------------------------------------------------------
// Head manifest error propagation test
// ---------------------------------------------------------------------------

// TestGCRProvider_HeadManifest_NetworkError verifies that a network error
// propagates correctly.
func TestGCRProvider_HeadManifest_NetworkError(t *testing.T) {
	// Use a server that closes immediately.
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Close the connection without responding.
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	server.Close() // Close so the client gets a connection refused / reset

	p := newGCRProviderForTest(server.Client(), newFakeTokenSource())
	p.setBaseURLForTest(server.URL)

	_, err := p.GetLatestDigest(context.Background(), "gcr.io/my-project/my-repo:v1.0.0", Criteria{})
	if err == nil {
		t.Fatal("Expected error for network failure, got nil")
	}
}
