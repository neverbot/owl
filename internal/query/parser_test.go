package query

import (
	"testing"
)

// tokeniseAll is a helper used only in tests.
func tokeniseAll(input string) []token {
	l := newLexer(input)
	var tokens []token
	for {
		tok := l.next()
		tokens = append(tokens, tok)
		if tok.kind == tokEOF || tok.kind == tokErr {
			break
		}
	}
	return tokens
}

func TestLexerBareMetric(t *testing.T) {
	toks := tokeniseAll("http_requests_total")
	if len(toks) != 2 {
		t.Fatalf("expected 2 tokens (ident + EOF), got %d: %v", len(toks), toks)
	}
	if toks[0].kind != tokIdent || toks[0].val != "http_requests_total" {
		t.Errorf("unexpected first token: %+v", toks[0])
	}
	if toks[1].kind != tokEOF {
		t.Errorf("expected EOF, got %+v", toks[1])
	}
}

func TestLexerLabelMatchers(t *testing.T) {
	toks := tokeniseAll(`http_requests_total{job="api",status!="500"}`)
	kinds := make([]tokenKind, len(toks))
	for i, tok := range toks {
		kinds[i] = tok.kind
	}
	// Expected sequence: ident, {, ident, =, str, ,, ident, !=, str, }, EOF
	want := []tokenKind{
		tokIdent, tokLBrace,
		tokIdent, tokEq, tokString, tokComma,
		tokIdent, tokNeq, tokString,
		tokRBrace, tokEOF,
	}
	if len(kinds) != len(want) {
		t.Fatalf("expected %d tokens, got %d: %v", len(want), len(kinds), toks)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("token[%d]: want kind %v, got %v (%+v)", i, want[i], kinds[i], toks[i])
		}
	}
}

func TestLexerRateExpr(t *testing.T) {
	toks := tokeniseAll("rate(http_requests_total[5m])")
	kinds := make([]tokenKind, len(toks))
	for i, tok := range toks {
		kinds[i] = tok.kind
	}
	// rate, (, ident, [, number, duration_unit, ], ), EOF
	want := []tokenKind{
		tokIdent, tokLParen, tokIdent,
		tokLBracket, tokNumber, tokDurationUnit,
		tokRBracket, tokRParen, tokEOF,
	}
	if len(kinds) != len(want) {
		t.Fatalf("expected %d tokens, got %d:\n  %v\n  %v", len(want), len(kinds), want, kinds)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("token[%d]: want %v, got %v (%+v)", i, want[i], kinds[i], toks[i])
		}
	}
}

func TestLexerNumber(t *testing.T) {
	toks := tokeniseAll("42.5")
	if len(toks) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(toks))
	}
	if toks[0].kind != tokNumber || toks[0].val != "42.5" {
		t.Errorf("unexpected token: %+v", toks[0])
	}
}

func TestParseBareSelector(t *testing.T) {
	node, err := Parse("http_requests_total")
	if err != nil {
		t.Fatal(err)
	}
	sel, ok := node.(*SelectorNode)
	if !ok {
		t.Fatalf("expected *SelectorNode, got %T", node)
	}
	if sel.Metric != "http_requests_total" {
		t.Errorf("want metric %q, got %q", "http_requests_total", sel.Metric)
	}
	if len(sel.Matchers) != 0 {
		t.Errorf("expected no matchers, got %v", sel.Matchers)
	}
}

func TestParseSelectorWithMatchers(t *testing.T) {
	node, err := Parse(`http_requests_total{job="api",status!="500"}`)
	if err != nil {
		t.Fatal(err)
	}
	sel, ok := node.(*SelectorNode)
	if !ok {
		t.Fatalf("expected *SelectorNode, got %T", node)
	}
	if sel.Metric != "http_requests_total" {
		t.Errorf("metric: want %q got %q", "http_requests_total", sel.Metric)
	}
	if len(sel.Matchers) != 2 {
		t.Fatalf("want 2 matchers, got %d", len(sel.Matchers))
	}
	if sel.Matchers[0] != (LabelMatcher{Name: "job", Op: "=", Value: "api"}) {
		t.Errorf("matcher[0]: %+v", sel.Matchers[0])
	}
	if sel.Matchers[1] != (LabelMatcher{Name: "status", Op: "!=", Value: "500"}) {
		t.Errorf("matcher[1]: %+v", sel.Matchers[1])
	}
}

