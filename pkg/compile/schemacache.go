package compile

import (
	"github.com/cloudboss/unobin/pkg/goschema"
	ubruntime "github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sourcecheck"
)

// SchemaCache memoizes Go library schema reads by source path.
type SchemaCache = sourcecheck.SchemaCache

// NewSchemaCache returns a cache that reads through ReadGoSchema with
// extra as the module roots for every lookup.
func NewSchemaCache(extra ...goschema.ModuleRoot) *SchemaCache {
	return NewSchemaCacheWithReader(
		func(sourcePath string) (*ubruntime.LibrarySchema, []string, error) {
			return ReadGoSchema(sourcePath, extra...)
		},
	)
}

// NewSchemaCacheWithReader returns a cache that reads through read.
func NewSchemaCacheWithReader(
	read func(sourcePath string) (*ubruntime.LibrarySchema, []string, error),
) *SchemaCache {
	return sourcecheck.NewSchemaCacheWithReader(read)
}
