package typecheck

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseExpr(t *testing.T, src string) lang.Expr {
	t.Helper()
	f, err := lang.ParseSource("expr.ub", []byte("v: "+src))
	require.NoError(t, err)
	require.NotNil(t, f.Body)
	require.NotEmpty(t, f.Body.Fields)
	return f.Body.Fields[0].Value
}

func TestInferLiterals(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)

	tests := []struct {
		src  string
		want Type
	}{
		{"'hi'", TString()},
		{"42", TInteger()},
		{"3.14", TNumber()},
		{"true", TBoolean()},
		{"null", TNull()},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			got := Infer(parseExpr(t, tt.src), TUnknown(), scope, errs)
			assert.True(t, got.Equal(tt.want), "got %s want %s", got, tt.want)
		})
	}
	assert.Empty(t, errs.Errors())
}

func TestInferArrayUntargetedJoins(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "[1, 2, 3]"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TList(TInteger())))
	assert.Empty(t, errs.Errors())
}

func TestInferArrayUntargetedMixedBecomesTuple(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "[1, 'hi', true]"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TTuple([]Type{TInteger(), TString(), TBoolean()})))
	assert.Empty(t, errs.Errors())
}

func TestInferArrayMatchesListTarget(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	target := TList(TString())
	got := Check(parseExpr(t, "['a', 'b']"), target, scope, errs)
	assert.True(t, got.Equal(target))
	assert.Empty(t, errs.Errors())
}

func TestInferArrayElementMismatch(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	target := TList(TString())
	Check(parseExpr(t, "['a', 5]"), target, scope, errs)
	require.Len(t, errs.Errors(), 1)
	assert.Contains(t, errs.Errors()[0].Msg, "expected string, got integer")
}

func TestInferArrayTupleTarget(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	target := TTuple([]Type{TString(), TInteger()})
	got := Check(parseExpr(t, "['a', 5]"), target, scope, errs)
	assert.True(t, got.Equal(target))
	assert.Empty(t, errs.Errors())
}

func TestInferObjectAgainstClosedTarget(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	target := TObject([]ObjectField{
		{Name: "id", Type: TString()},
		{Name: "count", Type: TInteger(), Optional: true},
	})
	got := Check(parseExpr(t, "{ id: 'x' }"), target, scope, errs)
	assert.Equal(t, Object, got.Kind)
	assert.Empty(t, errs.Errors())
}

func TestInferObjectReportsUnknownField(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	target := TObject([]ObjectField{{Name: "id", Type: TString()}})
	Check(parseExpr(t, "{ id: 'x', bogus: 1 }"), target, scope, errs)
	require.Len(t, errs.Errors(), 1)
	assert.Contains(t, errs.Errors()[0].Msg, `unknown field "bogus"`)
}

func TestInferObjectReportsMissingRequired(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	target := TObject([]ObjectField{
		{Name: "id", Type: TString()},
		{Name: "count", Type: TInteger()},
	})
	Check(parseExpr(t, "{ id: 'x' }"), target, scope, errs)
	require.Len(t, errs.Errors(), 1)
	assert.Contains(t, errs.Errors()[0].Msg, `missing required field "count"`)
}

func TestInferInput(t *testing.T) {
	scope := &Scope{
		Inputs: []ObjectField{
			{Name: "region", Type: TString()},
			{Name: "ports", Type: TList(TInteger())},
			{Name: "tags", Type: TMap(TString()), Optional: true},
		},
	}
	errs := lang.NewErrorList(0)

	assert.True(t, Infer(parseExpr(t, "input.region"), TUnknown(), scope, errs).Equal(TString()))
	assert.True(t, Infer(parseExpr(t, "input.ports"), TUnknown(), scope, errs).Equal(TList(TInteger())))
	assert.True(
		t,
		Infer(parseExpr(t, "input.tags"), TUnknown(), scope, errs).
			Equal(TOptional(TMap(TString()))),
	)
	assert.Empty(t, errs.Errors())
}

