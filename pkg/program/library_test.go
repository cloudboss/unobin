package program

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

func TestLibrarySpecRetainsRuntimeMetadata(t *testing.T) {
	for _, test := range []struct {
		name  string
		spec  LibrarySpec
		empty bool
	}{
		{name: "empty", empty: true},
		{name: "empty schema", spec: LibrarySpec{Schema: &runtime.LibrarySchema{}}, empty: true},
		{name: "static types", empty: true, spec: LibrarySpec{Schema: &runtime.LibrarySchema{
			Resources: map[string]*runtime.TypeSchema{
				"file": {Inputs: map[string]typecheck.Type{"path": typecheck.TString()}},
			},
		}}},
		{name: "defaults", spec: LibrarySpec{Defaults: map[string][]lang.DefaultSpec{
			"resource.file": {{Field: "input.mode", Value: "420"}},
		}}},
		{name: "constraints", spec: LibrarySpec{Constraints: map[string][]lang.ConstraintSpec{
			"action.run": {{Kind: "required-together", Fields: []string{"input.a", "input.b"}}},
		}}},
		{name: "sensitivity", spec: LibrarySpec{Schema: &runtime.LibrarySchema{
			DataSources: map[string]*runtime.TypeSchema{
				"query": {SensitiveOutputs: []string{"value"}},
			},
		}}},
		{name: "configuration", spec: LibrarySpec{Schema: &runtime.LibrarySchema{
			HasConfiguration: true, ConfigurationFields: []typecheck.ObjectField{
				{Name: "region", Type: typecheck.TString()},
			},
		}}},
		{name: "empty configuration", spec: LibrarySpec{Schema: &runtime.LibrarySchema{
			ConfigurationEmpty: true,
		}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.empty, test.spec.Empty())
		})
	}
}
