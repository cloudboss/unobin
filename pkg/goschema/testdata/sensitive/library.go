package sensitive

import (
	"context"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "sensitive",
		Resources: map[string]runtime.ResourceRegistration{
			"secret": runtime.MakeResource[Secret, *SecretOutput](),
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

func (s *Secret) SchemaVersion() int { return 1 }

func (s *Secret) Create(_ context.Context, _ any) (*SecretOutput, error) {
	return nil, nil
}

func (s *Secret) Read(_ context.Context, _ any, _ *SecretOutput) (*SecretOutput, error) {
	return nil, nil
}

func (s *Secret) Update(
	_ context.Context, _ any, _ runtime.Prior[Secret, *SecretOutput],
) (*SecretOutput, error) {
	return nil, nil
}

func (s *Secret) Delete(_ context.Context, _ any, _ *SecretOutput) error {
	return nil
}