func TestInferInputIntoStringSlotIntegerMismatch(t *testing.T) {
	scope := &Scope{
		Inputs: []ObjectField{
			{Name: "count", Type: TInteger()},
		},
	}
	errs := lang.NewErrorList(0)
	Check(parseExpr(t, "input.count"), TString(), scope, errs)
	require.Len(t, errs.Errors(), 1)
	assert.Contains(t, errs.Errors()[0].Msg, "expected string, got integer")
}

func TestInferNodeFieldTraversal(t *testing.T) {
	output := TObject([]ObjectField{
		{Name: "id", Type: TString()},
		{Name: "tags", Type: TMap(TString())},
	})
	scope := &Scope{
		LookupNode: func(kind, alias, typ, name string) (Type, bool) {
			if kind == "resource" && alias == "aws" && typ == "vpc" && name == "main" {
				return output, true
			}
			return Type{}, false
		},
	}
	errs := lang.NewErrorList(0)

	got := Infer(
		parseExpr(t, "resource.aws.vpc.main.id"),
		TUnknown(), scope, errs,
	)
	assert.True(t, got.Equal(TString()))

	got = Infer(
		parseExpr(t, "resource.aws.vpc.main.tags.name"),
		TUnknown(), scope, errs,
	)
	assert.True(t, got.Equal(TString()))

	got = Infer(
		parseExpr(t, "resource.aws.vpc.main.bogus"),
		TUnknown(), scope, errs,
	)
	assert.True(t, got.Equal(TUnknown()),
		"missing field traversal returns Unknown; existing reference checker reports the message")
	assert.Empty(t, errs.Errors())
}

func TestInferInputNestedObjectReportsUnknownField(t *testing.T) {
	scope := &Scope{
		Inputs: []ObjectField{
			{Name: "cfg", Type: TObject([]ObjectField{
				{Name: "host", Type: TString()},
				{Name: "port", Type: TInteger()},
			})},
		},
	}
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "input.cfg.bogus"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TUnknown()))
	require.Len(t, errs.Errors(), 1)
	assert.Contains(t, errs.Errors()[0].Msg, `unknown field "bogus" on object(`)
}

func TestInferInputDeeplyNestedObjectReportsUnknownField(t *testing.T) {
	scope := &Scope{
		Inputs: []ObjectField{
			{Name: "cfg", Type: TObject([]ObjectField{
				{Name: "db", Type: TObject([]ObjectField{
					{Name: "host", Type: TString()},
				})},
			})},
		},
	}
	errs := lang.NewErrorList(0)
	Infer(parseExpr(t, "input.cfg.db.port"), TUnknown(), scope, errs)
	require.Len(t, errs.Errors(), 1)
	assert.Contains(t, errs.Errors()[0].Msg, `unknown field "port" on object(`)
}

func TestInferNodeDeepNestedObjectReportsUnknownField(t *testing.T) {
	output := TObject([]ObjectField{
		{Name: "endpoint", Type: TObject([]ObjectField{
			{Name: "host", Type: TString()},
		})},
	})
	scope := &Scope{
		LookupNode: func(kind, alias, typ, name string) (Type, bool) {
			if kind == "resource" && alias == "aws" && typ == "rds" && name == "main" {
				return output, true
			}
			return Type{}, false
		},
	}
	errs := lang.NewErrorList(0)
	Infer(parseExpr(t, "resource.aws.rds.main.endpoint.port"), TUnknown(), scope, errs)
	require.Len(t, errs.Errors(), 1)
	assert.Contains(t, errs.Errors()[0].Msg, `unknown field "port" on object(`)
}

func TestInferNodeFirstSegmentUnknownStaysSilent(t *testing.T) {
	output := TObject([]ObjectField{
		{Name: "id", Type: TString()},
	})
	scope := &Scope{
		LookupNode: func(kind, alias, typ, name string) (Type, bool) {
			if kind == "resource" && alias == "aws" && typ == "vpc" && name == "main" {
				return output, true
			}
			return Type{}, false
		},
	}
	errs := lang.NewErrorList(0)
	Infer(parseExpr(t, "resource.aws.vpc.main.bogus"), TUnknown(), scope, errs)
	assert.Empty(t, errs.Errors(),
		"first trailing segment is the reference checker's responsibility")
}

