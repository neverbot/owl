package query

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// tokenKind identifies the category of a lexed token.
type tokenKind int

const (
	tokEOF          tokenKind = iota
	tokErr                    // lexer error
	tokIdent                  // metric name, label name, function name, keyword
	tokString                 // quoted label value (e.g. "api")
	tokNumber                 // integer or float literal
	tokDurationUnit           // s, m, h immediately after a number inside [...]
	tokLBrace                 // {
	tokRBrace                 // }
	tokLParen                 // (
	tokRParen                 // )
	tokLBracket               // [
	tokRBracket               // ]
	tokComma                  // ,
	tokEq                     // =  (but NOT ==)
	tokNeq                    // !=
	tokReEq                   // =~
	tokReNeq                  // !~
	tokPlus                   // +
	tokMinus                  // -
	tokStar                   // *
	tokSlash                  // /
)

type token struct {
	kind tokenKind
	val  string
}

func (t token) String() string {
	return fmt.Sprintf("{kind:%d val:%q}", t.kind, t.val)
}

type lexer struct {
	input string
	pos   int
	// insideBracket tracks whether we are lexing inside a [...] duration window.
	// When true, a bare letter immediately after a number is a tokDurationUnit.
	insideBracket bool
}

func newLexer(input string) *lexer {
	return &lexer{input: input}
}

func (l *lexer) peek() byte {
	if l.pos >= len(l.input) {
		return 0
	}
	return l.input[l.pos]
}

func (l *lexer) advance() byte {
	b := l.input[l.pos]
	l.pos++
	return b
}

func (l *lexer) next() token {
	// skip whitespace
	for l.pos < len(l.input) && (l.input[l.pos] == ' ' || l.input[l.pos] == '\t') {
		l.pos++
	}
	if l.pos >= len(l.input) {
		return token{kind: tokEOF}
	}

	ch := l.peek()

	// Quoted string
	if ch == '"' {
		return l.lexString()
	}

	// Number (possibly followed by duration unit inside bracket)
	if ch >= '0' && ch <= '9' || ch == '.' {
		return l.lexNumber()
	}

	// Identifier or keyword
	if ch == '_' || unicode.IsLetter(rune(ch)) {
		return l.lexIdent()
	}

	l.pos++
	switch ch {
	case '{':
		return token{kind: tokLBrace, val: "{"}
	case '}':
		return token{kind: tokRBrace, val: "}"}
	case '(':
		return token{kind: tokLParen, val: "("}
	case ')':
		return token{kind: tokRParen, val: ")"}
	case '[':
		l.insideBracket = true
		return token{kind: tokLBracket, val: "["}
	case ']':
		l.insideBracket = false
		return token{kind: tokRBracket, val: "]"}
	case ',':
		return token{kind: tokComma, val: ","}
	case '+':
		return token{kind: tokPlus, val: "+"}
	case '-':
		return token{kind: tokMinus, val: "-"}
	case '*':
		return token{kind: tokStar, val: "*"}
	case '/':
		return token{kind: tokSlash, val: "/"}
	case '=':
		if l.peek() == '~' {
			l.pos++
			return token{kind: tokReEq, val: "=~"}
		}
		return token{kind: tokEq, val: "="}
	case '!':
		if l.peek() == '=' {
			l.pos++
			return token{kind: tokNeq, val: "!="}
		}
		if l.peek() == '~' {
			l.pos++
			return token{kind: tokReNeq, val: "!~"}
		}
		return token{kind: tokErr, val: "unexpected '!'"}
	}
	return token{kind: tokErr, val: fmt.Sprintf("unexpected char %q", ch)}
}

func (l *lexer) lexString() token {
	l.pos++ // consume opening "
	var sb strings.Builder
	for l.pos < len(l.input) {
		ch := l.advance()
		if ch == '\\' && l.pos < len(l.input) {
			sb.WriteByte(l.advance())
			continue
		}
		if ch == '"' {
			return token{kind: tokString, val: sb.String()}
		}
		sb.WriteByte(ch)
	}
	return token{kind: tokErr, val: "unterminated string"}
}

func (l *lexer) lexNumber() token {
	start := l.pos
	for l.pos < len(l.input) && (l.input[l.pos] >= '0' && l.input[l.pos] <= '9' || l.input[l.pos] == '.') {
		l.pos++
	}
	numStr := l.input[start:l.pos]
	// Inside a [...] window, a letter immediately after the number is a duration unit.
	// We just emit the number; the unit will be emitted on the next call via lexIdent.
	return token{kind: tokNumber, val: numStr}
}

