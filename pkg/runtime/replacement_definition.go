package runtime

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sort"

	"github.com/cloudboss/unobin/pkg/encoding/ub"
)

// ResourceDefinition declares a resource's schema and lifecycle decisions.
type ResourceDefinition[In, Out, Config any] struct {
	SchemaVersion int
	Migrate       ResourceMigrationFunc
	Validate      ResourceValidateFunc[In, Config]

	Equality []InputEqualityRule[In]
	Replace  Replacement[In, Out, Config]
	StableID func(In, Out) (string, error)
}

// ResourceMigrationFunc migrates one older resource schema version.
type ResourceMigrationFunc func(
	oldVersion int,
	prior MigrationState,
) (MigrationState, error)

// ResourceValidateFunc validates concrete resource inputs and configuration.
type ResourceValidateFunc[In, Config any] func(context.Context, In, Config) error

// Replacement declares the changes that require resource recreation.
type Replacement[In, Out, Config any] struct {
	Fields              []AnyInputField[In]
	Rules               []ReplacementRule[In]
	ConfigurationFields []AnyConfigurationField[Config]
	Drift               []DriftRule[Out]
}

type resolvedResourceDefinition[In, Out, Config any] struct {
	schemaVersion       int
	migrate             ResourceMigrationFunc
	validate            ResourceValidateFunc[In, Config]
	equality            []resolvedInputEqualityRule[In]
	replacementFields   []fieldMetadata[In]
	replacementRules    []resolvedReplacementRule[In]
	configurationFields []fieldMetadata[Config]
	driftRules          []resolvedDriftRule[Out]
	stableID            func(In, Out) (string, error)
}

func resolveResourceDefinition[In, Out, Config any](
	definition ResourceDefinition[In, Out, Config],
) (resolvedResourceDefinition[In, Out, Config], error) {
	var zero resolvedResourceDefinition[In, Out, Config]
	if err := validateReplacementRoots[In, Out](); err != nil {
		return zero, err
	}
	if definition.SchemaVersion < 1 {
		return zero, fmt.Errorf("resource schema version must be greater than zero")
	}

	equality, err := resolveInputEqualityRules(definition.Equality)
	if err != nil {
		return zero, err
	}
	fields, err := resolveReplacementFields(definition.Replace.Fields)
	if err != nil {
		return zero, err
	}
	rules, err := resolveReplacementRules(definition.Replace.Rules)
	if err != nil {
		return zero, err
	}
	configurationFields, err := resolveConfigurationFields(
		definition.Replace.ConfigurationFields,
	)
	if err != nil {
		return zero, err
	}
	drift, err := resolveDriftRules(definition.Replace.Drift)
	if err != nil {
		return zero, err
	}
	if len(drift) > 0 && definition.StableID == nil {
		return zero, fmt.Errorf("drift replacement requires a stable ID")
	}
	if err := validateInputRuleOverlaps(equality, fields, rules); err != nil {
		return zero, err
	}

	return resolvedResourceDefinition[In, Out, Config]{
		schemaVersion:       definition.SchemaVersion,
		migrate:             definition.Migrate,
		validate:            definition.Validate,
		equality:            equality,
		replacementFields:   fields,
		replacementRules:    rules,
		configurationFields: configurationFields,
		driftRules:          drift,
		stableID:            definition.StableID,
	}, nil
}

func validateReplacementRoots[In, Out any]() error {
	inputType := reflect.TypeFor[In]()
	if inputType.Kind() != reflect.Struct {
		return fmt.Errorf("resource inputs must be a struct; got %s", inputType)
	}
	outputType := reflect.TypeFor[Out]()
	if outputType.Kind() != reflect.Pointer || outputType.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("resource outputs must be a pointer to a struct; got %s", outputType)
	}
	return nil
}

