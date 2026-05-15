package query

import "github.com/neverbot/owl/internal/storage"

// Querier is the narrow read-side interface the engine depends on.
// *storage.Store satisfies this interface (see internal/storage/querier.go).
type Querier interface {
	Query(metric string, from, to int64) ([]storage.Series, error)
}

// Result is what the engine returns from a range query.
type Result struct {
	Series []storage.Series
}

// Capabilities describes what the engine supports.
// Dashboard loaders use this to flag panels with unsupported queries.
type Capabilities struct {
	Functions []string // e.g. ["rate"]
	Aggrs     []string // e.g. ["sum","avg","min","max","count"]
	Matchers  []string // e.g. ["=","!=","=~","!~"]
	Operators []string // e.g. ["+","-","*","/"]
}

// Engine parses and evaluates PromQL expressions against the storage layer.
type Engine struct {
	q Querier
}

// NewEngine wires the engine to a Querier (typically *storage.Store).
// q may be nil when only Capabilities or IsSupported are needed.
func NewEngine(q Querier) *Engine {
	return &Engine{q: q}
}

// defaultStep is used when the caller passes step == 0.
const defaultStep int64 = 15_000 // 15 seconds in ms

// QueryRange evaluates expr over [from, to] (millisecond epoch) and
// returns the resulting series. step is the sample resolution in ms;
// rate() slides its window at this step. If step == 0 it defaults to
// 15000 (15 s). Gauge-style metrics (bare selectors, scalar arithmetic
// over them) still return the raw stored points in the range.
func (e *Engine) QueryRange(expr string, from, to, step int64) (Result, error) {
	if step == 0 {
		step = defaultStep
	}
	node, err := Parse(expr)
	if err != nil {
		return Result{}, err
	}
	ev := newRangeEvaluator(e.q, from, to, step)
	series, err := ev.eval(node)
	if err != nil {
		return Result{}, err
	}
	return Result{Series: series}, nil
}

// Capabilities returns a static description of what the engine supports.
func (e *Engine) Capabilities() Capabilities {
	return Capabilities{
		Functions: []string{"rate", "irate", "increase"},
		Aggrs: []string{
			"sum", "avg", "min", "max", "count",
			"sum_by", "avg_by", "min_by", "max_by", "count_by",
			"sum_without", "avg_without", "min_without", "max_without", "count_without",
		},
		Matchers:  []string{"=", "!=", "=~", "!~"},
		Operators: []string{"+", "-", "*", "/"},
	}
}

// IsSupported reports whether expr can be evaluated by this engine.
// It does NOT run the query; it only parses and inspects.
// Returns (true, "") if supported, or (false, reason) if not.
func (e *Engine) IsSupported(expr string) (bool, string) {
	_, err := Parse(expr)
	if err != nil {
		return false, err.Error()
	}
	return true, ""
}
