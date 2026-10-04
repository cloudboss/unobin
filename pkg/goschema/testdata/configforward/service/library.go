package service

import (
	"example.com/aws/config"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type Bucket struct {
	Name string
}

type BucketOutput struct {
	ID string
}

func Library() *runtime.Library {
	return &runtime.Library{
		Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},

		Name:          "aws-service",
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"bucket": runtime.MakeResource[Bucket, *BucketOutput, any](
				runtime.ResourceDefinition[Bucket, *BucketOutput, any]{SchemaVersion: 1},
			),
		},
	}
}
