package runtime

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/asset"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

func TestEvaluatedInputTypes(t *testing.T) {
	object := typecheck.TObject([]typecheck.ObjectField{
		{Name: "name", Type: typecheck.TString()},
	})
	for _, tc := range []struct {
		name  string
		typ   typecheck.Type
		value any
		path  string
	}{
		{"string", typecheck.TString(), int64(42), "input"},
		{"boolean", typecheck.TBoolean(), "false", "input"},
		{"integer", typecheck.TInteger(), 1.75, "input"},
		{"number", typecheck.TNumber(), "1.75", "input"},
		{"object", object, []any{}, "input"},
		{"list", typecheck.TList(typecheck.TString()), map[string]any{}, "input"},
		{"map", typecheck.TMap(typecheck.TString()), []any{}, "input"},
		{"list element", typecheck.TList(typecheck.TString()), []any{true}, "input[0]"},
		{"map element", typecheck.TMap(typecheck.TString()),
			map[string]any{"key": int64(42)}, `input["key"]`},
		{"object field", object, map[string]any{"name": int64(42)}, "input.name"},
		{"optional", typecheck.TOptional(typecheck.TString()), false, "input"},
		{"bytes", typecheck.TBytes(), "ordinary text", "input"},
		{"byte list element", typecheck.TBytes(), []any{true}, "input[0]"},
		{"tuple length", typecheck.TTuple([]typecheck.Type{typecheck.TString()}), []any{}, "input"},
		{"tuple element", typecheck.TTuple([]typecheck.Type{typecheck.TString()}),
			[]any{true}, "input[0]"},
		{"known element beside pending", typecheck.TList(typecheck.TString()),
			[]any{PendingValue{}, false}, "input[1]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := (optionalInputPaths{}).checkField(tc.typ, tc.value, true, "input")
			require.ErrorContains(t, err, "expected")
			require.ErrorContains(t, err, fmt.Sprintf("%q", tc.path))
		})
	}
}

func TestEvaluatedInputTypesAcceptCompatibleValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		typ   typecheck.Type
		value any
	}{
		{"string", typecheck.TString(), "value"},
		{"boolean", typecheck.TBoolean(), false},
		{"integer", typecheck.TInteger(), int64(80)},
		{"number from integer", typecheck.TNumber(), int64(80)},
		{"number", typecheck.TNumber(), 1.75},
		{"null", typecheck.TNull(), nil},
		{"opaque", typecheck.TOpaque(), map[string]any{"value": true}},
		{"unknown", typecheck.TUnknown(), 1.75},
		{"optional", typecheck.TOptional(typecheck.TString()), nil},
		{"pending", typecheck.TInteger(), PendingValue{}},
		{"nested pending", typecheck.TList(typecheck.TString()), []any{PendingValue{}, "ok"}},
		{"tuple", typecheck.TTuple([]typecheck.Type{typecheck.TString(), typecheck.TInteger()}),
			[]any{"ok", int64(80)}},
		{"asset path", typecheck.TString(), asset.PathRef("path")},
		{"bytes", typecheck.TBytes(), []byte{0, 255}},
		{"byte list", typecheck.TBytes(), []any{int64(0), int64(255)}},
		{"asset content", typecheck.TBytes(), asset.ContentRef("content")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t,
				(optionalInputPaths{}).checkField(tc.typ, tc.value, true, "input"))
		})
	}
}
