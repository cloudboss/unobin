package runtime

import (
	"context"
	"fmt"
	"reflect"
	"slices"
)

// ResourceDefinition declares a resource's schema, input semantics, identity,
// and replacement rules separately from its provider lifecycle methods.
type ResourceDefinition[In, Out, Config any] struct {
	SchemaVersion  int
	Migrate        ResourceMigrationFunc
	Validate       ResourceValidateFunc[In, Config]
	InputSemantics InputSemantics[In]
	Identity       ResourceIdentity[In, Out]
	Replacement    ReplacementRules[In, Out]
}

// ResourceValidateFunc validates concrete resource inputs and configuration.
type ResourceValidateFunc[In, Config any] func(context.Context, In, Config) error

// ResourceMigrationState contains the encoded resource values presented to a migration.
type ResourceMigrationState struct {
	Inputs  EncodedValue
	Outputs EncodedValue
}

// ResourceMigrationFunc migrates one older resource schema version.
type ResourceMigrationFunc func(
	oldVersion int,
	prior ResourceMigrationState,
) (ResourceMigrationState, error)

// InputSemantics contains the typed equality rules for resource inputs.
type InputSemantics[In any] struct {
	Rules []InputRule[In]
}

// ResourceIdentity declares how the runtime locates and identifies a resource.
type ResourceIdentity[In, Out any] struct {
	Version       int
	Scope         IdentityScope
	AddressInputs []AnyInputField[In]
	StableID      func(In, Out) (string, error)
	Migrate       IdentityMigrationFunc
}

// IdentityMigrationFunc migrates an identity derived from older resource values.
type IdentityMigrationFunc func(
	oldVersion int,
	resource ResourceMigrationState,
	priorStableID *string,
) (*string, error)

// IdentityScope states whether configuration is part of a resource's logical address.
type IdentityScope string

const (
	// IdentityConfiguration makes the selected library configuration part of identity.
	IdentityConfiguration IdentityScope = "configuration"
	// IdentityGlobal permits a resource identity to remain valid across configurations.
	IdentityGlobal IdentityScope = "global"
)

// ReplacementRules contains input-change and remote-drift replacement rules.
type ReplacementRules[In, Out any] struct {
	Inputs []ReplacementRule[In]
	Drift  []DriftRule[Out]
}

type resolvedResourceDefinition[In, Out, Config any] struct {
	schemaVersion     int
	migrate           ResourceMigrationFunc
	validate          ResourceValidateFunc[In, Config]
	inputRules        []resolvedInputRule[In]
	addressInputs     []fieldMetadata[In]
	identityVersion   int
	identityScope     IdentityScope
	stableID          func(In, Out) (string, error)
	identityMigrate   IdentityMigrationFunc
	replacementInputs []resolvedReplacementRule[In]
	driftRules        []resolvedDriftRule[Out]
}

func resolveResourceDefinition[In, Out, Config any](
	definition ResourceDefinition[In, Out, Config],
) (resolvedResourceDefinition[In, Out, Config], error) {
	var zero resolvedResourceDefinition[In, Out, Config]
	if err := validateResourceDefinitionRoots[In, Out](); err != nil {
		return zero, err
	}
	if definition.SchemaVersion < 1 {
		return zero, fmt.Errorf("resource schema version must be greater than zero")
	}
	if definition.Identity.Version < 1 {
		return zero, fmt.Errorf("resource identity version must be greater than zero")
	}
	switch definition.Identity.Scope {
	case IdentityConfiguration, IdentityGlobal:
	default:
		return zero, fmt.Errorf("invalid identity scope %q", definition.Identity.Scope)
	}

	addressInputs, err := resolveAddressInputs(definition.Identity.AddressInputs)
	if err != nil {
		return zero, err
	}
	inputRules, err := resolveInputRules(definition.InputSemantics.Rules)
	if err != nil {
		return zero, err
	}
	replacementInputs, err := resolveInputReplacementRules(definition.Replacement.Inputs)
	if err != nil {
		return zero, err
	}
	driftRules, err := resolveDriftRules(definition.Replacement.Drift)
	if err != nil {
		return zero, err
	}
	if len(driftRules) > 0 && definition.Identity.StableID == nil {
		return zero, fmt.Errorf("drift replacement requires a stable ID")
	}
	if err := validateResourceRuleOverlaps(
		addressInputs,
		inputRules,
		replacementInputs,
	); err != nil {
		return zero, err
	}

	return resolvedResourceDefinition[In, Out, Config]{
		schemaVersion:     definition.SchemaVersion,
		migrate:           definition.Migrate,
		validate:          definition.Validate,
		inputRules:        inputRules,
		addressInputs:     addressInputs,
		identityVersion:   definition.Identity.Version,
		identityScope:     definition.Identity.Scope,
		stableID:          definition.Identity.StableID,
		identityMigrate:   definition.Identity.Migrate,
		replacementInputs: replacementInputs,
		driftRules:        driftRules,
	}, nil
}

