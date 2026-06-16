package runner

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/runtime"
)

// supportedVersion is one entry from `factory.pin.supported-versions`.
type supportedVersion struct {
	Version         string
	ContentRevision string
}

// factoryEnvelope holds the values read from a stack file's `factory.pin:`
// block. The zero value (Present=false) means the stack file did not
// declare a `factory:` section, or no stack file was provided at all.
type factoryEnvelope struct {
	Present           bool
	LibraryPath       string
	SupportedVersions []supportedVersion
}

// loadFactoryEnvelope extracts the `factory.pin:` block from a
// pre-parsed stack file. A nil stack file or one without a `factory:` block
// returns a zero envelope without error so the caller can apply the same
// pin policy to both. path is preserved only for error messages.
func loadFactoryEnvelope(config *parsedConfig, path string) (factoryEnvelope, error) {
	stack := configStack(config)
	if stack == nil || stack.Factory == nil {
		return factoryEnvelope{}, nil
	}
	env := factoryEnvelope{Present: true}
	pinObj := stack.Factory.Pin
	if pinObj == nil {
		return env, nil
	}
	val, err := runtime.Eval(pinObj, configEvalContext(config))
	if err != nil {
		return factoryEnvelope{}, fmt.Errorf("stack file %s: %w", path, err)
	}
	m, ok := val.(map[string]any)
	if !ok {
		return factoryEnvelope{}, fmt.Errorf(
			"stack file %s: `factory.pin:` evaluated to %T, want map", path, val)
	}
	return parsePinBlock(path, env, m)
}

func parsePinBlock(path string, env factoryEnvelope, m map[string]any) (factoryEnvelope, error) {
	if v, ok := m["library-path"]; ok {
		s, ok := v.(string)
		if !ok {
			return env, fmt.Errorf(
				"stack file %s: `factory.pin.library-path` must be a string, got %T", path, v)
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
			"stack file %s: `factory.pin.supported-versions` must be a list, got %T", path, raw)
	}
	for i, item := range list {
		entry, ok := item.(map[string]any)
		if !ok {
			return env, fmt.Errorf(
				"stack file %s: `factory.pin.supported-versions[%d]`"+
					" must be an object, got %T",
				path, i, item)
		}
		version, _ := entry["version"].(string)
		revision, _ := entry["content-revision"].(string)
		if version == "" || revision == "" {
			return env, fmt.Errorf(
				"stack file %s: `factory.pin.supported-versions[%d]`"+
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
// stack file names a library-path that does not match the binary. It
// soft-fails (overridable by allowVersionMismatch) when the stack file does
// not pin any versions, or pins versions but not this binary's.
func verifyFactoryEnvelope(
	info Info, config *parsedConfig, configPath string, allowVersionMismatch bool,
) error {
	env, err := loadFactoryEnvelope(config, configPath)
	if err != nil {
		return err
	}
	if env.LibraryPath != "" && env.LibraryPath != info.LibraryPath {
		return fmt.Errorf(
			"factory library-path mismatch: stack file declares %q"+
				" but this binary is built from %q",
			env.LibraryPath, info.LibraryPath)
	}
	if len(env.SupportedVersions) == 0 {
		if allowVersionMismatch {
			return nil
		}
		return fmt.Errorf(
			"stack file does not pin any factory versions in `factory.pin.supported-versions`; " +
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
