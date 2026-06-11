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
				leafStep("resource.aws.vpc.main"),
				leafStep("resource.aws.subnet.this"),
			},
			dag: newDAG(map[string][]string{
				"resource.aws.subnet.this": {"resource.aws.vpc.main"},
				"resource.aws.vpc.main":    nil,
			}),
			want: []StepNode{
				{
					Address:  "resource.aws.vpc.main",
					Kind:     NodeResource,
					Decision: DecisionCreate,
				},
				{
					Address:   "resource.aws.subnet.this",
					Kind:      NodeResource,
					Decision:  DecisionCreate,
					DependsOn: []string{"resource.aws.vpc.main"},
				},
			},
		},
		{
			name: "for-each instances each wait on the shared dep",
			steps: []PlanStep{
				leafStep("resource.aws.subnet.this"),
				leafStep("resource.aws.instance.nodes['alpha']"),
				leafStep("resource.aws.instance.nodes['beta']"),
			},
			dag: newDAG(map[string][]string{
				"resource.aws.instance.nodes": {"resource.aws.subnet.this"},
				"resource.aws.subnet.this":    nil,
			}),
			want: []StepNode{
				{
					Address:  "resource.aws.subnet.this",
					Kind:     NodeResource,
					Decision: DecisionCreate,
				},
				{
					Address:   "resource.aws.instance.nodes['alpha']",
					Kind:      NodeResource,
					Decision:  DecisionCreate,
					DependsOn: []string{"resource.aws.subnet.this"},
				},
				{
					Address:   "resource.aws.instance.nodes['beta']",
					Kind:      NodeResource,
					Decision:  DecisionCreate,
					DependsOn: []string{"resource.aws.subnet.this"},
				},
			},
		},
		{
			name: "composite internals pair by instance key",
			steps: []PlanStep{
				leafStep("resource.net.cluster.web['k1']/resource.aws.vpc.this"),
				leafStep("resource.net.cluster.web['k1']/resource.aws.subnet.this"),
				leafStep("resource.net.cluster.web['k2']/resource.aws.vpc.this"),
				leafStep("resource.net.cluster.web['k2']/resource.aws.subnet.this"),
			},
			dag: newDAG(map[string][]string{
				"resource.net.cluster.web/resource.aws.subnet.this": {
					"resource.net.cluster.web/resource.aws.vpc.this",
				},
				"resource.net.cluster.web/resource.aws.vpc.this": nil,
			}),
			want: []StepNode{
				{
					Address:  "resource.net.cluster.web['k1']/resource.aws.vpc.this",
					Kind:     NodeResource,
					Decision: DecisionCreate,
				},
				{
					Address:  "resource.net.cluster.web['k1']/resource.aws.subnet.this",
					Kind:     NodeResource,
					Decision: DecisionCreate,
					DependsOn: []string{
						"resource.net.cluster.web['k1']/resource.aws.vpc.this",
					},
				},
				{
					Address:  "resource.net.cluster.web['k2']/resource.aws.vpc.this",
					Kind:     NodeResource,
					Decision: DecisionCreate,
				},
				{
					Address:  "resource.net.cluster.web['k2']/resource.aws.subnet.this",
					Kind:     NodeResource,
					Decision: DecisionCreate,
					DependsOn: []string{
						"resource.net.cluster.web['k2']/resource.aws.vpc.this",
					},
				},
			},
		},
		{
			name: "destroy steps wait on their reversed dependencies",
			steps: []PlanStep{
				destroyStep("resource.aws.vpc.main"),
				destroyStep("resource.aws.subnet.this", "resource.aws.vpc.main"),
			},
			dag: newDAG(map[string][]string{}),
			want: []StepNode{
				{
					Address:   "resource.aws.vpc.main",
					Kind:      NodeResource,
					Decision:  DecisionDestroy,
					DependsOn: []string{"resource.aws.subnet.this"},
				},
				{
					Address:  "resource.aws.subnet.this",
					Kind:     NodeResource,
					Decision: DecisionDestroy,
				},
			},
		},
		{
			name: "kind, decision, and boundary bit copied through",
			steps: []PlanStep{
				{
					Address:  "configuration.greet.fancy",
					Kind:     NodeConfiguration,
					Decision: DecisionEval,
				},
				{
					Address:  "action.greet.say.solo",
					Kind:     NodeAction,
					Decision: DecisionRerun,
				},
				{
					Address:   "resource.net.cluster.web",
					Kind:      NodeResource,
					Composite: true,
					Decision:  DecisionNoOp,
				},
			},
			dag: newDAG(map[string][]string{
				"action.greet.say.solo":     {"configuration.greet.fancy"},
				"configuration.greet.fancy": nil,
				"resource.net.cluster.web":  nil,
			}),
			want: []StepNode{
				{
					Address:  "configuration.greet.fancy",
					Kind:     NodeConfiguration,
					Decision: DecisionEval,
				},
				{
					Address:   "action.greet.say.solo",
					Kind:      NodeAction,
					Decision:  DecisionRerun,
					DependsOn: []string{"configuration.greet.fancy"},
				},
				{
					Address:   "resource.net.cluster.web",
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
		leafStep("resource.aws.subnet.this"),
		leafStep("resource.aws.instance.nodes['alpha']"),
		leafStep("resource.aws.instance.nodes['beta']"),
		leafStep("resource.aws.lb.web"),
	}
	dag := newDAG(map[string][]string{
		"resource.aws.instance.nodes": {"resource.aws.subnet.this"},
		"resource.aws.lb.web":         {"resource.aws.instance.nodes"},
		"resource.aws.subnet.this":    nil,
	})
	want := PlanGraph(&PlanFile{Steps: steps}, dag)
	for range 20 {
		assert.Equal(t, want, PlanGraph(&PlanFile{Steps: steps}, dag))
	}
}
