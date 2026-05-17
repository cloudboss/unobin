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
