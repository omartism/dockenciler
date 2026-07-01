# Agents Guide: Dockenciler

Dockenciler is a lightweight Docker reconciler written in Go that automatically updates containers based on image matching criteria.

## Developer Commands

- **Build**: `go build -o dockenciler .`
- **Test All**: `go test ./...`
- **Test Package**: `go test ./pkg/<package>`
- **Run with Docker Compose**: `docker compose up -d`

## Architecture & Project Structure

- `pkg/reconciler`: Core logic for monitoring and updating containers.
- `pkg/registry`: Registry interactions, including AWS ECR and IMDSv2 support.
- `pkg/config`: Configuration loading and validation.
- `pkg/notifier`: Notification providers (Slack, Teams, Email, etc.).
- `pkg/docker`: Wrapper for Docker Engine API.
- `internal/testutil`: Shared mocks and testing utilities.
- `.agents/skills/`: Collection of OpenCode skills (separate from core application logic).

## Configuration

Dockenciler is configured via a JSON file or environment variables (prefixed with `DOCKENCILER_`). Environment variables take precedence.

**Key Configs:**
- `DOCKENCILER_DRY_RUN`: Preview updates without applying.
- `DOCKENCILER_RECONCILE_INTERVAL`: Loop frequency (e.g., `1h`).
- `DOCKENCILER_DOCKER_LABEL_FILTER`: Label used to target containers (default: `dockenciler.autoupdate=true`).

## Operational Notes

- **Docker Socket**: Requires access to `/var/run/docker.sock`.
- **AWS Auth**: Supports IAM Access Keys or IMDSv2 instance roles for ECR.
- **Self-Update**: The instance skips itself if labeled with `dockenciler.instance=true`.
