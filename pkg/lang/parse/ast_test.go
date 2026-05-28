package parse

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNodeImplementations is a compile time check that every AST type we
// expect to satisfy Node and Expr does so. It catches drift in the
// interface surface during refactors.
func TestNodeImplementations(t *testing.T) {
	var _ Node = (*File)(nil)
	var _ Node = (*Field)(nil)

	var _ Expr = (*ObjectLit)(nil)
	var _ Expr = (*ArrayLit)(nil)
	var _ Expr = (*StringLit)(nil)
	var _ Expr = (*NumberLit)(nil)
	var _ Expr = (*BoolLit)(nil)
	var _ Expr = (*NullLit)(nil)
	var _ Expr = (*Ident)(nil)
	var _ Expr = (*DotPath)(nil)
	var _ Expr = (*Call)(nil)
	var _ Expr = (*Infix)(nil)
	var _ Expr = (*Prefix)(nil)

	var _ TypeExpr = (*TypeAtomic)(nil)
	var _ TypeExpr = (*TypeList)(nil)
	var _ TypeExpr = (*TypeSet)(nil)
	var _ TypeExpr = (*TypeMap)(nil)
	var _ TypeExpr = (*TypeObject)(nil)
	var _ TypeExpr = (*TypeTuple)(nil)
	var _ TypeExpr = (*TypeOptional)(nil)

	span := Span{Start: Position{Line: 1, Column: 1}}
	n := &StringLit{S: span, Value: "hi"}
	require.Equal(t, span, n.Span())
}

func TestFieldKeyIsMeta(t *testing.T) {
	cases := []struct {
		name string
		key  FieldKey
		want bool
	}{
		{"meta key", FieldKey{Kind: FieldIdent, Name: "@for-each"}, true},
		{"plain ident", FieldKey{Kind: FieldIdent, Name: "region"}, false},
		{"string key", FieldKey{Kind: FieldString, String: "@looks-meta"}, false},
		{"empty ident", FieldKey{Kind: FieldIdent, Name: ""}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, c.key.IsMeta())
		})
	}
}

func TestFileKindString(t *testing.T) {
	cases := map[FileKind]string{
		FileUnknown:      "unknown",
		FileFactory:      "factory",
		FileLibrary:      "library",
		FileExportedType: "exported-type",
		FileConfig:       "config",
	}
	for k, want := range cases {
		require.Equal(t, want, k.String(), "kind %d", k)
	}
}
