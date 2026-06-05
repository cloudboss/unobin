package runtime

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

// coreRegistrations binds each @core function to its typed
// implementation. The runtime registration and the compile-time
// signature both derive from this one list, so the language's
// namespace cannot disagree with what the checker believes about it.
// The set is part of the language's compatibility promise: behavior
// never changes, the set only grows, and a semantic change takes a
// new name.
var coreRegistrations = []struct {
	name        string
	description string
	fn          any
}{
	{"join",
		"Join a list's elements into one string with a separator between elements.",
		fnJoin},
	{"to-json", "Render a value as compact JSON.", fnToJSON},
	{"b64-encode", "Base64-encode a string.", fnB64Encode},
	{"b64-decode", "Base64-decode a string.", fnB64Decode},
	{"range", "Return the integers [0, n) as a list.", fnRange},
	{"length", "Return the number of elements in a string, list, or map.", fnLength},
	{"all", "Report whether every element of a list of booleans is true.", fnAll},
	{"any", "Report whether at least one element of a list of booleans is true.", fnAny},
}

// coreFunctions is the language's function namespace: what a call
// qualified with @core resolves to, in every evaluation context and
// with no import.
var coreFunctions = func() map[string]FunctionType {
	out := make(map[string]FunctionType, len(coreRegistrations))
	for _, reg := range coreRegistrations {
		out[reg.name] = MakeFunc(reg.name, reg.description, reg.fn)
	}
	return out
}()

// coreSigs holds each @core function's compile-time signature, read
// from the same implementations the runtime executes.
var coreSigs = func() map[string]typecheck.FuncSig {
	out := make(map[string]typecheck.FuncSig, len(coreRegistrations))
	for _, reg := range coreRegistrations {
		out[reg.name] = sigFromFunc(reg.fn)
	}
	return out
}()

// CoreFunctionSigs returns each @core function's signature, keyed by
// name, for compile-time existence, arity, and type checking.
func CoreFunctionSigs() map[string]typecheck.FuncSig {
	return coreSigs
}

// sigFromFunc reads a registered function's reflected signature into
// the form the inferrer checks calls against.
func sigFromFunc(fn any) typecheck.FuncSig {
	t := reflect.TypeOf(fn)
	sig := typecheck.FuncSig{Result: typeFromReflect(t.Out(0))}
	fixed := t.NumIn()
	if t.IsVariadic() {
		fixed--
		v := typeFromReflect(t.In(fixed).Elem())
		sig.Variadic = &v
	}
	for i := range fixed {
		sig.Params = append(sig.Params, typeFromReflect(t.In(i)))
	}
	return sig
}

// typeFromReflect maps a registered function's parameter or result
// type onto the language's type vocabulary, the way goschema maps a
// library function's source types.
func typeFromReflect(t reflect.Type) typecheck.Type {
	switch t {
	case reflect.TypeFor[bool]():
		return typecheck.TBoolean()
	case reflect.TypeFor[int64]():
		return typecheck.TInteger()
	case reflect.TypeFor[float64]():
		return typecheck.TNumber()
	case reflect.TypeFor[string]():
		return typecheck.TString()
	}
	switch t.Kind() {
	case reflect.Interface:
		return typecheck.TAny()
	case reflect.Slice:
		return typecheck.TList(typeFromReflect(t.Elem()))
	case reflect.Map:
		return typecheck.TMap(typeFromReflect(t.Elem()))
	}
	return typecheck.TUnknown()
}

// fnJoin joins a list's elements with sep between them. Elements render
// with the same rules as an interpolation slot, so a null or composite
// element is an error naming its index.
func fnJoin(elems []any, sep string) (string, error) {
	parts := make([]string, len(elems))
	for i, el := range elems {
		switch el.(type) {
		case nil:
			return "", fmt.Errorf("join: element %d is null", i)
		case []any, map[string]any:
			return "", fmt.Errorf(
				"join: element %d must be a scalar, got %s", i, lang.TypeMessage(el))
		}
		parts[i] = renderScalar(el)
	}
	return strings.Join(parts, sep), nil
}

// fnToJSON renders a value as compact JSON on one line. Object keys
// come out sorted, so the result is deterministic. HTML escaping is
// off so < > & pass through untouched; the output goes into rendered
// configuration, not web pages.
func fnToJSON(v any) (string, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", fmt.Errorf("to-json: %w", err)
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}

func fnB64Encode(s string) (string, error) {
	return base64.StdEncoding.EncodeToString([]byte(s)), nil
}

func fnB64Decode(s string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", fmt.Errorf("b64-decode: %w", err)
	}
	return string(decoded), nil
}

// fnRange returns the integers [0, n) as a list. The result is a list,
// so it is not a valid @for-each iterable; callers wanting fan-out write
// a map literal with intentional keys.
func fnRange(n int64) ([]int64, error) {
	if n < 0 {
		return nil, fmt.Errorf("range: argument must be non-negative, got %d", n)
	}
	out := make([]int64, n)
	for i := range n {
		out[i] = i
	}
	return out, nil
}

// fnLength returns the size of a string, list, or map. String length is
// in bytes; UTF-8 rune counting belongs in a separate helper if it is
// ever asked for.
func fnLength(v any) (int64, error) {
	switch x := v.(type) {
	case string:
		return int64(len(x)), nil
	case []any:
		return int64(len(x)), nil
	case map[string]any:
		return int64(len(x)), nil
	}
	return 0, fmt.Errorf(
		"length: argument must be a string, list, or map, got %s", lang.TypeMessage(v))
}

// fnAll reports whether every element of a list of booleans is true.
// An empty list is true: no element is false. Pairs with a boolean
// comprehension to quantify over a list, as a constraint predicate
// does: @core.all([for r in var.replicas: r.port > 0]).
func fnAll(bools []bool) (bool, error) {
	for _, b := range bools {
		if !b {
			return false, nil
		}
	}
	return true, nil
}

// fnAny reports whether at least one element of a list of booleans is
// true. An empty list is false: no element is true.
func fnAny(bools []bool) (bool, error) {
	for _, b := range bools {
		if b {
			return true, nil
		}
	}
	return false, nil
}
