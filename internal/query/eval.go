package query

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"

	"github.com/neverbot/owl/internal/storage"
)

// evaluator walks an AST and produces []storage.Series.
type evaluator struct {
	q    Querier
	from int64 // ms
	to   int64 // ms
	step int64 // ms; 0 means "instant query, single point at to"
}

func newEvaluator(q Querier, from, to int64) *evaluator {
	return &evaluator{q: q, from: from, to: to}
}

func newRangeEvaluator(q Querier, from, to, step int64) *evaluator {
	return &evaluator{q: q, from: from, to: to, step: step}
}

// eval dispatches to the appropriate method based on node type.
func (e *evaluator) eval(node Node) ([]storage.Series, error) {
	switch n := node.(type) {
	case *SelectorNode:
		return e.evalSelector(n)
	case *RangeFuncNode:
		return e.evalRangeFunc(n)
	case *AggregationNode:
		return e.evalAggregation(n)
	case *BinaryOpNode:
		return e.evalBinaryOp(n)
	case *BinaryExprNode:
		return e.evalBinaryExpr(n)
	case *HistogramQuantileNode:
		return e.evalHistogramQuantile(n)
	default:
		return nil, fmt.Errorf("eval: unknown node type %T", node)
	}
}

// compiledMatcher is a LabelMatcher with an optional pre-compiled regex.
type compiledMatcher struct {
	LabelMatcher
	re *regexp.Regexp
}

// evalSelector fetches series from storage and filters by matchers.
func (e *evaluator) evalSelector(n *SelectorNode) ([]storage.Series, error) {
	all, err := e.q.Query(n.Metric, e.from, e.to)
	if err != nil {
		return nil, fmt.Errorf("query %q: %w", n.Metric, err)
	}
	if len(n.Matchers) == 0 {
		return all, nil
	}
	// Compile regex matchers once.
	cms := make([]compiledMatcher, len(n.Matchers))
	for i, m := range n.Matchers {
		cm := compiledMatcher{LabelMatcher: m}
		if m.Op == "=~" || m.Op == "!~" {
			re, err := regexp.Compile("^(?:" + m.Value + ")$")
			if err != nil {
				return nil, fmt.Errorf("invalid regex %q: %w", m.Value, err)
			}
			cm.re = re
		}
		cms[i] = cm
	}

	out := make([]storage.Series, 0)
	for _, s := range all {
		if matchesAll(s.Labels, cms) {
			out = append(out, s)
		}
	}
	return out, nil
}

func matchesAll(labels map[string]string, matchers []compiledMatcher) bool {
	for _, m := range matchers {
		v := labels[m.Name]
		switch m.Op {
		case "=":
			if v != m.Value {
				return false
			}
		case "!=":
			if v == m.Value {
				return false
			}
		case "=~":
			if !m.re.MatchString(v) {
				return false
			}
		case "!~":
			if m.re.MatchString(v) {
				return false
			}
		}
	}
	return true
}

