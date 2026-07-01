package service

import (
	"context"

	"example.com/aws/awscfg"
	"example.com/aws/config"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type Region struct{}

type RegionOutput struct {
	Region string
}

func (r *Region) Run(_ context.Context, cfg *awscfg.Configuration) (*RegionOutput, error) {
	return &RegionOutput{Region: cfg.Region}, nil
}

func Library() *runtime.Library {
	return &runtime.Library{
		Name:          "aws-service",
		Configuration: config.LibraryConfiguration(),
		Actions: map[string]runtime.ActionRegistration{
			"region": runtime.MakeAction[Region, *RegionOutput, *awscfg.Configuration](),
		},
	}
}
