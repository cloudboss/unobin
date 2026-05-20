package lang

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// promoteFixture loads the canonical valid type-expressions fixture and
// returns its top-level fields keyed by name. Each value is the Expr that
// PromoteType is exercised against.
func promoteFixture(t *testing.T) map[string]Expr {
	t.Helper()
	f := loadFixture(t, "parse/testdata/valid/type-exprs.ub")
	out := make(map[string]Expr, len(f.Body.Fields))
	for _, fld := range f.Body.Fields {
		require.Equal(t, FieldIdent, fld.Key.Kind)
		out[fld.Key.Name] = fld.Value
	}
	return out
}

func mustPromote(t *testing.T, e Expr) TypeExpr {
	t.Helper()
	te, err := PromoteType(e)
	require.NoError(t, err)
	require.NotNil(t, te)
	return te
}

func TestPromoteAtomic(t *testing.T) {
	fields := promoteFixture(t)
	wants := []struct{ key, name string }{
		{"atomic-string", "string"},
		{"atomic-number", "number"},
		{"atomic-integer", "integer"},
		{"atomic-boolean", "boolean"},
		{"atomic-null", "null"},
		{"atomic-any", "any"},
	}
	for _, w := range wants {
		t.Run(w.key, func(t *testing.T) {
			te := mustPromote(t, fields[w.key])
			a, ok := te.(*TypeAtomic)
			require.True(t, ok, "got %T", te)
			require.Equal(t, w.name, a.Name)
			require.NotZero(t, a.S.Start.Line, "span should be populated")
		})
	}
}

func TestPromoteContainers(t *testing.T) {
	fields := promoteFixture(t)

	listStrings, ok := mustPromote(t, fields["list-strings"]).(*TypeList)
	require.True(t, ok)
	require.Equal(t, "string", listStrings.Elem.(*TypeAtomic).Name)

	setStrings, ok := mustPromote(t, fields["set-strings"]).(*TypeSet)
	require.True(t, ok)
	require.Equal(t, "string", setStrings.Elem.(*TypeAtomic).Name)

	mapStrings, ok := mustPromote(t, fields["map-strings"]).(*TypeMap)
	require.True(t, ok)
	require.Equal(t, "string", mapStrings.Elem.(*TypeAtomic).Name)

	nested, ok := mustPromote(t, fields["nested-list-of-list"]).(*TypeList)
	require.True(t, ok)
	inner, ok := nested.Elem.(*TypeList)
	require.True(t, ok)
	require.Equal(t, "integer", inner.Elem.(*TypeAtomic).Name)

	listOfMap, ok := mustPromote(t, fields["list-of-map"]).(*TypeList)
	require.True(t, ok)
	innerMap, ok := listOfMap.Elem.(*TypeMap)
	require.True(t, ok)
	require.Equal(t, "string", innerMap.Elem.(*TypeAtomic).Name)
}

func TestPromoteTuple(t *testing.T) {
	fields := promoteFixture(t)

	mixed, ok := mustPromote(t, fields["tuple-mixed"]).(*TypeTuple)
	require.True(t, ok)
	require.Len(t, mixed.Elements, 3)
	require.Equal(t, "string", mixed.Elements[0].(*TypeAtomic).Name)
	require.Equal(t, "integer", mixed.Elements[1].(*TypeAtomic).Name)
	require.Equal(t, "boolean", mixed.Elements[2].(*TypeAtomic).Name)

	empty, ok := mustPromote(t, fields["tuple-empty"]).(*TypeTuple)
	require.True(t, ok)
	require.Empty(t, empty.Elements)
}

func TestPromoteObject(t *testing.T) {
	fields := promoteFixture(t)

	simple, ok := mustPromote(t, fields["simple-object"]).(*TypeObject)
	require.True(t, ok)
	require.Len(t, simple.Fields, 2)
	require.Equal(t, "name", simple.Fields[0].Name)
	require.Equal(t, "string", simple.Fields[0].Type.(*TypeAtomic).Name)
	require.Nil(t, simple.Fields[0].Decl)
	require.Equal(t, "count", simple.Fields[1].Name)
	require.Equal(t, "integer", simple.Fields[1].Type.(*TypeAtomic).Name)

	nested, ok := mustPromote(t, fields["object-of-object"]).(*TypeObject)
	require.True(t, ok)
	require.Len(t, nested.Fields, 1)
	inner, ok := nested.Fields[0].Type.(*TypeObject)
	require.True(t, ok)
	require.Equal(t, "flag", inner.Fields[0].Name)
	require.Equal(t, "boolean", inner.Fields[0].Type.(*TypeAtomic).Name)
}

