package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type panicAction struct{}

func (a *panicAction) Run(context.Context, any) (any, error) {
	panic("boom in run")
}

type panicData struct{}

func (d *panicData) Read(context.Context, any) (any, error) {
	panic("boom in data read")
}

func requirePanicError(t *testing.T, err error, wantValue string) *PanicError {
	t.Helper()
	require.Error(t, err)
	var pe *PanicError
	require.True(t, errors.As(err, &pe), "want *PanicError, got %T", err)
	require.Contains(t, pe.Error(), wantValue)
	require.NotEmpty(t, pe.Stack, "a recovered panic should keep its stack")
	return pe
}

func TestPanicErrorMessage(t *testing.T) {
	named := &PanicError{Op: "creating this resource", Library: "boom", Value: "kaboom"}
	require.Equal(t,
		"panic in the boom library while creating this resource: kaboom", named.Error())
	unplaced := &PanicError{Op: "creating this resource", Value: "kaboom"}
	require.Equal(t,
		"panic in the library while creating this resource: kaboom", unplaced.Error())
	core := &PanicError{Op: "calling @core.length", Value: "kaboom", Core: true}
	require.Equal(t, "panic in unobin while calling @core.length: kaboom", core.Error())
}

func TestActionRunPanicBecomesError(t *testing.T) {
	reg := MakeAction[panicAction, any, any]()
	_, err := reg.Run(context.Background(), reg.NewReceiver(), nil)
	_ = requirePanicError(t, err, "boom in run")
}

func TestDataSourceReadPanicBecomesError(t *testing.T) {
	reg := MakeDataSource[panicData, any, any]()
	_, err := reg.Read(context.Background(), reg.NewReceiver(), nil)
	_ = requirePanicError(t, err, "boom in data read")
}

func TestLibraryFunctionPanicBecomesError(t *testing.T) {
	ctx := &EvalContext{Libraries: map[string]*Library{
		"boom": {
			Name: "boom",
			Functions: map[string]FunctionType{
				"explode": {Name: "explode", Func: func([]any) (any, error) { panic("boom in fn") }},
			},
		},
	}}
	_, err := Eval(parseValue(t, "boom.explode()"), ctx)
	pe := requirePanicError(t, err, "boom in fn")
	require.False(t, pe.Core, "a library function panic is attributed to the library")
	require.Equal(t, "boom", pe.Library, "the function names its own library")
}

func TestCoreFunctionPanicBecomesError(t *testing.T) {
	coreFunctions["test-panic"] = FunctionType{
		Name: "test-panic",
		Func: func([]any) (any, error) { panic("boom in core") },
	}
	defer delete(coreFunctions, "test-panic")
	_, err := evalCore(t, "@core.test-panic()", nil)
	pe := requirePanicError(t, err, "boom in core")
	require.True(t, pe.Core, "a @core panic is attributed to unobin")
}

func TestBlameLibrary(t *testing.T) {
	// Not a PanicError: no-op, must not panic.
	blameLibrary(errors.New("plain"), "boom")

	// Unplaced library panic takes the alias.
	fresh := &PanicError{Op: "reading this resource"}
	blameLibrary(fresh, "boom")
	require.Equal(t, "boom", fresh.Library)

	// Already attributed: not overwritten.
	already := &PanicError{Op: "x", Library: "boom"}
	blameLibrary(already, "other")
	require.Equal(t, "boom", already.Library)

	// A unobin (@core) panic is never blamed on a library.
	core := &PanicError{Op: "x", Core: true}
	blameLibrary(core, "boom")
	require.Empty(t, core.Library)
}
