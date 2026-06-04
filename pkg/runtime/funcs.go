package runtime

import (
	"fmt"
	"reflect"

	"github.com/cloudboss/unobin/pkg/lang"
)

// MakeFunc adapts a plain typed Go function into a FunctionType, the
// way text/template adapts a FuncMap entry. fn may take any number of
// parameters, variadic included, and must return (value, error); each
// parameter must be a type the evaluator's values convert to exactly:
// bool, int64, float64, string, any, or a slice or string-keyed map of
// those.
// Evaluated arguments convert to the parameter types on the way in and
// the result converts back to evaluator values on the way out, so the
// implementation stays plain typed Go and the argument count cannot
// disagree with the declaration.
//
// MakeFunc panics on a fn that does not fit. Registration runs when a
// factory binary starts and when a library's own tests construct it;
// the compiler rejects the same signatures from source, so a malformed
// registration fails the importing factory's compile first.
func MakeFunc(name, description string, fn any) FunctionType {
	v := reflect.ValueOf(fn)
	t := v.Type()
	validateFuncSignature(name, t)
	fixed := t.NumIn()
	if t.IsVariadic() {
		fixed--
	}
	return FunctionType{
		Name:        name,
		Description: description,
		ArgCount:    fixed,
		Variadic:    t.IsVariadic(),
		Func: func(args []any) (any, error) {
			if err := wantArgs(name, args, fixed, t.IsVariadic()); err != nil {
				return nil, err
			}
			in := make([]reflect.Value, 0, len(args))
			for i, raw := range args {
				target := t.In(min(i, fixed))
				if i >= fixed {
					target = t.In(fixed).Elem()
				}
				av, err := convertArg(name, i+1, target, raw)
				if err != nil {
					return nil, err
				}
				in = append(in, av)
			}
			rets := v.Call(in)
			if e, ok := rets[1].Interface().(error); ok && e != nil {
				return nil, e
			}
			return toEvalValue(rets[0]), nil
		},
	}
}

// validateFuncSignature panics unless t is a function returning (value,
// error) whose every parameter and value result is a supported
// language value type. The message names the function so a
// registration mistake reads clearly from a test failure or factory
// start.
func validateFuncSignature(name string, t reflect.Type) {
	if t.Kind() != reflect.Func {
		panic(fmt.Sprintf("function %s must be a Go function, got %s", name, t))
	}
	switch t.NumOut() {
	case 2:
	case 1:
		panic(fmt.Sprintf("function %s must return (value, error), got 1 result", name))
	default:
		panic(fmt.Sprintf("function %s must return (value, error), got %d results",
			name, t.NumOut()))
	}
	if t.Out(1) != reflect.TypeFor[error]() {
		panic(fmt.Sprintf("function %s must return (value, error); the second result is %s",
			name, t.Out(1)))
	}
	if !supportedParam(t.Out(0)) {
		panic(fmt.Sprintf("function %s result has unsupported type %s", name, t.Out(0)))
	}
	for i := range t.NumIn() {
		p := t.In(i)
		if t.IsVariadic() && i == t.NumIn()-1 {
			p = p.Elem()
		}
		if !supportedParam(p) {
			panic(fmt.Sprintf("function %s parameter %d has unsupported type %s",
				name, i+1, t.In(i)))
		}
	}
}

// supportedParam reports whether a parameter or result type is one the
// evaluator's values convert to and from exactly: bool, int64, float64,
// string, any, or a slice or string-keyed map of those. Other kinds,
// named types included, are rejected so a registration cannot validate
// and then leak values the language cannot hold.
func supportedParam(t reflect.Type) bool {
	switch t {
	case reflect.TypeFor[bool](), reflect.TypeFor[int64](),
		reflect.TypeFor[float64](), reflect.TypeFor[string]():
		return true
	}
	switch t.Kind() {
	case reflect.Interface:
		return t.NumMethod() == 0
	case reflect.Slice:
		return supportedParam(t.Elem())
	case reflect.Map:
		return t.Key().Kind() == reflect.String && supportedParam(t.Elem())
	}
	return false
}

