package goschema

import (
	"fmt"
	"go/ast"
	"go/token"
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
	names := w.fieldKebabByGoName(typeName)
	var out []lang.ConstraintSpec
	for _, call := range constraintCalls(method) {
		if spec, ok := w.specFromCall(call, names); ok {
			out = append(out, spec)
		}
	}
	return out
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

// constraintCalls returns the constructor call expressions in a
// Constraints method's returned slice literal. A body that is not a
// single return of a composite literal yields none.
func constraintCalls(method *ast.FuncDecl) []*ast.CallExpr {
	if method.Body == nil {
		return nil
	}
	var calls []*ast.CallExpr
	for _, stmt := range method.Body.List {
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

// entryFromCall turns one constructor call into a constraint entry. It
// peels a trailing .Message("...") for the message, then matches the base
// constructor against the set-constraint kinds; predicate constructors
// return ok=false and are left for a later pass. Each argument is read as
// a v.Field selector and mapped to its input name.
func (w *walker) specFromCall(
	call *ast.CallExpr, names map[string]string,
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
		whenStr, ok := w.whenCondition(when, names)
		if !ok {
			return lang.ConstraintSpec{}, false
		}
		return w.predicateSpec(whenStr, base.Args, message, names)
	}
	pkg, ok := identName(sel.X)
	if !ok || w.imports[pkg] != constraintPkgPath {
		return lang.ConstraintSpec{}, false
	}
	// Must(reqs...) is an unconditional predicate: its when is always true.
	if sel.Sel.Name == "Must" {
		return w.predicateSpec("true", base.Args, message, names)
	}
	kind, ok := setConstraintKinds[sel.Sel.Name]
	if !ok {
		return lang.ConstraintSpec{}, false
	}
	fields := make([]string, 0, len(base.Args))
	for _, arg := range base.Args {
		field, ok := w.selectorField(arg, names)
		if !ok {
			return lang.ConstraintSpec{}, false
		}
		fields = append(fields, field)
	}
	return lang.ConstraintSpec{Kind: kind, Fields: fields, Message: message}, true
}

// whenCondition reads the cond from a constraint.When(cond) call and
// renders it to a unobin expression string.
func (w *walker) whenCondition(when *ast.CallExpr, names map[string]string) (string, bool) {
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
	return w.condString(when.Args[0], names)
}

// predicateEntry builds a predicate entry from a rendered when-expression
// string and the require conditions, parsing both into lang expressions so
// they run through the same check a UB predicate does. Requirements join
// with && since every one must hold.
// predicateSpec builds a predicate spec from a rendered when-expression
// string and the require conditions. Requirements join with && since every
// one must hold. The when and require stay as source strings; a check
// parses them with lang.ParseSpecs.
func (w *walker) predicateSpec(
	whenStr string, reqArgs []ast.Expr, message string, names map[string]string,
) (lang.ConstraintSpec, bool) {
	reqs := make([]string, 0, len(reqArgs))
	for _, r := range reqArgs {
		s, ok := w.condString(r, names)
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
func (w *walker) condString(arg ast.Expr, names map[string]string) (string, bool) {
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
		return w.compareCond(call, "==", names)
	case "NotEquals":
		return w.compareCond(call, "!=", names)
	case "AtLeast":
		return w.orderedCond(call, ">=", names)
	case "Above":
		return w.orderedCond(call, ">", names)
	case "Below":
		return w.orderedCond(call, "<", names)
	case "AtMost":
		return w.orderedCond(call, "<=", names)
	case "IsTrue":
		return w.boolCond(call, "true", names)
	case "IsFalse":
		return w.boolCond(call, "false", names)
	case "Present":
		return w.nullCond(call, "!=", names)
	case "Absent":
		return w.nullCond(call, "==", names)
	case "OneOf":
		return w.oneOfCond(call, names)
	case "All":
		return w.joinCond(call, "&&", names)
	case "Any":
		return w.joinCond(call, "||", names)
	case "Not":
		return w.notCond(call, names)
	}
	return "", false
}

func (w *walker) compareCond(
	call *ast.CallExpr, op string, names map[string]string,
) (string, bool) {
	if len(call.Args) != 2 {
		return "", false
	}
	field, ok := w.selectorVar(call.Args[0], names)
	if !ok {
		return "", false
	}
	val, ok := w.valueString(call.Args[1], names)
	if !ok {
		return "", false
	}
	return "(" + field + " " + op + " " + val + ")", true
}

// orderedCond renders a numeric comparison (>=, >, <, <=). A null operand
// makes the condition pass, since null has no order; only field operands
// can be null, so each contributes a null guard.
func (w *walker) orderedCond(
	call *ast.CallExpr, op string, names map[string]string,
) (string, bool) {
	if len(call.Args) != 2 {
		return "", false
	}
	field, ok := w.selectorVar(call.Args[0], names)
	if !ok {
		return "", false
	}
	val, ok := w.valueString(call.Args[1], names)
	if !ok {
		return "", false
	}
	guards := []string{field + " == null"}
	if _, isField := call.Args[1].(*ast.SelectorExpr); isField {
		guards = append(guards, val+" == null")
	}
	return "(" + strings.Join(guards, " || ") + " || " + field + " " + op + " " + val + ")", true
}

func (w *walker) boolCond(call *ast.CallExpr, lit string, names map[string]string) (string, bool) {
	if len(call.Args) != 1 {
		return "", false
	}
	field, ok := w.selectorVar(call.Args[0], names)
	if !ok {
		return "", false
	}
	return "(" + field + " == " + lit + ")", true
}

func (w *walker) nullCond(call *ast.CallExpr, op string, names map[string]string) (string, bool) {
	if len(call.Args) != 1 {
		return "", false
	}
	field, ok := w.selectorVar(call.Args[0], names)
	if !ok {
		return "", false
	}
	return "(" + field + " " + op + " null)", true
}

func (w *walker) oneOfCond(call *ast.CallExpr, names map[string]string) (string, bool) {
	if len(call.Args) < 2 {
		return "", false
	}
	field, ok := w.selectorVar(call.Args[0], names)
	if !ok {
		return "", false
	}
	parts := make([]string, 0, len(call.Args)-1)
	for _, va := range call.Args[1:] {
		val, ok := w.valueString(va, names)
		if !ok {
			return "", false
		}
		parts = append(parts, field+" == "+val)
	}
	return "(" + strings.Join(parts, " || ") + ")", true
}

func (w *walker) joinCond(call *ast.CallExpr, op string, names map[string]string) (string, bool) {
	if len(call.Args) == 0 {
		return "", false
	}
	parts := make([]string, 0, len(call.Args))
	for _, a := range call.Args {
		s, ok := w.condString(a, names)
		if !ok {
			return "", false
		}
		parts = append(parts, s)
	}
	return "(" + strings.Join(parts, " "+op+" ") + ")", true
}

func (w *walker) notCond(call *ast.CallExpr, names map[string]string) (string, bool) {
	if len(call.Args) != 1 {
		return "", false
	}
	s, ok := w.condString(call.Args[0], names)
	if !ok {
		return "", false
	}
	return "!" + s, true
}

// selectorVar renders a v.Field argument as a var.<input-name> reference,
// the form the constraint checker resolves against the input values.
func (w *walker) selectorVar(arg ast.Expr, names map[string]string) (string, bool) {
	kebab, ok := w.selectorField(arg, names)
	if !ok {
		return "", false
	}
	return "var." + kebab, true
}

// valueString renders a comparison operand: a v.Field selector becomes a
// var reference, and a literal becomes its unobin form.
func (w *walker) valueString(arg ast.Expr, names map[string]string) (string, bool) {
	switch v := arg.(type) {
	case *ast.SelectorExpr:
		return w.selectorVar(v, names)
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

// selectorField reads a v.Field argument and returns the field's kebab
// input name. A non-selector argument, or a field absent from names,
// records an error and returns ok=false.
func (w *walker) selectorField(arg ast.Expr, names map[string]string) (string, bool) {
	sel, ok := arg.(*ast.SelectorExpr)
	if !ok {
		if w.errs != nil {
			*w.errs = append(*w.errs,
				fmt.Errorf("constraint field must be a struct field selector, got %T", arg))
		}
		return "", false
	}
	kebab, ok := names[sel.Sel.Name]
	if !ok {
		if w.errs != nil {
			*w.errs = append(*w.errs,
				fmt.Errorf("constraint references unknown field %q", sel.Sel.Name))
		}
		return "", false
	}
	return kebab, true
}
