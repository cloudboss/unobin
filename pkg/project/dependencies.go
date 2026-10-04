package project

import (
	"fmt"
	"slices"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/filechange"
	"github.com/cloudboss/unobin/pkg/toolchain"
)

type DependencyWriteResult struct {
	ProjectFile string
	LockFile    string
	Direct      int
	Indirect    int
	Selected    int
	Files       []filechange.Change
	Diagnostics []diagnostic.Diagnostic
}

type DependencyResult struct {
	Dependency      string
	Version         string
	SelectedVersion string
	Indirect        bool
	Write           *DependencyWriteResult
	Diagnostics     []diagnostic.Diagnostic
}

func SyncDependencies(options Options) (*DependencyWriteResult, error) {
	root, err := projectRoot(options.Path)
	if err != nil {
		return nil, err
	}
	project, projectName, err := readProjectOrEmpty(root)
	if err != nil {
		return nil, err
	}
	unobinReplace, err := UnobinReplacement(root, options.ReplaceUnobin, project.Replace)
	if err != nil {
		return nil, err
	}
	if _, err := options.Compatibility(root, project, unobinReplace); err != nil {
		return nil, err
	}
	imported, err := deps.ImportedPackages(root)
	if err != nil {
		return nil, err
	}
	projectLock, err := readProjectLockOrNil(root)
	if err != nil {
		return nil, err
	}
	resolver, err := options.resolver(root, options.ReplaceUnobin, project.Replace)
	if err != nil {
		return nil, err
	}
	resolver = deps.NewTrialResolver(resolver)
	if err := reconcileProject(
		projectName, project, imported, projectLock, resolver, options.listTags,
	); err != nil {
		return nil, err
	}
	prepared, err := prepareDependencies(root, project, options, resolver)
	if err != nil {
		return nil, err
	}
	result, err := writeDependencyFiles(root, prepared.Project, prepared.Lock)
	if result != nil {
		result.Diagnostics = prepared.Diagnostics
	}
	return result, err
}

func UpdateDependency(options Options, arg string) (*DependencyResult, error) {
	root, err := projectRoot(options.Path)
	if err != nil {
		return nil, err
	}
	dep, query, err := parseGetArg(arg)
	if err != nil {
		return nil, err
	}
	if deps.IsReplacementSentinel(query) {
		return nil, fmt.Errorf("%s is reserved for project replacements", query)
	}
	if dep.URL == toolchain.UnobinModulePath {
		return nil, fmt.Errorf(
			"%s is toolchain-versioned; pin it with the project's unobin-version line",
			dep.URL)
	}
	project, projectName, err := readProjectOrEmpty(root)
	if err != nil {
		return nil, err
	}
	unobinReplace, err := UnobinReplacement(root, options.ReplaceUnobin, project.Replace)
	if err != nil {
		return nil, err
	}
	if _, err := options.Compatibility(root, project, unobinReplace); err != nil {
		return nil, err
	}
	imported, err := deps.ImportedPackages(root)
	if err != nil {
		return nil, err
	}
	projectLock, err := readProjectLockOrNil(root)
	if err != nil {
		return nil, err
	}
	tags, err := options.listTags(dep.URL)
	if err != nil {
		return nil, err
	}
	candidates, err := deps.VersionCandidates(dep, query, tags)
	if err != nil {
		return nil, err
	}
	automatic := query == "" || query == "latest"
	exact := query != "" &&
		semver.Canonical(query) == strings.TrimSuffix(query, semver.Build(query))
	floor := ""
	if automatic && !deps.IsReplacementSentinel(project.Requires[dep].Version) {
		floor = project.Requires[dep].Version
	}
	notices := []diagnostic.Diagnostic{}
	var lastFailure error
	for _, version := range candidates {
		if floor != "" && semver.Compare(version, floor) < 0 {
			continue
		}
		prepared, indirect, err := prepareDependencyCandidate(root, projectName, project,
			imported, projectLock, options, dep, version)
		if err != nil {
			diagnostics := dependencyTrialDiagnostics(err, version, query, floor, nil)
			if exact || !retryDependencyCandidate(err) {
				return nil, diagnostic.WithDiagnostics(err,
					append(slices.Clone(notices), diagnostics...)...)
			}
			for i := range diagnostics {
				diagnostics[i].Severity = diagnostic.SeverityInfo
			}
			notices = append(notices, diagnostics...)
			lastFailure = err
			if options.Progress != nil {
				options.Progress(DependencyProgress{Dependency: dep, Version: version, Err: err})
			}
			continue
		}
		diagnostics := diagnostic.Merge(notices, prepared.Diagnostics)
		writeResult, err := writeDependencyFiles(root, prepared.Project, prepared.Lock)
		if writeResult != nil {
			writeResult.Diagnostics = prepared.Diagnostics
		}
		selectedVersion := prepared.Selection[dep]
		if err == nil && options.Progress != nil {
			options.Progress(DependencyProgress{
				Dependency: dep, Version: version, SelectedVersion: selectedVersion,
			})
		}
		if err != nil {
			err = diagnostic.WithDiagnostics(err, append(diagnostics,
				diagnostic.FromError(err, diagnostic.ConvertOptions{})...)...)
		}
		return &DependencyResult{
			Dependency: dep.String(), Version: version, SelectedVersion: selectedVersion,
			Indirect: indirect, Write: writeResult, Diagnostics: diagnostics,
		}, err
	}
	return nil, noCompatibleDependencyError(options, dep, query, floor, lastFailure, notices)
}
