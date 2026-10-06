package local

import (
	"errors"
	"fmt"

	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	localstate "github.com/cloudboss/unobin/pkg/state/local"
)

const Name = "local"

type Config struct {
	Path string
}

func Type() state.BackendType {
	return state.BackendType{
		Name: Name, Description: "Local filesystem state backend.",
		Configuration: &cfg.ConfigurationType[any]{
			Description: "Local state backend configuration.",
			New:         func() any { return &Config{} },
		},
		New: newBackend,
	}
}

func newBackend(config any, factory, stack string, enc encrypt.Encrypter) (state.Backend, error) {
	c, ok := config.(*Config)
	if !ok {
		return nil, fmt.Errorf("local backend: missing or wrong configuration (got %T)", config)
	}
	if c.Path == "" {
		return nil, errors.New("local backend: path is required")
	}
	return localstate.NewStore(c.Path, factory, stack, enc)
}
