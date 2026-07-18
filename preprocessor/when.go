package preprocessor

import (
	"runtime"
	"strconv"
	"strings"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/tokeniser"
)

type Processor struct {
	tokens []tokeniser.Token
	pos    int
	target targetValues
}

func Process(tokens []tokeniser.Token, targetTriple string) ([]tokeniser.Token, error) {
	p := &Processor{tokens: tokens, target: targetFromTriple(targetTriple)}
	return p.process()
}

func (p *Processor) process() ([]tokeniser.Token, error) {
	result := make([]tokeniser.Token, 0, len(p.tokens))
	for p.pos < len(p.tokens) {
		tok := p.tokens[p.pos]
		if tok.Type == tokeniser.TokenKeyword {
			switch tok.Value {
			case string(tokeniser.KeywordWhen):
				selected, err := p.processWhen()
				if err != nil {
					return nil, err
				}
				result = append(result, selected...)
				continue
			case string(tokeniser.KeywordCompilerError):
				return nil, p.compilerError()
			}
		}
		result = append(result, tok)
		p.pos++
	}
	return result, nil
}

func (p *Processor) compilerError() error {
	directive := p.tokens[p.pos]
	p.pos++
	if p.pos >= len(p.tokens) || p.tokens[p.pos].Type != tokeniser.TokenOpenParen {
		return shared.NewError(directive.Loc, "expected '(' after 'compiler_error'")
	}
	p.pos++
	for p.pos < len(p.tokens) && p.tokens[p.pos].Type == tokeniser.TokenNewline {
		p.pos++
	}
	if p.pos >= len(p.tokens) || p.tokens[p.pos].Type != tokeniser.TokenString {
		return shared.NewError(directive.Loc, "expected a string message in 'compiler_error'")
	}
	message := p.tokens[p.pos].Value
	p.pos++
	for p.pos < len(p.tokens) && p.tokens[p.pos].Type == tokeniser.TokenNewline {
		p.pos++
	}
	if p.pos >= len(p.tokens) || p.tokens[p.pos].Type != tokeniser.TokenCloseParen {
		return shared.NewError(directive.Loc, "expected ')' after 'compiler_error' message")
	}
	return shared.NewError(directive.Loc, "%s", message)
}

func (p *Processor) processWhen() ([]tokeniser.Token, error) {
	matched := false
	var selected []tokeniser.Token
	for {
		when := p.tokens[p.pos]
		p.pos++
		conditionTokens, err := p.readCondition()
		if err != nil {
			return nil, err
		}
		condition, err := parseCondition(conditionTokens, when.Loc)
		if err != nil {
			return nil, err
		}
		value, err := evaluate(condition, p.target)
		if err != nil {
			return nil, err
		}
		if value.kind != valueBool {
			return nil, shared.NewError(condition.GetLoc(), "compile-time condition must be boolean")
		}
		body, err := p.readBlock()
		if err != nil {
			return nil, err
		}
		if !matched && value.boolean {
			matched = true
			selected = body
		}

		next := p.skipNewlines(p.pos)
		if next >= len(p.tokens) || p.tokens[next].Type != tokeniser.TokenKeyword ||
			p.tokens[next].Value != string(tokeniser.KeywordElse) {
			break
		}
		p.pos = p.skipNewlines(next + 1)
		if p.pos < len(p.tokens) && p.tokens[p.pos].Type == tokeniser.TokenKeyword &&
			p.tokens[p.pos].Value == string(tokeniser.KeywordWhen) {
			continue
		}
		body, err = p.readBlock()
		if err != nil {
			return nil, err
		}
		if !matched {
			selected = body
		}
		break
	}

	nested := &Processor{tokens: selected, target: p.target}
	return nested.process()
}

func (p *Processor) readCondition() ([]tokeniser.Token, error) {
	start := p.pos
	parenDepth, squareDepth := 0, 0
	for p.pos < len(p.tokens) {
		tok := p.tokens[p.pos]
		switch tok.Type {
		case tokeniser.TokenOpenParen:
			parenDepth++
		case tokeniser.TokenCloseParen:
			parenDepth--
		case tokeniser.TokenOpenSquare:
			squareDepth++
		case tokeniser.TokenCloseSquare:
			squareDepth--
		case tokeniser.TokenOpenCurly:
			if parenDepth == 0 && squareDepth == 0 {
				if p.pos == start {
					return nil, shared.NewError(tok.Loc, "expected condition after 'when'")
				}
				condition := p.tokens[start:p.pos]
				p.pos++
				return condition, nil
			}
		case tokeniser.TokenEof:
			return nil, shared.NewError(tok.Loc, "expected '{' after compile-time condition")
		}
		if parenDepth < 0 || squareDepth < 0 {
			return nil, shared.NewError(tok.Loc, "unbalanced delimiter in compile-time condition")
		}
		p.pos++
	}
	return nil, shared.NewError(p.tokens[start-1].Loc, "expected '{' after compile-time condition")
}

