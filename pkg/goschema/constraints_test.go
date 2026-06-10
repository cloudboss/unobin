package goschema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
)

const constraintLibrary = `package lib

import (
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "lib",
		Resources: map[string]runtime.ResourceRegistration{
			"thing": runtime.MakeResource[Thing, *ThingOutput](),
		},
	}
}

type Thing struct {
	Name    *string
	Kind    *string
	Port    *int
	Items   []Item
	Methods []string
}

type Item struct {
	A    *string
	B    *string
	Subs []Sub
	Tags []string
}

type Sub struct {
	C *string
	D *string
}

type ThingOutput struct {
	ID string
}
`

func readConstraintLibrary(t *testing.T, src string) (*runtime.LibrarySchema, []string, error) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "library.go"), []byte(src), 0o644))
	return Read(dir)
}

func TestReadFoldsConstantMessageConcatenation(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{"two parts", `"pick a name " + "or a kind"`, "pick a name or a kind"},
		{"three parts", `"a " + "b " + "c"`, "a b c"},
		{"parenthesized", `("never " + "split ") + "a message"`, "never split a message"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := constraintLibrary + `
func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(v.Name, v.Kind).Message(` + tt.message + `),
	}
}
`
			schema, warnings, err := readConstraintLibrary(t, src)
			require.NoError(t, err)
			require.Empty(t, warnings)
			require.Equal(t, []lang.ConstraintSpec{
				{
					Kind:    "exactly-one-of",
					Fields:  []string{"var.name", "var.kind"},
					Message: tt.want,
				},
			}, schema.Resources["thing"].Constraints)
		})
	}
}

