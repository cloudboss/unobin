package compile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"

	"github.com/cloudboss/unobin/internal/ownedoutput"
	"github.com/cloudboss/unobin/pkg/filechange"
	"github.com/cloudboss/unobin/pkg/gopackage"
)

type buildInputs struct {
	Revision string
	digests  map[string][32]byte
}

func readBuildInputs(goBin, dir string) (buildInputs, error) {
	packages, err := readBuildPackages(goBin, dir)
	if err != nil {
		return buildInputs{}, err
	}
	context, err := readBuildContext(goBin, dir)
	if err != nil {
		return buildInputs{}, err
	}
	inventory := buildInputInventory{
		digests: map[string][32]byte{}, roots: map[string]string{}, overlays: map[string]string{},
		nativeRoots: map[string]bool{},
	}
	for _, pkg := range packages {
		if pkg.Module != nil && pkg.Module.local() {
			inventory.roots[pkg.Module.Dir] = pkg.Module.Path
		}
	}
	flags, err := (gopackage.Context{GOFLAGS: context["GOFLAGS"]}).Flags()
	if err != nil {
		return buildInputs{}, err
	}
	for i, flag := range flags {
		name, value, _ := strings.Cut(strings.TrimLeft(flag, "-"), "=")
		switch name {
		case "overlay":
			if err := inventory.readOverlay(dir, value); err != nil {
				return buildInputs{}, err
			}
			flags[i] = "-overlay=<overlay>"
		case "modfile":
			flags[i] = "-modfile=<modfile>"
		}
	}
	flagBytes, err := json.Marshal(flags)
	if err != nil {
		return buildInputs{}, err
	}
	context["GOFLAGS"] = string(flagBytes)
	if work := context["GOWORK"]; work != "" && work != "off" {
		body, err := os.ReadFile(work)
		if err != nil {
			return buildInputs{}, err
		}
		inventory.add("workspace", []byte(inventory.normalize(string(body))))
		context["GOWORK"] = "<workspace>"
	}
	for key, value := range context {
		context[key] = inventory.normalize(value)
	}
	if err := inventory.addJSON("context", context); err != nil {
		return buildInputs{}, err
	}
	for _, pkg := range packages {
		if err := inventory.readPackage(pkg); err != nil {
			return buildInputs{}, err
		}
	}
	if err := inventory.readGenerated(dir); err != nil {
		return buildInputs{}, err
	}
	hash := sha256.New()
	for _, key := range slices.Sorted(maps.Keys(inventory.digests)) {
		digest := inventory.digests[key]
		_, _ = fmt.Fprintf(hash, "%s\x00", key)
		_, _ = hash.Write(digest[:])
	}
	return buildInputs{
		Revision: hex.EncodeToString(hash.Sum(nil))[:12], digests: inventory.digests,
	}, nil
}