func TestParseRate(t *testing.T) {
	node, err := Parse("rate(http_requests_total[5m])")
	if err != nil {
		t.Fatal(err)
	}
	r, ok := node.(*RateNode)
	if !ok {
		t.Fatalf("expected *RateNode, got %T", node)
	}
	if r.Window.Value != 5 || r.Window.Unit != "m" {
		t.Errorf("window: want 5m, got %+v", r.Window)
	}
	sel, ok := r.Expr.(*SelectorNode)
	if !ok {
		t.Fatalf("rate inner: expected *SelectorNode, got %T", r.Expr)
	}
	if sel.Metric != "http_requests_total" {
		t.Errorf("inner metric: %q", sel.Metric)
	}
}

func TestParseAggregation(t *testing.T) {
	node, err := Parse("sum(http_requests_total)")
	if err != nil {
		t.Fatal(err)
	}
	agg, ok := node.(*AggregationNode)
	if !ok {
		t.Fatalf("expected *AggregationNode, got %T", node)
	}
	if agg.Op != "sum" {
		t.Errorf("op: want sum, got %q", agg.Op)
	}
	if agg.By != nil {
		t.Errorf("by: want nil, got %v", agg.By)
	}
}

func TestParseAggregationWithBy(t *testing.T) {
	node, err := Parse("sum by (job) (rate(http_requests_total[1m]))")
	if err != nil {
		t.Fatal(err)
	}
	agg, ok := node.(*AggregationNode)
	if !ok {
		t.Fatalf("expected *AggregationNode, got %T", node)
	}
	if agg.Op != "sum" {
		t.Errorf("op: want sum, got %q", agg.Op)
	}
	if len(agg.By) != 1 || agg.By[0] != "job" {
		t.Errorf("by: want [job], got %v", agg.By)
	}
	inner, ok := agg.Expr.(*RateNode)
	if !ok {
		t.Fatalf("inner: expected *RateNode, got %T", agg.Expr)
	}
	if inner.Window.Value != 1 || inner.Window.Unit != "m" {
		t.Errorf("window: want 1m, got %+v", inner.Window)
	}
}

func TestParseBinaryOpScalarRight(t *testing.T) {
	node, err := Parse("http_requests_total * 2")
	if err != nil {
		t.Fatal(err)
	}
	b, ok := node.(*BinaryOpNode)
	if !ok {
		t.Fatalf("expected *BinaryOpNode, got %T", node)
	}
	if b.Op != "*" {
		t.Errorf("op: want *, got %q", b.Op)
	}
	if b.Scalar != 2 {
		t.Errorf("scalar: want 2, got %v", b.Scalar)
	}
	if b.ScalarLeft {
		t.Error("ScalarLeft should be false")
	}
}

func TestParseBinaryOpScalarLeft(t *testing.T) {
	node, err := Parse("2 * http_requests_total")
	if err != nil {
		t.Fatal(err)
	}
	b, ok := node.(*BinaryOpNode)
	if !ok {
		t.Fatalf("expected *BinaryOpNode, got %T", node)
	}
	if b.Op != "*" {
		t.Errorf("op: want *, got %q", b.Op)
	}
	if b.Scalar != 2 {
		t.Errorf("scalar: want 2, got %v", b.Scalar)
	}
	if !b.ScalarLeft {
		t.Error("ScalarLeft should be true")
	}
}

func TestParseUnsupported(t *testing.T) {
	cases := []string{
		"histogram_quantile(0.9)", // unknown function
		"",                        // empty input
	}
	for _, tc := range cases {
		_, err := Parse(tc)
		if err == nil {
			t.Errorf("Parse(%q): expected error, got nil", tc)
		}
	}
}

func TestParseSeriesOnSeries(t *testing.T) {
	node, err := Parse("metric1 - metric2")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	b, ok := node.(*BinaryExprNode)
	if !ok {
		t.Fatalf("got %T, want *BinaryExprNode", node)
	}
	if b.Op != "-" {
		t.Errorf("Op = %q, want -", b.Op)
	}
	if _, ok := b.LHS.(*SelectorNode); !ok {
		t.Errorf("LHS = %T, want *SelectorNode", b.LHS)
	}
	if _, ok := b.RHS.(*SelectorNode); !ok {
		t.Errorf("RHS = %T, want *SelectorNode", b.RHS)
	}
}

func TestParseRegexMatchers(t *testing.T) {
	node, err := Parse(`http_requests_total{status=~"5.."}`)
	if err != nil {
		t.Fatal(err)
	}
	sel, ok := node.(*SelectorNode)
	if !ok {
		t.Fatalf("expected *SelectorNode, got %T", node)
	}
	if len(sel.Matchers) != 1 {
		t.Fatalf("want 1 matcher, got %d", len(sel.Matchers))
	}
	if sel.Matchers[0].Op != "=~" {
		t.Errorf("op: want =~, got %q", sel.Matchers[0].Op)
	}
}
