package lang

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func loadFixture(t *testing.T, path string) *File {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err, "read %s", path)
	f, err := ParseSource(path, b)
	require.NoError(t, err, "parse %s", path)
	return f
}

func identField(t *testing.T, fld *Field, key string) {
	t.Helper()
	require.Equal(t, FieldIdent, fld.Key.Kind, "expected ident key")
	require.Equal(t, key, fld.Key.Name)
}

func TestParseFixtureEmpty(t *testing.T) {
	f := loadFixture(t, "testdata/parse/valid/empty.ub")
	require.Empty(t, f.Body.Fields)
}

func TestParseFixtureWhitespace(t *testing.T) {
	f := loadFixture(t, "testdata/parse/valid/whitespace.ub")
	require.Empty(t, f.Body.Fields)
}

func TestParseFixtureBools(t *testing.T) {
	f := loadFixture(t, "testdata/parse/valid/bools.ub")
	require.Len(t, f.Body.Fields, 2)

	identField(t, f.Body.Fields[0], "bool-true")
	require.True(t, f.Body.Fields[0].Value.(*BoolLit).Value)

	identField(t, f.Body.Fields[1], "bool-false")
	require.False(t, f.Body.Fields[1].Value.(*BoolLit).Value)
}

func TestParseFixtureNull(t *testing.T) {
	f := loadFixture(t, "testdata/parse/valid/null.ub")
	require.Len(t, f.Body.Fields, 1)
	identField(t, f.Body.Fields[0], "empty")
	require.IsType(t, &NullLit{}, f.Body.Fields[0].Value)
}

func TestParseFixtureNumbers(t *testing.T) {
	f := loadFixture(t, "testdata/parse/valid/numbers.ub")

	wantInt := []struct {
		key string
		val int64
	}{
		{"int-zero", 0},
		{"int-pos", 1000},
		{"int-large", 9876543210},
		{"int-neg", -7654},
	}
	wantFloat := []struct {
		key string
		val float64
	}{
		{"float-zero", 0.0},
		{"float-pos", 123.456},
		{"float-small", 0.1},
		{"float-neg", -123.456},
	}
	require.Len(t, f.Body.Fields, len(wantInt)+len(wantFloat))

	for i, w := range wantInt {
		identField(t, f.Body.Fields[i], w.key)
		n := f.Body.Fields[i].Value.(*NumberLit)
		require.False(t, n.IsFloat, "%s should be int", w.key)
		require.Equal(t, w.val, n.ParsedInt, w.key)
	}
	for i, w := range wantFloat {
		idx := len(wantInt) + i
		identField(t, f.Body.Fields[idx], w.key)
		n := f.Body.Fields[idx].Value.(*NumberLit)
		require.True(t, n.IsFloat, "%s should be float", w.key)
		require.Equal(t, w.val, n.ParsedFloat, w.key)
	}
}

func TestParseFixtureStrings(t *testing.T) {
	f := loadFixture(t, "testdata/parse/valid/strings.ub")
	wants := []struct {
		key, val string
	}{
		{"simple", "hello"},
		{"empty", ""},
		{"with-spaces", "one two three"},
		{"with-quote", "it's here"},
		{"with-backslash", `a\b`},
		{"with-newline", "line1\nline2"},
		{"with-tab", "col1\tcol2"},
		{"with-cr", "r\rn"},
		{"unknown-esc", `keep \xliteral`},
		{"url", "github.com/cloudboss/unobin@v1.2.3"},
		{"regex", `^[a-z][a-z0-9-]*$`},
	}
	require.Len(t, f.Body.Fields, len(wants))
	for i, w := range wants {
		identField(t, f.Body.Fields[i], w.key)
		require.Equal(t, w.val, f.Body.Fields[i].Value.(*StringLit).Value, w.key)
	}
}

func TestParseFixtureMultilineStrings(t *testing.T) {
	f := loadFixture(t, "testdata/parse/valid/multiline-strings.ub")
	wants := []struct {
		key, val string
	}{
		{"single-line", "hello world"},
		{"two-line", "first\nsecond\n"},
		{"indented-content", `echo "starting"` + "\nrun-thing\n"},
		{"preserves-extra-indent", "outer\n  inner\n"},
		{"empty-blank-line", "one\n\ntwo\n"},
	}
	require.Len(t, f.Body.Fields, len(wants))
	for i, w := range wants {
		identField(t, f.Body.Fields[i], w.key)
		s := f.Body.Fields[i].Value.(*StringLit)
		require.True(t, s.Multiline, "%s: Multiline flag", w.key)
		require.Equal(t, w.val, s.Value, w.key)
	}
}

func TestParseFixtureIdents(t *testing.T) {
	f := loadFixture(t, "testdata/parse/valid/idents.ub")
	wants := []struct {
		key, val string
	}{
		{"type", "string"},
		{"kind", "required-together"},
		{"format", "date-time"},
		{"backend", "s3"},
	}
	require.Len(t, f.Body.Fields, len(wants))
	for i, w := range wants {
		identField(t, f.Body.Fields[i], w.key)
		require.Equal(t, w.val, f.Body.Fields[i].Value.(*Ident).Name, w.key)
	}
}

