package runtime

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

type resourceValueNetwork struct {
	SubnetID string  `ub:"subnet-id"`
	Note     *string `ub:"note"`
}

type resourceValueInput struct {
	Name      string                       `ub:"name"`
	Optional  *string                      `ub:"optional"`
	Nullable  *string                      `ub:"nullable"`
	Zones     []string                     `ub:"zones"`
	Labels    map[string]string            `ub:"labels"`
	Endpoints map[string]*resourceValueURL `ub:"endpoints"`
	Network   *resourceValueNetwork        `ub:"network"`
	Enabled   bool                         `ub:"enabled"`
	Ratio     float64                      `ub:"ratio"`
	Small     int8                         `ub:"small"`
	Ignored   string                       `ub:"-"`
}

type resourceValueURL struct {
	Value string `ub:"value"`
}

type resourceValueOutput struct {
	ID       string                 `ub:"id"`
	Optional *string                `ub:"optional"`
	Items    []int                  `ub:"items"`
	Labels   map[string]string      `ub:"labels"`
	Network  *resourceValueNetwork  `ub:"network"`
	URLs     map[string]*string     `ub:"urls"`
	Counts   map[string]uint16      `ub:"counts"`
	Nested   []resourceValueNetwork `ub:"nested"`
}

func TestPrepareResourceInputsPreservesEncodedKinds(t *testing.T) {
	values := map[string]any{
		"name":     "server",
		"nullable": nil,
		"zones": []any{
			"zone-a",
			PendingValue{Refs: []string{"resource.zone.id", "resource.name.id"}},
		},
		"labels": map[string]any{"env": "test"},
		"endpoints": map[string]any{
			"api": map[string]any{"value": "https://example.test"},
		},
		"network": map[string]any{"subnet-id": "subnet-1"},
		"enabled": true,
		"ratio":   int64(2),
		"small":   int64(7),
	}

	encoded, input, err := prepareResourceInputs[resourceValueInput](values)
	require.NoError(t, err)
	require.Equal(t, resourceValueInput{
		Name:    "server",
		Zones:   []string{"zone-a", ""},
		Labels:  map[string]string{"env": "test"},
		Network: &resourceValueNetwork{SubnetID: "subnet-1"},
		Enabled: true,
		Ratio:   2,
		Small:   7,
		Endpoints: map[string]*resourceValueURL{
			"api": {Value: "https://example.test"},
		},
	}, input)

	pending, err := PendingEncodedValue([]string{
		"resource.zone.id",
		"resource.name.id",
	})
	require.NoError(t, err)
	want := mustResourceObject(t, map[string]EncodedValue{
		"name":     StringValue("server"),
		"optional": AbsentValue(),
		"nullable": NullValue(),
		"zones": mustResourceList(t, []EncodedValue{
			StringValue("zone-a"),
			pending,
		}),
		"labels": mustResourceMap(t, map[string]EncodedValue{
			"env": StringValue("test"),
		}),
		"endpoints": mustResourceMap(t, map[string]EncodedValue{
			"api": mustResourceObject(t, map[string]EncodedValue{
				"value": StringValue("https://example.test"),
			}),
		}),
		"network": mustResourceObject(t, map[string]EncodedValue{
			"subnet-id": StringValue("subnet-1"),
			"note":      AbsentValue(),
		}),
		"enabled": BooleanValue(true),
		"ratio":   numberValueForTest(t, 2),
		"small":   IntegerValue(7),
	})
	require.True(t, encodedValuesEqual(want, encoded))
}

func TestPrepareResourceInputsRejectsInvalidValues(t *testing.T) {
	valid := func() map[string]any {
		return map[string]any{
			"name":      "server",
			"optional":  nil,
			"nullable":  nil,
			"zones":     []any{},
			"labels":    map[string]any{},
			"endpoints": map[string]any{},
			"network":   nil,
			"enabled":   true,
			"ratio":     float64(1),
			"small":     int64(1),
		}
	}
	tests := []struct {
		name    string
		change  func(map[string]any)
		message string
	}{
		{
			name:    "unknown field",
			change:  func(v map[string]any) { v["other"] = true },
			message: `unknown field "other"`,
		},
		{
			name:    "null required scalar",
			change:  func(v map[string]any) { v["name"] = nil },
			message: `field "name": null is invalid for string`,
		},
		{
			name:    "wrong scalar kind",
			change:  func(v map[string]any) { v["name"] = true },
			message: `field "name": expected string`,
		},
		{
			name:    "integer overflow",
			change:  func(v map[string]any) { v["small"] = int64(128) },
			message: `field "small": integer overflows int8`,
		},
		{
			name:    "non-finite number",
			change:  func(v map[string]any) { v["ratio"] = math.NaN() },
			message: `field "ratio": number must be finite`,
		},
		{
			name: "invalid pending value",
			change: func(v map[string]any) {
				v["name"] = PendingValue{}
			},
			message: `field "name": at least one pending reference is required`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := valid()
			tt.change(values)
			_, _, err := prepareResourceInputs[resourceValueInput](values)
			require.ErrorContains(t, err, tt.message)
		})
	}
}

