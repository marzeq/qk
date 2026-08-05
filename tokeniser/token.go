package tokeniser

import "github.com/marzeq/qk/shared"

type Token struct {
	Type  TokenKind
	Value string
	// Loc is the token's half-open source span.
	Loc shared.Location
	// NumberBase preserves the spelling class of numeric tokens after their
	// value has been normalized to decimal. It is zero for non-numbers.
	NumberBase int
}
type TokenKind uint

const (
	TokenEof TokenKind = iota
	TokenNewline

	TokenKeyword
	TokenIdentifier
	TokenNumber
	TokenFloat
	TokenString
	TokenCString
	TokenChar

	TokenOpenParen
	TokenCloseParen
	TokenOpenCurly
	TokenCloseCurly
	TokenOpenSquare
	TokenCloseSquare

	TokenEquals
	TokenEqualsEquals
	TokenNotEquals
	TokenLess
	TokenGreater
	TokenLessEquals
	TokenGreaterEquals

	TokenPlus
	TokenMinus
	TokenNoInitializer
	TokenAsterisk
	TokenSlash
	TokenPercent
	TokenAmpersand
	TokenLogicalAnd
	TokenPipe
	TokenLogicalOr
	TokenCaret
	TokenTilde
	TokenShiftLeft
	TokenShiftRight

	TokenIncBy
	TokenDecBy
	TokenMulBy
	TokenDivBy
	TokenModBy
	TokenBitwiseAndBy
	TokenBitwiseOrBy
	TokenBitwiseXorBy
	TokenShiftLeftBy
	TokenShiftRightBy

	TokenSemicolon
	TokenComma
	TokenExclam
	TokenColon
	TokenDot
	Token2Dots
	Token3Dots
	TokenArrow
	TokenFatArrow
	TokenAt
)

type KeywordKind string

const (
	KeywordLet      KeywordKind = "let"
	KeywordMut      KeywordKind = "mut"
	KeywordStruct   KeywordKind = "struct"
	KeywordEnum     KeywordKind = "enum"
	KeywordUnion    KeywordKind = "union"
	KeywordOpaque   KeywordKind = "opaque"
	KeywordTrait    KeywordKind = "trait"
	KeywordDyn      KeywordKind = "dyn"
	KeywordType     KeywordKind = "type"
	KeywordAlias    KeywordKind = "alias"
	KeywordIf       KeywordKind = "if"
	KeywordMatch    KeywordKind = "match"
	KeywordAs       KeywordKind = "as"
	KeywordWhen     KeywordKind = "when"
	KeywordElse     KeywordKind = "else"
	KeywordFor      KeywordKind = "for"
	KeywordBreak    KeywordKind = "break"
	KeywordContinue KeywordKind = "continue"
	KeywordReturn   KeywordKind = "return"
	KeywordDefer    KeywordKind = "defer"
	KeywordImport   KeywordKind = "import"
	KeywordModule   KeywordKind = "module"
	KeywordPub      KeywordKind = "pub"
	KeywordTrue     KeywordKind = "true"
	KeywordFalse    KeywordKind = "false"
	KeywordNil      KeywordKind = "nil"
	KeywordIn       KeywordKind = "in"
)

func (t Token) String() string {
	switch t.Type {
	case TokenEof:
		return "<eof>"
	case TokenNewline:
		return "\\n"
	case TokenKeyword:
		return "kw(" + t.Value + ")"
	case TokenIdentifier:
		return "ident(" + t.Value + ")"
	case TokenNumber:
		return "num(" + t.Value + ")"
	case TokenFloat:
		return "float(" + t.Value + ")"
	case TokenString:
		return "str(" + t.Value + ")"
	case TokenChar:
		return "ch(" + t.Value + ")"
	case TokenOpenParen:
		return "("
	case TokenCloseParen:
		return ")"
	case TokenOpenCurly:
		return "{"
	case TokenCloseCurly:
		return "}"
	case TokenOpenSquare:
		return "["
	case TokenCloseSquare:
		return "]"
	case TokenEquals:
		return "="
	case TokenEqualsEquals:
		return "=="
	case TokenNotEquals:
		return "!="
	case TokenLess:
		return "<"
	case TokenGreater:
		return ">"
	case TokenLessEquals:
		return "<="
	case TokenGreaterEquals:
		return ">="
	case TokenPlus:
		return "+"
	case TokenMinus:
		return "-"
	case TokenNoInitializer:
		return "---"
	case TokenAsterisk:
		return "*"
	case TokenSlash:
		return "/"
	case TokenPercent:
		return "%"
	case TokenAmpersand:
		return "&"
	case TokenLogicalAnd:
		return "&&"
	case TokenPipe:
		return "|"
	case TokenLogicalOr:
		return "||"
	case TokenCaret:
		return "^"
	case TokenTilde:
		return "~"
	case TokenShiftLeft:
		return "<<"
	case TokenShiftRight:
		return ">>"
	case TokenIncBy:
		return "+="
	case TokenDecBy:
		return "-="
	case TokenMulBy:
		return "*="
	case TokenDivBy:
		return "/="
	case TokenModBy:
		return "%="
	case TokenBitwiseAndBy:
		return "&="
	case TokenBitwiseOrBy:
		return "|="
	case TokenBitwiseXorBy:
		return "^="
	case TokenShiftLeftBy:
		return "<<="
	case TokenShiftRightBy:
		return ">>="
	case TokenSemicolon:
		return ";"
	case TokenComma:
		return ","
	case TokenExclam:
		return "!"
	case TokenColon:
		return ":"
	case TokenDot:
		return "."
	case Token2Dots:
		return ".."
	case Token3Dots:
		return "..."
	case TokenArrow:
		return "->"
	case TokenFatArrow:
		return "=>"
	case TokenAt:
		return "@"
	default:
		return "{UNKNOWN}"
	}
}
