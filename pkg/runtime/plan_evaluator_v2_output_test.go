package runtime

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestPlanEvaluationV2OutputRequestRecordsValues(t *testing.T) {
	libraries := map[string]*Library{
		"cloud": {
			LibraryPath: "example.com/cloud",
			Schema: &LibrarySchema{Resources: map[string]*TypeSchema{
				"server": {SensitiveOutputs: []string{"token"}},
			}},
		},
	}
	src := ubtest.ReadValidFixture(t, "testdata/ub/plan-evaluator-v2/output", "values")
	dag, source := syntaxDAGAndBody(t, src, libraries)
	executor := &Executor{DAG: dag, SyntaxSource: source, Libraries: libraries}
	pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
	snapshot := newPlanEvaluationV2Snapshot(t)
	evaluation, err := executor.preparePlanEvaluationV2(
		operationObject(t, map[string]EncodedValue{"token": StringValue("input-secret")}),
		snapshot, pass,
	)
	require.NoError(t, err)
	id, err := PendingEncodedValue([]string{"resource.main.id"})
	require.NoError(t, err)
	token, err := PendingEncodedValue([]string{"resource.main.token"})
	require.NoError(t, err)
	credentials, err := ListValue([]EncodedValue{StringValue("input-secret"), token})
	require.NoError(t, err)
	details, err := MapValue(map[string]EncodedValue{
		"name": StringValue("server"), "id": id, "credentials": credentials,
	})
	require.NoError(t, err)
	number, err := NumberValue(1.5)
	require.NoError(t, err)
	empty, err := ListValue([]EncodedValue{})
	require.NoError(t, err)
	tests := []struct {
		name      string
		value     EncodedValue
		sensitive bool
		dependsOn []string
	}{
		{"integer", IntegerValue(9007199254740993), false, []string{}},
		{"number", number, false, []string{}},
		{"boolean", BooleanValue(true), false, []string{}},
		{"null", NullValue(), false, []string{}},
		{"empty", empty, false, []string{}},
		{"public", StringValue("server"), false, []string{}},
		{"declared", StringValue("secret"), true, []string{}},
		{"token", StringValue("input-secret"), true, []string{}},
		{"provider", token, true, []string{"resource.main"}},
		{"id", id, false, []string{"resource.main"}},
		{"details", details, true, []string{"resource.main"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			address := "output." + test.name
			request, err := executor.planEvaluationV2OutputRequest(evaluation, dag.Nodes[address])
			require.NoError(t, err)
			require.Equal(t, address, request.Address)
			require.Equal(t, NodeOutput, request.Kind)
			require.Equal(t, test.dependsOn, request.DependsOn)
			step, err := request.Plan(context.Background(), pass)
			require.NoError(t, err)
			require.NoError(t, validatePlannedStepV2(request, step))
			require.Equal(t, &OutputPlanOperation{
				Decision: DecisionEval, Value: test.value, Sensitive: test.sensitive,
			}, step.Operation.Output)
		})
	}
	require.Equal(t, snapshot, evaluation.prior)
}

