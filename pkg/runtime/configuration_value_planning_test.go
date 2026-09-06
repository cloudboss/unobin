package runtime

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/typecheck"
)

func TestEncodePlanningConfigurationValuePreservesKinds(t *testing.T) {
	number, err := NumberValue(1.5)
	require.NoError(t, err)
	list, err := ListValue([]EncodedValue{StringValue("one"), StringValue("two")})
	require.NoError(t, err)
	mapping, err := MapValue(map[string]EncodedValue{"team": StringValue("runtime")})
	require.NoError(t, err)
	object := operationObject(t, map[string]EncodedValue{"enabled": BooleanValue(true)})
	tuple, err := ListValue([]EncodedValue{StringValue("name"), IntegerValue(2)})
	require.NoError(t, err)
	pending, err := PendingEncodedValue([]string{"resource.server.id"})
	require.NoError(t, err)
	content, err := ListValue([]EncodedValue{IntegerValue(0), IntegerValue(255)})
	require.NoError(t, err)

	tests := []struct {
		name  string
		typ   typecheck.Type
		value any
		want  EncodedValue
	}{
		{name: "bytes", typ: typecheck.TBytes(), value: []byte{0, 255}, want: content},
		{name: "recorded bytes", typ: typecheck.TBytes(), value: []any{int64(0), int64(255)},
			want: content},
		{name: "null", typ: typecheck.TOptional(typecheck.TString()), want: NullValue()},
		{name: "boolean", typ: typecheck.TBoolean(), value: true, want: BooleanValue(true)},
		{name: "string", typ: typecheck.TString(), value: "value", want: StringValue("value")},
		{name: "integer", typ: typecheck.TInteger(), value: int64(2), want: IntegerValue(2)},
		{name: "number", typ: typecheck.TNumber(), value: 1.5, want: number},
		{
			name:  "list",
			typ:   typecheck.TList(typecheck.TString()),
			value: []any{"one", "two"},
			want:  list,
		},
		{
			name:  "map",
			typ:   typecheck.TMap(typecheck.TString()),
			value: map[string]any{"team": "runtime"},
			want:  mapping,
		},
		{
			name: "object",
			typ: typecheck.TObject([]typecheck.ObjectField{{
				Name: "enabled",
				Type: typecheck.TBoolean(),
			}}),
			value: map[string]any{"enabled": true},
			want:  object,
		},
		{
			name:  "tuple",
			typ:   typecheck.TTuple([]typecheck.Type{typecheck.TString(), typecheck.TInteger()}),
			value: []any{"name", int64(2)},
			want:  tuple,
		},
		{
			name:  "pending",
			typ:   typecheck.TString(),
			value: PendingValue{Refs: []string{"resource.server.id"}},
			want:  pending,
		},
		{
			name:  "unknown map",
			typ:   typecheck.TUnknown(),
			value: map[string]any{"team": "runtime"},
			want:  mapping,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := encodePlanningValue(tt.typ, tt.value)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestEncodePlanningConfigurationValueRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		typ     typecheck.Type
		value   any
		message string
	}{
		{
			name:    "wrong scalar",
			typ:     typecheck.TString(),
			value:   int64(2),
			message: "expected string, got an integer",
		},
		{
			name:    "non-finite number",
			typ:     typecheck.TNumber(),
			value:   math.Inf(1),
			message: "number must be finite",
		},
		{
			name:    "invalid pending value",
			typ:     typecheck.TString(),
			value:   PendingValue{},
			message: "at least one pending reference is required",
		},
		{
			name:    "wrong collection",
			typ:     typecheck.TList(typecheck.TString()),
			value:   map[string]any{},
			message: "expected list, got an object",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := encodePlanningValue(tt.typ, tt.value)
			require.ErrorContains(t, err, tt.message)
		})
	}
}

func TestEncodePlanningConfigurationObjectRejectsInvalidFields(t *testing.T) {
	fields := []typecheck.ObjectField{
		{Name: "required", Type: typecheck.TString()},
		{Name: "optional", Type: typecheck.TString(), Optional: true},
	}

	_, err := encodePlanningConfigurationObject(fields, map[string]any{})
	require.ErrorContains(t, err, `field "required": required but not provided`)

	_, err = encodePlanningConfigurationObject(fields, map[string]any{
		"required": "value",
		"unknown":  true,
	})
	require.ErrorContains(t, err, `unknown field "unknown"`)

	got, err := encodePlanningConfigurationObject(fields, map[string]any{
		"required": "value",
	})
	require.NoError(t, err)
	require.Equal(t, operationObject(t, map[string]EncodedValue{
		"optional": AbsentValue(),
		"required": StringValue("value"),
	}), got)
}
