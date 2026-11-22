package lib

import (
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type Thing struct{ Port int64 }
type ThingOutput struct{ ID string }

func (t Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.Expr("input.port", constraint.Bind("port", t.Port))),
		constraint.Must(constraint.Expr("input.port > 0")),
		constraint.Must(constraint.Expr("external.check(input.port)",
			constraint.Bind("port", t.Port))),
		constraint.Must(constraint.Expr("input.port > 0",
			constraint.Bind("port", t.Port), constraint.Bind("port", t.Port))),
	}
}

func Library() *runtime.Library {
	return &runtime.Library{
		Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.1"},
		Resources: map[string]runtime.ResourceRegistration{
			"thing": runtime.MakeResource[Thing, *ThingOutput, any](
				runtime.ResourceDefinition[Thing, *ThingOutput, any]{SchemaVersion: 1},
			),
		},
	}
}
