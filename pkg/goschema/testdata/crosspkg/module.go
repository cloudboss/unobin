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
	Name string `mapstructure:"name"`
}

type DBOutput struct {
	ID       string               `mapstructure:"id"`
	Endpoint endpoints.Endpoint   `mapstructure:"endpoint"`
	Replicas []endpoints.Endpoint `mapstructure:"replicas"`
	Self     *DBOutput            `mapstructure:"self"`
}

func (d *DB) Create(_ context.Context) (*DBOutput, error) { return &DBOutput{}, nil }
func (d *DB) Read(_ context.Context) (*DBOutput, error)   { return &DBOutput{}, nil }
func (d *DB) Update(_ context.Context, _ *DBOutput) (*DBOutput, error) {
	return &DBOutput{}, nil
}
func (d *DB) Delete(_ context.Context, _ *DBOutput) error { return nil }
func (d *DB) SchemaVersion() int                          { return 1 }