func TestDecodeResourceInputsRequiresConcreteCompleteObject(t *testing.T) {
	values := map[string]any{
		"name":      "server",
		"nullable":  nil,
		"zones":     []any{"zone-a"},
		"labels":    map[string]any{"env": "test"},
		"endpoints": map[string]any{},
		"network":   nil,
		"enabled":   false,
		"ratio":     float64(1.5),
		"small":     int64(4),
	}
	encoded, prepared, err := prepareResourceInputs[resourceValueInput](values)
	require.NoError(t, err)

	decoded, err := decodeResourceInputs[resourceValueInput](encoded)
	require.NoError(t, err)
	require.Equal(t, prepared, decoded)

	pending, err := PendingEncodedValue([]string{"resource.zone.id"})
	require.NoError(t, err)
	_, err = decodeResourceInputs[resourceValueInput](mustResourceObject(
		t,
		map[string]EncodedValue{
			"name":      pending,
			"optional":  AbsentValue(),
			"nullable":  NullValue(),
			"zones":     mustResourceList(t, nil),
			"labels":    mustResourceMap(t, nil),
			"endpoints": mustResourceMap(t, nil),
			"network":   NullValue(),
			"enabled":   BooleanValue(false),
			"ratio":     numberValueForTest(t, 1),
			"small":     IntegerValue(1),
		},
	))
	require.ErrorContains(t, err, `field "name": pending value is not concrete`)

	_, err = decodeResourceInputs[resourceValueInput](mustResourceObject(
		t,
		map[string]EncodedValue{"name": StringValue("server")},
	))
	require.ErrorContains(t, err, `field "optional" is missing`)

	fields, ok := encoded.ObjectFields()
	require.True(t, ok)
	fields["other"] = BooleanValue(true)
	_, err = decodeResourceInputs[resourceValueInput](mustResourceObject(t, fields))
	require.ErrorContains(t, err, `unknown field "other"`)
}

func TestResourceOutputsEncodeAndDecode(t *testing.T) {
	value := "secret"
	outputs := &resourceValueOutput{
		ID:       "server-1",
		Optional: &value,
		URLs:     map[string]*string{"api": nil},
		Counts:   map[string]uint16{"ok": 3},
		Nested:   []resourceValueNetwork{{SubnetID: "subnet-1"}},
	}

	encoded, err := encodeResourceOutputs(outputs)
	require.NoError(t, err)
	want := mustResourceObject(t, map[string]EncodedValue{
		"id":       StringValue("server-1"),
		"optional": StringValue("secret"),
		"items":    mustResourceList(t, nil),
		"labels":   mustResourceMap(t, nil),
		"network":  NullValue(),
		"urls": mustResourceMap(t, map[string]EncodedValue{
			"api": NullValue(),
		}),
		"counts": mustResourceMap(t, map[string]EncodedValue{
			"ok": IntegerValue(3),
		}),
		"nested": mustResourceList(t, []EncodedValue{
			mustResourceObject(t, map[string]EncodedValue{
				"subnet-id": StringValue("subnet-1"),
				"note":      NullValue(),
			}),
		}),
	})
	require.True(t, encodedValuesEqual(want, encoded))

	decoded, err := decodeResourceOutputs[*resourceValueOutput](encoded)
	require.NoError(t, err)
	wantDecoded := *outputs
	wantDecoded.Items = []int{}
	wantDecoded.Labels = map[string]string{}
	require.Equal(t, &wantDecoded, decoded)

	fields, ok := encoded.ObjectFields()
	require.True(t, ok)
	fields["id"], err = PendingEncodedValue([]string{"resource.source.id"})
	require.NoError(t, err)
	_, err = decodeResourceOutputs[*resourceValueOutput](mustResourceObject(t, fields))
	require.ErrorContains(t, err, `field "id": pending value is not concrete`)
}

