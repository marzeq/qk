package comptime

import (
	"maps"
	"math/big"
	"slices"
	"strings"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/shared"
	qktarget "github.com/marzeq/qk/target"
	"github.com/marzeq/qk/tokeniser"
)

type expander struct {
	tokens []tokeniser.Token
	pos    int
	target targetValues
	scopes []map[string]Value
}

type Config struct {
	TargetTriple   string
	NoLibc         bool
	NoStdlib       bool
	ModuleBindings map[string]Value
}

// Expand evaluates compile-time declarations and selects compile-time branches,
// returning the token stream that should continue through the compiler pipeline.
func Expand(tokens []tokeniser.Token, config Config) ([]tokeniser.Token, error) {
	target := targetFromTriple(config.TargetTriple)
	target.noLibc = config.NoLibc
	target.noStdlib = config.NoStdlib
	target.bindings = cloneValues(config.ModuleBindings)
	currentModule := tokenModule(tokens)
	if currentModule != "" {
		for name, value := range config.ModuleBindings {
			prefix := currentModule + "."
			if strings.HasPrefix(name, prefix) && !strings.Contains(strings.TrimPrefix(name, prefix), ".") {
				target.bindings[strings.TrimPrefix(name, prefix)] = value
			}
		}
	}
	e := &expander{tokens: tokens, target: target}
	return e.expand()
}

func (e *expander) expand() ([]tokeniser.Token, error) {
	result := make([]tokeniser.Token, 0, len(e.tokens))
	for e.pos < len(e.tokens) {
		tok := e.tokens[e.pos]
		if tok.Type == tokeniser.TokenOpenCurly {
			e.scopes = append(e.scopes, e.target.bindings)
			e.target.bindings = cloneValues(e.target.bindings)
		} else if tok.Type == tokeniser.TokenCloseCurly && len(e.scopes) != 0 {
			e.target.bindings = e.scopes[len(e.scopes)-1]
			e.scopes = e.scopes[:len(e.scopes)-1]
		}
		if tok.Type == tokeniser.TokenKeyword {
			switch tok.Value {
			case string(tokeniser.KeywordLet):
				rewritten, ok, err := e.expandDeclaration()
				if err != nil {
					return nil, err
				}
				if ok {
					result = append(result, rewritten...)
					continue
				}
			case string(tokeniser.KeywordWhen):
				continuesPrevious := whenContinuesPrevious(result)
				if continuesPrevious {
					for len(result) > 0 && result[len(result)-1].Type == tokeniser.TokenNewline {
						result = result[:len(result)-1]
					}
				}
				selected, err := e.expandWhen()
				if err != nil {
					return nil, err
				}
				if continuesPrevious && len(selected) == 0 {
					return nil, shared.NewError(tok.Loc, "selected compile-time block cannot be empty here")
				}
				result = append(result, selected...)
				continue
			case string(tokeniser.KeywordCompilerError):
				return nil, e.compilerError()
			}
		}
		if tok.Type == tokeniser.TokenIdentifier && e.isSliceExtentReference() {
			if value, ok := e.target.bindings[tok.Value]; ok && value.kind == valueInteger {
				literal, err := literalToken(value, tok.Loc)
				if err != nil {
					return nil, err
				}
				result = append(result, literal)
				e.pos++
				continue
			}
		}
		result = append(result, tok)
		e.pos++
	}
	return result, nil
}

func (e *expander) isSliceExtentReference() bool {
	previous := e.pos - 1
	for previous >= 0 && e.tokens[previous].Type == tokeniser.TokenNewline {
		previous--
	}
	next := e.pos + 1
	for next < len(e.tokens) && e.tokens[next].Type == tokeniser.TokenNewline {
		next++
	}
	if previous < 0 || next >= len(e.tokens) || e.tokens[next].Type != tokeniser.TokenCloseSquare {
		return false
	}
	return e.tokens[previous].Type == tokeniser.TokenComma || e.tokens[previous].Type == tokeniser.TokenSemicolon
}

func cloneValues(values map[string]Value) map[string]Value {
	cloned := make(map[string]Value, len(values))
	maps.Copy(cloned, values)
	return cloned
}

