package task

import (
	"strings"

	"github.com/cloudboss/go-player/pkg/commands"
	"github.com/cloudboss/go-player/pkg/module"
	"github.com/cloudboss/go-player/pkg/types"
)

type Whener func() (bool, error)

type Task struct {
	Name       string                 `json:"name"`
	ModuleName string                 `json:"module"`
	Params     map[string]interface{} `json:"params,omitempty"`
	Module     module.Module
	When       Whener
}

func WhenExecute(exec string) Whener {
	parts := strings.Fields(exec)
	command := parts[0]
	var args []string
	if len(parts) > 1 {
		args = parts[1:]
	}
	return func() (bool, error) {
		commandOutput, err := commands.RunCommand(command, args...)
		if err != nil {
			return false, err
		}
		return commandOutput.ExitStatus == 0, nil
	}
}

func (t Task) Run() *types.Result {
	if t.When != nil {
		runTask, err := t.When()
		if err != nil {
			return &types.Result{
				Succeeded: false,
				Changed:   false,
				Module:    t.Module.Name(),
				Error:     err.Error(),
			}
		}
		if !runTask {
			return &types.Result{
				Succeeded: true,
				Changed:   false,
				Module:    t.Module.Name(),
			}
		}
	}
	return t.Module.Build()
}
