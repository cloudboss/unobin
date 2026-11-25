package structdecode

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
)

func integerDecodeHook(from, to reflect.Value) (any, error) {
	data := from.Interface()
	kind := to.Kind()
	signed := kind >= reflect.Int && kind <= reflect.Int64
	unsigned := kind >= reflect.Uint && kind <= reflect.Uint64
	if !signed && !unsigned {
		return data, nil
	}
	if number, ok := data.(json.Number); ok {
		if signed {
			return strconv.ParseInt(string(number), 10, to.Type().Bits())
		}
		return strconv.ParseUint(string(number), 10, to.Type().Bits())
	}
	var overflow bool
	switch from.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n := from.Int()
		if signed {
			overflow = to.OverflowInt(n)
		} else {
			overflow = n < 0 || to.OverflowUint(uint64(n))
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n := from.Uint()
		if signed {
			overflow = n > math.MaxInt64 || to.OverflowInt(int64(n))
		} else {
			overflow = to.OverflowUint(n)
		}
	case reflect.Float32, reflect.Float64:
		n := from.Float()
		if math.IsNaN(n) || math.IsInf(n, 0) || math.Trunc(n) != n {
			return nil, fmt.Errorf("expected a finite integer for %s", to.Type())
		}
		bits := to.Type().Bits()
		lower, upper := 0.0, math.Exp2(float64(bits))
		if signed {
			upper = math.Exp2(float64(bits - 1))
			lower = -upper
		}
		// The exclusive upper bound remains exact for 64-bit destinations.
		overflow = n < lower || n >= upper
	}
	if overflow {
		return nil, fmt.Errorf("integer overflows %s", to.Type())
	}
	return data, nil
}
