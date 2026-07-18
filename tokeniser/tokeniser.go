package tokeniser

import (
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/marzeq/qk/shared"
)

type Tokeniser struct {
	pos        int
	line       int
	col        int
	text       []rune
	fileOrigin string
	tokens     []Token
}

func NewTokeniserFromFile(path string) (*Tokeniser, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file '%s'", path)
	}

	return &Tokeniser{
		pos:        0,
		line:       1,
		col:        1,
		fileOrigin: path,
		text:       []rune(string(text)),
	}, nil
}

func (t *Tokeniser) Peek() rune {
	if t.pos >= len(t.text) || t.pos < 0 {
		return 0
	}

	return t.text[t.pos]
}

func (t *Tokeniser) Next() rune {
	pos := t.pos + 1

	if pos >= len(t.text) || pos < 0 {
		return 0
	}

	return t.text[pos]
}

func (t *Tokeniser) Inc() *Tokeniser {
	if t.Next() == '\n' {
		t.line++
		t.col = 1
	} else if t.Peek() != '\r' {
		t.col++
	}

	t.pos++

	return t
}

func (t *Tokeniser) Consume() rune {
	c := t.Peek()
	t.Inc()

	return c
}

func IsAlpha(c rune) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func IsNum(c rune) bool {
	return (c >= '0' && c <= '9')
}

func IsLegalWordStart(c rune) bool {
	return IsAlpha(c) || c == '_'
}

func IsLegalWordChar(c rune) bool {
	return IsLegalWordStart(c) || IsNum(c)
}

func IsSpace(c rune) bool {
	return c == ' ' || c == '\t' || c == '\r'
}

func (t *Tokeniser) ReadWord() string {
	var s strings.Builder

	for IsLegalWordChar(t.Peek()) {
		s.WriteString(string(t.Consume()))
	}

	return s.String()
}

func (t *Tokeniser) ReadNumber() (string, error) {
	negative := false

	if t.Peek() == '-' {
		t.Inc()
		negative = true
	}

	base := 10
	prefixed := false
	if t.Peek() == '0' {
		switch t.Next() {
		case 'b', 'B':
			base, prefixed = 2, true
		case 'o', 'O':
			base, prefixed = 8, true
		case 'x', 'X':
			base, prefixed = 16, true
		}
		if prefixed {
			t.Inc().Inc()
		}
	}

	var digits strings.Builder
	for !IsSpace(t.Peek()) && t.Peek() != '\n' {
		c := t.Peek()
		if !IsAlpha(c) && !IsNum(c) {
			break
		}
		if digitValue(c) >= base {
			return "", shared.NewError(t.GetLoc(), "invalid digit %q for base-%d integer literal", c, base)
		}
		digits.WriteRune(t.Consume())
	}

	if digits.Len() == 0 {
		return "", shared.NewError(t.GetLoc(), "expected digits in base-%d integer literal", base)
	}
	if prefixed && t.Peek() == '.' && t.Next() != '.' {
		return "", shared.NewError(t.GetLoc(), "base-%d floating-point literals are not supported", base)
	}

	value := digits.String()
	if prefixed {
		integer, ok := new(big.Int).SetString(value, base)
		if !ok {
			return "", shared.NewError(t.GetLoc(), "invalid base-%d integer literal", base)
		}
		value = integer.String()
	}
	if negative {
		value = "-" + value
	}
	return value, nil
}

func digitValue(c rune) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	default:
		return 36
	}
}

func (t *Tokeniser) HanldeEscape() (string, error) {
	c := t.Consume()
	if c != '\\' {
		return string(c), nil
	} else {
		switch t.Consume() {
		case '\\':
			return "\\", nil
		case '"':
			return "\"", nil
		case 'n':
			return "\n", nil
		case 'r':
			return "\r", nil
		case 't':
			return "\t", nil
		case 'b':
			return "\b", nil
		case 'f':
			return "\f", nil
		case 'v':
			return "\v", nil
		case 'a':
			return "\a", nil
		case '0':
			return string(rune(0)), nil
		default:
			return "", shared.NewError(t.GetLoc(), "invalid escape sequence")
		}
	}
}

func (t *Tokeniser) ReadString() (string, error) {
	var s strings.Builder

	if t.Peek() != '"' {
		return "", shared.NewError(t.GetLoc(), "expected '\"' to start a string")
	}
	t.Inc()

	for t.Peek() != '"' && t.Peek() != '\n' {
		ch, err := t.HanldeEscape()
		if err != nil {
			return "", err
		}
		s.WriteString(ch)
	}

	if t.Peek() != '"' {
		return "", shared.NewError(t.GetLoc(), "expected '\"' to end a string")
	}
	t.Inc()

	return s.String(), nil
}

func (t *Tokeniser) IgnoreComment() {
	t.Inc().Inc()

	for t.Peek() != '\n' {
		t.Inc()
	}
}

func (t *Tokeniser) IgnoreMultilineComment() {
	t.Inc().Inc()

	for {
		if t.Peek() == '*' && t.Next() == '/' {
			t.Inc().Inc()
			return
		}
		if t.Peek() == 0 {
			return
		}
		t.Inc()
	}
}

