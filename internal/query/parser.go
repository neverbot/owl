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

	// rate function
	if word == "rate" {
		return p.parseRate()
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

func (p *parser) parseAggregation() (*AggregationNode, error) {
	op := p.cur.val
	p.consume() // consume agg keyword

	var by []string
	// Check for "by (labels)" clause before the expression.
	if p.cur.kind == tokIdent && p.cur.val == "by" {
		p.consume() // consume "by"
		if p.cur.kind != tokLParen {
			return nil, fmt.Errorf("parse: expected '(' after 'by', got %q", p.cur.val)
		}
		p.consume() // consume (
		for p.cur.kind != tokRParen {
			if p.cur.kind == tokEOF {
				return nil, fmt.Errorf("parse: unterminated by clause")
			}
			if p.cur.kind != tokIdent {
				return nil, fmt.Errorf("parse: expected label name in by clause, got %q", p.cur.val)
			}
			by = append(by, p.cur.val)
			p.consume()
			if p.cur.kind == tokComma {
				p.consume()
			}
		}
		p.consume() // consume )
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

	return &AggregationNode{Op: op, By: by, Expr: inner}, nil
}

func (p *parser) parseRate() (*RateNode, error) {
	p.consume() // consume "rate"
	if p.cur.kind != tokLParen {
		return nil, fmt.Errorf("parse: expected '(' after 'rate', got %q", p.cur.val)
	}
	p.consume() // consume (

	// Inner expr must be a selector (no nested rate/aggregation in MVP).
	if p.cur.kind != tokIdent {
		return nil, fmt.Errorf("parse: expected metric selector inside rate(), got %q", p.cur.val)
	}
	metric := p.cur.val
	p.consume()
	inner, err := p.parseSelectorBody(metric)
	if err != nil {
		return nil, err
	}

	// Now expect [Nd]
	if p.cur.kind != tokLBracket {
		return nil, fmt.Errorf("parse: expected '[' for duration window in rate(), got %q", p.cur.val)
	}
	p.consume() // consume [

	if p.cur.kind != tokNumber {
		return nil, fmt.Errorf("parse: expected duration number in rate window, got %q", p.cur.val)
	}
	n, err := strconv.ParseInt(p.cur.val, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse: invalid duration number %q: %w", p.cur.val, err)
	}
	p.consume() // consume number

	if p.cur.kind != tokDurationUnit {
		return nil, fmt.Errorf("parse: expected duration unit (s/m/h) in rate window, got %q", p.cur.val)
	}
	unit := p.cur.val
	p.consume() // consume unit

	if p.cur.kind != tokRBracket {
		return nil, fmt.Errorf("parse: expected ']' after duration window, got %q", p.cur.val)
	}
	p.consume() // consume ]

	if p.cur.kind != tokRParen {
		return nil, fmt.Errorf("parse: expected ')' after rate() window, got %q", p.cur.val)
	}
	p.consume() // consume )

	return &RateNode{Expr: inner, Window: Duration{Value: n, Unit: unit}}, nil
}
