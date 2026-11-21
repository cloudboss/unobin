package lib

import (
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.1"},
		Name:          "lib",
		Resources: map[string]runtime.ResourceRegistration{
			"thing": runtime.MakeResource[Alias, *ThingOutput, any](
				runtime.ResourceDefinition[Alias, *ThingOutput, any]{SchemaVersion: 1},
			),
		},
	}
}

type Thing struct {
	Name   *string
	Domain string
	Count  int
	Tags   map[string]string
}

type ThingOutput struct{ ID string }

func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.Present(v.Name)),
	}
}

type Alias = Thing
