package cloudlib

import (
	"context"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name:        "cloudlib",
		Description: "Fixture library configured by a type from the unobin module.",
		Configuration: &cfg.ConfigurationType[any]{
			Description: "AWS connection settings.",
			New:         func() any { return &awscfg.Configuration{} },
		},
		Resources: map[string]runtime.ResourceRegistration{
			"thing": runtime.MakeResource[Thing, *ThingOutput, any](ThingDefinition()),
		},
	}
}

type Thing struct {
	Name string
}

type ThingOutput struct {
	ID string
}

func ThingDefinition() runtime.ResourceDefinition[Thing, *ThingOutput, any] {
	return runtime.ResourceDefinition[Thing, *ThingOutput, any]{
		SchemaVersion: 1,
		Identity: runtime.ResourceIdentity[Thing, *ThingOutput]{
			Version: 1,
			Scope:   runtime.IdentityConfiguration,
		},
	}
}

func (t *Thing) Create(_ context.Context, _ any) (*ThingOutput, error) {
	return &ThingOutput{}, nil
}

func (t *Thing) Read(_ context.Context, _ any, _ *ThingOutput) (*ThingOutput, error) {
	return &ThingOutput{}, nil
}

func (t *Thing) Update(
	_ context.Context, _ any, _ runtime.Prior[Thing, *ThingOutput],
) (*ThingOutput, error) {
	return &ThingOutput{}, nil
}

func (t *Thing) Delete(_ context.Context, _ any, _ *ThingOutput) error {
	return nil
}
