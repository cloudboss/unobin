package runner

import (
	"errors"
	"fmt"
	"os"

	"github.com/cloudboss/unobin/pkg/envencrypt"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
)

// resolverRef names one entry in a module's StateBackends or
// Encrypters map. Alias is empty for core's built-in bare names
// (`local`, `env-key`); otherwise it's the import alias from
// `imports:`.
type resolverRef struct {
	Alias string
	Name  string
	Body  map[string]any
}

// stateConfig captures the parsed state: block from config.ub. A nil
// field means the operator omitted that section; the resolver fills
// in defaults.
type stateConfig struct {
	Backend   *resolverRef
	Encrypter *resolverRef
}

// parseStateConfig extracts the `state:` block from a pre-parsed
// config. A nil file or an absent block returns an empty stateConfig
// and the caller falls back to defaults. The block is expected to be
// structurally validated by lang.ValidateStateConfig before this runs;
// this function evaluates body expressions and packages them for the
// resolver. path is preserved only for error messages from Eval.
func parseStateConfig(f *lang.File, path string) (*stateConfig, error) {
	if f == nil {
		return &stateConfig{}, nil
	}
	block := topLevelObject(f, "state")
	if block == nil {
		return &stateConfig{}, nil
	}
	return readStateBlock(path, block)
}

func readStateBlock(configPath string, block *lang.ObjectLit) (*stateConfig, error) {
	sc := &stateConfig{}
	body := map[string]any{}
	var alias, name string
	var errs []error

	for _, fld := range block.Fields {
		if fld.Key.IsMeta() {
			if fld.Key.Name == "@backend" {
				alias, name = resolverRefValue(fld.Value)
			}
			continue
		}
		if fld.Key.Kind != lang.FieldIdent {
			continue
		}
		if fld.Key.Name == "encryption" {
			obj, ok := fld.Value.(*lang.ObjectLit)
			if !ok {
				continue
			}
			ref, perr := readEncryptionBlock(configPath, obj)
			if perr != nil {
				errs = append(errs, perr)
				continue
			}
			sc.Encrypter = ref
			continue
		}
		val, err := runtime.Eval(fld.Value, &runtime.EvalContext{})
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"%s: state.%s: %w", configPath, fld.Key.Name, err))
			continue
		}
		body[fld.Key.Name] = val
	}
	if name != "" {
		sc.Backend = &resolverRef{Alias: alias, Name: name, Body: body}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return sc, nil
}

