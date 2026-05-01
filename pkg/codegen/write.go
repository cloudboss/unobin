package codegen

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// WriteSource lays out a stack binary's source tree in dir, ready for
// `go build` to consume. It writes:
//
//	<dir>/main.go    // From Generate.
//	<dir>/go.mod     // With the right require statements.
//
// goVersion is the Go toolchain version to declare. unobinVersion is
// the version of `github.com/cloudboss/unobin` the generated binary
// depends on. importVersions maps each module's Go import path to the
// version constraint to require.
func WriteSource(dir string, in Input, goVersion, unobinVersion string, importVersions map[string]string) error {
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
	mod, err := renderGoMod(in.StackName, goVersion, unobinVersion, in.GoImports, importVersions)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "go.mod"), mod, 0o644)
}

func renderGoMod(stackName, goVersion, unobinVersion string, goImports, importVersions map[string]string) ([]byte, error) {
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

	rootOf := func(p string) string { return p }
	for _, p := range paths {
		root := rootOf(p)
		if root == "github.com/cloudboss/unobin" || hasPrefix(root, "github.com/cloudboss/unobin/") {
			continue
		}
		b = append(b, "\t"+root+" "+importVersions[p]+"\n"...)
	}
	b = append(b, ")\n"...)
	return b, nil
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
