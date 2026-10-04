package library

import (
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

type Configuration struct {
	Region string
}

func LibraryConfiguration() *cfg.ConfigurationType[*Configuration] {
	return &cfg.ConfigurationType[*Configuration]{
		New: func() *Configuration { return &Configuration{} },
	}
}

func Library() *runtime.Library {
	return &runtime.Library{
		Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},
		Configuration: LibraryConfiguration(),
	}
}
