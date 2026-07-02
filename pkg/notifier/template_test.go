package notifier

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testNotification() Notification {
	return Notification{
		Subject:     "Container abc123 updated",
		Body:        "Container abc123 was updated from digest sha256:old to sha256:new",
		Level:       "info",
		Timestamp:   time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		ContainerID: "abc123",
		Image:       "myregistry.com/myapp:v1.2.3",
		OldDigest:   "sha256:old",
		NewDigest:   "sha256:new",
	}
}

func TestRenderTemplate_Default(t *testing.T) {
	n := testNotification()
	result, err := RenderTemplate(DefaultTemplate, n)
	require.NoError(t, err)
	assert.Contains(t, result, "Container abc123 updated")
	assert.Contains(t, result, "myregistry.com/myapp:v1.2.3")
	assert.Contains(t, result, "sha256:old")
	assert.Contains(t, result, "sha256:new")
	assert.Contains(t, result, "info")
	assert.Contains(t, result, "2025-01-15 10:30:00 UTC")
}

func TestRenderTemplate_CustomSlack(t *testing.T) {
	n := testNotification()
	result, err := RenderTemplate(DefaultSlackTemplate, n)
	require.NoError(t, err)
	assert.Contains(t, result, "*Container abc123 updated*")
	assert.Contains(t, result, "sha256:old → sha256:new")
}

func TestRenderTemplate_CustomTemplate(t *testing.T) {
	n := testNotification()
	customTmpl := `Alert: {{.ContainerID}} on {{.Image}} ({{.Level}})`
	result, err := RenderTemplate(customTmpl, n)
	require.NoError(t, err)
	assert.Equal(t, "Alert: abc123 on myregistry.com/myapp:v1.2.3 (info)", result)
}

func TestRenderTemplate_InvalidTemplate(t *testing.T) {
	n := testNotification()
	_, err := RenderTemplate("{{.InvalidField}}", n)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "template")
}

func TestRenderTemplate_SyntaxError(t *testing.T) {
	n := testNotification()
	_, err := RenderTemplate("{{if}}", n)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}

func TestGetTemplate_CustomOverride(t *testing.T) {
	result := getTemplate("custom", "fallback")
	assert.Equal(t, "custom", result)
}

func TestGetTemplate_Fallback(t *testing.T) {
	result := getTemplate("", "fallback")
	assert.Equal(t, "fallback", result)
}

func TestRenderTemplate_EmptyFields(t *testing.T) {
	n := Notification{
		Subject:   "Test",
		Body:      "Body",
		Level:     "info",
		Timestamp: time.Now(),
	}
	tmpl := `ID: {{.ContainerID}} Image: {{.Image}} Old: {{.OldDigest}} New: {{.NewDigest}}`
	result, err := RenderTemplate(tmpl, n)
	require.NoError(t, err)
	assert.Equal(t, "ID:  Image:  Old:  New: ", result)
}

func TestSlackNotifierWithTemplate(t *testing.T) {
	mockClient := &mockHTTPClient{
		responses: []mockHTTPResponse{{statusCode: 200, body: "ok"}},
	}

	notifier := NewSlackNotifierWithTemplate("https://hooks.slack.com/test", mockClient, `*{{.ContainerID}}* updated`)
	n := testNotification()
	err := notifier.Notify(context.Background(), n)
	require.NoError(t, err)

	var payload map[string]string
	json.Unmarshal([]byte(mockClient.requests[0].body), &payload)
	assert.Equal(t, "*abc123* updated", payload["text"])
}

func TestDiscordNotifierWithTemplate(t *testing.T) {
	mockClient := &mockHTTPClient{
		responses: []mockHTTPResponse{{statusCode: 200, body: "ok"}},
	}

	notifier := NewDiscordNotifierWithTemplate("https://discord.com/api/test", mockClient, `**{{.ContainerID}}** on {{.Image}}`)
	n := testNotification()
	err := notifier.Notify(context.Background(), n)
	require.NoError(t, err)

	var payload map[string]string
	json.Unmarshal([]byte(mockClient.requests[0].body), &payload)
	assert.Equal(t, "**abc123** on myregistry.com/myapp:v1.2.3", payload["content"])
}

func TestTelegramNotifierWithTemplate(t *testing.T) {
	mockClient := &mockHTTPClient{
		responses: []mockHTTPResponse{{statusCode: 200, body: `{"ok": true}`}},
	}

	notifier := NewTelegramNotifierWithTemplate("123:ABC", "12345", mockClient, `*{{.ContainerID}}* {{.Level}}`)
	n := testNotification()
	err := notifier.Notify(context.Background(), n)
	require.NoError(t, err)

	var payload map[string]string
	json.Unmarshal([]byte(mockClient.requests[0].body), &payload)
	assert.Equal(t, "*abc123* info", payload["text"])
}

func TestMSTeamsNotifierWithTemplate(t *testing.T) {
	mockClient := &mockHTTPClient{
		responses: []mockHTTPResponse{{statusCode: 200, body: "ok"}},
	}

	notifier := NewMSTeamsNotifierWithTemplate("https://teams.webhook/test", mockClient, `Update: {{.ContainerID}}`)
	n := testNotification()
	err := notifier.Notify(context.Background(), n)
	require.NoError(t, err)

	var payload map[string]interface{}
	json.Unmarshal([]byte(mockClient.requests[0].body), &payload)
	assert.Equal(t, "Update: abc123", payload["text"])
}

func TestGoogleChatNotifierWithTemplate(t *testing.T) {
	mockClient := &mockHTTPClient{
		responses: []mockHTTPResponse{{statusCode: 200, body: "ok"}},
	}

	notifier := NewGoogleChatNotifierWithTemplate("https://chat.googleapis.com/test", mockClient, `*{{.ContainerID}}* updated`)
	n := testNotification()
	err := notifier.Notify(context.Background(), n)
	require.NoError(t, err)

	var payload map[string]string
	json.Unmarshal([]byte(mockClient.requests[0].body), &payload)
	assert.Equal(t, "*abc123* updated", payload["text"])
}
