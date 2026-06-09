package lang

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// litEval reduces literal expressions used as defaults in the tests.
// It is intentionally tiny: only the shapes the tests pass as defaults
// need to evaluate. The real flow passes runtime.Eval.
func litEval(e Expr) (any, error) {
	switch v := e.(type) {
	case *StringLit:
		return v.Value, nil
	case *NumberLit:
		if v.IsFloat {
			return v.ParsedFloat, nil
		}
		return v.ParsedInt, nil
	case *BoolLit:
		return v.Value, nil
	case *NullLit:
		return nil, nil
	case *ArrayLit:
		out := make([]any, len(v.Elements))
		for i, el := range v.Elements {
			val, err := litEval(el)
			if err != nil {
				return nil, err
			}
			out[i] = val
		}
		return out, nil
	case *ObjectLit:
		out := make(map[string]any, len(v.Fields))
		for _, fld := range v.Fields {
			key := fld.Key.Name
			if fld.Key.Kind == FieldString {
				key = fld.Key.String
			}
			val, err := litEval(fld.Value)
			if err != nil {
				return nil, err
			}
			out[key] = val
		}
		return out, nil
	}
	return nil, nil
}

func TestValidateInputsAtomic(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  region: { type: string }
  size:   { type: integer }
  ratio:  { type: number }
  enable: { type: boolean }
}
`)
	values := map[string]any{
		"region": "us-east-1",
		"size":   int64(3),
		"ratio":  1.5,
		"enable": true,
	}
	out, errs := ValidateInputs(decl, values, litEval)
	require.Equal(t, 0, errs.Len(), errs.Err())
	require.Equal(t, values, out)
}

func TestValidateInputsTypeMismatch(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  size: { type: integer }
}
`)
	_, errs := ValidateInputs(decl, map[string]any{"size": "five"}, litEval)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), `input "size"`)
	require.Contains(t, errs.Err().Error(), "expected integer, got string")
}

func TestValidateInputsIntegerRejectsNumber(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  size: { type: integer }
}
`)
	for _, v := range []float64{5.0, 5.5} {
		_, errs := ValidateInputs(decl, map[string]any{"size": v}, litEval)
		var got []string
		for _, e := range errs.Errors() {
			got = append(got, e.Msg)
		}
		require.Equal(t, []string{`input "size": expected integer, got number`}, got)
	}
}

func TestValidateInputsRequiredMissing(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  region: { type: string }
}
`)
	_, errs := ValidateInputs(decl, map[string]any{}, litEval)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "required but not provided")
}

func TestValidateInputsRequiredNull(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  region: { type: string }
}
`)
	_, errs := ValidateInputs(decl, map[string]any{"region": nil}, litEval)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "required but is null")
}

func TestValidateInputsOptionalNoDefaultMissing(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  region: { type: optional(string) }
}
`)
	out, errs := ValidateInputs(decl, map[string]any{}, litEval)
	require.Equal(t, 0, errs.Len(), errs.Err())
	require.Nil(t, out["region"])
}

func TestValidateInputsOptionalNoDefaultExplicitNull(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  region: { type: optional(string) }
}
`)
	out, errs := ValidateInputs(decl, map[string]any{"region": nil}, litEval)
	require.Equal(t, 0, errs.Len(), errs.Err())
	require.Nil(t, out["region"])
}

func TestValidateInputsOptionalDefaultAppliedOnMissing(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  size: { type: optional(integer, 3) }
}
`)
	out, errs := ValidateInputs(decl, map[string]any{}, litEval)
	require.Equal(t, 0, errs.Len(), errs.Err())
	require.Equal(t, int64(3), out["size"])
}

func TestValidateInputsOptionalDefaultAppliedOnNull(t *testing.T) {
	// Per the missing-or-null decision: explicit null also triggers the default.
	decl := parseInputsBlock(t, `
inputs: {
  size: { type: optional(integer, 3) }
}
`)
	out, errs := ValidateInputs(decl, map[string]any{"size": nil}, litEval)
	require.Equal(t, 0, errs.Len(), errs.Err())
	require.Equal(t, int64(3), out["size"])
}

func TestValidateInputsOptionalDefaultRespectsValue(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  size: { type: optional(integer, 3) }
}
`)
	out, errs := ValidateInputs(decl, map[string]any{"size": int64(7)}, litEval)
	require.Equal(t, 0, errs.Len(), errs.Err())
	require.Equal(t, int64(7), out["size"])
}

func TestValidateInputsNestedDefaultsApplied(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  spec: {
    type: object({
      host:    string,
      port:    { type: optional(integer, 8080) },
      retries: optional(integer, 3),
    })
  }
}
`)
	out, errs := ValidateInputs(decl, map[string]any{
		"spec": map[string]any{"host": "h", "port": nil},
	}, litEval)
	require.Equal(t, 0, errs.Len(), errs.Err())
	require.Equal(t, map[string]any{
		"spec": map[string]any{
			"host":    "h",
			"port":    int64(8080),
			"retries": int64(3),
		},
	}, out)
}