func (p *Processor) readBlock() ([]tokeniser.Token, error) {
	if p.pos == 0 || p.tokens[p.pos-1].Type != tokeniser.TokenOpenCurly {
		if p.pos >= len(p.tokens) || p.tokens[p.pos].Type != tokeniser.TokenOpenCurly {
			loc := p.tokens[min(p.pos, len(p.tokens)-1)].Loc
			return nil, shared.NewError(loc, "expected '{' after 'else'")
		}
		p.pos++
	}
	start, depth := p.pos, 1
	for p.pos < len(p.tokens) {
		switch p.tokens[p.pos].Type {
		case tokeniser.TokenOpenCurly:
			depth++
		case tokeniser.TokenCloseCurly:
			depth--
			if depth == 0 {
				body := p.tokens[start:p.pos]
				p.pos++
				return body, nil
			}
		case tokeniser.TokenEof:
			return nil, shared.NewError(p.tokens[p.pos].Loc, "expected '}' to close compile-time block")
		}
		p.pos++
	}
	return nil, shared.NewError(p.tokens[start-1].Loc, "expected '}' to close compile-time block")
}

func (p *Processor) skipNewlines(pos int) int {
	for pos < len(p.tokens) && p.tokens[pos].Type == tokeniser.TokenNewline {
		pos++
	}
	return pos
}

func parseCondition(tokens []tokeniser.Token, fallback shared.Location) (parser.ExpressionNode, error) {
	condition := append([]tokeniser.Token(nil), tokens...)
	loc := fallback
	if len(condition) > 0 {
		loc = condition[len(condition)-1].Loc
	}
	condition = append(condition, tokeniser.Token{Type: tokeniser.TokenEof, Loc: loc})
	return parser.NewParser(condition).ParseWholeExpression()
}

type valueKind uint8

const (
	valueBool valueKind = iota
	valueInteger
	valueEnum
	valueEnumLiteral
)

type compileTimeValue struct {
	kind    valueKind
	boolean bool
	integer int64
	domain  string
	name    string
}

type targetValues struct {
	os          string
	arch        string
	environment string
	pointerBits int64
}

func evaluate(node parser.ExpressionNode, target targetValues) (compileTimeValue, error) {
	switch n := node.(type) {
	case *parser.BoolLiteralNode:
		return compileTimeValue{kind: valueBool, boolean: n.Value == string(tokeniser.KeywordTrue)}, nil
	case *parser.IntegerLiteralNode:
		value, err := strconv.ParseInt(n.Value, 10, 64)
		if err != nil {
			return compileTimeValue{}, shared.NewError(n.Loc, "invalid integer in compile-time condition")
		}
		return compileTimeValue{kind: valueInteger, integer: value}, nil
	case *parser.EnumLiteralNode:
		return compileTimeValue{kind: valueEnumLiteral, name: n.Variant}, nil
	case *parser.IdentifierNode:
		switch n.Name {
		case "OS":
			return compileTimeValue{kind: valueEnum, domain: "OS", name: target.os}, nil
		case "Arch":
			return compileTimeValue{kind: valueEnum, domain: "Arch", name: target.arch}, nil
		case "Environment":
			return compileTimeValue{kind: valueEnum, domain: "Environment", name: target.environment}, nil
		case "PointerBits":
			return compileTimeValue{kind: valueInteger, integer: target.pointerBits}, nil
		default:
			return compileTimeValue{}, shared.NewError(n.Loc, "unknown compile-time value %q", n.Name)
		}
	case *parser.UnaryOpNode:
		if n.Op != parser.UnaryOpLogicalNot {
			return compileTimeValue{}, unsupported(node)
		}
		operand, err := evaluate(n.Operand, target)
		if err != nil {
			return compileTimeValue{}, err
		}
		if operand.kind != valueBool {
			return compileTimeValue{}, shared.NewError(n.Loc, "operator 'not' requires a boolean compile-time value")
		}
		return compileTimeValue{kind: valueBool, boolean: !operand.boolean}, nil
	case *parser.BinaryOpNode:
		left, err := evaluate(n.Operand1, target)
		if err != nil {
			return compileTimeValue{}, err
		}
		right, err := evaluate(n.Operand2, target)
		if err != nil {
			return compileTimeValue{}, err
		}
		switch n.Op {
		case parser.BinaryOpLogicalAnd, parser.BinaryOpLogicalOr:
			if left.kind != valueBool || right.kind != valueBool {
				return compileTimeValue{}, shared.NewError(n.Loc, "logical compile-time operators require boolean operands")
			}
			value := left.boolean && right.boolean
			if n.Op == parser.BinaryOpLogicalOr {
				value = left.boolean || right.boolean
			}
			return compileTimeValue{kind: valueBool, boolean: value}, nil
		case parser.BinaryOpEqual, parser.BinaryOpNotEqual:
			equal, err := equalValues(left, right, n.Loc)
			if err != nil {
				return compileTimeValue{}, err
			}
			if n.Op == parser.BinaryOpNotEqual {
				equal = !equal
			}
			return compileTimeValue{kind: valueBool, boolean: equal}, nil
		default:
			return compileTimeValue{}, unsupported(node)
		}
	default:
		return compileTimeValue{}, unsupported(node)
	}
}