// wantArgs checks the evaluated argument count against the signature,
// so a miscounted call fails with a clear message instead of an index
// panic inside the implementation.
func wantArgs(name string, args []any, count int, variadic bool) error {
	if variadic {
		if len(args) < count {
			return fmt.Errorf("%s: expected at least %s, got %d",
				name, argWord(count), len(args))
		}
		return nil
	}
	if len(args) != count {
		return fmt.Errorf("%s: expected %s, got %d", name, argWord(count), len(args))
	}
	return nil
}

func argWord(n int) string {
	if n == 1 {
		return "1 argument"
	}
	return fmt.Sprintf("%d arguments", n)
}

// convertArg converts one evaluated argument to a parameter type,
// stepping into slices and maps element by element. pos is the
// 1-based argument position for the error message.
func convertArg(name string, pos int, target reflect.Type, v any) (reflect.Value, error) {
	out, err := convertValue(target, v)
	if err != nil {
		return reflect.Value{}, fmt.Errorf("%s: argument %d%s", name, pos, err)
	}
	return out, nil
}

// convertValue converts an evaluator value to the target type. The
// error begins with a space or colon so convertArg can append it to
// the argument position directly.
func convertValue(target reflect.Type, v any) (reflect.Value, error) {
	if target.Kind() == reflect.Interface && target.NumMethod() == 0 {
		if v == nil {
			return reflect.Zero(target), nil
		}
		return reflect.ValueOf(v), nil
	}
	if v == nil {
		return reflect.Value{}, fmt.Errorf(" must be %s, got null", typeWord(target))
	}
	rv := reflect.ValueOf(v)
	if rv.Type() == target {
		return rv, nil
	}
	switch target.Kind() {
	case reflect.Slice:
		lst, ok := v.([]any)
		if !ok {
			return reflect.Value{}, fmt.Errorf(
				" must be a list, got %s", lang.TypeMessage(v))
		}
		out := reflect.MakeSlice(target, len(lst), len(lst))
		for i, el := range lst {
			ev, err := convertValue(target.Elem(), el)
			if err != nil {
				return reflect.Value{}, fmt.Errorf(": element %d%s", i, err)
			}
			out.Index(i).Set(ev)
		}
		return out, nil
	case reflect.Map:
		m, ok := v.(map[string]any)
		if !ok || target.Key().Kind() != reflect.String {
			return reflect.Value{}, fmt.Errorf(
				" must be an object, got %s", lang.TypeMessage(v))
		}
		out := reflect.MakeMapWithSize(target, len(m))
		for k, el := range m {
			ev, err := convertValue(target.Elem(), el)
			if err != nil {
				return reflect.Value{}, fmt.Errorf(": key %q%s", k, err)
			}
			out.SetMapIndex(reflect.ValueOf(k), ev)
		}
		return out, nil
	}
	return reflect.Value{}, fmt.Errorf(
		" must be %s, got %s", typeWord(target), lang.TypeMessage(v))
}

// toEvalValue maps a typed Go value onto the evaluator's currency:
// slices become []any, string-keyed maps become map[string]any, and
// scalars pass through.
func toEvalValue(rv reflect.Value) any {
	if !rv.IsValid() {
		return nil
	}
	switch rv.Kind() {
	case reflect.Interface:
		if rv.IsNil() {
			return nil
		}
		return toEvalValue(rv.Elem())
	case reflect.Slice:
		if rv.Type() == reflect.TypeFor[[]any]() {
			return rv.Interface()
		}
		out := make([]any, rv.Len())
		for i := range rv.Len() {
			out[i] = toEvalValue(rv.Index(i))
		}
		return out
	case reflect.Map:
		if rv.Type() == reflect.TypeFor[map[string]any]() {
			return rv.Interface()
		}
		out := make(map[string]any, rv.Len())
		for _, k := range rv.MapKeys() {
			out[k.String()] = toEvalValue(rv.MapIndex(k))
		}
		return out
	}
	return rv.Interface()
}

// typeWord names a parameter type the way diagnostics name values, so
// a mismatch reads "must be a boolean, got a string".
func typeWord(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Bool:
		return "a boolean"
	case reflect.Int64:
		return "an integer"
	case reflect.Float64:
		return "a number"
	case reflect.String:
		return "a string"
	case reflect.Slice:
		return "a list"
	case reflect.Map:
		return "an object"
	}
	return "a " + t.String()
}
