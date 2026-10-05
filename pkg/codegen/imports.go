package codegen

import (
	"fmt"
	"slices"

	"github.com/cloudboss/unobin/pkg/program"
	"github.com/cloudboss/unobin/pkg/resolve"
)

type GeneratedImports struct {
	GoImports  map[string]string
	UBImports  map[string]string
	UBPackages map[string][]byte
}

func GenerateImports(imports *program.Imports, factoryName string) (*GeneratedImports, error) {
	if imports == nil {
		return nil, fmt.Errorf("codegen: import analysis is nil")
	}
	if factoryName == "" {
		factoryName = "stack"
	}
	ids := newUBPackageIDs()
	paths := make(map[string]string, len(imports.UBLibraries))
	for _, library := range imports.UBLibraries {
		if _, duplicate := paths[library.CanonicalKey]; duplicate {
			return nil, fmt.Errorf("codegen: duplicate library %s", library.CanonicalKey)
		}
		id := ids.ID(library.Name, library.CanonicalKey)
		paths[library.CanonicalKey] = factoryName + "/internal/" + id
	}
	generated := &GeneratedImports{
		GoImports: map[string]string{}, UBImports: map[string]string{},
		UBPackages: make(map[string][]byte, len(imports.UBLibraries)),
	}
	for _, imported := range imports.Top {
		switch imported.Kind {
		case resolve.ResolutionGo:
			generated.GoImports[imported.LocalAlias] = imported.Path
		case resolve.ResolutionUB:
			path, found := paths[imported.CanonicalKey]
			if !found {
				return nil, fmt.Errorf("codegen: missing library for import %q",
					imported.LocalAlias)
			}
			generated.UBImports[imported.LocalAlias] = path
		}
	}
	for _, library := range imports.UBLibraries {
		library.Composites = slices.Clone(library.Composites)
		for i := range library.Composites {
			library.Composites[i].Body.Assets = nil
		}
		id := ids.byKey[library.CanonicalKey]
		source, err := GenerateLibrary(id, library, paths)
		if err != nil {
			return nil, err
		}
		generated.UBPackages[id] = source
	}
	return generated, nil
}
