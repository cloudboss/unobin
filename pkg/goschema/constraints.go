package goschema

import (
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

// constraintPkgPath is the import path of the package whose builders a Go
// library type uses to declare its constraints. The extractor recognizes
// calls qualified by whatever local name that import is bound to.
const constraintPkgPath = "github.com/cloudboss/unobin/pkg/constraint"

// setConstraintKinds maps a pkg/constraint set-constraint constructor to
// the kind the checker uses. Predicate constructors (Must, When) are not
// here; they carry expressions rather than a field list and are handled
// separately, rendered into the same when/require a UB predicate uses.
var setConstraintKinds = map[string]string{
	"ExactlyOneOf":     "exactly-one-of",
	"AtLeastOneOf":     "at-least-one-of",
	"AtMostOneOf":      "at-most-one-of",
	"RequiredTogether": "required-together",
	"RequiredWith":     "required-with",
	"ForbiddenWith":    "forbidden-with",
}

// lookupConstraints resolves a registration's input type and returns the
// constraint entries declared by its Constraints method, each field
// selector mapped to its kebab input name. A type in a subpackage
// (PkgAlias set) is followed the same way lookupFields does.
func (w *walker) lookupConstraints(ref typeRef) []lang.ConstraintSpec {
	cw := w
	if ref.PkgAlias != "" {
		importPath, ok := w.imports[ref.PkgAlias]
		if !ok {
			return nil
		}
		sub := w.sub(importPath)
		if sub == nil {
			return nil
		}
		cw = sub
	}
	return cw.constraintsFromType(ref.TypeName)
}

func (w *walker) constraintsFromType(typeName string) []lang.ConstraintSpec {
	method := findMethod(w.files, typeName, "Constraints")
	if method == nil {
		return nil
	}
	w.subject = typeName
	scope := constraintScope{}
	if name, ok := receiverName(method); ok {
		scope[name] = scopeRoot{w: w, typeName: typeName, prefix: "input"}
	}
	var out []lang.ConstraintSpec
	for _, call := range w.listReturnCalls(
		method.Body, "Constraints method", "constraint", "pkg/constraint") {
		base, message, _ := peelMessage(call)
		if w.isForEachCall(base) {
			if message != "" {
				w.addErrf("Message applies to the constraints inside ForEach, not ForEach itself")
				continue
			}
			out = append(out, w.forEachSpecs(base, scope)...)
			continue
		}
		if spec, ok := w.specFromCall(call, scope); ok {
			out = append(out, spec)
		}
	}
	return out
}

// constraintScope maps each identifier a constraint may root a field
// selector at to the type the selector's hops walk from. Extraction
// seeds it with the Constraints method's receiver.
type constraintScope map[string]scopeRoot

// scopeRoot is the type behind one scope identifier, with the walker
// positioned at the package the type lives in. prefix is what the
// identifier stands for in a rendered reference: input for the receiver,
// the splatted list reference (input.replicas[*]) for a ForEach element.
// A scalar root has no type to select fields from; the identifier
// renders as its prefix alone, the element itself.
type scopeRoot struct {
	w         *walker
	typeName  string
	prefix    string
	scalar    bool
	valueType typecheck.Type
	nullable  bool
}

// receiverName returns the name a method binds its receiver to. ok is
// false for an unnamed or blank receiver, which cannot be referenced.
func receiverName(fn *ast.FuncDecl) (string, bool) {
	if len(fn.Recv.List) == 0 || len(fn.Recv.List[0].Names) == 0 {
		return "", false
	}
	name := fn.Recv.List[0].Names[0].Name
	if name == "" || name == "_" {
		return "", false
	}
	return name, true
}

// fieldKebabByGoName maps each struct field's Go name to its kebab input
// name, the reverse of what the input schema keys on. It turns a
// v.FieldName selector inside a constraint into the input name the
// checker expects.
func (w *walker) fieldKebabByGoName(typeName string) map[string]string {
	spec := findTypeSpec(w.files, typeName)
	if spec == nil {
		return nil
	}
	st, ok := spec.Type.(*ast.StructType)
	if !ok || st.Fields == nil {
		return nil
	}
	out := map[string]string{}
	for _, fld := range st.Fields.List {
		name, skip, _, _ := parseUBFieldTag(fld.Tag)
		if skip {
			continue
		}
		for _, goName := range fld.Names {
			kebab := name
			if kebab == "" {
				kebab = lang.PascalToKebab(goName.Name)
			}
			out[goName.Name] = kebab
		}
	}
	return out
}

// findMethod returns the named method on typeName (pointer or value
// receiver) from the package files, or nil.
func findMethod(files []*ast.File, typeName, methodName string) *ast.FuncDecl {
	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != methodName {
				continue
			}
			if receiverType(fn) == typeName {
				return fn
			}
		}
	}
	return nil
}

