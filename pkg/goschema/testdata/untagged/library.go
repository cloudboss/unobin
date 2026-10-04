package untagged

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},

		Name: "untagged",
		Resources: map[string]runtime.ResourceRegistration{
			"thing": runtime.MakeResource[Thing, *ThingOutput, any](
				runtime.ResourceDefinition[Thing, *ThingOutput, any]{SchemaVersion: 1},
			),
		},
	}
}

type Thing struct{}

// ThingOutput intentionally omits ub tags on some fields so the
// walker exercises the kebab-case fallback derived from the Go field
// name.
type ThingOutput struct {
	ID         string
	CidrBlock  string
	HTTPSProxy string
	Tagged     string `ub:"explicit-tag"`
}
