package notifier

import (
	"bytes"
	"fmt"
	"text/template"
)

// Default template strings for each provider
const (
	DefaultTemplate = `Container {{.ContainerID}} updated
Image: {{.Image}}
Digest: {{.OldDigest}} → {{.NewDigest}}
Level: {{.Level}}
Time: {{(.Timestamp.In .Location).Format "2006-01-02 15:04:05 MST"}}`

	DefaultSlackTemplate = `*Container {{.ContainerID}} updated*
Image: {{.Image}}
Digest: {{.OldDigest}} → {{.NewDigest}}
Level: {{.Level}}
Time: {{(.Timestamp.In .Location).Format "2006-01-02 15:04:05 MST"}}`

	DefaultDiscordTemplate = `**Container {{.ContainerID}} updated**
Image: {{.Image}}
Digest: {{.OldDigest}} → {{.NewDigest}}
Level: {{.Level}}
Time: {{(.Timestamp.In .Location).Format "2006-01-02 15:04:05 MST"}}`

	DefaultTelegramTemplate = `*Container {{.ContainerID}} updated*
Image: {{.Image}}
Digest: {{.OldDigest}} → {{.NewDigest}}
Level: {{.Level}}
Time: {{(.Timestamp.In .Location).Format "2006-01-02 15:04:05 MST"}}`

	DefaultEmailSubjectTemplate = `Container {{.ContainerID}} updated`
	DefaultEmailBodyTemplate    = `Container {{.ContainerID}} has been updated.

Image:      {{.Image}}
Old Digest: {{.OldDigest}}
New Digest: {{.NewDigest}}
Level:      {{.Level}}
Timestamp:  {{(.Timestamp.In .Location).Format "2006-01-02 15:04:05 MST"}}`

	DefaultMSTeamsTemplate = `**Container {{.ContainerID}} updated**
Image: {{.Image}}
Digest: {{.OldDigest}} → {{.NewDigest}}
Level: {{.Level}}
Time: {{(.Timestamp.In .Location).Format "2006-01-02 15:04:05 MST"}}`

	DefaultGoogleChatTemplate = `Container *{{.ContainerID}}* updated
Image: {{.Image}}
Digest: {{.OldDigest}} → {{.NewDigest}}
Level: {{.Level}}
Time: {{(.Timestamp.In .Location).Format "2006-01-02 15:04:05 MST"}}`
)

// RenderTemplate parses and executes a Go text/template with the given notification data.
// Returns the rendered string or an error if the template is invalid.
func RenderTemplate(tmplStr string, n Notification) (string, error) {
	t, err := template.New("notification").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse notification template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, n); err != nil {
		return "", fmt.Errorf("failed to execute notification template: %w", err)
	}

	return buf.String(), nil
}

// TemplateConfig holds optional custom templates for each notification provider.
// Empty strings fall back to the provider's default template.
type TemplateConfig struct {
	Default    string `json:"default"   mapstructure:"default"`
	Slack      string `json:"slack"     mapstructure:"slack"`
	Discord    string `json:"discord"   mapstructure:"discord"`
	Telegram   string `json:"telegram"  mapstructure:"telegram"`
	Email      string `json:"email"     mapstructure:"email"`
	MSTeams    string `json:"msteams"   mapstructure:"msteams"`
	GoogleChat string `json:"google_chat" mapstructure:"google_chat"`
}

// getTemplate returns the custom template if set, otherwise the default for the provider.
func getTemplate(custom, fallback string) string {
	if custom != "" {
		return custom
	}
	return fallback
}
