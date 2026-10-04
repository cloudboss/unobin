package project

import (
	"fmt"
	"maps"
	"os"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/resolve"
)

type dependencyPreparation struct {
	Project     *deps.Project
	Selection   map[deps.Dependency]string
	Resolution  *deps.Resolution
	Lock        *deps.ProjectLock
	Libraries   []golibrary.PackageMetadata
	Diagnostics []diagnostic.Diagnostic
}

func cloneDependencyProject(project *deps.Project) *deps.Project {
	copy := *project
	copy.Requires = maps.Clone(project.Requires)
	copy.Replace = maps.Clone(project.Replace)
	return &copy
}

func prepareDependencies(
	root string,
	project *deps.Project,
	options Options,
	resolver resolve.Resolver,
) (*dependencyPreparation, error) {
	toolOutput := options.toolOutput()
	project = cloneDependencyProject(project)
	if err := deps.CheckReplacementSentinels(project); err != nil {
		return nil, err
	}
	unobinReplace, err := UnobinReplacement(root, options.ReplaceUnobin, project.Replace)
	if err != nil {
		return nil, err
	}
	compatibility, err := options.Compatibility(root, project, unobinReplace)
	if err != nil {
		return nil, err
	}
	if resolver == nil {
		resolver, err = options.resolver(root, options.ReplaceUnobin, project.Replace)
		if err != nil {
			return nil, err
		}
		resolver = deps.NewTrialResolver(resolver)
	}
	resolution, err := deps.ResolveWithTrace(project, deps.NewFetcher(resolver))
	if err != nil {
		return nil, diagnostic.WithDiagnostics(err,
			dependencyTrialDiagnostics(err, "", "", "", resolution)...)
	}
	selection := resolution.Selection
	schemaRoots := compile.UnobinSchemaRoots(toolOutput, unobinReplace, options.UnobinVersion)
	prepared, err := deps.PrepareProjectLock(os.DirFS(root), selection, resolver, project.Replace,
		deps.ProjectLockOptions{SchemaRoots: schemaRoots, Compatibility: compatibility})
	if err != nil {
		return nil, diagnostic.WithDiagnostics(err,
			dependencyTrialDiagnostics(err, "", "", "", resolution)...)
	}
	prepared.Lock.ToolchainVersion = options.UnobinVersion
	manifest := prepared.Compatibility.Manifest()
	diagnostics := []diagnostic.Diagnostic{}
	for _, metadata := range manifest {
		module := metadata.Source.Module
		if module.Replacement == "" {
			continue
		}
		diagnostics = append(diagnostics, diagnostic.Diagnostic{
			Code: "unobin.library-api.module-source", Severity: diagnostic.SeverityInfo,
			Message: fmt.Sprintf("Using local package %s from %s",
				metadata.Package, module.Replacement),
			LibraryCompatibility: prepared.Compatibility.DiagnosticDetails(metadata),
		})
	}
	if project.UnobinVersion != "" && unobinReplace != "" {
		diagnostics = append(diagnostics, diagnostic.Diagnostic{
			Code: "unobin.compile.replaced-toolchain", Severity: diagnostic.SeverityInfo,
			Message: fmt.Sprintf("the project pins unobin %s; the replacement at %s runs instead",
				project.UnobinVersion, unobinReplace),
			LibraryCompatibility: &diagnostic.LibraryCompatibilityDetails{
				Floor: project.UnobinVersion, UnobinVersion: options.UnobinVersion,
				Replacement: unobinReplace,
			},
		})
	}
	return &dependencyPreparation{
		Project: project, Selection: selection, Lock: prepared.Lock,
		Resolution: resolution,
		Libraries:  manifest, Diagnostics: diagnostic.Merge(diagnostics),
	}, nil
}
