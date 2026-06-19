package crosspkg

import (
	"context"

	"github.com/cloudboss/unobin/pkg/runtime"

	"example.com/crosspkg/endpoints"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "crosspkg",
		Resources: map[string]runtime.ResourceRegistration{
			"db": runtime.MakeResource[DB, *DBOutput, any](),
		},
	}
}

type DB struct {
	Name string
}

type DBOutput struct {
	ID       string
	Endpoint endpoints.Endpoint
	Replicas []endpoints.Endpoint
	Self     *DBOutput
}

func (d *DB) Create(_ context.Context) (*DBOutput, error) { return &DBOutput{}, nil }
func (d *DB) Read(_ context.Context) (*DBOutput, error)   { return &DBOutput{}, nil }
func (d *DB) Update(_ context.Context, _ runtime.Prior[DB, *DBOutput]) (*DBOutput, error) {
	return &DBOutput{}, nil
}
func (d *DB) Delete(_ context.Context, _ *DBOutput) error { return nil }
func (d *DB) SchemaVersion() int                          { return 1 }
