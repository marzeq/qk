// Package formatter implements qkc's fixed, source-preserving QK style.
package formatter

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/tokeniser"
)

const (
	maxWidth  = 200
	softWidth = 100
)

// Format formats valid QK source without changing its significant tokens.
func Format(source, path string) (string, error) {
	original, err := lex(source, path)
	if err != nil {
		return "", err
	}
	if _, err = parser.NewParser(original).Parse(); err != nil {
		return "", err
	}
	current := strings.ReplaceAll(source, "\r\n", "\n")
	for round := 0; round < 8; round++ {
		before := current
		for _, pass := range []func(string, string) string{layoutTypeBodies, layoutFieldCommas, layoutGroups, layoutBlocks, layoutSpacing, layoutLines, layoutBlankLines} {
			candidate := pass(current, path)
			if valid(candidate, path, source, original) {
				current = candidate
			}
		}
		if current == before {
			break
		}
		if round == 7 {
			return "", fmt.Errorf("formatter did not converge")
		}
	}
	current = strings.TrimRight(current, " \t\n") + "\n"
	if !valid(current, path, source, original) {
		return "", fmt.Errorf("formatter could not preserve source syntax")
	}
	for lineNumber, line := range strings.Split(current, "\n") {
		if width := utf8.RuneCountInString(line); width > maxWidth {
			return "", fmt.Errorf("line %d is %d columns wide (maximum %d)", lineNumber+1, width, maxWidth)
		}
	}
	return current, nil
}

func layoutSpacing(source, path string) string {
	tokens, err := lex(source, path)
	if err != nil {
		return source
	}
	r := []rune(source)
	var edits []edit
	for i := 1; i < len(tokens); i++ {
		prev, next := tokens[i-1], tokens[i]
		if prev.Type == tokeniser.TokenNewline || next.Type == tokeniser.TokenNewline || next.Type == tokeniser.TokenEof || prev.Loc.EndLC.Line != next.Loc.LC.Line {
			continue
		}
		gap := string(r[prev.Loc.EndOffset:next.Loc.Offset])
		if strings.Contains(gap, "/") || strings.Contains(gap, "\\") {
			continue
		}
		wanted := " "
		switch {
		case next.Type == tokeniser.TokenComma || next.Type == tokeniser.TokenSemicolon || next.Type == tokeniser.TokenColon || next.Type == tokeniser.TokenCloseParen || next.Type == tokeniser.TokenCloseSquare:
			wanted = ""
		case next.Type == tokeniser.TokenDot && prev.Type != tokeniser.TokenEquals:
			wanted = ""
		case prev.Type == tokeniser.TokenDot || prev.Type == tokeniser.TokenAt || prev.Type == tokeniser.TokenDollar:
			wanted = ""
		case next.Type == tokeniser.TokenOpenParen && prev.Type != tokeniser.TokenKeyword:
			wanted = ""
		case next.Type == tokeniser.TokenOpenSquare && prev.Type != tokeniser.TokenEquals && prev.Type != tokeniser.TokenKeyword && prev.Type != tokeniser.TokenColon:
			wanted = ""
		case prev.Type == tokeniser.TokenCloseSquare && next.Type == tokeniser.TokenIdentifier:
			wanted = ""
		case next.Type == tokeniser.TokenOpenCurly && prev.Type == tokeniser.TokenDot:
			wanted = ""
		case prev.Type == tokeniser.TokenOpenParen || prev.Type == tokeniser.TokenOpenSquare:
			wanted = ""
		case prev.Type == tokeniser.TokenAsterisk || prev.Type == tokeniser.TokenAmpersand || prev.Type == tokeniser.TokenExclam || prev.Type == tokeniser.TokenTilde:
			wanted = ""
		case next.Type == tokeniser.TokenAsterisk && (prev.Type == tokeniser.TokenOpenParen || prev.Type == tokeniser.TokenOpenSquare):
			wanted = ""
		case next.Type == tokeniser.TokenCloseCurly && prev.Type == tokeniser.TokenOpenCurly:
			wanted = ""
		}
		if gap != wanted {
			edits = append(edits, edit{start: prev.Loc.EndOffset, end: next.Loc.Offset, text: wanted})
		}
	}
	return apply(source, edits)
}

