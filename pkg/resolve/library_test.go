package resolve

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/require"
)

func TestUBKindAndType(t *testing.T) {
	cases := []struct {
		filename string
		kind     string
		typeName string
		ok       bool
	}{
		{"resource-greeting.ub", "resource", "greeting", true},
		{"data-ami.ub", "data", "ami", true},
		{"action-notify.ub", "action", "notify", true},
		{"resource-vpc-wrapper.ub", "resource", "vpc-wrapper", true},
		{"action-run-once.ub", "action", "run-once", true},
		{"greeting.ub", "", "", false},           // no prefix
		{"main.ub", "", "", false},               // no hyphen
		{"widget-foo.ub", "", "", false},         // unknown prefix
		{"resources-foo.ub", "", "", false},      // plural is not reserved
		{"Resource-foo.ub", "", "", false},       // case-sensitive
		{"resource-.ub", "", "", false},          // empty type name
		{"resource-greeting.txt", "", "", false}, // not a .ub file
		{"README.md", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.filename, func(t *testing.T) {
			kind, typeName, ok := ubKindAndType(c.filename)
			require.Equal(t, c.ok, ok)
			require.Equal(t, c.kind, kind)
			require.Equal(t, c.typeName, typeName)
		})
	}
}

func TestIsUBLibraryAndContainsMainUB(t *testing.T) {
	cases := []struct {
		name             string
		files            map[string]string
		isLibrary        bool
		hasMain          bool
		hasFactorySource bool
	}{
		{
			name:      "resource composite",
			files:     map[string]string{"resource-greeting.ub": "description: 'g'"},
			isLibrary: true,
		},
		{
			name:      "data composite",
			files:     map[string]string{"data-ami.ub": "description: 'a'"},
			isLibrary: true,
		},
		{
			name:      "action composite",
			files:     map[string]string{"action-notify.ub": "description: 'n'"},
			isLibrary: true,
		},
		{
			name: "mixed kind composites",
			files: map[string]string{
				"resource-a.ub": "description: 'a'",
				"data-b.ub":     "description: 'b'",
			},
			isLibrary: true,
		},
		{
			name:      "misnamed-only directory is still a library so parse can flag it",
			files:     map[string]string{"greeting.ub": "description: 'g'"},
			isLibrary: true,
		},
		{
			name:             "factory directory",
			files:            map[string]string{"main.ub": "description: 'f'"},
			hasMain:          true,
			hasFactorySource: true,
		},
		{
			name:             "grammar-first factory directory",
			files:            map[string]string{"factory.ub": "factory: {}"},
			hasFactorySource: true,
		},
		{
			name: "factory with stray composite is still a factory",
			files: map[string]string{
				"main.ub":       "description: 'f'",
				"resource-a.ub": "description: 'a'",
			},
			hasMain:          true,
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
			src := newUBSource(t, c.files)
			require.Equal(t, c.isLibrary, IsUBLibrary(src), "IsUBLibrary")
			require.Equal(t, c.hasMain, ContainsMainUB(src), "ContainsMainUB")
			require.Equal(t, c.hasFactorySource, ContainsFactorySource(src),
				"ContainsFactorySource")
		})
	}
}

func TestIsUBLibraryNilSource(t *testing.T) {
	require.False(t, IsUBLibrary(nil))
	require.False(t, IsUBLibrary(&Source{}))
	require.False(t, ContainsMainUB(nil))
	require.False(t, ContainsMainUB(&Source{}))
	require.False(t, ContainsFactorySource(nil))
	require.False(t, ContainsFactorySource(&Source{}))
}

// walkOneUB resolves a single remote import pointing at src and returns
// the library the visitor recorded for it, or the walk error.
func formattedBody(t *testing.T, f *lang.File) string {
	t.Helper()
	out, err := lang.Format(f)
	require.NoError(t, err)
	return string(out)
}

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