func (l *lexer) lexIdent() token {
	start := l.pos
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if ch == '_' || unicode.IsLetter(rune(ch)) || (ch >= '0' && ch <= '9') {
			l.pos++
			continue
		}
		break
	}
	word := l.input[start:l.pos]
	// Inside a bracket, a bare s/m/h after a number becomes a duration unit.
	if l.insideBracket && (word == "s" || word == "m" || word == "h") {
		return token{kind: tokDurationUnit, val: word}
	}
	return token{kind: tokIdent, val: word}
}

// aggOps is the set of supported aggregation operators.
var aggOps = map[string]bool{
	"sum": true, "avg": true, "min": true, "max": true, "count": true,
}

// Parse parses a PromQL subset expression and returns the root AST node.
// Returns an error for unsupported constructs.
func Parse(expr string) (Node, error) {
	p := &parser{lex: newLexer(strings.TrimSpace(expr))}
	p.consume()
	if p.cur.kind == tokEOF {
		return nil, fmt.Errorf("unsupported: empty expression")
	}
	node, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.cur.kind != tokEOF {
		return nil, fmt.Errorf("unsupported: unexpected token %q after expression", p.cur.val)
	}
	return node, nil
}

type parser struct {
	lex *lexer
	cur token
}

func (p *parser) consume() {
	p.cur = p.lex.next()
}

// parseExpr handles the top level: binary ops, aggregations, rate, selectors.
func (p *parser) parseExpr() (Node, error) {
	// Scalar-left binary op: number OP expr
	if p.cur.kind == tokNumber {
		scalar, err := strconv.ParseFloat(p.cur.val, 64)
		if err != nil {
			return nil, fmt.Errorf("parse number %q: %w", p.cur.val, err)
		}
		p.consume()
		op, err := p.parseBinOp()
		if err != nil {
			return nil, fmt.Errorf("unsupported: scalar on left but no operator follows: %w", err)
		}
		rhs, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		return &BinaryOpNode{Op: op, Expr: rhs, Scalar: scalar, ScalarLeft: true}, nil
	}

	lhs, err := p.parseAtom()
	if err != nil {
		return nil, err
	}

	// Optional binary op. Right side may be a scalar (→ BinaryOpNode) or
	// another expression (→ BinaryExprNode, series-on-series).
	if isBinOpToken(p.cur.kind) {
		op := p.cur.val
		p.consume()
		if p.cur.kind == tokNumber {
			scalar, err := strconv.ParseFloat(p.cur.val, 64)
			if err != nil {
				return nil, fmt.Errorf("parse number %q: %w", p.cur.val, err)
			}
			p.consume()
			return &BinaryOpNode{Op: op, Expr: lhs, Scalar: scalar, ScalarLeft: false}, nil
		}
		rhs, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		return &BinaryExprNode{Op: op, LHS: lhs, RHS: rhs}, nil
	}

	return lhs, nil
}

func (p *parser) parseBinOp() (string, error) {
	if isBinOpToken(p.cur.kind) {
		op := p.cur.val
		p.consume()
		return op, nil
	}
	return "", fmt.Errorf("expected binary operator, got %q", p.cur.val)
}

func isBinOpToken(k tokenKind) bool {
	return k == tokPlus || k == tokMinus || k == tokStar || k == tokSlash
}

// parseAtom handles aggregation, rate, and selector.
func (p *parser) parseAtom() (Node, error) {
	if p.cur.kind != tokIdent {
		return nil, fmt.Errorf("unsupported: expected identifier, got %q", p.cur.val)
	}

	word := p.cur.val

	// Aggregation with optional "by (labels)" clause
	if aggOps[word] {
		return p.parseAggregation()
	}

	// rate / irate / increase: same shape `fn(selector[Nd])`.
	if rangeFuncs[word] {
		return p.parseRangeFunc(word)
	}

	// histogram_quantile(q, expr) — different shape: scalar literal,
	// comma, nested expression.
	if word == "histogram_quantile" {
		return p.parseHistogramQuantile()
	}

	// topk(k, expr) / bottomk(k, expr) — same two-arg shape as
	// histogram_quantile but K must be a positive integer literal.
	if word == "topk" || word == "bottomk" {
		return p.parseTopK(word)
	}

	// Consume the identifier.
	p.consume()

	// If the next token is '(', it's an unknown function call.
	if p.cur.kind == tokLParen {
		return nil, fmt.Errorf("unsupported: unknown function %q", word)
	}

	// Otherwise it's a plain selector — parse the optional {...}.
	return p.parseSelectorBody(word)
}

// parseSelectorBody parses the optional {matchers} after the metric name has already
// been consumed into `metric`.
func (p *parser) parseSelectorBody(metric string) (*SelectorNode, error) {
	node := &SelectorNode{Metric: metric}
	if p.cur.kind != tokLBrace {
		return node, nil
	}
	p.consume() // consume {
	for p.cur.kind != tokRBrace {
		if p.cur.kind == tokEOF {
			return nil, fmt.Errorf("parse: unterminated label matcher list")
		}
		m, err := p.parseLabelMatcher()
		if err != nil {
			return nil, err
		}
		node.Matchers = append(node.Matchers, m)
		if p.cur.kind == tokComma {
			p.consume()
		}
	}
	p.consume() // consume }
	return node, nil
}