func resolveInputEqualityRules[In any](
	rules []InputEqualityRule[In],
) ([]resolvedInputEqualityRule[In], error) {
	resolved := make([]resolvedInputEqualityRule[In], 0, len(rules))
	seen := make(map[string]bool, len(rules))
	for i, rule := range rules {
		if rule.compare == nil {
			return nil, fmt.Errorf("equality rule %d: equality callback is nil", i)
		}
		metadata, err := resolveInputField(rule.field)
		if err != nil {
			return nil, fmt.Errorf("equality rule %d: %w", i, err)
		}
		if seen[metadata.path] {
			return nil, fmt.Errorf("duplicate equality rule for %q", metadata.path)
		}
		seen[metadata.path] = true
		resolved = append(resolved, resolvedInputEqualityRule[In]{
			field:   metadata,
			compare: rule.compare,
		})
	}
	sort.Slice(resolved, func(i, j int) bool {
		return resolved[i].field.path < resolved[j].field.path
	})
	return resolved, nil
}

func resolveReplacementFields[In any](
	fields []AnyInputField[In],
) ([]fieldMetadata[In], error) {
	resolved := make([]fieldMetadata[In], 0, len(fields))
	seen := make(map[string]bool, len(fields))
	for i, field := range fields {
		metadata, err := resolveInputField(field)
		if err != nil {
			return nil, fmt.Errorf("replacement field %d: %w", i, err)
		}
		if seen[metadata.path] {
			return nil, fmt.Errorf("duplicate replacement field %q", metadata.path)
		}
		for _, other := range resolved {
			if fieldMetadataOverlap(other, metadata) {
				return nil, fmt.Errorf(
					"replacement fields overlap at %q and %q",
					other.path,
					metadata.path,
				)
			}
		}
		seen[metadata.path] = true
		resolved = append(resolved, metadata)
	}
	sortFieldMetadata(resolved)
	return resolved, nil
}

func resolveReplacementRules[In any](
	rules []ReplacementRule[In],
) ([]resolvedReplacementRule[In], error) {
	resolved := make([]resolvedReplacementRule[In], 0, len(rules))
	seen := make(map[string]bool, len(rules))
	for i, rule := range rules {
		if rule.predicate == nil {
			return nil, fmt.Errorf("replacement rule %d: replacement callback is nil", i)
		}
		metadata, err := resolveInputField(rule.field)
		if err != nil {
			return nil, fmt.Errorf("replacement rule %d: %w", i, err)
		}
		if seen[metadata.path] {
			return nil, fmt.Errorf("duplicate replacement rule for %q", metadata.path)
		}
		for _, other := range resolved {
			if fieldMetadataOverlap(other.field, metadata) {
				return nil, fmt.Errorf(
					"replacement rules overlap at %q and %q",
					other.field.path,
					metadata.path,
				)
			}
		}
		seen[metadata.path] = true
		resolved = append(resolved, resolvedReplacementRule[In]{
			field:     metadata,
			predicate: rule.predicate,
		})
	}
	sort.Slice(resolved, func(i, j int) bool {
		return resolved[i].field.path < resolved[j].field.path
	})
	return resolved, nil
}

func resolveConfigurationFields[Config any](
	fields []AnyConfigurationField[Config],
) ([]fieldMetadata[Config], error) {
	resolved := make([]fieldMetadata[Config], 0, len(fields))
	seen := make(map[string]bool, len(fields))
	for i, field := range fields {
		metadata, err := resolveConfigurationField(field)
		if err != nil {
			return nil, fmt.Errorf("configuration replacement field %d: %w", i, err)
		}
		if seen[metadata.path] {
			return nil, fmt.Errorf(
				"duplicate configuration replacement field %q",
				metadata.path,
			)
		}
		for _, other := range resolved {
			if fieldMetadataOverlap(other, metadata) {
				return nil, fmt.Errorf(
					"configuration replacement fields overlap at %q and %q",
					other.path,
					metadata.path,
				)
			}
		}
		seen[metadata.path] = true
		resolved = append(resolved, metadata)
	}
	sortFieldMetadata(resolved)
	return resolved, nil
}

