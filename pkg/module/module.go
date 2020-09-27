// Copyright © 2020 Joseph Wright <joseph@cloudboss.co>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

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
	Apply() *types.Result
	Destroy() *types.Result
}
