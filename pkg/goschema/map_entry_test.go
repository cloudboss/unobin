package goschema

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func TestMapEntryConstraints(t *testing.T) {
	schema, warnings, err := Read("testdata/map-entry")
	require.NoError(t, err)
	require.Empty(t, warnings)
	specs := schema.Resources["thing"].Constraints
	require.Equal(t, []lang.ConstraintSpec{
		{
			Kind: "predicate", ForEach: "input.items ?? {}", When: "(@each.value != null)",
			Require: "(@each.key == @each.value.name)", Message: "entry name",
		},
		{
			Kind: "predicate", When: "true", Require: "(@value.value == @entry.key)",
			Message: "entry values",
			ForEachLevels: []lang.ForEachSpecLevel{
				{Name: "@entry", In: "input.items ?? {}"},
				{Name: "@value", In: "@entry.value.values ?? []"},
			},
		},
		{
			Kind: "predicate", ForEach: "input.lists", When: "(@each.value[0].name != null)",
			Require: "(@each.key == @each.value[0].name)", Message: "indexed entry",
		},
		{
			Kind: "predicate", When: "(@entry.value != null)",
			Require: "(@entry.key == @entry.value.name)", Message: "row entry",
			ForEachLevels: []lang.ForEachSpecLevel{
				{Name: "@row", In: "input.rows"},
				{Name: "@entry", In: "@row.value"},
			},
		},
	}, specs)
	entries, errs := lang.ParseSpecs(specs)
	require.Empty(t, errs.Messages())
	for _, test := range []struct {
		name string
		item any
		want []string
	}{
		{name: "valid", item: map[string]any{"name": "name", "values": []any{"name"}}},
		{name: "null value"},
		{name: "empty value", item: map[string]any{}, want: []string{"entry name", "row entry"}},
		{
			name: "invalid", item: map[string]any{"name": "wrong", "values": []any{"wrong"}},
			want: []string{"entry name", "entry values", "indexed entry", "row entry"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := map[string]any{
				"items": map[string]any{"name": test.item},
				"lists": map[string]any{"name": []any{test.item}},
				"rows":  []any{map[string]any{"name": test.item}},
			}
			eval := func(expr lang.Expr, binds []lang.EachBinding) (any, error) {
				ctx := &runtime.EvalContext{Inputs: values, MissingAsNull: true}
				runtime.ApplyBindings(ctx, binds)
				return runtime.Eval(expr, ctx)
			}
			errs := lang.CheckConstraintEntries(entries, values, eval, lang.DisplayNodeRelative)
			require.Len(t, errs.Messages(), len(test.want))
			for i, want := range test.want {
				require.Contains(t, errs.Messages()[i], want)
			}
		})
	}
}
