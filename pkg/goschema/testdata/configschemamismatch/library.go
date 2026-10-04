package configschemamismatch

import (
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

type Configuration struct {
	Region string
}

type OtherConfiguration struct {
	Region string
}

func LibraryConfiguration() cfg.ConfigurationType[*Configuration] {
	return cfg.ConfigurationType[*Configuration]{
		Description: "Schema config fixture.",
		New: func() *Configuration {
			return &Configuration{}
		},
	}
}

func Library() *runtime.Library {
	return &runtime.Library{
		Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},

		Name: "mismatch",
		Configuration: &cfg.ConfigurationType[*OtherConfiguration]{
			Description: "Library config fixture.",
			New: func() *OtherConfiguration {
				return &OtherConfiguration{}
			},
		},
	}
}
