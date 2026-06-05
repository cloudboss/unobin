package core

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cloudboss/unobin/pkg/defaults"
)

// WaitForAction polls a command until it exits 0 or the deadline is reached.
// The command runs at most once per Interval (default 1s) and the whole
// poll loop runs for at most Timeout (default 5m).
type WaitForAction struct {
	Argv        []string
	Interval    time.Duration
	Timeout     time.Duration
	Environment map[string]string
	WorkingDir  string
}

// Defaults declares the inputs a body may leave out: one attempt per
// second for up to five minutes, an absent environment adding nothing
// to the parent's, and an absent working-dir inheriting the process
// directory.
func (a WaitForAction) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(a.Interval, time.Second),
		defaults.Value(a.Timeout, 5*time.Minute),
		defaults.Optional(a.Environment),
		defaults.Optional(a.WorkingDir),
	}
}

// WaitForActionOutput records how many attempts ran, the elapsed time, and the
// stdout/stderr of the successful attempt.
type WaitForActionOutput struct {
	Attempts int
	Duration time.Duration
	Stdout   string
	Stderr   string
}

// Run polls until the command exits 0, the timeout fires, or the context
// is cancelled. A nonzero exit triggers another attempt, and an error is
// returned if the process fails to start.
func (a *WaitForAction) Run(ctx context.Context, _ any) (*WaitForActionOutput, error) {
	if len(a.Argv) == 0 {
		return nil, errors.New("argv is required")
	}
	start := time.Now()
	deadline := start.Add(a.Timeout)
	var attempts int

	for {
		attempts++
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, fmt.Errorf("wait-for timed out after %d attempts (%v)", attempts-1, a.Timeout)
		}
		attemptCtx, cancel := context.WithTimeout(ctx, remaining)
		result, err := runProcess(attemptCtx, processSpec{
			Argv:        a.Argv,
			Environment: a.Environment,
			WorkingDir:  a.WorkingDir,
		})
		cancel()

		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, fmt.Errorf("wait-for timed out after %d attempts (%v)", attempts, a.Timeout)
			}
			return nil, err
		}
		if result.ExitCode == 0 {
			return &WaitForActionOutput{
				Attempts: attempts,
				Duration: time.Since(start),
				Stdout:   result.Stdout,
				Stderr:   result.Stderr,
			}, nil
		}

		select {
		case <-time.After(a.Interval):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
