package core

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// WaitForAction polls a command until it exits 0 or the deadline is reached.
// The command runs at most once per Interval (default 1s) and the whole
// poll loop runs for at most Timeout (default 5m).
type WaitForAction struct {
	Argv        []string          `mapstructure:"argv"`
	Interval    time.Duration     `mapstructure:"interval"`
	Timeout     time.Duration     `mapstructure:"timeout"`
	Environment map[string]string `mapstructure:"environment"`
	WorkingDir  string            `mapstructure:"working-dir"`
}

// WaitForResult records how many attempts ran, the elapsed time, and the
// stdout/stderr of the successful attempt.
type WaitForResult struct {
	Attempts int           `mapstructure:"attempts"`
	Duration time.Duration `mapstructure:"duration"`
	Stdout   string        `mapstructure:"stdout"`
	Stderr   string        `mapstructure:"stderr"`
}

// Run polls until the command exits 0, the timeout fires, or the context
// is cancelled. A nonzero exit triggers another attempt, and an error is
// returned if the process fails to start.
func (a *WaitForAction) Run(ctx context.Context, _ any) (any, error) {
	if len(a.Argv) == 0 {
		return nil, errors.New("argv is required")
	}
	interval := a.Interval
	if interval <= 0 {
		interval = time.Second
	}
	timeout := a.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	start := time.Now()
	deadline := start.Add(timeout)
	var attempts int

	for {
		attempts++
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, fmt.Errorf("wait-for timed out after %d attempts (%v)", attempts-1, timeout)
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
				return nil, fmt.Errorf("wait-for timed out after %d attempts (%v)", attempts, timeout)
			}
			return nil, err
		}
		if result.ExitCode == 0 {
			return WaitForResult{
				Attempts: attempts,
				Duration: time.Since(start),
				Stdout:   result.Stdout,
				Stderr:   result.Stderr,
			}, nil
		}

		select {
		case <-time.After(interval):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
