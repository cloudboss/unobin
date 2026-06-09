package badnew

import (
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

type Configuration struct {
	Region cfg.String
}

func newConfiguration() any { return &Configuration{} }

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "badnew",
		Configuration: &cfg.ConfigurationType{
			New: newConfiguration,
		},
	}
}
