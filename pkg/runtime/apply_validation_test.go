package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/typecheck"
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
	Spec  *testInputSpec

	counters *validationCounters
}

type testInputSpec struct {
	Capacity    map[string]string
	Validations *[]testPolicyValidation
}

type testPolicyValidation struct {
	Expression string
}

func TestUnknownEvaluatedOptionalListField(t *testing.T) {
	src := ubtest.ReadValidFixture(t, "testdata/ub/required-inputs", "unknown-list-field")
	for _, tc := range []struct {
		name      string
		selection string
		deferred  bool
	}{
		{name: "valid", selection: "valid"},
		{name: "fresh plan rejects unknown field", selection: "invalid"},
		{name: "apply rejects unknown field", deferred: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &validationCounters{}
			var seedRuns int64
			libs := validationModules(c)
			lib := libs["core"]
			lib.Actions = map[string]ActionRegistration{
				"seed": MakeActionWith[applyConstraintSeed, any, any](
					func() *applyConstraintSeed { return &applyConstraintSeed{runs: &seedRuns} },
				),
			}
			spec := typecheck.TObject([]typecheck.ObjectField{
				{Name: "capacity", Type: typecheck.TMap(typecheck.TString())},
				{Name: "validations", Optional: true, Type: typecheck.TList(
					typecheck.TObject([]typecheck.ObjectField{
						{Name: "expression", Type: typecheck.TString()},
					}),
				)},
			})
			lib.Schema = &LibrarySchema{Resources: map[string]*TypeSchema{
				"thing": {EvaluatedInputs: map[string]typecheck.Type{
					"name":  typecheck.TString(),
					"valid": typecheck.TBoolean(),
					"spec":  typecheck.TOptional(spec),
				}},
			}}
			exec := planTestExecutor(t, src, libs, newStateStore(t), state.FactoryInfo{})
			exec.Inputs = map[string]any{"selection": tc.selection, "deferred": tc.deferred}
			const unknown = `unknown field "spec.validations[0].reason"`
			if tc.selection == "invalid" {
				_, err := exec.Plan(context.Background())
				require.ErrorContains(t, err, unknown)
				require.Zero(t, seedRuns)
			} else {
				plan := savedConstraintPlan(t, exec)
				_, err := exec.ApplyPlan(context.Background(), plan)
				if tc.deferred {
					require.ErrorContains(t, err, unknown)
				} else {
					require.NoError(t, err)
					require.EqualValues(t, 1, c.creates)
					return
				}
			}
			require.Zero(t, c.creates)
			require.Zero(t, c.validations)
		})
	}
}

type validatingResourceOutput struct {
	ID    string
	Name  string
	Valid bool
}

func validateResourceInput(
	_ context.Context, input validatingResource, _ any,
) error {
	atomic.AddInt64(&input.counters.validations, 1)
	if !input.Valid {
		return errValidationFailed
	}
	return nil
}

func (r *validatingResource) Create(
	_ context.Context,
	_ any,
) (*validatingResourceOutput, error) {
	atomic.AddInt64(&r.counters.creates, 1)
	return &validatingResourceOutput{
		ID:    "resource-" + r.Name,
		Name:  r.Name,
		Valid: r.Valid,
	}, nil
}

func (r *validatingResource) Read(
	_ context.Context, _ any, prior Prior[validatingResource, *validatingResourceOutput, any],
) (*validatingResourceOutput, error) {
	if prior.Outputs == nil {
		return nil, ErrNotFound
	}
	return prior.Outputs, nil
}

func (r *validatingResource) Update(
	_ context.Context, _ any, prior Prior[validatingResource, *validatingResourceOutput, any],
) (*validatingResourceOutput, error) {
	atomic.AddInt64(&r.counters.updates, 1)
	output := prior.Outputs
	if output == nil {
		output = &validatingResourceOutput{}
	}
	output.Name = r.Name
	output.Valid = r.Valid
	return output, nil
}

func (r *validatingResource) Delete(
	_ context.Context, _ any, _ Prior[validatingResource, *validatingResourceOutput, any],
) error {
	atomic.AddInt64(&r.counters.deletes, 1)
	return nil
}

