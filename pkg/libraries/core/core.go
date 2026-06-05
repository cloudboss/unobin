package core

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

// Library returns the registration record for the builtin `core` library.
// Stacks reach its actions as `actions: { core: { command: { ... } } }`.
// Functions live in the language's @core namespace, not here.
func Library() *runtime.Library {
	return &runtime.Library{
		Name:        "core",
		Description: "Built-in actions",
		Actions: map[string]runtime.ActionRegistration{
			"command":  runtime.MakeAction[CommandAction, *CommandActionOutput](),
			"script":   runtime.MakeAction[ScriptAction, *ScriptActionOutput](),
			"http":     runtime.MakeAction[HTTPAction, *HTTPActionOutput](),
			"wait-for": runtime.MakeAction[WaitForAction, *WaitForActionOutput](),
		},
	}
}
