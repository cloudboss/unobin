package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/require"
)

var errValidationFailed = errors.New("validation failed")

var _ InputValidator[any] = (*validatingResource)(nil)

type validationCounters struct {
	creates     int64
	updates     int64
	deletes     int64
	validations int64
}

type validatingResource struct {
	Name  string
	Valid bool

	counters *validationCounters
}

func (r *validatingResource) SchemaVersion() int { return 1 }

func (r *validatingResource) ValidateInputs(_ context.Context, _ any) error {
	atomic.AddInt64(&r.counters.validations, 1)
	if !r.Valid {
		return errValidationFailed
	}
	return nil
}

func (r *validatingResource) Create(_ context.Context, _ any) (any, error) {
	atomic.AddInt64(&r.counters.creates, 1)
	return map[string]any{"id": "resource-" + r.Name, "name": r.Name, "valid": r.Valid}, nil
}

func (r *validatingResource) Read(_ context.Context, _ any, prior any) (any, error) {
	if prior == nil {
		return nil, ErrNotFound
	}
	return prior, nil
}

func (r *validatingResource) Update(
	_ context.Context, _ any, prior Prior[validatingResource, any],
) (any, error) {
	atomic.AddInt64(&r.counters.updates, 1)
	out, _ := prior.Outputs.(map[string]any)
	if out == nil {
		out = map[string]any{}
	}
	out["name"] = r.Name
	out["valid"] = r.Valid
	return out, nil
}

func (r *validatingResource) Delete(_ context.Context, _ any, _ any) error {
	atomic.AddInt64(&r.counters.deletes, 1)
	return nil
}

func (r *validatingResource) ReplaceFields() []string { return []string{"name"} }

func validationModules(c *validationCounters) map[string]*Library {
	return map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"thing": MakeResourceWith[validatingResource, any, any](
					func() *validatingResource { return &validatingResource{counters: c} },
				),
			},
		},
	}
}

func bindingValidationModules(oldC, newC *validationCounters) map[string]*Library {
	return map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"old": MakeResourceWith[validatingResource, any, any](
					func() *validatingResource { return &validatingResource{counters: oldC} },
				),
				"new": MakeResourceWith[validatingResource, any, any](
					func() *validatingResource { return &validatingResource{counters: newC} },
				),
			},
		},
	}
}

func TestInputValidatorPreventsCreate(t *testing.T) {
	var c validationCounters
	store := newStateStore(t)
	exec := validationExecutor(t, validationFixture(t, "create-invalid"), validationModules(&c), store)

	_, err := planAndApply(exec)
	require.ErrorIs(t, err, errValidationFailed)
	require.EqualValues(t, 1, c.validations)
	require.EqualValues(t, 0, c.creates)
}

func TestInputValidatorPreventsUpdate(t *testing.T) {
	var c validationCounters
	store := newStateStore(t)
	libs := validationModules(&c)
	applyOnce(t, validationExecutor(t, validationFixture(t, "replace-valid"), libs, store))

	exec := validationExecutor(t, validationFixture(t, "update-invalid"), libs, store)
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, DecisionUpdate, findStep(t, plan, "resource.one").Decision)

	_, err = planAndApplyExisting(exec, plan)
	require.ErrorIs(t, err, errValidationFailed)
	require.EqualValues(t, 2, c.validations)
	require.EqualValues(t, 1, c.creates)
	require.EqualValues(t, 0, c.updates)
}

func TestInputValidatorRunsBeforeReplacementDelete(t *testing.T) {
	var c validationCounters
	store := newStateStore(t)
	libs := validationModules(&c)
	applyOnce(t, validationExecutor(t, validationFixture(t, "replace-valid"), libs, store))

	exec := validationExecutor(t, validationFixture(t, "replace-invalid"), libs, store)
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, DecisionReplace, findStep(t, plan, "resource.one").Decision)

	_, err = planAndApplyExisting(exec, plan)
	require.ErrorIs(t, err, errValidationFailed)
	require.EqualValues(t, 2, c.validations)
	require.EqualValues(t, 1, c.creates)
	require.EqualValues(t, 0, c.deletes)
}

func TestInputValidatorUsesDesiredReceiverBeforePriorBindingDelete(t *testing.T) {
	oldC := &validationCounters{}
	newC := &validationCounters{}
	store := newStateStore(t)
	libs := bindingValidationModules(oldC, newC)
	applyOnce(t, validationExecutor(t, validationFixture(t, "binding-valid"), libs, store))

	exec := validationExecutor(t, validationFixture(t, "binding-invalid"), libs, store)
	plan, err := exec.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, DecisionReplace, findStep(t, plan, "resource.one").Decision)

	_, err = planAndApplyExisting(exec, plan)
	require.ErrorIs(t, err, errValidationFailed)
	require.EqualValues(t, 1, oldC.validations)
	require.EqualValues(t, 1, oldC.creates)
	require.EqualValues(t, 0, oldC.deletes)
	require.EqualValues(t, 1, newC.validations)
	require.EqualValues(t, 0, newC.creates)
}

func TestInputValidatorDoesNotRunForDestroy(t *testing.T) {
	var c validationCounters
	store := newStateStore(t)
	seedIncrementalState(t, store, validationEntry("resource.one", "alpha", false))
	exec := validationExecutor(t, validationFixture(t, "empty"), validationModules(&c), store)

	_, err := planAndApply(exec)
	require.NoError(t, err)
	require.EqualValues(t, 0, c.validations)
	require.EqualValues(t, 1, c.deletes)
}

func validationExecutor(
	t *testing.T,
	src string,
	libs map[string]*Library,
	store state.Backend,
) *Executor {
	t.Helper()
	stack := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	return applyPlanTestExecutor(t, src, libs, store, stack)
}

func validationFixture(t testing.TB, name string) string {
	t.Helper()
	return applyPlanFixture(t, "input-validation-"+name)
}

func validationEntry(address, name string, valid bool) *state.Entry {
	return &state.Entry{
		Address:       address,
		Type:          state.EntryLeaf,
		Category:      "resource",
		Binding:       &state.Binding{Alias: "core", Export: "thing"},
		SchemaVersion: 1,
		Inputs:        map[string]any{"name": name, "valid": valid},
		Outputs:       map[string]any{"id": "resource-" + name, "name": name, "valid": valid},
	}
}
