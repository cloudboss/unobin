package configuration

import (
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

func Library() *runtime.Library {
	return &runtime.Library{
		Name:        "configured",
		Description: "Fixture library with a configuration and no types.",
		Configuration: &cfg.ConfigurationType{
			Description: "Test configuration.",
			New:         func() any { return &Configuration{} },
		},
	}
}
