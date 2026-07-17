package parser

import (
	"errors"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/tokeniser"
)

func (p *Parser) ParseBlock() (*BlockNode, error) {
	beginLoc := p.CurrLoc()
	if !p.Expect(tokeniser.TokenOpenCurly) {
		return nil, shared.NewError(p.PrevLoc(), "expected '{' to start block")
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	var children []Node
	var parseErrors []error
	for !p.Match(tokeniser.TokenCloseCurly, tokeniser.TokenEof) {
		stmt, semiNeeded, err := p.ParseStatement()
		if err != nil {
			parseErrors = append(parseErrors, err)
			p.synchroniseStatement()
			continue
		}
		children = append(children, stmt)

		if p.Match(tokeniser.TokenCloseCurly) {
			break
		}

		if semiNeeded && !p.Match(tokeniser.TokenSemicolon, tokeniser.TokenNewline) {
			parseErrors = append(parseErrors, shared.NewError(p.CurrLoc(),
				"expected ';' or '\\n' to end statement"))
			p.synchroniseStatement()
			continue
		}

		for p.Match(tokeniser.TokenSemicolon, tokeniser.TokenNewline) {
			p.Inc()
		}
	}

	if !p.Expect(tokeniser.TokenCloseCurly) {
		return nil, shared.NewError(p.PrevLoc(), "expected '}' to close block")
	}

	return &BlockNode{
		Body: children,
		Loc:  beginLoc,
	}, errors.Join(parseErrors...)
}

func (p *Parser) synchroniseStatement() {
	// Failed speculative parses must not affect the next statement.
	p.posStack = nil
	if p.atStatementBoundary() && isStatementStart(p.Peek()) {
		return
	}
	depth := 0
	for !p.Match(tokeniser.TokenEof) {
		if depth == 0 && p.Match(tokeniser.TokenCloseCurly) {
			return
		}
		tok := p.Consume()
		switch tok.Type {
		case tokeniser.TokenOpenParen, tokeniser.TokenOpenSquare, tokeniser.TokenOpenCurly:
			depth++
		case tokeniser.TokenCloseParen, tokeniser.TokenCloseSquare, tokeniser.TokenCloseCurly:
			if depth > 0 {
				depth--
			}
		case tokeniser.TokenNewline, tokeniser.TokenSemicolon:
			if depth == 0 {
				return
			}
		}
	}
}

func (p *Parser) atStatementBoundary() bool {
	return p.pos == 0 || p.tokens[p.pos-1].Type == tokeniser.TokenNewline ||
		p.tokens[p.pos-1].Type == tokeniser.TokenSemicolon
}

func isStatementStart(tok tokeniser.Token) bool {
	if tok.Type == tokeniser.TokenIdentifier || tok.Type == tokeniser.TokenOpenCurly {
		return true
	}
	if tok.Type != tokeniser.TokenKeyword {
		return false
	}
	switch tok.Value {
	case string(tokeniser.KeywordLet),
		string(tokeniser.KeywordReturn), string(tokeniser.KeywordBreak),
		string(tokeniser.KeywordContinue), string(tokeniser.KeywordDefer),
		string(tokeniser.KeywordIf), string(tokeniser.KeywordFor):
		return true
	default:
		return false
	}
}

func (p *Parser) ParseFunctionDefinition() (*FunctionDefNode, error) {
	beginLoc := p.CurrLoc()

	if !p.Match(tokeniser.TokenKeyword) || p.Peek().Value != string(tokeniser.KeywordLet) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'let' keyword")
	}
	p.Inc()

	name, ok := p.ExpectGet(tokeniser.TokenIdentifier)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected function name")
	}

	if !p.Expect(tokeniser.TokenOpenParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected '('")
	}

	var args []*FunctionNodeArg
	variadic := false
	for !p.Match(tokeniser.TokenCloseParen) {
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}

		if p.Match(tokeniser.Token3Dots) {
			p.Inc()
			variadic = true
			break
		}

		mutable := false
		if p.Match(tokeniser.TokenKeyword) {
			kw := p.Consume().Value
			if kw == string(tokeniser.KeywordMut) {
				mutable = true
			} else {
				return nil, shared.NewError(p.CurrLoc(),
					"expected either 'mut' or argument name")
			}
		}

		arg, err := p.ParseIdent()
		if err != nil {
			return nil, err
		}

		if !p.Expect(tokeniser.TokenColon) {
			return nil, shared.NewError(p.PrevLoc(), "expected ':'")
		}

		argType, err := p.ParseType()
		if err != nil {
			return nil, err
		}

		args = append(args, &FunctionNodeArg{
			Name:    arg.Name,
			Type:    argType,
			Mutable: mutable,
		})

		if !p.Match(tokeniser.TokenComma) {
			break
		}

		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		p.Consume()
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	if !p.Expect(tokeniser.TokenCloseParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected ')'")
	}

	var retType TypeNode
	if p.Match(tokeniser.TokenColon) {
		p.Inc()
		argType, err := p.ParseType()
		if err != nil {
			return nil, err
		}
		retType = argType
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	expectsBody := true
	attrs, err := p.parseAttributes(name.Value)
	if err != nil {
		return nil, err
	}
	if attrs.Get(attributes.AttributeTypeForeign) != nil {
		expectsBody = false
	}

	var body Node

	if expectsBody {
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}

		if p.Match(tokeniser.TokenOpenCurly) {
			b, err := p.ParseBlock()
			if err != nil {
				return nil, err
			}
			body = b
		} else if p.Match(tokeniser.TokenEquals) {
			p.Inc() // consume '='

			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}

			b, err := p.ParseExpression()
			if err != nil {
				return nil, err
			}
			body = b
		} else {
			return nil, shared.NewError(p.PrevLoc(), "expected function body as either block or expression after '='")
		}
	}

	return &FunctionDefNode{
		Name:        name.Value,
		Args:        args,
		RetTypeNode: retType,
		Body:        body,
		Loc:         beginLoc,
		Attributes:  attrs,
		HasVariadic: variadic,
	}, nil
}

