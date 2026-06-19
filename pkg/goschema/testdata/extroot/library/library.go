package library

import (
	"example.com/shared/awscfg"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name:        "extroot",
		Description: "Fixture library whose configuration type lives in another module.",
		Configuration: &cfg.ConfigurationType[any]{
			Description: "Shared configuration.",
			New:         func() any { return &awscfg.Configuration{} },
		},
	}
}