func TestInferEachKeyValue(t *testing.T) {
	scope := &Scope{
		Each: &EachBinding{Key: TString(), Value: TInteger()},
	}
	errs := lang.NewErrorList(0)
	assert.True(
		t,
		Infer(parseExpr(t, "@each.key"), TUnknown(), scope, errs).Equal(TString()),
	)
	assert.True(
		t,
		Infer(parseExpr(t, "@each.value"), TUnknown(), scope, errs).Equal(TInteger()),
	)
	assert.Empty(t, errs.Errors())
}

func TestInferEachValueFieldTraversal(t *testing.T) {
	scope := &Scope{
		Each: &EachBinding{
			Key: TString(),
			Value: TObject([]ObjectField{
				{Name: "tls", Type: TBoolean(), Optional: true},
				{Name: "port", Type: TInteger()},
			}),
		},
	}
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "@each.value.tls"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TOptional(TBoolean())), "got %s", got)
	got = Infer(parseExpr(t, "@each.value.port"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TInteger()), "got %s", got)
	assert.Empty(t, errs.Errors())
}

func TestInferEachValueUnknownField(t *testing.T) {
	scope := &Scope{
		Each: &EachBinding{
			Key:   TString(),
			Value: TObject([]ObjectField{{Name: "port", Type: TInteger()}}),
		},
	}
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "@each.value.bogus"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TUnknown()))
	require.Equal(t,
		[]string{`unknown field "bogus" on object({ port: integer })`},
		errs.Messages())
}

func TestInferObjectLiteralAgainstOpenTarget(t *testing.T) {
	target := TOpenObject([]ObjectField{{Name: "url", Type: TString()}})
	errs := lang.NewErrorList(0)
	Check(parseExpr(t, "{ url: 'x', extra: 1 }"), target, &Scope{}, errs)
	assert.Empty(t, errs.Errors())

	// The same extra field against a closed target stays the typo catch.
	closed := TObject([]ObjectField{{Name: "url", Type: TString()}})
	Check(parseExpr(t, "{ url: 'x', extra: 1 }"), closed, &Scope{}, errs)
	require.Equal(t,
		[]string{`unknown field "extra" on object({ url: string })`},
		errs.Messages())
}

func TestInferObjectLiteralOpenTargetStillRequiresFields(t *testing.T) {
	target := TOpenObject([]ObjectField{{Name: "url", Type: TString()}})
	errs := lang.NewErrorList(0)
	Check(parseExpr(t, "{ extra: 1 }"), target, &Scope{}, errs)
	require.Equal(t,
		[]string{`missing required field "url" on open(object({ url: string }))`},
		errs.Messages())
}

func TestInferObjectLiteralOpenTargetChecksDeclaredFields(t *testing.T) {
	target := TOpenObject([]ObjectField{{Name: "url", Type: TString()}})
	errs := lang.NewErrorList(0)
	Check(parseExpr(t, "{ url: 7 }"), target, &Scope{}, errs)
	require.Equal(t,
		[]string{"type mismatch: expected string, got integer"},
		errs.Messages())
}

func TestNavigateOpenObjectFields(t *testing.T) {
	scope := &Scope{Inputs: []ObjectField{{
		Name: "payload",
		Type: TOpenObject([]ObjectField{{Name: "url", Type: TString()}}),
	}}}
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "input.payload.url"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TString()), "got %s", got)
	assert.Empty(t, errs.Errors())

	got = Infer(parseExpr(t, "input.payload.token"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TUnknown()))
	require.Equal(t,
		[]string{`unknown field "token" on open(object({ url: string })); ` +
			"declare the field to read it"},
		errs.Messages())
}

func TestIndexOpenObjectUndeclaredField(t *testing.T) {
	scope := &Scope{Inputs: []ObjectField{{
		Name: "payload",
		Type: TOpenObject([]ObjectField{{Name: "url", Type: TString()}}),
	}}}
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "input.payload['token']"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TUnknown()))
	require.Equal(t,
		[]string{`unknown field "token" on open(object({ url: string })); ` +
			"declare the field to read it"},
		errs.Messages())
}

