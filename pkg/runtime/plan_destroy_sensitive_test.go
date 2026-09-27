package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestDestroyPlanPreservesRecordedSensitivity(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		destroy bool
	}{
		{
			name: "explicit destroy", fixture: "plan-destroy-for-orphan-1", destroy: true,
		},
		{
			name: "removed resource", fixture: "plan-destroy-for-orphan-2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var counters resourceCounters
			libs := resourceModules(&counters)
			libs["core"].Schema = &LibrarySchema{
				Resources: map[string]*TypeSchema{
					"thing": {
						SensitiveInputs:  []string{"name"},
						SensitiveOutputs: []string{"id"},
					},
				},
			}
			store := newStateStore(t)
			factory := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
			applyOnce(t, planTestExecutor(t,
				planFixture(t, "plan-destroy-for-orphan-1"), libs, store, factory))

			libs["core"].Schema = nil
			exec := planTestExecutor(t, planFixture(t, tt.fixture), libs, store, factory)
			exec.Destroy = tt.destroy
			plan, err := exec.Plan(context.Background())
			require.NoError(t, err)
			step := findStep(t, plan, "resource.orph")
			require.Equal(t, DecisionDestroy, step.Decision)
			require.Equal(t, []string{"name"}, step.SensitiveInputs)
			require.Equal(t, []string{"id"}, step.SensitiveOutputs)
		})
	}
}
