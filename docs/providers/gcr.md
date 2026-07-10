# GCR / Artifact Registry Provider

## Overview

The GCR provider uses the **Docker Registry HTTP API v2** to interact with Google Container Registry (GCR) and Google Artifact Registry. It does not use the Google Cloud SDK, `gcloud` CLI, or the Artifact Registry gRPC API — it speaks the same registry protocol that the Docker engine uses (`HEAD /v2/<path>/manifests/<tag>`).

This means the provider works against `gcr.io`, `*.gcr.io`, and `*-docker.pkg.dev` with a single code path (`pkg/registry/gcr.go:29-30`).

## Supported hostnames

The following hostnames are **conventionally supported** (these are the well-known GCR and Artifact Registry endpoints):

- `gcr.io`
- `*.gcr.io` (e.g. `us.gcr.io`, `asia.gcr.io`, `eu.gcr.io`)
- `*-docker.pkg.dev` (e.g. `us-docker.pkg.dev`, `europe-docker.pkg.dev`)

**Important:** The image reference parser (`gcrParseFullRef` at `pkg/registry/gcr.go:353-385`) accepts **any hostname** without validation. If you configure an unsupported hostname, the registry API calls will reach a server that does not speak the Docker Registry HTTP API v2, and Docker pulls will fail. The hostname list above is a convention, not an enforced validation.

## Configuration

The GCR provider is configured under `registry.gcr` in the JSON config file, with a nested `auth` sub-block (`pkg/config/config.go:20-27`).

### JSON config — ADC (default)

```json
{
  "registry": {
    "type": "gcr",
    "gcr": {
      "auth": {
        "method": "adc"
      }
    }
  }
}
```

### JSON config — service account

```json
{
  "registry": {
    "type": "gcr",
    "gcr": {
      "auth": {
        "method": "service_account",
        "service_account_file": "/etc/dockenciler/gcp-key.json"
      }
    }
  }
}
```

### Environment variables

| Variable | Description | Default |
|---|---|---|
| `DOCKENCILER_REGISTRY_TYPE` | Set to `gcr` | `""` |
| `DOCKENCILER_REGISTRY_GCR_AUTH_METHOD` | Auth method: `adc` or `service_account` | `"adc"` |
| `DOCKENCILER_REGISTRY_GCR_AUTH_SERVICE_ACCOUNT_FILE` | Path to GCP service account JSON key | `""` |

## Authentication

### Application Default Credentials (default)

When `method` is `"adc"` (or empty, since `"adc"` is the default), the provider calls `google.DefaultTokenSource` (`pkg/registry/gcr.go:100-103`). This picks up credentials from the following sources in priority order:

1. **`GOOGLE_APPLICATION_CREDENTIALS`** environment variable pointing to a service account JSON key file.
2. **`gcloud auth application-default login`** — user credentials cached on the local machine.
3. **GCE/GKE metadata server** — the built-in service account of the Compute Engine instance or GKE node.
4. **Workload Identity** — on GKE with workload identity enabled.

The scope requested is the broad `https://www.googleapis.com/auth/cloud-platform` scope. A read-only scope such as `https://www.googleapis.com/auth/devstorage.read_only` would be sufficient for registry pulls, but the code currently requests the full cloud-platform scope.

This is the **recommended** method because:
- No service account key file to manage or rotate.
- Works automatically on GCE, GKE, Cloud Run, and Cloud Build.
- Falls back to `gcloud` credentials for local development.

### Service account JSON key

When `method` is `"service_account"`, the provider reads the JSON key file from the filesystem path specified in `service_account_file` (`pkg/registry/gcr.go:104-121`). The path is:

- Cleaned with `filepath.Clean()` (`pkg/registry/gcr.go:109`).
- Rejected if it contains `..` (path traversal protection, `pkg/registry/gcr.go:110-111`).

**Do not embed the JSON key contents inline in the config file or environment variable.** The provider only accepts a file path; it reads the file at startup and constructs an OAuth2 token source. Embedded JSON is not supported.