func TestReadWarnsOnUnextractableConstraints(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		extra     string
		wantSpecs []lang.ConstraintSpec
		wantWarns []string
	}{
		{
			name: "non-literal message keeps the rule and warns",
			method: `func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(v.Name, v.Kind).Message(defaultMessage),
	}
}`,
			extra: `const defaultMessage = "pick one"`,
			wantSpecs: []lang.ConstraintSpec{
				{Kind: "exactly-one-of", Fields: []string{"var.name", "var.kind"}},
			},
			wantWarns: []string{
				"Thing: Message must be a string literal, got defaultMessage",
			},
		},
		{
			name: "named constant as a condition value",
			method: `func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.OneOf(v.Kind, "alpha", kindDefault)),
	}
}`,
			extra: `const kindDefault = "beta"`,
			wantWarns: []string{
				"Thing: a condition value must be a literal or a field, got kindDefault",
			},
		},
		{
			name: "converted literal as a condition value",
			method: `func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.Equals(v.Port, int64(8080))),
	}
}`,
			wantWarns: []string{
				"Thing: a condition value must be a literal or a field, got int64(8080)",
			},
		},
		{
			name: "body builds the list in a variable",
			method: `func (v Thing) Constraints() []constraint.Constraint {
	cs := []constraint.Constraint{
		constraint.ExactlyOneOf(v.Name, v.Kind),
	}
	return cs
}`,
			wantWarns: []string{
				"Thing: the Constraints method must be a single return of a constraint list",
			},
		},
		{
			name: "non-constructor element keeps the rest",
			method: `func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		shared,
		constraint.ExactlyOneOf(v.Name, v.Kind),
	}
}`,
			extra: `var shared = constraint.RequiredTogether(nil, nil)`,
			wantSpecs: []lang.ConstraintSpec{
				{Kind: "exactly-one-of", Fields: []string{"var.name", "var.kind"}},
			},
			wantWarns: []string{
				"Thing: a constraint must be a pkg/constraint constructor call, got shared",
			},
		},
		{
			name: "helper call element",
			method: `func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		helper(),
	}
}`,
			extra: `func helper() constraint.Constraint { return constraint.Constraint{} }`,
			wantWarns: []string{
				"Thing: a constraint must be a pkg/constraint constructor call, got helper()",
			},
		},
		{
			name: "unknown constructor",
			method: `func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Exactly(v.Name),
	}
}`,
			wantWarns: []string{
				`Thing: unsupported constraint constructor "Exactly"`,
			},
		},
		{
			name: "condition that is not a condition call",
			method: `func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(v.Name),
	}
}`,
			wantWarns: []string{
				"Thing: a condition must be a pkg/constraint condition call, got v.Name",
			},
		},
		{
			name: "unknown condition",
			method: `func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.Frobnicate(v.Name)),
	}
}`,
			wantWarns: []string{
				`Thing: unsupported condition "Frobnicate"`,
			},
		},
		{
			name: "OneOf without values",
			method: `func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.OneOf(v.Kind)),
	}
}`,
			wantWarns: []string{
				"Thing: OneOf needs a field and at least one value",
			},
		},
		{
			name: "predicate without conditions",
			method: `func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(),
	}
}`,
			wantWarns: []string{
				"Thing: a predicate needs at least one condition",
			},
		},
		{
			name: "Require chain not started by When",
			method: `func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Clause{}.Require(constraint.Present(v.Name)),
	}
}`,
			wantWarns: []string{
				"Thing: a Require chain must start with constraint.When, got constraint.Clause{}",
			},
		},
		{
			name: "ForEach body builds the list in a variable",
			method: `func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ForEach(v.Items, func(it Item) []constraint.Constraint {
			out := []constraint.Constraint{
				constraint.RequiredTogether(it.A, it.B),
			}
			return out
		}),
	}
}`,
			wantWarns: []string{
				"Thing: the ForEach body must be a single return of a constraint list",
			},
		},
		{
			name: "happy predicate stays silent",
			method: `func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(v.Name)).
			Require(constraint.Present(v.Kind)).
			Message("a name needs a kind"),
	}
}`,
			wantSpecs: []lang.ConstraintSpec{
				{
					Kind:    "predicate",
					When:    "(var.name != null)",
					Require: "(var.kind != null)",
					Message: "a name needs a kind",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := constraintLibrary + "\n" + tt.method + "\n"
			if tt.extra != "" {
				src += "\n" + tt.extra + "\n"
			}
			schema, warnings, err := readConstraintLibrary(t, src)
			require.NoError(t, err)
			require.Equal(t, tt.wantWarns, warnings)
			require.Equal(t, tt.wantSpecs, schema.Resources["thing"].Constraints)
		})
	}
}

// TestReadExtractsLengthConditions proves the three length conditions
// lower to @core.length with their null guards: a missing list passes
// MinItems and MaxItems, presence staying Present's job, while
// NotEmpty requires the field set and non-empty.
func TestReadExtractsLengthConditions(t *testing.T) {
	src := constraintLibrary + `
func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEmpty(v.Items)).
			Message("items must list at least one entry"),
		constraint.When(constraint.Present(v.Items)).
			Require(constraint.MinItems(v.Items, 1), constraint.MaxItems(v.Items, 5)),
	}
}
`
	schema, warnings, err := readConstraintLibrary(t, src)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, []lang.ConstraintSpec{
		{
			Kind:    "predicate",
			When:    "true",
			Require: "((var.items != null) && (@core.length(var.items) >= 1))",
			Message: "items must list at least one entry",
		},
		{
			Kind: "predicate",
			When: "(var.items != null)",
			Require: "(var.items == null || @core.length(var.items) >= 1)" +
				" && (var.items == null || @core.length(var.items) <= 5)",
		},
	}, schema.Resources["thing"].Constraints)
}

