package sourcecheck

import (
	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// SchemaCache memoizes Go library schema reads by source path.
type SchemaCache struct {
	read    func(sourcePath string) (*runtime.LibrarySchema, []string, error)
	entries map[string]schemaCacheEntry
}

type schemaCacheEntry struct {
	schema   *runtime.LibrarySchema
	warnings []string
}

// NewSchemaCache returns a cache that reads Go source with extra module roots.
func NewSchemaCache(extra ...goschema.ModuleRoot) *SchemaCache {
	return NewSchemaCacheWithReader(func(sourcePath string) (*runtime.LibrarySchema, []string, error) {
		return readGoSchema(sourcePath, extra...)
	})
}

// NewSchemaCacheWithReader returns a cache that reads Go schemas through read.
func NewSchemaCacheWithReader(
	read func(sourcePath string) (*runtime.LibrarySchema, []string, error),
) *SchemaCache {
	return &SchemaCache{
		read:    read,
		entries: map[string]schemaCacheEntry{},
	}
}

// Read returns the schema and warnings for sourcePath, reading it once.
func (c *SchemaCache) Read(sourcePath string) (*runtime.LibrarySchema, []string, error) {
	if sourcePath == "" {
		return nil, nil, nil
	}
	if e, ok := c.entries[sourcePath]; ok {
		return e.schema, e.warnings, nil
	}
	schema, warnings, err := c.read(sourcePath)
	if err != nil {
		return nil, nil, err
	}
	c.entries[sourcePath] = schemaCacheEntry{schema: schema, warnings: warnings}
	return schema, warnings, nil
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
