package project

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/resolve"
)

type fakeResolver struct {
	local   *resolve.LocalResolver
	remotes map[string]*resolve.Source
}

func (r *fakeResolver) Resolve(ref resolve.ImportRef) (*resolve.Source, error) {
	if li, ok := ref.(*resolve.LocalImport); ok {
		return r.local.Resolve(li)
	}
	ri, ok := ref.(*resolve.RemoteImport)
	if !ok {
		return nil, fmt.Errorf("fake resolver: unsupported ref type %T", ref)
	}
	if src, found := r.remotes[remoteSourceKey(ri.URL, ri.Subdir, ri.Version)]; found {
		return sourceWithFS(src), nil
	}
	if ri.Subdir != "" {
		prefix := ri.Subdir + "/"
		version, ok := strings.CutPrefix(ri.Version, prefix)
		if ok {
			if src, found := r.remotes[remoteSourceKey(ri.URL, ri.Subdir, version)]; found {
				return sourceWithFS(src), nil
			}
		}
	}
	return nil, fmt.Errorf("fake resolver: no source for %s", remoteSourceKey(
		ri.URL, ri.Subdir, ri.Version))
}

func sourceWithFS(src *resolve.Source) *resolve.Source {
	if src == nil || src.FS != nil || src.Path == "" {
		return src
	}
	clone := *src
	clone.FS = os.DirFS(src.Path)
	return &clone
}

func remoteSourceKey(url, subdir, version string) string {
	key := url + "@" + version
	if subdir != "" {
		key = url + "//" + subdir + "@" + version
	}
	return key
}

func stubCompileResolver(t *testing.T, remotes map[string]*resolve.Source) {
	t.Helper()
	prev := newCompileResolver
	newCompileResolver = func(stackDir string) (resolve.Resolver, error) {
		return &fakeResolver{
			local:   resolve.NewLocalResolver(stackDir),
			remotes: remotes,
		}, nil
	}
	t.Cleanup(func() { newCompileResolver = prev })
}

var newCompileResolver = compile.NewProjectResolver
var depsListTags func(string) ([]string, error)

func SetDepsListTagsForTest(list func(string) ([]string, error)) func() {
	previous := depsListTags
	depsListTags = list
	return func() { depsListTags = previous }
}

func testOptions(options Options) Options {
	options.UnobinVersion = "v0.1.0"
	options.NewResolver = newCompileResolver
	options.ListTags = depsListTags
	return options
}

func getDependency(options *Options, arg string, output io.Writer,
	announce func(deps.Dependency, string),
) (*DependencyResult, error) {
	configured := testOptions(*options)
	configured.ToolOutput = output
	configured.Progress = func(progress DependencyProgress) {
		if progress.Err == nil && announce != nil {
			announce(progress.Dependency, progress.SelectedVersion)
		}
	}
	return UpdateDependency(configured, arg)
}

func syncDependencies(options *Options, output io.Writer) (*DependencyWriteResult, error) {
	configured := testOptions(*options)
	configured.ToolOutput = output
	return SyncDependencies(configured)
}
