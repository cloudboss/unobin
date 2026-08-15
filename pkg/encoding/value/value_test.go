package value

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScalarValues(t *testing.T) {
	number, err := Number(1.25)
	require.NoError(t, err)

	tests := []struct {
		name string
		got  Value
		kind Kind
	}{
		{name: "absent", got: Absent(), kind: KindAbsent},
		{name: "null", got: Null(), kind: KindNull},
		{name: "boolean", got: Boolean(false), kind: KindBoolean},
		{name: "string", got: String("text"), kind: KindString},
		{name: "integer", got: Integer(math.MaxInt64), kind: KindInteger},
		{name: "number", got: number, kind: KindNumber},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.kind, tt.got.Kind())
		})
	}

	boolean, ok := Boolean(false).Boolean()
	assert.True(t, ok)
	assert.False(t, boolean)

	text, ok := String("text").String()
	assert.True(t, ok)
	assert.Equal(t, "text", text)

	integer, ok := Integer(math.MinInt64).Integer()
	assert.True(t, ok)
	assert.Equal(t, int64(math.MinInt64), integer)

	gotNumber, ok := number.Number()
	assert.True(t, ok)
	assert.Equal(t, 1.25, gotNumber)

	_, ok = Null().String()
	assert.False(t, ok)
}

func TestNumberRejectsNonFiniteValues(t *testing.T) {
	for _, input := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := Number(input)
		require.Error(t, err)
	}
}

func TestCompositeValuesCopyTheirInputsAndOutputs(t *testing.T) {
	items := []Value{String("first")}
	list, err := List(items)
	require.NoError(t, err)
	items[0] = String("changed")

	gotItems, ok := list.Items()
	require.True(t, ok)
	require.Equal(t, []Value{String("first")}, gotItems)
	gotItems[0] = String("changed again")
	gotItems, ok = list.Items()
	require.True(t, ok)
	require.Equal(t, []Value{String("first")}, gotItems)

	entries := map[string]Value{"name": String("first")}
	mapValue, err := Map(entries)
	require.NoError(t, err)
	entries["name"] = String("changed")

	gotEntries, ok := mapValue.MapEntries()
	require.True(t, ok)
	require.Equal(t, map[string]Value{"name": String("first")}, gotEntries)
	gotEntries["name"] = String("changed again")
	gotEntries, ok = mapValue.MapEntries()
	require.True(t, ok)
	require.Equal(t, map[string]Value{"name": String("first")}, gotEntries)

	fields := map[string]Value{"name": String("first")}
	objectValue, err := Object(fields)
	require.NoError(t, err)
	fields["name"] = String("changed")

	gotFields, ok := objectValue.ObjectFields()
	require.True(t, ok)
	require.Equal(t, map[string]Value{"name": String("first")}, gotFields)
	gotFields["name"] = String("changed again")
	gotFields, ok = objectValue.ObjectFields()
	require.True(t, ok)
	require.Equal(t, map[string]Value{"name": String("first")}, gotFields)
}

func TestEmptyCollectionsRemainConcrete(t *testing.T) {
	list, err := List(nil)
	require.NoError(t, err)
	items, ok := list.Items()
	require.True(t, ok)
	require.NotNil(t, items)
	require.Empty(t, items)

	mapValue, err := Map(nil)
	require.NoError(t, err)
	entries, ok := mapValue.MapEntries()
	require.True(t, ok)
	require.NotNil(t, entries)
	require.Empty(t, entries)

	objectValue, err := Object(nil)
	require.NoError(t, err)
	fields, ok := objectValue.ObjectFields()
	require.True(t, ok)
	require.NotNil(t, fields)
	require.Empty(t, fields)
}

func TestCompositeValuesRejectInvalidChildren(t *testing.T) {
	tests := []struct {
		name string
		make func() error
	}{
		{
			name: "list",
			make: func() error {
				_, err := List([]Value{{}})
				return err
			},
		},
		{
			name: "map",
			make: func() error {
				_, err := Map(map[string]Value{"bad": {}})
				return err
			},
		},
		{
			name: "object",
			make: func() error {
				_, err := Object(map[string]Value{"bad": {}})
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, tt.make())
		})
	}
}

func TestPendingSortsAndDeduplicatesReferences(t *testing.T) {
	refs := []string{"resource.z.id", "resource.a.id", "resource.z.id"}
	value, err := Pending(refs)
	require.NoError(t, err)
	refs[0] = "changed"

	got, ok := value.PendingRefs()
	require.True(t, ok)
	require.Equal(t, []string{"resource.a.id", "resource.z.id"}, got)
	got[0] = "changed again"
	got, ok = value.PendingRefs()
	require.True(t, ok)
	require.Equal(t, []string{"resource.a.id", "resource.z.id"}, got)
}

func TestPendingRejectsMissingReferences(t *testing.T) {
	for _, refs := range [][]string{nil, {}, {""}} {
		_, err := Pending(refs)
		require.Error(t, err)
	}
}

