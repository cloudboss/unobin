package lib

import (
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"example.com/constraints/limits"
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

type Named struct{ Name string }
type Thing struct {
	ListenPorts *[]int64
	Period      *int64
	Volumes     *[]Named
	Mount       *string
	Labels      *map[string]string
	Selector    *string
	Text        string
	Size        int64
	User        *string
	Limit       int64
}
type ThingOutput struct{ ID string }

const (
	signedFormat = constraint.FormatInt64
	message      = "invalid user"
)

func (t Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.Expr(
			"@core.all([for p in input.ports ?? []: p > 0])",
			constraint.Bind("ports", t.ListenPorts),
		)).Message("invalid port"),
		constraint.When(constraint.Present(t.Period)).
			Require(constraint.Equals(constraint.Remainder(t.Period, limits.Period), 0)).
			Message("invalid period"),
		constraint.Must(constraint.IsUnique(
			constraint.Project(t.Volumes, func(v Named) any { return v.Name }),
		)).Message("duplicate volume"),
		constraint.When(constraint.Present(t.Mount)).
			Require(constraint.HasItem(
				constraint.Project(t.Volumes, func(v Named) any { return v.Name }), t.Mount,
			)).Message("unknown volume"),
		constraint.When(constraint.Present(t.Selector)).Require(
			constraint.HasKey(t.Labels, t.Selector),
			constraint.Equals(constraint.Lookup(t.Labels, t.Selector), "yes"),
		).Message("label mismatch"),
		constraint.Must(constraint.AtMost(constraint.CountWhere(
			t.Labels, func(e constraint.MapEntry[string]) constraint.Condition {
				return constraint.All(constraint.NotEquals(e.Key, "ignored"),
					constraint.Equals(e.Value, "yes"))
			},
		), limits.Maximum)).Message("too many matching labels"),
		constraint.Must(constraint.AtMost(
			constraint.Sum(constraint.Project(t.Volumes, func(v Named) any {
				return constraint.ByteLength(v.Name)
			})), t.Limit,
		)).Message("volume byte limit"),
		constraint.Must(constraint.Equals(constraint.Length(t.Text), t.Size)).
			Message("text size mismatch"),
		constraint.Must(constraint.Any(
			constraint.Equals(t.User, ""),
			constraint.IsFormat(t.User, signedFormat),
		)).Message(message),
	}
}