func (inputs buildInputs) verify(goBin, dir string) error {
	after, err := readBuildInputs(goBin, dir)
	if err != nil {
		return fmt.Errorf("factory build inputs changed during compilation: %w", err)
	}
	if maps.Equal(inputs.digests, after.digests) {
		return nil
	}
	for _, key := range slices.Sorted(maps.Keys(inputs.digests)) {
		if inputs.digests[key] != after.digests[key] {
			return fmt.Errorf("factory build inputs changed during compilation: %s", key)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(after.digests)) {
		if inputs.digests[key] != after.digests[key] {
			return fmt.Errorf("factory build inputs changed during compilation: %s", key)
		}
	}
	return errors.New("factory build inputs changed during compilation")
}

func publishCheckedBinary(
	dir, name string,
	build func(string) error,
	verify func() error,
) ([]filechange.Change, error) {
	if err := validateBinaryName(name); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	staged, err := os.CreateTemp(abs, ".unobin-build-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(staged.Name()) }()
	if err := staged.Close(); err != nil {
		return nil, err
	}
	if err := build(staged.Name()); err != nil {
		return nil, err
	}
	if err := verify(); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name)
	return filechange.Observe([]string{path}, func() error { return os.Rename(staged.Name(), path) })
}

func validateBinaryName(name string) error {
	if name == "" || name == "." || !filepath.IsLocal(name) || filepath.Base(name) != name ||
		strings.ContainsAny(name, `/\:`) || strings.HasSuffix(name, ".go") {
		return fmt.Errorf("invalid factory binary name %q; use a filename distinct from source", name)
	}
	switch name {
	case "go.mod", "go.sum", "factory.assets", ownedoutput.ManifestName:
		return fmt.Errorf("factory binary name %q conflicts with a generated output", name)
	}
	return nil
}

type buildModule struct {
	Path, Version, Sum, Dir, GoMod string
	Main                           bool
	Replace                        *buildModule
}

func (module buildModule) local() bool {
	return module.Main || module.Replace != nil && module.Replace.Version == ""
}

type buildPackage struct {
	Dir, ImportPath, Name, DefaultGODEBUG string
	Module                                *buildModule
	Imports                               []string
	ImportMap                             map[string]string
	GoFiles, CgoFiles, EmbedFiles         []string
	CFiles, CXXFiles, MFiles, HFiles      []string
	FFiles, SFiles, SwigFiles             []string
	SwigCXXFiles, SysoFiles               []string
}

func readBuildPackages(goBin, dir string) ([]buildPackage, error) {
	command := exec.Command(goBin, "list", "-buildvcs=false", "-deps", "-json", ".")
	command.Dir = dir
	output, err := command.Output()
	if err != nil {
		var failure *exec.ExitError
		if errors.As(err, &failure) {
			return nil, fmt.Errorf("inspect Go build graph: %w: %s", err, failure.Stderr)
		}
		return nil, fmt.Errorf("inspect Go build graph: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	var packages []buildPackage
	for {
		var pkg buildPackage
		if err := decoder.Decode(&pkg); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode Go build graph: %w", err)
		}
		packages = append(packages, pkg)
	}
	if len(packages) == 0 {
		return nil, errors.New("inspect Go build graph: no packages selected")
	}
	return packages, nil
}

func readBuildContext(goBin, dir string) (map[string]string, error) {
	args := []string{
		"env", "-json", "GOVERSION", "GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS", "GOEXPERIMENT",
		"GO386", "GOAMD64", "GOARM", "GOARM64", "GOMIPS", "GOMIPS64", "GOPPC64", "GORISCV64",
		"GOWASM", "GOWORK", "CC", "CXX", "FC", "AR", "PKG_CONFIG", "CGO_CFLAGS", "CGO_CPPFLAGS",
		"CGO_CXXFLAGS", "CGO_FFLAGS", "CGO_LDFLAGS",
	}
	command := exec.Command(goBin, args...)
	command.Dir = dir
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("inspect Go build context: %w", err)
	}
	var context map[string]string
	if err := json.Unmarshal(output, &context); err != nil {
		return nil, fmt.Errorf("decode Go build context: %w", err)
	}
	return context, nil
}

type buildInputInventory struct {
	digests     map[string][32]byte
	roots       map[string]string
	overlays    map[string]string
	nativeRoots map[string]bool
}

func (inventory *buildInputInventory) add(key string, body []byte) {
	inventory.digests[key] = sha256.Sum256(body)
}

func (inventory *buildInputInventory) addJSON(key string, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	inventory.add(key, body)
	return nil
}

func (inventory *buildInputInventory) normalize(value string) string {
	roots := slices.Sorted(maps.Keys(inventory.roots))
	slices.SortFunc(roots, func(a, b string) int { return len(b) - len(a) })
	for _, root := range roots {
		value = strings.ReplaceAll(value, root, "<"+inventory.roots[root]+">")
	}
	return value
}

func (inventory *buildInputInventory) readFile(key, path string) error {
	if replacement, exists := inventory.overlays[path]; exists {
		if replacement == "" {
			return nil
		}
		path = replacement
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read build input %s: %w", key, err)
	}
	if filepath.Base(path) == "go.mod" || strings.HasSuffix(key, "/go.mod") {
		module, err := modfile.Parse(path, body, nil)
		if err != nil {
			return fmt.Errorf("read build module %s: %w", key, err)
		}
		for _, replace := range module.Replace {
			if replace.New.Version == "" {
				err := module.AddReplace(replace.Old.Path, replace.Old.Version,
					"./local/"+replace.Old.Path, "")
				if err != nil {
					return err
				}
			}
		}
		body, err = module.Format()
		if err != nil {
			return err
		}
	}
	inventory.add(key, body)
	return nil
}

func (inventory *buildInputInventory) readOverlay(dir, path string) error {
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read Go overlay: %w", err)
	}
	var overlay struct{ Replace map[string]string }
	if err := json.Unmarshal(body, &overlay); err != nil {
		return fmt.Errorf("read Go overlay: %w", err)
	}
	for source, target := range overlay.Replace {
		if !filepath.IsAbs(source) {
			source = filepath.Join(dir, source)
		}
		if target != "" && !filepath.IsAbs(target) {
			target = filepath.Join(dir, target)
		}
		if target != "" {
			target = filepath.Clean(target)
		}
		inventory.overlays[filepath.Clean(source)] = target
	}
	return nil
}