// TestReadLengthConditionsCheckWithoutImports proves the lowered
// conditions evaluate through the same no-import evaluator the
// compile-time and plan-time constraint checks use.
func TestReadLengthConditionsCheckWithoutImports(t *testing.T) {
	src := constraintLibrary + `
func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEmpty(v.Items)),
		constraint.When(constraint.Present(v.Items)).
			Require(constraint.MaxItems(v.Items, 2)),
	}
}
`
	schema, warnings, err := readConstraintLibrary(t, src)
	require.NoError(t, err)
	require.Empty(t, warnings)
	entries, perr := lang.ParseSpecs(schema.Resources["thing"].Constraints)
	require.Equal(t, 0, perr.Len(), "specs should parse: %v", perr.Err())

	check := func(values map[string]any) int {
		eval := func(e lang.Expr, binds []lang.EachBinding) (any, error) {
			ctx := &runtime.EvalContext{Vars: values, MissingAsNull: true}
			for _, b := range binds {
				if ctx.Each == nil {
					ctx.Each = map[string]lang.EachValue{}
				}
				ctx.Each[b.Name] = lang.EachValue{Key: b.Key, Value: b.Value}
			}
			return runtime.Eval(e, ctx)
		}
		return lang.CheckConstraintEntries(entries, values, eval, lang.DisplayNodeRelative).Len()
	}

	require.Equal(t, 0, check(map[string]any{"items": []any{"a"}}),
		"one item passes both rules")
	require.Equal(t, 1, check(map[string]any{"items": []any{}}),
		"an explicitly empty list fails NotEmpty")
	require.Equal(t, 1, check(map[string]any{}),
		"an absent list fails NotEmpty but passes MaxItems")
	require.Equal(t, 1, check(map[string]any{"items": []any{"a", "b", "c"}}),
		"three items fail MaxItems(2)")
}

// TestReadRejectsNonLiteralLengthCount proves the count argument must
// be a whole-number literal the schema can embed.
func TestReadRejectsNonLiteralLengthCount(t *testing.T) {
	src := constraintLibrary + `
func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.MinItems(v.Items, maxTargets)),
	}
}

const maxTargets = 5
`
	schema, warnings, err := readConstraintLibrary(t, src)
	require.NoError(t, err)
	require.Equal(t,
		[]string{"Thing: MinItems takes a field and a whole-number literal"},
		warnings)
	require.Empty(t, schema.Resources["thing"].Constraints)
}

// TestReadExtractsScalarForEach proves a ForEach over a list of
// scalars binds the body parameter to @each.value itself: conditions
// compare the element directly, with no field selection.
func TestReadExtractsScalarForEach(t *testing.T) {
	src := constraintLibrary + `
func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ForEach(v.Methods, func(m string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(m, "GET", "PUT")).
					Message("methods must be supported verbs"),
				constraint.Must(constraint.NotEmpty(m)),
			}
		}),
	}
}
`
	schema, warnings, err := readConstraintLibrary(t, src)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, []lang.ConstraintSpec{
		{
			Kind:    "predicate",
			When:    "true",
			Require: "(@each.value == 'GET' || @each.value == 'PUT')",
			Message: "methods must be supported verbs",
			ForEach: "var.methods",
		},
		{
			Kind:    "predicate",
			When:    "true",
			Require: "((@each.value != null) && (@core.length(@each.value) >= 1))",
			ForEach: "var.methods",
		},
	}, schema.Resources["thing"].Constraints)
}

