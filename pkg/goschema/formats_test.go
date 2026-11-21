package goschema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/check"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func TestReadNestedAliasConstraint(t *testing.T) {
	schema, warnings, err := Read("testdata/format-constraints/nested-alias")
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, "(input.aliased.metadata.name != null)",
		schema.Resources["thing"].Constraints[0].Require)
}

func TestReadImportedAliasConstraint(t *testing.T) {
	schema, warnings, err := Read("testdata/format-constraints/imported-alias")
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, "(input.object.metadata.name != null)",
		schema.Resources["thing"].Constraints[0].Require)
}

func TestReadRootAliasConstraints(t *testing.T) {
	for _, test := range []struct {
		name string
		want string
	}{
		{"local-alias", "(input.name != null)"},
		{"alias-chain", "(input.name != null)"},
		{"defined-type", ""},
		{"direct-alias-method", "(input.domain == 'alias')"},
	} {
		t.Run(test.name, func(t *testing.T) {
			schema, warnings, err := Read(filepath.Join("testdata/format-constraints", test.name))
			require.NoError(t, err)
			require.Empty(t, warnings)
			specs := schema.Resources["thing"].Constraints
			if test.want == "" {
				require.Empty(t, specs)
				return
			}
			require.Len(t, specs, 1)
			require.Equal(t, test.want, specs[0].Require)
		})
	}
}

func TestReadImportedRootAliasConstraints(t *testing.T) {
	schema, warnings, err := Read("testdata/format-constraints/imported-root-alias")
	require.NoError(t, err)
	require.Empty(t, warnings)
	specs := schema.Resources["thing"].Constraints
	require.Len(t, specs, 1)
	require.Equal(t, "(input.name != null)", specs[0].Require)
}

func TestReadIntermediateAliasConstraints(t *testing.T) {
	schema, warnings, err := Read("testdata/format-constraints/intermediate-alias")
	require.NoError(t, err)
	require.Empty(t, warnings)
	specs := schema.Resources["thing"].Constraints
	require.Len(t, specs, 1)
	require.Equal(t, "(input.name != null)", specs[0].Require)
}

func TestReadImportedIntermediateAliasConstraints(t *testing.T) {
	schema, warnings, err := Read("testdata/format-constraints/imported-intermediate-alias")
	require.NoError(t, err)
	require.Empty(t, warnings)
	specs := schema.Resources["thing"].Constraints
	require.Len(t, specs, 1)
	require.Equal(t, "(input.name != null)", specs[0].Require)
}

func TestReadRejectsInvalidFormatDeclarations(t *testing.T) {
	for _, test := range []struct {
		name string
		want string
	}{
		{"non-string", "requires a string field"},
		{"missing-field", "requires 1 arguments"},
		{"extra-field", "requires 1 arguments"},
		{"invalid-pattern", "regular expression"},
		{"pattern-field", "constant string expression"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, warnings, err := Read(filepath.Join("testdata/format-constraints/invalid-format", test.name))
			require.ErrorContains(t, err, test.want)
			require.Empty(t, warnings)
		})
	}
}

func TestReadNativeFormatPredicates(t *testing.T) {
	schema, warnings, err := Read("testdata/native-predicates")
	require.NoError(t, err)
	require.Empty(t, warnings)
	specs := schema.Resources["thing"].Constraints
	require.Len(t, specs, 12)
	require.Equal(t, "(@core.matches(input.object.metadata.name, '^[a-z][a-z0-9.-]*$'))",
		specs[0].Require)
	entries, errs := lang.ParseSpecs(specs)
	require.Empty(t, errs.Errors())
	roots := map[string][]string{
		"label key":    {"labels"},
		"label value":  {"labels"},
		"group key":    {"expected", "groups"},
		"group value":  {"groups"},
		"list IP":      {"lists"},
		"nested value": {"nested"},
	}
	for _, entry := range entries {
		if want, ok := roots[entry.Message]; ok {
			require.ElementsMatch(t, want, lang.ConstraintFieldRoots(entry), entry.Message)
			for _, root := range want {
				require.True(t, entry.ReadsAny(map[string]bool{root: true}), entry.Message)
			}
			require.False(t, entry.ReadsAny(map[string]bool{"unrelated": true}), entry.Message)
		}
	}
	for _, name := range []string{"valid", "null", "invalid"} {
		t.Run(name, func(t *testing.T) {
			category := "valid"
			if name == "invalid" {
				category = "invalid"
			}
			path := filepath.Join("testdata/ub/native-predicates", category, name+".ub")
			src, err := os.ReadFile(path)
			require.NoError(t, err)
			file, err := lang.ParseSource(name+".ub", src)
			require.NoError(t, err)
			require.Len(t, file.Body.Fields, 1)
			value, err := runtime.Eval(file.Body.Fields[0].Value, &runtime.EvalContext{})
			require.NoError(t, err)
			values := value.(map[string]any)
			eval := func(expr lang.Expr, binds []lang.EachBinding) (any, error) {
				ctx := &runtime.EvalContext{Inputs: values, MissingAsNull: true}
				runtime.ApplyBindings(ctx, binds)
				return runtime.Eval(expr, ctx)
			}
			errs := lang.CheckConstraintEntries(entries, values, eval, lang.DisplayNodeRelative)
			if name != "invalid" {
				require.Empty(t, errs.Messages())
				return
			}
			require.Len(t, errs.Messages(), 12)
			for _, message := range []string{
				"name format", "regex format", "IP format", "CIDR format", "base64 format",
				"duration format", "label key (labels['z-invalid!'])",
				"label value (labels['z-invalid!'])", "group key", "group value",
				"list IP (lists['second'][1])",
				"nested value (nested['outer']['middle']['second'])",
			} {
				require.Contains(t, strings.Join(errs.Messages(), "\n"), message)
			}
		})
	}
}

