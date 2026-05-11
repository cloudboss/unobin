package lang

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// EvalFunc reduces an expression to a Go value against an empty
// context. Defaults inside `optional(T, default)` evaluate this way so
// the validator can apply them without depending on the runtime
// package.
type EvalFunc func(e Expr) (any, error)

// ValidateInputs validates an operator-supplied values map against a
// stack's `inputs:` declaration. Returns the validated values with
// `optional(T, default)` defaults applied. Errors cover missing
// required inputs, unknown keys, type mismatches, and modifier
// violations. The decl is the parsed `inputs:` block; values is the
// map produced by loadConfigInputs + applyEnvOverrides. evalDefault
// reduces default expressions to Go values; pass nil to refuse any
// default that requires evaluation.
func ValidateInputs(decl *ObjectLit, values map[string]any, evalDefault EvalFunc) (map[string]any, *ErrorList) {
	errs := NewErrorList(0)
	out := make(map[string]any)

	declared := map[string]bool{}
	if decl != nil {
		for _, fld := range decl.Fields {
			if fld.Key.Kind != FieldIdent || fld.Key.IsMeta() {
				continue
			}
			declared[fld.Key.Name] = true
			validateOneInput(fld, values, out, evalDefault, errs)
		}
	}

	for name := range values {
		if !declared[name] {
			errs.Addf(ErrSchema, Position{},
				"unknown input %q: not declared in the stack's `inputs:` block", name)
		}
	}
	return out, errs
}

func validateOneInput(fld *Field, values, out map[string]any, evalDefault EvalFunc, errs *ErrorList) {
	name := fld.Key.Name
	declObj, ok := fld.Value.(*ObjectLit)
	if !ok {
		return
	}
	typeExpr, defaultExpr, isOptional, ok := extractTypeAndDefault(declObj)
	if !ok {
		return
	}

	raw, present := values[name]
	if (!present || raw == nil) && isOptional && defaultExpr != nil {
		if evalDefault == nil {
			errs.Addf(ErrSchema, defaultExpr.Span().Start,
				"input %q: cannot evaluate default (no evaluator provided)", name)
			return
		}
		def, err := evalDefault(defaultExpr)
		if err != nil {
			errs.Addf(ErrSchema, defaultExpr.Span().Start,
				"input %q: invalid default: %v", name, err)
			return
		}
		raw = def
		present = true
	}

	if !present {
		if isOptional {
			out[name] = nil
			return
		}
		errs.Addf(ErrSchema, fld.Key.S.Start,
			"input %q: required but not provided", name)
		return
	}
	if raw == nil {
		if isOptional {
			out[name] = nil
			return
		}
		errs.Addf(ErrSchema, fld.Key.S.Start,
			"input %q: required but is null", name)
		return
	}

	coerced, err := checkValue(typeExpr, raw)
	if err != nil {
		errs.Addf(ErrType, fld.Key.S.Start, "input %q: %v", name, err)
		return
	}
	if err := checkModifiers(declObj, coerced); err != nil {
		errs.Addf(ErrSchema, fld.Key.S.Start, "input %q: %v", name, err)
		return
	}
	out[name] = coerced
}

// extractTypeAndDefault pulls the `type:` expression from an input
// declaration and unwraps a top-level `optional(T, default)` so the
// caller can treat the inner type as the value's actual type. ok is
// false if the declaration has no `type:` or its type expression is
// malformed; an earlier ValidateInputDeclarations pass should have
// reported either case.
func extractTypeAndDefault(decl *ObjectLit) (typeExpr TypeExpr, defaultExpr Expr, isOptional, ok bool) {
	for _, df := range decl.Fields {
		if df.Key.Kind != FieldIdent || df.Key.Name != "type" {
			continue
		}
		t, err := PromoteType(df.Value)
		if err != nil {
			return nil, nil, false, false
		}
		if opt, ok := t.(*TypeOptional); ok {
			return opt.Elem, opt.Default, true, true
		}
		return t, nil, false, true
	}
	return nil, nil, false, false
}

func checkValue(t TypeExpr, v any) (any, error) {
	switch tt := t.(type) {
	case *TypeAtomic:
		return checkAtomic(tt, v)
	case *TypeList:
		return checkList(tt, v)
	case *TypeSet:
		return checkList(&TypeList{S: tt.S, Elem: tt.Elem}, v)
	case *TypeMap:
		return checkMap(tt, v)
	case *TypeObject:
		return checkObject(tt, v)
	case *TypeTuple:
		return checkTuple(tt, v)
	case *TypeOptional:
		if v == nil {
			return nil, nil
		}
		return checkValue(tt.Elem, v)
	}
	return nil, fmt.Errorf("unsupported type %T", t)
}