// TestScalarForEachChecksAgainstValues proves the extracted specs
// iterate and judge real values through the constraint checker.
func TestScalarForEachChecksAgainstValues(t *testing.T) {
	src := constraintLibrary + `
func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ForEach(v.Methods, func(m string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(m, "GET", "PUT")).
					Message("methods must be supported verbs"),
			}
		}),
	}
}
`
	schema, _, err := readConstraintLibrary(t, src)
	require.NoError(t, err)
	entries, perr := lang.ParseSpecs(schema.Resources["thing"].Constraints)
	require.Equal(t, 0, perr.Len(), "specs should parse: %v", perr.Err())

	eval := func(values map[string]any) lang.ConstraintEvalFunc {
		return func(e lang.Expr, binds []lang.EachBinding) (any, error) {
			ctx := &runtime.EvalContext{Vars: values, MissingAsNull: true}
			for _, b := range binds {
				if ctx.Each == nil {
					ctx.Each = map[string]lang.EachValue{}
				}
				ctx.Each[b.Name] = lang.EachValue{Key: b.Key, Value: b.Value}
			}
			return runtime.Eval(e, ctx)
		}
	}

	good := map[string]any{"methods": []any{"GET", "PUT"}}
	ok := lang.CheckConstraintEntries(entries, good, eval(good), lang.DisplayNodeRelative)
	require.Equal(t, 0, ok.Len(), "supported verbs pass: %v", ok.Err())

	bad := map[string]any{"methods": []any{"GET", "DELETE"}}
	got := lang.CheckConstraintEntries(entries, bad, eval(bad), lang.DisplayNodeRelative)
	require.Equal(t, 1, got.Len(), "one violation expected: %v", got.Err())
	require.Contains(t, got.Err().Error(), "methods must be supported verbs (methods[1])")
}

// TestReadRejectsSetConstraintOverScalars proves a set kind inside a
// scalar ForEach is an error: with no element fields to relate, the
// rule cannot mean anything.
func TestReadRejectsSetConstraintOverScalars(t *testing.T) {
	src := constraintLibrary + `
func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ForEach(v.Methods, func(m string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.RequiredTogether(m, v.Name),
			}
		}),
	}
}
`
	_, _, err := readConstraintLibrary(t, src)
	require.Error(t, err)
	require.Contains(t, err.Error(), "scalar")
}

// TestReadExtractsNestedForEachSetConstraints proves an inner ForEach
// lowers its set constraints to fields splatting both lists, which the
// checker expands element by element at each level. A reference to the
// receiver still names a top-level field.
func TestReadExtractsNestedForEachSetConstraints(t *testing.T) {
	src := constraintLibrary + `
func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ForEach(v.Items, func(it Item) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.ForEach(it.Subs, func(s Sub) []constraint.Constraint {
					return []constraint.Constraint{
						constraint.ExactlyOneOf(s.C, s.D).Message("pick one"),
						constraint.RequiredWith(s.C, v.Name),
					}
				}),
			}
		}),
	}
}
`
	schema, warnings, err := readConstraintLibrary(t, src)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, []lang.ConstraintSpec{
		{
			Kind: "exactly-one-of",
			Fields: []string{
				"var.items[*].subs[*].c",
				"var.items[*].subs[*].d",
			},
			Message: "pick one",
		},
		{
			Kind: "required-with",
			Fields: []string{
				"var.items[*].subs[*].c",
				"var.name",
			},
		},
	}, schema.Resources["thing"].Constraints)
}

// TestReadExtractsChainedForEachPredicate proves a predicate inside a
// nested ForEach lowers to a chained @for-each: each Go parameter
// becomes a level binding, conditions reference the bindings, and an
// outer element stays reachable from the inner body.
func TestReadExtractsChainedForEachPredicate(t *testing.T) {
	src := constraintLibrary + `
func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ForEach(v.Items, func(it Item) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.ForEach(it.Subs, func(s Sub) []constraint.Constraint {
					return []constraint.Constraint{
						constraint.When(constraint.Present(s.C)).
							Require(constraint.Present(it.A)).
							Message("a sub with c needs its item's a"),
					}
				}),
			}
		}),
	}
}
`
	schema, warnings, err := readConstraintLibrary(t, src)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, []lang.ConstraintSpec{{
		Kind:    "predicate",
		When:    "(@s.value.c != null)",
		Require: "(@it.value.a != null)",
		Message: "a sub with c needs its item's a",
		ForEachLevels: []lang.ForEachSpecLevel{
			{Name: "@it", In: "var.items"},
			{Name: "@s", In: "@it.value.subs"},
		},
	}}, schema.Resources["thing"].Constraints)
}