func (p *Parser) parseAttributes(defaultName string) (attributes.Attributes, error) {
	var attrs attributes.Attributes
	for {
		pos := p.pos
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		if !p.Match(tokeniser.TokenAt) {
			p.pos = pos
			break
		}

		attr, err := p.parseAttribute(defaultName)
		if err != nil {
			return nil, err
		}
		attrs = append(attrs, attr)
	}
	return attrs, nil
}

func (p *Parser) parseAttribute(defaultName string) (attributes.Attribute, error) {
	if !p.Expect(tokeniser.TokenAt) {
		return nil, shared.NewError(p.PrevLoc(), "expected '@' for attribute")
	}
	nameIdent, err := p.ParseIdent()
	if err != nil {
		return nil, err
	}

	switch attributes.AttributeType(nameIdent.Name) {
	case attributes.AttributeTypeLinks:
		return p.parseLinksAttribute()
	case attributes.AttributeTypeInline:
		return p.parseInlineAttribute()
	case attributes.AttributeTypeNoInline:
		return p.parseNoInlineAttribute()
	case attributes.AttributeTypeNoReturn:
		return p.parseNoReturnAttribute()
	case attributes.AttributeTypeForeign:
		return p.parseForeignAttribute(defaultName)
	case attributes.AttributeTypeExport:
		return p.parseExportAttribute(defaultName)
	default:
		return nil, shared.NewError(nameIdent.Loc, "unknown attribute: %s", nameIdent.Name)
	}
}

