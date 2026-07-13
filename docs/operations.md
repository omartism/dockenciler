# Operations

## Logs

Dockenciler uses Go's structured `log/slog` package for logging. The log level is set via `LOG_LEVEL` or the `log_level` field in `config.json`.

| Level | When to use |
|---|---|
| `debug` | Troubleshooting — shows every digest comparison and API call |
| `info` | Normal operation — shows reconciliation summaries and update events |
| `warn` | Unexpected but non-fatal conditions |
| `error` | Failures that skip a container (auth errors, pull failures, API errors) |

Colorized output is enabled by default (`COLOR_LOGS=true`, `pkg/config/config.go:109`). Colors only render on TTY terminals. Set `COLOR_LOGS=false` for non-TTY environments (e.g., log aggregators).

Log output goes to stdout. There is no built-in file logging, log rotation, or JSON output toggle — `slog` defaults to text format.

### Understanding reconciliation log output

A typical reconciliation cycle produces:

```
"Starting reconciliation" "container_count" 3
"Checking container" "container_id" abc123 "image" "myapp:latest"
"Container is up to date" "container_id" abc123 "image" "myapp:latest" "digest" sha256:...
"Checking container" "container_id" def456 "image" "otherapp:latest"
"Update required for container" "container_id" def456 "current_digest" sha256:old "latest_digest" sha256:new
"Container updated successfully" "container_id" def456 "image" "otherapp:latest"
"Reconciliation completed" "total" 3 "checked" 2 "up_to_date" 1 "updated" 1 "skipped" 1 "failed" 0
```

The summary line shows:
- **total**: containers matching the label filter.
- **checked**: containers where digest comparison ran (excludes skipped and self-skip).
- **up_to_date**: containers already on the latest digest.
- **updated**: containers that were successfully recreated or service-updated.
- **skipped**: containers excluded by the exclusion list or self-skip.
- **failed**: containers where an error occurred (auth failure, pull failure, API error).

The startup sequence logs:

1. ASCII "DOCKENCILER" banner.
2. `"Starting dockenciler"` with interval, label filter, and timezone (`cmd/dockenciler/main.go:87`).
3. Initial reconciliation result.
4. Periodic reconciliation summaries at the configured interval.

### Debug logging

Enable `log_level: "debug"` (or `LOG_LEVEL=debug`) to see:

- Current digest for every container: `"Got current image digest"` (line 109 of reconciler.go).
- Registry digest for every container: `"Got latest registry digest"` (line 121).
- Auth credential resolution and cache status.
- Raw Docker API errors that may be suppressed at `info` level.

## Dry-run mode

Dry-run mode logs intended updates without pulling images or recreating containers. Enable it with:

```json
{
  "dry_run": true
}
```

Or via environment variable:

```bash
DRY_RUN=true
```

When dry-run is active, the reconciler still resolves the current and latest digests and compares them. If they differ, it logs:

```
Dry-run: would update container <id> from_digest <current> to_digest <latest>
```

And continues to the next container without calling `GetAuth`, `Authenticate`, `PullImage`, or `RecreateContainer`/`UpdateService` (`pkg/reconciler/reconciler.go:136-139`).

Use dry-run to verify your label filter, exclusion list, and criteria are working correctly before enabling real updates.

## Exclusions

Containers can be excluded from reconciliation by their container ID via the `exclusions` field:

```json
{
  "exclusions": ["abc123def456", "789ghi012jkl"]
}
```