The service account must have at minimum the `roles/artifactregistry.reader` role (or `roles/storage.objectViewer` for GCR-hosted images using the legacy GCS backend). See [Security](../security.md#gcr) for more.

## Image references

The provider parses image references using `gcrParseFullRef` (`pkg/registry/gcr.go:353-385`). Examples:

| Image reference | Host | Project path | Ref |
|---|---|---|---|
| `gcr.io/my-project/myapp:latest` | `gcr.io` | `my-project/myapp` | `latest` |
| `us.gcr.io/my-project/myapp:v1.2.3` | `us.gcr.io` | `my-project/myapp` | `v1.2.3` |
| `us-docker.pkg.dev/my-project/my-repo/myapp:latest` | `us-docker.pkg.dev` | `my-project/my-repo/myapp` | `latest` |
| `europe-docker.pkg.dev/p/r:v1` | `europe-docker.pkg.dev` | `p/r` | `v1` |

The parsing logic:

1. The first `/` separates the host from the rest (e.g. `gcr.io` from `my-project/myapp:latest`).
2. If the reference contains `@sha256:...`, that becomes the ref. Otherwise, the last `:` separates the project path from the tag. If no `:` is found, the ref defaults to `latest`.
3. Empty project paths cause an error — a bare hostname like `gcr.io` with no repository is invalid.

## Digest resolution

The `GetLatestDigest` method (`pkg/registry/gcr.go:203-227`) handles four cases based on criteria:

- **No criteria (default):** Uses the tag from the image reference (defaults to `latest`).
- **`criteria.version`:** Uses the version value as the target ref.
- **`criteria.regex`:** Lists all tags via `GET /v2/<projectPath>/tags/list`, filters by regex, sorts descending (best-effort semver), then fetches the manifest HEAD for the first matching tag (`pkg/registry/gcr.go:231-257`).
- **`criteria.digest`:** Returns the digest as-is (no API call needed).

The manifest lookup (`headManifest` at `pkg/registry/gcr.go:309-343`) performs an HTTP `HEAD` request against the Docker Registry v2 API:

```http
HEAD /v2/<projectPath>/manifests/<ref> HTTP/1.1
Host: <registry-host>
Authorization: Bearer <access-token>
Accept: application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.index.v1+json
```

The response `Docker-Content-Digest` header provides the current digest. This is the same protocol the Docker engine uses for digest resolution, ensuring the returned value matches what Docker considers the canonical digest.

## Registry API flow

The provider communicates with the registry exclusively through the Docker Registry HTTP API v2. This means it works without any Google Cloud SDK libraries — no gRPC, no `google.golang.org/api/artifactregistry`. The only Google dependency is `golang.org/x/oauth2` for token management.

Supported API endpoints:

| Method | Endpoint | Purpose |
|---|---|---|
| `GET /v2/<projectPath>/tags/list` | `listTags` (`gcr.go:274-305`) | Enumerate all tags for regex matching |
| `HEAD /v2/<projectPath>/manifests/<ref>` | `headManifest` (`gcr.go:309-343`) | Get the current digest for a tag |

## Token caching

The provider caches the OAuth2 access token with a **5-minute buffer** before expiry (`pkg/registry/gcr.go:159-182`). The token is refreshed when:

- No cached token exists (first request).
- The cached token is expired or invalid.
- The cached token expires within the next 5 minutes.

The cache is invalidated manually via `InvalidateCache()` (`pkg/registry/gcr.go:128-133`), which the reconciler calls after an auth failure to force a fresh token on retry.

## Troubleshooting

See [Troubleshooting](../troubleshooting.md#gcr-could-not-find-default-credentials) for common GCR errors, including ADC credential resolution, service account file path issues, and token expiry.

## See also

- [Provider overview](README.md) — selection guide and comparison.
- [Configuration reference](../configuration.md) — full env-var and JSON schema.
- [Security](../security.md#gcr) — least-privilege IAM for GCR/Artifact Registry.
- [Examples](../examples/README.md) — ready-to-use GCR config files.
