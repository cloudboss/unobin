package compile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/toolchain"
)

func TestSelectedBuildModulesUsesOneInventory(t *testing.T) {
	for _, test := range []struct {
		name     string
		version  string
		replaced bool
		missing  bool
		failure  string
	}{
		{name: "matching", version: "v0.12.0"},
		{name: "higher core", version: "v0.13.0", failure: "upgrade unobin"},
		{name: "replacement", version: "v0.13.0", replaced: true},
		{name: "missing core", missing: true, failure: "cannot read the selected version"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			module := selectedLibraryModule{
				Path: toolchain.UnobinModulePath, Version: test.version, Dir: dir,
			}
			if test.replaced {
				module.Replace = &selectedLibraryModule{Path: dir, Dir: dir}
			}
			modules := []selectedLibraryModule{module}
			if test.missing {
				modules = []selectedLibraryModule{{Path: "example.com/factory", Dir: dir}}
			}
			goBin := selectedModulesGoStub(t, dir, modules)
			var reporter diagnostic.Collector
			err := verifySelectedBuildModules(&reporter, goBin, dir, "v0.12.0", nil, nil)
			if test.failure == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, test.failure)
			}
			invocations, err := os.ReadFile(filepath.Join(dir, "invocations"))
			require.NoError(t, err)
			require.Equal(t, "list -m -json "+toolchain.UnobinModulePath+"\n", string(invocations))
			if test.replaced {
				require.Equal(t, []diagnostic.Diagnostic{{
					Code: "unobin.compile.selected-toolchain", Severity: diagnostic.SeverityInfo,
					Message: toolchain.UnobinModulePath +
						" is replaced; the factory runs the replacement, not v0.12.0",
				}}, reporter.Diagnostics())
			}
		})
	}
}

func TestSelectedBuildModulesChecksLinkedSelection(t *testing.T) {
	dir, library := t.TempDir(), t.TempDir()
	require.NoError(t, os.CopyFS(library, os.DirFS("../deps/testdata/go/compatibility")))
	context, err := golibrary.NewCompatibilityContext(golibrary.CompatibilityOptions{})
	require.NoError(t, err)
	require.NoError(t, context.CheckPackage(golibrary.PackageSource{
		Dir: library, Linked: true, Module: golibrary.ModuleSource{
			Dir: library, Version: "v0.1.0", Replacement: library,
		},
	}))
	goBin := selectedModulesGoStub(t, dir, []selectedLibraryModule{
		{Path: toolchain.UnobinModulePath, Version: "v0.12.0"},
		{Path: "example.com/lib", Version: "v0.2.0", Dir: library},
	})
	err = verifySelectedBuildModules(nil, goBin, dir, "v0.12.0", context, context.Manifest())
	var selection *libraryModuleError
	require.ErrorAs(t, err, &selection)
	invocations, err := os.ReadFile(filepath.Join(dir, "invocations"))
	require.NoError(t, err)
	require.Equal(t, "list -m -json all\n", string(invocations))
}

func selectedModulesGoStub(
	t *testing.T,
	dir string,
	modules []selectedLibraryModule,
) string {
	t.Helper()
	var data []byte
	for _, module := range modules {
		body, err := json.Marshal(module)
		require.NoError(t, err)
		data = append(data, body...)
		data = append(data, '\n')
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "modules.json"), data, 0o644))
	path := filepath.Join(dir, "go-stub")
	require.NoError(t, os.WriteFile(path, []byte(
		"#!/bin/sh\nprintf '%s\\n' \"${*}\" >> invocations\ncat modules.json\n"), 0o755))
	return path
}

func TestSelectedBuildModulesInventoryFailures(t *testing.T) {
	for _, test := range []struct {
		name   string
		output string
		status string
		want   string
	}{
		{name: "command failure", status: "exit 1",
			want: "go list -m -json " + toolchain.UnobinModulePath + " failed"},
		{name: "invalid inventory", output: "{", want: "read selected Go modules"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			goBin := selectedModulesGoStub(t, dir, nil)
			if test.status != "" {
				require.NoError(t, os.WriteFile(goBin,
					[]byte("#!/bin/sh\n"+test.status+"\n"), 0o755))
			} else {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "modules.json"),
					[]byte(test.output), 0o644))
			}
			err := verifySelectedBuildModules(nil, goBin, dir, "v0.12.0", nil, nil)
			require.ErrorContains(t, err, test.want)
		})
	}
}
