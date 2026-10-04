package root

import (
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/libraryapi"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func prepareDependencyCandidate(
	root, projectName string,
	project *deps.Project,
	imported map[deps.RemotePackage]bool,
	lock *deps.ProjectLock,
	cfg *depsSyncConfig,
	dependency deps.Dependency,
	version string,
	toolOutput io.Writer,
) (*dependencyPreparation, bool, error) {
	project = cloneDependencyProject(project)
	resolver, err := newDepsResolver(root, cfg.replaceUnobin, project.Replace)
	if err != nil {
		return nil, false, err
	}
	resolver = deps.NewTrialResolver(resolver)
	directTarget := dependencyOwnsImportedPackage(dependency, imported)
	if directTarget {
		project.SetRequire(dependency, version, false)
	}
	direct, err := directRequirementsForImports(projectName, project, imported, lock, resolver)
	if err != nil {
		return nil, false, err
	}
	for dependency, version := range direct {
		project.SetRequire(dependency, version, false)
	}
	if !directTarget {
		project.SetRequire(dependency, version, true)
		resolution, err := deps.ResolveWithTrace(project, deps.NewFetcher(resolver))
		if err != nil {
			return nil, true, diagnostic.WithDiagnostics(err,
				dependencyTrialDiagnostics(err, "", "", "", resolution)...)
		}
		if !dependencyReachableFromImports(resolution, direct, dependency) {
			return nil, true, fmt.Errorf(
				"%s is not imported directly or transitively by this project", dependency)
		}
	}
	prepared, err := prepareDependencies(root, project, cfg.replaceUnobin, toolOutput, resolver)
	return prepared, !directTarget, err
}

func retryDependencyCandidate(err error) bool {
	if err == nil {
		return false
	}
	switch failure := err.(type) {
	case *golibrary.CompatibilityError:
		return failure.Kind == golibrary.MissingDeclaration
	case *golibrary.ConfigurationSourceError, *golibrary.CoreDescriptorError,
		*golibrary.ToolchainPinError:
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !retryDependencyCandidate(child) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return retryDependencyCandidate(wrapped.Unwrap())
	}
	var unsupported *libraryapi.UnsupportedMajorError
	var newer *libraryapi.NewerMinorError
	var coreFloor *golibrary.CoreFloorError
	var selectedSource *deps.SourceSelectionError
	var modulePath *resolve.ModulePathError
	return errors.As(err, &unsupported) || errors.As(err, &newer) || errors.As(err, &coreFloor) ||
		errors.As(err, &selectedSource) || errors.As(err, &modulePath)
}

func dependencyTrialDiagnostics(
	err error,
	candidate, query, floor string,
	resolution *deps.Resolution,
) []diagnostic.Diagnostic {
	diagnostics := diagnostic.FromError(err, diagnostic.ConvertOptions{})
	for i := range diagnostics {
		details := diagnostics[i].LibraryCompatibility
		if details == nil {
			continue
		}
		if candidate != "" {
			details.CandidateVersion = candidate
			details.Query = query
			details.Floor = floor
		}
		if resolution == nil || details.Dependency == "" {
			continue
		}
		dependency, err := deps.ParseDependency(details.Dependency)
		if err != nil {
			continue
		}
		if details.Version == "" && details.Replacement == "" {
			details.Version = resolution.Selection[dependency]
		}
		chain := resolution.Chains[dependency]
		if len(chain) == 0 {
			continue
		}
		details.RequirementChain = make([]diagnostic.LibraryRequirementStep, 0, len(chain))
		for _, step := range chain {
			declaring := step.Dependency.String()
			if declaring == "" {
				declaring = deps.ProjectFileName
			}
			details.RequirementChain = append(details.RequirementChain, diagnostic.LibraryRequirementStep{
				Dependency: declaring, Version: step.Version,
				Requires: step.Requires.String(), MinimumVersion: step.MinimumVersion,
			})
		}
	}
	return diagnostic.Normalize(diagnostics)
}

func noCompatibleDependencyError(
	dependency deps.Dependency,
	query, floor string,
	cause error,
	notices []diagnostic.Diagnostic,
) error {
	message := fmt.Sprintf("%s: no compatible version", dependency)
	if query != "" {
		message += " for " + query
	}
	if floor != "" {
		message += "; project.ub requires at least " + floor
	}
	failure := errors.New(message)
	if cause != nil {
		failure = fmt.Errorf("%s: %w", message, cause)
	}
	descriptor := libraryapi.Current()
	if libraryAPIDescriptor != nil {
		descriptor = *libraryAPIDescriptor
	}
	details := &diagnostic.LibraryCompatibilityDetails{
		Dependency: dependency.String(), Query: query, Floor: floor,
		UnobinVersion: cliVersion(), ImplementedAPIs: slices.Clone(descriptor.ImplementedAPIs),
	}
	if floor != "" {
		details.RequirementChain = []diagnostic.LibraryRequirementStep{{
			Dependency: deps.ProjectFileName, Requires: dependency.String(), MinimumVersion: floor,
		}}
	}
	final := diagnostic.Diagnostic{
		Code: "unobin.library-api.no-compatible-version", Severity: diagnostic.SeverityError,
		Message: message, Path: deps.ProjectFileName, LibraryCompatibility: details,
		Hint: "Choose an explicit compatible floor with `unobin deps get <project>@<version>` " +
			"or update the requirement that forces an incompatible selection.",
	}
	return diagnostic.WithDiagnostics(failure, append(slices.Clone(notices), final)...)
}

func dependencyReachableFromImports(
	resolution *deps.Resolution,
	direct map[deps.Dependency]string,
	target deps.Dependency,
) bool {
	reachable := map[deps.Dependency]bool{}
	for dependency := range direct {
		reachable[dependency] = true
	}
	for changed := true; changed; {
		changed = false
		for _, step := range resolution.Requirements {
			if reachable[step.Dependency] && !reachable[step.Requires] {
				reachable[step.Requires] = true
				changed = true
			}
		}
	}
	return reachable[target]
}
