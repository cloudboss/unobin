package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/ubtest"
)

// panicResource panics in every CRUD method so the boundary guard can
// be exercised one operation at a time.
type panicResource struct {
	Name string
}

func (r *panicResource) SchemaVersion() int { return 1 }

func (r *panicResource) Create(context.Context, any) (any, error) {
	panic("boom in create")
}

func (r *panicResource) Read(context.Context, any, any) (any, error) {
	panic("boom in read")
}

func (r *panicResource) Update(context.Context, any, Prior[panicResource, any]) (any, error) {
	panic("boom in update")
}

func (r *panicResource) Delete(context.Context, any, any) error {
	panic("boom in delete")
}

func (r *panicResource) ReplaceFields() []string { return nil }

// createPanicResource panics only in Create and reports absence from
// Read, so a plan for a fresh resource reaches apply without tripping
// the guard during the plan-time read.
type createPanicResource struct {
	Name string
}

func (r *createPanicResource) SchemaVersion() int { return 1 }

func (r *createPanicResource) Create(context.Context, any) (any, error) {
	panic("boom in create")
}

func (r *createPanicResource) Read(context.Context, any, any) (any, error) {
	return nil, ErrNotFound
}

func (r *createPanicResource) Update(
	context.Context, any, Prior[createPanicResource, any],
) (any, error) {
	return nil, nil
}

func (r *createPanicResource) Delete(context.Context, any, any) error { return nil }

func (r *createPanicResource) ReplaceFields() []string { return nil }

// migratePanicResource reports a newer schema version than its recorded
// state and panics during migration, so the plan/refresh upgrade path
// can be exercised.
type migratePanicResource struct {
	Name string
}

func (r *migratePanicResource) SchemaVersion() int                       { return 2 }
func (r *migratePanicResource) Create(context.Context, any) (any, error) { return nil, nil }
func (r *migratePanicResource) Read(context.Context, any, any) (any, error) {
	return nil, nil
}

func (r *migratePanicResource) Update(
	context.Context, any, Prior[migratePanicResource, any],
) (any, error) {
	return nil, nil
}

func (r *migratePanicResource) Delete(context.Context, any, any) error { return nil }
func (r *migratePanicResource) ReplaceFields() []string                { return nil }

func (r *migratePanicResource) Migrate(int, MigrationState) (MigrationState, error) {
	panic("boom in migrate")
}

// schemaPanicResource creates cleanly but panics in SchemaVersion, an
// accessor the library-call guard does not cover. The panic escapes
// into the apply worker goroutine, where the backstop catches it.
type schemaPanicResource struct {
	Name string
}

func (r *schemaPanicResource) SchemaVersion() int { panic("boom in schema-version") }
func (r *schemaPanicResource) Create(context.Context, any) (any, error) {
	return map[string]any{"name": r.Name}, nil
}

func (r *schemaPanicResource) Read(context.Context, any, any) (any, error) {
	return nil, ErrNotFound
}

func (r *schemaPanicResource) Update(
	context.Context, any, Prior[schemaPanicResource, any],
) (any, error) {
	return nil, nil
}

func (r *schemaPanicResource) Delete(context.Context, any, any) error { return nil }
func (r *schemaPanicResource) ReplaceFields() []string                { return nil }

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

func TestResourceCreatePanicBecomesError(t *testing.T) {
	reg := MakeResource[panicResource, any, any]()
	_, err := reg.Create(context.Background(), reg.NewReceiver(), nil)
	pe := requirePanicError(t, err, "boom in create")
	require.False(t, pe.Core)
}

func TestResourceReadPanicBecomesError(t *testing.T) {
	reg := MakeResource[panicResource, any, any]()
	_, err := reg.Read(context.Background(), reg.NewReceiver(), nil, nil)
	_ = requirePanicError(t, err, "boom in read")
}

