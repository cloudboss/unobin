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

// parseStateConfig reads the state: block of config.ub (if any) and
// returns its parsed form. A missing config path or absent block
// returns an empty stateConfig; the caller falls back to defaults.
func parseStateConfig(configPath string) (*stateConfig, error) {
	if configPath == "" {
		return &stateConfig{}, nil
	}
	src, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	f, err := lang.ParseSource(configPath, src)
	if err != nil {
		return nil, err
	}
	f.Kind = lang.FileConfig
	if errs := lang.ValidateFile(f); errs.Len() > 0 {
		return nil, errs.Err()
	}
	block := topLevelObject(f, "state")
	if block == nil {
		return &stateConfig{}, nil
	}
	return readStateBlock(configPath, block)
}

func readStateBlock(configPath string, block *lang.ObjectLit) (*stateConfig, error) {
	sc := &stateConfig{}
	body := map[string]any{}
	var alias, name string
	var backendSet bool
	var errs []error

	for _, fld := range block.Fields {
		if fld.Key.IsMeta() {
			if fld.Key.Name == "@backend" {
				if backendSet {
					errs = append(errs, fmt.Errorf(
						"%s: duplicate @backend in state block", configPath))
					continue
				}
				a, n, perr := parseResolverRef(fld.Value)
				if perr != nil {
					errs = append(errs, fmt.Errorf("%s: @backend: %w", configPath, perr))
					continue
				}
				alias = a
				name = n
				backendSet = true
				continue
			}
			errs = append(errs, fmt.Errorf(
				"%s: unknown meta-key @%s in state block", configPath, fld.Key.Name))
			continue
		}
		if fld.Key.Kind != lang.FieldIdent {
			continue
		}
		if fld.Key.Name == "encryption" {
			obj, ok := fld.Value.(*lang.ObjectLit)
			if !ok {
				errs = append(errs, fmt.Errorf(
					"%s: encryption must be an object", configPath))
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
	if !backendSet {
		errs = append(errs, fmt.Errorf(
			"%s: state block missing @backend", configPath))
	} else {
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
	var keySourceSet bool
	var errs []error

	for _, fld := range block.Fields {
		if fld.Key.IsMeta() {
			if fld.Key.Name == "@key-source" {
				if keySourceSet {
					errs = append(errs, fmt.Errorf(
						"%s: duplicate @key-source in encryption block", configPath))
					continue
				}
				a, n, perr := parseResolverRef(fld.Value)
				if perr != nil {
					errs = append(errs, fmt.Errorf(
						"%s: @key-source: %w", configPath, perr))
					continue
				}
				alias = a
				name = n
				keySourceSet = true
				continue
			}
			errs = append(errs, fmt.Errorf(
				"%s: unknown meta-key @%s in encryption block", configPath, fld.Key.Name))
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
	if !keySourceSet {
		errs = append(errs, fmt.Errorf(
			"%s: encryption block missing @key-source", configPath))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return &resolverRef{Alias: alias, Name: name, Body: body}, nil
}

// parseResolverRef accepts either a bare identifier (`local`) or a
// two-segment dotted path (`aws.s3`) and returns the alias and name.
// A bare identifier yields an empty alias.
func parseResolverRef(expr lang.Expr) (alias, name string, err error) {
	switch v := expr.(type) {
	case *lang.Ident:
		return "", v.Name, nil
	case *lang.DotPath:
		if v.Root == nil || len(v.Segments) != 1 || v.Segments[0].Name == "" {
			return "", "", fmt.Errorf("expected `name` or `alias.name`")
		}
		return v.Root.Name, v.Segments[0].Name, nil
	default:
		return "", "", fmt.Errorf("expected `name` or `alias.name`, got %T", expr)
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
