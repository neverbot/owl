---
title: Authentication
section: Operating
nav_order: 4
---

owl can scrape `/metrics` endpoints that require authentication.
Three modes are supported — Bearer token, HTTP Basic, and arbitrary
headers — plus an optional TLS block for self-signed internal
endpoints. Credentials never sit inline in the YAML: they come from
environment variables or files that the operator manages out of band.

## Auth modes

### Bearer token

The most common form, used by services exposing JWT-based
authentication. Set under `auth.bearer_token`:

```yaml
targets:
  - name: watchtower
    url: "http://watchtower:8080/v1/metrics"
    auth:
      bearer_token: "${WATCHTOWER_TOKEN}"
```

owl sends `Authorization: Bearer <token>` on every scrape.

### HTTP Basic

For services protected by a username/password (traefik dashboard,
classic exporters):

```yaml
targets:
  - name: traefik
    url: "https://traefik/metrics"
    auth:
      basic:
        username: "metrics"
        password: "file:/run/secrets/traefik_pass"
```

### Custom headers

Catch-all for API keys and proxy headers. Headers can be combined
with `bearer_token` or `basic` (the only conflict is setting
`Authorization` here when you already use one of the helpers — owl
refuses that config at load time):

```yaml
targets:
  - name: custom-api
    url: "https://api/metrics"
    auth:
      headers:
        X-API-Key: "${API_KEY}"

  - name: traefik
    url: "https://traefik/metrics"
    auth:
      basic:
        username: "metrics"
        password: "${TRAEFIK_PASSWORD}"
      headers:
        X-Tenant: "neverbot"
```

## Secret expansion

Any string in `config.yml` can reference a secret. Three forms:

- `${VAR}` — process environment variable. owl refuses to start if
  `VAR` is unset.
- `${VAR:-default}` — variable or the literal `default` if unset.
- `file:/abs/path` — file contents, trailing whitespace trimmed.

Escape with `$$`: the literal `$${FOO}` stays `${FOO}` in the
expanded config.

The expansion runs on every load **and** on every hot reload
(`SIGHUP` or `POST /-/reload`), so rotating a `file:/run/secrets/x`
needs only `kill -HUP <pid>` — no redeploy.

owl never logs the expanded value. Logs say `expanded ${WATCHTOWER_TOKEN}`
and stop there.

## TLS

For `https://` targets owl uses the system root pool by default.
Override per target with the optional `tls` block:

```yaml
targets:
  - name: internal-prom
    url: "https://prom.internal/api/v1/query"
    tls:
      ca_file: "/etc/owl/ca/internal.pem"

  - name: dev-only
    url: "https://dev.local/metrics"
    tls:
      insecure_skip_verify: true
```

- `insecure_skip_verify: true` disables certificate verification —
  for self-signed internal endpoints during development. Never set
  this in production.
- `ca_file` points at a PEM bundle and **replaces** the system roots
  for that target. Bundle external + private CAs in the same PEM if
  you need both.

## End-to-end example: Watchtower

Watchtower exposes a Prometheus endpoint behind a bearer token. To
scrape it from owl:

### 1. Enable the metrics endpoint in the watchtower compose service

```yaml
services:
  watchtower:
    image: nickfedor/watchtower:latest
    expose:
      - "8080"     # internal only; owl reaches it via the network
    environment:
      WATCHTOWER_HTTP_API_METRICS: "true"
      WATCHTOWER_HTTP_API_TOKEN_FILE: /run/secrets/watchtower_token
    secrets:
      - watchtower_token

secrets:
  watchtower_token:
    file: ./secrets/watchtower_token
```

### 2. Mount the same secret into the owl container

```yaml
services:
  owl:
    image: ghcr.io/neverbot/owl:latest
    secrets:
      - watchtower_token
```

### 3. Reference it from owl's config

```yaml
targets:
  - name: watchtower
    url: "http://watchtower:8080/v1/metrics"
    auth:
      bearer_token: "file:/run/secrets/watchtower_token"
```

After a restart (or `SIGHUP`), the `/targets` page shows the
watchtower row with `auth=bearer` and ticks every 30s like any other
target.

## Error messages on `/targets`

owl gives 401 and 403 distinct messages so the operator can act
without `curl`-ing the endpoint:

- `401 Unauthorized — check auth: block in config for <url>`
- `403 Forbidden — credentials accepted but access denied for <url>`

The HTTP status code is also exposed on `/api/targets` as
`last_status_code`, useful for scripting around the API.

## Limits

The v1 surface intentionally does not cover:

- Client mTLS (`cert_file` / `key_file`).
- OAuth / OIDC client credentials.
- AWS SigV4, GCP IAM, Azure auth.
- TLS `server_name` override and `min_version` pinning.

If you need one of these, open an issue with the use case.
