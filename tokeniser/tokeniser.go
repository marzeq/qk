package tokeniser

import (
	"os"

	"github.com/marzeq/quokka/shared"
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
		return nil, err
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
	if t.Peek() == '\n' {
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
	return c == ' ' || c == '\t' || c == '\r' //|| c == '\n'
}

func (t *Tokeniser) ReadWord() string {
	s := ""

	for IsLegalWordChar(t.Peek()) {
		s += string(t.Consume())
	}

	return s
}

func (t *Tokeniser) ReadNumber() (string, error) {
	s := ""

	if t.Peek() == '-' {
		t.Inc()
		s = "-"
	}

	for !IsSpace(t.Peek()) && t.Peek() != '\n' {
		if IsAlpha(t.Peek()) {
			ch := t.Consume()
			return "", shared.NewError(t.GetLoc(), "invalid char in number literal: %c", ch)
		}
		if !IsNum(t.Peek()) {
			break
		}
		s += string(t.Consume())
	}

	return s, nil
}

func (t *Tokeniser) ReadString() (string, error) {
	s := ""

	if t.Peek() != '"' {
		return "", shared.NewError(t.GetLoc(), "expected '\"' to start a string")
	}
	t.Inc()

	for t.Peek() != '"' && t.Peek() != '\n' {
		if t.Peek() == '\\' {
			switch t.Consume() {
			case '\\':
				s += "\\"
			case '"':
				s += "\""
			case 'n':
				s += "\n"
			case 'r':
				s += "\r"
			case 't':
				s += "\t"
			case 'b':
				s += "\b"
			case 'f':
				s += "\f"
			case 'v':
				s += "\v"
			case 'a':
				s += "\a"
			case '0':
				s += string(rune(0))
			default:
				return "", shared.NewError(t.GetLoc(), "invalid escape sequence")
			}
			t.Inc()
		} else {
			s += string(t.Consume())
		}
	}

	if t.Peek() != '"' {
		return "", shared.NewError(t.GetLoc(), "expected '\"' to end a string")
	}
	t.Inc()

	return s, nil
}

func (t *Tokeniser) IgnoreComment() {
	t.Inc().Inc() // skip over the "--"

	for t.Peek() != '\n' {
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

func (t *Tokeniser) AddToken(ttype TokenType, loc shared.Location, _value ...string) {
	value := ""
	if len(_value) > 0 {
		value = _value[0]
	}

	t.tokens = append(t.tokens, Token{Type: ttype, Value: value, Loc: loc})
}

func IsKeyword(w string) bool {
	return w == "let" || w == "var" ||
		w == "if" || w == "else" ||
		w == "for" || w == "break" || w == "continue" ||
		w == "return" || w == "import" ||
		w == "and" || w == "or" || w == "not" ||
		w == "true" || w == "false"
}

func (t *Tokeniser) Tokenise() ([]Token, error) {
	for {
		c := t.Peek()

		if c == 0 {
			t.AddToken(TOKEN_TYPE_EOF, t.GetLoc())
			return t.tokens, nil
		}

		if IsSpace(c) {
			t.Inc()
			continue
		}

		if IsLegalWordStart(c) {
			pos := t.GetLoc()
			w := t.ReadWord()

			if IsKeyword(w) {
				t.AddToken(TOKEN_TYPE_KEYWORD, pos, w)
			} else {
				t.AddToken(TOKEN_TYPE_IDENT, pos, w)
			}
			continue
		}

		if IsNum(c) {
			pos := t.GetLoc()
			n, err := t.ReadNumber()
			if err != nil {
				return nil, err
			}

			t.AddToken(TOKEN_TYPE_NUMBER, pos, n)
			continue
		}

		switch c {
		case '"':
			pos := t.GetLoc()
			s, err := t.ReadString()
			if err != nil {
				return nil, err
			}

			t.AddToken(TOKEN_TYPE_STRING, pos, s)
			continue
		case '\\':
			if t.Next() == '\n' {
				t.Inc().Inc()
			}
			continue
		case '\n':
			t.AddToken(TOKEN_TYPE_NEWLINE, t.GetLoc())
			t.Inc()
			continue
		case '(':
			t.AddToken(TOKEN_TYPE_OPEN_PAREN, t.GetLoc())
			t.Inc()
			continue
		case ')':
			t.AddToken(TOKEN_TYPE_CLOSE_PAREN, t.GetLoc())
			t.Inc()
			continue
		case '{':
			t.AddToken(TOKEN_TYPE_OPEN_CURLY, t.GetLoc())
			t.Inc()
			continue
		case '}':
			t.AddToken(TOKEN_TYPE_CLOSE_CURLY, t.GetLoc())
			t.Inc()
			continue
		case '=':
			if t.Next() == '=' {
				t.AddToken(TOKEN_TYPE_EQUALS_EQUALS, t.GetLoc())
				t.Inc().Inc()
			} else {
				t.AddToken(TOKEN_TYPE_EQUALS, t.GetLoc())
				t.Inc()
			}
			continue
		case '!':
			if t.Next() == '=' {
				t.AddToken(TOKEN_TYPE_NOT_EQUALS, t.GetLoc())
				t.Inc().Inc()
			} else {
				t.AddToken(TOKEN_TYPE_EXCLAM, t.GetLoc())
				t.Inc()
			}
			continue
		case '<':
			if t.Next() == '=' {
				t.AddToken(TOKEN_TYPE_LESS_EQUALS, t.GetLoc())
				t.Inc().Inc()
			} else {
				t.AddToken(TOKEN_TYPE_LESS, t.GetLoc())
				t.Inc()
			}
			continue
		case '>':
			if t.Next() == '=' {
				t.AddToken(TOKEN_TYPE_GREATER_EQUALS, t.GetLoc())
				t.Inc().Inc()
			} else {
				t.AddToken(TOKEN_TYPE_GREATER, t.GetLoc())
				t.Inc()
			}
			continue
		case '+':
			if t.Next() == '=' {
				t.AddToken(TOKEN_TYPE_INC_BY, t.GetLoc())
				t.Inc().Inc()
			} else {
				t.AddToken(TOKEN_TYPE_PLUS, t.GetLoc())
				t.Inc()
			}
			continue
		case '-':
			if t.Next() == '-' {
				t.IgnoreComment()
			} else if IsNum(t.Next()) {
				pos := t.GetLoc()
				n, err := t.ReadNumber()
				if err != nil {
					return nil, err
				}

				t.AddToken(TOKEN_TYPE_NUMBER, pos, n)
			} else if t.Next() == '=' {
				t.AddToken(TOKEN_TYPE_DEC_BY, t.GetLoc())
				t.Inc().Inc()
			} else {
				t.AddToken(TOKEN_TYPE_MINUS, t.GetLoc())
				t.Inc()
			}
			continue
		case '*':
			if t.Next() == '=' {
				t.AddToken(TOKEN_TYPE_MUL_BY, t.GetLoc())
				t.Inc().Inc()
			} else {
				t.AddToken(TOKEN_TYPE_ASTERISK, t.GetLoc())
				t.Inc()
			}
			continue
		case '/':
			if t.Next() == '=' {
				t.AddToken(TOKEN_TYPE_DIV_BY, t.GetLoc())
				t.Inc().Inc()
			} else {
				t.AddToken(TOKEN_TYPE_SLASH, t.GetLoc())
				t.Inc()
			}
			continue
		case ';':
			t.AddToken(TOKEN_TYPE_SEMICOLON, t.GetLoc())
			t.Inc()
			continue
		case ',':
			t.AddToken(TOKEN_TYPE_COMMA, t.GetLoc())
			t.Inc()
			continue
		case ':':
			t.AddToken(TOKEN_TYPE_COLON, t.GetLoc())
			t.Inc()
			continue
		}

		panic("Unexpected char " + string(c))
	}
}
