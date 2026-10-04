package sourcecheck

import (
	"fmt"
	"path/filepath"

	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// SchemaCache memoizes Go library schema reads by source path.
type SchemaCache struct {
	compatibility     *golibrary.CompatibilityContext
	compatibilityErr  error
	read              func(sourcePath string) (*runtime.LibrarySchema, []string, error)
	readConfiguration func(sourcePath string) (*runtime.LibrarySchema, []string, error)
	entries           map[string]schemaCacheEntry
}

type schemaCacheEntry struct {
	snapshot              [32]byte
	schema                *runtime.LibrarySchema
	warnings              []string
	configurationSchema   *runtime.LibrarySchema
	configurationWarnings []string
}

func NewSchemaCacheWithCompatibility(
	compatibility *golibrary.CompatibilityContext, extra ...goschema.ModuleRoot,
) *SchemaCache {
	return NewSchemaCacheWithReadersAndCompatibility(
		compatibility,
		func(sourcePath string) (*runtime.LibrarySchema, []string, error) {
			return readGoSchema(sourcePath, extra...)
		},
		func(sourcePath string) (*runtime.LibrarySchema, []string, error) {
			return readGoConfigurationSchema(sourcePath, extra...)
		},
		extra...,
	)
}

func NewSchemaCacheWithReadersAndCompatibility(
	compatibility *golibrary.CompatibilityContext,
	read func(string) (*runtime.LibrarySchema, []string, error),
	readConfiguration func(string) (*runtime.LibrarySchema, []string, error),
	extra ...goschema.ModuleRoot,
) *SchemaCache {
	var err error
	if compatibility == nil {
		modules := make([]golibrary.ModuleSource, 0, len(extra))
		for _, root := range extra {
			modules = append(modules, golibrary.ModuleSource{Path: root.Path, Dir: root.Dir})
		}
		compatibility, err = golibrary.NewCompatibilityContext(golibrary.CompatibilityOptions{
			Modules: modules,
		})
	}
	return &SchemaCache{
		compatibility: compatibility, compatibilityErr: err,
		read: read, readConfiguration: readConfiguration,
		entries: map[string]schemaCacheEntry{},
	}
}

func (c *SchemaCache) CompatibilityContext() *golibrary.CompatibilityContext {
	return c.compatibility
}

func (c *SchemaCache) prepareSource(sourcePath string, linked bool) (string, error) {
	if c.compatibilityErr != nil {
		return "", c.compatibilityErr
	}
	abs, err := filepath.Abs(sourcePath)
	if err != nil {
		return "", err
	}
	if err := c.compatibility.CheckDirectory(abs, linked); err != nil {
		delete(c.entries, abs)
		return "", err
	}
	snapshot, err := c.compatibility.SourceSnapshot(abs)
	if err != nil {
		delete(c.entries, abs)
		return "", err
	}
	if entry := c.entries[abs]; entry.snapshot != snapshot {
		c.entries[abs] = schemaCacheEntry{snapshot: snapshot}
	}
	return abs, nil
}

// NewSchemaCache returns a cache that reads Go source with extra module roots.
func NewSchemaCache(extra ...goschema.ModuleRoot) *SchemaCache {
	return NewSchemaCacheWithCompatibility(nil, extra...)
}

// NewSchemaCacheWithReader returns a cache that reads Go schemas through read.
func NewSchemaCacheWithReader(
	read func(sourcePath string) (*runtime.LibrarySchema, []string, error),
) *SchemaCache {
	return NewSchemaCacheWithReaders(read, read)
}

// NewSchemaCacheWithReaders returns a cache with separate readers for full
// libraries and config-schema packages.
func NewSchemaCacheWithReaders(
	read func(sourcePath string) (*runtime.LibrarySchema, []string, error),
	readConfiguration func(sourcePath string) (*runtime.LibrarySchema, []string, error),
	extra ...goschema.ModuleRoot,
) *SchemaCache {
	return NewSchemaCacheWithReadersAndCompatibility(nil, read, readConfiguration, extra...)
}

// Read returns the schema and warnings for the current source contents.
func (c *SchemaCache) Read(sourcePath string) (*runtime.LibrarySchema, []string, error) {
	if sourcePath == "" {
		return nil, nil, nil
	}
	key, err := c.prepareSource(sourcePath, true)
	if err != nil {
		return nil, nil, err
	}
	if e, ok := c.entries[key]; ok && e.schema != nil {
		return e.schema, e.warnings, nil
	}
	snapshot := c.entries[key].snapshot
	schema, warnings, err := c.read(sourcePath)
	if err != nil {
		return nil, nil, err
	}
	if _, err := c.prepareSource(sourcePath, true); err != nil {
		return nil, nil, err
	}
	if c.entries[key].snapshot != snapshot {
		delete(c.entries, key)
		return nil, nil, fmt.Errorf("library source changed while reading schema: %s", sourcePath)
	}
	e := c.entries[key]
	e.schema = schema
	e.warnings = warnings
	c.entries[key] = e
	return schema, warnings, nil
}

// ReadLibraryConfiguration returns the config schema for sourcePath.
func (c *SchemaCache) ReadLibraryConfiguration(
	sourcePath string,
) (*runtime.LibrarySchema, []string, error) {
	if sourcePath == "" {
		return nil, nil, nil
	}
	key, err := c.prepareSource(sourcePath, false)
	if err != nil {
		return nil, nil, err
	}
	if e, ok := c.entries[key]; ok {
		if readableLibraryConfigSchema(e.schema) {
			return e.schema, e.warnings, nil
		}
		if e.configurationSchema != nil {
			return e.configurationSchema, e.configurationWarnings, nil
		}
	}
	snapshot := c.entries[key].snapshot
	schema, warnings, err := c.readConfiguration(sourcePath)
	if err != nil {
		return nil, nil, err
	}
	if _, err := c.prepareSource(sourcePath, false); err != nil {
		return nil, nil, err
	}
	if c.entries[key].snapshot != snapshot {
		delete(c.entries, key)
		return nil, nil, fmt.Errorf("library source changed while reading schema: %s", sourcePath)
	}
	e := c.entries[key]
	e.configurationSchema = schema
	e.configurationWarnings = warnings
	c.entries[key] = e
	return schema, warnings, nil
}

func readableLibraryConfigSchema(schema *runtime.LibrarySchema) bool {
	_, ok := runtime.LibraryConfigSchemaFromLibrarySchema("", schema)
	return ok
}

func readGoSchema(
	sourcePath string,
	extra ...goschema.ModuleRoot,
) (*runtime.LibrarySchema, []string, error) {
	if sourcePath == "" {
		return nil, nil, nil
	}
	moduleRoot, err := golibrary.FindModuleRoot(sourcePath)
	if err != nil {
		return nil, nil, err
	}
	if _, err := golibrary.ValidatePackage(moduleRoot, sourcePath); err != nil {
		return nil, nil, err
	}
	return goschema.Read(sourcePath, extra...)
}

func readGoConfigurationSchema(
	sourcePath string,
	extra ...goschema.ModuleRoot,
) (*runtime.LibrarySchema, []string, error) {
	if sourcePath == "" {
		return nil, nil, nil
	}
	return goschema.ReadLibraryConfiguration(sourcePath, extra...)
}