// evalRangeFunc evaluates the rate / irate / increase trio. They
// share the same windowing machinery: pick a set of evaluation
// timestamps, slice the inner series' samples that fall in each
// window, then call a function-specific compute step.
//
// For a range query (step > 0) it emits one value per step across
// [from, to]; for an instant query (step == 0) it emits a single
// value at to.
//
// Counter resets (prev > curr) are detected and handled by the inner
// compute helpers: the delta for that interval is treated as just
// the current value.
func (e *evaluator) evalRangeFunc(n *RangeFuncNode) ([]storage.Series, error) {
	windowMs := n.Window.Milliseconds()
	// Extend from by the window so the first step has enough lookback.
	extFrom := e.from - windowMs
	if extFrom < 0 {
		extFrom = 0
	}

	innerEv := &evaluator{q: e.q, from: extFrom, to: e.to}
	inner, err := innerEv.eval(n.Expr)
	if err != nil {
		return nil, err
	}

	// Pick the evaluation timestamps: every `step` ms in [from, to], or
	// just one point at `to` for instant queries.
	var timestamps []int64
	if e.step <= 0 {
		timestamps = []int64{e.to}
	} else {
		for t := e.from; t <= e.to; t += e.step {
			timestamps = append(timestamps, t)
		}
		// Always include the right edge.
		if len(timestamps) == 0 || timestamps[len(timestamps)-1] != e.to {
			timestamps = append(timestamps, e.to)
		}
	}

	compute := pickRangeCompute(n.Func, windowMs)

	out := make([]storage.Series, 0, len(inner))
	for _, s := range inner {
		pts := s.Points
		if len(pts) < 2 {
			continue
		}
		emitted := make([]storage.Point, 0, len(timestamps))
		for _, t := range timestamps {
			window := samplesInWindow(pts, t-windowMs, t)
			if len(window) < 2 {
				continue
			}
			emitted = append(emitted, storage.Point{TS: t, Value: compute(window)})
		}
		if len(emitted) == 0 {
			continue
		}
		out = append(out, storage.Series{
			Metric: s.Metric,
			Labels: s.Labels,
			Points: emitted,
		})
	}
	return out, nil
}

// pickRangeCompute returns the per-window value function for one of
// the supported range functions. Unknown function names fall back to
// computeRate; the parser already rejects unknown functions, so
// reaching the default would be an internal bug.
func pickRangeCompute(fn string, windowMs int64) func([]storage.Point) float64 {
	switch fn {
	case "irate":
		return computeIRate
	case "increase":
		windowSeconds := float64(windowMs) / 1000.0
		return func(pts []storage.Point) float64 {
			return computeRate(pts) * windowSeconds
		}
	default: // "rate"
		return computeRate
	}
}

// computeIRate returns the per-second rate computed from just the
// last two samples in the window. It is preferable to rate for
// volatile counters because it does not smear sudden bursts across
// the entire window — but it is also noisier, which is why rate
// remains the default.
func computeIRate(pts []storage.Point) float64 {
	if len(pts) < 2 {
		return 0
	}
	a := pts[len(pts)-2]
	b := pts[len(pts)-1]
	spanMs := b.TS - a.TS
	if spanMs <= 0 {
		return 0
	}
	var delta float64
	if b.Value >= a.Value {
		delta = b.Value - a.Value
	} else {
		// Counter reset: treat curr as a fresh accumulation from zero.
		delta = b.Value
	}
	return delta / (float64(spanMs) / 1000.0)
}

// samplesInWindow returns the subset of pts whose ts falls in [lo, hi]
// (both ends inclusive, matching Prometheus's range-vector semantics).
// Assumes pts is sorted by ts ascending.
func samplesInWindow(pts []storage.Point, lo, hi int64) []storage.Point {
	left, right := 0, len(pts)
	for left < right {
		mid := (left + right) / 2
		if pts[mid].TS < lo {
			left = mid + 1
		} else {
			right = mid
		}
	}
	start := left
	end := start
	for end < len(pts) && pts[end].TS <= hi {
		end++
	}
	return pts[start:end]
}

// computeRate calculates the per-second rate across all points,
// handling counter resets.
func computeRate(pts []storage.Point) float64 {
	if len(pts) < 2 {
		return 0
	}
	var totalDelta float64
	prev := pts[0].Value
	for i := 1; i < len(pts); i++ {
		curr := pts[i].Value
		if curr >= prev {
			totalDelta += curr - prev
		} else {
			// Counter reset: treat curr as fresh accumulation from zero.
			totalDelta += curr
		}
		prev = curr
	}
	spanMs := pts[len(pts)-1].TS - pts[0].TS
	if spanMs <= 0 {
		return 0
	}
	return totalDelta / (float64(spanMs) / 1000.0)
}

