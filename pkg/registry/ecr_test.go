package registry

import (
    "context"
    "fmt"
    "sync"
    "testing"
    "time"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/service/ecr"
    "github.com/aws/aws-sdk-go-v2/service/ecr/types"
    "github.com/stretchr/testify/assert"
)

// TestGetLatestDigest tests the ECRProvider.GetLatestDigest method with various criteria
func TestGetLatestDigest(t *testing.T) {
    tests := []struct {
        name          string
        imageRef      string
        criteria      Criteria
        mockSetup     func(*mockECRClient)
        expectedDigest string
        expectError   bool
        errorContains string
    }{
        {
            name:     "Basic tag matching",
            imageRef: "123456789.dkr.ecr.us-west-2.amazonaws.com/my-repo:latest",
            criteria: Criteria{},
            mockSetup: func(client *mockECRClient) {
                client.describeImagesOutput = &ecr.DescribeImagesOutput{
                    ImageDetails: []types.ImageDetail{
                        {
                            ImageTags:      []string{"latest"},
                            ImagePushedAt:  aws.Time(time.Now().Add(-1 * time.Hour)),
                            ImageDigest:    aws.String("sha256:digest1"),
                        },
                        {
                            ImageTags:      []string{"v1.0.0"},
                            ImagePushedAt:  aws.Time(time.Now().Add(-2 * time.Hour)),
                            ImageDigest:    aws.String("sha256:digest2"),
                        },
                    },
                }
                client.getAuthorizationTokenOutput = &ecr.GetAuthorizationTokenOutput{
                    AuthorizationData: []types.AuthorizationData{
                        {
                            AuthorizationToken: aws.String("dG9rZW46cGFzc3dvcmQ="), // base64 of "username:password"
                            ProxyEndpoint:      aws.String("https://123456789012.dkr.ecr.us-west-2.amazonaws.com"),
                        },
                    },
                }
                client.getAuthorizationTokenErr = nil
                client.getAuthorizationTokenCallCount = 0
            },
            expectedDigest: "sha256:digest1",
            expectError:   false,
        },
        {
            name:     "Version criteria - exact tag match",
            imageRef: "123456789.dkr.ecr.us-west-2.amazonaws.com/my-repo:latest",
            criteria: Criteria{Version: "v1.0.0"},
            mockSetup: func(client *mockECRClient) {
                client.describeImagesOutput = &ecr.DescribeImagesOutput{
                    ImageDetails: []types.ImageDetail{
                        {
                            ImageTags:      []string{"v1.0.0"},
                            ImagePushedAt:  aws.Time(time.Now().Add(-1 * time.Hour)),
                            ImageDigest:    aws.String("sha256:digest_v1"),
                        },
                    },
                }
                client.getAuthorizationTokenOutput = &ecr.GetAuthorizationTokenOutput{
                    AuthorizationData: []types.AuthorizationData{
                        {
                            AuthorizationToken: aws.String("dG9rZW46cGFzc3dvcmQ="),
                            ProxyEndpoint:      aws.String("https://123456789012.dkr.ecr.us-west-2.amazonaws.com"),
                        },
                    },
                }
                client.getAuthorizationTokenErr = nil
                client.getAuthorizationTokenCallCount = 0
            },
            expectedDigest: "sha256:digest_v1",
            expectError:   false,
        },
        {
            name:     "Regex criteria - pattern matching",
            imageRef: "123456789.dkr.ecr.us-west-2.amazonaws.com/my-repo:latest",
            criteria: Criteria{Regex: `^v\d+\.\d+\.\d+$`},
            mockSetup: func(client *mockECRClient) {
                client.describeImagesOutput = &ecr.DescribeImagesOutput{
                    ImageDetails: []types.ImageDetail{
                        {
                            ImageTags:      []string{"v1.0.0"},
                            ImagePushedAt:  aws.Time(time.Now().Add(-1 * time.Hour)),
                            ImageDigest:    aws.String("sha256:digest_v1"),
                        },
                        {
                            ImageTags:      []string{"v2.1.0"},
                            ImagePushedAt:  aws.Time(time.Now().Add(-2 * time.Hour)),
                            ImageDigest:    aws.String("sha256:digest_v2"),
                        },
                        {
                            ImageTags:      []string{"latest"},
                            ImagePushedAt:  aws.Time(time.Now().Add(-3 * time.Hour)),
                            ImageDigest:    aws.String("sha256:digest_latest"),
                        },
                    },
                }
                client.getAuthorizationTokenOutput = &ecr.GetAuthorizationTokenOutput{
                    AuthorizationData: []types.AuthorizationData{
                        {
                            AuthorizationToken: aws.String("dG9rZW46cGFzc3dvcmQ="),
                            ProxyEndpoint:      aws.String("https://123456789012.dkr.ecr.us-west-2.amazonaws.com"),
                        },
                    },
                }
                client.getAuthorizationTokenErr = nil
                client.getAuthorizationTokenCallCount = 0
            },
            expectedDigest: "sha256:digest_v1",
            expectError:   false,
        },
        {
            name:     "Digest criteria - exact digest match",
            imageRef: "123456789.dkr.ecr.us-west-2.amazonaws.com/my-repo:latest",
            criteria: Criteria{Digest: "sha256:digest_v1"},
            mockSetup: func(client *mockECRClient) {
                client.describeImagesOutput = &ecr.DescribeImagesOutput{
                    ImageDetails: []types.ImageDetail{
                        {
                            ImageTags:      []string{"v1.0.0"},
                            ImagePushedAt:  aws.Time(time.Now().Add(-1 * time.Hour)),
                            ImageDigest:    aws.String("sha256:digest_v1"),
                        },
                    },
                }
                client.getAuthorizationTokenOutput = &ecr.GetAuthorizationTokenOutput{
                    AuthorizationData: []types.AuthorizationData{
                        {
                            AuthorizationToken: aws.String("dG9rZW46cGFzc3dvcmQ="),
                            ProxyEndpoint:      aws.String("https://123456789012.dkr.ecr.us-west-2.amazonaws.com"),
                        },
                    },
                }
                client.getAuthorizationTokenErr = nil
                client.getAuthorizationTokenCallCount = 0
            },
            expectedDigest: "sha256:digest_v1",
            expectError:   false,
        },
        {
            name:     "No images found",
            imageRef: "123456789.dkr.ecr.us-west-2.amazonaws.com/my-repo:latest",
            criteria: Criteria{},
            mockSetup: func(client *mockECRClient) {
                client.describeImagesOutput = &ecr.DescribeImagesOutput{
                    ImageDetails: []types.ImageDetail{},
                }
                client.getAuthorizationTokenOutput = &ecr.GetAuthorizationTokenOutput{
                    AuthorizationData: []types.AuthorizationData{
                        {
                            AuthorizationToken: aws.String("dG9rZW46cGFzc3dvcmQ="),
                            ProxyEndpoint:      aws.String("https://123456789012.dkr.ecr.us-west-2.amazonaws.com"),
                        },
                    },
                }
                client.getAuthorizationTokenErr = nil
                client.getAuthorizationTokenCallCount = 0
            },
            expectedDigest: "",
            expectError:   true,
            errorContains: "no images found",
        },
        {
            name:     "Invalid regex - should return false",
            imageRef: "123456789.dkr.ecr.us-west-2.amazonaws.com/my-repo:latest",
            criteria: Criteria{Regex: "[invalid(regex"},
            mockSetup: func(client *mockECRClient) {
                client.describeImagesOutput = &ecr.DescribeImagesOutput{
                    ImageDetails: []types.ImageDetail{
                        {
                            ImageTags:      []string{"v1.0.0"},
                            ImagePushedAt:  aws.Time(time.Now()),
                            ImageDigest:    aws.String("sha256:digest1"),
                        },
                    },
                }
                client.getAuthorizationTokenOutput = &ecr.GetAuthorizationTokenOutput{
                    AuthorizationData: []types.AuthorizationData{
                        {
                            AuthorizationToken: aws.String("dG9rZW46cGFzc3dvcmQ="),
                            ProxyEndpoint:      aws.String("https://123456789012.dkr.ecr.us-west-2.amazonaws.com"),
                        },
                    },
                }
                client.getAuthorizationTokenErr = nil
                client.getAuthorizationTokenCallCount = 0
            },
            expectedDigest: "",
            expectError:   true,
            errorContains: "no images found matching regex",
        },
        {
            name:     "Image with no digest",
            imageRef: "123456789.dkr.ecr.us-west-2.amazonaws.com/my-repo:latest",
            criteria: Criteria{},
            mockSetup: func(client *mockECRClient) {
                client.describeImagesOutput = &ecr.DescribeImagesOutput{
                    ImageDetails: []types.ImageDetail{
                        {
                            ImageTags:      []string{"latest"},
                            ImagePushedAt:  aws.Time(time.Now().Add(-1 * time.Hour)),
                            ImageDigest:    nil, // No digest
                        },
                    },
                }
                client.getAuthorizationTokenOutput = &ecr.GetAuthorizationTokenOutput{
                    AuthorizationData: []types.AuthorizationData{
                        {
                            AuthorizationToken: aws.String("dG9rZW46cGFzc3dvcmQ="),
                            ProxyEndpoint:      aws.String("https://123456789012.dkr.ecr.us-west-2.amazonaws.com"),
                        },
                    },
                }
                client.getAuthorizationTokenErr = nil
                client.getAuthorizationTokenCallCount = 0
            },
            expectedDigest: "",
            expectError:   true,
            errorContains: "latest image has no digest",
        },
        {
            name:     "Empty image reference",
            imageRef: "",
            criteria: Criteria{},
            mockSetup: func(client *mockECRClient) {
                // Should fail during parsing
            },
            expectedDigest: "",
            expectError:   true,
            errorContains: "failed to parse image reference",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Create a new mock client for each test
            mockClient := &mockECRClient{}
            provider := NewECRProvider(mockClient)
            
            // Setup mock
            tt.mockSetup(mockClient)
            
            // Call the method
            digest, err := provider.GetLatestDigest(context.Background(), tt.imageRef, tt.criteria)
            
            // Assertions
            if tt.expectError {
                assert.Error(t, err)
                if tt.errorContains != "" {
                    assert.Contains(t, err.Error(), tt.errorContains)
                }
            } else {
                assert.NoError(t, err)
                assert.Equal(t, tt.expectedDigest, digest)
            }
        })
    }
}