func TestWalkUBDerivesKindAndTypeFromFilenames(t *testing.T) {
	src := newUBSource(t, map[string]string{
		"resource-greeting.ub": "description: 'g'",
		"data-ami.ub":          "description: 'a'",
		"action-notify.ub":     "description: 'n'",
	})
	lib, err := walkOneUB(t, src)
	require.NoError(t, err)
	require.NotNil(t, lib)

	require.Contains(t, lib.Bodies["resource"], "greeting")
	require.Contains(t, lib.Bodies["data"], "ami")
	require.Contains(t, lib.Bodies["action"], "notify")
}

func TestWalkUBParsesSourceDeclaredLibraryExports(t *testing.T) {
	src := newUBSource(t, map[string]string{
		"library.ub": `
greeting: resource {
  imports: {
    core: 'github.com/x/unobin//core'
  }
  resources: {
    file: core.file { path: '/tmp/greeting' }
  }
  outputs: {
    path: { value: resource.file.path }
  }
}

lookup: data {
  outputs: {
    id: { value: 'id-1' }
  }
}
`,
	})
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
	require.Contains(t, lib.Bodies["resource"], "greeting")
	require.Contains(t, lib.Bodies["data"], "lookup")
	body := lib.Bodies["resource"]["greeting"]
	require.Contains(t, formattedBody(t, body), "resource.core.file.file.path")
	bodyImports := lib.BodyImports["resource"]["greeting"]
	require.Len(t, bodyImports, 1)
	require.Equal(t, "core", bodyImports[0].LocalAlias)
	require.Equal(t, ResolutionGo, bodyImports[0].Kind)
}

func TestWalkUBKeepsMultiHyphenTypeName(t *testing.T) {
	src := newUBSource(t, map[string]string{
		"resource-vpc-wrapper.ub": "description: 'w'",
	})
	lib, err := walkOneUB(t, src)
	require.NoError(t, err)
	require.Contains(t, lib.Bodies["resource"], "vpc-wrapper")
}

func TestWalkUBAllowsSameExportNameAcrossKinds(t *testing.T) {
	src := newUBSource(t, map[string]string{
		"library.ub": `
vpc: resource {
  outputs: { id: { value: 'managed' } }
}

vpc: data {
  outputs: { id: { value: 'existing' } }
}

vpc: action {
  outputs: { id: { value: 'ran' } }
}
`,
	})
	lib, err := walkOneUB(t, src)
	require.NoError(t, err)
	require.Contains(t, lib.Bodies["resource"], "vpc")
	require.Contains(t, lib.Bodies["data"], "vpc")
	require.Contains(t, lib.Bodies["action"], "vpc")
}

func TestWalkUBRejectsMisnamedFiles(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
	}{
		{
			name: "unknown prefix",
			files: map[string]string{
				"resource-ok.ub": "description: 'ok'",
				"widget-bad.ub":  "description: 'bad'",
			},
		},
		{
			name:  "no prefix",
			files: map[string]string{"greeting.ub": "description: 'g'"},
		},
		{
			name:  "plural prefix",
			files: map[string]string{"resources-foo.ub": "description: 'f'"},
		},
		{
			name:  "empty type name",
			files: map[string]string{"resource-.ub": "description: 'x'"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := walkOneUB(t, newUBSource(t, c.files))
			require.Error(t, err)
			require.Contains(t, err.Error(),
				"must be named <resource|data|action>-<type>.ub")
		})
	}
}

func TestWalkUBRejectsFactoryImport(t *testing.T) {
	src := newUBSource(t, map[string]string{
		"main.ub":       "description: 'a factory'",
		"resource-a.ub": "description: 'a'",
	})
	_, err := walkOneUB(t, src)
	require.Error(t, err)
	require.Contains(t, err.Error(), "a factory")
	require.Contains(t, err.Error(), "cannot be imported")
}

func TestWalkUBRejectsGrammarFirstFactoryImport(t *testing.T) {
	src := newUBSource(t, map[string]string{
		"factory.ub": "factory: {}",
	})
	_, err := walkOneUB(t, src)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be imported")
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
