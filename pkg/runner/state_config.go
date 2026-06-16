package runner

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/backends"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
)

// resolverRef names one entry in the fixed backend or encrypter registry.
// Name is the bare state or encryption selector from the stack file;
// Body is the operator-provided configuration for it.
type resolverRef struct {
	Name string
	Body map[string]any
}

// stateConfig captures the parsed state: block from the stack file. A
// nil field means the operator omitted that section; the resolver fills
// in defaults.
type stateConfig struct {
	Backend   *resolverRef
	Encrypter *resolverRef
}

// parseStateConfig extracts the `state:` and `encryption:` declarations from
// a pre-parsed stack config. A nil config or an absent declaration leaves the
// matching field nil and the caller falls back to defaults. The declarations
// are already structurally validated; this function evaluates body expressions,
// with the file's locals in scope, and packages the values for the resolver.
// path is preserved only for error messages from Eval.
func parseStateConfig(config *parsedConfig, path string) (*stateConfig, error) {
	stack := configStack(config)
	if stack == nil {
		return &stateConfig{}, nil
	}
	sc := &stateConfig{}
	ctx := configEvalContext(config)
	var stateErr, encErr error
	if stack.State != nil {
		sc.Backend, stateErr = readResolverDecl(
			path, "state", stack.State.Selector.Name, stack.State.Body, ctx)
	}
	if stack.Encryption != nil {
		sc.Encrypter, encErr = readResolverDecl(
			path, "encryption", stack.Encryption.Selector.Name, stack.Encryption.Body, ctx)
	}
	if err := errors.Join(stateErr, encErr); err != nil {
		return nil, err
	}
	return sc, nil
}

func readResolverDecl(
	configPath string,
	section string,
	name string,
	bodyExpr *lang.ObjectLit,
	ctx *runtime.EvalContext,
) (*resolverRef, error) {
	if name == "" {
		return nil, nil
	}
	body := map[string]any{}
	var errs []error
	if bodyExpr == nil {
		return &resolverRef{Name: name, Body: body}, nil
	}
	for _, fld := range bodyExpr.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		val, err := runtime.Eval(fld.Value, ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"%s: %s.%s: %w", configPath, section, fld.Key.Name, err))
			continue
		}
		body[fld.Key.Name] = val
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return &resolverRef{Name: name, Body: body}, nil
}

// defaultKeyEnvVar is the env var the resolver falls back to when a
// config has no encryption block.
const defaultKeyEnvVar = "UB_STATE_KEY"

// resolveEncrypter constructs the encrypter named by the parsed state
// config. A nil ref means the operator omitted the encryption block;
// the resolver falls back to env-key against the default key env var,
// or the no-op if that env var is unset.
func resolveEncrypter(ref *resolverRef) (sdkencrypt.Encrypter, error) {
	if ref == nil {
		if os.Getenv(defaultKeyEnvVar) == "" {
			return encrypters.Noop{}, nil
		}
		return encrypters.NewEnvKey(defaultKeyEnvVar)
	}
	rt, err := lookupEncrypterType(ref)
	if err != nil {
		return nil, err
	}
	decoded, err := decodeRefConfig(rt.Configuration, ref)
	if err != nil {
		return nil, fmt.Errorf("encryption: %w", err)
	}
	return rt.New(decoded, ref.Body)
}

// resolveBackend constructs the backend named by the parsed state
// config. A nil ref means the config has no state: block, which is an
// error: a state backend must be configured explicitly.
func resolveBackend(
	ref *resolverRef,
	factory, stack string,
	enc sdkencrypt.Encrypter,
) (sdkstate.Backend, error) {
	if ref == nil {
		return nil, errors.New(
			"state: a state backend must be configured; add a state: block to the stack file " +
				"(run 'schema template' for a starter)")
	}
	bt, err := lookupBackendType(ref)
	if err != nil {
		return nil, err
	}
	decoded, err := decodeRefConfig(bt.Configuration, ref)
	if err != nil {
		return nil, fmt.Errorf("state: %w", err)
	}
	return bt.New(decoded, factory, stack, enc)
}

// lookupBackendType finds the named backend in the fixed registry, or
// reports the available names.
func lookupBackendType(ref *resolverRef) (sdkstate.BackendType, error) {
	registry := backends.Backends()
	bt, ok := registry[ref.Name]
	if !ok {
		return sdkstate.BackendType{}, fmt.Errorf(
			"state: no backend named %q; available: %s", ref.Name, sortedNames(registry))
	}
	return bt, nil
}

// lookupEncrypterType finds the named encrypter in the fixed registry, or
// reports the available names.
func lookupEncrypterType(ref *resolverRef) (sdkencrypt.EncrypterType, error) {
	registry := encrypters.Encrypters()
	et, ok := registry[ref.Name]
	if !ok {
		return sdkencrypt.EncrypterType{}, fmt.Errorf(
			"encryption: no key-source named %q; available: %s", ref.Name, sortedNames(registry))
	}
	return et, nil
}

func decodeRefConfig(ct *cfg.ConfigurationType, ref *resolverRef) (any, error) {
	if ct == nil {
		if len(ref.Body) > 0 {
			return nil, fmt.Errorf("%q accepts no configuration fields", ref.Name)
		}
		return nil, nil
	}
	return cfg.Decode(ct, ref.Body)
}

// sortedNames renders the registry keys in sorted order for an error that
// lists the available backends or encrypters.
func sortedNames[V any](m map[string]V) string {
	return strings.Join(slices.Sorted(maps.Keys(m)), ", ")
}

// toRuntimeStateRef copies a resolverRef into the public runtime type used
// inside the plan file. Returns nil when ref is nil so the plan field stays
// omit-empty.
func toRuntimeStateRef(ref *resolverRef) *runtime.StateRef {
	if ref == nil {
		return nil
	}
	return &runtime.StateRef{Name: ref.Name, Body: ref.Body}
}

// fromRuntimeStateRef is the inverse of toRuntimeStateRef. Apply uses it to
// feed pf.Backend back through the resolver.
func fromRuntimeStateRef(ref *runtime.StateRef) *resolverRef {
	if ref == nil {
		return nil
	}
	return &resolverRef{Name: ref.Name, Body: ref.Body}
}