func (p *parser) parseLabelMatcher() (LabelMatcher, error) {
	if p.cur.kind != tokIdent {
		return LabelMatcher{}, fmt.Errorf("parse: expected label name, got %q", p.cur.val)
	}
	name := p.cur.val
	p.consume()

	var op string
	switch p.cur.kind {
	case tokEq:
		op = "="
	case tokNeq:
		op = "!="
	case tokReEq:
		op = "=~"
	case tokReNeq:
		op = "!~"
	default:
		return LabelMatcher{}, fmt.Errorf("parse: expected label operator, got %q", p.cur.val)
	}
	p.consume()

	if p.cur.kind != tokString {
		return LabelMatcher{}, fmt.Errorf("parse: expected quoted string for label value, got %q", p.cur.val)
	}
	val := p.cur.val
	p.consume()
	return LabelMatcher{Name: name, Op: op, Value: val}, nil
}

// parseAggregation parses an aggregation expression of the form
// `op (expr)`, `op by (labels) (expr)`, or `op without (labels) (expr)`.
// The current token is the aggregation operator identifier when called.
// At most one of `by` and `without` may appear on a given aggregation.
func (p *parser) parseAggregation() (*AggregationNode, error) {
	op := p.cur.val
	p.consume() // consume agg keyword

	var by, without []string
	// Check for an optional "by (labels)" or "without (labels)" clause
	// before the expression. Only one of the two is allowed.
	if p.cur.kind == tokIdent && (p.cur.val == "by" || p.cur.val == "without") {
		keyword := p.cur.val
		p.consume() // consume "by" / "without"
		labels, err := p.parseLabelList(keyword)
		if err != nil {
			return nil, err
		}
		if keyword == "by" {
			by = labels
		} else {
			without = labels
		}
	}

	if p.cur.kind != tokLParen {
		return nil, fmt.Errorf("parse: expected '(' after aggregation operator %q, got %q", op, p.cur.val)
	}
	p.consume() // consume (

	inner, err := p.parseExpr()
	if err != nil {
		return nil, err
	}

	if p.cur.kind != tokRParen {
		return nil, fmt.Errorf("parse: expected ')' after aggregation expression, got %q", p.cur.val)
	}
	p.consume() // consume )

	return &AggregationNode{Op: op, By: by, Without: without, Expr: inner}, nil
}

// parseLabelList parses a parenthesised, comma-separated list of label
// names: `( a, b, c )`. keyword is the modifier name ("by" or "without")
// and is only used in error messages. The opening '(' is required even
// for an empty list.
func (p *parser) parseLabelList(keyword string) ([]string, error) {
	if p.cur.kind != tokLParen {
		return nil, fmt.Errorf("parse: expected '(' after %q, got %q", keyword, p.cur.val)
	}
	p.consume() // consume (
	var labels []string
	for p.cur.kind != tokRParen {
		if p.cur.kind == tokEOF {
			return nil, fmt.Errorf("parse: unterminated %s clause", keyword)
		}
		if p.cur.kind != tokIdent {
			return nil, fmt.Errorf("parse: expected label name in %s clause, got %q", keyword, p.cur.val)
		}
		labels = append(labels, p.cur.val)
		p.consume()
		if p.cur.kind == tokComma {
			p.consume()
		}
	}
	p.consume() // consume )
	return labels, nil
}

// rangeFuncs lists the range-vector functions the parser recognises.
// Every entry has the same shape `fn(selector[Nd])`, so they share a
// single parser routine; the evaluator dispatches on the name.
var rangeFuncs = map[string]bool{
	"rate":            true,
	"irate":           true,
	"increase":        true,
	"delta":           true,
	"avg_over_time":   true,
	"sum_over_time":   true,
	"min_over_time":   true,
	"max_over_time":   true,
	"count_over_time": true,
}

