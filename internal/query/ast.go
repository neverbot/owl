// Package query implements a PromQL subset parser and evaluator.
package query

// Node is implemented by every AST node type.
type Node interface {
	nodeMarker()
}

// LabelMatcher holds a single label filter.
// Op is one of: "=", "!=", "=~", "!~".
type LabelMatcher struct {
	Name  string
	Op    string
	Value string
}

// SelectorNode selects a metric with optional label matchers.
// Example: http_requests_total{job="api",status!="500"}
type SelectorNode struct {
	Metric   string
	Matchers []LabelMatcher
}

func (*SelectorNode) nodeMarker() {}

// Duration holds a parsed [Xm] window for rate/increase.
type Duration struct {
	Value int64  // numeric part
	Unit  string // "s", "m", "h"
}

// Milliseconds converts the duration to milliseconds.
func (d Duration) Milliseconds() int64 {
	switch d.Unit {
	case "h":
		return d.Value * 3_600_000
	case "m":
		return d.Value * 60_000
	default: // "s"
		return d.Value * 1_000
	}
}

// RangeFuncNode represents a function applied to a range-vector
// selector with a window: `func(expr[Nd])`. Func is one of "rate",
// "irate", or "increase".
//
//   - rate: per-second average across every sample pair in the window.
//   - irate: per-second rate using only the last two samples in the
//     window — better for spiky counters than rate's smoothing.
//   - increase: total counter delta across the window
//     (mathematically equivalent to rate * window-seconds).
//
// All three share the same parser, the same per-series sliding
// window, and the same counter-reset handling; they differ only in
// the value emitted at each step.
type RangeFuncNode struct {
	Func   string
	Expr   Node
	Window Duration
}

func (*RangeFuncNode) nodeMarker() {}

// AggregationNode represents sum/avg/min/max/count, optionally with by(labels).
type AggregationNode struct {
	Op   string   // "sum", "avg", "min", "max", "count"
	By   []string // nil means no grouping
	Expr Node
}

func (*AggregationNode) nodeMarker() {}

// BinaryOpNode applies a scalar arithmetic op to each series value.
// If ScalarLeft is true, the scalar is on the left (scalar OP expr).
type BinaryOpNode struct {
	Op         string // "+", "-", "*", "/"
	Expr       Node
	Scalar     float64
	ScalarLeft bool
}

func (*BinaryOpNode) nodeMarker() {}

// BinaryExprNode applies a binary op to two sub-expressions
// (series-on-series). Used when neither side is a numeric literal —
// for example `node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes`.
//
// Matching is by exact label set (the metric name is dropped from the
// output). When one side has a single series, it is broadcast against
// every series of the other side.
type BinaryExprNode struct {
	Op  string
	LHS Node
	RHS Node
}

func (*BinaryExprNode) nodeMarker() {}