func TestValidateInputsNestedOptionalNoDefaultStaysNull(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  spec: {
    type: object({
      host: string,
      note: { type: optional(string) },
    })
  }
}
`)
	out, errs := ValidateInputs(decl, map[string]any{
		"spec": map[string]any{"host": "h"},
	}, litEval)
	require.Equal(t, 0, errs.Len(), errs.Err())
	require.Equal(t, map[string]any{
		"spec": map[string]any{"host": "h", "note": nil},
	}, out)
}

func TestValidateInputsNestedModifierEnforced(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  spec: {
    type: object({
      host: { type: string, min-length: 3 },
    })
  }
}
`)
	_, errs := ValidateInputs(decl, map[string]any{
		"spec": map[string]any{"host": "ab"},
	}, litEval)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), `field "host"`)
	require.Contains(t, errs.Err().Error(), "below min-length")

	_, errs = ValidateInputs(decl, map[string]any{
		"spec": map[string]any{"host": "abc"},
	}, litEval)
	require.Equal(t, 0, errs.Len(), errs.Err())
}

func TestValidateInputsNestedDefaultSatisfiesType(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  spec: {
    type: object({
      port: { type: optional(integer, 8080), minimum: 1024 },
    })
  }
}
`)
	out, errs := ValidateInputs(decl, map[string]any{
		"spec": map[string]any{},
	}, litEval)
	require.Equal(t, 0, errs.Len(), errs.Err())
	require.Equal(t, map[string]any{
		"spec": map[string]any{"port": int64(8080)},
	}, out)
}

func TestValidateInputsUnknownKey(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  region: { type: string }
}
`)
	_, errs := ValidateInputs(decl,
		map[string]any{"region": "us-east-1", "clustr-name": "web"}, litEval)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), `unknown input "clustr-name"`)
}

func TestValidateInputsList(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  subnets: { type: list(string) }
}
`)
	out, errs := ValidateInputs(decl,
		map[string]any{"subnets": []any{"a", "b"}}, litEval)
	require.Equal(t, 0, errs.Len(), errs.Err())
	require.Equal(t, []any{"a", "b"}, out["subnets"])
}

func TestValidateInputsListElementTypeMismatch(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  subnets: { type: list(string) }
}
`)
	_, errs := ValidateInputs(decl,
		map[string]any{"subnets": []any{"a", int64(5)}}, litEval)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "element 1")
	require.Contains(t, errs.Err().Error(), "expected string")
}

func TestValidateInputsMap(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  tags: { type: map(string) }
}
`)
	out, errs := ValidateInputs(decl,
		map[string]any{"tags": map[string]any{"Name": "web"}}, litEval)
	require.Equal(t, 0, errs.Len(), errs.Err())
	require.Equal(t, map[string]any{"Name": "web"}, out["tags"])
}

func TestValidateInputsObject(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  rule: { type: object({ from-port: integer, to-port: integer }) }
}
`)
	out, errs := ValidateInputs(decl,
		map[string]any{"rule": map[string]any{
			"from-port": int64(80),
			"to-port":   int64(443),
		}}, litEval)
	require.Equal(t, 0, errs.Len(), errs.Err())
	require.Equal(t, map[string]any{"from-port": int64(80), "to-port": int64(443)}, out["rule"])
}

func TestValidateInputsObjectMissingField(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  rule: { type: object({ from-port: integer, to-port: integer }) }
}
`)
	_, errs := ValidateInputs(decl,
		map[string]any{"rule": map[string]any{"from-port": int64(80)}}, litEval)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), `field "to-port"`)
}

func TestValidateInputsObjectUnknownField(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  rule: { type: object({ from-port: integer }) }
}
`)
	_, errs := ValidateInputs(decl,
		map[string]any{"rule": map[string]any{
			"from-port": int64(80),
			"surprise":  "yes",
		}}, litEval)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), `unknown field "surprise"`)
}

func TestValidateInputsOpenObjectKeepsExtraFields(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  payload: { type: open(object({ kind: string })) }
}
`)
	out, errs := ValidateInputs(decl,
		map[string]any{"payload": map[string]any{
			"kind":  "deploy",
			"extra": int64(7),
			"deep":  map[string]any{"a": true},
		}}, litEval)
	require.Equal(t, 0, errs.Len(), errs.Err())
	require.Equal(t, map[string]any{
		"kind":  "deploy",
		"extra": int64(7),
		"deep":  map[string]any{"a": true},
	}, out["payload"])
}

func TestValidateInputsOpenObjectChecksDeclaredFields(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  payload: { type: open(object({ kind: string })) }
}
`)
	_, errs := ValidateInputs(decl,
		map[string]any{"payload": map[string]any{
			"kind":  int64(1),
			"extra": "kept",
		}}, litEval)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), `field "kind"`)
	require.Contains(t, errs.Err().Error(), "expected string")
}

