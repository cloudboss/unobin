package root

import (
	"path/filepath"

	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/libraryapi"
)

var libraryAPIDescriptor *libraryapi.Descriptor

func newCommandCompatibility(
	projectDir string, project *deps.Project, replacement string,
) (*golibrary.CompatibilityContext, error) {
	options := golibrary.CompatibilityOptions{
		Descriptor: libraryAPIDescriptor, UnobinVersion: cliVersion(), CoreReplacement: replacement,
		ProjectFile: filepath.Join(projectDir, deps.ProjectFileName),
	}
	if project != nil {
		options.ToolchainPin = project.UnobinVersion
	}
	return golibrary.NewCompatibilityContext(options)
}

func SetLibraryAPIDescriptorForTest(descriptor *libraryapi.Descriptor) func() {
	previous := libraryAPIDescriptor
	libraryAPIDescriptor = descriptor
	return func() { libraryAPIDescriptor = previous }
}
