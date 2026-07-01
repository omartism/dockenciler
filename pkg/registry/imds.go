package registry

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// IMDSv2Provider fetches AWS credentials from the EC2 Instance Metadata Service (IMDS) version 2.
type IMDSv2Provider struct {
	// HTTP client to use for requests. If nil, http.DefaultClient is used.
	Client *http.Client
	// Base URL for the IMDS endpoint. If empty, defaults to "http://169.254.169.254".
	// This field is useful for testing.
	BaseURL string
	// Token TTL in seconds. If zero, defaults to 60 seconds.
	TokenTTL int
}

// getBaseURL returns the base URL for IMDS requests, using the default if BaseURL is empty.
func (p *IMDSv2Provider) getBaseURL() string {
	if p.BaseURL != "" {
		return p.BaseURL
	}
	return "http://169.254.169.254"
}

// GetCredentials retrieves AWS credentials from IMDSv2.
// It first fetches a token, then uses that token to get the IAM role credentials.
// Returns an error if the instance metadata service is not available or if the credentials cannot be retrieved.
func (p *IMDSv2Provider) GetCredentials(ctx context.Context) (aws.Credentials, error) {
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}

	// Step 1: Get the IMDSv2 token.
	token, err := p.getToken(ctx, client)
	if err != nil {
		return aws.Credentials{}, err
	}

	// Step 2: Discover the IAM role name.
	roleName, err := p.getRoleName(ctx, client, token)
	if err != nil {
		return aws.Credentials{}, err
	}

	// Step 3: Get the credentials for the role.
	creds, err := p.getRoleCredentials(ctx, client, token, roleName)
	if err != nil {
		return aws.Credentials{}, err
	}

	return creds, nil
}

// getToken fetches the IMDSv2 token.
func (p *IMDSv2Provider) getToken(ctx context.Context, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, p.getBaseURL()+"/latest/api/token", nil)
	if err != nil {
		return "", err
	}
	ttl := p.TokenTTL
	if ttl == 0 {
		ttl = 60 // default TTL
	}
	req.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", strconv.Itoa(ttl))

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", errors.New("failed to retrieve IMDSv2 token: unexpected status code " + resp.Status)
	}

	// Read the token from the response body.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// getRoleName discovers the IAM role name from the IMDSv2 endpoint.
func (p *IMDSv2Provider) getRoleName(ctx context.Context, client *http.Client, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.getBaseURL()+"/latest/meta-data/iam/security-credentials/", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-aws-ec2-metadata-token", token)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", errors.New("failed to retrieve IAM role name: unexpected status code " + resp.Status)
	}

	// The response body contains the role name (plain text).
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// getRoleCredentials fetches the AWS credentials for the given IAM role.
func (p *IMDSv2Provider) getRoleCredentials(ctx context.Context, client *http.Client, token, roleName string) (aws.Credentials, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.getBaseURL()+"/latest/meta-data/iam/security-credentials/"+roleName, nil)
	if err != nil {
		return aws.Credentials{}, err
	}
	req.Header.Set("X-aws-ec2-metadata-token", token)

	resp, err := client.Do(req)
	if err != nil {
		return aws.Credentials{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return aws.Credentials{}, errors.New("failed to retrieve IAM role credentials: unexpected status code " + resp.Status)
	}

	// Parse the JSON response into a temporary struct.
	var credsResp struct {
		AccessKeyID     string `json:"AccessKeyId"`
		SecretAccessKey string `json:"SecretAccessKey"`
		Token           string `json:"Token"`
		Expiration      string `json:"Expiration"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&credsResp); err != nil {
		return aws.Credentials{}, err
	}

	// Convert Expiration string to time.Time.
	var expiration time.Time
	if credsResp.Expiration != "" {
		var err error
		expiration, err = time.Parse(time.RFC3339, credsResp.Expiration)
		if err != nil {
			return aws.Credentials{}, err
		}
	}

	return aws.Credentials{
		AccessKeyID:     credsResp.AccessKeyID,
		SecretAccessKey: credsResp.SecretAccessKey,
		SessionToken:    credsResp.Token,
		Expires:         expiration,
	}, nil
}