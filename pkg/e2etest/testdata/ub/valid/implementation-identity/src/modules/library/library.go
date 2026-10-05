package library

import (
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

type Configuration struct{}

func Library() *runtime.Library {
	return &runtime.Library{
		Name:          "identity",
		Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},
		Configuration: &cfg.ConfigurationType[*Configuration]{
			New: func() *Configuration { return &Configuration{} },
		},
		Resources: map[string]runtime.ResourceRegistration{
			"marker": runtime.MakeResource[Marker, *Output, *Configuration](
				runtime.ResourceDefinition[Marker, *Output, *Configuration]{SchemaVersion: 1},
			),
		},
	}
}
