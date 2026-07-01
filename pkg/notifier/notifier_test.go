package notifier

import (
    "context"
    "encoding/json"
    "io"
    "log/slog"
    "net/http"
    "strings"
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// MockNotifier is a mock implementation of the Notifier interface for testing
type MockNotifier struct {
    notifications []Notification
    mu            chan struct{}
}

func NewMockNotifier() *MockNotifier {
    return &MockNotifier{
        notifications: make([]Notification, 0),
        mu:            make(chan struct{}, 1),
    }
}

func (m *MockNotifier) Notify(ctx context.Context, n Notification) error {
    m.mu <- struct{}{}
    m.notifications = append(m.notifications, n)
    <-m.mu
    return nil
}

func TestLogNotifier(t *testing.T) {
    // Create a mock logger that captures log messages
    var logMessages []string
    mockLogger := slog.New(slog.NewTextHandler(&testLogWriter{&logMessages}, nil))

    // Create a LogNotifier with the mock logger
    notifier := NewLogNotifier(mockLogger)

    // Create a test notification
    n := Notification{
        Subject:    "Test Subject",
        Body:       "Test Body",
        Level:      "info",
        Timestamp:  time.Now(),
    }

    // Notify
    err := notifier.Notify(context.Background(), n)
    require.NoError(t, err)

    // Verify the notification was logged
    assert.Len(t, logMessages, 1)
    assert.Contains(t, logMessages[0], "Test Subject")
    assert.Contains(t, logMessages[0], "Test Body")
}

func TestCompositeNotifier_Dispatch(t *testing.T) {
    // Create multiple mock notifiers
    notifier1 := NewMockNotifier()
    notifier2 := NewMockNotifier()
    notifier3 := NewMockNotifier()

    // Create a CompositeNotifier with the mock notifiers
    composite := NewCompositeNotifier(notifier1, notifier2, notifier3)
    defer composite.Close()

    // Create a test notification
    n := Notification{
        Subject:    "Test Subject",
        Body:       "Test Body",
        Level:      "warning",
        Timestamp:  time.Now(),
    }

    // Notify
    err := composite.Notify(context.Background(), n)
    require.NoError(t, err)

    // Wait for all notifiers to receive the notification
    time.Sleep(100 * time.Millisecond)

    // Verify all notifiers received the notification
    assert.Len(t, notifier1.notifications, 1)
    assert.Len(t, notifier2.notifications, 1)
    assert.Len(t, notifier3.notifications, 1)

    // Verify the notification content
    assert.Equal(t, n.Subject, notifier1.notifications[0].Subject)
    assert.Equal(t, n.Body, notifier1.notifications[0].Body)
    assert.Equal(t, n.Level, notifier1.notifications[0].Level)
}

func TestCompositeNotifier_ContextCancellation(t *testing.T) {
    // Create a mock notifier that blocks
    blockingNotifier := NewMockNotifier()

    // Create a CompositeNotifier with the blocking notifier
    composite := NewCompositeNotifier(blockingNotifier)
    defer composite.Close()

    // Create a cancellable context
    ctx, cancel := context.WithCancel(context.Background())

    // Send a notification
    n := Notification{
        Subject:    "Test Subject",
        Body:       "Test Body",
        Level:      "info",
        Timestamp:  time.Now(),
    }
    err := composite.Notify(ctx, n)
    require.NoError(t, err)

    // Cancel the context
    cancel()

    // Wait for the notification to be processed (or timeout)
    select {
    case <-time.After(500 * time.Millisecond):
        // Timeout - notification was not processed
        t.Log("Notification was not processed due to context cancellation")
    default:
        // Notification was processed
    }

    // Verify the notification was not added to the notifier
    assert.Len(t, blockingNotifier.notifications, 0)
}

func TestSlackNotifier(t *testing.T) {
    // Create a mock HTTP client to intercept requests
    mockClient := &mockHTTPClient{
        responses: []mockHTTPResponse{
            {statusCode: 200, body: "ok"},
        },
    }

    // Create a SlackNotifier with the mock client
    notifier := NewSlackNotifier("https://hooks.slack.com/services/test", mockClient)

    // Create a test notification
    n := Notification{
        Subject:    "Test Subject",
        Body:       "Test Body",
        Level:      "info",
        Timestamp:  time.Now(),
    }

    // Notify
    err := notifier.Notify(context.Background(), n)
    require.NoError(t, err)

    // Verify the request was made
    assert.Len(t, mockClient.requests, 1)
    req := mockClient.requests[0]
    assert.Equal(t, "POST", req.method)
    assert.Equal(t, "https://hooks.slack.com/services/test", req.url)
    assert.Equal(t, "application/json", req.headers["Content-Type"])

    // Verify the payload
    var payload map[string]string
    json.Unmarshal([]byte(req.body), &payload)
    assert.Equal(t, "Test Subject\nTest Body", payload["text"])
}

// TestDiscordNotifier tests the DiscordNotifier implementation
func TestDiscordNotifier(t *testing.T) {
    // Create a mock HTTP client to intercept requests
    mockClient := &mockHTTPClient{
        responses: []mockHTTPResponse{
            {statusCode: 200, body: "ok"},
        },
    }

    // Create a DiscordNotifier with the mock client
    notifier := NewDiscordNotifier("https://discord.com/api/webhooks/test", mockClient)

    // Create a test notification
    n := Notification{
        Subject:    "Test Subject",
        Body:       "Test Body",
        Level:      "info",
        Timestamp:  time.Now(),
    }

    // Notify
    err := notifier.Notify(context.Background(), n)
    require.NoError(t, err)

    // Verify the request was made
    assert.Len(t, mockClient.requests, 1)
    req := mockClient.requests[0]
    assert.Equal(t, "POST", req.method)
    assert.Equal(t, "https://discord.com/api/webhooks/test", req.url)
    assert.Equal(t, "application/json", req.headers["Content-Type"])

    // Verify the payload
    var payload map[string]string
    json.Unmarshal([]byte(req.body), &payload)
    assert.Equal(t, "Test Subject\nTest Body", payload["content"])
}

// TestEmailNotifier tests the EmailNotifier implementation
func TestEmailNotifier(t *testing.T) {
    // Create an EmailNotifier with test configuration
    notifier := &EmailNotifier{
        host:     "smtp.example.com",
        port:     "587",
        user:     "test@example.com",
        password: "password",
        from:     "test@example.com",
        to:       "recipient@example.com",
        logger:   slog.Default(),
    }

    // Create a test notification
    n := Notification{
        Subject:    "Test Subject",
        Body:       "Test Body",
        Level:      "info",
        Timestamp:  time.Now(),
    }

    // Notify - we can't easily test the actual SMTP sending without a real server
    // So we'll just verify the method exists and can be called
    err := notifier.Notify(context.Background(), n)
    // This will fail because there's no SMTP server, but that's expected
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "failed to send email notification")
}