func lex(source, path string) ([]tokeniser.Token, error) {
	return tokeniser.NewTokeniser(source, path).Tokenise()
}

func valid(source, path, originalSource string, original []tokeniser.Token) bool {
	tokens, err := lex(source, path)
	if err != nil || !sameTokens(original, tokens) || !sameComments(originalSource, original, source, tokens) {
		return false
	}
	_, err = parser.NewParser(tokens).Parse()
	return err == nil
}

func sameComments(aSource string, aTokens []tokeniser.Token, bSource string, bTokens []tokeniser.Token) bool {
	a := comments(aSource, aTokens)
	b := comments(bSource, bTokens)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func comments(source string, tokens []tokeniser.Token) []string {
	r := []rune(source)
	var result []string
	last := 0
	for _, token := range tokens {
		if token.Loc.Offset < last || token.Loc.Offset > len(r) {
			continue
		}
		gap := r[last:token.Loc.Offset]
		for len(gap) > 1 {
			if gap[0] == '/' && gap[1] == '/' {
				n := 0
				for n < len(gap) && gap[n] != '\n' {
					n++
				}
				result = append(result, string(gap[:n]))
				gap = gap[n:]
			} else if gap[0] == '/' && gap[1] == '*' {
				end := strings.Index(string(gap), "*/")
				if end < 0 {
					break
				}
				n := utf8.RuneCountInString(string(gap[:end+2]))
				result = append(result, string(gap[:n]))
				gap = gap[n:]
			} else {
				gap = gap[1:]
			}
		}
		last = token.Loc.EndOffset
	}
	return result
}

func sameTokens(a, b []tokeniser.Token) bool {
	x, y := 0, 0
	for {
		for x < len(a) && (a[x].Type == tokeniser.TokenNewline || a[x].Type == tokeniser.TokenComma && trailingComma(a, x)) {
			x++
		}
		for y < len(b) && (b[y].Type == tokeniser.TokenNewline || b[y].Type == tokeniser.TokenComma && trailingComma(b, y)) {
			y++
		}
		if x == len(a) || y == len(b) {
			return x == len(a) && y == len(b)
		}
		if a[x].Type != b[y].Type || a[x].Value != b[y].Value || a[x].NumberBase != b[y].NumberBase {
			return false
		}
		x++
		y++
	}
}

func trailingComma(tokens []tokeniser.Token, at int) bool {
	for i := at + 1; i < len(tokens); i++ {
		if tokens[i].Type == tokeniser.TokenNewline {
			continue
		}
		return tokens[i].Type == tokeniser.TokenCloseParen || tokens[i].Type == tokeniser.TokenCloseCurly || tokens[i].Type == tokeniser.TokenCloseSquare
	}
	return false
}

type edit struct {
	start, end int
	text       string
}

func apply(source string, edits []edit) string {
	r := []rune(source)
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	limit := len(r)
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		if e.start < 0 || e.end > limit || e.start > e.end {
			continue
		}
		r = append(append(append([]rune{}, r[:e.start]...), []rune(e.text)...), r[e.end:]...)
		limit = e.start
	}
	return string(r)
}

