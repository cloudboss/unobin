package provider

import (
	"context"

	"github.com/cloudboss/unobin/pkg/runtime"
)

type Value struct{}
type Output struct{ Value string }

func (*Value) Read(context.Context, runtime.NoConfig) (*Output, error) {
	return &Output{Value: "provided"}, nil
}

func Library() *runtime.Library {
	return &runtime.Library{
		Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},
		DataSources: map[string]runtime.DataSourceRegistration{
			"value": runtime.MakeDataSource[Value, *Output, runtime.NoConfig](),
		},
	}
}
