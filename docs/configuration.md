# Configuration

## How configuration is loaded

Dockenciler uses Viper (`github.com/spf13/viper`) for configuration. The loading order is:

1. **Defaults** set by `v.SetDefault()` in `pkg/config/config.go:97-129`.
2. **JSON config file** (optional) — path is the first and only CLI positional argument.
3. **Environment variables** — all prefixed with `DOCKENCILER_`; nested keys use `_` as a separator (`pkg/config/config.go:94` and `pkg/config/config.go:132`).

If both the JSON file and an environment variable set the same value, **the environment variable wins**.

## CLI flags

Dockenciler takes **zero flags**. The only positional argument is the path to a JSON config file:

```bash
dockenciler /path/to/config.json
```

Without an argument, Dockenciler runs on defaults plus environment variables only (`cmd/dockenciler/main.go:109-114`).

There is no `--version`, `--help`, or `--config` flag. The version string is hardcoded as `"alpha"` (`cmd/dockenciler/main.go:125`).

## JSON config file

The full config schema is defined in `pkg/config/config.go:14-83`. Below is a complete example with all available fields.

### Complete ECR example

```json
{
  "registry": {
    "type": "ecr",
    "ecr": {
      "region": "us-east-1",
      "access_key": "YOUR_AWS_ACCESS_KEY_ID",
      "secret_key": "YOUR_AWS_SECRET_ACCESS_KEY"
    }
  },
  "docker": {
    "socket_path": "/var/run/docker.sock",
    "label_filter": "dockenciler.autoupdate=true"
  },
  "reconcile_interval": "30m",
  "log_level": "info",
  "color_logs": true,
  "dry_run": false,
  "exclusions": ["container_id_1", "container_id_2"],
  "timezone": "America/New_York",
  "criteria": {
    "version": "",
    "regex": "^v\\d+\\.\\d+\\.\\d+$",
    "digest": ""
  },
  "notifications": {
    "slack_webhook_url": "https://hooks.slack.com/services/...",
    "discord_webhook_url": "",
    "telegram_bot_token": "",
    "telegram_chat_id": "",
    "email_host": "",
    "email_port": "",
    "email_user": "",
    "email_password": "",
    "email_from": "",
    "email_to": "",
    "msteams_webhook_url": "",
    "google_chat_webhook_url": "",
    "templates": {
      "default": "",
      "slack": "",
      "discord": "",
      "telegram": "",
      "email": "",
      "msteams": "",
      "google_chat": ""
    }
  }
}
```

### GCR example

```json
{
  "registry": {
    "type": "gcr",
    "gcr": {
      "auth": {
        "method": "adc"
      }
    }
  },
  "reconcile_interval": "1h",
  "log_level": "info"
}
```

For a GCR service account, set the auth method and file path:

```json
{
  "registry": {
    "type": "gcr",
    "gcr": {
      "auth": {
        "method": "service_account",
        "service_account_file": "/etc/dockenciler/gcp-key.json"
      }
    }
  }
}
```

### Schema reference