func TestNavigateIntoScalar(t *testing.T) {
	scope := &Scope{Inputs: []ObjectField{
		{Name: "name", Type: TString()},
		{Name: "count", Type: TInteger()},
		{Name: "ports", Type: TList(TInteger())},
		{Name: "cfg", Type: TObject([]ObjectField{{Name: "tag", Type: TString()}})},
	}}
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{
			"string field",
			"input.name.length",
			[]string{`cannot read field "length" of string`},
		},
		{
			"integer field",
			"input.count.value",
			[]string{`cannot read field "value" of integer`},
		},
		{
			"list field",
			"input.ports.first",
			[]string{`cannot read field "first" of list(integer)`},
		},
		{
			"deep scalar field",
			"input.cfg.tag.upper",
			[]string{`cannot read field "upper" of string`},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := lang.NewErrorList(0)
			got := Infer(parseExpr(t, c.src), TUnknown(), scope, errs)
			assert.True(t, got.Equal(TUnknown()), "got %s", got)
			require.Equal(t, c.want, errs.Messages())
		})
	}
}

func TestNavigateIntoOpaque(t *testing.T) {
	scope := &Scope{Inputs: []ObjectField{
		{Name: "blob", Type: TOpaque()},
		{Name: "cfg", Type: TObject([]ObjectField{{Name: "inner", Type: TOpaque()}})},
		{Name: "tags", Type: TMap(TOpaque())},
	}}
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{
			"dot",
			"input.blob.url",
			[]string{"input.blob is opaque; declare the fields you read, " +
				"like open(object({ url: ... }))"},
		},
		{
			"guarded dot",
			"input.blob?.url",
			[]string{"input.blob is opaque; declare the fields you read, " +
				"like open(object({ url: ... }))"},
		},
		{
			"string index",
			"input.blob['url']",
			[]string{"input.blob is opaque; declare the fields you read, " +
				"like open(object({ url: ... }))"},
		},
		{
			"integer index",
			"input.blob[0]",
			[]string{"input.blob is opaque; declare its type to index into it"},
		},
		{
			"deep field",
			"input.cfg.inner.url",
			[]string{"input.cfg.inner is opaque; declare the fields you read, " +
				"like open(object({ url: ... }))"},
		},
		{
			"map element",
			"input.tags['a'].x",
			[]string{"input.tags['a'] is opaque; declare the fields you read, " +
				"like open(object({ x: ... }))"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := lang.NewErrorList(0)
			got := Infer(parseExpr(t, c.src), TUnknown(), scope, errs)
			assert.True(t, got.Equal(TUnknown()), "got %s", got)
			require.Equal(t, c.want, errs.Messages())
		})
	}
}

func TestOpaqueReadsWholeValue(t *testing.T) {
	scope := &Scope{Inputs: []ObjectField{
		{Name: "blob", Type: TOpaque()},
		{Name: "tags", Type: TMap(TOpaque())},
	}}
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "input.blob"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TOpaque()), "got %s", got)
	got = Infer(parseExpr(t, "input.tags['a']"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TOpaque()), "got %s", got)
	assert.Empty(t, errs.Errors())
}

func TestOpaqueOperandsRejected(t *testing.T) {
	scope := &Scope{Inputs: []ObjectField{{Name: "blob", Type: TOpaque()}}}
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{"plus", "input.blob + 1",
			[]string{"+: operand must be a number or a string, got opaque"}},
		{"minus", "1 - input.blob",
			[]string{"-: operand must be a number, got opaque"}},
		{"and", "input.blob && true",
			[]string{"&&: operand must be a boolean, got opaque"}},
		{"not", "!input.blob",
			[]string{"!: operand must be a boolean, got opaque"}},
		{"negate", "-input.blob",
			[]string{"-: operand must be a number, got opaque"}},
		{"ordering", "input.blob < 3",
			[]string{"<: operand must be a number or a string, got opaque"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := lang.NewErrorList(0)
			Infer(parseExpr(t, c.src), TUnknown(), scope, errs)
			require.Equal(t, c.want, errs.Messages())
		})
	}
}

