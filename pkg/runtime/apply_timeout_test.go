package runtime

import (
	"testing"
	"time"

	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractTimeout(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want time.Duration
	}{
		{
			name: "action",
			src:  `actions: { x: core.slow { @timeout: '30s', delay-ms: 1 } }`,
			want: 30 * time.Second,
		},
		{
			name: "resource",
			src:  `resources: { x: aws.vpc { @timeout: '5m', cidr: '10.0.0.0/16' } }`,
			want: 5 * time.Minute,
		},
		{
			name: "data",
			src:  `data: { x: aws.ami { @timeout: '1h30m', most-recent: true } }`,
			want: 90 * time.Minute,
		},
		{
			name: "none",
			src:  `resources: { x: aws.vpc { cidr: '10.0.0.0/16' } }`,
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes := ExtractSyntaxNodes(syntaxFactoryBody(t, tt.src), nil)
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
	src := `
actions: { x: core.slow { @timeout: '20ms', delay-ms: 500 } }
`
	_, err := planAndApply(timeoutExecutor(t, src))
	require.Error(t, err)
	assert.ErrorContains(t, err, "deadline exceeded")
}

func TestApplyTimeoutAllowsAStepThatFinishesInTime(t *testing.T) {
	src := `
actions: { x: core.slow { @timeout: '5s', delay-ms: 5 } }
`
	_, err := planAndApply(timeoutExecutor(t, src))
	require.NoError(t, err)
}