func TestPlanEvaluationV2OutputRequestUsesResourcePlanningResults(t *testing.T) {
	for _, changed := range []bool{false, true} {
		name := "unchanged"
		if changed {
			name = "updated"
		}
		t.Run(name, func(t *testing.T) {
			capture := &registeredPlanningCapture{}
			resourceRequest := planEvaluationV2ResourceStepRequest(t, capture)
			if changed {
				resourceRequest.Desired.Inputs = operationObject(t, map[string]EncodedValue{
					"name": StringValue("server"), "size": IntegerValue(2),
				})
			}
			libraries := map[string]*Library{"cloud": {LibraryPath: "example.com/cloud"}}
			src := ubtest.ReadValidFixture(t, "testdata/ub/plan-evaluator-v2/output", "values")
			dag, source := syntaxDAGAndBody(t, src, libraries)
			executor := &Executor{DAG: dag, SyntaxSource: source, Libraries: libraries}
			snapshot := newPlanEvaluationV2Snapshot(t)
			addPlanEvaluationV2Entry(t, snapshot, state.StateEntryV2{
				Address: "resource.main", Kind: state.StateResource,
				Payload: state.StatePayload{
					Kind:     state.StateResource,
					Resource: &state.ResourceStatePayload{Target: *resourceRequest.Prior},
				},
			})
			node := dag.Nodes["output.id"]
			node.Body = parseValue(t, "resource.main.value")
			request := validPlanFileV2PlanningRequest(t)
			passes := 0
			request.Evaluate = func(pass *planningPassState) ([]planStepV2Request, error) {
				passes++
				evaluation, err := executor.preparePlanEvaluationV2(request.Inputs, snapshot, pass)
				require.NoError(t, err)
				evaluation.run.eval.Resources["main"] = map[string]any{"value": "stale"}
				output, err := executor.planEvaluationV2OutputRequest(evaluation, node)
				require.NoError(t, err)
				return []planStepV2Request{
					{
						Address: "resource.main", Kind: NodeResource, DependsOn: []string{},
						Plan: func(ctx context.Context, pass *planningPassState) (*PlanStepV2, error) {
							return executor.planEvaluationV2ResourceStep(ctx, evaluation, pass, resourceRequest)
						},
					},
					output,
				}, nil
			}
			plan, err := planPlanFileV2(context.Background(), request)
			require.NoError(t, err)
			require.NoError(t, plan.Validate())
			require.Len(t, plan.Steps, 2)
			want := StringValue("server")
			if changed {
				want, err = PendingEncodedValue([]string{"resource.main.value"})
				require.NoError(t, err)
				require.Equal(t, 2, passes)
			} else {
				require.Equal(t, 1, passes)
			}
			require.Equal(t, &OutputPlanOperation{
				Decision: DecisionEval, Value: want,
			}, plan.Steps[1].Operation.Output)
			require.Equal(t, []string{"server:same"}, capture.reads)
			encoded, err := encodePlanFileV2(plan)
			require.NoError(t, err)
			decoded, err := decodePlanFileV2(encoded)
			require.NoError(t, err)
			require.Equal(t, plan, decoded)
		})
	}
}

func TestPlanEvaluationV2OutputRequestDetachesMetadataAndValues(t *testing.T) {
	dag := newDAG(map[string][]string{
		"output.result": {"resource.second", "input.token", "resource.first"},
	})
	executor := &Executor{DAG: dag}
	pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
	evaluation, err := executor.preparePlanEvaluationV2(
		operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t), pass,
	)
	require.NoError(t, err)
	values := map[string]any{"id": "original"}
	evaluation.run.eval.Resources["first"] = values
	node := &Node{
		Address: "output.result", Kind: NodeOutput, Body: parseValue(t, "resource.first"),
	}
	request, err := executor.planEvaluationV2OutputRequest(evaluation, node)
	require.NoError(t, err)
	require.Equal(t, []string{"resource.first", "resource.second"}, request.DependsOn)
	request.DependsOn[0] = "resource.changed"
	dag.Edges[node.Address][0] = "resource.changed"
	node.Address = "output.changed"
	node.Body = parseValue(t, "'changed'")
	step, err := request.Plan(context.Background(), pass)
	require.NoError(t, err)
	require.Equal(t, "output.result", step.Address)
	require.Equal(t, []string{"resource.first", "resource.second"}, step.DependsOn)
	values["id"] = "changed"
	want, err := MapValue(map[string]EncodedValue{"id": StringValue("original")})
	require.NoError(t, err)
	require.Equal(t, want, step.Operation.Output.Value)
}