func TestResourceUpdatePanicBecomesError(t *testing.T) {
	reg := MakeResource[panicResource, any, any]()
	_, err := reg.Update(context.Background(), reg.NewReceiver(), nil, nil, nil, nil)
	_ = requirePanicError(t, err, "boom in update")
}

func TestResourceDeletePanicBecomesError(t *testing.T) {
	reg := MakeResource[panicResource, any, any]()
	err := reg.Delete(context.Background(), reg.NewReceiver(), nil, nil)
	_ = requirePanicError(t, err, "boom in delete")
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

// TestReadObservedPanicNamesLibrary covers the plan, refresh, reconcile,
// and destroy read paths at once: they all funnel resource reads through
// readObserved, which names the failing library from the alias in hand.
func TestReadObservedPanicNamesLibrary(t *testing.T) {
	reg := MakeResource[panicResource, any, any]()
	_, err := readObserved(context.Background(), reg, "boom", nil, nil, nil)
	pe := requirePanicError(t, err, "boom in read")
	require.Equal(t, "boom", pe.Library)
}

func TestMigrateEntryPanicNamesLibrary(t *testing.T) {
	reg := MakeResource[migratePanicResource, any, any]()
	_, err := migrateEntry(reg, "boom", 1, MigrationState{})
	pe := requirePanicError(t, err, "boom in migrate")
	require.Equal(t, "boom", pe.Library)
}

// TestApplyResourcePanicBecomesApplyError is the end-to-end case: a
// library that panics in Create no longer crashes the process. The
// panic is recovered at the boundary, flows back through the scheduler
// as an ApplyError, and unwraps to the PanicError, so the lock releases
// and the operator gets a re-plannable failure.
func TestApplyResourcePanicBecomesApplyError(t *testing.T) {
	libs := map[string]*Library{
		"boom": {
			Name: "boom",
			Resources: map[string]ResourceRegistration{
				"it": MakeResource[createPanicResource, any, any](),
			},
		},
	}
	dag, syntaxSource := syntaxDAGAndBody(t,
		ubtest.ReadValidFixture(t, "testdata/ub/panic", "resource"), libs)
	exec := &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        newStateStore(t),
		Factory:      state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism:  1,
	}
	_, err := planAndApply(exec)
	require.Error(t, err)

	var ae *ApplyError
	require.True(t, errors.As(err, &ae), "want *ApplyError, got %T", err)
	assert.Equal(t, "resource.x", ae.Address)

	var pe *PanicError
	require.True(t, errors.As(err, &pe), "ApplyError should unwrap to *PanicError")
	assert.Equal(t, "boom", pe.Library, "the scheduler names the failing node's library")
	assert.Contains(t, pe.Error(), "boom in create")
	assert.Contains(t, pe.Error(),
		"panic in the boom library while creating this resource")
}

// TestApplyRuntimePanicHitsBackstop proves the worker-goroutine backstop:
// a panic that escapes the library-call guards (here SchemaVersion, an
// unguarded accessor) is recovered in the apply worker instead of
// crashing, and is reported as an ApplyError attributed to unobin.
func TestApplyRuntimePanicHitsBackstop(t *testing.T) {
	libs := map[string]*Library{
		"boom": {
			Name: "boom",
			Resources: map[string]ResourceRegistration{
				"it": MakeResource[schemaPanicResource, any, any](),
			},
		},
	}
	dag, syntaxSource := syntaxDAGAndBody(t,
		ubtest.ReadValidFixture(t, "testdata/ub/panic", "resource"), libs)
	exec := &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        newStateStore(t),
		Factory:      state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
		Parallelism:  1,
	}
	_, err := planAndApply(exec)
	require.Error(t, err)

	var ae *ApplyError
	require.True(t, errors.As(err, &ae), "want *ApplyError, got %T", err)

	var pe *PanicError
	require.True(t, errors.As(err, &pe), "ApplyError should unwrap to *PanicError")
	require.True(t, pe.Core, "a panic outside the library calls is attributed to unobin")
	assert.Contains(t, pe.Error(), "boom in schema-version")
}
