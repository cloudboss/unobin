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
			"db": runtime.MakeResource[DB, *DBOutput, any](
				runtime.ResourceDefinition[DB, *DBOutput, any]{SchemaVersion: 1},
			),
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

func (d *DB) Create(_ context.Context, _ any) (*DBOutput, error) { return &DBOutput{}, nil }
func (d *DB) Read(
	_ context.Context, _ any, _ runtime.Prior[DB, *DBOutput, any],
) (*DBOutput, error) {
	return &DBOutput{}, nil
}
func (d *DB) Update(
	_ context.Context, _ any, _ runtime.Prior[DB, *DBOutput, any],
) (*DBOutput, error) {
	return &DBOutput{}, nil
}
func (d *DB) Delete(
	_ context.Context, _ any, _ runtime.Prior[DB, *DBOutput, any],
) error {
	return nil
}
