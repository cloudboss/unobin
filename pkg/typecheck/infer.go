package typecheck

import (
	"maps"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
)

// Scope carries the lexical information the inferrer needs to type
// an expression: local input declarations, an optional @each
// binding, and a callback that returns the output Type for a node
// address (resource/data/action.<alias>.<type>.<name>). LookupNode may
// be nil when the caller has no node table; the walker returns
// Unknown for any node reference in that case.
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
	// Bindings holds comprehension-bound names. They resolve as bare
	// values and as dot-path roots ahead of var/resource/data/action,
	// so an inner binding shadows an outer one.
	Bindings map[string]Type
}

// FuncSig describes a library function to the inferrer: the fixed
// parameter types in order, the element type of a variadic tail (nil
// when the function is not variadic), and the result type. A function
// registered without declared types reads as all-Unknown, which checks
// nothing and infers nothing.
type FuncSig struct {
	Params   []Type
	Variadic *Type
	Result   Type
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
// Key is the index type (integer for lists, string for maps,
// element type for sets). Value is the iterated element type.
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
// the declared result. An argument past a fixed signature checks
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
	for i, arg := range c.Args {
		target := TUnknown()
		switch {
		case i < len(sig.Params):
			target = sig.Params[i]
		case sig.Variadic != nil:
			target = *sig.Variadic
		}
		Check(arg, target, scope, errs)
	}
	return sig.Result
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
// be null. Unknown and any fail open, so a value the inferrer cannot
// reason about is left to the runtime guard in evalInterpolated.
func checkInterpolatedSlot(t Type, pos lang.Position, errs *lang.ErrorList) {
	switch t.Kind {
	case Unknown, Any, String, Integer, Number, Boolean:
		return
	case Null, Optional:
		errs.Addf(lang.ErrType, pos,
			"interpolation slot may be null; narrow it before interpolating (got %s)", t)
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
	if srcT.Unwrap().Kind == Set {
		errs.Addf(lang.ErrType, c.Source.Span().Start, "comprehension source cannot be a set")
	}
	key, elem := comprehensionBindingTypes(srcT)
	child := scope.withBindings(c.Names, key, elem)
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
		if t, ok := scope.Bindings[dp.Root.Name]; ok {
			return traverseSegments(t, dp.Segments, errs, false)
		}
	}
	switch dp.Root.Name {
	case "var":
		return inferVar(dp, scope, errs)
	case "resource", "data", "action":
		return inferNode(dp, scope, errs)
	case "local":
		return inferLocal(dp, scope, errs)
	case "@each":
		return inferEach(dp, scope, errs)
	}
	return TUnknown()
}

func inferLocal(dp *lang.DotPath, scope *Scope, errs *lang.ErrorList) Type {
	if scope == nil || scope.LookupLocal == nil || len(dp.Segments) == 0 {
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
	return traverseSegments(t, dp.Segments[1:], errs, false)
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
	// A defaulted optional reads as its inner type: the default
	// replaces a missing or null value before anything reads it.
	if field.Optional && !field.Defaulted {
		t = TOptional(t)
	}
	return traverseSegments(t, dp.Segments[1:], errs, false)
}

func inferNode(dp *lang.DotPath, scope *Scope, errs *lang.ErrorList) Type {
	if scope == nil || scope.LookupNode == nil || len(dp.Segments) < 3 {
		return TUnknown()
	}
	alias := dp.Segments[0].Name
	typ := dp.Segments[1].Name
	name := dp.Segments[2].Name
	if alias == "" || typ == "" || name == "" {
		return TUnknown()
	}
	t, ok := scope.LookupNode(dp.Root.Name, alias, typ, name)
	if !ok {
		return TUnknown()
	}
	rest := dp.Segments[3:]
	if len(rest) > 0 && rest[0].Index != nil && rest[0].Name == "" {
		rest = rest[1:]
	}
	return traverseSegments(t, rest, errs, true)
}

func inferEach(dp *lang.DotPath, scope *Scope, errs *lang.ErrorList) Type {
	if scope == nil || scope.Each == nil || len(dp.Segments) == 0 {
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
	return traverseSegments(t, dp.Segments[1:], errs, false)
}

// traverseSegments walks the trailing field segments after a root
// reference, narrowing the type as it descends. Each .name segment
// looks up an object field; each [expr] segment unwraps a list,
// set, or map element. Returns Unknown when a segment cannot be
// resolved.
//
// skipFirst suppresses the unknown-field diagnostic at segs[0] so
// callers whose first segment is already checked elsewhere (the
// reference checker's `unknown field "x" on <alias>.<type>` message
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
		if seg.Splat {
			var elem Type
			switch current.Kind {
			case List:
				elem = elemOr(current)
			case Any:
				elem = TAny()
			default:
				errs.Addf(lang.ErrType, seg.S.Start, "splat [*] needs a list, got %s", current)
				return TUnknown()
			}
			return TList(traverseSegments(elem, segs[i+1:], errs, false))
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

// inferInfix types a binary operator expression and checks the
// operands the same way the evaluator will: logical operators want
// booleans, arithmetic wants numbers (`+` also takes two strings),
// orderings want two numbers or two strings, and equality wants types
// that could ever match. An operand the inferrer cannot type passes
// unchecked, so the checks mirror the runtime without false positives.
func inferInfix(in *lang.Infix, scope *Scope, errs *lang.ErrorList) Type {
	left := Infer(in.Left, TUnknown(), scope, errs)
	right := Infer(in.Right, TUnknown(), scope, errs)
	switch in.Op {
	case "&&", "||":
		checkBooleanOperand(in.Op, in.Left, left, errs)
		checkBooleanOperand(in.Op, in.Right, right, errs)
		return TBoolean()
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
// about. Optional unwraps, matching Assignable's pre-decode leniency;
// Unknown and any return ok=false so partial information fails open.
func operandKind(t Type) (Kind, bool) {
	k := t.Unwrap().Kind
	if k == Unknown || k == Any {
		return k, false
	}
	return k, true
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