// TestTelegramNotifier tests the TelegramNotifier implementation
func TestTelegramNotifier(t *testing.T) {
    // Create a mock HTTP client to intercept requests
    mockClient := &mockHTTPClient{
        responses: []mockHTTPResponse{
            {statusCode: 200, body: "{\"ok\": true, \"result\": {}}"},
        },
    }

    // Create a TelegramNotifier with the mock client
    notifier := NewTelegramNotifier("123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11", "123456789", mockClient)

    // Create a test notification
    n := Notification{
        Subject:    "Test Subject",
        Body:       "Test Body",
        Level:      "info",
        Timestamp:  time.Now(),
    }

    // Notify
    err := notifier.Notify(context.Background(), n)
    require.NoError(t, err)

    // Verify the request was made
    assert.Len(t, mockClient.requests, 1)
    req := mockClient.requests[0]
    assert.Equal(t, "POST", req.method)
    assert.Equal(t, "https://api.telegram.org/bot123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11/sendMessage", req.url)
    assert.Equal(t, "application/json", req.headers["Content-Type"])

    // Verify the payload
    var payload map[string]string
    json.Unmarshal([]byte(req.body), &payload)
    assert.Equal(t, "123456789", payload["chat_id"])
    assert.Equal(t, "Test Subject\nTest Body", payload["text"])
    assert.Equal(t, "Markdown", payload["parse_mode"])
}