func (inventory *buildInputInventory) readPackage(pkg buildPackage) error {
	moduleID := "stdlib"
	if pkg.Module != nil {
		module := *pkg.Module
		moduleID = module.Path
		module.Dir, module.GoMod = "", ""
		if module.Replace != nil {
			replacement := *module.Replace
			replacement.Dir, replacement.GoMod = "", ""
			if replacement.Version == "" {
				replacement.Path = "<local>"
			}
			module.Replace = &replacement
		}
		if err := inventory.addJSON("module:"+moduleID, module); err != nil {
			return err
		}
		if pkg.Module.local() {
			if err := inventory.readFile("module:"+moduleID+"/go.mod", pkg.Module.GoMod); err != nil {
				return err
			}
		}
	}
	selection := struct {
		Name, Module, DefaultGODEBUG string
		Imports                      []string
		ImportMap                    map[string]string
	}{pkg.Name, moduleID, pkg.DefaultGODEBUG, pkg.Imports, pkg.ImportMap}
	if err := inventory.addJSON("package:"+pkg.ImportPath, selection); err != nil {
		return err
	}
	if pkg.Module == nil || !pkg.Module.local() {
		return nil
	}
	files := slices.Concat(pkg.GoFiles, pkg.CgoFiles, pkg.EmbedFiles, pkg.CFiles, pkg.CXXFiles,
		pkg.MFiles, pkg.HFiles, pkg.FFiles, pkg.SFiles, pkg.SwigFiles, pkg.SwigCXXFiles, pkg.SysoFiles)
	if len(pkg.CgoFiles)+len(pkg.SwigFiles)+len(pkg.SwigCXXFiles) > 0 {
		if err := inventory.readNativeFiles(pkg); err != nil {
			return err
		}
	}
	for _, name := range files {
		key := "package:" + pkg.ImportPath + "/" + filepath.ToSlash(name)
		if err := inventory.readFile(key, filepath.Join(pkg.Dir, name)); err != nil {
			return err
		}
	}
	return nil
}

func (inventory *buildInputInventory) readNativeFiles(pkg buildPackage) error {
	root := pkg.Module.Dir
	if inventory.nativeRoots[root] {
		return nil
	}
	inventory.nativeRoots[root] = true
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".h", ".hh", ".hpp", ".hxx", ".inc", ".inl", ".a", ".o", ".so", ".dylib", ".lib":
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			key := "native:" + pkg.Module.Path + "/" + filepath.ToSlash(rel)
			return inventory.readFile(key, path)
		}
		return nil
	})
}

func (inventory *buildInputInventory) readGenerated(dir string) error {
	return filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		name := entry.Name()
		if name != "go.mod" && name != "go.sum" && name != "factory.assets" &&
			!strings.HasSuffix(name, ".go") {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		return inventory.readFile("factory/"+filepath.ToSlash(rel), path)
	})
}
