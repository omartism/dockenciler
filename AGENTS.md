# Agents Guide: Dockenciler

Lightweight Docker reconciler that automatically updates containers based on image matching criteria.

## IMPORTANT
- Always update README.md if changes affect user-facing docs, config options, or CLI behavior.
- Only ECR registry is supported — `cfg.Registry.Type` must be `"ecr"` or the process exits.

## Developer Commands
Use the `Makefile` for most tasks:
- **Build**: `make build` — outputs binary `./dockenciler` to repo root.
- **Test (all)**: `make test` — runs `go test -v ./...`.
- **Test (single)**: `go test -v -run TestName ./pkg/notifier/` — run one test by name.
- **Test (package)**: `go test -v ./pkg/config/` — run all tests in a package.
- **Format/Tidy**: `make fmt` / `make tidy`
- **Security scan**: `make security-scan` — runs Trivy filesystem + config scans (requires `trivy` CLI).
- **Docker Build**: `make docker-build`
- **Docker Run**: `make docker-up` (requires `.env`)

### CI checks (PRs)
CI runs `go test -race -v ./...` and `go vet ./...`. Locally, `make test` omits `-race`; add it manually when debugging race conditions.

## Architecture
- **Entrypoint**: `cmd/dockenciler/main.go` — signal setup → config → logging → Docker client → registry → notifier → reconciler → ticker loop.
- `pkg/config`: Viper-based config loading. JSON file + `DOCKENCILER_` env vars (env takes precedence). See `ResolveTimezone()` for timezone handling.
- `pkg/reconciler`: Core reconciliation loop. Watches labeled containers, compares image digests, triggers updates + notifications.
- `pkg/registry`: ECR client with IMDSv2 instance role support. Token caching with 5-min expiry check.
- `pkg/notifier`: Composite notifier (log always-on + Slack, Discord, Telegram, Email, MS Teams, Google Chat). Templates use Go `text/template`.
- `pkg/docker`: Docker Engine API wrapper for container listing, image pull, container recreate.

### Config loading order
1. Defaults set via `v.SetDefault()`
2. JSON config file (optional, path from CLI arg or empty)
3. `DOCKENCILER_*` env vars override everything

### Notification template fields
`{{.ContainerID}}`, `{{.Image}}`, `{{.OldDigest}}`, `{{.NewDigest}}`, `{{.Level}}`, `{{.Timestamp}}`, `{{.Location}}`, `{{.Subject}}`, `{{.Body}}`

## Testing
- **Frameworks**: `stretchr/testify` (assert/require) + `gotest.tools/v3` (cmp/icmp).
- **Pattern**: Table-driven tests in `*_test.go` alongside source files.
- **Test helper**: `pkg/notifier/template_test.go` uses `testNotification()` to build fixtures.
- **No integration tests**: All tests are unit tests. No Docker daemon or AWS credentials needed.

## Operational Notes
- **Permissions**: Requires access to `/var/run/docker.sock`.
- **AWS Auth**: Supports IAM Access Keys and IMDSv2 instance roles (IMDSv2 recommended for EC2).
- **Self-Update**: Skips containers labeled `dockenciler.instance=true`.
- **Build**: Dockerfile uses multi-stage build: `golang:1.26-alpine` → `gcr.io/distroless/static-debian12` (`CGO_ENABLED=0`, `-ldflags="-s -w"`).
- **Healthcheck**: `ps aux | grep dockenciler` (in Dockerfile).
- **Env template**: `.env.example` documents all env vars — copy to `.env` for `make docker-up`.

## CI/CD
- `.github/workflows/test.yml` — PRs to main/master: `go test -race -v ./...` + `go vet ./...`.
- `.github/workflows/release.yml` — `v*` tags: Trivy security scan → build multi-arch (amd64/arm64) → push to GHCR → GitHub Release.
- `.github/workflows/codeql.yml` — CodeQL analysis.
- Release tags: `v1.2.3` → GHCR tags `1.2.3`, `1.2`, `1`, `stable`, `latest`. Pre-release: `-alpha.N`, `-beta.N`, `-rc.N`.
