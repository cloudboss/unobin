package nested

import (
	"context"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func Module() *runtime.Module {
	return &runtime.Module{
		Name: "nested",
		Resources: map[string]runtime.ResourceRegistration{
			"db": runtime.MakeResource[DB, *DBOutput](),
		},
	}
}

type DB struct {
	Name string `mapstructure:"name"`
	Code DBCode `mapstructure:"code"`
}

type DBCode struct {
	Inline   *string    `mapstructure:"inline"`
	FromFile *string    `mapstructure:"from-file"`
	Signing  *DBSigning `mapstructure:"signing"`
}

type DBSigning struct {
	KeyArn  *string `mapstructure:"key-arn"`
	Profile string  `mapstructure:"profile"`
}

type Endpoint struct {
	Host string `mapstructure:"host"`
	Port int64  `mapstructure:"port"`
}

type DBOutput struct {
	ID       string   `mapstructure:"id"`
	Endpoint Endpoint `mapstructure:"endpoint"`
	Replicas []Endpoint
	Self     *DBOutput `mapstructure:"self"`
}

func (d *DB) Create(_ context.Context) (*DBOutput, error) { return &DBOutput{}, nil }
func (d *DB) Read(_ context.Context) (*DBOutput, error)   { return &DBOutput{}, nil }
func (d *DB) Update(_ context.Context, _ *DBOutput) (*DBOutput, error) {
	return &DBOutput{}, nil
}
func (d *DB) Delete(_ context.Context, _ *DBOutput) error { return nil }
func (d *DB) SchemaVersion() int                          { return 1 }
