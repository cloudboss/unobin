package configschemacombined

import (
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

type Configuration struct {
	Region string
}

func LibraryConfiguration() cfg.ConfigurationType[*Configuration] {
	return cfg.ConfigurationType[*Configuration]{
		Description: "Combined config fixture.",
		New: func() *Configuration {
			return &Configuration{}
		},
	}
}

func Library() *runtime.Library {
	return &runtime.Library{
		Name:          "combined",
		Configuration: LibraryConfiguration(),
	}
}