func layoutBlocks(source, path string) string {
	tokens, err := lex(source, path)
	if err != nil {
		return source
	}
	r := []rune(source)
	type frame struct{ index int }
	var stack []frame
	var edits []edit
	for i, t := range tokens {
		if t.Type == tokeniser.TokenOpenCurly {
			stack = append(stack, frame{i})
			continue
		}
		if t.Type != tokeniser.TokenCloseCurly || len(stack) == 0 {
			continue
		}
		g := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		open := tokens[g.index]
		if g.index > 0 && tokens[g.index-1].Type == tokeniser.TokenDot {
			continue
		}
		decl := false
		for j := g.index - 1; j >= 0 && j >= g.index-8; j-- {
			if tokens[j].Value == "struct" || tokens[j].Value == "enum" || tokens[j].Value == "union" || tokens[j].Value == "trait" || tokens[j].Value == "flags" {
				decl = true
				break
			}
			if tokens[j].Type == tokeniser.TokenOpenCurly || tokens[j].Type == tokeniser.TokenNewline {
				break
			}
		}
		if decl {
			continue
		}
		first, last := -1, -1
		for j := g.index + 1; j < i; j++ {
			if tokens[j].Type != tokeniser.TokenNewline {
				if first < 0 {
					first = j
				}
				last = j
			}
		}
		if first < 0 {
			continue
		}
		content := string(r[open.Loc.EndOffset:t.Loc.Offset])
		if strings.Contains(content, "//") || strings.Contains(content, "/*") {
			continue
		}
		keep := false
		if open.Loc.LC.Line == t.Loc.LC.Line {
			lineStart := g.index - 1
			for lineStart >= 0 && tokens[lineStart].Loc.LC.Line == open.Loc.LC.Line {
				lineStart--
			}
			isIf, expr := false, false
			for j := lineStart + 1; j < g.index; j++ {
				if tokens[j].Value == "if" {
					isIf = true
				}
				if tokens[j].Type == tokeniser.TokenEquals || tokens[j].Value == "return" {
					expr = true
				}
			}
			body := strings.TrimSpace(content)
			keep = isIf && (expr && utf8.RuneCountInString(body) <= 32 || strings.HasPrefix(body, "return") && utf8.RuneCountInString(body) < 10)
		}
		if keep {
			continue
		}
		if open.Loc.EndLC.Line == tokens[first].Loc.LC.Line {
			edits = append(edits, edit{open.Loc.EndOffset, tokens[first].Loc.Offset, "\n"})
		}
		if tokens[last].Loc.EndLC.Line == t.Loc.LC.Line {
			edits = append(edits, edit{tokens[last].Loc.EndOffset, t.Loc.Offset, "\n"})
		}
	}
	return apply(source, edits)
}