func receiverType(fn *ast.FuncDecl) string {
	if len(fn.Recv.List) == 0 {
		return ""
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// listReturnCalls returns the constructor call expressions in the
// returned slice literal of a function body. context names the body and
// elem the kind of element it declares (with pkgLabel its constructor
// package) for diagnostics. A body that is not a single return of a
// composite literal yields only what its conforming returns hold, with
// a warning; an element that is not a constructor call warns and is
// skipped.
func (w *walker) listReturnCalls(
	body *ast.BlockStmt, context, elem, pkgLabel string,
) []*ast.CallExpr {
	if body == nil {
		return nil
	}
	if !isSingleListReturn(body) {
		w.addWarnf("the %s must be a single return of a %s list", context, elem)
	}
	var calls []*ast.CallExpr
	for _, stmt := range body.List {
		ret, ok := stmt.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			continue
		}
		lit, ok := ret.Results[0].(*ast.CompositeLit)
		if !ok {
			continue
		}
		for _, el := range lit.Elts {
			call, ok := el.(*ast.CallExpr)
			if !ok {
				w.addWarnf("a %s must be a %s constructor call, got %s",
					elem, pkgLabel, renderExpr(el))
				continue
			}
			calls = append(calls, call)
		}
	}
	return calls
}

// isSingleListReturn reports whether a body is exactly one return
// of a composite literal, the only form extraction reads in full.
func isSingleListReturn(body *ast.BlockStmt) bool {
	if len(body.List) != 1 {
		return false
	}
	ret, ok := body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false
	}
	_, ok = ret.Results[0].(*ast.CompositeLit)
	return ok
}

// isForEachCall reports whether a call is constraint.ForEach, with an
// explicit type instantiation (constraint.ForEach[T]) unwrapped.
func (w *walker) isForEachCall(call *ast.CallExpr) bool {
	fun := call.Fun
	if idx, ok := fun.(*ast.IndexExpr); ok {
		fun = idx.X
	}
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "ForEach" {
		return false
	}
	pkg, ok := identName(sel.X)
	return ok && w.imports[pkg] == constraintPkgPath
}

// forEachSpecs renders a constraint.ForEach call into the specs of its
// body. A set constraint's element fields root under the list with
// [*], running through the per-element field expansion; a predicate
// renders the element as @each.value with the list as the spec's
// @for-each, running through the iterating predicate check. References
// to the receiver still name top-level fields either way. An inner
// ForEach lowers its set constraints with every enclosing list
// splatted, which the checker expands level by level; a predicate
// inside lowers to a chained @for-each, each Go parameter a level
// binding read as @param.value.
func (w *walker) forEachSpecs(call *ast.CallExpr, scope constraintScope) []lang.ConstraintSpec {
	return w.forEachSpecsAt(call, scope, scope, nil)
}

func (w *walker) forEachSpecsAt(
	call *ast.CallExpr,
	scope constraintScope,
	chainScope constraintScope,
	chain []lang.ForEachSpecLevel,
) []lang.ConstraintSpec {
	if len(call.Args) != 2 {
		w.addErrf("ForEach takes a list field and a function")
		return nil
	}
	listPath, elem, ok := w.listField(call.Args[0], scope)
	if !ok {
		return nil
	}
	fl, ok := call.Args[1].(*ast.FuncLit)
	if !ok {
		w.addErrf("the ForEach body must be a function literal, got %T", call.Args[1])
		return nil
	}
	param, ok := singleParamName(fl)
	if !ok {
		w.addErrf("the ForEach body must take the element as its one named parameter")
		return nil
	}
	if _, exists := scope[param]; exists {
		w.addErrf("ForEach parameter %q shadows an enclosing name; rename it", param)
		return nil
	}
	levels := append(slices.Clip(chain),
		lang.ForEachSpecLevel{Name: "@" + param, In: chainPath(listPath, scope, chainScope)})

	innerSet := make(constraintScope, len(scope)+1)
	maps.Copy(innerSet, scope)
	splatted := elem
	splatted.prefix = listPath + "[*]"
	innerSet[param] = splatted

	innerChain := make(constraintScope, len(chainScope)+1)
	maps.Copy(innerChain, chainScope)
	chained := elem
	chained.prefix = "@" + param + ".value"
	innerChain[param] = chained

	innerEach := make(constraintScope, len(scope)+1)
	maps.Copy(innerEach, scope)
	bound := elem
	bound.prefix = "@each.value"
	innerEach[param] = bound

	var out []lang.ConstraintSpec
	for _, c := range w.listReturnCalls(fl.Body, "ForEach body", "constraint", "pkg/constraint") {
		base, message, _ := peelMessage(c)
		if w.isForEachCall(base) {
			if message != "" {
				w.addErrf("Message applies to the constraints inside ForEach, not ForEach itself")
				continue
			}
			if elem.scalar {
				w.addErrf("cannot iterate inside %q; its elements are scalars", listPath)
				continue
			}
			out = append(out, w.forEachSpecsAt(base, innerSet, innerChain, levels)...)
			continue
		}
		if isPredicateCall(c) {
			if len(chain) > 0 {
				spec, ok := w.specFromCall(c, innerChain)
				if !ok {
					continue
				}
				spec.ForEachLevels = levels
				out = append(out, spec)
				continue
			}
			spec, ok := w.specFromCall(c, innerEach)
			if !ok {
				continue
			}
			spec.ForEach = listPath
			out = append(out, spec)
			continue
		}
		if elem.scalar {
			w.addErrf(
				"a set constraint cannot iterate %q; its elements are scalars with no fields",
				listPath)
			continue
		}
		spec, ok := w.specFromCall(c, innerSet)
		if !ok {
			continue
		}
		out = append(out, spec)
	}
	return out
}