func TestResourceOutputsRequireNonNilPointerToStruct(t *testing.T) {
	_, err := encodeResourceOutputs[*resourceValueOutput](nil)
	require.ErrorContains(t, err, "resource outputs must not be nil")

	_, err = encodeResourceOutputs(resourceValueOutput{})
	require.ErrorContains(t, err, "output root must be a pointer to a struct")

	_, err = decodeResourceOutputs[resourceValueOutput](mustResourceObject(t, nil))
	require.ErrorContains(t, err, "output root must be a pointer to a struct")
}

func TestResourceValueConversionSupportsEmptyRoots(t *testing.T) {
	inputs, typed, err := prepareResourceInputs[struct{}](map[string]any{})
	require.NoError(t, err)
	require.Equal(t, struct{}{}, typed)
	require.True(t, encodedValuesEqual(mustResourceObject(t, nil), inputs))

	outputs, err := encodeResourceOutputs(&struct{}{})
	require.NoError(t, err)
	require.True(t, encodedValuesEqual(mustResourceObject(t, nil), outputs))

	decoded, err := decodeResourceOutputs[*struct{}](outputs)
	require.NoError(t, err)
	require.Equal(t, &struct{}{}, decoded)
}

type resourceValueUnsigned struct {
	Value uint64 `ub:"value"`
}

func TestResourceValueConversionRejectsUnrepresentableIntegers(t *testing.T) {
	_, _, err := prepareResourceInputs[resourceValueUnsigned](map[string]any{
		"value": uint64(math.MaxInt64) + 1,
	})
	require.ErrorContains(t, err, "integer exceeds int64")

	_, err = encodeResourceOutputs(&resourceValueUnsigned{
		Value: uint64(math.MaxInt64) + 1,
	})
	require.ErrorContains(t, err, "integer exceeds int64")

	_, _, err = prepareResourceInputs[resourceValueUnsigned](map[string]any{
		"value": int64(-1),
	})
	require.ErrorContains(t, err, "integer overflows uint64")
}

type resourceValueRecursive struct {
	Next *resourceValueRecursive `ub:"next"`
}

type resourceValueBadMap struct {
	Values map[int]string `ub:"values"`
}

type resourceValueDuplicate struct {
	First  string `ub:"value"`
	Second string `ub:"value"`
}

type resourceValueZeroSized struct {
	Empty struct{} `ub:"empty"`
}

func TestResourceValueConversionRejectsUnsupportedSchemas(t *testing.T) {
	tests := []struct {
		name    string
		convert func() error
		message string
	}{
		{
			name: "recursive",
			convert: func() error {
				_, _, err := prepareResourceInputs[resourceValueRecursive](nil)
				return err
			},
			message: "recursive type",
		},
		{
			name: "non-string map key",
			convert: func() error {
				_, _, err := prepareResourceInputs[resourceValueBadMap](nil)
				return err
			},
			message: "unsupported map key type int",
		},
		{
			name: "duplicate field name",
			convert: func() error {
				_, _, err := prepareResourceInputs[resourceValueDuplicate](nil)
				return err
			},
			message: `field name "value" is ambiguous`,
		},
		{
			name: "zero-sized field",
			convert: func() error {
				_, _, err := prepareResourceInputs[resourceValueZeroSized](nil)
				return err
			},
			message: `zero-sized field "empty"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorContains(t, tt.convert(), tt.message)
		})
	}
}

func mustResourceObject(t *testing.T, fields map[string]EncodedValue) EncodedValue {
	t.Helper()
	if fields == nil {
		fields = map[string]EncodedValue{}
	}
	value, err := ObjectValue(fields)
	require.NoError(t, err)
	return value
}

func mustResourceMap(t *testing.T, entries map[string]EncodedValue) EncodedValue {
	t.Helper()
	if entries == nil {
		entries = map[string]EncodedValue{}
	}
	value, err := MapValue(entries)
	require.NoError(t, err)
	return value
}

func mustResourceList(t *testing.T, items []EncodedValue) EncodedValue {
	t.Helper()
	if items == nil {
		items = []EncodedValue{}
	}
	value, err := ListValue(items)
	require.NoError(t, err)
	return value
}

func numberValueForTest(t *testing.T, number float64) EncodedValue {
	t.Helper()
	value, err := NumberValue(number)
	require.NoError(t, err)
	return value
}
