package compile

import (
	"github.com/cloudboss/unobin/pkg/goschema"
	ubruntime "github.com/cloudboss/unobin/pkg/runtime"
)

// SchemaCache memoizes Go library schema reads by source path. The
// module roots are fixed for a compile run, so one cache serves every
// import site and a library imported at the factory root and by
// composite bodies is parsed once.
type SchemaCache struct {
	read    func(sourcePath string) (*ubruntime.LibrarySchema, []string, error)
	entries map[string]schemaCacheEntry
}

type schemaCacheEntry struct {
	schema   *ubruntime.LibrarySchema
	warnings []string
}

// NewSchemaCache returns a cache that reads through ReadGoSchema with
// extra as the module roots for every lookup.
func NewSchemaCache(extra ...goschema.ModuleRoot) *SchemaCache {
	return &SchemaCache{
		read: func(sourcePath string) (*ubruntime.LibrarySchema, []string, error) {
			return ReadGoSchema(sourcePath, extra...)
		},
		entries: map[string]schemaCacheEntry{},
	}
}

// Read returns the schema and warnings for the Go library source at
// sourcePath, reading it on first use and replaying the stored result
// after. A failed read is not stored, so the error reaches every site
// that asks.
func (c *SchemaCache) Read(sourcePath string) (*ubruntime.LibrarySchema, []string, error) {
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
