package goschema

import (
	"errors"
	"fmt"
	"go/token"
	"slices"
)

func (c *analysisContext) loadConfigurationPackage(
	from *indexedPackage, alias string, pos token.Pos,
) (*indexedPackage, bool) {
	pkg, found := c.loadImportedPackage(from, alias, pos)
	if found {
		c.configurationPackages[pkg.importPath] = pkg
	}
	return pkg, found
}

func (c *analysisContext) checkForwardedConfigurations() error {
	checked := map[string]bool{c.root.importPath: true}
	var failures []error
	for {
		pending := make([]string, 0, len(c.configurationPackages))
		for importPath := range c.configurationPackages {
			if !checked[importPath] {
				pending = append(pending, importPath)
			}
		}
		if len(pending) == 0 {
			return errors.Join(failures...)
		}
		slices.Sort(pending)
		for _, importPath := range pending {
			checked[importPath] = true
			pkg := c.configurationPackages[importPath]
			fn := findPackageFunc(pkg.files, "LibraryConfiguration")
			if fn == nil {
				failures = append(failures,
					fmt.Errorf("configuration package %s has no LibraryConfiguration()",
						importPath))
				continue
			}
			context := &analysisContext{
				dir: pkg.dir, root: pkg, roots: c.roots, packages: c.packages,
				configurationPackages: c.configurationPackages,
			}
			if _, err := context.readLibraryConfigurationSchema(fn); err != nil {
				failures = append(failures,
					fmt.Errorf("configuration package %s: %w", importPath, err))
			}
			c.warnings = append(c.warnings, context.warnings...)
		}
	}
}