func (p *Parser) parseLinksAttribute() (attributes.Attribute, error) {
	if !p.Expect(tokeniser.TokenOpenParen) {
		return nil, shared.NewError(p.PrevLoc(), "expected '(' after @links")
	}

	links := []attributes.Link{}
	for {
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		if p.Match(tokeniser.TokenCloseParen) {
			if len(links) == 0 {
				return nil, shared.NewError(p.CurrLoc(), "@links requires at least one link")
			}
			p.Inc()
			break
		}

		kindTok, ok := p.ExpectGet(tokeniser.TokenIdentifier)
		if !ok {
			return nil, shared.NewError(p.PrevLoc(), "expected 'lib', 'path' or 'search' in @links")
		}
		var kind attributes.LinkKind
		switch kindTok.Value {
		case "lib":
			kind = attributes.LinkLibrary
		case "path":
			kind = attributes.LinkPath
		case "search":
			kind = attributes.LinkSearchPath
		default:
			return nil, shared.NewError(kindTok.Loc, "unknown @links entry kind %q; expected 'lib', 'path' or 'search'", kindTok.Value)
		}
		value, ok := p.ExpectGet(tokeniser.TokenString)
		if !ok {
			return nil, shared.NewError(p.PrevLoc(), "expected string after %s in @links", kindTok.Value)
		}
		if value.Value == "" {
			return nil, shared.NewError(value.Loc, "@links values cannot be empty")
		}
		links = append(links, attributes.Link{Kind: kind, Value: value.Value})

		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		if p.Match(tokeniser.TokenComma) {
			p.Inc()
			continue
		}
		if !p.Match(tokeniser.TokenCloseParen) {
			return nil, shared.NewError(p.CurrLoc(), "expected ',' or ')' in @links")
		}
	}

	return attributes.ModuleAttributeLinks{Links: links}, nil
}

func (p *Parser) parseInlineAttribute() (attributes.Attribute, error) {
	if p.Match(tokeniser.TokenOpenParen) {
		p.Inc()
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		if !p.Expect(tokeniser.TokenCloseParen) {
			return nil, shared.NewError(p.PrevLoc(), "inline attribute does not take any arguments")
		}
	}
	return attributes.AttributeInline{}, nil
}

func (p *Parser) parseNoInlineAttribute() (attributes.Attribute, error) {
	if p.Match(tokeniser.TokenOpenParen) {
		p.Inc()
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		if !p.Expect(tokeniser.TokenCloseParen) {
			return nil, shared.NewError(p.PrevLoc(), "noinline attribute does not take any arguments")
		}
	}
	return attributes.AttributeNoInline{}, nil
}

func (p *Parser) parseNoReturnAttribute() (attributes.Attribute, error) {
	if p.Match(tokeniser.TokenOpenParen) {
		p.Inc()
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
		if !p.Expect(tokeniser.TokenCloseParen) {
			return nil, shared.NewError(p.PrevLoc(), "noreturn attribute does not take any arguments")
		}
	}
	return attributes.AttributeNoReturn{}, nil
}

func (p *Parser) parseForeignAttribute(defaultName string) (attributes.Attribute, error) {
	if !p.Match(tokeniser.TokenOpenParen) {
		return attributes.FunctionAttributeForeign{From: defaultName}, nil
	}
	p.Inc()
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	if p.Match(tokeniser.TokenCloseParen) {
		p.Inc()
		return attributes.FunctionAttributeForeign{From: defaultName}, nil
	}
	name, ok := p.ExpectGet(tokeniser.TokenString)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "foreign attribute argument must be a string literal")
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	if !p.Expect(tokeniser.TokenCloseParen) {
		return nil, shared.NewError(p.PrevLoc(), "foreign attribute takes at most one string argument")
	}
	return attributes.FunctionAttributeForeign{From: name.Value}, nil
}

func (p *Parser) parseExportAttribute(defaultName string) (attributes.Attribute, error) {
	if !p.Match(tokeniser.TokenOpenParen) {
		return attributes.FunctionAttributeExport{As: defaultName}, nil
	}
	p.Inc()
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	if p.Match(tokeniser.TokenCloseParen) {
		p.Inc()
		return attributes.FunctionAttributeExport{As: defaultName}, nil
	}
	name, ok := p.ExpectGet(tokeniser.TokenString)
	if !ok || name.Value == "" {
		return nil, shared.NewError(p.PrevLoc(), "export attribute argument must be a non-empty string")
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}
	if !p.Expect(tokeniser.TokenCloseParen) {
		return nil, shared.NewError(p.PrevLoc(), "export attribute takes at most one string argument")
	}
	return attributes.FunctionAttributeExport{As: name.Value}, nil
}

