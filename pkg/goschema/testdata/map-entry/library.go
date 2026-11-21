package lib

import (
	rules "github.com/cloudboss/unobin/pkg/constraint"
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

type Entry = rules.MapEntry[*Item]
type Items map[string]*Item
type Item struct {
	Name   *string
	Values []string
}
type Thing struct {
	Items *Items
	Lists map[string][]Item
	Rows  []Items
}
type ThingOutput struct{ ID string }

func (v Thing) Constraints() []rules.Constraint {
	return []rules.Constraint{
		rules.ForEach[Entry](v.Items, func(entry Entry) []rules.Constraint {
			return []rules.Constraint{
				rules.When(rules.Present(entry.Value)).Require(
					rules.Equals(entry.Key, entry.Value.Name),
				).Message("entry name"),
				rules.ForEach(entry.Value.Values, func(value string) []rules.Constraint {
					return []rules.Constraint{
						rules.Must(rules.Equals(value, entry.Key)).Message("entry values"),
					}
				}),
			}
		}),
		rules.ForEach(v.Lists, func(entry rules.MapEntry[[]Item]) []rules.Constraint {
			return []rules.Constraint{
				rules.When(rules.Present(entry.Value[0].Name)).Require(
					rules.Equals(entry.Key, entry.Value[0].Name),
				).Message("indexed entry"),
			}
		}),
		rules.ForEach(v.Rows, func(row Items) []rules.Constraint {
			return []rules.Constraint{
				rules.ForEach(row, func(entry rules.MapEntry[*Item]) []rules.Constraint {
					return []rules.Constraint{
						rules.When(rules.Present(entry.Value)).Require(
							rules.Equals(entry.Key, entry.Value.Name),
						).Message("row entry"),
					}
				}),
			}
		}),
	}
}
