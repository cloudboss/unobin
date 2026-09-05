package runtime

import (
	"fmt"
	"maps"
	"slices"
)

// LibraryRegistration declares one implementation for a canonical library path.
type LibraryRegistration struct {
	LibraryPath string
	New         func() *Library
}

// LibraryCatalog owns the library instances shared by a factory's import aliases.
type LibraryCatalog struct {
	libraries map[string]*Library
	resources *resourceCatalog
}

func NewLibraryCatalog(registrations []LibraryRegistration) (*LibraryCatalog, error) {
	catalog := &LibraryCatalog{libraries: make(map[string]*Library, len(registrations))}
	for _, registration := range registrations {
		path := registration.LibraryPath
		if err := (Binding{LibraryPath: path, Export: "library"}).Validate(); err != nil {
			return nil, fmt.Errorf("library registration: %w", err)
		}
		if _, exists := catalog.libraries[path]; exists {
			return nil, fmt.Errorf("library path %q has multiple registrations", path)
		}
		if registration.New == nil {
			return nil, fmt.Errorf("library %q constructor is required", path)
		}
		catalog.libraries[path] = nil
	}
	for _, registration := range registrations {
		path := registration.LibraryPath
		library, err := guard("registering library "+path, false, func() (*Library, error) {
			return registration.New(), nil
		})
		if err != nil {
			return nil, err
		}
		if library == nil {
			return nil, fmt.Errorf("library %q is nil", path)
		}
		if library.LibraryPath != "" && library.LibraryPath != path {
			return nil, fmt.Errorf("library %q has conflicting path %q", path, library.LibraryPath)
		}
		library.LibraryPath = path
		catalog.libraries[path] = library
	}
	for _, path := range slices.Sorted(maps.Keys(catalog.libraries)) {
		library := catalog.libraries[path]
		for _, composites := range []map[string]*CompositeType{
			library.ResourceComposites, library.DataComposites, library.ActionComposites,
		} {
			for _, name := range slices.Sorted(maps.Keys(composites)) {
				composite := composites[name]
				if composite == nil {
					return nil, fmt.Errorf("library %q composite %q is nil", path, name)
				}
				if composite.LibraryBindings != nil && composite.Libraries != nil {
					return nil, fmt.Errorf(
						"library %q composite %q declares two import tables", path, name,
					)
				}
				if composite.LibraryBindings != nil {
					imports, err := catalog.Libraries(composite.LibraryBindings)
					if err != nil {
						return nil, fmt.Errorf("library %q composite %q: %w", path, name, err)
					}
					composite.Libraries = imports
				}
				for _, alias := range slices.Sorted(maps.Keys(composite.Libraries)) {
					imported := composite.Libraries[alias]
					if imported == nil || catalog.libraries[imported.LibraryPath] != imported {
						return nil, fmt.Errorf(
							"library %q composite %q import %q is not in the catalog", path, name, alias,
						)
					}
				}
			}
		}
	}
	resources, err := newResourceCatalog(catalog.libraries)
	if err != nil {
		return nil, err
	}
	catalog.resources = resources
	return catalog, nil
}

func (c *LibraryCatalog) Libraries(bindings map[string]string) (map[string]*Library, error) {
	if c == nil {
		return nil, fmt.Errorf("library catalog is required")
	}
	libraries := make(map[string]*Library, len(bindings))
	for _, alias := range slices.Sorted(maps.Keys(bindings)) {
		if alias == "" {
			return nil, fmt.Errorf("library import alias is required")
		}
		path := bindings[alias]
		library := c.libraries[path]
		if library == nil {
			return nil, fmt.Errorf("import %q: library %q is unavailable", alias, path)
		}
		libraries[alias] = library
	}
	return libraries, nil
}