| JSON path | Type | Default | Description |
|---|---|---|---|
| `registry.type` | string | `""` | Registry provider: `"ecr"` or `"gcr"` (required) |
| `registry.ecr.region` | string | `""` | AWS region (required when `registry.type=ecr`) |
| `registry.ecr.access_key` | string | `""` | AWS access key (leave empty for IMDSv2 instance role) |
| `registry.ecr.secret_key` | string | `""` | AWS secret key (leave empty for IMDSv2 instance role) |
| `registry.gcr.auth.method` | string | `"adc"` | GCR auth method: `"adc"` or `"service_account"` |
| `registry.gcr.auth.service_account_file` | string | `""` | Path to GCP service account JSON key (required when `method=service_account`) |
| `docker.socket_path` | string | `"/var/run/docker.sock"` | Docker engine socket path |
| `docker.label_filter` | string | `"dockenciler.autoupdate=true"` | Label selector for containers to watch |
| `reconcile_interval` | string | `"1h"` | Duration between reconciliation loops. **Binary default: `1h`. Docker image default: `5m` (overridden by `ENV` in `Dockerfile:33`).** |
| `log_level` | string | `"info"` | Log level: `debug`, `info`, `warn`, `error` |
| `color_logs` | bool | `true` | Enable colorized log output (TTY only) |
| `dry_run` | bool | `false` | When true, log intended updates without applying them |
| `exclusions` | array of strings | `[]` | Container IDs to skip during reconciliation. **Use the JSON array form only; env-var comma-splitting is not verified to work.** |
| `timezone` | string | `"Host"` | `"Host"` or empty = system timezone. Any other string = IANA timezone name (e.g. `"America/New_York"`). Invalid names cause an error. |
| `criteria.version` | string | `""` | Exact image tag to match (e.g. `"latest"`, `"v1.2.3"`) |
| `criteria.regex` | string | `""` | Regex pattern to match image tags (e.g. `"^v\\d+\\.\\d+\\.\\d+$"`) |
| `criteria.digest` | string | `""` | Exact image digest to match (e.g. `"sha256:abc123..."`) |
| `notifications.slack_webhook_url` | string | `""` | Slack incoming webhook URL |
| `notifications.discord_webhook_url` | string | `""` | Discord webhook URL |
| `notifications.telegram_bot_token` | string | `""` | Telegram bot token (from @BotFather) |
| `notifications.telegram_chat_id` | string | `""` | Telegram chat ID |
| `notifications.email_host` | string | `""` | SMTP server hostname |
| `notifications.email_port` | string | `""` | SMTP server port |
| `notifications.email_user` | string | `""` | SMTP username |
| `notifications.email_password` | string | `""` | SMTP password |
| `notifications.email_from` | string | `""` | Sender email address |
| `notifications.email_to` | string | `""` | Recipient email address |
| `notifications.msteams_webhook_url` | string | `""` | Microsoft Teams incoming webhook URL |
| `notifications.google_chat_webhook_url` | string | `""` | Google Chat webhook URL |
| `notifications.templates.default` | string | `""` | Default notification template (Go `text/template`). **Only used by the Log notifier; the other six notifiers do not consult this field.** |
| `notifications.templates.slack` | string | `""` | Slack-specific template override |
| `notifications.templates.discord` | string | `""` | Discord-specific template override |
| `notifications.templates.telegram` | string | `""` | Telegram-specific template override |
| `notifications.templates.email` | string | `""` | Email body template override. **Note: the email subject is not user-configurable; it uses the built-in `DefaultEmailSubjectTemplate` (`pkg/notifier/template.go:35`).** |
| `notifications.templates.msteams` | string | `""` | MS Teams-specific template override |
| `notifications.templates.google_chat` | string | `""` | Google Chat-specific template override |

### Config structure notes

- The `registry` block uses **peer pointer fields**: `registry.ecr` and `registry.gcr` are mutually exclusive substructs, not flattened fields. Setting `registry.region` at the top level of the `registry` object has no effect — the binary will exit with `"ECR registry type requires ecr configuration"` (`cmd/dockenciler/main.go:129-131`).
- The `notifications` block at the JSON level maps directly to the `Notifications` struct (`pkg/config/config.go:59-73`). Each notification provider has its own field; the `templates` sub-block is a peer struct (`pkg/config/config.go:75-83`).

## Environment variables

All environment variables use the prefix `DOCKENCILER_`. Nested configuration keys (separated by `.` in JSON) become underscores. For example, `notifications.slack_webhook_url` becomes `DOCKENCILER_NOTIFICATIONS_SLACK_WEBHOOK_URL`.

This mapping is handled by `v.SetEnvPrefix("DOCKENCILER")` (`pkg/config/config.go:94`) combined with `v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))` (`pkg/config/config.go:132`).

### Registry

| Environment variable | JSON path | Description | Default |
|---|---|---|---|
| `DOCKENCILER_REGISTRY_TYPE` | `registry.type` | Registry provider: `ecr` or `gcr` | `""` |
| `DOCKENCILER_REGISTRY_ECR_REGION` | `registry.ecr.region` | AWS region | `""` |
| `DOCKENCILER_REGISTRY_ECR_ACCESS_KEY` | `registry.ecr.access_key` | AWS access key (leave empty for IMDSv2) | `""` |
| `DOCKENCILER_REGISTRY_ECR_SECRET_KEY` | `registry.ecr.secret_key` | AWS secret key | `""` |
| `DOCKENCILER_REGISTRY_GCR_AUTH_METHOD` | `registry.gcr.auth.method` | GCR auth method: `adc` or `service_account` | `"adc"` |
| `DOCKENCILER_REGISTRY_GCR_AUTH_SERVICE_ACCOUNT_FILE` | `registry.gcr.auth.service_account_file` | Path to GCP service account JSON key | `""` |

### Docker

| Environment variable | JSON path | Description | Default |
|---|---|---|---|
| `DOCKENCILER_DOCKER_SOCKET_PATH` | `docker.socket_path` | Docker engine socket path | `"/var/run/docker.sock"` |
| `DOCKENCILER_DOCKER_LABEL_FILTER` | `docker.label_filter` | Label selector for containers to watch | `"dockenciler.autoupdate=true"` |

