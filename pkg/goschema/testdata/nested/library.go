package nested

import (
	"context"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "nested",
		Resources: map[string]runtime.ResourceRegistration{
			"db": runtime.MakeResource[DB, *DBOutput](),
		},
	}
}

type DB struct {
	Name string
	Code DBCode
}

func (d DB) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(d.Code.Inline, d.Code.FromFile),
		constraint.When(constraint.Present(d.Code.Signing)).
			Require(constraint.Present(d.Code.Signing.KeyArn)).
			Message("signing requires a key arn"),
	}
}

type DBCode struct {
	Inline   *string
	FromFile *string
	Signing  *DBSigning
}

type DBSigning struct {
	KeyArn  *string
	Profile string
}

type Endpoint struct {
	Host string
	Port int64
}

type DBOutput struct {
	ID       string
	Endpoint Endpoint
	Replicas []Endpoint
	Self     *DBOutput
}

func (d *DB) Create(_ context.Context) (*DBOutput, error) { return &DBOutput{}, nil }
func (d *DB) Read(_ context.Context) (*DBOutput, error)   { return &DBOutput{}, nil }
func (d *DB) Update(_ context.Context, _ runtime.Prior[DB, *DBOutput]) (*DBOutput, error) {
	return &DBOutput{}, nil
}
func (d *DB) Delete(_ context.Context, _ *DBOutput) error { return nil }
func (d *DB) SchemaVersion() int                          { return 1 }
