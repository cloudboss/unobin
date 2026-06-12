package typecheck

import "slices"

// MergeShallow computes the type of a left-to-right shallow merge of
// objects. When every argument is a known object, optional-wrapped or
// null allowed, the result is the merged object type: a required
// field of a definitely-present later argument replaces an earlier
// field outright, while a field that may be absent at runtime (an
// optional field, or any field of a possibly-null argument) joins
// what it may replace. An argument that is a map, opaque, or unknown
// can hold keys the checker cannot see, so the whole result is
// Unknown. No arguments merge to the empty object.
func MergeShallow(args []Type) Type {
	type state struct {
		t        Type
		required bool
	}
	var order []string
	fields := map[string]*state{}
	for _, arg := range args {
		if arg.Kind == Null {
			continue
		}
		argMayMiss := false
		if arg.Kind == Optional {
			argMayMiss = true
			arg = arg.Unwrap()
		}
		if arg.Kind != Object {
			return TUnknown()
		}
		for _, f := range arg.Fields {
			mayMiss := argMayMiss || f.Optional
			s, ok := fields[f.Name]
			if !ok {
				order = append(order, f.Name)
				fields[f.Name] = &state{t: f.Type, required: !mayMiss}
				continue
			}
			if mayMiss {
				s.t = joinTypes(s.t, f.Type)
				continue
			}
			s.t = f.Type
			s.required = true
		}
	}
	out := make([]ObjectField, 0, len(order))
	for _, name := range order {
		s := fields[name]
		out = append(out, ObjectField{Name: name, Type: s.t, Optional: !s.required})
	}
	return TObject(out)
}

// joinTypes returns the type of a value that may hold either of two
// types, folding equal types together and extending an existing union
// instead of nesting one.
func joinTypes(a, b Type) Type {
	if a.Equal(b) {
		return a
	}
	if a.Kind == Union {
		for _, m := range a.Elems {
			if m.Equal(b) {
				return a
			}
		}
		return TUnion(append(slices.Clone(a.Elems), b))
	}
	return TUnion([]Type{a, b})
}
