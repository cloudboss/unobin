package structdecode

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeRejectsLossyIntegerConversion(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
	}{
		{name: "fraction", value: 80.5},
		{name: "negative fraction", value: -1.75},
		{name: "positive overflow", value: int64(math.MaxInt32) + 1},
		{name: "negative overflow", value: int64(math.MinInt32) - 1},
		{name: "float overflow", value: float64(math.MaxInt32) + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got struct{ Port int32 }
			err := Decode(&got, map[string]any{"port": tc.value})
			assert.Error(t, err, "Conversion of %v to int32 must fail; decoded %d", tc.value, got.Port)
		})
	}
}

func TestDecodeIntegerBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		target  reflect.Type
		value   any
		want    any
		invalid bool
	}{
		{"int8 max", reflect.TypeFor[int8](), int64(127), int8(127), false},
		{"int8 overflow", reflect.TypeFor[int8](), int64(128), nil, true},
		{"int16 min", reflect.TypeFor[int16](), int64(-32768), int16(-32768), false},
		{"int16 overflow", reflect.TypeFor[int16](), int64(-32769), nil, true},
		{"int64 max", reflect.TypeFor[int64](), int64(math.MaxInt64), int64(math.MaxInt64), false},
		{"int64 min", reflect.TypeFor[int64](), int64(math.MinInt64), int64(math.MinInt64), false},
		{"float at int64 limit", reflect.TypeFor[int64](), math.Exp2(63), nil, true},
		{"float at int64 min", reflect.TypeFor[int64](), -math.Exp2(63), int64(math.MinInt64), false},
		{"uint64 to int64", reflect.TypeFor[int64](), uint64(math.MaxUint64), nil, true},
		{"uint8 max", reflect.TypeFor[uint8](), int64(255), uint8(255), false},
		{"uint8 overflow", reflect.TypeFor[uint8](), int64(256), nil, true},
		{"uint16 negative", reflect.TypeFor[uint16](), int64(-1), nil, true},
		{"uint32 overflow", reflect.TypeFor[uint32](), uint64(math.MaxUint32) + 1, nil, true},
		{"uint64 max", reflect.TypeFor[uint64](), uint64(math.MaxUint64), uint64(math.MaxUint64), false},
		{"float at uint64 limit", reflect.TypeFor[uint64](), math.Exp2(64), nil, true},
		{"uint fraction", reflect.TypeFor[uint](), 1.75, nil, true},
		{"nan", reflect.TypeFor[int](), math.NaN(), nil, true},
		{"infinity", reflect.TypeFor[int](), math.Inf(1), nil, true},
		{"negative infinity", reflect.TypeFor[uint](), math.Inf(-1), nil, true},
		{"integral float", reflect.TypeFor[int32](), 80.0, int32(80), false},
		{"float32 fraction", reflect.TypeFor[int32](), float32(1.75), nil, true},
		{"json overflow", reflect.TypeFor[int32](), json.Number("2147483648"), nil, true},
		{"json fraction", reflect.TypeFor[int32](), json.Number("1.75"), nil, true},
		{"json uint max", reflect.TypeFor[uint64](),
			json.Number("18446744073709551615"), uint64(math.MaxUint64), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			typ := reflect.StructOf([]reflect.StructField{{Name: "Port", Type: tc.target}})
			got := reflect.New(typ)
			err := Decode(got.Interface(), map[string]any{"port": tc.value})
			if tc.invalid {
				require.ErrorContains(t, err, "Port")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got.Elem().Field(0).Interface())
		})
	}
}

func TestDecodeRejectsNestedIntegerFractions(t *testing.T) {
	for _, input := range []map[string]any{
		{"pointer": 1.75},
		{"list": []any{1.75}},
		{"values": map[string]any{"key": 1.75}},
		{"nested": map[string]any{"port": 1.75}},
	} {
		var got struct {
			Pointer *int32
			List    []int32
			Values  map[string]int32
			Nested  struct{ Port int32 }
		}
		require.Error(t, Decode(&got, input))
	}
}
