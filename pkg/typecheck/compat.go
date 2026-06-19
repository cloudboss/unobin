package typecheck

// Assignable reports whether a value of type src can flow into a
// slot declared with type dst. Either side being Unknown returns
// true so partial schema information fails open rather than
// producing spurious errors.
//
// Rules in plain terms:
//   - opaque accepts anything, and an opaque source flows only into
//     an opaque slot: the value passes through unread, and a typed
//     slot is a promise to read. null is assignable only into a slot
//     that includes null (an optional() wrapper or the null atom).
//   - integer widens into number but not the other way.
//   - optional(T) accepts T, null, or optional(T); a possibly-null
//     source never flows into a slot that wants a value, so null
//     handling is settled at compile and a null can only appear
//     where the program wrote down what happens then.
//   - list/map/tuple compare element-wise.
//   - object types compare structurally: every required dst field
//     must have a compatible src field; extra src fields are
//     allowed (open against the source) and missing-optional dst
//     fields are tolerated.
func Assignable(dst, src Type) bool {
	if !dst.IsKnown() || !src.IsKnown() {
		return true
	}
	if dst.Kind == Opaque {
		return true
	}

	if dst.Kind == Optional {
		if src.Kind == Null {
			return true
		}
		inner := dst.Elem
		if inner == nil {
			return true
		}
		return Assignable(*inner, src.Unwrap())
	}
	if src.Kind == Opaque {
		return false
	}
	if src.Kind == Optional {
		return false
	}
	if src.Kind == Null {
		return dst.Kind == Null
	}

	switch dst.Kind {
	case LibraryConfig:
		return libraryConfigAssignable(dst, src)
	case Union:
		for _, m := range dst.Elems {
			if Assignable(m, src) {
				return true
			}
		}
		return false
	case String, Boolean, Null:
		return dst.Kind == src.Kind
	case Integer:
		return src.Kind == Integer
	case Number:
		return src.Kind == Integer || src.Kind == Number
	case List:
		return listAssignable(dst, src)
	case Map:
		return mapAssignable(dst, src)
	case Tuple:
		return tupleAssignable(dst, src)
	case Object:
		return objectAssignable(dst, src)
	}
	return false
}

func libraryConfigAssignable(dst, src Type) bool {
	if src.Kind == LibraryConfig {
		return dst.Equal(src)
	}
	if src.Kind == Object || src.Kind == Map {
		return objectAssignable(TObject(dst.Fields), src)
	}
	return false
}

func listAssignable(dst, src Type) bool {
	if dst.Elem == nil {
		return true
	}
	if src.Kind == List {
		if src.Elem == nil {
			return true
		}
		return Assignable(*dst.Elem, *src.Elem)
	}
	if src.Kind == Tuple {
		for _, e := range src.Elems {
			if !Assignable(*dst.Elem, e) {
				return false
			}
		}
		return true
	}
	return false
}

func mapAssignable(dst, src Type) bool {
	if dst.Elem == nil {
		return true
	}
	if src.Kind == Map {
		if src.Elem == nil {
			return true
		}
		return Assignable(*dst.Elem, *src.Elem)
	}
	if src.Kind == Object {
		for _, f := range src.Fields {
			if !Assignable(*dst.Elem, f.Type) {
				return false
			}
		}
		return true
	}
	return false
}

func tupleAssignable(dst, src Type) bool {
	if src.Kind != Tuple {
		return false
	}
	if len(dst.Elems) != len(src.Elems) {
		return false
	}
	for i := range dst.Elems {
		if !Assignable(dst.Elems[i], src.Elems[i]) {
			return false
		}
	}
	return true
}

func objectAssignable(dst, src Type) bool {
	if src.Kind == LibraryConfig {
		src = TObject(src.Fields)
	}
	if dst.Kind == LibraryConfig {
		dst = TObject(dst.Fields)
	}
	if src.Kind != Object && src.Kind != Map {
		return false
	}
	if src.Kind == Map {
		if src.Elem == nil {
			return true
		}
		for _, f := range dst.Fields {
			if !Assignable(f.Type, *src.Elem) {
				return false
			}
		}
		return true
	}
	srcFields := map[string]ObjectField{}
	for _, f := range src.Fields {
		srcFields[f.Name] = f
	}
	for _, f := range dst.Fields {
		got, present := srcFields[f.Name]
		if !present {
			if f.Optional {
				continue
			}
			return false
		}
		want := f.Type
		if f.Optional {
			want = TOptional(want)
		}
		if !Assignable(want, got.Type) {
			return false
		}
	}
	return true
}
