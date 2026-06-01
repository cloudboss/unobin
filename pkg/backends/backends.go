// Package backends holds the fixed set of state backends and encrypters a
// factory can use. An operator selects one by bare name in a config's
// state: block (@backend, @key-source), and the resolver looks the name up
// here.
package backends

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/envencrypt"
	"github.com/cloudboss/unobin/pkg/localstate"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
)

// Backends returns the state backends keyed by the bare name an operator
// selects with @backend. Names are unique by construction: this is one map
// literal, so a duplicate is a compile error.
func Backends() map[string]sdkstate.BackendType {
	return map[string]sdkstate.BackendType{
		"local": {
			Name:        "local",
			Description: "Local filesystem state backend.",
			Configuration: &cfg.ConfigurationType{
				Description: "Local state backend configuration.",
				New:         func() any { return &LocalBackendConfig{} },
			},
			New: newLocalBackend,
		},
	}
}

// Encrypters returns the state encrypters keyed by the bare name an operator
// selects with @key-source.
func Encrypters() map[string]sdkencrypt.EncrypterType {
	return map[string]sdkencrypt.EncrypterType{
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
	}
}

// LocalBackendConfig is the operator-facing body under
// `state: { @backend: local ... }`.
type LocalBackendConfig struct {
	Path cfg.String
}

func newLocalBackend(
	config any,
	factory, stack string,
	enc sdkencrypt.Encrypter,
) (sdkstate.Backend, error) {
	c, ok := config.(*LocalBackendConfig)
	if !ok {
		return nil, fmt.Errorf("local backend: missing or wrong configuration (got %T)", config)
	}
	return localstate.NewLocalStore(c.Path.Value, factory, stack, enc)
}

// EnvKeyConfig is the operator-facing body under
// `encryption: { @key-source: env-key ... }`.
type EnvKeyConfig struct {
	EnvVar cfg.String
}

func newEnvKey(config any) (sdkencrypt.Encrypter, error) {
	c, ok := config.(*EnvKeyConfig)
	if !ok {
		return nil, fmt.Errorf("env-key encrypter: missing or wrong configuration (got %T)", config)
	}
	return envencrypt.NewEnvKey(c.EnvVar.Value)
}

// newNoop builds the no-op encrypter, which writes state as plaintext. It is
// the explicit opt-out for unencrypted state, selected as `noop` in a
// config's encryption block.
func newNoop(_ any) (sdkencrypt.Encrypter, error) {
	return envencrypt.Noop{}, nil
}
