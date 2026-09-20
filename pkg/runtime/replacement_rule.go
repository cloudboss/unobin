package runtime

import (
	"fmt"
	"reflect"
)

type inputEqualityCompare[In any] func(
	field fieldMetadata[In],
	prior, desired In,
) (equal, known bool, err error)

// InputEqualityRule gives one input field custom semantic equality.
type InputEqualityRule[In any] struct {
	field   AnyInputField[In]
	compare inputEqualityCompare[In]
}

type inputReplacementPredicate[In any] func(
	field fieldMetadata[In],
	prior, desired In,
) (replace, known bool, err error)

// ReplacementRule conditionally selects replacement from an input change.
type ReplacementRule[In any] struct {
	field     AnyInputField[In]
	predicate inputReplacementPredicate[In]
}

type driftReplacementPredicate[Out any] func(
	field fieldMetadata[Out],
	recorded, observed Out,
) (replace, known bool, err error)

// DriftRule conditionally selects replacement from observed output drift.
type DriftRule[Out any] struct {
	field     AnyOutputField[Out]
	predicate driftReplacementPredicate[Out]
}

type resolvedInputEqualityRule[In any] struct {
	field   fieldMetadata[In]
	compare inputEqualityCompare[In]
}

type resolvedReplacementRule[In any] struct {
	field     fieldMetadata[In]
	predicate inputReplacementPredicate[In]
}

type resolvedDriftRule[Out any] struct {
	field     fieldMetadata[Out]
	predicate driftReplacementPredicate[Out]
}

// EqualBy declares semantic equality for one input field.
func EqualBy[In, Value any](
	field InputDescriptor[In, Value],
	equal func(prior, desired Value) bool,
) InputEqualityRule[In] {
	rule := InputEqualityRule[In]{field: field}
	if equal == nil {
		return rule
	}
	rule.compare = func(
		metadata fieldMetadata[In],
		prior, desired In,
	) (bool, bool, error) {
		priorValue, priorKnown := selectedFieldValue[In, Value](prior, metadata.index)
		desiredValue, desiredKnown := selectedFieldValue[In, Value](desired, metadata.index)
		if !priorKnown || !desiredKnown {
			return false, false, nil
		}
		result, err := guard(
			"comparing input field "+metadata.path,
			false,
			func() (bool, error) {
				return equal(priorValue, desiredValue), nil
			},
		)
		return result, true, err
	}
	return rule
}

// ReplaceWhen conditionally replaces a resource after one input field changes.
func ReplaceWhen[In, Value any](
	field InputDescriptor[In, Value],
	predicate func(prior, desired Value) bool,
) ReplacementRule[In] {
	rule := ReplacementRule[In]{field: field}
	if predicate == nil {
		return rule
	}
	rule.predicate = func(
		metadata fieldMetadata[In],
		prior, desired In,
	) (bool, bool, error) {
		priorValue, priorKnown := selectedFieldValue[In, Value](prior, metadata.index)
		desiredValue, desiredKnown := selectedFieldValue[In, Value](desired, metadata.index)
		if !priorKnown || !desiredKnown {
			return false, false, nil
		}
		result, err := guard(
			"checking replacement for input field "+metadata.path,
			false,
			func() (bool, error) {
				return predicate(priorValue, desiredValue), nil
			},
		)
		return result, true, err
	}
	return rule
}

// ReplaceOnDrift conditionally replaces a resource after one output field changes.
func ReplaceOnDrift[Out, Value any](
	field OutputDescriptor[Out, Value],
	predicate func(recorded, observed Value) bool,
) DriftRule[Out] {
	rule := DriftRule[Out]{field: field}
	if predicate == nil {
		return rule
	}
	rule.predicate = func(
		metadata fieldMetadata[Out],
		recorded, observed Out,
	) (bool, bool, error) {
		recordedValue, recordedKnown := selectedFieldValue[Out, Value](
			recorded,
			metadata.index,
		)
		observedValue, observedKnown := selectedFieldValue[Out, Value](
			observed,
			metadata.index,
		)
		if !recordedKnown || !observedKnown {
			return false, false, nil
		}
		result, err := guard(
			"checking replacement for drift field "+metadata.path,
			false,
			func() (bool, error) {
				return predicate(recordedValue, observedValue), nil
			},
		)
		return result, true, err
	}
	return rule
}

func (r resolvedInputEqualityRule[In]) equivalent(
	prior, desired In,
) (bool, bool, error) {
	if r.compare == nil {
		return false, false, fmt.Errorf("equality callback is nil")
	}
	return r.compare(r.field, prior, desired)
}

func (r resolvedReplacementRule[In]) matches(
	prior, desired In,
) (bool, bool, error) {
	if r.predicate == nil {
		return false, false, fmt.Errorf("replacement callback is nil")
	}
	return r.predicate(r.field, prior, desired)
}

func (r resolvedDriftRule[Out]) matches(
	recorded, observed Out,
) (bool, bool, error) {
	if r.predicate == nil {
		return false, false, fmt.Errorf("drift callback is nil")
	}
	return r.predicate(r.field, recorded, observed)
}

func selectedFieldValue[Root, Value any](
	root Root,
	index []int,
) (Value, bool) {
	var zero Value
	current := reflect.ValueOf(root)
	current, ok := dereferenceSelectedRoot(current)
	if !ok {
		return zero, false
	}
	for i, fieldIndex := range index {
		if current.Kind() != reflect.Struct || fieldIndex >= current.NumField() {
			return zero, false
		}
		current = current.Field(fieldIndex)
		if i != len(index)-1 {
			current, ok = dereferenceSelectedRoot(current)
			if !ok {
				return zero, false
			}
		}
	}
	if !current.IsValid() || !current.CanInterface() {
		return zero, false
	}
	value, ok := current.Interface().(Value)
	if !ok {
		return zero, false
	}
	return value, true
}

func dereferenceSelectedRoot(value reflect.Value) (reflect.Value, bool) {
	for value.IsValid() && value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return reflect.Value{}, false
		}
		value = value.Elem()
	}
	return value, value.IsValid()
}