// chainPath renders a level's iterable for the chained form. A list
// rooted at an enclosing parameter swaps that parameter's splatted
// prefix for its binding (@it.value.subs); one rooted at the receiver
// reads as written.
func chainPath(listPath string, scope, chainScope constraintScope) string {
	for name, chained := range chainScope {
		set, ok := scope[name]
		if !ok || set.prefix == chained.prefix {
			continue
		}
		if rest, ok := strings.CutPrefix(listPath, set.prefix); ok {
			return chained.prefix + rest
		}
	}
	return listPath
}

// isPredicateCall reports whether a constructor call builds a
// predicate: Must, or a When chain completed by Require, with any
// trailing Message peeled first.
func isPredicateCall(call *ast.CallExpr) bool {
	base, _, _ := peelMessage(call)
	sel, ok := base.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return sel.Sel.Name == "Must" || sel.Sel.Name == "Require"
}

// listField resolves ForEach's list argument to its rendered input reference
// (input.replicas) and the scope root of the list's element type.
func (w *walker) listField(arg ast.Expr, scope constraintScope) (string, scopeRoot, bool) {
	sel, ok := arg.(*ast.SelectorExpr)
	if !ok {
		w.addErrf("ForEach list must be a struct field selector, got %T", arg)
		return "", scopeRoot{}, false
	}
	root, hops, ok := flattenSelector(sel)
	if !ok {
		w.addErrf("ForEach list must be a chain of struct fields, got %T", arg)
		return "", scopeRoot{}, false
	}
	entry, ok := scope[root]
	if !ok {
		w.addErrf("ForEach list references unknown name %q", root)
		return "", scopeRoot{}, false
	}
	path, ok := entry.w.fieldPath(entry.typeName, hops)
	if !ok {
		w.addErrf("ForEach list references unknown field %q", hopNames(hops))
		return "", scopeRoot{}, false
	}
	path = entry.prefix + "." + path
	cw, typeName := entry.w, entry.typeName
	for _, hop := range hops[:len(hops)-1] {
		cw, typeName, ok = cw.nestedStruct(typeName, hop)
		if !ok {
			w.addErrf("ForEach list %q must be a slice of in-library structs", path)
			return "", scopeRoot{}, false
		}
	}
	last := hops[len(hops)-1]
	elemHop := last
	elemHop.indexes = append(slices.Clone(last.indexes), 0)
	structWalker, elemType, ok := cw.nestedStruct(typeName, elemHop)
	if !ok {
		w.addErrf("ForEach list %q must be a slice of in-library structs or scalars", path)
		return "", scopeRoot{}, false
	}
	listRef, _ := entry.w.fieldRefFromRoot(entry, hops)
	elemValueType, elemNullable := listElementType(listRef.valueType)
	if _, isScalar := primitiveFromName(elemType); isScalar {
		if elemValueType.Kind == typecheck.Unknown {
			elemValueType, _ = primitiveFromName(elemType)
		}
		return path, scopeRoot{
			w: cw, scalar: true, valueType: elemValueType, nullable: elemNullable,
		}, true
	}
	return path, scopeRoot{
		w: structWalker, typeName: elemType, valueType: elemValueType, nullable: elemNullable,
	}, true
}

// singleParamName returns the name of a function literal's one
// parameter. ok is false for any other parameter list.
func singleParamName(fl *ast.FuncLit) (string, bool) {
	params := fl.Type.Params
	if params == nil || len(params.List) != 1 || len(params.List[0].Names) != 1 {
		return "", false
	}
	name := params.List[0].Names[0].Name
	if name == "" || name == "_" {
		return "", false
	}
	return name, true
}

