// Package storage persists time-series samples in SQLite.
package storage

import (
	"sort"
	"strings"
)

// Sample is one observation written by a collector or scraper.
type Sample struct {
	Metric string
	Labels map[string]string
	TS     int64   // Unix milliseconds
	Value  float64
}

// Series is the result of a Query for a single (metric, labels) combination.
type Series struct {
	Metric string
	Labels map[string]string
	Points []Point
}

// Point is one (timestamp, value) within a Series.
type Point struct {
	TS    int64
	Value float64
}

// CanonicalLabels returns a stable string representation of a label set,
// used as the row key alongside the metric name. Labels are sorted by key
// and joined as k=v,k=v. Commas and equals signs in keys/values are not
// escaped at this stage (collectors must not emit them).
func CanonicalLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
	}
	return b.String()
}
