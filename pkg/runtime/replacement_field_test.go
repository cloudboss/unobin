package runtime

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

type selectorTestNested struct {
	Value string `ub:"value"`
}

type selectorTestInput struct {
	Name        string              `ub:"name"`
	Nested      *selectorTestNested `ub:"nested"`
	Labels      map[string]string   `ub:"labels,sensitive"`
	Ignored     string              `ub:"-"`
	hidden      string
	Empty       struct{}
	Unsupported chan string
}

type selectorTestConfig struct {
	Region string `ub:"provider-region"`
}

type selectorTestOutput struct {
	ID string `ub:"provider-id"`
}

func TestReplacementFieldsResolveCanonicalMetadata(t *testing.T) {
	tests := []struct {
		name      string
		field     AnyInputField[selectorTestInput]
		index     []int
		path      string
		valueType reflect.Type
		sensitive bool
	}{
		{
			name: "direct",
			field: InputField(func(v *selectorTestInput) *string {
				return &v.Name
			}),
			index:     []int{0},
			path:      "name",
			valueType: reflect.TypeFor[string](),
		},
		{
			name: "nested",
			field: InputField(func(v *selectorTestInput) *string {
				return &v.Nested.Value
			}),
			index:     []int{1, 0},
			path:      "nested.value",
			valueType: reflect.TypeFor[string](),
		},
		{
			name: "atomic map",
			field: InputField(func(v *selectorTestInput) *map[string]string {
				return &v.Labels
			}),
			index:     []int{2},
			path:      "labels",
			valueType: reflect.TypeFor[map[string]string](),
			sensitive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveInputField(tt.field)
			require.NoError(t, err)
			require.Equal(t, tt.index, got.index)
			require.Equal(t, tt.path, got.path)
			require.Equal(t, reflect.TypeFor[selectorTestInput](), got.rootType)
			require.Equal(t, tt.valueType, got.valueType)
			require.Equal(t, tt.sensitive, got.sensitive)
			require.Regexp(t, `^[0-9a-f]{64}$`, got.schemaDigest)
			require.Nil(t, got.resolve)
		})
	}

	configuration, err := resolveConfigurationField(ConfigurationField(
		func(v *selectorTestConfig) *string { return &v.Region },
	))
	require.NoError(t, err)
	require.Equal(t, "provider-region", configuration.path)
	require.Equal(t, reflect.TypeFor[*selectorTestConfig](), configuration.rootType)

	output, err := resolveOutputField(OutputField(
		func(v *selectorTestOutput) *string { return &v.ID },
	))
	require.NoError(t, err)
	require.Equal(t, "provider-id", output.path)
	require.Equal(t, reflect.TypeFor[*selectorTestOutput](), output.rootType)
}

func TestReplacementFieldRejectsInvalidSelectors(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		field := InputField[selectorTestInput, string](nil)
		requireSelectorError(t, field, "selector is nil")
	})

	t.Run("panic", func(t *testing.T) {
		field := InputField(func(*selectorTestInput) *string {
			panic("bad selector")
		})
		requireSelectorError(t, field, "selector panicked: bad selector")
	})

	t.Run("nil result", func(t *testing.T) {
		field := InputField(func(*selectorTestInput) *string { return nil })
		requireSelectorError(t, field, "selector returned nil")
	})

	t.Run("outside root", func(t *testing.T) {
		external := "external"
		field := InputField(func(*selectorTestInput) *string { return &external })
		requireSelectorError(t, field, "does not select a field")
	})

	t.Run("non-deterministic", func(t *testing.T) {
		calls := 0
		field := InputField(func(v *selectorTestInput) *string {
			calls++
			if calls == 1 {
				return &v.Name
			}
			return &v.Nested.Value
		})
		requireSelectorError(t, field, "selected different fields")
	})

	t.Run("ignored", func(t *testing.T) {
		field := InputField(func(v *selectorTestInput) *string { return &v.Ignored })
		requireSelectorError(t, field, "ignored field")
	})

	t.Run("unexported", func(t *testing.T) {
		field := InputField(func(v *selectorTestInput) *string { return &v.hidden })
		requireSelectorError(t, field, "unexported field")
	})

	t.Run("zero sized", func(t *testing.T) {
		field := InputField(func(v *selectorTestInput) *struct{} { return &v.Empty })
		requireSelectorError(t, field, "zero-sized field")
	})

	t.Run("unsupported type", func(t *testing.T) {
		field := InputField(func(v *selectorTestInput) *chan string {
			return &v.Unsupported
		})
		requireSelectorError(t, field, "unsupported type chan string")
	})
}

func TestReplacementFieldRejectsInvalidRoots(t *testing.T) {
	input := InputField(func(v *string) *string { return v })
	_, err := resolveInputField(input)
	require.ErrorContains(t, err, "input root must be a struct")

	configuration := ConfigurationField(func(v selectorTestConfig) *string {
		return &v.Region
	})
	_, err = resolveConfigurationField(configuration)
	require.ErrorContains(t, err, "configuration root must be a pointer to a struct")

	output := OutputField(func(v selectorTestOutput) *string { return &v.ID })
	_, err = resolveOutputField(output)
	require.ErrorContains(t, err, "output root must be a pointer to a struct")
}

func requireSelectorError[In, Value any](
	t *testing.T,
	field InputDescriptor[In, Value],
	message string,
) {
	t.Helper()
	_, err := resolveInputField(field)
	require.ErrorContains(t, err, message)
}
