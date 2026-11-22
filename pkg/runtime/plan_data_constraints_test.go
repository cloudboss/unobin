package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type planConstraintDataSource struct {
	Key string

	reads   *int64
	readErr error
}

func (d *planConstraintDataSource) Read(_ context.Context, _ any) (any, error) {
	atomic.AddInt64(d.reads, 1)
	if d.readErr != nil {
		return nil, d.readErr
	}
	return map[string]any{"value": d.Key}, nil
}

func planConstraintDataLibrary(reads *int64, readErr error) map[string]*Library {
	value := "unused"
	libs := dataPlanModules(&value, reads)
	libs["core"].DataSources["dial"] = MakeDataSourceWith[planConstraintDataSource, any, any](
		func() *planConstraintDataSource {
			return &planConstraintDataSource{reads: reads, readErr: readErr}
		},
	)
	libs["core"].Constraints = map[string][]lang.ConstraintSpec{
		"data-source.dial": {predSpec("true", "input.key == 'good'")},
	}
	return libs
}

func TestPlanDataConstraintsBeforeRead(t *testing.T) {
	lookupErr := errors.New("lookup failed")
	for _, value := range []string{"good", "bad"} {
		for _, readErr := range []error{nil, lookupErr} {
			name := value + "/read-success"
			if readErr != nil {
				name = value + "/read-error"
			}
			t.Run(name, func(t *testing.T) {
				var reads int64
				libs := planConstraintDataLibrary(&reads, readErr)
				exec := planTestExecutor(t, planDataFixture(t, "constraint-input"),
					libs, newStateStore(t), state.FactoryInfo{})
				exec.Inputs = map[string]any{"key": value}
				_, err := exec.Plan(context.Background())
				if value == "bad" {
					require.ErrorContains(t, err, "data-source.cfg: schema: constraints[0]")
					require.NotErrorIs(t, err, lookupErr)
					require.Zero(t, reads)
					return
				}
				if readErr == nil {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, lookupErr)
				}
				require.EqualValues(t, 1, reads)
			})
		}
	}
}

func TestPlanDataConstraintsSeeDefaults(t *testing.T) {
	for _, value := range []string{"good", "bad"} {
		t.Run(value, func(t *testing.T) {
			var reads int64
			libs := planConstraintDataLibrary(&reads, nil)
			libs["core"].Defaults = map[string][]lang.DefaultSpec{
				"data-source.dial": {{Field: "input.key", Value: "'" + value + "'"}},
			}
			exec := planTestExecutor(t, planDataFixture(t, "constraint-default"),
				libs, newStateStore(t), state.FactoryInfo{})
			plan, err := exec.Plan(context.Background())
			if value == "bad" {
				require.ErrorContains(t, err, "data-source.cfg: schema: constraints[0]")
				require.Zero(t, reads)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, 1, reads)
			require.Equal(t, map[string]any{"value": "good"},
				findStep(t, plan, "data-source.cfg").ObservedOutputs)
		})
	}
}

func TestPlanDataConstraintsBeforeUnchangedRead(t *testing.T) {
	var reads int64
	libs := planConstraintDataLibrary(&reads, nil)
	exec := planTestExecutor(t, planDataFixture(t, "constraint-input"),
		libs, newStateStore(t), state.FactoryInfo{})
	exec.Inputs = map[string]any{"key": "good"}
	applyOnce(t, exec)
	require.EqualValues(t, 2, reads)
	reads = 0
	exec.Inputs["key"] = "bad"
	_, err := exec.Plan(context.Background())
	require.ErrorContains(t, err, "data-source.cfg: schema: constraints[0]")
	require.Zero(t, reads)
}
