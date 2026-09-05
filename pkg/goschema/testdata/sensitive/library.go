package sensitive

import (
	"context"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "sensitive",
		Resources: map[string]runtime.ResourceRegistration{
			"secret": runtime.MakeResource[Secret, *SecretOutput, any](SecretDefinition()),
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

func SecretDefinition() runtime.ResourceDefinition[Secret, *SecretOutput, any] {
	return runtime.ResourceDefinition[Secret, *SecretOutput, any]{
		SchemaVersion: 1,
		Identity: runtime.ResourceIdentity[Secret, *SecretOutput]{
			Version: 1,
			Scope:   runtime.IdentityConfiguration,
		},
	}
}

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
