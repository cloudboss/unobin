package resolve

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHasInternalSegment(t *testing.T) {
	tests := []struct {
		name   string
		subdir string
		want   bool
	}{
		{name: "empty", subdir: "", want: false},
		{name: "bare internal", subdir: "internal", want: true},
		{name: "internal at head", subdir: "internal/secret", want: true},
		{name: "internal at tail", subdir: "pkg/internal", want: true},
		{name: "internal in middle", subdir: "pkg/internal/util", want: true},
		{name: "internal deep", subdir: "a/b/internal/c/d", want: true},
		{name: "no internal", subdir: "pkg/util", want: false},
		{name: "internal as prefix only", subdir: "internalish", want: false},
		{name: "internal as substring of segment", subdir: "pkg/internalish/x", want: false},
		{name: "plural is not internal", subdir: "x/internals", want: false},
		{name: "case sensitive", subdir: "INTERNAL", want: false},
		{name: "real library subdir", subdir: "ub/helloer", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, hasInternalSegment(tt.subdir))
		})
	}
}

func TestCrossRepoInternal(t *testing.T) {
	tests := []struct {
		name     string
		fromRepo string
		ref      ImportRef
		wantBad  bool
	}{
		{
			name:     "factory reaches remote internal",
			fromRepo: "",
			ref:      &RemoteImport{URL: "github.com/x/y", Subdir: "internal/secret", Version: "v1"},
			wantBad:  true,
		},
		{
			name:     "other repo reaches remote internal",
			fromRepo: "github.com/a/b",
			ref:      &RemoteImport{URL: "github.com/x/y", Subdir: "internal/secret", Version: "v1"},
			wantBad:  true,
		},
		{
			name:     "same repo reaches its own internal",
			fromRepo: "github.com/x/y",
			ref:      &RemoteImport{URL: "github.com/x/y", Subdir: "internal/secret", Version: "v1"},
			wantBad:  false,
		},
		{
			name:     "remote non-internal across repos",
			fromRepo: "github.com/a/b",
			ref:      &RemoteImport{URL: "github.com/x/y", Subdir: "pkg/a", Version: "v1"},
			wantBad:  false,
		},
		{
			name:     "remote root of repo",
			fromRepo: "",
			ref:      &RemoteImport{URL: "github.com/x/y", Subdir: "", Version: "v1"},
			wantBad:  false,
		},
		{
			name:     "local internal-looking path never crosses",
			fromRepo: "",
			ref:      &LocalImport{Path: "./internal/helper"},
			wantBad:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, bad := crossRepoInternal(tt.fromRepo, tt.ref)
			require.Equal(t, tt.wantBad, bad)
			if tt.wantBad {
				require.Equal(t, tt.ref, got)
			} else {
				require.Nil(t, got)
			}
		})
	}
}

func TestWalkUBRefusesCrossRepoInternalImport(t *testing.T) {
	refs := map[string]ImportRef{
		"secret": &RemoteImport{URL: "github.com/x/y", Subdir: "internal/secret", Version: "v1"},
	}
	_, err := WalkUB(refs, &fakeUBResolver{}, newRecordingVisitor(), nil)
	require.EqualError(t, err, `import "secret": github.com/x/y//internal/secret `+
		`is internal to github.com/x/y and cannot be imported from another repository`)
}

func TestWalkUBAllowsSameRepoInternalImport(t *testing.T) {
	aSrc := newUBSource(t, map[string]string{
		"library.ub": `
widget: resource {
  description: 'widget'
  imports: { shared: 'github.com/x/y//internal/shared' }
  inputs: { x: { type: string } }
}
`,
	})
	sharedSrc := newUBSource(t, map[string]string{
		"library.ub": "shared: resource { description: 'shared' inputs: { y: { type: string } } }\n",
	})
	r := &fakeUBResolver{remotes: map[string]*Source{
		"github.com/x/y//pkg/a@v1":           aSrc,
		"github.com/x/y//internal/shared@v1": sharedSrc,
	}}
	refs := map[string]ImportRef{
		"a": &RemoteImport{URL: "github.com/x/y", Subdir: "pkg/a"},
	}
	v := newRecordingVisitor()
	_, err := WalkUB(refs, r, v, map[string]string{"github.com/x/y": "v1"})
	require.NoError(t, err)
	// The internal library must actually be walked, not skipped: a same-repo
	// importer reaches it and the walk proceeds into it. shared is recorded
	// first because pkg/a's body imports are walked before pkg/a itself.
	require.Equal(t, []string{
		"shared=remote:github.com/x/y//internal/shared@v1",
		"a=remote:github.com/x/y//pkg/a@v1",
	}, v.ubCalls)
}

func TestWalkUBRefusesInternalImportInCompositeBody(t *testing.T) {
	aSrc := newUBSource(t, map[string]string{
		"library.ub": `
widget: resource {
  description: 'widget'
  imports: { secret: 'github.com/other/z//internal/secret' }
  inputs: { x: { type: string } }
}
`,
	})
	r := &fakeUBResolver{remotes: map[string]*Source{
		"github.com/x/y//pkg/a@v1": aSrc,
	}}
	refs := map[string]ImportRef{
		"a": &RemoteImport{URL: "github.com/x/y", Subdir: "pkg/a"},
	}
	versions := map[string]string{
		"github.com/x/y":     "v1",
		"github.com/other/z": "v1",
	}
	_, err := WalkUB(refs, r, newRecordingVisitor(), versions)
	require.EqualError(t, err, `import "a": composite "widget": `+
		`import "secret": github.com/other/z//internal/secret `+
		`is internal to github.com/other/z and cannot be imported from another repository`)
}

func TestWalkUBAllowsLocalInternalPath(t *testing.T) {
	localSrc := newUBSource(t, map[string]string{
		"library.ub": "thing: resource { description: 't' inputs: { x: { type: string } } }\n",
	})
	r := &fakeUBResolver{locals: map[string]*Source{
		"./internal/helper": localSrc,
	}}
	refs := map[string]ImportRef{
		"helper": &LocalImport{Path: "./internal/helper"},
	}
	_, err := WalkUB(refs, r, newRecordingVisitor(), nil)
	require.NoError(t, err)
}
