package local

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
)

const (
	EnvKeyName = "env-key"
	NoopName   = "noop"
)

type EnvKeyConfig struct {
	EnvVar string
}

func Types() []sdkencrypt.EncrypterType {
	return []sdkencrypt.EncrypterType{
		{
			Name:        EnvKeyName,
			Description: "AES-256-GCM with a base64 key read from an env input.",
			Configuration: &cfg.ConfigurationType[any]{
				Description: "Env-key encrypter configuration.",
				New:         func() any { return &EnvKeyConfig{} },
			},
			New: newEnvKey,
		},
		{
			Name: NoopName, Description: "No encryption; state is written as plaintext.",
			New: func(_ any, _ map[string]any) (sdkencrypt.Encrypter, error) {
				return Noop{}, nil
			},
		},
	}
}

func newEnvKey(config any, _ map[string]any) (sdkencrypt.Encrypter, error) {
	c, ok := config.(*EnvKeyConfig)
	if !ok {
		return nil, fmt.Errorf("env-key encrypter: missing or wrong configuration (got %T)", config)
	}
	return NewEnvKey(c.EnvVar)
}
