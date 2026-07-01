# Agents Guide: Dockenciler

Dockenciler is a lightweight Docker reconciler written in Go that automatically updates containers based on image matching criteria.

## Developer Commands

- **Build**: `go build -o dockenciler .` (requires main.go - currently missing)
- **Test All**: `go test ./...`
- **Test Package**: `go test ./pkg/<package>`
- **Run with Docker Compose**: `docker compose up -d` (requires .env file)

## Architecture & Project Structure

- `pkg/reconciler`: Core logic for monitoring and updating containers.
- `pkg/registry`: Registry interactions, including AWS ECR and IMDSv2 support.
- `pkg/config`: Configuration loading and validation using viper.
- `pkg/notifier`: Notification providers (Slack, Teams, Email, etc.).
- `pkg/docker`: Wrapper for Docker Engine API (github.com/docker/docker v28+).
- `internal/testutil`: Shared mocks and testing utilities.
- `.agents/skills/`: Collection of OpenCode skills (separate from core application logic).

## Configuration

Dockenciler is configured via a JSON file or environment variables (prefixed with `DOCKENCILER_`). Environment variables take precedence. Uses `github.com/spf13/viper` with `mapstructure` tags.

**Key Configs:**
- `DOCKENCILER_DRY_RUN`: Preview updates without applying.
- `DOCKENCILER_RECONCILE_INTERVAL`: Loop frequency (e.g., `1h`).
- `DOCKENCILER_DOCKER_LABEL_FILTER`: Label used to target containers (default: `dockenciler.autoupdate=true`).
- `DOCKENCILER_REGISTRY_TYPE`: Registry type (`ecr`).
- `DOCKENCILER_REGISTRY_REGION`: AWS Region for ECR.
- `DOCKENCILER_CRITERIA_REGEX`: Regex to match image tags.
- `DOCKENCILER_EXCLUSIONS`: Comma-separated container IDs to skip.

## Operational Notes

- **Docker Socket**: Requires access to `/var/run/docker.sock`.
- **AWS Auth**: Supports IAM Access Keys or IMDSv2 instance roles for ECR.
- **Self-Update**: The instance skips itself if labeled with `dockenciler.instance=true`.
- **Missing Entry Point**: No `main.go` exists - Docker build will fail. Entry point must be created in root or `cmd/` directory.
- **Multi-stage Build**: Dockerfile uses distroless static base with CGO_ENABLED=0.
- **Healthcheck**: Process-based (`ps aux | grep dockenciler`).

## CI/CD

- **Release Workflow**: `.github/workflows/release.yml` triggers on `v*` tags.
- Builds and pushes to GHCR.
- Creates GitHub Release with auto-generated notes.

## Dependencies

- `github.com/aws/aws-sdk-go-v2` - AWS ECR integration
- `github.com/docker/docker` - Docker Engine API client
- `github.com/spf13/viper` - Configuration management
- `github.com/stretchr/testify` - Test assertions/mocks
- `gotest.tools/v3` - Additional test utilities