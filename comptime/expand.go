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
	Sysroot        string
	ReleaseMode    ReleaseMode
	PackagePath    string
	ModuleBindings map[string]Value
}

type ReleaseMode uint8

const (
	ReleaseModeDebug ReleaseMode = iota
	ReleaseModeRelease
)

// Expand evaluates compile-time declarations and selects compile-time branches,
// returning the token stream that should continue through the compiler pipeline.
func Expand(tokens []tokeniser.Token, config Config) ([]tokeniser.Token, error) {
	if err := validateLinkAttributes(tokens); err != nil {
		return nil, err
	}
	if err := validateWhenPlacements(tokens); err != nil {
		return nil, err
	}
	target := targetFromConfig(config)
	target.releaseMode = config.ReleaseMode
	target.bindings = make(map[string]Value)
	currentModule := config.PackagePath
	if currentModule == "" {
		currentModule = tokenModule(tokens)
	}
	addBindings := func(packagePath, qualifier string) {
		prefix := packagePath + "."
		for name, value := range config.ModuleBindings {
			bindingName, ok := strings.CutPrefix(name, prefix)
			if !ok || strings.Contains(bindingName, ".") {
				continue
			}
			target.bindings[qualifier+bindingName] = value
		}
	}
	if currentModule != "" {
		addBindings(currentModule, "")
	}
	if header, err := parser.ScanSourceHeader(tokens); err == nil {
		for i, imported := range header.Imports {
			addBindings(imported, imported+".")
			if i < len(header.Aliases) && header.Aliases[i] != "" {
				addBindings(imported, header.Aliases[i]+".")
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
		if tok.Type == tokeniser.TokenAt && e.pos+1 < len(e.tokens) &&
			e.tokens[e.pos+1].Type == tokeniser.TokenIdentifier {
			switch e.tokens[e.pos+1].Value {
			case "compiler_error":
				return nil, e.compilerError()
			case "compiler_assert":
				if err := e.compilerAssert(); err != nil {
					return nil, err
				}
				continue
			}
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
			}
		}
		if tok.Type == tokeniser.TokenIdentifier && e.isExtentReference() {
			name, end := tok.Value, e.pos+1
			for end+1 < len(e.tokens) && e.tokens[end].Type == tokeniser.TokenDot && e.tokens[end+1].Type == tokeniser.TokenIdentifier {
				name += "." + e.tokens[end+1].Value
				end += 2
			}
			if value, ok := e.target.bindings[name]; ok && value.kind == valueInteger {
				literal, err := literalToken(value, tok.Loc)
				if err != nil {
					return nil, err
				}
				result = append(result, literal)
				e.pos = end
				continue
			}
		}
		result = append(result, tok)
		e.pos++
	}
	return result, nil
}

func (e *expander) isExtentReference() bool {
	if e.pos > 0 && e.tokens[e.pos-1].Type == tokeniser.TokenDot {
		return false
	}
	open := -1
	depth := 0
	for pos := e.pos - 1; pos >= 0; pos-- {
		switch e.tokens[pos].Type {
		case tokeniser.TokenCloseSquare:
			depth++
		case tokeniser.TokenOpenSquare:
			if depth == 0 {
				open = pos
				pos = -1
			} else {
				depth--
			}
		}
	}
	if open < 0 {
		return false
	}
	close := -1
	depth = 0
	for pos := open + 1; pos < len(e.tokens); pos++ {
		switch e.tokens[pos].Type {
		case tokeniser.TokenOpenSquare:
			depth++
		case tokeniser.TokenCloseSquare:
			if depth == 0 {
				close = pos
				pos = len(e.tokens)
			} else {
				depth--
			}
		}
	}
	if close < 0 || e.pos >= close {
		return false
	}

	// The expression after a top-level semicolon is a repeated literal's
	// extent, regardless of whether it is followed by another type.
	depth = 0
	for pos := open + 1; pos < e.pos; pos++ {
		switch e.tokens[pos].Type {
		case tokeniser.TokenOpenParen, tokeniser.TokenOpenSquare:
			depth++
		case tokeniser.TokenCloseParen, tokeniser.TokenCloseSquare:
			depth--
		case tokeniser.TokenSemicolon:
			if depth == 0 {
				return true
			}
		}
	}

	next := close + 1
	for next < len(e.tokens) && e.tokens[next].Type == tokeniser.TokenNewline {
		next++
	}
	return next < len(e.tokens) && tokenStartsType(e.tokens[next])
}

func tokenStartsType(tok tokeniser.Token) bool {
	switch tok.Type {
	case tokeniser.TokenIdentifier, tokeniser.TokenAsterisk, tokeniser.TokenOpenSquare, tokeniser.TokenOpenParen, tokeniser.TokenAt:
		return true
	case tokeniser.TokenKeyword:
		switch tok.Value {
		case string(tokeniser.KeywordDyn), string(tokeniser.KeywordMut),
			string(tokeniser.KeywordStruct), string(tokeniser.KeywordEnum), string(tokeniser.KeywordUnion),
			string(tokeniser.KeywordOpaque), string(tokeniser.KeywordTrait):
			return true
		}
	}
	return false
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
	if namePos >= len(e.tokens) || e.tokens[namePos].Type != tokeniser.TokenDollar {
		return nil, false, nil
	}
	namePos++
	if namePos >= len(e.tokens) || e.tokens[namePos].Type != tokeniser.TokenIdentifier {
		return nil, false, nil
	}
	equals := namePos + 1
	if equals < len(e.tokens) && e.tokens[equals].Type == tokeniser.TokenOpenParen {
		// Functions and parameterized compile-time bindings remain for the
		// parser and semantic specialization pipeline.
		return nil, false, nil
	}
	for equals < len(e.tokens) && e.tokens[equals].Type != tokeniser.TokenEquals && e.tokens[equals].Type != tokeniser.TokenNewline && e.tokens[equals].Type != tokeniser.TokenSemicolon {
		if e.tokens[equals].Type == tokeniser.TokenLess {
			return nil, false, nil
		}
		equals++
	}
	if equals+1 >= len(e.tokens) || e.tokens[equals].Type != tokeniser.TokenEquals {
		return nil, false, nil
	}
	exprStart := equals + 1
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
		return nil, true, shared.NewError(name.Loc, "expected expression for compile-time binding")
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
		return previous.Value == string(tokeniser.KeywordType) || previous.Value == string(tokeniser.KeywordReturn)
	}
	switch previous.Type {
	case tokeniser.TokenEquals, tokeniser.TokenFatArrow:
		return true
	default:
		return false
	}
}

