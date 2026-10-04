package golibrary

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/libraryapi"
)

// ModuleSource identifies a selected Go module and its effective source.
type ModuleSource struct {
	Path        string
	Dir         string
	Dependency  string
	Version     string
	Commit      string
	Replacement string
}

// PackageSource identifies an inspected package and whether the build links it.
type PackageSource struct {
	Module ModuleSource
	Dir    string
	Linked bool
}

// PackageMetadata records the declaration and module requirements used by a check.
type PackageMetadata struct {
	Source              PackageSource
	Package             string
	Declaration         CompatibilityDeclaration
	RequiredCoreVersion string
	MinimumGoVersion    string
}

// CompatibilityOptions supplies one operation's toolchain and selected sources.
type CompatibilityOptions struct {
	Descriptor      *libraryapi.Descriptor
	UnobinVersion   string
	CoreReplacement string
}

// CompatibilityContext checks eligibility before registration or schema reads.
type CompatibilityContext struct {
	descriptor libraryapi.Descriptor
	options    CompatibilityOptions
	entries    map[string]PackageMetadata
}

// NewCompatibilityContext validates the descriptor and effective core replacement.
func NewCompatibilityContext(options CompatibilityOptions) (*CompatibilityContext, error) {
	descriptor := libraryapi.Current()
	if options.Descriptor != nil {
		descriptor = *options.Descriptor
		descriptor.ImplementedAPIs = slices.Clone(descriptor.ImplementedAPIs)
	}
	if err := descriptor.Validate(); err != nil {
		return nil, diagnostic.WithDiagnostics(err, diagnostic.Diagnostic{
			Code: "unobin.library-api.core-descriptor", Severity: diagnostic.SeverityError,
			Message: "invalid toolchain library API descriptor: " + err.Error(),
		})
	}
	options.Descriptor = nil
	c := &CompatibilityContext{
		descriptor: descriptor, options: options, entries: map[string]PackageMetadata{},
	}
	if err := c.checkCoreReplacement(); err != nil {
		return nil, err
	}
	return c, nil
}

