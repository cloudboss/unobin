package program

import (
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type LibrarySpec struct {
	Constraints map[string][]lang.ConstraintSpec
	Defaults    map[string][]lang.DefaultSpec
	Schema      *runtime.LibrarySchema
}

func (s LibrarySpec) Empty() bool {
	if len(s.Constraints) > 0 || len(s.Defaults) > 0 {
		return false
	}
	schema := s.Schema
	if schema == nil {
		return true
	}
	if schema.HasConfiguration || schema.Configuration != nil ||
		len(schema.ConfigurationFields) > 0 || len(schema.ConfigurationDefaults) > 0 ||
		len(schema.ConfigurationConstraints) > 0 || schema.ConfigurationIdentity != "" ||
		schema.ConfigurationDigest != "" || schema.ConfigurationEmpty {
		return false
	}
	for _, types := range []map[string]*runtime.TypeSchema{
		schema.Resources, schema.DataSources, schema.Actions,
	} {
		for _, typ := range types {
			if typ != nil && (len(typ.SensitiveInputs) > 0 || len(typ.SensitiveOutputs) > 0) {
				return false
			}
		}
	}
	return true
}

type Composite struct {
	Category             string
	Export               string
	Body                 syntax.FactoryBody
	Imports              []resolve.Resolution
	Libraries            map[string]*runtime.Library
	LibraryConfigSchemas map[string]runtime.LibraryConfigSchema
	AssetSetID           string
}

type Library struct {
	Name         string
	CanonicalKey string
	Composites   []Composite
	Specs        map[string]LibrarySpec
	SourceFiles  map[string]syntax.SourceFileSpec
}
