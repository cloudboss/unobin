package typecheck

import (
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
)

// Scope carries the lexical information the inferrer needs to type
// an expression: local input declarations, an optional @each
// binding, and a callback that returns the output Type for a node
// address (resource/data/action.<ns>.<type>.<name>). LookupNode may
// be nil when the caller has no node table; the walker returns
// Unknown for any node reference in that case.
type Scope struct {
	Inputs     []ObjectField
	Each       *EachBinding
	LookupNode LookupNodeFn
	// Locals holds comprehension-bound names. They resolve as bare
	// values and as dot-path roots ahead of var/resource/data/action,
	// so an inner binding shadows an outer one.
	Locals map[string]Type
}

// withLocals returns a child scope that adds the comprehension
// bindings to Locals. One name binds the element (list) or value
// (map); two names bind index/key plus element/value. The parent is
// left untouched.
func (s *Scope) withLocals(names []string, key, elem Type) *Scope {
	child := *s
	locals := make(map[string]Type, len(s.Locals)+2)
	for k, v := range s.Locals {
		locals[k] = v
	}
	switch len(names) {
	case 1:
		locals[names[0]] = elem
	case 2:
		locals[names[0]] = key
		locals[names[1]] = elem
	}
	child.Locals = locals
	return &child
}

// EachBinding is the type pair bound by an enclosing @for-each.
// Key is the index type (integer for lists, string for maps,
// element type for sets). Value is the iterated element type.
type EachBinding struct {
	Key   Type
	Value Type
}

// LookupNodeFn returns the output Type of a node by kind, namespace,
// type, and name. The boolean is false when the node is not known;
// the inferrer then returns Unknown without an error (the existing
// reference checker has the responsibility to report unresolved
// node addresses).
type LookupNodeFn func(kind, ns, typ, name string) (Type, bool)

// Infer walks e and returns its inferred type. The target steers
// how ambiguous literals decide between list/tuple and how object
// literals match against a declared type; pass TUnknown when no
// target is in effect. Errors found during inference are appended
// to errs; the return value is best-effort and may be Unknown when
// nothing useful can be determined.
func Infer(e lang.Expr, target Type, scope *Scope, errs *lang.ErrorList) Type {
	if e == nil {
		return TUnknown()
	}
	switch v := e.(type) {
	case *lang.StringLit:
		return TString()
	case *lang.NumberLit:
		if v.IsFloat {
			return TNumber()
		}
		return TInteger()
	case *lang.BoolLit:
		return TBoolean()
	case *lang.NullLit:
		return TNull()
	case *lang.Ident:
		if scope != nil {
			if t, ok := scope.Locals[v.Name]; ok {
				return t
			}
		}
		return TUnknown()
	case *lang.ArrayLit:
		return inferArray(v, target, scope, errs)
	case *lang.ObjectLit:
		return inferObject(v, target, scope, errs)
	case *lang.DotPath:
		return inferDotPath(v, scope, errs)
	case *lang.Call:
		return TUnknown()
	case *lang.Infix:
		return inferInfix(v, scope, errs)
	case *lang.Prefix:
		return inferPrefix(v, scope, errs)
	case *lang.Conditional:
		return inferConditional(v, target, scope, errs)
	case *lang.Comprehension:
		return inferComprehension(v, target, scope, errs)
	}
	return TUnknown()
}

// inferConditional types `if cond then a else b`. The condition must be
// a boolean. The result is the join of the two branch types; when the
// branches have incompatible known types it reports a mismatch and
// yields Unknown.
func inferConditional(
	c *lang.Conditional, target Type, scope *Scope, errs *lang.ErrorList,
) Type {
	Check(c.Cond, TBoolean(), scope, errs)
	thenT := Infer(c.Then, target, scope, errs)
	elseT := Infer(c.Else, target, scope, errs)
	if j, ok := join(thenT, elseT); ok {
		return j
	}
	if thenT.IsKnown() && elseT.IsKnown() {
		errs.Addf(lang.ErrType, c.S.Start,
			"if branches have different types: %s and %s", thenT, elseT)
	}
	return TUnknown()
}

