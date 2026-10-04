package project

import (
	"errors"
	"io/fs"
	"os"

	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/resolve"
)

type DependencyEntry struct {
	ID       string               `json:"id"       ub:"id"`
	Kind     deps.ProjectLockKind `json:"kind"     ub:"kind"`
	Version  string               `json:"version"  ub:"version"`
	Indirect bool                 `json:"indirect" ub:"indirect"`
}

func ListDependencies(path string) ([]DependencyEntry, error) {
	root, err := projectRoot(path)
	if err != nil {
		return nil, err
	}
	projectLock, err := readProjectLock(path)
	if err != nil {
		return nil, err
	}
	project, err := deps.ReadProject(os.DirFS(root))
	if errors.Is(err, fs.ErrNotExist) {
		project = nil
	} else if err != nil {
		return nil, err
	}
	dependencies := []DependencyEntry{}
	for _, id := range projectLock.SortedIDs() {
		selected := projectLock.Deps[id]
		indirect := true
		if project != nil {
			dependency, parseErr := deps.ParseDependency(id)
			if parseErr != nil {
				return nil, parseErr
			}
			if requirement, ok := project.Requires[dependency]; ok {
				indirect = requirement.Indirect
			}
		}
		dependencies = append(dependencies, DependencyEntry{
			ID:       id,
			Kind:     selected.Kind,
			Version:  selected.Version,
			Indirect: indirect,
		})
	}
	return dependencies, nil
}

func VerifyDependencies(options Options) (*deps.VerifyResult, error) {
	projectLock, err := readProjectLock(options.Path)
	if err != nil {
		return nil, err
	}
	root, err := projectRoot(options.Path)
	if err != nil {
		return nil, err
	}
	resolver, err := options.resolver(root, options.ReplaceUnobin, nil)
	if err != nil {
		return nil, err
	}
	verified, err := deps.Verify(projectLock, resolver)
	if err != nil {
		return nil, err
	}
	return verified, nil
}

type CacheCleanResult struct {
	Path    string
	Removed bool
}

func CleanDependencies(options Options) (*CacheCleanResult, error) {
	factory := options.NewRemoteResolver
	if factory == nil {
		factory = resolve.NewRemoteResolver
	}
	resolver, err := factory()
	if err != nil {
		return nil, err
	}
	removed := false
	if _, err := os.Stat(resolver.ImportsDir()); err == nil {
		removed = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	dir, err := resolver.CleanImports()
	if err != nil {
		return nil, err
	}
	return &CacheCleanResult{Path: dir, Removed: removed}, nil
}