### Application

| Environment variable | JSON path | Description | Default |
|---|---|---|---|
| `DOCKENCILER_RECONCILE_INTERVAL` | `reconcile_interval` | Duration between reconciliation loops | Binary: `"1h"`, Docker image: `"5m"` (overridden via `Dockerfile:33`) |
| `DOCKENCILER_LOG_LEVEL` | `log_level` | Log level: `debug`, `info`, `warn`, `error` | `"info"` |
| `DOCKENCILER_COLOR_LOGS` | `color_logs` | Enable colorized log output | `true` |
| `DOCKENCILER_DRY_RUN` | `dry_run` | Preview updates without applying them | `false` |
| `DOCKENCILER_EXCLUSIONS` | `exclusions` | Container IDs to skip | `[]` |
| `DOCKENCILER_TIMEZONE` | `timezone` | Timezone: `"Host"` (system) or IANA name | `"Host"` |

### Criteria

| Environment variable | JSON path | Description | Default |
|---|---|---|---|
| `DOCKENCILER_CRITERIA_VERSION` | `criteria.version` | Exact image tag to match | `""` |
| `DOCKENCILER_CRITERIA_REGEX` | `criteria.regex` | Regex pattern to match image tags | `""` |
| `DOCKENCILER_CRITERIA_DIGEST` | `criteria.digest` | Exact image digest to match | `""` |

### Notifications

| Environment variable | JSON path | Description | Default |
|---|---|---|---|
| `DOCKENCILER_NOTIFICATIONS_SLACK_WEBHOOK_URL` | `notifications.slack_webhook_url` | Slack incoming webhook URL | `""` |
| `DOCKENCILER_NOTIFICATIONS_DISCORD_WEBHOOK_URL` | `notifications.discord_webhook_url` | Discord webhook URL | `""` |
| `DOCKENCILER_NOTIFICATIONS_TELEGRAM_BOT_TOKEN` | `notifications.telegram_bot_token` | Telegram bot token | `""` |
| `DOCKENCILER_NOTIFICATIONS_TELEGRAM_CHAT_ID` | `notifications.telegram_chat_id` | Telegram chat ID | `""` |
| `DOCKENCILER_NOTIFICATIONS_EMAIL_HOST` | `notifications.email_host` | SMTP server hostname | `""` |
| `DOCKENCILER_NOTIFICATIONS_EMAIL_PORT` | `notifications.email_port` | SMTP server port | `""` |
| `DOCKENCILER_NOTIFICATIONS_EMAIL_USER` | `notifications.email_user` | SMTP username | `""` |
| `DOCKENCILER_NOTIFICATIONS_EMAIL_PASSWORD` | `notifications.email_password` | SMTP password | `""` |
| `DOCKENCILER_NOTIFICATIONS_EMAIL_FROM` | `notifications.email_from` | Sender email address | `""` |
| `DOCKENCILER_NOTIFICATIONS_EMAIL_TO` | `notifications.email_to` | Recipient email address | `""` |
| `DOCKENCILER_NOTIFICATIONS_MSTEAMS_WEBHOOK_URL` | `notifications.msteams_webhook_url` | Microsoft Teams incoming webhook URL | `""` |
| `DOCKENCILER_NOTIFICATIONS_GOOGLE_CHAT_WEBHOOK_URL` | `notifications.google_chat_webhook_url` | Google Chat webhook URL | `""` |

### Notification templates

| Environment variable | JSON path | Description | Default |
|---|---|---|---|
| `DOCKENCILER_NOTIFICATIONS_TEMPLATES_DEFAULT` | `notifications.templates.default` | Default notification template | `""` (built-in used) |
| `DOCKENCILER_NOTIFICATIONS_TEMPLATES_SLACK` | `notifications.templates.slack` | Slack-specific template override | `""` (built-in used) |
| `DOCKENCILER_NOTIFICATIONS_TEMPLATES_DISCORD` | `notifications.templates.discord` | Discord-specific template override | `""` (built-in used) |
| `DOCKENCILER_NOTIFICATIONS_TEMPLATES_TELEGRAM` | `notifications.templates.telegram` | Telegram-specific template override | `""` (built-in used) |
| `DOCKENCILER_NOTIFICATIONS_TEMPLATES_EMAIL` | `notifications.templates.email` | Email body template override | `""` (built-in used) |
| `DOCKENCILER_NOTIFICATIONS_TEMPLATES_MSTEAMS` | `notifications.templates.msteams` | MS Teams-specific template override | `""` (built-in used) |
| `DOCKENCILER_NOTIFICATIONS_TEMPLATES_GOOGLE_CHAT` | `notifications.templates.google_chat` | Google Chat-specific template override | `""` (built-in used) |

