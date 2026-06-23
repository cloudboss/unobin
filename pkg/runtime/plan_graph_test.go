package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPlanGraph(t *testing.T) {
	tests := []struct {
		name  string
		steps []PlanStep
		dag   *DAG
		want  []StepNode
	}{
		{
			name: "chain of leaves",
			steps: []PlanStep{
				leafStep("resource.vpc"),
				leafStep("resource.subnet"),
			},
			dag: newDAG(map[string][]string{
				"resource.subnet": {"resource.vpc"},
				"resource.vpc":    nil,
			}),
			want: []StepNode{
				{
					Address:  "resource.vpc",
					Kind:     NodeResource,
					Decision: DecisionCreate,
				},
				{
					Address:   "resource.subnet",
					Kind:      NodeResource,
					Decision:  DecisionCreate,
					DependsOn: []string{"resource.vpc"},
				},
			},
		},
		{
			name: "for-each instances each wait on the shared dep",
			steps: []PlanStep{
				leafStep("resource.subnet"),
				leafStep("resource.nodes['alpha']"),
				leafStep("resource.nodes['beta']"),
			},
			dag: newDAG(map[string][]string{
				"resource.nodes":  {"resource.subnet"},
				"resource.subnet": nil,
			}),
			want: []StepNode{
				{
					Address:  "resource.subnet",
					Kind:     NodeResource,
					Decision: DecisionCreate,
				},
				{
					Address:   "resource.nodes['alpha']",
					Kind:      NodeResource,
					Decision:  DecisionCreate,
					DependsOn: []string{"resource.subnet"},
				},
				{
					Address:   "resource.nodes['beta']",
					Kind:      NodeResource,
					Decision:  DecisionCreate,
					DependsOn: []string{"resource.subnet"},
				},
			},
		},
		{
			name: "composite internals pair by instance key",
			steps: []PlanStep{
				leafStep("resource.web['k1']/resource.vpc"),
				leafStep("resource.web['k1']/resource.subnet"),
				leafStep("resource.web['k2']/resource.vpc"),
				leafStep("resource.web['k2']/resource.subnet"),
			},
			dag: newDAG(map[string][]string{
				"resource.web/resource.subnet": {
					"resource.web/resource.vpc",
				},
				"resource.web/resource.vpc": nil,
			}),
			want: []StepNode{
				{
					Address:  "resource.web['k1']/resource.vpc",
					Kind:     NodeResource,
					Decision: DecisionCreate,
				},
				{
					Address:  "resource.web['k1']/resource.subnet",
					Kind:     NodeResource,
					Decision: DecisionCreate,
					DependsOn: []string{
						"resource.web['k1']/resource.vpc",
					},
				},
				{
					Address:  "resource.web['k2']/resource.vpc",
					Kind:     NodeResource,
					Decision: DecisionCreate,
				},
				{
					Address:  "resource.web['k2']/resource.subnet",
					Kind:     NodeResource,
					Decision: DecisionCreate,
					DependsOn: []string{
						"resource.web['k2']/resource.vpc",
					},
				},
			},
		},
		{
			name: "destroy steps wait on their reversed dependencies",
			steps: []PlanStep{
				destroyStep("resource.vpc"),
				destroyStep("resource.subnet", "resource.vpc"),
			},
			dag: newDAG(map[string][]string{}),
			want: []StepNode{
				{
					Address:   "resource.vpc",
					Kind:      NodeResource,
					Decision:  DecisionDestroy,
					DependsOn: []string{"resource.subnet"},
				},
				{
					Address:  "resource.subnet",
					Kind:     NodeResource,
					Decision: DecisionDestroy,
				},
			},
		},
		{
			name: "kind, decision, and boundary bit copied through",
			steps: []PlanStep{
				{
					Address:  "library-config.fancy",
					Kind:     NodeLibraryConfig,
					Decision: DecisionEval,
				},
				{
					Address:  "action.solo",
					Kind:     NodeAction,
					Decision: DecisionRerun,
				},
				{
					Address:   "resource.web",
					Kind:      NodeResource,
					Composite: true,
					Decision:  DecisionNoOp,
				},
			},
			dag: newDAG(map[string][]string{
				"action.solo":          {"library-config.fancy"},
				"library-config.fancy": nil,
				"resource.web":         nil,
			}),
			want: []StepNode{
				{
					Address:  "library-config.fancy",
					Kind:     NodeLibraryConfig,
					Decision: DecisionEval,
				},
				{
					Address:   "action.solo",
					Kind:      NodeAction,
					Decision:  DecisionRerun,
					DependsOn: []string{"library-config.fancy"},
				},
				{
					Address:   "resource.web",
					Kind:      NodeResource,
					Composite: true,
					Decision:  DecisionNoOp,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PlanGraph(&PlanFile{Steps: tt.steps}, tt.dag)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPlanGraphDeterministic(t *testing.T) {
	steps := []PlanStep{
		leafStep("resource.subnet"),
		leafStep("resource.nodes['alpha']"),
		leafStep("resource.nodes['beta']"),
		leafStep("resource.lb"),
	}
	dag := newDAG(map[string][]string{
		"resource.nodes":  {"resource.subnet"},
		"resource.lb":     {"resource.nodes"},
		"resource.subnet": nil,
	})
	want := PlanGraph(&PlanFile{Steps: steps}, dag)
	for range 20 {
		assert.Equal(t, want, PlanGraph(&PlanFile{Steps: steps}, dag))
	}
}
