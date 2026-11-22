package native

import (
	"context"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type Echo struct {
	Name string
}

type EchoOutput struct {
	Value string
}

func (a *Echo) Run(context.Context, runtime.NoConfig) (*EchoOutput, error) {
	return &EchoOutput{Value: a.Name}, nil
}

func (a Echo) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.IsMatch(a.Name, "^[a-z]+$")),
		constraint.Must(constraint.AtMost(constraint.Length(a.Name), 8)).
			Message("name must have at most eight characters"),
	}
}

func Library() *runtime.Library {
	return &runtime.Library{
		Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.1"},
		Actions: map[string]runtime.ActionRegistration{
			"echo": runtime.MakeAction[Echo, *EchoOutput, runtime.NoConfig](),
		},
	}
}
