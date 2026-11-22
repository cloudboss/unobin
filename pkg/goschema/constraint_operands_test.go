package goschema

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func TestComputedConstraintParity(t *testing.T) {
	schema, warnings, err := Read("testdata/computed-constraints")
	require.NoError(t, err)
	require.Empty(t, warnings)
	specs := schema.Resources["thing"].Constraints
	entries, errs := lang.ParseSpecs(specs)
	require.Empty(t, errs.Strings())
	require.Equal(t, []string{"listen-ports"}, lang.ConstraintFieldRoots(entries[0]))

	src, err := os.ReadFile("testdata/ub/computed-constraints/valid/rules.ub")
	require.NoError(t, err)
	f, err := lang.ParseSource("rules.ub", src)
	require.NoError(t, err)
	block := f.Body.Fields[0].Value.(*lang.ArrayLit)
	require.Len(t, entries, len(block.Elements))
	for _, test := range []struct {
		name   string
		values map[string]any
		want   []string
	}{
		{
			name: "valid",
			values: map[string]any{
				"listen-ports": []any{int64(443)}, "period": int64(60),
				"volumes": []any{map[string]any{"name": "é"}}, "mount": "é",
				"labels": map[string]any{"a": "yes", "ignored": "yes"}, "selector": "a",
				"text": "é", "size": int64(1), "user": "-9223372036854775808", "limit": int64(2),
			},
		},
		{
			name:   "omitted",
			values: map[string]any{"text": "", "size": int64(0), "limit": int64(0)},
		},
		{
			name: "invalid",
			values: map[string]any{
				"listen-ports": []any{int64(0)}, "period": int64(61),
				"volumes": []any{map[string]any{"name": "é"}, map[string]any{"name": "é"}},
				"mount":   "missing",
				"labels":  map[string]any{"a": "yes", "b": "yes"}, "selector": "missing",
				"text": "é", "size": int64(2), "user": "9223372036854775808", "limit": int64(3),
			},
			want: []string{
				"constraints[0] (predicate): invalid port",
				"constraints[1] (predicate): invalid period",
				"constraints[2] (predicate): duplicate volume",
				"constraints[3] (predicate): unknown volume",
				"constraints[4] (predicate): label mismatch",
				"constraints[5] (predicate): too many matching labels",
				"constraints[6] (predicate): volume byte limit",
				"constraints[7] (predicate): text size mismatch",
				"constraints[8] (predicate): invalid user",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			eval := func(expr lang.Expr, binds []lang.EachBinding) (any, error) {
				ctx := &runtime.EvalContext{Inputs: test.values, MissingAsNull: true}
				runtime.ApplyBindings(ctx, binds)
				return runtime.Eval(expr, ctx)
			}
			native := lang.CheckConstraintEntries(
				entries, test.values, eval, lang.DisplayNodeRelative,
			)
			ub := lang.CheckConstraints(block, test.values, eval, lang.DisplayNodeRelative)
			require.Equal(t, ub.Strings(), native.Strings())
			var messages []string
			for _, diagnostic := range native.Errors() {
				messages = append(messages, diagnostic.Msg)
			}
			require.Equal(t, test.want, messages)
		})
	}
}

func TestInvalidConstraintExpressions(t *testing.T) {
	_, warnings, err := Read("testdata/invalid-expressions")
	require.Error(t, err)
	require.Empty(t, warnings)
	for _, message := range []string{
		"expected boolean",
		"use Bind",
		"only @core calls are available",
		"binding names must be nonempty and distinct constant strings",
	} {
		require.ErrorContains(t, err, message)
	}
}
