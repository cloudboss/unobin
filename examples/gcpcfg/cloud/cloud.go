// Package cloud is a small demonstration library whose configuration
// is gcpcfg.Configuration, the shared Google Cloud connection schema.
package cloud

import (
	"context"

	"github.com/cloudboss/unobin/pkg/gcpcfg"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

// DescribeAction reports the connection settings its configuration selects.
type DescribeAction struct {
	Label string
}

// DescribeActionOutput is the action's output.
type DescribeActionOutput struct {
	Label           string
	Project         string
	Region          string
	StorageEndpoint string
	KMSEndpoint     string
	FirstScope      string
}

func (a *DescribeAction) Run(
	_ context.Context,
	config *gcpcfg.Configuration,
) (*DescribeActionOutput, error) {
	out := &DescribeActionOutput{
		Label:      a.Label,
		Project:    "ambient",
		Region:     "default",
		FirstScope: "none",
	}
	if config == nil {
		return out, nil
	}
	if config.Project != nil {
		out.Project = *config.Project
	}
	if config.Region != nil {
		out.Region = *config.Region
	}
	out.StorageEndpoint = config.StorageEndpoint()
	out.KMSEndpoint = config.KMSEndpoint()
	if scopes := config.ScopeValues(); len(scopes) > 0 {
		out.FirstScope = scopes[0]
	}
	return out, nil
}

// LibraryConfiguration returns the shared Google Cloud config registration.
func LibraryConfiguration() *cfg.ConfigurationType[*gcpcfg.Configuration] {
	return &cfg.ConfigurationType[*gcpcfg.Configuration]{
		Description: "Google Cloud connection settings.",
		New:         func() *gcpcfg.Configuration { return &gcpcfg.Configuration{} },
	}
}

// Library returns the registration record for the cloud library.
func Library() *runtime.Library {
	return &runtime.Library{
		Name:          "cloud",
		Description:   "Reports Google Cloud connection settings.",
		Configuration: LibraryConfiguration(),
		Actions: map[string]runtime.ActionRegistration{
			"describe": runtime.MakeAction[
				DescribeAction,
				*DescribeActionOutput,
				*gcpcfg.Configuration,
			](),
		},
	}
}
