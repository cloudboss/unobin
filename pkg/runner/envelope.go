package runner

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// supportedVersion is one entry from `stack.supported-versions`.
type supportedVersion struct {
	Version         string
	ContentRevision string
}

// stackEnvelope holds the values read from a config's `stack:` block.
// The zero value (Present=false) means the config did not declare a
// `stack:` section, or no config was provided at all.
type stackEnvelope struct {
	Present           bool
	LibraryPath       string
	SupportedVersions []supportedVersion
}

// loadStackEnvelope extracts the `stack:` block from a pre-parsed
// config. A nil file returns a zero envelope without error so the
// caller can apply the same pin policy to "no config" and "config
// without stack block." path is preserved only for error messages.
func loadStackEnvelope(f *lang.File, path string) (stackEnvelope, error) {
	if f == nil {
		return stackEnvelope{}, nil
	}
	for _, fld := range f.Body.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.Name != "stack" {
			continue
		}
		obj, ok := fld.Value.(*lang.ObjectLit)
		if !ok {
			return stackEnvelope{},
				fmt.Errorf("config %s: `stack:` must be an object", path)
		}
		val, err := runtime.Eval(obj, &runtime.EvalContext{})
		if err != nil {
			return stackEnvelope{}, fmt.Errorf("config %s: %w", path, err)
		}
		m, ok := val.(map[string]any)
		if !ok {
			return stackEnvelope{}, fmt.Errorf(
				"config %s: `stack:` evaluated to %T, want map", path, val)
		}
		return parseStackBlock(path, m)
	}
	return stackEnvelope{}, nil
}

func parseStackBlock(path string, m map[string]any) (stackEnvelope, error) {
	env := stackEnvelope{Present: true}
	if v, ok := m["library-path"]; ok {
		s, ok := v.(string)
		if !ok {
			return env, fmt.Errorf(
				"config %s: `stack.library-path` must be a string, got %T", path, v)
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
			"config %s: `stack.supported-versions` must be a list, got %T", path, raw)
	}
	for i, item := range list {
		entry, ok := item.(map[string]any)
		if !ok {
			return env, fmt.Errorf(
				"config %s: `stack.supported-versions[%d]`"+
					" must be an object, got %T",
				path, i, item)
		}
		version, _ := entry["version"].(string)
		revision, _ := entry["content-revision"].(string)
		if version == "" || revision == "" {
			return env, fmt.Errorf(
				"config %s: `stack.supported-versions[%d]`"+
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

// verifyStackEnvelope enforces version pinning. It hard-fails when the
// config names a library-path that does not match the binary. It
// soft-fails (overridable by allowVersionMismatch) when the config does
// not pin any versions, or pins versions but not this binary's.
func verifyStackEnvelope(
	info Info, f *lang.File, configPath string, allowVersionMismatch bool,
) error {
	env, err := loadStackEnvelope(f, configPath)
	if err != nil {
		return err
	}
	if env.LibraryPath != "" && env.LibraryPath != info.LibraryPath {
		return fmt.Errorf(
			"stack library-path mismatch: config declares %q"+
				" but this binary is built from %q",
			env.LibraryPath, info.LibraryPath)
	}
	if len(env.SupportedVersions) == 0 {
		if allowVersionMismatch {
			return nil
		}
		return fmt.Errorf(
			"config does not pin any stack versions in `stack.supported-versions`; " +
				"add an entry or pass --allow-version-mismatch")
	}
	for _, sv := range env.SupportedVersions {
		if sv.Version == info.StackVersion && sv.ContentRevision == info.ContentRevision {
			return nil
		}
	}
	if allowVersionMismatch {
		return nil
	}
	return fmt.Errorf(
		"binary %s (content-revision %s) is not in `stack.supported-versions`; "+
			"add an entry or pass --allow-version-mismatch",
		info.StackVersion, info.ContentRevision)
}
