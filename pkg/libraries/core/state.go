package core

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/localstate"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
)

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