// specFromCall turns one constructor call into a constraint spec. It
// peels a trailing .Message("...") for the message, then matches the base
// constructor against the set-constraint kinds; predicate constructors
// render through predicateSpec instead. Each argument is read as a
// v.Field selector and mapped to its input name. A constructor that
// cannot be extracted warns and returns ok=false.
func (w *walker) specFromCall(
	call *ast.CallExpr, scope constraintScope,
) (lang.ConstraintSpec, bool) {
	base, message, badMessage := peelMessage(call)
	if badMessage != nil {
		w.addWarnf("Message must be a string literal, got %s", renderExpr(badMessage))
	}
	sel, ok := base.Fun.(*ast.SelectorExpr)
	if !ok {
		w.addWarnf("a constraint must be a pkg/constraint constructor call, got %s",
			renderExpr(base))
		return lang.ConstraintSpec{}, false
	}
	// When(cond).Require(reqs...): the base call is .Require on a When call.
	if sel.Sel.Name == "Require" {
		when, ok := sel.X.(*ast.CallExpr)
		if !ok {
			w.addWarnf("a Require chain must start with constraint.When, got %s",
				renderExpr(sel.X))
			return lang.ConstraintSpec{}, false
		}
		whenStr, ok := w.whenCondition(when, scope)
		if !ok {
			return lang.ConstraintSpec{}, false
		}
		return w.predicateSpec(whenStr, base.Args, message, scope)
	}
	pkg, ok := identName(sel.X)
	if !ok || w.imports[pkg] != constraintPkgPath {
		w.addWarnf("a constraint must be a pkg/constraint constructor call, got %s",
			renderExpr(base))
		return lang.ConstraintSpec{}, false
	}
	// Must(reqs...) is an unconditional predicate: its when is always true.
	if sel.Sel.Name == "Must" {
		return w.predicateSpec("true", base.Args, message, scope)
	}
	kind, ok := setConstraintKinds[sel.Sel.Name]
	if !ok {
		w.addWarnf("unsupported constraint constructor %q", sel.Sel.Name)
		return lang.ConstraintSpec{}, false
	}
	fields := make([]string, 0, len(base.Args))
	for _, arg := range base.Args {
		field, ok := w.selectorField(arg, scope)
		if !ok {
			return lang.ConstraintSpec{}, false
		}
		fields = append(fields, field)
	}
	return lang.ConstraintSpec{Kind: kind, Fields: fields, Message: message}, true
}

// whenCondition reads the cond from a constraint.When(cond) call and
// renders it to a unobin expression string.
func (w *walker) whenCondition(when *ast.CallExpr, scope constraintScope) (string, bool) {
	sel, ok := when.Fun.(*ast.SelectorExpr)
	if !ok {
		w.addWarnf("a Require chain must start with constraint.When, got %s", renderExpr(when))
		return "", false
	}
	pkg, ok := identName(sel.X)
	if !ok || w.imports[pkg] != constraintPkgPath || sel.Sel.Name != "When" {
		w.addWarnf("a Require chain must start with constraint.When, got %s", renderExpr(when))
		return "", false
	}
	if len(when.Args) != 1 {
		w.addWarnf("When takes exactly one condition")
		return "", false
	}
	return w.condString(when.Args[0], scope)
}

// predicateSpec builds a predicate spec from a rendered when-expression
// string and the require conditions. Requirements join with && since every
// one must hold. The when and require stay as source strings; a check
// parses them with lang.ParseSpecs.
func (w *walker) predicateSpec(
	whenStr string, reqArgs []ast.Expr, message string, scope constraintScope,
) (lang.ConstraintSpec, bool) {
	reqs := make([]string, 0, len(reqArgs))
	for _, r := range reqArgs {
		s, ok := w.condString(r, scope)
		if !ok {
			return lang.ConstraintSpec{}, false
		}
		reqs = append(reqs, s)
	}
	if len(reqs) == 0 {
		w.addWarnf("a predicate needs at least one condition")
		return lang.ConstraintSpec{}, false
	}
	return lang.ConstraintSpec{
		Kind:    "predicate",
		When:    whenStr,
		Require: strings.Join(reqs, " && "),
		Message: message,
	}, true
}

// condString renders one condition builder call into a parenthesized
// unobin expression. Nested All/Any/Not recurse. An unrecognized or
// malformed call warns and returns ok=false.
func (w *walker) condString(arg ast.Expr, scope constraintScope) (string, bool) {
	call, ok := arg.(*ast.CallExpr)
	if !ok {
		w.addWarnf("a condition must be a pkg/constraint condition call, got %s",
			renderExpr(arg))
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		w.addWarnf("a condition must be a pkg/constraint condition call, got %s",
			renderExpr(arg))
		return "", false
	}
	pkg, ok := identName(sel.X)
	if !ok || w.imports[pkg] != constraintPkgPath {
		w.addWarnf("a condition must be a pkg/constraint condition call, got %s",
			renderExpr(arg))
		return "", false
	}
	switch sel.Sel.Name {
	case "Equals":
		return w.compareCond(call, "==", scope)
	case "NotEquals":
		return w.compareCond(call, "!=", scope)
	case "AtLeast":
		return w.orderedCond(call, ">=", scope)
	case "Above":
		return w.orderedCond(call, ">", scope)
	case "Below":
		return w.orderedCond(call, "<", scope)
	case "AtMost":
		return w.orderedCond(call, "<=", scope)
	case "IsTrue":
		return w.boolCond(call, "true", scope)
	case "IsFalse":
		return w.boolCond(call, "false", scope)
	case "Present":
		return w.nullCond(call, "!=", scope)
	case "Absent":
		return w.nullCond(call, "==", scope)
	case "OneOf":
		return w.oneOfCond(call, scope)
	case "NotEmpty":
		return w.notEmptyCond(call, scope)
	case "MinItems":
		return w.itemsCond(call, ">=", scope)
	case "MaxItems":
		return w.itemsCond(call, "<=", scope)
	case "All":
		return w.joinCond(call, "&&", scope)
	case "Any":
		return w.joinCond(call, "||", scope)
	case "Not":
		return w.notCond(call, scope)
	}
	w.addWarnf("unsupported condition %q", sel.Sel.Name)
	return "", false
}

