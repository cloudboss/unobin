package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompositeOutputsUseSyntaxBody(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t, `
greeting: resource {
  inputs: { path: { type: string } }
  locals: { decorated: var.path + '!' }
  outputs: { path: { value: local.decorated } }
}
`)
	body := composite.body
	node := &Node{CompositeSyntaxBody: &body}
	scope := &EvalContext{
		Vars:   map[string]any{"path": "hello"},
		locals: compositeLocalScope(node),
	}

	got, err := evalCompositeOutputs(node, scope)

	require.NoError(t, err)
	require.Equal(t, map[string]any{"path": "hello!"}, got)
}

func TestPlanCompositeOutputsUseSyntaxBody(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t, `
greeting: resource {
  outputs: {
    ready: { value: var.ready }
    later: { value: resource.later.id }
  }
}
`)
	body := composite.body
	node := &Node{CompositeSyntaxBody: &body}
	scope := &EvalContext{Vars: map[string]any{"ready": "ok"}}

	got, err := planCompositeOutputs(node, scope)

	require.NoError(t, err)
	require.Equal(t, map[string]any{"ready": "ok"}, got)
}

func TestSeedCompositeOutputsUsesSyntaxBody(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t, `
greeting: resource {
  outputs: { ready: { value: var.ready } }
}
`)
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
			"resource.app": {Vars: map[string]any{"ready": "ok"}},
		},
	}

	err := exec.seedCompositeOutputs(rs, &PlanStep{Address: "resource.app", Composite: true})

	require.NoError(t, err)
	require.Equal(t, map[string]any{"app": map[string]any{"ready": "ok"}}, rs.eval.Resources)
}

func TestCheckCompositeConstraintsUseSyntaxBody(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t, `
greeting: resource {
  inputs: { name: { type: optional(string) } }
  constraints: [ { kind: predicate, when: true, require: var.name != null } ]
}
`)
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
			Vars:      map[string]any{},
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