func TestPlanEvaluationV2OutputRequestRejectsInvalidSetup(t *testing.T) {
	tests := []struct {
		name   string
		change func(**Executor, **planEvaluationV2, **Node)
		want   string
	}{
		{"executor", func(e **Executor, _ **planEvaluationV2, _ **Node) { *e = nil },
			"executor is required"},
		{"graph", func(e **Executor, _ **planEvaluationV2, _ **Node) { (*e).DAG = nil },
			"dependency graph is required"},
		{"evaluation", func(_ **Executor, e **planEvaluationV2, _ **Node) { *e = nil },
			"version 2 plan evaluation is required"},
		{"scope", func(_ **Executor, e **planEvaluationV2, _ **Node) { (*e).run.eval = nil },
			"version 2 plan evaluation is required"},
		{"node", func(_ **Executor, _ **planEvaluationV2, n **Node) { *n = nil },
			"output node is required"},
		{"kind", func(_ **Executor, _ **planEvaluationV2, n **Node) { (*n).Kind = NodeResource },
			"node must be a root output"},
		{"nested", func(_ **Executor, _ **planEvaluationV2, n **Node) { (*n).Composite = "resource.box" },
			"node must be a root output"},
		{"address", func(_ **Executor, _ **planEvaluationV2, n **Node) { (*n).Address = "resource.main" },
			"output address is invalid"},
		{"expression", func(_ **Executor, _ **planEvaluationV2, n **Node) { (*n).Body = nil },
			"output expression is required"},
		{"for each", func(_ **Executor, _ **planEvaluationV2, n **Node) { (*n).ForEach = (*n).Body },
			"output cannot declare for-each"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &Executor{DAG: newDAG(map[string][]string{})}
			evaluation, err := executor.preparePlanEvaluationV2(
				operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t),
				newPlanEvaluationV2Pass(newPlanEvaluationV2Facts()),
			)
			require.NoError(t, err)
			node := &Node{Address: "output.result", Kind: NodeOutput, Body: parseValue(t, "'value'")}
			test.change(&executor, &evaluation, &node)
			request, err := executor.planEvaluationV2OutputRequest(evaluation, node)
			require.ErrorContains(t, err, test.want)
			require.Nil(t, request.Plan)
		})
	}
}

func TestPlanEvaluationV2OutputRequestStopsOnEvaluationErrors(t *testing.T) {
	wantErr := errors.New("function failed")
	tests := []struct {
		name       string
		expression string
		result     any
		err        error
		message    string
	}{
		{"expression", "1 / 0", nil, nil, "division by zero"},
		{"partial expression", "[resource.main.id, 1 / 0]", nil, nil, "division by zero"},
		{"function", "cloud.result()", nil, wantErr, "function failed"},
		{"non-finite number", "cloud.result()", math.Inf(1), nil, "number must be finite"},
		{"unsupported value", "cloud.result()", make(chan int), nil, "unsupported planning value"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			executor := &Executor{
				DAG: newDAG(map[string][]string{}),
				Libraries: map[string]*Library{"cloud": {
					Functions: map[string]FunctionType{"result": {
						Func: func([]any) (any, error) { calls++; return test.result, test.err },
					}},
				}},
			}
			pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
			evaluation, err := executor.preparePlanEvaluationV2(
				operationObject(t, map[string]EncodedValue{}), newPlanEvaluationV2Snapshot(t), pass,
			)
			require.NoError(t, err)
			request, err := executor.planEvaluationV2OutputRequest(evaluation, &Node{
				Address: "output.result", Kind: NodeOutput, Body: parseValue(t, test.expression),
			})
			require.NoError(t, err)
			require.Zero(t, calls)
			step, err := request.Plan(nil, pass)
			require.ErrorContains(t, err, "output planning context is required")
			require.Nil(t, step)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			step, err = request.Plan(ctx, pass)
			require.ErrorIs(t, err, context.Canceled)
			require.Nil(t, step)
			require.Zero(t, calls)
			step, err = request.Plan(context.Background(), pass)
			require.ErrorContains(t, err, "output.result")
			require.ErrorContains(t, err, test.message)
			if test.err != nil {
				require.ErrorIs(t, err, test.err)
			}
			require.Nil(t, step)
		})
	}
}
