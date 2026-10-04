package compile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/golibrary"
)

type selectedLibraryModule struct {
	Path    string
	Version string
	Dir     string
	Replace *selectedLibraryModule
}

type libraryModuleError struct {
	message string
}

func (e *libraryModuleError) Error() string {
	return e.message
}

func readSelectedLibraryModules(goBin, dir string) ([]selectedLibraryModule, error) {
	command := exec.Command(goBin, "list", "-m", "-json", "all")
	command.Dir = dir
	output, err := command.Output()
	if err != nil {
		return nil, diagnostic.Context("go list -m -json all failed", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	var modules []selectedLibraryModule
	for {
		var module selectedLibraryModule
		if err := decoder.Decode(&module); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("read selected Go modules: %w", err)
		}
		modules = append(modules, module)
	}
	return modules, nil
}

func verifySelectedLibraries(
	goBin, dir string,
	compatibility *golibrary.CompatibilityContext,
	manifest []golibrary.PackageMetadata,
) error {
	var linked bool
	for _, metadata := range manifest {
		linked = linked || metadata.Source.Linked
	}
	if !linked {
		return nil
	}
	modules, err := readSelectedLibraryModules(goBin, dir)
	if err != nil {
		return err
	}
	return checkSelectedLibraryModules(compatibility, manifest, modules)
}

func checkSelectedLibraryModules(
	compatibility *golibrary.CompatibilityContext,
	manifest []golibrary.PackageMetadata,
	modules []selectedLibraryModule,
) error {
	context, err := compatibility.WithModuleResolver(nil)
	if err != nil {
		return err
	}
	compatibility = context
	actual := make(map[string]selectedLibraryModule, len(modules))
	inspected := make(map[string]golibrary.ModuleSource, len(manifest))
	for _, metadata := range manifest {
		inspected[metadata.Source.Module.Path] = metadata.Source.Module
	}
	roots := make([]golibrary.ModuleSource, 0, len(modules))
	for _, module := range modules {
		actual[module.Path] = module
		root := inspected[module.Path]
		root.Path, root.Dir, root.Version = module.Path, module.Dir, module.Version
		root.Replacement = ""
		if module.Replace != nil && module.Replace.Version == "" {
			root.Replacement = module.Replace.Dir
		}
		roots = append(roots, root)
	}
	var failures []error
	for _, metadata := range manifest {
		if !metadata.Source.Linked {
			continue
		}
		expected := metadata.Source.Module
		module, found := actual[expected.Path]
		details := compatibility.DiagnosticDetails(metadata)
		details.ActualVersion = module.Version
		if module.Replace != nil {
			details.ActualReplacement = module.Replace.Dir
		}
		failure := func(code, message, hint, path string, span *diagnostic.Span, cause error) error {
			var err error = &libraryModuleError{message: message}
			if cause != nil {
				err = fmt.Errorf("%w: %w", err, cause)
			}
			return diagnostic.WithDiagnostics(err, diagnostic.Diagnostic{
				Code: "unobin.library-api." + code, Severity: diagnostic.SeverityError,
				Message: message, Hint: hint, Path: path, Span: span, LibraryCompatibility: details,
			})
		}
		selectionHint := "Update the project's dependency floor and run `unobin deps sync` " +
			"before compiling against the selected module."
		var conflict string
		switch {
		case !found:
			conflict = fmt.Sprintf("the build does not include inspected module %s", expected.Path)
		case expected.Replacement != "":
			if module.Replace == nil || module.Replace.Version != "" ||
				filepath.Clean(module.Replace.Dir) != filepath.Clean(expected.Replacement) {
				conflict = fmt.Sprintf("the build uses a different replacement for %s", expected.Path)
			}
		case module.Replace != nil:
			conflict = fmt.Sprintf("the build replaces inspected module %s", expected.Path)
		case expected.Version == "" || module.Version != expected.Version:
			conflict = fmt.Sprintf("the build selected %s %s; preflight inspected %s",
				expected.Path, module.Version, expected.Version)
		}
		if conflict == "" && module.Dir == "" {
			conflict = fmt.Sprintf("the build has no source directory for %s", expected.Path)
		}
		if conflict != "" {
			failures = append(failures, failure("module-selection", conflict, selectionHint,
				metadata.Declaration.Path, metadata.Declaration.RequiredAPISpan, nil))
			continue
		}
		rel, err := filepath.Rel(expected.Dir, metadata.Source.Dir)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		packageDir := filepath.Join(module.Dir, rel)
		declaration, err := golibrary.ReadCompatibility(module.Dir, packageDir)
		sourceHint := "Publish a new immutable library tag, update the project's floor, " +
			"and run `unobin deps sync` before rebuilding."
		if err != nil {
			var invalid *golibrary.CompatibilityError
			if errors.As(err, &invalid) {
				failures = append(failures, failure("module-source",
					"the actual package has different compatibility metadata: "+metadata.Package,
					sourceHint, packageDir, nil, err))
			} else {
				failures = append(failures, diagnostic.Context("read actual library metadata", err))
			}
			continue
		}
		details.ActualRequiredAPI = declaration.RequiredAPI
		if declaration.RequiredAPI != metadata.Declaration.RequiredAPI ||
			declaration.SuggestedUnobinVersion != metadata.Declaration.SuggestedUnobinVersion {
			failures = append(failures, failure("module-source",
				"the actual package has different compatibility metadata: "+metadata.Package,
				sourceHint, declaration.Path, declaration.RequiredAPISpan, nil))
			continue
		}
		context, err := compatibility.WithModules(roots)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		source := metadata.Source
		source.Module.Dir = module.Dir
		source.Dir = packageDir
		if err := context.CheckPackage(source); err != nil {
			failures = append(failures, diagnostic.Context("check actual library metadata", err))
		}
	}
	return errors.Join(failures...)
}
