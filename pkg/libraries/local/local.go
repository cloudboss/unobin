// Package local provides primitive resources backed by the local
// filesystem.
package local

import "github.com/cloudboss/unobin/pkg/runtime"

// Library returns the registration record for the `local` library.
// Stacks reach its resources as `resources: { local: { file: { ... } } }`.
func Library() *runtime.Library {
	return &runtime.Library{
		Name:        "local",
		Description: "Local filesystem primitives",
		Resources: map[string]runtime.ResourceRegistration{
			"file": runtime.MakeResource[File, *FileOutput](),
		},
	}
}