// TestGetAuthToken tests the ECRProvider.GetAuthToken method with caching
func TestGetAuthToken(t *testing.T) {
    t.Run("returns cached token when valid", func(t *testing.T) {
        mockClient := &mockECRClient{
            getAuthorizationTokenOutput: &ecr.GetAuthorizationTokenOutput{
                AuthorizationData: []types.AuthorizationData{
                    {
                        AuthorizationToken: aws.String("dG9rZW46cGFzc3dvcmQ="),
                        ProxyEndpoint:        aws.String("https://example.com"),
                    },
                },
            },
            getAuthorizationTokenErr: nil,
        }
        provider := NewECRProvider(mockClient)
        // Set the cache to be valid (expiry in the future)
        provider.mu.Lock()
        provider.cachedToken = "password" // decoded from base64 "dG9rZW46cGFzc3dvcmQ="
        provider.cachedRegistryURL = "https://example.com"
        provider.tokenExpiry = time.Now().Add(1 * time.Hour)
        provider.mu.Unlock()

        // Call GetAuthToken
        registryURL, token, err := provider.GetAuthToken(context.Background())

        // Assertions
        assert.NoError(t, err)
        assert.Equal(t, "https://example.com", registryURL)
        assert.Equal(t, "password", token)
        // Ensure the mock was not called
        assert.Equal(t, 0, mockClient.getAuthorizationTokenCallCount)
    })

    t.Run("fetches new token when cache is expired", func(t *testing.T) {
        mockClient := &mockECRClient{
            getAuthorizationTokenOutput: &ecr.GetAuthorizationTokenOutput{
                AuthorizationData: []types.AuthorizationData{
                    {
                        AuthorizationToken: aws.String("dG9rZW46cGFzc3dvcmQ="),
                        ProxyEndpoint:        aws.String("https://new-example.com"),
                    },
                },
            },
            getAuthorizationTokenErr: nil,
        }
        provider := NewECRProvider(mockClient)
        // Set the cache to be valid but with less than 5 minutes left (so it will be refreshed)
        provider.mu.Lock()
        provider.cachedToken = "old-password"
        provider.cachedRegistryURL = "https://old-example.com"
        provider.tokenExpiry = time.Now().Add(4 * time.Minute) // Expires in 4 minutes (<5 min)
        provider.mu.Unlock()

        // Call GetAuthToken
        registryURL, token, err := provider.GetAuthToken(context.Background())

        // Assertions
        assert.NoError(t, err)
        assert.Equal(t, "https://new-example.com", registryURL)
        assert.Equal(t, "password", token) // decoded from base64 "dG9rZW46cGFzc3dvcmQ="
        // Ensure the mock was called once
        assert.Equal(t, 1, mockClient.getAuthorizationTokenCallCount)
        // Ensure the cache was updated
        provider.mu.RLock()
        assert.Equal(t, "password", provider.cachedToken) // decoded value
        assert.Equal(t, "https://new-example.com", provider.cachedRegistryURL)
        // The expiry should be set to about 12 hours from now (allow a small margin for test execution time)
        assert.True(t, provider.tokenExpiry.After(time.Now().Add(11*time.Hour)), "expiry should be in the future")
        provider.mu.RUnlock()
    })

    t.Run("returns error when GetAuthorizationToken fails", func(t *testing.T) {
        mockClient := &mockECRClient{
            getAuthorizationTokenErr: fmt.Errorf("aws error"),
        }
        provider := NewECRProvider(mockClient)

        // Call GetAuthToken
        _, _, err := provider.GetAuthToken(context.Background())

        // Assertions
        assert.Error(t, err)
        assert.Contains(t, err.Error(), "aws error")
        // Ensure the cache was not updated (still zero values from init)
        provider.mu.RLock()
        assert.Empty(t, provider.cachedToken)
        assert.Empty(t, provider.cachedRegistryURL)
        assert.True(t, provider.tokenExpiry.IsZero())
        provider.mu.RUnlock()
    })

    t.Run("returns error when no authorization data", func(t *testing.T) {
        mockClient := &mockECRClient{
            getAuthorizationTokenOutput: &ecr.GetAuthorizationTokenOutput{
                AuthorizationData: []types.AuthorizationData{},
            },
            getAuthorizationTokenErr: nil,
        }
        provider := NewECRProvider(mockClient)

        // Call GetAuthToken
        _, _, err := provider.GetAuthToken(context.Background())

        // Assertions
        assert.Error(t, err)
        assert.Contains(t, err.Error(), "no authorization data returned")
    })

    t.Run("returns error when authorization token is nil", func(t *testing.T) {
        mockClient := &mockECRClient{
            getAuthorizationTokenOutput: &ecr.GetAuthorizationTokenOutput{
                AuthorizationData: []types.AuthorizationData{
                    {
                        AuthorizationToken: nil,
                        ProxyEndpoint:        aws.String("https://example.com"),
                    },
                },
            },
            getAuthorizationTokenErr: nil,
        }
        provider := NewECRProvider(mockClient)

        // Call GetAuthToken
        _, _, err := provider.GetAuthToken(context.Background())

        // Assertions
        assert.Error(t, err)
        assert.Contains(t, err.Error(), "authorization token is nil")
    })

    t.Run("returns error when proxy endpoint is nil", func(t *testing.T) {
        mockClient := &mockECRClient{
            getAuthorizationTokenOutput: &ecr.GetAuthorizationTokenOutput{
                AuthorizationData: []types.AuthorizationData{
                    {
                        AuthorizationToken: aws.String("dG9rZW46cGFzc3dvcmQ="),
                        ProxyEndpoint:        nil,
                    },
                },
            },
            getAuthorizationTokenErr: nil,
        }
        provider := NewECRProvider(mockClient)

        // Call GetAuthToken
        _, _, err := provider.GetAuthToken(context.Background())

        // Assertions
        assert.Error(t, err)
        assert.Contains(t, err.Error(), "proxy endpoint is nil")
    })
}

