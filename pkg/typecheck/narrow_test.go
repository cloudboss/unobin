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
	got, rest, at, ok := narrowedLookup(scope, dp)
	require.True(t, ok)
	assert.True(t, got.Equal(tlsObject), "got %s", got)
	require.Len(t, rest, 1)
	require.Equal(t, "port", rest[0].Name)
	require.Equal(t, "var.tls", at)

	dp = parseExpr(t, "var.tls[0].port").(*lang.DotPath)
	got, rest, _, ok = narrowedLookup(scope, dp)
	require.True(t, ok, "an index past the narrowed prefix still matches the prefix")
	assert.True(t, got.Equal(tlsObject), "got %s", got)
	require.Len(t, rest, 2)

	dp = parseExpr(t, "var.other.port").(*lang.DotPath)
	_, _, _, ok = narrowedLookup(scope, dp)
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
		ObjectField{Name: "cfg", Type: TObject([]ObjectField{
			{Name: "db", Type: TObject([]ObjectField{
				{Name: "host", Type: TString()},
			}), Optional: true},
		}), Optional: true},
		ObjectField{Name: "opt-count", Type: TInteger(), Optional: true},
		ObjectField{Name: "opt-tags", Type: TMap(TString()), Optional: true},
		ObjectField{Name: "opt-flag", Type: TBoolean(), Optional: true},
	)
	return s
}