func (p *Parser) ParseTypeAlias() (*TypeAliasNode, error) {
	beginLoc := p.CurrLoc()
	if !p.Match(tokeniser.TokenKeyword) || p.Peek().Value != string(tokeniser.KeywordLet) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'let' keyword")
	}
	p.Inc()

	name, ok := p.ExpectGet(tokeniser.TokenIdentifier)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected type alias name")
	}

	if !p.Expect(tokeniser.TokenEquals) {
		return nil, shared.NewError(p.PrevLoc(), "expected '='")
	}

	if !p.Match(tokeniser.TokenKeyword) || p.Peek().Value != string(tokeniser.KeywordType) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'type' keyword")
	}
	p.Inc()
	transparent := false
	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordAlias) {
		transparent = true
		p.Inc()
	}

	tpe, err := p.ParseType()
	if err != nil {
		return nil, err
	}

	return &TypeAliasNode{
		Name: name.Value,
		Type: tpe, Transparent: transparent,
		Loc: beginLoc,
	}, nil
}

func (p *Parser) ParseImport() (*ImportNode, error) {
	beginLoc := p.CurrLoc()
	if kw, ok := p.ExpectGet(tokeniser.TokenKeyword); !ok ||
		kw.Value != string(tokeniser.KeywordImport) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'import' keyword")
	}

	modules := []string{}
	aliases := []string{}

	if p.Match(tokeniser.TokenOpenParen) {
		p.Inc()

		for {
			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}

			if p.Match(tokeniser.TokenCloseParen) {
				p.Inc()
				break
			}

			name, ok := p.ExpectGet(tokeniser.TokenIdentifier)
			if !ok {
				return nil, shared.NewError(p.PrevLoc(), "expected module name")
			}
			modules = append(modules, name.Value)
			alias := name.Value
			if p.Match(tokeniser.TokenIdentifier) {
				alias = p.Consume().Value
			}
			aliases = append(aliases, alias)

			if p.Match(tokeniser.TokenComma) {
				p.Inc()

				for p.Match(tokeniser.TokenNewline) {
					p.Inc()
				}
				continue
			}

			if p.Match(tokeniser.TokenNewline) {
				p.Inc()
				continue
			}

			if p.Match(tokeniser.TokenCloseParen) {
				p.Inc()
				break
			}

			return nil, shared.NewError(p.PrevLoc(), "expected ',', newline, or ')'")
		}
	} else {
		name, ok := p.ExpectGet(tokeniser.TokenIdentifier)
		if !ok {
			return nil, shared.NewError(p.PrevLoc(), "expected module name")
		}
		modules = append(modules, name.Value)
		alias := name.Value
		if p.Match(tokeniser.TokenIdentifier) {
			alias = p.Consume().Value
		}
		aliases = append(aliases, alias)
	}

	return &ImportNode{
		Modules: modules,
		Aliases: aliases,
		Loc:     beginLoc,
	}, nil
}

func (p *Parser) ParseModule() (*ModuleNode, error) {
	beginLoc := p.CurrLoc()
	if kw, ok := p.ExpectGet(tokeniser.TokenKeyword); !ok ||
		kw.Value != string(tokeniser.KeywordModule) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'module' keyword")
	}

	nameTok, ok := p.ExpectGet(tokeniser.TokenIdentifier)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected module name")
	}
	name := nameTok.Value
	attrs, err := p.parseAttributes(name)
	if err != nil {
		return nil, err
	}

	return &ModuleNode{
		Name:       name,
		Attributes: attrs,
		Loc:        beginLoc,
	}, nil
}

