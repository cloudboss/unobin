package runner

import (
	"bytes"
	"testing"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

func TestPrintPlanShowsDeferredLibraryConfigReads(t *testing.T) {
	plan := &runtime.Plan{Steps: []*runtime.PlanStep{
		{
			Address:  "resource.fix.config-echo.app",
			Kind:     runtime.NodeResource,
			Decision: runtime.DecisionCreate,
			Inputs:   map[string]any{"name": "apps"},
		},
		{
			Address:        "resource.fix.other.b",
			Kind:           runtime.NodeResource,
			Decision:       runtime.DecisionNoOp,
			DeferredConfig: "library-config.fix",
		},
		{
			Address:        "data.fix.probe.p",
			Kind:           runtime.NodeData,
			Decision:       runtime.DecisionRead,
			DeferredConfig: "library-config.fix",
		},
	}}
	buf := &bytes.Buffer{}
	printPlan(buf, plan, false)
	want := `  + resource.fix.config-echo.app
      name: 'apps'

Deferred reads (2):
  data.fix.probe.p    library-config.fix pending; read deferred to apply
  resource.fix.other.b    library-config.fix pending; drift unchecked this plan

Plan: 1 to create, 0 to update, 0 to replace, 0 to destroy, 0 to rerun.
`
	require.Equal(t, want, buf.String())
}
