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

func mustPromoteParsedFixture(t *testing.T, fixture string) TypeExpr {
	t.Helper()
	path := filepath.Join("testdata", "types", "parsed", fixture+".ub")
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	parsed, err := ParseType(path, src)
	require.NoError(t, err)
	got := mustPromote(t, parsed)
	require.Same(t, parsed, got)
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

func requireObjectField(t *testing.T, obj *TypeObject, idx int, name string) *TypeObjectField {
	t.Helper()
	require.Greater(t, len(obj.Fields), idx)
	field := obj.Fields[idx]
	require.Equal(t, name, field.Name)
	require.NotZero(t, field.S.Start.Line)
	return field
}

func TestPromoteAcceptsParsedTypeExpr(t *testing.T) {
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
			te := mustPromoteParsedFixture(t, tt.fixture)
			tt.check(t, te)
		})
	}
}

func TestPromoteAtomic(t *testing.T) {
	fields := promoteFixture(t)
	wants := []struct{ key, name string }{
		{"atomic-string", "string"},
		{"atomic-number", "number"},
		{"atomic-integer", "integer"},
		{"atomic-boolean", "boolean"},
		{"atomic-null", "null"},
		{"atomic-opaque", "opaque"},
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
	// The declaration object is left intact; the schema validator promotes
	// its type field later.
	require.Equal(t, "type", size.Decl.Fields[0].Key.Name)
	require.Equal(t, "integer", size.Decl.Fields[0].Value.(*Ident).Name)
}

func TestPromoteOpen(t *testing.T) {
	fields := promoteFixture(t)

	obj, ok := mustPromote(t, fields["open-object"]).(*TypeObject)
	require.True(t, ok)
	require.True(t, obj.Open)
	require.Len(t, obj.Fields, 1)
	require.Equal(t, "url", obj.Fields[0].Name)
	require.Equal(t, "string", obj.Fields[0].Type.(*TypeAtomic).Name)

	nested, ok := mustPromote(t, fields["open-nested"]).(*TypeObject)
	require.True(t, ok)
	require.True(t, nested.Open)
	inner, ok := nested.Fields[0].Type.(*TypeObject)
	require.True(t, ok)
	require.True(t, inner.Open)

	opt, ok := mustPromote(t, fields["optional-open"]).(*TypeOptional)
	require.True(t, ok)
	optInner, ok := opt.Elem.(*TypeObject)
	require.True(t, ok)
	require.True(t, optInner.Open)

	closed, ok := mustPromote(t, fields["simple-object"]).(*TypeObject)
	require.True(t, ok)
	require.False(t, closed.Open)
}

func TestPromoteOpenErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			"no args",
			`t: open()`,
			"open takes exactly 1 type argument, got 0",
		},
		{
			"two args",
			`t: open(object({ a: string }), 1)`,
			"open takes exactly 1 type argument, got 2",
		},
		{
			"atomic",
			`t: open(string)`,
			"open applies to object types, got string",
		},
		{
			"list",
			`t: open(list(string))`,
			"open applies to object types, got list",
		},
		{
			"optional",
			`t: open(optional(object({ a: string })))`,
			"open applies to object types, got optional; " +
				"write optional(open(object({ ... })))",
		},
		{
			"double open",
			`t: open(open(object({ a: string })))`,
			"open(open(...)) is redundant; write open once",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := ParseSource("factory.ub", []byte(tt.src))
			require.NoError(t, err)
			_, err = PromoteType(f.Body.Fields[0].Value)
			require.Error(t, err)
			var pe *Error
			require.True(t, errors.As(err, &pe))
			require.Equal(t, tt.want, pe.Msg)
		})
	}
}

func TestPromoteOptional(t *testing.T) {
	fields := promoteFixture(t)

	bare, ok := mustPromote(t, fields["optional-bare"]).(*TypeOptional)
	require.True(t, ok)
	require.Equal(t, "string", bare.Elem.(*TypeAtomic).Name)

	withList, ok := mustPromote(t, fields["optional-list"]).(*TypeOptional)
	require.True(t, ok)
	innerList, ok := withList.Elem.(*TypeList)
	require.True(t, ok)
	require.Equal(t, "string", innerList.Elem.(*TypeAtomic).Name)

	withMap, ok := mustPromote(t, fields["optional-map"]).(*TypeOptional)
	require.True(t, ok)
	require.IsType(t, &TypeMap{}, withMap.Elem)
}

func TestPromoteDeepNested(t *testing.T) {
	fields := promoteFixture(t)

	deep, ok := mustPromote(t, fields["deep"]).(*TypeOptional)
	require.True(t, ok)

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
