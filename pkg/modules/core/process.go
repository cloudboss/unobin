package core

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"sort"
	"time"
)

// processSpec is the shape both CommandAction and ScriptAction reduce to:
// an argv to exec, an env to merge, and an optional working directory.
type processSpec struct {
	Argv        []string
	Environment map[string]string
	WorkingDir  string
}

func runProcess(ctx context.Context, spec processSpec) (CommandActionOutput, error) {
	cmd := exec.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
	cmd.Env = mergedEnv(spec.Environment)
	if spec.WorkingDir != "" {
		cmd.Dir = spec.WorkingDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	if ctxErr := ctx.Err(); ctxErr != nil {
		return CommandActionOutput{}, ctxErr
	}
	exitCode := 0
	if err != nil {
		exitErr, ok := errors.AsType[*exec.ExitError](err)
		if !ok {
			return CommandActionOutput{}, err
		}
		exitCode = exitErr.ExitCode()
	}

	return CommandActionOutput{
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
