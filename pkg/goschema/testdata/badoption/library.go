package badoption

import (
	"context"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "badoption",
		Resources: map[string]runtime.ResourceRegistration{
			"thing": runtime.MakeResource[Thing, *ThingOutput, any](ThingDefinition()),
		},
	}
}

// Thing has a misspelled sensitive option. The schema reader must
// reject it rather than silently leave the field unmasked.
type Thing struct {
	Password string `ub:",sensitiv"`
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

func (t *Thing) Create(_ context.Context, _ any) (*ThingOutput, error) { return nil, nil }

func (t *Thing) Read(_ context.Context, _ any, _ *ThingOutput) (*ThingOutput, error) {
	return nil, nil
}

func (t *Thing) Update(
	_ context.Context, _ any, _ runtime.Prior[Thing, *ThingOutput],
) (*ThingOutput, error) {
	return nil, nil
}

func (t *Thing) Delete(_ context.Context, _ any, _ *ThingOutput) error { return nil }
