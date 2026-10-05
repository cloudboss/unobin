package fileimports

import (
	"example.com/fileimports/config"
	types "github.com/cloudboss/unobin/pkg/sdk/cfg"
)

func LibraryConfiguration() *types.ConfigurationType[*options.Config] {
	return options.LibraryConfiguration()
}
