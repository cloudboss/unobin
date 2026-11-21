package models

import "github.com/cloudboss/unobin/pkg/constraint"

type Input struct{ Name *string }
type InputAlias = Input

func (v Input) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.Present(v.Name)),
	}
}
