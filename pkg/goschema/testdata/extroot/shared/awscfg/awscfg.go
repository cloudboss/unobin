package awscfg

import (
	"example.com/shared/awscfg/inner"

	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

type Configuration struct {
	Region     cfg.String
	Endpoint   *cfg.String
	AssumeRole *AssumeRole
	Tuning     cfg.Object[inner.Tuning]
}

type AssumeRole struct {
	RoleArn    cfg.String
	ExternalId *cfg.String
}
