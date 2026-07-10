# Agents Guide: Dockenciler

Lightweight Docker reconciler that watches labeled containers and recreates them when their image digest changes.

## House rules
- **Update `README.md`** whenever you change user-facing config, env vars, CLI flags, or behavior.
- **Update `.env.example`** alongside `README.md` for any new env var.
- `make test` does **not** include `-race`. CI does (see `## CI/CD` below). When debugging concurrency, run `go test -race ./...` yourself.
- The binary is committed in some branches by accident — don't `git add dockenciler` unless you mean to. `.gitignore` does not list it.

## Quick reference
| Task | Command |
|---|---|
| Build | `make build` (outputs `./dockenciler` at repo root) |
| All tests | `make test` |
| One test | `go test -v -run TestName ./pkg/<package>/` |
| One package | `go test -v ./pkg/<package>/` |
| Race + verbose | `go test -race -v ./...` |
| Format / tidy | `make fmt` / `make tidy` |
| Security scan (local) | `make security-scan` (needs `trivy` CLI) |
| Docker image | `make docker-build` |
| Run via Compose | `make docker-up` (needs `.env` from `.env.example`) |

## Architecture
- **Entrypoint**: `cmd/dockenciler/main.go` — signal setup → config → logging → Docker client → registry → notifier → reconciler → ticker loop.
- **`pkg/config`** — Viper-based loader. JSON file + env vars (env wins). Nested struct: `Registry{Type, ECR, GCR}` with peer `ECRConfig`/`GCRConfig`; see `ResolveTimezone()` for tz handling.
- **`pkg/registry`** — Provider interface (`GetLatestDigest`, `GetImageVersion`, `GetAuth`, `InvalidateCache`). `Auth{Username, Password, RegistryHost}` carries the auth tuple. Providers:
  - `ecr.go` — AWS SDK v2; IMDSv2 instance role supported via the AWS SDK's default credential chain (`cmd/dockenciler/main.go:133`, `awscfg.LoadDefaultConfig`). `imds.go` is a standalone reference implementation (used only by `imds_test.go`).
  - `gcr.go` — Docker Registry v2 HTTP API (`HEAD /v2/<path>/manifests/<tag>`); no AR/gcloud SDK. Token cache uses `golang.org/x/oauth2` `TokenSource` with 5-min buffer.
- **`pkg/reconciler`** — Core loop. Only call site for `Registry.GetAuth` + `DockerClient.Authenticate` (see `reconciler.go` near `reconcileContainer`).
- **`pkg/docker`** — Engine API wrapper. `Authenticate(ctx, username, password, registryHost)` — username is **not** hardcoded to "AWS" anymore.
- **`pkg/notifier`** — Composite: log always-on + Slack/Discord/Telegram/Email/MSTeams/GoogleChat. Templates are Go `text/template`.
- **`internal/testutil`** — Shared mocks (`MockRegistry`, `MockDockerClient`) with function-field injection. All tests import from here; do not duplicate mock types in test files.

### Config loading order
1. Defaults via `v.SetDefault()` (see `pkg/config/config.go`)
2. JSON config file (optional, path from CLI arg)
3. Env vars (no prefix; override everything — note `.` → `_` in nested keys)

### Adding a new registry provider
1. Implement the `Registry` interface in `pkg/registry/<name>.go` (all four methods).
2. Add a config struct under `pkg/config/config.go` as a peer field on `Registry` (do **not** flatten onto the `Registry` struct).
3. Add a factory in `cmd/dockenciler/main.go` switch on `cfg.Registry.Type`; gate on a nil-check for the new config field.
4. Add a test hook constructor (e.g., `newGCRProviderForTest`) so tests can inject `httpClient` and `TokenSource` without network/credentials.
5. Update `README.md` and `.env.example`. Update `mocks.go` in `internal/testutil`.

### Provider-local image parsing
Image refs are parsed **inside each provider**, not via a shared helper. ECR strips `<account>.dkr.ecr.<region>.amazonaws.com/`; GCR splits into `host + projectPath + ref`. Don't introduce a shared `parseImageRef` — it will rot.

## Testing
- **Frameworks**: `stretchr/testify` (`assert`/`require`) + `gotest.tools/v3` (`cmp`/`icmp`).
- **Pattern**: Table-driven, `*_test.go` next to source.
- **No integration tests.** No Docker daemon, AWS, or GCP credentials required. Don't add any.
- **Mocking**: Use `internal/testutil` mocks for cross-package tests (reconciler). For provider-internal tests, use `httptest.NewTLSServer` (GCR) or a mock `ECRClient` interface (ECR).
- **Fixtures**: `pkg/notifier/template_test.go` has `testNotification()` helper — reuse it for new notifier tests.
- Token sources and HTTP clients in `GCRProvider` are interface-typed and injectable; tests should never need real GCP credentials.

## Notification template fields
`{{.ContainerID}}`, `{{.ContainerName}}` (reserved, currently empty), `{{.Image}}`, `{{.OldDigest}}`, `{{.NewDigest}}`, `{{.Level}}`, `{{.Timestamp}}`, `{{.Location}}`, `{{.Subject}}`, `{{.Body}}`

## Operational gotchas
- **Permissions**: Needs `/var/run/docker.sock`. Container runs as root by default; non-root needs `docker` group.
- **Self-update**: Skips containers labeled `dockenciler.instance=true`. Don't drop this label from the docs example.
- **GCR supported hostnames**: `gcr.io`, `*.gcr.io` (e.g. `us.gcr.io`), `*-docker.pkg.dev` (e.g. `us-docker.pkg.dev`). Conventionally supported; the parser accepts other hostnames but Docker pulls will fail.
- **GCR auth**:
  - `adc` (default) — `google.DefaultTokenSource`. Picks up `GOOGLE_APPLICATION_CREDENTIALS`, GCE/GKE metadata, `gcloud auth application-default login`. Zero config.
  - `service_account` — JSON key file path only. Path is `filepath.Clean`-ed and `..` is rejected. Do not accept embedded JSON in env/config.
- **Token handling**: Never log tokens. `oauth2.Token` access is fine in-memory; never write to disk.
- **Build**: Dockerfile is multi-stage `golang:1.26-alpine` → `gcr.io/distroless/static-debian12` (`CGO_ENABLED=0`, `-ldflags="-s -w"`).

## CI/CD (`.github/workflows/`)
- `test.yml` — PRs to `main`/`master`: `go test -race -v ./...` + `go vet ./...` (Go 1.24).
- `release.yml` — On `v*` tags: Trivy scan → multi-arch build (amd64/arm64) → push GHCR → GitHub Release.
- `codeql.yml` — CodeQL analysis.
- `labeler.yml`, `stale.yml`, `summarize-issue.yml` — repo hygiene.
- **Release tag conventions**: `v1.2.3` → GHCR tags `1.2.3`, `1.2`, `1`, `stable`, `latest`. Pre-release: `-alpha.N`, `-beta.N`, `-rc.N`.
