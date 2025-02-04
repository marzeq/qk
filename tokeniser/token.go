package tokeniser

import "github.com/marzeq/quokka/shared"

type Token struct {
	Type  TokenType
	Value string
	Loc   shared.Location
}

func (t Token) String() string {
	switch t.Type {
	case TOKEN_TYPE_EOF:
		return "<eof>"
	case TOKEN_TYPE_NEWLINE:
		return "\\n"
	case TOKEN_TYPE_KEYWORD:
		return "kw(" + t.Value + ")"
	case TOKEN_TYPE_IDENT:
		return "ident(" + t.Value + ")"
	case TOKEN_TYPE_NUMBER:
		return "num(" + t.Value + ")"
	case TOKEN_TYPE_STRING:
		return "str(" + t.Value + ")"
	case TOKEN_TYPE_OPEN_PAREN:
		return "("
	case TOKEN_TYPE_CLOSE_PAREN:
		return ")"
	case TOKEN_TYPE_OPEN_CURLY:
		return "{"
	case TOKEN_TYPE_CLOSE_CURLY:
		return "}"
	case TOKEN_TYPE_EQUALS:
		return "="
	case TOKEN_TYPE_EQUALS_EQUALS:
		return "=="
	case TOKEN_TYPE_NOT_EQUALS:
		return "!="
	case TOKEN_TYPE_LESS:
		return "<"
	case TOKEN_TYPE_GREATER:
		return ">"
	case TOKEN_TYPE_LESS_EQUALS:
		return "<="
	case TOKEN_TYPE_GREATER_EQUALS:
		return ">="
	case TOKEN_TYPE_PLUS:
		return "+"
	case TOKEN_TYPE_MINUS:
		return "-"
	case TOKEN_TYPE_ASTERISK:
		return "*"
	case TOKEN_TYPE_SLASH:
		return "/"
	case TOKEN_TYPE_INC_BY:
		return "+="
	case TOKEN_TYPE_DEC_BY:
		return "-="
	case TOKEN_TYPE_MUL_BY:
		return "*="
	case TOKEN_TYPE_DIV_BY:
		return "/="
	case TOKEN_TYPE_SEMICOLON:
		return ";"
	case TOKEN_TYPE_COMMA:
		return ","
	case TOKEN_TYPE_EXCLAM:
		return "!"
	case TOKEN_TYPE_COLON:
		return ":"
	default:
		return "{UNKNOWN}"
	}
}

type TokenType uint

const (
	TOKEN_TYPE_EOF TokenType = iota
	TOKEN_TYPE_NEWLINE

	TOKEN_TYPE_KEYWORD
	TOKEN_TYPE_IDENT
	TOKEN_TYPE_NUMBER
	TOKEN_TYPE_STRING

	TOKEN_TYPE_OPEN_PAREN
	TOKEN_TYPE_CLOSE_PAREN
	TOKEN_TYPE_OPEN_CURLY
	TOKEN_TYPE_CLOSE_CURLY

	TOKEN_TYPE_EQUALS
	TOKEN_TYPE_EQUALS_EQUALS
	TOKEN_TYPE_NOT_EQUALS
	TOKEN_TYPE_LESS
	TOKEN_TYPE_GREATER
	TOKEN_TYPE_LESS_EQUALS
	TOKEN_TYPE_GREATER_EQUALS

	TOKEN_TYPE_PLUS
	TOKEN_TYPE_MINUS
	TOKEN_TYPE_ASTERISK
	TOKEN_TYPE_SLASH

	TOKEN_TYPE_INC_BY
	TOKEN_TYPE_DEC_BY
	TOKEN_TYPE_MUL_BY
	TOKEN_TYPE_DIV_BY

	TOKEN_TYPE_SEMICOLON
	TOKEN_TYPE_COMMA
	TOKEN_TYPE_EXCLAM
	TOKEN_TYPE_COLON
)
