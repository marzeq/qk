package tokeniser

import "github.com/marzeq/qk/shared"

type Token struct {
	Type  TokenType
	Value string
	Loc   shared.Location
}
type TokenType uint

const (
	TOKEN_TYPE_EOF TokenType = iota
	TOKEN_TYPE_NEWLINE

	TOKEN_TYPE_KEYWORD
	TOKEN_TYPE_IDENT
	TOKEN_TYPE_NUMBER
	TOKEN_TYPE_STRING
	TOKEN_TYPE_CHAR

	TOKEN_TYPE_OPEN_PAREN
	TOKEN_TYPE_CLOSE_PAREN
	TOKEN_TYPE_OPEN_CURLY
	TOKEN_TYPE_CLOSE_CURLY
	TOKEN_TYPE_OPEN_SQUARE
	TOKEN_TYPE_CLOSE_SQUARE

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
	TOKEN_TYPE_PERCENT
	TOKEN_TYPE_AMPERSAND

	TOKEN_TYPE_INC_BY
	TOKEN_TYPE_DEC_BY
	TOKEN_TYPE_MUL_BY
	TOKEN_TYPE_DIV_BY
	TOKEN_TYPE_MOD_BY

	TOKEN_TYPE_SEMICOLON
	TOKEN_TYPE_COMMA
	TOKEN_TYPE_EXCLAM
	TOKEN_TYPE_COLON
	TOKEN_TYPE_DOT
	TOKEN_TYPE_3DOTS
	TOKEN_TYPE_ARROW
)

type KeywordType string

const (
	KEYWORD_LET      KeywordType = "let"
	KEYWORD_VAR      KeywordType = "var"
	KEYWORD_EXTERN   KeywordType = "extern"
	KEYWORD_STRUCT   KeywordType = "struct"
	KEYWORD_IF       KeywordType = "if"
	KEYWORD_ELSE     KeywordType = "else"
	KEYWORD_GIVEN    KeywordType = "given"
	KEYWORD_FOR      KeywordType = "for"
	KEYWORD_BREAK    KeywordType = "break"
	KEYWORD_CONTINUE KeywordType = "continue"
	KEYWORD_RETURN   KeywordType = "return"
	KEYWORD_IMPORT   KeywordType = "import"
	KEYWORD_MODULE   KeywordType = "module"
	KEYWORD_PUB      KeywordType = "pub"
	KEYWORD_AND      KeywordType = "and"
	KEYWORD_OR       KeywordType = "or"
	KEYWORD_NOT      KeywordType = "not"
	KEYWORD_TRUE     KeywordType = "true"
	KEYWORD_FALSE    KeywordType = "false"
	KEYWORD_NIL      KeywordType = "nil"
	KEYWORD_AS       KeywordType = "as"
	KEYWORD_SIZEOF   KeywordType = "sizeof"
)

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
	case TOKEN_TYPE_CHAR:
		return "ch(" + t.Value + ")"
	case TOKEN_TYPE_OPEN_PAREN:
		return "("
	case TOKEN_TYPE_CLOSE_PAREN:
		return ")"
	case TOKEN_TYPE_OPEN_CURLY:
		return "{"
	case TOKEN_TYPE_CLOSE_CURLY:
		return "}"
	case TOKEN_TYPE_OPEN_SQUARE:
		return "["
	case TOKEN_TYPE_CLOSE_SQUARE:
		return "]"
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
	case TOKEN_TYPE_PERCENT:
		return "%"
	case TOKEN_TYPE_AMPERSAND:
		return "&"
	case TOKEN_TYPE_INC_BY:
		return "+="
	case TOKEN_TYPE_DEC_BY:
		return "-="
	case TOKEN_TYPE_MUL_BY:
		return "*="
	case TOKEN_TYPE_DIV_BY:
		return "/="
	case TOKEN_TYPE_MOD_BY:
		return "%="
	case TOKEN_TYPE_SEMICOLON:
		return ";"
	case TOKEN_TYPE_COMMA:
		return ","
	case TOKEN_TYPE_EXCLAM:
		return "!"
	case TOKEN_TYPE_COLON:
		return ":"
	case TOKEN_TYPE_DOT:
		return "."
	case TOKEN_TYPE_3DOTS:
		return "..."
	case TOKEN_TYPE_ARROW:
		return "->"
	default:
		return "{UNKNOWN}"
	}
}
