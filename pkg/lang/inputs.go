package lang

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// EvalFunc reduces an expression to a Go value against an empty
// context. Input declaration defaults evaluate this way so the
// validator can apply them without depending on the runtime package.
type EvalFunc func(e Expr) (any, error)

// LibraryConfigSchema is the object view of a library-config input type.
type LibraryConfigSchema struct {
	Type     *TypeObject
	Defaults []DefaultSpec
}

// LibraryConfigResolver resolves a library-config path literal to its schema.
type LibraryConfigResolver func(path string) (LibraryConfigSchema, bool)

// ValidateInputs validates an operator-supplied values map against a
// stack's `inputs:` declaration. Returns the validated values with
// declaration defaults applied. Errors cover missing
// required inputs, unknown keys, type mismatches, and modifier
// violations. The decl is the parsed `inputs:` block; values is the
// map produced by loadConfigInputs + applyEnvOverrides. evalDefault
// reduces default expressions to Go values; pass nil to refuse any
// default that requires evaluation.
func ValidateInputs(
	decl *ObjectLit, values map[string]any, evalDefault EvalFunc,
) (map[string]any, *ErrorList) {
	return validateInputs(decl, values, evalDefault, nil)
}

// ValidateInputsWithLibraryConfigs validates inputs with library-config schemas.
func ValidateInputsWithLibraryConfigs(
	decl *ObjectLit,
	values map[string]any,
	evalDefault EvalFunc,
	resolve LibraryConfigResolver,
) (map[string]any, *ErrorList) {
	return validateInputs(decl, values, evalDefault, resolve)
}

func validateInputs(
	decl *ObjectLit,
	values map[string]any,
	evalDefault EvalFunc,
	resolve LibraryConfigResolver,
) (map[string]any, *ErrorList) {
	errs := NewErrorList(0)
	out := make(map[string]any)

	declared := map[string]bool{}
	if decl != nil {
		for _, fld := range decl.Fields {
			if fld.Key.Kind != FieldIdent || fld.Key.IsMeta() {
				continue
			}
			declared[fld.Key.Name] = true
			validateOneInput(fld, values, out, evalDefault, resolve, errs)
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

func validateOneInput(
	fld *Field,
	values, out map[string]any,
	evalDefault EvalFunc,
	resolve LibraryConfigResolver,
	errs *ErrorList,
) {
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
	if !present && defaultExpr != nil {
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

	coerced, err := checkValue(typeExpr, raw, evalDefault, resolve)
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

// extractTypeAndDefault pulls the `type:` and optional `default:`
// expressions from an input declaration. ok is false if the declaration
// has no `type:` or its type expression is malformed; an earlier
// ValidateInputDeclarations pass should have reported either case.
func extractTypeAndDefault(
	decl *ObjectLit,
) (typeExpr TypeExpr, defaultExpr Expr, isOptional, ok bool) {
	var typeField, defaultField Expr
	for _, df := range decl.Fields {
		if df.Key.Kind != FieldIdent {
			continue
		}
		switch df.Key.Name {
		case "type":
			typeField = df.Value
		case "default":
			defaultField = df.Value
		}
	}
	if typeField == nil {
		return nil, nil, false, false
	}
	t, ok := typeField.(TypeExpr)
	if !ok {
		return nil, nil, false, false
	}
	if opt, ok := t.(*TypeOptional); ok {
		return opt.Elem, defaultField, true, true
	}
	return t, defaultField, false, true
}

// checkValue validates v against the declared type and returns the
// coerced value. ev evaluates declaration defaults on nested object
// fields; nil refuses any nested default that would need it.
func checkValue(
	t TypeExpr,
	v any,
	ev EvalFunc,
	resolve LibraryConfigResolver,
) (any, error) {
	switch tt := t.(type) {
	case *TypeAtomic:
		return checkAtomic(tt, v)
	case *TypeList:
		return checkList(tt, v, ev, resolve)
	case *TypeMap:
		return checkMap(tt, v, ev, resolve)
	case *TypeObject:
		return checkObject(tt, v, ev, resolve)
	case *TypeTuple:
		return checkTuple(tt, v, ev, resolve)
	case *TypeOptional:
		if v == nil {
			return nil, nil
		}
		return checkValue(tt.Elem, v, ev, resolve)
	case *TypeLibraryConfig:
		return checkLibraryConfig(tt, v, ev, resolve)
	}
	return nil, fmt.Errorf("unsupported type %T", t)
}

func checkLibraryConfig(
	t *TypeLibraryConfig,
	v any,
	ev EvalFunc,
	resolve LibraryConfigResolver,
) (any, error) {
	if t == nil || t.Path == nil {
		return nil, fmt.Errorf("library-config path is missing")
	}
	if resolve == nil {
		return nil, fmt.Errorf("library-config %q has no schema resolver", t.Path.Value)
	}
	schema, ok := resolve(t.Path.Value)
	if !ok || schema.Type == nil {
		return nil, fmt.Errorf("library-config %q schema is unavailable", t.Path.Value)
	}
	raw, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected object, got %s", typeName(v))
	}
	withDefaults := cloneMap(raw)
	if err := applyDefaultSpecs(withDefaults, schema.Defaults, ev); err != nil {
		return nil, err
	}
	return checkObject(schema.Type, withDefaults, ev, resolve)
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneValue(v)
	}
	return out
}

func cloneValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return cloneMap(x)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = cloneValue(item)
		}
		return out
	}
	return v
}

