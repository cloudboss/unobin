package runtime

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

type descriptorNetwork struct {
	SubnetID string            `ub:"subnet-id"`
	Labels   map[string]string `ub:",sensitive"`
}

type descriptorCredentials struct {
	Token string
}

type descriptorInput struct {
	DisplayName string
	Region      string                `ub:"provider-region"`
	Network     *descriptorNetwork    `ub:"network"`
	Tags        map[string]string     `ub:"tags"`
	Zones       []string              `ub:"zones"`
	Credentials descriptorCredentials `ub:"credentials,sensitive"`
	Optional    *string               `ub:"optional"`
}

type descriptorOutput struct {
	ID      string             `ub:"provider-id"`
	Network *descriptorNetwork `ub:"network"`
}

func TestInputFieldResolvesDirectNestedAndAtomicFields(t *testing.T) {
	tests := []struct {
		name      string
		field     AnyInputField[descriptorInput]
		index     []int
		path      string
		valueType reflect.Type
		sensitive bool
	}{
		{
			name: "derived name",
			field: InputField(func(v *descriptorInput) *string {
				return &v.DisplayName
			}),
			index:     []int{0},
			path:      "display-name",
			valueType: reflect.TypeFor[string](),
		},
		{
			name: "explicit name",
			field: InputField(func(v *descriptorInput) *string {
				return &v.Region
			}),
			index:     []int{1},
			path:      "provider-region",
			valueType: reflect.TypeFor[string](),
		},
		{
			name: "nested pointer",
			field: InputField(func(v *descriptorInput) *string {
				return &v.Network.SubnetID
			}),
			index:     []int{2, 0},
			path:      "network.subnet-id",
			valueType: reflect.TypeFor[string](),
		},
		{
			name: "pointer field",
			field: InputField(func(v *descriptorInput) **descriptorNetwork {
				return &v.Network
			}),
			index:     []int{2},
			path:      "network",
			valueType: reflect.TypeFor[*descriptorNetwork](),
		},
		{
			name: "map",
			field: InputField(func(v *descriptorInput) *map[string]string {
				return &v.Tags
			}),
			index:     []int{3},
			path:      "tags",
			valueType: reflect.TypeFor[map[string]string](),
		},
		{
			name: "list",
			field: InputField(func(v *descriptorInput) *[]string {
				return &v.Zones
			}),
			index:     []int{4},
			path:      "zones",
			valueType: reflect.TypeFor[[]string](),
		},
		{
			name: "inherited sensitivity",
			field: InputField(func(v *descriptorInput) *string {
				return &v.Credentials.Token
			}),
			index:     []int{5, 0},
			path:      "credentials.token",
			valueType: reflect.TypeFor[string](),
			sensitive: true,
		},
		{
			name: "optional scalar",
			field: InputField(func(v *descriptorInput) **string {
				return &v.Optional
			}),
			index:     []int{6},
			path:      "optional",
			valueType: reflect.TypeFor[*string](),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveInputField(tt.field)
			require.NoError(t, err)
			require.Equal(t, tt.index, got.index)
			require.Equal(t, tt.path, got.path)
			require.Equal(t, reflect.TypeFor[descriptorInput](), got.rootType)
			require.Equal(t, tt.valueType, got.valueType)
			require.Equal(t, tt.sensitive, got.sensitive)
			require.Regexp(t, `^[0-9a-f]{64}$`, got.schemaDigest)
			require.Nil(t, got.resolve)
		})
	}
}

func TestOutputFieldResolvesPointerRoot(t *testing.T) {
	field := OutputField(func(v *descriptorOutput) *string {
		return &v.Network.SubnetID
	})

	got, err := resolveOutputField(field)
	require.NoError(t, err)
	require.Equal(t, []int{1, 0}, got.index)
	require.Equal(t, "network.subnet-id", got.path)
	require.Equal(t, reflect.TypeFor[*descriptorOutput](), got.rootType)
	require.Equal(t, reflect.TypeFor[string](), got.valueType)
	require.Regexp(t, `^[0-9a-f]{64}$`, got.schemaDigest)
}

func TestFieldSelectorRunsOnlyDuringResolution(t *testing.T) {
	inputCalls := 0
	input := InputField(func(v *descriptorInput) *string {
		inputCalls++
		return &v.DisplayName
	})
	outputCalls := 0
	output := OutputField(func(v *descriptorOutput) *string {
		outputCalls++
		return &v.ID
	})
	require.Zero(t, inputCalls)
	require.Zero(t, outputCalls)

	first, err := resolveInputField(input)
	require.NoError(t, err)
	second, err := resolveInputField(input)
	require.NoError(t, err)
	_, err = resolveOutputField(output)
	require.NoError(t, err)

	require.Equal(t, 4, inputCalls)
	require.Equal(t, 2, outputCalls)
	require.Equal(t, first.index, second.index)
	require.Equal(t, first.path, second.path)
	require.Equal(t, first.rootType, second.rootType)
	require.Equal(t, first.valueType, second.valueType)
	require.Equal(t, first.schemaDigest, second.schemaDigest)
	require.Equal(t, first.sensitive, second.sensitive)
}

