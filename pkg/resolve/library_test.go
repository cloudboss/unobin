package resolve

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsUBLibraryAndContainsFactorySource(t *testing.T) {
	cases := []struct {
		name             string
		fixture          string
		files            map[string]string
		isLibrary        bool
		hasFactorySource bool
	}{
		{
			name:      "source-declared resource composite",
			fixture:   "library-classification/valid/source-declared-resource",
			isLibrary: true,
		},
		{
			name:      "source-declared data composite",
			fixture:   "library-classification/valid/source-declared-data",
			isLibrary: true,
		},
		{
			name:      "source-declared action composite",
			fixture:   "library-classification/valid/source-declared-action",
			isLibrary: true,
		},
		{
			name:      "mixed source-declared composites",
			fixture:   "library-classification/valid/mixed-source-declared-composites",
			isLibrary: true,
		},
		{
			name:      "library with project",
			fixture:   "library-classification/valid/library-with-project",
			isLibrary: true,
		},
		{
			name:    "project only",
			fixture: "library-classification/valid/project-only",
		},
		{
			name: "project-lock only",
			files: map[string]string{
				"project-lock.ub": "project-lock: { version: 1 toolchain: { unobin-version: 'dev' } deps: {} }",
			},
		},
		{
			name:      "misnamed-only directory is still a library so parse can flag it",
			fixture:   "library-classification/valid/misnamed-only",
			isLibrary: true,
		},
		{
			name:      "factory file with wrong role is parsed later",
			fixture:   "library-classification/valid/factory-file-wrong-role",
			isLibrary: true,
		},
		{
			name:             "grammar-first factory directory",
			fixture:          "library-classification/valid/grammar-first-factory",
			hasFactorySource: true,
		},
		{
			name:             "factory with stray composite is still a factory",
			fixture:          "library-classification/valid/factory-with-stray-composite",
			hasFactorySource: true,
		},
		{
			name:  "go library",
			files: map[string]string{"go.mod": "module x", "lib.go": "package x"},
		},
		{
			name:  "empty directory",
			files: map[string]string{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := sourceForLibraryCase(t, c.fixture, c.files)
			require.Equal(t, c.isLibrary, IsUBLibrary(src), "IsUBLibrary")
			require.Equal(t, c.hasFactorySource, ContainsFactorySource(src),
				"ContainsFactorySource")
		})
	}
}

func sourceForLibraryCase(t *testing.T, fixture string, files map[string]string) *Source {
	t.Helper()
	if fixture != "" {
		return newUBFixtureSource(t, fixture)
	}
	return newUBSource(t, files)
}

func TestIsUBLibraryNilSource(t *testing.T) {
	require.False(t, IsUBLibrary(nil))
	require.False(t, IsUBLibrary(&Source{}))
	require.False(t, ContainsFactorySource(nil))
	require.False(t, ContainsFactorySource(&Source{}))
}

// walkOneUB resolves a single remote import pointing at src and returns
// the library the visitor recorded for it, or the walk error.
func walkOneUB(t *testing.T, src *Source) (*UBLibrary, error) {
	t.Helper()
	refs := map[string]ImportRef{
		"lib": &RemoteImport{URL: "github.com/x/y", Version: "v1"},
	}
	r := &fakeUBResolver{remotes: map[string]*Source{"github.com/x/y@v1": src}}
	v := newRecordingVisitor()
	if _, err := WalkUB(refs, r, v, nil); err != nil {
		return nil, err
	}
	return v.ubLibs["remote:github.com/x/y@v1"], nil
}

func TestWalkUBParsesSourceDeclaredKindsAndNames(t *testing.T) {
	src := newUBFixtureSource(t, "library/valid/source-declared-kinds-and-names")
	lib, err := walkOneUB(t, src)
	require.NoError(t, err)
	require.NotNil(t, lib)

	require.Contains(t, lib.SyntaxBodies["resource"], "greeting")
	require.Contains(t, lib.SyntaxBodies["data-source"], "ami")
	require.Contains(t, lib.SyntaxBodies["action"], "notify")
}

func TestIsGoLibrary(t *testing.T) {
	tests := []struct {
		name string
		src  *Source
		want bool
	}{
		{name: "go mod", src: newUBSource(t, map[string]string{"go.mod": "module x\n"}), want: true},
		{
			name: "go package",
			src:  newUBSource(t, map[string]string{"lib.go": "package lib\n"}),
			want: true,
		},
		{name: "project only", src: newUBFixtureSource(t, "library-classification/valid/project-only")},
		{name: "empty", src: newUBSource(t, map[string]string{})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, IsGoLibrary(tt.src))
		})
	}
}