> **Important:** Setting `DOCKENCILER_EXCLUSIONS` as a comma-separated string (e.g. `DOCKENCILER_EXCLUSIONS=id1,id2`) is not verified to work. The Viper configuration in `pkg/config/config.go` does not include a `StringToSliceHookFunc` to split strings on commas, and the existing test at `pkg/config/config_test.go:175-189` only tests `json.Unmarshal` directly. Use the JSON array form in `config.json` instead. See [Troubleshooting](troubleshooting.md) for details.

## Loading order examples

### Env-only configuration

```bash
DOCKENCILER_REGISTRY_TYPE=ecr
DOCKENCILER_REGISTRY_ECR_REGION=us-east-1
DOCKENCILER_REGISTRY_ECR_ACCESS_KEY=YOUR_AWS_ACCESS_KEY_ID
DOCKENCILER_REGISTRY_ECR_SECRET_KEY=YOUR_AWS_SECRET_ACCESS_KEY
DOCKENCILER_LOG_LEVEL=info
DOCKENCILER_RECONCILE_INTERVAL=30m
```

Run with just env vars:

```bash
dockenciler
```

### JSON-only configuration

```bash
dockenciler /etc/dockenciler/config.json
```

### JSON + env with env override

If `config.json` sets `reconcile_interval` to `"1h"` but the environment has `DOCKENCILER_RECONCILE_INTERVAL=5m`, the runtime value will be `5m` — env wins.

### Partial JSON with defaults filling in

If your `config.json` contains only:

```json
{
  "registry": {
    "type": "ecr",
    "ecr": {
      "region": "us-east-1"
    }
  }
}
```

All unspecified fields use their defaults from `pkg/config/config.go:97-129` — `reconcile_interval` defaults to `"1h"` (binary), `log_level` to `"info"`, `color_logs` to `true`, `docker.socket_path` to `"/var/run/docker.sock"`, and so on.

## Notifications and template configuration

Detailed provider setup instructions (Slack webhooks, Telegram bots, SMTP credentials, etc.) are in [Notifications](notifications.md). This section covers how templates are wired at the configuration level.

### Template priority

The template cascade differs between the Log notifier and the other six notifiers:

- **Log notifier:** full cascade — per-provider (which is always `templates.default`) > built-in. This is because `NewLogNotifierWithTemplate` at `cmd/dockenciler/main.go:169` receives `cfg.Notifications.Templates.Default` as its template argument.
- **All other notifiers (Slack, Discord, Telegram, Email, MS Teams, Google Chat):** partial cascade — per-provider > built-in. Each notifier receives its own template field directly (e.g. `NewSlackNotifierWithTemplate(..., tmpl.Slack)`). If that field is empty, the notifier falls back to its provider-specific built-in template. The `templates.default` value is **not consulted** for these notifiers.

| Notifier | Config field for template | Falls back to `templates.default`? | Falls back to built-in? |
|---|---|---|---|
| Log | `templates.default` | N/A (this IS the default) | Yes |
| Slack | `templates.slack` | No | Yes |
| Discord | `templates.discord` | No | Yes |
| Telegram | `templates.telegram` | No | Yes |
| Email | `templates.email` (body only) | No | Yes (body only; subject is always built-in) |
| MS Teams | `templates.msteams` | No | Yes |
| Google Chat | `templates.google_chat` | No | Yes |

### Email subject

The email subject is not user-configurable. It always uses the built-in `DefaultEmailSubjectTemplate` (`pkg/notifier/template.go:35`). Only the email body is customizable via `templates.email`.

### Template syntax

Templates use Go's `text/template` syntax. The available fields are:

| Field | Description |
|---|---|
| `{{.ContainerID}}` | Short Docker container ID |
| `{{.ContainerName}}` | Container name (currently empty — reserved field) |
| `{{.Image}}` | Full image reference |
| `{{.OldDigest}}` | Previous image digest |
| `{{.NewDigest}}` | New image digest |
| `{{.Level}}` | Notification level (`info`, `warning`, `error`) |
| `{{.Timestamp}}` | Time of the update (Go `time.Time`) |
| `{{.Location}}` | Configured timezone location (`*time.Location`) |
| `{{.Subject}}` | Default subject line |
| `{{.Body}}` | Default body text |
