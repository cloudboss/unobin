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
		Name:          "aws-service",
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"bucket": runtime.MakeResource[Bucket, *BucketOutput, any](),
		},
	}
}