func (p *Parser) ParseStatement() (Node, bool, error) {
	if p.Match(tokeniser.TokenKeyword) {
		kw := p.Peek().Value
		switch kw {
		case string(tokeniser.KeywordLet):
			p.PushPos()
			p.Inc() // consume `let`

			if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordMut) {
				p.PopPos()
				node, err := p.ParseDeclaration()
				return node, true, err
			}

			if !p.Expect(tokeniser.TokenIdentifier) {
				return nil, false, shared.NewError(p.PrevLoc(), "expected name")
			}

			switch {
			case p.Match(tokeniser.TokenOpenParen):
				// let fn(...) = ...
				p.PopPos()
				node, err := p.ParseFunctionDefinition()
				return node, true, err

			case p.Match(tokeniser.TokenColon):
				// let x: T = ...
				p.PopPos()
				node, err := p.ParseDeclaration()
				return node, true, err

			case p.Match(tokeniser.TokenEquals):
				p.Inc() // look past '='

				if p.Match(tokeniser.TokenKeyword) &&
					p.Peek().Value == string(tokeniser.KeywordType) {

					// let A = type ...
					p.PopPos()
					node, err := p.ParseTypeAlias()
					return node, true, err
				}

				// let x = ...
				p.PopPos()
				node, err := p.ParseDeclaration()
				return node, true, err
			}

			return nil, false, shared.NewError(p.PrevLoc(), "invalid let statement")
		case
			string(tokeniser.KeywordReturn),
			string(tokeniser.KeywordBreak),
			string(tokeniser.KeywordContinue):
			node, err := p.ParseControlKeyword()
			return node, true, err
		case string(tokeniser.KeywordDefer):
			node, err := p.ParseDefer()
			return node, true, err
		case string(tokeniser.KeywordIf):
			node, err := p.ParseIfStatement()
			return node, false, err
		case string(tokeniser.KeywordFor):
			node, err := p.ParseForLoop()
			return node, false, err
		default:
			return nil, false, shared.NewError(p.CurrLoc(), "unexpected keyword: %s", kw)
		}
	}

	if p.Match(tokeniser.TokenOpenCurly) {
		node, err := p.ParseBlock()
		return node, true, err
	}

	expr, err := p.ParseExpression()
	if err != nil {
		return nil, false, err
	}

	if p.Match(tokeniser.TokenEquals) {
		p.Inc()
		parsed, err := p.ParseAssignment(expr)
		if err != nil {
			return nil, false, err
		}
		return parsed, true, nil
	}

	if p.Match(tokeniser.TokenIncBy, tokeniser.TokenDecBy, tokeniser.TokenMulBy, tokeniser.TokenDivBy, tokeniser.TokenModBy) {
		parsed, err := p.ParseCompoundAssignment(p.Consume(), expr)
		if err != nil {
			return nil, false, err
		}
		return parsed, true, nil
	}

	switch expr.(type) {
	case *FunctionCallNode:
		return expr, true, nil
	default:
		return nil, false, shared.NewError(p.CurrLoc(), "expected a valid statement")
	}
}

func (p *Parser) ParseDefer() (*DeferNode, error) {
	loc := p.CurrLoc()
	kw, ok := p.ExpectGet(tokeniser.TokenKeyword)
	if !ok || kw.Value != string(tokeniser.KeywordDefer) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'defer'")
	}
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	var action Node
	if p.Match(tokeniser.TokenOpenCurly) {
		block, err := p.ParseBlock()
		if err != nil {
			return nil, err
		}
		action = block
	} else {
		expr, err := p.ParseExpression()
		if err != nil {
			return nil, err
		}
		action = expr
	}
	return &DeferNode{Action: action, Loc: loc}, nil
}

func (p *Parser) ParseDeclaration() (*DeclarationNode, error) {
	beginLoc := p.CurrLoc()

	kw, ok := p.ExpectGet(tokeniser.TokenKeyword)
	if !ok || kw.Value != string(tokeniser.KeywordLet) {
		return nil, shared.NewError(p.PrevLoc(), "expected `let` keyword")
	}

	mutable := false
	if p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordMut) {
		p.Inc()
		mutable = true
	}

	ident, ok := p.ExpectGet(tokeniser.TokenIdentifier)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected name")
	}

	var tpe TypeNode
	if p.Match(tokeniser.TokenColon) {
		p.Inc()

		t, err := p.ParseType()
		if err != nil {
			return nil, err
		}
		tpe = t
	}

	var value ExpressionNode
	var attrs attributes.Attributes
	if p.Match(tokeniser.TokenEquals) {
		p.Inc()
		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}

		expr, err := p.ParseExpression()
		if err != nil {
			return nil, err
		}
		value = expr
	}

	attrs, err := p.parseAttributes(ident.Value)
	if err != nil {
		return nil, err
	}

	if value == nil && len(attrs) == 0 {
		return nil, shared.NewError(p.PrevLoc(), "expected '=' or declaration attribute")
	}
	if value == nil && attrs.Get(attributes.AttributeTypeForeign) != nil && tpe == nil {
		return nil, shared.NewError(ident.Loc, "external declaration requires a type annotation")
	}

	return &DeclarationNode{
		Name:       ident.Value,
		TypeNode:   tpe,
		Mutable:    mutable,
		Value:      value,
		Attributes: attrs,
		Loc:        beginLoc,
	}, nil
}

