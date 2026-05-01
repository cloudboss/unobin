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
			"script": {
				Name:        "script",
				Description: "Run a shell script and capture its output",
				New:         func() runtime.Action { return &ScriptAction{} },
			},
			"http": {
				Name:        "http",
				Description: "Issue an HTTP request and capture the response",
				New:         func() runtime.Action { return &HTTPAction{} },
			},
			"wait-for": {
				Name:        "wait-for",
				Description: "Poll a command until it exits 0 or a timeout fires",
				New:         func() runtime.Action { return &WaitForAction{} },
			},
		},
	}
}
