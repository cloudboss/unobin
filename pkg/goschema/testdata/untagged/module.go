package untagged

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func Module() *runtime.Module {
	return &runtime.Module{
		Name: "untagged",
		Resources: map[string]runtime.ResourceType{
			"thing": {
				Name: "thing",
				New:  func() runtime.Resource { return &Thing{} },
			},
		},
	}
}

type Thing struct{}

// ThingOutput intentionally omits mapstructure tags on some fields so
// the walker exercises the kebab-case fallback derived from the Go
// field name.
type ThingOutput struct {
	ID         string
	CidrBlock  string
	HTTPSProxy string
	Tagged     string `mapstructure:"explicit-tag"`
}
