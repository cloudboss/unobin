package runtime

import (
	"errors"
	"fmt"
	"runtime/debug"
)

// PanicError reports that code the runtime called panicked. Every call
// into a library - a resource, action, data source, or function - is
// recovered at the boundary, so a defect there fails the step like any
// other error instead of crashing the process. A panic in unobin's own
// @core functions is recovered the same way but attributed to unobin
// rather than to a library.
//
// Op names what was running when the panic happened. Library is the
// import alias to blame, filled in where the failing node is known and
// left empty when the runtime cannot place it. Value is whatever was
// passed to panic. Stack is the goroutine stack captured at the moment
// of recovery, kept for a verbose report.
type PanicError struct {
	Op      string
	Library string
	Value   any
	Stack   []byte
	Core    bool
}

func (e *PanicError) Error() string {
	switch {
	case e.Core:
		return fmt.Sprintf("panic in unobin while %s: %v", e.Op, e.Value)
	case e.Library != "":
		return fmt.Sprintf("panic in the %s library while %s: %v", e.Library, e.Op, e.Value)
	default:
		return fmt.Sprintf("panic in the library while %s: %v", e.Op, e.Value)
	}
}

// guard runs fn and turns a panic into a PanicError so a defect in
// called code fails the operation rather than crashing the process. op
// names the operation; core marks a defect in unobin's own code rather
// than a library's.
func guard[R any](op string, core bool, fn func() (R, error)) (r R, err error) {
	defer func() {
		if v := recover(); v != nil {
			err = &PanicError{Op: op, Value: v, Stack: debug.Stack(), Core: core}
		}
	}()
	return fn()
}

// guardErr is guard for a call that returns only an error.
func guardErr(op string, core bool, fn func() error) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = &PanicError{Op: op, Value: v, Stack: debug.Stack(), Core: core}
		}
	}()
	return fn()
}

// blameLibrary records alias as the library a recovered panic came
// from, unless the panic is already attributed or belongs to unobin
// itself. It is a no-op for any error that is not a PanicError, so a
// caller can hand it the raw error from a library call.
func blameLibrary(err error, alias string) {
	var pe *PanicError
	if errors.As(err, &pe) && pe.Library == "" && !pe.Core {
		pe.Library = alias
	}
}
