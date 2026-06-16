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

// parseStateConfig extracts the `state:` and `encryption:` blocks from
// a pre-parsed config. A nil file or an absent block leaves the matching
// field nil and the caller falls back to defaults. The blocks are
// expected to be structurally validated by lang.ValidateStateConfig and
// lang.ValidateEncryptionConfig before this runs; this function
// evaluates body expressions, with the file's locals in scope, and
// packages the values for the resolver. path is preserved only for
// error messages from Eval.
func parseStateConfig(f *lang.File, path string) (*stateConfig, error) {
	if f == nil {
		return &stateConfig{}, nil
	}
	sc := &stateConfig{}
	ctx := runtime.NewEvalContext(f)
	var stateErr, encErr error
	if block := lang.TopLevelBlock(f, "state"); block != nil {
		sc.Backend, stateErr = readStateBlock(path, block, ctx)
	}
	if block := lang.TopLevelBlock(f, "encryption"); block != nil {
		sc.Encrypter, encErr = readEncryptionBlock(path, block, ctx)
	}
	if err := errors.Join(stateErr, encErr); err != nil {
		return nil, err
	}
	return sc, nil
}

func readStateBlock(
	configPath string, block *lang.ObjectLit, ctx *runtime.EvalContext,
) (*resolverRef, error) {
	body := map[string]any{}
	var name string
	var errs []error

	for _, fld := range block.Fields {
		if fld.Key.IsMeta() {
			if fld.Key.Name == "@backend" {
				name = resolverRefValue(fld.Value)
			}
			continue
		}
		if fld.Key.Kind != lang.FieldIdent {
			continue
		}
		val, err := runtime.Eval(fld.Value, ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"%s: state.%s: %w", configPath, fld.Key.Name, err))
			continue
		}
		body[fld.Key.Name] = val
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	if name == "" {
		return nil, nil
	}
	return &resolverRef{Name: name, Body: body}, nil
}

func readEncryptionBlock(
	configPath string, block *lang.ObjectLit, ctx *runtime.EvalContext,
) (*resolverRef, error) {
	body := map[string]any{}
	var name string
	var errs []error

	for _, fld := range block.Fields {
		if fld.Key.IsMeta() {
			if fld.Key.Name == "@key-source" {
				name = resolverRefValue(fld.Value)
			}
			continue
		}
		if fld.Key.Kind != lang.FieldIdent {
			continue
		}
		val, err := runtime.Eval(fld.Value, ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"%s: encryption.%s: %w", configPath, fld.Key.Name, err))
			continue
		}
		body[fld.Key.Name] = val
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	if name == "" {
		return nil, nil
	}
	return &resolverRef{Name: name, Body: body}, nil
}

// resolverRefValue extracts the bare name from a lowered selector marker.
// lang.ValidateStateConfig has already rejected anything but a bare
// identifier upstream; this returns "" for any other value so callers
// fall back to defaults.
func resolverRefValue(expr lang.Expr) string {
	if id, ok := expr.(*lang.Ident); ok {
		return id.Name
	}
	return ""
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
