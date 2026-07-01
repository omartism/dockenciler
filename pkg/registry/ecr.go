package registry

import (
    "context"
    "encoding/base64"
    "fmt"
    "regexp"
    "sort"
    "strings"
    "sync"
    "time"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/service/ecr"
    "github.com/aws/aws-sdk-go-v2/service/ecr/types"
)

// ECRClient defines the interface for ECR client operations needed by ECRProvider
// This allows for easier testing with mock implementations
//
//go:generate mockgen -destination=../../mocks/mock_ecr_client.go -package=mocks github.com/omarismael/dockenciler/pkg/registry ECRClient
//
//go:generate mockgen -destination=../../mocks/mock_ecr_client_test.go -package=mocks_test github.com/omarismael/dockenciler/pkg/registry ECRClient

// ECRClient interface defines the methods required from an ECR client
// This is used to decouple ECRProvider from the concrete *ecr.Client implementation
//
//go:generate mockgen -destination=../../mocks/mock_ecr_client.go -package=mocks github.com/omarismael/dockenciler/pkg/registry ECRClient
//
//go:generate mockgen -destination=../../mocks/mock_ecr_client_test.go -package=mocks_test github.com/omarismael/dockenciler/pkg/registry ECRClient

type ECRClient interface {
    DescribeImages(ctx context.Context, input *ecr.DescribeImagesInput, optFns ...func(*ecr.Options)) (*ecr.DescribeImagesOutput, error)
    GetAuthorizationToken(ctx context.Context, input *ecr.GetAuthorizationTokenInput, optFns ...func(*ecr.Options)) (*ecr.GetAuthorizationTokenOutput, error)
}

// matchTag checks if a tag matches the given criteria
func matchTag(tag string, criteria Criteria) bool {
    if criteria.Version != "" {
        return tag == criteria.Version
    }
    if criteria.Regex != "" {
        re, err := regexp.Compile(criteria.Regex)
        if err != nil {
            // If regex is invalid, return false
            return false
        }
        return re.MatchString(tag)
    }
    if criteria.Digest != "" {
        // For digest matching, we need to check if the image exists with that digest
        // This will be handled in GetLatestDigest
        return true
    }
    return false
}

// ECRProvider implements the Registry interface for AWS ECR
type ECRProvider struct {
    client     ECRClient
    authClient ECRClient
    cachedToken string
    cachedRegistryURL string
    tokenExpiry time.Time
    mu          sync.RWMutex
}

// NewECRProvider creates a new ECRProvider with the given ECR client implementation
func NewECRProvider(client ECRClient) *ECRProvider {
    return &ECRProvider{
        client:     client,
        authClient: client,
    }
}

// GetAuthToken retrieves a valid authorization token from ECR, using caching with a 5-minute buffer.
// GetAuthToken retrieves a valid authorization token from ECR, using caching with a 5-minute buffer.
func (p *ECRProvider) GetAuthToken(ctx context.Context) (string, string, error) {
    p.mu.RLock()
    if p.tokenExpiry.After(time.Now().Add(5*time.Minute)) && p.cachedToken != "" && p.cachedRegistryURL != "" {
        // Token is still valid, return cached token and registry URL
        token := p.cachedToken
        registryURL := p.cachedRegistryURL
        p.mu.RUnlock()
        return registryURL, token, nil
    }
    p.mu.RUnlock()

    // Lock for writing to avoid race condition when refreshing token
    p.mu.Lock()
    defer p.mu.Unlock()

    // Double-check after acquiring lock
    if p.tokenExpiry.After(time.Now().Add(5*time.Minute)) && p.cachedToken != "" && p.cachedRegistryURL != "" {
        return p.cachedRegistryURL, p.cachedToken, nil
    }

    // Call GetAuthorizationToken
    output, err := p.authClient.GetAuthorizationToken(ctx, &ecr.GetAuthorizationTokenInput{})
    if err != nil {
        return "", "", fmt.Errorf("failed to get authorization token: %w", err)
    }

    if len(output.AuthorizationData) == 0 {
        return "", "", fmt.Errorf("no authorization data returned")
    }

    data := output.AuthorizationData[0]
    if data.AuthorizationToken == nil {
        return "", "", fmt.Errorf("authorization token is nil")
    }
    if data.ProxyEndpoint == nil {
        return "", "", fmt.Errorf("proxy endpoint is nil")
    }

    // Decode the token to verify it's valid (optional but recommended)
    if _, err := base64.StdEncoding.DecodeString(*data.AuthorizationToken); err != nil {
        return "", "", fmt.Errorf("failed to decode authorization token: %w", err)
    }
    // Optionally, you can validate the decoded token format here (e.g., contains colon)
    // For now, we just use the encoded token as the token value, as that's what Docker expects.
    // The token is usually used as the password in `docker login -u AWS -p <token> <registry>`

    // Cache the token and registry URL
    p.cachedToken = *data.AuthorizationToken
    p.cachedRegistryURL = *data.ProxyEndpoint
    // Set expiry: the token is valid for 12 hours, but we can also use the expiresAt if provided?
    // The AuthorizationData does not have an expiresAt field. According to AWS docs, the token is valid for 12 hours.
    // We'll set the expiry to 12 hours from now.
    p.tokenExpiry = time.Now().Add(12 * time.Hour)

    return p.cachedRegistryURL, p.cachedToken, nil
}

