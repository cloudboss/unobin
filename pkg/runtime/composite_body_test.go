package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
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

func TestPlanCompositeOutputsKeepPendingFields(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t,
		ubtest.ReadValidFixture(t, "testdata/ub/composite-body", "plan-pending-output"))
	body := composite.body
	node := &Node{CompositeSyntaxBody: &body}
	scope := &EvalContext{Inputs: map[string]any{"ready": "ok"}}

	got, err := planCompositeOutputs(node, scope)

	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"ready": "ok",
		"later": PendingValue{Refs: []string{"resource.later.id"}},
	}, got)
}