func TestValidateInputsTuple(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  pair: { type: tuple([string, integer]) }
}
`)
	out, errs := ValidateInputs(decl,
		map[string]any{"pair": []any{"web", int64(3)}}, litEval)
	require.Equal(t, 0, errs.Len(), errs.Err())
	require.Equal(t, []any{"web", int64(3)}, out["pair"])
}

func TestValidateInputsTupleWrongArity(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  pair: { type: tuple([string, integer]) }
}
`)
	_, errs := ValidateInputs(decl,
		map[string]any{"pair": []any{"web"}}, litEval)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "tuple of 2 elements, got 1")
}

func TestValidateInputsListOptionalDefaultEmpty(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  subnets: { type: optional(list(string), []) }
}
`)
	out, errs := ValidateInputs(decl, map[string]any{}, litEval)
	require.Equal(t, 0, errs.Len(), errs.Err())
	require.Equal(t, []any{}, out["subnets"])
}

func TestValidateInputsPattern(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  cluster-name: { type: string, pattern: '^[a-z][a-z0-9-]{0,30}$' }
}
`)
	_, errs := ValidateInputs(decl,
		map[string]any{"cluster-name": "Web-Prod"}, litEval)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "does not match pattern")
}

func TestValidateInputsPatternOK(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  cluster-name: { type: string, pattern: '^[a-z][a-z0-9-]{0,30}$' }
}
`)
	_, errs := ValidateInputs(decl,
		map[string]any{"cluster-name": "web-prod"}, litEval)
	require.Equal(t, 0, errs.Len(), errs.Err())
}

func TestValidateInputsMinimumMaximum(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  size: { type: integer, minimum: 1, maximum: 100 }
}
`)
	_, errs := ValidateInputs(decl, map[string]any{"size": int64(0)}, litEval)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "below minimum")

	_, errs = ValidateInputs(decl, map[string]any{"size": int64(101)}, litEval)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "above maximum")

	_, errs = ValidateInputs(decl, map[string]any{"size": int64(50)}, litEval)
	require.Equal(t, 0, errs.Len(), errs.Err())
}

func TestValidateInputsMinMaxLength(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  name: { type: string, min-length: 2, max-length: 5 }
}
`)
	_, errs := ValidateInputs(decl, map[string]any{"name": "a"}, litEval)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "below min-length")

	_, errs = ValidateInputs(decl, map[string]any{"name": "abcdef"}, litEval)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "above max-length")

	_, errs = ValidateInputs(decl, map[string]any{"name": "abc"}, litEval)
	require.Equal(t, 0, errs.Len(), errs.Err())
}

func TestValidateInputsLengthCountsRunes(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  name: { type: string, min-length: 2, max-length: 5 }
}
`)
	// Five characters, six bytes: counting characters keeps it within
	// the max-length of five.
	_, errs := ValidateInputs(decl, map[string]any{"name": "naïve"}, litEval)
	require.Equal(t, 0, errs.Len(), errs.Err())
}

func TestValidateInputsMinMaxItems(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  subnets: { type: list(string), min-items: 1, max-items: 3 }
}
`)
	_, errs := ValidateInputs(decl, map[string]any{"subnets": []any{}}, litEval)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "below min-items")

	_, errs = ValidateInputs(decl,
		map[string]any{"subnets": []any{"a", "b", "c", "d"}}, litEval)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "above max-items")

	_, errs = ValidateInputs(decl,
		map[string]any{"subnets": []any{"a", "b"}}, litEval)
	require.Equal(t, 0, errs.Len(), errs.Err())
}

func TestValidateInputsEnum(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  region: { type: string, enum: ['us-east-1', 'us-west-2'] }
}
`)
	_, errs := ValidateInputs(decl, map[string]any{"region": "ap-south-1"}, litEval)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "not one of the allowed enum")

	_, errs = ValidateInputs(decl, map[string]any{"region": "us-east-1"}, litEval)
	require.Equal(t, 0, errs.Len(), errs.Err())
}

func TestValidateInputsFormatDateTime(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  when: { type: string, format: date-time }
}
`)
	_, errs := ValidateInputs(decl,
		map[string]any{"when": "2026-05-11T15:00:00Z"}, litEval)
	require.Equal(t, 0, errs.Len(), errs.Err())

	_, errs = ValidateInputs(decl, map[string]any{"when": "yesterday"}, litEval)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), "not a valid date-time")
}

func TestValidateInputsCollectsMultipleErrors(t *testing.T) {
	decl := parseInputsBlock(t, `
inputs: {
  region: { type: string }
  size:   { type: integer, minimum: 1 }
}
`)
	_, errs := ValidateInputs(decl,
		map[string]any{"size": int64(0), "extra": true}, litEval)
	require.GreaterOrEqual(t, errs.Len(), 3)
}
