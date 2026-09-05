package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type definitionSchemaField[Value any] struct {
	Value Value `ub:"value"`
}

type definitionSchemaRecursiveStruct struct {
	Next *definitionSchemaRecursiveStruct `ub:"next"`
}

type definitionSchemaRecursiveList []definitionSchemaRecursiveList

type definitionSchemaRecursiveMap map[string]definitionSchemaRecursiveMap

type definitionSchemaRecursivePointer *definitionSchemaRecursivePointer

type definitionSchemaDuplicateFields struct {
	First  string `ub:"same"`
	Second string `ub:"same"`
}

type definitionSchemaIgnoredFields struct {
	Name    string `ub:"name"`
	Client  func() `ub:"-"`
	private chan int
}

func definitionSchemaError[In, Out any]() error {
	_, err := resolveResourceDefinition(ResourceDefinition[In, Out, NoConfig]{
		SchemaVersion: 1,
		Identity: ResourceIdentity[In, Out]{
			Version: 1,
			Scope:   IdentityConfiguration,
		},
	})
	return err
}

func TestResourceDefinitionRejectsUnencodableSchemas(t *testing.T) {
	tests := []struct {
		name    string
		resolve func() error
		message string
	}{
		{
			name:    "input interface",
			resolve: definitionSchemaError[definitionSchemaField[any], *struct{}],
			message: "resource inputs: unsupported type interface {}",
		},
		{
			name:    "output function",
			resolve: definitionSchemaError[struct{}, *definitionSchemaField[func()]],
			message: "resource outputs: unsupported type func()",
		},
		{
			name:    "nested input map key",
			resolve: definitionSchemaError[definitionSchemaField[[]map[int]string], *struct{}],
			message: "resource inputs: unsupported map key type int",
		},
		{
			name: "nested output channel",
			resolve: definitionSchemaError[
				struct{}, *definitionSchemaField[map[string][]chan int],
			],
			message: "resource outputs: unsupported type chan int",
		},
		{
			name:    "duplicate input names",
			resolve: definitionSchemaError[definitionSchemaDuplicateFields, *struct{}],
			message: `resource inputs: field name "same" is ambiguous`,
		},
		{
			name: "nested duplicate output names",
			resolve: definitionSchemaError[
				struct{}, *definitionSchemaField[*definitionSchemaDuplicateFields],
			],
			message: `resource outputs: field name "same" is ambiguous`,
		},
		{
			name:    "zero sized input field",
			resolve: definitionSchemaError[definitionSchemaField[struct{}], *struct{}],
			message: `resource inputs: zero-sized field "value"`,
		},
		{
			name:    "zero sized output field",
			resolve: definitionSchemaError[struct{}, *definitionSchemaField[[0]string]],
			message: `resource outputs: zero-sized field "value"`,
		},
		{
			name:    "recursive input struct",
			resolve: definitionSchemaError[definitionSchemaRecursiveStruct, *struct{}],
			message: "resource inputs: recursive type",
		},
		{
			name: "recursive output list",
			resolve: definitionSchemaError[
				struct{}, *definitionSchemaField[definitionSchemaRecursiveList],
			],
			message: "resource outputs: recursive type",
		},
		{
			name: "recursive input map",
			resolve: definitionSchemaError[
				definitionSchemaField[definitionSchemaRecursiveMap], *struct{},
			],
			message: "resource inputs: recursive type",
		},
		{
			name: "recursive output pointer",
			resolve: definitionSchemaError[
				struct{}, *definitionSchemaField[definitionSchemaRecursivePointer],
			],
			message: "resource outputs: recursive type",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.ErrorContains(t, test.resolve(), test.message)
		})
	}
}

func TestResourceDefinitionAcceptsEncodableSchemas(t *testing.T) {
	tests := []struct {
		name    string
		resolve func() error
	}{
		{
			name:    "empty roots",
			resolve: definitionSchemaError[struct{}, *struct{}],
		},
		{
			name:    "nested values",
			resolve: definitionSchemaError[resourceValueInput, *resourceValueOutput],
		},
		{
			name: "repeated nested types",
			resolve: definitionSchemaError[struct {
				First  *resourceValueNetwork           `ub:"first"`
				Second map[string]resourceValueNetwork `ub:"second"`
				Third  [2]resourceValueNetwork         `ub:"third"`
				Fourth []*resourceValueNetwork         `ub:"fourth"`
			}, *resourceValueOutput],
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, test.resolve())
		})
	}
}

func TestResourceDefinitionIgnoresProviderFields(t *testing.T) {
	require.NoError(t,
		definitionSchemaError[definitionSchemaIgnoredFields, *definitionSchemaIgnoredFields](),
	)
	outputs, err := encodeResourceOutputs(&definitionSchemaIgnoredFields{
		Name:    "logs",
		Client:  func() {},
		private: make(chan int),
	})
	require.NoError(t, err)
	require.Equal(t, mustResourceObject(t, map[string]EncodedValue{
		"name": StringValue("logs"),
	}), outputs)
}