// GetLatestDigest retrieves the latest image digest from ECR based on criteria
func (p *ECRProvider) GetLatestDigest(ctx context.Context, imageRef string, criteria Criteria) (string, error) {
    // Parse image reference to extract repository and tag
    repo, tag, err := parseImageRef(imageRef)
    if err != nil {
        return "", fmt.Errorf("failed to parse image reference: %w", err)
    }

    // Describe images for the repository
    input := &ecr.DescribeImagesInput{
        RepositoryName: aws.String(repo),
    }

    // Apply criteria filters
    if criteria.Version != "" {
        // Filter by exact version/tag match
        input.ImageIds = []types.ImageIdentifier{{
            ImageTag: aws.String(criteria.Version),
        }}
    } else if criteria.Regex != "" {
        // For regex matching, we need to get all images and filter locally
        // First get all images without tag filtering
        input.ImageIds = nil
    } else if criteria.Digest != "" {
        // Filter by exact digest match
        input.ImageIds = []types.ImageIdentifier{{
            ImageDigest: aws.String(criteria.Digest),
        }}
    } else {
        // No criteria - filter by the tag from imageRef
        input.ImageIds = []types.ImageIdentifier{{
            ImageTag: aws.String(tag),
        }}
    }

    result, err := p.client.DescribeImages(ctx, input)
    if err != nil {
        return "", fmt.Errorf("failed to describe images: %w", err)
    }

    if result == nil || len(result.ImageDetails) == 0 {
        return "", fmt.Errorf("no images found in repository %s", repo)
    }

    // If regex criteria is used, filter images by matching tags
    if criteria.Regex != "" {
        var matchingImages []types.ImageDetail
        for _, img := range result.ImageDetails {
            for _, imgTag := range img.ImageTags {
                if matchTag(imgTag, criteria) {
                    matchingImages = append(matchingImages, img)
                    break
                }
            }
        }
        if len(matchingImages) == 0 {
            return "", fmt.Errorf("no images found matching regex %s in repository %s", criteria.Regex, repo)
        }
        result.ImageDetails = matchingImages
    }

    // Sort images by pushed time (newest first)
    sort.Slice(result.ImageDetails, func(i, j int) bool {
        return result.ImageDetails[i].ImagePushedAt.After(*result.ImageDetails[j].ImagePushedAt)
    })

    // Return the digest of the latest image
    latestImage := result.ImageDetails[0]
    if latestImage.ImageDigest != nil {
        return *latestImage.ImageDigest, nil
    }

    return "", fmt.Errorf("latest image has no digest")
}

// GetImageVersion retrieves the tag of the latest image from ECR
func (p *ECRProvider) GetImageVersion(ctx context.Context, imageRef string) (string, error) {
    // Parse image reference to extract repository and tag
    repo, _, err := parseImageRef(imageRef)
    if err != nil {
        return "", fmt.Errorf("failed to parse image reference: %w", err)
    }

    // Describe images for the repository
    input := &ecr.DescribeImagesInput{
        RepositoryName: aws.String(repo),
    }

    result, err := p.client.DescribeImages(ctx, input)
    if err != nil {
        return "", fmt.Errorf("failed to describe images: %w", err)
    }

    if result == nil || len(result.ImageDetails) == 0 {
        return "", fmt.Errorf("no images found in repository %s", repo)
    }

    // Sort images by pushed time (newest first)
    sort.Slice(result.ImageDetails, func(i, j int) bool {
        return result.ImageDetails[i].ImagePushedAt.After(*result.ImageDetails[j].ImagePushedAt)
    })

    // Return the tag of the latest image
    latestImage := result.ImageDetails[0]
    if len(latestImage.ImageTags) > 0 {
        return latestImage.ImageTags[0], nil
    }

    return "", fmt.Errorf("latest image has no tag")
}

// parseImageRef parses an image reference in formats:
//   - "repository:tag"
//   - "repository@sha256:digest"
//   - "registry/repo:tag"
//   - "registry/repo@sha256:digest"
//
// For ECR, it extracts just the repository name without the registry prefix.
func parseImageRef(imageRef string) (repo string, tag string, err error) {
    if len(imageRef) == 0 {
        return "", "", fmt.Errorf("empty image reference")
    }

    // Strip digest part if present (e.g., "repo:tag@sha256:abc" -> "repo:tag")
    if idx := strings.Index(imageRef, "@"); idx != -1 {
        imageRef = imageRef[:idx]
    }

    // Find the last colon to separate repo and tag
    lastColon := -1
    for i := len(imageRef) - 1; i >= 0; i-- {
        if imageRef[i] == ':' {
            lastColon = i
            break
        }
    }

    if lastColon == -1 {
        repo = imageRef
        tag = "latest"
    } else {
        repo = imageRef[:lastColon]
        tag = imageRef[lastColon+1:]
    }

    if repo == "" {
        return "", "", fmt.Errorf("empty repository name")
    }

    // Strip registry prefix if present (e.g., "123456789.dkr.ecr.region.amazonaws.com/repo" -> "repo")
    if idx := strings.LastIndex(repo, "/"); idx != -1 {
        repo = repo[idx+1:]
    }

    return repo, tag, nil
}