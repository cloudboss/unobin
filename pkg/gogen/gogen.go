package gogen

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/cloudboss/unobin/internal/ownedoutput"
	"github.com/cloudboss/unobin/pkg/filechange"
)

// SchemaAdapter fetches resource and data source schemas from an external
// source (e.g. a TF provider schema). FetchConfiguration returns the
// library-level configuration schema (the provider's own config block in
// TF). A nil return means the source has no configuration to expose; the
// generated library then omits the Configuration field.
type SchemaAdapter interface {
	Name() string
	FetchResources(ctx context.Context, resources []string) ([]ResourceSchema, error)
	FetchDataSources(ctx context.Context, resources []string) ([]DataSourceSchema, error)
	FetchConfiguration(ctx context.Context) (*ConfigurationSchema, error)
}

// ConfigurationSchema describes the operator-facing library configuration.
// Fields carry primitive Go types ("string", "int64", "float64", "bool",
// "[]string", "map[string]string", "any", ...); the renderer wraps each
// in the matching cfg.* wrapper type when it emits the struct.
type ConfigurationSchema struct {
	GoName      string
	Description string
	Fields      []Field
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
	From          string
	ReplaceUnobin string // local path to github.com/cloudboss/unobin for go.mod replace
	UnobinVersion string // the CLI's own unobin version, pinned in the generated go.mod
}

// Output reports what was generated.
type Output struct {
	OutDir      string
	ModulePath  string
	Resources   int
	DataSources int
	Files       []filechange.Change
}

// Generate fetches schemas from the adapter, renders Go source files, and
// writes them to disk. Resources go into a resources/ sub-package and
// data sources into a data/ sub-package so that name collisions between
// resource and data source types cannot happen.
func Generate(ctx context.Context, adapter SchemaAdapter, in Input) (*Output, error) {
	outDir := in.OutDir
	if len(outDir) == 0 {
		outDir = "./" + adapter.Name() + "-library"
	}
	outDir = filepath.Clean(outDir)

	resources, err := adapter.FetchResources(ctx, in.Resources)
	if err != nil {
		return nil, fmt.Errorf("fetch resources: %w", err)
	}

	dataSources, err := adapter.FetchDataSources(ctx, in.Resources)
	if err != nil {
		return nil, fmt.Errorf("fetch data sources: %w", err)
	}

	configuration, err := adapter.FetchConfiguration(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch configuration: %w", err)
	}

	if len(resources) == 0 && len(dataSources) == 0 {
		return nil, fmt.Errorf("no resources or data sources found for %v", in.Resources)
	}
	output := &Output{
		OutDir:      outDir,
		ModulePath:  in.ModulePath,
		Resources:   len(resources),
		DataSources: len(dataSources),
		Files:       []filechange.Change{},
	}
	in.OutDir = outDir
	files, err := renderOutputFiles(adapter.Name(), in, resources, dataSources, configuration)
	if err != nil {
		return nil, err
	}
	output.Files, err = ownedoutput.Apply(outDir,
		ownedoutput.Generator{Name: "unobin.golibrary", Version: "1"}, files)
	composed, composeErr := filechange.Compose(output.Files)
	output.Files = composed
	if err = errors.Join(err, composeErr); err != nil {
		return generationFailure(output, err)
	}
	return output, nil
}

func generationFailure(output *Output, err error) (*Output, error) {
	if output != nil && len(output.Files) > 0 {
		return output, err
	}
	return nil, err
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
