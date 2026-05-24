package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEvalInterpolated(t *testing.T) {
	ctx := &EvalContext{Vars: map[string]any{
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
	}}
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"literal only", `$'hello world'`, "hello world"},
		{"empty", `$''`, ""},
		{"single slot", `$'{{var.region}}'`, "us-east-1"},
		{"lit slot lit", `$'cluster-{{var.name}}-prod'`, "cluster-web-prod"},
		{"two slots", `$'{{var.a}}/{{var.b}}'`, "x/y"},
		{"nested dot path", `$'{{var.net.id}}'`, "vpc-123"},
		{"default int", `$'n={{var.n}}'`, "n=5"},
		{"default float", `$'{{var.f}}'`, "3.14159"},
		{"default bool true", `$'{{var.prod}}'`, "true"},
		{"default bool false", `$'{{var.flag}}'`, "false"},
		{"verb int zero pad", `$'{{var.n:%03d}}'`, "005"},
		{"verb float precision", `$'{{var.f:%.2f}}'`, "3.14"},
		{"verb string width", `$'{{var.s:%-5s}}|'`, "ab   |"},
		{"conditional slot", `$'{{if var.prod then 'big' else 'small'}}'`, "big"},
		{"call slot", `$'{{format('%s!', var.a)}}'`, "x!"},
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

func TestEvalInterpolatedRejectsNonScalar(t *testing.T) {
	ctx := &EvalContext{Vars: map[string]any{
		"nothing": nil,
		"list":    []any{"a", "b"},
		"obj":     map[string]any{"k": "v"},
	}}
	tests := []struct {
		name string
		src  string
	}{
		{"null slot", `$'{{var.nothing}}'`},
		{"list slot", `$'x-{{var.list}}'`},
		{"map slot", `$'{{var.obj}}'`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Eval(parseValue(t, tt.src), ctx)
			require.Error(t, err)
		})
	}
}
