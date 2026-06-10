package parse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func interpolatedString(t *testing.T, src string) *InterpolatedString {
	t.Helper()
	f := mustParse(t, "x: "+src)
	is, ok := f.Body.Fields[0].Value.(*InterpolatedString)
	require.True(t, ok, "value is %T, want *InterpolatedString", f.Body.Fields[0].Value)
	return is
}

func TestInterpolatedParts(t *testing.T) {
	type want struct {
		lit  string // literal text when this part is not a slot
		slot bool   // true when this part is a {{ }} slot
		verb string // expected printf verb for a slot
	}
	tests := []struct {
		name  string
		src   string
		parts []want
	}{
		{"literal only", `$'hello world'`, []want{{lit: "hello world"}}},
		{"empty", `$''`, nil},
		{"slot only", `$'{{var.x}}'`, []want{{slot: true}}},
		{"lit slot lit", `$'a{{var.x}}b'`, []want{{lit: "a"}, {slot: true}, {lit: "b"}}},
		{"adjacent slots", `$'{{var.x}}{{var.y}}'`, []want{{slot: true}, {slot: true}}},
		{"slot with verb", `$'{{var.size:%03d}}'`, []want{{slot: true, verb: "%03d"}}},
		{"verb with flags and spaces", `$'{{ var.n : %-10s }}'`, []want{{slot: true, verb: "%-10s"}}},
		{"single brace is literal", `$'a{b}c'`, []want{{lit: "a{b}c"}}},
		{"escaped open brace", `$'\{{x}}'`, []want{{lit: "{{x}}"}}},
		{"escaped quote in literal", `$'it\'s {{var.x}}'`, []want{{lit: "it's "}, {slot: true}}},
		{"conditional slot", `$'{{if var.p then var.a else var.b}}'`, []want{{slot: true}}},
		{"call slot", `$'{{format('%s', var.x)}}'`, []want{{slot: true}}},
		{"braces inside slot string", `$'{{ 'a}}b' }}'`, []want{{slot: true}}},
		{"leading slot trailing literal", `$'{{var.x}} done'`, []want{{slot: true}, {lit: " done"}}},
		{"newline escape then slot", `$'line\n{{var.x}}'`, []want{{lit: "line\n"}, {slot: true}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := interpolatedString(t, tt.src)
			require.Len(t, is.Parts, len(tt.parts))
			for i, w := range tt.parts {
				got := is.Parts[i]
				if w.slot {
					require.NotNil(t, got.Expr, "part %d should be a slot", i)
					assert.Equal(t, w.verb, got.Verb, "part %d verb", i)
				} else {
					assert.Nil(t, got.Expr, "part %d should be a literal", i)
					assert.Equal(t, w.lit, got.Lit, "part %d literal", i)
				}
			}
		})
	}
}

func TestInterpolatedSlotExprTypes(t *testing.T) {
	t.Run("dot path", func(t *testing.T) {
		is := interpolatedString(t, `$'{{var.region}}'`)
		dp, ok := is.Parts[0].Expr.(*DotPath)
		require.True(t, ok, "want *DotPath, got %T", is.Parts[0].Expr)
		assert.Equal(t, "var", dp.Root.Name)
		require.Len(t, dp.Segments, 1)
		assert.Equal(t, "region", dp.Segments[0].Name)
	})

	t.Run("conditional", func(t *testing.T) {
		is := interpolatedString(t, `$'{{if var.p then var.a else var.b}}'`)
		_, ok := is.Parts[0].Expr.(*Conditional)
		assert.True(t, ok, "want *Conditional, got %T", is.Parts[0].Expr)
	})

	t.Run("call", func(t *testing.T) {
		is := interpolatedString(t, `$'{{format('%s', var.x)}}'`)
		_, ok := is.Parts[0].Expr.(*Call)
		assert.True(t, ok, "want *Call, got %T", is.Parts[0].Expr)
	})
}

// TestSlotFormParity locks the two interpolation forms to one slot
// behavior: the same slot text in the single- and triple-quoted forms
// either parses in both or fails in both with the same message.
func TestSlotFormParity(t *testing.T) {
	tests := []struct {
		name    string
		slot    string
		wantErr string // "" means both forms parse
	}{
		{"plain", "{{ var.x }}", ""},
		{"verb", "{{ var.n : %03d }}", ""},
		{"colon inside index string", "{{ var.m['a:b'] }}", ""},
		{"closer inside string", "{{ 'a}}b' }}", ""},
		{"nested braces", "{{ {a: {b: 1}} }}", ""},
		{"conditional", "{{ if var.p then var.a else var.b }}", ""},
		{"empty", "{{}}", "empty interpolation slot"},
		{"empty spaces", "{{   }}", "empty interpolation slot"},
		{"bad verb", "{{ var.x : bad }}",
			"interpolation directive must be a printf verb like %03d"},
		{"quoted closer in directive", "{{ var.x : '}}' }}",
			"interpolation directive must be a printf verb like %03d"},
		{"newline in slot", "{{ 1 +\n2 }}", "interpolation slot must be on one line"},
		{"newline only slot", "{{\n}}", "interpolation slot must be on one line"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			forms := []struct{ label, src string }{
				{"single", "x: $'pre " + tt.slot + " post'\n"},
				{"triple", "x: $'''|\n  pre " + tt.slot + " post\n  '''\n"},
			}
			for _, form := range forms {
				_, err := ParseSource("test.ub", []byte(form.src))
				if tt.wantErr == "" {
					assert.NoError(t, err, form.label)
					continue
				}
				if assert.Error(t, err, form.label) {
					assert.Contains(t, err.Error(), tt.wantErr, form.label)
				}
			}
		})
	}
}

func TestInterpolatedInvalid(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"unterminated slot", `x: $'{{var.x'`},
		{"empty slot", `x: $'{{}}'`},
		{"verb without percent", `x: $'{{var.x:bad}}'`},
		{"escaped close brace", `x: $'bad\}}'`},
		{"unknown escape in literal", `x: $'oops\q'`},
		{"single escaped brace", `x: $'lit\{here'`},
		{"unterminated string", `x: $'no end`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSource("test.ub", []byte(tt.src))
			require.Error(t, err)
		})
	}
}
