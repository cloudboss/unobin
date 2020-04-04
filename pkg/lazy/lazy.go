package lazy

import (
	"fmt"

	"github.com/cloudboss/go-player/pkg/playbook"
	"github.com/cloudboss/go-player/pkg/types"
)

type Interface func(*types.Frame) InterfaceValue
type InterfaceValue func() (interface{}, error)
type String func(*types.Frame) StringValue
type StringValue func() (string, error)
type Bool func(*types.Frame) BoolValue
type BoolValue func() (bool, error)

func S(s string) String {
	return func(*types.Frame) StringValue {
		return func() (string, error) {
			return s, nil
		}
	}
}

func B(b bool) Bool {
	return func(*types.Frame) BoolValue {
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

func Sprintf(format string, fs ...InterfaceValue) StringValue {
	return func() (string, error) {
		args := make([]interface{}, len(fs))
		var err error
		for i, f := range fs {
			args[i], err = f()
			if err != nil {
				return "", err
			}
		}
		return fmt.Sprintf(format, args...), nil
	}
}

func Output(task, path string) Interface {
	return func(frame *types.Frame) InterfaceValue {
		return func() (interface{}, error) {
			moduleOutput, ok := frame.State[task].(map[string]interface{})
			if !ok {
				return "", fmt.Errorf("task `%s` output not found", task)
			}
			s, err := playbook.ResolveString(moduleOutput, path)
			if err != nil {
				return "", err
			}
			return s, nil
		}
	}
}

func Var(path string) Interface {
	return func(frame *types.Frame) InterfaceValue {
		return func() (interface{}, error) {
			s, err := playbook.ResolveString(frame.Vars, path)
			if err != nil {
				return "", err
			}
			return s, nil
		}
	}
}
