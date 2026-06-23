package lsp

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/projectmarker"
	"github.com/cloudboss/unobin/pkg/resolve"
)

// Project holds cached LSP data for one project marker root.
type Project struct {
	Root        string
	Marker      projectmarker.Marker
	DepsProject *deps.Project
	ProjectLock *deps.ProjectLock
	Resolver    *ImportResolver
	GoSchemas   *compile.SchemaCache
	GoIndex     *goschema.SourceIndexCache
}

// ProjectCache stores project data by marker root.
type ProjectCache struct {
	workspaceRoot string
	projects      map[string]*Project
	remoteFactory func() (cachedRemoteSource, error)
}

// NewProjectCache returns an empty project cache.
func NewProjectCache(workspaceRoot string) *ProjectCache {
	return newProjectCacheWithRemote(workspaceRoot, func() (cachedRemoteSource, error) {
		return resolve.NewRemoteResolver()
	})
}

func newProjectCacheWithRemote(
	workspaceRoot string,
	remoteFactory func() (cachedRemoteSource, error),
) *ProjectCache {
	return &ProjectCache{
		workspaceRoot: workspaceRoot,
		projects:      map[string]*Project{},
		remoteFactory: remoteFactory,
	}
}

// ProjectForPath returns cached project data for path's nearest marker root.
func (c *ProjectCache) ProjectForPath(path string) (*Project, error) {
	root, marker, err := deps.FindProjectMarkerDir(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) || c.workspaceRoot == "" {
			return nil, err
		}
		root = c.workspaceRoot
		marker = projectmarker.Marker{Kind: projectmarker.None}
	}
	root = filepath.Clean(root)
	if project, ok := c.projects[root]; ok {
		return project, nil
	}
	project, err := c.readProject(root, marker)
	if err != nil {
		return nil, err
	}
	c.projects[root] = project
	return project, nil
}

// InvalidatePath removes cached project data affected by a saved path.
func (c *ProjectCache) InvalidatePath(path string) {
	if c == nil {
		return
	}
	for root := range c.projects {
		if pathInDir(path, root) {
			delete(c.projects, root)
		}
	}
}

func (c *ProjectCache) readProject(root string, marker projectmarker.Marker) (*Project, error) {
	var depProject *deps.Project
	var projectLock *deps.ProjectLock
	if marker.Kind == projectmarker.UB {
		var err error
		depProject, err = deps.ReadProject(os.DirFS(root))
		if err != nil {
			return nil, err
		}
		projectLock, err = deps.ReadProjectLock(os.DirFS(root))
		if errors.Is(err, fs.ErrNotExist) {
			projectLock = nil
		} else if err != nil {
			return nil, err
		}
	}
	remote, err := c.remoteFactory()
	if err != nil {
		return nil, err
	}
	return &Project{
		Root:        root,
		Marker:      marker,
		DepsProject: depProject,
		ProjectLock: projectLock,
		Resolver:    NewImportResolver(root, depProject, projectLock, remote),
		GoSchemas:   compile.NewSchemaCache(schemaRootsForProject(root, marker)...),
		GoIndex:     goschema.NewSourceIndexCache(schemaRootsForProject(root, marker)...),
	}, nil
}

func schemaRootsForProject(root string, marker projectmarker.Marker) []goschema.ModuleRoot {
	if marker.Kind != projectmarker.Go || marker.ModulePath == "" {
		return nil
	}
	return []goschema.ModuleRoot{{Path: marker.ModulePath, Dir: root}}
}

func pathInDir(path string, root string) bool {
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		pathAbs = filepath.Clean(path)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		rootAbs = filepath.Clean(root)
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