// TestMSTeamsNotifier tests the MSTeamsNotifier implementation
func TestMSTeamsNotifier(t *testing.T) {
    // Create a mock HTTP client to intercept requests
    mockClient := &mockHTTPClient{
        responses: []mockHTTPResponse{
            {statusCode: 200, body: "ok"},
        },
    }

    // Create a MSTeamsNotifier with the mock client
    notifier := NewMSTeamsNotifier("https://outlook.office.com/webhook/test", mockClient)

    // Create a test notification
    n := Notification{
        Subject:    "Test Subject",
        Body:       "Test Body",
        Level:      "info",
        Timestamp:  time.Now(),
    }

    // Notify
    err := notifier.Notify(context.Background(), n)
    require.NoError(t, err)

    // Verify the request was made
    assert.Len(t, mockClient.requests, 1)
    req := mockClient.requests[0]
    assert.Equal(t, "POST", req.method)
    assert.Equal(t, "https://outlook.office.com/webhook/test", req.url)
    assert.Equal(t, "application/json", req.headers["Content-Type"])

    // Verify the payload
    var payload map[string]interface{}
    json.Unmarshal([]byte(req.body), &payload)
    assert.Equal(t, "MessageCard", payload["@type"])
    assert.Equal(t, "http://schema.org/extensions", payload["@context"])
    assert.Equal(t, "Test Subject\nTest Body", payload["text"])
}

// TestGoogleChatNotifier tests the GoogleChatNotifier implementation
func TestGoogleChatNotifier(t *testing.T) {
    // Create a mock HTTP client to intercept requests
    mockClient := &mockHTTPClient{
        responses: []mockHTTPResponse{
            {statusCode: 200, body: "ok"},
        },
    }

    // Create a GoogleChatNotifier with the mock client
    notifier := NewGoogleChatNotifier("https://chat.googleapis.com/v1/spaces/test/messages", mockClient)

    // Create a test notification
    n := Notification{
        Subject:    "Test Subject",
        Body:       "Test Body",
        Level:      "info",
        Timestamp:  time.Now(),
    }

    // Notify
    err := notifier.Notify(context.Background(), n)
    require.NoError(t, err)

    // Verify the request was made
    assert.Len(t, mockClient.requests, 1)
    req := mockClient.requests[0]
    assert.Equal(t, "POST", req.method)
    assert.Equal(t, "https://chat.googleapis.com/v1/spaces/test/messages", req.url)
    assert.Equal(t, "application/json", req.headers["Content-Type"])

    // Verify the payload
    var payload map[string]string
    json.Unmarshal([]byte(req.body), &payload)
    assert.Equal(t, "Test Subject\nTest Body", payload["text"])
}

// mockHTTPClient is a mock HTTP client for testing
type mockHTTPClient struct {
    requests  []mockHTTPRequest
    responses []mockHTTPResponse
    index     int
}

// mockHTTPRequest stores request information for testing
type mockHTTPRequest struct {
    method  string
    url     string
    headers map[string]string
    body    string
}

// mockHTTPResponse stores response information for testing
type mockHTTPResponse struct {
    statusCode int
    body       string
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
    // Read the request body
    body, _ := io.ReadAll(req.Body)

    // Store the request
    m.requests = append(m.requests, mockHTTPRequest{
        method:  req.Method,
        url:     req.URL.String(),
        headers: make(map[string]string),
        body:    string(body),
    })
    for k, v := range req.Header {
        if len(v) > 0 {
            m.requests[len(m.requests)-1].headers[k] = v[0]
        }
    }

    // Return a mock response
    if m.index >= len(m.responses) {
        m.index = 0
    }
    resp := m.responses[m.index]
    m.index++

    return &http.Response{
        StatusCode: resp.statusCode,
        Body:       io.NopCloser(strings.NewReader(resp.body)),
        Header:     make(http.Header),
    }, nil
}

// testLogWriter is a simple io.Writer that captures log messages
type testLogWriter struct {
    messages *[]string
}

func (w *testLogWriter) Write(p []byte) (n int, err error) {
    *w.messages = append(*w.messages, string(p))
    return len(p), nil
}