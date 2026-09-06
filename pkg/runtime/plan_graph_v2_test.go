package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlanGraphV2UsesApplyDependenciesAndCanonicalBindings(t *testing.T) {
	for _, decision := range []Decision{DecisionCreate, DecisionDestroy} {
		t.Run(string(decision), func(t *testing.T) {
			plan := &PlanFileV2{Steps: []PlanStepV2{
				applyScheduleV2ResourceStep(t, "resource.parent", []string{}, decision),
				applyScheduleV2ResourceStep(t, "resource.child['']", []string{"resource.parent"}, decision),
			}}
			graph, err := PlanGraphV2(plan, &DAG{Nodes: map[string]*Node{
				"resource.child": {Alias: "cloud"},
			}})
			require.NoError(t, err)
			require.Equal(t, "resource.child['']", graph[1].Address)
			require.Equal(t, "child['']", graph[1].Name)
			require.Equal(t, "cloud", graph[1].ImportAlias)
			require.Equal(t, "resource", graph[1].Category)
			require.Equal(t, planStepV2LibraryPath(plan.Steps[1]), graph[1].LibraryPath)
			require.NotEmpty(t, graph[1].ExportKind)
			if decision == DecisionCreate {
				require.Equal(t, []string{"resource.parent"}, graph[1].DependsOn)
				require.Empty(t, graph[0].DependsOn)
			} else {
				require.Equal(t, []string{"resource.child['']"}, graph[0].DependsOn)
				require.Empty(t, graph[1].DependsOn)
			}
		})
	}
}

func TestPlanGraphV2RejectsDuplicateSteps(t *testing.T) {
	plan := &PlanFileV2{Steps: []PlanStepV2{
		applyScheduleV2ResourceStep(t, "resource.child", []string{}, DecisionCreate),
	}}
	plan.Steps = append(plan.Steps, plan.Steps[0])
	graph, err := PlanGraphV2(plan, nil)
	require.Error(t, err)
	require.Nil(t, graph)
}
