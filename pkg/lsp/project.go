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
	Root          string
	Marker        projectmarker.Marker
	DepsProject   *deps.Project
	ProjectLock   *deps.ProjectLock
	Resolver      *ImportResolver
	GoSchemas     *compile.SchemaCache
	GoIndex       *goschema.SourceIndexCache
	GoModuleRoots []goschema.ModuleRoot
}

// ProjectCache stores project data by marker root.
type ProjectCache struct {
	workspaceRoot  string
	workspaceRoots []string
	projects       map[string]*Project
	remoteFactory  func() (cachedRemoteSource, error)
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
	cache := &ProjectCache{
		projects:      map[string]*Project{},
		remoteFactory: remoteFactory,
	}
	cache.SetWorkspaceRoots(singleWorkspaceRoot(workspaceRoot))
	return cache
}

// SetWorkspaceRoots sets weak roots used for loose files without project markers.
func (c *ProjectCache) SetWorkspaceRoots(roots []string) {
	if c == nil {
		return
	}
	c.workspaceRoots = cleanWorkspaceRoots(roots)
	c.workspaceRoot = ""
	if len(c.workspaceRoots) > 0 {
		c.workspaceRoot = c.workspaceRoots[0]
	}
}

// ProjectForPath returns cached project data for path's nearest marker root.
func (c *ProjectCache) ProjectForPath(path string) (*Project, error) {
	root, marker, err := deps.FindProjectMarkerDir(path)
	if err != nil {
		weakRoot, ok := c.workspaceRootForPath(path)
		if !errors.Is(err, fs.ErrNotExist) || !ok {
			return nil, err
		}
		root = weakRoot
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
	for root, project := range c.projects {
		if pathInDir(path, root) {
			delete(c.projects, root)
			continue
		}
		if strings.HasSuffix(path, ".go") {
			project.invalidateGoPath(path)
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
	roots := schemaRootsForProject(root, marker)
	return &Project{
		Root:          root,
		Marker:        marker,
		DepsProject:   depProject,
		ProjectLock:   projectLock,
		Resolver:      NewImportResolver(root, depProject, projectLock, remote),
		GoSchemas:     compile.NewSchemaCache(roots...),
		GoIndex:       goschema.NewSourceIndexCache(roots...),
		GoModuleRoots: roots,
	}, nil
}

// EnsureGoModuleRoot adds the Go module root for source when it can be found.
func (p *Project) EnsureGoModuleRoot(source *resolve.Source) {
	root, ok := goModuleRootForSource(source)
	if !ok || p.hasGoModuleRoot(root) {
		return
	}
	p.GoModuleRoots = append(p.GoModuleRoots, root)
	p.GoSchemas = compile.NewSchemaCache(p.GoModuleRoots...)
	p.GoIndex = goschema.NewSourceIndexCache(p.GoModuleRoots...)
}

func (p *Project) hasGoModuleRoot(root goschema.ModuleRoot) bool {
	for _, existing := range p.GoModuleRoots {
		if existing.Path == root.Path && filepath.Clean(existing.Dir) == filepath.Clean(root.Dir) {
			return true
		}
	}
	return false
}

func (p *Project) invalidateGoPath(path string) {
	for _, root := range p.GoModuleRoots {
		if pathInDir(path, root.Dir) {
			p.GoSchemas = compile.NewSchemaCache(p.GoModuleRoots...)
			p.GoIndex = goschema.NewSourceIndexCache(p.GoModuleRoots...)
			return
		}
	}
}

func (c *ProjectCache) workspaceRootForPath(path string) (string, bool) {
	var best string
	for _, root := range c.workspaceRoots {
		if pathInDir(path, root) && len(root) > len(best) {
			best = root
		}
	}
	if best != "" {
		return best, true
	}
	return "", false
}

func singleWorkspaceRoot(root string) []string {
	if root == "" {
		return nil
	}
	return []string{root}
}

func cleanWorkspaceRoots(roots []string) []string {
	out := make([]string, 0, len(roots))
	seen := map[string]struct{}{}
	for _, root := range roots {
		if root == "" {
			continue
		}
		clean := filepath.Clean(root)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func goModuleRootForSource(source *resolve.Source) (goschema.ModuleRoot, bool) {
	if source == nil {
		return goschema.ModuleRoot{}, false
	}
	if source.ModuleRootPath != "" && source.ModulePath != "" {
		return goschema.ModuleRoot{Path: source.ModulePath, Dir: source.ModuleRootPath}, true
	}
	if source.Path == "" {
		return goschema.ModuleRoot{}, false
	}
	root, marker, err := deps.FindProjectMarkerDir(source.Path)
	if err != nil || marker.Kind != projectmarker.Go || marker.ModulePath == "" {
		return goschema.ModuleRoot{}, false
	}
	return goschema.ModuleRoot{Path: marker.ModulePath, Dir: root}, true
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