// inferComprehension types a list or map comprehension. It binds the
// element (and index/key) types from the source into a child scope,
// requires a boolean filter and a string map key, and returns the
// produced list or map type. A grouped map (`...`) collects values
// into a list per key.
func inferComprehension(
	c *lang.Comprehension, target Type, scope *Scope, errs *lang.ErrorList,
) Type {
	if scope == nil {
		scope = &Scope{}
	}
	srcT := Infer(c.Source, TUnknown(), scope, errs)
	if srcT.Unwrap().Kind == Set {
		errs.Addf(lang.ErrType, c.Source.Span().Start,
			"comprehension source cannot be a set; convert it with to-list first")
	}
	key, elem := comprehensionBindingTypes(srcT)
	child := scope.withLocals(c.Names, key, elem)
	if c.Filter != nil {
		Check(c.Filter, TBoolean(), child, errs)
	}
	if c.Kind == lang.CompMap {
		Check(c.Key, TString(), child, errs)
		valT := Infer(c.Value, TUnknown(), child, errs)
		if c.Group {
			return TMap(TList(valT))
		}
		return TMap(valT)
	}
	if elemTarget, ok := listElemTarget(target); ok {
		Check(c.Value, elemTarget, child, errs)
		return TList(elemTarget)
	}
	return TList(Infer(c.Value, TUnknown(), child, errs))
}

// comprehensionBindingTypes derives the binding types from the source.
// key is the first binding (index for a list, key for a map); elem is
// the iterated element or value.
func comprehensionBindingTypes(src Type) (key, elem Type) {
	src = src.Unwrap()
	switch src.Kind {
	case List, Set:
		return TInteger(), elemOr(src)
	case Map:
		return TString(), elemOr(src)
	}
	return TUnknown(), TUnknown()
}

func elemOr(t Type) Type {
	if t.Elem != nil {
		return *t.Elem
	}
	return TUnknown()
}

func listElemTarget(target Type) (Type, bool) {
	if (target.Kind == List || target.Kind == Set) && target.Elem != nil {
		return *target.Elem, true
	}
	return Type{}, false
}

// Check infers the type of e and verifies it is assignable to the
// declared target. Returns the inferred type and appends a
// mismatch diagnostic to errs when the types are incompatible.
// Container and object targets are already enforced bidirectionally
// inside Infer (mismatches are reported at the element or field
// level); Check's own Assignable comparison runs only for atomic
// targets so the same mistake is not reported twice.
func Check(e lang.Expr, target Type, scope *Scope, errs *lang.ErrorList) Type {
	got := Infer(e, target, scope, errs)
	if !target.IsKnown() || !got.IsKnown() {
		return got
	}
	switch target.Kind {
	case List, Set, Map, Tuple, Object:
		return got
	}
	if !Assignable(target, got) {
		errs.Addf(lang.ErrType, e.Span().Start,
			"type mismatch: expected %s, got %s", target, got)
	}
	return got
}

func inferArray(
	a *lang.ArrayLit, target Type, scope *Scope, errs *lang.ErrorList,
) Type {
	switch target.Kind {
	case List, Set:
		if target.Elem == nil {
			return inferArrayFree(a, scope, errs)
		}
		for _, el := range a.Elements {
			Check(el, *target.Elem, scope, errs)
		}
		return TList(*target.Elem)
	case Tuple:
		if len(target.Elems) != len(a.Elements) {
			errs.Addf(lang.ErrType, a.S.Start,
				"type mismatch: expected %s, got tuple with %d elements",
				target, len(a.Elements))
			return inferArrayFree(a, scope, errs)
		}
		elems := make([]Type, len(a.Elements))
		for i, el := range a.Elements {
			elems[i] = Check(el, target.Elems[i], scope, errs)
		}
		return TTuple(elems)
	}
	return inferArrayFree(a, scope, errs)
}

func inferArrayFree(a *lang.ArrayLit, scope *Scope, errs *lang.ErrorList) Type {
	if len(a.Elements) == 0 {
		return TList(TUnknown())
	}
	elems := make([]Type, len(a.Elements))
	for i, el := range a.Elements {
		elems[i] = Infer(el, TUnknown(), scope, errs)
	}
	if common, ok := joinAll(elems); ok {
		return TList(common)
	}
	return TTuple(elems)
}

func inferObject(
	o *lang.ObjectLit, target Type, scope *Scope, errs *lang.ErrorList,
) Type {
	switch target.Kind {
	case Object:
		return inferObjectAgainstObject(o, target, scope, errs)
	case Map:
		if target.Elem == nil {
			return inferObjectFree(o, scope, errs)
		}
		for _, fld := range o.Fields {
			if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
				continue
			}
			Check(fld.Value, *target.Elem, scope, errs)
		}
		return TMap(*target.Elem)
	}
	return inferObjectFree(o, scope, errs)
}