// TestGetAuthTokenConcurrent tests that multiple concurrent calls to GetAuthToken
// only result in a single call to the underlying AWS service.
func TestGetAuthTokenConcurrent(t *testing.T) {
    // Set up a mock that returns a token after a short delay to simulate network latency
    callCount := 0
    var mu sync.Mutex
    mockClient := &mockECRClient{
        getAuthorizationTokenFn: func(ctx context.Context, input *ecr.GetAuthorizationTokenInput, optFns ...func(*ecr.Options)) (*ecr.GetAuthorizationTokenOutput, error) {
            mu.Lock()
            defer mu.Unlock()
            callCount++
            time.Sleep(10 * time.Millisecond) // Simulate delay
            return &ecr.GetAuthorizationTokenOutput{
                AuthorizationData: []types.AuthorizationData{
                    {
                        AuthorizationToken: aws.String("dG9rZW46cGFzc3dvcmQ="),
                        ProxyEndpoint:      aws.String("https://example.com"),
                    },
                },
            }, nil
        },
    }
    provider := NewECRProvider(mockClient)

    // Call GetAuthToken concurrently multiple times
    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            provider.GetAuthToken(context.Background())
        }()
    }
    wg.Wait()

    // Assert that the underlying method was called only once
    if callCount != 1 {
        t.Errorf("Expected GetAuthorizationToken to be called once, but it was called %d times", callCount)
    }
}