func validateWhenPlacements(tokens []tokeniser.Token) error {
	for pos, tok := range tokens {
		if tok.Type != tokeniser.TokenKeyword || tok.Value != string(tokeniser.KeywordWhen) {
			continue
		}
		previous := pos - 1
		for previous >= 0 && tokens[previous].Type == tokeniser.TokenNewline {
			previous--
		}
		if previous >= 0 && tokens[previous].Type == tokeniser.TokenKeyword &&
			tokens[previous].Value == string(tokeniser.KeywordElse) {
			continue
		}
		if insideLinkAttribute(tokens, pos) || previous < 0 {
			continue
		}
		immediate := tokens[pos-1].Type
		if immediate == tokeniser.TokenNewline || immediate == tokeniser.TokenSemicolon ||
			immediate == tokeniser.TokenOpenCurly || immediate == tokeniser.TokenCloseCurly {
			if insideDelimitedExpression(tokens, pos) {
				return shared.NewError(tok.Loc, "'when' cannot splice an element into a delimited expression")
			}
			continue
		}
		before := tokens[previous]
		if before.Type == tokeniser.TokenEquals || before.Type == tokeniser.TokenFatArrow ||
			before.Type == tokeniser.TokenKeyword &&
				(before.Value == string(tokeniser.KeywordType) || before.Value == string(tokeniser.KeywordReturn)) {
			continue
		}
		return shared.NewError(tok.Loc, "'when' must select a complete declaration, statement, expression, or @link item group")
	}
	return nil
}