func (w *walker) compareCond(
	call *ast.CallExpr, op string, scope constraintScope,
) (string, bool) {
	if len(call.Args) != 2 {
		w.addWarnf("%s takes a field and a value", condName(call))
		return "", false
	}
	field, ok := w.selectorField(call.Args[0], scope)
	if !ok {
		return "", false
	}
	val, ok := w.valueString(call.Args[1], scope)
	if !ok {
		return "", false
	}
	return "(" + field + " " + op + " " + val + ")", true
}

// orderedCond renders a numeric comparison (>=, >, <, <=). A null operand
// makes the condition pass, since null has no order; only field operands
// can be null, so each contributes a null guard.
func (w *walker) orderedCond(
	call *ast.CallExpr, op string, scope constraintScope,
) (string, bool) {
	if len(call.Args) != 2 {
		w.addWarnf("%s takes a field and a value", condName(call))
		return "", false
	}
	field, ok := w.selectorFieldRef(call.Args[0], scope)
	if !ok {
		return "", false
	}
	val, ok := w.valueString(call.Args[1], scope)
	if !ok {
		return "", false
	}
	guards := []string{}
	if field.nullable {
		guards = append(guards, field.expr+" == null")
	}
	if valRef, isField := w.valueFieldRef(call.Args[1], scope); isField && valRef.nullable {
		guards = append(guards, val+" == null")
	}
	check := field.expr + " " + op + " " + val
	if len(guards) == 0 {
		return "(" + check + ")", true
	}
	return "(" + strings.Join(guards, " || ") + " || " + check + ")", true
}

func (w *walker) boolCond(call *ast.CallExpr, lit string, scope constraintScope) (string, bool) {
	if len(call.Args) != 1 {
		w.addWarnf("%s takes one field", condName(call))
		return "", false
	}
	field, ok := w.selectorField(call.Args[0], scope)
	if !ok {
		return "", false
	}
	return "(" + field + " == " + lit + ")", true
}

func (w *walker) nullCond(call *ast.CallExpr, op string, scope constraintScope) (string, bool) {
	if len(call.Args) != 1 {
		w.addWarnf("%s takes one field", condName(call))
		return "", false
	}
	field, ok := w.selectorFieldRef(call.Args[0], scope)
	if !ok {
		return "", false
	}
	if !field.nullable {
		if op == "!=" {
			return "true", true
		}
		return "false", true
	}
	return "(" + field.expr + " " + op + " null)", true
}

func (w *walker) oneOfCond(call *ast.CallExpr, scope constraintScope) (string, bool) {
	if len(call.Args) < 2 {
		w.addWarnf("OneOf needs a field and at least one value")
		return "", false
	}
	field, ok := w.selectorField(call.Args[0], scope)
	if !ok {
		return "", false
	}
	parts := make([]string, 0, len(call.Args)-1)
	for _, va := range call.Args[1:] {
		val, ok := w.valueString(va, scope)
		if !ok {
			return "", false
		}
		parts = append(parts, field+" == "+val)
	}
	return "(" + strings.Join(parts, " || ") + ")", true
}

// notEmptyCond renders NotEmpty: the field must be set and hold at
// least one element, so an explicitly empty list fails.
func (w *walker) notEmptyCond(call *ast.CallExpr, scope constraintScope) (string, bool) {
	if len(call.Args) != 1 {
		w.addWarnf("NotEmpty takes one field")
		return "", false
	}
	field, ok := w.selectorFieldRef(call.Args[0], scope)
	if !ok {
		return "", false
	}
	check := "(@core.length(" + field.expr + ") >= 1)"
	if !field.nullable {
		return check, true
	}
	return "((" + field.expr + " != null) && " + check + ")", true
}

// itemsCond renders MinItems (>=) and MaxItems (<=). A null field
// passes, since presence is Present's job; only the count argument is
// embedded, so it must be a whole-number literal.
func (w *walker) itemsCond(call *ast.CallExpr, op string, scope constraintScope) (string, bool) {
	if len(call.Args) != 2 {
		w.addWarnf("%s takes a field and a whole-number literal", condName(call))
		return "", false
	}
	field, ok := w.selectorFieldRef(call.Args[0], scope)
	if !ok {
		return "", false
	}
	n, ok := intLiteral(call.Args[1])
	if !ok {
		w.addWarnf("%s takes a field and a whole-number literal", condName(call))
		return "", false
	}
	check := "@core.length(" + field.expr + ") " + op + " " + strconv.Itoa(n)
	if !field.nullable {
		return "(" + check + ")", true
	}
	return "(" + field.expr + " == null || " + check + ")", true
}

