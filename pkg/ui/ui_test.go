package ui

import (
	"bufio"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/runtime"
)

var testClock = time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)

func testGraph() []runtime.StepNode {
	return []runtime.StepNode{
		{
			Address:  "resource.aws.vpc.main",
			Kind:     runtime.NodeResource,
			Decision: runtime.DecisionCreate,
		},
		{
			Address:   "resource.aws.subnet.this",
			Kind:      runtime.NodeResource,
			Decision:  runtime.DecisionCreate,
			DependsOn: []string{"resource.aws.vpc.main"},
		},
	}
}

func startTestServer(t *testing.T) *Server {
	t.Helper()
	s, err := Start(Config{
		Factory: "fac",
		Stack:   "prod",
		Graph:   testGraph(),
		now:     func() time.Time { return testClock },
	})
	require.NoError(t, err)
	t.Cleanup(s.Close)
	return s
}

// connectSSE opens the event stream and returns a reader over it.
func connectSSE(t *testing.T, s *Server) *bufio.Reader {
	t.Helper()
	resp, err := http.Get(s.URL() + "events")
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	return bufio.NewReader(resp.Body)
}

// sseFrames reads the next n data payloads from the stream.
func sseFrames(t *testing.T, br *bufio.Reader, n int) []string {
	t.Helper()
	frames := make([]string, 0, n)
	for len(frames) < n {
		line, err := br.ReadString('\n')
		require.NoError(t, err)
		line = strings.TrimRight(line, "\n")
		if rest, ok := strings.CutPrefix(line, "data: "); ok {
			frames = append(frames, rest)
		}
	}
	return frames
}

func startEvent(addr string) runtime.ApplyEvent {
	return runtime.ApplyEvent{
		Address:  addr,
		Kind:     runtime.NodeResource,
		Decision: runtime.DecisionCreate,
		Stage:    runtime.StageStart,
		Time:     testClock,
	}
}

func doneEvent(addr string, elapsed time.Duration) runtime.ApplyEvent {
	return runtime.ApplyEvent{
		Address:  addr,
		Kind:     runtime.NodeResource,
		Decision: runtime.DecisionCreate,
		Stage:    runtime.StageDone,
		Time:     testClock,
		Elapsed:  elapsed,
	}
}

func failEvent(addr string, elapsed time.Duration, msg string) runtime.ApplyEvent {
	return runtime.ApplyEvent{
		Address:  addr,
		Kind:     runtime.NodeResource,
		Decision: runtime.DecisionCreate,
		Stage:    runtime.StageFail,
		Time:     testClock,
		Elapsed:  elapsed,
		Err:      errors.New(msg),
	}
}

func TestURLNamesLoopbackWithToken(t *testing.T) {
	s := startTestServer(t)
	assert.Regexp(t,
		regexp.MustCompile(`^http://127\.0\.0\.1:\d+/[0-9a-f]{32}/$`), s.URL())
}

func TestServerPaths(t *testing.T) {
	s := startTestServer(t)
	base := s.URL()
	tests := []struct {
		name        string
		url         string
		status      int
		contentType string
		contains    string
	}{
		{
			name:        "index",
			url:         base,
			status:      http.StatusOK,
			contentType: "text/html; charset=utf-8",
			contains:    `<svg id="graph"`,
		},
		{
			name:        "stylesheet",
			url:         base + "assets/style.css",
			status:      http.StatusOK,
			contentType: "text/css; charset=utf-8",
			contains:    ".step",
		},
		{
			name:        "script",
			url:         base + "assets/app.js",
			status:      http.StatusOK,
			contentType: "text/javascript; charset=utf-8",
			contains:    "EventSource",
		},
		{
			name:        "layout library",
			url:         base + "assets/dagre.min.js",
			status:      http.StatusOK,
			contentType: "text/javascript; charset=utf-8",
			contains:    "dagre",
		},
		{
			name:        "logo",
			url:         base + "assets/unobin.svg",
			status:      http.StatusOK,
			contentType: "image/svg+xml",
			contains:    "<svg",
		},
		{
			name:   "missing asset",
			url:    base + "assets/nope.js",
			status: http.StatusNotFound,
		},
		{
			name:   "wrong token",
			url:    strings.Replace(base, s.token, strings.Repeat("0", 32), 1),
			status: http.StatusNotFound,
		},
		{
			name:   "root",
			url:    "http://" + s.ln.Addr().String() + "/",
			status: http.StatusNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(tt.url)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()
			assert.Equal(t, tt.status, resp.StatusCode)
			if tt.contentType != "" {
				assert.Equal(t, tt.contentType, resp.Header.Get("Content-Type"))
			}
			if tt.contains != "" {
				body := new(strings.Builder)
				_, err := bufio.NewReader(resp.Body).WriteTo(body)
				require.NoError(t, err)
				assert.Contains(t, body.String(), tt.contains)
			}
		})
	}
}

func TestAppUsesStepNodeKindKey(t *testing.T) {
	script, err := assetsFS.ReadFile("assets/app.js")
	require.NoError(t, err)
	text := string(script)
	assert.Contains(t, text, "['node-kind']")
	assert.NotContains(t, text, "n.kind === 'output'")
	assert.NotContains(t, text, "st.node.kind")
}

