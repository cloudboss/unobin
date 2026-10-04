package golibrary

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type sourceSnapshot struct {
	roots []ModuleSource
	files map[string][]byte
	seen  map[string]bool
}

func SourceSnapshot(dir string, roots []ModuleSource) ([32]byte, error) {
	moduleRoot, err := FindModuleRoot(dir)
	if err != nil {
		return [32]byte{}, err
	}
	modulePath, err := readModulePath(moduleRoot)
	if err != nil {
		return [32]byte{}, err
	}
	s := &sourceSnapshot{
		roots: []ModuleSource{{Path: modulePath, Dir: moduleRoot}},
		files: map[string][]byte{}, seen: map[string]bool{},
	}
	for _, root := range roots {
		if root.Dir == "" || root.Path == "" {
			continue
		}
		root.Dir, err = filepath.Abs(root.Dir)
		if err != nil {
			return [32]byte{}, err
		}
		s.roots = append(s.roots, root)
	}
	if err := s.readPackage(dir); err != nil {
		return [32]byte{}, err
	}
	names := make([]string, 0, len(s.files))
	for name := range s.files {
		names = append(names, name)
	}
	slices.Sort(names)
	hash := sha256.New()
	for _, name := range names {
		body := s.files[name]
		_, _ = hash.Write([]byte(name))
		_, _ = hash.Write([]byte{0})
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(body)))
		_, _ = hash.Write(size[:])
		_, _ = hash.Write(body)
	}
	var digest [32]byte
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}

func (s *sourceSnapshot) readPackage(dir string) error {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if s.seen[dir] {
		return nil
	}
	s.seen[dir] = true
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	moduleRoot, err := FindModuleRoot(dir)
	if err != nil {
		return err
	}
	moduleFile := filepath.Join(moduleRoot, "go.mod")
	if _, ok := s.files[moduleFile]; !ok {
		body, err := os.ReadFile(moduleFile)
		if err != nil {
			return err
		}
		s.files[moduleFile] = body
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		filename := filepath.Join(dir, name)
		body, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		s.files[filename] = body
		file, err := parser.ParseFile(token.NewFileSet(), filename, body, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if err := s.readImport(importPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *sourceSnapshot) readImport(importPath string) error {
	var selected ModuleSource
	for _, root := range s.roots {
		if importPath != root.Path && !strings.HasPrefix(importPath, root.Path+"/") {
			continue
		}
		if len(root.Path) > len(selected.Path) {
			selected = root
		}
	}
	if selected.Dir == "" {
		return nil
	}
	relative := strings.TrimPrefix(strings.TrimPrefix(importPath, selected.Path), "/")
	dir := filepath.Join(selected.Dir, filepath.FromSlash(relative))
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		s.files[dir] = nil
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		s.files[dir] = nil
		return nil
	}
	return s.readPackage(dir)
}

func (c *CompatibilityContext) SourceSnapshot(dir string) ([32]byte, error) {
	roots := slices.Clone(c.options.Modules)
	if c.options.CoreReplacement != "" {
		roots = append(roots, ModuleSource{
			Path: "github.com/cloudboss/unobin", Dir: c.options.CoreReplacement,
		})
	}
	return SourceSnapshot(dir, roots)
}

func (c *CompatibilityContext) CheckDirectory(dir string, linked bool) error {
	if dir == "" {
		return nil
	}
	if err := c.checkCoreReplacement(); err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	moduleRoot, err := FindModuleRoot(abs)
	if err != nil {
		return err
	}
	source := PackageSource{Module: ModuleSource{Dir: moduleRoot}, Dir: abs, Linked: linked}
	for _, root := range c.options.Modules {
		if root.Dir == "" {
			continue
		}
		root.Dir, err = filepath.Abs(root.Dir)
		if err != nil {
			return err
		}
		if root.Dir == moduleRoot {
			source.Module = root
			break
		}
	}
	return c.CheckPackage(source)
}
