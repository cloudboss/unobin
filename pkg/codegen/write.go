package codegen

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// Replaces maps a library path to a local filesystem path to substitute
// at build time, used in the generated `go.mod`'s replace directives.
// The value is typically the absolute path to a local checkout. An
// empty map means no replace directives.
type Replaces map[string]string

// WriteSource lays out a generated binary's source tree in dir, ready for
// `go build` to consume. It writes:
//
//	<dir>/main.go    // From Generate.
//	<dir>/go.mod     // With the right require statements.
//
// goVersion is the Go toolchain version to declare. unobinVersion is
// the version of `github.com/cloudboss/unobin` the generated binary
// depends on. importVersions maps each library's Go import path to the
// version constraint to require for callers that have not set Input.GoModules.
// replaces maps a module path to a local path to substitute via `replace`.
func WriteSource(
	dir string,
	in Input,
	goVersion, unobinVersion string,
	importVersions map[string]string,
	replaces Replaces,
) error {
	source, err := Generate(in)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), source, 0o644); err != nil {
		return err
	}
	goModules, err := modulesForGoMod(in.GoImports, in.GoModules, importVersions)
	if err != nil {
		return err
	}
	lib, err := renderGoMod(in.FactoryName, goVersion, unobinVersion, goModules, replaces)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "go.mod"), lib, 0o644)
}

func modulesForGoMod(
	goImports, goModules, importVersions map[string]string,
) (map[string]string, error) {
	if len(goModules) > 0 {
		out := make(map[string]string, len(goModules))
		for modulePath, version := range goModules {
			if version == "" {
				return nil, fmt.Errorf("codegen: no version for module %q", modulePath)
			}
			out[modulePath] = version
		}
		return out, nil
	}
	out := make(map[string]string, len(goImports))
	for alias, path := range goImports {
		version, ok := importVersions[path]
		if !ok {
			return nil, fmt.Errorf("codegen: no version for import %q (%s)", alias, path)
		}
		out[path] = version
	}
	return out, nil
}

func renderGoMod(
	factoryName, goVersion, unobinVersion string,
	goModules map[string]string,
	replaces Replaces,
) ([]byte, error) {
	if goVersion == "" {
		return nil, fmt.Errorf("codegen: goVersion is required")
	}
	if unobinVersion == "" {
		return nil, fmt.Errorf("codegen: unobinVersion is required")
	}
	var b []byte
	b = append(b, "module "+factoryName+"\n\n"...)
	b = append(b, "go "+goVersion+"\n\n"...)
	b = append(b, "require (\n"...)
	b = append(b, "\tgithub.com/cloudboss/unobin "+unobinVersion+"\n"...)

	paths := make([]string, 0, len(goModules))
	for p := range goModules {
		paths = append(paths, p)
	}
	slices.Sort(paths)

	for _, p := range paths {
		if p == "github.com/cloudboss/unobin" || hasPrefix(p, "github.com/cloudboss/unobin/") {
			continue
		}
		b = append(b, "\t"+p+" "+goModules[p]+"\n"...)
	}
	b = append(b, ")\n"...)

	if len(replaces) > 0 {
		replaceKeys := make([]string, 0, len(replaces))
		for k := range replaces {
			replaceKeys = append(replaceKeys, k)
		}
		slices.Sort(replaceKeys)
		b = append(b, "\nreplace (\n"...)
		for _, k := range replaceKeys {
			b = append(b, "\t"+k+" => "+replaces[k]+"\n"...)
		}
		b = append(b, ")\n"...)
	}
	return b, nil
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
