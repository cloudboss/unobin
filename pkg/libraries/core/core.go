package core

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

// Library returns the registration record for the builtin `core` library.
// Stacks reach its actions as `actions: { core: { command: { ... } } }` and
// its functions as `core.format(...)`.
func Library() *runtime.Library {
	return &runtime.Library{
		Name:        "core",
		Description: "Built-in actions and functions",
		Actions: map[string]runtime.ActionRegistration{
			"command":  runtime.MakeAction[CommandAction, *CommandActionOutput](),
			"script":   runtime.MakeAction[ScriptAction, *ScriptActionOutput](),
			"http":     runtime.MakeAction[HTTPAction, *HTTPActionOutput](),
			"wait-for": runtime.MakeAction[WaitForAction, *WaitForActionOutput](),
		},
		Functions: map[string]runtime.FunctionType{
			"format": {
				Name:        "format",
				Description: "Printf-style string formatting; the first argument is the format string.",
				ArgCount:    1,
				Variadic:    true,
				Func:        fnFormat,
			},
			"b64-encode": {
				Name:        "b64-encode",
				Description: "Base64-encode a string.",
				ArgCount:    1,
				Func:        fnB64Encode,
			},
			"b64-decode": {
				Name:        "b64-decode",
				Description: "Base64-decode a string.",
				ArgCount:    1,
				Func:        fnB64Decode,
			},
			"range": {
				Name:        "range",
				Description: "Return the integers [0, n) as a list.",
				ArgCount:    1,
				Func:        fnRange,
			},
			"length": {
				Name:        "length",
				Description: "Return the number of elements in a string, list, or map.",
				ArgCount:    1,
				Func:        fnLength,
			},
		},
	}
}
