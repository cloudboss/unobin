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

type validatingResourceOutput struct {
	ID    string
	Name  string
	Valid bool
}

func validatingResourceDefinition(counters *validationCounters) ResourceDefinition[
	validatingResource,
	*validatingResourceOutput,
	any,
] {
	return ResourceDefinition[validatingResource, *validatingResourceOutput, any]{
		Validate: func(_ context.Context, inputs validatingResource, _ any) error {
			atomic.AddInt64(&counters.validations, 1)
			if !inputs.Valid {
				return errValidationFailed
			}
			return nil
		},
		SchemaVersion: 1,
		Identity: ResourceIdentity[validatingResource, *validatingResourceOutput]{
			Version: 1,
			Scope:   IdentityConfiguration,
		},
		Replacement: ReplacementRules[validatingResource, *validatingResourceOutput]{
			Inputs: []ReplacementRule[validatingResource]{
				ReplaceWhenChanged(InputField(func(v *validatingResource) *string { return &v.Name })),
			},
		},
	}
}

func (r *validatingResource) Create(_ context.Context, _ any) (*validatingResourceOutput, error) {
	atomic.AddInt64(&r.counters.creates, 1)
	return &validatingResourceOutput{ID: "resource-" + r.Name, Name: r.Name, Valid: r.Valid}, nil
}

func (r *validatingResource) Read(
	_ context.Context,
	_ any,
	prior *validatingResourceOutput,
) (*validatingResourceOutput, error) {
	if prior == nil {
		return nil, ErrNotFound
	}
	return prior, nil
}

func (r *validatingResource) Update(
	_ context.Context, _ any, prior Prior[validatingResource, *validatingResourceOutput],
) (*validatingResourceOutput, error) {
	atomic.AddInt64(&r.counters.updates, 1)
	out := &validatingResourceOutput{}
	if prior.Outputs != nil {
		*out = *prior.Outputs
	}
	out.Name = r.Name
	out.Valid = r.Valid
	return out, nil
}

func (r *validatingResource) Delete(_ context.Context, _ any, _ *validatingResourceOutput) error {
	atomic.AddInt64(&r.counters.deletes, 1)
	return nil
}

func validationModules(c *validationCounters) map[string]*Library {
	return map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"thing": MakeResourceWith[validatingResource, *validatingResourceOutput, any](
					validatingResourceDefinition(c),
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
				"old": MakeResourceWith[validatingResource, *validatingResourceOutput, any](
					validatingResourceDefinition(oldC),
					func() *validatingResource { return &validatingResource{counters: oldC} },
				),
				"new": MakeResourceWith[validatingResource, *validatingResourceOutput, any](
					validatingResourceDefinition(newC),
					func() *validatingResource { return &validatingResource{counters: newC} },
				),
			},
		},
	}
}

func TestDefinitionValidationPreventsCreate(t *testing.T) {
	var c validationCounters
	store := newStateStore(t)
	exec := validationExecutor(t, validationFixture(t, "create-invalid"), validationModules(&c), store)

	_, err := planAndApply(exec)
	require.ErrorIs(t, err, errValidationFailed)
	require.EqualValues(t, 1, c.validations)
	require.EqualValues(t, 0, c.creates)
}

func TestDefinitionValidationPreventsUpdate(t *testing.T) {
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

func TestDefinitionValidationRunsBeforeReplacementDelete(t *testing.T) {
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

func TestDefinitionValidationUsesDesiredReceiverBeforePriorBindingDelete(t *testing.T) {
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

func TestDefinitionValidationDoesNotRunForDestroy(t *testing.T) {
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
