package web

import (
	"fmt"
	"net/http"
	"runtime"
	rtmetrics "runtime/metrics"
)

// metrics serves owl's own `/metrics` endpoint in Prometheus text
// exposition format so external Prometheus servers — or owl itself —
// can scrape it. The payload is intentionally small: process-level
// vitals (goroutines, allocated bytes), storage size, dashboard
// count. Counters tied to scrape failures or query latencies live in
// the relevant packages and would be exposed here as they get
// instrumented.
func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	// Goroutines.
	emit(w, "owl_goroutines",
		"Number of goroutines that currently exist.",
		"gauge",
		float64(runtime.NumGoroutine()))

	// Allocated heap bytes via runtime/metrics.
	samples := []rtmetrics.Sample{
		{Name: "/memory/classes/heap/objects:bytes"},
	}
	rtmetrics.Read(samples)
	if samples[0].Value.Kind() == rtmetrics.KindUint64 {
		emit(w, "owl_heap_objects_bytes",
			"Bytes of currently live heap objects.",
			"gauge",
			float64(samples[0].Value.Uint64()))
	}

	// Storage stats.
	if s.opt.Store != nil {
		if stats, err := s.opt.Store.Stats(); err == nil {
			emit(w, "owl_storage_samples_total",
				"Total number of samples currently in storage.",
				"gauge",
				float64(stats.SampleCount))
			emit(w, "owl_storage_size_bytes",
				"On-disk size of the SQLite database in bytes.",
				"gauge",
				float64(stats.SizeBytes))
		}
	}

	// Dashboards loaded.
	if s.opt.Loader != nil {
		emit(w, "owl_dashboards_loaded",
			"Number of dashboards currently indexed.",
			"gauge",
			float64(len(s.opt.Loader.List())))
	}

	// Alerter snapshot. Counts cycles, webhook deliveries, failures
	// and the live count of firing lineages. Lets the operator wire
	// an alert on the alerter itself — "fire if no successful
	// webhook in 5 minutes".
	if s.opt.Alerter != nil {
		st := s.opt.Alerter.Snapshot()
		emit(w, "owl_alerts_evaluations_total",
			"Total alert-rule evaluation cycles since process start.",
			"counter",
			float64(st.EvaluationsTotal))
		emit(w, "owl_alerts_webhook_sends_total",
			"Total webhook delivery attempts since process start.",
			"counter",
			float64(st.WebhookSendsTotal))
		emit(w, "owl_alerts_webhook_failures_total",
			"Total webhook deliveries that returned an error since process start.",
			"counter",
			float64(st.WebhookFailuresTotal))
		emit(w, "owl_alerts_firing",
			"Number of (rule, series) lineages currently in the firing state.",
			"gauge",
			float64(st.Firing))
	}
}

// emit writes one HELP / TYPE / sample triplet to w. Owl's exposition
// format is deliberately minimal — no labels, single value — because
// these metrics describe the owl process as a whole.
func emit(w http.ResponseWriter, name, help, kind string, v float64) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s %s\n", name, kind)
	fmt.Fprintf(w, "%s %g\n", name, v)
}
