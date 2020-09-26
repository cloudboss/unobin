package command

import (
	"os"
	"strings"

	"github.com/cloudboss/unobin/pkg/commands"
	"github.com/cloudboss/unobin/pkg/lazy"
	"github.com/cloudboss/unobin/pkg/types"
	"github.com/cloudboss/unobin/pkg/util"
)

const moduleName = "command"

type Command struct {
	Execute lazy.StringValue
	Creates lazy.StringValue
	Removes lazy.StringValue
}

func (c *Command) Initialize() error {
	if c.Creates == nil {
		c.Creates = lazy.EmptyString
	}
	if c.Removes == nil {
		c.Removes = lazy.EmptyString
	}
	return nil
}

func (c *Command) Name() string {
	return moduleName
}

func (c *Command) Apply() *types.Result {
	execute, err := c.Execute()
	if err != nil {
		return util.ErrResult(err.Error(), moduleName)
	}
	parts := strings.Fields(execute)
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
	creates, err := c.Creates()
	if err != nil {
		return false, err
	}
	removes, err := c.Removes()
	if err != nil {
		return false, err
	}
	if creates == "" && removes == "" {
		return false, nil
	}

	var predicates []types.Predicate
	if creates != "" {
		predicates = append(predicates, func() (bool, error) {
			return c.created()
		})
	}
	if removes != "" {
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
	creates, err := c.Creates()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(creates)
	if err == nil {
		return true, nil
	}
	return false, nil
}

func (c *Command) removed() (bool, error) {
	removes, err := c.Removes()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(removes)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	return false, nil
}