func (w *walker) joinCond(call *ast.CallExpr, op string, scope constraintScope) (string, bool) {
	if len(call.Args) == 0 {
		w.addWarnf("%s needs at least one condition", condName(call))
		return "", false
	}
	parts := make([]string, 0, len(call.Args))
	for _, a := range call.Args {
		s, ok := w.condString(a, scope)
		if !ok {
			return "", false
		}
		parts = append(parts, s)
	}
	return "(" + strings.Join(parts, " "+op+" ") + ")", true
}

func (w *walker) notCond(call *ast.CallExpr, scope constraintScope) (string, bool) {
	if len(call.Args) != 1 {
		w.addWarnf("Not takes one condition")
		return "", false
	}
	s, ok := w.condString(call.Args[0], scope)
	if !ok {
		return "", false
	}
	return "!" + s, true
}

// valueString renders a comparison operand: a v.Field selector becomes a
// input reference, and a literal becomes its unobin form. Anything else,
// a named constant or a conversion included, has no value in the
// source, so it warns and returns ok=false.
func (w *walker) valueString(arg ast.Expr, scope constraintScope) (string, bool) {
	switch v := arg.(type) {
	case *ast.SelectorExpr:
		return w.selectorField(v, scope)
	case *ast.BasicLit:
		if s, ok := basicLitString(v); ok {
			return s, true
		}
	case *ast.Ident:
		if v.Name == "true" || v.Name == "false" {
			return v.Name, true
		}
		if entry, found := scope[v.Name]; found && entry.scalar {
			return entry.prefix, true
		}
	case *ast.UnaryExpr:
		if v.Op == token.SUB {
			if bl, ok := v.X.(*ast.BasicLit); ok &&
				(bl.Kind == token.INT || bl.Kind == token.FLOAT) {
				return "-" + bl.Value, true
			}
		}
	}
	w.addWarnf("a condition value must be a literal or a field, got %s", renderExpr(arg))
	return "", false
}

// basicLitString renders a Go string, int, or float literal in unobin
// form: a Go double-quoted string becomes a single-quoted unobin string;
// numbers pass through unchanged.
func basicLitString(bl *ast.BasicLit) (string, bool) {
	switch bl.Kind {
	case token.STRING:
		s, err := strconv.Unquote(bl.Value)
		if err != nil {
			return "", false
		}
		return "'" + s + "'", true
	case token.INT, token.FLOAT:
		return bl.Value, true
	}
	return "", false
}

// peelMessage unwraps a trailing .Message("...") call, returning the
// inner constructor call and the message text (empty when absent). The
// argument may concatenate string literals; the constant fold joins
// them. An argument with no constant value comes back as bad, so the
// caller can warn; the message is then empty and the rule still
// extracts.
func peelMessage(call *ast.CallExpr) (inner *ast.CallExpr, msg string, bad ast.Expr) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Message" {
		return call, "", nil
	}
	inner, ok = sel.X.(*ast.CallExpr)
	if !ok {
		return call, "", nil
	}
	if len(call.Args) != 1 {
		return inner, "", call
	}
	if s, ok := stringConstant(call.Args[0]); ok {
		return inner, s, nil
	}
	return inner, "", call.Args[0]
}

// stringConstant folds an expression to its constant string value: a
// string literal, a parenthesized constant, or a + concatenation of
// constants. ok is false for anything the fold cannot reduce.
func stringConstant(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		return stringLit(v)
	case *ast.ParenExpr:
		return stringConstant(v.X)
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		left, ok := stringConstant(v.X)
		if !ok {
			return "", false
		}
		right, ok := stringConstant(v.Y)
		if !ok {
			return "", false
		}
		return left + right, true
	}
	return "", false
}

// selectorField reads a v.Field argument and returns the field's input
// reference (input.code.inline), the one spelling a constraint uses for a
// field whether it sits in a fields list or a predicate. The selector's
// root identifier must be in scope; the hops walk from its type into
// nested struct types, joining each hop's kebab name with a dot under
// the root's prefix. A non-selector argument, an out-of-scope root, or
// a chain naming a field that does not exist records an error and
// returns ok=false.
func (w *walker) selectorField(arg ast.Expr, scope constraintScope) (string, bool) {
	field, ok := w.selectorFieldRef(arg, scope)
	if !ok {
		return "", false
	}
	return field.expr, true
}

type constraintFieldRef struct {
	expr      string
	valueType typecheck.Type
	nullable  bool
}