func (p *Parser) ParseAssignment(subj ExpressionNode) (*AssignmentNode, error) {
	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	expr, err := p.ParseExpression()
	if err != nil {
		return nil, err
	}

	return &AssignmentNode{
		Assignee: subj,
		Value:    expr,
		Loc:      subj.GetLoc(),
	}, err
}

func (p *Parser) ParseCompoundAssignment(opTok tokeniser.Token, subj ExpressionNode) (*AssignmentNode, error) {
	var op BinaryOpKind

	switch opTok.Type {
	case tokeniser.TokenIncBy:
		op = BinaryOpAdd
	case tokeniser.TokenDecBy:
		op = BinaryOpSubtract
	case tokeniser.TokenMulBy:
		op = BinaryOpMultiply
	case tokeniser.TokenDivBy:
		op = BinaryOpDivide
	case tokeniser.TokenModBy:
		op = BinaryOpModulo
	default:
		return nil, shared.NewError(opTok.Loc, "unexpected compound assignment operator %s", opTok)
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	expr, err := p.ParseExpression()
	if err != nil {
		return nil, err
	}

	return &AssignmentNode{
		Assignee: subj,
		Value: &BinaryOpNode{
			Op:       op,
			Operand1: subj,
			Operand2: expr,
			Loc:      expr.GetLoc(),
		},
		Loc: subj.GetLoc(),
	}, nil
}

func (p *Parser) ParseIfStatement() (*IfNode, error) {
	beginLoc := p.CurrLoc()
	if !p.Expect(tokeniser.TokenKeyword) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'if'")
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	wasDisambiguatingTrailingBlock := p.disambiguateTrailingBlock
	p.disambiguateTrailingBlock = true
	condition, err := p.ParseExpression()
	p.disambiguateTrailingBlock = wasDisambiguatingTrailingBlock
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	thenBlock, err := p.ParseBlock()
	if err != nil {
		return nil, err
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	node := &IfNode{
		Loc: beginLoc,
		IfBranch: IfBranch{
			Condition: condition,
			Node:      thenBlock,
		},
	}

	for p.Match(tokeniser.TokenKeyword) && p.Peek().Value == string(tokeniser.KeywordElse) {
		p.Consume()

		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}

		if p.Match(tokeniser.TokenKeyword) &&
			p.Peek().Value == string(tokeniser.KeywordIf) {
			p.Consume()

			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}

			wasDisambiguatingTrailingBlock := p.disambiguateTrailingBlock
			p.disambiguateTrailingBlock = true
			elseifCondition, err := p.ParseExpression()
			p.disambiguateTrailingBlock = wasDisambiguatingTrailingBlock
			if err != nil {
				return nil, err
			}

			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}

			elseifBlock, err := p.ParseBlock()
			if err != nil {
				return nil, err
			}

			elseIfBranch := IfBranch{
				Condition: elseifCondition,
				Node:      elseifBlock,
			}
			node.ElseIfBranches = append(node.ElseIfBranches, elseIfBranch)

			for p.Match(tokeniser.TokenNewline) {
				p.Inc()
			}
		} else {
			elseBlock, err := p.ParseBlock()
			if err != nil {
				return nil, err
			}

			node.ElseBranch = elseBlock
			break
		}
	}

	return node, nil
}

func (p *Parser) ParseControlKeyword() (*ControlKeywordNode, error) {
	loc := p.CurrLoc()
	kw, ok := p.ExpectGet(tokeniser.TokenKeyword)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected 'return', 'break' or 'continue'")
	}
	if kw.Value != string(tokeniser.KeywordReturn) &&
		kw.Value != string(tokeniser.KeywordBreak) &&
		kw.Value != string(tokeniser.KeywordContinue) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'return', 'break' or 'continue'")
	}

	var expr ExpressionNode
	if kw.Value == string(tokeniser.KeywordReturn) && !p.Match(tokeniser.TokenNewline, tokeniser.TokenSemicolon) {
		got, err := p.ParseExpression()
		if err != nil {
			return nil, err
		}
		expr = got
	}

	return &ControlKeywordNode{
		Keyword:     tokeniser.KeywordKind(kw.Value),
		ReturnValue: expr,
		Loc:         loc,
	}, nil
}

