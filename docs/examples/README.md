# Examples

This directory contains ready-to-use config file examples. Each file is a complete `config.json` that can be passed as a positional argument to `dockenciler`.

**These examples are validated by an automated test** (`TestExampleConfigs` in `pkg/config/config_test.go`); they unmarshal cleanly into the `Config` struct.

## Available examples

| File | Provider | Auth | Highlights |
|---|---|---|---|
| `ecr-basic.json` | ECR | Static IAM keys | Minimal ECR config |
| `ecr-imds.json` | ECR | IMDSv2 instance role | No static credentials; relies on AWS SDK credential chain |
| `swarm-rolling.json` | ECR | Static IAM keys | Docker Swarm use case (dockenciler auto-detects Swarm at runtime) |
| `advanced-matching.json` | ECR | Static IAM keys | Match on a specific tag (`criteria.version`), with exclusion list |
| `gcr-adc.json` | GCR | Application Default Credentials | Zero-config GCR (ADC is the default) |
| `gcr-service-account.json` | GCR | Service account JSON key | Production GCR with explicit credentials |
| `multi-notifier.json` | ECR | Static IAM keys | Slack + Telegram + Email with custom templates |
| `dry-run.json` | ECR | Static IAM keys | Dry-run mode for testing |

## How to use

```bash
dockenciler /path/to/docs/examples/ecr-basic.json
```

Or in `docker-compose.yml`:

```yaml
services:
  dockenciler:
    image: ghcr.io/omartism/dockenciler
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./docs/examples/ecr-basic.json:/home/dockenciler/config.json:ro
    command: ["/home/dockenciler/config.json"]
    environment:
      LOG_LEVEL: info
```

## Credential safety

All example files use safe placeholders (`YOUR_AWS_ACCESS_KEY_ID`, `YOUR_AWS_SECRET_ACCESS_KEY`). These are not real credentials — replace them with your actual values when deploying.

For real deployments, replace these placeholders with real values passed via environment variables, Docker secrets, or sealed secrets — never commit real credentials.

## See also

- [Configuration reference](../configuration.md) — full env var and JSON schema reference
- [ECR provider](../providers/ecr.md)
- [GCR provider](../providers/gcr.md)
- [Notifications](../notifications.md)
- [Operations](../operations.md)
