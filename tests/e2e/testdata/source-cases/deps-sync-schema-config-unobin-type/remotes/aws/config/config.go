package config

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

func LibraryConfiguration() *cfg.ConfigurationType[*awscfg.Configuration] {
	return &cfg.ConfigurationType[*awscfg.Configuration]{
		Description: "AWS config fixture.",
		New: func() *awscfg.Configuration {
			return &awscfg.Configuration{}
		},
	}
}