// Guarded navigation: ?. requires a possibly-null value, reads it
// when present, and makes the whole path optional. Every nullable
// level wears its own marker.
func TestGuardedNavigation(t *testing.T) {
	tests := []struct {
		src      string
		want     Type
		wantErrs []string
	}{
		{src: "var.tls?.port", want: TOptional(TInteger())},
		{src: "var.cfg?.db?.host", want: TOptional(TString())},
		{
			src:  "var.cfg?.db.host",
			want: TUnknown(),
			wantErrs: []string{
				"var.cfg?.db may be null; read it with var.cfg?.db?.host, " +
					"or test it first (got optional(object({ host: string })))",
			},
		},
		{
			src:  "var.y?.anything",
			want: TUnknown(),
			wantErrs: []string{
				"var.y is never null; write var.y.anything (got string)",
			},
		},
		{
			src:  "if var.tls != null then var.tls?.port else 0",
			want: TInteger(),
			wantErrs: []string{
				"var.tls is never null; write var.tls.port (got object({ port: integer }))",
			},
		},
		{
			src:  "var?.y",
			want: TUnknown(),
			wantErrs: []string{
				"var is never null; write var.y",
			},
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

// ?? supplies the fallback that lands a possibly-null value: the
// result joins the discharged left with the right.
func TestCoalesce(t *testing.T) {
	tests := []struct {
		src      string
		want     Type
		wantErrs []string
	}{
		{src: "var.x ?? 'd'", want: TString()},
		{src: "var.opt-count ?? 0", want: TInteger()},
		{src: "var.opt-count ?? 1.5", want: TNumber()},
		{src: "var.opt-flag ?? false", want: TBoolean()},
		{src: "var.cfg?.db?.host ?? 'none'", want: TString()},
		{src: "var.tls?.port ?? 0", want: TInteger()},
		{src: "var.x ?? null", want: TOptional(TString())},
		{src: "var.x ?? var.x", want: TOptional(TString())},
		{src: "var.x ?? var.x ?? 'd'", want: TString()},
		{src: "null ?? 'd'", want: TString()},
		{src: "var.maybe-list ?? []", want: TList(TString())},
		{src: "var.opt-tags ?? {}", want: TMap(TString())},
		{src: "var.tls ?? { port: 1 }", want: TObject([]ObjectField{
			{Name: "port", Type: TInteger()},
		})},
		{src: "var.nope ?? 'd'", want: TString()},
		{src: "$'{{ var.x ?? '-' }}'", want: TString()},
		{src: "$'{{ var.cfg?.db?.host ?? '-' }}-{{ var.opt-count ?? 0 }}'", want: TString()},
		{
			src:  "var.y ?? 'd'",
			want: TString(),
			wantErrs: []string{
				"left of ?? is never null; write it without the fallback (got string)",
			},
		},
		{
			src:  "var.tls ?? { port: 1 } ?? { port: 2 }",
			want: TObject([]ObjectField{{Name: "port", Type: TInteger()}}),
			wantErrs: []string{
				"left of ?? is never null; write it without the fallback " +
					"(got object({ port: integer }))",
			},
		},
		{
			src:  "var.x ?? 5",
			want: TUnknown(),
			wantErrs: []string{
				"?? sides have different types: string and integer",
			},
		},
		{
			src:  "var.opt-count ?? 'd'",
			want: TUnknown(),
			wantErrs: []string{
				"?? sides have different types: integer and string",
			},
		},
		{
			src:  "var.maybe-list ?? {}",
			want: TUnknown(),
			wantErrs: []string{
				"?? sides have different types: list(string) and object({  })",
			},
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

// ?? binds loosest: everything to its right down to the next ?? is
// the fallback, so a comparison or || on the right belongs to the
// fallback, and parentheses pull the coalesce inside.
func TestCoalescePrecedence(t *testing.T) {
	tests := []struct {
		src      string
		want     Type
		wantErrs []string
	}{
		{src: "var.opt-flag ?? var.y == 'a'", want: TBoolean()},
		{src: "var.opt-flag ?? true || false", want: TBoolean()},
		{src: "(var.x ?? 'd') == 'a'", want: TBoolean()},
		{src: "var.opt-count ?? 1 + 2", want: TInteger()},
		{
			src:  "var.x ?? 'd' == 'a'",
			want: TUnknown(),
			wantErrs: []string{
				"?? sides have different types: string and boolean",
			},
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

// In constraint scope a guard means the same thing it means anywhere
// else; the lenient dot just makes it rarely necessary.
func TestGuardedNavigationUnderMissingAsNull(t *testing.T) {
	scope := strictScope()
	scope.MissingAsNull = true
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "var.cfg?.db?.host"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TOptional(TString())), "got %s", got)
	require.Equal(t, []string(nil), errorMessages(errs))
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
				"var.tls may be null; read it with var.tls?.port, or test it first " +
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
				"var.maybe-list may be null; test it first, like " +
					"if var.maybe-list != null then var.maybe-list[0] else <fallback> " +
					"(got optional(list(string)))",
			},
		},
		{
			src:  "var.maybe-list[*]",
			want: TUnknown(),
			wantErrs: []string{
				"var.maybe-list may be null; test it first, like " +
					"if var.maybe-list != null then var.maybe-list[*]... else [] " +
					"(got optional(list(string)))",
			},
		},
		{
			src:  "[ for s in var.maybe-list : s ]",
			want: TList(TString()),
			wantErrs: []string{
				"comprehension source may be null; supply a fallback, like " +
					"xs ?? [] (got optional(list(string)))",
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

// An optional object field accepts an optional or null value: the
// field's absence and a null value mean the same thing to the
// decoder, so a possibly-null source is at home there.
func TestOptionalFieldsAcceptOptionalValues(t *testing.T) {
	scope := &Scope{
		Inputs: []ObjectField{
			{Name: "maybe-tls", Type: TBoolean(), Optional: true},
		},
	}
	target := TObject([]ObjectField{
		{Name: "number", Type: TInteger()},
		{Name: "tls", Type: TBoolean(), Optional: true},
	})
	for _, src := range []string{
		"{ number: 1, tls: var.maybe-tls }",
		"{ number: 1, tls: null }",
		"{ number: 1 }",
	} {
		t.Run(src, func(t *testing.T) {
			errs := lang.NewErrorList(0)
			Check(parseExpr(t, src), target, scope, errs)
			require.Equal(t, []string(nil), errorMessages(errs))
		})
	}

	errs := lang.NewErrorList(0)
	Check(parseExpr(t, "{ number: var.maybe-tls }"), target, scope, errs)
	require.Equal(t, []string{
		"type mismatch: expected integer, got optional(boolean)",
	}, errorMessages(errs))
}

func TestAssignableOptionalObjectFields(t *testing.T) {
	dst := TObject([]ObjectField{
		{Name: "tls", Type: TBoolean(), Optional: true},
	})
	src := TObject([]ObjectField{
		{Name: "tls", Type: TOptional(TBoolean())},
	})
	assert.True(t, Assignable(dst, src))
}

// The flip itself: a possibly-null value no longer flows into a slot
// that wants a value, and the error shows the test that fixes it.
func TestCheckRejectsOptionalIntoRequiredSlot(t *testing.T) {
	scope := narrowScope()
	errs := lang.NewErrorList(0)
	Check(parseExpr(t, "var.x"), TString(), scope, errs)
	require.Equal(t, []string{
		"type mismatch: expected string, got optional(string); " +
			"test it first, like if x != null then x else <fallback>",
	}, errorMessages(errs))

	errs = lang.NewErrorList(0)
	Check(parseExpr(t, "if var.x != null then var.x else 'd'"), TString(), scope, errs)
	require.Equal(t, []string(nil), errorMessages(errs))
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
				"interpolation slot may be null; supply a fallback, like " +
					"{{ x ?? '-' }} (got optional(string))",
			}, errorMessages(errs))
		})
	}
}