func (e *expander) expandDeclaration() ([]tokeniser.Token, bool, error) {
	start := e.pos
	namePos := start + 1
	if namePos < len(e.tokens) && e.tokens[namePos].Type == tokeniser.TokenKeyword && e.tokens[namePos].Value == string(tokeniser.KeywordMut) {
		namePos++
	}
	if namePos >= len(e.tokens) || e.tokens[namePos].Type != tokeniser.TokenIdentifier {
		return nil, false, nil
	}
	equals := namePos + 1
	for equals < len(e.tokens) && e.tokens[equals].Type != tokeniser.TokenEquals && e.tokens[equals].Type != tokeniser.TokenNewline && e.tokens[equals].Type != tokeniser.TokenSemicolon {
		if e.tokens[equals].Type == tokeniser.TokenLess {
			return nil, false, nil
		}
		equals++
	}
	if equals+1 >= len(e.tokens) || e.tokens[equals].Type != tokeniser.TokenEquals || e.tokens[equals+1].Type != tokeniser.TokenIdentifier || e.tokens[equals+1].Value != "comptime" {
		return nil, false, nil
	}
	exprStart := equals + 2
	end, parens, squares := exprStart, 0, 0
	for end < len(e.tokens) {
		tok := e.tokens[end]
		switch tok.Type {
		case tokeniser.TokenOpenParen:
			parens++
		case tokeniser.TokenCloseParen:
			parens--
		case tokeniser.TokenOpenSquare:
			squares++
		case tokeniser.TokenCloseSquare:
			squares--
		}
		if parens == 0 && squares == 0 && (tok.Type == tokeniser.TokenNewline || tok.Type == tokeniser.TokenSemicolon || tok.Type == tokeniser.TokenEof || tok.Type == tokeniser.TokenCloseCurly) {
			break
		}
		end++
	}
	name := e.tokens[namePos]
	if end == exprStart {
		return nil, true, shared.NewError(name.Loc, "expected expression after comptime")
	}
	expr, err := parseCondition(e.tokens[exprStart:end], name.Loc)
	if err != nil {
		return nil, true, err
	}
	value, err := evaluate(expr, e.target, nil)
	if err != nil {
		return nil, true, err
	}
	literal, err := literalToken(value, name.Loc)
	if err != nil {
		return nil, true, err
	}
	rewritten := append([]tokeniser.Token(nil), e.tokens[start:equals]...)
	rewritten = append(rewritten, e.tokens[equals])
	rewritten = append(rewritten, e.tokens[equals+1])
	rewritten = append(rewritten, literal)
	e.target.bindings[name.Value] = value
	e.pos = end
	return rewritten, true, nil
}

func whenContinuesPrevious(tokens []tokeniser.Token) bool {
	pos := len(tokens) - 1
	for pos >= 0 && tokens[pos].Type == tokeniser.TokenNewline {
		pos--
	}
	if pos < 0 {
		return false
	}
	previous := tokens[pos]
	if previous.Type == tokeniser.TokenKeyword {
		return previous.Value == string(tokeniser.KeywordType)
	}
	switch previous.Type {
	case tokeniser.TokenEquals, tokeniser.TokenComma, tokeniser.TokenColon,
		tokeniser.TokenOpenParen, tokeniser.TokenOpenSquare,
		tokeniser.TokenPlus, tokeniser.TokenMinus, tokeniser.TokenAsterisk,
		tokeniser.TokenSlash, tokeniser.TokenPercent, tokeniser.TokenAmpersand,
		tokeniser.TokenPipe, tokeniser.TokenCaret, tokeniser.TokenShiftLeft,
		tokeniser.TokenShiftRight, tokeniser.TokenEqualsEquals, tokeniser.TokenNotEquals,
		tokeniser.TokenLess, tokeniser.TokenLessEquals, tokeniser.TokenGreater,
		tokeniser.TokenGreaterEquals, tokeniser.TokenArrow, tokeniser.TokenFatArrow:
		return true
	default:
		return false
	}
}

func (e *expander) compilerError() error {
	directive := e.tokens[e.pos]
	e.pos++
	if e.pos >= len(e.tokens) || e.tokens[e.pos].Type != tokeniser.TokenOpenParen {
		return shared.NewError(directive.Loc, "expected '(' after 'compiler_error'")
	}
	e.pos++
	for e.pos < len(e.tokens) && e.tokens[e.pos].Type == tokeniser.TokenNewline {
		e.pos++
	}
	if e.pos >= len(e.tokens) || e.tokens[e.pos].Type != tokeniser.TokenString {
		return shared.NewError(directive.Loc, "expected a string message in 'compiler_error'")
	}
	message := e.tokens[e.pos].Value
	e.pos++
	for e.pos < len(e.tokens) && e.tokens[e.pos].Type == tokeniser.TokenNewline {
		e.pos++
	}
	if e.pos >= len(e.tokens) || e.tokens[e.pos].Type != tokeniser.TokenCloseParen {
		return shared.NewError(directive.Loc, "expected ')' after 'compiler_error' message")
	}
	return shared.NewError(directive.Loc, "%s", message)
}

