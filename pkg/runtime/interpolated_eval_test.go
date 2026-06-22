package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEvalInterpolated(t *testing.T) {
	ctx := &EvalContext{
		Inputs: map[string]any{
			"region": "us-east-1",
			"name":   "web",
			"a":      "x",
			"b":      "y",
			"n":      int64(5),
			"f":      3.14159,
			"s":      "ab",
			"prod":   true,
			"flag":   false,
			"net":    map[string]any{"id": "vpc-123"},
		},
		Libraries: map[string]*Library{"lib": {
			Name: "lib",
			Functions: map[string]FunctionType{
				"bang": {Name: "bang", Func: func(args []any) (any, error) {
					return args[0].(string) + "!", nil
				}},
			},
		}},
	}
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"literal only", `$'hello world'`, "hello world"},
		{"empty", `$''`, ""},
		{"single slot", `$'{{input.region}}'`, "us-east-1"},
		{"lit slot lit", `$'cluster-{{input.name}}-prod'`, "cluster-web-prod"},
		{"two slots", `$'{{input.a}}/{{input.b}}'`, "x/y"},
		{"nested dot path", `$'{{input.net.id}}'`, "vpc-123"},
		{"default int", `$'n={{input.n}}'`, "n=5"},
		{"default float", `$'{{input.f}}'`, "3.14159"},
		{"default bool true", `$'{{input.prod}}'`, "true"},
		{"default bool false", `$'{{input.flag}}'`, "false"},
		{"verb int zero pad", `$'{{input.n:%03d}}'`, "005"},
		{"verb float precision", `$'{{input.f:%.2f}}'`, "3.14"},
		{"verb string width", `$'{{input.s:%-5s}}|'`, "ab   |"},
		{"conditional slot", `$'{{if input.prod then 'big' else 'small'}}'`, "big"},
		{"call slot", `$'{{lib.bang(input.a)}}'`, "x!"},
		{"escaped open brace", `$'\{{x}}'`, "{{x}}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Eval(parseValue(t, tt.src), ctx)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestEvalInterpolatedTriple(t *testing.T) {
	ctx := &EvalContext{Inputs: map[string]any{"name": "web", "region": "us-east-1"}}
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			"single line",
			`$'''Hello {{ input.name }}!'''`,
			"Hello web!",
		},
		{
			"folded strip joins lines with a space",
			"$'''>-\n  Hello {{ input.name }},\n  region {{ input.region }}\n  '''",
			"Hello web, region us-east-1",
		},
		{
			"literal strip keeps the newline",
			"$'''|-\n  host {{ input.name }}\n  zone {{ input.region }}\n  '''",
			"host web\nzone us-east-1",
		},
		{
			"joined strip drops the line break",
			"$'''\\-\n  {{ input.name }}\n  -{{ input.region }}\n  '''",
			"web-us-east-1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Eval(parseValue(t, tt.src), ctx)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestEvalInterpolatedRejectsNonScalar(t *testing.T) {
	ctx := &EvalContext{Inputs: map[string]any{
		"nothing": nil,
		"list":    []any{"a", "b"},
		"obj":     map[string]any{"k": "v"},
	}}
	tests := []struct {
		name string
		src  string
	}{
		{"null slot", `$'{{input.nothing}}'`},
		{"list slot", `$'x-{{input.list}}'`},
		{"map slot", `$'{{input.obj}}'`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Eval(parseValue(t, tt.src), ctx)
			require.Error(t, err)
		})
	}
}
