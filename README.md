# Dockenciler

Dockenciler is a lightweight and efficient open-source Docker reconciler written in Golang. It automatically monitors and updates your Docker containers with new images based on customizable criteria, ensuring your environment stays up-to-date without manual intervention.

## 🚀 Features

- 🎉 **Docker image under 50MB!** Built with a multi-stage build (golang:1.26-alpine → distroless/static-debian12).
- **Flexible Image Matching**: Update containers based on the `latest` tag, specific version numbers, or custom regular expressions.
- **Smart Filtering**: Update all containers by default, or target specific containers using the label `dockenciler.autoupdate=true` (customizable via `docker.label_filter`).
- **Update Strategies**: In-place container recreation (default) or rolling updates in Docker Swarm mode for minimized downtime.
- **Secure Authentication**: AWS ECR (IAM access keys or IMDSv2 instance role), GCR / Artifact Registry (ADC or service account JSON key), and Docker Hub (anonymous access for public images).
- **Extensive Notifications**: Email, Slack, MS Teams, Google Chat, Telegram, Discord, and local logs — all with customizable Go `text/template` templates.
- **Safety Rails**: Dry-run mode, self-update exclusion via `dockenciler.instance=true` label, and configurable exclusion lists.
- **Multiple Configuration Sources**: JSON config file, environment variables (env vars override file), and sensible defaults.

## 🛠 Quickstart (Docker Compose)

The easiest way to run Dockenciler is with Docker Compose using a JSON configuration file.

### Docker Compose Setup

Create a `docker-compose.yml`:

```yaml
services:
  dockenciler:
    image: ghcr.io/omartism/dockenciler:latest
    container_name: dockenciler
    restart: unless-stopped
    labels:
      - "dockenciler.instance=true"
    command: ["/home/dockenciler/config.json"]
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./config.json:/home/dockenciler/config.json:ro
    env_file: .env
```

The `command:` line passes the config file path to Dockenciler. The binary's `ENTRYPOINT` is `/dockenciler`; this single-element array is appended so the effective invocation becomes `/dockenciler /home/dockenciler/config.json`.

If you prefer to use environment variables only (no `config.json`), omit the `command:` line and the config file volume mount. All options can be set via environment variables in the `.env` file.

> **Note:** Dockenciler needs access to the Docker socket to manage containers. The image runs as root by default, which has the necessary permissions. If you run it as a non-root user, ensure the user is in the `docker` group (e.g., via `group_add` in Docker Compose or `--group-add docker` with `docker service create`).

### Self-update exclusion

Dockenciler automatically skips containers labeled `dockenciler.instance=true`, preventing it from attempting to update its own container during reconciliation cycles. The `labels:` entry in the compose example above ensures this exclusion is applied.

### Configuration

Dockenciler supports three registry providers: AWS ECR, GCR / Artifact Registry, and Docker Hub. Pick one below.

#### ECR (Elastic Container Registry)

**`config.json`:**
```json
{
  "registry": {
    "type": "ecr",
    "ecr": {
      "region": "us-east-1"
    }
  },
  "reconcile_interval": "30m",
  "log_level": "info",
  "notifications": {
    "slack_webhook_url": "https://hooks.slack.com/services/..."
  }
}
```

Place secrets in `.env` (copy from `.env.example`):

```bash
REGISTRY_ECR_ACCESS_KEY=YOUR_AWS_ACCESS_KEY_ID
REGISTRY_ECR_SECRET_KEY=YOUR_AWS_SECRET_ACCESS_KEY
NOTIFICATIONS_SLACK_WEBHOOK_URL=https://hooks.slack.com/services/T00/B00/xxxx
```

Leave `access_key` and `secret_key` empty to use IMDSv2 on EC2. The IAM role needs `ecr:GetAuthorizationToken`. See [ECR Provider](docs/providers/ecr.md) for region setup, IAM policies, and IMDSv2 details.

#### GCR / Artifact Registry

**`config.json`:**
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
  "reconcile_interval": "30m",
  "log_level": "info"
}
```

No ECR-related env vars are needed. The `adc` method (default) picks up `GOOGLE_APPLICATION_CREDENTIALS`, GCE/GKE metadata, or `gcloud auth application-default login`. For a service account JSON key, switch to `"method": "service_account"` and set `"service_account_file"` to the key path. See [GCR Provider](docs/providers/gcr.md) for auth methods, supported hostnames, and IAM setup.

#### Docker Hub

**`config.json`:**
```json
{
  "registry": {
    "type": "dockerhub"
  },
  "reconcile_interval": "30m",
  "log_level": "info"
}
```

No credentials are required for public images. Dockenciler obtains anonymous bearer tokens for registry API queries and the Docker daemon handles pulls without authentication. Add `dockenciler.autoupdate=true` as a label on any container running a public Docker Hub image to start automatic updates.

Supported image reference formats:
- `postgres:18-alpine` (official image, auto-prefixed with `library/`)
- `library/postgres:18-alpine` (explicit library namespace)
- `myuser/myimage:tag` (user/organization repository)
- `docker.io/library/postgres:18-alpine` (fully-qualified)

> **Note:** Private Docker Hub repositories are not yet supported. The provider only works with publicly accessible images.

Start the container:

```bash
docker compose up -d
```

### Docker Swarm

For Docker Swarm deployments, use a stack file. Dockenciler automatically detects Swarm mode and performs rolling updates on detected services. The service must run on manager nodes to access the Docker API:

When updates are needed, Dockenciler updates the service definition (image and digest) rather than recreating containers directly, enabling zero-downtime rolling updates.

```yaml
services:
  dockenciler:
    image: ghcr.io/omartism/dockenciler:latest
    environment:
      REGISTRY_TYPE: "ecr"
      DOCKER_LABEL_FILTER: "dockenciler.autoupdate=true"
      REGISTRY_ECR_REGION: "eu-west-2"
      RECONCILE_INTERVAL: "1m"
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

