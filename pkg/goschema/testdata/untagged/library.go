package untagged

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "untagged",
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

// ThingOutput intentionally omits ub tags on some fields so the
// walker exercises the kebab-case fallback derived from the Go field
// name.
type ThingOutput struct {
	ID         string
	CidrBlock  string
	HTTPSProxy string
	Tagged     string `ub:"explicit-tag"`
}