func TestPromoteObjectWithDecl(t *testing.T) {
	fields := promoteFixture(t)

	obj, ok := mustPromote(t, fields["object-with-decl"]).(*TypeObject)
	require.True(t, ok)
	require.Len(t, obj.Fields, 2)

	name := obj.Fields[0]
	require.Equal(t, "name", name.Name)
	require.Nil(t, name.Type, "Decl path: Type must be nil")
	require.NotNil(t, name.Decl, "Decl path: Decl must be set")
	// Decl carries the unprocessed input declaration; downstream validation
	// promotes its `type:` value when it walks Decl.
	require.Equal(t, "type", name.Decl.Fields[0].Key.Name)
	require.Equal(t, "string", name.Decl.Fields[0].Value.(*Ident).Name)
	require.Equal(t, "pattern", name.Decl.Fields[1].Key.Name)

	size := obj.Fields[1]
	require.Equal(t, "size", size.Name)
	require.Nil(t, size.Type)
	require.NotNil(t, size.Decl)
	// `type: optional(integer, 3)` inside the Decl is left as a Call - the
	// schema validator promotes it later.
	require.Equal(t, "type", size.Decl.Fields[0].Key.Name)
	require.IsType(t, &Call{}, size.Decl.Fields[0].Value)
}

func TestPromoteOptional(t *testing.T) {
	fields := promoteFixture(t)

	bare, ok := mustPromote(t, fields["optional-bare"]).(*TypeOptional)
	require.True(t, ok)
	require.Equal(t, "string", bare.Elem.(*TypeAtomic).Name)
	require.Nil(t, bare.Default)

	withInt, ok := mustPromote(t, fields["optional-with-default"]).(*TypeOptional)
	require.True(t, ok)
	require.Equal(t, "integer", withInt.Elem.(*TypeAtomic).Name)
	require.Equal(t, int64(3), withInt.Default.(*NumberLit).ParsedInt)

	withList, ok := mustPromote(t, fields["optional-list"]).(*TypeOptional)
	require.True(t, ok)
	innerList, ok := withList.Elem.(*TypeList)
	require.True(t, ok)
	require.Equal(t, "string", innerList.Elem.(*TypeAtomic).Name)
	require.IsType(t, &ArrayLit{}, withList.Default)

	withMap, ok := mustPromote(t, fields["optional-map"]).(*TypeOptional)
	require.True(t, ok)
	require.IsType(t, &TypeMap{}, withMap.Elem)
	require.IsType(t, &ObjectLit{}, withMap.Default)
}

func TestPromoteDeepNested(t *testing.T) {
	fields := promoteFixture(t)

	deep, ok := mustPromote(t, fields["deep"]).(*TypeOptional)
	require.True(t, ok)
	require.IsType(t, &ArrayLit{}, deep.Default)

	list, ok := deep.Elem.(*TypeList)
	require.True(t, ok)
	obj, ok := list.Elem.(*TypeObject)
	require.True(t, ok)
	require.Len(t, obj.Fields, 5)

	wantNames := []string{"from-port", "to-port", "protocol", "cidr-blocks", "description"}
	for i, n := range wantNames {
		require.Equal(t, n, obj.Fields[i].Name)
	}
	require.Equal(t, "integer", obj.Fields[0].Type.(*TypeAtomic).Name)
	require.Equal(t, "string", obj.Fields[2].Type.(*TypeAtomic).Name)

	cidrs, ok := obj.Fields[3].Type.(*TypeList)
	require.True(t, ok)
	require.Equal(t, "string", cidrs.Elem.(*TypeAtomic).Name)

	desc, ok := obj.Fields[4].Type.(*TypeOptional)
	require.True(t, ok)
	require.Equal(t, "string", desc.Elem.(*TypeAtomic).Name)
	require.Nil(t, desc.Default)
}

func TestPromoteInvalidFixtures(t *testing.T) {
	matches, err := filepath.Glob("testdata/types/invalid/*.ub")
	require.NoError(t, err)
	require.NotEmpty(t, matches)

	for _, path := range matches {
		t.Run(filepath.Base(path), func(t *testing.T) {
			b, err := os.ReadFile(path)
			require.NoError(t, err)
			f, err := ParseSource(path, b)
			require.NoError(t, err, "fixture should parse cleanly: %s", path)
			require.Len(t, f.Body.Fields, 1, "fixture should have exactly one top-level field")

			_, err = PromoteType(f.Body.Fields[0].Value)
			require.Error(t, err, "PromoteType should reject %s", path)

			var pe *Error
			require.True(t, errors.As(err, &pe), "error should be *lang.Error: %v", err)
			require.Equal(t, ErrType, pe.Kind, "error kind should be ErrType")
			require.NotZero(t, pe.Pos.Line, "error should carry a source position")
		})
	}
}
