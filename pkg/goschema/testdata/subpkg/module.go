package subpkg

import (
	"github.com/cloudboss/unobin/pkg/runtime"

	"example.com/subpkg/data"
	"example.com/subpkg/resources"
)

func Module() *runtime.Module {
	return &runtime.Module{
		Name: "subpkg",
		Resources: map[string]runtime.ResourceType{
			"thing": {
				Name: "thing",
				New:  func() runtime.Resource { return &resources.Thing{} },
			},
		},
		DataSources: map[string]runtime.DataSourceType{
			"ami": {
				Name: "ami",
				New:  func() runtime.DataSource { return &data.AMI{} },
			},
		},
	}
}
