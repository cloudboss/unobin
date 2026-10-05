package fileimports

import (
	types "example.com/fileimports/left"
	clock "example.com/fileimports/right"
	"github.com/cloudboss/unobin/pkg/runtime"
)

var _ clock.Record

func Library() *runtime.Library {
	return &runtime.Library{
		Name:          "fileimports",
		Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},
		Configuration: LibraryConfiguration(),
		DataSources: map[string]runtime.DataSourceRegistration{
			"remote": runtime.MakeDataSource[types.Query, *types.Result, any](),
			"local":  runtime.MakeDataSource[Local, *LocalOutput, any](),
		},
	}
}
