package compile

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/toolchain"
)

func TestCompileRejectsCoreDescriptorBeforeResolver(t *testing.T) {
	root := compileResultRoot(t)
	opts := compileResultOptions(root)
	opts.ReplaceUnobin = t.TempDir()
	opts.NewResolver = func(string) (resolve.Resolver, error) {
		t.Fatal("resolver created before checking the core descriptor")
		return nil, nil
	}
	result, err := RunResult(opts)
	var descriptor *golibrary.CoreDescriptorError
	require.ErrorAs(t, err, &descriptor)
	require.Nil(t, result)
	require.NoDirExists(t, opts.OutDir)
}

func TestCompileChecksTypedToolchainPin(t *testing.T) {
	root := compileResultRoot(t)
	project := ubtest.ReadValidFixture(t, "testdata/ub/library-compatibility", "project")
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ProjectFileName), []byte(project), 0o644))
	opts := compileResultOptions(root)
	result, err := RunResult(opts)
	var pin *golibrary.ToolchainPinError
	require.ErrorAs(t, err, &pin)
	require.Nil(t, result)
	require.NoDirExists(t, opts.OutDir)
	ds := diagnostic.FromError(err, diagnostic.ConvertOptions{})
	require.Equal(t, "unobin.library-api.toolchain-pin", ds[0].Code)

	core, err := filepath.Abs("../..")
	require.NoError(t, err)
	opts.CLIVersion = "dev"
	opts.ReplaceUnobin = core
	opts.Stderr = io.Discard
	collector := &diagnostic.Collector{}
	opts.Reporter = collector
	result, err = RunResult(opts)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, collector.Diagnostics()[0].Message, "pins unobin v0.13.0")
}

func TestExplicitCoreReplacementOverridesProject(t *testing.T) {
	root := t.TempDir()
	projectCore := t.TempDir()
	core, err := filepath.Abs("../..")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(projectCore, "go.mod"),
		[]byte("module github.com/cloudboss/unobin\n"), 0o644))
	resolver, err := WrapReplaces(failingResolver{}, root, core, map[deps.Dependency]string{
		{URL: toolchain.UnobinModulePath}: projectCore,
	})
	require.NoError(t, err)
	source, err := resolver.Resolve(&resolve.RemoteImport{
		URL: toolchain.UnobinModulePath, Subdir: "pkg/awscfg",
	})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(core, "pkg", "awscfg"), source.Path)
}

func TestReplacementUsesNestedModuleRoot(t *testing.T) {
	core := t.TempDir()
	nested := filepath.Join(core, "lib")
	pkg := filepath.Join(nested, "config")
	require.NoError(t, os.MkdirAll(pkg, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(core, "go.mod"),
		[]byte("module github.com/cloudboss/unobin\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(nested, "go.mod"),
		[]byte("module github.com/cloudboss/unobin/lib\n"), 0o644))
	resolver, err := WrapReplaces(failingResolver{}, t.TempDir(), core, nil)
	require.NoError(t, err)
	source, err := resolver.Resolve(&resolve.RemoteImport{
		URL: toolchain.UnobinModulePath, Subdir: "lib/config",
	})
	require.NoError(t, err)
	require.Equal(t, nested, source.ModuleRootPath)
	require.Equal(t, "github.com/cloudboss/unobin/lib", source.ModulePath)
	require.Equal(t, "github.com/cloudboss/unobin/lib/config", source.GoImportPath)
}

func TestCompileUsesEffectiveCoreReplacementInGeneratedModule(t *testing.T) {
	root := compileResultRoot(t)
	factory := ubtest.ReadValidFixture(t, "testdata/ub/library-compatibility", "core-factory")
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), []byte(factory), 0o644))
	project := &deps.Project{Replace: map[deps.Dependency]string{
		{URL: toolchain.UnobinModulePath}: "./unselected-core",
	}}
	_, err := deps.WriteProjectChange(filepath.Join(root, deps.ProjectFileName), project)
	require.NoError(t, err)
	core := t.TempDir()
	require.NoError(t, os.CopyFS(filepath.Join(core, "pkg", "lib"),
		os.DirFS("../deps/testdata/go/compatibility")))
	require.NoError(t, os.Remove(filepath.Join(core, "pkg", "lib", "go.mod")))
	require.NoError(t, os.WriteFile(filepath.Join(core, "go.mod"),
		[]byte("module github.com/cloudboss/unobin\n\ngo 1.26.2\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(core, "pkg", "libraryapi"), 0o755))
	descriptor, err := os.ReadFile("../libraryapi/descriptor.json")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(core, "pkg", "libraryapi", "descriptor.json"),
		descriptor, 0o644))
	for _, generalOption := range []bool{false, true} {
		t.Run(fmt.Sprint(generalOption), func(t *testing.T) {
			opts := compileResultOptions(root)
			opts.CLIVersion = "dev"
			opts.Stderr = io.Discard
			if generalOption {
				opts.ReplaceGoModules = map[string]string{toolchain.UnobinModulePath: core}
			} else {
				opts.ReplaceUnobin = core
			}
			result, err := RunResult(opts)
			require.NoError(t, err)
			body, err := os.ReadFile(result.GoModPath)
			require.NoError(t, err)
			module, err := modfile.Parse(result.GoModPath, body, nil)
			require.NoError(t, err)
			require.Len(t, module.Replace, 1)
			require.Equal(t, toolchain.UnobinModulePath, module.Replace[0].Old.Path)
			require.Equal(t, core, module.Replace[0].New.Path)
			require.NotContains(t, string(body), "unselected-core")
		})
	}
}
