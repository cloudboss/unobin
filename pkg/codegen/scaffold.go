package codegen

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/filechange"
	"github.com/cloudboss/unobin/pkg/lang"
)

type ScaffoldInput struct {
	OutDir        string
	TypeName      string
	Force         bool
	UnobinVersion string
}

type ScaffoldOutput struct {
	OutDir string
	Files  []filechange.Change
}

//go:embed templates/factory.ub.tmpl
var factoryStub string

//go:embed templates/composite.ub.tmpl
var compositeStub string

func ScaffoldFactory(input ScaffoldInput) (*ScaffoldOutput, error) {
	if input.OutDir == "" {
		return nil, fmt.Errorf("--output must not be empty")
	}
	outDir := filepath.Clean(input.OutDir)

	if _, err := os.Stat(outDir); err == nil {
		if !input.Force {
			return nil, fmt.Errorf(
				"output directory %q already exists; pass --force to overwrite", outDir,
			)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	output := &ScaffoldOutput{OutDir: outDir, Files: []filechange.Change{}}

	factoryPath := filepath.Join(outDir, "factory.ub")
	factorySource, err := lang.Canonicalize(factoryPath, []byte(factoryStub))
	if err != nil {
		return nil, err
	}
	factoryChange, err := filechange.WriteFile(factoryPath, factorySource, 0o644)
	if err != nil {
		return nil, err
	}
	output.Files = append(output.Files, factoryChange)

	project := &deps.Project{}
	if v := input.UnobinVersion; semver.IsValid(v) {
		project.UnobinVersion = v
	}
	projectPath := filepath.Join(outDir, deps.ProjectFileName)
	projectChange, err := deps.WriteProjectChange(projectPath, project)
	if err != nil {
		return partialFailure(output, err)
	}
	output.Files = append(output.Files, projectChange)
	output.Files, err = filechange.Compose(output.Files)
	if err != nil {
		return output, err
	}
	return output, nil
}

func ScaffoldLibrary(input ScaffoldInput) (*ScaffoldOutput, error) {
	if input.OutDir == "" {
		return nil, fmt.Errorf("--output must not be empty")
	}
	if err := validateUblibraryTypeName(input.TypeName); err != nil {
		return nil, err
	}
	outDir := filepath.Clean(input.OutDir)

	if _, err := os.Stat(outDir); err == nil {
		if !input.Force {
			return nil, fmt.Errorf(
				"output directory %q already exists; pass --force to overwrite", outDir,
			)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}

	typePath := filepath.Join(outDir, input.TypeName+".ub")
	source, err := lang.Canonicalize(typePath, fmt.Appendf(nil, compositeStub, input.TypeName))
	if err != nil {
		return nil, err
	}
	change, err := filechange.WriteFile(typePath, source, 0o644)
	if err != nil {
		return nil, err
	}
	return &ScaffoldOutput{
		OutDir: outDir,
		Files:  []filechange.Change{change},
	}, nil
}

func validateUblibraryTypeName(name string) error {
	if name == "" {
		return fmt.Errorf("--type must not be empty")
	}
	if strings.ContainsAny(name, `/\\`) {
		return fmt.Errorf("--type must be a file name, got %q", name)
	}
	switch name {
	case "factory", "main", "project", "project-lock":
		return fmt.Errorf("--type %q is reserved; choose another type name", name)
	}
	return nil
}

func partialFailure(output *ScaffoldOutput, err error) (*ScaffoldOutput, error) {
	if output != nil && len(output.Files) > 0 {
		return output, err
	}
	return nil, err
}
