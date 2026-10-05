package runtime

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestApplySerialFailurePreventsLaterCalls(t *testing.T) {
	for range 32 {
		var calls atomic.Int64
		libraries := slowLibraries()
		libraries["slow"].Resources["r"] = MakeResourceWith[
			countingSlowResource, *slowResourceOutput, any,
		](testResourceDefinition[countingSlowResource, *slowResourceOutput, any](),
			func() *countingSlowResource { return &countingSlowResource{runs: &calls} })
		dag, body := syntaxDAGAndBody(t,
			ubtest.ReadValidFixture(t, "testdata/ub/apply-schedule", "serial-failure"), libraries)
		executor := &Executor{
			DAG: dag, SyntaxSource: body, Libraries: libraries, Store: newStateStore(t),
			Factory:     state.FactoryInfo{Name: "serial", Version: "v0", ContentRevision: "c0"},
			Parallelism: 1,
		}
		_, err := planAndApply(executor)
		require.ErrorContains(t, err, "slow-fail failure")
		require.Zero(t, calls.Load(), "a serial apply must stop before the next library call")
	}
}
