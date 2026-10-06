package compile

import (
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/toolchain"
)

func verifySelectedBuildModules(
	reporter diagnostic.Reporter,
	goBin, dir, expected string,
	compatibility *golibrary.CompatibilityContext,
	manifest []golibrary.PackageMetadata,
) error {
	target := toolchain.UnobinModulePath
	for _, metadata := range manifest {
		if metadata.Source.Linked {
			target = "all"
			break
		}
	}
	modules, err := readSelectedLibraryModules(goBin, dir, target)
	if err != nil {
		return err
	}
	var selected string
	for _, module := range modules {
		if module.Path != toolchain.UnobinModulePath {
			continue
		}
		selected = module.Version
		if module.Replace != nil {
			selected += " replaced"
		}
		break
	}
	notice, err := decideSelectedUnobin(selected, expected)
	if err != nil {
		return err
	}
	if notice != "" {
		diagnostic.Report(reporter, diagnostic.Diagnostic{
			Code:     "unobin.compile.selected-toolchain",
			Severity: diagnostic.SeverityInfo,
			Message:  notice,
		})
	}
	for _, metadata := range manifest {
		if metadata.Source.Linked {
			return checkSelectedLibraryModules(compatibility, manifest, modules)
		}
	}
	return nil
}
