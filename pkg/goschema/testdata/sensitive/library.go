package sensitive

import (
	"context"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},

		Name: "sensitive",
		Resources: map[string]runtime.ResourceRegistration{
			"secret": runtime.MakeResource[Secret, *SecretOutput, any](
				runtime.ResourceDefinition[Secret, *SecretOutput, any]{SchemaVersion: 1},
			),
		},
	}
}

type Secret struct {
	Name     string
	Password string `ub:",sensitive"`
}

type SecretOutput struct {
	ARN   string
	Value string `ub:",sensitive"`
}

func (s *Secret) Create(_ context.Context, _ any) (*SecretOutput, error) {
	return nil, nil
}

func (s *Secret) Read(
	_ context.Context, _ any, _ runtime.Prior[Secret, *SecretOutput, any],
) (*SecretOutput, error) {
	return nil, nil
}

func (s *Secret) Update(
	_ context.Context, _ any, _ runtime.Prior[Secret, *SecretOutput, any],
) (*SecretOutput, error) {
	return nil, nil
}

func (s *Secret) Delete(
	_ context.Context, _ any, _ runtime.Prior[Secret, *SecretOutput, any],
) error {
	return nil
}
