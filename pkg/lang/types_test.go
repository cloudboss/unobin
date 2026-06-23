package lang

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
)

func parseTypeFixture(t *testing.T, fixture string) TypeExpr {
	t.Helper()
	path := filepath.Join("testdata", "ub", "types", "valid", fixture+".ub")
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	got, err := ParseType(path, src)
	require.NoError(t, err)
	return got
}

func requireAtomic(t *testing.T, te TypeExpr, name string) *TypeAtomic {
	t.Helper()
	atomic, ok := te.(*TypeAtomic)
	require.True(t, ok, "got %T", te)
	require.Equal(t, name, atomic.Name)
	require.NotZero(t, atomic.S.Start.Line)
	return atomic
}

func requireList(t *testing.T, te TypeExpr) *TypeList {
	t.Helper()
	list, ok := te.(*TypeList)
	require.True(t, ok, "got %T", te)
	require.NotZero(t, list.S.Start.Line)
	return list
}

func requireMap(t *testing.T, te TypeExpr) *TypeMap {
	t.Helper()
	m, ok := te.(*TypeMap)
	require.True(t, ok, "got %T", te)
	require.NotZero(t, m.S.Start.Line)
	return m
}

func requireTuple(t *testing.T, te TypeExpr) *TypeTuple {
	t.Helper()
	tuple, ok := te.(*TypeTuple)
	require.True(t, ok, "got %T", te)
	require.NotZero(t, tuple.S.Start.Line)
	return tuple
}

func requireObject(t *testing.T, te TypeExpr) *TypeObject {
	t.Helper()
	obj, ok := te.(*TypeObject)
	require.True(t, ok, "got %T", te)
	require.NotZero(t, obj.S.Start.Line)
	return obj
}

func requireOptional(t *testing.T, te TypeExpr) *TypeOptional {
	t.Helper()
	opt, ok := te.(*TypeOptional)
	require.True(t, ok, "got %T", te)
	require.NotZero(t, opt.S.Start.Line)
	return opt
}

func requireLibraryConfig(t *testing.T, te TypeExpr) *TypeLibraryConfig {
	t.Helper()
	lib, ok := te.(*TypeLibraryConfig)
	require.True(t, ok, "got %T", te)
	require.NotZero(t, lib.S.Start.Line)
	return lib
}

func requireObjectField(t *testing.T, obj *TypeObject, idx int, name string) *TypeObjectField {
	t.Helper()
	require.Greater(t, len(obj.Fields), idx)
	field := obj.Fields[idx]
	require.Equal(t, name, field.Name)
	require.NotZero(t, field.S.Start.Line)
	return field
}

func TestParseTypeAtomics(t *testing.T) {
	for _, name := range []string{"string", "number", "integer", "boolean", "null", "opaque"} {
		t.Run(name, func(t *testing.T) {
			got, err := ParseType("type.ub", []byte(name))
			require.NoError(t, err)
			requireAtomic(t, got, name)
		})
	}
}

