# GHCR Provider

## Overview

The GHCR provider uses the **Docker Registry HTTP API v2** to interact with the GitHub Container Registry (`ghcr.io`). It obtains **bearer tokens** from `ghcr.io/token` (`service=ghcr.io`, `scope=repository:<owner>/<repo>:pull`) for registry API queries. Tokens are cached per repository and reused until 1 minute before expiry.

When credentials are configured (GitHub username + personal access token with the `read:packages` scope), they are sent as HTTP Basic auth on the token request, enabling access to private repositories, and returned from `GetAuth` so the Docker daemon can authenticate pulls. Without credentials, anonymous tokens are used for public images only, and empty credentials are returned for daemon pulls.

The source is in `pkg/registry/ghcr.go`.

## Supported reference formats

| Image reference | Host | Repository path | Ref |
|---|---|---|---|
| `ghcr.io/owner/repo:tag` | `ghcr.io` | `owner/repo` | `tag` |
| `ghcr.io/owner/repo` | `ghcr.io` | `owner/repo` | `latest` |
| `ghcr.io/owner/repo@sha256:abc...` | `ghcr.io` | `owner/repo` | `sha256:abc...` |

References must start with `ghcr.io/` and include an `owner/repo` path. Anything else (including bare Docker Hub names) is rejected with an error. Conversely, pointing `registry.type=dockerhub` at a `ghcr.io` image fails fast with an error suggesting the `ghcr` type — the Docker Hub token endpoint (`auth.docker.io`) cannot issue tokens for GHCR repositories.

> **Note:** a bare `sha256:` ID never reaches this parser in practice. Once a
> tag moves, Docker's list API decays the container's image to its ID, but
> `ListContainers` recovers the create-time reference via inspect before the
> reconciler calls the provider.

## Configuration

For public images no credentials are needed. For private images, create a personal access token with the `read:packages` scope ([classic PAT](https://github.com/settings/tokens) or fine-grained) and configure it alongside your GitHub username.

### JSON config

```json
{
  "registry": {
    "type": "ghcr",
    "ghcr": {
      "username": "octocat",
      "password": "ghp_..."
    }
  }
}
```

Leave `username` and `password` empty for anonymous public image access.

### Environment variables

| Variable | Description | Default |
|---|---|---|
| `REGISTRY_TYPE` | Set to `ghcr` | `""` |
| `REGISTRY_GHCR_USERNAME` | GitHub username (leave empty for anonymous) | `""` |
| `REGISTRY_GHCR_PASSWORD` | Personal access token with `read:packages` scope | `""` |

> **Note:** Private GHCR repositories are supported when credentials are configured. Without credentials, only public images can be updated.

## Digest resolution

The `GetLatestDigest` method (`pkg/registry/ghcr.go`) handles four cases based on criteria:

- **No criteria (default):** Uses the tag from the image reference (defaults to `latest`).
- **`criteria.version`:** Uses the version value as the target ref.
- **`criteria.regex`:** Lists all tags via `GET /v2/<owner>/<repo>/tags/list`, filters by regex, sorts descending, then fetches the manifest HEAD for the first matching tag.
- **`criteria.digest`:** Returns the digest as-is (no API call needed).

The manifest lookup performs an HTTP `HEAD` request against the Docker Registry v2 API:

```http
HEAD /v2/<owner>/<repo>/manifests/<ref> HTTP/1.1
Host: ghcr.io
Authorization: Bearer <token>
Accept: application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.index.v1+json
```

The response `Docker-Content-Digest` header contains the digest, which the reconciler compares against the running container's image digest.
