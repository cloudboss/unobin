package sourcecheck

import (
	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// SchemaCache memoizes Go library schema reads by source path.
type SchemaCache struct {
	read              func(sourcePath string) (*runtime.LibrarySchema, []string, error)
	readConfiguration func(sourcePath string) (*runtime.LibrarySchema, []string, error)
	entries           map[string]schemaCacheEntry
}

type schemaCacheEntry struct {
	schema                *runtime.LibrarySchema
	warnings              []string
	configurationSchema   *runtime.LibrarySchema
	configurationWarnings []string
}

// NewSchemaCache returns a cache that reads Go source with extra module roots.
func NewSchemaCache(extra ...goschema.ModuleRoot) *SchemaCache {
	return NewSchemaCacheWithReaders(
		func(sourcePath string) (*runtime.LibrarySchema, []string, error) {
			return readGoSchema(sourcePath, extra...)
		},
		func(sourcePath string) (*runtime.LibrarySchema, []string, error) {
			return readGoConfigurationSchema(sourcePath, extra...)
		},
	)
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
) *SchemaCache {
	return &SchemaCache{
		read:              read,
		readConfiguration: readConfiguration,
		entries:           map[string]schemaCacheEntry{},
	}
}

// Read returns the schema and warnings for sourcePath, reading it once.
func (c *SchemaCache) Read(sourcePath string) (*runtime.LibrarySchema, []string, error) {
	if sourcePath == "" {
		return nil, nil, nil
	}
	if e, ok := c.entries[sourcePath]; ok && e.schema != nil {
		return e.schema, e.warnings, nil
	}
	schema, warnings, err := c.read(sourcePath)
	if err != nil {
		return nil, nil, err
	}
	e := c.entries[sourcePath]
	e.schema = schema
	e.warnings = warnings
	c.entries[sourcePath] = e
	return schema, warnings, nil
}

// ReadLibraryConfiguration returns the config schema for sourcePath.
func (c *SchemaCache) ReadLibraryConfiguration(
	sourcePath string,
) (*runtime.LibrarySchema, []string, error) {
	if sourcePath == "" {
		return nil, nil, nil
	}
	if e, ok := c.entries[sourcePath]; ok {
		if readableLibraryConfigSchema(e.schema) {
			return e.schema, e.warnings, nil
		}
		if e.configurationSchema != nil {
			return e.configurationSchema, e.configurationWarnings, nil
		}
	}
	schema, warnings, err := c.readConfiguration(sourcePath)
	if err != nil {
		return nil, nil, err
	}
	e := c.entries[sourcePath]
	e.configurationSchema = schema
	e.configurationWarnings = warnings
	c.entries[sourcePath] = e
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
