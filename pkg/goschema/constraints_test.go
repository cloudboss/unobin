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
	Name  *string
	Kind  *string
	Port  *int
	Items []Item
}

type Item struct {
	A *string
	B *string
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
