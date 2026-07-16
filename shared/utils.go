package shared

import (
	"fmt"
	"os"
	"strings"
)

type Error struct {
	message   string
	loc       Location
	isWarning bool
}

func NewError(loc Location, message string, a ...any) Error {
	return Error{
		message:   fmt.Sprintf(message, a...),
		loc:       loc,
		isWarning: false,
	}
}

func NewWarning(loc Location, message string, a ...any) Error {
	return Error{
		message:   fmt.Sprintf(message, a...),
		loc:       loc,
		isWarning: true,
	}
}

func (err Error) Error() string {
	f, e := os.ReadFile(err.loc.FilePath)
	if e != nil {
		return err.message
	}

	lines := strings.Split(string(f), "\n")
	lineIdx := err.loc.LC.Line - 1
	if lineIdx < 0 || lineIdx >= len(lines) {
		return err.message
	}

	start := max(0, lineIdx-2)
	end := min(len(lines)-1, lineIdx+2)

	var b strings.Builder
	if err.isWarning {
		b.WriteString("Warning: ")
	} else {
		b.WriteString("Error: ")
	}
	fmt.Fprintf(&b, "%s:%d:%d\n", err.loc.FilePath, err.loc.LC.Line, err.loc.LC.Col)

	const red = "\x1b[31m"
	const yellow = "\x1b[33m"
	const reset = "\x1b[0m"

	var color string
	if err.isWarning {
		color = yellow
	} else {
		color = red
	}

	for i := start; i <= end; i++ {
		ln := i + 1
		if i == lineIdx {
			fmt.Fprintf(&b, "%s%4d |%s %s  %s(col %d)%s\n",
				color, ln, reset, lines[i], color, err.loc.LC.Col, reset)
		} else {
			fmt.Fprintf(&b, "%4d | %s\n", ln, lines[i])
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
	LC       LineCol
	FilePath string
}

func (l Location) String() string {
	return fmt.Sprintf("%s:%d:%d", l.FilePath, l.LC.Line, l.LC.Col)
}

type Pair[T1 any, T2 any] struct {
	L T1
	R T2
}
