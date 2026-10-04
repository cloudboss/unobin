package cmdconfig

import (
	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/libraryapi"
	"github.com/cloudboss/unobin/pkg/project"
	"github.com/cloudboss/unobin/pkg/resolve"
)

var CLIVersion = func() string { return "dev" }
var LibraryAPIDescriptor *libraryapi.Descriptor
var NewResolver = compile.NewProjectResolver
var NewRemoteResolver = resolve.NewRemoteResolver
var ListTags func(string) ([]string, error)

func ProjectOptions(path, replacement string) project.Options {
	return project.Options{
		Path: path, ReplaceUnobin: replacement, UnobinVersion: CLIVersion(),
		LibraryAPIDescriptor: LibraryAPIDescriptor,
		NewResolver:          NewResolver, NewRemoteResolver: NewRemoteResolver, ListTags: ListTags,
	}
}

func SetDepsListTagsForTest(listTags func(string) ([]string, error)) func() {
	previous := ListTags
	ListTags = listTags
	return func() { ListTags = previous }
}

func SetRemoteResolverForTest(factory func() (*resolve.RemoteResolver, error)) func() {
	previous := NewRemoteResolver
	NewRemoteResolver = factory
	return func() { NewRemoteResolver = previous }
}

func SetCompileResolverForTest(factory func(string) (resolve.Resolver, error)) func() {
	previous := NewResolver
	NewResolver = factory
	return func() { NewResolver = previous }
}

func SetLibraryAPIDescriptorForTest(descriptor *libraryapi.Descriptor) func() {
	previous := LibraryAPIDescriptor
	LibraryAPIDescriptor = descriptor
	return func() { LibraryAPIDescriptor = previous }
}

const DependencyPathHelp = "Path to the factory source file or project directory."
const DependencyReplacementHelp = "Local path to substitute for " +
	"github.com/cloudboss/unobin so the resolver reads from a working tree instead of fetching."