func TestOpaqueNullTestsAndCoalesceStayLegal(t *testing.T) {
	scope := &Scope{Inputs: []ObjectField{{Name: "blob", Type: TOpaque()}}}
	errs := lang.NewErrorList(0)

	got := Infer(parseExpr(t, "input.blob == null"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TBoolean()), "got %s", got)

	got = Infer(parseExpr(t, "input.blob != 'x'"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TBoolean()), "got %s", got)

	// An opaque value includes null, so a fallback is dischargeable;
	// the result may be the opaque side, so the join is opaque.
	got = Infer(parseExpr(t, "input.blob ?? {}"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TOpaque()), "got %s", got)

	got = Infer(parseExpr(t, "if input.blob == null then {} else input.blob"),
		TUnknown(), scope, errs)
	assert.True(t, got.Equal(TOpaque()), "got %s", got)

	assert.Empty(t, errs.Errors())
}

func TestCheckTeachesOpaqueMismatch(t *testing.T) {
	scope := &Scope{Inputs: []ObjectField{{Name: "blob", Type: TOpaque()}}}

	errs := lang.NewErrorList(0)
	Check(parseExpr(t, "input.blob"), TString(), scope, errs)
	require.Equal(t,
		[]string{"type mismatch: expected string, got opaque; " +
			"pass it as JSON text with @core.to-json(x), " +
			"or declare the value's type where it enters"},
		errs.Messages())

	errs = lang.NewErrorList(0)
	Check(parseExpr(t, "input.blob"), TInteger(), scope, errs)
	require.Equal(t,
		[]string{"type mismatch: expected integer, got opaque; " +
			"declare the value's type where it enters"},
		errs.Messages())

	// Opaque slots remain the legal home for an opaque value.
	errs = lang.NewErrorList(0)
	Check(parseExpr(t, "input.blob"), TOpaque(), scope, errs)
	Check(parseExpr(t, "input.blob"), TOptional(TOpaque()), scope, errs)
	assert.Empty(t, errs.Errors())
}

func TestInferInfixOperators(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	tests := []struct {
		src  string
		want Type
	}{
		{"1 == 2", TBoolean()},
		{"'a' != 'b'", TBoolean()},
		{"true && false", TBoolean()},
		{"1 + 2", TInteger()},
		{"1 + 2.0", TNumber()},
		{"'a' + 'b'", TString()},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			got := Infer(parseExpr(t, tt.src), TUnknown(), scope, errs)
			assert.True(t, got.Equal(tt.want), "got %s want %s", got, tt.want)
		})
	}
	assert.Empty(t, errs.Errors())
}

func TestInferUnknownSkipsCheck(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	got := Check(parseExpr(t, "'hi'"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TString()))
	assert.Empty(t, errs.Errors())
}

func TestInferOperandErrors(t *testing.T) {
	scope := &Scope{
		Inputs: []ObjectField{
			{Name: "count", Type: TInteger()},
		},
	}
	tests := []struct {
		src      string
		wantErrs []string
	}{
		{"true && 1", []string{"&&: operand must be a boolean, got integer"}},
		{"1 || false", []string{"||: operand must be a boolean, got integer"}},
		{"!5", []string{"!: operand must be a boolean, got integer"}},
		{"-'a'", []string{"-: operand must be a number, got string"}},
		{"'a' - 'b'", []string{
			"-: operand must be a number, got string",
			"-: operand must be a number, got string",
		}},
		{"2 * true", []string{"*: operand must be a number, got boolean"}},
		{"null / 2", []string{"/: operand must be a number, got null"}},
		{"'a' + true", []string{"+: operand must be a number or a string, got boolean"}},
		{"'a' + 1", []string{
			"+: operands must both be numbers or both be strings, got string and integer",
		}},
		{"1 + 'a'", []string{
			"+: operands must both be numbers or both be strings, got integer and string",
		}},
		{"1 < 'a'", []string{
			"<: operands must both be numbers or both be strings, got integer and string",
		}},
		{"true < 1", []string{"<: operand must be a number or a string, got boolean"}},
		{"[1] >= 2", []string{">=: operand must be a number or a string, got list(integer)"}},
		{"1 < 2 < 3", []string{"<: comparisons do not chain; combine two comparisons with &&"}},
		{"1 == 2 == 3", []string{"==: comparisons do not chain; combine two comparisons with &&"}},
		{"1 == 'a'", []string{"==: comparing integer with string is always false"}},
		{"1 != 'a'", []string{"!=: comparing integer with string is always true"}},
		{"true == 1", []string{"==: comparing boolean with integer is always false"}},
		{"[1] == 'a'", []string{"==: comparing list(integer) with string is always false"}},
		{"input.count + 'a'", []string{
			"+: operands must both be numbers or both be strings, got integer and string",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			errs := lang.NewErrorList(0)
			Infer(parseExpr(t, tt.src), TUnknown(), scope, errs)
			require.Equal(t, tt.wantErrs, errs.Messages())
		})
	}
}