func resolveDriftRules[Out any](
	rules []DriftRule[Out],
) ([]resolvedDriftRule[Out], error) {
	resolved := make([]resolvedDriftRule[Out], 0, len(rules))
	seen := make(map[string]bool, len(rules))
	for i, rule := range rules {
		if rule.predicate == nil {
			return nil, fmt.Errorf("drift rule %d: drift callback is nil", i)
		}
		metadata, err := resolveOutputField(rule.field)
		if err != nil {
			return nil, fmt.Errorf("drift rule %d: %w", i, err)
		}
		if seen[metadata.path] {
			return nil, fmt.Errorf("duplicate drift rule for %q", metadata.path)
		}
		for _, other := range resolved {
			if fieldMetadataOverlap(other.field, metadata) {
				return nil, fmt.Errorf(
					"drift rules overlap at %q and %q",
					other.field.path,
					metadata.path,
				)
			}
		}
		seen[metadata.path] = true
		resolved = append(resolved, resolvedDriftRule[Out]{
			field:     metadata,
			predicate: rule.predicate,
		})
	}
	sort.Slice(resolved, func(i, j int) bool {
		return resolved[i].field.path < resolved[j].field.path
	})
	return resolved, nil
}

func validateInputRuleOverlaps[In any](
	equality []resolvedInputEqualityRule[In],
	fields []fieldMetadata[In],
	rules []resolvedReplacementRule[In],
) error {
	for i, rule := range equality {
		for _, other := range equality[:i] {
			if fieldMetadataOverlap(rule.field, other.field) {
				return fmt.Errorf(
					"equality rules overlap at %q and %q",
					other.field.path,
					rule.field.path,
				)
			}
		}
		for _, field := range fields {
			if sameFieldMetadata(rule.field, field) {
				continue
			}
			if fieldMetadataOverlap(rule.field, field) {
				return fmt.Errorf(
					"equality rule %q overlaps replacement field %q",
					rule.field.path,
					field.path,
				)
			}
		}
		for _, replacement := range rules {
			if sameFieldMetadata(rule.field, replacement.field) {
				continue
			}
			if fieldMetadataOverlap(rule.field, replacement.field) {
				return fmt.Errorf(
					"equality rule %q overlaps replacement rule %q",
					rule.field.path,
					replacement.field.path,
				)
			}
		}
	}

	for _, rule := range rules {
		for _, field := range fields {
			if fieldMetadataOverlap(rule.field, field) {
				return fmt.Errorf(
					"replacement rule %q overlaps replacement field %q",
					rule.field.path,
					field.path,
				)
			}
		}
	}
	return nil
}

func sortFieldMetadata[Root any](fields []fieldMetadata[Root]) {
	sort.Slice(fields, func(i, j int) bool {
		return fields[i].path < fields[j].path
	})
}

func fieldMetadataOverlap[A, B any](a fieldMetadata[A], b fieldMetadata[B]) bool {
	return indexPrefix(a.index, b.index) || indexPrefix(b.index, a.index)
}

func sameFieldMetadata[A, B any](a fieldMetadata[A], b fieldMetadata[B]) bool {
	return slices.Equal(a.index, b.index)
}

func indexPrefix(prefix, value []int) bool {
	return len(prefix) <= len(value) && slices.Equal(prefix, value[:len(prefix)])
}

func (definition resolvedResourceDefinition[In, Out, Config]) replacementReasons(
	priorInputs, desiredInputs In,
	priorConfig, desiredConfig Config,
	recordedOutputs, observedOutputs Out,
) ([]string, error) {
	reasons := map[string]bool{}
	for _, field := range definition.replacementFields {
		equal, err := definition.inputFieldEqual(field, priorInputs, desiredInputs)
		if err != nil {
			return nil, err
		}
		if !equal {
			reasons[field.path] = true
		}
	}
	for _, rule := range definition.replacementRules {
		equal, err := definition.inputFieldEqual(rule.field, priorInputs, desiredInputs)
		if err != nil {
			return nil, err
		}
		if equal {
			continue
		}
		replace, known, err := rule.matches(priorInputs, desiredInputs)
		if err != nil {
			return nil, err
		}
		if !known {
			return nil, fmt.Errorf(
				"cannot evaluate replacement rule for input field %q",
				rule.field.path,
			)
		}
		if replace {
			reasons[rule.field.path] = true
		}
	}
	for _, field := range definition.configurationFields {
		if !selectedFieldsEqual(priorConfig, desiredConfig, field.index) {
			reasons[field.path] = true
		}
	}
	for _, rule := range definition.driftRules {
		if selectedFieldsEqual(recordedOutputs, observedOutputs, rule.field.index) {
			continue
		}
		replace, known, err := rule.matches(recordedOutputs, observedOutputs)
		if err != nil {
			return nil, err
		}
		if !known {
			return nil, fmt.Errorf(
				"cannot evaluate drift rule for output field %q",
				rule.field.path,
			)
		}
		if replace {
			reasons[rule.field.path] = true
		}
	}

	result := make([]string, 0, len(reasons))
	for reason := range reasons {
		result = append(result, reason)
	}
	if len(result) == 0 {
		return nil, nil
	}
	sort.Strings(result)
	return result, nil
}

