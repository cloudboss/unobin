package lazy

import (
	"fmt"
	"strings"

	"github.com/cloudboss/unobin/pkg/commands"
	"github.com/cloudboss/unobin/pkg/playbook"
	"github.com/cloudboss/unobin/pkg/types"
)

type Interface func(*types.Context) InterfaceValue
type InterfaceValue func() (interface{}, error)
type String func(*types.Context) StringValue
type StringValue func() (string, error)
type Bool func(*types.Context) BoolValue
type BoolValue func() (bool, error)

func S(s string) String {
	return func(*types.Context) StringValue {
		return func() (string, error) {
			return s, nil
		}
	}
}

func B(b bool) Bool {
	return func(*types.Context) BoolValue {
		return func() (bool, error) {
			return b, nil
		}
	}
}

func False() (bool, error) {
	return false, nil
}

func True() (bool, error) {
	return true, nil
}

func EmptyString() (string, error) {
	return "", nil
}

func Format(format StringValue, fs ...InterfaceValue) String {
	return func(_ctx *types.Context) StringValue {
		return func() (string, error) {
			formatStr, err := format()
			if err != nil {
				return "", err
			}
			args := make([]interface{}, len(fs))
			for i, f := range fs {
				args[i], err = f()
				if err != nil {
					return "", err
				}
			}
			return fmt.Sprintf(formatStr, args...), nil
		}
	}
}

func AnyOutput(task, path StringValue) Interface {
	return func(ctx *types.Context) InterfaceValue {
		return func() (interface{}, error) {
			taskStr, err := task()
			if err != nil {
				return "", err
			}
			pathStr, err := path()
			if err != nil {
				return "", err
			}
			moduleOutput, ok := ctx.State[taskStr].(map[string]interface{})
			if !ok {
				return "", fmt.Errorf("task `%s` output not found", taskStr)
			}
			s, err := playbook.ResolveString(moduleOutput, pathStr)
			if err != nil {
				return "", err
			}
			return s, nil
		}
	}
}

func StringOutput(task, path StringValue) String {
	return func(ctx *types.Context) StringValue {
		return func() (string, error) {
			taskStr, err := task()
			if err != nil {
				return "", err
			}
			pathStr, err := path()
			if err != nil {
				return "", err
			}
			moduleOutput, ok := ctx.State[taskStr].(map[string]interface{})
			if !ok {
				return "", fmt.Errorf("task `%s` output not found", taskStr)
			}
			s, err := playbook.ResolveString(moduleOutput, pathStr)
			if err != nil {
				return "", err
			}
			return s, nil
		}
	}
}

func StringVar(path StringValue) String {
	return func(ctx *types.Context) StringValue {
		return func() (string, error) {
			pathStr, err := path()
			if err != nil {
				return "", err
			}
			s, err := playbook.ResolveString(ctx.Vars, pathStr)
			if err != nil {
				return "", err
			}
			return s, nil
		}
	}
}

func AnyVar(path StringValue) Interface {
	return func(ctx *types.Context) InterfaceValue {
		return func() (interface{}, error) {
			pathStr, err := path()
			if err != nil {
				return "", err
			}
			s, err := playbook.ResolveString(ctx.Vars, pathStr)
			if err != nil {
				return "", err
			}
			return s, nil
		}
	}
}

func WhenExecute(expr StringValue) Bool {
	return func(_ctx *types.Context) BoolValue {
		return func() (bool, error) {
			exec, err := expr()
			if err != nil {
				return false, err
			}
			parts := strings.Fields(exec)
			command := parts[0]
			var args []string
			if len(parts) > 1 {
				args = parts[1:]
			}
			commandOutput, err := commands.RunCommand(command, args...)
			if err != nil {
				return false, err
			}
			return commandOutput.ExitStatus == 0, nil
		}
	}
}
