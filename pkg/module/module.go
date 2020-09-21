package module

import (
	"fmt"
	"strings"

	"github.com/cloudboss/unobin/pkg/types"
)

type ModuleImport struct {
	// Alias is the name given to an import in a playbook's imports map.
	// It is the key in the map for an import, and the name that playbook
	// tasks use to refer to a module.
	Alias string
	// GoImportPath is a valid Go import for the package containing a module.
	GoImportPath string
	// QualifiedIdentifier is the dot-separated package and module name.
	QualifiedIdentifier string
}

// NewModuleImport returns a `*ModuleImport` for the given alias and import path.
// The import path is the import path as used in a playbook and is not a valid Go
// import path, as it has the module appended as a suffix.
func NewModuleImport(alias, importPath string) (*ModuleImport, error) {
	qi := qualifiedIdentifier(importPath)
	parts := strings.Split(qi, ".")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid import path %s", importPath)
	}
	parts = strings.Split(importPath, ".")
	ml := &ModuleImport{
		Alias:               alias,
		GoImportPath:        strings.Join(parts[0:len(parts)-1], "."),
		QualifiedIdentifier: qi,
	}
	return ml, nil
}

// qualifiedIdentifier takes a playbook import path for a module and returns the qualified
// identifier for the module at that path. The qualified identifier is a dot-separated package
// and module name. For example, "github.com/cloudboss/unobin/modules/command.Command"
// results in "command.Command". An empty importPath will result in an empty string.
func qualifiedIdentifier(importPath string) string {
	if importPath == "" {
		return ""
	}
	parts := strings.Split(importPath, "/")
	if len(parts) == 1 {
		return parts[0]
	} else {
		return parts[len(parts)-1]
	}
}

type Module interface {
	Initialize() error
	Name() string
	Build() *types.Result
	Destroy() *types.Result
}
