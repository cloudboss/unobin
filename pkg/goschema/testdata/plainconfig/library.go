package plainconfig

import (
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

type Configuration struct {
	Region      string
	Profile     *string
	MaxAttempts int64
	Tags        map[string]string
	AssumeRole  *AssumeRole `ub:"assume-role"`
}

type AssumeRole struct {
	RoleARN    string `ub:"role-arn"`
	ExternalID *string
}

func (c Configuration) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(c.MaxAttempts, int64(3)),
	}
}

func (c Configuration) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEmpty(c.Region)).Message("region is required"),
		constraint.RequiredWith(c.AssumeRole.ExternalID, c.AssumeRole.RoleARN),
	}
}

type Bucket struct {
	Name string
}

type BucketOutput struct {
	ID string
}

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "plain-config",
		Configuration: &cfg.ConfigurationType[*Configuration]{
			Description: "Plain Go config fixture.",
			New: func() *Configuration {
				return &Configuration{}
			},
		},
		Resources: map[string]runtime.ResourceRegistration{
			"bucket": runtime.MakeResource[Bucket, *BucketOutput, *Configuration](),
		},
	}
}
