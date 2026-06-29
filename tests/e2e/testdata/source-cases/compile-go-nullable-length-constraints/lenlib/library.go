package lenlib

import (
	"context"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "lenlib",
		Resources: map[string]runtime.ResourceRegistration{
			"length": runtime.MakeResource[LengthInputs, *LengthOutput, any](),
		},
	}
}

type LengthInputs struct {
	Name            string
	OptionalMethods *[]string
	OptionalTags    *map[string]string
	Profile         *string
	Them            *[]*string
}

func (v LengthInputs) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEmpty(v.OptionalMethods)).
			Message("methods required"),
		constraint.Must(constraint.Not(constraint.NotEmpty(v.OptionalMethods))).
			Message("methods must be empty"),
		constraint.Must(constraint.NotEmpty(v.OptionalTags)).
			Message("tags required"),
		constraint.Must(constraint.NotEmpty(v.Profile)).
			Message("profile required"),
		constraint.Must(constraint.MaxItems(v.OptionalMethods, 5)).
			Message("too many methods"),
		constraint.Must(constraint.MinItems(v.OptionalMethods, 1)).
			Message("null or at least one method"),
		constraint.ForEach(v.Them, func(s *string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.NotEmpty(s)).
					Message("them values required"),
			}
		}),
	}
}

type LengthOutput struct {
	ID string
}

func (v *LengthInputs) SchemaVersion() int { return 1 }

func (v *LengthInputs) Create(context.Context, any) (*LengthOutput, error) {
	return &LengthOutput{ID: v.Name}, nil
}

func (v *LengthInputs) Read(context.Context, any, *LengthOutput) (*LengthOutput, error) {
	return &LengthOutput{ID: v.Name}, nil
}

func (v *LengthInputs) Update(
	context.Context,
	any,
	runtime.Prior[LengthInputs, *LengthOutput],
) (*LengthOutput, error) {
	return &LengthOutput{ID: v.Name}, nil
}

func (v *LengthInputs) Delete(context.Context, any, *LengthOutput) error { return nil }

func (v *LengthInputs) ReplaceFields() []string { return nil }