Deploy with:

```bash
docker stack deploy -c dockenciler-stack.yml dockenciler
```

### Building from Source

Dockenciler includes a `Makefile` for convenience:

- **Build**: `make build` — Compiles the binary to `./dockenciler` in the project root.
- **Test**: `make test` — Runs all tests (does not include the race detector; use `go test -race ./...` when debugging concurrency).
- **Docker Image**: `make docker-build` — Multi-stage distroless build (`golang:1.26-alpine` → `gcr.io/distroless/static-debian12`, `CGO_ENABLED=0`, `-ldflags="-s -w"`).
- **Run with Compose**: `make docker-up` — Starts via Docker Compose (needs `.env` from `.env.example`).
- **Security Scan**: `make security-scan` — Runs Trivy filesystem scan (requires `trivy` CLI).
- **Format / Tidy**: `make fmt` / `make tidy` — Code formatting and dependency cleanup.

## ⚙️ Configuration

Dockenciler is configured through a JSON file (passed as a command-line argument) and environment variables. Environment variables take precedence over the JSON file. All env vars correspond to the config key with dots replaced by underscores (e.g., `notifications.slack_webhook_url` becomes `NOTIFICATIONS_SLACK_WEBHOOK_URL`).

See the [Configuration Reference](docs/configuration.md) for the complete list of options with defaults, JSON structure, and environment variable mappings.

## 🔔 Notifications

Dockenciler can notify you when containers are updated via seven providers: Log (always active, stdout), Slack, Discord, Telegram, Email (SMTP), Microsoft Teams, and Google Chat. Multiple providers can be enabled simultaneously.

Templates use Go's `text/template` syntax. The available template fields are:

- `{{.ContainerID}}` — Container ID
- `{{.ContainerName}}` — Container name (reserved, currently empty; reconciler does not populate)
- `{{.Image}}` — Full image reference (e.g., `registry.example.com/repo:tag`)
- `{{.OldDigest}}` — Previous image digest
- `{{.NewDigest}}` — New image digest
- `{{.Level}}` — Notification level (`info`, `warning`, `error`)
- `{{.Timestamp}}` — Timestamp of the update (Go `time.Time`)
- `{{.Location}}` — Timezone location
- `{{.Subject}}` — Default subject line
- `{{.Body}}` — Default body text

See [Notifications](docs/notifications.md) for provider setup guides, template customization, and the template priority cascade.

## 📄 Documentation

| Topic | Description | Link |
|---|---|---|
| Getting Started | 5-minute quickstart guide | [docs/README.md](docs/README.md) |
| Installation | Docker Compose, Swarm, binary, from-source | [docs/installation.md](docs/installation.md) |
| Configuration | Full env var table, JSON schema, defaults | [docs/configuration.md](docs/configuration.md) |
| ECR Provider | IAM keys, IMDSv2, region setup | [docs/providers/ecr.md](docs/providers/ecr.md) |
| GCR / Artifact Registry | ADC, service account, supported hostnames | [docs/providers/gcr.md](docs/providers/gcr.md) |
| Docker Hub Provider | Public image support, anonymous access | [docs/providers/dockerhub.md](docs/providers/dockerhub.md) |
| Notifications | Provider setup, templates, field reference | [docs/notifications.md](docs/notifications.md) |
| Security | Permissions, secrets, Docker socket hardening | [docs/security.md](docs/security.md) |
| Operations & CI | Logs, dry-run, releases, CI pipeline | [docs/operations.md](docs/operations.md) |
| Troubleshooting | FAQ, common errors, recovery | [docs/troubleshooting.md](docs/troubleshooting.md) |
| Examples | JSON configuration samples | [docs/examples/README.md](docs/examples/README.md) |

## 📜 Versioning

Releases are tagged per the [SemVer](https://semver.org/) convention; see [Operations & CI](docs/operations.md#releases--ci) for tag conventions and the multi-arch build pipeline. The runtime `--version` flag is not implemented; the binary reports a hardcoded value (see [Troubleshooting](docs/troubleshooting.md) for details).

## 📄 License

MIT
