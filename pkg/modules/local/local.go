// Package local provides primitive resources backed by the local
// filesystem.
package local

import "github.com/cloudboss/unobin/pkg/runtime"

// Module returns the registration record for the `local` module.
// Stacks reach its resources as `resources: { local: { file: { ... } } }`.
func Module() *runtime.Module {
	return &runtime.Module{
		Name:        "local",
		Description: "Local filesystem primitives",
		Resources: map[string]runtime.ResourceRegistration{
			"file": runtime.MakeResource[File, *FileOutput](),
		},
	}
}
