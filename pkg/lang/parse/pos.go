package parse

import "fmt"

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