func (w *walker) selectorFieldRef(
	arg ast.Expr, scope constraintScope,
) (constraintFieldRef, bool) {
	if id, ok := arg.(*ast.Ident); ok {
		if entry, found := scope[id.Name]; found && entry.scalar {
			valueType, nullable := nullableValueType(entry.valueType)
			return constraintFieldRef{
				expr: entry.prefix, valueType: valueType, nullable: entry.nullable || nullable,
			}, true
		}
	}
	sel, ok := arg.(*ast.SelectorExpr)
	if !ok {
		w.addErrf("constraint field must be a struct field selector, got %T", arg)
		return constraintFieldRef{}, false
	}
	root, hops, ok := flattenSelector(sel)
	if !ok {
		w.addErrf("constraint field must be a chain of struct fields, got %T", arg)
		return constraintFieldRef{}, false
	}
	entry, ok := scope[root]
	if !ok {
		w.addErrf("constraint references unknown name %q", root)
		return constraintFieldRef{}, false
	}
	field, ok := entry.w.fieldRefFromRoot(entry, hops)
	if !ok {
		w.addErrf("constraint references unknown field %q", hopNames(hops))
		return constraintFieldRef{}, false
	}
	return field, true
}

func nullableValueType(t typecheck.Type) (typecheck.Type, bool) {
	if t.Kind == typecheck.Optional {
		return t.Unwrap(), true
	}
	return t, false
}

func (w *walker) valueFieldRef(
	arg ast.Expr, scope constraintScope,
) (constraintFieldRef, bool) {
	switch v := arg.(type) {
	case *ast.SelectorExpr:
		return w.selectorFieldRef(v, scope)
	case *ast.Ident:
		if entry, found := scope[v.Name]; found && entry.scalar {
			return w.selectorFieldRef(v, scope)
		}
	}
	return constraintFieldRef{}, false
}

// hopNames joins the Go field names of a selector chain for an error
// message, indexes left out.
func hopNames(hops []selectorHop) string {
	names := make([]string, 0, len(hops))
	for _, hop := range hops {
		names = append(names, hop.name)
	}
	return strings.Join(names, ".")
}

// addErrf records a formatted extraction error when the walker is
// collecting them.
func (w *walker) addErrf(format string, args ...any) {
	if w.errs != nil {
		*w.errs = append(*w.errs, fmt.Errorf(format, args...))
	}
}

// addWarnf records a formatted extraction warning, prefixed with the
// subject under extraction, when the walker is collecting them. A
// warning marks a rule the library's source declares but the schema
// will not enforce, so a library's tests can assert there are none.
func (w *walker) addWarnf(format string, args ...any) {
	if w.warns == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if w.subject != "" {
		msg = w.subject + ": " + msg
	}
	*w.warns = append(*w.warns, msg)
}

// condName returns the constructor name of a condition call for a
// diagnostic, or "condition" when the call has no selector form.
func condName(call *ast.CallExpr) string {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		return sel.Sel.Name
	}
	return "condition"
}

// renderExpr prints an expression as source for a diagnostic.
func renderExpr(e ast.Expr) string {
	var b strings.Builder
	if err := printer.Fprint(&b, token.NewFileSet(), e); err != nil {
		return fmt.Sprintf("%T", e)
	}
	return b.String()
}

// selectorHop is one field selection in a constraint selector chain,
// with any whole-number indexes applied to it, so the Listeners[0] of
// v.Listeners[0].Cert is {name: "Listeners", indexes: [0]}.
type selectorHop struct {
	name    string
	indexes []int
}

// flattenSelector unwinds a selector chain such as v.Code.Inline into
// its root identifier and the field hops in source order ("v",
// [Code, Inline]). A hop may be indexed by whole-number literals
// (v.Listeners[0].Cert). ok is false for anything that is not an
// ident-rooted chain of field selections, an index on the root
// included.
func flattenSelector(sel *ast.SelectorExpr) (string, []selectorHop, bool) {
	hops := []selectorHop{{name: sel.Sel.Name}}
	var pending []int
	for cur := sel.X; ; {
		switch x := cur.(type) {
		case *ast.Ident:
			if len(pending) > 0 {
				return "", nil, false
			}
			slices.Reverse(hops)
			return x.Name, hops, true
		case *ast.SelectorExpr:
			slices.Reverse(pending)
			hops = append(hops, selectorHop{name: x.Sel.Name, indexes: pending})
			pending = nil
			cur = x.X
		case *ast.IndexExpr:
			n, ok := intLiteral(x.Index)
			if !ok {
				return "", nil, false
			}
			pending = append(pending, n)
			cur = x.X
		default:
			return "", nil, false
		}
	}
}

// intLiteral reads an index expression as a whole-number literal.
func intLiteral(e ast.Expr) (int, bool) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.INT {
		return 0, false
	}
	n, err := strconv.Atoi(bl.Value)
	if err != nil {
		return 0, false
	}
	return n, true
}

