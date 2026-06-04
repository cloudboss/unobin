package core

import (
	"encoding/base64"
	"fmt"

	"github.com/cloudboss/unobin/pkg/lang"
)

// fnFormat is the canonical interpolation helper: a printf-style format
// string followed by zero or more values. Verbs are Go's fmt package
// verbs, so %s accepts a string and %d an integer. Lists and maps are
// pre-rendered as UB literals so an operator sees ['a', 'b'] instead of
// Go's space-separated [a b] form.
func fnFormat(f string, args ...any) (string, error) {
	rendered := make([]any, len(args))
	for i, a := range args {
		rendered[i] = renderForFormat(a)
	}
	return fmt.Sprintf(f, rendered...), nil
}

// renderForFormat returns lists and maps as UB literal strings so they
// print readably, and leaves primitives alone so type-specific verbs
// like %d still work.
func renderForFormat(v any) any {
	switch v.(type) {
	case []any, map[string]any:
		return lang.Render(v)
	}
	return v
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
// does: core.all([for r in var.replicas: r.port > 0]).
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
