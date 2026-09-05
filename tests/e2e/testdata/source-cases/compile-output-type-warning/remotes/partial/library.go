package partial

import "github.com/cloudboss/unobin/pkg/runtime"

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "partial",
		Resources: map[string]runtime.ResourceRegistration{
			"thing": runtime.MakeResource[Thing, *ThingOutput, any](ThingDefinition()),
		},
	}
}

type Thing struct{}

func ThingDefinition() runtime.ResourceDefinition[Thing, *ThingOutput, any] {
	return runtime.ResourceDefinition[Thing, *ThingOutput, any]{
		SchemaVersion: 1,
		Identity: runtime.ResourceIdentity[Thing, *ThingOutput]{
			Version: 1,
			Scope:   runtime.IdentityConfiguration,
		},
	}
}