func TestReadFormatPatternEscapes(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
	}{
		{"double-quote", `"`},
		{"apostrophe", "it's 123"},
		{"backslash", `\`},
		{"control-characters", "\x00\a\b\f\v\n\r\t\u0085\u2028"},
		{"unicode", "hélloλ"},
	} {
		t.Run(test.name, func(t *testing.T) {
			schema, warnings, err := Read(filepath.Join("testdata/format-constraints/pattern", test.name))
			require.NoError(t, err)
			require.Empty(t, warnings)
			entries, errs := lang.ParseSpecs(schema.Resources["thing"].Constraints)
			require.Empty(t, errs.Messages())
			for _, input := range []string{test.value, "invalid"} {
				value, err := runtime.Eval(entries[0].Require, &runtime.EvalContext{
					Inputs: map[string]any{"domain": input}, MissingAsNull: true,
				})
				require.NoError(t, err)
				require.Equal(t, input == test.value, value)
			}
		})
	}
}

func TestReadConcatenatedFormatPattern(t *testing.T) {
	schema, warnings, err := Read("testdata/format-constraints/pattern/concatenated")
	require.NoError(t, err)
	require.Empty(t, warnings)
	entries, errs := lang.ParseSpecs(schema.Resources["thing"].Constraints)
	require.Empty(t, errs.Messages())
	value, err := runtime.Eval(entries[0].Require, &runtime.EvalContext{
		Inputs: map[string]any{"domain": "it's 123"}, MissingAsNull: true,
	})
	require.NoError(t, err)
	require.Equal(t, true, value)
}

func TestReadRejectsInvalidMapDeclarations(t *testing.T) {
	for _, test := range []struct {
		name string
		want string
	}{
		{"non-map", "must be a list field or a map with string keys"},
		{"wrong-value-type", "does not match element type"},
		{"shadowed-receiver", "shadows an enclosing name"},
		{"set-rule", "a set constraint cannot iterate map"},
		{"missing-entry", "map parameter must be constraint.MapEntry[T]"},
		{"pointer-entry", "map parameter must be constraint.MapEntry[T]"},
		{"lookalike-entry", "map parameter must be constraint.MapEntry[T]"},
		{"entry-on-list", "does not match element type"},
		{"unknown-selector", "unknown field"},
		{"nested-shadow", "shadows an enclosing name"},
		{"nested-set-rule", "a set constraint cannot iterate map"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, warnings, err := Read(filepath.Join("testdata/format-constraints/invalid-map", test.name))
			require.ErrorContains(t, err, test.want)
			require.Empty(t, warnings)
		})
	}
}

func TestNativePredicatesLiteralChecks(t *testing.T) {
	schema, warnings, err := Read("testdata/native-predicates")
	require.NoError(t, err)
	require.Empty(t, warnings)
	for _, name := range []string{"valid-factory", "invalid-factory", "deferred-factory"} {
		t.Run(name, func(t *testing.T) {
			category := "valid"
			if name == "invalid-factory" {
				category = "invalid"
			}
			path := filepath.Join("testdata/ub/native-predicates", category, name+".ub")
			src, err := os.ReadFile(path)
			require.NoError(t, err)
			file, err := syntax.ParseSource("factory.ub", src)
			require.NoError(t, err)
			checker := check.NewSyntax(file.Factory.Body,
				map[string]*runtime.Library{"lib": {Schema: schema}}, nil, "")
			errs := checker.LiteralConstraints()
			if name == "invalid-factory" {
				require.Len(t, errs.Messages(), 2)
				require.Contains(t, strings.Join(errs.Messages(), "\n"), "name format")
				require.Contains(t, strings.Join(errs.Messages(), "\n"), "label key")
			} else {
				require.Empty(t, errs.Messages())
			}
		})
	}
}
