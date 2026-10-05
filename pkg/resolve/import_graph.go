package resolve

import (
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io/fs"
	"maps"
	"slices"
)

// ImportGraph retains resolved imports and the parsed UB libraries they reach.
type ImportGraph struct {
	Top       []Resolution
	Libraries map[string]*UBLibrary
	sources   map[*Source][32]byte
}

// ResolveUBGraph resolves imports and invokes the visitor during traversal.
func ResolveUBGraph(
	refs map[string]ImportRef,
	resolver Resolver,
	visitor UBVisitor,
	versions map[string]string,
	source *Source,
) (*ImportGraph, error) {
	w := &ubWalker{
		resolver: resolver, visitor: visitor, versions: versions,
		factoryRoot: factoryRootSource(source),
		parsed:      map[string]*UBLibrary{}, inProgress: map[string]bool{},
		goModuleProjects: map[string]string{}, goPackageModules: map[string]string{},
		sourceRevisions: map[*Source][32]byte{},
	}
	if source != nil && source.FS != nil {
		files, err := readUBSourceFiles(source)
		if err != nil {
			return nil, err
		}
		w.sourceRevisions[source] = sourceDigest(files)
	}
	top, err := w.walkRefs(refs, "", source, sourceKey(source, ""))
	if err != nil {
		return nil, err
	}
	return &ImportGraph{Top: top, Libraries: w.parsed, sources: w.sourceRevisions}, nil
}

// Visit reuses resolved imports and parsed bodies without reading source files.
func (g *ImportGraph) Visit(visitor UBVisitor) error {
	if g == nil || visitor == nil {
		return nil
	}
	seen := map[string]bool{}
	var visit func([]Resolution) error
	visit = func(imports []Resolution) error {
		for _, res := range imports {
			if res.Kind == ResolutionGo {
				if err := visitor.OnGoImport(
					res.LocalAlias, res.Path, res.ModulePath, res.Version,
				); err != nil {
					return fmt.Errorf("import %q: %w", res.LocalAlias, err)
				}
				continue
			}
			if seen[res.CanonicalKey] {
				continue
			}
			lib := g.Libraries[res.CanonicalKey]
			if lib == nil {
				return fmt.Errorf("import %q: missing resolved library %s",
					res.LocalAlias, res.CanonicalKey)
			}
			seen[res.CanonicalKey] = true
			for _, entry := range lib.CompositeEntries() {
				if err := visit(lib.BodyImports[entry.Kind][entry.Name]); err != nil {
					return fmt.Errorf("import %q: composite %q: %w",
						res.LocalAlias, entry.Name, err)
				}
			}
			if err := visitor.OnUBLibrary(res.LocalAlias, res.CanonicalKey, res.Ref, lib); err != nil {
				return fmt.Errorf("import %q: %w", res.LocalAlias, err)
			}
		}
		return nil
	}
	return visit(g.Top)
}

// ValidateSources rejects changes to the UB inputs used by the graph.
func (g *ImportGraph) ValidateSources() error {
	if g == nil {
		return nil
	}
	for _, source := range slices.SortedFunc(maps.Keys(g.sources), func(a, b *Source) int {
		return cmp.Compare(a.Path, b.Path)
	}) {
		files, err := readUBSourceFiles(source)
		if err != nil {
			return fmt.Errorf("read source %s: %w", source.Path, err)
		}
		if sourceDigest(files) != g.sources[source] {
			return fmt.Errorf("UB source changed during import analysis: %s", source.Path)
		}
	}
	return nil
}

func sourceDigest(files map[string][]byte) [32]byte {
	hash := sha256.New()
	for _, name := range slices.Sorted(maps.Keys(files)) {
		_, _ = hash.Write([]byte(name))
		_, _ = hash.Write([]byte{0})
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(files[name])))
		_, _ = hash.Write(size[:])
		_, _ = hash.Write(files[name])
	}
	var digest [32]byte
	copy(digest[:], hash.Sum(nil))
	return digest
}

func readUBSourceFiles(source *Source) (map[string][]byte, error) {
	names, err := fs.Glob(source.FS, "*.ub")
	if err != nil {
		return nil, err
	}
	files := make(map[string][]byte, len(names))
	for _, name := range names {
		body, err := readSourceFile(source, name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		files[name] = body
	}
	return files, nil
}
