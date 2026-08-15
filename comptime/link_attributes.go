package comptime

import (
	"github.com/marzeq/qk/shared"
	"github.com/marzeq/qk/tokeniser"
)

// validateLinkAttributes treats @link as a compile-time collection of complete
// link specifications. Every conditional branch is checked, including branches
// that are not selected for the current target.
func validateLinkAttributes(tokens []tokeniser.Token) error {
	for pos := 0; pos+2 < len(tokens); pos++ {
		if tokens[pos].Type != tokeniser.TokenAt || tokens[pos+1].Type != tokeniser.TokenIdentifier ||
			tokens[pos+1].Value != "link" {
			continue
		}
		validator := linkAttributeValidator{tokens: tokens, pos: pos + 2}
		if err := validator.parseAttribute(); err != nil {
			return err
		}
		pos = validator.pos - 1
	}
	return nil
}

type linkAttributeValidator struct {
	tokens []tokeniser.Token
	pos    int
}

func (v *linkAttributeValidator) parseAttribute() error {
	if !v.match(tokeniser.TokenOpenParen) {
		return shared.NewError(v.current().Loc, "expected '(' after @link")
	}
	v.pos++
	if err := v.parseEntries(tokeniser.TokenCloseParen); err != nil {
		return err
	}
	v.pos++
	return nil
}

func (v *linkAttributeValidator) parseEntries(close tokeniser.TokenKind) error {
	for {
		v.skipSeparators()
		if v.match(close) {
			return nil
		}
		if v.match(tokeniser.TokenEof) {
			return shared.NewError(v.current().Loc, "expected closing delimiter in @link")
		}
		if v.keyword(tokeniser.KeywordWhen) {
			if err := v.parseWhen(); err != nil {
				return err
			}
			continue
		}
		if v.match(tokeniser.TokenAt) {
			if err := v.parseCompilerError(); err != nil {
				return err
			}
			continue
		}
		if err := v.parseLink(); err != nil {
			return err
		}
	}
}

func (v *linkAttributeValidator) parseCompilerError() error {
	start := v.current()
	v.pos++
	if v.current().Type != tokeniser.TokenIdentifier || v.current().Value != "compiler_error" {
		return shared.NewError(start.Loc, "only @compiler_error is valid as an @link collection directive")
	}
	v.pos++
	if !v.match(tokeniser.TokenOpenParen) {
		return shared.NewError(v.current().Loc, "expected '(' after @compiler_error")
	}
	v.pos++
	if !v.match(tokeniser.TokenString) {
		return shared.NewError(v.current().Loc, "expected string message in @compiler_error")
	}
	v.pos++
	if !v.match(tokeniser.TokenCloseParen) {
		return shared.NewError(v.current().Loc, "expected ')' after @compiler_error message")
	}
	v.pos++
	return nil
}

func (v *linkAttributeValidator) parseLink() error {
	kind := v.current()
	if kind.Type != tokeniser.TokenIdentifier {
		return shared.NewError(kind.Loc, "expected complete link entry or 'when' in @link")
	}
	switch kind.Value {
	case "system", "path", "search", "framework":
	default:
		return shared.NewError(kind.Loc, "unknown @link entry kind %q", kind.Value)
	}
	v.pos++
	if !v.match(tokeniser.TokenString) {
		return shared.NewError(v.current().Loc, "expected string after %s in @link", kind.Value)
	}
	if v.current().Value == "" {
		return shared.NewError(v.current().Loc, "@link values cannot be empty")
	}
	v.pos++
	if !v.match(tokeniser.TokenComma, tokeniser.TokenNewline, tokeniser.TokenCloseParen, tokeniser.TokenCloseCurly) {
		return shared.NewError(v.current().Loc, "expected separator after complete @link entry")
	}
	return nil
}

func (v *linkAttributeValidator) parseWhen() error {
	when := v.current()
	v.pos++
	conditionStart := v.pos
	parenDepth, squareDepth := 0, 0
	for !v.match(tokeniser.TokenEof) {
		switch v.current().Type {
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
				if v.pos == conditionStart {
					return shared.NewError(when.Loc, "expected condition after 'when' in @link")
				}
				v.pos++
				if err := v.parseEntries(tokeniser.TokenCloseCurly); err != nil {
					return err
				}
				v.pos++
				return v.parseElse()
			}
		}
		if parenDepth < 0 || squareDepth < 0 {
			return shared.NewError(v.current().Loc, "unbalanced condition in @link")
		}
		v.pos++
	}
	return shared.NewError(when.Loc, "expected '{' after @link condition")
}

func (v *linkAttributeValidator) parseElse() error {
	saved := v.pos
	for v.match(tokeniser.TokenNewline) {
		v.pos++
	}
	if !v.keyword(tokeniser.KeywordElse) {
		v.pos = saved
		return nil
	}
	v.pos++
	for v.match(tokeniser.TokenNewline) {
		v.pos++
	}
	if v.keyword(tokeniser.KeywordWhen) {
		return v.parseWhen()
	}
	if !v.match(tokeniser.TokenOpenCurly) {
		return shared.NewError(v.current().Loc, "expected 'when' or '{' after 'else' in @link")
	}
	v.pos++
	if err := v.parseEntries(tokeniser.TokenCloseCurly); err != nil {
		return err
	}
	v.pos++
	return nil
}

func (v *linkAttributeValidator) skipSeparators() {
	for v.match(tokeniser.TokenNewline, tokeniser.TokenComma) {
		v.pos++
	}
}

func (v *linkAttributeValidator) match(kinds ...tokeniser.TokenKind) bool {
	current := v.current().Type
	for _, kind := range kinds {
		if current == kind {
			return true
		}
	}
	return false
}

func (v *linkAttributeValidator) keyword(keyword tokeniser.KeywordKind) bool {
	return v.current().Type == tokeniser.TokenKeyword && v.current().Value == string(keyword)
}

func (v *linkAttributeValidator) current() tokeniser.Token {
	if v.pos >= len(v.tokens) {
		return tokeniser.Token{Type: tokeniser.TokenEof}
	}
	return v.tokens[v.pos]
}