// evalAggregation evaluates the inner expression then aggregates.
// A non-empty Without is resolved into the equivalent By list at eval
// time: every label seen on any input series, minus the ones the
// caller asked to drop. With no modifier the aggregation collapses
// every series into a single output series.
func (e *evaluator) evalAggregation(n *AggregationNode) ([]storage.Series, error) {
	inner, err := e.eval(n.Expr)
	if err != nil {
		return nil, err
	}
	if len(n.Without) > 0 {
		by := computeWithoutGroupKey(inner, n.Without)
		return aggregateBy(n.Op, by, inner)
	}
	if len(n.By) == 0 {
		return aggregateAll(n.Op, inner)
	}
	return aggregateBy(n.Op, n.By, inner)
}

// computeWithoutGroupKey returns the sorted set of label names that
// remain after removing `without` from the union of labels found on
// any series in `inner`. The result is suitable as the `by` list for
// aggregateBy and is stable across calls so output ordering is
// deterministic.
func computeWithoutGroupKey(inner []storage.Series, without []string) []string {
	drop := make(map[string]struct{}, len(without))
	for _, k := range without {
		drop[k] = struct{}{}
	}
	seen := make(map[string]struct{})
	for _, s := range inner {
		for k := range s.Labels {
			if _, skip := drop[k]; skip {
				continue
			}
			seen[k] = struct{}{}
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// aggregateAll collapses all series into one.
func aggregateAll(op string, series []storage.Series) ([]storage.Series, error) {
	// Collect all points grouped by TS across all series.
	tsBucket := make(map[int64][]float64)
	var tsOrder []int64
	for _, s := range series {
		for _, p := range s.Points {
			if _, ok := tsBucket[p.TS]; !ok {
				tsOrder = append(tsOrder, p.TS)
			}
			tsBucket[p.TS] = append(tsBucket[p.TS], p.Value)
		}
	}
	sort.Slice(tsOrder, func(i, j int) bool { return tsOrder[i] < tsOrder[j] })

	pts := make([]storage.Point, 0, len(tsOrder))
	for _, ts := range tsOrder {
		vals := tsBucket[ts]
		v, err := applyAggOp(op, vals)
		if err != nil {
			return nil, err
		}
		pts = append(pts, storage.Point{TS: ts, Value: v})
	}

	metric := ""
	if len(series) > 0 {
		metric = series[0].Metric
	}
	return []storage.Series{{Metric: metric, Labels: map[string]string{}, Points: pts}}, nil
}

// aggregateBy groups series by the specified label set then aggregates each group.
func aggregateBy(op string, by []string, series []storage.Series) ([]storage.Series, error) {
	type groupKey string
	groups := make(map[groupKey]*groupState)
	var order []groupKey

	for _, s := range series {
		key := groupKey(groupLabelKey(s.Labels, by))
		g, ok := groups[key]
		if !ok {
			// Retain only the "by" labels.
			lbls := make(map[string]string, len(by))
			for _, k := range by {
				lbls[k] = s.Labels[k]
			}
			g = &groupState{metric: s.Metric, labels: lbls, tsBucket: make(map[int64][]float64)}
			groups[key] = g
			order = append(order, key)
		}
		for _, p := range s.Points {
			g.tsBucket[p.TS] = append(g.tsBucket[p.TS], p.Value)
		}
	}

	out := make([]storage.Series, 0, len(order))
	for _, key := range order {
		g := groups[key]
		tsOrder := make([]int64, 0, len(g.tsBucket))
		for ts := range g.tsBucket {
			tsOrder = append(tsOrder, ts)
		}
		sort.Slice(tsOrder, func(i, j int) bool { return tsOrder[i] < tsOrder[j] })
		pts := make([]storage.Point, 0, len(tsOrder))
		for _, ts := range tsOrder {
			v, err := applyAggOp(op, g.tsBucket[ts])
			if err != nil {
				return nil, err
			}
			pts = append(pts, storage.Point{TS: ts, Value: v})
		}
		out = append(out, storage.Series{Metric: g.metric, Labels: g.labels, Points: pts})
	}
	return out, nil
}

type groupState struct {
	metric   string
	labels   map[string]string
	tsBucket map[int64][]float64
}

// groupLabelKey builds a stable string key for a subset of labels.
func groupLabelKey(labels map[string]string, by []string) string {
	sorted := append([]string{}, by...)
	sort.Strings(sorted)
	result := ""
	for i, k := range sorted {
		if i > 0 {
			result += ","
		}
		result += k + "=" + labels[k]
	}
	return result
}

func applyAggOp(op string, vals []float64) (float64, error) {
	if len(vals) == 0 {
		return 0, nil
	}
	switch op {
	case "sum":
		var s float64
		for _, v := range vals {
			s += v
		}
		return s, nil
	case "avg":
		var s float64
		for _, v := range vals {
			s += v
		}
		return s / float64(len(vals)), nil
	case "min":
		m := vals[0]
		for _, v := range vals[1:] {
			if v < m {
				m = v
			}
		}
		return m, nil
	case "max":
		m := vals[0]
		for _, v := range vals[1:] {
			if v > m {
				m = v
			}
		}
		return m, nil
	case "count":
		return float64(len(vals)), nil
	default:
		return 0, fmt.Errorf("unknown aggregation op %q", op)
	}
}

// evalBinaryOp applies scalar arithmetic to each point value.
func (e *evaluator) evalBinaryOp(n *BinaryOpNode) ([]storage.Series, error) {
	inner, err := e.eval(n.Expr)
	if err != nil {
		return nil, err
	}
	out := make([]storage.Series, len(inner))
	for i, s := range inner {
		pts := make([]storage.Point, len(s.Points))
		for j, p := range s.Points {
			pts[j] = storage.Point{TS: p.TS, Value: applyBinOp(n.Op, p.Value, n.Scalar, n.ScalarLeft)}
		}
		out[i] = storage.Series{Metric: s.Metric, Labels: s.Labels, Points: pts}
	}
	return out, nil
}

// evalBinaryExpr applies a binary op to two sub-expressions. Series
// from the LHS are matched to series from the RHS by exact label set
// (metric name dropped, per Prometheus convention). When one side has
// a single series, it is broadcast against every series on the other.
// Timestamps are aligned by exact-equal inner join; unaligned points
// are dropped.
func (e *evaluator) evalBinaryExpr(n *BinaryExprNode) ([]storage.Series, error) {
	lhs, err := e.eval(n.LHS)
	if err != nil {
		return nil, err
	}
	rhs, err := e.eval(n.RHS)
	if err != nil {
		return nil, err
	}
	if len(lhs) == 0 || len(rhs) == 0 {
		return nil, nil
	}

	// Index RHS by canonical label signature.
	rhsByLabels := make(map[string]*storage.Series, len(rhs))
	for i := range rhs {
		rhsByLabels[labelSig(rhs[i].Labels)] = &rhs[i]
	}

	out := make([]storage.Series, 0, len(lhs))
	for i := range lhs {
		l := &lhs[i]
		var r *storage.Series
		if match, ok := rhsByLabels[labelSig(l.Labels)]; ok {
			r = match
		} else if len(rhs) == 1 {
			// Broadcast: single-series RHS pairs with every LHS series.
			r = &rhs[0]
		} else {
			continue
		}

		// Inner-join points by timestamp.
		rByTs := make(map[int64]float64, len(r.Points))
		for _, p := range r.Points {
			rByTs[p.TS] = p.Value
		}
		pts := make([]storage.Point, 0, len(l.Points))
		for _, p := range l.Points {
			rv, ok := rByTs[p.TS]
			if !ok {
				continue
			}
			pts = append(pts, storage.Point{TS: p.TS, Value: applyBinExpr(n.Op, p.Value, rv)})
		}
		if len(pts) == 0 {
			continue
		}
		out = append(out, storage.Series{
			Metric: "", // Prometheus drops the metric name on binary ops.
			Labels: l.Labels,
			Points: pts,
		})
	}
	return out, nil
}

// applyBinExpr is the series-on-series counterpart of applyBinOp.
func applyBinExpr(op string, l, r float64) float64 {
	switch op {
	case "+":
		return l + r
	case "-":
		return l - r
	case "*":
		return l * r
	case "/":
		if r == 0 {
			return 0
		}
		return l / r
	}
	return l
}

// labelSig is the canonical "sorted k=v,k=v" signature used for
// matching series across the two sides of a binary expression.
func labelSig(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b []byte
	for i, k := range keys {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, k...)
		b = append(b, '=')
		b = append(b, labels[k]...)
	}
	return string(b)
}

// evalHistogramQuantile computes histogram_quantile(q, expr).
//
// The inner expression must return Prometheus-style cumulative bucket
// series, each carrying a `le` label. Series are grouped by every
// other label; within each group, buckets are sorted by their `le`
// value (with `+Inf` last). For every timestamp present in the group,
// the requested quantile is computed by linear interpolation between
// adjacent bucket boundaries, using 0 as the implicit lower edge of
// the smallest bucket and clamping the `+Inf` upper edge to the
// highest finite boundary. Groups whose total count is zero at a
// given timestamp emit no point at that timestamp (no NaN samples,
// since storage rejects them).
func (e *evaluator) evalHistogramQuantile(n *HistogramQuantileNode) ([]storage.Series, error) {
	inner, err := e.eval(n.Expr)
	if err != nil {
		return nil, err
	}

	// Group input series by their label set minus `le`. We also
	// preserve the first-seen insertion order for deterministic output.
	type group struct {
		labels  map[string]string
		buckets []histogramBucket
	}
	groups := make(map[string]*group)
	var order []string
	for i := range inner {
		s := &inner[i]
		leStr, ok := s.Labels["le"]
		if !ok {
			return nil, fmt.Errorf("histogram_quantile: input series missing `le` label")
		}
		le, err := parseLE(leStr)
		if err != nil {
			return nil, fmt.Errorf("histogram_quantile: invalid `le` label %q: %w", leStr, err)
		}
		key := groupKeyWithoutLE(s.Labels)
		g, exists := groups[key]
		if !exists {
			lbls := make(map[string]string, len(s.Labels))
			for k, v := range s.Labels {
				if k == "le" {
					continue
				}
				lbls[k] = v
			}
			g = &group{labels: lbls}
			groups[key] = g
			order = append(order, key)
		}
		g.buckets = append(g.buckets, histogramBucket{le: le, series: s})
	}

	out := make([]storage.Series, 0, len(order))
	for _, key := range order {
		g := groups[key]
		sort.Slice(g.buckets, func(i, j int) bool {
			return g.buckets[i].le < g.buckets[j].le
		})

		// Collect the union of timestamps across all buckets in the group.
		tsSet := make(map[int64]struct{})
		for _, b := range g.buckets {
			for _, p := range b.series.Points {
				tsSet[p.TS] = struct{}{}
			}
		}
		timestamps := make([]int64, 0, len(tsSet))
		for ts := range tsSet {
			timestamps = append(timestamps, ts)
		}
		sort.Slice(timestamps, func(i, j int) bool { return timestamps[i] < timestamps[j] })

		// Pre-index each bucket's points for O(1) timestamp lookup.
		perBucketByTS := make([]map[int64]float64, len(g.buckets))
		for i, b := range g.buckets {
			m := make(map[int64]float64, len(b.series.Points))
			for _, p := range b.series.Points {
				m[p.TS] = p.Value
			}
			perBucketByTS[i] = m
		}

		pts := make([]storage.Point, 0, len(timestamps))
		for _, ts := range timestamps {
			v, ok := computeQuantileAtTS(n.Quantile, g.buckets, perBucketByTS, ts)
			if !ok {
				continue
			}
			pts = append(pts, storage.Point{TS: ts, Value: v})
		}
		if len(pts) == 0 {
			continue
		}
		// Metric name is dropped, matching Prometheus convention for
		// derived series.
		out = append(out, storage.Series{Metric: "", Labels: g.labels, Points: pts})
	}
	return out, nil
}

// computeQuantileAtTS returns the interpolated quantile for the given
// timestamp, or (0, false) if the group has no observations at this
// timestamp (so no point should be emitted).
//
// buckets is sorted by le ascending. perBucketByTS[i] holds the
// already-indexed points for buckets[i]. A bucket missing a value at
// ts is treated as zero, which is consistent with cumulative-histogram
// semantics (no observations recorded yet for that bucket).
// histogramBucket pairs a parsed `le` boundary with the bucket series.
type histogramBucket struct {
	le     float64 // math.Inf(+1) for the "+Inf" catch-all bucket
	series *storage.Series
}

func computeQuantileAtTS(q float64, buckets []histogramBucket, perBucketByTS []map[int64]float64, ts int64) (float64, bool) {
	if len(buckets) == 0 {
		return 0, false
	}
	counts := make([]float64, len(buckets))
	for i := range buckets {
		counts[i] = perBucketByTS[i][ts]
	}

	// Total is the count in the last (highest le) bucket, which by
	// cumulative-histogram convention is `+Inf` if present, otherwise
	// the highest finite bucket.
	total := counts[len(counts)-1]
	if total <= 0 {
		return 0, false
	}

	// Quantile=1: by convention return the highest finite bucket
	// boundary (the +Inf upper edge is unbounded, so we clamp).
	if q >= 1 {
		return highestFiniteLE(buckets), true
	}
	target := q * total

	// Walk buckets in ascending le order.
	prevCount := 0.0
	prevLE := 0.0 // implicit lower edge for the smallest bucket
	for i, b := range buckets {
		c := counts[i]
		if c >= target {
			// Use 0 as the lower edge for the very first bucket.
			loLE := prevLE
			if i == 0 {
				loLE = 0
				// Negative bucket boundaries (rare in practice) would
				// require a different convention; clamp to 0.
				if b.le < 0 {
					loLE = b.le
				}
			}
			hiLE := b.le
			if math.IsInf(hiLE, +1) {
				// Clamp +Inf to the highest finite bucket boundary.
				hiLE = highestFiniteLE(buckets)
				// If no finite bucket exists, fall back to prevLE.
				if math.IsInf(hiLE, +1) {
					hiLE = loLE
				}
			}
			span := c - prevCount
			if span <= 0 {
				return hiLE, true
			}
			return loLE + (target-prevCount)/span*(hiLE-loLE), true
		}
		prevCount = c
		prevLE = b.le
	}
	// Should not be reachable because target <= total = counts[last].
	return highestFiniteLE(buckets), true
}

// highestFiniteLE returns the largest finite `le` boundary in
// buckets, or +Inf if every bucket is +Inf (degenerate input).
// buckets must be sorted by le ascending.
func highestFiniteLE(buckets []histogramBucket) float64 {
	for i := len(buckets) - 1; i >= 0; i-- {
		if !math.IsInf(buckets[i].le, +1) {
			return buckets[i].le
		}
	}
	return math.Inf(+1)
}

// parseLE parses a histogram bucket boundary string. Prometheus emits
// `+Inf` for the catch-all bucket; everything else is a decimal float.
func parseLE(s string) (float64, error) {
	if s == "+Inf" || s == "Inf" {
		return math.Inf(+1), nil
	}
	return strconv.ParseFloat(s, 64)
}

// groupKeyWithoutLE returns a stable key for the label set with `le`
// excluded. Used to group histogram bucket series.
func groupKeyWithoutLE(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		if k == "le" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b []byte
	for i, k := range keys {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, k...)
		b = append(b, '=')
		b = append(b, labels[k]...)
	}
	return string(b)
}

func applyBinOp(op string, seriesVal, scalar float64, scalarLeft bool) float64 {
	l, r := seriesVal, scalar
	if scalarLeft {
		l, r = scalar, seriesVal
	}
	switch op {
	case "+":
		return l + r
	case "-":
		return l - r
	case "*":
		return l * r
	case "/":
		if r == 0 {
			return 0
		}
		return l / r
	}
	return seriesVal
}
