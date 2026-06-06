package typecheck

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func narrowScope() *Scope {
	return &Scope{
		Inputs: []ObjectField{
			{Name: "x", Type: TString(), Optional: true},
			{Name: "y", Type: TString()},
			{Name: "xs", Type: TList(TOptional(TString()))},
			{Name: "tls", Type: TObject([]ObjectField{
				{Name: "port", Type: TInteger()},
			}), Optional: true},
			{Name: "subnets", Type: TList(TObject([]ObjectField{
				{Name: "cert", Type: TString(), Optional: true},
			}))},
		},
	}
}

func TestNullFacts(t *testing.T) {
	tlsObject := TObject([]ObjectField{{Name: "port", Type: TInteger()}})
	tests := []struct {
		src       string
		whenTrue  map[string]Type
		whenFalse map[string]Type
	}{
		{
			src:       "var.x != null",
			whenTrue:  map[string]Type{"var.x": TString()},
			whenFalse: map[string]Type{"var.x": TNull()},
		},
		{
			src:       "var.x == null",
			whenTrue:  map[string]Type{"var.x": TNull()},
			whenFalse: map[string]Type{"var.x": TString()},
		},
		{
			src:       "null != var.x",
			whenTrue:  map[string]Type{"var.x": TString()},
			whenFalse: map[string]Type{"var.x": TNull()},
		},
		{
			src:       "!(var.x == null)",
			whenTrue:  map[string]Type{"var.x": TString()},
			whenFalse: map[string]Type{"var.x": TNull()},
		},
		{
			src: "var.x != null && var.tls != null",
			whenTrue: map[string]Type{
				"var.x":   TString(),
				"var.tls": tlsObject,
			},
		},
		{src: "var.x == var.y"},
		{src: "var.xs[0] != null"},
		{src: "var.x != null || var.tls != null"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			whenTrue, whenFalse := nullFacts(parseExpr(t, tt.src), narrowScope())
			require.Equal(t, tt.whenTrue, whenTrue)
			require.Equal(t, tt.whenFalse, whenFalse)
		})
	}
}

func TestNarrowedLookupPrefixes(t *testing.T) {
	tlsObject := TObject([]ObjectField{{Name: "port", Type: TInteger()}})
	scope := &Scope{Narrowed: map[string]Type{"var.tls": tlsObject}}

	dp := parseExpr(t, "var.tls.port").(*lang.DotPath)
	got, rest, ok := narrowedLookup(scope, dp)
	require.True(t, ok)
	assert.True(t, got.Equal(tlsObject), "got %s", got)
	require.Len(t, rest, 1)
	require.Equal(t, "port", rest[0].Name)

	dp = parseExpr(t, "var.tls[0].port").(*lang.DotPath)
	got, rest, ok = narrowedLookup(scope, dp)
	require.True(t, ok, "an index past the narrowed prefix still matches the prefix")
	assert.True(t, got.Equal(tlsObject), "got %s", got)
	require.Len(t, rest, 2)

	dp = parseExpr(t, "var.other.port").(*lang.DotPath)
	_, _, ok = narrowedLookup(scope, dp)
	require.False(t, ok)
}

// The error message's own recipe: an optional discharged by a null
// test interpolates without complaint, in either branch order. The
// control in TestNarrowDoesNotInvent proves the same slot complains
// without the test.
func TestNarrowConditionalDischargesSlot(t *testing.T) {
	for _, src := range []string{
		`$'a-{{ if var.x == null then '-' else var.x }}'`,
		`$'a-{{ if var.x != null then var.x else '-' }}'`,
		`$'a-{{ if !(var.x == null) then var.x else '-' }}'`,
	} {
		t.Run(src, func(t *testing.T) {
			errs := lang.NewErrorList(0)
			got := Infer(parseExpr(t, src), TUnknown(), narrowScope(), errs)
			assert.True(t, got.Equal(TString()), "got %s", got)
			require.Equal(t, []string(nil), errorMessages(errs))
		})
	}
}

// The branch type itself proves the narrowing: without it the joins
// would produce optional(string).
func TestNarrowConditionalJoinsToInner(t *testing.T) {
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "if var.x != null then var.x else 'd'"),
		TUnknown(), narrowScope(), errs)
	assert.True(t, got.Equal(TString()), "got %s", got)
	require.Equal(t, []string(nil), errorMessages(errs))
}

func TestNarrowThenBranchSeesNull(t *testing.T) {
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "if var.x == null then var.x else var.x"),
		TUnknown(), narrowScope(), errs)
	assert.True(t, got.Equal(TOptional(TString())), "got %s", got)
	require.Equal(t, []string(nil), errorMessages(errs))
}