// TestChainedForEachChecksAgainstValues proves the lowered chain runs
// through the real evaluator with both bindings live, naming a failure
// through both levels.
func TestChainedForEachChecksAgainstValues(t *testing.T) {
	src := constraintLibrary + `
func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ForEach(v.Items, func(it Item) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.ForEach(it.Subs, func(s Sub) []constraint.Constraint {
					return []constraint.Constraint{
						constraint.When(constraint.Present(s.C)).
							Require(constraint.Present(it.A)).
							Message("a sub with c needs its item's a"),
					}
				}),
			}
		}),
	}
}
`
	schema, _, err := readConstraintLibrary(t, src)
	require.NoError(t, err)
	entries, perr := lang.ParseSpecs(schema.Resources["thing"].Constraints)
	require.Equal(t, 0, perr.Len(), "specs should parse: %v", perr.Err())

	eval := func(values map[string]any) lang.ConstraintEvalFunc {
		return func(e lang.Expr, binds []lang.EachBinding) (any, error) {
			ctx := &runtime.EvalContext{Vars: values, MissingAsNull: true}
			for _, b := range binds {
				if ctx.Each == nil {
					ctx.Each = map[string]lang.EachValue{}
				}
				ctx.Each[b.Name] = lang.EachValue{Key: b.Key, Value: b.Value}
			}
			return runtime.Eval(e, ctx)
		}
	}

	good := map[string]any{"items": []any{
		map[string]any{"a": "x", "subs": []any{map[string]any{"c": "y"}}},
		map[string]any{"subs": []any{map[string]any{}}},
	}}
	ok := lang.CheckConstraintEntries(entries, good, eval(good), lang.DisplayNodeRelative)
	require.Equal(t, 0, ok.Len(), "valid values pass: %v", ok.Err())

	bad := map[string]any{"items": []any{
		map[string]any{"a": "x", "subs": []any{map[string]any{"c": "y"}}},
		map[string]any{"subs": []any{
			map[string]any{},
			map[string]any{"c": "y"},
		}},
	}}
	got := lang.CheckConstraintEntries(entries, bad, eval(bad), lang.DisplayNodeRelative)
	require.Equal(t, 1, got.Len(), "one violation expected: %v", got.Err())
	require.Contains(t, got.Err().Error(),
		"a sub with c needs its item's a (items[1].subs[1])")
}

// TestReadExtractsChainedScalarForEach proves a scalar inner list
// chains too: the inner binding's value is the element itself.
func TestReadExtractsChainedScalarForEach(t *testing.T) {
	src := constraintLibrary + `
func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ForEach(v.Items, func(it Item) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.ForEach(it.Tags, func(tag string) []constraint.Constraint {
					return []constraint.Constraint{
						constraint.Must(constraint.OneOf(tag, "a", "b")),
					}
				}),
			}
		}),
	}
}
`
	schema, warnings, err := readConstraintLibrary(t, src)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, []lang.ConstraintSpec{{
		Kind:    "predicate",
		When:    "true",
		Require: "(@tag.value == 'a' || @tag.value == 'b')",
		ForEachLevels: []lang.ForEachSpecLevel{
			{Name: "@it", In: "var.items"},
			{Name: "@tag", In: "@it.value.tags"},
		},
	}}, schema.Resources["thing"].Constraints)
}

// TestReadRejectsShadowedForEachParameter proves a nested parameter
// reusing an enclosing name is an error: the chain's bindings must be
// distinct.
func TestReadRejectsShadowedForEachParameter(t *testing.T) {
	src := constraintLibrary + `
func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ForEach(v.Items, func(it Item) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.ForEach(it.Subs, func(it Sub) []constraint.Constraint {
					return []constraint.Constraint{
						constraint.Must(constraint.Present(it.C)),
					}
				}),
			}
		}),
	}
}
`
	_, _, err := readConstraintLibrary(t, src)
	require.Error(t, err)
	require.Contains(t, err.Error(), "shadows")
}