func layoutGroups(source, path string) string {
	tokens, err := lex(source, path)
	if err != nil {
		return source
	}
	r := []rune(source)
	type opening struct {
		index  int
		commas []int
	}
	var stack []opening
	var edits []edit
	for i, t := range tokens {
		switch t.Type {
		case tokeniser.TokenOpenParen, tokeniser.TokenOpenSquare, tokeniser.TokenOpenCurly:
			stack = append(stack, opening{index: i})
		case tokeniser.TokenComma:
			if len(stack) > 0 {
				stack[len(stack)-1].commas = append(stack[len(stack)-1].commas, i)
			}
		case tokeniser.TokenCloseParen, tokeniser.TokenCloseSquare, tokeniser.TokenCloseCurly:
			if len(stack) == 0 {
				continue
			}
			g := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			open := tokens[g.index]
			if open.Loc.LC.Line != t.Loc.LC.Line {
				continue
			}
			literal := open.Type == tokeniser.TokenOpenCurly && g.index > 0 && tokens[g.index-1].Type == tokeniser.TokenDot
			structBody := open.Type == tokeniser.TokenOpenCurly && g.index > 0 && tokens[g.index-1].Value == "struct"
			if open.Type != tokeniser.TokenOpenParen && !literal && !structBody {
				continue
			}
			if len(g.commas) == 0 && !literal && !structBody {
				continue
			}
			content := string(r[open.Loc.EndOffset:t.Loc.Offset])
			if strings.Contains(content, "//") || strings.Contains(content, "/*") {
				continue
			}
			count := len(g.commas) + 1
			if strings.TrimSpace(content) == "" {
				count = 0
			}
			if g.commas != nil && strings.TrimSpace(string(r[tokens[g.commas[len(g.commas)-1]].Loc.EndOffset:t.Loc.Offset])) == "" {
				count--
			}
			lineStart := strings.LastIndex(string(r[:open.Loc.Offset]), "\n") + 1
			prefixWidth := utf8.RuneCountInString(string(r[lineStart:open.Loc.Offset]))
			width := utf8.RuneCountInString(string(r[open.Loc.Offset:t.Loc.EndOffset]))
			threshold := 4
			if literal {
				threshold = 5
			}
			shouldBreak := count >= threshold || prefixWidth+width > softWidth
			if structBody {
				shouldBreak = true
			}
			if shouldBreak && count > 0 {
				// Replace only the group's whitespace and commas. Nested expressions
				// keep their exact spelling and receive indentation in the next pass.
				start := open.Loc.EndOffset
				for _, commaIndex := range g.commas {
					comma := tokens[commaIndex]
					part := strings.TrimSpace(string(r[start:comma.Loc.Offset]))
					if part != "" {
						edits = append(edits, edit{start: start, end: comma.Loc.EndOffset, text: "\n" + part + ","})
					}
					start = comma.Loc.EndOffset
				}
				tail := strings.TrimSpace(string(r[start:t.Loc.Offset]))
				if tail != "" {
					if !strings.HasSuffix(tail, "...") && (open.Type == tokeniser.TokenOpenParen || literal || structBody) {
						tail += ","
					}
					edits = append(edits, edit{start: start, end: t.Loc.Offset, text: "\n" + tail + "\n"})
				} else {
					edits = append(edits, edit{start: start, end: t.Loc.Offset, text: "\n"})
				}
			} else if literal && count > 0 {
				edits = append(edits, edit{start: open.Loc.EndOffset, end: t.Loc.Offset, text: " " + strings.TrimSpace(content) + " "})
			}
		}
	}
	return apply(source, edits)
}

func layoutLines(source, path string) string {
	tokens, err := lex(source, path)
	if err != nil {
		return source
	}
	// Braces, parentheses, and brackets contribute to continuation indentation.
	type depth struct{ curly, paren, square int }
	lines := strings.Split(source, "\n")
	byLine := make(map[int][]tokeniser.Token)
	for _, t := range tokens {
		if t.Type != tokeniser.TokenEof && t.Type != tokeniser.TokenNewline {
			byLine[t.Loc.LC.Line] = append(byLine[t.Loc.LC.Line], t)
		}
	}
	d := depth{}
	for i, line := range lines {
		ts := byLine[i+1]
		if strings.TrimSpace(line) == "" {
			lines[i] = ""
			continue
		}
		indent := d.curly + d.paren + d.square
		if len(ts) > 0 {
			switch ts[0].Type {
			case tokeniser.TokenCloseCurly, tokeniser.TokenCloseParen, tokeniser.TokenCloseSquare:
				indent--
			}
		}
		if indent < 0 {
			indent = 0
		}
		// Keep the interior of multiline comments verbatim.
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "*") && !strings.HasPrefix(trimmed, "*/") {
			lines[i] = strings.Repeat("  ", indent) + strings.TrimRight(strings.TrimLeft(line, " \t"), " \t")
		}
		for _, t := range ts {
			switch t.Type {
			case tokeniser.TokenOpenCurly:
				d.curly++
			case tokeniser.TokenCloseCurly:
				d.curly--
			case tokeniser.TokenOpenParen:
				d.paren++
			case tokeniser.TokenCloseParen:
				d.paren--
			case tokeniser.TokenOpenSquare:
				d.square++
			case tokeniser.TokenCloseSquare:
				d.square--
			}
		}
	}
	return strings.Join(lines, "\n")
}

