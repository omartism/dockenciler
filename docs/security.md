# Security

## Docker socket exposure

Dockenciler needs access to the Docker daemon socket (`/var/run/docker.sock`) to inspect containers, pull images, and recreate them. Mounting the Docker socket inside a container grants the container effective root access to the Docker host.

- The official image runs on `gcr.io/distroless/static-debian12`, which defaults to root.
- Mounting the socket is required — there is no alternative API for container lifecycle management.
- Mount `/var/run/docker.sock` (typically `/var/run/docker.sock:/var/run/docker.sock`). The `:ro` flag can be added for principle (no reason dockenciler needs to write the socket), but note that `:ro` on a Unix socket does not restrict Docker API access — the container can still issue pulls, recreates, and exec calls. The real security boundary is the trust you place in dockenciler itself.

### What access the Docker socket grants

Mounting `/var/run/docker.sock` into a container gives that container the ability to:

- List, inspect, start, stop, and remove any container on the host.
- Pull and push images to any registry the host has access to.
- Create, modify, and delete networks, volumes, and secrets.
- Execute commands inside any container via `docker exec`.
- Access host resources through container mounts (e.g., mount the host filesystem as a volume).

Dockenciler uses only a subset of these capabilities: `ListContainers`, `InspectContainer`, `PullImage`, `RecreateContainer`, `RegistryLogin`, `ImageInspectWithRaw`, and service operations via the Swarm API. But the socket itself does not enforce least-privilege — any compromise of the Dockenciler process grants full Docker API access.

### Reducing the risk

- Run Dockenciler on a dedicated host or in a dedicated VM to limit the blast radius of a compromised socket mount.
- Use network-level access controls to restrict which hosts can reach the Docker socket.
- For non-root operation, add `group_add: ["docker"]` to the Docker Compose service definition. Note that `distroless/static-debian12` does not include `/etc/passwd` or a `docker` group — creating a non-root user requires a custom image. Running as root is the practical default with the distroless image.
- Consider read-only root filesystem (`read_only: true` in the service definition) to limit post-exploitation damage. Dockenciler does not write to its filesystem.

## Secrets management

### What NOT to commit

- `config.json` containing real AWS access keys, GCP service account paths, or notification webhook URLs.
- `.env` files with real credentials.
- Service account JSON key files.

The example values in the documentation use safe placeholders (e.g., `YOUR_AWS_ACCESS_KEY_ID` / `YOUR_AWS_SECRET_ACCESS_KEY`). Replace them with your actual credentials before deploying.

### Recommended patterns

| Pattern | Description |
|---|---|
| **Environment variables only** | Skip `config.json` entirely. Pass all sensitive values via env vars (`DOCKENCILER_NOTIFICATIONS_*`, `DOCKENCILER_REGISTRY_ECR_*`). Example in [Installation](installation.md#configuration-via-env-only-no-configjson). |
| **Docker Secrets** | Use Docker Swarm secrets for `service_account_file` or `email_password`. Mount secrets as files and reference them by path. |
| **Sealed Secrets** | For GitOps workflows, encrypt secrets with `kubeseal` / Bitnami Sealed Secrets and decrypt at deploy time. |
| **Vault sidecar** | Use HashiCorp Vault or similar to inject secrets into the environment at runtime. |
| **Read-only config file** | If you mount a `config.json`, mount it read-only (`:ro`) as shown in all compose examples. Dockenciler never writes to its config file. |

### Additional considerations

- **Logs may contain secrets:** Error-path logs may include the `Notification` struct (`pkg/notifier/notifier.go:16-27`) at `slog.Error` level — this contains `Image`, `OldDigest`, `NewDigest`, `ContainerID`, and `Timestamp`, but never credentials. Credentials (webhook URLs, bot tokens, SMTP password) live on the notifier structs and are not exposed to templates or logs. Review log retention policies accordingly.
- **Environment variable exposure:** `docker inspect` on the running container will show all environment variables, including credentials passed via `env_file` or `environment`. Restrict access to `docker inspect` to trusted operators.

## IAM / least-privilege

### ECR

The ECR provider calls `GetAuthorizationToken` via the AWS SDK v2 (`pkg/registry/ecr.go:108`). The minimum IAM permission is:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": "ecr:GetAuthorizationToken",
      "Resource": "*"
    }
  ]
}
```

If you use static access keys, create an IAM user with only this policy attached. For EC2 instances, attach this policy to the instance's IAM role.

No other ECR permissions are required. The provider does not call `BatchGetImage`, `BatchCheckLayerAvailability`, or any pull-related API — those happen through the Docker engine after authentication.

### GCR

For **GCR** (gcr.io images), the service account needs `roles/storage.objectViewer` on the GCS bucket backing the registry.

For **Artifact Registry** (`*-docker.pkg.dev` images), the service account needs `roles/artifactregistry.reader`.

For Application Default Credentials, the GCE/GKE default service account often has these roles by default. If not, grant them explicitly:

```bash
gcloud projects add-iam-policy-binding <PROJECT> \
  --member=serviceAccount:<SA-EMAIL> \
  --role=roles/artifactregistry.reader
