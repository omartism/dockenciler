# Installation

## Docker Compose (recommended)

The Docker image is published at `ghcr.io/omartism/dockenciler:latest` and supports linux/amd64 and linux/arm64.

### With config.json (recommended)

This example uses both a JSON config file and environment variables. The JSON file holds the configuration structure; the `.env` file holds secrets and runtime overrides.

**`docker-compose.yml`:**

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

**`config.json`:**

```json
{
  "registry": {
    "type": "ecr",
    "ecr": {
      "region": "us-east-1",
      "access_key": "YOUR_AWS_ACCESS_KEY_ID",
      "secret_key": "YOUR_AWS_SECRET_ACCESS_KEY"
    }
  },
  "log_level": "info"
}
```

**`.env`:**

```bash
DOCKENCILER_LOG_LEVEL=info
```

> **Why the `command:` line?** The Dockerfile uses `ENTRYPOINT ["/dockenciler"]` with no `CMD` (`Dockerfile:43`). The binary reads its config path from `os.Args[1]` (`cmd/dockenciler/main.go:109-114`). Mounting `config.json` at `/home/dockenciler/config.json` without passing that path means the file is never read. The `command:` line appends the path as an argument to the entrypoint. Without it, the file mount has no effect.

Start the container:

```bash
docker compose up -d
```

### Configuration via env only (no config.json)

When all configuration is supplied through environment variables, the config file mount becomes unnecessary. This is the simpler path for most users.

**`docker-compose.yml`:**

```yaml
services:
  dockenciler:
    image: ghcr.io/omartism/dockenciler:latest
    container_name: dockenciler
    labels:
      - "dockenciler.instance=true"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    env_file: .env
    restart: unless-stopped
```

No `command:` line is needed because there is no config file to read. All configuration comes from the `.env` file. See [Configuration](configuration.md) for the full list of environment variables.

## Docker Swarm

For Docker Swarm deployments, use a stack file with env vars directly in the service definition. Environment variables are the simplest approach in Swarm mode since config file mounts require shared volumes across nodes.

**`dockenciler-stack.yml`:**

```yaml
services:
  dockenciler:
    image: ghcr.io/omartism/dockenciler:latest
    environment:
      DOCKENCILER_REGISTRY_TYPE: "ecr"
      DOCKENCILER_REGISTRY_ECR_REGION: "eu-west-2"
      DOCKENCILER_DOCKER_LABEL_FILTER: "dockenciler.autoupdate=true"
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

> **Note:** The `node.role == manager` placement constraint is required because Dockenciler needs access to the Docker API to manage services and containers. Manager nodes have this access by default.

## Binary

You can build and run Dockenciler as a standalone binary. This is useful for testing or running outside Docker.

### Build the binary

```bash
make build
```

This produces `./dockenciler` at the repository root.

### Run it

```bash
./dockenciler /path/to/config.json
```

Without a path argument, Dockenciler runs on defaults and environment variables only (`cmd/dockenciler/main.go:109-114`).

> **Note:** The binary is not listed in `.gitignore`. If you build it inside a Git working tree, be careful not to commit it — the binary has been accidentally committed in some branches.

### Requirements

- Go 1.26 or later (for building from source; the Docker image includes the correct version). Matches `go.mod:3` (`go 1.26.3`) and `Dockerfile:2` (`golang:1.26-alpine`).
- Access to the Docker daemon socket (`/var/run/docker.sock`).

## From source

Use the Makefile targets for development and testing:

| Task | Command |
|------|---------|
| Build | `make build` (outputs `./dockenciler`) |
| Test | `make test` (all tests) |
| Format | `make fmt` |
| Tidy | `make tidy` |
| Docker image | `make docker-build` |
| Run via Compose | `make docker-up` (requires `.env` from `.env.example`) |
| Security scan | `make security-scan` (requires `trivy` CLI) |

Single-package tests:

```bash
go test -v ./pkg/config/
go test -v -run TestName ./pkg/<package>/
```

Race-enabled testing (matching CI):

```bash
go test -race -v ./...
```

## Permissions

Dockenciler needs access to the Docker daemon socket (`/var/run/docker.sock`) to inspect containers, pull images, and recreate containers. The official image runs on `gcr.io/distroless/static-debian12`, which has no user namespace — the default user is root.

- **Root user:** Works out of the box with the Docker socket mount.
- **Non-root user:** The container must be in the `docker` group. In Docker Compose, add `group_add: ["docker"]` to the service definition. Note that `distroless/static-debian12` does not include `/etc/passwd` or `adduser`, so creating a non-root user inside the container requires a custom image. Running as root is the practical default.

## Self-update exclusion

Dockenciler skips its own container to avoid killing itself during a reconciliation cycle. The check is hardcoded at `pkg/reconciler/reconciler.go:73-79`: any container with the label `dockenciler.instance=true` is skipped.

All example compose files include this label:

```yaml
labels:
  - "dockenciler.instance=true"
```

Do not drop this label from your own Dockenciler service definition.

## Verifying the install

After starting the container, check the logs:

```bash
docker compose logs -f dockenciler
```

You should see:

1. The ASCII "DOCKENCILER" banner.
2. `"Starting dockenciler"` with the configured interval, label filter, and timezone.
3. Periodic reconciliation log lines at the configured interval.

If the logs show `"Initial reconciliation failed"`, check your registry configuration and credentials. See [Troubleshooting](troubleshooting.md) for common issues.
