# Dockenciler

Dockenciler is a lightweight and efficient open-source Docker reconciler written in Golang. It automatically monitors and updates your Docker containers with new images based on customizable criteria, ensuring your environment stays up-to-date without manual intervention.

## 🚀 Features

- **Flexible Image Matching**: Update containers based on the `latest` tag, specific version numbers, or custom regular expressions.
- **Smart Filtering**: 
  - Update all containers by default.
  - Or, target specific containers using the label `dockenciler.autoupdate=true` (customizable).
- **Update Strategies**:
  - **In-place (Default)**: Recreates the container with the new image.
  - **Rolling Update**: Supports rolling updates for minimized downtime when running in Docker Swarm mode.
- **Docker Swarm Support**: Fully compatible with Docker Swarm orchestration.
- **Secure Authentication**:
  - AWS ECR support via IAM Access Keys and Secret Keys.
  - **Recommended**: IMDSv2 instance role for enhanced security on AWS.
- **Extensive Notifications**: Get notified about update events via:
  - Email, Slack, MS Teams, Google Chat, Telegram, Discord, and local logs.
  - *Customizable notification templates for clear and concise alerts.*
- **Configuration**: Easily configured via JSON files or environment variables.
- **Safety Rails**:
  - **Dry-Run Mode**: Preview updates without applying changes.
  - **Self-Update Exclusion**: Automatically skips the Dockenciler instance itself (via `dockenciler.instance=true` label) and configurable exclusion lists.

## 🛠 Installation & Development

### Using Docker Compose

The easiest way to run Dockenciler is using Docker Compose.

1. Create a `docker-compose.yml` file:

```yaml
services:
  dockenciler:
    image: ghcr.io/omartism/dockenciler:latest
    container_name: dockenciler
    labels:
      - "dockenciler.instance=true"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./config.json:/config.json
    environment:
      - DOCKENCILER_LOG_LEVEL=info
    restart: always
```

> **Note:** Dockenciler needs access to the Docker socket to manage containers. The image runs as root by default, which has the necessary permissions. If you run it as a non-root user, ensure the user is in the `docker` group (e.g., via `group_add` in Docker Compose or `--group-add docker` with `docker service create`).

2. Start the container:

```bash
docker compose up -d
```

### Docker Swarm

For Docker Swarm deployments, use a stack file:

```yaml
services:
  dockenciler:
    image: ghcr.io/omartism/dockenciler:latest
    environment:
      DOCKENCILER_REGISTRY_TYPE: "ecr"
      DOCKENCILER_DOCKER_LABEL_FILTER: "dockenciler.instance=true"
      DOCKENCILER_REGISTRY_REGION: "eu-west-2"
      DOCKENCILER_RECONCILE_INTERVAL: "1m"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    networks:
      - proxy
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "5"
    deploy:
      placement:
        constraints:
          - node.role == manager
      restart_policy:
        condition: on-failure

networks:
  proxy:
    external: true
```

Deploy the stack:

```bash
docker stack deploy -c dockenciler-stack.yml dockenciler
```

### Building from Source

Dockenciler includes a `Makefile` for convenient development:

- **Build**: `make build` - Compiles the binary to the root directory.
- **Test**: `make test` - Runs all tests in the repository.
- **Tidy**: `make tidy` - Cleans up `go.mod` and `go.sum`.
- **Format**: `make fmt` - Formats code according to Go standards.
- **Docker Image**: `make docker-build` - Builds the production Docker image.


## ⚙️ Configuration

Dockenciler can be configured using a JSON file or environment variables. Environment variables take precedence over the JSON file.

### Environment Variables

All environment variables are prefixed with `DOCKENCILER_`. Nested configuration fields are separated by underscores.