```

## Network exposure

Notification webhook URLs (Slack, Discord, MS Teams, Google Chat) are bearer tokens. Compromised webhook URLs allow anyone to post messages to your channels. Treat them as secrets:

- Do not commit webhook URLs to version control.
- Pass them via environment variables, not config files on disk.
- Rotate webhook URLs if accidentally exposed.

Telegram bot tokens are similarly sensitive — a leaked token allows anyone to send messages as your bot and read updates from group chats the bot is in.

### Notification secrets

Notification provider secrets — Slack/Discord/MS Teams/Google Chat webhook URLs, Telegram bot tokens, and SMTP passwords — are bearer tokens that grant write access to the destination. Treat them as carefully as database credentials:

- Do not commit them in `config.json` or in `.env` files that get committed.
- Pass them via environment variables (`DOCKENCILER_NOTIFICATIONS_*`) read from a secret manager (Docker secrets, Kubernetes secrets, Vault, AWS Secrets Manager, etc.).
- Rotate immediately if exposure is suspected.

## Self-update safety

Dockenciler skips its own container to prevent self-destruction during a reconciliation cycle. The check is hardcoded at `pkg/reconciler/reconciler.go:73-79`: any container with the label `dockenciler.instance=true` is skipped.

This label must be present on the Dockenciler service itself. All example compose files include it:

```yaml
labels:
  - "dockenciler.instance=true"
```

Without this label, Dockenciler may attempt to recreate its own container on the next reconciliation cycle, which would kill the process and disrupt monitoring.

The self-update skip is applied **before** the exclusion list check (`pkg/reconciler/reconciler.go:72-96`). This means even if the Dockenciler's own container ID were accidentally added to the exclusion list, the self-update skip would handle it first. The two checks are independent and both result in the container being counted as "skipped" in the reconciliation summary.

## Observability security

### Logs as audit trail

Dockenciler's structured logs can serve as an audit trail for container updates. Each update produces log lines with container ID, image reference, and both digests (old and new). In production deployments, forward these logs to a centralized log aggregator with appropriate access controls.

### Monitoring the monitor

Because Dockenciler can update itself (if the `dockenciler.instance=true` label were removed), a compromised image could disable the reconciler. Best practices:

- Always keep the self-update label on the Dockenciler container.
- Use a separate monitoring system (e.g., Prometheus alert on "reconciliation not seen in >2 intervals") that does not depend on Dockenciler.
- Pin the Dockenciler image to a specific digest in production, not a floating tag like `latest`.

## Known project issues

### Go version mismatch in CI

The CI workflow `.github/workflows/test.yml` uses **Go 1.24**, but `go.mod` requires **Go 1.26.3**. This means CI tests will fail to compile. The mismatch is a project bug that needs a one-line fix in `test.yml` (bump the `go-version` matrix entry to `'1.26'`). It is outside the scope of this documentation to fix, but is flagged here because it affects any CI-based validation you may attempt.

## Secret rotation

Credentials used by Dockenciler should be rotated periodically:

- **ECR access keys:** Rotate IAM user access keys every 90 days. The ECR auth token from `GetAuthorizationToken` is temporary (typically 12 hours) and is automatically refreshed by the provider's token cache.
- **GCR service account keys:** Rotate service account JSON keys every 90 days. The provider reads the key file at startup, so a key rotation requires restarting the Dockenciler container. For zero-downtime rotation, use Workload Identity on GKE instead of static keys.
- **Notification webhook URLs:** Rotate if exposed. Slack, Discord, MS Teams, and Google Chat all support regenerating webhook URLs from their respective admin interfaces.

## See also

- [Installation](installation.md#permissions) — Docker socket mount and group requirements.
- [Operations](operations.md) — dry-run mode, exclusions, and self-update.
- [ECR provider](providers/ecr.md) — IAM authentication details.
- [GCR provider](providers/gcr.md) — service account authentication.
- [Troubleshooting](troubleshooting.md) — common auth and permission errors.