func TestInferOperandLeniency(t *testing.T) {
	scope := &Scope{
		Inputs: []ObjectField{
			{Name: "count", Type: TInteger()},
			{Name: "maybe", Type: TString(), Optional: true},
			{Name: "opt-count", Type: TInteger(), Optional: true},
		},
	}
	tests := []string{
		"input.count + 1",
		"input.nope + 1",
		"if input.opt-count != null then input.opt-count + 1 else 0",
		"input.maybe == null",
		"null == input.maybe",
		"input.count == null",
		"1 == 1.0",
		"1.5 > input.count",
		"input.maybe != 'a'",
		"'a' + input.nope",
	}
	for _, src := range tests {
		t.Run(src, func(t *testing.T) {
			errs := lang.NewErrorList(0)
			Infer(parseExpr(t, src), TUnknown(), scope, errs)
			assert.Empty(t, errs.Errors())
		})
	}
}

// An operand that may be null is the same deferred dereference as a
// navigation: the operators reject it until a null test discharges
// it.
func TestInferOperandsRejectOptionals(t *testing.T) {
	scope := &Scope{
		Inputs: []ObjectField{
			{Name: "opt-count", Type: TInteger(), Optional: true},
		},
	}
	tests := []struct {
		src  string
		want []string
	}{
		{"input.opt-count + 1", []string{
			"+: operand must be a number or a string, got optional(integer)",
		}},
		{"-input.opt-count", []string{
			"-: operand must be a number, got optional(integer)",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			errs := lang.NewErrorList(0)
			Infer(parseExpr(t, tt.src), TUnknown(), scope, errs)
			require.Equal(t, tt.want, errs.Messages())
		})
	}
}

func TestInferPlusPartialString(t *testing.T) {
	scope := &Scope{}
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "'a' + input.nope"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TString()), "got %s", got)
	assert.Empty(t, errs.Errors())
}

func TestCheckCompositeTargets(t *testing.T) {
	scope := &Scope{
		Inputs: []ObjectField{
			{Name: "ports", Type: TList(TInteger())},
			{Name: "tags", Type: TMap(TString())},
			{Name: "cfg", Type: TObject([]ObjectField{
				{Name: "host", Type: TString()},
			})},
			{Name: "pair", Type: TTuple([]Type{TInteger(), TInteger()})},
		},
	}
	tests := []struct {
		name     string
		src      string
		target   Type
		wantErrs []string
	}{
		{
			name:   "list element mismatch",
			src:    "input.ports",
			target: TList(TString()),
			wantErrs: []string{
				"type mismatch: expected list(string), got list(integer)",
			},
		},
		{
			name:   "map element mismatch",
			src:    "input.tags",
			target: TMap(TInteger()),
			wantErrs: []string{
				"type mismatch: expected map(integer), got map(string)",
			},
		},
		{
			name: "object missing required field",
			src:  "input.cfg",
			target: TObject([]ObjectField{
				{Name: "host", Type: TString()},
				{Name: "port", Type: TInteger()},
			}),
			wantErrs: []string{
				"type mismatch: expected object({ host: string  port: integer }), " +
					"got object({ host: string })",
			},
		},
		{
			name:     "atom literal against list target",
			src:      "'a'",
			target:   TList(TString()),
			wantErrs: []string{"type mismatch: expected list(string), got string"},
		},
		{
			name:   "conditional of references against list target",
			src:    "if true then input.ports else input.ports",
			target: TList(TString()),
			wantErrs: []string{
				"type mismatch: expected list(string), got list(integer)",
			},
		},
		{
			name:   "list comprehension against map target",
			src:    "[ for p in input.ports : p ]",
			target: TMap(TString()),
			wantErrs: []string{
				"type mismatch: expected map(string), got list(integer)",
			},
		},
		{name: "matching list reference", src: "input.ports", target: TList(TInteger())},
		{name: "widening into any elements", src: "input.ports", target: TList(TOpaque())},
		{name: "tuple into list", src: "input.pair", target: TList(TInteger())},
		{name: "object into map", src: "input.cfg", target: TMap(TString())},
		{
			name:   "literal still enforced at elements only",
			src:    "['a', 5]",
			target: TList(TString()),
			wantErrs: []string{
				"type mismatch: expected string, got integer",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := lang.NewErrorList(0)
			Check(parseExpr(t, tt.src), tt.target, scope, errs)
			require.Equal(t, tt.wantErrs, errs.Messages())
		})
	}
}

