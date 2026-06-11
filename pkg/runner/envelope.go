package runner

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// supportedVersion is one entry from `factory.supported-versions`.
type supportedVersion struct {
	Version         string
	ContentRevision string
}

// factoryEnvelope holds the values read from a config's `factory:` block.
// The zero value (Present=false) means the config did not declare a
// `factory:` section, or no config was provided at all.
type factoryEnvelope struct {
	Present           bool
	LibraryPath       string
	SupportedVersions []supportedVersion
}

// loadFactoryEnvelope extracts the `factory:` block from a pre-parsed
// config. A nil file returns a zero envelope without error so the
// caller can apply the same pin policy to "no config" and "config
// without factory block." path is preserved only for error messages.
func loadFactoryEnvelope(f *lang.File, path string) (factoryEnvelope, error) {
	if f == nil {
		return factoryEnvelope{}, nil
	}
	for _, fld := range f.Body.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.Name != "factory" {
			continue
		}
		obj, ok := fld.Value.(*lang.ObjectLit)
		if !ok {
			return factoryEnvelope{},
				fmt.Errorf("config %s: `factory:` must be an object", path)
		}
		val, err := runtime.Eval(obj, runtime.NewEvalContext(f))
		if err != nil {
			return factoryEnvelope{}, fmt.Errorf("config %s: %w", path, err)
		}
		m, ok := val.(map[string]any)
		if !ok {
			return factoryEnvelope{}, fmt.Errorf(
				"config %s: `factory:` evaluated to %T, want map", path, val)
		}
		return parseFactoryBlock(path, m)
	}
	return factoryEnvelope{}, nil
}

func parseFactoryBlock(path string, m map[string]any) (factoryEnvelope, error) {
	env := factoryEnvelope{Present: true}
	if v, ok := m["library-path"]; ok {
		s, ok := v.(string)
		if !ok {
			return env, fmt.Errorf(
				"config %s: `factory.library-path` must be a string, got %T", path, v)
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
			"config %s: `factory.supported-versions` must be a list, got %T", path, raw)
	}
	for i, item := range list {
		entry, ok := item.(map[string]any)
		if !ok {
			return env, fmt.Errorf(
				"config %s: `factory.supported-versions[%d]`"+
					" must be an object, got %T",
				path, i, item)
		}
		version, _ := entry["version"].(string)
		revision, _ := entry["content-revision"].(string)
		if version == "" || revision == "" {
			return env, fmt.Errorf(
				"config %s: `factory.supported-versions[%d]`"+
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
			"config does not pin any factory versions in `factory.supported-versions`; " +
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
		"binary %s (content-revision %s) is not in `factory.supported-versions`; "+
			"add an entry or pass --allow-version-mismatch",
		info.FactoryVersion, info.ContentRevision)
}
