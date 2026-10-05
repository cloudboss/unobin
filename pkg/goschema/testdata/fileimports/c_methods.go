package fileimports

import (
	clock "time"

	types "github.com/cloudboss/unobin/pkg/constraint"
	fields "github.com/cloudboss/unobin/pkg/defaults"
)

func (v Local) Defaults() []fields.Default {
	return []fields.Default{
		fields.Value(v.Timeout, 2*clock.Second),
	}
}

func (v Local) Constraints() []types.Constraint {
	return []types.Constraint{
		types.ExactlyOneOf(v.Primary, v.Secondary).Message("choose a value"),
		types.Must(types.Above(v.Nested.Number, 0)),
	}
}
