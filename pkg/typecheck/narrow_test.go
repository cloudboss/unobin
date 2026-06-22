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
			src:       "input.x != null",
			whenTrue:  map[string]Type{"input.x": TString()},
			whenFalse: map[string]Type{"input.x": TNull()},
		},
		{
			src:       "input.x == null",
			whenTrue:  map[string]Type{"input.x": TNull()},
			whenFalse: map[string]Type{"input.x": TString()},
		},
		{
			src:       "null != input.x",
			whenTrue:  map[string]Type{"input.x": TString()},
			whenFalse: map[string]Type{"input.x": TNull()},
		},
		{
			src:       "!(input.x == null)",
			whenTrue:  map[string]Type{"input.x": TString()},
			whenFalse: map[string]Type{"input.x": TNull()},
		},
		{
			src: "input.x != null && input.tls != null",
			whenTrue: map[string]Type{
				"input.x":   TString(),
				"input.tls": tlsObject,
			},
		},
		{src: "input.x == input.y"},
		{src: "input.xs[0] != null"},
		{src: "input.x != null || input.tls != null"},
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
	scope := &Scope{Narrowed: map[string]Type{"input.tls": tlsObject}}

	dp := parseExpr(t, "input.tls.port").(*lang.DotPath)
	got, rest, at, ok := narrowedLookup(scope, dp)
	require.True(t, ok)
	assert.True(t, got.Equal(tlsObject), "got %s", got)
	require.Len(t, rest, 1)
	require.Equal(t, "port", rest[0].Name)
	require.Equal(t, "input.tls", at)

	dp = parseExpr(t, "input.tls[0].port").(*lang.DotPath)
	got, rest, _, ok = narrowedLookup(scope, dp)
	require.True(t, ok, "an index past the narrowed prefix still matches the prefix")
	assert.True(t, got.Equal(tlsObject), "got %s", got)
	require.Len(t, rest, 2)

	dp = parseExpr(t, "input.other.port").(*lang.DotPath)
	_, _, _, ok = narrowedLookup(scope, dp)
	require.False(t, ok)
}

// The error message's own recipe: an optional discharged by a null
// test interpolates without complaint, in either branch order. The
// control in TestNarrowDoesNotInvent proves the same slot complains
// without the test.
func TestNarrowConditionalDischargesSlot(t *testing.T) {
	for _, src := range []string{
		`$'a-{{ if input.x == null then '-' else input.x }}'`,
		`$'a-{{ if input.x != null then input.x else '-' }}'`,
		`$'a-{{ if !(input.x == null) then input.x else '-' }}'`,
	} {
		t.Run(src, func(t *testing.T) {
			errs := lang.NewErrorList(0)
			got := Infer(parseExpr(t, src), TUnknown(), narrowScope(), errs)
			assert.True(t, got.Equal(TString()), "got %s", got)
			require.Equal(t, []string(nil), errs.Messages())
		})
	}
}

// The branch type itself proves the narrowing: without it the joins
// would produce optional(string).
func TestNarrowConditionalJoinsToInner(t *testing.T) {
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "if input.x != null then input.x else 'd'"),
		TUnknown(), narrowScope(), errs)
	assert.True(t, got.Equal(TString()), "got %s", got)
	require.Equal(t, []string(nil), errs.Messages())
}

func TestNarrowThenBranchSeesNull(t *testing.T) {
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "if input.x == null then input.x else input.x"),
		TUnknown(), narrowScope(), errs)
	assert.True(t, got.Equal(TOptional(TString())), "got %s", got)
	require.Equal(t, []string(nil), errs.Messages())
}

// Each conjunct's narrowing is visible in the branch type it decides:
// input.x reads string, and input.tls reads the bare object, where the
// un-narrowed joins would both wrap in optional().
func TestNarrowConjunctionFacts(t *testing.T) {
	tlsObject := TObject([]ObjectField{{Name: "port", Type: TInteger()}})
	errs := lang.NewErrorList(0)

	got := Infer(parseExpr(t,
		"if input.x != null && input.tls != null then input.x else 'd'"),
		TUnknown(), narrowScope(), errs)
	assert.True(t, got.Equal(TString()), "left conjunct narrows, got %s", got)

	got = Infer(parseExpr(t,
		"if input.x != null && input.tls != null then input.tls else { port: 0 }"),
		TUnknown(), narrowScope(), errs)
	assert.True(t, got.Equal(tlsObject), "right conjunct narrows, got %s", got)

	require.Equal(t, []string(nil), errs.Messages())
}

