package program

import (
	"github.com/cloudboss/unobin/pkg/asset"
	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type Imports struct {
	Top                  []resolve.Resolution
	Libraries            map[string]*runtime.Library
	LibraryConfigSchemas map[string]runtime.LibraryConfigSchema
	GoModules            map[string]string
	UBLibraries          []Library
	Assets               *asset.Collection
	RootAssetSetID       string
	Compatibility        *golibrary.CompatibilityContext
	LibraryMetadata      []golibrary.PackageMetadata
}