func (definition resolvedResourceDefinition[In, Out, Config]) inputFieldEqual(
	field fieldMetadata[In],
	prior, desired In,
) (bool, error) {
	for _, rule := range definition.equality {
		if !sameFieldMetadata(field, rule.field) {
			continue
		}
		equal, known, err := rule.equivalent(prior, desired)
		if err != nil {
			return false, err
		}
		if known {
			return equal, nil
		}
		break
	}
	return selectedFieldsEqual(prior, desired, field.index), nil
}

func selectedFieldsEqual[Root any](prior, desired Root, index []int) bool {
	priorValue, priorKnown := selectedReflectValue(reflect.ValueOf(prior), index)
	desiredValue, desiredKnown := selectedReflectValue(reflect.ValueOf(desired), index)
	if !priorKnown || !desiredKnown {
		return priorKnown == desiredKnown
	}
	return reflect.DeepEqual(priorValue.Interface(), desiredValue.Interface())
}

func selectedReflectValue(value reflect.Value, index []int) (reflect.Value, bool) {
	value, ok := dereferenceSelectedRoot(value)
	if !ok {
		return reflect.Value{}, false
	}
	for i, fieldIndex := range index {
		if value.Kind() != reflect.Struct || fieldIndex >= value.NumField() {
			return reflect.Value{}, false
		}
		value = value.Field(fieldIndex)
		if i != len(index)-1 {
			value, ok = dereferenceSelectedRoot(value)
			if !ok {
				return reflect.Value{}, false
			}
		}
	}
	if !value.IsValid() || !value.CanInterface() {
		return reflect.Value{}, false
	}
	return value, true
}

func (definition resolvedResourceDefinition[In, Out, Config]) inputsEqual(
	prior, desired In,
) (bool, error) {
	return definition.inputValuesEqual(
		prior,
		desired,
		reflect.ValueOf(prior),
		reflect.ValueOf(desired),
		nil,
	)
}

func (definition resolvedResourceDefinition[In, Out, Config]) inputValuesEqual(
	priorRoot, desiredRoot In,
	priorValue, desiredValue reflect.Value,
	index []int,
) (bool, error) {
	for _, rule := range definition.equality {
		if slices.Equal(index, rule.field.index) {
			equal, known, err := rule.equivalent(priorRoot, desiredRoot)
			if err != nil {
				return false, err
			}
			if known {
				return equal, nil
			}
			return selectedFieldsEqual(priorRoot, desiredRoot, index), nil
		}
	}

	priorValue, priorKnown := dereferenceSelectedRoot(priorValue)
	desiredValue, desiredKnown := dereferenceSelectedRoot(desiredValue)
	if !priorKnown || !desiredKnown {
		return priorKnown == desiredKnown, nil
	}
	if priorValue.Kind() != reflect.Struct || desiredValue.Kind() != reflect.Struct {
		return reflect.DeepEqual(priorValue.Interface(), desiredValue.Interface()), nil
	}

	t := priorValue.Type()
	for i := range t.NumField() {
		field := t.Field(i)
		tag := ub.ParseTag(field.Tag.Get("ub"))
		if !field.IsExported() || tag.Skip {
			continue
		}
		equal, err := definition.inputValuesEqual(
			priorRoot,
			desiredRoot,
			priorValue.Field(i),
			desiredValue.Field(i),
			appendFieldIndex(index, i),
		)
		if err != nil || !equal {
			return equal, err
		}
	}
	return true, nil
}
