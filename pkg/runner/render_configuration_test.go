package runner

import (
	"bytes"
	"testing"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

// A consumer whose internal configuration is pending shows the
// selection in pending brackets, and every step whose read was held
// back is listed with the reason.
func TestPrintPlanShowsPendingConfigurationAndDeferredReads(t *testing.T) {
	plan := &runtime.Plan{Steps: []*runtime.PlanStep{
		{
			Address:       "resource.fix.config-echo.app",
			Kind:          runtime.NodeResource,
			Decision:      runtime.DecisionCreate,
			Inputs:        map[string]any{"name": "apps"},
			Configuration: runtime.ConfigRef{Alias: "fix", Name: "cluster"},
		},
		{
			Address:      "resource.fix.other.b",
			Kind:         runtime.NodeResource,
			Decision:     runtime.DecisionNoOp,
			DeferredRead: runtime.ConfigRef{Alias: "fix", Name: "cluster"},
		},
		{
			Address:      "data.fix.probe.p",
			Kind:         runtime.NodeData,
			Decision:     runtime.DecisionRead,
			DeferredRead: runtime.ConfigRef{Alias: "fix", Name: "cluster"},
		},
	}}
	buf := &bytes.Buffer{}
	printPlan(buf, plan, false)
	want := `  + resource.fix.config-echo.app
      @configuration: <fix.cluster>
      name: 'apps'

Deferred reads (2):
  data.fix.probe.p    @configuration: fix.cluster pending; read deferred to apply
  resource.fix.other.b    @configuration: fix.cluster pending; drift unchecked this plan

Plan: 1 to create, 0 to update, 0 to replace, 0 to destroy, 0 to rerun.
`
	require.Equal(t, want, buf.String())
}