func (p *Parser) ParseForLoop() (Node, error) {
	beginLoc := p.CurrLoc()
	if !p.Expect(tokeniser.TokenKeyword) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'for'")
	}

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	if p.Match(tokeniser.TokenIdentifier) &&
		p.Next().Type == tokeniser.TokenKeyword &&
		p.Next().Value == string(tokeniser.KeywordIn) {
		return p.parseRangeOrForEach(beginLoc)
	}

	var err error

	exprsOrStmts := []Node{}
	wasDisambiguatingTrailingBlock := p.disambiguateTrailingBlock
	p.disambiguateTrailingBlock = true

	for !p.Match(tokeniser.TokenOpenCurly) {
		ogPos := p.pos
		exOrSt, _, err := p.ParseStatement()
		if err != nil {
			p.pos = ogPos
			exOrSt, err = p.ParseExpression()
			if err != nil {
				return nil, err
			}
		}

		exprsOrStmts = append(exprsOrStmts, exOrSt)

		if p.Match(tokeniser.TokenOpenCurly) {
			break
		}

		if !p.Expect(tokeniser.TokenSemicolon) {
			return nil, shared.NewError(p.PrevLoc(),
				"expected ';' to end statement or expression")
		}

		for p.Match(tokeniser.TokenNewline) {
			p.Inc()
		}
	}
	p.disambiguateTrailingBlock = wasDisambiguatingTrailingBlock

	for p.Match(tokeniser.TokenNewline) {
		p.Inc()
	}

	body, err := p.ParseBlock()
	if err != nil {
		return nil, err
	}

	return &ForNode{
		ExprsOrStmts: exprsOrStmts,
		Body:         body,
		Loc:          beginLoc,
	}, nil
}

func (p *Parser) parseRangeOrForEach(beginLoc shared.Location) (Node, error) {
	name, ok := p.ExpectGet(tokeniser.TokenIdentifier)
	if !ok {
		return nil, shared.NewError(p.PrevLoc(), "expected loop variable name")
	}
	if kw, ok := p.ExpectGet(tokeniser.TokenKeyword); !ok || kw.Value != string(tokeniser.KeywordIn) {
		return nil, shared.NewError(p.PrevLoc(), "expected 'in' in for loop")
	}

	wasDisambiguatingTrailingBlock := p.disambiguateTrailingBlock
	p.disambiguateTrailingBlock = true
	iterable, err := p.ParseExpression()
	p.disambiguateTrailingBlock = wasDisambiguatingTrailingBlock
	if err != nil {
		return nil, err
	}

	if p.Match(tokeniser.Token2Dots) {
		p.Inc()
		inclusive := p.Match(tokeniser.TokenEquals)
		if inclusive {
			p.Inc()
		}

		// Suppress struct-literal parsing for the range bound as well: the
		// following loop body starts with '{', which would otherwise be consumed
		// as a struct literal after an identifier bound.
		wasDisambiguatingTrailingBlock := p.disambiguateTrailingBlock
		p.disambiguateTrailingBlock = true
		end, err := p.ParseExpression()
		p.disambiguateTrailingBlock = wasDisambiguatingTrailingBlock
		if err != nil {
			return nil, err
		}
		body, err := p.ParseBlock()
		if err != nil {
			return nil, err
		}
		return &RangeForNode{
			Name:      name.Value,
			Start:     iterable,
			End:       end,
			Inclusive: inclusive,
			Body:      body,
			Loc:       beginLoc,
		}, nil
	}

	body, err := p.ParseBlock()
	if err != nil {
		return nil, err
	}
	return &ForEachNode{
		Name:     name.Value,
		Iterable: iterable,
		Body:     body,
		Loc:      beginLoc,
	}, nil
}
