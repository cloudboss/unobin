package runner

import (
	"fmt"
	"os"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// supportedVersion is one entry from `stack.supported-versions`.
type supportedVersion struct {
	Version string
	Commit  string
}

// stackEnvelope holds the values read from a config's `stack:` block.
// The zero value (Present=false) means the config did not declare a
// `stack:` section, or no config was provided at all.
type stackEnvelope struct {
	Present           bool
	ModulePath        string
	SupportedVersions []supportedVersion
}

// loadStackEnvelope parses the config at path and returns the `stack:`
// block. An empty path returns a zero envelope without error so the
// caller can apply the same pin policy to "no config" and "config
// without stack block."
func loadStackEnvelope(path string) (stackEnvelope, error) {
	if path == "" {
		return stackEnvelope{}, nil
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return stackEnvelope{}, err
	}
	f, err := lang.ParseSource(path, src)
	if err != nil {
		return stackEnvelope{}, err
	}
	f.Kind = lang.FileConfig
	if errs := lang.ValidateFile(f); errs.Len() > 0 {
		return stackEnvelope{}, errs.Err()
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
	if v, ok := m["module-path"]; ok {
		s, ok := v.(string)
		if !ok {
			return env, fmt.Errorf(
				"config %s: `stack.module-path` must be a string, got %T", path, v)
		}
		env.ModulePath = s
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
		commit, _ := entry["commit"].(string)
		if version == "" || commit == "" {
			return env, fmt.Errorf(
				"config %s: `stack.supported-versions[%d]`"+
					" must have non-empty `version` and `commit`",
				path, i)
		}
		env.SupportedVersions = append(env.SupportedVersions, supportedVersion{
			Version: version,
			Commit:  commit,
		})
	}
	return env, nil
}

// verifyStackEnvelope enforces version pinning. It hard-fails when the
// config names a module-path that does not match the binary. It
// soft-fails (overridable by allowVersionMismatch) when the config does
// not pin any versions, or pins versions but not this binary's.
func verifyStackEnvelope(
	info Info, configPath string, allowVersionMismatch bool,
) error {
	env, err := loadStackEnvelope(configPath)
	if err != nil {
		return err
	}
	if env.ModulePath != "" && env.ModulePath != info.ModulePath {
		return fmt.Errorf(
			"stack module-path mismatch: config declares %q"+
				" but this binary is built from %q",
			env.ModulePath, info.ModulePath)
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
		if sv.Version == info.StackVersion && sv.Commit == info.StackCommit {
			return nil
		}
	}
	if allowVersionMismatch {
		return nil
	}
	return fmt.Errorf(
		"binary %s (commit %s) is not in `stack.supported-versions`; "+
			"add an entry or pass --allow-version-mismatch",
		info.StackVersion, info.StackCommit)
}
