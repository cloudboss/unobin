package runtime

import (
	"errors"
	"fmt"
	"math"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

func fnByteLength(value any) (int64, error) {
	if s, ok := value.(string); ok {
		return int64(len(s)), nil
	}
	bytes, err := constraintBytes("byte-length", value)
	return int64(len(bytes)), err
}

func fnRemainder(left, right any) (any, error) {
	if left == nil || right == nil {
		return nil, nil
	}
	value, valueOK := left.(int64)
	divisor, divisorOK := right.(int64)
	if !valueOK || !divisorOK {
		return nil, errors.New("remainder: arguments must be integers")
	}
	if divisor == 0 {
		return nil, errors.New("remainder: division by zero")
	}
	return value % divisor, nil
}

func remainderType(args []typecheck.Type) typecheck.Type {
	for _, arg := range args {
		switch arg.Kind {
		case typecheck.Null:
			return typecheck.TNull()
		case typecheck.Optional, typecheck.Unknown:
			return typecheck.TOptional(typecheck.TInteger())
		}
	}
	return typecheck.TInteger()
}

func fnLookup(collection, key any) (any, error) {
	if collection == nil {
		return nil, nil
	}
	var value any
	var err error
	switch key := key.(type) {
	case string:
		value, _, err = mapStep(collection, key, "lookup")
	case int64:
		value, _, err = indexElement(collection, key, "lookup")
	default:
		return nil, errors.New("lookup: key must be a string or integer")
	}
	if errors.Is(err, ErrEvalNotFound) {
		return nil, nil
	}
	return value, err
}

func lookupType(args []typecheck.Type) typecheck.Type {
	if len(args) != 0 {
		collection := args[0].Unwrap()
		if collection.Kind == typecheck.Null {
			return typecheck.TNull()
		}
		if collection.Elem != nil {
			return typecheck.TOptional(*collection.Elem)
		}
	}
	return typecheck.TUnknown()
}

func fnHasKey(collection any, key string) (bool, error) {
	if collection == nil {
		return false, nil
	}
	values, ok := collection.(map[string]any)
	if !ok {
		return false, fmt.Errorf("has-key: argument must be a map, got %s",
			lang.TypeMessage(collection))
	}
	_, present := values[key]
	return present, nil
}

func scalarList(name string, value any) ([]any, error) {
	list, err := optionalList(name, value)
	if err != nil {
		return nil, err
	}
	for i, item := range list {
		if !isConstraintScalar(item) {
			return nil, fmt.Errorf("%s: element %d must be a scalar, got %s",
				name, i, lang.TypeMessage(item))
		}
	}
	return list, nil
}

func optionalList(name string, value any) ([]any, error) {
	if value == nil {
		return nil, nil
	}
	list, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: argument must be a list, got %s", name, lang.TypeMessage(value))
	}
	return list, nil
}

func isConstraintScalar(value any) bool {
	switch value.(type) {
	case nil, bool, string, int64, float64:
		return true
	default:
		return false
	}
}

func fnHasItem(collection, value any) (bool, error) {
	list, err := scalarList("has-item", collection)
	if err != nil {
		return false, err
	}
	if !isConstraintScalar(value) {
		return false, errors.New("has-item: value must be a scalar")
	}
	for _, item := range list {
		if evalEq(item, value) {
			return true, nil
		}
	}
	return false, nil
}

func fnIsUnique(collection any) (bool, error) {
	list, err := scalarList("is-unique", collection)
	if err != nil {
		return false, err
	}
	seen := make(map[any]struct{}, len(list))
	integers := map[float64]struct{}{}
	nanSeen := false
	for _, item := range list {
		if _, exists := seen[item]; exists {
			return false, nil
		}
		switch number := item.(type) {
		case int64:
			promoted := float64(number)
			if _, exists := seen[promoted]; exists {
				return false, nil
			}
			integers[promoted] = struct{}{}
		case float64:
			if _, exists := integers[number]; exists || math.IsNaN(number) && nanSeen {
				return false, nil
			}
			nanSeen = nanSeen || math.IsNaN(number)
		}
		seen[item] = struct{}{}
	}
	return true, nil
}

func fnSum(collection any) (any, error) {
	list, err := optionalList("sum", collection)
	if err != nil {
		return nil, err
	}
	var approximate float64
	var fractional bool
	for i, item := range list {
		number, ok := numericFloat(item)
		if !ok {
			return nil, fmt.Errorf("sum: element %d must be numeric, got %s",
				i, lang.TypeMessage(item))
		}
		approximate += number
		if _, ok := item.(float64); ok {
			fractional = true
		}
	}
	if fractional {
		return approximate, nil
	}
	var total int64
	for _, item := range list {
		n := item.(int64)
		if n > 0 && total > math.MaxInt64-n || n < 0 && total < math.MinInt64-n {
			return nil, errors.New("sum: integer overflow")
		}
		total += n
	}
	return total, nil
}

func sumType(args []typecheck.Type) typecheck.Type {
	if len(args) != 0 {
		switch collection := args[0].Unwrap(); collection.Kind {
		case typecheck.List:
			if collection.Elem != nil && collection.Elem.Kind == typecheck.Integer {
				return typecheck.TInteger()
			}
		case typecheck.Tuple:
			for _, elem := range collection.Elems {
				if elem.Kind != typecheck.Integer {
					return typecheck.TNumber()
				}
			}
			return typecheck.TInteger()
		case typecheck.Null:
			return typecheck.TInteger()
		}
	}
	return typecheck.TNumber()
}
