package typecheck

import (
	"maps"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
)

// Scope carries the lexical information the inferrer needs to type
// an expression: local input declarations, an optional @each binding,
// and a callback that returns the output Type for a node address.
// LookupNode may be nil when the caller has no node table; the walker
// returns Unknown for any node reference in that case.
type Scope struct {
	Inputs      []ObjectField
	Each        *EachBinding
	LookupNode  LookupNodeFn
	LookupLocal LookupLocalFn
	// LookupFunction resolves a library-qualified function to its
	// signature so a call's arguments and result type-check. Nil, or a
	// false return, leaves the call inferring Unknown; existence and
	// argument count are the reference checker's to enforce.
	LookupFunction func(library, name string) (FuncSig, bool)
	// LookupConfiguration resolves a library selector to the object type
	// of its configuration schema. Nil, or a false return, leaves the
	// reference inferring Unknown.
	LookupConfiguration func(alias string) (Type, bool)
	// Bindings holds comprehension-bound names. They resolve as bare
	// values and as dot-path roots ahead of var/resource/data/action.
	// Names are distinct across nesting; validation rejects an inner
	// comprehension that rebinds an enclosing name.
	Bindings map[string]Type
	// Narrowed overrides reference types inside a region guarded by a
	// null test, keyed by the canonical dot path ("var.x.y"). A lookup
	// takes the longest matching prefix and resumes traversal from the
	// narrowed type. Sound because values never change between the
	// test and the read.
	Narrowed map[string]Type
	// MissingAsNull mirrors the evaluation mode of the same name:
	// navigating into a possibly-null value reads as null instead of
	// failing, so the result of such a path is optional. Constraint
	// expressions check in this mode; everything else is strict.
	MissingAsNull bool
	// Observe, when set, receives every inferred expression with its
	// type. The residual-Unknown harness reads the stream to find
	// positions the checker cannot type.
	Observe func(e lang.Expr, t Type)
}

// FuncSig describes a library function to the inferrer: the fixed
// parameter types in order, the element type of a variadic tail (nil
// when the function is not variadic), and the result type. A function
// registered without declared types reads as all-Unknown, which checks
// nothing and infers nothing.
//
// Infer, when set, computes the call's type from the argument types in
// place of Result. Arguments of such a call are inferred without a
// target so the hook sees their natural types: an object literal stays
// the precise object it spells, not the parameter type it flowed into.
type FuncSig struct {
	Params   []Type
	Variadic *Type
	Result   Type
	Infer    func(args []Type) Type
}

// withBindings returns a child scope that adds the comprehension
// bindings to Bindings. One name binds the element (list) or value
// (map); two names bind index/key plus element/value. The parent is
// left untouched.
func (s *Scope) withBindings(names []string, key, elem Type) *Scope {
	child := *s
	binds := make(map[string]Type, len(s.Bindings)+2)
	maps.Copy(binds, s.Bindings)
	switch len(names) {
	case 1:
		binds[names[0]] = elem
	case 2:
		binds[names[0]] = key
		binds[names[1]] = elem
	}
	child.Bindings = binds
	return &child
}

// EachBinding is the type pair bound by an enclosing @for-each.
// Key is the index type (integer for lists, string for maps).
// Value is the iterated element type.
type EachBinding struct {
	Key   Type
	Value Type
}

// LookupNodeFn returns the output Type of a node by kind, alias,
// type, and name. The boolean is false when the node is not known;
// the inferrer then returns Unknown without an error (the existing
// reference checker has the responsibility to report unresolved
// node addresses).
type LookupNodeFn func(kind, alias, typ, name string) (Type, bool)

// LookupLocalFn returns the inferred Type of a `locals:` entry by
// name. The boolean is false when no such local is declared; the
// inferrer then returns Unknown without an error (the reference
// checker reports an unknown local name).
type LookupLocalFn func(name string) (Type, bool)

// Infer walks e and returns its inferred type. The target steers
// how ambiguous literals decide between list/tuple and how object
// literals match against a declared type; pass TUnknown when no
// target is in effect. Errors found during inference are appended
// to errs; the return value is best-effort and may be Unknown when
// nothing useful can be determined.
func Infer(e lang.Expr, target Type, scope *Scope, errs *lang.ErrorList) Type {
	t := infer(e, target, scope, errs)
	if scope != nil && scope.Observe != nil && e != nil {
		scope.Observe(e, t)
	}
	return t
}