func insideDelimitedExpression(tokens []tokeniser.Token, pos int) bool {
	parens, squares := 0, 0
	for index := pos - 1; index >= 0; index-- {
		switch tokens[index].Type {
		case tokeniser.TokenCloseParen:
			parens++
		case tokeniser.TokenOpenParen:
			if parens == 0 {
				return true
			}
			parens--
		case tokeniser.TokenCloseSquare:
			squares++
		case tokeniser.TokenOpenSquare:
			if squares == 0 {
				return true
			}
			squares--
		case tokeniser.TokenOpenCurly, tokeniser.TokenSemicolon:
			if parens == 0 && squares == 0 {
				return false
			}
		}
	}
	return false
}

func insideLinkAttribute(tokens []tokeniser.Token, pos int) bool {
	depth := 0
	for index := pos - 1; index >= 0; index-- {
		switch tokens[index].Type {
		case tokeniser.TokenCloseParen:
			depth++
		case tokeniser.TokenOpenParen:
			if depth != 0 {
				depth--
				continue
			}
			if index >= 2 && tokens[index-1].Type == tokeniser.TokenIdentifier && tokens[index-1].Value == "link" &&
				tokens[index-2].Type == tokeniser.TokenAt {
				return true
			}
		}
	}
	return false
}

func (e *expander) compilerError() error {
	directive := e.tokens[e.pos]
	directive.Loc = directive.Loc.WithEnd(e.tokens[e.pos+1].Loc)
	e.pos += 2
	if e.pos >= len(e.tokens) || e.tokens[e.pos].Type != tokeniser.TokenOpenParen {
		return shared.NewError(directive.Loc, "expected '(' after '@compiler_error'")
	}
	e.pos++
	for e.pos < len(e.tokens) && e.tokens[e.pos].Type == tokeniser.TokenNewline {
		e.pos++
	}
	if e.pos >= len(e.tokens) || e.tokens[e.pos].Type != tokeniser.TokenString {
		return shared.NewError(directive.Loc, "expected a string message in '@compiler_error'")
	}
	message := e.tokens[e.pos].Value
	e.pos++
	for e.pos < len(e.tokens) && e.tokens[e.pos].Type == tokeniser.TokenNewline {
		e.pos++
	}
	if e.pos >= len(e.tokens) || e.tokens[e.pos].Type != tokeniser.TokenCloseParen {
		return shared.NewError(directive.Loc, "expected ')' after '@compiler_error' message")
	}
	return shared.NewError(directive.Loc, "%s", message)
}