func inferObjectAgainstObject(
	o *lang.ObjectLit, target Type, scope *Scope, errs *lang.ErrorList,
) Type {
	declared := map[string]ObjectField{}
	for _, f := range target.Fields {
		declared[f.Name] = f
	}
	seen := map[string]bool{}
	fields := make([]ObjectField, 0, len(o.Fields))
	for _, fld := range o.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		name := fld.Key.Name
		seen[name] = true
		spec, ok := declared[name]
		if !ok {
			errs.Addf(lang.ErrType, fld.Key.S.Start,
				"unknown field %q on %s", name, target)
			fields = append(fields, ObjectField{
				Name: name,
				Type: Infer(fld.Value, TUnknown(), scope, errs),
			})
			continue
		}
		got := Check(fld.Value, spec.Type, scope, errs)
		fields = append(fields, ObjectField{Name: name, Type: got})
	}
	for _, f := range target.Fields {
		if f.Optional || seen[f.Name] {
			continue
		}
		errs.Addf(lang.ErrType, o.S.Start,
			"missing required field %q on %s", f.Name, target)
	}
	return TObject(fields)
}

func inferObjectFree(
	o *lang.ObjectLit, scope *Scope, errs *lang.ErrorList,
) Type {
	fields := make([]ObjectField, 0, len(o.Fields))
	for _, fld := range o.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		fields = append(fields, ObjectField{
			Name: fld.Key.Name,
			Type: Infer(fld.Value, TUnknown(), scope, errs),
		})
	}
	return TObject(fields)
}

func inferDotPath(dp *lang.DotPath, scope *Scope, errs *lang.ErrorList) Type {
	if dp.Root == nil {
		return TUnknown()
	}
	if scope != nil {
		if t, ok := scope.Locals[dp.Root.Name]; ok {
			return traverseSegments(t, dp.Segments, errs, false)
		}
	}
	switch dp.Root.Name {
	case "var":
		return inferVar(dp, scope, errs)
	case "resource", "data", "action":
		return inferNode(dp, scope, errs)
	case "@each":
		return inferEach(dp, scope)
	}
	return TUnknown()
}

func inferVar(dp *lang.DotPath, scope *Scope, errs *lang.ErrorList) Type {
	if scope == nil || len(dp.Segments) == 0 {
		return TUnknown()
	}
	name := dp.Segments[0].Name
	field, ok := findInput(scope.Inputs, name)
	if !ok {
		return TUnknown()
	}
	t := field.Type
	if field.Optional {
		t = TOptional(t)
	}
	return traverseSegments(t, dp.Segments[1:], errs, false)
}

func inferNode(dp *lang.DotPath, scope *Scope, errs *lang.ErrorList) Type {
	if scope == nil || scope.LookupNode == nil || len(dp.Segments) < 3 {
		return TUnknown()
	}
	ns := dp.Segments[0].Name
	typ := dp.Segments[1].Name
	name := dp.Segments[2].Name
	if ns == "" || typ == "" || name == "" {
		return TUnknown()
	}
	t, ok := scope.LookupNode(dp.Root.Name, ns, typ, name)
	if !ok {
		return TUnknown()
	}
	rest := dp.Segments[3:]
	if len(rest) > 0 && rest[0].Index != nil && rest[0].Name == "" {
		rest = rest[1:]
	}
	return traverseSegments(t, rest, errs, true)
}

func inferEach(dp *lang.DotPath, scope *Scope) Type {
	if scope == nil || scope.Each == nil || len(dp.Segments) == 0 {
		return TUnknown()
	}
	switch dp.Segments[0].Name {
	case "key":
		return scope.Each.Key
	case "value":
		return scope.Each.Value
	}
	return TUnknown()
}

// traverseSegments walks the trailing field segments after a root
// reference, narrowing the type as it descends. Each .name segment
// looks up an object field; each [expr] segment unwraps a list,
// set, or map element. Returns Unknown when a segment cannot be
// resolved.
//
// skipFirst suppresses the unknown-field diagnostic at segs[0] so
// callers whose first segment is already checked elsewhere (the
// reference checker's `unknown field "x" on <ns>.<type>` message
// for resource/data/action paths) do not report twice. Deeper
// segments always report.
func traverseSegments(
	t Type, segs []lang.DotSegment, errs *lang.ErrorList, skipFirst bool,
) Type {
	current := t
	for i, seg := range segs {
		current = current.Unwrap()
		if !current.IsKnown() {
			return TUnknown()
		}
		if seg.Index != nil && seg.Name == "" {
			switch current.Kind {
			case List, Set, Map:
				if current.Elem == nil {
					return TUnknown()
				}
				current = *current.Elem
				continue
			}
			return TUnknown()
		}
		if seg.Name == "" {
			return TUnknown()
		}
		switch current.Kind {
		case Object:
			field, ok := current.Field(seg.Name)
			if !ok {
				if !(skipFirst && i == 0) {
					errs.Addf(lang.ErrType, seg.S.Start,
						"unknown field %q on %s", seg.Name, current)
				}
				return TUnknown()
			}
			current = field.Type
			if field.Optional {
				current = TOptional(current)
			}
		case Map:
			if current.Elem == nil {
				return TUnknown()
			}
			current = *current.Elem
		case Any:
			return TAny()
		default:
			return TUnknown()
		}
	}
	return current
}