func infer(e lang.Expr, target Type, scope *Scope, errs *lang.ErrorList) Type {
	if e == nil {
		return TUnknown()
	}
	switch v := e.(type) {
	case *lang.StringLit:
		return TString()
	case *lang.InterpolatedString:
		return inferInterpolated(v, scope, errs)
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
			if t, ok := scope.Narrowed[v.Name]; ok {
				return t
			}
			if t, ok := scope.Bindings[v.Name]; ok {
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
		return inferCall(v, scope, errs)
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

// inferCall types a library function call when the scope can describe
// the function. Each argument checks against its parameter type and a
// variadic tail against the tail's element type; the call's type is
// the declared result, or what the signature's Infer hook computes
// from the argument types. An argument past a fixed signature checks
// freely, since the argument count is the reference checker's to
// report. An unresolvable call infers Unknown.
func inferCall(c *lang.Call, scope *Scope, errs *lang.ErrorList) Type {
	if scope == nil || scope.LookupFunction == nil || c.Library == nil || c.Func == nil {
		return TUnknown()
	}
	sig, ok := scope.LookupFunction(c.Library.Name, c.Func.Name)
	if !ok {
		return TUnknown()
	}
	var argTypes []Type
	if sig.Infer != nil {
		argTypes = make([]Type, 0, len(c.Args))
	}
	for i, arg := range c.Args {
		target := TUnknown()
		switch {
		case i < len(sig.Params):
			target = sig.Params[i]
		case sig.Variadic != nil:
			target = *sig.Variadic
		}
		switch {
		case sig.Infer != nil:
			got := Infer(arg, TUnknown(), scope, errs)
			if target.IsKnown() && got.IsKnown() && !Assignable(target, got) {
				reportMismatch(arg, target, got, errs)
			}
			argTypes = append(argTypes, got)
		case target.Kind == Union:
			checkUnionArg(c.Func.Name, arg, target, scope, errs)
		default:
			Check(arg, target, scope, errs)
		}
	}
	if sig.Infer != nil {
		return sig.Infer(argTypes)
	}
	return sig.Result
}

// checkUnionArg checks an argument against a builtin's union-typed
// parameter. The diagnostic reuses the runtime function's own prose,
// so a compile rejection and a plan rejection read the same.
func checkUnionArg(
	fnName string, arg lang.Expr, target Type, scope *Scope, errs *lang.ErrorList,
) {
	got := Infer(arg, TUnknown(), scope, errs)
	if !got.IsKnown() || Assignable(target, got) {
		return
	}
	errs.Addf(lang.ErrType, arg.Span().Start,
		"%s: argument must be %s, got %s", fnName, unionProse(target), typeProse(got))
}

// unionProse names a union's members the way the runtime functions
// do in their errors: "a string, list, or map".
func unionProse(t Type) string {
	nouns := make([]string, len(t.Elems))
	for i, m := range t.Elems {
		nouns[i] = kindNoun(m.Kind)
	}
	if len(nouns) > 2 {
		return "a " + strings.Join(nouns[:len(nouns)-1], ", ") + ", or " + nouns[len(nouns)-1]
	}
	return "a " + strings.Join(nouns, " or ")
}

func kindNoun(k Kind) string {
	switch k {
	case String:
		return "string"
	case Integer:
		return "integer"
	case Number:
		return "number"
	case Boolean:
		return "boolean"
	case List:
		return "list"
	case Map:
		return "map"
	case Object:
		return "object"
	case Tuple:
		return "tuple"
	}
	return "value"
}

// typeProse renders a type for prose the way the runtime's
// TypeMessage names values: plain kinds get their article, anything
// richer prints structurally.
func typeProse(t Type) string {
	switch t.Kind {
	case String:
		return "a string"
	case Integer:
		return "an integer"
	case Number:
		return "a number"
	case Boolean:
		return "a boolean"
	case List:
		return "a list"
	case Map:
		return "a map"
	case Tuple:
		return "a tuple"
	case Object:
		return "an object"
	case Null:
		return "null"
	}
	return t.String()
}

// inferConditional types `if cond then a else b`. The condition must be
// a boolean. The result is the join of the two branch types; when the
// branches have incompatible known types it reports a mismatch and
// yields Unknown.
func inferConditional(
	c *lang.Conditional, target Type, scope *Scope, errs *lang.ErrorList,
) Type {
	Check(c.Cond, TBoolean(), scope, errs)
	tFacts, fFacts := nullFacts(c.Cond, scope)
	thenT := Infer(c.Then, target, scope.narrowed(tFacts), errs)
	elseT := Infer(c.Else, target, scope.narrowed(fFacts), errs)
	if j, ok := join(thenT, elseT); ok {
		return j
	}
	if thenT.IsKnown() && elseT.IsKnown() {
		errs.Addf(lang.ErrType, c.S.Start,
			"if branches have different types: %s and %s", thenT, elseT)
	}
	return TUnknown()
}

// inferInterpolated types an interpolated string. The result is always
// a string; each slot is checked to be a scalar that cannot be null,
// since the only sensible thing to splice into text is a single value.
func inferInterpolated(s *lang.InterpolatedString, scope *Scope, errs *lang.ErrorList) Type {
	for _, part := range s.Parts {
		if part.Expr == nil {
			continue
		}
		t := Infer(part.Expr, TUnknown(), scope, errs)
		checkInterpolatedSlot(t, part.Expr.Span().Start, errs)
	}
	return TString()
}

// checkInterpolatedSlot reports when a slot type is not a scalar or may
// be null. Unknown fails open, so a value the inferrer cannot reason
// about is left to the runtime guard in evalInterpolated. An opaque
// value does not splice into text directly; serializing it is the way
// to spell that.
func checkInterpolatedSlot(t Type, pos lang.Position, errs *lang.ErrorList) {
	if t.Unwrap().Kind == Opaque {
		errs.Addf(lang.ErrType, pos,
			"interpolation slot is opaque; render it as text with @core.to-json(x)")
		return
	}
	switch t.Kind {
	case Unknown, String, Integer, Number, Boolean:
		return
	case Null, Optional:
		errs.Addf(lang.ErrType, pos,
			"interpolation slot may be null; supply a fallback, like "+
				"{{ x ?? '-' }} (got %s)", t)
	default:
		errs.Addf(lang.ErrType, pos,
			"interpolation slot must be a scalar, got %s", t)
	}
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
	checkComprehensionSource(srcT, c.Source.Span().Start, errs)
	key, elem := comprehensionBindingTypes(srcT)
	child := scope.withBindings(c.Names, key, elem)
	if c.Filter != nil {
		Check(c.Filter, TBoolean(), child, errs)
		facts, _ := nullFacts(c.Filter, child)
		child = child.narrowed(facts)
	}
	if c.Kind == lang.CompMap {
		Check(c.Key, TString(), child, errs)
		if valTarget, ok := mapValueTarget(target, c.Group); ok {
			Check(c.Value, valTarget, child, errs)
			if c.Group {
				return TMap(TList(valTarget))
			}
			return TMap(valTarget)
		}
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

// checkComprehensionSource reports a comprehension over something
// that has no elements to iterate, or that may be null, since the
// evaluator rejects a null source. Tuples and objects pass: at
// runtime they are the same lists and maps every comprehension
// consumes. An opaque source is closed off: iterating would read
// into a value that passes through unread.
func checkComprehensionSource(src Type, pos lang.Position, errs *lang.ErrorList) {
	if src.Unwrap().Kind == Opaque {
		errs.Addf(lang.ErrType, pos,
			"comprehension source is opaque; declare its type, like list(...) or map(...)")
		return
	}
	switch src.Kind {
	case Unknown, List, Map, Object, Tuple:
		return
	case Optional:
		switch src.Unwrap().Kind {
		case Unknown, List, Map, Object, Tuple:
			errs.Addf(lang.ErrType, pos,
				"comprehension source may be null; supply a fallback, like "+
					"xs ?? [] (got %s)", src)
			return
		}
	}
	errs.Addf(lang.ErrType, pos,
		"comprehension source must be a list or map, got %s", src)
}

// comprehensionBindingTypes derives the binding types from the source.
// key is the first binding (index for a list, key for a map); elem is
// the iterated element or value. A tuple or object binds the join of
// its element types, since any element may be the one in hand.
func comprehensionBindingTypes(src Type) (key, elem Type) {
	src = src.Unwrap()
	switch src.Kind {
	case List:
		return TInteger(), elemOr(src)
	case Map:
		return TString(), elemOr(src)
	case Tuple:
		if j, ok := joinAll(src.Elems); ok {
			return TInteger(), j
		}
		return TInteger(), TUnknown()
	case Object:
		types := make([]Type, len(src.Fields))
		for i, f := range src.Fields {
			types[i] = f.Type
		}
		if j, ok := joinAll(types); ok {
			return TString(), j
		}
		return TString(), TUnknown()
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
	if target.Kind == List && target.Elem != nil {
		return *target.Elem, true
	}
	return Type{}, false
}

// mapValueTarget extracts the type a map comprehension's value
// expression should check against. A grouped comprehension collects
// values into a list per key, so the value target is the element of
// a map-of-lists target.
func mapValueTarget(target Type, group bool) (Type, bool) {
	if target.Kind != Map || target.Elem == nil {
		return Type{}, false
	}
	if !group {
		return *target.Elem, true
	}
	if target.Elem.Kind == List && target.Elem.Elem != nil {
		return *target.Elem.Elem, true
	}
	return Type{}, false
}

// Check infers the type of e and verifies it is assignable to the
// declared target. Returns the inferred type and appends a mismatch
// diagnostic to errs when the types are incompatible. An array or
// object literal matching a container target is enforced element by
// element inside Infer, so Check's own Assignable comparison skips
// exactly those pairings and the same mistake is not reported twice;
// everything else, references and calls included, is compared here.
func Check(e lang.Expr, target Type, scope *Scope, errs *lang.ErrorList) Type {
	got := Infer(e, target, scope, errs)
	if !target.IsKnown() || !got.IsKnown() {
		return got
	}
	if literalEnforced(e, target) {
		return got
	}
	if !Assignable(target, got) {
		reportMismatch(e, target, got, errs)
	}
	return got
}

// reportMismatch appends the diagnostic for a value of type got in a
// slot of type target, with the hint matching how the mismatch can be
// fixed: an opaque value wants its type declared at its entry point,
// and a possibly-null value wants a null test.
func reportMismatch(e lang.Expr, target, got Type, errs *lang.ErrorList) {
	if got.Unwrap().Kind == Opaque {
		if target.Unwrap().Kind == String {
			errs.Addf(lang.ErrType, e.Span().Start,
				"type mismatch: expected %s, got %s; "+
					"pass it as JSON text with @core.to-json(x), "+
					"or declare the value's type where it enters",
				target, got)
			return
		}
		errs.Addf(lang.ErrType, e.Span().Start,
			"type mismatch: expected %s, got %s; "+
				"declare the value's type where it enters",
			target, got)
		return
	}
	if got.Kind == Optional && Assignable(target, got.Unwrap()) {
		errs.Addf(lang.ErrType, e.Span().Start,
			"type mismatch: expected %s, got %s; "+
				"test it first, like if x != null then x else <fallback>",
			target, got)
		return
	}
	errs.Addf(lang.ErrType, e.Span().Start,
		"type mismatch: expected %s, got %s", target, got)
}

// literalEnforced reports whether Infer already checked e against
// target at the element or field level: an array literal against a
// list, set, or tuple target, or an object literal against an object
// or map target. Opaque other pairing falls back to free inference, so
// Check must compare the result itself.
func literalEnforced(e lang.Expr, target Type) bool {
	switch e.(type) {
	case *lang.ArrayLit:
		return target.Kind == List || target.Kind == Tuple
	case *lang.ObjectLit:
		return target.Kind == Object || target.Kind == Map
	}
	return false
}

func inferArray(
	a *lang.ArrayLit, target Type, scope *Scope, errs *lang.ErrorList,
) Type {
	switch target.Kind {
	case List:
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
			// An open target admits fields beyond the declared ones, so
			// an extra literal field is not the typo it is elsewhere.
			if !target.Open {
				errs.Addf(lang.ErrType, fld.Key.S.Start,
					"unknown field %q on %s", name, target)
			}
			fields = append(fields, ObjectField{
				Name: name,
				Type: Infer(fld.Value, TUnknown(), scope, errs),
			})
			continue
		}
		// An optional field's value slot takes null too: absence and
		// null mean the same thing at the decode boundary.
		want := spec.Type
		if spec.Optional {
			want = TOptional(want)
		}
		got := Check(fld.Value, want, scope, errs)
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
	if t, rest, at, ok := narrowedLookup(scope, dp); ok {
		return traverseSegments(t, rest, at, scope, errs, false)
	}
	if scope != nil {
		if t, ok := scope.Bindings[dp.Root.Name]; ok {
			return traverseSegments(t, dp.Segments, dp.Root.Name, scope, errs, false)
		}
	}
	switch dp.Root.Name {
	case "var":
		return inferVar(dp, scope, errs)
	case "resource", "data", "action":
		return inferNode(dp, scope, errs)
	case "local":
		return inferLocal(dp, scope, errs)
	case "configuration":
		return inferConfiguration(dp, scope, errs)
	case "@each":
		return inferEach(dp, scope, errs)
	}
	return TUnknown()
}

// inferConfiguration types a configuration.<name> reference from the library's
// configuration schema. The schema describes the whole declared form, so
// navigation past the name checks field by field; an unknown schema infers
// Unknown.
func inferConfiguration(dp *lang.DotPath, scope *Scope, errs *lang.ErrorList) Type {
	if scope == nil || scope.LookupConfiguration == nil || len(dp.Segments) < 2 {
		return TUnknown()
	}
	if rejectGuardedRoot("configuration", dp.Segments, 2, errs) {
		return TUnknown()
	}
	alias, name := dp.Segments[0].Name, dp.Segments[1].Name
	if alias == "" || name == "" {
		return TUnknown()
	}
	t, ok := scope.LookupConfiguration(alias)
	if !ok {
		return TUnknown()
	}
	return traverseSegments(t, dp.Segments[2:],
		"configuration."+name, scope, errs, false)
}

// rejectGuardedRoot reports a `?.` used where the navigation cannot
// be null: the var, local, and @each tables always exist, and a node
// address is a name, not a value.
func rejectGuardedRoot(root string, segs []lang.DotSegment, n int, errs *lang.ErrorList) bool {
	for i := 0; i < n && i < len(segs); i++ {
		if segs[i].Guarded {
			errs.Addf(lang.ErrType, segs[i].S.Start,
				"%s is never null; write %s.%s", root, root, segs[i].Name)
			return true
		}
	}
	return false
}

func inferLocal(dp *lang.DotPath, scope *Scope, errs *lang.ErrorList) Type {
	if scope == nil || scope.LookupLocal == nil || len(dp.Segments) == 0 {
		return TUnknown()
	}
	if rejectGuardedRoot("local", dp.Segments, 1, errs) {
		return TUnknown()
	}
	name := dp.Segments[0].Name
	if name == "" {
		return TUnknown()
	}
	t, ok := scope.LookupLocal(name)
	if !ok {
		return TUnknown()
	}
	return traverseSegments(t, dp.Segments[1:], "local."+name, scope, errs, false)
}

func inferVar(dp *lang.DotPath, scope *Scope, errs *lang.ErrorList) Type {
	if scope == nil || len(dp.Segments) == 0 {
		return TUnknown()
	}
	if rejectGuardedRoot("var", dp.Segments, 1, errs) {
		return TUnknown()
	}
	name := dp.Segments[0].Name
	field, ok := findInput(scope.Inputs, name)
	if !ok {
		return TUnknown()
	}
	t := field.Type
	if field.Optional && !field.Defaulted {
		t = TOptional(t)
	}
	return traverseSegments(t, dp.Segments[1:], "var."+name, scope, errs, false)
}

func inferNode(dp *lang.DotPath, scope *Scope, errs *lang.ErrorList) Type {
	if scope == nil || scope.LookupNode == nil || len(dp.Segments) == 0 {
		return TUnknown()
	}
	if len(dp.Segments) >= 3 && plainNodeSegments(dp.Segments[:3]) {
		alias := dp.Segments[0].Name
		typ := dp.Segments[1].Name
		name := dp.Segments[2].Name
		if t, ok := scope.LookupNode(dp.Root.Name, alias, typ, name); ok {
			if rejectGuardedRoot(dp.Root.Name, dp.Segments, 3, errs) {
				return TUnknown()
			}
			at := dp.Root.Name + "." + alias + "." + typ + "." + name
			return inferNodeRest(t, at, dp.Segments[3:], scope, errs)
		}
	}
	if dp.Segments[0].Name == "" {
		return TUnknown()
	}
	if t, ok := scope.LookupNode(dp.Root.Name, "", "", dp.Segments[0].Name); ok {
		if rejectGuardedRoot(dp.Root.Name, dp.Segments, 1, errs) {
			return TUnknown()
		}
		at := dp.Root.Name + "." + dp.Segments[0].Name
		return inferNodeRest(t, at, dp.Segments[1:], scope, errs)
	}
	return TUnknown()
}

func plainNodeSegments(segs []lang.DotSegment) bool {
	for _, seg := range segs {
		if seg.Name == "" || seg.Index != nil || seg.Splat {
			return false
		}
	}
	return true
}

func inferNodeRest(
	t Type,
	at string,
	rest []lang.DotSegment,
	scope *Scope,
	errs *lang.ErrorList,
) Type {
	if len(rest) > 0 && rest[0].Index != nil && rest[0].Name == "" {
		at += segIndexText(rest[0])
		rest = rest[1:]
	}
	return traverseSegments(t, rest, at, scope, errs, true)
}

func inferEach(dp *lang.DotPath, scope *Scope, errs *lang.ErrorList) Type {
	if scope == nil || scope.Each == nil || len(dp.Segments) == 0 {
		return TUnknown()
	}
	if rejectGuardedRoot("@each", dp.Segments, 1, errs) {
		return TUnknown()
	}
	var t Type
	switch dp.Segments[0].Name {
	case "key":
		t = scope.Each.Key
	case "value":
		t = scope.Each.Value
	default:
		return TUnknown()
	}
	return traverseSegments(t, dp.Segments[1:], "@each."+dp.Segments[0].Name, scope, errs, false)
}

// traverseSegments walks the trailing field segments after a root
// reference, narrowing the type as it descends. Each .name segment
// looks up an object field; each [expr] segment indexes a list, set,
// map, tuple, or object element. Returns Unknown when a segment
// cannot be resolved. at is the source form of the path consumed so
// far, grown per segment so diagnostics name the exact read.
//
// skipFirst suppresses the unknown-field diagnostic at segs[0] so
// callers whose first segment is already checked elsewhere (the
// reference checker's `unknown field "x" on <alias>.<type>` message
// for resource/data/action paths) do not report twice. Deeper
// segments always report.
func traverseSegments(
	t Type, segs []lang.DotSegment, at string, scope *Scope, errs *lang.ErrorList,
	skipFirst bool,
) Type {
	current := t
	mayBeNull := false
	for i, seg := range segs {
		if seg.Guarded {
			switch current.Kind {
			case Optional:
				mayBeNull = true
				current = current.Unwrap()
			case Unknown, Opaque:
				// Nullability is unknowable here; the guard is allowed
				// and decides nothing.
			default:
				errs.Addf(lang.ErrType, seg.S.Start,
					"%s is never null; write %s.%s (got %s)", at, at, seg.Name, current)
				return TUnknown()
			}
		} else if current.Kind == Optional && scope != nil && scope.MissingAsNull {
			mayBeNull = true
			current = current.Unwrap()
		}
		if !current.IsKnown() {
			return TUnknown()
		}
		if seg.Splat {
			var elem Type
			switch current.Kind {
			case List:
				elem = elemOr(current)
			case Opaque:
				errs.Addf(lang.ErrType, seg.S.Start,
					"%s is opaque; declare its type, like list(object({ ... }))", at)
				return TUnknown()
			case Optional:
				if current.Unwrap().Kind == List {
					errs.Addf(lang.ErrType, seg.S.Start,
						"%s may be null; test it first, like "+
							"if %s != null then %s[*]... else [] (got %s)",
						at, at, at, current)
				} else {
					errs.Addf(lang.ErrType, seg.S.Start, "splat [*] needs a list, got %s", current)
				}
				return TUnknown()
			default:
				errs.Addf(lang.ErrType, seg.S.Start, "splat [*] needs a list, got %s", current)
				return TUnknown()
			}
			out := TList(traverseSegments(elem, segs[i+1:], at+"[*]", scope, errs, false))
			if mayBeNull {
				return TOptional(out)
			}
			return out
		}
		if seg.Index != nil && seg.Name == "" {
			current = indexSegmentType(current, seg, at, scope, errs)
			if !current.IsKnown() {
				return TUnknown()
			}
			at += segIndexText(seg)
			continue
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
						"unknown field %q on %s%s", seg.Name, current,
						openFieldHint(current))
				}
				return TUnknown()
			}
			current = field.Type
			if field.Optional && !field.Defaulted {
				current = TOptional(current)
			}
		case Map:
			if current.Elem == nil {
				return TUnknown()
			}
			current = *current.Elem
		case Optional:
			errs.Addf(lang.ErrType, seg.S.Start,
				"%s may be null; read it with %s?.%s, or test it first (got %s)",
				at, at, seg.Name, current)
			return TUnknown()
		case Opaque:
			errs.Addf(lang.ErrType, seg.S.Start,
				"%s is opaque; declare the fields you read, like open(object({ %s: ... }))",
				at, seg.Name)
			return TUnknown()
		default:
			if !(skipFirst && i == 0) {
				errs.Addf(lang.ErrType, seg.S.Start,
					"cannot read field %q of %s", seg.Name, current)
			}
			return TUnknown()
		}
		if seg.Guarded {
			at += "?." + seg.Name
		} else {
			at += "." + seg.Name
		}
	}
	if mayBeNull {
		return TOptional(current)
	}
	return current
}

// openFieldHint explains an unknown field on an open object: the
// field may well be present, but open admits fields without making
// them readable.
func openFieldHint(t Type) string {
	if t.Open {
		return "; declare the field to read it"
	}
	return ""
}

// segIndexText renders an index segment for a diagnostic: literal
// indexes exactly, anything computed as [..].
func segIndexText(seg lang.DotSegment) string {
	switch v := seg.Index.(type) {
	case *lang.NumberLit:
		return "[" + v.Value + "]"
	case *lang.StringLit:
		return "['" + v.Value + "']"
	}
	return "[..]"
}

// inferInfix types a binary operator expression and checks the
// operands the same way the evaluator will: logical operators want
// booleans, arithmetic wants numbers (`+` also takes two strings),
// orderings want two numbers or two strings, and equality wants types
// that could ever match. An operand the inferrer cannot type passes
// unchecked, so the checks mirror the runtime without false positives.
// indexSegmentType resolves a bare [expr] segment against the type
// being navigated. Lists and sets index by integer, maps by string. A
// tuple or object needs a literal index to pick a precise element
// type, since its elements differ by position or name; a computed
// index still has its type checked but yields the elements' join (for
// a tuple) or Unknown (for an object).
func indexSegmentType(
	current Type, seg lang.DotSegment, at string, scope *Scope, errs *lang.ErrorList,
) Type {
	switch current.Kind {
	case List:
		Check(seg.Index, TInteger(), scope, errs)
		return elemOr(current)
	case Map:
		Check(seg.Index, TString(), scope, errs)
		return elemOr(current)
	case Tuple:
		Check(seg.Index, TInteger(), scope, errs)
		lit, ok := seg.Index.(*lang.NumberLit)
		if !ok || lit.IsFloat {
			if j, ok := joinAll(current.Elems); ok {
				return j
			}
			return TUnknown()
		}
		i := lit.ParsedInt
		if i < 0 || i >= int64(len(current.Elems)) {
			errs.Addf(lang.ErrType, seg.S.Start,
				"index %d out of range for %s", i, current)
			return TUnknown()
		}
		return current.Elems[i]
	case Object:
		Check(seg.Index, TString(), scope, errs)
		lit, ok := seg.Index.(*lang.StringLit)
		if !ok {
			return TUnknown()
		}
		field, ok := current.Field(lit.Value)
		if !ok {
			errs.Addf(lang.ErrType, seg.S.Start,
				"unknown field %q on %s%s", lit.Value, current,
				openFieldHint(current))
			return TUnknown()
		}
		if field.Optional {
			return TOptional(field.Type)
		}
		return field.Type
	case Optional:
		errs.Addf(lang.ErrType, seg.S.Start,
			"%s may be null; test it first, like "+
				"if %s != null then %s%s else <fallback> (got %s)",
			at, at, at, segIndexText(seg), current)
		return TUnknown()
	case Opaque:
		Infer(seg.Index, TUnknown(), scope, errs)
		if lit, ok := seg.Index.(*lang.StringLit); ok {
			errs.Addf(lang.ErrType, seg.S.Start,
				"%s is opaque; declare the fields you read, like open(object({ %s: ... }))",
				at, lit.Value)
			return TUnknown()
		}
		errs.Addf(lang.ErrType, seg.S.Start,
			"%s is opaque; declare its type to index into it", at)
		return TUnknown()
	}
	errs.Addf(lang.ErrType, seg.S.Start, "cannot index into %s", current)
	return TUnknown()
}

func inferInfix(in *lang.Infix, scope *Scope, errs *lang.ErrorList) Type {
	left := Infer(in.Left, TUnknown(), scope, errs)
	// The right operand of a short-circuit operator only evaluates
	// when the left decided nothing, so a null test on the left is in
	// force on the right: proven for &&, refuted for ||.
	rightScope := scope
	switch in.Op {
	case "&&":
		facts, _ := nullFacts(in.Left, scope)
		rightScope = scope.narrowed(facts)
	case "||":
		_, facts := nullFacts(in.Left, scope)
		rightScope = scope.narrowed(facts)
	}
	right := Infer(in.Right, TUnknown(), rightScope, errs)
	switch in.Op {
	case "&&", "||":
		checkBooleanOperand(in.Op, in.Left, left, errs)
		checkBooleanOperand(in.Op, in.Right, right, errs)
		return TBoolean()
	case "??":
		return inferCoalesce(in, left, right, errs)
	case "==", "!=":
		checkEqualityOperands(in, left, right, errs)
		return TBoolean()
	case "<", "<=", ">", ">=":
		checkOrderingOperand(in.Op, in.Left, left, errs)
		checkOrderingOperand(in.Op, in.Right, right, errs)
		checkOperandFamilies(in, left, right, errs)
		return TBoolean()
	case "+":
		return inferPlus(in, left, right, errs)
	case "-", "*", "/":
		checkNumericOperand(in.Op, in.Left, left, errs)
		checkNumericOperand(in.Op, in.Right, right, errs)
		return numericResult(left, right)
	}
	return TUnknown()
}

// inferCoalesce types `left ?? right`, the fallback for a
// possibly-null left side: the result is the join of the discharged
// left and the right. A left side that can never be null makes the
// fallback dead, which is an error the way a dead ?. is. An opaque
// left stays legal: opaque includes null, and the fallback tests for
// it without reading inside.
func inferCoalesce(in *lang.Infix, left, right Type, errs *lang.ErrorList) Type {
	if lk, ok := operandKind(left); ok && lk != Optional && lk != Null && lk != Opaque {
		errs.Addf(lang.ErrType, in.Left.Span().Start,
			"left of ?? is never null; write it without the fallback (got %s)", left)
		return left
	}
	// An always-null left side contributes nothing; the result is the
	// fallback alone.
	if left.Kind == Null {
		return right
	}
	if j, ok := join(left.Unwrap(), right); ok {
		return j
	}
	if left.IsKnown() && right.IsKnown() {
		errs.Addf(lang.ErrType, in.S.Start,
			"?? sides have different types: %s and %s", left.Unwrap(), right)
	}
	return TUnknown()
}

// inferPlus types `+`, which adds two numbers or concatenates two
// strings. A known string on either side fixes the result to string;
// mixing the two families is an error at compile, as it is at eval.
func inferPlus(in *lang.Infix, left, right Type, errs *lang.ErrorList) Type {
	lk, lok := operandKind(left)
	rk, rok := operandKind(right)
	if lok && !isNumericKind(lk) && lk != String {
		errs.Addf(lang.ErrType, in.Left.Span().Start,
			"+: operand must be a number or a string, got %s", left)
		return TUnknown()
	}
	if rok && !isNumericKind(rk) && rk != String {
		errs.Addf(lang.ErrType, in.Right.Span().Start,
			"+: operand must be a number or a string, got %s", right)
		return TUnknown()
	}
	checkOperandFamilies(in, left, right, errs)
	if (lok && lk == String) || (rok && rk == String) {
		if lok && rok && lk != rk {
			return TUnknown()
		}
		return TString()
	}
	return numericResult(left, right)
}

// operandKind reduces a type to the kind the operator checks reason
// about. Unknown returns ok=false so partial information fails open.
// Optional stays as it is: an operand that may be null wants a null
// test, the same as a navigation. Opaque is a real kind here: an
// operator reads its operands, which an opaque value forbids.
func operandKind(t Type) (Kind, bool) {
	if t.Kind == Unknown {
		return t.Kind, false
	}
	if t.Kind == Optional && !t.IsKnown() {
		return Unknown, false
	}
	return t.Kind, true
}

func isNumericKind(k Kind) bool {
	return k == Integer || k == Number
}

func checkBooleanOperand(op string, e lang.Expr, t Type, errs *lang.ErrorList) {
	if k, ok := operandKind(t); ok && k != Boolean {
		errs.Addf(lang.ErrType, e.Span().Start,
			"%s: operand must be a boolean, got %s", op, t)
	}
}

func checkNumericOperand(op string, e lang.Expr, t Type, errs *lang.ErrorList) {
	if k, ok := operandKind(t); ok && !isNumericKind(k) {
		errs.Addf(lang.ErrType, e.Span().Start,
			"%s: operand must be a number, got %s", op, t)
	}
}

// checkOrderingOperand reports an ordering operand that is neither a
// number nor a string. An operand that is itself a comparison gets the
// chained-comparison message, since `a < b < c` is the mistake being
// made; `(a < b) < c` is equally meaningless either way.
func checkOrderingOperand(op string, e lang.Expr, t Type, errs *lang.ErrorList) {
	k, ok := operandKind(t)
	if !ok || isNumericKind(k) || k == String {
		return
	}
	if isComparison(e) {
		errs.Addf(lang.ErrType, e.Span().Start,
			"%s: comparisons do not chain; combine two comparisons with &&", op)
		return
	}
	errs.Addf(lang.ErrType, e.Span().Start,
		"%s: operand must be a number or a string, got %s", op, t)
}

// checkOperandFamilies reports two known operands that are each valid
// for `+` or an ordering but do not belong to the same family: one
// string and one number never combine.
func checkOperandFamilies(in *lang.Infix, left, right Type, errs *lang.ErrorList) {
	lk, lok := operandKind(left)
	rk, rok := operandKind(right)
	if !lok || !rok {
		return
	}
	lValid := isNumericKind(lk) || lk == String
	rValid := isNumericKind(rk) || rk == String
	if !lValid || !rValid {
		return
	}
	if (lk == String) != (rk == String) {
		errs.Addf(lang.ErrType, in.S.Start,
			"%s: operands must both be numbers or both be strings, got %s and %s",
			in.Op, left, right)
	}
}

// checkEqualityOperands reports an equality whose operand types can
// never match, which makes the whole expression a constant. Null on
// either side stays legal: comparing against null is how a program
// tests an optional value.
func checkEqualityOperands(in *lang.Infix, left, right Type, errs *lang.ErrorList) {
	if !left.IsKnown() || !right.IsKnown() {
		return
	}
	if left.Unwrap().Kind == Null || right.Unwrap().Kind == Null {
		return
	}
	if Assignable(left, right) || Assignable(right, left) {
		return
	}
	if isComparison(in.Left) || isComparison(in.Right) {
		errs.Addf(lang.ErrType, in.S.Start,
			"%s: comparisons do not chain; combine two comparisons with &&", in.Op)
		return
	}
	verdict := "false"
	if in.Op == "!=" {
		verdict = "true"
	}
	errs.Addf(lang.ErrType, in.S.Start,
		"%s: comparing %s with %s is always %s", in.Op, left, right, verdict)
}

func isComparison(e lang.Expr) bool {
	in, ok := e.(*lang.Infix)
	if !ok {
		return false
	}
	switch in.Op {
	case "==", "!=", "<", "<=", ">", ">=":
		return true
	}
	return false
}

func inferPrefix(p *lang.Prefix, scope *Scope, errs *lang.ErrorList) Type {
	inner := Infer(p.Expr, TUnknown(), scope, errs)
	switch p.Op {
	case "!":
		checkBooleanOperand(p.Op, p.Expr, inner, errs)
		return TBoolean()
	case "-":
		checkNumericOperand(p.Op, p.Expr, inner, errs)
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
	// Anything joined with opaque is opaque: the result may be the
	// opaque side, so nothing could be read from it either way.
	if a.Kind == Opaque || b.Kind == Opaque {
		return TOpaque(), true
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
		case List, Map:
			j, ok := join(*a.Elem, *b.Elem)
			if !ok {
				return Type{}, false
			}
			return Type{Kind: a.Kind, Elem: &j}, true
		}
	}
	// An object literal joins with a map when its fields fit the
	// map's elements; `if x == null then {} else x` discharging an
	// optional map is the common case.
	if a.Kind == Map && b.Kind == Object && Assignable(a, b) {
		return a, true
	}
	if b.Kind == Map && a.Kind == Object && Assignable(b, a) {
		return b, true
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
