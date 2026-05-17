package main

import (
	"encoding/json"
	"math"
	"sort"
)

// Fixture is the JSON shape chart.js expects from /api/query (and now
// also from a static file via data-static).
type Fixture struct {
	Series []FixtureSeries `json:"series"`
}

// FixtureSeries is one labelled time series.
type FixtureSeries struct {
	Metric string            `json:"metric"`
	Labels map[string]string `json:"labels"`
	Points []FixturePoint    `json:"points"`
}

// FixturePoint is one (timestamp-ms, value) pair.
type FixturePoint struct {
	TS    int64   `json:"ts"`
	Value float64 `json:"value"`
}

// fixtures is the registry every {{> chart}} invocation looks up.
// Names are referenced from .md content; an unknown name fails
// `make docs-check` (Task 16).
var fixtures = map[string]func() Fixture{
	"rate-typical":      rateTypical,
	"gauge-memory":      gaugeMemory,
	"histogram-latency": histogramLatency,
	"hero-multi-series": heroMultiSeries,
	"host-cpu":          hostCPU,
	"container-memory":  containerMemory,
}

// FixtureNames returns every registered fixture name, sorted.
// Used by --check to verify referenced names exist.
func FixtureNames() []string {
	out := make([]string, 0, len(fixtures))
	for k := range fixtures {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// LookupFixture returns the named fixture, or false if missing.
func LookupFixture(name string) (Fixture, bool) {
	fn, ok := fixtures[name]
	if !ok {
		return Fixture{}, false
	}
	return fn(), true
}

// MarshalFixture serialises a fixture to JSON.
func MarshalFixture(f Fixture) ([]byte, error) { return json.Marshal(f) }

// --- concrete fixtures (deterministic, designed to look plausible) ---

// baseTS is the fixed epoch ms used as the first timestamp in every
// fixture, ensuring reproducible output across runs.
const baseTS int64 = 1747526400000

// tsAt returns the timestamp (ms) for sample index i at stepSec
// resolution, anchored on baseTS.
func tsAt(i, stepSec int) int64 { return baseTS + int64(i)*int64(stepSec)*1000 }

// rateTypical synthesises three jobs' request-rate series with
// staggered sinusoidal bursts.
func rateTypical() Fixture {
	const n = 60
	series := []FixtureSeries{}
	for s, name := range []string{"api", "worker", "scheduler"} {
		pts := make([]FixturePoint, n)
		for i := 0; i < n; i++ {
			x := float64(i) / float64(n)
			base := 12 + 3*float64(s)
			burst := math.Max(0, 8*math.Sin(2*math.Pi*x*1.5+float64(s)))
			pts[i] = FixturePoint{TS: tsAt(i, 60), Value: base + burst}
		}
		series = append(series, FixtureSeries{
			Metric: "http_requests_per_second",
			Labels: map[string]string{"job": name},
			Points: pts,
		})
	}
	return Fixture{Series: series}
}

// gaugeMemory synthesises a single-instance resident memory gauge
// with two overlaid sinusoids.
func gaugeMemory() Fixture {
	const n = 60
	pts := make([]FixturePoint, n)
	for i := 0; i < n; i++ {
		x := float64(i)
		pts[i] = FixturePoint{TS: tsAt(i, 60),
			Value: 1.2e8 + 1.5e7*math.Sin(x/8) + 3e6*math.Sin(x/2.3)}
	}
	return Fixture{Series: []FixtureSeries{{
		Metric: "process_resident_memory_bytes",
		Labels: map[string]string{"instance": "app-a"},
		Points: pts,
	}}}
}

// histogramLatency synthesises a classic six-bucket request-duration
// histogram with monotonically growing counts.
func histogramLatency() Fixture {
	const n = 30
	out := []FixtureSeries{}
	for _, le := range []string{"0.05", "0.1", "0.25", "0.5", "1", "+Inf"} {
		pts := make([]FixturePoint, n)
		for i := 0; i < n; i++ {
			pts[i] = FixturePoint{TS: tsAt(i, 60), Value: float64(i)*bucketWeight(le) + 10}
		}
		out = append(out, FixtureSeries{
			Metric: "http_request_duration_seconds_bucket",
			Labels: map[string]string{"le": le},
			Points: pts,
		})
	}
	return Fixture{Series: out}
}

// bucketWeight returns the per-sample increment for the bucket with
// upper bound le, shaping a plausible latency distribution.
func bucketWeight(le string) float64 {
	switch le {
	case "0.05":
		return 6
	case "0.1":
		return 12
	case "0.25":
		return 18
	case "0.5":
		return 22
	case "1":
		return 24
	default:
		return 25
	}
}

// heroMultiSeries synthesises five distinct sinusoidal series with
// phase offsets, suited to a wide hero chart.
func heroMultiSeries() Fixture {
	const n = 90
	series := []FixtureSeries{}
	for s, name := range []string{"web", "db", "cache", "queue", "edge"} {
		pts := make([]FixturePoint, n)
		for i := 0; i < n; i++ {
			x := float64(i) / float64(n)
			pts[i] = FixturePoint{TS: tsAt(i, 60),
				Value: 30 + 18*math.Sin(2*math.Pi*x+float64(s)*0.7) + float64(s)*2}
		}
		series = append(series, FixtureSeries{
			Metric: "demo_signal",
			Labels: map[string]string{"surface": name},
			Points: pts,
		})
	}
	return Fixture{Series: series}
}

// hostCPU synthesises four CPU mode time series (user/system/iowait/idle)
// for a single core.
func hostCPU() Fixture {
	const n = 60
	out := []FixtureSeries{}
	for _, mode := range []string{"user", "system", "iowait", "idle"} {
		pts := make([]FixturePoint, n)
		var base float64
		switch mode {
		case "user":
			base = 22
		case "system":
			base = 9
		case "iowait":
			base = 3
		case "idle":
			base = 66
		}
		for i := 0; i < n; i++ {
			pts[i] = FixturePoint{TS: tsAt(i, 60),
				Value: base + 5*math.Sin(float64(i)/6+base/10)}
		}
		out = append(out, FixtureSeries{
			Metric: "node_cpu_seconds_total",
			Labels: map[string]string{"mode": mode, "cpu": "0"},
			Points: pts,
		})
	}
	return Fixture{Series: out}
}

// containerMemory synthesises four containers' anonymous-memory
// series with mild oscillation.
func containerMemory() Fixture {
	const n = 60
	out := []FixtureSeries{}
	for s, name := range []string{"app-a", "app-b", "app-c", "edge-a"} {
		pts := make([]FixturePoint, n)
		for i := 0; i < n; i++ {
			pts[i] = FixturePoint{TS: tsAt(i, 60),
				Value: (50 + float64(s)*20) * 1024 * 1024 *
					(1 + 0.05*math.Sin(float64(i)/4+float64(s)))}
		}
		out = append(out, FixtureSeries{
			Metric: "container_memory_anon_bytes",
			Labels: map[string]string{"name": name},
			Points: pts,
		})
	}
	return Fixture{Series: out}
}