| Environment Variable | JSON Path | Description | Default |
|----------------------|-----------|-------------|---------|
| `DOCKENCILER_LOG_LEVEL` | `log_level` | Log level (`debug`, `info`, `warn`, `error`) | `info` |
| `DOCKENCILER_DRY_RUN` | `dry_run` | Enable dry-run mode | `false` |
| `DOCKENCILER_RECONCILE_INTERVAL` | `reconcile_interval` | Interval between reconciliation loops | `1h` |
| `DOCKENCILER_DOCKER_SOCKET_PATH` | `docker.socket_path` | Path to Docker socket | `/var/run/docker.sock` |
| `DOCKENCILER_DOCKER_LABEL_FILTER` | `docker.label_filter` | Label to target containers | `dockenciler.autoupdate=true` |
| `DOCKENCILER_REGISTRY_TYPE` | `registry.type` | Registry type (e.g., `ecr`) | - |
| `DOCKENCILER_REGISTRY_REGION` | `registry.region` | AWS Region for ECR | - |
| `DOCKENCILER_REGISTRY_ACCESS_KEY` | `registry.access_key` | AWS Access Key | - |
| `DOCKENCILER_REGISTRY_SECRET_KEY` | `registry.secret_key` | AWS Secret Key | - |
| `DOCKENCILER_CRITERIA_VERSION` | `criteria.version` | Exact tag version to match | - |
| `DOCKENCILER_CRITERIA_REGEX` | `criteria.regex` | Regex to match tags | - |
| `DOCKENCILER_CRITERIA_DIGEST` | `criteria.digest` | Exact image digest to match | - |
| `DOCKENCILER_EXCLUSIONS` | `exclusions` | Comma-separated list of container IDs to skip | `[]` |
| `DOCKENCILER_SLACK_WEBHOOK_URL` | `notifications.slack_webhook_url` | Slack webhook URL | - |
| `DOCKENCILER_DISCORD_WEBHOOK_URL` | `notifications.discord_webhook_url` | Discord webhook URL | - |
| `DOCKENCILER_TELEGRAM_BOT_TOKEN` | `notifications.telegram_bot_token` | Telegram bot token | - |
| `DOCKENCILER_TELEGRAM_CHAT_ID` | `notifications.telegram_chat_id` | Telegram chat ID | - |
| `DOCKENCILER_EMAIL_HOST` | `notifications.email_host` | SMTP host | - |
| `DOCKENCILER_EMAIL_PORT` | `notifications.email_port` | SMTP port | - |
| `DOCKENCILER_EMAIL_USER` | `notifications.email_user` | SMTP username | - |
| `DOCKENCILER_EMAIL_PASSWORD` | `notifications.email_password` | SMTP password | - |
| `DOCKENCILER_EMAIL_FROM` | `notifications.email_from` | Sender email address | - |
| `DOCKENCILER_EMAIL_TO` | `notifications.email_to` | Recipient email address | - |
| `DOCKENCILER_MSTEAMS_WEBHOOK_URL` | `notifications.msteams_webhook_url` | Microsoft Teams webhook URL | - |
| `DOCKENCILER_GOOGLE_CHAT_WEBHOOK_URL` | `notifications.google_chat_webhook_url` | Google Chat webhook URL | - |

### Example Configuration (`config.json`)

```json
{
  "registry": {
    "type": "ecr",
    "region": "us-east-1",
    "access_key": "AKIA...",
    "secret_key": "..."
  },
  "docker": {
    "socket_path": "/var/run/docker.sock",
    "label_filter": "dockenciler.autoupdate=true"
  },
  "reconcile_interval": "30m",
  "log_level": "info",
  "criteria": {
    "regex": "^v\\d+\\.\\d+\\.\\d+$"
  },
  "dry_run": false,
  "exclusions": ["container_id_1", "container_id_2"]
}
```

## 🔔 Notifications

Dockenciler can send notifications when containers are updated. Notifications are configured via environment variables and multiple providers can be used simultaneously.

### Supported Providers