// CheckPackage reads current source metadata without deriving schemas or executing Go.
func (c *CompatibilityContext) CheckPackage(source PackageSource) error {
	if source.Dir == "" {
		return nil
	}
	var err error
	source.Dir, err = filepath.Abs(source.Dir)
	if err != nil {
		return err
	}
	previous := c.entries[source.Dir]
	delete(c.entries, source.Dir)
	if source.Module.Dir == "" {
		source.Module.Dir, err = FindModuleRoot(source.Dir)
		if err != nil {
			return err
		}
	}
	source.Module.Dir, err = filepath.Abs(source.Module.Dir)
	if err != nil {
		return err
	}
	module, err := readModuleFile(source.Module.Dir)
	if err != nil {
		return err
	}
	source.Module.Path = module.Module.Mod.Path
	source.Linked = source.Linked || previous.Source.Linked
	rel, err := filepath.Rel(source.Module.Dir, source.Dir)
	if err != nil {
		return err
	}
	packagePath := source.Module.Path
	if rel != "." {
		packagePath += "/" + filepath.ToSlash(rel)
	}
	metadata := PackageMetadata{Source: source, Package: packagePath}
	details := &diagnostic.LibraryCompatibilityDetails{
		Dependency: source.Module.Dependency, Package: packagePath, ModulePath: source.Module.Path,
		Version: source.Module.Version, Commit: source.Module.Commit,
		Replacement: source.Module.Replacement, UnobinVersion: c.options.UnobinVersion,
		ImplementedAPIs: slices.Clone(c.descriptor.ImplementedAPIs),
	}
	if module.Go != nil {
		metadata.MinimumGoVersion = module.Go.Version
		details.MinimumGoVersion = module.Go.Version
	}
	var coreRequirement *modfile.Require
	for _, requirement := range module.Require {
		if requirement.Mod.Path == "github.com/cloudboss/unobin" {
			coreRequirement = requirement
			metadata.RequiredCoreVersion = requirement.Mod.Version
			details.RequiredCoreVersion = requirement.Mod.Version
			break
		}
	}
	declaration, err := ReadCompatibility(source.Module.Dir, source.Dir)
	if err != nil {
		var invalid *CompatibilityError
		if !errors.As(err, &invalid) {
			return err
		}
		ds := invalid.Diagnostics()
		for i := range ds {
			if ds[i].LibraryCompatibility != nil {
				details.RequiredAPI = ds[i].LibraryCompatibility.RequiredAPI
				details.SuggestedUnobinVersion = ds[i].LibraryCompatibility.SuggestedUnobinVersion
			}
			ds[i].LibraryCompatibility = details
		}
		return diagnostic.WithDiagnostics(err, ds...)
	}
	metadata.Declaration = *declaration
	details.RequiredAPI = declaration.RequiredAPI
	details.SuggestedUnobinVersion = declaration.SuggestedUnobinVersion
	if err := libraryapi.Check(declaration.RequiredAPI, c.descriptor); err != nil {
		code := "unobin.library-api.unsupported-major"
		var newer *libraryapi.NewerMinorError
		if errors.As(err, &newer) {
			code = "unobin.library-api.newer-minor"
		}
		hint := "Upgrade Unobin to a release implementing the required API, " +
			"or select a compatible library release with `unobin deps get`."
		if declaration.SuggestedUnobinVersion != "" {
			hint = "The library recommends Unobin " + declaration.SuggestedUnobinVersion + ". " + hint
		}
		return diagnostic.WithDiagnostics(err, diagnostic.Diagnostic{
			Code: code, Severity: diagnostic.SeverityError,
			Message: fmt.Sprintf("library %s requires API %s; Unobin %s implements %s",
				packagePath, declaration.RequiredAPI, c.options.UnobinVersion,
				strings.Join(c.descriptor.ImplementedAPIs, ", ")),
			Path: declaration.Path, Span: declaration.RequiredAPISpan, Hint: hint,
			LibraryCompatibility: details,
		})
	}
	version := semver.Canonical(strings.TrimSuffix(c.options.UnobinVersion, "+dirty"))
	if source.Linked && c.options.CoreReplacement == "" && coreRequirement != nil &&
		version != "" && semver.Compare(coreRequirement.Mod.Version, version) > 0 {
		err := &CoreFloorError{RequiredVersion: coreRequirement.Mod.Version, UnobinVersion: version}
		position := coreRequirement.Syntax.Start
		return diagnostic.WithDiagnostics(err, diagnostic.Diagnostic{
			Code: "unobin.library-api.core-floor", Severity: diagnostic.SeverityError,
			Message: err.Error(), Path: filepath.Join(source.Module.Dir, "go.mod"),
			Span: &diagnostic.Span{Start: diagnostic.Position{
				Line: position.Line, Column: position.LineRune, Offset: position.Byte,
			}},
			Hint: "Use a compatible Unobin release or explicitly select a " +
				"compatible library release.",
			LibraryCompatibility: details,
		})
	}
	c.entries[source.Dir] = metadata
	return nil
}

// Manifest returns independent metadata records in deterministic package order.
func (c *CompatibilityContext) Manifest() []PackageMetadata {
	entries := make([]PackageMetadata, 0, len(c.entries))
	for _, entry := range c.entries {
		entry.Declaration = cloneDeclaration(entry.Declaration)
		entries = append(entries, entry)
	}
	slices.SortFunc(entries, func(a, b PackageMetadata) int {
		if n := cmp.Compare(a.Package, b.Package); n != 0 {
			return n
		}
		return cmp.Compare(a.Source.Dir, b.Source.Dir)
	})
	return entries
}

// CoreFloorError identifies a linked module requiring a newer core release.
type CoreFloorError struct {
	RequiredVersion string
	UnobinVersion   string
}

