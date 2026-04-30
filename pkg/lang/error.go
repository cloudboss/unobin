package lang

import (
	"fmt"
	"sort"
	"strings"
)

// ErrorKind tags an Error so callers can branch on category. The set is
// deliberately small - finer-grained classification is the message's job.
type ErrorKind int

const (
	ErrUnknown ErrorKind = iota
	ErrParse             // Grammar didn't match.
	ErrLex               // Bad token (unterminated string, invalid escape, etc.).
	ErrSchema            // Shape didn't match the file kind's schema.
	ErrType              // Type checker rejected an expression.
	ErrResolve           // Reference target not found.
)

func (k ErrorKind) String() string {
	switch k {
	case ErrParse:
		return "parse"
	case ErrLex:
		return "lex"
	case ErrSchema:
		return "schema"
	case ErrType:
		return "type"
	case ErrResolve:
		return "resolve"
	default:
		return "error"
	}
}

// Error is a single diagnostic. It always carries a Position so output
// formatters can produce file:line:col prefixes consistently.
type Error struct {
	Kind ErrorKind
	Pos  Position
	Msg  string
	// Hint is an optional second line offering a fix or pointer.
	Hint string
}

func (e *Error) Error() string {
	var b strings.Builder
	if !e.Pos.IsZero() {
		b.WriteString(e.Pos.String())
		b.WriteString(": ")
	}
	b.WriteString(e.Kind.String())
	b.WriteString(": ")
	b.WriteString(e.Msg)
	if e.Hint != "" {
		b.WriteString("\n  hint: ")
		b.WriteString(e.Hint)
	}
	return b.String()
}

// Errorf constructs an Error with a formatted message.
func Errorf(kind ErrorKind, pos Position, format string, args ...any) *Error {
	return &Error{Kind: kind, Pos: pos, Msg: fmt.Sprintf(format, args...)}
}

// ErrorList collects diagnostics from a compilation step (parsing,
// type checking, etc.). Callers append errors and continue until the
// budget is exceeded, then inspect Len() to decide whether to advance
// to the next step or surface the errors.
type ErrorList struct {
	errs   []*Error
	budget int // 0 means unlimited.
}

// NewErrorList returns an ErrorList that stops collecting after `budget`
// errors. Use 0 for unlimited.
func NewErrorList(budget int) *ErrorList {
	return &ErrorList{budget: budget}
}

// Add appends e. If the budget has been reached, the error is dropped.
func (l *ErrorList) Add(e *Error) {
	if l.budget > 0 && len(l.errs) >= l.budget {
		return
	}
	l.errs = append(l.errs, e)
}

// Addf is a convenience for Errorf + Add.
func (l *ErrorList) Addf(kind ErrorKind, pos Position, format string, args ...any) {
	l.Add(Errorf(kind, pos, format, args...))
}

// Len returns the number of collected errors.
func (l *ErrorList) Len() int { return len(l.errs) }

// Errors returns the collected errors in source order (file path, then line,
// then column). The returned slice aliases internal storage; callers should
// treat it as readonly.
func (l *ErrorList) Errors() []*Error {
	sort.SliceStable(l.errs, func(i, j int) bool {
		a, b := l.errs[i].Pos, l.errs[j].Pos
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Column < b.Column
	})
	return l.errs
}

// Err returns nil if there are no errors, the single error if exactly one,
// and an aggregate error otherwise. Useful as the return value of a
// compilation step that wants to surface its accumulated diagnostics.
func (l *ErrorList) Err() error {
	switch len(l.errs) {
	case 0:
		return nil
	case 1:
		return l.errs[0]
	default:
		return l
	}
}

func (l *ErrorList) Error() string {
	errs := l.Errors()
	var b strings.Builder
	fmt.Fprintf(&b, "%d errors:\n", len(errs))
	for _, e := range errs {
		b.WriteString("  ")
		b.WriteString(e.Error())
		b.WriteByte('\n')
	}
	return b.String()
}
