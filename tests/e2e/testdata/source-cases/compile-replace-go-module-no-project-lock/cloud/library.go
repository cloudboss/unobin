package cloud

import "github.com/cloudboss/unobin/pkg/runtime"

func Library() *runtime.Library {
	return &runtime.Library{
		Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},

		Actions: map[string]runtime.ActionRegistration{
			"describe": nil,
		},
	}
}
