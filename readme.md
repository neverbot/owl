# owl

Tiny, lightweight self-hosted observability. One static Go binary, one
container, one SQLite file. Built for a single low-end host where
Prometheus + Grafana is too heavy and a SaaS funnel is the wrong shape.

## What it does today

- Scrapes Prometheus-format `/metrics` endpoints on an interval,
  parses the text exposition format, persists samples to SQLite.
- Emits self-metrics from the Go runtime (`owl_runtime_goroutines`,
  `owl_runtime_alloc_bytes`, `owl_runtime_gc_pause_total_ms`) so the
  binary always has something to show, and exposes a
  Prometheus-format `/metrics` endpoint that can be scraped by owl
  itself or by an external Prometheus.
- Optional Linux host collector reading `/proc` (CPU per mode, load
  average, memory, network, disk). Off by default, enabled per
  `host.enabled` in the config.
- Optional Docker integration via the daemon socket: per-container
  CPU / memory / network / disk metrics (cAdvisor-compatible
  `container_*` names, plus `container_memory_anon_bytes` that
  excludes the kernel page cache and reflects the real process
  footprint) plus label-based scrape-target discovery (a container
  labelled `owl.scrape=true, owl.scrape.port=9100` is automatically
  scraped without editing `config.yml`).
- Stores time series in an embedded SQLite database with a dual
  retention policy: drop samples older than a time window, or once
  the database exceeds a size cap — whichever triggers first.
- Loads dashboards as JSON files from a directory. The format is a
  subset of Grafana's dashboard JSON (including `{{label}}`
  `legendFormat` templates), so a Grafana export can usually be
  dropped in and rendered with a best-effort result.
- Renders dashboards as server-rendered HTML at `/d/{id}`. A small
  vanilla-JS layer polls the API per panel and draws SVG charts with
  axes, a crosshair on hover, and a tooltip that lists series sorted
  by value. Light/dark theme toggle and a time-window picker
  (5m/15m/1h/6h/24h) sit in the top bar. No build pipeline, no SPA,
  no CDN.
- Evaluates a useful subset of PromQL on the fly. Panels whose
  queries fall outside the subset render with a clear "unsupported"
  message instead of breaking the whole dashboard.
- Threshold alerting: rules in `config.yml` fire / resolve via a
  webhook POST. Each rule fans out across every series its
  expression returns, so one rule covers every container or
  filesystem the matcher selects. Per-rule + per-series dedup and a
  configurable `for` hold.
- A scrape-health page at `/targets` (and JSON at `/api/targets`)
  lists every active target — explicit and Docker-discovered — with
  last scrape time, sample count, duration and any error.
- Atomic live reload of config, dashboards, scrape targets, alert
  rules and the alert webhook URL via `SIGHUP` or `POST /-/reload`.
  Optional mtime-based dashboards watcher (off by default) auto-
  reloads JSON edits without a signal.
- Structured logging through `log/slog` at the level configured by
  `log_level` (info / debug / warn / error). Graceful shutdown that
  waits for every collector and worker to drain before exiting.

## Try it from a clone

```sh
git clone https://github.com/neverbot/owl.git
cd owl
docker compose up
```

Then browse to `http://localhost:9090/`. You should see the bundled
**Owl Runtime** dashboard with three panels driven by the binary's own
Go runtime metrics, updating every few seconds.