func inferInfix(in *lang.Infix, scope *Scope, errs *lang.ErrorList) Type {
	left := Infer(in.Left, TUnknown(), scope, errs)
	right := Infer(in.Right, TUnknown(), scope, errs)
	switch in.Op {
	case "==", "!=", "<", "<=", ">", ">=", "&&", "||":
		return TBoolean()
	case "+":
		if left.Kind == String || right.Kind == String {
			return TString()
		}
		return numericResult(left, right)
	case "-", "*", "/", "%":
		return numericResult(left, right)
	}
	return TUnknown()
}

func inferPrefix(p *lang.Prefix, scope *Scope, errs *lang.ErrorList) Type {
	inner := Infer(p.Expr, TUnknown(), scope, errs)
	switch p.Op {
	case "!":
		return TBoolean()
	case "-", "+":
		if inner.Kind == Integer {
			return TInteger()
		}
		if inner.Kind == Number {
			return TNumber()
		}
	}
	return TUnknown()
}

func numericResult(left, right Type) Type {
	if left.Kind == Integer && right.Kind == Integer {
		return TInteger()
	}
	if (left.Kind == Integer || left.Kind == Number) &&
		(right.Kind == Integer || right.Kind == Number) {
		return TNumber()
	}
	return TUnknown()
}

func findInput(inputs []ObjectField, name string) (ObjectField, bool) {
	for _, in := range inputs {
		if in.Name == name {
			return in, true
		}
	}
	return ObjectField{}, false
}

// joinAll returns a single type that all of ts are assignable to,
// or ok=false when no such type is reachable from this V1's join
// rules. Used by inferArrayFree to pick `list(T)` for a literal
// whose elements share a common type.
func joinAll(ts []Type) (Type, bool) {
	if len(ts) == 0 {
		return TUnknown(), true
	}
	current := ts[0]
	for _, t := range ts[1:] {
		j, ok := join(current, t)
		if !ok {
			return Type{}, false
		}
		current = j
	}
	return current, true
}

func join(a, b Type) (Type, bool) {
	if !a.IsKnown() {
		return b, true
	}
	if !b.IsKnown() {
		return a, true
	}
	if a.Equal(b) {
		return a, true
	}
	if (a.Kind == Integer && b.Kind == Number) ||
		(a.Kind == Number && b.Kind == Integer) {
		return TNumber(), true
	}
	if a.Kind == Null {
		return TOptional(b), true
	}
	if b.Kind == Null {
		return TOptional(a), true
	}
	if a.Kind == Optional && a.Elem != nil {
		j, ok := join(*a.Elem, b)
		if !ok {
			return Type{}, false
		}
		return TOptional(j), true
	}
	if b.Kind == Optional && b.Elem != nil {
		j, ok := join(a, *b.Elem)
		if !ok {
			return Type{}, false
		}
		return TOptional(j), true
	}
	// Same container kind: join element types covariantly. An empty
	// literal contributes an unknown element that joins to the other
	// side, so `if c then [strings] else []` is a list of strings.
	if a.Kind == b.Kind && a.Elem != nil && b.Elem != nil {
		switch a.Kind {
		case List, Set, Map:
			j, ok := join(*a.Elem, *b.Elem)
			if !ok {
				return Type{}, false
			}
			return Type{Kind: a.Kind, Elem: &j}, true
		}
	}
	return Type{}, false
}

// FieldKindLabel returns the lowercase singular noun for a node
// kind ("resource", "data source", "action") used in error
// messages. Lives here so check_refs.go and the inferrer agree.
func FieldKindLabel(kind string) string {
	switch strings.ToLower(kind) {
	case "data":
		return "data source"
	default:
		return strings.ToLower(kind)
	}
}
