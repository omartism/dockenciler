package registry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestIMDSv2Provider_GetCredentials tests the GetCredentials method with a mock IMDSv2 server.
func TestIMDSv2Provider_GetCredentials(t *testing.T) {
	// Mock IMDSv2 endpoints.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Log the request for debugging.
		t.Logf("Received request: %s %s", r.Method, r.URL.Path)
		t.Logf("Headers: %v", r.Header)
		switch r.URL.Path {
		case "/latest/api/token":
			if r.Method != http.MethodPut {
				t.Errorf("expected PUT request, got %s", r.Method)
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			// Check for the token TTL header.
			if r.Header.Get("X-aws-ec2-metadata-token-ttl-seconds") == "" {
				t.Errorf("expected header X-aws-ec2-metadata-token-ttl-seconds")
			}
			// Return a fixed token.
			token := []byte("mock-token-v2")
			w.Header().Set("X-aws-ec2-metadata-token-ttl-seconds", "60")
			w.Header().Set("Content-Length", strconv.Itoa(len(token)))
			w.Write(token)
		case "/latest/meta-data/iam/security-credentials/":
			if r.Method != http.MethodGet {
				t.Errorf("expected GET request, got %s", r.Method)
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			// Check for the token header.
			if r.Header.Get("X-aws-ec2-metadata-token") != "mock-token-v2" {
				t.Errorf("expected token header")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			// Return a role name.
			role := []byte("my-role")
			w.Header().Set("Content-Length", strconv.Itoa(len(role)))
			w.Write(role)
		case "/latest/meta-data/iam/security-credentials/my-role":
			if r.Method != http.MethodGet {
				t.Errorf("expected GET request, got %s", r.Method)
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			if r.Header.Get("X-aws-ec2-metadata-token") != "mock-token-v2" {
				t.Errorf("expected token header")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			// Return mock credentials.
			resp := map[string]interface{}{
				"AccessKeyId":     "AKIAIOSFODNN7EXAMPLE",
				"SecretAccessKey": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYzEXAMPLEKEY",
				"Token":           "AQoDYXdzEPT//////////wEXAMPLEtc764bNrC9SAPBSM22wDOk4x4HIZ8j4FZTwdQWLsK2HCu+JL7rQYLQwzW2aequal/0s0+Fbm0aVY79n8WDkk0+ewtHJ/VBAEXAMPLE",
				"Expiration":      "2025-01-01T12:00:00Z",
			}
			jsonData, err := json.Marshal(resp)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Length", strconv.Itoa(len(jsonData)))
			w.Write(jsonData)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	// Create a provider that uses the test server.
	p := &IMDSv2Provider{
		Client: ts.Client(),
		BaseURL: ts.URL, // Use the test server as the base URL.
		// Use a short TTL for testing.
		TokenTTL: 60,
	}

	// Call GetCredentials.
	ctx := context.Background()
	creds, err := p.GetCredentials(ctx)
	if err != nil {
		t.Fatalf("GetCredentials failed: %v", err)
	}
	require.NoError(t, err)

	// Check the returned credentials.
	require.Equal(t, "AKIAIOSFODNN7EXAMPLE", creds.AccessKeyID)
	require.Equal(t, "wJalrXUtnFEMI/K7MDENG/bPxRfiCYzEXAMPLEKEY", creds.SecretAccessKey)
	require.Equal(t, "AQoDYXdzEPT//////////wEXAMPLEtc764bNrC9SAPBSM22wDOk4x4HIZ8j4FZTwdQWLsK2HCu+JL7rQYLQwzW2aequal/0s0+Fbm0aVY79n8WDkk0+ewtHJ/VBAEXAMPLE", creds.SessionToken)
	// Check expiration time (should be close to 2025-01-01T12:00:00Z).
	expectedExp, _ := time.Parse(time.RFC3339, "2025-01-01T12:00:00Z")
	within := time.Second * 1
	if diff := creds.Expires.Sub(expectedExp); diff < -within || diff > within {
		t.Fatalf("Expected expiration %v, got %v", expectedExp, creds.Expires)
	}
}