func equalValues(left, right compileTimeValue, loc shared.Location) (bool, error) {
	if left.kind == valueEnumLiteral && right.kind == valueEnum {
		left, right = right, left
	}
	if left.kind == valueEnum && right.kind == valueEnumLiteral {
		if !validVariant(left.domain, right.name) {
			return false, shared.NewError(loc, "unknown %s value .%s", left.domain, right.name)
		}
		return left.name == right.name, nil
	}
	if left.kind != right.kind {
		return false, shared.NewError(loc, "cannot compare different compile-time value types")
	}
	switch left.kind {
	case valueBool:
		return left.boolean == right.boolean, nil
	case valueInteger:
		return left.integer == right.integer, nil
	case valueEnum:
		if left.domain != right.domain {
			return false, shared.NewError(loc, "cannot compare %s and %s values", left.domain, right.domain)
		}
		return left.name == right.name, nil
	default:
		return false, shared.NewError(loc, "enum literals require a target value for their type")
	}
}

func unsupported(node parser.ExpressionNode) error {
	return shared.NewError(node.GetLoc(), "expression is not supported in a compile-time condition")
}

func validVariant(domain, variant string) bool {
	values := map[string][]string{
		"OS":          {"Windows", "Linux", "MacOS", "FreeBSD", "OpenBSD", "NetBSD", "DragonFly", "WASI"},
		"Arch":        {"X86", "X86_64", "ARM32", "AArch64", "Wasm32", "Wasm64"},
		"Environment": {"GNU", "MSVC", "Musl", "Unknown"},
	}
	for _, value := range values[domain] {
		if variant == value {
			return true
		}
	}
	return false
}

func targetFromTriple(triple string) targetValues {
	if triple == "" {
		triple = runtime.GOARCH + "-" + runtime.GOOS
	}
	target := strings.ToLower(triple)
	archName := target
	if before, _, ok := strings.Cut(target, "-"); ok {
		archName = before
	}
	values := targetValues{os: "Unknown", arch: "Unknown", environment: "Unknown"}
	switch {
	case strings.Contains(target, "windows"), strings.Contains(target, "mingw"), strings.Contains(target, "msvc"), strings.Contains(target, "win32"):
		values.os = "Windows"
	case strings.Contains(target, "linux"):
		values.os = "Linux"
	case strings.Contains(target, "darwin"), strings.Contains(target, "apple"), strings.Contains(target, "macos"):
		values.os = "MacOS"
	case strings.Contains(target, "freebsd"):
		values.os = "FreeBSD"
	case strings.Contains(target, "openbsd"):
		values.os = "OpenBSD"
	case strings.Contains(target, "netbsd"):
		values.os = "NetBSD"
	case strings.Contains(target, "dragonfly"):
		values.os = "DragonFly"
	case strings.Contains(target, "wasi"):
		values.os = "WASI"
	}
	switch archName {
	case "386", "i386", "i486", "i586", "i686", "x86":
		values.arch, values.pointerBits = "X86", 32
	case "x86_64", "amd64":
		values.arch, values.pointerBits = "X86_64", 64
	case "arm", "armv6", "armv7", "armv7a", "armv7l", "thumb", "thumbv7", "thumbv7a":
		values.arch, values.pointerBits = "ARM32", 32
	case "aarch64", "arm64":
		values.arch, values.pointerBits = "AArch64", 64
	case "wasm32":
		values.arch, values.pointerBits = "Wasm32", 32
	case "wasm64":
		values.arch, values.pointerBits = "Wasm64", 64
	}
	switch {
	case strings.Contains(target, "msvc"):
		values.environment = "MSVC"
	case strings.Contains(target, "musl"):
		values.environment = "Musl"
	case strings.Contains(target, "gnu"), strings.Contains(target, "mingw"):
		values.environment = "GNU"
	}
	return values
}
