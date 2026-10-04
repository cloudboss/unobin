package project

import (
	"context"
	"io"
	"path/filepath"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/git"
	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/libraryapi"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/toolchain"
)

type Options struct {
	Path                 string
	ReplaceUnobin        string
	UnobinVersion        string
	LibraryAPIDescriptor *libraryapi.Descriptor
	NewResolver          func(string) (resolve.Resolver, error)
	NewRemoteResolver    func() (*resolve.RemoteResolver, error)
	ListTags             func(string) ([]string, error)
	ToolOutput           io.Writer
	Progress             func(DependencyProgress)
}

type DependencyProgress struct {
	Dependency      deps.Dependency
	Version         string
	SelectedVersion string
	Err             error
}

func (o Options) toolOutput() io.Writer {
	if o.ToolOutput == nil {
		return io.Discard
	}
	return o.ToolOutput
}

func (o Options) listTags(url string) ([]string, error) {
	if o.ListTags != nil {
		return o.ListTags(url)
	}
	return git.ListTags(context.Background(), resolve.WithDefaultScheme(url))
}

func (o Options) Compatibility(
	projectDir string, project *deps.Project, replacement string,
) (*golibrary.CompatibilityContext, error) {
	options := golibrary.CompatibilityOptions{
		Descriptor: o.LibraryAPIDescriptor, UnobinVersion: o.UnobinVersion,
		CoreReplacement: replacement,
		ProjectFile:     filepath.Join(projectDir, deps.ProjectFileName),
	}
	if project != nil {
		options.ToolchainPin = project.UnobinVersion
	}
	return golibrary.NewCompatibilityContext(options)
}

func UnobinReplacement(
	projectDir string,
	cliReplace string,
	replace map[deps.Dependency]string,
) (string, error) {
	if cliReplace != "" {
		return filepath.Abs(cliReplace)
	}
	path, ok := replace[deps.Dependency{URL: toolchain.UnobinModulePath}]
	if !ok {
		return "", nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(projectDir, path)
	}
	return filepath.Abs(path)
}

func (o Options) resolver(
	root, replaceUnobin string, replace map[deps.Dependency]string,
) (resolve.Resolver, error) {
	factory := o.NewResolver
	if factory == nil {
		factory = compile.NewProjectResolver
	}
	resolver, err := factory(root)
	if err != nil {
		return nil, err
	}
	return compile.WrapReplaces(resolver, root, replaceUnobin, replace)
}
