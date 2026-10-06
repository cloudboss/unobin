package runner

import (
	"maps"
	"slices"

	"github.com/cloudboss/unobin/pkg/backends"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/factorycli"
)

type Info = factorycli.Info
type Registries = factorycli.Registries
type Format = factorycli.Format

const (
	EnvVarPrefix = factorycli.EnvVarPrefix
	FormatText   = factorycli.FormatText
	FormatJSON   = factorycli.FormatJSON
	FormatUnobin = factorycli.FormatUnobin
)

func ParseFormat(value string) (Format, error) {
	return factorycli.ParseFormat(value)
}

// Run enables all built-in types when Info.Registries is nil.
func Run(info Info) {
	factorycli.Run(withDefaultRegistries(info))
}

func withDefaultRegistries(info Info) Info {
	if info.Registries != nil {
		return info
	}
	info.Registries = &Registries{}
	availableBackends := backends.Backends()
	for _, name := range slices.Sorted(maps.Keys(availableBackends)) {
		info.Registries.Backends = append(info.Registries.Backends, availableBackends[name])
	}
	availableEncrypters := encrypters.Encrypters()
	for _, name := range slices.Sorted(maps.Keys(availableEncrypters)) {
		info.Registries.Encrypters = append(info.Registries.Encrypters, availableEncrypters[name])
	}
	return info
}
