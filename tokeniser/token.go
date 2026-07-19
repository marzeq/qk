package tokeniser

import "github.com/marzeq/qk/shared"

type Token struct {
	Type  TokenKind
	Value string
	Loc   shared.Location
}
type TokenKind uint

const (
	TokenEof TokenKind = iota
	TokenNewline

	TokenKeyword
	TokenIdentifier
	TokenNumber
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
	TokenIncrement
	TokenDecrement
	TokenAsterisk
	TokenSlash
	TokenPercent
	TokenAmpersand
	TokenPipe
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
	TokenAt
)

type KeywordKind string

const (
	KeywordLet           KeywordKind = "let"
	KeywordMut           KeywordKind = "mut"
	KeywordStruct        KeywordKind = "struct"
	KeywordEnum          KeywordKind = "enum"
	KeywordUnion         KeywordKind = "union"
	KeywordOpaque        KeywordKind = "opaque"
	KeywordTrait         KeywordKind = "trait"
	KeywordType          KeywordKind = "type"
	KeywordAlias         KeywordKind = "alias"
	KeywordIf            KeywordKind = "if"
	KeywordWhen          KeywordKind = "when"
	KeywordCompilerError KeywordKind = "compiler_error"
	KeywordElse          KeywordKind = "else"
	KeywordGiven         KeywordKind = "given"
	KeywordFor           KeywordKind = "for"
	KeywordBreak         KeywordKind = "break"
	KeywordContinue      KeywordKind = "continue"
	KeywordReturn        KeywordKind = "return"
	KeywordDefer         KeywordKind = "defer"
	KeywordImport        KeywordKind = "import"
	KeywordModule        KeywordKind = "module"
	KeywordPub           KeywordKind = "pub"
	KeywordAnd           KeywordKind = "and"
	KeywordOr            KeywordKind = "or"
	KeywordNot           KeywordKind = "not"
	KeywordTrue          KeywordKind = "true"
	KeywordFalse         KeywordKind = "false"
	KeywordNil           KeywordKind = "nil"
	KeywordAs            KeywordKind = "as"
	KeywordIs            KeywordKind = "is"
	KeywordSizeof        KeywordKind = "sizeof"
	KeywordAlignof       KeywordKind = "alignof"
	KeywordOffsetof      KeywordKind = "offsetof"
	KeywordLen           KeywordKind = "len"
	KeywordIn            KeywordKind = "in"
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
	case TokenIncrement:
		return "++"
	case TokenDecrement:
		return "--"
	case TokenAsterisk:
		return "*"
	case TokenSlash:
		return "/"
	case TokenPercent:
		return "%"
	case TokenAmpersand:
		return "&"
	case TokenPipe:
		return "|"
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
	case TokenAt:
		return "@"
	default:
		return "{UNKNOWN}"
	}
}
