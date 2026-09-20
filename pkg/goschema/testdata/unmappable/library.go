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
			"thing": runtime.MakeResource[Thing, *ThingOutput, any](
				runtime.ResourceDefinition[Thing, *ThingOutput, any]{SchemaVersion: 1},
			),
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

func (t *Thing) Create(_ context.Context, _ any) (*ThingOutput, error) {
	return &ThingOutput{}, nil
}
func (t *Thing) Read(
	_ context.Context, _ any, _ runtime.Prior[Thing, *ThingOutput, any],
) (*ThingOutput, error) {
	return &ThingOutput{}, nil
}
func (t *Thing) Update(
	_ context.Context, _ any, _ runtime.Prior[Thing, *ThingOutput, any],
) (*ThingOutput, error) {
	return &ThingOutput{}, nil
}
func (t *Thing) Delete(
	_ context.Context, _ any, _ runtime.Prior[Thing, *ThingOutput, any],
) error {
	return nil
}
