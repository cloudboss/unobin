package samepkg

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func Module() *runtime.Module {
	return &runtime.Module{
		Name: "samepkg",
		Actions: map[string]runtime.ActionType{
			"do": {
				Name: "do",
				New:  func() runtime.Action { return &DoAction{} },
			},
			"do2": {
				Name: "do2",
				New:  func() runtime.Action { return &Do2Action{} },
			},
		},
	}
}
