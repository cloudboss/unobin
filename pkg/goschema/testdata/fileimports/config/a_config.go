package options

import (
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

type Config struct {
	Region  string
	Profile *cfg.String
}

func LibraryConfiguration() *cfg.ConfigurationType[*Config] {
	return &cfg.ConfigurationType[*Config]{
		New: func() *Config {
			return &Config{Profile: &cfg.String{Default: "dev"}}
		},
	}
}

func Library() *runtime.Library {
	return &runtime.Library{
		Name:          "options",
		Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},
		Configuration: LibraryConfiguration(),
	}
}
