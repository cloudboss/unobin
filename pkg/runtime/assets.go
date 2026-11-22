package runtime

import (
	"fmt"
	"slices"

	"github.com/cloudboss/unobin/pkg/asset"
	"github.com/cloudboss/unobin/pkg/diagnostic"
)

type assetResolver struct {
	cache            *asset.Cache
	catalog          *asset.Catalog
	materializePaths bool
}

func expressionAssetResolver(ctx *EvalContext) assetResolver {
	if ctx == nil {
		return assetResolver{}
	}
	return assetResolver{cache: ctx.AssetCache, catalog: ctx.Assets.Catalog()}
}

func resolveAssetValue(cache *asset.Cache, value any) (any, error) {
	return (assetResolver{cache: cache, materializePaths: true}).resolve(value)
}

func (r assetResolver) resolve(value any) (any, error) {
	switch typed := value.(type) {
	case []any:
		resolved := make([]any, len(typed))
		for i, element := range typed {
			next, err := r.resolve(element)
			if err != nil {
				return nil, diagnostic.Context(fmt.Sprintf("index %d", i), err)
			}
			resolved[i] = next
		}
		return resolved, nil
	case map[string]any:
		resolved := make(map[string]any, len(typed))
		for key, element := range typed {
			next, err := r.resolve(element)
			if err != nil {
				return nil, diagnostic.Context(fmt.Sprintf("key %q", key), err)
			}
			resolved[key] = next
		}
		return resolved, nil
	default:
		return r.resolveLeaf(value, true)
	}
}

func (r assetResolver) resolveLeaf(value any, allowStoredReferences bool) (any, error) {
	switch typed := value.(type) {
	case asset.PathRef:
		return r.resolveReference(string(typed), asset.ReferenceKindPath)
	case asset.ContentRef:
		return r.resolveReference(string(typed), asset.ReferenceKindContent)
	case string:
		if allowStoredReferences {
			if ref, ok := asset.ParseReference(typed); ok {
				return r.resolveReference(typed, ref.Kind)
			}
		}
	case []byte:
		if r.materializePaths {
			return slices.Clone(typed), nil
		}
	}
	return value, nil
}

func (r assetResolver) resolveReference(token string, kind asset.ReferenceKind) (any, error) {
	if !r.materializePaths && kind == asset.ReferenceKindPath {
		return token, nil
	}
	if r.cache != nil {
		return r.cache.Resolve(token)
	}
	if r.materializePaths {
		return nil, fmt.Errorf(
			"asset %s: cache is not configured",
			asset.DisplayReference(token),
		)
	}
	if r.catalog == nil {
		return nil, fmt.Errorf("asset content: no asset catalog is available")
	}
	return r.catalog.Content(token)
}

func (r assetResolver) resolveArgs(args []any, context string) error {
	for i, value := range args {
		resolved, err := r.resolve(value)
		if err != nil {
			return diagnostic.Context(fmt.Sprintf("%s arg %d", context, i), err)
		}
		args[i] = resolved
	}
	return nil
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
	return decodeLibraryConfigWith(lib, raw, e.resolveAssetMap, e.AssetCache)
}
