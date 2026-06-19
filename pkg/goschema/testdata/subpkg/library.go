package subpkg

import (
	"github.com/cloudboss/unobin/pkg/runtime"

	"example.com/subpkg/data"
	"example.com/subpkg/resources"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "subpkg",
		Resources: map[string]runtime.ResourceRegistration{
			"thing": runtime.MakeResource[resources.Thing, *resources.ThingOutput, any](),
		},
		DataSources: map[string]runtime.DataSourceRegistration{
			"ami": runtime.MakeDataSource[data.AMI, *data.AMIOutput, any](),
		},
	}
}
