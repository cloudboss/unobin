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
			"thing": runtime.MakeResource[Thing, *ThingOutput, any](
				runtime.ResourceDefinition[Thing, *ThingOutput, any]{SchemaVersion: 1},
			),
		},
	}
}

type Thing struct {
	Name   *string
	Domain string
	Count  int
	Tags   map[string]string
	Items  []string
	Groups map[string]map[string]string
	Lists  map[string][]Item
}

type ThingOutput struct{ ID string }

type Item struct {
	A *string
	B *string
}

func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ForEach(v.Lists, func(entry constraint.MapEntry[[]Item]) []constraint.Constraint {
			return []constraint.Constraint{constraint.ForEach(entry.Value, func(
				item Item,
			) []constraint.Constraint {
				return []constraint.Constraint{constraint.ExactlyOneOf(item.A, item.B)}
			})}
		}),
	}
}
