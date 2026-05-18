package main

import (
	"encoding/json"
	"math"
	"math/rand"
	"sort"
)

// Fixture is the JSON shape chart.js expects from /api/query (and now
// also from a static file via data-static).
type Fixture struct {
	Series []FixtureSeries `json:"series"`
}

// FixtureSeries is one labelled time series. The wire shape matches
// the runtime /api/query JSON response exactly so chart.js consumes
// fixtures without branching.
type FixtureSeries struct {
	Metric string            `json:"metric"`
	Labels map[string]string `json:"labels"`
	Points []FixturePoint    `json:"points"`
}

// FixturePoint is one (timestamp-ms, value) pair. It marshals as a
// two-element JSON array `[ts, value]` to match the runtime API
// shape; the struct fields stay named for readability inside Go.
type FixturePoint struct {
	TS    int64
	Value float64
}

// MarshalJSON emits the point as [ts, value], the compact array
// representation chart.js expects.
func (p FixturePoint) MarshalJSON() ([]byte, error) {
	return json.Marshal([2]float64{float64(p.TS), p.Value})
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

// --- concrete fixtures ----------------------------------------------------
//
// All fixtures are deterministic: each one seeds its own rand.Rand so
// the same name always produces the same series across runs. The
// shapes aim to look like the kind of data a real owl deployment
// would surface — noisy random walks, occasional spikes, GC-style
// sawtooth on memory, monotonic counters with bursts. No pure sine
// waves: real metrics are never that smooth.

// baseTS is the fixed epoch ms used as the first timestamp in every
// fixture, ensuring reproducible output across runs.
const baseTS int64 = 1747526400000

// tsAt returns the timestamp (ms) for sample index i at stepSec
// resolution, anchored on baseTS.
func tsAt(i, stepSec int) int64 { return baseTS + int64(i)*int64(stepSec)*1000 }

// seriesGen is a small helper bag of pseudo-random series builders
// shared by the fixtures below. Each method honours the rng it was
// constructed with so a single seed produces a stable fixture.
type seriesGen struct {
	r *rand.Rand
}

func newGen(seed int64) *seriesGen { return &seriesGen{r: rand.New(rand.NewSource(seed))} }

// noise returns a uniform sample in [-amp, +amp].
func (g *seriesGen) noise(amp float64) float64 { return (g.r.Float64()*2 - 1) * amp }

// randomWalk builds n samples that drift around base with per-step
// gaussian-flavoured noise plus occasional spikes that decay over a
// few samples. base is the starting value; drift adds a slow trend;
// noiseAmp scales tick-to-tick jitter; spikeProb is the probability
// per sample that a spike starts; spikeAmp is the spike's magnitude
// (sign random); clampMin/clampMax bound the output (use
// math.Inf(-1)/math.Inf(1) to disable).
func (g *seriesGen) randomWalk(n int, base, drift, noiseAmp, spikeProb, spikeAmp, clampMin, clampMax float64) []float64 {
	out := make([]float64, n)
	v := base
	spike := 0.0
	spikeDecay := 0.0
	for i := 0; i < n; i++ {
		v += drift + g.noise(noiseAmp)
		if g.r.Float64() < spikeProb {
			sign := 1.0
			if g.r.Float64() < 0.5 {
				sign = -1.0
			}
			spike = sign * spikeAmp * (0.6 + 0.4*g.r.Float64())
			spikeDecay = spike / 3.0
		}
		v += spike
		if spike != 0 {
			spike -= spikeDecay
			if (spikeDecay > 0 && spike <= 0) || (spikeDecay < 0 && spike >= 0) {
				spike = 0
			}
		}
		if v < clampMin {
			v = clampMin
		} else if v > clampMax {
			v = clampMax
		}
		out[i] = v
	}
	return out
}

// sawtooth approximates a GC-style cycle: slow upward drift with
// periodic drops back toward base. Drop interval is randomised
// around `meanGap`. Useful for resident-memory style gauges.
func (g *seriesGen) sawtooth(n int, base, slope, dropAmp, meanGap, noiseAmp float64) []float64 {
	out := make([]float64, n)
	v := base
	nextDrop := int(meanGap + g.noise(meanGap*0.3))
	for i := 0; i < n; i++ {
		v += slope + g.noise(noiseAmp)
		if i == nextDrop {
			v -= dropAmp * (0.7 + 0.6*g.r.Float64())
			if v < base*0.6 {
				v = base * 0.6
			}
			nextDrop = i + int(meanGap+g.noise(meanGap*0.3))
		}
		out[i] = v
	}
	return out
}

// counter builds a strictly monotonic series with per-step increments
// drawn around mean ± jitter. Optional bursts multiply the increment
// for a few consecutive samples.
func (g *seriesGen) counter(n int, start, meanStep, jitter, burstProb, burstMul float64) []float64 {
	out := make([]float64, n)
	v := start
	burst := 0
	for i := 0; i < n; i++ {
		step := meanStep + g.noise(jitter)
		if step < 0 {
			step = 0
		}
		if burst == 0 && g.r.Float64() < burstProb {
			burst = 3 + g.r.Intn(4)
		}
		if burst > 0 {
			step *= burstMul
			burst--
		}
		v += step
		out[i] = v
	}
	return out
}

// pointsFor wraps a []float64 of values into timestamped points at
// stepSec resolution starting from baseTS.
func pointsFor(values []float64, stepSec int) []FixturePoint {
	out := make([]FixturePoint, len(values))
	for i, v := range values {
		out[i] = FixturePoint{TS: tsAt(i, stepSec), Value: v}
	}
	return out
}

// --- concrete fixtures ---------------------------------------------------

// rateTypical synthesises three jobs' request-rate series with
// noisy random walks and occasional bursts — what an HTTP service
// looks like under variable load.
func rateTypical() Fixture {
	const n = 60
	out := []FixtureSeries{}
	bases := map[string]float64{"api": 18, "worker": 9, "scheduler": 4}
	for s, name := range []string{"api", "worker", "scheduler"} {
		g := newGen(int64(101 + s))
		vals := g.randomWalk(n, bases[name], 0, bases[name]*0.18, 0.04, bases[name]*0.7, 0, math.Inf(1))
		out = append(out, FixtureSeries{
			Metric: "http_requests_per_second",
			Labels: map[string]string{"job": name},
			Points: pointsFor(vals, 60),
		})
	}
	return Fixture{Series: out}
}

// gaugeMemory synthesises a single resident-memory series with the
// characteristic GC sawtooth: slow growth between collections and
// sudden drops back toward baseline.
func gaugeMemory() Fixture {
	const n = 90
	g := newGen(202)
	vals := g.sawtooth(n,
		/*base*/ 1.05e8,
		/*slope*/ 4.0e5,
		/*dropAmp*/ 3.0e7,
		/*meanGap*/ 12,
		/*noiseAmp*/ 8.0e5)
	return Fixture{Series: []FixtureSeries{{
		Metric: "process_resident_memory_bytes",
		Labels: map[string]string{"instance": "app-a"},
		Points: pointsFor(vals, 30),
	}}}
}

// histogramLatency synthesises six cumulative bucket counter series
// (le=0.05 … le=+Inf). Each bucket grows monotonically; the +Inf
// bucket carries the full count, finer buckets carry monotonically
// fewer observations — the standard Prometheus histogram shape.
func histogramLatency() Fixture {
	const n = 60
	out := []FixtureSeries{}
	// bucket fractions of total observations (cumulative). Real
	// services tend to have a long tail past 100ms.
	fractions := map[string]float64{
		"0.05": 0.42,
		"0.1":  0.62,
		"0.25": 0.83,
		"0.5":  0.93,
		"1":    0.98,
		"+Inf": 1.00,
	}
	for s, le := range []string{"0.05", "0.1", "0.25", "0.5", "1", "+Inf"} {
		g := newGen(int64(303 + s))
		total := g.counter(n, 1200, 22, 6, 0.07, 3.0)
		// scale by fraction to get bucket count
		vals := make([]float64, n)
		f := fractions[le]
		for i, v := range total {
			vals[i] = math.Floor(v * f)
		}
		out = append(out, FixtureSeries{
			Metric: "http_request_duration_seconds_bucket",
			Labels: map[string]string{"le": le},
			Points: pointsFor(vals, 60),
		})
	}
	return Fixture{Series: out}
}

// heroMultiSeries synthesises five distinct random walks suited to a
// wide multi-series hero chart. Each surface has its own baseline,
// drift and noise scale so the curves stay distinguishable while
// looking like real telemetry rather than parametric curves.
func heroMultiSeries() Fixture {
	const n = 120
	out := []FixtureSeries{}
	type cfg struct {
		base, drift, noise, spikeProb, spike float64
	}
	confs := map[string]cfg{
		"web":   {base: 48, drift: 0.05, noise: 3.5, spikeProb: 0.05, spike: 14},
		"db":    {base: 36, drift: -0.02, noise: 2.5, spikeProb: 0.03, spike: 9},
		"cache": {base: 26, drift: 0.08, noise: 1.8, spikeProb: 0.02, spike: 6},
		"queue": {base: 60, drift: -0.06, noise: 4.5, spikeProb: 0.08, spike: 18},
		"edge":  {base: 42, drift: 0.04, noise: 3.0, spikeProb: 0.04, spike: 12},
	}
	for s, name := range []string{"web", "db", "cache", "queue", "edge"} {
		c := confs[name]
		g := newGen(int64(404 + s))
		vals := g.randomWalk(n, c.base, c.drift, c.noise, c.spikeProb, c.spike, 0, math.Inf(1))
		out = append(out, FixtureSeries{
			Metric: "demo_signal",
			Labels: map[string]string{"surface": name},
			Points: pointsFor(vals, 60),
		})
	}
	return Fixture{Series: out}
}

// hostCPU synthesises four cpu-mode time series (user, system,
// iowait, idle) for a single core. user/system spike up under load
// and idle drops to mirror; iowait stays low and bursty.
func hostCPU() Fixture {
	const n = 80
	out := []FixtureSeries{}
	confs := map[string]struct {
		base, drift, noise, spikeProb, spike float64
	}{
		"user":   {base: 22, drift: 0, noise: 4.5, spikeProb: 0.07, spike: 25},
		"system": {base: 9, drift: 0, noise: 2.0, spikeProb: 0.05, spike: 9},
		"iowait": {base: 3, drift: 0, noise: 1.0, spikeProb: 0.04, spike: 7},
		"idle":   {base: 66, drift: 0, noise: 4.5, spikeProb: 0.05, spike: -22},
	}
	for s, mode := range []string{"user", "system", "iowait", "idle"} {
		c := confs[mode]
		g := newGen(int64(505 + s))
		clampMax := 100.0
		if mode == "idle" {
			clampMax = 100.0
		}
		vals := g.randomWalk(n, c.base, c.drift, c.noise, c.spikeProb, c.spike, 0, clampMax)
		out = append(out, FixtureSeries{
			Metric: "node_cpu_seconds_total",
			Labels: map[string]string{"mode": mode, "cpu": "0"},
			Points: pointsFor(vals, 30),
		})
	}
	return Fixture{Series: out}
}

// containerMemory synthesises four containers' anonymous-memory
// series. Each container drifts upward (long-lived process), with
// small noise and occasional step drops (restarts, eviction, GC).
func containerMemory() Fixture {
	const n = 90
	out := []FixtureSeries{}
	bases := map[string]float64{
		"app-a":  60 * 1024 * 1024,
		"app-b":  82 * 1024 * 1024,
		"app-c":  48 * 1024 * 1024,
		"edge-a": 110 * 1024 * 1024,
	}
	for s, name := range []string{"app-a", "app-b", "app-c", "edge-a"} {
		g := newGen(int64(606 + s))
		base := bases[name]
		vals := g.sawtooth(n,
			/*base*/ base,
			/*slope*/ base*0.0015,
			/*dropAmp*/ base*0.18,
			/*meanGap*/ 28,
			/*noiseAmp*/ base*0.006)
		out = append(out, FixtureSeries{
			Metric: "container_memory_anon_bytes",
			Labels: map[string]string{"name": name},
			Points: pointsFor(vals, 60),
		})
	}
	return Fixture{Series: out}
}