func TestInferIndexSegments(t *testing.T) {
	scope := &Scope{
		Inputs: []ObjectField{
			{Name: "ports", Type: TList(TInteger())},
			{Name: "tags", Type: TMap(TString())},
			{Name: "pair", Type: TTuple([]Type{TString(), TInteger()})},
			{Name: "cfg", Type: TObject([]ObjectField{
				{Name: "host", Type: TString()},
			})},
			{Name: "name", Type: TString()},
			{Name: "count", Type: TInteger()},
			{Name: "anything", Type: TOpaque()},
		},
	}
	unknown := TUnknown()
	tests := []struct {
		src      string
		want     Type
		wantErrs []string
	}{
		{src: "input.tags[0]", want: TString(), wantErrs: []string{
			"type mismatch: expected string, got integer",
		}},
		{src: "input.ports['a']", want: TInteger(), wantErrs: []string{
			"type mismatch: expected integer, got string",
		}},
		{src: "input.pair[5]", want: unknown, wantErrs: []string{
			"index 5 out of range for tuple(string, integer)",
		}},
		{src: "input.pair['a']", want: unknown, wantErrs: []string{
			"type mismatch: expected integer, got string",
		}},
		{src: "input.cfg[0]", want: unknown, wantErrs: []string{
			"type mismatch: expected string, got integer",
		}},
		{src: "input.cfg['bogus']", want: unknown, wantErrs: []string{
			`unknown field "bogus" on object({ host: string })`,
		}},
		{src: "input.name[0]", want: unknown, wantErrs: []string{
			"cannot index into string",
		}},
		{src: "input.count[0]", want: unknown, wantErrs: []string{
			"cannot index into integer",
		}},
		{src: "input.anything[0]", want: unknown, wantErrs: []string{
			"input.anything is opaque; declare its type to index into it",
		}},
		{src: "input.ports[1 + 1]", want: TInteger()},
		{src: "input.pair[0]", want: TString()},
		{src: "input.pair[1]", want: TInteger()},
		{src: "input.cfg['host']", want: TString()},
		{src: "input.tags[input.name]", want: TString()},
		{src: "[ for i, p in input.ports : input.ports[i] ]", want: TList(TInteger())},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			errs := lang.NewErrorList(0)
			got := Infer(parseExpr(t, tt.src), TUnknown(), scope, errs)
			assert.True(t, got.Equal(tt.want), "got %s want %s", got, tt.want)
			require.Equal(t, tt.wantErrs, errs.Messages())
		})
	}
}

func TestCheckMapComprehensionValueTarget(t *testing.T) {
	scope := &Scope{
		Inputs: []ObjectField{
			{Name: "tags", Type: TMap(TString())},
		},
	}
	errs := lang.NewErrorList(0)
	got := Check(
		parseExpr(t, "{ for k, v in input.tags : k => 1 }"),
		TMap(TString()), scope, errs,
	)
	assert.True(t, got.Equal(TMap(TString())), "got %s", got)
	require.Len(t, errs.Errors(), 1)
	assert.Contains(t, errs.Errors()[0].Msg, "expected string, got integer")
}
