package runner

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// supportedVersion is one entry from `factory.pin.supported-versions`.
type supportedVersion struct {
	Version         string
	ContentRevision string
}

// factoryEnvelope holds the values read from a config's `factory.pin:`
// block. The zero value (Present=false) means the config did not
// declare a `factory:` section, or no config was provided at all.
type factoryEnvelope struct {
	Present           bool
	LibraryPath       string
	SupportedVersions []supportedVersion
}

// factoryChildObject returns the object bound to the named key inside
// the config's factory: block, or nil when the config, the block, or
// the key is absent. The validator reports non-object forms first on
// the normal load path, so the errors here are a backstop for callers
// holding an unvalidated file. path is preserved only for error
// messages.
func factoryChildObject(f *lang.File, path, name string) (*lang.ObjectLit, error) {
	if f == nil || f.Body == nil {
		return nil, nil
	}
	factoryField := findField(f.Body, "factory")
	if factoryField == nil {
		return nil, nil
	}
	factoryObj, ok := factoryField.Value.(*lang.ObjectLit)
	if !ok {
		return nil, fmt.Errorf("config %s: `factory:` must be an object", path)
	}
	child := findField(factoryObj, name)
	if child == nil {
		return nil, nil
	}
	obj, ok := child.Value.(*lang.ObjectLit)
	if !ok {
		return nil, fmt.Errorf("config %s: `factory.%s:` must be an object", path, name)
	}
	return obj, nil
}

// loadFactoryEnvelope extracts the `factory.pin:` block from a
// pre-parsed config. A nil file or a config without a `factory:` block
// returns a zero envelope without error so the caller can apply the
// same pin policy to both. path is preserved only for error messages.
func loadFactoryEnvelope(f *lang.File, path string) (factoryEnvelope, error) {
	if f == nil || f.Body == nil {
		return factoryEnvelope{}, nil
	}
	if findField(f.Body, "factory") == nil {
		return factoryEnvelope{}, nil
	}
	pinObj, err := factoryChildObject(f, path, "pin")
	if err != nil {
		return factoryEnvelope{}, err
	}
	env := factoryEnvelope{Present: true}
	if pinObj == nil {
		return env, nil
	}
	val, err := runtime.Eval(pinObj, runtime.NewEvalContext(f))
	if err != nil {
		return factoryEnvelope{}, fmt.Errorf("config %s: %w", path, err)
	}
	m, ok := val.(map[string]any)
	if !ok {
		return factoryEnvelope{}, fmt.Errorf(
			"config %s: `factory.pin:` evaluated to %T, want map", path, val)
	}
	return parsePinBlock(path, env, m)
}

func parsePinBlock(path string, env factoryEnvelope, m map[string]any) (factoryEnvelope, error) {
	if v, ok := m["library-path"]; ok {
		s, ok := v.(string)
		if !ok {
			return env, fmt.Errorf(
				"config %s: `factory.pin.library-path` must be a string, got %T", path, v)
		}
		env.LibraryPath = s
	}
	raw, ok := m["supported-versions"]
	if !ok {
		return env, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return env, fmt.Errorf(
			"config %s: `factory.pin.supported-versions` must be a list, got %T", path, raw)
	}
	for i, item := range list {
		entry, ok := item.(map[string]any)
		if !ok {
			return env, fmt.Errorf(
				"config %s: `factory.pin.supported-versions[%d]`"+
					" must be an object, got %T",
				path, i, item)
		}
		version, _ := entry["version"].(string)
		revision, _ := entry["content-revision"].(string)
		if version == "" || revision == "" {
			return env, fmt.Errorf(
				"config %s: `factory.pin.supported-versions[%d]`"+
					" must have non-empty `version` and `content-revision`",
				path, i)
		}
		env.SupportedVersions = append(env.SupportedVersions, supportedVersion{
			Version:         version,
			ContentRevision: revision,
		})
	}
	return env, nil
}

// verifyFactoryEnvelope enforces version pinning. It hard-fails when the
// config names a library-path that does not match the binary. It
// soft-fails (overridable by allowVersionMismatch) when the config does
// not pin any versions, or pins versions but not this binary's.
func verifyFactoryEnvelope(
	info Info, f *lang.File, configPath string, allowVersionMismatch bool,
) error {
	env, err := loadFactoryEnvelope(f, configPath)
	if err != nil {
		return err
	}
	if env.LibraryPath != "" && env.LibraryPath != info.LibraryPath {
		return fmt.Errorf(
			"factory library-path mismatch: config declares %q"+
				" but this binary is built from %q",
			env.LibraryPath, info.LibraryPath)
	}
	if len(env.SupportedVersions) == 0 {
		if allowVersionMismatch {
			return nil
		}
		return fmt.Errorf(
			"config does not pin any factory versions in `factory.pin.supported-versions`; " +
				"add an entry or pass --allow-version-mismatch")
	}
	for _, sv := range env.SupportedVersions {
		if sv.Version == info.FactoryVersion && sv.ContentRevision == info.ContentRevision {
			return nil
		}
	}
	if allowVersionMismatch {
		return nil
	}
	return fmt.Errorf(
		"binary %s (content-revision %s) is not in `factory.pin.supported-versions`; "+
			"add an entry or pass --allow-version-mismatch",
		info.FactoryVersion, info.ContentRevision)
}
