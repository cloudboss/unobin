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
	Name      string
	Code      DBCode
	Listeners []DBListener
	Replicas  []DBReplica
	CACert    *string
}

func (d DB) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(d.Code.Inline, d.Code.FromFile),
		constraint.When(constraint.Present(d.Code.Signing)).
			Require(constraint.Present(d.Code.Signing.KeyArn)).
			Message("signing requires a key arn"),
		constraint.RequiredTogether(d.Listeners[0].Cert, d.Listeners[0].Key),
		constraint.ForEach(d.Replicas, func(r DBReplica) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.ExactlyOneOf(r.Inline, r.FromFile),
				constraint.RequiredWith(r.TLS, d.CACert),
				constraint.When(constraint.IsTrue(r.TLS)).
					Require(constraint.Present(r.Cert)).
					Message("tls requires a cert"),
			}
		}),
	}
}

type DBListener struct {
	Cert *string
	Key  *string
}

type DBReplica struct {
	Inline   *string
	FromFile *string
	TLS      *bool
	Cert     *string
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
