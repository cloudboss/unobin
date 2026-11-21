package lib

import (
	"github.com/cloudboss/unobin/pkg/runtime"

	models "example.com/lib/models"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.1"},
		Name:          "lib",
		Resources: map[string]runtime.ResourceRegistration{
			"thing": runtime.MakeResource[Alias, *ThingOutput, any](
				runtime.ResourceDefinition[Alias, *ThingOutput, any]{SchemaVersion: 1},
			),
		},
	}
}

type Thing struct {
	Name   *string
	Domain string
	Count  int
	Tags   map[string]string
}

type ThingOutput struct{ ID string }

type Alias = models.InputAlias
