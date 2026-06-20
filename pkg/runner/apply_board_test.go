package runner

import (
	"bytes"
	"testing"
	"time"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderBoard(t *testing.T) {
	tests := []struct {
		name    string
		running []boardEntry
		want    []string
	}{
		{name: "empty", running: nil, want: nil},
		{
			name: "one step",
			running: []boardEntry{
				{address: "resource.aws.vpc.main", decision: runtime.DecisionCreate, elapsed: 5 * time.Second},
			},
			want: []string{
				"",
				"Still running (1):",
				"  creating resource.aws.vpc.main (5.0s)",
			},
		},
		{
			name: "sorted by address",
			running: []boardEntry{
				{address: "resource.b", decision: runtime.DecisionUpdate, elapsed: 90 * time.Second},
				{address: "resource.a", decision: runtime.DecisionCreate, elapsed: 3 * time.Second},
			},
			want: []string{
				"",
				"Still running (2):",
				"  creating resource.a (3.0s)",
				"  updating resource.b (90.0s)",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, renderBoard(tt.running))
		})
	}
}

func TestApplyHeartbeatEscalates(t *testing.T) {
	buf := &bytes.Buffer{}
	now := time.Unix(1000, 0)
	r := &applyRenderer{
		out:     buf,
		format:  FormatText,
		now:     func() time.Time { return now },
		running: map[string]*runningStep{},
	}
	r.running["resource.aws.db.main"] = &runningStep{
		decision: runtime.DecisionCreate,
		start:    now,
		nextBeat: firstBeat,
	}

	now = now.Add(9 * time.Second)
	r.heartbeat()
	require.Empty(t, buf.String(), "a step under the first threshold stays quiet")

	now = now.Add(1 * time.Second)
	r.heartbeat()
	require.Contains(t, buf.String(), "still creating resource.aws.db.main (10.0s)")

	buf.Reset()
	now = now.Add(5 * time.Second)
	r.heartbeat()
	require.Empty(t, buf.String(), "no reminder between the first and second threshold")

	buf.Reset()
	now = now.Add(5 * time.Second)
	r.heartbeat()
	require.Contains(t, buf.String(), "still creating resource.aws.db.main (20.0s)")

	buf.Reset()
	now = now.Add(20 * time.Second)
	r.heartbeat()
	require.Contains(t, buf.String(), "still creating resource.aws.db.main (40.0s)")

	buf.Reset()
	now = now.Add(40 * time.Second)
	r.heartbeat()
	require.Contains(t, buf.String(), "still creating resource.aws.db.main (80.0s)")

	buf.Reset()
	now = now.Add(39 * time.Second)
	r.heartbeat()
	require.Empty(t, buf.String(), "after the cap, the next reminder waits 40 seconds")

	buf.Reset()
	now = now.Add(1 * time.Second)
	r.heartbeat()
	require.Contains(t, buf.String(), "still creating resource.aws.db.main (120.0s)")
}

func TestApplyRendererPlainStartDone(t *testing.T) {
	buf := &bytes.Buffer{}
	r := newApplyRenderer(buf, FormatText)
	base := time.Unix(2000, 0)
	r.handleEvent(runtime.ApplyEvent{
		Address: "resource.aws.vpc.main", Kind: runtime.NodeResource,
		Decision: runtime.DecisionCreate, Stage: runtime.StageStart, Time: base,
	})
	r.handleEvent(runtime.ApplyEvent{
		Address: "resource.aws.vpc.main", Kind: runtime.NodeResource,
		Decision: runtime.DecisionCreate, Stage: runtime.StageDone,
		Time: base.Add(time.Second), Elapsed: time.Second,
	})
	out := buf.String()
	require.Contains(t, out, "creating resource.aws.vpc.main")
	require.Contains(t, out, "created resource.aws.vpc.main (1.0s)")
	require.NotContains(t, out, "\x1b[", "a non-terminal writer gets no cursor moves")
}

func TestApplyRendererTTYBoard(t *testing.T) {
	buf := &bytes.Buffer{}
	now := time.Unix(3000, 0)
	r := &applyRenderer{
		out: buf, format: FormatText, tty: true,
		now: func() time.Time { return now }, running: map[string]*runningStep{},
	}
	start := func(addr string) {
		r.handleEvent(runtime.ApplyEvent{
			Address: addr, Kind: runtime.NodeResource,
			Decision: runtime.DecisionCreate, Stage: runtime.StageStart, Time: now,
		})
	}
	start("resource.aws.vpc.main")
	start("resource.aws.subnet.a")
	out := buf.String()
	require.Contains(t, out, "Still running (2):")
	require.Contains(t, out, "creating resource.aws.subnet.a")
	require.Contains(t, out, "\x1b[", "the live board redraw uses cursor moves on a terminal")
}

func TestApplyRendererTTYPrintsFinishedAboveBoard(t *testing.T) {
	buf := &bytes.Buffer{}
	now := time.Unix(3000, 0)
	r := &applyRenderer{
		out: buf, format: FormatText, tty: true,
		now: func() time.Time { return now }, running: map[string]*runningStep{},
	}
	ev := func(addr string, stage runtime.ApplyStage, elapsed time.Duration) runtime.ApplyEvent {
		return runtime.ApplyEvent{
			Address: addr, Kind: runtime.NodeResource, Decision: runtime.DecisionCreate,
			Stage: stage, Time: now, Elapsed: elapsed,
		}
	}
	r.handleEvent(ev("resource.a", runtime.StageStart, 0))
	r.handleEvent(ev("resource.b", runtime.StageStart, 0))
	r.handleEvent(ev("resource.a", runtime.StageDone, 2*time.Second))
	out := buf.String()
	require.Contains(t, out, "created resource.a (2.0s)", "a finished step prints as a permanent line")
	require.Contains(t, out, "Still running (1):", "the board removes the finished step")
}

func TestIsTerminalBuffer(t *testing.T) {
	require.False(t, isTerminal(&bytes.Buffer{}), "a buffer is not a terminal")
}
