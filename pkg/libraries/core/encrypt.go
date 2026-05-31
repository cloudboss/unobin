package core

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/envencrypt"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
)

// EnvKeyConfig is the operator-facing body under
// `encryption: { @key-source: env ... }`.
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

// newNoop builds the no-op encrypter, which writes state as plaintext.
// It is the explicit opt-out for unencrypted state, chosen as core.noop
// in a config's encryption block.
func newNoop(_ any) (sdkencrypt.Encrypter, error) {
	return envencrypt.Noop{}, nil
}
