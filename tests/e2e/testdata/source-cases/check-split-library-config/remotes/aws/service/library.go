package service

import (
	"example.com/aws/config"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type Bucket struct {
	Name string
}

func BucketDefinition() runtime.ResourceDefinition[Bucket, *BucketOutput, any] {
	return runtime.ResourceDefinition[Bucket, *BucketOutput, any]{
		SchemaVersion: 1,
		Identity: runtime.ResourceIdentity[Bucket, *BucketOutput]{
			Version: 1,
			Scope:   runtime.IdentityConfiguration,
		},
	}
}

type BucketOutput struct {
	ID string
}

func Library() *runtime.Library {
	return &runtime.Library{
		Name:          "aws-service",
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"bucket": runtime.MakeResource[Bucket, *BucketOutput, any](BucketDefinition()),
		},
	}
}