func layoutBlankLines(source, path string) string {
	tokens, err := lex(source, path)
	if err != nil {
		return source
	}
	lines := strings.Split(source, "\n")
	// Fields and variants stay together; methods get two empty lines.
	type scope struct {
		fields bool
		seen   bool
	}
	var scopes []scope
	wanted := make(map[int]int)
	for i, t := range tokens {
		if t.Type == tokeniser.TokenOpenCurly {
			fields := false
			if i > 0 {
				for j := i - 1; j >= 0 && j >= i-8; j-- {
					if tokens[j].Value == "struct" || tokens[j].Value == "enum" || tokens[j].Value == "union" || tokens[j].Value == "trait" || tokens[j].Value == "flags" {
						fields = true
						break
					}
					if tokens[j].Type == tokeniser.TokenOpenCurly {
						break
					}
				}
			}
			scopes = append(scopes, scope{fields: fields})
		} else if t.Type == tokeniser.TokenCloseCurly {
			if len(scopes) > 0 {
				scopes = scopes[:len(scopes)-1]
			}
		} else if len(scopes) > 0 && scopes[len(scopes)-1].fields && t.Loc.LC.Col > 0 {
			line := t.Loc.LC.Line
			if line <= len(lines) && strings.TrimSpace(lines[line-1]) != "" && strings.TrimSpace(lines[line-1])[0] != '}' {
				first := true
				for j := i - 1; j >= 0 && tokens[j].Loc.LC.Line == line; j-- {
					if tokens[j].Type != tokeniser.TokenNewline {
						first = false
						break
					}
				}
				if first && t.Type != tokeniser.TokenNewline && t.Type != tokeniser.TokenComma && t.Type != tokeniser.TokenCloseParen {
					s := &scopes[len(scopes)-1]
					trimmed := strings.TrimSpace(lines[line-1])
					method := strings.HasPrefix(trimmed, "let ") || strings.HasPrefix(trimmed, "pub let ")
					if s.seen && method {
						wanted[line] = 2
					}
					s.seen = true
				}
			}
		}
	}
	depth := 0
	lastLine := 0
	for _, t := range tokens {
		if t.Type == tokeniser.TokenNewline || t.Type == tokeniser.TokenEof {
			continue
		}
		if t.Loc.LC.Line != lastLine {
			line := t.Loc.LC.Line
			if depth == 0 && line > 1 && (t.Type == tokeniser.TokenKeyword && (t.Value == "let" || t.Value == "pub" || t.Value == "import") || t.Type == tokeniser.TokenDollar) {
				previous := strings.TrimSpace(lines[line-2])
				comment := strings.HasPrefix(previous, "//") || strings.HasPrefix(previous, "/*") || strings.HasPrefix(previous, "*")
				importsTogether := t.Value == "import" && strings.HasPrefix(previous, "import ")
				if !comment && !importsTogether && !strings.HasPrefix(previous, "@") && wanted[line] < 1 {
					wanted[line] = 1
				}
			}
			lastLine = line
		}
		if t.Type == tokeniser.TokenOpenCurly {
			depth++
		}
		if t.Type == tokeniser.TokenCloseCurly {
			depth--
		}
	}
	for line := 2; line <= len(lines); line++ {
		trimmed := strings.TrimSpace(lines[line-1])
		if !strings.HasPrefix(trimmed, "//") || strings.HasPrefix(lines[line-1], " ") || strings.HasPrefix(lines[line-1], "\t") {
			continue
		}
		previous := strings.TrimSpace(lines[line-2])
		if strings.HasPrefix(previous, "import ") || strings.HasPrefix(previous, "module ") || strings.HasSuffix(previous, "}") {
			wanted[line] = 1
		}
	}
	var out []string
	for i, line := range lines {
		if wanted[i+1] > 0 {
			for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
				out = out[:len(out)-1]
			}
			if len(out) > 0 {
				for n := 0; n < wanted[i+1]; n++ {
					out = append(out, "")
				}
			}
		}
		out = append(out, line)
	}
	var capped []string
	blanks := 0
	for _, line := range out {
		if strings.TrimSpace(line) == "" {
			blanks++
			if blanks > 2 {
				continue
			}
		} else {
			blanks = 0
		}
		capped = append(capped, line)
	}
	return strings.Join(capped, "\n")
}

