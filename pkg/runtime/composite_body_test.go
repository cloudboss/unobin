package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/ubtest"
)

func TestCompositeOutputsUseSyntaxBody(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t,
		ubtest.ReadValidFixture(t, "testdata/ub/composite-body", "outputs-with-local"))
	body := composite.body
	node := &Node{CompositeSyntaxBody: &body}
	scope := &EvalContext{
		Inputs: map[string]any{"path": "hello"},
		locals: compositeLocalScope(node),
	}

	got, err := evalCompositeOutputs(node, scope)

	require.NoError(t, err)
	require.Equal(t, map[string]any{"path": "hello!"}, got)
}

func TestPlanCompositeOutputsUseSyntaxBody(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t,
		ubtest.ReadValidFixture(t, "testdata/ub/composite-body", "plan-pending-output"))
	body := composite.body
	node := &Node{CompositeSyntaxBody: &body}
	scope := &EvalContext{Inputs: map[string]any{"ready": "ok"}}

	got, err := planCompositeOutputs(node, scope)

	require.NoError(t, err)
	require.Equal(t, map[string]any{"ready": "ok"}, got)
}

func TestSeedCompositeOutputsUsesSyntaxBody(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t,
		ubtest.ReadValidFixture(t, "testdata/ub/composite-body", "seed-output"))
	body := composite.body
	node := &Node{
		Address:             "resource.app",
		Kind:                NodeResource,
		CompositeSyntaxBody: &body,
	}
	exec := &Executor{DAG: &DAG{Nodes: map[string]*Node{"resource.app": node}}}
	rs := &runState{
		eval: &EvalContext{Resources: map[string]any{}},
		composites: map[string]*EvalContext{
			"resource.app": {Inputs: map[string]any{"ready": "ok"}},
		},
	}

	err := exec.seedCompositeOutputs(rs, &PlanStep{Address: "resource.app", Composite: true})

	require.NoError(t, err)
	require.Equal(t, map[string]any{"app": map[string]any{"ready": "ok"}}, rs.eval.Resources)
}

func TestCheckCompositeConstraintsUseSyntaxBody(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t,
		ubtest.ReadValidFixture(t, "testdata/ub/composite-body", "constraint-predicate"))
	body := composite.body
	node := &Node{
		Address:             "resource.app",
		Kind:                NodeResource,
		CompositeSyntaxBody: &body,
		Libraries:           map[string]*Library{},
	}
	exec := &Executor{DAG: &DAG{Nodes: map[string]*Node{"resource.app": node}}}
	rs := &runState{composites: map[string]*EvalContext{
		"resource.app": {
			Inputs:    map[string]any{},
			Libraries: map[string]*Library{},
			locals:    newLocalScope(nil),
		},
	}}
	step := &PlanStep{Address: "resource.app", Composite: true}

	got := exec.checkCompositeConstraints(rs, step)

	require.Len(t, got, 1)
	require.EqualError(t, got[0],
		"resource.app: schema: constraints[0] (predicate): "+
			"predicate requirement not satisfied")
}
