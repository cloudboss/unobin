package fs

import "github.com/cloudboss/unobin/pkg/runtime"

func Library() *runtime.Library {
	return &runtime.Library{
		Resources: map[string]runtime.ResourceRegistration{
			"x": nil,
		},
	}
}
