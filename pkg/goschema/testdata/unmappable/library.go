package unmappable

import (
	"context"
	"time"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "unmappable",
		Resources: map[string]runtime.ResourceRegistration{
			"thing": runtime.MakeResource[Thing, *ThingOutput, any](ThingDefinition()),
		},
	}
}

type Thing struct {
	Name    string
	Updates chan string
}

type ThingOutput struct {
	ID   string
	Seen time.Time
}

func (t *Thing) Create(_ context.Context) (*ThingOutput, error) { return &ThingOutput{}, nil }
func (t *Thing) Read(_ context.Context) (*ThingOutput, error)   { return &ThingOutput{}, nil }
func (t *Thing) Update(_ context.Context, _ runtime.Prior[Thing, *ThingOutput]) (*ThingOutput, error) {
	return &ThingOutput{}, nil
}
func (t *Thing) Delete(_ context.Context, _ *ThingOutput) error { return nil }
func ThingDefinition() runtime.ResourceDefinition[Thing, *ThingOutput, any] {
	return runtime.ResourceDefinition[Thing, *ThingOutput, any]{
		SchemaVersion: 1,
		Identity: runtime.ResourceIdentity[Thing, *ThingOutput]{
			Version: 1,
			Scope:   runtime.IdentityConfiguration,
		},
	}
}