type invalidDescriptorRecursive struct {
	Name string
	Next *invalidDescriptorRecursive
}

type opaqueDescriptorValue struct {
	_ byte
}

type invalidDescriptorInput struct {
	First       string
	Second      string
	Ignored     string `ub:"-"`
	hidden      string
	Values      []string
	Empty       struct{}
	Unsupported chan string
	Recursive   *invalidDescriptorRecursive
	Opaque      opaqueDescriptorValue
	DuplicateA  string `ub:"duplicate"`
	DuplicateB  string `ub:"duplicate"`
}

func TestInputFieldRejectsInvalidSelectors(t *testing.T) {
	t.Run("nil selector", func(t *testing.T) {
		field := InputField[invalidDescriptorInput, string](nil)
		requireInputFieldError(t, field, "selector is nil")
	})

	t.Run("panic", func(t *testing.T) {
		field := InputField(func(*invalidDescriptorInput) *string {
			panic("bad selector")
		})
		requireInputFieldError(t, field, "selector panicked: bad selector")
	})

	t.Run("nil result", func(t *testing.T) {
		field := InputField(func(*invalidDescriptorInput) *string { return nil })
		requireInputFieldError(t, field, "selector returned nil")
	})

	t.Run("outside root", func(t *testing.T) {
		external := "external"
		field := InputField(func(*invalidDescriptorInput) *string { return &external })
		requireInputFieldError(t, field, "does not select a field")
	})

	t.Run("different fields", func(t *testing.T) {
		calls := 0
		field := InputField(func(v *invalidDescriptorInput) *string {
			calls++
			if calls == 1 {
				return &v.First
			}
			return &v.Second
		})
		requireInputFieldError(t, field, "selected different fields")
	})

	t.Run("ignored field", func(t *testing.T) {
		field := InputField(func(v *invalidDescriptorInput) *string { return &v.Ignored })
		requireInputFieldError(t, field, "ignored field")
	})

	t.Run("unexported field", func(t *testing.T) {
		field := InputField(func(v *invalidDescriptorInput) *string { return &v.hidden })
		requireInputFieldError(t, field, "unexported field")
	})

	t.Run("collection element", func(t *testing.T) {
		field := InputField(func(v *invalidDescriptorInput) *string {
			v.Values = []string{"one"}
			return &v.Values[0]
		})
		requireInputFieldError(t, field, "does not select a field")
	})

	t.Run("zero-sized field", func(t *testing.T) {
		field := InputField(func(v *invalidDescriptorInput) *struct{} { return &v.Empty })
		requireInputFieldError(t, field, "zero-sized field")
	})

	t.Run("unsupported type", func(t *testing.T) {
		field := InputField(func(v *invalidDescriptorInput) *chan string {
			return &v.Unsupported
		})
		requireInputFieldError(t, field, "unsupported type chan string")
	})

	t.Run("recursive type", func(t *testing.T) {
		field := InputField(func(v *invalidDescriptorInput) **invalidDescriptorRecursive {
			return &v.Recursive
		})
		requireInputFieldError(t, field, "recursive type")
	})

	t.Run("opaque struct", func(t *testing.T) {
		field := InputField(func(v *invalidDescriptorInput) *opaqueDescriptorValue {
			return &v.Opaque
		})
		requireInputFieldError(t, field, "unsupported type runtime.opaqueDescriptorValue")
	})

	t.Run("ambiguous path", func(t *testing.T) {
		field := InputField(func(v *invalidDescriptorInput) *string {
			return &v.DuplicateA
		})
		requireInputFieldError(t, field, `field path "duplicate" is ambiguous`)
	})
}

func TestFieldResolutionRejectsInvalidRoots(t *testing.T) {
	input := InputField(func(v *string) *string { return v })
	_, err := resolveInputField(input)
	require.ErrorContains(t, err, "input root must be a struct")

	output := OutputField(func(v descriptorOutput) *string { return &v.ID })
	_, err = resolveOutputField(output)
	require.ErrorContains(t, err, "output root must be a pointer to a struct")
}

func TestFieldResolutionRejectsMissingDescriptor(t *testing.T) {
	var missing AnyInputField[descriptorInput]
	_, err := resolveInputField(missing)
	require.ErrorContains(t, err, "input descriptor is nil")

	_, err = resolveInputField(InputDescriptor[descriptorInput, string]{})
	require.ErrorContains(t, err, "input descriptor is empty")

	var missingOutput AnyOutputField[*descriptorOutput]
	_, err = resolveOutputField(missingOutput)
	require.ErrorContains(t, err, "output descriptor is nil")
}

func requireInputFieldError[In, Value any](
	t *testing.T,
	field InputDescriptor[In, Value],
	message string,
) {
	t.Helper()
	_, err := resolveInputField(field)
	require.ErrorContains(t, err, message)
}
