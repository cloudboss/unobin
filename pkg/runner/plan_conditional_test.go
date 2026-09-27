package runner

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func TestPlanRendersConditionalReplacement(t *testing.T) {
	step := &runtime.PlanStep{
		Address: "resource.instance", Kind: runtime.NodeResource,
		Decision:                  runtime.DecisionReplace,
		AllowedDecisions:          []runtime.Decision{runtime.DecisionUpdate, runtime.DecisionReplace},
		Inputs:                    map[string]any{"subnet": nil},
		UnresolvedInputs:          map[string][]string{"subnet": {"resource.subnet.id"}},
		PendingReplacementReasons: []string{"subnet"},
	}
	plan := &runtime.Plan{Steps: []*runtime.PlanStep{step}}
	var out bytes.Buffer
	printPlan(&out, plan, true)
	require.Contains(t, out.String(), "[?] resource.instance  (update or replace)")
	require.Contains(t, out.String(), "replacement depends on resolved value")
	require.Contains(t, out.String(), "0 to replace")
	require.Contains(t, out.String(), "1 conditional")
	require.NotContains(t, out.String(), "forces replacement")

	summary, err := buildPlanSummary(Info{}, plan, nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, summary.Summary.Conditional)
	require.Zero(t, summary.Summary.Replace)
	require.Equal(t, step.AllowedDecisions, summary.Steps[0].AllowedDecisions)
	require.Equal(t, []string{"subnet"}, summary.Steps[0].PendingReplacementReasons)
	require.Empty(t, summary.Steps[0].ReplacementReasons)
}