**Important:** Use the JSON array form in `config.json`. The env-var form (`EXCLUSIONS=abc123,def456`) is not verified to work — Viper is not configured with a `StringToSliceHookFunc` (`pkg/config/config.go`, no `StringToSlice` hook). See [Troubleshooting](troubleshooting.md#exclusionsid1id2-not-working) for details.

## Self-update exclusion

Dockenciler skips any container with the label `dockenciler.instance=true` (`pkg/reconciler/reconciler.go:73-79`). This is a **hardcoded** check — not configurable through `label_filter` or `exclusions`.

This prevents the reconciler from killing itself during a reconciliation cycle. If the Dockenciler container were updated, it would be recreated, and the process would be terminated mid-cycle, potentially leaving other updates incomplete.

All example compose files include this label on the Dockenciler service. Do not remove it.

## Timezone

The `timezone` field controls the timezone used for notification timestamps (`{{.Timestamp}}` in templates). It is resolved by `config.ResolveTimezone()` (`pkg/config/config.go`, see `ResolveTimezone`).

| Value | Behavior |
|---|---|
| `""` (empty) | Uses system timezone (`time.Local`) |
| `"Host"` | Uses system timezone (`time.Local`) |
| Any IANA name (e.g. `"America/New_York"`, `"Europe/London"`) | Uses `time.LoadLocation(name)`. Invalid names cause a startup error: `"Invalid timezone"`. |

IANA timezone names are platform-dependent. On Alpine / distroless images, the `tzdata` package must be installed for full IANA name support. The official Docker image uses `gcr.io/distroless/static-debian12`, which includes timezone data.

## Version

The binary version is hardcoded as `"alpha"` (`cmd/dockenciler/main.go:125`). There is no `--version` flag, no build-time version injection, and no `--help` flag.

This is a known limitation. The version string is logged at startup:

```
"Dockenciler started" "version" "alpha"
```

## Releases & CI

The project uses GitHub Actions for continuous integration and release automation. All workflow files are in `.github/workflows/`.

### Release flow

A GitHub Release is triggered by pushing a tag matching `v*`. The release pipeline has two sequential jobs:

1. **Security scan**: Trivy filesystem scan (`trivy fs`) on the repository. Fails on CRITICAL or HIGH severity findings (`exit-code: 1`). Results are uploaded to the GitHub Security tab as SARIF.

2. **Build and push** (after security scan passes):
   - Docker Buildx builds a multi-arch image (`linux/amd64` + `linux/arm64`).
   - Image is pushed to **GHCR** (`ghcr.io/<owner>/dockenciler`).
   - A GitHub Release is created with auto-generated release notes.

The `docker/metadata-action@v5` in `release.yml` derives the image tags from the Git tag.

#### Image tag conventions

| Git tag | GHCR tags created |
|---|---|
| `v1.2.3` | `1.2.3`, `1.2`, `1`, `stable`, `latest` |
| `v1.2.3-alpha.1` | `alpha` (floating) |
| `v1.2.3-beta.2` | `beta` (floating) |
| `v1.2.3-rc.3` | `rc` (floating) |

The `stable` and `latest` tags are only created for full releases (tags without a pre-release suffix). Pre-release floating tags (`alpha`, `beta`, `rc`) overwrite on each new pre-release push.

### CI workflows

| Workflow | File | Trigger | What it does |
|---|---|---|---|---|
| Unit Tests | `test.yml` | PRs to `main`/`master` | `go test -race -v ./...` + `go vet ./...` (Go 1.24) |
| Release | `release.yml` | Push of `v*` tag | Trivy scan → multi-arch build → GHCR push to `ghcr.io/omartism/dockenciler` → GitHub Release |
| CodeQL | `codeql.yml` | PR to `main` | GitHub CodeQL security analysis (`language: go`) |
| Labeler | `labeler.yml` | PR / Issue opened | Auto-labels PRs and issues by keyword matching |
| Stale | `stale.yml` | Cron (daily) | Marks stale issues and PRs with `no-issue-activity` / `no-pr-activity` |
| Summarize Issue | `summarize-issue.yml` | Issue opened | AI-generated summary comment on new issues |

All workflows are standard GitHub Actions configurations in `.github/workflows/`.

## Running from source

For development and ad-hoc testing, you can build and run the binary directly:

```bash
# Build the binary
make build

# Run with a config file
./dockenciler /path/to/config.json

# Run with env vars only (no config file)
REGISTRY_TYPE=gcr REGISTRY_GCR_AUTH_METHOD=adc ./dockenciler
```

The Makefile provides the following targets:

| Target | Description |
|---|---|
| `build` | Build binary to `./dockenciler` |
| `test` | Run all unit tests (no `-race`) |
| `fmt` | Format Go source files |
| `tidy` | Tidy `go.mod` and `go.sum` |
| `docker-build` | Build Docker image |
| `docker-up` | Run via Docker Compose |
| `security-scan` | Run Trivy filesystem scan |
| `clean` | Remove build artifacts |
| `help` | Show available targets |

See the [Makefile](../Makefile) for the full definition.

## Known project issues (informational)

### Go version mismatch in CI

The CI workflow `test.yml` uses **Go 1.24** (`actions/setup-go@v5` with `go-version: '1.24'`), but the project's `go.mod` requires **Go 1.26.3**. CI will fail to compile. This is a project bug — the `test.yml` `go-version` should be `'1.26'` to match `Dockerfile:2` and `go.mod:3`.

This is outside the documentation scope to fix, but is documented here because it affects any CI validation you may attempt.

## See also

- [Configuration reference](configuration.md) — all env vars and defaults.
- [Troubleshooting](troubleshooting.md) — common runtime errors and fixes.
- [Security](security.md) — permissions, secrets, and least-privilege.
- [Notifications](notifications.md) — log output, templates, message customization.
