package typecheck

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromLangAtomics(t *testing.T) {
	tests := []struct {
		name string
		in   lang.TypeExpr
		want Type
	}{
		{"string", &lang.TypeAtomic{Name: "string"}, TString()},
		{"integer", &lang.TypeAtomic{Name: "integer"}, TInteger()},
		{"number", &lang.TypeAtomic{Name: "number"}, TNumber()},
		{"boolean", &lang.TypeAtomic{Name: "boolean"}, TBoolean()},
		{"null", &lang.TypeAtomic{Name: "null"}, TNull()},
		{"opaque", &lang.TypeAtomic{Name: "opaque"}, TOpaque()},
		{"unrecognized", &lang.TypeAtomic{Name: "bogus"}, TUnknown()},
		{"nil", nil, TUnknown()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FromLang(tt.in)
			assert.True(t, got.Equal(tt.want), "got %s want %s", got, tt.want)
		})
	}
}

func TestInputsFromBlockNestedDefaults(t *testing.T) {
	f, err := lang.ParseSource("x.ub", []byte(`
inputs: {
  spec: {
    type: object({
      port:    { type: optional(integer, 8080) },
      retries: optional(integer, 3),
      note:    { type: optional(string) },
    })
  }
}
`))
	require.NoError(t, err)
	inputs := InputsFromBlock(f.Body.Fields[0].Value.(*lang.ObjectLit))
	scope := &Scope{Inputs: inputs}
	errs := lang.NewErrorList(0)

	got := Infer(parseExpr(t, "var.spec.port"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TInteger()), "port reads defaulted, got %s", got)
	got = Infer(parseExpr(t, "var.spec.retries"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TInteger()), "retries reads defaulted, got %s", got)
	got = Infer(parseExpr(t, "var.spec.note"), TUnknown(), scope, errs)
	assert.True(t, got.Equal(TOptional(TString())), "note stays optional, got %s", got)
	assert.Empty(t, errs.Errors())
}

func TestFromLangContainers(t *testing.T) {
	str := &lang.TypeAtomic{Name: "string"}
	intT := &lang.TypeAtomic{Name: "integer"}

	assert.True(t, FromLang(&lang.TypeList{Elem: str}).Equal(TList(TString())))
	assert.True(t, FromLang(&lang.TypeList{Elem: intT}).Equal(TList(TInteger())))
	assert.True(t, FromLang(&lang.TypeMap{Elem: str}).Equal(TMap(TString())))
	assert.True(
		t,
		FromLang(&lang.TypeOptional{Elem: str}).Equal(TOptional(TString())),
	)
	assert.True(
		t,
		FromLang(&lang.TypeTuple{Elements: []lang.TypeExpr{str, intT}}).Equal(
			TTuple([]Type{TString(), TInteger()}),
		),
	)
}

func TestFromLangObjectBareFields(t *testing.T) {
	in := &lang.TypeObject{Fields: []*lang.TypeObjectField{
		{Name: "id", Type: &lang.TypeAtomic{Name: "string"}},
		{Name: "tags", Type: &lang.TypeMap{Elem: &lang.TypeAtomic{Name: "string"}}},
	}}
	got := FromLang(in)
	want := TObject([]ObjectField{
		{Name: "id", Type: TString()},
		{Name: "tags", Type: TMap(TString())},
	})
	assert.True(t, got.Equal(want), "got %s want %s", got, want)
}

func TestInputsFromBlockHandlesOptional(t *testing.T) {
	src := `
inputs: {
  region: { type: string }
  count: { type: optional(integer, 1) }
  label: { type: optional(string) }
}
`
	f, err := lang.ParseSource("main.ub", []byte(src))
	require.NoError(t, err)

	inputs := topLevelInputs(t, f)
	got := InputsFromBlock(inputs)
	require.Len(t, got, 3)

	assert.Equal(t, "region", got[0].Name)
	assert.True(t, got[0].Type.Equal(TString()))
	assert.False(t, got[0].Optional)
	assert.False(t, got[0].Defaulted)

	assert.Equal(t, "count", got[1].Name)
	assert.True(t, got[1].Type.Equal(TInteger()))
	assert.True(t, got[1].Optional)
	assert.True(t, got[1].Defaulted)

	assert.Equal(t, "label", got[2].Name)
	assert.True(t, got[2].Type.Equal(TString()))
	assert.True(t, got[2].Optional)
	assert.False(t, got[2].Defaulted)
}

func topLevelInputs(t *testing.T, f *lang.File) *lang.ObjectLit {
	t.Helper()
	require.NotNil(t, f.Body)
	for _, fld := range f.Body.Fields {
		if fld.Key.Kind == lang.FieldIdent && fld.Key.Name == "inputs" {
			result, ok := fld.Value.(*lang.ObjectLit)
			require.True(t, ok)
			return result
		}
	}
	t.Fatalf("inputs block not found")
	return nil
}