func checkAtomic(t *TypeAtomic, v any) (any, error) {
	switch t.Name {
	case "any":
		return v, nil
	case "string":
		if s, ok := v.(string); ok {
			return s, nil
		}
		return nil, fmt.Errorf("expected string, got %s", typeName(v))
	case "integer":
		switch x := v.(type) {
		case int64:
			return x, nil
		case float64:
			if x == float64(int64(x)) {
				return int64(x), nil
			}
			return nil, fmt.Errorf("expected integer, got %v (number with fraction)", x)
		}
		return nil, fmt.Errorf("expected integer, got %s", typeName(v))
	case "number":
		switch v.(type) {
		case int64, float64:
			return v, nil
		}
		return nil, fmt.Errorf("expected number, got %s", typeName(v))
	case "boolean":
		if b, ok := v.(bool); ok {
			return b, nil
		}
		return nil, fmt.Errorf("expected boolean, got %s", typeName(v))
	case "null":
		if v == nil {
			return nil, nil
		}
		return nil, fmt.Errorf("expected null, got %s", typeName(v))
	}
	return nil, fmt.Errorf("unknown atomic type %q", t.Name)
}

func checkList(t *TypeList, v any) (any, error) {
	xs, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("expected list, got %s", typeName(v))
	}
	out := make([]any, len(xs))
	for i, el := range xs {
		coerced, err := checkValue(t.Elem, el)
		if err != nil {
			return nil, fmt.Errorf("element %d: %w", i, err)
		}
		out[i] = coerced
	}
	return out, nil
}

func checkMap(t *TypeMap, v any) (any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected map, got %s", typeName(v))
	}
	out := make(map[string]any, len(m))
	for k, val := range m {
		coerced, err := checkValue(t.Elem, val)
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", k, err)
		}
		out[k] = coerced
	}
	return out, nil
}

func checkObject(t *TypeObject, v any) (any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected object, got %s", typeName(v))
	}
	out := make(map[string]any, len(t.Fields))
	declared := map[string]bool{}
	for _, f := range t.Fields {
		declared[f.Name] = true
		raw, present := m[f.Name]
		ft := f.Type
		if ft == nil && f.Decl != nil {
			te, _, _, ok := extractTypeAndDefault(f.Decl)
			if !ok {
				continue
			}
			ft = te
		}
		if ft == nil {
			continue
		}
		_, isOpt := ft.(*TypeOptional)
		if !present {
			if isOpt {
				out[f.Name] = nil
				continue
			}
			return nil, fmt.Errorf("field %q: required but not provided", f.Name)
		}
		if raw == nil {
			if isOpt {
				out[f.Name] = nil
				continue
			}
			return nil, fmt.Errorf("field %q: required but is null", f.Name)
		}
		coerced, err := checkValue(ft, raw)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", f.Name, err)
		}
		out[f.Name] = coerced
	}
	for k := range m {
		if !declared[k] {
			return nil, fmt.Errorf("unknown field %q", k)
		}
	}
	return out, nil
}

func checkTuple(t *TypeTuple, v any) (any, error) {
	xs, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("expected tuple, got %s", typeName(v))
	}
	if len(xs) != len(t.Elements) {
		return nil, fmt.Errorf("expected tuple of %d elements, got %d", len(t.Elements), len(xs))
	}
	out := make([]any, len(xs))
	for i, el := range xs {
		coerced, err := checkValue(t.Elements[i], el)
		if err != nil {
			return nil, fmt.Errorf("element %d: %w", i, err)
		}
		out[i] = coerced
	}
	return out, nil
}

func checkModifiers(decl *ObjectLit, v any) error {
	for _, df := range decl.Fields {
		if df.Key.Kind != FieldIdent || df.Key.IsMeta() {
			continue
		}
		switch df.Key.Name {
		case "type", "description":
			continue
		}
		if err := applyModifier(df.Key.Name, df.Value, v); err != nil {
			return err
		}
	}
	return nil
}

func applyModifier(name string, expr Expr, v any) error {
	switch name {
	case "pattern":
		return checkPattern(expr, v)
	case "minimum":
		return checkBound("minimum", expr, v, true)
	case "maximum":
		return checkBound("maximum", expr, v, false)
	case "min-length":
		return checkLengthBound("min-length", expr, v, true)
	case "max-length":
		return checkLengthBound("max-length", expr, v, false)
	case "min-items":
		return checkItemsBound("min-items", expr, v, true)
	case "max-items":
		return checkItemsBound("max-items", expr, v, false)
	case "enum":
		return checkEnum(expr, v)
	case "format":
		return checkFormat(expr, v)
	}
	return nil
}