// MaxWidth reports the formatter's hard target for tooling and tests.
func MaxWidth() int { return maxWidth }

func typeBody(tokens []tokeniser.Token, at int) bool {
	for j := at - 1; j >= 0 && j >= at-8; j-- {
		switch tokens[j].Value {
		case "struct", "enum", "union", "trait", "flags":
			return true
		}
		if tokens[j].Type == tokeniser.TokenOpenCurly || tokens[j].Type == tokeniser.TokenNewline {
			return false
		}
	}
	return false
}

func matchBody(tokens []tokeniser.Token, at int) bool {
	for j := at - 1; j >= 0; j-- {
		if tokens[j].Value == "match" {
			return true
		}
		if tokens[j].Type == tokeniser.TokenNewline || tokens[j].Type == tokeniser.TokenOpenCurly || tokens[j].Type == tokeniser.TokenCloseCurly {
			return false
		}
	}
	return false
}

func layoutTypeBodies(source, path string) string {
	tokens, err := lex(source, path)
	if err != nil {
		return source
	}
	type frame struct {
		index  int
		fields bool
	}
	var stack []frame
	var edits []edit
	for i, t := range tokens {
		if t.Type == tokeniser.TokenOpenCurly {
			stack = append(stack, frame{i, typeBody(tokens, i)})
			continue
		}
		if t.Type != tokeniser.TokenCloseCurly || len(stack) == 0 {
			continue
		}
		g := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !g.fields {
			continue
		}
		first, last := -1, -1
		for j := g.index + 1; j < i; j++ {
			if tokens[j].Type != tokeniser.TokenNewline {
				if first < 0 {
					first = j
				}
				last = j
			}
		}
		if first < 0 {
			continue
		}
		open := tokens[g.index]
		if open.Loc.LC.Line == tokens[first].Loc.LC.Line {
			edits = append(edits, edit{open.Loc.EndOffset, tokens[first].Loc.Offset, "\n"})
		}
		if tokens[last].Loc.EndLC.Line == t.Loc.LC.Line {
			edits = append(edits, edit{tokens[last].Loc.EndOffset, t.Loc.Offset, "\n"})
		}
	}
	return apply(source, edits)
}

func layoutFieldCommas(source, path string) string {
	tokens, err := lex(source, path)
	if err != nil {
		return source
	}
	type frame struct {
		kind   tokeniser.TokenKind
		fields bool
	}
	var stack []frame
	var edits []edit
	for i, t := range tokens {
		switch t.Type {
		case tokeniser.TokenOpenParen, tokeniser.TokenOpenSquare:
			stack = append(stack, frame{kind: t.Type})
		case tokeniser.TokenOpenCurly:
			stack = append(stack, frame{kind: t.Type, fields: typeBody(tokens, i) || matchBody(tokens, i)})
		case tokeniser.TokenCloseParen, tokeniser.TokenCloseSquare, tokeniser.TokenCloseCurly:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case tokeniser.TokenComma:
			if len(stack) == 0 || !stack[len(stack)-1].fields {
				continue
			}
			j := i + 1
			for j < len(tokens) && tokens[j].Type == tokeniser.TokenNewline {
				j++
			}
			if j < len(tokens) && tokens[j].Type != tokeniser.TokenCloseCurly && tokens[j].Loc.LC.Line == t.Loc.LC.Line {
				edits = append(edits, edit{t.Loc.EndOffset, tokens[j].Loc.Offset, "\n"})
			}
		}
	}
	return apply(source, edits)
}
