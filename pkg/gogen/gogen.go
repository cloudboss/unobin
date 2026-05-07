package gogen

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// SchemaAdapter fetches resource and data source schemas from an external
// source (e.g. a TF provider schema).
type SchemaAdapter interface {
	Name() string
	FetchResources(ctx context.Context, resources []string) ([]ResourceSchema, error)
	FetchDataSources(ctx context.Context, resources []string) ([]DataSourceSchema, error)
}

// ResourceSchema describes one cloud resource type for code generation.
type ResourceSchema struct {
	GoName            string
	CloudType         string
	Description       string
	InputFields       []Field
	OutputFields      []Field
	CreateOnlyFields  []string
	PrimaryIdentifier []string
}

// DataSourceSchema describes one cloud data source for code generation.
type DataSourceSchema struct {
	GoName       string
	CloudType    string
	Description  string
	InputFields  []Field
	OutputFields []Field
}

// Field is one property of a resource or data source.
type Field struct {
	Name        string
	GoType      string
	Description string
	Required    bool
}

// Input configures a generation run.
type Input struct {
	Resources     []string
	OutDir        string
	ModulePath    string
	Source        string
	ReplaceUnobin string // local path to github.com/cloudboss/unobin for go.mod replace
}

// Output reports what was generated.
type Output struct {
	ModulePath  string
	Resources   int
	DataSources int
}

// Generate fetches schemas from the adapter, renders Go source files, and
// writes them to disk. Resources go into a resources/ sub-package and
// data sources into a data/ sub-package so that name collisions between
// resource and data source types cannot happen.
func Generate(ctx context.Context, adapter SchemaAdapter, in Input) (*Output, error) {
	outDir := in.OutDir
	if len(outDir) == 0 {
		outDir = "./" + adapter.Name() + "-module"
	}

	resources, err := adapter.FetchResources(ctx, in.Resources)
	if err != nil {
		return nil, fmt.Errorf("fetch resources: %w", err)
	}

	dataSources, err := adapter.FetchDataSources(ctx, in.Resources)
	if err != nil {
		return nil, fmt.Errorf("fetch data sources: %w", err)
	}

	if len(resources) == 0 && len(dataSources) == 0 {
		return nil, fmt.Errorf("no resources or data sources found for %v", in.Resources)
	}

	if err := os.MkdirAll(outDir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", outDir, err)
	}

	if len(resources) > 0 {
		resourcesDir := filepath.Join(outDir, "resources")
		if err := os.MkdirAll(resourcesDir, 0755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", resourcesDir, err)
		}
		for _, rs := range resources {
			src, err := ResourceFile(rs, in.Source)
			if err != nil {
				return nil, fmt.Errorf("render %s: %w", rs.GoName, err)
			}
			path := filepath.Join(resourcesDir, toSnake(rs.GoName)+"_rsrc.go")
			if err := os.WriteFile(path, src, 0644); err != nil {
				return nil, fmt.Errorf("write %s: %w", path, err)
			}
		}
	}

	if len(dataSources) > 0 {
		dataDir := filepath.Join(outDir, "data")
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", dataDir, err)
		}
		for _, ds := range dataSources {
			src, err := DataSourceFile(ds, in.Source)
			if err != nil {
				return nil, fmt.Errorf("render data source %s: %w", ds.GoName, err)
			}
			path := filepath.Join(dataDir, toSnake(ds.GoName)+"_dsrc.go")
			if err := os.WriteFile(path, src, 0644); err != nil {
				return nil, fmt.Errorf("write %s: %w", path, err)
			}
		}
	}

	modSrc, err := ModuleFile(adapter.Name(), resources, dataSources, in.ModulePath, in.Source)
	if err != nil {
		return nil, fmt.Errorf("render module.go: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "module.go"), modSrc, 0644); err != nil {
		return nil, fmt.Errorf("write module.go: %w", err)
	}

	goModSrc, err := GoMod(in.ModulePath, in.ReplaceUnobin)
	if err != nil {
		return nil, fmt.Errorf("render go.mod: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "go.mod"), goModSrc, 0644); err != nil {
		return nil, fmt.Errorf("write go.mod: %w", err)
	}

	return &Output{
		ModulePath:  outDir,
		Resources:   len(resources),
		DataSources: len(dataSources),
	}, nil
}

// toSnake converts a PascalCase name to snake_case.
func toSnake(s string) string {
	if len(s) == 0 {
		return s
	}
	var b []byte
	for i, c := range s {
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				prev := s[i-1]
				if prev >= 'a' && prev <= 'z' {
					b = append(b, '_')
				} else if prev >= '0' && prev <= '9' {
					b = append(b, '_')
				} else if i+1 < len(s) && s[i+1] >= 'a' && s[i+1] <= 'z' &&
					prev >= 'A' && prev <= 'Z' {
					b = append(b, '_')
				}
			}
			b = append(b, byte(c)+32)
		} else {
			b = append(b, byte(c))
		}
	}
	return string(b)
}