// parseRangeFunc parses `<fn>(metric[Nd])` for any fn in rangeFuncs.
// The current token is the function-name identifier when called.
func (p *parser) parseRangeFunc(fn string) (*RangeFuncNode, error) {
	p.consume() // consume function name
	if p.cur.kind != tokLParen {
		return nil, fmt.Errorf("parse: expected '(' after %q, got %q", fn, p.cur.val)
	}
	p.consume() // consume (

	// Inner expr must be a selector (no nested function calls or
	// aggregations on the range-vector argument in the MVP).
	if p.cur.kind != tokIdent {
		return nil, fmt.Errorf("parse: expected metric selector inside %s(), got %q", fn, p.cur.val)
	}
	metric := p.cur.val
	p.consume()
	inner, err := p.parseSelectorBody(metric)
	if err != nil {
		return nil, err
	}

	// Now expect [Nd]
	if p.cur.kind != tokLBracket {
		return nil, fmt.Errorf("parse: expected '[' for duration window in %s(), got %q", fn, p.cur.val)
	}
	p.consume() // consume [

	if p.cur.kind != tokNumber {
		return nil, fmt.Errorf("parse: expected duration number in %s window, got %q", fn, p.cur.val)
	}
	n, err := strconv.ParseInt(p.cur.val, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse: invalid duration number %q: %w", p.cur.val, err)
	}
	p.consume() // consume number

	if p.cur.kind != tokDurationUnit {
		return nil, fmt.Errorf("parse: expected duration unit (s/m/h) in %s window, got %q", fn, p.cur.val)
	}
	unit := p.cur.val
	p.consume() // consume unit

	if p.cur.kind != tokRBracket {
		return nil, fmt.Errorf("parse: expected ']' after duration window, got %q", p.cur.val)
	}
	p.consume() // consume ]

	if p.cur.kind != tokRParen {
		return nil, fmt.Errorf("parse: expected ')' after %s() window, got %q", fn, p.cur.val)
	}
	p.consume() // consume )

	return &RangeFuncNode{Func: fn, Expr: inner, Window: Duration{Value: n, Unit: unit}}, nil
}

// parseHistogramQuantile parses `histogram_quantile(q, expr)`. The
// current token is the function-name identifier when called. q must
// be a numeric literal in [0, 1]; expr is any expression accepted by
// parseExpr (typically a `rate(metric_bucket[w])` or a `sum by (..., le) (...)`).
func (p *parser) parseHistogramQuantile() (*HistogramQuantileNode, error) {
	p.consume() // consume "histogram_quantile"
	if p.cur.kind != tokLParen {
		return nil, fmt.Errorf("parse: expected '(' after histogram_quantile, got %q", p.cur.val)
	}
	p.consume() // consume (

	if p.cur.kind != tokNumber {
		return nil, fmt.Errorf("parse: expected numeric quantile literal in histogram_quantile, got %q", p.cur.val)
	}
	q, err := strconv.ParseFloat(p.cur.val, 64)
	if err != nil {
		return nil, fmt.Errorf("parse: invalid quantile %q: %w", p.cur.val, err)
	}
	if q < 0 || q > 1 {
		return nil, fmt.Errorf("parse: histogram_quantile q must be in [0, 1], got %v", q)
	}
	p.consume() // consume number

	if p.cur.kind != tokComma {
		return nil, fmt.Errorf("parse: expected ',' after quantile in histogram_quantile, got %q", p.cur.val)
	}
	p.consume() // consume ,

	inner, err := p.parseExpr()
	if err != nil {
		return nil, err
	}

	if p.cur.kind != tokRParen {
		return nil, fmt.Errorf("parse: expected ')' after histogram_quantile expression, got %q", p.cur.val)
	}
	p.consume() // consume )

	return &HistogramQuantileNode{Quantile: q, Expr: inner}, nil
}

// parseTopK parses `topk(k, expr)` / `bottomk(k, expr)`. The current
// token is the function-name identifier when called. K must be a
// positive integer literal; fractional or non-positive values are
// rejected at parse time.
func (p *parser) parseTopK(fn string) (*TopKNode, error) {
	p.consume() // consume "topk" / "bottomk"
	if p.cur.kind != tokLParen {
		return nil, fmt.Errorf("parse: expected '(' after %s, got %q", fn, p.cur.val)
	}
	p.consume() // consume (

	if p.cur.kind != tokNumber {
		return nil, fmt.Errorf("parse: expected integer K in %s, got %q", fn, p.cur.val)
	}
	// Reject fractional literals (anything containing a dot).
	if strings.Contains(p.cur.val, ".") {
		return nil, fmt.Errorf("parse: %s K must be a positive integer, got %q", fn, p.cur.val)
	}
	k, err := strconv.ParseInt(p.cur.val, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("parse: invalid %s K %q: %w", fn, p.cur.val, err)
	}
	if k <= 0 {
		return nil, fmt.Errorf("parse: %s K must be > 0, got %d", fn, k)
	}
	p.consume() // consume number

	if p.cur.kind != tokComma {
		return nil, fmt.Errorf("parse: expected ',' after K in %s, got %q", fn, p.cur.val)
	}
	p.consume() // consume ,

	inner, err := p.parseExpr()
	if err != nil {
		return nil, err
	}

	if p.cur.kind != tokRParen {
		return nil, fmt.Errorf("parse: expected ')' after %s expression, got %q", fn, p.cur.val)
	}
	p.consume() // consume )

	return &TopKNode{Op: fn, K: int(k), Expr: inner}, nil
}
