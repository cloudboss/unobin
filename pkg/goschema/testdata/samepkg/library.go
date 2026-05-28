package samepkg

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "samepkg",
		Actions: map[string]runtime.ActionRegistration{
			"do":  runtime.MakeAction[DoAction, *DoActionOutput](),
			"do2": runtime.MakeAction[Do2Action, *Do2ActionOutput](),
		},
	}
}
