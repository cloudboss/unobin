package diagnostic

import (
	"cmp"
	"fmt"
	"io"
	"slices"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

type Position struct {
	Line   int `json:"line"   ub:"line"`
	Column int `json:"column" ub:"column"`
	Offset int `json:"offset" ub:"offset"`
}

type Span struct {
	Start Position  `json:"start"         ub:"start"`
	End   *Position `json:"end,omitempty" ub:"end,omitempty"`
}

type Diagnostic struct {
	Code     string   `json:"code"               ub:"code"`
	Severity Severity `json:"severity"           ub:"severity"`
	Message  string   `json:"message"            ub:"message"`
	Hint     string   `json:"hint,omitempty"     ub:"hint,omitempty"`
	Path     string   `json:"path,omitempty"     ub:"path,omitempty"`
	Span     *Span    `json:"span,omitempty"     ub:"span,omitempty"`
}

type Reporter interface {
	Report(Diagnostic)
}

func Report(r Reporter, d Diagnostic) {
	if r != nil {
		r.Report(d)
	}
}

func Normalize(ds []Diagnostic) []Diagnostic {
	out := make([]Diagnostic, 0, len(ds))
	for _, d := range ds {
		if slices.ContainsFunc(out, func(existing Diagnostic) bool {
			return diagnosticEqual(existing, d)
		}) {
			continue
		}
		out = append(out, cloneDiagnostic(d))
	}
	slices.SortFunc(out, compareDiagnostic)
	return out
}

func Merge(groups ...[]Diagnostic) []Diagnostic {
	var merged []Diagnostic
	for _, group := range groups {
		merged = append(merged, group...)
	}
	return Normalize(merged)
}

func WriteText(out io.Writer, d Diagnostic) error {
	var prefix string
	switch d.Severity {
	case SeverityWarning:
		prefix = "warning: "
	case SeverityInfo:
		prefix = "notice: "
	case SeverityError:
		return fmt.Errorf("diagnostic: text rendering rejects error severity")
	default:
		return fmt.Errorf("diagnostic: unknown severity %q", d.Severity)
	}
	_, err := fmt.Fprintln(out, prefix+d.Message)
	return err
}

func cloneDiagnostic(d Diagnostic) Diagnostic {
	if d.Span == nil {
		return d
	}
	span := *d.Span
	if span.End != nil {
		end := *span.End
		span.End = &end
	}
	d.Span = &span
	return d
}

func diagnosticEqual(a, b Diagnostic) bool {
	return a.Code == b.Code &&
		a.Severity == b.Severity &&
		a.Message == b.Message &&
		a.Hint == b.Hint &&
		a.Path == b.Path &&
		spanEqual(a.Span, b.Span)
}

func spanEqual(a, b *Span) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Start != b.Start {
		return false
	}
	if a.End == nil || b.End == nil {
		return a.End == b.End
	}
	return *a.End == *b.End
}

func compareDiagnostic(a, b Diagnostic) int {
	if n := cmp.Compare(a.Path, b.Path); n != 0 {
		return n
	}
	if n := comparePresence(a.Span != nil, b.Span != nil, true); n != 0 {
		return n
	}
	if a.Span != nil {
		if n := comparePosition(a.Span.Start, b.Span.Start); n != 0 {
			return n
		}
		if n := comparePresence(a.Span.End != nil, b.Span.End != nil, false); n != 0 {
			return n
		}
		if a.Span.End != nil {
			if n := comparePosition(*a.Span.End, *b.Span.End); n != 0 {
				return n
			}
		}
	}
	if n := cmp.Compare(severityRank(a.Severity), severityRank(b.Severity)); n != 0 {
		return n
	}
	if n := cmp.Compare(a.Severity, b.Severity); n != 0 {
		return n
	}
	if n := cmp.Compare(a.Code, b.Code); n != 0 {
		return n
	}
	if n := cmp.Compare(a.Message, b.Message); n != 0 {
		return n
	}
	return cmp.Compare(a.Hint, b.Hint)
}

func comparePosition(a, b Position) int {
	if n := cmp.Compare(a.Offset, b.Offset); n != 0 {
		return n
	}
	if n := cmp.Compare(a.Line, b.Line); n != 0 {
		return n
	}
	return cmp.Compare(a.Column, b.Column)
}

func comparePresence(a, b bool, presentFirst bool) int {
	if a == b {
		return 0
	}
	if a == presentFirst {
		return -1
	}
	return 1
}

func severityRank(severity Severity) int {
	switch severity {
	case SeverityError:
		return 0
	case SeverityWarning:
		return 1
	case SeverityInfo:
		return 2
	default:
		return 3
	}
}
