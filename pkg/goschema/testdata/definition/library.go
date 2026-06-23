package definition

import (
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"

	"example.com/definition/shared"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "definition",
		Configuration: &cfg.ConfigurationType[*Config]{
			New: func() *Config { return &Config{} },
		},
		Resources: map[string]runtime.ResourceRegistration{
			"server": runtime.MakeResource[Server, *ServerOutput, any](),
		},
		DataSources: map[string]runtime.DataSourceRegistration{
			"lookup": runtime.MakeDataSource[Lookup, *LookupOutput, any](),
		},
		Actions: map[string]runtime.ActionRegistration{
			"deploy": runtime.MakeAction[Deploy, *DeployOutput, any](),
		},
		Functions: map[string]runtime.FunctionType{
			"slug": runtime.MakeFunc("slug", "build slug", makeSlug),
		},
	}
}

type Config struct {
	Region string
	Retry  shared.Retry
}

type Server struct {
	ID       string
	Name     string `ub:"server-name"`
	Settings shared.Settings
}

type ServerOutput struct {
	ID       string
	Endpoint shared.Endpoint
}

type Lookup struct {
	Query string
}

type LookupOutput struct {
	Result string
}

type Deploy struct {
	Version string
}

type DeployOutput struct {
	Status string
}

func makeSlug(name string) (string, error) {
	return name, nil
}
