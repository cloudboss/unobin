package goschema

import (
	"fmt"
	"go/ast"
	"go/token"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
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
	scope := constraintScope{}
	if name, ok := receiverName(method); ok {
		scope[name] = scopeRoot{w: w, typeName: typeName, prefix: "var"}
	}
	var out []lang.ConstraintSpec
	for _, call := range constraintCalls(method.Body) {
		base, message := peelMessage(call)
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
// identifier stands for in a rendered reference: var for the receiver,
// the splatted list reference (var.replicas[*]) for a ForEach element.
type scopeRoot struct {
	w        *walker
	typeName string
	prefix   string
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

// constraintCalls returns the constructor call expressions in the
// returned slice literal of a function body, whether the body is a
// Constraints method's or a ForEach body's. A body that is not a single
// return of a composite literal yields none.
func constraintCalls(body *ast.BlockStmt) []*ast.CallExpr {
	if body == nil {
		return nil
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
			if call, ok := el.(*ast.CallExpr); ok {
				calls = append(calls, call)
			}
		}
	}
	return calls
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

// forEachSpecs renders a constraint.ForEach call into the specs of its body,
// every element field rooted under the list with [*]. The body's
// element parameter joins the scope, so references to the receiver
// still name top-level fields. Set constraints only: a predicate or a
// nested ForEach inside the body records an error.
func (w *walker) forEachSpecs(call *ast.CallExpr, scope constraintScope) []lang.ConstraintSpec {
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
	inner := make(constraintScope, len(scope)+1)
	maps.Copy(inner, scope)
	elem.prefix = listPath + "[*]"
	inner[param] = elem
	var out []lang.ConstraintSpec
	for _, c := range constraintCalls(fl.Body) {
		if w.isForEachCall(c) {
			w.addErrf("a ForEach inside a ForEach is not supported")
			continue
		}
		spec, ok := w.specFromCall(c, inner)
		if !ok {
			continue
		}
		if spec.Kind == "predicate" {
			w.addErrf("ForEach does not support predicate constraints")
			continue
		}
		out = append(out, spec)
	}
	return out
}

// listField resolves ForEach's list argument to its rendered var reference
// (var.replicas) and the scope root of the list's element type.
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
	last.indexes = append(slices.Clone(last.indexes), 0)
	cw, elemType, ok := cw.nestedStruct(typeName, last)
	if !ok {
		w.addErrf("ForEach list %q must be a slice of in-library structs", path)
		return "", scopeRoot{}, false
	}
	return path, scopeRoot{w: cw, typeName: elemType}, true
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
// v.Field selector and mapped to its input name.
func (w *walker) specFromCall(
	call *ast.CallExpr, scope constraintScope,
) (lang.ConstraintSpec, bool) {
	base, message := peelMessage(call)
	sel, ok := base.Fun.(*ast.SelectorExpr)
	if !ok {
		return lang.ConstraintSpec{}, false
	}
	// When(cond).Require(reqs...): the base call is .Require on a When call.
	if sel.Sel.Name == "Require" {
		when, ok := sel.X.(*ast.CallExpr)
		if !ok {
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
		return lang.ConstraintSpec{}, false
	}
	// Must(reqs...) is an unconditional predicate: its when is always true.
	if sel.Sel.Name == "Must" {
		return w.predicateSpec("true", base.Args, message, scope)
	}
	kind, ok := setConstraintKinds[sel.Sel.Name]
	if !ok {
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
		return "", false
	}
	pkg, ok := identName(sel.X)
	if !ok || w.imports[pkg] != constraintPkgPath || sel.Sel.Name != "When" {
		return "", false
	}
	if len(when.Args) != 1 {
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
// malformed call returns ok=false.
func (w *walker) condString(arg ast.Expr, scope constraintScope) (string, bool) {
	call, ok := arg.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := identName(sel.X)
	if !ok || w.imports[pkg] != constraintPkgPath {
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
	case "All":
		return w.joinCond(call, "&&", scope)
	case "Any":
		return w.joinCond(call, "||", scope)
	case "Not":
		return w.notCond(call, scope)
	}
	return "", false
}

func (w *walker) compareCond(
	call *ast.CallExpr, op string, scope constraintScope,
) (string, bool) {
	if len(call.Args) != 2 {
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
	guards := []string{field + " == null"}
	if _, isField := call.Args[1].(*ast.SelectorExpr); isField {
		guards = append(guards, val+" == null")
	}
	return "(" + strings.Join(guards, " || ") + " || " + field + " " + op + " " + val + ")", true
}

func (w *walker) boolCond(call *ast.CallExpr, lit string, scope constraintScope) (string, bool) {
	if len(call.Args) != 1 {
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
		return "", false
	}
	field, ok := w.selectorField(call.Args[0], scope)
	if !ok {
		return "", false
	}
	return "(" + field + " " + op + " null)", true
}

func (w *walker) oneOfCond(call *ast.CallExpr, scope constraintScope) (string, bool) {
	if len(call.Args) < 2 {
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

func (w *walker) joinCond(call *ast.CallExpr, op string, scope constraintScope) (string, bool) {
	if len(call.Args) == 0 {
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
		return "", false
	}
	s, ok := w.condString(call.Args[0], scope)
	if !ok {
		return "", false
	}
	return "!" + s, true
}

// valueString renders a comparison operand: a v.Field selector becomes a
// var reference, and a literal becomes its unobin form.
func (w *walker) valueString(arg ast.Expr, scope constraintScope) (string, bool) {
	switch v := arg.(type) {
	case *ast.SelectorExpr:
		return w.selectorField(v, scope)
	case *ast.BasicLit:
		return basicLitString(v)
	case *ast.Ident:
		if v.Name == "true" || v.Name == "false" {
			return v.Name, true
		}
	case *ast.UnaryExpr:
		if v.Op == token.SUB {
			if bl, ok := v.X.(*ast.BasicLit); ok &&
				(bl.Kind == token.INT || bl.Kind == token.FLOAT) {
				return "-" + bl.Value, true
			}
		}
	}
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

// peelMessage unwraps a trailing .Message("...") call, returning the inner
// constructor call and the message text (empty when absent).
func peelMessage(call *ast.CallExpr) (*ast.CallExpr, string) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Message" {
		return call, ""
	}
	inner, ok := sel.X.(*ast.CallExpr)
	if !ok {
		return call, ""
	}
	msg := ""
	if len(call.Args) == 1 {
		if s, ok := stringLit(call.Args[0]); ok {
			msg = s
		}
	}
	return inner, msg
}

// selectorField reads a v.Field argument and returns the field's var
// reference (var.code.inline), the one spelling a constraint uses for a
// field whether it sits in a fields list or a predicate. The selector's
// root identifier must be in scope; the hops walk from its type into
// nested struct types, joining each hop's kebab name with a dot under
// the root's prefix. A non-selector argument, an out-of-scope root, or
// a chain naming a field that does not exist records an error and
// returns ok=false.
func (w *walker) selectorField(arg ast.Expr, scope constraintScope) (string, bool) {
	sel, ok := arg.(*ast.SelectorExpr)
	if !ok {
		w.addErrf("constraint field must be a struct field selector, got %T", arg)
		return "", false
	}
	root, hops, ok := flattenSelector(sel)
	if !ok {
		w.addErrf("constraint field must be a chain of struct fields, got %T", arg)
		return "", false
	}
	entry, ok := scope[root]
	if !ok {
		w.addErrf("constraint references unknown name %q", root)
		return "", false
	}
	path, ok := entry.w.fieldPath(entry.typeName, hops)
	if !ok {
		w.addErrf("constraint references unknown field %q", hopNames(hops))
		return "", false
	}
	return entry.prefix + "." + path, true
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
	if st.Fields == nil {
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
