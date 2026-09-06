package runtime

import "fmt"

func (c *LibraryCatalog) resource(binding Binding) (
	*resourceDefinitionRegistration,
	*resolvedConfigurationDefinition,
	error,
) {
	if c == nil {
		return nil, nil, fmt.Errorf("library catalog is required")
	}
	registration, err := c.resources.resource(binding)
	if err != nil {
		return nil, nil, fmt.Errorf("resource %q in library %q: %w",
			binding.Export, binding.LibraryPath, err)
	}
	definition := registration.resourceDefinition()
	if definition == nil {
		return nil, nil, fmt.Errorf("resource %q in library %q has no definition",
			binding.Export, binding.LibraryPath)
	}
	configuration, err := resolveLibraryConfigurationDefinition(
		binding.LibraryPath, c.libraries[binding.LibraryPath],
	)
	if err != nil {
		return nil, nil, fmt.Errorf("resource %q in library %q: %w",
			binding.Export, binding.LibraryPath, err)
	}
	return definition, &configuration, nil
}
