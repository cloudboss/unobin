package runner

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func TestPrintPlanExplainsReplacementMetadata(t *testing.T) {
	for _, tt := range []struct{ reason, want string }{
		{reason: "binding", want: "provider binding changed"},
		{reason: "configuration", want: "configuration changed"},
		{reason: "configuration-pending", want: "configuration resolved at apply"},
	} {
		t.Run(tt.reason, func(t *testing.T) {
			view := &planView{Steps: []*planStepView{{Address: "resource.server",
				Kind: runtime.NodeResource, Decision: runtime.DecisionReplace,
				ReplaceTriggers: []string{tt.reason}}}}
			var out bytes.Buffer
			printPlan(&out, view, true)
			require.Contains(t, out.String(), "replacement: "+tt.want)
		})
	}
}

func TestPrintPlanGroupsEmptyInstanceKey(t *testing.T) {
	view := &planView{Steps: []*planStepView{
		{Address: "resource.server['']", Kind: runtime.NodeResource, Decision: runtime.DecisionCreate},
		{Address: "resource.server['a']", Kind: runtime.NodeResource, Decision: runtime.DecisionCreate},
	}}
	var out bytes.Buffer
	printPlan(&out, view, true)
	require.Contains(t, out.String(), "resource.server  (for-each, 2 instances)")
	require.Contains(t, out.String(), "['']")
}

func TestPlanViewPreservesTypedChangesAndMasksPaths(t *testing.T) {
	object := func(fields map[string]runtime.EncodedValue) runtime.EncodedValue {
		v, err := runtime.ObjectValue(fields)
		require.NoError(t, err)
		return v
	}
	number, err := runtime.NumberValue(1)
	require.NoError(t, err)
	pending, err := runtime.PendingEncodedValue([]string{"resource.seed.name"})
	require.NoError(t, err)
	before := object(map[string]runtime.EncodedValue{
		"settings": object(map[string]runtime.EncodedValue{
			"token": runtime.StringValue("old-secret"), "region": runtime.StringValue("west"),
		}), "size": runtime.IntegerValue(1),
	})
	after := object(map[string]runtime.EncodedValue{
		"settings": object(map[string]runtime.EncodedValue{
			"token": runtime.StringValue("new-secret"), "region": runtime.StringValue("east"),
		}), "size": number, "name": pending,
	})
	plan := &runtime.PlanFileV2{Stack: "prod", StateRevision: "rev", Parallelism: 2,
		Steps: []runtime.PlanStepV2{{Address: "resource.server", Kind: runtime.NodeResource,
			Operation: runtime.StepOperation{Kind: runtime.StepResource,
				Resource: &runtime.ResourcePlanOperation{Decision: runtime.DecisionReplace,
					Desired: &runtime.PlannedResourceTarget{Inputs: after,
						SensitiveInputPaths: []string{"/settings/token"}},
					Prior:       &runtime.ResourceTarget{Inputs: before, Outputs: before},
					Observation: &runtime.ResourceObservation{Status: runtime.ObservationPresent, Outputs: &after},
					Reasons:     []string{"input:settings.token"},
				},
			},
		}},
	}
	view := newPlanView(plan)
	require.Equal(t, "prod", view.Stack)
	require.Equal(t, "rev", view.StateRev)
	require.Equal(t, 2, view.Parallelism)
	step := view.Steps[0]
	require.Equal(t, runtime.DecisionReplace, step.Decision)
	require.Equal(t, []string{"input:settings.token"}, step.ReplaceTriggers)
	require.Equal(t, "  (forces replacement)", replaceNote(step, "settings"))
	rendered := formatValue(step.Inputs["settings"])
	require.Contains(t, rendered, "east")
	require.Contains(t, rendered, sensitivePlaceholder)
	require.NotContains(t, rendered, "new-secret")
	require.NotContains(t, formatValue(step.PriorInputs["settings"]), "old-secret")
	require.Equal(t, "<resource.seed.name>", formatValue(step.Inputs["name"]))
	require.False(t, sameJSONValue(step.PriorInputs["size"], step.Inputs["size"]))
}

func TestPlanViewKeepsEveryOperationDecision(t *testing.T) {
	plan := &runtime.PlanFileV2{Mode: runtime.PlanDestroy, Steps: []runtime.PlanStepV2{
		{Kind: runtime.NodeResource, Operation: runtime.StepOperation{Kind: runtime.StepResource,
			Resource: &runtime.ResourcePlanOperation{Decision: runtime.DecisionDestroy,
				Observation: &runtime.ResourceObservation{Status: runtime.ObservationAbsent}}}},
		{Kind: runtime.NodeAction, Operation: runtime.StepOperation{Kind: runtime.StepAction,
			Action: &runtime.ActionPlanOperation{Decision: runtime.DecisionRerun}}},
		{Kind: runtime.NodeDataSource, Operation: runtime.StepOperation{Kind: runtime.StepDataSource,
			DataSource: &runtime.DataSourcePlanOperation{Decision: runtime.DecisionRead}}},
		{Kind: runtime.NodeLibraryConfiguration, Operation: runtime.StepOperation{
			Kind: runtime.StepLibraryConfiguration,
			LibraryConfiguration: &runtime.LibraryConfigurationPlanOperation{
				Decision: runtime.DecisionEval}}},
		{Kind: runtime.NodeAction, Operation: runtime.StepOperation{
			Kind:      runtime.StepComposite,
			Composite: &runtime.CompositePlanOperation{Decision: runtime.DecisionEval}}},
		{Kind: runtime.NodeOutput, Operation: runtime.StepOperation{Kind: runtime.StepOutput,
			Output: &runtime.OutputPlanOperation{Decision: runtime.DecisionEval}}},
	}}
	view := newPlanView(plan)
	require.True(t, view.Destroy)
	require.True(t, view.Steps[0].AlreadyGone)
	require.Equal(t, runtime.DecisionRerun, view.Steps[1].Decision)
	require.Equal(t, runtime.DecisionRead, view.Steps[2].Decision)
	require.Equal(t, runtime.DecisionEval, view.Steps[3].Decision)
	require.Equal(t, runtime.NodeLibraryConfig, view.Steps[3].Kind)
	require.True(t, view.Steps[4].Composite)
	require.Equal(t, runtime.DecisionEval, view.Steps[5].Decision)
}