func applyDefaultSpecs(values map[string]any, specs []DefaultSpec, ev EvalFunc) error {
	for _, spec := range specs {
		if spec.Optional {
			continue
		}
		path, ok := strings.CutPrefix(spec.Field, "var.")
		if !ok || path == "" {
			continue
		}
		if ev == nil {
			return fmt.Errorf("field %q: cannot evaluate default (no evaluator provided)", path)
		}
		expr, err := ParseExpr("default", []byte(spec.Value))
		if err != nil {
			return fmt.Errorf("field %q: invalid default: %v", path, err)
		}
		value, err := ev(expr)
		if err != nil {
			return fmt.Errorf("field %q: invalid default: %v", path, err)
		}
		applyDefaultValue(values, strings.Split(path, "."), value)
	}
	return nil
}

func applyDefaultValue(values map[string]any, path []string, value any) {
	if len(path) == 0 {
		return
	}
	target := values
	for _, parent := range path[:len(path)-1] {
		child, ok := target[parent]
		if !ok {
			next := map[string]any{}
			target[parent] = next
			target = next
			continue
		}
		next, ok := child.(map[string]any)
		if !ok {
			return
		}
		target = next
	}
	leaf := path[len(path)-1]
	if _, ok := target[leaf]; ok {
		return
	}
	target[leaf] = value
}

func checkAtomic(t *TypeAtomic, v any) (any, error) {
	switch t.Name {
	case "opaque":
		return v, nil
	case "string":
		if s, ok := v.(string); ok {
			return s, nil
		}
		return nil, fmt.Errorf("expected string, got %s", typeName(v))
	case "integer":
		if x, ok := v.(int64); ok {
			return x, nil
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

func checkList(
	t *TypeList,
	v any,
	ev EvalFunc,
	resolve LibraryConfigResolver,
) (any, error) {
	xs, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("expected list, got %s", typeName(v))
	}
	out := make([]any, len(xs))
	for i, el := range xs {
		coerced, err := checkValue(t.Elem, el, ev, resolve)
		if err != nil {
			return nil, fmt.Errorf("element %d: %w", i, err)
		}
		out[i] = coerced
	}
	return out, nil
}

func checkMap(
	t *TypeMap,
	v any,
	ev EvalFunc,
	resolve LibraryConfigResolver,
) (any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected map, got %s", typeName(v))
	}
	out := make(map[string]any, len(m))
	for k, val := range m {
		coerced, err := checkValue(t.Elem, val, ev, resolve)
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", k, err)
		}
		out[k] = coerced
	}
	return out, nil
}

func checkObject(
	t *TypeObject,
	v any,
	ev EvalFunc,
	resolve LibraryConfigResolver,
) (any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected object, got %s", typeName(v))
	}
	out := make(map[string]any, len(t.Fields))
	declared := map[string]bool{}
	for _, f := range t.Fields {
		declared[f.Name] = true
		raw, present := m[f.Name]
		ft, defaultExpr, isOpt, ok := fieldTypeAndDefault(f)
		if !ok {
			continue
		}
		if !present && defaultExpr != nil {
			if ev == nil {
				return nil, fmt.Errorf(
					"field %q: cannot evaluate default (no evaluator provided)", f.Name)
			}
			def, err := ev(defaultExpr)
			if err != nil {
				return nil, fmt.Errorf("field %q: invalid default: %v", f.Name, err)
			}
			raw, present = def, true
		}
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
		coerced, err := checkValue(ft, raw, ev, resolve)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", f.Name, err)
		}
		if f.Decl != nil {
			if err := checkModifiers(f.Decl, coerced); err != nil {
				return nil, fmt.Errorf("field %q: %v", f.Name, err)
			}
		}
		out[f.Name] = coerced
	}
	for k := range m {
		if declared[k] {
			continue
		}
		// An open object keeps undeclared fields as they came in; they
		// pass through unread, so there is nothing to check them against.
		if t.Open {
			out[k] = m[k]
			continue
		}
		return nil, fmt.Errorf("unknown field %q", k)
	}
	return out, nil
}

// fieldTypeAndDefault resolves an object field to its value type,
// default expression, and optionality, whichever declaration form it
// used: a full declaration object, a bare optional(T), or a bare type.
func fieldTypeAndDefault(f *TypeObjectField) (TypeExpr, Expr, bool, bool) {
	if f.Decl != nil {
		return extractTypeAndDefault(f.Decl)
	}
	if f.Type == nil {
		return nil, nil, false, false
	}
	if opt, ok := f.Type.(*TypeOptional); ok {
		return opt.Elem, nil, true, true
	}
	return f.Type, nil, false, true
}

func checkTuple(
	t *TypeTuple,
	v any,
	ev EvalFunc,
	resolve LibraryConfigResolver,
) (any, error) {
	xs, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("expected tuple, got %s", typeName(v))
	}
	if len(xs) != len(t.Elements) {
		return nil, fmt.Errorf("expected tuple of %d elements, got %d", len(t.Elements), len(xs))
	}
	out := make([]any, len(xs))
	for i, el := range xs {
		coerced, err := checkValue(t.Elements[i], el, ev, resolve)
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
		case "type", "default", "description":
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

// checkLengthBound checks a string against a min-length or max-length
// bound, counting characters (Unicode code points) the way @core.length
// does, not bytes.
func checkLengthBound(name string, expr Expr, v any, isMin bool) error {
	bound, err := intLiteral(expr)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("%s: only applies to string values, got %s", name, typeName(v))
	}
	n := int64(utf8.RuneCountInString(s))
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
