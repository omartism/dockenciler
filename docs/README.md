# Dockenciler Documentation

Dockenciler is a lightweight and efficient open-source Docker reconciler written in Golang. It watches labeled containers and recreates them when their image digest changes, keeping your running services up to date without manual intervention.

## How it works

Dockenciler runs as a long-lived process. On a configurable interval (default 1 hour, or 5 minutes in the Docker image), it:

1. Lists all running containers filtered by a configurable label (default `dockenciler.autoupdate=true`).
2. For each matching container, resolves the current image digest from the registry.
3. Compares it with the digest of the running image.
4. If they differ, authenticates to the registry, pulls the new image, and recreates the container with the same configuration (name, ports, volumes, networks, environment).

The reconciliation loop runs once on startup and then on a ticker at the configured interval (`pkg/reconciler/reconciler.go`). Dockenciler skips its own container (label `dockenciler.instance=true`), respects an exclusion list, and supports a dry-run mode that logs planned updates without executing them.

The process architecture is defined in `cmd/dockenciler/main.go`: signal handler → config loader → logger → Docker client → registry provider → notifier → reconciler → ticker loop. Each component is a self-contained package under `pkg/`.

Before pulling a new image, Dockenciler authenticates with the registry using provider-specific credentials. ECR uses `GetAuthorizationToken` (AWS SDK v2), while GCR uses OAuth2 token exchange with either ADC or a service account. The `Auth` struct (`pkg/registry/registry.go`) carries `Username`, `Password`, and `RegistryHost` — sufficient for the Docker engine's `X-Registry-Auth` payload (`pkg/docker/docker.go:282-300`).

## 5-Minute Quickstart

The fastest way to try Dockenciler is with Docker Compose and a container hosted on AWS ECR.

### 1. Create `docker-compose.yml`

```yaml
services:
  dockenciler:
    image: ghcr.io/omartism/dockenciler:latest
    container_name: dockenciler
    labels:
      - "dockenciler.instance=true"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./config.json:/home/dockenciler/config.json:ro
    env_file: .env
    command: ["/home/dockenciler/config.json"]
    restart: unless-stopped
```

### 2. Create `config.json`

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

The `registry` block uses a nested structure defined at `pkg/config/config.go:29-33`. The `ecr` and `gcr` fields are peer pointer substructs, not flattened onto the parent. Setting fields at the wrong level (e.g. `registry.region`) causes Viper to silently fail to populate the inner struct — the binary exits with `"ECR registry type requires ecr configuration"`. Always use the nested form `{ "registry": { "ecr": { "region": "..." } } }`.

The `ecr.access_key` and `ecr.secret_key` fields are optional when using IMDSv2 on EC2. If omitted, the AWS SDK automatically resolves credentials from the instance metadata service.

The Docker socket at `/var/run/docker.sock` is mounted into the container so Dockenciler can inspect containers, pull images, and recreate them. The image runs as root on `gcr.io/distroless/static-debian12`, which has the necessary permissions by default.

### 3. Create `.env`

```bash
LOG_LEVEL=info
REGISTRY_ECR_ACCESS_KEY=YOUR_AWS_ACCESS_KEY_ID
REGISTRY_ECR_SECRET_KEY=YOUR_AWS_SECRET_ACCESS_KEY
```

Replace the access key and secret key with your actual AWS credentials. If you are running on an EC2 instance with an IAM role, leave the keys empty and Dockenciler will use IMDSv2 automatically.

### 4. Start Dockenciler

```bash
docker compose up -d
```

### 5. Watch the logs

```bash
docker compose logs -f dockenciler
```

If you see the ASCII banner followed by `"Starting dockenciler"` with the interval, label filter, and timezone, everything is working. The first reconciliation runs immediately after startup, then repeats at the configured interval (5 minutes by default in the Docker image, overridden from the binary default of 1 hour via `ENV` at `Dockerfile:33`).

To stop Dockenciler: `docker compose down`.

### Why the `command:` line?

The Dockerfile uses `ENTRYPOINT ["/dockenciler"]` with no `CMD` (`Dockerfile:43`). The binary reads its config path from `os.Args[1]` (`cmd/dockenciler/main.go:109-114`). Mounting `config.json` at `/home/dockenciler/config.json` without passing that path as a command argument means the file is silently never read. The `command:` line appends the path as an argument to the entrypoint. Without it, the mount has no effect and Dockenciler runs on defaults plus environment variables only.

