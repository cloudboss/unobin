package runtime

import (
	"context"
	"math"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

type resolvedInputCalls struct {
	creates atomic.Int64
	reads   atomic.Int64
	port    atomic.Int32
	seeds   atomic.Int64
	actions atomic.Int64
}

type resolvedInputResource struct {
	Data         map[string]string
	FieldManager string
	Force        bool
	Port         int32
	calls        *resolvedInputCalls
}

type resolvedInputOutput struct {
	ID   string
	Port int32
}

func (r *resolvedInputResource) Create(context.Context, any) (*resolvedInputOutput, error) {
	r.calls.creates.Add(1)
	r.calls.port.Store(r.Port)
	return &resolvedInputOutput{ID: "example", Port: r.Port}, nil
}

func (r *resolvedInputResource) Read(
	_ context.Context, _ any,
	prior Prior[resolvedInputResource, *resolvedInputOutput, any],
) (*resolvedInputOutput, error) {
	r.calls.reads.Add(1)
	return prior.Outputs, nil
}

func (r *resolvedInputResource) Update(
	ctx context.Context, config any, _ Prior[resolvedInputResource, *resolvedInputOutput, any],
) (*resolvedInputOutput, error) {
	return r.Create(ctx, config)
}

func (r *resolvedInputResource) Delete(
	context.Context, any, Prior[resolvedInputResource, *resolvedInputOutput, any],
) error {
	return nil
}

func (r *resolvedInputResource) Run(context.Context, any) (*resolvedInputOutput, error) {
	r.calls.actions.Add(1)
	r.calls.port.Store(r.Port)
	return &resolvedInputOutput{ID: "action", Port: r.Port}, nil
}

type resolvedInputSeed struct {
	Selection string
	calls     *resolvedInputCalls
}

func (s *resolvedInputSeed) Run(context.Context, any) (any, error) {
	s.calls.seeds.Add(1)
	return map[string]any{"value": s.Selection}, nil
}

type resolvedInputData struct {
	Port  int32
	calls *resolvedInputCalls
}

func (d *resolvedInputData) Read(context.Context, any) (*resolvedInputOutput, error) {
	d.calls.reads.Add(1)
	d.calls.port.Store(d.Port)
	return &resolvedInputOutput{ID: "fallback", Port: d.Port}, nil
}

func resolvedInputLibrary(calls *resolvedInputCalls) map[string]*Library {
	secret := &TypeSchema{EvaluatedInputs: map[string]typecheck.Type{
		"data":          typecheck.TMap(typecheck.TString()),
		"field-manager": typecheck.TString(),
		"force":         typecheck.TBoolean(),
	}}
	endpoint := &TypeSchema{EvaluatedInputs: map[string]typecheck.Type{
		"port": typecheck.TInteger(),
	}}
	resource := MakeResourceWith[resolvedInputResource, *resolvedInputOutput, any](
		testResourceDefinition[resolvedInputResource, *resolvedInputOutput, any](),
		func() *resolvedInputResource { return &resolvedInputResource{calls: calls} },
	)
	return map[string]*Library{"core": {
		Name: "core",
		Resources: map[string]ResourceRegistration{
			"secret": resource, "endpoint": resource,
		},
		DataSources: map[string]DataSourceRegistration{
			"endpoint": MakeDataSourceWith[resolvedInputData, *resolvedInputOutput, any](
				func() *resolvedInputData { return &resolvedInputData{calls: calls} },
			),
		},
		Actions: map[string]ActionRegistration{
			"endpoint": MakeActionWith[resolvedInputResource, *resolvedInputOutput, any](
				func() *resolvedInputResource { return &resolvedInputResource{calls: calls} },
			),
			"seed": MakeActionWith[resolvedInputSeed, any, any](
				func() *resolvedInputSeed { return &resolvedInputSeed{calls: calls} },
			),
		},
		Schema: &LibrarySchema{
			Resources: map[string]*TypeSchema{
				"secret": secret, "endpoint": endpoint,
			},
			DataSources: map[string]*TypeSchema{"endpoint": endpoint},
			Actions:     map[string]*TypeSchema{"endpoint": endpoint},
		},
	}}
}

func resolvedInputExecutor(
	t *testing.T, fixture string, calls *resolvedInputCalls,
) *Executor {
	t.Helper()
	src := ubtest.ReadValidFixture(t, "testdata/ub/resolved-inputs", fixture)
	return planTestExecutor(t, src, resolvedInputLibrary(calls), newStateStore(t),
		state.FactoryInfo{})
}

func TestResolvedInputTypesRejectedDuringPlan(t *testing.T) {
	for _, selection := range []string{
		"list-data", "number-entry", "number-manager", "string-force",
	} {
		t.Run(selection, func(t *testing.T) {
			calls := &resolvedInputCalls{}
			exec := resolvedInputExecutor(t, "secret", calls)
			exec.Inputs = map[string]any{"selection": selection, "deferred": false}
			_, err := exec.Plan(context.Background())
			require.ErrorContains(t, err, "expected")
			require.Zero(t, calls.creates.Load())
		})
	}
}

func TestResolvedIntegerFractionRejectedDuringPlan(t *testing.T) {
	for _, fixture := range []string{"resource-port", "data-port", "action-port"} {
		t.Run(fixture, func(t *testing.T) {
			calls := &resolvedInputCalls{}
			exec := resolvedInputExecutor(t, fixture, calls)
			exec.Inputs = map[string]any{"port": 80.5, "deferred": false}
			_, err := exec.Plan(context.Background())
			require.ErrorContains(t, err, `field "port": expected integer`)
			require.Zero(t, calls.creates.Load())
			require.Zero(t, calls.reads.Load())
			require.Zero(t, calls.actions.Load())
		})
	}
}

func TestResolvedIntegerFractionRejectedForExistingResource(t *testing.T) {
	calls := &resolvedInputCalls{}
	exec := resolvedInputExecutor(t, "resource-port", calls)
	exec.Inputs = map[string]any{"port": int64(80), "deferred": false}
	applyOnce(t, exec)
	exec.Inputs["port"] = 80.5
	_, err := exec.Plan(context.Background())
	require.ErrorContains(t, err, `field "port": expected integer`)
	require.EqualValues(t, 1, calls.creates.Load())
}

func TestSavedNoOpRejectsInvalidResolvedInteger(t *testing.T) {
	calls := &resolvedInputCalls{}
	exec := resolvedInputExecutor(t, "resource-port", calls)
	exec.Inputs = map[string]any{"port": int64(80), "deferred": false}
	applyOnce(t, exec)
	plan := savedConstraintPlan(t, exec)
	for i := range plan.Steps {
		step := &plan.Steps[i]
		if step.Address == "resource.example" {
			require.Equal(t, DecisionNoOp, step.Decision)
			step.Inputs["port"] = 80.5
		}
	}
	plan.Inputs["port"] = 80.5
	exec.Inputs = nil
	_, err := exec.ApplyPlan(context.Background(), plan)
	require.ErrorContains(t, err, `field "port": expected integer`)
	require.EqualValues(t, 1, calls.creates.Load())
}

func TestSavedApplyRejectsIntegerOverflowBeforeCreate(t *testing.T) {
	calls := &resolvedInputCalls{}
	exec := resolvedInputExecutor(t, "resource-port", calls)
	exec.Inputs = map[string]any{"port": int64(math.MaxInt32) + 1, "deferred": false}
	plan := savedConstraintPlan(t, exec)
	_, err := exec.ApplyPlan(context.Background(), plan)
	require.ErrorContains(t, err, "integer overflows int32")
	require.Zero(t, calls.creates.Load())
}

func TestDeferredInputTypesRejectedDuringSavedApply(t *testing.T) {
	for _, selection := range []string{
		"list-data", "number-entry", "number-manager", "string-force",
	} {
		t.Run(selection, func(t *testing.T) {
			calls := &resolvedInputCalls{}
			exec := resolvedInputExecutor(t, "secret", calls)
			exec.Inputs = map[string]any{"selection": selection, "deferred": true}
			plan := savedConstraintPlan(t, exec)
			require.Zero(t, calls.seeds.Load())
			_, err := exec.ApplyPlan(context.Background(), plan)
			require.ErrorContains(t, err, "expected")
			require.EqualValues(t, 1, calls.seeds.Load())
			require.Zero(t, calls.creates.Load())
		})
	}
}

func TestDeferredIntegerFractionRejectedDuringSavedApply(t *testing.T) {
	for _, fixture := range []string{"resource-port", "data-port", "action-port"} {
		t.Run(fixture, func(t *testing.T) {
			calls := &resolvedInputCalls{}
			exec := resolvedInputExecutor(t, fixture, calls)
			exec.Inputs = map[string]any{"port": 1.75, "deferred": true}
			plan := savedConstraintPlan(t, exec)
			require.Zero(t, calls.seeds.Load())
			_, err := exec.ApplyPlan(context.Background(), plan)
			require.ErrorContains(t, err, `field "port": expected integer`)
			require.EqualValues(t, 1, calls.seeds.Load())
			require.Zero(t, calls.creates.Load())
			require.Zero(t, calls.reads.Load())
			require.Zero(t, calls.actions.Load())
		})
	}
}

func TestResolvedInputsAcceptValidValues(t *testing.T) {
	for _, fixture := range []string{"secret", "resource-port", "data-port", "action-port"} {
		t.Run(fixture, func(t *testing.T) {
			calls := &resolvedInputCalls{}
			exec := resolvedInputExecutor(t, fixture, calls)
			exec.Inputs = map[string]any{
				"selection": "valid", "port": int64(80), "deferred": false,
			}
			plan := savedConstraintPlan(t, exec)
			_, err := exec.ApplyPlan(context.Background(), plan)
			require.NoError(t, err)
			switch fixture {
			case "data-port":
				require.EqualValues(t, 1, calls.reads.Load())
			case "action-port":
				require.EqualValues(t, 1, calls.actions.Load())
			default:
				require.EqualValues(t, 1, calls.creates.Load())
			}
			if fixture != "secret" {
				require.EqualValues(t, 80, calls.port.Load())
			}
		})
	}
}
