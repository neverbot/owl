---
title: Host collector
section: Operating
nav_order: 1
---

The host collector reads Linux kernel counters from `/proc` (and a
few from `/sys`) and turns them into Prometheus-style metrics with
familiar `node_*` names — CPU per mode, load average, memory,
network, disk. It is off by default and enabled per `host.enabled`
in the config.

```yaml
host:
  enabled: true
  proc_path: "/host/proc"
  sys_path: "/host/sys"
  interval: 5s
```

The bundled compose file bind-mounts `/proc:/host/proc:ro` and
`/sys:/host/sys:ro` so the container sees the real kernel's
counters. The bundled **Host** dashboard plots them.

{{> chart fixture=host-cpu expr="rate(node_cpu_seconds_total[1m])" unit=percent title="CPU by mode"}}

## Docker Desktop VM caveat

On Linux hosts, the container shares the host kernel directly.
Bind-mounting `/proc` gives the collector an honest view of the real
host's CPU, memory, load, disk and network.

On macOS and Windows, Docker Desktop runs a hidden Linux VM. The
"host" the container sees is **that VM**, not the underlying OS. The
collector will surface the VM's metrics — useful for confirming the
loop works end-to-end, but not the OS-level metrics you would see on
a production Linux deployment. There is no clean fix from owl's side;
it is a property of Docker Desktop's architecture.

In practice: develop and smoke-test on Docker Desktop, then trust
real numbers only when reading from a Linux production host.

## What is collected

- **CPU** — `node_cpu_seconds_total` per `cpu` × `mode` (user,
  system, iowait, idle, …). Use `rate(...[1m])` to plot percentage
  usage.
- **Load average** — `node_load1`, `node_load5`, `node_load15`.
- **Memory** — `node_memory_MemTotal_bytes`,
  `node_memory_MemAvailable_bytes`, and the cached / buffered
  counterparts used by the Host dashboard.
- **Network** — `node_network_receive_bytes_total` /
  `_transmit_bytes_total` per device.
- **Disk / filesystem** — `node_filesystem_avail_bytes`,
  `node_filesystem_size_bytes` per mountpoint; per-device IO
  counters.

The collector is intentionally lean: no per-process scrape, no
`/proc/pressure` parsing, no GPU. owl does not ship `gopsutil` and
hand-rolls the few `/proc` files it needs to keep the binary small.
