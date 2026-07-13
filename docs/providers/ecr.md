# AWS ECR Provider

## Overview

The ECR provider uses the **AWS SDK v2** to interact with Amazon Elastic Container Registry. It implements the `Registry` interface (`pkg/registry/registry.go:24-29`) with four methods: `GetLatestDigest`, `GetImageVersion`, `GetAuth`, and `InvalidateCache`.

Choose the ECR provider when your container images are stored in ECR and you want to use IAM-based access control.

## Configuration

The ECR provider is configured under `registry.ecr` in the JSON config file. The fields are **peer pointer fields** (`pkg/config/config.go:29-33`): `registry.ecr` is a sub-block, not a flat field on `registry`. Setting `registry.region` at the top level of the `registry` object has no effect — the binary exits with `"ECR registry type requires ecr configuration"` (`cmd/dockenciler/main.go:129-131`).

### JSON config

```json
{
  "registry": {
    "type": "ecr",
    "ecr": {
      "region": "us-east-1",
      "access_key": "YOUR_AWS_ACCESS_KEY_ID",
      "secret_key": "YOUR_AWS_SECRET_ACCESS_KEY"
    }
  }
}
```

### Environment variables

| Variable | Description | Default |
|---|---|---|
| `REGISTRY_TYPE` | Set to `ecr` | `""` |
| `REGISTRY_ECR_REGION` | AWS region (required) | `""` |
| `REGISTRY_ECR_ACCESS_KEY` | AWS access key (leave empty for IMDSv2) | `""` |
| `REGISTRY_ECR_SECRET_KEY` | AWS secret key (leave empty for IMDSv2) | `""` |