func (w *walker) fieldRefFromRoot(
	root scopeRoot, hops []selectorHop,
) (constraintFieldRef, bool) {
	cw, typeName := root.w, root.typeName
	parts := make([]string, 0, len(hops))
	nullable := root.nullable
	valueType := root.valueType
	for i, hop := range hops {
		kebab, ok := cw.fieldKebabByGoName(typeName)[hop.name]
		if !ok {
			return constraintFieldRef{}, false
		}
		ft := fieldTypeByGoName(fieldStruct(cw.files, typeName), hop.name)
		if ft == nil {
			return constraintFieldRef{}, false
		}
		valueType, ok = typeAfterSelectorHop(cw.typeFromAST(ft), hop.indexes, &nullable)
		if !ok {
			valueType = typecheck.TUnknown()
		}
		for _, idx := range hop.indexes {
			kebab += "[" + strconv.Itoa(idx) + "]"
		}
		parts = append(parts, kebab)
		if i == len(hops)-1 {
			break
		}
		cw, typeName, ok = cw.nestedStruct(typeName, hop)
		if !ok {
			return constraintFieldRef{}, false
		}
	}
	return constraintFieldRef{
		expr: root.prefix + "." + strings.Join(parts, "."), valueType: valueType,
		nullable: nullable,
	}, true
}

func fieldStruct(files []*ast.File, typeName string) *ast.StructType {
	spec := findTypeSpec(files, typeName)
	if spec == nil {
		return nil
	}
	st, _ := spec.Type.(*ast.StructType)
	return st
}

func typeAfterSelectorHop(
	t typecheck.Type, indexes []int, nullable *bool,
) (typecheck.Type, bool) {
	if t.Kind == typecheck.Optional {
		*nullable = true
		t = t.Unwrap()
	}
	if len(indexes) > 0 {
		*nullable = true
	}
	for range indexes {
		if t.Kind == typecheck.Optional {
			*nullable = true
			t = t.Unwrap()
		}
		if t.Kind != typecheck.List || t.Elem == nil {
			return typecheck.TUnknown(), false
		}
		t = *t.Elem
		if t.Kind == typecheck.Optional {
			*nullable = true
			t = t.Unwrap()
		}
	}
	return t, true
}

func listElementType(t typecheck.Type) (typecheck.Type, bool) {
	if t.Kind != typecheck.List || t.Elem == nil {
		return typecheck.TUnknown(), false
	}
	return nullableValueType(*t.Elem)
}

// fieldPath walks the field hops from a scope root's type, mapping each
// Go name to its kebab name and descending into the nested struct type
// for the next hop, and returns the dotted input path (code.inline,
// listeners[0].cert). ok is false when a hop names no field, or a
// non-final hop does not reach a struct.
func (w *walker) fieldPath(rootType string, hops []selectorHop) (string, bool) {
	cw, typeName := w, rootType
	parts := make([]string, 0, len(hops))
	for i, hop := range hops {
		kebab, ok := cw.fieldKebabByGoName(typeName)[hop.name]
		if !ok {
			return "", false
		}
		for _, idx := range hop.indexes {
			kebab += "[" + strconv.Itoa(idx) + "]"
		}
		parts = append(parts, kebab)
		if i == len(hops)-1 {
			break
		}
		cw, typeName, ok = cw.nestedStruct(typeName, hop)
		if !ok {
			return "", false
		}
	}
	return strings.Join(parts, "."), true
}

// nestedStruct resolves the struct type a hop descends into, following
// a pointer and a subpackage selector the way the schema walk does, and
// stepping into a list's element type once per index on the hop. The
// returned walker is positioned at the package the struct lives in. ok
// is false when the hop does not reach an in-library struct.
func (w *walker) nestedStruct(typeName string, hop selectorHop) (*walker, string, bool) {
	spec := findTypeSpec(w.files, typeName)
	if spec == nil {
		return nil, "", false
	}
	st, ok := spec.Type.(*ast.StructType)
	if !ok {
		return nil, "", false
	}
	ft := fieldTypeByGoName(st, hop.name)
	if ft == nil {
		return nil, "", false
	}
	for range hop.indexes {
		if star, ok := ft.(*ast.StarExpr); ok {
			ft = star.X
		}
		arr, ok := ft.(*ast.ArrayType)
		if !ok {
			return nil, "", false
		}
		ft = arr.Elt
	}
	if star, ok := ft.(*ast.StarExpr); ok {
		ft = star.X
	}
	switch t := ft.(type) {
	case *ast.Ident:
		return w, t.Name, true
	case *ast.SelectorExpr:
		pkg, ok := identName(t.X)
		if !ok {
			return nil, "", false
		}
		sub := w.sub(w.imports[pkg])
		if sub == nil {
			return nil, "", false
		}
		return sub, t.Sel.Name, true
	}
	return nil, "", false
}

// fieldTypeByGoName returns the AST type of the struct field with the
// given Go name, or nil when no such field is declared.
func fieldTypeByGoName(st *ast.StructType, goName string) ast.Expr {
	if st == nil || st.Fields == nil {
		return nil
	}
	for _, fld := range st.Fields.List {
		for _, n := range fld.Names {
			if n.Name == goName {
				return fld.Type
			}
		}
	}
	return nil
}
