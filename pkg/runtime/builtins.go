package runtime

import (
	"encoding/base64"
	"fmt"
)

// BuiltinFunc is the signature every built-in implements. Eval has
// already reduced each argument to a Go value; the function validates
// its own arity and types and returns the result.
type BuiltinFunc func(args []any) (any, error)

// builtins is the registry Eval consults when it sees a Call with a
// bare identifier callee. Module-qualified calls (`alias.func(...)`)
// route through a separate path that is not yet implemented.
var builtins = map[string]BuiltinFunc{
	"format":     builtinFormat,
	"b64-encode": builtinB64Encode,
	"b64-decode": builtinB64Decode,
	"range":      builtinRange,
	"length":     builtinLength,
}

// builtinFormat is the canonical interpolation helper: a printf-style
// format string followed by zero or more values. Verbs are Go's fmt
// package verbs, so `%s` accepts a string and `%d` an integer.
func builtinFormat(args []any) (any, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("format: needs at least the format string")
	}
	f, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("format: first argument must be a string, got %T", args[0])
	}
	return fmt.Sprintf(f, args[1:]...), nil
}

func builtinB64Encode(args []any) (any, error) {
	s, err := singleStringArg("b64-encode", args)
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.EncodeToString([]byte(s)), nil
}

func builtinB64Decode(args []any) (any, error) {
	s, err := singleStringArg("b64-decode", args)
	if err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("b64-decode: %w", err)
	}
	return string(decoded), nil
}

// builtinRange returns the integers `[0, n)` as a list. Spec carve-out:
// the result is a list, so it is not a valid `@for-each` iterable;
// callers wanting fan-out write a map literal with intentional keys.
func builtinRange(args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("range: takes one argument, got %d", len(args))
	}
	n, ok := args[0].(int64)
	if !ok {
		return nil, fmt.Errorf("range: argument must be an integer, got %T", args[0])
	}
	if n < 0 {
		return nil, fmt.Errorf("range: argument must be non-negative, got %d", n)
	}
	out := make([]any, n)
	for i := range n {
		out[i] = i
	}
	return out, nil
}

// builtinLength returns the size of a string, list, or map. String
// length is in bytes; UTF-8 rune counting belongs in a separate helper
// if it is ever asked for.
func builtinLength(args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("length: takes one argument, got %d", len(args))
	}
	switch v := args[0].(type) {
	case string:
		return int64(len(v)), nil
	case []any:
		return int64(len(v)), nil
	case map[string]any:
		return int64(len(v)), nil
	}
	return nil, fmt.Errorf("length: argument must be a string, list, or map, got %T", args[0])
}

func singleStringArg(name string, args []any) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("%s: takes one argument, got %d", name, len(args))
	}
	s, ok := args[0].(string)
	if !ok {
		return "", fmt.Errorf("%s: argument must be a string, got %T", name, args[0])
	}
	return s, nil
}
