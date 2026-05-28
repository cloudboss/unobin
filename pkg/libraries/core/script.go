package core

import (
	"context"
	"errors"
)

// ScriptAction runs a shell script via `<shell> -c <script>`. Shell defaults
// to `sh`; set it to `bash`, `python3`, or any other interpreter that
// accepts `-c`.
type ScriptAction struct {
	Script      string
	Shell       string
	Environment map[string]string
	WorkingDir  string
}

// ScriptActionOutput is the captured output of a script run. It is the
// same shape as the command action's output because both reduce to a
// process exec; the alias keeps the convention that every action type
// has a sibling type named `<GoName>Output`.
type ScriptActionOutput = CommandActionOutput

// Run invokes the configured shell with the script. Output mirrors
// what CommandAction returns.
func (a *ScriptAction) Run(ctx context.Context, _ any) (*ScriptActionOutput, error) {
	if a.Script == "" {
		return nil, errors.New("script is required")
	}
	shell := a.Shell
	if shell == "" {
		shell = "sh"
	}
	return runProcess(ctx, processSpec{
		Argv:        []string{shell, "-c", a.Script},
		Environment: a.Environment,
		WorkingDir:  a.WorkingDir,
	})
}
