---
title: Docker integration
section: Operating
nav_order: 2
---

owl talks to the Docker daemon over its UNIX socket to do two
related things:

- **Per-container metrics** — CPU, memory (including a "real
  process footprint" gauge), network and disk for every running
  container, with cAdvisor-compatible `container_*` names.
- **Label-based scrape-target discovery** — any container with the
  right labels is automatically added to the scrape list without
  editing `config.yml`.

Both features share one socket connection and can be enabled
independently:

```yaml
docker:
  enabled: true
  socket_path: "/var/run/docker.sock"
  metrics:
    enabled: true
    interval: 10s
  discovery:
    enabled: true
    label_prefix: "owl.scrape"
    interval: 30s
```

The bundled **Containers** dashboard plots the result.

{{> chart fixture=container-memory expr="container_memory_anon_bytes" unit=bytes title="Container memory (anon)"}}

## Label-based discovery

Mark a container as a scrape target by adding two labels:

```yaml
services:
  app:
    image: my-app:latest
    labels:
      owl.scrape: "true"
      owl.scrape.port: "9100"
```

owl will reach the container on its own network (typically the
compose network) at `http://<container_name>:9100/metrics` and treat
it as if it had been listed under `targets:`. Removing the labels
removes the target on the next discovery tick — `targets:` configured
explicitly in YAML stays untouched.

Optional label refinements:
- `owl.scrape.path` — override the default `/metrics` path.
- `owl.scrape.interval` — per-target scrape interval.
- `owl.scrape.<label>` — copied onto every scraped series as a
  label (subject to the `label_prefix`).

## `container_memory_anon_bytes` vs `container_memory_usage_bytes`

cgroup v2 reports a container's memory usage as anonymous memory
**plus** the kernel page cache the process happens to be holding for
that container. `container_memory_usage_bytes` is the latter — the
raw kernel number. It tracks the full working set including caches
that the kernel will gladly evict under pressure.

`container_memory_anon_bytes` is what you usually want for an alert
or a capacity chart: anonymous memory only, i.e. heap / stacks /
non-evictable allocations. It is the "real process footprint" — the
number that, if it grows unboundedly, indicates a leak rather than a
working set warming up.

Use `_anon_bytes` for headroom alerts; use `_usage_bytes` only when
you really want to know how much memory pressure the container is
generating on the host (including evictable caches).

## Socket permissions

owl runs as the distroless `nonroot` user (UID 65532). The Docker
socket is normally owned by `root:root` on Docker Desktop and
`root:docker` on production Linux hosts. The container needs to be
in the socket's group to read it.

The bundled `compose.yml` uses `group_add: ["0"]` which works on
Docker Desktop. On a Linux server, replace `"0"` with your host's
docker group GID — `stat -c '%g' /var/run/docker.sock` prints it
(commonly `999` on Debian / Ubuntu).
