package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/diagnostic"
)

type applySignalGolden struct {
	Cases []applySignalCaseGolden `json:"cases"`
}

type applySignalCaseGolden struct {
	Name         string                 `json:"name"`
	Notice       *diagnostic.Diagnostic `json:"notice"`
	DrainClosed  bool                   `json:"drain-closed"`
	ContextCause string                 `json:"context-cause"`
	SignalCause  string                 `json:"signal-cause"`
	Grace        string                 `json:"grace"`
}

func TestApplySignalControllerGolden(t *testing.T) {
	result := applySignalGolden{Cases: []applySignalCaseGolden{
		applySignalFirstInterrupt(t),
		applySignalGraceTimeout(t),
		applySignalSecondInterrupt(t),
		applySignalTermination(t),
		applySignalExternalCancel(t),
		applySignalStop(t),
	}}
	got, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')
	want, err := os.ReadFile("testdata/apply-signal.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}

type applySignalHarness struct {
	controller *applySignalController
	signals    chan os.Signal
	timer      chan time.Time
	durations  chan time.Duration
}

func newApplySignalHarness() applySignalHarness {
	signals := make(chan os.Signal, 2)
	timer := make(chan time.Time, 1)
	durations := make(chan time.Duration, 1)
	controller := newApplySignalController(
		context.Background(), signals, 42*time.Second,
		func(value time.Duration) <-chan time.Time {
			durations <- value
			return timer
		},
	)
	return applySignalHarness{
		controller: controller, signals: signals, timer: timer, durations: durations,
	}
}

func applySignalFirstInterrupt(t *testing.T) applySignalCaseGolden {
	t.Helper()
	harness := newApplySignalHarness()
	harness.signals <- os.Interrupt
	notice := receiveApplySignalNotice(t, harness.controller.Notices())
	grace := receiveApplySignalDuration(t, harness.durations)
	result := applySignalSnapshot(
		"first interrupt", harness.controller, notice, grace,
	)
	harness.controller.Stop()
	return result
}

func applySignalGraceTimeout(t *testing.T) applySignalCaseGolden {
	t.Helper()
	harness := newApplySignalHarness()
	harness.signals <- os.Interrupt
	notice := receiveApplySignalNotice(t, harness.controller.Notices())
	grace := receiveApplySignalDuration(t, harness.durations)
	harness.timer <- time.Unix(0, 0)
	waitApplySignalContext(t, harness.controller.Context())
	result := applySignalSnapshot(
		"grace timeout", harness.controller, notice, grace,
	)
	harness.controller.Stop()
	return result
}

func applySignalSecondInterrupt(t *testing.T) applySignalCaseGolden {
	t.Helper()
	harness := newApplySignalHarness()
	harness.signals <- os.Interrupt
	notice := receiveApplySignalNotice(t, harness.controller.Notices())
	grace := receiveApplySignalDuration(t, harness.durations)
	harness.signals <- os.Interrupt
	waitApplySignalContext(t, harness.controller.Context())
	result := applySignalSnapshot(
		"second interrupt", harness.controller, notice, grace,
	)
	harness.controller.Stop()
	return result
}

func applySignalTermination(t *testing.T) applySignalCaseGolden {
	t.Helper()
	harness := newApplySignalHarness()
	harness.signals <- syscall.SIGTERM
	waitApplySignalContext(t, harness.controller.Context())
	result := applySignalSnapshot(
		"termination", harness.controller, nil, 0,
	)
	harness.controller.Stop()
	return result
}

func applySignalExternalCancel(t *testing.T) applySignalCaseGolden {
	t.Helper()
	harness := newApplySignalHarness()
	harness.controller.Cancel(errors.New("encoding failed"))
	waitApplySignalContext(t, harness.controller.Context())
	result := applySignalSnapshot(
		"external cancellation", harness.controller, nil, 0,
	)
	harness.controller.Stop()
	return result
}

func applySignalStop(t *testing.T) applySignalCaseGolden {
	t.Helper()
	harness := newApplySignalHarness()
	harness.controller.Stop()
	return applySignalSnapshot("stop", harness.controller, nil, 0)
}

func applySignalSnapshot(
	name string,
	controller *applySignalController,
	notice *diagnostic.Diagnostic,
	grace time.Duration,
) applySignalCaseGolden {
	return applySignalCaseGolden{
		Name: name, Notice: notice, DrainClosed: applySignalChannelClosed(controller.Drain()),
		ContextCause: applySignalErrorString(context.Cause(controller.Context())),
		SignalCause:  applySignalErrorString(controller.SignalCause()),
		Grace:        grace.String(),
	}
}

func receiveApplySignalNotice(
	t *testing.T,
	notices <-chan diagnostic.Diagnostic,
) *diagnostic.Diagnostic {
	t.Helper()
	select {
	case notice := <-notices:
		return &notice
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for apply signal notice")
		return nil
	}
}

func receiveApplySignalDuration(t *testing.T, durations <-chan time.Duration) time.Duration {
	t.Helper()
	select {
	case duration := <-durations:
		return duration
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for apply signal timer")
		return 0
	}
}

func waitApplySignalContext(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for apply signal cancellation")
	}
}

func applySignalChannelClosed(channel <-chan struct{}) bool {
	select {
	case <-channel:
		return true
	default:
		return false
	}
}

func applySignalErrorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