func (e *CoreFloorError) Error() string {
	return fmt.Sprintf("library requires Unobin module %s; this compiler is %s",
		e.RequiredVersion, e.UnobinVersion)
}

// CoreDescriptorError identifies an unusable effective toolchain descriptor.
type CoreDescriptorError struct {
	Path    string
	Message string
	Cause   error
}

func (e *CoreDescriptorError) Error() string { return e.Message }
func (e *CoreDescriptorError) Unwrap() error { return e.Cause }

func readModuleFile(dir string) (*modfile.File, error) {
	path := filepath.Join(dir, "go.mod")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	module, err := modfile.Parse(path, data, nil)
	if err != nil {
		return nil, err
	}
	if module.Module == nil || module.Module.Mod.Path == "" {
		return nil, fmt.Errorf("%s: missing module path", path)
	}
	return module, nil
}

func (c *CompatibilityContext) checkCoreReplacement() error {
	failure := func(path, message string, cause error) error {
		err := &CoreDescriptorError{Path: path, Message: message, Cause: cause}
		return diagnostic.WithDiagnostics(err, diagnostic.Diagnostic{
			Code: "unobin.library-api.core-descriptor", Severity: diagnostic.SeverityError,
			Message: message, Path: path,
			Hint: "Rebuild or use Unobin from the matching core source before checking libraries.",
			LibraryCompatibility: &diagnostic.LibraryCompatibilityDetails{
				ImplementedAPIs: slices.Clone(c.descriptor.ImplementedAPIs),
				UnobinVersion:   c.options.UnobinVersion, Replacement: c.options.CoreReplacement,
			},
		})
	}
	if c.options.CoreReplacement == "" {
		if c.options.UnobinVersion == "dev" {
			return failure("", "a dev compiler requires a matching local core replacement", nil)
		}
		if c.options.UnobinVersion != "" &&
			!semver.IsValid(strings.TrimSuffix(c.options.UnobinVersion, "+dirty")) {
			return failure("", "invalid compiler module version "+c.options.UnobinVersion, nil)
		}
		return nil
	}
	abs, err := filepath.Abs(c.options.CoreReplacement)
	if err != nil {
		return err
	}
	c.options.CoreReplacement = abs
	path := filepath.Join(abs, "pkg", "libraryapi", "descriptor.json")
	descriptor, err := libraryapi.ReadDescriptor(os.DirFS(abs))
	if err != nil {
		return failure(path, "cannot read the effective core API descriptor: "+err.Error(), err)
	}
	if descriptor.FormatVersion != c.descriptor.FormatVersion ||
		!slices.Equal(descriptor.ImplementedAPIs, c.descriptor.ImplementedAPIs) ||
		descriptor.GeneratorAPI != c.descriptor.GeneratorAPI {
		return failure(path, "the effective core API descriptor differs from this CLI", nil)
	}
	module, err := readModuleFile(abs)
	if err != nil {
		return failure(filepath.Join(abs, "go.mod"), "cannot read the effective core module", err)
	}
	if module.Module.Mod.Path != "github.com/cloudboss/unobin" {
		return failure(filepath.Join(abs, "go.mod"), "the core replacement has a different module path",
			nil)
	}
	return nil
}

func cloneDeclaration(declaration CompatibilityDeclaration) CompatibilityDeclaration {
	cloneSpan := func(span *diagnostic.Span) *diagnostic.Span {
		if span == nil {
			return nil
		}
		clone := *span
		if span.End != nil {
			end := *span.End
			clone.End = &end
		}
		return &clone
	}
	declaration.Span = cloneSpan(declaration.Span)
	declaration.RequiredAPISpan = cloneSpan(declaration.RequiredAPISpan)
	declaration.SuggestedUnobinVersionSpan = cloneSpan(declaration.SuggestedUnobinVersionSpan)
	return declaration
}
