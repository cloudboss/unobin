package factorycli

import (
	"errors"
	"fmt"

	localbackend "github.com/cloudboss/unobin/pkg/backends/local"
	localencrypt "github.com/cloudboss/unobin/pkg/encrypters/local"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
)

// A nil selection enables local state, env-key, and noop encryption.
// An explicit empty selection enables no implementations.
type Registries struct {
	Backends   []sdkstate.BackendType
	Encrypters []sdkencrypt.EncrypterType
}

type registry struct {
	backends   map[string]sdkstate.BackendType
	encrypters map[string]sdkencrypt.EncrypterType
}

func (info Info) registered() (*registry, error) {
	if info.options != nil {
		return info.options.registry, info.options.registryError
	}
	return info.Registries.build()
}

func (r *Registries) build() (*registry, error) {
	if r == nil {
		r = &Registries{
			Backends:   []sdkstate.BackendType{localbackend.Type()},
			Encrypters: localencrypt.Types(),
		}
	}
	result := &registry{
		backends:   make(map[string]sdkstate.BackendType, len(r.Backends)),
		encrypters: make(map[string]sdkencrypt.EncrypterType, len(r.Encrypters)),
	}
	for _, backend := range r.Backends {
		if backend.Name == "" {
			return nil, errors.New("state: backend name is empty")
		}
		if _, exists := result.backends[backend.Name]; exists {
			return nil, fmt.Errorf("state: duplicate backend %q", backend.Name)
		}
		if backend.New == nil {
			return nil, fmt.Errorf("state: backend %q has no constructor", backend.Name)
		}
		if backend.Configuration != nil {
			if err := cfg.ValidateConfigurationType(backend.Configuration); err != nil {
				return nil, fmt.Errorf("state: backend %q: %w", backend.Name, err)
			}
		}
		result.backends[backend.Name] = backend
	}
	for _, encrypter := range r.Encrypters {
		if encrypter.Name == "" {
			return nil, errors.New("encryption: key-source name is empty")
		}
		if _, exists := result.encrypters[encrypter.Name]; exists {
			return nil, fmt.Errorf("encryption: duplicate key-source %q", encrypter.Name)
		}
		if encrypter.New == nil {
			return nil, fmt.Errorf("encryption: key-source %q has no constructor", encrypter.Name)
		}
		if encrypter.Configuration != nil {
			if err := cfg.ValidateConfigurationType(encrypter.Configuration); err != nil {
				return nil, fmt.Errorf("encryption: key-source %q: %w", encrypter.Name, err)
			}
		}
		result.encrypters[encrypter.Name] = encrypter
	}
	return result, nil
}
