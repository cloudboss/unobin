package resolve

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifySource(t *testing.T) {
	tests := []struct {
		name       string
		source     *Source
		wantKind   SourceKind
		wantExport bool
	}{
		{
			name:       "UB library",
			source:     newUBFixtureSource(t, "library-classification/valid/source-declared-resource"),
			wantKind:   SourceUBLibrary,
			wantExport: true,
		},
		{
			name:       "factory",
			source:     newUBFixtureSource(t, "library-classification/valid/grammar-first-factory"),
			wantKind:   SourceFactory,
			wantExport: false,
		},
		{
			name:       "factory with composite exports",
			source:     newUBFixtureSource(t, "library-classification/valid/factory-with-stray-composite"),
			wantKind:   SourceFactory,
			wantExport: true,
		},
		{
			name:     "Go library",
			source:   newUBSource(t, map[string]string{"go.mod": "module x\n"}),
			wantKind: SourceGoLibrary,
		},
		{
			name:     "invalid",
			source:   newUBSource(t, map[string]string{"README.md": "not importable"}),
			wantKind: SourceInvalid,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifySource(tt.source)
			require.Equal(t, tt.wantKind, got.Kind)
			require.Equal(t, tt.wantExport, got.HasCompositeExports)
		})
	}
}
