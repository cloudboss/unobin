package gopackage

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

type Context struct {
	Build        build.Context
	GoVersion    string
	GOFLAGS      string
	GOEXPERIMENT string
	Architecture map[string]string
}

type File struct {
	Name   string
	Source []byte
	Syntax *ast.File
	Active bool
}

type Package struct {
	Name  string
	FSet  *token.FileSet
	Files []File
}

func CurrentContext() (Context, error) {
	context := Context{
		Build: build.Default, GoVersion: runtime.Version(),
		GOFLAGS: os.Getenv("GOFLAGS"), GOEXPERIMENT: os.Getenv("GOEXPERIMENT"),
		Architecture: map[string]string{},
	}
	context.Build.GOOS = envOr("GOOS", build.Default.GOOS)
	context.Build.GOARCH = envOr("GOARCH", build.Default.GOARCH)
	context.Build.BuildTags = slices.Clone(build.Default.BuildTags)
	context.Build.ReleaseTags = slices.Clone(build.Default.ReleaseTags)
	if value := os.Getenv("CGO_ENABLED"); value != "" {
		if value != "0" && value != "1" {
			return Context{}, fmt.Errorf("invalid CGO_ENABLED %q: use 0 or 1", value)
		}
		context.Build.CgoEnabled = value == "1"
	} else if context.Build.GOOS != runtime.GOOS || context.Build.GOARCH != runtime.GOARCH {
		context.Build.CgoEnabled = false
	}
	flags, err := splitGoFlags(context.GOFLAGS)
	if err != nil {
		return Context{}, err
	}
	toolFlags := map[string]bool{}
	for _, flag := range flags {
		name, value, hasValue := strings.Cut(strings.TrimPrefix(flag, "-"), "=")
		name = strings.TrimPrefix(name, "-")
		switch name {
		case "tags":
			if !hasValue {
				return Context{}, fmt.Errorf("GOFLAGS: tags requires a value")
			}
			context.Build.BuildTags = strings.Fields(strings.ReplaceAll(value, ",", " "))
		case "race", "msan", "asan":
			if value == "" {
				value = "true"
			}
			enabled, err := strconv.ParseBool(value)
			if err != nil {
				return Context{}, fmt.Errorf("GOFLAGS %s: %w", name, err)
			}
			toolFlags[name] = enabled
		case "compiler":
			context.Build.Compiler = value
		}
	}
	for _, name := range []string{"race", "msan", "asan"} {
		if toolFlags[name] {
			context.Build.BuildTags = append(context.Build.BuildTags, name)
		}
	}
	for _, name := range []string{
		"GO386", "GOAMD64", "GOARM", "GOARM64", "GOMIPS", "GOMIPS64",
		"GOPPC64", "GORISCV64", "GOWASM",
	} {
		context.Architecture[name] = os.Getenv(name)
	}
	tags, err := architectureTags(context.Build.GOARCH)
	if err != nil {
		return Context{}, err
	}
	context.Build.ToolTags = append(tags, experimentTags(context)...)
	return context, nil
}

func (c Context) Digest() [32]byte {
	settings := struct {
		GoVersion    string
		GOOS         string
		GOARCH       string
		Compiler     string
		CgoEnabled   bool
		BuildTags    []string
		ToolTags     []string
		ReleaseTags  []string
		GOFLAGS      string
		GOEXPERIMENT string
		Architecture map[string]string
	}{
		c.GoVersion, c.Build.GOOS, c.Build.GOARCH, c.Build.Compiler, c.Build.CgoEnabled,
		c.Build.BuildTags, c.Build.ToolTags, c.Build.ReleaseTags, c.GOFLAGS, c.GOEXPERIMENT,
		c.Architecture,
	}
	encoded, _ := json.Marshal(settings)
	return sha256.Sum256(encoded)
}

func Load(dir string, context Context) (*Package, error) {
	pkg, err := Read(dir, context)
	if err != nil {
		return nil, err
	}
	if pkg.Name == "" {
		return nil, fmt.Errorf("no Go package found in %s", dir)
	}
	for _, file := range pkg.Files {
		if file.Active && file.Syntax.Name.Name != pkg.Name {
			return nil, fmt.Errorf("more than one Go package found in %s", dir)
		}
	}
	return pkg, nil
}

func Read(dir string, context Context) (*Package, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	pkg := &Package{FSet: token.NewFileSet()}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		active, err := context.Build.MatchFile(dir, name)
		if err != nil {
			return nil, fmt.Errorf("select %s: %w", filepath.Join(dir, name), err)
		}
		path := filepath.Join(dir, name)
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		file, err := parser.ParseFile(pkg.FSet, path, body, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		if file.Name.Name == "documentation" {
			active = false
		}
		if !context.Build.CgoEnabled {
			for _, spec := range file.Imports {
				importPath, err := strconv.Unquote(spec.Path.Value)
				if err == nil && importPath == "C" {
					active = false
				}
			}
		}
		if active && pkg.Name == "" {
			pkg.Name = file.Name.Name
		}
		pkg.Files = append(pkg.Files, File{Name: name, Source: body, Syntax: file, Active: active})
	}
	return pkg, nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func splitGoFlags(flags string) ([]string, error) {
	var out []string
	for flags = strings.TrimSpace(flags); flags != ""; flags = strings.TrimSpace(flags) {
		if quote := flags[0]; quote == '\'' || quote == '"' {
			end := strings.IndexByte(flags[1:], quote)
			if end < 0 {
				return nil, fmt.Errorf("GOFLAGS: unterminated quote")
			}
			out = append(out, flags[1:end+1])
			flags = flags[end+2:]
			continue
		}
		end := strings.IndexAny(flags, " \t\n\r")
		if end < 0 {
			out = append(out, flags)
			break
		}
		out = append(out, flags[:end])
		flags = flags[end:]
	}
	return out, nil
}

func experimentTags(context Context) []string {
	enabled := map[string]bool{}
	for _, tag := range build.Default.ToolTags {
		if strings.HasPrefix(tag, "goexperiment.") {
			enabled[tag] = true
		}
	}
	if context.Build.GOOS == "darwin" || context.Build.GOOS == "ios" ||
		context.Build.GOOS == "aix" {
		delete(enabled, "goexperiment.dwarf5")
	}
	for value := range strings.SplitSeq(context.GOEXPERIMENT, ",") {
		switch {
		case value == "none":
			clear(enabled)
		case value == "regabi":
			enabled["goexperiment.regabiargs"] = true
			enabled["goexperiment.regabiwrappers"] = true
		case value == "noregabi":
			delete(enabled, "goexperiment.regabiargs")
			delete(enabled, "goexperiment.regabiwrappers")
		case strings.HasPrefix(value, "no"):
			delete(enabled, "goexperiment."+strings.TrimPrefix(value, "no"))
		case value != "":
			enabled["goexperiment."+value] = true
		}
	}
	switch context.Build.GOARCH {
	case "amd64", "arm64", "loong64", "ppc64", "ppc64le", "riscv64":
		enabled["goexperiment.regabiargs"] = true
		enabled["goexperiment.regabiwrappers"] = true
	case "s390x":
	default:
		delete(enabled, "goexperiment.regabiargs")
		delete(enabled, "goexperiment.regabiwrappers")
	}
	var tags []string
	for tag := range enabled {
		tags = append(tags, tag)
	}
	slices.Sort(tags)
	return tags
}
