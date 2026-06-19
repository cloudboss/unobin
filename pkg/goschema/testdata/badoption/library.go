package badoption

import (
	"context"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "badoption",
		Resources: map[string]runtime.ResourceRegistration{
			"thing": runtime.MakeResource[Thing, *ThingOutput, any](),
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

func (t *Thing) SchemaVersion() int { return 1 }

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

func (t *Thing) ReplaceFields() []string { return nil }
