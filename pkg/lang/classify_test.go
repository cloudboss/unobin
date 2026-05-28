package lang

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifyByFilename(t *testing.T) {
	cases := []struct {
		path string
		want FileKind
	}{
		{"stack.ub", FileStack},
		{"library.ub", FileLibrary},
		{"deep/path/stack.ub", FileStack},
		{"deep/path/library.ub", FileLibrary},
		{"cluster.ub", FileUnknown},
		{"config.ub", FileUnknown},
		{"prod.ub", FileUnknown},
		{"", FileUnknown},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			require.Equal(t, c.want, ClassifyByFilename(c.path))
		})
	}
}

func TestParseSourceSetsKind(t *testing.T) {
	cases := []struct {
		path string
		want FileKind
	}{
		{"stack.ub", FileStack},
		{"library.ub", FileLibrary},
		{"cluster.ub", FileUnknown},
		{"", FileUnknown},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			f, err := ParseSource(c.path, []byte("description: 'x'\n"))
			require.NoError(t, err)
			require.Equal(t, c.want, f.Kind)
		})
	}
}