func TestParseFixtureObjects(t *testing.T) {
	f := loadFixture(t, "testdata/parse/valid/objects.ub")
	require.Len(t, f.Body.Fields, 12)

	mustObj := func(idx int, key string) *ObjectLit {
		t.Helper()
		identField(t, f.Body.Fields[idx], key)
		o, ok := f.Body.Fields[idx].Value.(*ObjectLit)
		require.True(t, ok, "%s: expected *ObjectLit, got %T", key, f.Body.Fields[idx].Value)
		return o
	}

	require.Empty(t, mustObj(0, "empty").Fields)
	require.Empty(t, mustObj(1, "empty-spaces").Fields)
	require.Empty(t, mustObj(2, "empty-newline").Fields)
	require.Empty(t, mustObj(3, "empty-with-comments").Fields)

	one := mustObj(4, "one-line")
	require.Len(t, one.Fields, 2)
	identField(t, one.Fields[0], "one")
	require.Equal(t, int64(1), one.Fields[0].Value.(*NumberLit).ParsedInt)
	identField(t, one.Fields[1], "two")
	require.Equal(t, int64(2), one.Fields[1].Value.(*NumberLit).ParsedInt)

	multi := mustObj(5, "multiline")
	require.Len(t, multi.Fields, 2)
	identField(t, multi.Fields[0], "one")
	require.Equal(t, int64(1), multi.Fields[0].Value.(*NumberLit).ParsedInt)

	mixed := mustObj(6, "mixed-pairs-per-line")
	require.Len(t, mixed.Fields, 3)

	commentInline := mustObj(7, "comment-inline")
	require.Len(t, commentInline.Fields, 2)

	stringKeys := mustObj(8, "string-keys")
	require.Len(t, stringKeys.Fields, 2)
	require.Equal(t, FieldString, stringKeys.Fields[0].Key.Kind)
	require.Equal(t, "one", stringKeys.Fields[0].Key.String)

	mixedKeys := mustObj(9, "mixed-keys")
	require.Equal(t, FieldIdent, mixedKeys.Fields[0].Key.Kind)
	require.Equal(t, "bare-key", mixedKeys.Fields[0].Key.Name)
	require.Equal(t, FieldString, mixedKeys.Fields[1].Key.Kind)
	require.Equal(t, "string-key", mixedKeys.Fields[1].Key.String)
	require.Equal(t, FieldString, mixedKeys.Fields[2].Key.Kind)
	require.Equal(t, "$id", mixedKeys.Fields[2].Key.String)

	arbitrary := mustObj(10, "arbitrary-keys")
	wantKeys := []string{
		"",
		" leading space",
		"has spaces in middle",
		"with.dots",
		"with/slashes/path",
		"with-dashes-and_underscores",
		"starts-with-digit-1",
		"@looks-like-meta",
		"special $#%&* chars",
		`with"double"quotes`,
		`escaped 'apostrophe'`,
		"unicode-cafe",
		"true",
	}
	require.Len(t, arbitrary.Fields, len(wantKeys))
	for i, want := range wantKeys {
		require.Equal(t, FieldString, arbitrary.Fields[i].Key.Kind, "arbitrary-keys[%d]", i)
		require.Equal(t, want, arbitrary.Fields[i].Key.String, "arbitrary-keys[%d]", i)
	}

	all := mustObj(11, "all-kinds-of-values")
	require.GreaterOrEqual(t, len(all.Fields), 10)
}

func TestParseFixtureArrays(t *testing.T) {
	f := loadFixture(t, "testdata/parse/valid/arrays.ub")
	require.Len(t, f.Body.Fields, 9)

	mustArr := func(idx int, key string) *ArrayLit {
		t.Helper()
		identField(t, f.Body.Fields[idx], key)
		a, ok := f.Body.Fields[idx].Value.(*ArrayLit)
		require.True(t, ok, "%s: expected *ArrayLit, got %T", key, f.Body.Fields[idx].Value)
		return a
	}

	require.Empty(t, mustArr(0, "empty").Elements)
	require.Empty(t, mustArr(1, "empty-spaces").Elements)
	require.Empty(t, mustArr(2, "empty-newline").Elements)
	require.Empty(t, mustArr(3, "empty-with-comments").Elements)

	one := mustArr(4, "one-line")
	require.Len(t, one.Elements, 3)
	for i, want := range []int64{1, 2, 3} {
		require.Equal(t, want, one.Elements[i].(*NumberLit).ParsedInt)
	}

	require.Len(t, mustArr(5, "multiline-tight").Elements, 6)
	require.Len(t, mustArr(6, "multiline-loose").Elements, 5)

	commentInline := mustArr(7, "comment-inline")
	require.Len(t, commentInline.Elements, 2)
	require.Equal(t, int64(1), commentInline.Elements[0].(*NumberLit).ParsedInt)
	require.Equal(t, int64(2), commentInline.Elements[1].(*NumberLit).ParsedInt)

	all := mustArr(8, "all-kinds-of-values")
	require.Len(t, all.Elements, 10)
	require.Equal(t, "1", all.Elements[0].(*StringLit).Value)
	require.IsType(t, &ArrayLit{}, all.Elements[1])
	require.IsType(t, &NumberLit{}, all.Elements[6])
	require.False(t, all.Elements[7].(*BoolLit).Value)
	require.True(t, all.Elements[8].(*BoolLit).Value)
	require.IsType(t, &NullLit{}, all.Elements[9])
}

