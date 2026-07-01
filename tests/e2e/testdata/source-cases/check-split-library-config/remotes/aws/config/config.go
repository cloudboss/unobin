package config

import (
	"example.com/aws/awscfg"
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