func TestMarshalCanonicalValues(t *testing.T) {
	list, err := List([]Value{Null(), Boolean(true)})
	require.NoError(t, err)
	mapValue, err := Map(map[string]Value{"z": list, "a": Integer(1)})
	require.NoError(t, err)
	objectValue, err := Object(map[string]Value{"text": String("x"), "map": mapValue})
	require.NoError(t, err)
	pending, err := Pending([]string{"resource.z.id", "resource.a.id"})
	require.NoError(t, err)
	negativeZero, err := Number(math.Copysign(0, -1))
	require.NoError(t, err)

	tests := []struct {
		name  string
		value Value
		want  string
	}{
		{name: "absent", value: Absent(), want: `{"kind":"absent"}`},
		{name: "null", value: Null(), want: `{"kind":"null"}`},
		{name: "boolean", value: Boolean(true), want: `{"kind":"boolean","value":true}`},
		{name: "string", value: String("text"), want: `{"kind":"string","value":"text"}`},
		{
			name:  "integer",
			value: Integer(math.MaxInt64),
			want:  `{"kind":"integer","value":"9223372036854775807"}`,
		},
		{name: "number", value: negativeZero, want: `{"kind":"number","value":"-0"}`},
		{
			name:  "list",
			value: list,
			want:  `{"kind":"list","items":[{"kind":"null"},{"kind":"boolean","value":true}]}`,
		},
		{
			name:  "map",
			value: mapValue,
			want: `{"kind":"map","entries":[` +
				`{"key":"a","value":{"kind":"integer","value":"1"}},` +
				`{"key":"z","value":{"kind":"list","items":[` +
				`{"kind":"null"},{"kind":"boolean","value":true}]}}]}`,
		},
		{
			name:  "object",
			value: objectValue,
			want: `{"kind":"object","fields":[` +
				`{"name":"map","value":{"kind":"map","entries":[` +
				`{"key":"a","value":{"kind":"integer","value":"1"}},` +
				`{"key":"z","value":{"kind":"list","items":[` +
				`{"kind":"null"},{"kind":"boolean","value":true}]}}]}},` +
				`{"name":"text","value":{"kind":"string","value":"x"}}]}`,
		},
		{
			name:  "pending",
			value: pending,
			want:  `{"kind":"pending","refs":["resource.a.id","resource.z.id"]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.value)
			require.NoError(t, err)
			require.Equal(t, tt.want, string(got))
		})
	}
}

func TestDecodeCanonicalValues(t *testing.T) {
	tests := []string{
		`{"kind":"absent"}`,
		`{"kind":"null"}`,
		`{"kind":"boolean","value":false}`,
		`{"kind":"string","value":"text"}`,
		`{"kind":"integer","value":"-9223372036854775808"}`,
		`{"kind":"number","value":"1.25"}`,
		`{"kind":"list","items":[{"kind":"null"}]}`,
		`{"kind":"map","entries":[{"key":"name","value":{"kind":"string","value":"x"}}]}`,
		`{"kind":"object","fields":[{"name":"field","value":{"kind":"absent"}}]}`,
		`{"kind":"pending","refs":["resource.example.id"]}`,
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			got, err := Decode([]byte(input))
			require.NoError(t, err)
			encoded, err := json.Marshal(got)
			require.NoError(t, err)
			require.Equal(t, input, string(encoded))
		})
	}
}

func TestDecodeAcceptsInsignificantWhitespaceAndMemberOrder(t *testing.T) {
	got, err := Decode([]byte(" { \n \"value\" : true, \"kind\" : \"boolean\" } \n"))
	require.NoError(t, err)
	require.Equal(t, Boolean(true), got)

	encoded, err := json.Marshal(got)
	require.NoError(t, err)
	require.Equal(t, `{"kind":"boolean","value":true}`, string(encoded))
}

func TestDecodeRejectsInvalidRootValues(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: "$"},
		{name: "non-object", in: `[]`, want: "$: expected object"},
		{name: "trailing value", in: `{"kind":"null"} {}`, want: "$"},
		{name: "missing kind", in: `{}`, want: `$.kind`},
		{name: "non-string kind", in: `{"kind":1}`, want: `$.kind`},
		{name: "unknown kind", in: `{"kind":"other"}`, want: `$.kind`},
		{
			name: "duplicate kind",
			in:   `{"kind":"null","kind":"null"}`,
			want: `$.kind: duplicate member`,
		},
		{
			name: "unknown member",
			in:   `{"kind":"null","other":true}`,
			want: `$.other: unknown member`,
		},
		{
			name: "member invalid for kind",
			in:   `{"kind":"null","value":null}`,
			want: `$.value: unknown member`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode([]byte(tt.in))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestDecodeRejectsInvalidScalars(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "missing boolean",
			in:   `{"kind":"boolean"}`,
			want: `$.value`,
		},
		{
			name: "wrong boolean type",
			in:   `{"kind":"boolean","value":"true"}`,
			want: `$.value`,
		},
		{
			name: "wrong string type",
			in:   `{"kind":"string","value":1}`,
			want: `$.value`,
		},
		{
			name: "integer leading zero",
			in:   `{"kind":"integer","value":"01"}`,
			want: `$.value: noncanonical integer`,
		},
		{
			name: "integer negative zero",
			in:   `{"kind":"integer","value":"-0"}`,
			want: `$.value: noncanonical integer`,
		},
		{
			name: "integer overflow",
			in:   `{"kind":"integer","value":"9223372036854775808"}`,
			want: `$.value: invalid integer`,
		},
		{
			name: "number decimal zero",
			in:   `{"kind":"number","value":"1.0"}`,
			want: `$.value: noncanonical number`,
		},
		{
			name: "number not finite",
			in:   `{"kind":"number","value":"NaN"}`,
			want: `$.value: number must be finite`,
		},
		{
			name: "number invalid",
			in:   `{"kind":"number","value":"nope"}`,
			want: `$.value: invalid number`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode([]byte(tt.in))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestDecodeRejectsInvalidCollections(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "items not array",
			in:   `{"kind":"list","items":{}}`,
			want: `$.items: expected array`,
		},
		{
			name: "nested invalid value",
			in:   `{"kind":"list","items":[{"kind":"integer","value":"01"}]}`,
			want: `$.items[0].value: noncanonical integer`,
		},
		{
			name: "map entries unsorted",
			in: `{"kind":"map","entries":[` +
				`{"key":"z","value":{"kind":"null"}},` +
				`{"key":"a","value":{"kind":"null"}}]}`,
			want: `$.entries[1].key: entries are not sorted`,
		},
		{
			name: "map duplicate key",
			in: `{"kind":"map","entries":[` +
				`{"key":"a","value":{"kind":"null"}},` +
				`{"key":"a","value":{"kind":"null"}}]}`,
			want: `$.entries[1].key: duplicate key`,
		},
		{
			name: "map entry missing value",
			in:   `{"kind":"map","entries":[{"key":"a"}]}`,
			want: `$.entries[0].value`,
		},
		{
			name: "map entry unknown member",
			in:   `{"kind":"map","entries":[{"key":"a","value":{"kind":"null"},"x":1}]}`,
			want: `$.entries[0].x: unknown member`,
		},
		{
			name: "object fields unsorted",
			in: `{"kind":"object","fields":[` +
				`{"name":"z","value":{"kind":"null"}},` +
				`{"name":"a","value":{"kind":"null"}}]}`,
			want: `$.fields[1].name: fields are not sorted`,
		},
		{
			name: "object duplicate name",
			in: `{"kind":"object","fields":[` +
				`{"name":"a","value":{"kind":"null"}},` +
				`{"name":"a","value":{"kind":"null"}}]}`,
			want: `$.fields[1].name: duplicate name`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode([]byte(tt.in))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestDecodeRejectsInvalidPendingReferences(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "missing refs",
			in:   `{"kind":"pending"}`,
			want: `$.refs`,
		},
		{
			name: "empty refs",
			in:   `{"kind":"pending","refs":[]}`,
			want: `$.refs: at least one reference is required`,
		},
		{
			name: "empty ref",
			in:   `{"kind":"pending","refs":[""]}`,
			want: `$.refs[0]: reference is empty`,
		},
		{
			name: "unsorted refs",
			in:   `{"kind":"pending","refs":["z","a"]}`,
			want: `$.refs[1]: references are not sorted`,
		},
		{
			name: "duplicate ref",
			in:   `{"kind":"pending","refs":["a","a"]}`,
			want: `$.refs[1]: duplicate reference`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode([]byte(tt.in))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestDecodeRejectsDuplicateNestedMembers(t *testing.T) {
	input := `{"kind":"list","items":[{"kind":"string","value":"a","value":"b"}]}`
	_, err := Decode([]byte(input))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `$.items[0].value: duplicate member`)
}

func TestUnmarshalDoesNotChangeValueAfterError(t *testing.T) {
	got := String("before")
	err := json.Unmarshal([]byte(`{"kind":"integer","value":"01"}`), &got)
	require.Error(t, err)
	require.Equal(t, String("before"), got)
}

func TestZeroValueCannotBeMarshaled(t *testing.T) {
	_, err := json.Marshal(Value{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kind is required")
}

func TestHasPendingFindsNestedValues(t *testing.T) {
	pending, err := Pending([]string{"resource.example.id"})
	require.NoError(t, err)
	list, err := List([]Value{Null(), pending})
	require.NoError(t, err)
	mapValue, err := Map(map[string]Value{"list": list})
	require.NoError(t, err)
	objectValue, err := Object(map[string]Value{"map": mapValue})
	require.NoError(t, err)

	assert.True(t, pending.HasPending())
	assert.True(t, list.HasPending())
	assert.True(t, mapValue.HasPending())
	assert.True(t, objectValue.HasPending())
	assert.False(t, Null().HasPending())
}
