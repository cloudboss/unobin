package lazy

import (
	"fmt"

	"github.com/cloudboss/go-player/pkg/playbook"
	"github.com/cloudboss/go-player/pkg/types"
)

type Interface func(*types.Frame) InterfaceValue
type InterfaceValue func() interface{}
type String func(*types.Frame) StringValue
type StringValue func() string
type Bool func(*types.Frame) BoolValue
type BoolValue func() bool

func S(s string) String {
	return func(*types.Frame) StringValue {
		return func() string {
			return s
		}
	}
}

func B(b bool) Bool {
	return func(*types.Frame) BoolValue {
		return func() bool {
			return b
		}
	}
}

func False() bool {
	return false
}

func True() bool {
	return true
}

func EmptyString() string {
	return ""
}

func Sprintf(format string, fs ...InterfaceValue) StringValue {
	return func() string {
		args := make([]interface{}, len(fs))
		for i, f := range fs {
			args[i] = f()
		}
		return fmt.Sprintf(format, args...)
	}
}

func Output(task, path string) Interface {
	return func(frame *types.Frame) InterfaceValue {
		return func() interface{} {
			moduleOutput, ok := frame.State[task].(map[string]interface{})
			if !ok {
				return ""
			}
			return playbook.ResolveString(moduleOutput, path)
		}
	}
}