func TestParseFixtureComments(t *testing.T) {
	f := loadFixture(t, "testdata/parse/valid/comments.ub")
	require.Len(t, f.Body.Fields, 3)

	identField(t, f.Body.Fields[0], "one")
	require.Equal(t, int64(1), f.Body.Fields[0].Value.(*NumberLit).ParsedInt)
	identField(t, f.Body.Fields[1], "two")
	require.Equal(t, int64(2), f.Body.Fields[1].Value.(*NumberLit).ParsedInt)
	identField(t, f.Body.Fields[2], "three")
	require.Equal(t, int64(3), f.Body.Fields[2].Value.(*NumberLit).ParsedInt)
}

func TestParseFixtureMetaKeys(t *testing.T) {
	f := loadFixture(t, "testdata/parse/valid/meta-keys.ub")
	wants := []string{"@for-each", "@depends-on", "@sensitive", "@trigger", "@module"}
	require.Len(t, f.Body.Fields, len(wants))
	for i, want := range wants {
		k := f.Body.Fields[i].Key
		require.True(t, k.IsMeta(), "field %d", i)
		require.Equal(t, want, k.Name, "field %d", i)
	}
}

func TestParseFixtureNested(t *testing.T) {
	f := loadFixture(t, "testdata/parse/valid/nested.ub")
	require.Len(t, f.Body.Fields, 1)
	identField(t, f.Body.Fields[0], "root")
	root := f.Body.Fields[0].Value.(*ObjectLit)
	require.Len(t, root.Fields, 2)

	identField(t, root.Fields[0], "items")
	items := root.Fields[0].Value.(*ArrayLit)
	require.Len(t, items.Elements, 2)

	first := items.Elements[0].(*ObjectLit)
	identField(t, first.Fields[0], "name")
	require.Equal(t, "first", first.Fields[0].Value.(*StringLit).Value)
	identField(t, first.Fields[1], "size")
	require.Equal(t, int64(1), first.Fields[1].Value.(*NumberLit).ParsedInt)

	second := items.Elements[1].(*ObjectLit)
	require.Equal(t, "second", second.Fields[0].Value.(*StringLit).Value)
	require.Equal(t, int64(2), second.Fields[1].Value.(*NumberLit).ParsedInt)

	identField(t, root.Fields[1], "meta")
	meta := root.Fields[1].Value.(*ObjectLit)
	identField(t, meta.Fields[0], "flags")
	flags := meta.Fields[0].Value.(*ArrayLit)
	for i, want := range []bool{true, false, true} {
		require.Equal(t, want, flags.Elements[i].(*BoolLit).Value)
	}
	identField(t, meta.Fields[1], "owner")
	require.Equal(t, "me", meta.Fields[1].Value.(*StringLit).Value)
}

func TestParseFixtureRealistic(t *testing.T) {
	f := loadFixture(t, "testdata/parse/valid/realistic.ub")
	wants := []string{"description", "inputs", "constraints", "imports", "outputs", "notes"}
	require.Len(t, f.Body.Fields, len(wants))
	for i, want := range wants {
		identField(t, f.Body.Fields[i], want)
	}

	inputs := f.Body.Fields[1].Value.(*ObjectLit)
	require.Len(t, inputs.Fields, 5)

	region := inputs.Fields[0].Value.(*ObjectLit)
	identField(t, region.Fields[0], "type")
	require.Equal(t, "string", region.Fields[0].Value.(*Ident).Name)
	identField(t, region.Fields[1], "description")
	require.Equal(t, "AWS region", region.Fields[1].Value.(*StringLit).Value)

	constraints := f.Body.Fields[2].Value.(*ArrayLit)
	require.Len(t, constraints.Elements, 1)

	notes := f.Body.Fields[5].Value.(*StringLit)
	require.True(t, notes.Multiline)
	require.Equal(t,
		"Multi-line notes preserve their content with the leading\nindent stripped to the closing-backtick column.\n",
		notes.Value)
}

func TestParseInvalidFixtures(t *testing.T) {
	matches, err := filepath.Glob("testdata/parse/invalid/*.ub")
	require.NoError(t, err)
	require.NotEmpty(t, matches)

	for _, path := range matches {
		t.Run(filepath.Base(path), func(t *testing.T) {
			b, err := os.ReadFile(path)
			require.NoError(t, err)
			_, err = ParseSource(path, b)
			require.Error(t, err)
		})
	}
}
