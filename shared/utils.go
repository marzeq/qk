package shared

import (
  "fmt"
  "os"
  "strings"
)

type Error struct {
  message string
  loc     Location
}

func NewError(loc Location, message string, a ...any) Error {
  return Error{
    message: fmt.Sprintf(message, a...),
    loc:     loc,
  }
}

func (err Error) Error() string {
  f, e := os.ReadFile(err.loc.FilePath)
  if e != nil {
    return err.message
  }

  line := strings.Split(string(f), "\n")[err.loc.LC.Line-1]
  pointer := ""
  if err.loc.LC.Col == 1 {
    pointer = "^"
  } else {
    pointer = strings.Repeat(" ", max(0, err.loc.LC.Col-2)) + "^" + strings.Repeat(" ", len(line)-err.loc.LC.Col+1)
  }

  return fmt.Sprintf("%s:%d:%d\n%s\n%s\n\n%s", err.loc.FilePath, err.loc.LC.Line, err.loc.LC.Col, line, pointer, err.message)
}

type LineCol struct {
  Line int
  Col  int
}

type Location struct {
  LC       LineCol
  FilePath string
}

type Pair[T1 any, T2 any] struct {
  L T1
  R T2
}