func (e *expander) expandWhen() ([]tokeniser.Token, error) {
	matched := false
	var selected []tokeniser.Token
	for {
		when := e.tokens[e.pos]
		e.pos++
		conditionTokens, err := e.readCondition()
		if err != nil {
			return nil, err
		}
		condition, err := parseCondition(conditionTokens, when.Loc)
		if err != nil {
			return nil, err
		}
		value, err := evaluate(condition, e.target, nil)
		if err != nil {
			return nil, err
		}
		if value.kind != valueBool {
			return nil, shared.NewError(condition.GetLoc(), "compile-time condition must be boolean")
		}
		body, err := e.readBlock()
		if err != nil {
			return nil, err
		}
		if !matched && value.boolean {
			matched = true
			selected = body
		}

		next := e.skipNewlines(e.pos)
		if next >= len(e.tokens) || e.tokens[next].Type != tokeniser.TokenKeyword ||
			e.tokens[next].Value != string(tokeniser.KeywordElse) {
			break
		}
		e.pos = e.skipNewlines(next + 1)
		if e.pos < len(e.tokens) && e.tokens[e.pos].Type == tokeniser.TokenKeyword &&
			e.tokens[e.pos].Value == string(tokeniser.KeywordWhen) {
			continue
		}
		body, err = e.readBlock()
		if err != nil {
			return nil, err
		}
		if !matched {
			selected = body
		}
		break
	}

	selected = trimBoundaryNewlines(selected)
	nested := &expander{tokens: selected, target: e.target}
	expanded, err := nested.expand()
	e.target.bindings = nested.target.bindings
	return expanded, err
}

func trimBoundaryNewlines(tokens []tokeniser.Token) []tokeniser.Token {
	start, end := 0, len(tokens)
	for start < end && tokens[start].Type == tokeniser.TokenNewline {
		start++
	}
	for end > start && tokens[end-1].Type == tokeniser.TokenNewline {
		end--
	}
	return tokens[start:end]
}

func (e *expander) readCondition() ([]tokeniser.Token, error) {
	start := e.pos
	parenDepth, squareDepth := 0, 0
	for e.pos < len(e.tokens) {
		tok := e.tokens[e.pos]
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
				if e.pos == start {
					return nil, shared.NewError(tok.Loc, "expected condition after 'when'")
				}
				condition := e.tokens[start:e.pos]
				e.pos++
				return condition, nil
			}
		case tokeniser.TokenEof:
			return nil, shared.NewError(tok.Loc, "expected '{' after compile-time condition")
		}
		if parenDepth < 0 || squareDepth < 0 {
			return nil, shared.NewError(tok.Loc, "unbalanced delimiter in compile-time condition")
		}
		e.pos++
	}
	return nil, shared.NewError(e.tokens[start-1].Loc, "expected '{' after compile-time condition")
}

func (e *expander) readBlock() ([]tokeniser.Token, error) {
	if e.pos == 0 || e.tokens[e.pos-1].Type != tokeniser.TokenOpenCurly {
		if e.pos >= len(e.tokens) || e.tokens[e.pos].Type != tokeniser.TokenOpenCurly {
			loc := e.tokens[min(e.pos, len(e.tokens)-1)].Loc
			return nil, shared.NewError(loc, "expected '{' after 'else'")
		}
		e.pos++
	}
	start, depth := e.pos, 1
	for e.pos < len(e.tokens) {
		switch e.tokens[e.pos].Type {
		case tokeniser.TokenOpenCurly:
			depth++
		case tokeniser.TokenCloseCurly:
			depth--
			if depth == 0 {
				body := e.tokens[start:e.pos]
				e.pos++
				return body, nil
			}
		case tokeniser.TokenEof:
			return nil, shared.NewError(e.tokens[e.pos].Loc, "expected '}' to close compile-time block")
		}
		e.pos++
	}
	return nil, shared.NewError(e.tokens[start-1].Loc, "expected '}' to close compile-time block")
}

