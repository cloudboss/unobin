package parse

import (
	"fmt"
	"sort"
)

// Position locates a byte in a source file.
//
// Line and Column are 1-based; Offset is 0-based bytes from start of file.
// File is the path supplied to the parser (may be empty for in-memory inputs).
type Position struct {
	File   string
	Line   int
	Column int
	Offset int
}

// Span is a half open byte range from Start (inclusive) to End (exclusive).
// Both ends share the File from Start. End may be the zero value when only
// a point is known.
type Span struct {
	Start Position
	End   Position
}

// SourceFile maps byte offsets in one file to source positions.
type SourceFile struct {
	File       string
	LineStarts []int
}

// LineStarts returns the byte offset of the first byte of each line.
func LineStarts(src []byte) []int {
	starts := []int{0}
	for i, b := range src {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// NewSourceFile creates a file position helper from a line-start table.
func NewSourceFile(file string, lineStarts []int) SourceFile {
	if len(lineStarts) == 0 {
		lineStarts = []int{0}
	}
	starts := make([]int, len(lineStarts))
	copy(starts, lineStarts)
	return SourceFile{File: file, LineStarts: starts}
}

// Position returns the 1-based line and column for offset.
func (f SourceFile) Position(offset int) Position {
	starts := f.LineStarts
	if len(starts) == 0 {
		starts = []int{0}
	}
	line := max(sort.Search(len(starts), func(i int) bool {
		return starts[i] > offset
	})-1, 0)
	return Position{
		File:   f.File,
		Line:   line + 1,
		Column: offset - starts[line] + 1,
		Offset: offset,
	}
}

// Span returns the source span between two byte offsets.
func (f SourceFile) Span(start, end int) Span {
	return Span{Start: f.Position(start), End: f.Position(end)}
}

func (p Position) String() string {
	if p.File == "" {
		return fmt.Sprintf("%d:%d", p.Line, p.Column)
	}
	return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Column)
}

// IsZero reports whether p has not been set.
func (p Position) IsZero() bool {
	return p.Line == 0 && p.Column == 0 && p.Offset == 0 && p.File == ""
}
