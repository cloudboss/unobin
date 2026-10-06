package encrypters

import "github.com/cloudboss/unobin/pkg/encrypters/local"

type EnvKey = local.EnvKey

func NewEnvKey(envVar string) (*EnvKey, error) {
	return local.NewEnvKey(envVar)
}
