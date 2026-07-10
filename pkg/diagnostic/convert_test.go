package diagnostic

import (
	"errors"
	"fmt"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang/parse"
)

type conversionGolden struct {
	Cases         []conversionCaseGolden `json:"cases"`
	ContextNil    bool                   `json:"context-nil"`
	ContextText   string                 `json:"context-text"`
	ContextUnwrap bool                   `json:"context-unwrap"`
	Deterministic bool                   `json:"deterministic"`
}

type conversionCaseGolden struct {
	Name        string       `json:"name"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

func TestFromErrorGolden(t *testing.T) {
	tests := []struct {
		name string
		err  func() error
		opts ConvertOptions
	}{
		{name: "nil", err: func() error { return nil }},
		{name: "generic default code", err: func() error { return errors.New("broken") }},
		{
			name: "generic selected code",
			err:  func() error { return errors.New("broken") },
			opts: ConvertOptions{DefaultCode: "unobin.io"},
		},
		{name: "unknown parse error", err: parseErr(parse.ErrUnknown, "unknown")},
		{name: "parse error", err: parseErr(parse.ErrParse, "parse")},
		{name: "lex error", err: parseErr(parse.ErrLex, "lex")},
		{name: "schema error", err: parseErr(parse.ErrSchema, "schema")},
		{name: "type error", err: parseErr(parse.ErrType, "type")},
		{name: "resolve error", err: parseErr(parse.ErrResolve, "resolve")},
		{
			name: "parse hint and mapped path",
			err: func() error {
				return &parse.Error{
					Kind: parse.ErrSchema,
					Pos: parse.Position{
						File: "absolute/source.ub", Line: 2, Column: 4, Offset: 8,
					},
					Msg:  "bad field",
					Hint: "remove it",
				}
			},
			opts: ConvertOptions{Path: func(path string) string { return "mapped/" + path }},
		},
		{
			name: "error list",
			err: func() error {
				list := parse.NewErrorList(0)
				list.Add(&parse.Error{
					Kind: parse.ErrType,
					Pos:  parse.Position{File: "b.ub", Line: 2, Column: 1, Offset: 3},
					Msg:  "second",
				})
				list.Add(&parse.Error{
					Kind: parse.ErrLex,
					Pos:  parse.Position{File: "a.ub", Line: 1, Column: 2, Offset: 1},
					Msg:  "first",
				})
				return list
			},
		},
		{
			name: "ordinary wrapper emits recognized descendant",
			err: func() error {
				return fmt.Errorf("outer detail: %w", &parse.Error{
					Kind: parse.ErrParse, Msg: "inner parse",
				})
			},
		},
		{
			name: "semantic context",
			err: func() error {
				return Context("import 'lib'", &parse.Error{
					Kind: parse.ErrSchema, Msg: "bad schema",
				})
			},
		},
		{
			name: "nested semantic context",
			err: func() error {
				return Context("factory", Context("import 'lib'", errors.New("unavailable")))
			},
		},
		{
			name: "joined parse and generic errors",
			err: func() error {
				return errors.Join(
					&parse.Error{Kind: parse.ErrParse, Msg: "parse failed"},
					errors.New("disk failed"),
				)
			},
		},
		{
			name: "shared descendant same context",
			err: func() error {
				leaf := errors.New("shared")
				return errors.Join(leaf, leaf)
			},
		},
		{
			name: "shared descendant distinct contexts",
			err: func() error {
				leaf := errors.New("shared")
				return errors.Join(Context("first", leaf), Context("second", leaf))
			},
		},
		{
			name: "self-referential error",
			err: func() error {
				cycle := &cycleError{message: "cycle"}
				cycle.next = cycle
				return cycle
			},
		},
		{
			name: "cycle beside ordinary leaf",
			err: func() error {
				cycle := &cycleError{message: "cycle"}
				cycle.next = cycle
				return &multiError{message: "root", children: []error{
					cycle,
					errors.New("leaf"),
				}}
			},
		},
		{
			name: "byte column from multibyte source",
			err: func() error {
				src := []byte("éx")
				pos := parse.NewSourceFile("utf8.ub", parse.LineStarts(src)).Position(2)
				return &parse.Error{Kind: parse.ErrLex, Pos: pos, Msg: "at x"}
			},
		},
	}

	result := conversionGolden{}
	for _, tc := range tests {
		result.Cases = append(result.Cases, conversionCaseGolden{
			Name:        tc.name,
			Diagnostics: FromError(tc.err(), tc.opts),
		})
	}

	cause := errors.New("cause")
	contextErr := Context("loading stack", cause)
	result.ContextNil = Context("unused", nil) == nil
	result.ContextText = contextErr.Error()
	result.ContextUnwrap = errors.Is(contextErr, cause)

	deterministicErr := errors.Join(
		Context("b", errors.New("leaf")),
		Context("a", errors.New("leaf")),
	)
	want := FromError(deterministicErr, ConvertOptions{})
	result.Deterministic = true
	for range 5 {
		if !errorsEqual(want, FromError(deterministicErr, ConvertOptions{})) {
			result.Deterministic = false
		}
	}

	requireDiagnosticGolden(t, "testdata/convert.json", result)
}

func parseErr(kind parse.ErrorKind, message string) func() error {
	return func() error {
		return &parse.Error{Kind: kind, Msg: message}
	}
}

func errorsEqual(a, b []Diagnostic) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !diagnosticEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

type cycleError struct {
	message string
	next    error
}

func (e *cycleError) Error() string {
	return e.message
}

func (e *cycleError) Unwrap() error {
	return e.next
}

type multiError struct {
	message  string
	children []error
}

func (e *multiError) Error() string {
	return e.message
}

func (e *multiError) Unwrap() []error {
	return e.children
}
