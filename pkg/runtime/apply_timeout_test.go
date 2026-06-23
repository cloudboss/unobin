package runtime

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestExtractTimeout(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		want    time.Duration
	}{
		{name: "action", fixture: "action-timeout", want: 30 * time.Second},
		{name: "resource", fixture: "resource-timeout", want: 5 * time.Minute},
		{name: "data-source", fixture: "data-timeout", want: 90 * time.Minute},
		{name: "none", fixture: "no-timeout", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := ubtest.ReadValidFixture(t, "testdata/ub/apply-timeout", tt.fixture)
			nodes := ExtractSyntaxNodes(syntaxFactoryBody(t, src), nil)
			require.Len(t, nodes, 1)
			assert.Equal(t, tt.want, nodes[0].Timeout)
		})
	}
}

func timeoutExecutor(t *testing.T, src string) *Executor {
	var track concurrencyTracker
	libs := slowActionModules(&track)
	dag, syntaxSource := syntaxDAGAndBody(t, src, libs)
	return &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        newStateStore(t),
		Factory:      state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"},
	}
}

func TestApplyTimeoutFailsAnOverrunningStep(t *testing.T) {
	src := ubtest.ReadValidFixture(t, "testdata/ub/apply-timeout", "overrun")
	_, err := planAndApply(timeoutExecutor(t, src))
	require.Error(t, err)
	assert.ErrorContains(t, err, "deadline exceeded")
}

func TestApplyTimeoutAllowsAStepThatFinishesInTime(t *testing.T) {
	src := ubtest.ReadValidFixture(t, "testdata/ub/apply-timeout", "finishes")
	_, err := planAndApply(timeoutExecutor(t, src))
	require.NoError(t, err)
}