The `access_key` and `secret_key` fields are optional. When both are empty, the AWS SDK resolves credentials from the environment or EC2 instance metadata. See [Authentication](#authentication) below.

## Authentication

The ECR provider supports two authentication methods, selected implicitly by whether you provide static credentials.

### Static IAM credentials

When `access_key` and `secret_key` are both set in the config, the provider creates an AWS SDK credential chain using those keys directly (`cmd/dockenciler/main.go:140-149`). This is the right choice when:

- Running outside AWS (local development, on-premise).
- Using a CI/CD runner that has IAM user access keys.
- The instance does not have an IAM role attached.

The IAM user or role must have at minimum the `ecr:GetAuthorizationToken` permission. See [Security](../security.md#ecr) for a least-privilege policy example.

### IMDSv2 instance role (recommended on EC2)

IMDSv2 support comes from the AWS SDK's built-in credential chain (`awscfg.LoadDefaultConfig` at `cmd/dockenciler/main.go:133`). The ECR provider does not call IMDSv2 directly. The custom `IMDSv2Provider` struct in `pkg/registry/imds.go` is a standalone reference implementation (used only by `imds_test.go`); it is not wired into the production ECR flow.

When both `access_key` and `secret_key` are **empty** (or omitted), the AWS SDK loads credentials from the default credential chain. On EC2 instances with an attached IAM role, this resolves through the Instance Metadata Service v2.

The IMDSv2 flow (`pkg/registry/imds.go:37-62`):

1. PUT request to `http://169.254.169.254/latest/api/token` to obtain a session token (default TTL: 60 seconds).
2. GET request to `/latest/meta-data/iam/security-credentials/` to discover the IAM role name.
3. GET request to `/latest/meta-data/iam/security-credentials/<role>` to retrieve temporary credentials (access key, secret key, session token, expiration).

This is the recommended approach on EC2 because:

- No static credentials to rotate or leak.
- Temporary credentials are automatically refreshed.
- The IAM role can be scoped to the minimum permissions needed.

#### Verifying IMDSv2 availability

Run this on the EC2 instance (requires the `aws` CLI):

```bash
TOKEN=$(curl -s -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 60")
curl -s -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/iam/security-credentials/
```

If this returns a role name, the instance has IMDSv2 and an IAM role attached.

#### Known limitation

IMDSv2 fails on non-EC2 hosts (local Docker, on-premise servers) that do not expose the `169.254.169.254` metadata endpoint. In those environments, use static IAM credentials instead. See [Troubleshooting](../troubleshooting.md#ecr-imdsv2-fails-on-non-ec2-hosts).

### Auth token caching

The ECR provider calls `GetAuthorizationToken` via the AWS SDK and caches the decoded password (`pkg/registry/ecr.go:87-148`). The cache uses a **5-minute buffer** before expiry — the token is considered valid if it expires more than 5 minutes from now. When the buffer is exceeded, the token is refreshed on the next reconciliation cycle.

The username is always `"AWS"` (`pkg/registry/ecr.go:94`, 104, 147). This is passed through to `DockerClient.Authenticate()` (`pkg/docker/docker.go:282-300`), which sends it to the Docker engine's `X-Registry-Auth` payload.

## Image references

The ECR provider parses image references using `ecrParseRef` (`pkg/registry/ecr.go:280-317`). It strips the registry prefix to extract just the repository name and tag.

The parsing logic:

1. If the reference contains `@sha256:...`, the digest part is stripped (line 287).
2. The last `:` separates the repository from the tag. If no `:` is found, the tag defaults to `latest`.
3. Everything after the last `/` is the repository name — the registry hostname prefix is discarded.

Examples:

| Image reference | Repository | Tag |
|---|---|---|
| `123456789.dkr.ecr.us-east-1.amazonaws.com/myapp:latest` | `myapp` | `latest` |
| `123456789.dkr.ecr.us-east-1.amazonaws.com/team/service:v1.2.3` | `team/service` | `v1.2.3` |
| `myrepo:latest` | `myrepo` | `latest` |

The provider extracts the repository name from after the last `/`, so ECR image references work both with and without the full registry URL prefix.

## Digest resolution

The `GetLatestDigest` method (`pkg/registry/ecr.go:150-229`) uses the AWS SDK v2 `DescribeImages` API to find the latest image. The resolution depends on the configured `criteria`:

- **No criteria (default):** Filters by the tag from the image reference.
- **`criteria.version`:** Filters by exact tag match.
- **`criteria.regex`:** Lists all images for the repository, filters locally by matching each image's tags against the regex, then sorts by `ImagePushedAt` descending (newest first) (`pkg/registry/ecr.go:212-219`).
- **`criteria.digest`:** Filters by exact digest match.

The returned digest is the `imageDigest` of the most recent matching image (`pkg/registry/ecr.go:222-228`).

## Auth token lifecycle

The full lifecycle of an ECR auth token is:

1. **First reconciliation:** `GetAuth` is called. The cache is empty, so `GetAuthorizationToken` is called via the AWS SDK (`pkg/registry/ecr.go:108`).
2. **Response handling:** The base64-decoded token (the password after stripping the `"AWS:"` prefix) and the proxy endpoint URL are cached. The `ExpiresAt` from the response sets the cache expiry (`pkg/registry/ecr.go:141-145`).
3. **Subsequent reconciliations:** `GetAuth` checks the cache. If the token expires more than 5 minutes from now, the cached token is returned without an API call (`pkg/registry/ecr.go:89-95`).
4. **Cache invalidation:** If a pull fails with an auth error, the reconciler calls `InvalidateCache()` (`pkg/reconciler/reconciler.go:169`), and the next `GetAuth` call fetches a fresh token.

## Troubleshooting

See [Troubleshooting](../troubleshooting.md#configuration-errors) for common ECR errors, including flat-schema configuration mistakes, IMDSv2 failures, and IAM permissions.

## IMDSv2 provider internals

**Note:** This subsection describes the standalone `IMDSv2Provider` struct in `pkg/registry/imds.go`. The production ECR provider does not call this struct directly; it relies on the AWS SDK's built-in credential chain. The struct is included here as a reference for how IMDSv2 handshakes work, in case you need to debug IMDS-related issues.

The IMDSv2 reference implementation is a standalone `IMDSv2Provider` struct (`pkg/registry/imds.go:16-24`) with configurable HTTP client, base URL, and token TTL. The SDK's default credential chain includes:

1. Environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`).
2. Shared credentials file (`~/.aws/credentials`).
3. Container credentials (ECS task IAM roles).
4. **EC2 Instance Metadata Service** (IMDSv2, with IMDSv1 fallback).

When static keys are set in the config, the provider constructs a static credentials provider that takes precedence over the SDK's default chain (`cmd/dockenciler/main.go:140-149`).

## Build and deployment

The ECR provider requires the full AWS SDK v2. The Docker image includes the SDK binaries; the binary build uses `CGO_ENABLED=0` for static linking (`Dockerfile:20`). No additional AWS CLI tools are needed in the container — the SDK communicates with ECR through its HTTPS API endpoint.

## See also

- [Provider overview](README.md) — selection guide and comparison.
- [Configuration reference](../configuration.md) — full env-var and JSON schema.
- [Security](../security.md#ecr) — least-privilege IAM policy for ECR.
- [Examples](../examples/README.md) — ready-to-use ECR config files.
