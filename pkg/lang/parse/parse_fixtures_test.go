package parse

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
	f := loadFixture(t, "testdata/valid/empty.ub")
	require.Empty(t, f.Body.Fields)
}

func TestParseFixtureWhitespace(t *testing.T) {
	f := loadFixture(t, "testdata/valid/whitespace.ub")
	require.Empty(t, f.Body.Fields)
}

func TestParseFixtureBools(t *testing.T) {
	f := loadFixture(t, "testdata/valid/bools.ub")
	require.Len(t, f.Body.Fields, 2)

	identField(t, f.Body.Fields[0], "bool-true")
	require.True(t, f.Body.Fields[0].Value.(*BoolLit).Value)

	identField(t, f.Body.Fields[1], "bool-false")
	require.False(t, f.Body.Fields[1].Value.(*BoolLit).Value)
}

func TestParseFixtureNull(t *testing.T) {
	f := loadFixture(t, "testdata/valid/null.ub")
	require.Len(t, f.Body.Fields, 1)
	identField(t, f.Body.Fields[0], "empty")
	require.IsType(t, &NullLit{}, f.Body.Fields[0].Value)
}

func TestParseFixtureNumbers(t *testing.T) {
	f := loadFixture(t, "testdata/valid/numbers.ub")

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
	f := loadFixture(t, "testdata/valid/strings.ub")
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

func TestParseFixtureTripleStrings(t *testing.T) {
	f := loadFixture(t, "testdata/valid/triple-strings.ub")
	wants := []struct {
		key  string
		form StringForm
		val  string
	}{
		{
			key:  "single",
			form: StringTripleQuoteSingleLine,
			val:  "hello world",
		},
		{
			key:  "literal-clip",
			form: StringLiteralClip,
			val:  "one\ntwo\n",
		},
		{
			key:  "literal-strip",
			form: StringLiteralStrip,
			val:  "one\ntwo",
		},
		{
			key:  "folded-clip",
			form: StringFoldedClip,
			val:  "one two three\n",
		},
		{
			key:  "folded-paragraphs",
			form: StringFoldedClip,
			val:  "first paragraph continues here.\nsecond paragraph also continues.\n",
		},
		{
			key:  "folded-more-indented",
			form: StringFoldedClip,
			val:  "prose paragraph that folds normally.\nfollowed by:\n  item one\n  item two\n  item three\nback to folded prose that continues here.\n",
		},
		{
			key:  "folded-strip",
			form: StringFoldedStrip,
			val:  "one two three",
		},
		{
			key:  "joined-clip",
			form: StringJoinedClip,
			val:  "https://example.com/api/v1/users/12345\n",
		},
		{
			key:  "joined-strip",
			form: StringJoinedStrip,
			val:  "arn:aws:s3:::very-long-bucket-name/some/key/with/a/long/prefix/and-a-filename.tar.gz",
		},
		{
			key:  "joined-skips-blanks",
			form: StringJoinedStrip,
			val:  "firstsecond",
		},
		{
			key:  "embedded-triple-quote",
			form: StringLiteralClip,
			val:  "before '''mid''' after\n",
		},
		{
			key:  "trailing-whitespace-trim",
			form: StringLiteralClip,
			val:  "one\ntwo\n",
		},
	}
	require.Len(t, f.Body.Fields, len(wants))
	for i, w := range wants {
		identField(t, f.Body.Fields[i], w.key)
		s := f.Body.Fields[i].Value.(*StringLit)
		require.Equal(t, w.form, s.Form, "%s: Form", w.key)
		require.Equal(t, w.val, s.Value, "%s: Value", w.key)
	}
}

func TestParseFixtureMultilineStrings(t *testing.T) {
	f := loadFixture(t, "testdata/valid/multiline-strings.ub")
	wants := []struct {
		key, val string
		form     StringForm
	}{
		{"single-line", "hello world", StringTripleQuoteSingleLine},
		{"two-line", "first\nsecond\n", StringLiteralClip},
		{"indented-content", `echo "starting"` + "\nrun-thing\n", StringLiteralClip},
		{"preserves-extra-indent", "outer\n  inner\n", StringLiteralClip},
		{"empty-blank-line", "one\n\ntwo\n", StringLiteralClip},
	}
	require.Len(t, f.Body.Fields, len(wants))
	for i, w := range wants {
		identField(t, f.Body.Fields[i], w.key)
		s := f.Body.Fields[i].Value.(*StringLit)
		require.Equal(t, w.form, s.Form, "%s: Form", w.key)
		require.Equal(t, w.val, s.Value, w.key)
	}
}

func TestParseFixtureIdents(t *testing.T) {
	f := loadFixture(t, "testdata/valid/idents.ub")
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
	f := loadFixture(t, "testdata/valid/objects.ub")
	require.Len(t, f.Body.Fields, 14)

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

	oneCommas := mustObj(12, "one-line-commas")
	require.Len(t, oneCommas.Fields, 2)
	identField(t, oneCommas.Fields[0], "one")
	require.Equal(t, int64(1), oneCommas.Fields[0].Value.(*NumberLit).ParsedInt)

	multiCommas := mustObj(13, "multiline-commas")
	require.Len(t, multiCommas.Fields, 2)
}

func TestParseFixtureArrays(t *testing.T) {
	f := loadFixture(t, "testdata/valid/arrays.ub")
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
	f := loadFixture(t, "testdata/valid/comments.ub")
	require.Len(t, f.Body.Fields, 3)

	identField(t, f.Body.Fields[0], "one")
	require.Equal(t, int64(1), f.Body.Fields[0].Value.(*NumberLit).ParsedInt)
	identField(t, f.Body.Fields[1], "two")
	require.Equal(t, int64(2), f.Body.Fields[1].Value.(*NumberLit).ParsedInt)
	identField(t, f.Body.Fields[2], "three")
	require.Equal(t, int64(3), f.Body.Fields[2].Value.(*NumberLit).ParsedInt)
}

func TestParseFixtureMetaKeys(t *testing.T) {
	f := loadFixture(t, "testdata/valid/meta-keys.ub")
	wants := []string{"@for-each", "@depends-on", "@sensitive", "@trigger", "@module"}
	require.Len(t, f.Body.Fields, len(wants))
	for i, want := range wants {
		k := f.Body.Fields[i].Key
		require.True(t, k.IsMeta(), "field %d", i)
		require.Equal(t, want, k.Name, "field %d", i)
	}
}

func TestParseFixtureNested(t *testing.T) {
	f := loadFixture(t, "testdata/valid/nested.ub")
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
	f := loadFixture(t, "testdata/valid/realistic.ub")
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
	require.Equal(t, StringLiteralClip, notes.Form)
	require.Equal(t,
		"Multi-line notes preserve their content with the leading\nindent stripped to the closing-quote column.\n",
		notes.Value)
}

func TestParseFixtureDotPaths(t *testing.T) {
	f := loadFixture(t, "testdata/valid/dot-paths.ub")
	require.Len(t, f.Body.Fields, 8)

	getPath := func(idx int, key string) *DotPath {
		t.Helper()
		identField(t, f.Body.Fields[idx], key)
		p, ok := f.Body.Fields[idx].Value.(*DotPath)
		require.True(t, ok, "%s: got %T, want *DotPath", key, f.Body.Fields[idx].Value)
		return p
	}

	single := getPath(0, "single-segment")
	require.Equal(t, "var", single.Root.Name)
	require.Len(t, single.Segments, 1)
	require.Equal(t, "region", single.Segments[0].Name)

	deep := getPath(2, "deep")
	require.Equal(t, "resource", deep.Root.Name)
	wantNames := []string{"aws", "vpc", "main", "id"}
	require.Len(t, deep.Segments, len(wantNames))
	for i, n := range wantNames {
		require.Equal(t, n, deep.Segments[i].Name)
	}

	indexed := getPath(3, "indexed-string")
	require.Equal(t, "resource", indexed.Root.Name)
	require.Len(t, indexed.Segments, 4)
	require.Equal(t, "alpha", indexed.Segments[3].Index.(*StringLit).Value)

	indexedThenAttr := getPath(4, "indexed-then-attr")
	require.Len(t, indexedThenAttr.Segments, 5)
	require.Equal(t, "alpha", indexedThenAttr.Segments[3].Index.(*StringLit).Value)
	require.Equal(t, "arn", indexedThenAttr.Segments[4].Name)

	eachKey := getPath(5, "each-key")
	require.Equal(t, "@each", eachKey.Root.Name)
	require.Equal(t, "key", eachKey.Segments[0].Name)

	nestedIndex := getPath(7, "nested-index")
	require.IsType(t, &DotPath{}, nestedIndex.Segments[2].Index)
	inner := nestedIndex.Segments[2].Index.(*DotPath)
	require.Equal(t, "var", inner.Root.Name)
	require.Equal(t, "key", inner.Segments[0].Name)
}

func TestParseFixtureCalls(t *testing.T) {
	f := loadFixture(t, "testdata/valid/calls.ub")
	require.Len(t, f.Body.Fields, 8)

	getCall := func(idx int, key string) *Call {
		t.Helper()
		identField(t, f.Body.Fields[idx], key)
		c, ok := f.Body.Fields[idx].Value.(*Call)
		require.True(t, ok, "%s: got %T, want *Call", key, f.Body.Fields[idx].Value)
		return c
	}

	noArgs := getCall(0, "no-args")
	require.Equal(t, "range", noArgs.Callee.Name)
	require.Empty(t, noArgs.Args)

	one := getCall(1, "one-arg")
	require.Equal(t, "range", one.Callee.Name)
	require.Len(t, one.Args, 1)
	require.Equal(t, int64(3), one.Args[0].(*NumberLit).ParsedInt)

	multi := getCall(2, "multi-arg")
	require.Equal(t, "format", multi.Callee.Name)
	require.Len(t, multi.Args, 3)
	require.Equal(t, "%s-%s", multi.Args[0].(*StringLit).Value)
	require.IsType(t, &DotPath{}, multi.Args[1])
	require.IsType(t, &DotPath{}, multi.Args[2])

	nested := getCall(3, "nested-calls")
	require.Equal(t, "format", nested.Callee.Name)
	inner := nested.Args[1].(*Call)
	require.Equal(t, "b64-encode", inner.Callee.Name)
	require.Equal(t, "plaintext", inner.Args[0].(*StringLit).Value)

	mod := getCall(4, "module-call")
	require.Nil(t, mod.Callee)
	require.Equal(t, "lib", mod.Module.Name)
	require.Equal(t, "index-by", mod.Func.Name)
	require.Len(t, mod.Args, 2)

	modNoArgs := getCall(5, "module-no-args")
	require.Equal(t, "lib", modNoArgs.Module.Name)
	require.Equal(t, "now", modNoArgs.Func.Name)
	require.Empty(t, modNoArgs.Args)

	trailing := getCall(7, "trailing-comma")
	require.Equal(t, "format", trailing.Callee.Name)
	require.Len(t, trailing.Args, 2)
}

func TestParseFixtureOperators(t *testing.T) {
	f := loadFixture(t, "testdata/valid/operators.ub")
	require.Len(t, f.Body.Fields, 23)

	getInfix := func(idx int, key, op string) *Infix {
		t.Helper()
		identField(t, f.Body.Fields[idx], key)
		i, ok := f.Body.Fields[idx].Value.(*Infix)
		require.True(t, ok, "%s: got %T", key, f.Body.Fields[idx].Value)
		require.Equal(t, op, i.Op)
		return i
	}

	add := getInfix(0, "add", "+")
	require.Equal(t, int64(1), add.Left.(*NumberLit).ParsedInt)
	require.Equal(t, int64(2), add.Right.(*NumberLit).ParsedInt)

	for i, want := range []struct {
		key, op string
	}{
		{"add", "+"}, {"sub", "-"}, {"mul", "*"}, {"div", "/"},
		{"eq", "=="}, {"ne", "!="}, {"lt", "<"}, {"le", "<="},
		{"gt", ">"}, {"ge", ">="}, {"both-and", "&&"}, {"either-or", "||"},
	} {
		identField(t, f.Body.Fields[i], want.key)
		require.Equal(t, want.op, f.Body.Fields[i].Value.(*Infix).Op, want.key)
	}

	identField(t, f.Body.Fields[12], "negate")
	neg := f.Body.Fields[12].Value.(*Prefix)
	require.Equal(t, "!", neg.Op)
	require.Equal(t, "a", neg.Expr.(*Ident).Name)

	identField(t, f.Body.Fields[13], "unary-neg")
	un := f.Body.Fields[13].Value.(*Prefix)
	require.Equal(t, "-", un.Op)
	require.Equal(t, "x", un.Expr.(*Ident).Name)

	chainAdd := getInfix(14, "chain-add", "+")
	require.Equal(t, "+", chainAdd.Left.(*Infix).Op)
	require.Equal(t, int64(3), chainAdd.Right.(*NumberLit).ParsedInt)

	mixed := getInfix(16, "mixed", "+")
	require.Equal(t, int64(1), mixed.Left.(*NumberLit).ParsedInt)
	require.Equal(t, "*", mixed.Right.(*Infix).Op)

	parens := getInfix(17, "parens", "*")
	require.Equal(t, "+", parens.Left.(*Infix).Op)
	require.Equal(t, int64(3), parens.Right.(*NumberLit).ParsedInt)

	logic := getInfix(19, "logic", "||")
	require.Equal(t, "&&", logic.Left.(*Infix).Op)

	multi := getInfix(20, "multi-line", "+")
	require.Equal(t, "+", multi.Left.(*Infix).Op)
	require.Equal(t, int64(3), multi.Right.(*NumberLit).ParsedInt)

	tightAdd := getInfix(21, "tight-add", "+")
	require.Equal(t, int64(1), tightAdd.Left.(*NumberLit).ParsedInt)
	require.Equal(t, int64(2), tightAdd.Right.(*NumberLit).ParsedInt)

	tightMul := getInfix(22, "tight-mul", "*")
	require.Equal(t, int64(3), tightMul.Left.(*NumberLit).ParsedInt)
	require.Equal(t, int64(4), tightMul.Right.(*NumberLit).ParsedInt)
}

func TestParseFixtureComplex(t *testing.T) {
	f := loadFixture(t, "testdata/valid/complex.ub")
	require.Len(t, f.Body.Fields, 18)

	byKey := make(map[string]Expr, len(f.Body.Fields))
	for _, fld := range f.Body.Fields {
		byKey[fld.Key.Name] = fld.Value
	}

	// call-plus-string: format(...) + '-suffix'
	cps := byKey["call-plus-string"].(*Infix)
	require.Equal(t, "+", cps.Op)
	require.IsType(t, &Call{}, cps.Left)
	require.Equal(t, "format", cps.Left.(*Call).Callee.Name)
	require.Equal(t, "-suffix", cps.Right.(*StringLit).Value)

	// nested-calls: lib.index-by(format('%s', var.x), 'name')
	nc := byKey["nested-calls"].(*Call)
	require.Equal(t, "lib", nc.Module.Name)
	require.Equal(t, "index-by", nc.Func.Name)
	require.Len(t, nc.Args, 2)
	require.Equal(t, "format", nc.Args[0].(*Call).Callee.Name)
	require.Equal(t, "name", nc.Args[1].(*StringLit).Value)

	// call-as-operand: count(var.items) > 0 && var.enabled
	cao := byKey["call-as-operand"].(*Infix)
	require.Equal(t, "&&", cao.Op)
	cmp := cao.Left.(*Infix)
	require.Equal(t, ">", cmp.Op)
	require.Equal(t, "count", cmp.Left.(*Call).Callee.Name)
	require.Equal(t, int64(0), cmp.Right.(*NumberLit).ParsedInt)
	require.Equal(t, "var", cao.Right.(*DotPath).Root.Name)

	// arith-with-vars: (var.size + 1) * 2
	awv := byKey["arith-with-vars"].(*Infix)
	require.Equal(t, "*", awv.Op)
	add := awv.Left.(*Infix)
	require.Equal(t, "+", add.Op)
	require.Equal(t, "var", add.Left.(*DotPath).Root.Name)
	require.Equal(t, int64(1), add.Right.(*NumberLit).ParsedInt)
	require.Equal(t, int64(2), awv.Right.(*NumberLit).ParsedInt)

	// deep-comparison: var.region == 'us-east-1' || var.region == 'us-west-2'
	dc := byKey["deep-comparison"].(*Infix)
	require.Equal(t, "||", dc.Op)
	require.Equal(t, "==", dc.Left.(*Infix).Op)
	require.Equal(t, "==", dc.Right.(*Infix).Op)
	require.Equal(t, "us-east-1", dc.Left.(*Infix).Right.(*StringLit).Value)
	require.Equal(t, "us-west-2", dc.Right.(*Infix).Right.(*StringLit).Value)

	// indexed-in-arith: var.tags['Name'] + '-x'
	iia := byKey["indexed-in-arith"].(*Infix)
	require.Equal(t, "+", iia.Op)
	tags := iia.Left.(*DotPath)
	require.Equal(t, "var", tags.Root.Name)
	require.Equal(t, "tags", tags.Segments[0].Name)
	require.Equal(t, "Name", tags.Segments[1].Index.(*StringLit).Value)
	require.Equal(t, "-x", iia.Right.(*StringLit).Value)

	// arr-of-exprs: [1+1, 2*2, format(...), var.x]
	arr := byKey["arr-of-exprs"].(*ArrayLit)
	require.Len(t, arr.Elements, 4)
	require.Equal(t, "+", arr.Elements[0].(*Infix).Op)
	require.Equal(t, "*", arr.Elements[1].(*Infix).Op)
	require.Equal(t, "format", arr.Elements[2].(*Call).Callee.Name)
	require.Equal(t, "var", arr.Elements[3].(*DotPath).Root.Name)

	// obj-of-exprs
	obj := byKey["obj-of-exprs"].(*ObjectLit)
	require.Len(t, obj.Fields, 4)
	require.Equal(t, "+", obj.Fields[0].Value.(*Infix).Op)
	require.Equal(t, "!", obj.Fields[1].Value.(*Prefix).Op)
	require.Equal(t, "format", obj.Fields[2].Value.(*Call).Callee.Name)
	guarded := obj.Fields[3].Value.(*Infix)
	require.Equal(t, "&&", guarded.Op)

	// deep-paren-mix: ((a+1) * (b-2)) / (c+3)
	dpm := byKey["deep-paren-mix"].(*Infix)
	require.Equal(t, "/", dpm.Op)
	require.Equal(t, "*", dpm.Left.(*Infix).Op)
	require.Equal(t, "+", dpm.Right.(*Infix).Op)

	// call-in-call-in-call: outer(middle(inner('deep')), 1)
	cic := byKey["call-in-call-in-call"].(*Call)
	require.Equal(t, "outer", cic.Callee.Name)
	require.Len(t, cic.Args, 2)
	mid := cic.Args[0].(*Call)
	require.Equal(t, "middle", mid.Callee.Name)
	in := mid.Args[0].(*Call)
	require.Equal(t, "inner", in.Callee.Name)
	require.Equal(t, "deep", in.Args[0].(*StringLit).Value)
	require.Equal(t, int64(1), cic.Args[1].(*NumberLit).ParsedInt)

	// mixed-precedence: a + b * c == d - e / f && g
	// && binds loosest, so top is &&
	mp := byKey["mixed-precedence"].(*Infix)
	require.Equal(t, "&&", mp.Op)
	// Left of && is the equality (== binds looser than +/-)
	require.Equal(t, "==", mp.Left.(*Infix).Op)
	require.Equal(t, "g", mp.Right.(*Ident).Name)
	// Left of == is "a + b * c"
	eqLeft := mp.Left.(*Infix).Left.(*Infix)
	require.Equal(t, "+", eqLeft.Op)
	require.Equal(t, "*", eqLeft.Right.(*Infix).Op)
	// Right of == is "d - e / f"
	eqRight := mp.Left.(*Infix).Right.(*Infix)
	require.Equal(t, "-", eqRight.Op)
	require.Equal(t, "/", eqRight.Right.(*Infix).Op)

	// unary-on-call: !lib.is-valid(var.x)
	uoc := byKey["unary-on-call"].(*Prefix)
	require.Equal(t, "!", uoc.Op)
	call := uoc.Expr.(*Call)
	require.Equal(t, "lib", call.Module.Name)
	require.Equal(t, "is-valid", call.Func.Name)

	// unary-on-paren: -(var.x + var.y)
	uop := byKey["unary-on-paren"].(*Prefix)
	require.Equal(t, "-", uop.Op)
	require.Equal(t, "+", uop.Expr.(*Infix).Op)

	// chain-with-calls: a + format(...) + b - left-associated
	cwc := byKey["chain-with-calls"].(*Infix)
	require.Equal(t, "+", cwc.Op)
	require.Equal(t, "+", cwc.Left.(*Infix).Op)
	require.Equal(t, "b", cwc.Right.(*Ident).Name)

	// call-with-arr-arg: build([1,2,3], var.opts)
	cwa := byKey["call-with-arr-arg"].(*Call)
	require.Equal(t, "build", cwa.Callee.Name)
	require.IsType(t, &ArrayLit{}, cwa.Args[0])
	require.Len(t, cwa.Args[0].(*ArrayLit).Elements, 3)

	// call-with-obj-arg: merge({a:1,b:2}, var.extra)
	cwo := byKey["call-with-obj-arg"].(*Call)
	require.Equal(t, "merge", cwo.Callee.Name)
	require.IsType(t, &ObjectLit{}, cwo.Args[0])
	require.Len(t, cwo.Args[0].(*ObjectLit).Fields, 2)

	// double-indexed: data.x['outer']['inner'].field
	di := byKey["double-indexed"].(*DotPath)
	require.Equal(t, "data", di.Root.Name)
	require.Len(t, di.Segments, 4)
	require.Equal(t, "x", di.Segments[0].Name)
	require.Equal(t, "outer", di.Segments[1].Index.(*StringLit).Value)
	require.Equal(t, "inner", di.Segments[2].Index.(*StringLit).Value)
	require.Equal(t, "field", di.Segments[3].Name)
}

func TestParseTripleQuoteInvalidReasons(t *testing.T) {
	tests := []struct {
		file string
		want string
	}{
		{"triple-missing-sigil.ub", "no match found"},
		{"triple-sigil-then-content.ub", "no match found"},
		{"triple-tab-in-baseline.ub", "no tabs"},
		{"triple-less-indented.ub", "less indented"},
		{"triple-trailing-after-close.ub", "no match found"},
		{"multiline-bad-indent.ub", "less indented"},
		{"multiline-content-on-close-line.ub", "no match found"},
		{"unclosed-multiline.ub", "no match found"},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			path := filepath.Join("testdata/invalid", tt.file)
			b, err := os.ReadFile(path)
			require.NoError(t, err)
			_, err = ParseSource(path, b)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want,
				"expected error containing %q, got %v", tt.want, err)
		})
	}
}

func TestParseInvalidFixtures(t *testing.T) {
	matches, err := filepath.Glob("testdata/invalid/*.ub")
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
