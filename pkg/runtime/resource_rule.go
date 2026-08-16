package runtime

import (
	"fmt"
	"reflect"
)

type inputRuleCompare[In any] func(
	field fieldMetadata[In],
	prior, desired In,
) (equal, known bool, err error)

// InputRule gives one input field custom semantic equality.
type InputRule[In any] struct {
	field   AnyInputField[In]
	compare inputRuleCompare[In]
}

type replacementRuleKind uint8

const (
	replacementRuleInvalid replacementRuleKind = iota
	replacementRuleChanged
	replacementRuleConditional
)

type inputReplacementPredicate[In any] func(
	field fieldMetadata[In],
	prior, desired In,
) (replace, known bool, err error)

// ReplacementRule selects replacement from a change to one input field.
type ReplacementRule[In any] struct {
	field     AnyInputField[In]
	kind      replacementRuleKind
	predicate inputReplacementPredicate[In]
}

type driftRuleCompare[Out any] func(
	field fieldMetadata[Out],
	recorded, observed Out,
) (equal, known bool, err error)

// DriftRule selects replacement from a change to one observed output field.
type DriftRule[Out any] struct {
	field   AnyOutputField[Out]
	compare driftRuleCompare[Out]
}

type resolvedInputRule[In any] struct {
	field   fieldMetadata[In]
	compare inputRuleCompare[In]
}

type resolvedReplacementRule[In any] struct {
	field     fieldMetadata[In]
	kind      replacementRuleKind
	predicate inputReplacementPredicate[In]
}

type resolvedDriftRule[Out any] struct {
	field   fieldMetadata[Out]
	compare driftRuleCompare[Out]
}

// EqualBy declares semantic equality for one input field.
func EqualBy[In, Value any](
	field InputDescriptor[In, Value],
	equal func(Value, Value) bool,
) InputRule[In] {
	rule := InputRule[In]{field: field}
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

// ReplaceWhenChanged replaces a resource whenever one input field changes.
func ReplaceWhenChanged[In, Value any](
	field InputDescriptor[In, Value],
) ReplacementRule[In] {
	return ReplacementRule[In]{
		field: field,
		kind:  replacementRuleChanged,
	}
}

// ReplaceWhen conditionally replaces a resource after one input field changes.
func ReplaceWhen[In, Value any](
	field InputDescriptor[In, Value],
	predicate func(prior, desired Value) bool,
) ReplacementRule[In] {
	rule := ReplacementRule[In]{
		field: field,
		kind:  replacementRuleConditional,
	}
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

// ReplaceOnDrift replaces a resource when one recorded and observed output differs.
func ReplaceOnDrift[Out, Value any](
	field OutputDescriptor[Out, Value],
	equal func(recorded, observed Value) bool,
) DriftRule[Out] {
	rule := DriftRule[Out]{field: field}
	if equal == nil {
		return rule
	}
	rule.compare = func(
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
			"comparing drift output field "+metadata.path,
			false,
			func() (bool, error) {
				return equal(recordedValue, observedValue), nil
			},
		)
		return result, true, err
	}
	return rule
}

func (r resolvedInputRule[In]) equivalent(prior, desired In) (bool, bool, error) {
	if r.compare == nil {
		return false, false, fmt.Errorf("equality callback is nil")
	}
	return r.compare(r.field, prior, desired)
}

func (r resolvedReplacementRule[In]) matches(
	prior, desired In,
	equivalent bool,
) (bool, bool, error) {
	if equivalent {
		return false, true, nil
	}
	switch r.kind {
	case replacementRuleChanged:
		return true, true, nil
	case replacementRuleConditional:
		if r.predicate == nil {
			return false, false, fmt.Errorf("replacement callback is nil")
		}
		return r.predicate(r.field, prior, desired)
	default:
		return false, false, fmt.Errorf("replacement rule is invalid")
	}
}

func (r resolvedDriftRule[Out]) matches(recorded, observed Out) (bool, bool, error) {
	if r.compare == nil {
		return false, false, fmt.Errorf("drift equality callback is nil")
	}
	equal, known, err := r.compare(r.field, recorded, observed)
	if err != nil || !known {
		return false, known, err
	}
	return !equal, true, nil
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
