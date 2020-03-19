package lazy

import (
	"fmt"

	"github.com/cloudboss/go-player/pkg/playbook"
	"github.com/cloudboss/go-player/pkg/types"
)

type Interface func(*types.Frame) interface{}
type String func(*types.Frame) string
type Bool func(*types.Frame) bool

func S(s string) String {
	return func(*types.Frame) string {
		return s
	}
}

func B(b bool) Bool {
	return func(*types.Frame) bool {
		return b
	}
}

func False(*types.Frame) bool {
	return false
}

func True(*types.Frame) bool {
	return true
}

func EmptyString(*types.Frame) string {
	return ""
}

func Sprintf(format string, fs ...Interface) String {
	return func(frame *types.Frame) string {
		args := make([]interface{}, len(fs))
		for i, f := range fs {
			args[i] = f(frame)
		}
		return fmt.Sprintf(format, args...)
	}
}

func Output(task, path string) Interface {
	return func(frame *types.Frame) interface{} {
		moduleOutput, ok := frame.State[task].(map[string]interface{})
		if !ok {
			return ""
		}
		return playbook.ResolveString(moduleOutput, path)
	}
}