| Provider | Environment Variable | Description |
|----------|---------------------|-------------|
| **Log** | Always enabled | Logs to stdout (always active) |
| **Slack** | `DOCKENCILER_SLACK_WEBHOOK_URL` | Slack incoming webhook |
| **Discord** | `DOCKENCILER_DISCORD_WEBHOOK_URL` | Discord webhook |
| **Telegram** | `DOCKENCILER_TELEGRAM_BOT_TOKEN` + `DOCKENCILER_TELEGRAM_CHAT_ID` | Telegram bot |
| **Email** | `DOCKENCILER_EMAIL_HOST`, `DOCKENCILER_EMAIL_PORT`, etc. | SMTP email |
| **Microsoft Teams** | `DOCKENCILER_MSTEAMS_WEBHOOK_URL` | Teams incoming webhook |
| **Google Chat** | `DOCKENCILER_GOOGLE_CHAT_WEBHOOK_URL` | Google Chat webhook |

### Slack Setup

1. Create a Slack app at https://api.slack.com/apps
2. Enable "Incoming Webhooks" and create a webhook URL
3. Add the webhook URL to your configuration:

```bash
export DOCKENCILER_SLACK_WEBHOOK_URL="<your-slack-webhook-url>"
```

Or in `config.json`:
```json
{
  "notifications": {
    "slack_webhook_url": "<your-slack-webhook-url>"
  }
}
}
```

### Discord Setup

1. Go to Server Settings → Integrations → Webhooks
2. Create a new webhook and copy the URL
3. Add the webhook URL to your configuration:

```bash
export DOCKENCILER_DISCORD_WEBHOOK_URL="<your-discord-webhook-url>"
```

### Telegram Setup

1. Create a bot via @BotFather on Telegram
2. Get the bot token from BotFather
3. Add the bot to your group/channel and get the chat ID
4. Configure both values:

```bash
export DOCKENCILER_TELEGRAM_BOT_TOKEN="123456789:********"
export DOCKENCILER_TELEGRAM_CHAT_ID="*****"
```

### Email (SMTP) Setup

Configure your SMTP server details:

```bash
export DOCKENCILER_EMAIL_HOST="smtp.gmail.com"
export DOCKENCILER_EMAIL_PORT="587"
export DOCKENCILER_EMAIL_USER="your-email@gmail.com"
export DOCKENCILER_EMAIL_PASSWORD="your-app-password"
export DOCKENCILER_EMAIL_FROM="dockenciler@yourdomain.com"
export DOCKENCILER_EMAIL_TO="alerts@yourdomain.com"
```

> **Note:** For Gmail, use an [App Password](https://support.google.com/accounts/answer/185833) instead of your regular password.

### Microsoft Teams Setup

1. Go to your Teams channel → Connectors (or Workflows)
2. Create an "Incoming Webhook" connector
3. Copy the webhook URL:

```bash
export DOCKENCILER_MSTEAMS_WEBHOOK_URL="<your-msteams-webhook-url>"
```

### Google Chat Setup

1. Open the space where you want to add the bot
2. Click the down arrow → Manage webhooks
3. Create a new webhook and copy the URL:

```bash
export DOCKENCILER_GOOGLE_CHAT_WEBHOOK_URL="<your-google-chat-webhook-url>"
```

### Example: Multiple Providers

You can enable multiple providers at once:

```bash
export DOCKENCILER_SLACK_WEBHOOK_URL="<your-slack-webhook-url>"
export DOCKENCILER_TELEGRAM_BOT_TOKEN="<your-telegram-bot-token>"
export DOCKENCILER_TELEGRAM_CHAT_ID="<your-telegram-chat-id>"
export DOCKENCILER_LOG_LEVEL="info"
```

Or in `config.json`:
```json
{
  "notifications": {
    "slack_webhook_url": "<your-slack-webhook-url>",
    "telegram_bot_token": "<your-telegram-bot-token>",
    "telegram_chat_id": "<your-telegram-chat-id>"
  }
}
```

### Notification Format

When a container is updated, Dockenciler sends a notification with:

- **Subject**: `Container <container_id> updated`
- **Body**: `Container <container_id> was updated from digest <old_digest> to <new_digest>`
- **Level**: `info`

Logs are always written to stdout regardless of other notification providers.

## 📜 Versioning

Dockenciler follows the [Semantic Versioning (SemVer)](https://semver.org/) standard: `MAJOR.MINOR.PATCH`.
- `latest` tag always points to the most recent stable release.

## 📄 License

MIT