func (t *Tokeniser) GetLoc() shared.Location {
	return shared.Location{
		FilePath: t.fileOrigin,
		LC: shared.LineCol{
			Line: t.line,
			Col:  t.col,
		},
	}
}

func (t *Tokeniser) AddToken(ttype TokenKind, loc shared.Location, _value ...string) {
	value := ""
	if len(_value) > 0 {
		value = _value[0]
	}

	t.tokens = append(t.tokens, Token{Type: ttype, Value: value, Loc: loc})
}

var keywords = map[string]struct{}{
	string(KeywordLet):           {},
	string(KeywordMut):           {},
	string(KeywordStruct):        {},
	string(KeywordEnum):          {},
	string(KeywordUnion):         {},
	string(KeywordOpaque):        {},
	string(KeywordType):          {},
	string(KeywordAlias):         {},
	string(KeywordIf):            {},
	string(KeywordWhen):          {},
	string(KeywordCompilerError): {},
	string(KeywordElse):          {},
	string(KeywordGiven):         {},
	string(KeywordFor):           {},
	string(KeywordBreak):         {},
	string(KeywordContinue):      {},
	string(KeywordReturn):        {},
	string(KeywordDefer):         {},
	string(KeywordImport):        {},
	string(KeywordModule):        {},
	string(KeywordPub):           {},
	string(KeywordAnd):           {},
	string(KeywordOr):            {},
	string(KeywordNot):           {},
	string(KeywordTrue):          {},
	string(KeywordFalse):         {},
	string(KeywordNil):           {},
	string(KeywordAs):            {},
	string(KeywordSizeof):        {},
	string(KeywordAlignof):       {},
	string(KeywordOffsetof):      {},
	string(KeywordLen):           {},
	string(KeywordIn):            {},
}

func IsKeyword(w string) bool {
	_, ok := keywords[w]
	return ok
}

