package shared

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

type Error struct {
	message     string
	loc         Location
	isWarning   bool
	warningKind WarningKind
}

type WarningKind string

const (
	WarningUnusedVariable  WarningKind = "unused-variable"
	WarningUnusedParameter WarningKind = "unused-parameter"
)

func NewError(loc Location, message string, a ...any) Error {
	return Error{
		message:   fmt.Sprintf(message, a...),
		loc:       loc,
		isWarning: false,
	}
}

func NewWarning(kind WarningKind, loc Location, message string, a ...any) Error {
	return Error{
		message:     fmt.Sprintf(message, a...),
		loc:         loc,
		isWarning:   true,
		warningKind: kind,
	}
}

func (err Error) WarningKind() WarningKind { return err.warningKind }

func (err Error) AsError() Error {
	err.isWarning = false
	return err
}

func (err Error) Error() string {
	source := err.loc.SourceText
	if source == "" {
		f, e := os.ReadFile(err.loc.FilePath)
		if e == nil {
			source = string(f)
		}
	}
	if source == "" {
		if err.loc.FilePath != "" && err.loc.LC.Line > 0 {
			kind := "Error"
			if err.isWarning {
				kind = "Warning"
			}
			return fmt.Sprintf("%s: %s:%d:%d\n\n%s", kind, err.loc.FilePath,
				err.loc.LC.Line, err.loc.LC.Col, err.message)
		}
		return err.message
	}

	lines := strings.Split(source, "\n")
	startLineIdx := err.loc.LC.Line - 1
	if startLineIdx < 0 || startLineIdx >= len(lines) {
		return err.message
	}
	endLC := err.loc.EndLC
	if endLC.Line <= 0 || endLC.Col <= 0 ||
		endLC.Line < err.loc.LC.Line ||
		(endLC.Line == err.loc.LC.Line && endLC.Col <= err.loc.LC.Col) {
		endLC = LineCol{Line: err.loc.LC.Line, Col: err.loc.LC.Col + 1}
	}
	endLineIdx := min(len(lines)-1, endLC.Line-1)

	start := max(0, startLineIdx-2)
	end := min(len(lines)-1, endLineIdx+2)

	var b strings.Builder
	if err.isWarning {
		b.WriteString("Warning: ")
	} else {
		b.WriteString("Error: ")
	}
	fmt.Fprintf(&b, "%s:%d:%d\n", err.loc.FilePath, err.loc.LC.Line, err.loc.LC.Col)

	red := "\x1b[31m"
	yellow := "\x1b[33m"
	reset := "\x1b[0m"
	if runtime.GOOS == "windows" || os.Getenv("NO_COLOR") != "" {
		red = ""
		yellow = ""
		reset = ""
	}

	var color string
	if err.isWarning {
		color = yellow
	} else {
		color = red
	}

	for i := start; i <= end; i++ {
		ln := i + 1
		if i < startLineIdx || i > endLineIdx {
			fmt.Fprintf(&b, "%4d | %s\n", ln, lines[i])
			continue
		}

		line := []rune(lines[i])
		highlightStart := 0
		if i == startLineIdx {
			highlightStart = max(0, err.loc.LC.Col-1)
		}
		highlightEnd := len(line)
		if i == endLineIdx {
			highlightEnd = max(0, endLC.Col-1)
		}
		highlightStart = min(highlightStart, len(line))
		highlightEnd = min(max(highlightEnd, highlightStart), len(line))
		if highlightEnd == highlightStart && highlightStart < len(line) {
			highlightEnd++
		}
		fmt.Fprintf(&b, "%s%4d |%s %s%s%s%s\n", color, ln, reset,
			string(line[:highlightStart]), color, string(line[highlightStart:highlightEnd]), reset+string(line[highlightEnd:]))
		if color == "" {
			markerWidth := max(1, highlightEnd-highlightStart)
			fmt.Fprintf(&b, "     | %s%s\n", strings.Repeat(" ", highlightStart), strings.Repeat("^", markerWidth))
		}
	}

	fmt.Fprintf(&b, "\n%s", err.message)
	return b.String()
}

type LineCol struct {
	Line int
	Col  int
}

type Location struct {
	LC         LineCol
	EndLC      LineCol
	Offset     int
	EndOffset  int
	FilePath   string
	SourceText string
}

func (l Location) WithEnd(end Location) Location {
	l.EndLC = end.EndLC
	l.EndOffset = end.EndOffset
	if l.EndLC.Line == 0 {
		l.EndLC = end.LC
		l.EndOffset = end.Offset
	}
	return l
}

func (l Location) String() string {
	return fmt.Sprintf("%s:%d:%d", l.FilePath, l.LC.Line, l.LC.Col)
}

type Pair[T1 any, T2 any] struct {
	L T1
	R T2
}
