# Docker Hub Provider

## Overview

The Docker Hub provider uses the **Docker Registry HTTP API v2** to interact with Docker Hub (`registry-1.docker.io`). It obtains **bearer tokens** from `auth.docker.io` for registry API queries. When credentials are configured (username and password or personal access token), they are sent as HTTP Basic auth on the token request, enabling authenticated access to public and private repositories. Without credentials, anonymous bearer tokens are used for public images only.

When credentials are configured, the provider returns them from `GetAuth` so the Docker daemon can authenticate pulls (`pkg/docker/docker.go:282-299`). For anonymous access, empty credentials are returned and the Docker daemon handles anonymous pulls.

The source is in `pkg/registry/dockerhub.go`.

## Supported reference formats

Docker Hub image references come in several forms, and the provider normalizes them all:

| Image reference | Host | Repository path | Ref |
|---|---|---|---|
| `postgres:18-alpine` | `registry-1.docker.io` | `library/postgres` | `18-alpine` |
| `postgres` | `registry-1.docker.io` | `library/postgres` | `latest` |
| `library/postgres:18-alpine` | `registry-1.docker.io` | `library/postgres` | `18-alpine` |
| `myuser/myimage:tag` | `registry-1.docker.io` | `myuser/myimage` | `tag` |
| `docker.io/library/postgres:18-alpine` | `docker.io` | `library/postgres` | `18-alpine` |
| `index.docker.io/library/postgres:18-alpine` | `docker.io` | `library/postgres` | `18-alpine` |
| `registry-1.docker.io/library/postgres:18-alpine` | `registry-1.docker.io` | `library/postgres` | `18-alpine` |

The parsing logic (`dockerHubParseRef` at `pkg/registry/dockerhub.go`):

1. Explicit host prefixes (`docker.io/`, `index.docker.io/`, `registry-1.docker.io/`) are stripped and the host is set accordingly.
2. If no explicit host is found and the first path segment contains a `.` or `:`, it's treated as a non-Docker-Hub registry host.
3. Single-component names (no `/`) are auto-prefixed with `library/` for official images.
4. Tag/digest separators (`:` or `@`) are parsed to extract the reference.

## Configuration

The Docker Hub provider requires no credentials for public images. For authenticated access (private repositories or to avoid rate limits), provide a Docker Hub username and password or personal access token.

### JSON config

```json
{
  "registry": {
    "type": "dockerhub",
    "dockerhub": {
      "username": "",
      "password": ""
    }
  }
}
```

Leave `username` and `password` empty for anonymous public image access.

### Environment variables

| Variable | Description | Default |
|---|---|---|
| `REGISTRY_TYPE` | Set to `dockerhub` | `""` |
| `REGISTRY_DOCKERHUB_USERNAME` | Docker Hub username (leave empty for anonymous) | `""` |
| `REGISTRY_DOCKERHUB_PASSWORD` | Docker Hub password or personal access token | `""` |

> **Note:** Private Docker Hub repositories are supported when credentials are configured. Without credentials, only public images can be updated.

## Digest resolution

The `GetLatestDigest` method (`pkg/registry/dockerhub.go`) handles four cases based on criteria:

- **No criteria (default):** Uses the tag from the image reference (defaults to `latest`).
- **`criteria.version`:** Uses the version value as the target ref.
- **`criteria.regex`:** Lists all tags via `GET /v2/<repoPath>/tags/list`, filters by regex, sorts descending, then fetches the manifest HEAD for the first matching tag.
- **`criteria.digest`:** Returns the digest as-is (no API call needed).

The manifest lookup performs an HTTP `HEAD` request against the Docker Registry v2 API:

```http
HEAD /v2/<repoPath>/manifests/<ref> HTTP/1.1
Host: registry-1.docker.io
Authorization: Bearer <anonymous-token>
Accept: application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.index.v1+json
```

The response `Docker-Content-Digest` header provides the current digest.

## Authentication flow

### Token acquisition

Docker Hub issues bearer tokens via its auth service. When credentials are configured, the token request includes HTTP Basic authentication:

```
GET https://auth.docker.io/token?service=registry.docker.io&scope=repository:<repo>:pull
Authorization: Basic base64(username:password)
```

Without credentials, anonymous token requests are sent (same endpoint, no Authorization header).

The response is a JSON object:

```json
{
  "token": "<bearer-token>",
  "expires_in": 300,
  "issued_at": "2024-01-01T00:00:00Z"
}
```

The token is cached and reused until 1 minute before expiry (`pkg/registry/dockerhub.go`).

### Docker daemon authentication

When credentials are configured, the provider returns them from `GetAuth` so the Docker daemon can authenticate pulls. For anonymous access, empty credentials are returned — the `Authenticate` call is a no-op that stores the server address but skips `RegistryLogin`.

## API surface

The provider implements the full `Registry` interface (`pkg/registry/registry.go:24-29`):

| Method | Purpose |
|---|---|
| `GetLatestDigest` | Resolve the current digest for an image reference |
| `GetImageVersion` | Extract the tag from the image reference |
| `GetAuth` | Return auth credentials (username/password when configured, empty for anonymous) |
| `InvalidateCache` | Clear the cached anonymous token |

## Token caching

The provider caches the bearer token until 1 minute before expiry. The token is refreshed when:

- No cached token exists (first request).
- The cached token expires within the next minute.

A global mutex (`dockerHubTokenMu`) serializes token fetches, and double-checked locking ensures only one goroutine fetches a new token when multiple goroutines concurrently request it.

The cache is invalidated manually via `InvalidateCache()`, which the reconciler calls after an auth failure to force a fresh token on retry.

## Troubleshooting

### "registry returned status 404"

The image or tag does not exist on Docker Hub. Verify the image name and tag.

### "registry returned status 401"

The repository is private or the anonymous token was rejected. Configure a Docker Hub username and password (or personal access token) in the `dockerhub` config block to enable authenticated access.

### "token endpoint returned status 403"

The `auth.docker.io` token service denied the request. This can happen if Docker Hub rate-limits anonymous access (100 pulls per 6 hours per IP). Configure Docker Hub credentials to avoid rate limits, or switch to ECR/GCR if rate limits are a concern.

## See also

- [Provider overview](README.md) — selection guide and comparison.
- [Configuration reference](../configuration.md) — full env-var and JSON schema.
- [Examples](../examples/README.md) — ready-to-use Docker Hub config files.
