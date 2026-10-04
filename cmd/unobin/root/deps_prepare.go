package root

import (
	"io"
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
	replaceUnobin string,
	toolOutput io.Writer,
	resolver resolve.Resolver,
) (*dependencyPreparation, error) {
	project = cloneDependencyProject(project)
	if err := deps.CheckReplacementSentinels(project); err != nil {
		return nil, err
	}
	unobinReplace, err := printGraphUnobinReplace(root, replaceUnobin, project.Replace)
	if err != nil {
		return nil, err
	}
	compatibility, err := newCommandCompatibility(root, project, unobinReplace)
	if err != nil {
		return nil, err
	}
	if resolver == nil {
		resolver, err = newDepsResolver(root, replaceUnobin, project.Replace)
		if err != nil {
			return nil, err
		}
		resolver = deps.NewTrialResolver(resolver)
	}
	selection, err := deps.Resolve(project, deps.NewFetcher(resolver))
	if err != nil {
		return nil, err
	}
	schemaRoots := compile.UnobinSchemaRoots(toolOutput, unobinReplace, cliVersion())
	prepared, err := deps.PrepareProjectLock(os.DirFS(root), selection, resolver, project.Replace,
		deps.ProjectLockOptions{SchemaRoots: schemaRoots, Compatibility: compatibility})
	if err != nil {
		return nil, err
	}
	prepared.Lock.ToolchainVersion = cliVersion()
	return &dependencyPreparation{
		Project: project, Selection: selection, Lock: prepared.Lock,
		Libraries: prepared.Compatibility.Manifest(), Diagnostics: []diagnostic.Diagnostic{},
	}, nil
}