// mockECRClient is a mock implementation of the ECRClient interface for testing
// This is a simplified mock that only implements what's needed for GetLatestDigest and GetAuthToken tests
// In a real implementation, you would use testify/mock or similar
// For this test, we'll use a struct with function fields

type mockECRClient struct {
    describeImagesOutput *ecr.DescribeImagesOutput

    // Fields for GetAuthorizationToken mock
    getAuthorizationTokenOutput *ecr.GetAuthorizationTokenOutput
    getAuthorizationTokenErr    error
    getAuthorizationTokenFn     func(context.Context, *ecr.GetAuthorizationTokenInput, ...func(*ecr.Options)) (*ecr.GetAuthorizationTokenOutput, error)
    getAuthorizationTokenCallCount int
}

// DescribeImages is a mock implementation of the DescribeImages method
func (m *mockECRClient) DescribeImages(ctx context.Context, input *ecr.DescribeImagesInput, optFns ...func(*ecr.Options)) (*ecr.DescribeImagesOutput, error) {
    return m.describeImagesOutput, nil
}

// GetAuthorizationToken is a mock implementation of the GetAuthorizationToken method
func (m *mockECRClient) GetAuthorizationToken(ctx context.Context, input *ecr.GetAuthorizationTokenInput, optFns ...func(*ecr.Options)) (*ecr.GetAuthorizationTokenOutput, error) {
    m.getAuthorizationTokenCallCount++
    if m.getAuthorizationTokenFn != nil {
        return m.getAuthorizationTokenFn(ctx, input, optFns...)
    }
    return m.getAuthorizationTokenOutput, m.getAuthorizationTokenErr
}