func TestEventStream(t *testing.T) {
	s := startTestServer(t)
	br := connectSSE(t, s)

	frames := sseFrames(t, br, 2)
	assert.Equal(t,
		`{"kind":"graph","seq":0,"factory":"fac","stack":"prod","steps":[`+
			`{"address":"resource.aws.vpc.main","node-kind":"resource","decision":"create"},`+
			`{"address":"resource.aws.subnet.this","node-kind":"resource","decision":"create",`+
			`"depends-on":["resource.aws.vpc.main"]}]}`,
		frames[0])
	assert.Equal(t, `{"kind":"snapshot","seq":0,"steps":{}}`, frames[1])

	s.Observe(startEvent("resource.aws.vpc.main"))
	s.Observe(doneEvent("resource.aws.vpc.main", 1500*time.Millisecond))
	frames = sseFrames(t, br, 2)
	assert.Equal(t,
		`{"kind":"apply-event","seq":1,"address":"resource.aws.vpc.main",`+
			`"decision":"create","stage":"start"}`,
		frames[0])
	assert.Equal(t,
		`{"kind":"apply-event","seq":2,"address":"resource.aws.vpc.main",`+
			`"decision":"create","stage":"done","elapsed-ms":1500}`,
		frames[1])

	s.Complete(true, "")
	frames = sseFrames(t, br, 1)
	assert.Equal(t,
		`{"kind":"run-complete","seq":3,"ok":true,"succeeded":1,"failed":0,`+
			`"not-run":1,"elapsed-ms":0}`,
		frames[0])

	rest, err := io.ReadAll(br)
	require.NoError(t, err)
	assert.Equal(t, "\n", string(rest),
		"stream should close after the run-complete frame")
}

func TestLateClientSeesFinishedRun(t *testing.T) {
	s := startTestServer(t)
	s.Observe(startEvent("resource.aws.vpc.main"))
	s.Observe(doneEvent("resource.aws.vpc.main", 1500*time.Millisecond))
	s.Observe(startEvent("resource.aws.subnet.this"))
	s.Observe(failEvent("resource.aws.subnet.this", 2*time.Second, "boom"))
	s.Complete(false, "")

	br := connectSSE(t, s)
	frames := sseFrames(t, br, 3)
	assert.Contains(t, frames[0], `"kind":"graph"`)
	assert.Equal(t,
		`{"kind":"snapshot","seq":5,"steps":{`+
			`"resource.aws.subnet.this":{"stage":"fail","decision":"create",`+
			`"elapsed-ms":2000,"err":"boom"},`+
			`"resource.aws.vpc.main":{"stage":"done","decision":"create",`+
			`"elapsed-ms":1500}}}`,
		frames[1])
	assert.Equal(t,
		`{"kind":"run-complete","seq":5,"ok":false,"succeeded":1,"failed":1,`+
			`"not-run":0,"elapsed-ms":0}`,
		frames[2])
}

func TestSnapshotShowsRunningElapsed(t *testing.T) {
	clock := testClock
	s, err := Start(Config{
		Factory: "fac",
		Graph:   testGraph(),
		now:     func() time.Time { return clock },
	})
	require.NoError(t, err)
	t.Cleanup(s.Close)

	s.Observe(startEvent("resource.aws.vpc.main"))
	clock = clock.Add(4 * time.Second)
	br := connectSSE(t, s)
	frames := sseFrames(t, br, 2)
	assert.Equal(t,
		`{"kind":"snapshot","seq":1,"steps":{`+
			`"resource.aws.vpc.main":{"stage":"start","decision":"create",`+
			`"elapsed-ms":4000}}}`,
		frames[1])
}

func TestObserveUnknownAddressStillStreams(t *testing.T) {
	s := startTestServer(t)
	br := connectSSE(t, s)
	sseFrames(t, br, 2)
	s.Observe(startEvent("resource.aws.orphan.zombie"))
	frames := sseFrames(t, br, 1)
	assert.Equal(t,
		`{"kind":"apply-event","seq":1,"address":"resource.aws.orphan.zombie",`+
			`"decision":"create","stage":"start"}`,
		frames[0])
}

func TestCompleteMessageKeptOnlyOnFailure(t *testing.T) {
	s := startTestServer(t)
	s.Complete(false, "interrupted")
	br := connectSSE(t, s)
	frames := sseFrames(t, br, 3)
	assert.Equal(t,
		`{"kind":"run-complete","seq":1,"ok":false,"message":"interrupted",`+
			`"succeeded":0,"failed":0,"not-run":2,"elapsed-ms":0}`,
		frames[2])
}

func TestCompleteIsIdempotent(t *testing.T) {
	s := startTestServer(t)
	s.Complete(true, "")
	s.Complete(false, "again")
	br := connectSSE(t, s)
	frames := sseFrames(t, br, 3)
	assert.Contains(t, frames[2], `"ok":true`)
}

func TestWaitServed(t *testing.T) {
	s := startTestServer(t)
	assert.False(t, s.WaitServed(time.Second),
		"WaitServed before Complete reports false without waiting")

	s.Complete(true, "")
	assert.False(t, s.WaitServed(20*time.Millisecond),
		"no client has seen the run-complete frame yet")

	br := connectSSE(t, s)
	sseFrames(t, br, 3)
	assert.True(t, s.WaitServed(2*time.Second))
}

func TestSlowSubscriberEvicted(t *testing.T) {
	s := startTestServer(t)
	s.subCap = 4
	ch := s.subscribe()
	got := 0
	drained := false
	for !drained {
		select {
		case _, ok := <-ch:
			if !ok {
				drained = true
				break
			}
			got++
		default:
			drained = true
		}
	}
	require.Equal(t, 2, got, "graph and snapshot frames queued on subscribe")

	for i := range 10 {
		s.Observe(doneEvent("resource.aws.vpc.main", time.Duration(i)*time.Second))
	}
	closed := false
	buffered := 0
	for !closed {
		_, ok := <-ch
		if !ok {
			closed = true
			break
		}
		buffered++
	}
	assert.True(t, closed, "overflowing subscriber is closed")
	assert.Equal(t, 4, buffered, "evicted subscriber keeps what fit in its buffer")
}