func checkPattern(expr Expr, v any) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("pattern: only applies to string values")
	}
	lit, ok := expr.(*StringLit)
	if !ok {
		return fmt.Errorf("pattern: must be a string literal")
	}
	re, err := regexp.Compile(lit.Value)
	if err != nil {
		return fmt.Errorf("pattern: invalid regex: %w", err)
	}
	if !re.MatchString(s) {
		return fmt.Errorf("value %q does not match pattern %q", s, lit.Value)
	}
	return nil
}

func checkBound(name string, expr Expr, v any, isMin bool) error {
	bound, err := numberLiteral(expr)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	val, ok := numberValue(v)
	if !ok {
		return fmt.Errorf("%s: only applies to number values, got %s", name, typeName(v))
	}
	if isMin && val < bound {
		return fmt.Errorf("value %v is below %s %v", val, name, bound)
	}
	if !isMin && val > bound {
		return fmt.Errorf("value %v is above %s %v", val, name, bound)
	}
	return nil
}

func checkLengthBound(name string, expr Expr, v any, isMin bool) error {
	bound, err := intLiteral(expr)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("%s: only applies to string values, got %s", name, typeName(v))
	}
	n := int64(len(s))
	if isMin && n < bound {
		return fmt.Errorf("string length %d is below %s %d", n, name, bound)
	}
	if !isMin && n > bound {
		return fmt.Errorf("string length %d is above %s %d", n, name, bound)
	}
	return nil
}

func checkItemsBound(name string, expr Expr, v any, isMin bool) error {
	bound, err := intLiteral(expr)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	xs, ok := v.([]any)
	if !ok {
		return fmt.Errorf("%s: only applies to list values, got %s", name, typeName(v))
	}
	n := int64(len(xs))
	if isMin && n < bound {
		return fmt.Errorf("list has %d items, below %s %d", n, name, bound)
	}
	if !isMin && n > bound {
		return fmt.Errorf("list has %d items, above %s %d", n, name, bound)
	}
	return nil
}

func checkEnum(expr Expr, v any) error {
	arr, ok := expr.(*ArrayLit)
	if !ok {
		return fmt.Errorf("enum: must be an array of allowed values")
	}
	for _, el := range arr.Elements {
		want, err := literalValue(el)
		if err != nil {
			return fmt.Errorf("enum: %w", err)
		}
		if literalsEqual(want, v) {
			return nil
		}
	}
	return fmt.Errorf("value %v is not one of the allowed enum values", v)
}

func checkFormat(expr Expr, v any) error {
	id, ok := expr.(*Ident)
	if !ok {
		return fmt.Errorf("format: must be a bare identifier (e.g. date-time)")
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("format: only applies to string values, got %s", typeName(v))
	}
	switch id.Name {
	case "date-time":
		if _, err := time.Parse(time.RFC3339, s); err != nil {
			return fmt.Errorf("value %q is not a valid date-time: %w", s, err)
		}
		return nil
	}
	return fmt.Errorf("format: unknown format %q", id.Name)
}

func numberLiteral(e Expr) (float64, error) {
	if n, ok := e.(*NumberLit); ok {
		if n.IsFloat {
			return n.ParsedFloat, nil
		}
		return float64(n.ParsedInt), nil
	}
	return 0, fmt.Errorf("expected a number literal")
}

func intLiteral(e Expr) (int64, error) {
	n, ok := e.(*NumberLit)
	if !ok || n.IsFloat {
		return 0, fmt.Errorf("expected an integer literal")
	}
	return n.ParsedInt, nil
}

func numberValue(v any) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

func literalValue(e Expr) (any, error) {
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
	case *Ident:
		return v.Name, nil
	}
	return nil, fmt.Errorf("expected a literal")
}

func literalsEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == b
	}
	if af, aok := numberValue(a); aok {
		if bf, bok := numberValue(b); bok {
			return af == bf
		}
	}
	return a == b
}

func typeName(v any) string {
	if v == nil {
		return "null"
	}
	switch v.(type) {
	case string:
		return "string"
	case int64:
		return "integer"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "list"
	case map[string]any:
		return "map"
	}
	return strings.TrimPrefix(fmt.Sprintf("%T", v), "*")
}