`docker compose up` pulls
[`ghcr.io/neverbot/owl:master`](https://github.com/neverbot/owl/pkgs/container/owl)
by default. To rebuild from your local checkout instead — useful when
hacking on the code — run `docker compose up --build`. `docker compose
down -v` removes the data volume for a clean slate.

## Deploying

The published container image is the same artefact as the quickstart,
just consumed directly:

```sh
docker run --rm -d \
  --name owl \
  -p 9090:9090 \
  -v $PWD/config.yml:/etc/owl/config.yml:ro \
  -v $PWD/dashboards:/etc/owl/dashboards:ro \
  -v owl-data:/data \
  ghcr.io/neverbot/owl:master \
  --config /etc/owl/config.yml
```

A minimal `config.yml`:

```yaml
listen: "0.0.0.0:9090"

storage:
  path: "/data/owl.db"
  retention:
    time: 30d
    size: 500MB

scrape:
  default_interval: 30s
  default_timeout: 10s

targets:
  - name: traefik
    url: "http://traefik:8082/metrics"
    labels:
      job: traefik
    # Optional metric-name filters. `keep` is an allowlist applied
    # first; `drop` then prunes survivors. Regex (RE2 syntax). Use
    # them to keep the SQLite footprint reasonable when a target
    # exposes very verbose histograms or counters you do not need.
    # keep:
    #   - "^traefik_(service|router|entrypoint)_requests_total$"
    #   - "^traefik_(service|router|entrypoint)_request_duration_seconds_(count|sum)$"
    #   - "^traefik_open_connections$"
    # drop:
    #   - "_bucket$"

dashboards:
  dir: "/etc/owl/dashboards"
  # watch: true        # poll *.json mtimes and reload on change
  # watch_interval: 5s
```

## Configuration

Three layers, in order of increasing precedence:

1. **`config.yml`** (mounted; structured fields).
2. **Environment variables** for operational values and secrets:
   - `OWL_LISTEN_ADDR`
   - `OWL_LOG_LEVEL`
   - `OWL_DB_PATH`
   - `OWL_ALERT_WEBHOOK_URL`
3. **CLI flags**: `--config <path>`, `--check-config`, `--version`.

Validate without starting:

```sh
owl --config /etc/owl/config.yml --check-config
```

## Dashboards

Each `*.json` file in `dashboards.dir` becomes a dashboard whose ID is
the filename slug. Fields honoured:

- Top level: `title`, `panels[]`, `time.from`, `time.to`, `refresh`.
- Per panel: `id`, `title`, `type` (`timeseries`, `stat`, `gauge`),
  `gridPos.{x,y,w,h}` (24-column grid),
  `fieldConfig.defaults.unit`, `targets[].expr`,
  `targets[].legendFormat`.

Unknown fields are ignored silently. Panels with an unsupported query
or panel type render an explanation in place of the chart, but the rest
of the dashboard still works.

## PromQL subset

owl ships its own PromQL parser and evaluator — small, focused on the
features a single-host operator actually uses on a dashboard. Anything
outside the subset returns a parse error with a clear "unsupported"
message that names the offending construct, and panels that use such
queries render an explanation in place of the chart (the rest of the
dashboard keeps working).

### Supported

**Selectors**

```promql
metric_name
metric_name{job="api"}
metric_name{status=~"5..", method!="OPTIONS"}
```

Label-matcher operators: `=`, `!=`, `=~` (regex match), `!~` (regex
non-match). Regex anchoring follows Prometheus's convention: the
pattern is implicitly anchored at both ends.

**Functions**

```promql
rate(metric_name[1m])
rate(http_requests_total{status="200"}[5m])
irate(http_requests_total[1m])
increase(http_requests_total[1h])
```

Three range-vector functions, all sharing the shape `fn(expr[Nw])`
where `N` is a positive integer and `w` is `s`, `m`, or `h`. Counter
resets are detected in all three: when a sample is strictly less than
its predecessor the engine treats it as a fresh start (handles process
restarts cleanly).

- **`rate(expr[w])`** — per-second average across every sample pair
  in the window. Smooth; best for graphs.
- **`irate(expr[w])`** — per-second rate computed from only the last
  two samples in the window. Noisier than `rate`, but does not smear
  sudden bursts across the whole window. Best for alerts on volatile
  counters where you want the instantaneous reading.
- **`increase(expr[w])`** — total counter delta across the window.
  Mathematically equivalent to `rate(expr[w]) * window-seconds`.
  Best for "how many X happened in the last hour" tables and
  threshold rules.

**Aggregations**

```promql
sum(expr)            avg(expr)
min(expr)            max(expr)
count(expr)

sum   by (job)      (rate(http_requests_total[1m]))
avg   by (instance) (cpu_usage)
count by (status)   (http_requests_total)

sum without (instance, replica) (http_requests_total)
```

Operators: `sum`, `avg`, `min`, `max`, `count`. The `by (labels)` form
groups output series by the listed labels; the `without (labels)` form
drops the listed labels and groups by every label that remains.

**Arithmetic**

```promql
cpu_usage * 100
100 - cpu_idle_pct
node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes
errors_total / requests_total
sum by (status) (rate(http_requests_total[1m])) / sum (rate(http_requests_total[1m]))
```

Operators: `+`, `-`, `*`, `/`.

- **scalar OP expr** and **expr OP scalar** apply the op pointwise with
  the scalar.
- **expr OP expr** (series-on-series) matches LHS series to RHS series
  by **exact label set** (the metric name is dropped on output, per
  Prometheus convention). If one side has a single series and the other
  has many, the single series is broadcast against every other.
  Timestamps are inner-joined: only points that exist on both sides
  produce output.

Division by zero returns `0` (not `+Inf` / `NaN`), to keep the chart
layer clean of special-case rendering.

### Not supported

The list below is concrete. Any of these will return a parse error or
fail to match a series; the dashboard layer marks the affected panel
as "unsupported" with the engine's reason. PRs welcome.

**Functions**: `irate`, `increase`, `delta`, `idelta`, `deriv`,
`predict_linear`, `holt_winters`, `histogram_quantile`,
`*_over_time` (avg_over_time, max_over_time, …), `abs`/`ceil`/`floor`/`round`/`sqrt`/`ln`/`log2`/`log10`/`exp`,
`topk`/`bottomk`/`quantile`, `clamp`/`clamp_min`/`clamp_max`,
`label_replace`/`label_join`, `time`/`vector`/`scalar`,
`sort`/`sort_desc`, `absent`/`absent_over_time`, `changes`, `resets`.

**Aggregation operators**: `stddev`, `stdvar`, `quantile`.

**Vector-matching modifiers**: `on(labels)`, `ignoring(labels)`,
`group_left`/`group_right` — matching is exact-label-set only.

**Modifiers / syntax**: `offset`, `@` modifier (instant queries at a
fixed time), subqueries (`expr[5m:30s]`), `__name__` regex matching,
string literals, numeric literals as a top-level expression.

**Logical / set ops**: `and`, `or`, `unless`.

**Comparison ops**: `>`, `<`, `==`, `!=`, `>=`, `<=` (relevant for
alerting once it lands).

**Operator precedence**: left-to-right only, no PEMDAS. Use parentheses
around aggregations and `rate()` calls (the parser already requires
this for the unambiguous cases); chained arithmetic without parens
binds left.

If a dashboard you care about uses one of these and it would be easy
to add, raise an issue with the exact expression — most of these are a
short addition to the parser/evaluator once a real need pins them
down.

## API

| Endpoint | Description |
|---|---|
| `GET /` | Index of dashboards |
| `GET /d/{id}` | Server-rendered dashboard view |
| `GET /api/query?expr=&from=&to=&step=` | Evaluate a PromQL expression and return series JSON |
| `GET /api/dashboards` | List of dashboards |
| `GET /api/dashboards/{id}` | One dashboard with its panels |
| `GET /targets` | Server-rendered table of scrape targets and their last status |
| `GET /api/targets` | Per-target health (URL, last scrape, duration, samples, last error) as JSON |
| `GET /-/healthy` | `ok` on a healthy process |
| `POST /-/reload` (also `GET`) | Re-read config.yml and dashboards/*.json; same effect as `SIGHUP` |
| `GET /metrics` | owl's own metrics in Prometheus text exposition format |
| `GET /static/*` | Embedded JS / CSS assets |

Times are millisecond Unix timestamps.

## Alerting

Threshold rules in `config.yml` are evaluated against the query
engine on a fixed interval and produce JSON events posted to the
configured webhook on every state transition.

```yaml
alerts:
  webhook_url: "https://hooks.example/abc"
  rules:
    - name: high_cpu
      expr: "sum(rate(node_cpu_seconds_total{mode!=\"idle\"}[1m])) / count(node_cpu_seconds_total{mode=\"idle\"})"
      op: ">"
      threshold: 0.8
      for: 2m
    - name: low_disk
      expr: "node_filesystem_avail_bytes"
      op: "<"
      threshold: 1073741824
      for: 5m
```

Each rule has `op` (`>`, `>=`, `<`, `<=`), `threshold`, and a `for`
duration the condition must hold before owl marks the rule firing.

A rule **fans out across every series its expression returns**. The
`low_disk` example above produces one independent alert per
filesystem; `node_filesystem_avail_bytes{mountpoint="/"}` and
`{mountpoint="/data"}` track separate firing/resolved lifecycles
identified by their labels. The webhook receives one
`{"status":"firing", ...}` event per series when it crosses, and one
`{"status":"resolved", ...}` once that specific series clears — no
duplicates while a rule + series pair stays in either state.

Payload fields:

```json
{
  "rule":      "low_disk",
  "expr":      "node_filesystem_avail_bytes",
  "op":        "<",
  "threshold": 1073741824,
  "value":     536870912,
  "status":    "firing",
  "labels":    {"mountpoint": "/data", "fstype": "ext4"},
  "fired_at":  "2026-05-15T20:31:04Z"
}
```

`resolved` events carry the same shape plus `resolved_at`.

The webhook URL is normally injected via `OWL_ALERT_WEBHOOK_URL` so it
stays out of the YAML file.

### Testing the alerter locally

Run a one-shot HTTP echo on the host and point owl at it. The
[mendhak/http-https-echo](https://hub.docker.com/r/mendhak/http-https-echo)
image logs every received request to stdout:

```sh
docker run --rm -p 9091:8080 mendhak/http-https-echo:31
```

In `config.yml`, add a rule guaranteed to fire (owl always exports
its own runtime metrics):

```yaml
alerts:
  webhook_url: "http://host.docker.internal:9091/alert"  # Docker Desktop
  # webhook_url: "http://172.17.0.1:9091/alert"          # Linux
  rules:
    - name: smoke_test
      expr: "owl_runtime_goroutines"
      op: ">"
      threshold: 0
      for: 0s
```

Reload (`docker kill -s HUP owl`) and within ~10 s the echo container
prints the firing event. Drop the rule (or change the threshold to
something unreachable) and reload again to see the matching
`resolved` event.

On Docker Desktop, leave a one-second gap between saving the config
and sending `SIGHUP`: the bind-mount inode swap (most editors save by
write-and-rename) sometimes lags the signal by a beat, and owl will
re-read the old contents otherwise. On a Linux host the propagation
is immediate.

## Reload

owl re-reads `config.yml` and `dashboards/*.json` atomically on:

- `SIGHUP` to the process (`docker kill -s HUP owl`).
- `POST /-/reload` (or `GET /-/reload` for the curl-from-browser
  case).

The following take effect immediately:

- Dashboard JSON files in `dashboards.dir`.
- Explicit scrape targets in `targets:` (the scrape manager reconciles
  per-target goroutines without dropping in-flight samples).
- Alert rules in `alerts.rules` (existing rules keep their firing
  state so a reload doesn't re-trigger a webhook).
- `alerts.webhook_url` — the delivery sink is swapped atomically;
  in-flight POSTs finish against the old URL, the next state
  transition uses the new one.

What still requires a restart:

- `listen` — the HTTP server is constructed once.
- `storage.path` and `storage.retention` settings — storage is opened
  once.

Reload validates the new YAML before applying any change. If the file
is invalid, the in-memory config stays exactly as it was and the
endpoint returns `500` with the parse error.

## Build targets and runtime profile

- Container image: published to
  `ghcr.io/neverbot/owl` on every push to `master` and on every `v*`
  tag. Multi-arch (`linux/amd64`, `linux/arm64`). Built on
  `gcr.io/distroless/static-debian12:nonroot`; image size sits around
  12 MB today.
- Targets that shape the design: image ≤ 30 MB, idle RAM ≤ 20 MB,
  active RAM ≤ 40 MB, no external dependencies (no SaaS, no cloud
  sign-in, no telemetry).
- Tested on Go 1.25 with the race detector on.

## License

MIT — see [LICENSE](LICENSE).

## Building from source

```sh
make test        # go test ./... -race -count=1
make build       # CGO_ENABLED=0 static binary
make vet
make fmt
make tidy
```

Container:

```sh
docker build -t owl:dev .
docker run --rm owl:dev --version
```

## Status

Early but useful. Wired today: configuration loader, SQLite storage
with dual time+size retention, runtime self-metrics, a Linux host
collector (`/proc` parsing, opt-in), the HTTP scraper, the Docker
integration (per-container metrics + label-based scrape-target
discovery, opt-in), the PromQL subset documented above, the dashboard
loader (with Grafana-style `{{label}}` legend templating), the web
server that renders them, threshold alerting to a webhook, `SIGHUP` /
`POST /-/reload` for live reload of config / dashboards / scrape
targets / alert rules, a self-observability `/metrics` endpoint,
graceful shutdown that waits for every collector and worker to drain,
and structured logging (`log/slog`) at a level chosen by
`log_level` in the YAML or `OWL_LOG_LEVEL` in the env.

Only the listener address and the storage path / retention take their
value from the YAML at startup — changing them needs a process
restart. Everything else (targets, dashboards, alert rules, the alert
webhook URL) updates live on reload.

### Docker socket permission

owl runs as the distroless `nonroot` user (UID 65532) and the Docker
socket is typically owned by `root:root` on Docker Desktop and
`root:docker` on Linux production hosts. The container needs to be a
member of the socket's group to read it.

The bundled `compose.yml` uses `group_add: ["0"]` which works on
Docker Desktop. On a Linux server, replace `"0"` with your host's
docker group GID — `stat -c '%g' /var/run/docker.sock` prints it
(commonly `999` on Debian/Ubuntu).

### Host metrics caveat (macOS / Windows / Docker Desktop)

On Linux hosts, owl runs in a container that shares the host kernel.
Bind-mounting `/proc:/host/proc:ro` (already in the example
`compose.yml`) gives the host collector a direct view of the real
host's CPU / memory / load / disk / net stats.

On macOS and Windows, Docker Desktop runs a hidden Linux VM. The
"host" the container sees is that VM, not the underlying OS. The host
collector will surface the VM's metrics — useful for confirming the
loop works end-to-end, but not the OS-level metrics you'd see on a
production Linux deployment. There is no clean fix from owl's side; it
is a property of Docker Desktop's architecture.
