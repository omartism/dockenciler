# Notifications

Dockenciler can notify you when a container is updated. The notification system is a **composite** — all configured providers receive the same notification concurrently. The **Log notifier is always-on** and emits structured logs via `slog`; the other six are optional and enabled by configuration.

All seven providers live in a single file: `pkg/notifier/notifier.go` (551 lines).

## Quick reference

| Provider | Required config | Setup guide |
|---|---|---|
| Log | Always-on (no config needed) | Not applicable — always active |
| Slack | `notifications.slack_webhook_url` | [Slack](#slack) |
| Discord | `notifications.discord_webhook_url` | [Discord](#discord) |
| Telegram | `notifications.telegram_bot_token` + `notifications.telegram_chat_id` | [Telegram](#telegram) |
| Email | `notifications.email_host` + `notifications.email_port` + `notifications.email_user` + `notifications.email_password` + `notifications.email_from` + `notifications.email_to` | [Email](#email) |
| MS Teams | `notifications.msteams_webhook_url` | [MS Teams](#ms-teams) |
| Google Chat | `notifications.google_chat_webhook_url` | [Google Chat](#google-chat) |

The full configuration schema (JSON paths, env var names, defaults) is in the [Configuration reference](configuration.md#notifications).

## Setup guides

### Slack

1. Go to your Slack workspace's [Incoming Webhooks](https://slack.com/apps/A0F7XDUAZ-incoming-webhooks) page.
2. Click **Add to Slack**, select a channel, and click **Add Incoming Webhooks integration**.
3. Copy the Webhook URL (starts with `https://hooks.slack.com/services/...`).

Configure Dockenciler:

```json
{
  "notifications": {
    "slack_webhook_url": "https://hooks.slack.com/services/your-webhook-url"
  }
}
```

Or via environment variable:

```bash
NOTIFICATIONS_SLACK_WEBHOOK_URL=https://hooks.slack.com/services/your-webhook-url
```

### Discord

1. Open your Discord server settings and go to **Integrations** > **Webhooks**.
2. Click **Create Webhook**, give it a name, and select the channel.
3. Copy the Webhook URL (starts with `https://discord.com/api/webhooks/...`).

```json
{
  "notifications": {
    "discord_webhook_url": "https://discord.com/api/webhooks/..."
  }
}
```

### Telegram

1. Open Telegram and search for **@BotFather**.
2. Send `/newbot` and follow the prompts to create a new bot. BotFather will give you a **bot token** (looks like `123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11`).
3. Start a chat with your new bot and send any message.
4. Visit `https://api.telegram.org/bot<YOUR_BOT_TOKEN>/getUpdates` to find the `chat_id` (the `"chat":{"id":...}` field).

```json
{
  "notifications": {
    "telegram_bot_token": "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
    "telegram_chat_id": "123456789"
  }
}
```

Or via environment variables:

```bash
NOTIFICATIONS_TELEGRAM_BOT_TOKEN=123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11
NOTIFICATIONS_TELEGRAM_CHAT_ID=123456789
```

### Email

Email uses SMTP with plain authentication. The SMTP configuration fields are set directly on the `notifications` block — there is no `email_smtp_` infix.

```json
{
  "notifications": {
    "email_host": "smtp.example.com",
    "email_port": "587",
    "email_user": "user@example.com",
    "email_password": "app-password",
    "email_from": "dockenciler@example.com",
    "email_to": "admin@example.com"
  }
}
```

If `email_port` is not set, there is no default — the provider requires it. Typical values are `587` (STARTTLS) and `465` (SSL). The provider uses `smtp.PlainAuth` (`notifier.go:319`) for authentication.

### MS Teams

1. In Microsoft Teams, go to the channel where you want notifications.
2. Click the **...** (More options) menu > **Connectors** > **Incoming Webhook**.
3. Click **Add**, give it a name, and copy the Webhook URL (starts with `https://<tenant>.webhook.office.com/...`).

```json
{
  "notifications": {
    "msteams_webhook_url": "https://tenant.webhook.office.com/..."
  }
}
```

### Google Chat

1. In Google Chat, go to the space where you want notifications.
2. Click the space name > **Configure webhooks**.
3. Click **Add webhook**, give it a name, and copy the URL (starts with `https://chat.googleapis.com/v1/spaces/...`).

```json
{
  "notifications": {
    "google_chat_webhook_url": "https://chat.googleapis.com/v1/spaces/..."
  }
}
```

Or via environment variable:

```bash
NOTIFICATIONS_GOOGLE_CHAT_WEBHOOK_URL=https://chat.googleapis.com/v1/spaces/...
```

## Log notifier (always-on)

The Log notifier is always active. It renders the notification template and emits it via `slog.Logger.Log()` at the appropriate level (`info`, `warning`, or `error`) (`pkg/notifier/notifier.go:119-136`).

The log output is structured text (slog text-handler format, key=value pairs) when `COLOR_LOGS=false`, or human-readable with colors (default, TTY only). Log level is controlled by `LOG_LEVEL`.

## Templates

All notification messages are rendered using Go's `text/template` syntax (`pkg/notifier/template.go:59-71`). Each provider has a built-in default template.

### Template fields

Templates have access to 10 fields: `{{.ContainerID}}`, `{{.ContainerName}}` (reserved, currently empty), `{{.Image}}`, `{{.OldDigest}}`, `{{.NewDigest}}`, `{{.Level}}`, `{{.Timestamp}}`, `{{.Location}}`, `{{.Subject}}`, `{{.Body}}`. For the full field reference and template syntax rules, see [Configuration → Notifications and template configuration](configuration.md#notifications-and-template-configuration).

**Note on `ContainerName`:** The field exists in the struct but the reconciler does not populate it (`pkg/reconciler/reconciler.go:265-276`). It is reserved for future use and defaults to empty.

### Template priority (cascade)

The template cascade differs between the Log notifier and the other six providers. This is determined by how the notifiers are wired in `cmd/dockenciler/main.go:166-209`.

- **Log notifier:** Full cascade. Receives `templates.default` directly (`cmd/dockenciler/main.go:169`). Resolution: per-provider template (which is `templates.default`) > built-in `DefaultTemplate`.
- **All other notifiers (Slack, Discord, Telegram, Email, MS Teams, Google Chat):** Partial cascade. Each receives its own template field (`templates.slack`, `templates.discord`, etc.). Resolution: per-provider template > built-in provider-specific default. The `templates.default` field is **not consulted** for these notifiers.

The full cascade table is in the [Configuration reference](configuration.md#template-priority).

### Email subject

**The email subject is not user-configurable.** The subject always uses the built-in `DefaultEmailSubjectTemplate` (`pkg/notifier/template.go:35`):

```
Container {{.ContainerID}} updated
```

The `NewEmailNotifierWithTemplate` constructor at `notifier.go:302-314` accepts both a `subjectTmpl` and `bodyTmpl` parameter, but the reconciler at `cmd/dockenciler/main.go:198-206` passes an empty string for the subject template, causing it to fall back to the built-in default. Only the email body is customizable via `templates.email`.

### Template syntax reference

Templates use Go's `text/template` syntax. The following operations are available:

- **Field access:** `{{.FieldName}}` — prints the field value (e.g. `{{.ContainerID}}`).
- **Method calls:** `{{(.Timestamp.In .Location).Format "2006-01-02 15:04:05 MST"}}` — the default templates use this pattern to format timestamps in the configured timezone.
- **Pipelines:** `{{.FieldName | func}}` — chain transformations (not used in defaults but available in custom templates).
- **Conditionals:** `{{if .OldDigest}}{{.OldDigest}}{{end}}` — conditionally include content.

All 7 default templates use the same time formatting pattern. See `pkg/notifier/template.go:11-55` for the full default template strings.

### Custom template example

The following example overrides the Telegram and Slack templates with a custom message:

```json
{
  "notifications": {
    "telegram_bot_token": "...",
    "telegram_chat_id": "...",
    "slack_webhook_url": "...",
    "templates": {
      "slack": "*Container updated*\nImage: {{.Image}}\nFrom: {{.OldDigest}}\nTo: {{.NewDigest}}",
      "telegram": "*Container updated*\nImage: {{.Image}}\nFrom: {{.OldDigest}}\nTo: {{.NewDigest}}"
    }
  }
}
```

Or via environment variables:

```bash
NOTIFICATIONS_TEMPLATES_SLACK="*Container updated*\nImage: {{.Image}}\nFrom: {{.OldDigest}}\nTo: {{.NewDigest}}"
```

## Composite notifier architecture

All configured notifiers are wrapped in a `CompositeNotifier` (`pkg/notifier/notifier.go:41-61`). When a notification is produced:

1. The reconciler calls `Notifier.Notify(ctx, notification)` at `pkg/reconciler/reconciler.go:277`.
2. The notification is enqueued into a buffered channel (capacity 100, `notifier.go:54`).
3. A background worker (`notifier.go:76-94`) reads from the queue and dispatches each notification to all registered notifiers concurrently (one goroutine per notifier).
4. Each notifier renders its template and sends the message via its protocol (HTTP POST, SMTP, or `slog.Log`).

The queue is buffered to avoid blocking the reconciliation loop on slow notification delivery. If the queue is full, `Notify` respects context cancellation — either the caller's context or the composite notifier's internal context.

### Shutdown behavior

Calling `Close()` on the composite notifier (`notifier.go:97-100`) cancels the internal context and blocks until the worker goroutine finishes, ensuring all queued notifications are flushed before the process exits. The reconciler does not currently call `Close()` — the notifier is terminated by process exit on SIGINT/SIGTERM.

## Troubleshooting notifications

| Symptom | Likely cause |
|---|---|
| Slack/Discord/MS Teams/Google Chat not sending | Webhook URL is invalid, unreachable, or the provider returned a non-2xx status code. Check the logs for `"Slack notification failed"` or equivalent (`notifier.go:196-200`). |
| Telegram not sending | `bot_token` or `chat_id` is wrong, or the bot was blocked by the user. The provider posts to `https://api.telegram.org/bot<token>/sendMessage` (`notifier.go:382`). |
| Email not sending | SMTP credentials are wrong, port is unreachable, or the email server rejects plain auth. The provider uses `smtp.PlainAuth` (`notifier.go:319`). Try `email_host=localhost` and `email_port=1025` with a local SMTP server like MailHog for testing. |
| Notification template renders as empty or incorrect | Template syntax error causes a fallback to the default template (logged at `slog.Error` level). Check template escaping: JSON strings require `\\n` for newlines. |
| No notifications at all | The reconciler constructs `Subject` and `Body` from hardcoded strings. If reconciliation errors before the `Notify` call (`pkg/reconciler/reconciler.go:277`), no notification is produced. |

## See also

- [Configuration reference](configuration.md#notifications-and-template-configuration) — full env-var table, template cascade table, and field descriptions.
- [Operations](operations.md) — log levels, dry-run mode.
- [Security](security.md#notification-secrets) — handling webhook URLs and bot tokens securely.
- [Examples](examples/README.md) — multi-notifier config example.