func TestParseTypeValidFixtures(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		check   func(t *testing.T, te TypeExpr)
	}{
		{
			name:    "atomic string",
			fixture: "atomic-string",
			check: func(t *testing.T, te TypeExpr) {
				requireAtomic(t, te, "string")
			},
		},
		{
			name:    "atomic null",
			fixture: "atomic-null",
			check: func(t *testing.T, te TypeExpr) {
				requireAtomic(t, te, "null")
			},
		},
		{
			name:    "list",
			fixture: "list-string",
			check: func(t *testing.T, te TypeExpr) {
				list := requireList(t, te)
				requireAtomic(t, list.Elem, "string")
			},
		},
		{
			name:    "map",
			fixture: "map-integer",
			check: func(t *testing.T, te TypeExpr) {
				m := requireMap(t, te)
				requireAtomic(t, m.Elem, "integer")
			},
		},
		{
			name:    "tuple",
			fixture: "tuple-mixed",
			check: func(t *testing.T, te TypeExpr) {
				tuple := requireTuple(t, te)
				require.Len(t, tuple.Elements, 3)
				requireAtomic(t, tuple.Elements[0], "string")
				requireAtomic(t, tuple.Elements[1], "integer")
				requireAtomic(t, tuple.Elements[2], "boolean")
			},
		},
		{
			name:    "object fields",
			fixture: "object-fields",
			check: func(t *testing.T, te TypeExpr) {
				obj := requireObject(t, te)
				require.False(t, obj.Open)
				require.Len(t, obj.Fields, 3)
				requireAtomic(t, requireObjectField(t, obj, 0, "name").Type, "string")
				tags := requireObjectField(t, obj, 1, "tags")
				requireAtomic(t, requireMap(t, tags.Type).Elem, "string")
				enabled := requireObjectField(t, obj, 2, "enabled")
				requireAtomic(t, enabled.Type, "boolean")
			},
		},
		{
			name:    "object declaration fields",
			fixture: "object-with-decl",
			check: func(t *testing.T, te TypeExpr) {
				obj := requireObject(t, te)
				require.Len(t, obj.Fields, 2)

				name := requireObjectField(t, obj, 0, "name")
				require.Nil(t, name.Type)
				require.NotNil(t, name.Decl)
				require.Len(t, name.Decl.Fields, 2)
				require.Equal(t, "type", name.Decl.Fields[0].Key.Name)
				requireAtomic(t, name.Decl.Fields[0].Value.(TypeExpr), "string")
				require.Equal(t, "pattern", name.Decl.Fields[1].Key.Name)

				size := requireObjectField(t, obj, 1, "size")
				require.Nil(t, size.Type)
				require.NotNil(t, size.Decl)
				require.Len(t, size.Decl.Fields, 3)
				require.Equal(t, "type", size.Decl.Fields[0].Key.Name)
				requireAtomic(t, size.Decl.Fields[0].Value.(TypeExpr), "integer")
				require.Equal(t, "default", size.Decl.Fields[1].Key.Name)
				require.Equal(t, int64(3), size.Decl.Fields[1].Value.(*NumberLit).ParsedInt)
			},
		},
		{
			name:    "open object",
			fixture: "open-object",
			check: func(t *testing.T, te TypeExpr) {
				obj := requireObject(t, te)
				require.True(t, obj.Open)
				require.Len(t, obj.Fields, 1)
				url := requireObjectField(t, obj, 0, "url")
				requireAtomic(t, url.Type, "string")
			},
		},
		{
			name:    "library config",
			fixture: "library-config",
			check: func(t *testing.T, te TypeExpr) {
				lib := requireLibraryConfig(t, te)
				require.Equal(t, "github.com/acme/aws", lib.Path.Value)
			},
		},
		{
			name:    "optional open object",
			fixture: "optional-open",
			check: func(t *testing.T, te TypeExpr) {
				opt := requireOptional(t, te)
				obj := requireObject(t, opt.Elem)
				require.True(t, obj.Open)
				retries := requireObjectField(t, obj, 0, "retries")
				requireAtomic(t, retries.Type, "integer")
			},
		},
		{
			name:    "deep nesting",
			fixture: "deep",
			check: func(t *testing.T, te TypeExpr) {
				opt := requireOptional(t, te)
				list := requireList(t, opt.Elem)
				obj := requireObject(t, list.Elem)
				require.Len(t, obj.Fields, 2)

				endpoints := requireObjectField(t, obj, 0, "endpoints")
				endpointsList := requireList(t, endpoints.Type)
				endpoint := requireObject(t, endpointsList.Elem)
				require.Len(t, endpoint.Fields, 2)
				requireAtomic(t, requireObjectField(t, endpoint, 0, "host").Type, "string")

				ports := requireObjectField(t, endpoint, 1, "ports")
				tuple := requireTuple(t, ports.Type)
				require.Len(t, tuple.Elements, 2)
				requireAtomic(t, tuple.Elements[0], "integer")
				requireAtomic(t, tuple.Elements[1], "integer")

				metadata := requireObjectField(t, obj, 1, "metadata")
				requireAtomic(t, requireMap(t, metadata.Type).Elem, "string")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			te := parseTypeFixture(t, tt.fixture)
			tt.check(t, te)
		})
	}
}

func TestParseTypeInvalidFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/types/invalid", func(name string, src []byte) (string, []string) {
		_, err := ParseType(name+".ub", src)
		if err == nil {
			return "", nil
		}
		return "", []string{typeParseMessage(err)}
	})
}