func (e *expander) compilerAssert() error {
	directive := e.tokens[e.pos]
	directive.Loc = directive.Loc.WithEnd(e.tokens[e.pos+1].Loc)
	e.pos += 2
	if e.pos >= len(e.tokens) || e.tokens[e.pos].Type != tokeniser.TokenOpenParen {
		return shared.NewError(directive.Loc, "expected '(' after '@compiler_assert'")
	}
	e.pos++
	conditionStart := e.pos
	parenDepth, squareDepth, curlyDepth := 0, 0, 0
	for e.pos < len(e.tokens) {
		tok := e.tokens[e.pos]
		switch tok.Type {
		case tokeniser.TokenOpenParen:
			parenDepth++
		case tokeniser.TokenCloseParen:
			if parenDepth == 0 && squareDepth == 0 && curlyDepth == 0 {
				return shared.NewError(directive.Loc, "expected ',' after '@compiler_assert' condition")
			}
			parenDepth--
		case tokeniser.TokenOpenSquare:
			squareDepth++
		case tokeniser.TokenCloseSquare:
			squareDepth--
		case tokeniser.TokenOpenCurly:
			curlyDepth++
		case tokeniser.TokenCloseCurly:
			curlyDepth--
		case tokeniser.TokenComma:
			if parenDepth == 0 && squareDepth == 0 && curlyDepth == 0 {
				conditionTokens := e.tokens[conditionStart:e.pos]
				condition, err := parseCondition(conditionTokens, directive.Loc)
				if err != nil {
					return err
				}
				value, err := evaluate(condition, e.target, nil)
				if err != nil {
					return err
				}
				if value.kind != valueBool {
					return shared.NewError(condition.GetLoc(), "compile-time assertion condition must be boolean")
				}

				e.pos++
				for e.pos < len(e.tokens) && e.tokens[e.pos].Type == tokeniser.TokenNewline {
					e.pos++
				}
				if e.pos >= len(e.tokens) || e.tokens[e.pos].Type != tokeniser.TokenString {
					return shared.NewError(directive.Loc, "expected a string message in '@compiler_assert'")
				}
				message := e.tokens[e.pos].Value
				e.pos++
				for e.pos < len(e.tokens) && e.tokens[e.pos].Type == tokeniser.TokenNewline {
					e.pos++
				}
				if e.pos >= len(e.tokens) || e.tokens[e.pos].Type != tokeniser.TokenCloseParen {
					return shared.NewError(directive.Loc, "expected ')' after '@compiler_assert' message")
				}
				e.pos++
				if !value.boolean {
					return shared.NewError(directive.Loc, "compiler assertion failed: %s", message)
				}
				return nil
			}
		case tokeniser.TokenEof:
			return shared.NewError(directive.Loc, "expected ',' after '@compiler_assert' condition")
		}
		if parenDepth < 0 || squareDepth < 0 || curlyDepth < 0 {
			return shared.NewError(tok.Loc, "unbalanced delimiter in compile-time assertion")
		}
		e.pos++
	}
	return shared.NewError(directive.Loc, "expected ',' after '@compiler_assert' condition")
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
	os                   string
	arch                 string
	environment          string
	pointerBits          int64
	cCharSigned          bool
	targetHasLibc        bool
	targetHasFilesystem  bool
	targetHasEnvironment bool
	targetHasProcessExit bool
	noLibc               bool
	noStdlib             bool
	releaseMode          ReleaseMode
	bindings             map[string]Value
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
		case "ReleaseMode":
			name := "Debug"
			if target.releaseMode == ReleaseModeRelease {
				name = "Release"
			}
			return Value{kind: valueEnum, domain: "ReleaseMode", name: name}, nil
		case "PointerBits":
			return Value{kind: valueInteger, integer: big.NewInt(target.pointerBits)}, nil
		case "CCharSigned":
			return Value{kind: valueBool, boolean: target.cCharSigned}, nil
		case "TargetHasLibc":
			return Value{kind: valueBool, boolean: target.targetHasLibc}, nil
		case "TargetHasFilesystem":
			return Value{kind: valueBool, boolean: target.targetHasFilesystem}, nil
		case "TargetHasEnvironment":
			return Value{kind: valueBool, boolean: target.targetHasEnvironment}, nil
		case "TargetHasProcessExit":
			return Value{kind: valueBool, boolean: target.targetHasProcessExit}, nil
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
		"ReleaseMode": {"Release", "Debug"},
	}
	return slices.Contains(values[domain], variant)
}

func targetFromTriple(triple string) targetValues {
	triple = qktarget.EffectiveTriple(triple)
	target := strings.ToLower(triple)
	archName := qktarget.Arch(triple)
	values := targetValues{os: "Unknown", arch: "Unknown", environment: "Unknown"}
	values.cCharSigned = qktarget.CCharSigned(triple)
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
	nativeOS := values.os == "Windows" || values.os == "Linux" || values.os == "MacOS" ||
		values.os == "FreeBSD" || values.os == "OpenBSD" || values.os == "NetBSD" || values.os == "DragonFly"
	values.targetHasLibc = nativeOS
	values.targetHasFilesystem = nativeOS
	values.targetHasEnvironment = nativeOS
	values.targetHasProcessExit = nativeOS && (values.os == "Windows" || values.arch == "X86_64" || values.arch == "AArch64")
	return values
}

func targetFromConfig(config Config) targetValues {
	values := targetFromTriple(config.TargetTriple)
	if values.os == "WASI" && config.Sysroot != "" {
		values.targetHasLibc = true
	}
	return values
}
