package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/smtp"
	"time"
)

// Notification represents a notification to be sent
type Notification struct {
	Subject       string
	Body          string
	Level         string // "info", "warning", "error"
	Timestamp     time.Time
	Location      *time.Location
	ContainerID   string
	ContainerName string
	Image         string
	OldDigest     string
	NewDigest     string
}

// Notifier interface defines the contract for sending notifications
type Notifier interface {
	Notify(ctx context.Context, n Notification) error
}

// HTTPClient interface defines the contract for HTTP clients
// This allows mocking HTTP clients in tests
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// queuedNotification pairs a notification with its request-scoped context
type queuedNotification struct {
	n   Notification
	ctx context.Context
}

// CompositeNotifier holds multiple notifiers and dispatches notifications to all of them
type CompositeNotifier struct {
	notifiers []Notifier
	queue     chan queuedNotification
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
}

// NewCompositeNotifier creates a new CompositeNotifier with the given notifiers
func NewCompositeNotifier(notifiers ...Notifier) *CompositeNotifier {
	ctx, cancel := context.WithCancel(context.Background())
	cn := &CompositeNotifier{
		notifiers: notifiers,
		queue:     make(chan queuedNotification, 100),
		ctx:       ctx,
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	go cn.worker()
	return cn
}

// Notify queues a notification for asynchronous dispatch to all registered notifiers
func (cn *CompositeNotifier) Notify(ctx context.Context, n Notification) error {
	select {
	case cn.queue <- queuedNotification{n: n, ctx: ctx}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-cn.ctx.Done():
		return cn.ctx.Err()
	}
}

// worker processes notifications from the queue and dispatches them to all notifiers
func (cn *CompositeNotifier) worker() {
	defer close(cn.done)
	for {
		select {
		case <-cn.ctx.Done():
			return
		case qn, ok := <-cn.queue:
			if !ok {
				return
			}
			// Check the per-notification context before dispatching
			if qn.ctx.Err() != nil {
				continue
			}
			// Dispatch to all notifiers concurrently
			for _, notifier := range cn.notifiers {
				go func(n Notification, notifier Notifier, ctx context.Context) {
					notifier.Notify(ctx, n)
				}(qn.n, notifier, qn.ctx)
			}
		}
	}
}

// Close stops the composite notifier and waits for all queued notifications to be processed
func (cn *CompositeNotifier) Close() {
	cn.cancel()
	<-cn.done
}

// LogNotifier is a simple notifier that logs notifications using slog
type LogNotifier struct {
	logger   *slog.Logger
	template string
}

// NewLogNotifier creates a new LogNotifier with the given logger
func NewLogNotifier(logger *slog.Logger) *LogNotifier {
	return &LogNotifier{logger: logger, template: DefaultTemplate}
}

// NewLogNotifierWithTemplate creates a new LogNotifier with a custom template
func NewLogNotifierWithTemplate(logger *slog.Logger, tmpl string) *LogNotifier {
	return &LogNotifier{logger: logger, template: getTemplate(tmpl, DefaultTemplate)}
}

// Notify logs the notification
func (ln *LogNotifier) Notify(ctx context.Context, n Notification) error {
	var level slog.Level
	switch n.Level {
	case "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	rendered, err := RenderTemplate(ln.template, n)
	if err != nil {
		ln.logger.Error("Failed to render notification template, falling back to default", "error", err)
		rendered = fmt.Sprintf("%s\n%s", n.Subject, n.Body)
	}
	ln.logger.Log(ctx, level, rendered)
	return nil
}

// SlackNotifier sends notifications to Slack via webhook
type SlackNotifier struct {
	webhookURL string
	client     HTTPClient
	logger     *slog.Logger
	template   string
}

// NewSlackNotifier creates a new SlackNotifier with the given webhook URL and HTTP client
func NewSlackNotifier(webhookURL string, client HTTPClient) *SlackNotifier {
	return &SlackNotifier{
		webhookURL: webhookURL,
		client:     client,
		logger:     slog.Default(),
		template:   DefaultSlackTemplate,
	}
}

// NewSlackNotifierWithTemplate creates a new SlackNotifier with a custom template
func NewSlackNotifierWithTemplate(webhookURL string, client HTTPClient, tmpl string) *SlackNotifier {
	return &SlackNotifier{
		webhookURL: webhookURL,
		client:     client,
		logger:     slog.Default(),
		template:   getTemplate(tmpl, DefaultSlackTemplate),
	}
}

// Notify sends a notification to Slack
func (sn *SlackNotifier) Notify(ctx context.Context, n Notification) error {
	text, err := RenderTemplate(sn.template, n)
	if err != nil {
		sn.logger.Error("Failed to render Slack template, falling back to default", "error", err)
		text = fmt.Sprintf("%s\n%s", n.Subject, n.Body)
	}
	payload := map[string]string{
		"text": text,
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		sn.logger.Error("Failed to marshal Slack payload", "error", err, "notification", n)
		return fmt.Errorf("failed to marshal Slack payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sn.webhookURL, bytes.NewBuffer(jsonData))
	if err != nil {
		sn.logger.Error("Failed to create Slack request", "error", err, "notification", n)
		return fmt.Errorf("failed to create Slack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := sn.client.Do(req)
	if err != nil {
		sn.logger.Error("Failed to send Slack notification", "error", err, "notification", n)
		return fmt.Errorf("failed to send Slack notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		sn.logger.Error("Slack notification failed", "status", resp.StatusCode, "body", string(body), "notification", n)
		return fmt.Errorf("Slack notification failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// DiscordNotifier sends notifications to Discord via webhook
type DiscordNotifier struct {
	webhookURL string
	client     HTTPClient
	logger     *slog.Logger
	template   string
}

// NewDiscordNotifier creates a new DiscordNotifier with the given webhook URL and HTTP client
func NewDiscordNotifier(webhookURL string, client HTTPClient) *DiscordNotifier {
	return &DiscordNotifier{
		webhookURL: webhookURL,
		client:     client,
		logger:     slog.Default(),
		template:   DefaultDiscordTemplate,
	}
}

// NewDiscordNotifierWithTemplate creates a new DiscordNotifier with a custom template
func NewDiscordNotifierWithTemplate(webhookURL string, client HTTPClient, tmpl string) *DiscordNotifier {
	return &DiscordNotifier{
		webhookURL: webhookURL,
		client:     client,
		logger:     slog.Default(),
		template:   getTemplate(tmpl, DefaultDiscordTemplate),
	}
}

// Notify sends a notification to Discord
func (dn *DiscordNotifier) Notify(ctx context.Context, n Notification) error {
	text, err := RenderTemplate(dn.template, n)
	if err != nil {
		dn.logger.Error("Failed to render Discord template, falling back to default", "error", err)
		text = fmt.Sprintf("%s\n%s", n.Subject, n.Body)
	}
	payload := map[string]string{
		"content": text,
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		dn.logger.Error("Failed to marshal Discord payload", "error", err, "notification", n)
		return fmt.Errorf("failed to marshal Discord payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dn.webhookURL, bytes.NewBuffer(jsonData))
	if err != nil {
		dn.logger.Error("Failed to create Discord request", "error", err, "notification", n)
		return fmt.Errorf("failed to create Discord request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := dn.client.Do(req)
	if err != nil {
		dn.logger.Error("Failed to send Discord notification", "error", err, "notification", n)
		return fmt.Errorf("failed to send Discord notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		dn.logger.Error("Discord notification failed", "status", resp.StatusCode, "body", string(body), "notification", n)
		return fmt.Errorf("Discord notification failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// EmailNotifier sends notifications via SMTP
type EmailNotifier struct {
	host        string
	port        string
	user        string
	password    string
	from        string
	to          string
	logger      *slog.Logger
	subjectTmpl string
	bodyTmpl    string
}

// NewEmailNotifier creates a new EmailNotifier with the given SMTP configuration
func NewEmailNotifier(host, port, user, password, from, to string) *EmailNotifier {
	return &EmailNotifier{
		host:        host,
		port:        port,
		user:        user,
		password:    password,
		from:        from,
		to:          to,
		logger:      slog.Default(),
		subjectTmpl: DefaultEmailSubjectTemplate,
		bodyTmpl:    DefaultEmailBodyTemplate,
	}
}

// NewEmailNotifierWithTemplate creates a new EmailNotifier with custom templates.
// The bodyTmpl parameter is used for the email body. The subject is always derived from the subjectTmpl.
func NewEmailNotifierWithTemplate(host, port, user, password, from, to, subjectTmpl, bodyTmpl string) *EmailNotifier {
	return &EmailNotifier{
		host:        host,
		port:        port,
		user:        user,
		password:    password,
		from:        from,
		to:          to,
		logger:      slog.Default(),
		subjectTmpl: getTemplate(subjectTmpl, DefaultEmailSubjectTemplate),
		bodyTmpl:    getTemplate(bodyTmpl, DefaultEmailBodyTemplate),
	}
}

// Notify sends a notification via email
func (en *EmailNotifier) Notify(ctx context.Context, n Notification) error {
	addr := fmt.Sprintf("%s:%s", en.host, en.port)
	auth := smtp.PlainAuth("", en.user, en.password, en.host)

	subject, err := RenderTemplate(en.subjectTmpl, n)
	if err != nil {
		en.logger.Error("Failed to render email subject template, falling back to default", "error", err)
		subject = n.Subject
	}

	body, err := RenderTemplate(en.bodyTmpl, n)
	if err != nil {
		en.logger.Error("Failed to render email body template, falling back to default", "error", err)
		body = fmt.Sprintf("%s\n%s", n.Subject, n.Body)
	}

	msg := []byte(fmt.Sprintf("Subject: %s\n\n%s", subject, body))

	err = smtp.SendMail(addr, auth, en.from, []string{en.to}, msg)
	if err != nil {
		en.logger.Error("Failed to send email notification", "error", err, "notification", n)
		return fmt.Errorf("failed to send email notification: %w", err)
	}

	return nil
}

// TelegramNotifier sends notifications to Telegram via bot API
type TelegramNotifier struct {
	botToken string
	chatID   string
	client   HTTPClient
	logger   *slog.Logger
	template string
}

// NewTelegramNotifier creates a new TelegramNotifier with the given bot token, chat ID, and HTTP client
func NewTelegramNotifier(botToken, chatID string, client HTTPClient) *TelegramNotifier {
	return &TelegramNotifier{
		botToken: botToken,
		chatID:   chatID,
		client:   client,
		logger:   slog.Default(),
		template: DefaultTelegramTemplate,
	}
}

// NewTelegramNotifierWithTemplate creates a new TelegramNotifier with a custom template
func NewTelegramNotifierWithTemplate(botToken, chatID string, client HTTPClient, tmpl string) *TelegramNotifier {
	return &TelegramNotifier{
		botToken: botToken,
		chatID:   chatID,
		client:   client,
		logger:   slog.Default(),
		template: getTemplate(tmpl, DefaultTelegramTemplate),
	}
}

// Notify sends a notification to Telegram
func (tn *TelegramNotifier) Notify(ctx context.Context, n Notification) error {
	text, err := RenderTemplate(tn.template, n)
	if err != nil {
		tn.logger.Error("Failed to render Telegram template, falling back to default", "error", err)
		text = fmt.Sprintf("%s\n%s", n.Subject, n.Body)
	}
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", tn.botToken)
	payload := map[string]string{
		"chat_id":    tn.chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		tn.logger.Error("Failed to marshal Telegram payload", "error", err, "notification", n)
		return fmt.Errorf("failed to marshal Telegram payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		tn.logger.Error("Failed to create Telegram request", "error", err, "notification", n)
		return fmt.Errorf("failed to create Telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := tn.client.Do(req)
	if err != nil {
		tn.logger.Error("Failed to send Telegram notification", "error", err, "notification", n)
		return fmt.Errorf("failed to send Telegram notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		tn.logger.Error("Telegram notification failed", "status", resp.StatusCode, "body", string(body), "notification", n)
		return fmt.Errorf("Telegram notification failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// MSTeamsNotifier sends notifications to Microsoft Teams via webhook
type MSTeamsNotifier struct {
	webhookURL string
	client     HTTPClient
	logger     *slog.Logger
	template   string
}

// NewMSTeamsNotifier creates a new MSTeamsNotifier with the given webhook URL and HTTP client
func NewMSTeamsNotifier(webhookURL string, client HTTPClient) *MSTeamsNotifier {
	return &MSTeamsNotifier{
		webhookURL: webhookURL,
		client:     client,
		logger:     slog.Default(),
		template:   DefaultMSTeamsTemplate,
	}
}

// NewMSTeamsNotifierWithTemplate creates a new MSTeamsNotifier with a custom template
func NewMSTeamsNotifierWithTemplate(webhookURL string, client HTTPClient, tmpl string) *MSTeamsNotifier {
	return &MSTeamsNotifier{
		webhookURL: webhookURL,
		client:     client,
		logger:     slog.Default(),
		template:   getTemplate(tmpl, DefaultMSTeamsTemplate),
	}
}

// Notify sends a notification to Microsoft Teams
func (mn *MSTeamsNotifier) Notify(ctx context.Context, n Notification) error {
	text, err := RenderTemplate(mn.template, n)
	if err != nil {
		mn.logger.Error("Failed to render MSTeams template, falling back to default", "error", err)
		text = fmt.Sprintf("%s\n%s", n.Subject, n.Body)
	}
	payload := map[string]interface{}{
		"@type":    "MessageCard",
		"@context": "http://schema.org/extensions",
		"text":     text,
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		mn.logger.Error("Failed to marshal MSTeams payload", "error", err, "notification", n)
		return fmt.Errorf("failed to marshal MSTeams payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mn.webhookURL, bytes.NewBuffer(jsonData))
	if err != nil {
		mn.logger.Error("Failed to create MSTeams request", "error", err, "notification", n)
		return fmt.Errorf("failed to create MSTeams request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := mn.client.Do(req)
	if err != nil {
		mn.logger.Error("Failed to send MSTeams notification", "error", err, "notification", n)
		return fmt.Errorf("failed to send MSTeams notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		mn.logger.Error("MSTeams notification failed", "status", resp.StatusCode, "body", string(body), "notification", n)
		return fmt.Errorf("MSTeams notification failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// GoogleChatNotifier sends notifications to Google Chat via webhook
type GoogleChatNotifier struct {
	webhookURL string
	client     HTTPClient
	logger     *slog.Logger
	template   string
}

// NewGoogleChatNotifier creates a new GoogleChatNotifier with the given webhook URL and HTTP client
func NewGoogleChatNotifier(webhookURL string, client HTTPClient) *GoogleChatNotifier {
	return &GoogleChatNotifier{
		webhookURL: webhookURL,
		client:     client,
		logger:     slog.Default(),
		template:   DefaultGoogleChatTemplate,
	}
}

// NewGoogleChatNotifierWithTemplate creates a new GoogleChatNotifier with a custom template
func NewGoogleChatNotifierWithTemplate(webhookURL string, client HTTPClient, tmpl string) *GoogleChatNotifier {
	return &GoogleChatNotifier{
		webhookURL: webhookURL,
		client:     client,
		logger:     slog.Default(),
		template:   getTemplate(tmpl, DefaultGoogleChatTemplate),
	}
}

// Notify sends a notification to Google Chat
func (gcn *GoogleChatNotifier) Notify(ctx context.Context, n Notification) error {
	text, err := RenderTemplate(gcn.template, n)
	if err != nil {
		gcn.logger.Error("Failed to render Google Chat template, falling back to default", "error", err)
		text = fmt.Sprintf("%s\n%s", n.Subject, n.Body)
	}
	payload := map[string]string{
		"text": text,
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		gcn.logger.Error("Failed to marshal Google Chat payload", "error", err, "notification", n)
		return fmt.Errorf("failed to marshal Google Chat payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gcn.webhookURL, bytes.NewBuffer(jsonData))
	if err != nil {
		gcn.logger.Error("Failed to create Google Chat request", "error", err, "notification", n)
		return fmt.Errorf("failed to create Google Chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := gcn.client.Do(req)
	if err != nil {
		gcn.logger.Error("Failed to send Google Chat notification", "error", err, "notification", n)
		return fmt.Errorf("failed to send Google Chat notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		gcn.logger.Error("Google Chat notification failed", "status", resp.StatusCode, "body", string(body), "notification", n)
		return fmt.Errorf("Google Chat notification failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
