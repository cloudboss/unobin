package command

import (
	"os"
	"strings"

	"github.com/cloudboss/go-player/pkg/commands"
	"github.com/cloudboss/go-player/pkg/types"
	"github.com/cloudboss/go-player/pkg/util"
)

const moduleName = "command"

type Command struct {
	Execute types.StringF
	Creates types.StringF
	Removes types.StringF
}

func (c *Command) Initialize() error {
	if c.Creates == nil {
		c.Creates = func() string { return "" }
	}
	if c.Removes == nil {
		c.Removes = func() string { return "" }
	}
	return nil
}

func (c *Command) Name() string {
	return moduleName
}

func (c *Command) Build() *types.Result {
	parts := strings.Fields(c.Execute())
	command := parts[0]
	args := parts[1:]
	return types.DoIf(
		moduleName,
		func() (bool, error) {
			return c.done()
		},
		func() *types.Result {
			commandOutput, err := commands.RunCommand(command, args...)
			if err != nil {
				return &types.Result{
					Module: moduleName,
					Error:  err.Error(),
				}
			}
			succeeded := commandOutput.ExitStatus == 0
			var errMsg string
			if !succeeded {
				errMsg = commandOutput.Stderr
			}
			return &types.Result{
				Succeeded: succeeded,
				Changed:   true,
				Error:     errMsg,
				Module:    moduleName,
				Output: map[string]interface{}{
					"exit_status":  commandOutput.ExitStatus,
					"stdout":       commandOutput.Stdout,
					"stderr":       commandOutput.Stderr,
					"stdout_lines": commandOutput.StdoutLines,
					"stderr_lines": commandOutput.StderrLines,
				},
			}
		},
	)
}

func (c *Command) Destroy() *types.Result {
	return nil
}

func (c *Command) done() (bool, error) {
	if c.Creates() == "" && c.Removes() == "" {
		return false, nil
	}

	var predicates []types.Predicate
	if c.Creates() != "" {
		predicates = append(predicates, func() (bool, error) {
			return c.created()
		})
	}
	if c.Removes() != "" {
		predicates = append(predicates, func() (bool, error) {
			return c.removed()
		})
	}

	var results []bool
	for _, predicate := range predicates {
		result, err := predicate()
		if err != nil {
			return false, err
		}
		results = append(results, result)
	}
	return util.All(results), nil
}

func (c *Command) created() (bool, error) {
	_, err := os.Stat(c.Creates())
	if err == nil {
		return true, nil
	}
	return false, nil
}

func (c *Command) removed() (bool, error) {
	_, err := os.Stat(c.Removes())
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	return false, nil
}
