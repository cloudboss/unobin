package buildcontext

import (
	"context"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name:          "buildcontext",
		Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},
		DataSources: map[string]runtime.DataSourceRegistration{
			"query": runtime.MakeDataSource[Query, *Output, any](),
		},
	}
}

type Query struct {
	Platform Output
	CPU      CPU
	Feature  Feature
	Native   Foreign
	Tuning   Tuning
}

func (q *Query) Read(context.Context, any) (*Output, error) {
	return &Output{}, nil
}