func validateResourceDefinitionRoots[In, Out any]() error {
	inputType := reflect.TypeFor[In]()
	if inputType.Kind() != reflect.Struct {
		return fmt.Errorf("input root must be a struct; got %s", inputType)
	}
	outputType := reflect.TypeFor[Out]()
	if outputType.Kind() != reflect.Pointer || outputType.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("output root must be a pointer to a struct; got %s", outputType)
	}
	return nil
}

func resolveAddressInputs[In any](
	fields []AnyInputField[In],
) ([]fieldMetadata[In], error) {
	resolved := make([]fieldMetadata[In], 0, len(fields))
	seen := make(map[string]bool, len(fields))
	for i, field := range fields {
		metadata, err := resolveInputField(field)
		if err != nil {
			return nil, fmt.Errorf("address input %d: %w", i, err)
		}
		if seen[metadata.path] {
			return nil, fmt.Errorf("duplicate address input %q", metadata.path)
		}
		seen[metadata.path] = true
		resolved = append(resolved, metadata)
	}
	return resolved, nil
}

func resolveInputRules[In any](rules []InputRule[In]) ([]resolvedInputRule[In], error) {
	resolved := make([]resolvedInputRule[In], 0, len(rules))
	seen := make(map[string]bool, len(rules))
	for i, rule := range rules {
		if rule.compare == nil {
			return nil, fmt.Errorf("input rule %d: equality callback is nil", i)
		}
		metadata, err := resolveInputField(rule.field)
		if err != nil {
			return nil, fmt.Errorf("input rule %d: %w", i, err)
		}
		if seen[metadata.path] {
			return nil, fmt.Errorf("duplicate equality rule for %q", metadata.path)
		}
		seen[metadata.path] = true
		resolved = append(resolved, resolvedInputRule[In]{
			field:   metadata,
			compare: rule.compare,
		})
	}
	return resolved, nil
}

func resolveInputReplacementRules[In any](
	rules []ReplacementRule[In],
) ([]resolvedReplacementRule[In], error) {
	resolved := make([]resolvedReplacementRule[In], 0, len(rules))
	seen := make(map[string]bool, len(rules))
	for i, rule := range rules {
		switch rule.kind {
		case replacementRuleChanged:
		case replacementRuleConditional:
			if rule.predicate == nil {
				return nil, fmt.Errorf("input replacement rule %d: replacement callback is nil", i)
			}
		default:
			return nil, fmt.Errorf("input replacement rule %d is invalid", i)
		}
		metadata, err := resolveInputField(rule.field)
		if err != nil {
			return nil, fmt.Errorf("input replacement rule %d: %w", i, err)
		}
		if seen[metadata.path] {
			return nil, fmt.Errorf("duplicate input replacement rule for %q", metadata.path)
		}
		seen[metadata.path] = true
		resolved = append(resolved, resolvedReplacementRule[In]{
			field:     metadata,
			kind:      rule.kind,
			predicate: rule.predicate,
		})
	}
	return resolved, nil
}

func resolveDriftRules[Out any](rules []DriftRule[Out]) ([]resolvedDriftRule[Out], error) {
	resolved := make([]resolvedDriftRule[Out], 0, len(rules))
	seen := make(map[string]bool, len(rules))
	for i, rule := range rules {
		if rule.compare == nil {
			return nil, fmt.Errorf("drift rule %d: drift equality callback is nil", i)
		}
		metadata, err := resolveOutputField(rule.field)
		if err != nil {
			return nil, fmt.Errorf("drift rule %d: %w", i, err)
		}
		if seen[metadata.path] {
			return nil, fmt.Errorf("duplicate drift rule for %q", metadata.path)
		}
		seen[metadata.path] = true
		resolved = append(resolved, resolvedDriftRule[Out]{
			field:   metadata,
			compare: rule.compare,
		})
	}
	return resolved, nil
}

func validateResourceRuleOverlaps[In any](
	addressInputs []fieldMetadata[In],
	inputRules []resolvedInputRule[In],
	replacementRules []resolvedReplacementRule[In],
) error {
	for i, rule := range inputRules {
		for _, other := range inputRules[:i] {
			if fieldMetadataOverlap(rule.field, other.field) {
				return fmt.Errorf(
					"equality rules overlap at %q and %q",
					other.field.path,
					rule.field.path,
				)
			}
		}
		for _, address := range addressInputs {
			if fieldMetadataOverlap(rule.field, address) {
				return fmt.Errorf(
					"equality rule %q overlaps address input %q",
					rule.field.path,
					address.path,
				)
			}
		}
	}

	for _, rule := range replacementRules {
		for _, address := range addressInputs {
			if fieldMetadataOverlap(rule.field, address) {
				return fmt.Errorf(
					"input replacement rule %q overlaps address input %q",
					rule.field.path,
					address.path,
				)
			}
		}
		for _, equality := range inputRules {
			if sameFieldMetadata(rule.field, equality.field) {
				continue
			}
			if fieldMetadataOverlap(rule.field, equality.field) {
				return fmt.Errorf(
					"equality rule %q overlaps input replacement rule %q",
					equality.field.path,
					rule.field.path,
				)
			}
		}
	}
	return nil
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
