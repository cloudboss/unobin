package lib

import (
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},
		Name:          "lib",
		Resources: map[string]runtime.ResourceRegistration{
			"thing": runtime.MakeResource[Thing, *ThingOutput, any](
				runtime.ResourceDefinition[Thing, *ThingOutput, any]{SchemaVersion: 1},
			),
		},
	}
}

type Labels map[string]string
type Metadata struct{ Name *string }
type MetadataAlias = Metadata
type Object struct{ Metadata *MetadataAlias }
type Thing struct {
	Object   *Object
	Pattern  *string
	IP       *string
	CIDR     *string
	Data     *string
	Duration *string
	Labels   *Labels
	Groups   *map[string]Labels
	Lists    *map[string][]string
	Nested   *map[string]map[string]Labels
	Expected string
}
type ThingOutput struct{ ID string }

func (v Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.IsMatch(v.Object.Metadata.Name, "^[a-z][a-z0-9.-]*$")).
			Message("name format"),
		constraint.Must(constraint.IsFormat(v.Pattern, "regex")).Message("regex format"),
		constraint.Must(constraint.IsFormat(v.IP, "ip")).Message("IP format"),
		constraint.Must(constraint.IsFormat(v.CIDR, "cidr")).Message("CIDR format"),
		constraint.Must(constraint.IsFormat(v.Data, "base64")).Message("base64 format"),
		constraint.Must(constraint.IsFormat(v.Duration, "positive-duration")).Message("duration format"),
		constraint.ForEach(v.Labels, func(entry constraint.MapEntry[string]) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.IsMatch(entry.Key, "^[a-z]+$")).Message("label key"),
			}
		}),
		constraint.ForEach(v.Labels, func(entry constraint.MapEntry[string]) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.IsFormat(entry.Value, "base64")).Message("label value"),
			}
		}),
		constraint.ForEach(v.Groups, func(group constraint.MapEntry[Labels]) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.ForEach(group.Value, func(
					entry constraint.MapEntry[string],
				) []constraint.Constraint {
					return []constraint.Constraint{
						constraint.Must(constraint.Equals(entry.Key, v.Expected)).Message("group key"),
					}
				}),
				constraint.ForEach(group.Value, func(
					entry constraint.MapEntry[string],
				) []constraint.Constraint {
					return []constraint.Constraint{
						constraint.Must(
							constraint.IsFormat(entry.Value, "base64"),
							constraint.MinItems(group.Value, 1),
						).Message("group value"),
					}
				}),
			}
		}),
		constraint.ForEach(v.Lists, func(list constraint.MapEntry[[]string]) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.ForEach(list.Value, func(value string) []constraint.Constraint {
					return []constraint.Constraint{
						constraint.Must(constraint.IsFormat(value, "ip")).Message("list IP"),
					}
				}),
			}
		}),
		constraint.ForEach(v.Nested, func(
			outer constraint.MapEntry[map[string]Labels],
		) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.ForEach(outer.Value, func(
					middle constraint.MapEntry[Labels],
				) []constraint.Constraint {
					return []constraint.Constraint{
						constraint.ForEach(middle.Value, func(
							entry constraint.MapEntry[string],
						) []constraint.Constraint {
							return []constraint.Constraint{
								constraint.Must(
									constraint.IsFormat(entry.Value, "base64"),
									constraint.MinItems(outer.Value, 1),
								).Message("nested value"),
							}
						}),
					}
				}),
			}
		}),
	}
}
