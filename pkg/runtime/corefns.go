package runtime

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

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
//
// sig, when set, replaces the reflected signature: a function whose
// Go parameter is wider than its language face (length takes any but
// accepts three kinds) declares the face here, and a parity test
// holds the declaration to the implementation.
var coreRegistrations = []struct {
	name        string
	description string
	fn          any
	sig         *typecheck.FuncSig
}{
	{"join",
		"Join a list's elements into one string with a separator between elements.",
		fnJoin, nil},
	{"to-json", "Render a value as compact JSON.", fnToJSON, nil},
	{"b64-encode", "Base64-encode a string.", fnB64Encode, nil},
	{"b64-decode", "Base64-decode a string.", fnB64Decode, nil},
	{"range", "Return the integers [0, n) as a list.", fnRange, nil},
	{"length", "Return the number of elements in a string, list, or map.", fnLength,
		&typecheck.FuncSig{
			Params: []typecheck.Type{typecheck.TUnion([]typecheck.Type{
				typecheck.TString(),
				typecheck.TList(typecheck.TOpaque()),
				typecheck.TMap(typecheck.TOpaque()),
			})},
			Result: typecheck.TInteger(),
		}},
	{"all", "Report whether every element of a list of booleans is true.", fnAll, nil},
	{"any", "Report whether at least one element of a list of booleans is true.", fnAny, nil},
	{"to-integer", "Convert a number or a numeric string to an integer.", fnToInteger,
		&typecheck.FuncSig{
			Params: []typecheck.Type{typecheck.TUnion([]typecheck.Type{
				typecheck.TString(), typecheck.TNumber(),
			})},
			Result: typecheck.TInteger(),
		}},
	{"to-number", "Convert an integer or a numeric string to a number.", fnToNumber,
		&typecheck.FuncSig{
			Params: []typecheck.Type{typecheck.TUnion([]typecheck.Type{
				typecheck.TString(), typecheck.TNumber(),
			})},
			Result: typecheck.TNumber(),
		}},
	{"to-string", "Render a string, number, or boolean as text.", fnToString,
		&typecheck.FuncSig{
			Params: []typecheck.Type{typecheck.TUnion([]typecheck.Type{
				typecheck.TString(), typecheck.TNumber(), typecheck.TBoolean(),
			})},
			Result: typecheck.TString(),
		}},
	{"to-boolean", `Convert the string "true" or "false" to a boolean.`, fnToBoolean,
		&typecheck.FuncSig{
			Params: []typecheck.Type{typecheck.TUnion([]typecheck.Type{
				typecheck.TString(), typecheck.TBoolean(),
			})},
			Result: typecheck.TBoolean(),
		}},
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
// from the same implementations the runtime executes unless the
// registration declares an explicit face.
var coreSigs = func() map[string]typecheck.FuncSig {
	out := make(map[string]typecheck.FuncSig, len(coreRegistrations))
	for _, reg := range coreRegistrations {
		if reg.sig != nil {
			out[reg.name] = *reg.sig
			continue
		}
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
		return typecheck.TOpaque()
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

// fnLength returns the size of a string, list, or map. A string's
// length is its number of characters (Unicode code points), not its
// byte count.
func fnLength(v any) (int64, error) {
	switch x := v.(type) {
	case string:
		return int64(utf8.RuneCountInString(x)), nil
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

// fnToInteger converts a number or a numeric string to an integer. A
// number is truncated toward zero -- the lossy direction the implicit
// widening from integer to number will not take -- and a string must
// spell an integer in base ten.
func fnToInteger(v any) (int64, error) {
	switch x := v.(type) {
	case int64:
		return x, nil
	case float64:
		return int64(x), nil
	case string:
		n, err := strconv.ParseInt(x, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("to-integer: %q is not an integer", x)
		}
		return n, nil
	}
	return 0, fmt.Errorf(
		"to-integer: argument must be a string or number, got %s", lang.TypeMessage(v))
}

// fnToNumber converts an integer or a numeric string to a number.
func fnToNumber(v any) (float64, error) {
	switch x := v.(type) {
	case float64:
		return x, nil
	case int64:
		return float64(x), nil
	case string:
		n, err := strconv.ParseFloat(x, 64)
		if err != nil {
			return 0, fmt.Errorf("to-number: %q is not a number", x)
		}
		return n, nil
	}
	return 0, fmt.Errorf(
		"to-number: argument must be a string or number, got %s", lang.TypeMessage(v))
}

// fnToString renders a string, number, or boolean as text, the same
// rendering an interpolation slot uses. A list or map is an error;
// render those as JSON with @core.to-json.
func fnToString(v any) (string, error) {
	switch v.(type) {
	case string, bool, int64, float64:
		return renderScalar(v), nil
	}
	return "", fmt.Errorf(
		"to-string: argument must be a string, number, or boolean, got %s", lang.TypeMessage(v))
}

// fnToBoolean converts the string "true" or "false" to a boolean, and
// returns a boolean unchanged.
func fnToBoolean(v any) (bool, error) {
	switch x := v.(type) {
	case bool:
		return x, nil
	case string:
		switch x {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return false, fmt.Errorf("to-boolean: %q is not true or false", x)
	}
	return false, fmt.Errorf(
		"to-boolean: argument must be a string or boolean, got %s", lang.TypeMessage(v))
}
