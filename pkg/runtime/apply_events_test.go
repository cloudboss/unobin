package runtime

import (
	"context"
)

type plainResource struct {
	Name string
}

type plainResourceOutput struct{ Name string }

func plainResourceDefinition() ResourceDefinition[plainResource, *plainResourceOutput, any] {
	return ResourceDefinition[plainResource, *plainResourceOutput, any]{
		SchemaVersion: 1,
		Identity: ResourceIdentity[plainResource, *plainResourceOutput]{
			Version: 1,
			Scope:   IdentityConfiguration,
		},
	}
}

func (r *plainResource) Create(_ context.Context, _ any) (*plainResourceOutput, error) {
	return &plainResourceOutput{Name: r.Name}, nil
}
func (r *plainResource) Read(
	_ context.Context,
	_ any,
	_ *plainResourceOutput,
) (*plainResourceOutput, error) {
	return nil, ErrNotFound
}
func (r *plainResource) Update(
	_ context.Context, _ any, _ Prior[plainResource, *plainResourceOutput],
) (*plainResourceOutput, error) {
	return &plainResourceOutput{Name: r.Name}, nil
}
func (r *plainResource) Delete(_ context.Context, _ any, _ *plainResourceOutput) error {
	return nil
}