func readEncryptionBlock(configPath string, block *lang.ObjectLit) (*resolverRef, error) {
	body := map[string]any{}
	var alias, name string
	var errs []error

	for _, fld := range block.Fields {
		if fld.Key.IsMeta() {
			if fld.Key.Name == "@key-source" {
				alias, name = resolverRefValue(fld.Value)
			}
			continue
		}
		if fld.Key.Kind != lang.FieldIdent {
			continue
		}
		val, err := runtime.Eval(fld.Value, &runtime.EvalContext{})
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
	return &resolverRef{Alias: alias, Name: name, Body: body}, nil
}

// resolverRefValue extracts the (alias, name) pair from a `@backend:`
// or `@key-source:` value. The caller relies on lang.ValidateStateConfig
// to have rejected anything other than an Ident or a two-segment DotPath
// upstream; this helper returns ("", "") for any other value so callers
// fall back to defaults.
func resolverRefValue(expr lang.Expr) (alias, name string) {
	switch v := expr.(type) {
	case *lang.Ident:
		return "", v.Name
	case *lang.DotPath:
		if v.Root == nil || len(v.Segments) != 1 {
			return "", ""
		}
		return v.Root.Name, v.Segments[0].Name
	default:
		return "", ""
	}
}

// resolveEncrypter constructs the encrypter named by the parsed state
// config. A nil ref means the operator omitted the encryption block;
// the resolver falls back to env-key against UB_STATE_KEY, or the
// no-op if that env var is unset.
func resolveEncrypter(info Info, ref *resolverRef) (sdkencrypt.Encrypter, error) {
	if ref == nil {
		if os.Getenv("UB_STATE_KEY") == "" {
			return envencrypt.Noop{}, nil
		}
		return envencrypt.NewEnvKey("UB_STATE_KEY")
	}
	rt, err := lookupEncrypterType(info.Modules, ref)
	if err != nil {
		return nil, err
	}
	decoded, err := decodeRefConfig(rt.Configuration, ref)
	if err != nil {
		return nil, fmt.Errorf("encryption: %w", err)
	}
	return rt.New(decoded)
}

// resolveBackend constructs the backend named by the parsed state
// config. A nil ref falls back to the local backend at
// `.unobin/state`.
func resolveBackend(
	info Info,
	ref *resolverRef,
	stack, deploymentID string,
	enc sdkencrypt.Encrypter,
) (sdkstate.Backend, error) {
	if ref == nil {
		ref = &resolverRef{
			Alias: "",
			Name:  "local",
			Body:  map[string]any{"path": ".unobin/state"},
		}
	}
	bt, err := lookupBackendType(info.Modules, ref)
	if err != nil {
		return nil, err
	}
	decoded, err := decodeRefConfig(bt.Configuration, ref)
	if err != nil {
		return nil, fmt.Errorf("state: %w", err)
	}
	return bt.New(decoded, stack, deploymentID, enc)
}

func lookupBackendType(
	modules map[string]*runtime.Module,
	ref *resolverRef,
) (sdkstate.BackendType, error) {
	moduleAlias := ref.Alias
	if moduleAlias == "" {
		moduleAlias = "core"
	}
	mod, ok := modules[moduleAlias]
	if !ok {
		return sdkstate.BackendType{}, fmt.Errorf(
			"state: backend %s: import %q not found", refLabel(ref), moduleAlias)
	}
	bt, ok := mod.StateBackends[ref.Name]
	if !ok {
		return sdkstate.BackendType{}, fmt.Errorf(
			"state: backend %s: module %q registers no backend named %q",
			refLabel(ref), moduleAlias, ref.Name)
	}
	return bt, nil
}

func lookupEncrypterType(
	modules map[string]*runtime.Module,
	ref *resolverRef,
) (sdkencrypt.EncrypterType, error) {
	moduleAlias := ref.Alias
	if moduleAlias == "" {
		moduleAlias = "core"
	}
	mod, ok := modules[moduleAlias]
	if !ok {
		return sdkencrypt.EncrypterType{}, fmt.Errorf(
			"encryption: key-source %s: import %q not found", refLabel(ref), moduleAlias)
	}
	et, ok := mod.Encrypters[ref.Name]
	if !ok {
		return sdkencrypt.EncrypterType{}, fmt.Errorf(
			"encryption: key-source %s: module %q registers no encrypter named %q",
			refLabel(ref), moduleAlias, ref.Name)
	}
	return et, nil
}

func decodeRefConfig(ct *cfg.ConfigurationType, ref *resolverRef) (any, error) {
	if ct == nil {
		if len(ref.Body) > 0 {
			return nil, fmt.Errorf("%s accepts no configuration fields", refLabel(ref))
		}
		return nil, nil
	}
	return cfg.Decode(ct, ref.Body)
}

func refLabel(ref *resolverRef) string {
	if ref.Alias == "" {
		return ref.Name
	}
	return ref.Alias + "." + ref.Name
}

// toRuntimeStateRef copies a resolverRef into the public runtime
// type used inside the plan file. Returns nil when ref is nil so
// the plan field stays omit-empty.
func toRuntimeStateRef(ref *resolverRef) *runtime.StateRef {
	if ref == nil {
		return nil
	}
	return &runtime.StateRef{Alias: ref.Alias, Name: ref.Name, Body: ref.Body}
}

// fromRuntimeStateRef is the inverse of toRuntimeStateRef. Apply uses
// it to feed pf.Backend back through the resolver.
func fromRuntimeStateRef(ref *runtime.StateRef) *resolverRef {
	if ref == nil {
		return nil
	}
	return &resolverRef{Alias: ref.Alias, Name: ref.Name, Body: ref.Body}
}
