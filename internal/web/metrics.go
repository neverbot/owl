package web

import (
	"fmt"
	"math"
	"net/http"
	"runtime"
	rtmetrics "runtime/metrics"
)

// MetricDescriptor describes one metric exported on /metrics.
// The docs site reads the registry to render its metrics reference
// table; the runtime handler iterates the registry to emit values.
type MetricDescriptor struct {
	// Name is the Prometheus exposition name (e.g. "owl_goroutines").
	Name string
	// Type is the Prometheus metric type word ("gauge", "counter").
	Type string
	// Help is the one-line human description emitted as "# HELP".
	Help string
	// Family groups metrics in the docs table: "process", "storage",
	// "alerts", "dashboards". Empty falls back to "misc".
	Family string
}

// Registry returns every metric owl publishes on /metrics, in the
// order the handler emits them. Both the runtime handler and the
// docs generator iterate this list; adding a new metric in one place
// is enough.
func Registry() []MetricDescriptor {
	return []MetricDescriptor{
		{Name: "owl_goroutines", Type: "gauge", Family: "process",
			Help: "Number of goroutines that currently exist."},
		{Name: "owl_heap_objects_bytes", Type: "gauge", Family: "process",
			Help: "Bytes of currently live heap objects."},
		{Name: "owl_gc_pause_seconds_total", Type: "counter", Family: "process",
			Help: "Cumulative time spent in GC stop-the-world pauses since process start."},
		{Name: "owl_storage_samples_total", Type: "gauge", Family: "storage",
			Help: "Total number of samples currently in storage."},
		{Name: "owl_storage_size_bytes", Type: "gauge", Family: "storage",
			Help: "On-disk size of the SQLite database in bytes."},
		{Name: "owl_dashboards_loaded", Type: "gauge", Family: "dashboards",
			Help: "Number of dashboards currently indexed."},
		{Name: "owl_alerts_evaluations_total", Type: "counter", Family: "alerts",
			Help: "Total alert-rule evaluation cycles since process start."},
		{Name: "owl_alerts_webhook_sends_total", Type: "counter", Family: "alerts",
			Help: "Total webhook delivery attempts since process start."},
		{Name: "owl_alerts_webhook_failures_total", Type: "counter", Family: "alerts",
			Help: "Total webhook deliveries that returned an error since process start."},
		{Name: "owl_alerts_firing", Type: "gauge", Family: "alerts",
			Help: "Number of (rule, series) lineages currently in the firing state."},
	}
}

// metrics serves owl's own `/metrics` endpoint in Prometheus text
// exposition format so external Prometheus servers — or owl itself —
// can scrape it. The handler walks Registry() and looks each name up
// in a dispatch table built from the live subsystem snapshots; a
// (name, false) result means the metric is unavailable in this
// configuration (e.g. no Store wired in) and is silently omitted.
func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	values := s.metricValues()
	for _, d := range Registry() {
		v, ok := values[d.Name]
		if !ok {
			continue
		}
		emit(w, d.Name, d.Help, d.Type, v)
	}
}

// metricValues collects the live numeric value of every metric the
// server can currently publish. Names absent from the returned map
// are skipped by the /metrics handler — that's how the endpoint
// stays well-behaved when an optional subsystem (Store, Alerter)
// is not wired in.
func (s *Server) metricValues() map[string]float64 {
	out := make(map[string]float64, len(Registry()))

	// Process-level.
	out["owl_goroutines"] = float64(runtime.NumGoroutine())

	samples := []rtmetrics.Sample{
		{Name: "/memory/classes/heap/objects:bytes"},
		{Name: "/gc/pauses:seconds"},
	}
	rtmetrics.Read(samples)
	if samples[0].Value.Kind() == rtmetrics.KindUint64 {
		out["owl_heap_objects_bytes"] = float64(samples[0].Value.Uint64())
	}
	if samples[1].Value.Kind() == rtmetrics.KindFloat64Histogram {
		out["owl_gc_pause_seconds_total"] = gcPauseTotalSeconds(samples[1].Value.Float64Histogram())
	}

	// Storage stats.
	if s.opt.Store != nil {
		if stats, err := s.opt.Store.Stats(); err == nil {
			out["owl_storage_samples_total"] = float64(stats.SampleCount)
			out["owl_storage_size_bytes"] = float64(stats.SizeBytes)
		}
	}

	// Dashboards loaded.
	if s.opt.Loader != nil {
		out["owl_dashboards_loaded"] = float64(len(s.opt.Loader.List()))
	}

	// Alerter snapshot.
	if s.opt.Alerter != nil {
		st := s.opt.Alerter.Snapshot()
		out["owl_alerts_evaluations_total"] = float64(st.EvaluationsTotal)
		out["owl_alerts_webhook_sends_total"] = float64(st.WebhookSendsTotal)
		out["owl_alerts_webhook_failures_total"] = float64(st.WebhookFailuresTotal)
		out["owl_alerts_firing"] = float64(st.Firing)
	}

	return out
}

// emit writes one HELP / TYPE / sample triplet to w. Owl's exposition
// format is deliberately minimal — no labels, single value — because
// these metrics describe the owl process as a whole.
func emit(w http.ResponseWriter, name, help, kind string, v float64) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s %s\n", name, kind)
	fmt.Fprintf(w, "%s %g\n", name, v)
}

// gcPauseTotalSeconds collapses the runtime/metrics GC-pause histogram
// into a single cumulative-seconds value by summing each bucket's
// midpoint times its count. Buckets with an infinite boundary are
// skipped — they would contribute NaN to the sum and SQLite STRICT
// rejects NaN values when the metric is later persisted via scrape.
func gcPauseTotalSeconds(h *rtmetrics.Float64Histogram) float64 {
	var total float64
	for i, count := range h.Counts {
		if i+1 >= len(h.Buckets) {
			break
		}
		lo, hi := h.Buckets[i], h.Buckets[i+1]
		if math.IsInf(lo, 0) || math.IsInf(hi, 0) {
			continue
		}
		total += float64(count) * (lo + hi) / 2
	}
	if math.IsNaN(total) || math.IsInf(total, 0) {
		return 0
	}
	return total
}