// The right operand of && only evaluates when the left held, so a
// null test on the left narrows the right; || mirrors with the test
// failing. The control in TestNarrowDoesNotInvent proves the same
// slot complains under a guard that proves nothing.
func TestNarrowShortCircuitOperands(t *testing.T) {
	for _, src := range []string{
		`input.x != null && $'{{input.x}}' == 'a'`,
		`input.x == null || $'{{input.x}}' == 'a'`,
	} {
		t.Run(src, func(t *testing.T) {
			errs := lang.NewErrorList(0)
			Infer(parseExpr(t, src), TUnknown(), narrowScope(), errs)
			require.Equal(t, []string(nil), errs.Messages())
		})
	}
}

// The element type proves the filter narrowed the value expression:
// without it the comprehension would produce list(optional(string)).
func TestNarrowComprehensionFilter(t *testing.T) {
	errs := lang.NewErrorList(0)
	got := Infer(
		parseExpr(t, `[ for s in input.subnets : s.cert when s.cert != null ]`),
		TUnknown(), narrowScope(), errs)
	assert.True(t, got.Equal(TList(TString())), "got %s", got)
	require.Equal(t, []string(nil), errs.Messages())
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
		{src: "input.tls?.port", want: TOptional(TInteger())},
		{src: "input.cfg?.db?.host", want: TOptional(TString())},
		{
			src:  "input.cfg?.db.host",
			want: TUnknown(),
			wantErrs: []string{
				"input.cfg?.db may be null; read it with input.cfg?.db?.host, " +
					"or test it first (got optional(object({ host: string })))",
			},
		},
		{
			src:  "input.y?.anything",
			want: TUnknown(),
			wantErrs: []string{
				"input.y is never null; write input.y.anything (got string)",
			},
		},
		{
			src:  "if input.tls != null then input.tls?.port else 0",
			want: TInteger(),
			wantErrs: []string{
				"input.tls is never null; write input.tls.port (got object({ port: integer }))",
			},
		},
		{
			src:  "input?.y",
			want: TUnknown(),
			wantErrs: []string{
				"input is never null; write input.y",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			errs := lang.NewErrorList(0)
			got := Infer(parseExpr(t, tt.src), TUnknown(), strictScope(), errs)
			assert.True(t, got.Equal(tt.want), "got %s want %s", got, tt.want)
			require.Equal(t, tt.wantErrs, errs.Messages())
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
		{src: "input.x ?? 'd'", want: TString()},
		{src: "input.opt-count ?? 0", want: TInteger()},
		{src: "input.opt-count ?? 1.5", want: TNumber()},
		{src: "input.opt-flag ?? false", want: TBoolean()},
		{src: "input.cfg?.db?.host ?? 'none'", want: TString()},
		{src: "input.tls?.port ?? 0", want: TInteger()},
		{src: "input.x ?? null", want: TOptional(TString())},
		{src: "input.x ?? input.x", want: TOptional(TString())},
		{src: "input.x ?? input.x ?? 'd'", want: TString()},
		{src: "null ?? 'd'", want: TString()},
		{src: "input.maybe-list ?? []", want: TList(TString())},
		{src: "input.opt-tags ?? {}", want: TMap(TString())},
		{src: "input.tls ?? { port: 1 }", want: TObject([]ObjectField{
			{Name: "port", Type: TInteger()},
		})},
		{src: "input.nope ?? 'd'", want: TString()},
		{src: "$'{{ input.x ?? '-' }}'", want: TString()},
		{src: "$'{{ input.cfg?.db?.host ?? '-' }}-{{ input.opt-count ?? 0 }}'", want: TString()},
		{
			src:  "input.y ?? 'd'",
			want: TString(),
			wantErrs: []string{
				"left of ?? is never null; write it without the fallback (got string)",
			},
		},
		{
			src:  "input.tls ?? { port: 1 } ?? { port: 2 }",
			want: TObject([]ObjectField{{Name: "port", Type: TInteger()}}),
			wantErrs: []string{
				"left of ?? is never null; write it without the fallback " +
					"(got object({ port: integer }))",
			},
		},
		{
			src:  "input.x ?? 5",
			want: TUnknown(),
			wantErrs: []string{
				"?? sides have different types: string and integer",
			},
		},
		{
			src:  "input.opt-count ?? 'd'",
			want: TUnknown(),
			wantErrs: []string{
				"?? sides have different types: integer and string",
			},
		},
		{
			src:  "input.maybe-list ?? {}",
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
			require.Equal(t, tt.wantErrs, errs.Messages())
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
		{src: "input.opt-flag ?? input.y == 'a'", want: TBoolean()},
		{src: "input.opt-flag ?? true || false", want: TBoolean()},
		{src: "(input.x ?? 'd') == 'a'", want: TBoolean()},
		{src: "input.opt-count ?? 1 + 2", want: TInteger()},
		{
			src:  "input.x ?? 'd' == 'a'",
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
			require.Equal(t, tt.wantErrs, errs.Messages())
		})
	}
}

// In constraint scope a guard means the same thing it means anywhere
// else; the lenient dot just makes it rarely necessary.
func TestGuardedNavigationUnderMissingAsNull(t *testing.T) {
	scope := strictScope()
	scope.MissingAsNull = true
	errs := lang.NewErrorList(0)
	got := Infer(parseExpr(t, "input.cfg?.db?.host"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TOptional(TString())), "got %s", got)
	require.Equal(t, []string(nil), errs.Messages())
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
			src:  "input.tls.port",
			want: TUnknown(),
			wantErrs: []string{
				"input.tls may be null; read it with input.tls?.port, or test it first " +
					"(got optional(object({ port: integer })))",
			},
		},
		{
			src:  "if input.tls != null then input.tls.port else 0",
			want: TInteger(),
		},
		{
			src:  "input.maybe-list[0]",
			want: TUnknown(),
			wantErrs: []string{
				"input.maybe-list may be null; test it first, like " +
					"if input.maybe-list != null then input.maybe-list[0] else <fallback> " +
					"(got optional(list(string)))",
			},
		},
		{
			src:  "input.maybe-list[*]",
			want: TUnknown(),
			wantErrs: []string{
				"input.maybe-list may be null; test it first, like " +
					"if input.maybe-list != null then input.maybe-list[*]... else [] " +
					"(got optional(list(string)))",
			},
		},
		{
			src:  "[ for s in input.maybe-list : s ]",
			want: TList(TString()),
			wantErrs: []string{
				"comprehension source may be null; supply a fallback, like " +
					"xs ?? [] (got optional(list(string)))",
			},
		},
		{
			src:  "if input.maybe-list == null then [] else [ for s in input.maybe-list : s ]",
			want: TList(TString()),
		},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			errs := lang.NewErrorList(0)
			got := Infer(parseExpr(t, tt.src), TUnknown(), strictScope(), errs)
			assert.True(t, got.Equal(tt.want), "got %s want %s", got, tt.want)
			require.Equal(t, tt.wantErrs, errs.Messages())
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
		"{ number: 1, tls: input.maybe-tls }",
		"{ number: 1, tls: null }",
		"{ number: 1 }",
	} {
		t.Run(src, func(t *testing.T) {
			errs := lang.NewErrorList(0)
			Check(parseExpr(t, src), target, scope, errs)
			require.Equal(t, []string(nil), errs.Messages())
		})
	}

	errs := lang.NewErrorList(0)
	Check(parseExpr(t, "{ number: input.maybe-tls }"), target, scope, errs)
	require.Equal(t, []string{
		"type mismatch: expected integer, got optional(boolean)",
	}, errs.Messages())
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
	Check(parseExpr(t, "input.x"), TString(), scope, errs)
	require.Equal(t, []string{
		"type mismatch: expected string, got optional(string); " +
			"test it first, like if x != null then x else <fallback>",
	}, errs.Messages())

	errs = lang.NewErrorList(0)
	Check(parseExpr(t, "if input.x != null then input.x else 'd'"), TString(), scope, errs)
	require.Equal(t, []string(nil), errs.Messages())
}

// No narrowing without a null test, and none through an index: the
// slot complaints stay.
func TestNarrowDoesNotInvent(t *testing.T) {
	for _, src := range []string{
		`if input.x == input.y then $'{{input.x}}' else '-'`,
		`input.xs[0] != null && $'{{input.xs[0]}}' == 'a'`,
	} {
		t.Run(src, func(t *testing.T) {
			errs := lang.NewErrorList(0)
			Infer(parseExpr(t, src), TUnknown(), narrowScope(), errs)
			require.Equal(t, []string{
				"interpolation slot may be null; supply a fallback, like " +
					"{{ x ?? '-' }} (got optional(string))",
			}, errs.Messages())
		})
	}
}
