package core

import (
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
)

// Module returns the registration record for the builtin `core` module.
// Stacks reach its actions as `actions: { core: { command: { ... } } }`.
// State and encrypter dispatch use the same module: a bare
// `@backend: local` resolves to StateBackends["local"], and
// `@key-source: env-key` resolves to Encrypters["env-key"].
func Module() *runtime.Module {
	return &runtime.Module{
		Name:        "core",
		Description: "Built-in actions, state backend, and encrypter",
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
		},
	}
}
