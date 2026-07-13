# Troubleshooting

## Configuration errors

### "ECR registry type requires ecr configuration"

**Error text:** `"ECR registry type requires ecr configuration"` at `cmd/dockenciler/main.go:129-131`.

**Cause:** The `registry` block uses a flat schema — `registry.region` instead of the required nested form `registry.ecr.region`.

**Wrong:**
```json
{
  "registry": {
    "type": "ecr",
    "region": "us-east-1"
  }
}
```

**Correct:**
```json
{
  "registry": {
    "type": "ecr",
    "ecr": {
      "region": "us-east-1"
    }
  }
}
```

The `registry.ecr` and `registry.gcr` fields are peer pointer substructs (`pkg/config/config.go:29-33`). The flat form silently fails to populate the inner struct, and the nil-check at `cmd/dockenciler/main.go:129` causes the error.

### "GCR registry type requires gcr configuration"

Same root cause as above, but for GCR. Use `registry.gcr` instead of flat fields:

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

### `EXCLUSIONS=id1,id2` not working

The `exclusions` field is a `[]string` in Go. The Viper configuration (`pkg/config/config.go`) does not include a `StringToSliceHookFunc` to split comma-separated strings. The env-var form is not verified to work.

**Solution:** Use the JSON array form in `config.json`:

```json
{
  "exclusions": ["abc123def456", "789ghi012jkl"]
}
```

See the [Configuration reference](configuration.md#application) for details and related discussion.

## Authentication errors

### ECR IMDSv2 fails on non-EC2 hosts

**Error:** The reconciler fails to authenticate with ECR when running on a non-EC2 host (local Docker, on-premise server).

**Cause:** The AWS SDK attempts to resolve credentials from the EC2 Instance Metadata Service (IMDS) at `http://169.254.169.254`. Non-EC2 hosts do not expose this endpoint.

**Solution:** Provide static IAM access keys:

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

### ECR access denied

**Error:** `"failed to get authorization token"` or `"AccessDeniedException"` from ECR.

**Cause:** The IAM user or role does not have the `ecr:GetAuthorizationToken` permission.

**Solution:** Attach a policy with at minimum:

```json
{
  "Effect": "Allow",
  "Action": "ecr:GetAuthorizationToken",
  "Resource": "*"
}
```

### GCR: "could not find default credentials"

**Error:** `"failed to build GCR token source: ... could not find default credentials"`.

**Cause:** The provider is using `method: "adc"` (default) but no Application Default Credentials are available.

**Solution:** Set one of the following:
- **`GOOGLE_APPLICATION_CREDENTIALS`** environment variable pointing to a service account JSON key file.
- Run `gcloud auth application-default login` on the host machine.
- Run on GCE/GKE with a default service account that has `artifactregistry.reader` or `storage.objectViewer`.

Alternatively, switch to the `service_account` method:

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

### GCR: "invalid_grant" or token expired

**Error:** The OAuth2 token refresh fails with `"invalid_grant"`.

**Cause:** The service account key file path is wrong, the file is unreadable, the JSON is malformed, or the service account has been revoked/deleted.

**Solution:** Verify the file path exists and is readable:

```bash
ls -la /etc/dockenciler/gcp-key.json
```

The provider cleans the path with `filepath.Clean()` and rejects `..` traversal (`pkg/registry/gcr.go:109-111`). Ensure the path does not contain `..` segments.

## Image resolution errors

### Reconciliation runs but no updates

**Scenario:** Dockenciler starts, runs reconciliation, logs "checked" containers, but never logs "update required". Containers have newer images available.

**Checklist:**

1. **Label filter:** Verify the container has the label matching `docker.label_filter` (default `dockenciler.autoupdate=true`).
   ```bash
   docker inspect <container> --format '{{json .Config.Labels}}'
   ```
2. **Exclusion list:** Check if the container ID is in the `exclusions` array.
3. **Criteria regex:** If `criteria.regex` is set, the image tag must match the regex. For example, `^v\\d+\\.\\d+\\.\\d+$` only matches tags like `v1.2.3`, not `latest`.
4. **Image digest comparison:** If the image is pinned by digest, the reconciler compares digests — a tag push to the registry does not change the digest of that specific digest-pinned image.

### "manifest unknown" or "image not found"

**Error:** Docker pull fails with `"manifest unknown"` or `"image not found"`.

**Causes:**

- The registry hostname is not recognized by the Docker engine. For GCR, ensure the hostname is among the conventionally supported set: `gcr.io`, `*.gcr.io`, `*-docker.pkg.dev`. The GCR provider's parser accepts any hostname (`gcrParseFullRef` at `pkg/registry/gcr.go:353-385`), but Docker pulls will fail for unsupported hosts.
- The image tag or digest does not exist in the registry.
- ECR authorization token has expired (less likely due to the 5-minute buffer, but possible if the token refresh fails silently).

## Runtime errors

### "invalid timezone"

**Error:** `"Invalid timezone"` at startup.

**Cause:** The `timezone` field is set to a string that is not empty, `"Host"`, or a valid IANA timezone name.

**Solution:** Set `timezone` to one of:

- `""` (empty) — uses system timezone.
- `"Host"` — uses system timezone (same as empty).
- A valid IANA name, e.g. `"America/New_York"`, `"Europe/London"`, `"Asia/Tokyo"`.

If you are unsure of the IANA name, use `""` or `"Host"` to fall back to the system timezone.

### Reconciler exits immediately

**Symptom:** Dockenciler starts, logs the banner, then exits with an error before the first reconciliation.

**Check the logs:**

- `"Invalid timezone"` — see above.
- `"Failed to load config"` — check JSON syntax or env var completeness.
- `"Failed to create <provider> provider"` — check the registry configuration is nested correctly.
- `"Unsupported registry type"` — `registry.type` must be `ecr` or `gcr`.

Enable debug logging to see the full startup sequence:

```bash
LOG_LEVEL=debug
```

## Getting help

If the troubleshooting steps above do not resolve your issue, open a GitHub issue at:

[https://github.com/omarismael/dockenciler/issues](https://github.com/omarismael/dockenciler/issues)

Include the following in your bug report:

- The full startup log (with `LOG_LEVEL=debug`).
- Your configuration file (with secrets redacted).
- The Docker version (`docker version`) and platform.
- Whether you are running the binary or the Docker image.

## See also

- [Configuration reference](configuration.md) — env vars, defaults, and schema.
- [Security](security.md) — permissions, IAM policies, and secret handling.
- [Operations](operations.md) — dry-run, log levels, and runtime behavior.