// Each conjunct's narrowing is visible in the branch type it decides:
// var.x reads string, and var.tls reads the bare object, where the
// un-narrowed joins would both wrap in optional().
func TestNarrowConjunctionFacts(t *testing.T) {
	tlsObject := TObject([]ObjectField{{Name: "port", Type: TInteger()}})
	errs := lang.NewErrorList(0)

	got := Infer(parseExpr(t,
		"if var.x != null && var.tls != null then var.x else 'd'"),
		TUnknown(), narrowScope(), errs)
	assert.True(t, got.Equal(TString()), "left conjunct narrows, got %s", got)

	got = Infer(parseExpr(t,
		"if var.x != null && var.tls != null then var.tls else { port: 0 }"),
		TUnknown(), narrowScope(), errs)
	assert.True(t, got.Equal(tlsObject), "right conjunct narrows, got %s", got)

	require.Equal(t, []string(nil), errorMessages(errs))
}

// The right operand of && only evaluates when the left held, so a
// null test on the left narrows the right; || mirrors with the test
// failing. The control in TestNarrowDoesNotInvent proves the same
// slot complains under a guard that proves nothing.
func TestNarrowShortCircuitOperands(t *testing.T) {
	for _, src := range []string{
		`var.x != null && $'{{var.x}}' == 'a'`,
		`var.x == null || $'{{var.x}}' == 'a'`,
	} {
		t.Run(src, func(t *testing.T) {
			errs := lang.NewErrorList(0)
			Infer(parseExpr(t, src), TUnknown(), narrowScope(), errs)
			require.Equal(t, []string(nil), errorMessages(errs))
		})
	}
}

// The element type proves the filter narrowed the value expression:
// without it the comprehension would produce list(optional(string)).
func TestNarrowComprehensionFilter(t *testing.T) {
	errs := lang.NewErrorList(0)
	got := Infer(
		parseExpr(t, `[ for s in var.subnets : s.cert when s.cert != null ]`),
		TUnknown(), narrowScope(), errs)
	assert.True(t, got.Equal(TList(TString())), "got %s", got)
	require.Equal(t, []string(nil), errorMessages(errs))
}

func strictScope() *Scope {
	s := narrowScope()
	s.Inputs = append(s.Inputs,
		ObjectField{Name: "maybe-list", Type: TList(TString()), Optional: true},
	)
	return s
}

// Navigating into a value that may be null is the deferred null
// dereference; the checker wants the test first. A narrowed prefix
// reads straight through.
func TestStrictOptionalNavigation(t *testing.T) {
	tests := []struct {
		src      string
		want     Type
		wantErrs []string
	}{
		{
			src:  "var.tls.port",
			want: TUnknown(),
			wantErrs: []string{
				"value may be null; test it first, like " +
					"if x != null then x.port else <fallback> " +
					"(got optional(object({ port: integer })))",
			},
		},
		{
			src:  "if var.tls != null then var.tls.port else 0",
			want: TInteger(),
		},
		{
			src:  "var.maybe-list[0]",
			want: TUnknown(),
			wantErrs: []string{
				"value may be null; test it first, like " +
					"if xs != null then xs[0] else <fallback> " +
					"(got optional(list(string)))",
			},
		},
		{
			src:  "var.maybe-list[*]",
			want: TUnknown(),
			wantErrs: []string{
				"value may be null; test it first, like " +
					"if xs != null then xs[*].field else [] " +
					"(got optional(list(string)))",
			},
		},
		{
			src:  "[ for s in var.maybe-list : s ]",
			want: TList(TString()),
			wantErrs: []string{
				"comprehension source may be null; test it first, like " +
					"if xs == null then [] else [ for x in xs : x ] " +
					"(got optional(list(string)))",
			},
		},
		{
			src:  "if var.maybe-list == null then [] else [ for s in var.maybe-list : s ]",
			want: TList(TString()),
		},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			errs := lang.NewErrorList(0)
			got := Infer(parseExpr(t, tt.src), TUnknown(), strictScope(), errs)
			assert.True(t, got.Equal(tt.want), "got %s want %s", got, tt.want)
			require.Equal(t, tt.wantErrs, errorMessages(errs))
		})
	}
}

// No narrowing without a null test, and none through an index: the
// slot complaints stay.
func TestNarrowDoesNotInvent(t *testing.T) {
	for _, src := range []string{
		`if var.x == var.y then $'{{var.x}}' else '-'`,
		`var.xs[0] != null && $'{{var.xs[0]}}' == 'a'`,
	} {
		t.Run(src, func(t *testing.T) {
			errs := lang.NewErrorList(0)
			Infer(parseExpr(t, src), TUnknown(), narrowScope(), errs)
			require.Equal(t, []string{
				"interpolation slot may be null; test it first, like " +
					"{{ if x == null then '-' else x }} (got optional(string))",
			}, errorMessages(errs))
		})
	}
}