func (e *expander) skipNewlines(pos int) int {
	for pos < len(e.tokens) && e.tokens[pos].Type == tokeniser.TokenNewline {
		pos++
	}
	return pos
}

func parseCondition(tokens []tokeniser.Token, fallback shared.Location) (parser.ExpressionNode, error) {
	condition := make([]tokeniser.Token, 0, len(tokens)+1)
	for _, token := range tokens {
		if token.Type != tokeniser.TokenNewline {
			condition = append(condition, token)
		}
	}
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

// Value is a value resolved during compile-time expansion.
type Value struct {
	kind    valueKind
	boolean bool
	integer *big.Int
	domain  string
	name    string
}

type targetValues struct {
	os          string
	arch        string
	environment string
	pointerBits int64
	noLibc      bool
	noStdlib    bool
	bindings    map[string]Value
}

func evaluate(node parser.ExpressionNode, target targetValues, resolveBinding func(string, shared.Location) (Value, error)) (Value, error) {
	switch n := node.(type) {
	case *parser.BoolLiteralNode:
		return Value{kind: valueBool, boolean: n.Value == string(tokeniser.KeywordTrue)}, nil
	case *parser.IntegerLiteralNode:
		value, ok := new(big.Int).SetString(n.Value, 10)
		if !ok {
			return Value{}, shared.NewError(n.Loc, "invalid integer in compile-time expression")
		}
		return Value{kind: valueInteger, integer: value}, nil
	case *parser.EnumLiteralNode:
		return Value{kind: valueEnumLiteral, name: n.Variant}, nil
	case *parser.IdentifierNode:
		switch n.Name {
		case "OS":
			return Value{kind: valueEnum, domain: "OS", name: target.os}, nil
		case "Arch":
			return Value{kind: valueEnum, domain: "Arch", name: target.arch}, nil
		case "Environment":
			return Value{kind: valueEnum, domain: "Environment", name: target.environment}, nil
		case "PointerBits":
			return Value{kind: valueInteger, integer: big.NewInt(target.pointerBits)}, nil
		case "NoLibc":
			return Value{kind: valueBool, boolean: target.noLibc}, nil
		case "NoStdlib":
			return Value{kind: valueBool, boolean: target.noStdlib}, nil
		default:
			if resolveBinding != nil {
				value, err := resolveBinding(n.Name, n.Loc)
				if err == nil {
					return value, nil
				}
				return Value{}, err
			}
			if value, ok := target.bindings[n.Name]; ok {
				return value, nil
			}
			return Value{}, shared.NewError(n.Loc, "unknown compile-time value %q", n.Name)
		}
	case *parser.FieldAccessNode:
		name, ok := compileTimeName(n)
		if !ok {
			return Value{}, unsupported(node)
		}
		if resolveBinding != nil {
			return resolveBinding(name, n.Loc)
		}
		if value, ok := target.bindings[name]; ok {
			return value, nil
		}
		return Value{}, shared.NewError(n.Loc, "unknown compile-time value %q", name)
	case *parser.UnaryOpNode:
		operand, err := evaluate(n.Operand, target, resolveBinding)
		if err != nil {
			return Value{}, err
		}
		if n.Op == parser.UnaryOpLogicalNot && operand.kind == valueBool {
			return Value{kind: valueBool, boolean: !operand.boolean}, nil
		}
		if n.Op == parser.UnaryOpNegate && operand.kind == valueInteger {
			return Value{kind: valueInteger, integer: new(big.Int).Neg(operand.integer)}, nil
		}
		return Value{}, shared.NewError(n.Loc, "invalid unary operator for compile-time value")
	case *parser.BinaryOpNode:
		left, err := evaluate(n.Operand1, target, resolveBinding)
		if err != nil {
			return Value{}, err
		}
		right, err := evaluate(n.Operand2, target, resolveBinding)
		if err != nil {
			return Value{}, err
		}
		switch n.Op {
		case parser.BinaryOpLogicalAnd, parser.BinaryOpLogicalOr:
			if left.kind != valueBool || right.kind != valueBool {
				return Value{}, shared.NewError(n.Loc, "logical compile-time operators require boolean operands")
			}
			value := left.boolean && right.boolean
			if n.Op == parser.BinaryOpLogicalOr {
				value = left.boolean || right.boolean
			}
			return Value{kind: valueBool, boolean: value}, nil
		case parser.BinaryOpEqual, parser.BinaryOpNotEqual:
			equal, err := equalValues(left, right, n.Loc)
			if err != nil {
				return Value{}, err
			}
			if n.Op == parser.BinaryOpNotEqual {
				equal = !equal
			}
			return Value{kind: valueBool, boolean: equal}, nil
		case parser.BinaryOpAdd, parser.BinaryOpSubtract, parser.BinaryOpMultiply, parser.BinaryOpDivide, parser.BinaryOpModulo:
			if left.kind != valueInteger || right.kind != valueInteger {
				return Value{}, shared.NewError(n.Loc, "arithmetic compile-time operators require integer operands")
			}
			if (n.Op == parser.BinaryOpDivide || n.Op == parser.BinaryOpModulo) && right.integer.Sign() == 0 {
				return Value{}, shared.NewError(n.Loc, "division by zero in compile-time expression")
			}
			value := new(big.Int)
			switch n.Op {
			case parser.BinaryOpAdd:
				value.Add(left.integer, right.integer)
			case parser.BinaryOpSubtract:
				value.Sub(left.integer, right.integer)
			case parser.BinaryOpMultiply:
				value.Mul(left.integer, right.integer)
			case parser.BinaryOpDivide:
				value.Quo(left.integer, right.integer)
			case parser.BinaryOpModulo:
				value.Rem(left.integer, right.integer)
			}
			return Value{kind: valueInteger, integer: value}, nil
		case parser.BinaryOpLess, parser.BinaryOpLessEqual, parser.BinaryOpGreater, parser.BinaryOpGreaterEqual:
			if left.kind != valueInteger || right.kind != valueInteger {
				return Value{}, shared.NewError(n.Loc, "ordered compile-time comparisons require integer operands")
			}
			comparison := left.integer.Cmp(right.integer)
			var value bool
			switch n.Op {
			case parser.BinaryOpLess:
				value = comparison < 0
			case parser.BinaryOpLessEqual:
				value = comparison <= 0
			case parser.BinaryOpGreater:
				value = comparison > 0
			case parser.BinaryOpGreaterEqual:
				value = comparison >= 0
			}
			return Value{kind: valueBool, boolean: value}, nil
		default:
			return Value{}, unsupported(node)
		}
	default:
		return Value{}, unsupported(node)
	}
}

func compileTimeName(node parser.ExpressionNode) (string, bool) {
	switch n := node.(type) {
	case *parser.IdentifierNode:
		return n.Name, true
	case *parser.FieldAccessNode:
		prefix, ok := compileTimeName(n.Subject)
		if !ok {
			return "", false
		}
		return prefix + "." + n.Field.Name, true
	default:
		return "", false
	}
}

func equalValues(left, right Value, loc shared.Location) (bool, error) {
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
		return left.integer.Cmp(right.integer) == 0, nil
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
	return shared.NewError(node.GetLoc(), "expression is not supported in a compile-time context")
}

func validVariant(domain, variant string) bool {
	values := map[string][]string{
		"OS":          {"Windows", "Linux", "MacOS", "FreeBSD", "OpenBSD", "NetBSD", "DragonFly", "WASI"},
		"Arch":        {"X86", "X86_64", "ARM32", "AArch64", "Wasm32", "Wasm64"},
		"Environment": {"GNU", "MSVC", "Musl", "Unknown"},
	}
	return slices.Contains(values[domain], variant)
}

func targetFromTriple(triple string) targetValues {
	triple = qktarget.EffectiveTriple(triple)
	target := strings.ToLower(triple)
	archName := qktarget.Arch(triple)
	values := targetValues{os: "Unknown", arch: "Unknown", environment: "Unknown"}
	if bits, ok := qktarget.PointerBits(triple); ok {
		values.pointerBits = int64(bits)
	}
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
		values.arch = "X86"
	case "x86_64", "amd64":
		values.arch = "X86_64"
	case "arm", "armv6", "armv7", "armv7a", "armv7l", "thumb", "thumbv7", "thumbv7a":
		values.arch = "ARM32"
	case "aarch64", "arm64":
		values.arch = "AArch64"
	case "wasm32":
		values.arch = "Wasm32"
	case "wasm64":
		values.arch = "Wasm64"
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