func TestWalkUBErrorsForNonPackageRootImport(t *testing.T) {
	src := newUBFixtureSource(t, "library/valid/non-package-root-import")
	refs := map[string]ImportRef{
		"scratch": &RemoteImport{URL: "github.com/x/libs"},
	}
	r := &fakeUBResolver{remotes: map[string]*Source{
		"github.com/x/libs@v0.8.0": src,
	}}
	v := newRecordingVisitor()

	_, err := WalkUB(refs, r, v, map[string]string{"github.com/x/libs": "v0.8.0"})

	require.Error(t, err)
	require.Contains(t, err.Error(), "not a UB library or Go library")
	require.Empty(t, v.ubCalls)
}

func TestWalkUBParsesPackageFilesFromOwningProjectVersion(t *testing.T) {
	src := newUBFixtureSource(t, "library/valid/package-files")
	refs := map[string]ImportRef{
		"helloer": &RemoteImport{URL: "github.com/x/libs", Subdir: "ub/helloer"},
	}
	r := &fakeUBResolver{remotes: map[string]*Source{
		"github.com/x/libs//ub/helloer@v0.8.0": src,
	}}
	v := newRecordingVisitor()

	top, err := WalkUB(refs, r, v, map[string]string{"github.com/x/libs": "v0.8.0"})

	require.NoError(t, err)
	require.Len(t, top, 1)
	require.Equal(t, ResolutionUB, top[0].Kind)
	lib := v.ubLibs["remote:github.com/x/libs//ub/helloer@v0.8.0"]
	require.NotNil(t, lib)
	require.Contains(t, lib.SyntaxBodies["resource"], "hello")
}

func TestWalkUBParsesSubdirPackageLibraryFiles(t *testing.T) {
	src := newUBFixtureSource(t, "library/valid/package-files")
	refs := map[string]ImportRef{
		"helloer": &RemoteImport{URL: "github.com/x/libs", Subdir: "ub/helloer"},
	}
	r := &fakeUBResolver{remotes: map[string]*Source{
		"github.com/x/libs//ub/helloer@v0.8.0": src,
	}}
	v := newRecordingVisitor()

	top, err := WalkUB(refs, r, v, map[string]string{"github.com/x/libs//ub/helloer": "v0.8.0"})

	require.NoError(t, err)
	require.Len(t, top, 1)
	require.Equal(t, ResolutionUB, top[0].Kind)
	lib := v.ubLibs["remote:github.com/x/libs//ub/helloer@v0.8.0"]
	require.NotNil(t, lib)
	require.Contains(t, lib.SyntaxBodies["resource"], "hello")
}

func TestWalkUBSkipsPackageMetadataFiles(t *testing.T) {
	src := newUBFixtureSource(t, "library/valid/package-metadata-files")

	lib, err := walkOneUB(t, src)
	require.NoError(t, err)
	require.NotNil(t, lib)
	require.Contains(t, lib.SyntaxBodies["resource"], "greeting")
}

func TestWalkUBParsesSourceDeclaredLibraryExports(t *testing.T) {
	src := newUBFixtureSource(t, "library/valid/source-declared-library-exports")
	refs := map[string]ImportRef{
		"hello": &RemoteImport{URL: "github.com/x/hello"},
	}
	r := &fakeUBResolver{remotes: map[string]*Source{
		"github.com/x/hello@v1.0.0": src,
	}}
	versions := map[string]string{
		"github.com/x/hello":  "v1.0.0",
		"github.com/x/unobin": "v0.1.0",
	}
	v := newRecordingVisitor()

	top, err := WalkUB(refs, r, v, versions)

	require.NoError(t, err)
	require.Len(t, top, 1)
	lib := v.ubLibs["remote:github.com/x/hello@v1.0.0"]
	require.NotNil(t, lib)
	require.Contains(t, lib.SyntaxBodies["resource"], "greeting")
	require.Contains(t, lib.SyntaxBodies["data-source"], "lookup")
	body := lib.SyntaxBodies["resource"]["greeting"]
	require.Len(t, body.Resources, 1)
	require.Len(t, body.Outputs, 1)
	bodyImports := lib.BodyImports["resource"]["greeting"]
	require.Len(t, bodyImports, 1)
	require.Equal(t, "core", bodyImports[0].LocalAlias)
	require.Equal(t, ResolutionGo, bodyImports[0].Kind)
}

func TestWalkUBKeepsMultiHyphenTypeName(t *testing.T) {
	src := newUBFixtureSource(t, "library/valid/multi-hyphen-type-name")
	lib, err := walkOneUB(t, src)
	require.NoError(t, err)
	require.Contains(t, lib.SyntaxBodies["resource"], "vpc-wrapper")
}

func TestWalkUBAllowsSameExportNameAcrossKinds(t *testing.T) {
	src := newUBFixtureSource(t, "library/valid/same-export-name-across-kinds")
	lib, err := walkOneUB(t, src)
	require.NoError(t, err)
	require.Contains(t, lib.SyntaxBodies["resource"], "vpc")
	require.Contains(t, lib.SyntaxBodies["data-source"], "vpc")
	require.Contains(t, lib.SyntaxBodies["action"], "vpc")
}