// TestReadMixedNestedForEachBody proves a set constraint and a
// predicate in the same nested body each lower their own way: the set
// kind to multi-splat fields, the predicate to a chain.
func TestReadMixedNestedForEachBody(t *testing.T) {
	src := constraintLibrary + `
func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ForEach(v.Items, func(it Item) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.ForEach(it.Subs, func(s Sub) []constraint.Constraint {
					return []constraint.Constraint{
						constraint.ExactlyOneOf(s.C, s.D),
						constraint.Must(constraint.Present(s.C)),
					}
				}),
			}
		}),
	}
}
`
	schema, warnings, err := readConstraintLibrary(t, src)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, []lang.ConstraintSpec{
		{
			Kind: "exactly-one-of",
			Fields: []string{
				"var.items[*].subs[*].c",
				"var.items[*].subs[*].d",
			},
		},
		{
			Kind:    "predicate",
			When:    "true",
			Require: "(@s.value.c != null)",
			ForEachLevels: []lang.ForEachSpecLevel{
				{Name: "@it", In: "var.items"},
				{Name: "@s", In: "@it.value.subs"},
			},
		},
	}, schema.Resources["thing"].Constraints)
}

// TestNestedForEachChecksAgainstValues proves the multi-splat fields
// judge real nested values, naming the failing element at both levels.
func TestNestedForEachChecksAgainstValues(t *testing.T) {
	src := constraintLibrary + `
func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ForEach(v.Items, func(it Item) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.ForEach(it.Subs, func(s Sub) []constraint.Constraint {
					return []constraint.Constraint{
						constraint.ExactlyOneOf(s.C, s.D).Message("pick one"),
					}
				}),
			}
		}),
	}
}
`
	schema, _, err := readConstraintLibrary(t, src)
	require.NoError(t, err)
	entries, perr := lang.ParseSpecs(schema.Resources["thing"].Constraints)
	require.Equal(t, 0, perr.Len(), "specs should parse: %v", perr.Err())

	good := map[string]any{"items": []any{
		map[string]any{"subs": []any{map[string]any{"c": "x"}}},
		map[string]any{"subs": []any{}},
		map[string]any{},
	}}
	ok := lang.CheckConstraintEntries(entries, good, nil, lang.DisplayNodeRelative)
	require.Equal(t, 0, ok.Len(), "valid nested values pass: %v", ok.Err())

	bad := map[string]any{"items": []any{
		map[string]any{"subs": []any{map[string]any{"c": "x"}}},
		map[string]any{"subs": []any{
			map[string]any{"c": "x"},
			map[string]any{"c": "x", "d": "y"},
		}},
	}}
	got := lang.CheckConstraintEntries(entries, bad, nil, lang.DisplayNodeRelative)
	require.Equal(t, 1, got.Len(), "one violation expected: %v", got.Err())
	require.Contains(t, got.Err().Error(),
		"got 2 (items[1].subs[1].c, items[1].subs[1].d)")
}

func TestReadKeepsUnknownConstraintFieldAsError(t *testing.T) {
	src := constraintLibrary + `
func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.Equals(v.Bogus, "x")),
	}
}
`
	_, _, err := readConstraintLibrary(t, src)
	require.Error(t, err)
	require.Contains(t, err.Error(), `"Bogus"`)
}

// Every kind the constructor table renders must be one the language
// validates and dispatches; the checker silently skips a kind it does
// not know, so a drifted name would otherwise disable a constraint
// without a word.
func TestSetConstraintKindsMatchLanguage(t *testing.T) {
	known := lang.FieldsConstraintKinds()
	for ctor, kind := range setConstraintKinds {
		require.Contains(t, known, kind, "constructor %s", ctor)
	}
}
