package codegen

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Replaces maps a module path to a local filesystem path to substitute
// at build time, used in the generated `go.mod`'s replace directives.
// The value is typically the absolute path to a local checkout. An
// empty map means no replace directives.
type Replaces map[string]string

// WriteSource lays out a stack binary's source tree in dir, ready for
// `go build` to consume. It writes:
//
//	<dir>/main.go    // From Generate.
//	<dir>/go.mod     // With the right require statements.
//
// goVersion is the Go toolchain version to declare. unobinVersion is
// the version of `github.com/cloudboss/unobin` the generated binary
// depends on. importVersions maps each module's Go import path to the
// version constraint to require. replaces maps a module path to a
// local path to substitute via `replace`.
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
	mod, err := renderGoMod(in.StackName, goVersion, unobinVersion,
		in.GoImports, importVersions, replaces)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "go.mod"), mod, 0o644)
}

func renderGoMod(
	stackName, goVersion, unobinVersion string,
	goImports, importVersions map[string]string,
	replaces Replaces,
) ([]byte, error) {
	if goVersion == "" {
		return nil, fmt.Errorf("codegen: goVersion is required")
	}
	if unobinVersion == "" {
		return nil, fmt.Errorf("codegen: unobinVersion is required")
	}
	for alias, path := range goImports {
		if _, ok := importVersions[path]; !ok {
			return nil, fmt.Errorf("codegen: no version for import %q (%s)", alias, path)
		}
	}

	var b []byte
	b = append(b, "module "+stackName+"\n\n"...)
	b = append(b, "go "+goVersion+"\n\n"...)
	b = append(b, "require (\n"...)
	b = append(b, "\tgithub.com/cloudboss/unobin "+unobinVersion+"\n"...)

	paths := make([]string, 0, len(goImports))
	seen := make(map[string]bool, len(goImports))
	for _, p := range goImports {
		if !seen[p] {
			paths = append(paths, p)
			seen[p] = true
		}
	}
	sort.Strings(paths)

	for _, p := range paths {
		if p == "github.com/cloudboss/unobin" || hasPrefix(p, "github.com/cloudboss/unobin/") {
			continue
		}
		b = append(b, "\t"+p+" "+importVersions[p]+"\n"...)
	}
	b = append(b, ")\n"...)

	if len(replaces) > 0 {
		replaceKeys := make([]string, 0, len(replaces))
		for k := range replaces {
			replaceKeys = append(replaceKeys, k)
		}
		sort.Strings(replaceKeys)
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
