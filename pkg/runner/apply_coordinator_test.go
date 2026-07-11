package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type applyCoordinatorGolden struct {
	Cases []applyCoordinatorCaseGolden `json:"cases"`
}

type applyCoordinatorCaseGolden struct {
	Name         string   `json:"name"`
	Stdout       string   `json:"stdout"`
	Error        string   `json:"error"`
	Reported     bool     `json:"reported"`
	Records      int      `json:"records"`
	Terminals    int      `json:"terminals"`
	TerminalLast bool     `json:"terminal-last"`
	ProducerDone bool     `json:"producer-done"`
	ViewEvents   []string `json:"view-events"`
	ViewOK       bool     `json:"view-ok"`
	ViewMessage  string   `json:"view-message"`
}

func TestApplyMachineCoordinatorGolden(t *testing.T) {
	result := applyCoordinatorGolden{Cases: []applyCoordinatorCaseGolden{
		applyCoordinatorSuccess(t),
		applyCoordinatorBrowserFailure(t, false),
		applyCoordinatorBrowserFailure(t, true),
		applyCoordinatorStepFailure(t),
		applyCoordinatorInterruption(t),
		applyCoordinatorRevisionFailure(t),
		applyCoordinatorCancellationDrain(t),
		applyCoordinatorInvalidResult(t),
		applyCoordinatorEncodingFailure(t),
		applyCoordinatorWriteFailure(t),
	}}
	got, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')
	want, err := os.ReadFile("testdata/apply-coordinator.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}

func applyCoordinatorSuccess(t *testing.T) applyCoordinatorCaseGolden {
	t.Helper()
	view := &applyCoordinatorView{url: "http://127.0.0.1/run"}
	return runApplyCoordinatorCase(t, "success", view, applyMachineOptions{
		openBrowser: func(context.Context, string) error { return nil },
		apply: func(
			_ context.Context,
			executor *runtime.Executor,
			_ *runtime.PlanFile,
		) (*runtime.ExecResult, error) {
			executor.Events <- runtime.ApplyEvent{
				Stage: runtime.StageStart, Decision: runtime.DecisionCreate,
				Address: "resource.visible",
			}
			executor.Events <- runtime.ApplyEvent{
				Stage: runtime.StageDone, Decision: runtime.DecisionCreate,
				Address: "resource.visible", Elapsed: time.Second,
			}
			return &runtime.ExecResult{
				WrittenRev: "written-revision",
				Outputs:    map[string]any{"z-last": true, "a-first": "value"},
			}, nil
		},
	}, false, nil)
}

func applyCoordinatorBrowserFailure(
	t *testing.T,
	timeout bool,
) applyCoordinatorCaseGolden {
	t.Helper()
	name := "browser failure"
	openBrowser := func(context.Context, string) error {
		return errors.New("browser failed")
	}
	if timeout {
		name = "browser timeout"
		openBrowser = func(ctx context.Context, _ string) error {
			<-ctx.Done()
			return ctx.Err()
		}
	}
	view := &applyCoordinatorView{url: "http://127.0.0.1/run"}
	return runApplyCoordinatorCase(t, name, view, applyMachineOptions{
		browserTimeout: time.Millisecond,
		openBrowser:    openBrowser,
		apply: func(
			context.Context,
			*runtime.Executor,
			*runtime.PlanFile,
		) (*runtime.ExecResult, error) {
			return &runtime.ExecResult{WrittenRev: "written-revision"}, nil
		},
	}, false, nil)
}

func applyCoordinatorStepFailure(t *testing.T) applyCoordinatorCaseGolden {
	t.Helper()
	view := &applyCoordinatorView{url: "http://127.0.0.1/run"}
	return runApplyCoordinatorCase(t, "step failure", view, applyMachineOptions{
		openBrowser: func(context.Context, string) error { return nil },
		apply: func(
			_ context.Context,
			executor *runtime.Executor,
			_ *runtime.PlanFile,
		) (*runtime.ExecResult, error) {
			executor.Events <- runtime.ApplyEvent{
				Stage: runtime.StageFail, Decision: runtime.DecisionUpdate,
				Address: "resource.bad", Err: errors.New("provider failed"),
			}
			return nil, runtime.NewApplyFailure(
				runtime.ApplyFailureExecute,
				&runtime.ApplyError{
					Address: "resource.bad", Decision: runtime.DecisionUpdate,
					LibraryPath: "example.com/local", Err: errors.New("provider failed"),
				},
			)
		},
	}, false, nil)
}

func applyCoordinatorRevisionFailure(t *testing.T) applyCoordinatorCaseGolden {
	t.Helper()
	entry := runApplyCoordinatorCase(t, "revision read failure", nil, applyMachineOptions{
		apply: func(
			context.Context,
			*runtime.Executor,
			*runtime.PlanFile,
		) (*runtime.ExecResult, error) {
			return nil, runtime.NewApplyFailure(
				runtime.ApplyFailureFinalize, errors.New("persist failed"),
			)
		},
	}, false, nil)
	return entry
}

func applyCoordinatorCancellationDrain(t *testing.T) applyCoordinatorCaseGolden {
	t.Helper()
	return runApplyCoordinatorCase(t, "encoding cancellation drain", nil, applyMachineOptions{
		apply: func(
			ctx context.Context,
			executor *runtime.Executor,
			_ *runtime.PlanFile,
		) (*runtime.ExecResult, error) {
			executor.Events <- runtime.ApplyEvent{Stage: "queued", Address: "resource.bad"}
			select {
			case <-ctx.Done():
				return nil, context.Cause(ctx)
			case <-time.After(time.Second):
				return nil, errors.New("coordinator did not cancel execution")
			}
		},
	}, false, nil)
}

func applyCoordinatorInterruption(t *testing.T) applyCoordinatorCaseGolden {
	t.Helper()
	signals := make(chan os.Signal, 1)
	return runApplyCoordinatorCase(t, "interruption", nil, applyMachineOptions{
		apply: func(
			_ context.Context,
			executor *runtime.Executor,
			_ *runtime.PlanFile,
		) (*runtime.ExecResult, error) {
			signals <- os.Interrupt
			select {
			case <-executor.Drain:
			case <-time.After(time.Second):
				return nil, errors.New("drain was not requested")
			}
			return nil, runtime.NewApplyFailure(
				runtime.ApplyFailureExecute, runtime.ErrInterrupted,
			)
		},
	}, false, signals)
}

func applyCoordinatorEncodingFailure(t *testing.T) applyCoordinatorCaseGolden {
	t.Helper()
	return runApplyCoordinatorCase(t, "encoding failure", nil, applyMachineOptions{
		apply: func(
			context.Context,
			*runtime.Executor,
			*runtime.PlanFile,
		) (*runtime.ExecResult, error) {
			return &runtime.ExecResult{
				WrittenRev: "written-revision",
				Outputs:    map[string]any{"invalid": func() {}},
			}, nil
		},
	}, false, nil)
}

func applyCoordinatorInvalidResult(t *testing.T) applyCoordinatorCaseGolden {
	t.Helper()
	return runApplyCoordinatorCase(t, "invalid result before outputs", nil, applyMachineOptions{
		apply: func(
			context.Context,
			*runtime.Executor,
			*runtime.PlanFile,
		) (*runtime.ExecResult, error) {
			return &runtime.ExecResult{Outputs: map[string]any{"would-write": "value"}}, nil
		},
	}, false, nil)
}

func applyCoordinatorWriteFailure(t *testing.T) applyCoordinatorCaseGolden {
	t.Helper()
	return runApplyCoordinatorCase(t, "write failure", nil, applyMachineOptions{
		apply: func(
			context.Context,
			*runtime.Executor,
			*runtime.PlanFile,
		) (*runtime.ExecResult, error) {
			return &runtime.ExecResult{WrittenRev: "written-revision"}, nil
		},
	}, true, nil)
}

func runApplyCoordinatorCase(
	t *testing.T,
	name string,
	view *applyCoordinatorView,
	options applyMachineOptions,
	failWrite bool,
	signals chan os.Signal,
) applyCoordinatorCaseGolden {
	t.Helper()
	if signals == nil {
		signals = make(chan os.Signal, 1)
	}
	controller := newApplySignalController(
		context.Background(), signals, time.Hour, time.After,
	)
	writer := &applyCoordinatorWriter{fail: failWrite}
	stream := newApplyStream(writer, cmdout.FormatJSON, applyTestClock())
	prepared := &preparedApplyCommand{
		plan:   &runtime.PlanFile{},
		parsed: &parsedFactory{},
		store: &applyCoordinatorState{
			revision: "current-revision",
		},
	}
	if name == "revision read failure" {
		prepared.store = &applyCoordinatorState{err: errors.New("revision failed")}
	}
	options.now = applyTestClock()
	var producerDone atomic.Bool
	apply := options.apply
	options.apply = func(
		ctx context.Context,
		executor *runtime.Executor,
		plan *runtime.PlanFile,
	) (*runtime.ExecResult, error) {
		defer producerDone.Store(true)
		return apply(ctx, executor, plan)
	}
	var runView applyRunView
	if view != nil {
		runView = view
	}
	err := coordinateApplyMachine(
		stream, Info{}, prepared, controller, runView, options,
	)
	result := applyCoordinatorCaseGolden{
		Name: name, Stdout: writer.String(), Reported: cmdout.IsReported(err),
		ProducerDone: producerDone.Load(), ViewEvents: []string{},
	}
	result.Records, result.Terminals, result.TerminalLast = applyCoordinatorRecords(result.Stdout)
	if err != nil {
		result.Error = err.Error()
	}
	if view != nil {
		result.ViewEvents, result.ViewOK, result.ViewMessage = view.snapshot()
	}
	return result
}

func applyCoordinatorRecords(stdout string) (int, int, bool) {
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return 0, 0, false
	}
	terminals := 0
	terminalLast := false
	for index, line := range lines {
		var record struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal([]byte(line), &record) != nil {
			continue
		}
		terminal := record.Kind == "apply-result" || record.Kind == "apply-error"
		if terminal {
			terminals++
			terminalLast = index == len(lines)-1
		}
	}
	return len(lines), terminals, terminalLast
}

type applyCoordinatorState struct {
	state.Backend
	revision string
	err      error
}

func (s *applyCoordinatorState) CurrentRev() (string, error) {
	if s.err != nil {
		return "", s.err
	}
	if s.revision == "" {
		return "", state.ErrNoCurrent
	}
	return s.revision, nil
}

type applyCoordinatorWriter struct {
	bytes.Buffer
	fail bool
}

func (w *applyCoordinatorWriter) Write(value []byte) (int, error) {
	if w.fail {
		return 0, errors.New("writer failed")
	}
	return w.Buffer.Write(value)
}

type applyCoordinatorView struct {
	mu      sync.Mutex
	url     string
	events  []string
	ok      bool
	message string
}

func (v *applyCoordinatorView) URL() string {
	return v.url
}

func (v *applyCoordinatorView) Observe(event runtime.ApplyEvent) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.events = append(v.events, string(event.Stage)+":"+event.Address)
}

func (v *applyCoordinatorView) Complete(ok bool, message string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.ok = ok
	v.message = message
}

func (v *applyCoordinatorView) WaitServed(time.Duration) bool {
	return true
}

func (v *applyCoordinatorView) Close() {}

func (v *applyCoordinatorView) snapshot() ([]string, bool, string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]string{}, v.events...), v.ok, v.message
}
