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

## 🛠 Installation

### Using Docker Compose

The easiest way to run Dockenciler is using Docker Compose.

1. Create a `docker-compose.yml` file:

```yaml
services:
  dockenciler:
    image: your-repo/dockenciler:latest
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

2. Start the container:

```bash
docker compose up -d
```

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

## 📜 Versioning

Dockenciler follows the [Semantic Versioning (SemVer)](https://semver.org/) standard: `MAJOR.MINOR.PATCH`.
- `latest` tag always points to the most recent stable release.

## 📄 License

MIT