If you prefer to avoid a config file entirely, skip the mount and `command:` line and use environment variables instead. See [Installation](installation.md#configuration-via-env-only-no-configjson) for the env-only compose example.

### GCR quickstart alternative

Dockenciler supports two registry providers: AWS ECR (default in this quickstart) and GCR / Artifact Registry. The provider is selected by `registry.type`.

For GCR or Artifact Registry, replace `config.json` with the following. No ECR-related env vars are needed; authentication is handled by `gcloud` or `GOOGLE_APPLICATION_CREDENTIALS`:

```json
{
  "registry": {
    "type": "gcr",
    "gcr": {
      "auth": {
        "method": "adc"
      }
    }
  }
}
```

And remove the ECR env vars from `.env`. No service account file is needed when using Application Default Credentials. See [Providers](providers/README.md) for details and the service-account method.

## Where to go next

- [Installation](installation.md) — Docker Compose (with or without a config file), Docker Swarm, standalone binary, and from-source builds.
- [Configuration](configuration.md) — every environment variable, JSON config field, and CLI option. The env-var reference uses the correct `NOTIFICATIONS_*` naming for all notification providers.
- [Providers](providers/README.md) — AWS ECR (access keys or IMDSv2) and GCR / Artifact Registry (ADC or service-account JSON key).
- [Notifications](notifications.md) — Slack, Discord, Telegram, Email, MS Teams, and Google Chat. Customizable templates with Go `text/template`.
- [Security](security.md) — permissions, secrets management, and Docker socket hardening.
- [Operations](operations.md) — logs, dry-run mode, exclusions, self-update skip, and release workflow.
- [Troubleshooting](troubleshooting.md) — common errors, configuration mistakes, and recovery steps.
- [Examples](examples/README.md) — ready-to-use JSON config files for common scenarios.

## Features

**Flexible Image Matching.** Update on the `latest` tag, an exact version like `v1.2.3`, a regex pattern like `^v\\d+\\.\\d+\\.\\d+$`, or a specific digest. Set criteria in the `criteria` block of `config.json` or via `CRITERIA_*` environment variables. When no criteria are set, any change in the image digest triggers an update.

**Selective Targeting.** By default, only containers with the label `dockenciler.autoupdate=true` are watched. The label is fully configurable through `docker.label_filter` / `DOCKER_LABEL_FILTER`.

**Update Strategies.** Two strategies depending on the deployment mode:
- **In-place (default):** For standalone containers — the container is stopped, the new image is pulled, and the container is recreated with the same name, ports, volumes, networks, and environment.
- **Rolling update:** For Docker Swarm services — Dockenciler uses the Swarm API to perform a rolling update, minimizing downtime.

**Registry Support.** Two providers:
- **AWS ECR:** Static IAM access keys or IMDSv2 instance role (recommended on EC2). Uses AWS SDK v2 `GetAuthorizationToken` with a 5-minute auth token buffer.
- **GCR / Artifact Registry:** Application Default Credentials or a service-account JSON key file. Uses the Docker Registry v2 HTTP API (`HEAD /v2/<path>/manifests/<tag>`) with a 5-minute OAuth2 token cache. Supported hostnames: `gcr.io`, `*.gcr.io`, `*-docker.pkg.dev`.

**Notifications.** Seven providers: always-on structured log output, plus Slack, Discord, Telegram, Email, MS Teams, and Google Chat. Each supports Go `text/template` message customization. Templates are set under `notifications.templates` in `config.json` or via `NOTIFICATIONS_TEMPLATES_*` environment variables. Multiple providers can be active simultaneously.

**Configuration.** Three layers: binary defaults (`pkg/config/config.go:97-129`), optional JSON config file (path from CLI arg), and environment variables (no prefix). Env vars override JSON and defaults. See [Configuration](configuration.md) for the complete reference, including the correct `NOTIFICATIONS_*` naming for all notification providers.

**Safety Rails.**
- **Dry-run mode:** Set `dry_run: true` or `DRY_RUN=true` to log intended updates without applying them.
- **Self-update exclusion:** Any container with the label `dockenciler.instance=true` is automatically skipped (`pkg/reconciler/reconciler.go:73-79`). All example compose files include this label on the Dockenciler service itself.
- **Exclusion list:** A configurable list of container IDs in `exclusions` prevents specific containers from being updated. Use the JSON array form in `config.json` — env-var comma-splitting is not verified to work (`pkg/config/config.go:55`, no `StringToSliceHookFunc` configured).

## License

MIT
