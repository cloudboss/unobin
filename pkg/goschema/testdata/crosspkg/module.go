package crosspkg

import (
	"context"

	"github.com/cloudboss/unobin/pkg/runtime"

	"example.com/crosspkg/endpoints"
)

func Module() *runtime.Module {
	return &runtime.Module{
		Name: "crosspkg",
		Resources: map[string]runtime.ResourceRegistration{
			"db": runtime.MakeResource[DB, *DBOutput](),
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
func (d *DB) Update(_ context.Context, _ *DBOutput) (*DBOutput, error) {
	return &DBOutput{}, nil
}
func (d *DB) Delete(_ context.Context, _ *DBOutput) error { return nil }
func (d *DB) SchemaVersion() int                          { return 1 }
