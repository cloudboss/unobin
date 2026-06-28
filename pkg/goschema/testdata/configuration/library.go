package configuration

import (
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

type Configuration struct {
	Region     cfg.String
	Profile    *cfg.String
	Retries    cfg.Integer
	Ratio      *cfg.Number
	Verbose    cfg.Boolean
	Tags       cfg.Map[cfg.String]
	Subnets    cfg.List[cfg.String]
	Zones      *cfg.List[cfg.String]
	Labels     *cfg.Map[cfg.String]
	Extra      cfg.Any
	Endpoint   cfg.Object[Endpoint]
	AssumeRole *AssumeRole
}

type Endpoint struct {
	Host cfg.String
}

type AssumeRole struct {
	RoleArn  cfg.String
	External *cfg.String
}

func (c Configuration) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEmpty(c.Region)).Message("region is required"),
		constraint.RequiredWith(c.AssumeRole.RoleArn, c.Region),
	}
}

func Library() *runtime.Library {
	return &runtime.Library{
		Name:        "configured",
		Description: "Fixture library with a configuration and no types.",
		Configuration: &cfg.ConfigurationType[any]{
			Description: "Test configuration.",
			New: func() any {
				return &Configuration{
					Profile: &cfg.String{Default: "default"},
					Ratio:   &cfg.Number{Default: 0.5},
					Zones: &cfg.List[cfg.String]{
						Default: []cfg.String{{Value: "zone-a"}, {Value: "zone-b"}},
					},
					Labels: &cfg.Map[cfg.String]{
						Default: map[string]cfg.String{"env": {Value: "dev"}},
					},
				}
			},
		},
	}
}
