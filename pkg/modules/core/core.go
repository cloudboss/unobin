package core

import "github.com/cloudboss/unobin/pkg/runtime"

// Module returns the registration record for the builtin `core` module.
// Stacks reach its actions as `actions: { core: { command: { ... } } }`.
func Module() *runtime.Module {
	return &runtime.Module{
		Name:        "core",
		Description: "Built-in actions: command, http, wait-for, script",
		Actions: map[string]runtime.ActionType{
			"command": {
				Name:        "command",
				Description: "Execute a process and capture its output",
				New:         func() runtime.Action { return &CommandAction{} },
			},
		},
	}
}
