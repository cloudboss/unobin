package runtime

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
)

type resourceCatalog struct {
	resources map[string]map[string]ResourceRegistration
}

func newResourceCatalog(libraryTables ...map[string]*Library) (*resourceCatalog, error) {
	resources := make(map[string]map[string]ResourceRegistration)
	libraries := make(map[string]*Library)
	for _, table := range libraryTables {
		for _, alias := range slices.Sorted(maps.Keys(table)) {
			library := table[alias]
			if library == nil {
				return nil, fmt.Errorf("library %q is nil", alias)
			}
			binding := Binding{LibraryPath: library.LibraryPath, Export: "resource"}
			if err := binding.Validate(); err != nil {
				return nil, fmt.Errorf("library %q: %w", alias, err)
			}
			if registered, ok := libraries[library.LibraryPath]; ok {
				if registered != library {
					return nil, fmt.Errorf(
						"library path %q has multiple registrations",
						library.LibraryPath,
					)
				}
				continue
			}

			registeredResources := make(map[string]ResourceRegistration, len(library.Resources))
			for _, export := range slices.Sorted(maps.Keys(library.Resources)) {
				registration := library.Resources[export]
				binding.Export = export
				if err := binding.Validate(); err != nil {
					return nil, fmt.Errorf(
						"library %q resource %w",
						library.LibraryPath,
						err,
					)
				}
				if nilResourceRegistration(registration) {
					return nil, fmt.Errorf(
						"library %q resource %q is nil",
						library.LibraryPath,
						export,
					)
				}
				registeredResources[export] = registration
			}
			libraries[library.LibraryPath] = library
			resources[library.LibraryPath] = registeredResources
		}
	}
	return &resourceCatalog{resources: resources}, nil
}

func nilResourceRegistration(registration ResourceRegistration) bool {
	if registration == nil {
		return true
	}
	value := reflect.ValueOf(registration)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (c *resourceCatalog) resource(binding Binding) (ResourceRegistration, error) {
	if err := binding.Validate(); err != nil {
		return nil, fmt.Errorf("resource binding: %w", err)
	}
	if c == nil {
		return nil, fmt.Errorf("resource catalog is required")
	}
	resources, ok := c.resources[binding.LibraryPath]
	if !ok {
		return nil, fmt.Errorf(
			"resource library %q is unavailable",
			binding.LibraryPath,
		)
	}
	registration, ok := resources[binding.Export]
	if !ok {
		return nil, fmt.Errorf(
			"resource library %q has no export %q",
			binding.LibraryPath,
			binding.Export,
		)
	}
	return registration, nil
}
