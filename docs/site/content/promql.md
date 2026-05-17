---
title: PromQL
section: Reference
nav_order: 2
---

owl ships its own PromQL parser and evaluator — small, focused on
the features a single-host operator actually uses on a dashboard.
Anything outside the subset returns a parse error with a clear
"unsupported" message that names the offending construct. Panels that
use such queries render an explanation in place of the chart; the rest
of the dashboard keeps working.

The full machine-generated capability matrix is at the bottom of the
page. The narrative sections below walk each supported function
family with one snippet and, where it helps, an illustrative chart
backed by a synthetic fixture.

## Selectors

```promql
metric_name
metric_name{job="api"}
metric_name{status=~"5..", method!="OPTIONS"}
```

Label-matcher operators: `=`, `!=`, `=~` (regex match), `!~` (regex
non-match). Regex anchoring follows Prometheus's convention: the
pattern is implicitly anchored at both ends, so `5..` matches `500`,
`503`, `504` but not `1503`.

## rate

Per-second average over every sample pair inside the window. Smooth
output; best for graphs you read over time.

```promql
rate(http_requests_total[5m])
```

The shape `fn(expr[Nw])` is shared with `irate` and `increase`: `N`
is a positive integer; `w` is `s`, `m`, or `h`. Counter resets — a
sample strictly less than its predecessor — are treated as a fresh
start, so a process restart does not produce a negative spike.

{{> chart fixture=rate-typical expr="rate(http_requests_total[1m])" unit=ops title="rate"}}

## irate

Per-second rate computed from the **last two samples only**. Noisier
than `rate`, but does not smear sudden bursts across the whole
window. Best for alert expressions on volatile counters where you
want the instantaneous reading.

```promql
irate(http_requests_total[1m])
```

## increase

Total counter delta across the window. Mathematically equivalent to
`rate(expr[w]) * window-seconds`. Best for "how many X happened in
the last hour" tables and threshold rules.

```promql
increase(owl_alerts_webhook_failures_total[10m])
```

## delta

`last - first` across the window. Unlike `increase`, it does not
assume the input is a monotonic counter — decreases yield negative
values. Use it for gauges.

```promql
delta(node_filesystem_avail_bytes[1h])
```

## *_over_time

`avg_over_time`, `sum_over_time`, `min_over_time`, `max_over_time`,
`count_over_time` collapse every sample in the per-step window to a
single value per series. Useful for smoothing noisy gauges or
counting how many readings fell inside the window.

```promql
avg_over_time(node_load1[15m])
count_over_time(up[1h])
```

## histogram_quantile

Computes the `q` quantile (with `q` a literal in `[0, 1]`) from
Prometheus-style cumulative histogram bucket series. The input must
yield a vector where every series carries a `le` label whose value is
the bucket's upper bound (a finite number or `+Inf`).

```promql
histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m]))
```

Bucket series are grouped by every label except `le`; within each
group, the quantile is found by linear interpolation between adjacent
boundaries, using `0` as the implicit lower edge of the smallest
bucket and clamping the `+Inf` upper edge to the highest finite
boundary. Groups with zero total observations at a timestamp emit no
point at that timestamp.

{{> chart fixture=histogram-latency expr="histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m]))" unit=s title="p95 latency"}}

## Aggregations

```promql
sum(expr)            avg(expr)
min(expr)            max(expr)
count(expr)

sum   by (job)      (rate(http_requests_total[1m]))
avg   by (instance) (cpu_usage)
count by (status)   (http_requests_total)

sum without (instance, replica) (http_requests_total)
```

Operators: `sum`, `avg`, `min`, `max`, `count`. The `by (labels)`
form groups output series by the listed labels; the `without
(labels)` form drops the listed labels and groups by every label that
remains. Aggregations always emit one series per group.

## topk / bottomk

At each evaluation step, `topk(k, expr)` keeps only the `k` series
with the largest values; `bottomk(k, expr)` keeps the smallest. The
rest are dropped from the result. Labels are preserved (unlike
`sum`/`avg`, which group). Ties break deterministically by canonical
label string, so output is stable across runs.

```promql
topk(5, rate(container_cpu_usage_seconds_total[1m]))
bottomk(3, node_filesystem_avail_bytes)
```

## Binary operations

```promql
cpu_usage * 100
100 - cpu_idle_pct
node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes
errors_total / requests_total
sum by (status) (rate(http_requests_total[1m])) / sum (rate(http_requests_total[1m]))
```

Operators: `+`, `-`, `*`, `/`.

- **scalar OP expr** and **expr OP scalar** apply the op pointwise
  with the scalar.
- **expr OP expr** (series-on-series) matches LHS series to RHS
  series by **exact label set** (the metric name is dropped on
  output, per Prometheus convention). If one side has a single series
  and the other has many, the single series is broadcast across every
  other. Timestamps are inner-joined: only points that exist on both
  sides produce output.

Division by zero returns `0` (not `+Inf` / `NaN`), to keep the chart
layer clean of special-case rendering.

## What is not supported

The list is concrete. owl's parser rejects each item below with a
named reason and the affected panel is marked "unsupported"; the rest
of the dashboard keeps working.

- **Functions**: `idelta`, `deriv`, `predict_linear`, `holt_winters`,
  `abs`/`ceil`/`floor`/`round`/`sqrt`/`ln`/`log2`/`log10`/`exp`,
  `quantile`, `clamp`/`clamp_min`/`clamp_max`,
  `label_replace`/`label_join`, `time`/`vector`/`scalar`,
  `sort`/`sort_desc`, `absent`/`absent_over_time`, `changes`,
  `resets`.
- **Aggregation operators**: `stddev`, `stdvar`, `quantile`.
- **Vector-matching modifiers**: `on(labels)`, `ignoring(labels)`,
  `group_left`/`group_right` — matching is exact-label-set only.
- **Modifiers / syntax**: `offset`, the `@` modifier, subqueries
  (`expr[5m:30s]`), `__name__` regex matching, string literals,
  numeric literals as a top-level expression.
- **Logical / set ops**: `and`, `or`, `unless`.
- **Comparison ops**: `>`, `<`, `==`, `!=`, `>=`, `<=` are valid
  inside alert rule `op:` but not as PromQL operators.
- **Operator precedence**: left-to-right only, no PEMDAS. Parenthesise
  aggregations and `rate()` calls; chained arithmetic without parens
  binds left.

If a dashboard you care about uses one of these and it would be
straightforward to add, raise an issue with the exact expression.
Most of these are a short addition to the parser/evaluator once a
real need pins them down.

## Capability matrix

{{> promql-capabilities}}
