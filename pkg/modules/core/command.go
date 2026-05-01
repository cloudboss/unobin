package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"time"
)

// CommandAction execs a single process and captures its output.
type CommandAction struct {
	Argv        []string          `mapstructure:"argv"`
	Environment map[string]string `mapstructure:"environment"`
	WorkingDir  string            `mapstructure:"working-dir"`
}

// CommandResult carries the captured output of a command run. Run returns
// an error when the process fails to start or the context is canceled.
type CommandResult struct {
	Stdout   string        `mapstructure:"stdout"`
	Stderr   string        `mapstructure:"stderr"`
	ExitCode int           `mapstructure:"exit-code"`
	Duration time.Duration `mapstructure:"duration"`
}

// Run execs argv[0] with argv[1:] as arguments. Environment is merged
// with the parent, with user-supplied variables taking precedence.
func (a *CommandAction) Run(ctx context.Context) (any, error) {
	if len(a.Argv) == 0 {
		return nil, errors.New("argv is required")
	}
	if a.Argv[0] == "" {
		return nil, fmt.Errorf("argv[0] is empty")
	}
	cmd := exec.CommandContext(ctx, a.Argv[0], a.Argv[1:]...)
	cmd.Env = mergedEnv(a.Environment)
	if a.WorkingDir != "" {
		cmd.Dir = a.WorkingDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	exitCode := 0
	if err != nil {
		exitErr, ok := errors.AsType[*exec.ExitError](err)
		if !ok {
			return nil, err
		}
		exitCode = exitErr.ExitCode()
	}

	return CommandResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
		Duration: duration,
	}, nil
}

func mergedEnv(extra map[string]string) []string {
	env := append([]string(nil), os.Environ()...)
	if len(extra) == 0 {
		return env
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+extra[k])
	}
	return env
}