func (t *Tokeniser) Tokenise() ([]Token, error) {
	for {
		c := t.Peek()

		if c == 0 {
			t.AddToken(TokenEof, t.GetLoc())
			return t.tokens, nil
		}

		if IsSpace(c) {
			t.Inc()
			continue
		}

		if c == 'c' && t.Next() == '"' {
			pos := t.GetLoc()
			t.Inc()
			s, err := t.ReadString()
			if err != nil {
				return nil, err
			}
			t.AddToken(TokenCString, pos, s)
			continue
		}

		if IsLegalWordStart(c) {
			pos := t.GetLoc()
			w := t.ReadWord()

			if IsKeyword(w) {
				t.AddToken(TokenKeyword, pos, w)
			} else {
				t.AddToken(TokenIdentifier, pos, w)
			}
			continue
		}

		if IsNum(c) {
			if len(t.tokens) > 0 && t.tokens[len(t.tokens)-1].Type == TokenDot && c == '0' {
				switch t.Next() {
				case 'b', 'B', 'o', 'O', 'x', 'X':
					return nil, shared.NewError(t.GetLoc(), "prefixed integer literal cannot be used as a decimal fraction")
				}
			}
			pos := t.GetLoc()
			n, err := t.ReadNumber()
			if err != nil {
				return nil, err
			}

			t.AddToken(TokenNumber, pos, n)
			continue
		}

		switch c {
		case '"':
			pos := t.GetLoc()
			s, err := t.ReadString()
			if err != nil {
				return nil, err
			}

			t.AddToken(TokenString, pos, s)
			continue
		case '\\':
			if t.Next() == '\n' {
				t.Inc().Inc()
			}
			continue
		case '\n':
			t.AddToken(TokenNewline, t.GetLoc())
			t.Inc()
			continue
		case '(':
			t.AddToken(TokenOpenParen, t.GetLoc())
			t.Inc()
			continue
		case ')':
			t.AddToken(TokenCloseParen, t.GetLoc())
			t.Inc()
			continue
		case '{':
			t.AddToken(TokenOpenCurly, t.GetLoc())
			t.Inc()
			continue
		case '}':
			t.AddToken(TokenCloseCurly, t.GetLoc())
			t.Inc()
			continue
		case '[':
			t.AddToken(TokenOpenSquare, t.GetLoc())
			t.Inc()
			continue
		case ']':
			t.AddToken(TokenCloseSquare, t.GetLoc())
			t.Inc()
			continue
		case '=':
			if t.Next() == '=' {
				t.AddToken(TokenEqualsEquals, t.GetLoc())
				t.Inc().Inc()
			} else {
				t.AddToken(TokenEquals, t.GetLoc())
				t.Inc()
			}
			continue
		case '!':
			if t.Next() == '=' {
				t.AddToken(TokenNotEquals, t.GetLoc())
				t.Inc().Inc()
			} else {
				t.AddToken(TokenExclam, t.GetLoc())
				t.Inc()
			}
			continue
		case '<':
			if t.Next() == '<' && t.pos+2 < len(t.text) && t.text[t.pos+2] == '=' {
				t.AddToken(TokenShiftLeftBy, t.GetLoc())
				t.Inc().Inc().Inc()
			} else if t.Next() == '<' {
				t.AddToken(TokenShiftLeft, t.GetLoc())
				t.Inc().Inc()
			} else if t.Next() == '=' {
				t.AddToken(TokenLessEquals, t.GetLoc())
				t.Inc().Inc()
			} else {
				t.AddToken(TokenLess, t.GetLoc())
				t.Inc()
			}
			continue
		case '>':
			if t.Next() == '>' && t.pos+2 < len(t.text) && t.text[t.pos+2] == '=' {
				t.AddToken(TokenShiftRightBy, t.GetLoc())
				t.Inc().Inc().Inc()
			} else if t.Next() == '>' {
				t.AddToken(TokenShiftRight, t.GetLoc())
				t.Inc().Inc()
			} else if t.Next() == '=' {
				t.AddToken(TokenGreaterEquals, t.GetLoc())
				t.Inc().Inc()
			} else {
				t.AddToken(TokenGreater, t.GetLoc())
				t.Inc()
			}
			continue
		case '+':
			if t.Next() == '=' {
				t.AddToken(TokenIncBy, t.GetLoc())
				t.Inc().Inc()
			} else if t.Next() == '+' {
				t.AddToken(TokenIncrement, t.GetLoc())
				t.Inc().Inc()
			} else {
				t.AddToken(TokenPlus, t.GetLoc())
				t.Inc()
			}
			continue
		case '-':
			if IsNum(t.Next()) {
				pos := t.GetLoc()
				n, err := t.ReadNumber()
				if err != nil {
					return nil, err
				}

				t.AddToken(TokenNumber, pos, n)
			} else if t.Next() == '=' {
				t.AddToken(TokenDecBy, t.GetLoc())
				t.Inc().Inc()
			} else if t.Next() == '>' {
				t.AddToken(TokenArrow, t.GetLoc())
				t.Inc().Inc()
			} else if t.Next() == '-' {
				t.AddToken(TokenDecrement, t.GetLoc())
				t.Inc().Inc()
			} else {
				t.AddToken(TokenMinus, t.GetLoc())
				t.Inc()
			}
			continue
		case '*':
			if t.Next() == '=' {
				t.AddToken(TokenMulBy, t.GetLoc())
				t.Inc().Inc()
			} else {
				t.AddToken(TokenAsterisk, t.GetLoc())
				t.Inc()
			}
			continue
		case '/':
			if t.Next() == '/' {
				t.IgnoreComment()
			} else if t.Next() == '*' {
				t.IgnoreMultilineComment()
			} else if t.Next() == '=' {
				t.AddToken(TokenDivBy, t.GetLoc())
				t.Inc().Inc()
			} else {
				t.AddToken(TokenSlash, t.GetLoc())
				t.Inc()
			}
			continue
		case '%':
			if t.Next() == '=' {
				t.AddToken(TokenModBy, t.GetLoc())
				t.Inc().Inc()
			} else {
				t.AddToken(TokenPercent, t.GetLoc())
				t.Inc()
			}
			continue
		case '&':
			if t.Next() == '=' {
				t.AddToken(TokenBitwiseAndBy, t.GetLoc())
				t.Inc().Inc()
			} else {
				t.AddToken(TokenAmpersand, t.GetLoc())
				t.Inc()
			}
			continue
		case '|':
			if t.Next() == '=' {
				t.AddToken(TokenBitwiseOrBy, t.GetLoc())
				t.Inc().Inc()
			} else {
				t.AddToken(TokenPipe, t.GetLoc())
				t.Inc()
			}
			continue
		case '^':
			if t.Next() == '=' {
				t.AddToken(TokenBitwiseXorBy, t.GetLoc())
				t.Inc().Inc()
			} else {
				t.AddToken(TokenCaret, t.GetLoc())
				t.Inc()
			}
			continue
		case '~':
			t.AddToken(TokenTilde, t.GetLoc())
			t.Inc()
			continue
		case ';':
			t.AddToken(TokenSemicolon, t.GetLoc())
			t.Inc()
			continue
		case ',':
			t.AddToken(TokenComma, t.GetLoc())
			t.Inc()
			continue
		case ':':
			t.AddToken(TokenColon, t.GetLoc())
			t.Inc()
			continue
		case '@':
			t.AddToken(TokenAt, t.GetLoc())
			t.Inc()
			continue
		case '.':
			loc := t.GetLoc()
			if t.Next() == '.' {
				t.Inc()
				if t.Next() == '.' {
					t.Inc().Inc()
					t.AddToken(Token3Dots, loc)
					continue
				}
				t.Inc()
				t.AddToken(Token2Dots, loc)
				continue
			}

			t.AddToken(TokenDot, t.GetLoc())
			t.Inc()
			continue
		case '\'':
			loc := t.GetLoc()
			t.Inc()
			ch, err := t.HanldeEscape()
			if err != nil {
				return nil, err
			}
			if t.Consume() != '\'' {
				return nil, shared.NewError(loc, "Expected ' to end char literal")
			}
			t.AddToken(TokenChar, loc, ch)
			continue
		}

		return nil, shared.NewError(t.GetLoc(), "unexpected char: %c", c)
	}
}