func validatingResourceDefinition() ResourceDefinition[
	validatingResource,
	*validatingResourceOutput,
	any,
] {
	return ResourceDefinition[validatingResource, *validatingResourceOutput, any]{
		SchemaVersion: 1,
		Validate:      validateResourceInput,
		Replace: Replacement[validatingResource, *validatingResourceOutput, any]{
			Fields: []AnyInputField[validatingResource]{
				InputField(func(input *validatingResource) *string { return &input.Name }),
			},
		},
	}
}

func validationModules(c *validationCounters) map[string]*Library {
	return map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"thing": MakeResourceWith[validatingResource, *validatingResourceOutput, any](
					validatingResourceDefinition(),
					func() *validatingResource { return &validatingResource{counters: c} },
				),
			},
		},
	}
}

func TestRequiredEvaluatedInputBeforeReplacement(t *testing.T) {
	c := &validationCounters{}
	var seedRuns int64
	libs := validationModules(c)
	lib := libs["core"]
	lib.Actions = map[string]ActionRegistration{
		"seed": MakeActionWith[applyConstraintSeed, any, any](
			func() *applyConstraintSeed { return &applyConstraintSeed{runs: &seedRuns} },
		),
	}
	lib.Schema = &LibrarySchema{Resources: map[string]*TypeSchema{
		"thing": {EvaluatedInputs: map[string]typecheck.Type{
			"name":  typecheck.TString(),
			"valid": typecheck.TBoolean(),
			"spec": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
				{Name: "capacity", Type: typecheck.TMap(typecheck.TString())},
			})),
		}},
	}}
	store := newStateStore(t)
	fixture := func(name string) string {
		return ubtest.ReadValidFixture(t, "testdata/ub/required-inputs", name)
	}
	ctx := context.Background()
	known := planTestExecutor(t, fixture("known"), libs, store, state.FactoryInfo{})
	known.Inputs = map[string]any{"name": "old", "selection": "good"}
	applyOnce(t, known)
	known.Inputs = map[string]any{"name": "new", "selection": "bad"}
	_, err := known.Plan(ctx)
	require.ErrorContains(t, err, `spec.capacity": required but not provided`)

	deferred := planTestExecutor(t, fixture("deferred"), libs, store, state.FactoryInfo{})
	plan := savedConstraintPlan(t, deferred)
	for _, step := range plan.Steps {
		if step.Address == "resource.consumer" {
			require.Equal(t, DecisionReplace, step.Decision)
			require.Equal(t, map[string][]string{"spec": {"action.seed.value"}},
				step.UnresolvedInputs)
		}
	}
	_, err = deferred.ApplyPlan(ctx, plan)
	require.ErrorContains(t, err, `spec.capacity": required but not provided`)
	require.EqualValues(t, 1, seedRuns)
	require.EqualValues(t, 1, c.creates)
	require.Zero(t, c.deletes)
	snapshot, err := store.Current()
	require.NoError(t, err)
	require.Equal(t, "old", snapshot.Find("resource.consumer").Inputs["name"])
}

func bindingValidationModules(oldC, newC *validationCounters) map[string]*Library {
	return map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"old": MakeResourceWith[validatingResource, *validatingResourceOutput, any](
					validatingResourceDefinition(),
					func() *validatingResource { return &validatingResource{counters: oldC} },
				),
				"new": MakeResourceWith[validatingResource, *validatingResourceOutput, any](
					validatingResourceDefinition(),
					func() *validatingResource { return &validatingResource{counters: newC} },
				),
			},
		},
	}
}

func TestResourceValidationPreventsCreate(t *testing.T) {
	var c validationCounters
	store := newStateStore(t)
	exec := validationExecutor(t, validationFixture(t, "create-invalid"), validationModules(&c), store)

	_, err := planAndApply(exec)
	require.ErrorIs(t, err, errValidationFailed)
	require.EqualValues(t, 1, c.validations)
	require.EqualValues(t, 0, c.creates)
}

func TestResourceValidationPreventsUpdate(t *testing.T) {
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

func TestResourceValidationRunsBeforeReplacementDelete(t *testing.T) {
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

func TestResourceValidationUsesDesiredReceiverBeforePriorBindingDelete(t *testing.T) {
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

func TestResourceValidationDoesNotRunForDestroy(t *testing.T) {
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
		Composite:     false,
		Category:      "resource",
		Binding:       &state.Binding{Alias: "core", Export: "thing"},
		SchemaVersion: 1,
		Inputs:        map[string]any{"name": name, "valid": valid},
		Outputs:       map[string]any{"id": "resource-" + name, "name": name, "valid": valid},
	}
}
