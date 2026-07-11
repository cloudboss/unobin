package runner

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/filechange"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type planSummaryErrorGolden struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

func TestPlanSummaryGolden(t *testing.T) {
	digest := "sha256:0123456789abcdef"
	file := filechange.Change{Path: "dev.ubp", Action: filechange.ActionCreated}
	plan := planSummaryFixture()
	result, err := buildPlanSummary(
		Info{
			FactoryName: "appdeploy", FactoryVersion: "v0.1.0",
			ContentRevision: "abc123def456", LibraryPath: "example.com/appdeploy",
		},
		plan,
		&digest,
		&file,
		[]diagnostic.Diagnostic{
			{Code: "z.notice", Severity: diagnostic.SeverityInfo, Message: "later"},
			{Code: "a.warning", Severity: diagnostic.SeverityWarning, Message: "first"},
		},
	)
	require.NoError(t, err)

	for _, tc := range []struct {
		format cmdout.Format
		path   string
	}{
		{format: cmdout.FormatJSON, path: "testdata/plan-summary.json"},
		{format: cmdout.FormatUnobin, path: "testdata/plan-summary-unobin.stdout"},
	} {
		var got bytes.Buffer
		require.NoError(t, cmdout.WriteDocument(&got, tc.format, result))
		want, err := os.ReadFile(tc.path)
		require.NoError(t, err)
		require.Equal(t, string(want), got.String())
	}
}

func TestPlanSummaryArtifactActionsGolden(t *testing.T) {
	digest := "sha256:0123456789abcdef"
	plan := &runtime.Plan{Stack: "dev", Parallelism: 2, Steps: []*runtime.PlanStep{}}
	var got bytes.Buffer
	for _, action := range []filechange.Action{
		filechange.ActionCreated,
		filechange.ActionUpdated,
		filechange.ActionRemoved,
		filechange.ActionUnchanged,
	} {
		file := filechange.Change{Path: "dev.ubp", Action: action}
		result, err := buildPlanSummary(Info{FactoryName: "appdeploy"}, plan, &digest, &file, nil)
		require.NoError(t, err)
		require.NoError(t, cmdout.WriteDocument(&got, cmdout.FormatJSON, result))
	}
	want, err := os.ReadFile("testdata/plan-summary-actions.jsonl")
	require.NoError(t, err)
	require.Equal(t, string(want), got.String())
}

func TestPlanSummaryErrorsGolden(t *testing.T) {
	digest := "sha256:0123456789abcdef"
	file := filechange.Change{Path: "dev.ubp", Action: filechange.ActionCreated}
	invalidFile := filechange.Change{Path: "dev.ubp", Action: "rewritten"}
	emptyPlan := &runtime.Plan{Steps: []*runtime.PlanStep{}}
	cases := []struct {
		name   string
		plan   *runtime.Plan
		digest *string
		file   *filechange.Change
	}{
		{name: "missing plan", digest: &digest, file: &file},
		{name: "digest without file", plan: emptyPlan, digest: &digest},
		{name: "file without digest", plan: emptyPlan, file: &file},
		{name: "invalid file action", plan: emptyPlan, digest: &digest, file: &invalidFile},
		{
			name: "unsupported decision",
			plan: &runtime.Plan{Steps: []*runtime.PlanStep{{Decision: "future"}}},
		},
		{name: "nil step", plan: &runtime.Plan{Steps: []*runtime.PlanStep{nil}}},
	}
	result := make([]planSummaryErrorGolden, 0, len(cases))
	for _, tc := range cases {
		_, err := buildPlanSummary(Info{}, tc.plan, tc.digest, tc.file, nil)
		result = append(result, planSummaryErrorGolden{Name: tc.name, Error: runnerErrorString(err)})
	}
	got, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')
	want, err := os.ReadFile("testdata/plan-summary-errors.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}

func planSummaryFixture() *runtime.Plan {
	steps := []*runtime.PlanStep{
		{
			Address: "z.create", Kind: runtime.NodeResource, Decision: runtime.DecisionCreate,
			Inputs:       map[string]any{"token": "super-secret"},
			PriorOutputs: map[string]any{"token": "super-secret"},
			TriggerHash:  "super-secret", SensitiveInputs: []string{"token"},
			ReplaceTriggers: []string{"z-last", "a-first"},
			DeferredConfig:  "library-config.cloud",
		},
		{Address: "a.read", Kind: runtime.NodeDataSource, Decision: runtime.DecisionRead},
		{
			Address: "b.update", Kind: runtime.NodeResource, Decision: runtime.DecisionUpdate,
			PriorOutputs:    map[string]any{"value": "before"},
			ObservedOutputs: map[string]any{"value": "after"},
		},
		{
			Address: "c.replace", Kind: runtime.NodeResource,
			Decision: runtime.DecisionReplace, ReplaceTriggers: []string{"second", "first"},
		},
		{
			Address: "d.destroy", Kind: runtime.NodeResource,
			Decision: runtime.DecisionDestroy, AlreadyGone: true,
		},
		{
			Address: "e.rerun", Kind: runtime.NodeAction,
			Decision: runtime.DecisionRerun, Composite: true,
		},
		{Address: "f.skip", Kind: runtime.NodeAction, Decision: runtime.DecisionSkip},
		{Address: "g.no-op", Kind: runtime.NodeOutput, Decision: runtime.DecisionNoOp},
		{Address: "h.eval", Kind: runtime.NodeLibraryConfig, Decision: runtime.DecisionEval},
	}
	return &runtime.Plan{
		Stack: "dev", StateRev: "revision-1", Parallelism: 0, Steps: steps,
		StateMoves: []runtime.PlannedEntryMove{
			{From: "resource.z", To: "resource.a"},
			{From: "resource.b", To: "resource.c"},
		},
	}
}
