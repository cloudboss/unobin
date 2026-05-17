package core

import (
	"context"
	"errors"
	"fmt"
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
func (a *CommandAction) Run(ctx context.Context, _ any) (any, error) {
	if len(a.Argv) == 0 {
		return nil, errors.New("argv is required")
	}
	if a.Argv[0] == "" {
		return nil, fmt.Errorf("argv[0] is empty")
	}
	return runProcess(ctx, processSpec{
		Argv:        a.Argv,
		Environment: a.Environment,
		WorkingDir:  a.WorkingDir,
	})
}
