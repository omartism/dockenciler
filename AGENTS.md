# Agents Guide: Dockenciler

Lightweight Docker reconciler that automatically updates containers based on image matching criteria.

## IMPORTANT
Always update Documentation in README.md if needed

## Developer Commands
Use the `Makefile` for most tasks:
- **Build**: `make build` (outputs binary to root)
- **Test**: `make test` (runs all tests)
- **Format/Tidy**: `make fmt` / `make tidy`
- **Docker Build**: `make docker-build`
- **Docker Run**: `make docker-up` (requires `.env`)

## Architecture
- **Entrypoint**: `cmd/dockenciler/main.go`
- `pkg/reconciler`: Core monitoring and update logic.
- `pkg/registry`: ECR and IMDSv2 integration.
- `pkg/config`: Viper-based config loading.
- `pkg/notifier`: Multi-provider notification system.
- `pkg/docker`: Docker Engine API wrapper.

## Configuration
Configured via JSON or `DOCKENCILER_` environment variables (priority).
- `DOCKENCILER_DRY_RUN`: Preview updates without applying.
- `DOCKENCILER_RECONCILE_INTERVAL`: Loop frequency (e.g., `1h`).
- `DOCKENCILER_DOCKER_LABEL_FILTER`: Target containers (default: `dockenciler.autoupdate=true`).
- `DOCKENCILER_REGISTRY_TYPE`: Registry type (e.g., `ecr`).
- `DOCKENCILER_CRITERIA_REGEX`: Regex for image tag matching.
- `DOCKENCILER_TIMEZONE`: Timezone for notification timestamps (IANA name or `Host`, default: `Host`).

## Operational Notes
- **Permissions**: Requires access to `/var/run/docker.sock`.
- **AWS Auth**: Supports IAM Access Keys and IMDSv2 instance roles.
- **Self-Update**: Skips containers labeled `dockenciler.instance=true`.
- **Build**: Dockerfile uses a multi-stage build with a distroless static base (`CGO_ENABLED=0`).
- **Healthcheck**: Verified via `ps aux | grep dockenciler`.

## CI/CD
- `.github/workflows/release.yml` triggers on `v*` tags, building and pushing to GHCR.
