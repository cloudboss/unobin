package runtime

import (
	"fmt"
	"slices"

	"github.com/cloudboss/unobin/pkg/asset"
	"github.com/cloudboss/unobin/pkg/diagnostic"
)

func resolveAssetValue(cache *asset.Cache, value any) (any, error) {
	switch typed := value.(type) {
	case asset.PathRef:
		return resolveAssetReference(cache, string(typed))
	case asset.ContentRef:
		return resolveAssetReference(cache, string(typed))
	case string:
		if _, ok := asset.ParseReference(typed); !ok {
			return typed, nil
		}
		return resolveAssetReference(cache, typed)
	case []byte:
		return slices.Clone(typed), nil
	case []any:
		resolved := make([]any, len(typed))
		for i, element := range typed {
			next, err := resolveAssetValue(cache, element)
			if err != nil {
				return nil, diagnostic.Context(fmt.Sprintf("index %d", i), err)
			}
			resolved[i] = next
		}
		return resolved, nil
	case map[string]any:
		resolved := make(map[string]any, len(typed))
		for key, element := range typed {
			next, err := resolveAssetValue(cache, element)
			if err != nil {
				return nil, diagnostic.Context(fmt.Sprintf("key %q", key), err)
			}
			resolved[key] = next
		}
		return resolved, nil
	default:
		return value, nil
	}
}

func resolveAssetReference(cache *asset.Cache, token string) (any, error) {
	if cache == nil {
		return nil, fmt.Errorf(
			"asset %s: cache is not configured",
			asset.DisplayReference(token),
		)
	}
	return cache.Resolve(token)
}

func (e *Executor) decodeInputs(dst any, logical map[string]any) error {
	inputs, err := e.resolveAssetMap(logical)
	if err != nil {
		return err
	}
	return Decode(dst, inputs)
}

func (e *Executor) resolveAssetMap(logical map[string]any) (map[string]any, error) {
	resolved, err := resolveAssetValue(e.AssetCache, logical)
	if err != nil {
		return nil, err
	}
	inputs, ok := resolved.(map[string]any)
	if !ok && resolved != nil {
		return nil, fmt.Errorf("resolved value is %T, not an object", resolved)
	}
	return inputs, nil
}

func (e *Executor) decodeLibraryConfig(lib *Library, raw map[string]any) (any, error) {
	return decodeLibraryConfigWith(lib, raw, e.resolveAssetMap)
}
