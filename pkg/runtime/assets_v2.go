package runtime

import "github.com/cloudboss/unobin/pkg/asset"

func resolveEncodedAssets(cache *asset.Cache, value EncodedValue) (EncodedValue, error) {
	switch value.Kind() {
	case EncodedValueString:
		token, _ := value.String()
		if _, ok := asset.ParseReference(token); !ok {
			return value, nil
		}
		resolved, err := resolveAssetReference(cache, token)
		if err != nil {
			return EncodedValue{}, err
		}
		if content, ok := resolved.([]byte); ok {
			items := make([]EncodedValue, len(content))
			for i, b := range content {
				items[i] = IntegerValue(int64(b))
			}
			return ListValue(items)
		}
		return StringValue(resolved.(string)), nil
	case EncodedValueList:
		items, _ := value.Items()
		for i := range items {
			resolved, err := resolveEncodedAssets(cache, items[i])
			if err != nil {
				return EncodedValue{}, err
			}
			items[i] = resolved
		}
		return ListValue(items)
	case EncodedValueObject, EncodedValueMap:
		fields, _ := value.ObjectFields()
		if value.Kind() == EncodedValueMap {
			fields, _ = value.MapEntries()
		}
		for name, field := range fields {
			resolved, err := resolveEncodedAssets(cache, field)
			if err != nil {
				return EncodedValue{}, err
			}
			fields[name] = resolved
		}
		if value.Kind() == EncodedValueMap {
			return MapValue(fields)
		}
		return ObjectValue(fields)
	default:
		return value, nil
	}
}

func (e *Executor) factoryResourceV2(binding Binding) (
	*resourceDefinitionRegistration, *resolvedConfigurationDefinition, error,
) {
	registration, configuration, err := e.LibraryCatalog.resource(binding)
	if err != nil {
		return nil, configuration, err
	}
	if registration.forAssets != nil {
		registration = registration.forAssets(e.AssetCache)
	}
	scoped := *configuration
	scoped.assetCache = e.AssetCache
	return registration, &scoped, nil
}

func (e *Executor) factoryConfigurationV2(library *Library) (
	resolvedConfigurationDefinition, error,
) {
	definition, err := resolveLibraryConfigurationDefinition(library.LibraryPath, library)
	if err != nil {
		return definition, err
	}
	definition.assetCache = e.AssetCache
	return definition, nil
}
