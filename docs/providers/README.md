# Providers

Dockenciler watches containers running images hosted on a container registry. To determine whether an update is available, it checks the registry for the latest image digest and compares it with the digest of the currently running image.

Two registry providers are supported. Choose based on where your images are stored.

## When to use ECR

- Your container images are hosted in **Amazon Elastic Container Registry (ECR)**.
- You run on EC2 and want IMDSv2 instance-role-based authentication (no static keys to manage).
- You already use AWS SDK v2 and IAM for access control.

## When to use GCR / Artifact Registry

- Your container images are hosted in **Google Container Registry (gcr.io)** or **Google Artifact Registry (`*-docker.pkg.dev`)**.
- You want zero-config authentication via Application Default Credentials on GCE, GKE, or Cloud Run.
- You have a GCP service account JSON key for offline or non-GCP environments.

## Comparison

| Feature | ECR | GCR / Artifact Registry |
|---|---|---|
| Auth methods | Static IAM keys or IMDSv2 instance role | ADC (default) or service-account JSON key file |
| Image parsing | Strips `account.dkr.ecr.region.amazonaws.com/` prefix | Splits into `host`, `projectPath`, `ref` |
| Underlying API | AWS SDK v2 (`DescribeImages`, `GetAuthorizationToken`) | Docker Registry HTTP API v2 (`HEAD /v2/.../manifests/...`) |
| Auth username | `"AWS"` (`pkg/registry/ecr.go:94`, 104, 147) | `"oauth2accesstoken"` (`pkg/registry/gcr.go:152`) |
| Token buffer | 5 minutes before ECR token expiry (`pkg/registry/ecr.go:89`, 103) | 5 minutes before OAuth2 token expiry (`pkg/registry/gcr.go:164`, 172) |
| Supported hostnames | Any valid ECR endpoint (`*.dkr.ecr.*.amazonaws.com`) | Convention: `gcr.io`, `*.gcr.io`, `*-docker.pkg.dev` (parser accepts any hostname but Docker pulls fail for unsupported hosts) |

## How the provider is selected

The provider is selected by setting `registry.type` in the JSON config or `DOCKENCILER_REGISTRY_TYPE` in the environment. The value must be `"ecr"` or `"gcr"` (`cmd/dockenciler/main.go:51-69`). Any other value causes the binary to exit with `"Unsupported registry type"`.

Each provider's configuration (region, auth method, credentials) lives in its own nested sub-block under `registry`:

- ECR: `registry.ecr.region`, `registry.ecr.access_key`, `registry.ecr.secret_key`
- GCR: `registry.gcr.auth.method`, `registry.gcr.auth.service_account_file`

These are peer pointer fields (`pkg/config/config.go:29-33`). They are **mutually exclusive** — only one can be non-nil at a time. Setting fields at the wrong level (e.g. `registry.region` instead of `registry.ecr.region`) causes a startup error.

## Image parsing is provider-local

Each provider parses image references independently. There is no shared `parseImageRef` helper. This is by design: ECR and GCR have fundamentally different registry URL structures, and a shared parser would introduce unnecessary coupling.

- ECR strips the `<account>.dkr.ecr.<region>.amazonaws.com/` prefix to extract the repository name and tag.
- GCR splits the reference into three parts: `host`, `projectPath`, and `ref` (tag or digest).

## Next steps

- [ECR provider guide](ecr.md) — configuration, authentication, image references, troubleshooting.
- [GCR / Artifact Registry provider guide](gcr.md) — configuration, authentication methods, token caching.
- [Configuration reference](../configuration.md) — full env-var and JSON schema documentation.
- [Examples](../examples/README.md) — ready-to-use config files for ECR and GCR scenarios.
