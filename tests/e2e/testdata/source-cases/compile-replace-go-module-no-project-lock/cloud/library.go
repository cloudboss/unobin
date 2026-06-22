package cloud

import "github.com/cloudboss/unobin/pkg/runtime"

func Library() *runtime.Library {
	return &runtime.Library{
		Actions: map[string]runtime.ActionRegistration{
			"describe": nil,
		},
	}
}
