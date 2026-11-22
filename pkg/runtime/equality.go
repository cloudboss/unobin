package runtime

import (
	"bytes"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/cloudboss/unobin/pkg/diagnostic"
)

func evalEq(a, b any) bool {
	equal, _ := equalValues(a, b, nil)
	return equal
}

func equalValues(a, b any, assets *assetResolver) (bool, error) {
	if a == nil || b == nil {
		return a == nil && b == nil, nil
	}
	if assets != nil {
		// A typed reference can be identified without a source. A string
		// can refer to a stored asset when a catalog or cache is available.
		allowStoredReferences := assets.catalog != nil || assets.cache != nil
		var err error
		a, err = assets.resolveLeaf(a, allowStoredReferences)
		if err != nil {
			return false, diagnostic.Context("left operand", err)
		}
		b, err = assets.resolveLeaf(b, allowStoredReferences)
		if err != nil {
			return false, diagnostic.Context("right operand", err)
		}
	}
	if c, ok := numericCmp(a, b); ok {
		return c == 0, nil
	}
	if content, ok := a.([]byte); ok {
		return equalBytes(content, b), nil
	}
	if content, ok := b.([]byte); ok {
		return equalBytes(content, a), nil
	}
	switch a := a.(type) {
	case []any:
		right, ok := b.([]any)
		if !ok || len(a) != len(right) {
			return false, nil
		}
		for i, item := range a {
			equal, err := equalValues(item, right[i], assets)
			if err != nil {
				return false, diagnostic.Context(fmt.Sprintf("index %d", i), err)
			}
			if !equal {
				return false, nil
			}
		}
		return true, nil
	case map[string]any:
		right, ok := b.(map[string]any)
		if !ok || len(a) != len(right) {
			return false, nil
		}
		return equalObjects(a, right, assets)
	default:
		return reflect.DeepEqual(a, b), nil
	}
}

func equalObjects(a, b map[string]any, assets *assetResolver) (bool, error) {
	for key := range a {
		if _, ok := b[key]; !ok {
			return false, nil
		}
	}
	for _, key := range slices.Sorted(maps.Keys(a)) {
		equal, err := equalValues(a[key], b[key], assets)
		if err != nil {
			return false, diagnostic.Context(fmt.Sprintf("key %q", key), err)
		}
		if !equal {
			return false, nil
		}
	}
	return true, nil
}

func equalBytes(content []byte, other any) bool {
	switch other := other.(type) {
	case []byte:
		return bytes.Equal(content, other)
	case []any:
		if len(content) != len(other) {
			return false
		}
		for i, value := range other {
			number, ok := value.(int64)
			if !ok || number != int64(content[i]) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
