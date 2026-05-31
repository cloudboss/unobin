package core

import (
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
)

// Library returns the registration record for the builtin `core` library.
// Stacks reach its actions as `actions: { core: { command: { ... } } }`.
// State and encrypter dispatch use the same library: a bare
// `@backend: local` resolves to StateBackends["local"], and
// `@key-source: env-key` resolves to Encrypters["env-key"].
func Library() *runtime.Library {
	return &runtime.Library{
		Name:        "core",
		Description: "Built-in actions, functions, state backend, and encrypter",
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
		StateBackends: map[string]sdkstate.BackendType{
			"local": {
				Name:        "local",
				Description: "Local filesystem state backend.",
				Configuration: &cfg.ConfigurationType{
					Description: "Local state backend configuration.",
					New:         func() any { return &LocalBackendConfig{} },
				},
				New: newLocalBackend,
			},
		},
		Encrypters: map[string]sdkencrypt.EncrypterType{
			"env-key": {
				Name:        "env-key",
				Description: "AES-256-GCM with a base64 key read from an env var.",
				Configuration: &cfg.ConfigurationType{
					Description: "Env-key encrypter configuration.",
					New:         func() any { return &EnvKeyConfig{} },
				},
				New: newEnvKey,
			},
			"noop": {
				Name:        "noop",
				Description: "No encryption; state is written as plaintext.",
				New:         newNoop,
			},
		},
	}
}
