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
		Actions: map[string]runtime.ActionRegistration{
			"command":  runtime.MakeAction[CommandAction, *CommandActionOutput](),
			"script":   runtime.MakeAction[ScriptAction, *ScriptActionOutput](),
			"http":     runtime.MakeAction[HTTPAction, *HTTPActionOutput](),
			"wait-for": runtime.MakeAction[WaitForAction, *WaitForActionOutput](),
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
