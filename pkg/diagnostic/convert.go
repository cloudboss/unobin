package diagnostic

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang/parse"
)

type ConvertOptions struct {
	DefaultCode string
	Path        func(string) string
}

func FromError(err error, opts ConvertOptions) []Diagnostic {
	if err == nil {
		return []Diagnostic{}
	}
	if opts.DefaultCode == "" {
		opts.DefaultCode = "unobin.error"
	}
	w := errorWalker{
		opts:    opts,
		active:  map[errorIdentity]bool{},
		visited: map[errorVisit]bool{},
	}
	w.walk(err, "")
	return Normalize(w.diagnostics)
}

func Context(message string, err error) error {
	if err == nil {
		return nil
	}
	return &contextError{message: message, cause: err}
}

type contextError struct {
	message string
	cause   error
}

func (e *contextError) Error() string {
	return e.message + ": " + e.cause.Error()
}

func (e *contextError) Unwrap() error {
	return e.cause
}

type errorIdentity struct {
	typ string
	id  string
}

type errorVisit struct {
	err     errorIdentity
	context string
}

type errorWalker struct {
	opts        ConvertOptions
	diagnostics []Diagnostic
	active      map[errorIdentity]bool
	visited     map[errorVisit]bool
}

func (w *errorWalker) walk(err error, context string) bool {
	if err == nil {
		return false
	}
	identity := identifyError(err)
	visit := errorVisit{err: identity, context: context}
	if produced, ok := w.visited[visit]; ok {
		return produced
	}
	if w.active[identity] {
		return false
	}
	w.active[identity] = true
	defer delete(w.active, identity)

	var produced bool
	switch e := err.(type) {
	case *parse.Error:
		w.diagnostics = append(w.diagnostics, w.fromParseError(e, context))
		produced = true
	case *parse.ErrorList:
		for _, parseErr := range e.Errors() {
			w.diagnostics = append(w.diagnostics, w.fromParseError(parseErr, context))
			produced = true
		}
	case *contextError:
		produced = w.walk(e.cause, joinContext(context, e.message))
		if !produced {
			w.diagnostics = append(w.diagnostics, w.generic(err, context))
			produced = true
		}
	default:
		produced = w.walkChildren(err, context)
		if !produced {
			w.diagnostics = append(w.diagnostics, w.generic(err, context))
			produced = true
		}
	}
	w.visited[visit] = produced
	return produced
}

func (w *errorWalker) walkChildren(err error, context string) bool {
	if many, ok := err.(interface{ Unwrap() []error }); ok {
		var produced bool
		for _, child := range many.Unwrap() {
			if w.walk(child, context) {
				produced = true
			}
		}
		return produced
	}
	if one, ok := err.(interface{ Unwrap() error }); ok {
		return w.walk(one.Unwrap(), context)
	}
	return false
}

func (w *errorWalker) generic(err error, context string) Diagnostic {
	return Diagnostic{
		Code:     w.opts.DefaultCode,
		Severity: SeverityError,
		Message:  joinContext(context, err.Error()),
	}
}

func (w *errorWalker) fromParseError(err *parse.Error, context string) Diagnostic {
	d := Diagnostic{
		Code:     parseCode(err.Kind),
		Severity: SeverityError,
		Message:  joinContext(context, err.Msg),
		Hint:     err.Hint,
		Path:     err.Pos.File,
	}
	if d.Path != "" && w.opts.Path != nil {
		d.Path = w.opts.Path(d.Path)
	}
	if !err.Pos.IsZero() {
		d.Span = &Span{Start: Position{
			Line: err.Pos.Line, Column: err.Pos.Column, Offset: err.Pos.Offset,
		}}
	}
	return d
}

func parseCode(kind parse.ErrorKind) string {
	switch kind {
	case parse.ErrParse:
		return "unobin.parse"
	case parse.ErrLex:
		return "unobin.lex"
	case parse.ErrSchema:
		return "unobin.schema"
	case parse.ErrType:
		return "unobin.type"
	case parse.ErrResolve:
		return "unobin.resolve"
	default:
		return "unobin.error"
	}
}

func joinContext(context, message string) string {
	if context == "" {
		return message
	}
	if message == "" {
		return context
	}
	return context + ": " + message
}

func identifyError(err error) errorIdentity {
	value := reflect.ValueOf(err)
	typ := value.Type().String()
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer,
		reflect.Slice, reflect.UnsafePointer:
		return errorIdentity{typ: typ, id: fmt.Sprintf("%p", err)}
	}
	return errorIdentity{typ: typ, id: strings.TrimSpace(fmt.Sprintf("%#v", err))}
}
