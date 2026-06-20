// Package cloud is a small demonstration library whose configuration
// is awscfg.Configuration, the shared AWS connection schema unobin's
// own state backend and encrypter read. The describe action makes no
// AWS calls; it reports the connection settings it would use, so the
// example runs anywhere.
package cloud

import (
	"context"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

// DescribeAction reports the connection settings its configuration
// selects.
type DescribeAction struct {
	Label string
}

// DescribeActionOutput is the action's output.
type DescribeActionOutput struct {
	Label   string
	Region  string
	RoleArn string
	Source  string
}

func (a *DescribeAction) Run(
	_ context.Context,
	config *awscfg.Configuration,
) (*DescribeActionOutput, error) {
	out := &DescribeActionOutput{Label: a.Label, Region: "default", Source: "ambient"}
	if config == nil {
		return out, nil
	}
	if config.Region != nil {
		out.Region = config.Region.Value
	}
	if config.AssumeRole != nil {
		out.RoleArn = config.AssumeRole.RoleArn.Value
		out.Source = "assume-role"
	}
	return out, nil
}

// Library returns the registration record for the cloud library.
func Library() *runtime.Library {
	return &runtime.Library{
		Name:        "cloud",
		Description: "Reports the AWS connection settings a configuration selects.",
		Configuration: &cfg.ConfigurationType[*awscfg.Configuration]{
			Description: "AWS connection settings, shared with unobin's own backends.",
			New:         func() *awscfg.Configuration { return &awscfg.Configuration{} },
		},
		Actions: map[string]runtime.ActionRegistration{
			"describe": runtime.MakeAction[
				DescribeAction,
				*DescribeActionOutput,
				*awscfg.Configuration,
			](),
		},
	}
}
