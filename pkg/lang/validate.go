package lang

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// allowedTopLevelKeys is the set of identifier keys permitted at the
// top level of each file kind. An exported type body matches a factory
// except for `configurations`, which only the factory root may define.
// A config holds factory identity, state config, locals, input values,
// and library configurations.
var allowedTopLevelKeys = map[FileKind]map[string]topLevelValueKind{
	FileFactory: {
		"description":    topLevelString,
		"inputs":         topLevelObject,
		"locals":         topLevelObject,
		"constraints":    topLevelArray,
		"imports":        topLevelObject,
		"data":           topLevelObject,
		"resources":      topLevelObject,
		"actions":        topLevelObject,
		"outputs":        topLevelObject,
		"configurations": topLevelObject,
	},
	FileExportedType: {
		"description": topLevelString,
		"inputs":      topLevelObject,
		"locals":      topLevelObject,
		"constraints": topLevelArray,
		"imports":     topLevelObject,
		"data":        topLevelObject,
		"resources":   topLevelObject,
		"actions":     topLevelObject,
		"outputs":     topLevelObject,
	},
	FileConfig: {
		"factory":        topLevelObject,
		"parallelism":    topLevelNumber,
		"state":          topLevelObject,
		"encryption":     topLevelObject,
		"locals":         topLevelObject,
		"inputs":         topLevelObject,
		"configurations": topLevelObject,
	},
	FileManifest: {
		"unobin-version": topLevelString,
		"requires":       topLevelObject,
		"replace":        topLevelObject,
	},
}

// topLevelValueKind is the value form a top-level key requires. The
// block validators assume their block's form, so a key whose value
// has the wrong form is an error here rather than a silent skip.
type topLevelValueKind int

const (
	topLevelObject topLevelValueKind = iota
	topLevelArray
	topLevelString
	topLevelNumber
)

func (k topLevelValueKind) matches(e Expr) bool {
	switch k {
	case topLevelObject:
		_, ok := e.(*ObjectLit)
		return ok
	case topLevelArray:
		_, ok := e.(*ArrayLit)
		return ok
	case topLevelString:
		_, ok := e.(*StringLit)
		return ok
	case topLevelNumber:
		_, ok := e.(*NumberLit)
		return ok
	}
	return true
}

func (k topLevelValueKind) String() string {
	switch k {
	case topLevelObject:
		return "an object"
	case topLevelArray:
		return "an array"
	case topLevelString:
		return "a string"
	case topLevelNumber:
		return "a number"
	}
	return "a value"
}

// ValidateTopLevelKeys checks that every top-level field in f.Body uses
// an identifier key permitted for f.Kind, and that no key appears twice.
// Returns the collected errors. f.Kind must already be classified;
// FileUnknown produces a single error directing the caller to classify
// first.
func ValidateTopLevelKeys(f *File) *ErrorList {
	errs := NewErrorList(0)
	if f.Kind == FileUnknown {
		errs.Addf(ErrSchema, f.S.Start,
			"cannot validate top level keys: file kind is unknown "+
				"(classify by filename or caller context first)")
		return errs
	}
	allowed, ok := allowedTopLevelKeys[f.Kind]
	if !ok {
		errs.Addf(ErrSchema, f.S.Start, "no top level key set defined for file kind %s", f.Kind)
		return errs
	}
	seen := make(map[string]Position, len(f.Body.Fields))
	for _, fld := range f.Body.Fields {
		if fld.Key.Kind == FieldString {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"top level key must be an identifier, got quoted string %q", fld.Key.String)
			continue
		}
		name := fld.Key.Name
		if fld.Key.IsMeta() {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"@-prefixed key %q is not allowed at top level", name)
			continue
		}
		want, ok := allowed[name]
		if !ok {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"%q is not a valid top level key for a %s file", name, f.Kind)
			continue
		}
		if prev, dup := seen[name]; dup {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"duplicate top level key %q (first defined at %s)", name, prev)
			continue
		}
		seen[name] = fld.Key.S.Start
		if !want.matches(fld.Value) {
			errs.Addf(ErrSchema, fld.Value.Span().Start,
				"`%s:` must be %s, got %s", name, want, exprKind(fld.Value))
		}
	}
	return errs
}

// inputModifierKeys is the set of modifier keys permitted alongside `type:`
// inside an input declaration.
var inputModifierKeys = map[string]struct{}{
	"type":        {},
	"description": {},
	"pattern":     {},
	"minimum":     {},
	"maximum":     {},
	"min-items":   {},
	"max-items":   {},
	"format":      {},
	"min-length":  {},
	"max-length":  {},
	"enum":        {},
}

// ValidateInputDeclarations checks the shape of an `inputs:` block as it
// appears in a stack or exported-type body. Every entry must be an
// identifier name bound to an object declaration carrying a `type:`
// expression and any number of permitted modifiers; types are promoted
// here so callers see syntactic and type level errors in one batch.
//
// Config file `inputs:` blocks have a different shape (values, not
// declarations) and are not validated by this function.
func ValidateInputDeclarations(block *ObjectLit) *ErrorList {
	errs := NewErrorList(0)
	seen := make(map[string]Position, len(block.Fields))
	for _, fld := range block.Fields {
		if fld.Key.Kind == FieldString {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"input name must be an identifier, got quoted string %q", fld.Key.String)
			continue
		}
		if fld.Key.IsMeta() {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"@-prefixed key %q is not a valid input name", fld.Key.Name)
			continue
		}
		name := fld.Key.Name
		if prev, dup := seen[name]; dup {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"duplicate input %q (first defined at %s)", name, prev)
			continue
		}
		seen[name] = fld.Key.S.Start
		validateInputDecl(name, fld, errs)
	}
	return errs
}

func validateInputDecl(name string, fld *Field, errs *ErrorList) {
	decl, ok := fld.Value.(*ObjectLit)
	if !ok {
		errs.Addf(ErrSchema, fld.Value.Span().Start,
			"input %q must be an object declaration with a `type:` key", name)
		return
	}
	validateDeclObject(name, decl, true, errs)
}

// validateDeclObject checks one declaration object, top level or
// nested inside an object() type: the key greenlist, duplicates, the
// promoted type, the declared default against the declaration's own
// type and modifiers, and every nested declaration the type contains.
// topLevel admits @sensitive, which has no meaning below the top.
func validateDeclObject(name string, decl *ObjectLit, topLevel bool, errs *ErrorList) {
	var hasType bool
	innerSeen := make(map[string]Position, len(decl.Fields))
	for _, df := range decl.Fields {
		if df.Key.Kind == FieldString {
			errs.Addf(ErrSchema, df.Key.S.Start,
				"input %q: declaration key must be an identifier, got quoted string %q",
				name, df.Key.String)
			continue
		}
		keyName := df.Key.Name
		if df.Key.IsMeta() {
			if keyName == "@sensitive" {
				if !topLevel {
					errs.Addf(ErrSchema, df.Key.S.Start,
						"input %q: @sensitive applies to top-level inputs only", name)
					continue
				}
				if prev, dup := innerSeen[keyName]; dup {
					errs.Addf(ErrSchema, df.Key.S.Start,
						"input %q: duplicate key %q (first defined at %s)", name, keyName, prev)
					continue
				}
				innerSeen[keyName] = df.Key.S.Start
				continue
			}
			errs.Addf(ErrSchema, df.Key.S.Start,
				"input %q: meta key %q is not allowed in an input declaration", name, keyName)
			continue
		}
		if _, ok := inputModifierKeys[keyName]; !ok {
			errs.Addf(ErrSchema, df.Key.S.Start,
				"input %q: unknown modifier %q", name, keyName)
			continue
		}
		if prev, dup := innerSeen[keyName]; dup {
			errs.Addf(ErrSchema, df.Key.S.Start,
				"input %q: duplicate key %q (first defined at %s)", name, keyName, prev)
			continue
		}
		innerSeen[keyName] = df.Key.S.Start
		switch keyName {
		case "type":
			hasType = true
			t, err := PromoteType(df.Value)
			if err != nil {
				if pe, ok := errors.AsType[*Error](err); ok {
					errs.Add(pe)
				} else {
					errs.Addf(ErrType, df.Value.Span().Start, "input %q: %v", name, err)
				}
				continue
			}
			if opt, ok := t.(*TypeOptional); ok && opt.Default != nil {
				checkDefaultIdents(name, opt.Default, nil, errs)
			}
			checkDeclaredDefault(name, decl, t, errs)
			validateNestedDecls(name, t, errs)
		case "enum":
			checkEnumMembers(name, df.Value, errs)
		}
	}
	if !hasType {
		errs.Addf(ErrSchema, decl.S.Start, "input %q: missing required `type:` key", name)
	}
}

// validateNestedDecls walks a promoted type for object fields written
// as full declarations and validates each one, named by its dotted
// path from the input.
func validateNestedDecls(name string, t TypeExpr, errs *ErrorList) {
	switch v := t.(type) {
	case *TypeList:
		validateNestedDecls(name, v.Elem, errs)
	case *TypeMap:
		validateNestedDecls(name, v.Elem, errs)
	case *TypeOptional:
		validateNestedDecls(name, v.Elem, errs)
	case *TypeTuple:
		for _, e := range v.Elements {
			validateNestedDecls(name, e, errs)
		}
	case *TypeObject:
		for _, f := range v.Fields {
			if f.Decl != nil {
				validateDeclObject(name+"."+f.Name, f.Decl, false, errs)
				continue
			}
			if f.Type != nil {
				validateNestedDecls(name+"."+f.Name, f.Type, errs)
			}
		}
	}
}

// checkDeclaredDefault verifies a literal optional() default against
// the declaration's own type and modifiers, so a self-contradictory
// declaration fails at compile instead of on the first omission. A
// computed default is checked when it is applied.
func checkDeclaredDefault(name string, decl *ObjectLit, t TypeExpr, errs *ErrorList) {
	opt, ok := t.(*TypeOptional)
	if !ok || opt.Default == nil {
		return
	}
	val, ok := staticLiteral(opt.Default)
	if !ok || val == nil {
		return
	}
	coerced, err := checkValue(opt.Elem, val, staticDefaultEval)
	if err != nil {
		errs.Addf(ErrSchema, opt.Default.Span().Start, "input %q: default: %v", name, err)
		return
	}
	if err := checkModifiers(decl, coerced); err != nil {
		errs.Addf(ErrSchema, opt.Default.Span().Start, "input %q: default: %v", name, err)
	}
}

func staticDefaultEval(e Expr) (any, error) {
	v, ok := staticLiteral(e)
	if !ok {
		return nil, fmt.Errorf("default is not a literal")
	}
	return v, nil
}

// checkEnumMembers requires every enum member to be a quoted string,
// number, boolean, or null. Enum members are user data, so a bare
// identifier is the unknown-name mistake, not vocabulary.
func checkEnumMembers(name string, e Expr, errs *ErrorList) {
	arr, ok := e.(*ArrayLit)
	if !ok {
		errs.Addf(ErrSchema, e.Span().Start,
			"input %q: enum: must be an array of allowed values", name)
		return
	}
	for _, el := range arr.Elements {
		switch v := el.(type) {
		case *StringLit, *NumberLit, *BoolLit, *NullLit:
		case *Ident:
			errs.Addf(ErrSchema, v.S.Start,
				"input %q: enum: unknown name %q; write '%s' for a string", name, v.Name, v.Name)
		default:
			errs.Addf(ErrSchema, el.Span().Start,
				"input %q: enum: members must be literal values", name)
		}
	}
}

// checkDefaultIdents reports bare identifiers in a default
// expression. A default evaluates against an empty scope, so a bare
// name can only be a comprehension binding; anything else would have
// become a string by accident.
func checkDefaultIdents(name string, e Expr, bound map[string]bool, errs *ErrorList) {
	switch v := e.(type) {
	case *Ident:
		if !bound[v.Name] {
			errs.Addf(ErrSchema, v.S.Start,
				"input %q: default: unknown name %q; write '%s' for a string",
				name, v.Name, v.Name)
		}
	case *ArrayLit:
		for _, el := range v.Elements {
			checkDefaultIdents(name, el, bound, errs)
		}
	case *ObjectLit:
		for _, fld := range v.Fields {
			checkDefaultIdents(name, fld.Value, bound, errs)
		}
	case *Call:
		for _, a := range v.Args {
			checkDefaultIdents(name, a, bound, errs)
		}
	case *Infix:
		checkDefaultIdents(name, v.Left, bound, errs)
		checkDefaultIdents(name, v.Right, bound, errs)
	case *Prefix:
		checkDefaultIdents(name, v.Expr, bound, errs)
	case *Conditional:
		checkDefaultIdents(name, v.Cond, bound, errs)
		checkDefaultIdents(name, v.Then, bound, errs)
		checkDefaultIdents(name, v.Else, bound, errs)
	case *Comprehension:
		checkDefaultIdents(name, v.Source, bound, errs)
		inner := make(map[string]bool, len(bound)+len(v.Names))
		for n := range bound {
			inner[n] = true
		}
		for _, n := range v.Names {
			inner[n] = true
		}
		checkDefaultIdents(name, v.Key, inner, errs)
		checkDefaultIdents(name, v.Value, inner, errs)
		checkDefaultIdents(name, v.Filter, inner, errs)
	case *InterpolatedString:
		for _, part := range v.Parts {
			checkDefaultIdents(name, part.Expr, bound, errs)
		}
	case *DotPath:
		for _, seg := range v.Segments {
			checkDefaultIdents(name, seg.Index, bound, errs)
		}
	case nil:
		return
	}
}

// staticLiteral reduces a literal expression to its value: scalars,
// and arrays or objects of literals. ok is false for anything that
// needs evaluation, including bare identifiers, which name things
// rather than hold values.
func staticLiteral(e Expr) (any, bool) {
	switch v := e.(type) {
	case *StringLit:
		return v.Value, true
	case *NumberLit:
		if v.IsFloat {
			return v.ParsedFloat, true
		}
		return v.ParsedInt, true
	case *BoolLit:
		return v.Value, true
	case *NullLit:
		return nil, true
	case *ArrayLit:
		out := make([]any, len(v.Elements))
		for i, el := range v.Elements {
			val, ok := staticLiteral(el)
			if !ok {
				return nil, false
			}
			out[i] = val
		}
		return out, true
	case *ObjectLit:
		out := make(map[string]any, len(v.Fields))
		for _, fld := range v.Fields {
			key := fld.Key.Name
			if fld.Key.Kind == FieldString {
				key = fld.Key.String
			} else if fld.Key.IsMeta() {
				return nil, false
			}
			val, ok := staticLiteral(fld.Value)
			if !ok {
				return nil, false
			}
			out[key] = val
		}
		return out, true
	}
	return nil, false
}

// ValidateConstraints walks a `constraints:` array and checks each entry
// per its declared `kind:`. Field-based kinds take a nonempty `fields:`
// list of var references, dotted to reach a field inside a nested
// input; the `predicate` kind takes `when:` and `require:` expressions
// plus an optional `message:`.
func ValidateConstraints(arr *ArrayLit) *ErrorList {
	errs := NewErrorList(0)
	for i, e := range arr.Elements {
		validateConstraint(i, e, errs)
	}
	return errs
}

func validateConstraint(idx int, e Expr, errs *ErrorList) {
	obj, ok := e.(*ObjectLit)
	if !ok {
		errs.Addf(ErrSchema, e.Span().Start,
			"constraints[%d]: entry must be an object, got %s", idx, exprKind(e))
		return
	}
	var kindField *Field
	for _, f := range obj.Fields {
		if f.Key.Kind == FieldIdent && f.Key.Name == "kind" {
			kindField = f
			break
		}
	}
	if kindField == nil {
		errs.Addf(ErrSchema, obj.S.Start, "constraints[%d]: missing required `kind:` key", idx)
		return
	}
	kindIdent, ok := kindField.Value.(*Ident)
	if !ok {
		errs.Addf(ErrSchema, kindField.Value.Span().Start,
			"constraints[%d]: `kind:` must be an identifier", idx)
		return
	}
	kind := kindIdent.Name
	switch {
	case kind == "predicate":
		validatePredicateConstraint(idx, obj, errs)
	case isFieldsBasedKind(kind):
		validateFieldsConstraint(idx, kind, obj, errs)
	case kind == "mutually-exclusive":
		errs.Addf(ErrSchema, kindIdent.S.Start,
			"constraints[%d]: unknown constraint kind %q; write at-most-one-of", idx, kind)
	default:
		errs.Addf(ErrSchema, kindIdent.S.Start,
			"constraints[%d]: unknown constraint kind %q", idx, kind)
	}
}

func isFieldsBasedKind(s string) bool {
	_, ok := fieldsConstraintCheckers[s]
	return ok
}

func validateFieldsConstraint(idx int, kind string, obj *ObjectLit, errs *ErrorList) {
	var fieldsField *Field
	seen := make(map[string]Position, len(obj.Fields))
	for _, f := range obj.Fields {
		if !validateConstraintCommonKey(idx, f, seen, errs) {
			continue
		}
		switch f.Key.Name {
		case "kind":
			// Already handled.
		case "fields":
			fieldsField = f
		default:
			errs.Addf(ErrSchema, f.Key.S.Start,
				"constraints[%d]: unknown key %q for kind %q", idx, f.Key.Name, kind)
		}
	}
	if fieldsField == nil {
		errs.Addf(ErrSchema, obj.S.Start,
			"constraints[%d]: %q requires a `fields:` list", idx, kind)
		return
	}
	arr, ok := fieldsField.Value.(*ArrayLit)
	if !ok {
		errs.Addf(ErrSchema, fieldsField.Value.Span().Start,
			"constraints[%d]: `fields:` must be an array of var references", idx)
		return
	}
	if len(arr.Elements) == 0 {
		errs.Addf(ErrSchema, arr.S.Start,
			"constraints[%d]: `fields:` must not be empty", idx)
		return
	}
	names := make([]string, 0, len(arr.Elements))
	hasSplat := false
	for j, el := range arr.Elements {
		name, ok := constraintFieldName(el)
		if !ok {
			errs.Addf(ErrSchema, el.Span().Start,
				"constraints[%d].fields[%d]: %s", idx, j, fieldNameProblem(el))
			continue
		}
		if strings.Contains(name, splatMarker) {
			hasSplat = true
		}
		names = append(names, name)
	}
	if hasSplat && len(names) == len(arr.Elements) {
		if msg := splatRuleViolation(names); msg != "" {
			errs.Addf(ErrSchema, arr.S.Start, "constraints[%d]: %s", idx, msg)
		}
	}
}

// fieldNameProblem describes why a `fields:` element does not render to
// a var reference, with a pointed message for the bare-name, splat, and
// index mistakes a constraint invites.
func fieldNameProblem(e Expr) string {
	const generic = "must be a var reference to an input, like var.vpc-id"
	switch v := e.(type) {
	case *Ident:
		return fmt.Sprintf("must be a var reference: write var.%s", v.Name)
	case *DotPath:
		if v.Root == nil {
			return generic
		}
		if v.Root.Name != "var" {
			return fmt.Sprintf("must be a var reference: write var.%s", dotPathString(v))
		}
		splats := 0
		for i, seg := range v.Segments {
			if seg.Splat {
				splats++
				if splats > 1 {
					return "only one [*] is allowed in a field"
				}
				if i == len(v.Segments)-1 {
					return "splat [*] must be followed by a field, like var.replicas[*].host"
				}
				continue
			}
			if seg.Index != nil {
				if _, ok := literalIndex(seg.Index); !ok {
					return "a list index in a field must be a whole number, like var.listeners[0]"
				}
			}
		}
		return generic
	}
	return generic
}

func validatePredicateConstraint(idx int, obj *ObjectLit, errs *ErrorList) {
	var hasWhen, hasRequire bool
	seen := make(map[string]Position, len(obj.Fields))
	for _, f := range obj.Fields {
		// @for-each is the one meta key a predicate takes: it iterates
		// the when/require pair per element, binding @each in the bare
		// form or the named level bindings in the chained one.
		if f.Key.Kind == FieldIdent && f.Key.Name == "@for-each" {
			if prev, dup := seen[f.Key.Name]; dup {
				errs.Addf(ErrSchema, f.Key.S.Start,
					"constraints[%d]: duplicate key %q (first defined at %s)",
					idx, f.Key.Name, prev)
				continue
			}
			seen[f.Key.Name] = f.Key.S.Start
			if arr, ok := f.Value.(*ArrayLit); ok {
				validateForEachChain(idx, arr, errs)
			}
			continue
		}
		if !validateConstraintCommonKey(idx, f, seen, errs) {
			continue
		}
		switch f.Key.Name {
		case "kind":
			// Already handled.
		case "when":
			hasWhen = true
		case "require":
			hasRequire = true
		case "message":
			// Optional, no shape check at this level.
		default:
			errs.Addf(ErrSchema, f.Key.S.Start,
				"constraints[%d]: unknown key %q for kind \"predicate\"", idx, f.Key.Name)
		}
	}
	if !hasWhen {
		errs.Addf(ErrSchema, obj.S.Start,
			"constraints[%d]: predicate requires a `when:` expression", idx)
	}
	if !hasRequire {
		errs.Addf(ErrSchema, obj.S.Start,
			"constraints[%d]: predicate requires a `require:` expression", idx)
	}
}

// validateForEachChain checks the chained @for-each form: one level
// after another, each binding one fresh @-name to an iterable.
func validateForEachChain(idx int, arr *ArrayLit, errs *ErrorList) {
	if len(arr.Elements) == 0 {
		errs.Addf(ErrSchema, arr.S.Start,
			"constraints[%d]: a chained @for-each needs at least one level", idx)
		return
	}
	declared := make(map[string]Position, len(arr.Elements))
	for _, el := range arr.Elements {
		obj, ok := el.(*ObjectLit)
		if !ok || len(obj.Fields) != 1 || obj.Fields[0].Key.Kind != FieldIdent {
			errs.Addf(ErrSchema, el.Span().Start,
				"constraints[%d]: a chain level binds one @-name to an iterable,"+
					" like { @rule: var.rules }", idx)
			continue
		}
		key := obj.Fields[0].Key
		switch {
		case !strings.HasPrefix(key.Name, "@"):
			errs.Addf(ErrSchema, key.S.Start,
				"constraints[%d]: a chain level's binding must be @-named, like @%s",
				idx, key.Name)
		case key.Name == "@each":
			errs.Addf(ErrSchema, key.S.Start,
				"constraints[%d]: @each is the bare form's binding; give this level"+
					" its own name", idx)
		case key.Name == CoreNamespace:
			errs.Addf(ErrSchema, key.S.Start,
				"constraints[%d]: %s is reserved; choose another binding name",
				idx, CoreNamespace)
		default:
			if prev, dup := declared[key.Name]; dup {
				errs.Addf(ErrSchema, key.S.Start,
					"constraints[%d]: duplicate binding %q (first defined at %s)",
					idx, key.Name, prev)
				continue
			}
			declared[key.Name] = key.S.Start
		}
	}
}

// ValidateOutputs checks an `outputs:` block. Every entry is a
// bare identifier name bound to an object wrapper of the form
// `{ value: expr }`, optionally carrying `description: '...'` and
// `@sensitive: true`. The wrapper exists so per-output metadata
// keys can ride alongside the value without ambiguity.
func ValidateOutputs(block *ObjectLit) *ErrorList {
	errs := NewErrorList(0)
	seen := make(map[string]Position, len(block.Fields))
	for _, fld := range block.Fields {
		if fld.Key.Kind == FieldString {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"output name must be a bare identifier, got quoted string %q", fld.Key.String)
			continue
		}
		if fld.Key.IsMeta() {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"@-prefixed key %q is not a valid output name", fld.Key.Name)
			continue
		}
		name := fld.Key.Name
		if prev, dup := seen[name]; dup {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"duplicate output %q (first defined at %s)", name, prev)
			continue
		}
		seen[name] = fld.Key.S.Start
		validateOutputEntry(name, fld.Value, errs)
	}
	return errs
}

// validateOutputEntry enforces the wrapper shape on one output
// entry's value. The value must be an object literal carrying a
// `value:` key plus, optionally, a string-literal `description:`
// and `@sensitive: true`.
func validateOutputEntry(name string, value Expr, errs *ErrorList) {
	obj, ok := value.(*ObjectLit)
	if !ok {
		errs.Addf(ErrSchema, value.Span().Start,
			"output %q: value must be a wrapper object of the form { value: <expr> }", name)
		return
	}
	var hasValue bool
	innerSeen := make(map[string]Position, len(obj.Fields))
	for _, df := range obj.Fields {
		keyName := df.Key.Name
		if df.Key.IsMeta() {
			if keyName != "@sensitive" {
				errs.Addf(ErrSchema, df.Key.S.Start,
					"output %q: unknown meta key %q", name, keyName)
				continue
			}
			if prev, dup := innerSeen[keyName]; dup {
				errs.Addf(ErrSchema, df.Key.S.Start,
					"output %q: duplicate key %q (first defined at %s)", name, keyName, prev)
				continue
			}
			innerSeen[keyName] = df.Key.S.Start
			if _, ok := df.Value.(*BoolLit); !ok {
				errs.Addf(ErrType, df.Value.Span().Start,
					"output %q: %s must be a boolean literal", name, keyName)
			}
			continue
		}
		if df.Key.Kind == FieldString {
			errs.Addf(ErrSchema, df.Key.S.Start,
				"output %q: wrapper key must be an identifier, got quoted string %q",
				name, df.Key.String)
			continue
		}
		if keyName != "value" && keyName != "description" {
			errs.Addf(ErrSchema, df.Key.S.Start,
				"output %q: unknown wrapper key %q (allowed: value, description)", name, keyName)
			continue
		}
		if prev, dup := innerSeen[keyName]; dup {
			errs.Addf(ErrSchema, df.Key.S.Start,
				"output %q: duplicate key %q (first defined at %s)", name, keyName, prev)
			continue
		}
		innerSeen[keyName] = df.Key.S.Start
		if keyName == "description" {
			if _, ok := df.Value.(*StringLit); !ok {
				errs.Addf(ErrType, df.Value.Span().Start,
					"output %q: description must be a string literal", name)
			}
			continue
		}
		hasValue = true
	}
	if !hasValue {
		errs.Addf(ErrSchema, obj.S.Start,
			"output %q: wrapper missing required `value:` key", name)
	}
}

// ValidateLocals checks a `locals:` block. Every entry is a bare
// identifier name bound to an arbitrary expression; a local's type is
// inferred from its value, never declared. Names must be unique. The
// entry is referenced elsewhere as `local.<name>`. The value
// expression's own validity (references, cycles) is checked in later
// passes, not here.
func ValidateLocals(block *ObjectLit) *ErrorList {
	errs := NewErrorList(0)
	seen := make(map[string]Position, len(block.Fields))
	for _, fld := range block.Fields {
		if fld.Key.Kind == FieldString {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"local name must be a bare identifier, got quoted string %q", fld.Key.String)
			continue
		}
		if fld.Key.IsMeta() {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"@-prefixed key %q is not a valid local name", fld.Key.Name)
			continue
		}
		name := fld.Key.Name
		if prev, dup := seen[name]; dup {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"duplicate local %q (first defined at %s)", name, prev)
			continue
		}
		seen[name] = fld.Key.S.Start
	}
	return errs
}

// ValidateConstraintReferences checks that every var reference in the
// `fields:` list of each constraint names a declared input. A reference
// is checked by the segment after var, the input the path starts from,
// with any [N] or [*] suffix set aside. Malformed entries are skipped.
func ValidateConstraintReferences(constraints *ArrayLit, inputs *ObjectLit) *ErrorList {
	errs := NewErrorList(0)
	known := make(map[string]struct{}, len(inputs.Fields))
	for _, fld := range inputs.Fields {
		if fld.Key.Kind == FieldIdent && !fld.Key.IsMeta() {
			known[fld.Key.Name] = struct{}{}
		}
	}
	for i, e := range constraints.Elements {
		obj, ok := e.(*ObjectLit)
		if !ok {
			continue
		}
		var fieldsField *Field
		for _, f := range obj.Fields {
			if f.Key.Kind == FieldIdent && f.Key.Name == "fields" {
				fieldsField = f
				break
			}
		}
		if fieldsField == nil {
			continue
		}
		arr, ok := fieldsField.Value.(*ArrayLit)
		if !ok {
			continue
		}
		for j, el := range arr.Elements {
			name, ok := constraintFieldName(el)
			if !ok {
				continue
			}
			rest, ok := strings.CutPrefix(name, "var.")
			if !ok {
				continue
			}
			root, _, _ := strings.Cut(rest, ".")
			root, _, _ = strings.Cut(root, "[")
			if _, exists := known[root]; !exists {
				errs.Addf(ErrResolve, el.Span().Start,
					"constraints[%d].fields[%d]: input %q not declared in `inputs:`",
					i, j, root)
			}
		}
	}
	return errs
}

// ValidateFile runs every schema check appropriate to f.Kind and returns
// the combined diagnostics. The file must already be classified; FileUnknown
// produces only the top-level-keys error directing the caller to classify.
func ValidateFile(f *File) *ErrorList {
	errs := ValidateTopLevelKeys(f)
	if f.Kind == FileUnknown {
		return errs
	}
	blocks := indexTopLevelBlocks(f)
	mergeErrors(errs, ValidateComprehensionBindings(f))
	switch f.Kind {
	case FileFactory, FileExportedType:
		if obj, ok := blocks["inputs"].(*ObjectLit); ok {
			mergeErrors(errs, ValidateInputDeclarations(obj))
		}
		if obj, ok := blocks["locals"].(*ObjectLit); ok {
			mergeErrors(errs, ValidateLocals(obj))
		}
		if arr, ok := blocks["constraints"].(*ArrayLit); ok {
			mergeErrors(errs, ValidateConstraints(arr))
			if iobj, ok := blocks["inputs"].(*ObjectLit); ok {
				mergeErrors(errs, ValidateConstraintReferences(arr, iobj))
			}
		}
		if obj, ok := blocks["imports"].(*ObjectLit); ok {
			mergeErrors(errs, ValidateImports(obj))
		}
		if obj, ok := blocks["resources"].(*ObjectLit); ok {
			mergeErrors(errs, ValidateResources(obj))
		}
		if obj, ok := blocks["data"].(*ObjectLit); ok {
			mergeErrors(errs, ValidateDataSources(obj))
		}
		if obj, ok := blocks["actions"].(*ObjectLit); ok {
			mergeErrors(errs, ValidateActions(obj))
		}
		if obj, ok := blocks["outputs"].(*ObjectLit); ok {
			mergeErrors(errs, ValidateOutputs(obj))
		}
		if f.Kind == FileFactory {
			if obj, ok := blocks["configurations"].(*ObjectLit); ok {
				mergeErrors(errs, ValidateFactoryConfigurations(obj))
			}
		}
		mergeErrors(errs, ValidateCalls(f))
	case FileConfig:
		locals := configLocalNames(blocks["locals"])
		if obj, ok := blocks["locals"].(*ObjectLit); ok {
			mergeErrors(errs, ValidateLocals(obj))
			mergeErrors(errs, ValidateConfigLocals(obj))
		}
		if obj, ok := blocks["inputs"].(*ObjectLit); ok {
			mergeErrors(errs, ValidateConfigInputs(obj, locals))
		}
		if obj, ok := blocks["configurations"].(*ObjectLit); ok {
			mergeErrors(errs, ValidateConfigurations(obj, locals))
		}
		if obj, ok := blocks["state"].(*ObjectLit); ok {
			mergeErrors(errs, ValidateStateConfig(obj, locals))
		}
		if obj, ok := blocks["encryption"].(*ObjectLit); ok {
			mergeErrors(errs, ValidateEncryptionConfig(obj, locals))
		}
	case FileManifest:
		if obj, ok := blocks["requires"].(*ObjectLit); ok {
			mergeErrors(errs, ValidateManifestRequires(obj))
		}
		if obj, ok := blocks["replace"].(*ObjectLit); ok {
			mergeErrors(errs, ValidateManifestReplace(obj))
		}
	}
	return errs
}

// CoreNamespace is the language's function namespace: a call qualified
// with it resolves against the functions the toolchain provides, with
// no import. The @ keeps it outside the alias namespace, so an import
// can never collide with it or stand in for it.
const CoreNamespace = "@core"

// ValidateCalls walks every expression in f and rejects two kinds of
// function call: a bare call with no qualifier (a call names either an
// imported library or @core), and a qualified call whose alias is
// missing from the file's imports block. @core needs no import; any
// other @-name is rejected, since the language provides only @core.
// The type constructors in an input declaration's type (list(string),
// optional(integer, 0)) are left alone: they share call syntax but denote
// types; a default inside such a type stays a value and is still checked.
// Whether a named library function exists is not checked here; that is
// a runtime concern, since a library's function set lives in compiled
// Go code. The @core set is fixed and the reference checker enforces it.
func ValidateCalls(f *File) *ErrorList {
	errs := NewErrorList(0)
	imports := importedAliases(f)
	inType := typeConstructorCalls(f)
	Walk(f.Body, func(e Expr) {
		c, ok := e.(*Call)
		if !ok {
			return
		}
		if _, isType := inType[e]; isType {
			return
		}
		if c.Library == nil {
			pos := c.S.Start
			name := ""
			if c.Callee != nil {
				pos = c.Callee.S.Start
				name = c.Callee.Name
			}
			errs.Addf(ErrResolve, pos,
				"function %q must be qualified with %s or an imported library,"+
					" e.g. %s.%s(...)",
				name, CoreNamespace, CoreNamespace, name)
			return
		}
		if c.Library.Name == CoreNamespace {
			return
		}
		if strings.HasPrefix(c.Library.Name, "@") {
			errs.Addf(ErrResolve, c.Library.S.Start,
				"%q is not a namespace; the language provides only %s",
				c.Library.Name, CoreNamespace)
			return
		}
		if _, declared := imports[c.Library.Name]; !declared {
			errs.Addf(ErrResolve, c.Library.S.Start,
				"library %q is not imported (called as %s.%s)",
				c.Library.Name, c.Library.Name, c.Func.Name)
		}
	})
	return errs
}

// ValidateComprehensionBindings walks every expression in f and
// requires each comprehension to introduce fresh binding names: a name
// may not repeat within one comprehension, and a nested comprehension
// may not rebind a name bound by an enclosing one. A rebound name
// would silently hide the outer value for the rest of the inner body.
// A comprehension in source position binds nothing for its own source,
// so reusing a name there is not shadowing.
func ValidateComprehensionBindings(f *File) *ErrorList {
	errs := NewErrorList(0)
	checkComprehensionBindings(f.Body, map[string]Position{}, errs)
	return errs
}

// checkComprehensionBindings recurses through e with the comprehension
// bindings in scope, each keyed to the position that bound it.
func checkComprehensionBindings(e Expr, bound map[string]Position, errs *ErrorList) {
	switch v := e.(type) {
	case *ObjectLit:
		for _, fld := range v.Fields {
			checkComprehensionBindings(fld.Value, bound, errs)
		}
	case *ArrayLit:
		for _, el := range v.Elements {
			checkComprehensionBindings(el, bound, errs)
		}
	case *Call:
		for _, a := range v.Args {
			checkComprehensionBindings(a, bound, errs)
		}
	case *Infix:
		checkComprehensionBindings(v.Left, bound, errs)
		checkComprehensionBindings(v.Right, bound, errs)
	case *Prefix:
		checkComprehensionBindings(v.Expr, bound, errs)
	case *DotPath:
		for _, seg := range v.Segments {
			checkComprehensionBindings(seg.Index, bound, errs)
		}
	case *Conditional:
		checkComprehensionBindings(v.Cond, bound, errs)
		checkComprehensionBindings(v.Then, bound, errs)
		checkComprehensionBindings(v.Else, bound, errs)
	case *Comprehension:
		checkComprehensionBindings(v.Source, bound, errs)
		inner := make(map[string]Position, len(bound)+len(v.Names))
		maps.Copy(inner, bound)
		for i, n := range v.Names {
			if slices.Contains(v.Names[:i], n) {
				errs.Addf(ErrSchema, v.S.Start, "comprehension binds %s twice", n)
				continue
			}
			if prev, dup := bound[n]; dup {
				errs.Addf(ErrSchema, v.S.Start,
					"binding %s shadows an enclosing comprehension binding"+
						" (bound at %s); rename it", n, prev)
			}
			inner[n] = v.S.Start
		}
		checkComprehensionBindings(v.Key, inner, errs)
		checkComprehensionBindings(v.Value, inner, errs)
		checkComprehensionBindings(v.Filter, inner, errs)
	case *InterpolatedString:
		for _, part := range v.Parts {
			checkComprehensionBindings(part.Expr, bound, errs)
		}
	}
}

// typeConstructorCalls returns the Call nodes that name a type constructor
// (list, map, optional, ...) rather than a function, so the call checker
// leaves them alone. It starts only from input declarations, since that is
// the one place a type can be written, and follows the type sub-grammar
// from each declared type. Default values inside optional(...) and object
// field declarations are values, not types, so their calls are not
// collected and stay subject to the call checker.
func typeConstructorCalls(f *File) map[Expr]struct{} {
	skip := map[Expr]struct{}{}
	inputs, ok := indexTopLevelBlocks(f)["inputs"].(*ObjectLit)
	if !ok {
		return skip
	}
	for _, decl := range inputs.Fields {
		if obj, ok := decl.Value.(*ObjectLit); ok {
			collectTypeConstructors(fieldValue(obj, "type"), skip)
		}
	}
	return skip
}

// collectTypeConstructors adds e to skip if it is a type constructor, then
// recurses into its type arguments. It mirrors promoteCall's structure but
// is tolerant: a malformed type just stops the descent and is reported by
// ValidateInputDeclarations instead.
func collectTypeConstructors(e Expr, skip map[Expr]struct{}) {
	c, ok := e.(*Call)
	if !ok || c.Library != nil || c.Callee == nil || !isTypeConstructor(c.Callee.Name) {
		return
	}
	skip[c] = struct{}{}
	if len(c.Args) == 0 {
		return
	}
	switch c.Callee.Name {
	case "list", "set", "map", "optional", "open":
		// The element or inner type is the first argument; optional's second
		// argument is a default value and is left for the call checker.
		collectTypeConstructors(c.Args[0], skip)
	case "tuple":
		if arr, ok := c.Args[0].(*ArrayLit); ok {
			for _, el := range arr.Elements {
				collectTypeConstructors(el, skip)
			}
		}
	case "object":
		if obj, ok := c.Args[0].(*ObjectLit); ok {
			for _, fld := range obj.Fields {
				collectObjectFieldType(fld, skip)
			}
		}
	}
}

// collectObjectFieldType handles one field of an object(...) type. The
// field value is either a direct type or a nested declaration carrying its
// own type: key; only that type is a type position.
func collectObjectFieldType(f *Field, skip map[Expr]struct{}) {
	if obj, ok := f.Value.(*ObjectLit); ok {
		collectTypeConstructors(fieldValue(obj, "type"), skip)
		return
	}
	collectTypeConstructors(f.Value, skip)
}

// fieldValue returns the value of the bare-identifier field named name, or
// nil when the object has no such field.
func fieldValue(o *ObjectLit, name string) Expr {
	for _, f := range o.Fields {
		if f.Key.Kind == FieldIdent && f.Key.Name == name {
			return f.Value
		}
	}
	return nil
}

// ValidateFactoryConfigurations checks the structure of a factory's
// configurations: block: import alias, then configuration name, then an
// object literal of fields. Field values are ordinary expressions and
// are not constrained here.
func ValidateFactoryConfigurations(block *ObjectLit) *ErrorList {
	errs := NewErrorList(0)
	seen := make(map[string]Position, len(block.Fields))
	for _, fld := range block.Fields {
		if fld.Key.Kind == FieldString {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"configurations import alias must be a bare identifier, got quoted string %q",
				fld.Key.String)
			continue
		}
		if fld.Key.IsMeta() {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"@-prefixed key %q is not a valid import alias", fld.Key.Name)
			continue
		}
		alias := fld.Key.Name
		if prev, dup := seen[alias]; dup {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"duplicate configurations entry %q (first defined at %s)", alias, prev)
			continue
		}
		seen[alias] = fld.Key.S.Start
		obj, ok := fld.Value.(*ObjectLit)
		if !ok {
			errs.Addf(ErrSchema, fld.Value.Span().Start,
				"configurations.%s: must be an object of named configurations", alias)
			continue
		}
		validateConfigurationNames(alias, obj, errs)
	}
	return errs
}

// validateConfigurationNames checks one alias entry of a factory's
// configurations: block: every key names a configuration and binds an
// object literal of fields.
func validateConfigurationNames(alias string, obj *ObjectLit, errs *ErrorList) {
	seen := make(map[string]Position, len(obj.Fields))
	for _, fld := range obj.Fields {
		if fld.Key.Kind == FieldString {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"configurations.%s: configuration name must be a bare identifier, "+
					"got quoted string %q", alias, fld.Key.String)
			continue
		}
		if fld.Key.IsMeta() {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"configurations.%s: @-prefixed key %q is not a valid configuration name",
				alias, fld.Key.Name)
			continue
		}
		name := fld.Key.Name
		if prev, dup := seen[name]; dup {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"configurations.%s: duplicate configuration %q (first defined at %s)",
				alias, name, prev)
			continue
		}
		seen[name] = fld.Key.S.Start
		obj, ok := fld.Value.(*ObjectLit)
		if !ok {
			errs.Addf(ErrSchema, fld.Value.Span().Start,
				"configurations.%s.%s: a configuration must be an object of fields", alias, name)
			continue
		}
		for _, cf := range obj.Fields {
			if cf.Key.IsMeta() {
				errs.Addf(ErrSchema, cf.Key.S.Start,
					"configurations.%s.%s: @-prefixed key %q is not valid in a configuration",
					alias, name, cf.Key.Name)
			}
		}
	}
}

// ValidateConfigInputs checks that every value in a config file's inputs:
// block is a static value; see checkStaticConfigBlock. locals holds the
// names declared by the file's locals: block, referenceable from any
// config value.
func ValidateConfigInputs(block *ObjectLit, locals map[string]bool) *ErrorList {
	return checkStaticConfigBlock(block, staticConfigRules{locals: locals})
}

// ValidateConfigurations applies the static-value rule to a config file's
// configurations: block, with one widening: values may reference inputs
// (var.x), which the runner resolves against the effective inputs before
// decoding. The block nests by import alias and then configuration name;
// the walk recurses through both to the leaf values. locals holds the
// names declared by the file's locals: block.
func ValidateConfigurations(block *ObjectLit, locals map[string]bool) *ErrorList {
	return checkStaticConfigBlock(block, staticConfigRules{locals: locals, allowVar: true})
}

// ValidateConfigLocals checks the values of a config file's locals:
// block. A config local is the file's own scope: a static value that may
// reference other locals, but never inputs. The config supplies input
// values to the factory without being able to read them back, so a var.x
// here is rejected with wording that says why. Locals referencing each
// other in a loop are reported as cycles.
func ValidateConfigLocals(block *ObjectLit) *ErrorList {
	errs := NewErrorList(0)
	rules := staticConfigRules{locals: configLocalNames(block), inLocals: true}
	for _, f := range block.Fields {
		checkConfigValue(f.Value, map[string]bool{}, rules, errs)
	}
	checkConfigLocalCycles(block, errs)
	return errs
}

// configLocalNames collects the declared local names from a locals:
// block expression, tolerating a nil or non-object value so callers can
// pass whatever the top-level index holds.
func configLocalNames(e Expr) map[string]bool {
	obj, ok := e.(*ObjectLit)
	if !ok {
		return nil
	}
	names := make(map[string]bool, len(obj.Fields))
	for _, fld := range obj.Fields {
		if fld.Key.Kind == FieldIdent && !fld.Key.IsMeta() {
			names[fld.Key.Name] = true
		}
	}
	return names
}

// staticConfigRules selects what a config block's values may reference
// beyond literals. locals holds the file's declared local names, which
// any config value may reference. allowVar admits var.x references (the
// configurations block). inLocals marks the locals block itself, which
// rewords a var.x rejection around scope: a local cannot read inputs.
type staticConfigRules struct {
	locals   map[string]bool
	allowVar bool
	inLocals bool
}

// refError picks the rejection wording for a reference that no rule
// admits. root is the dot-path root, or empty for a bare identifier.
func (r staticConfigRules) refError(root string) string {
	if r.inLocals && root == "var" {
		return "a local may not reference %s: inputs are supplied by the config, not in its scope"
	}
	if r.allowVar {
		return "configuration values may reference only inputs and locals, but %s is neither"
	}
	return "config values must be static, but %s is a reference"
}

// checkStaticConfigBlock reports calls and free references in a config
// block's values. Config values are static data: the runner evaluates
// them before any cloud or state I/O, with no library table in scope, so
// a function call or a reference to anything but an input or a local has
// nothing to resolve against. Literals, operators, conditionals, and
// comprehensions over literals are allowed; a comprehension's own bound
// names are in scope inside its body, and the file's declared locals are
// in scope everywhere.
func checkStaticConfigBlock(block *ObjectLit, rules staticConfigRules) *ErrorList {
	errs := NewErrorList(0)
	for _, f := range block.Fields {
		checkConfigValue(f.Value, map[string]bool{}, rules, errs)
	}
	return errs
}

// checkConfigValue reports calls and free references in a config value.
// bound is the set of names a surrounding comprehension brought into
// scope; a bound name shadows the local root, matching evaluation order.
func checkConfigValue(e Expr, bound map[string]bool, rules staticConfigRules, errs *ErrorList) {
	if e == nil {
		return
	}
	switch v := e.(type) {
	case *StringLit, *NumberLit, *BoolLit, *NullLit:
		// A literal is always a valid config value.
	case *ArrayLit:
		for _, el := range v.Elements {
			checkConfigValue(el, bound, rules, errs)
		}
	case *ObjectLit:
		for _, f := range v.Fields {
			checkConfigValue(f.Value, bound, rules, errs)
		}
	case *InterpolatedString:
		for _, p := range v.Parts {
			checkConfigValue(p.Expr, bound, rules, errs)
		}
	case *Infix:
		checkConfigValue(v.Left, bound, rules, errs)
		checkConfigValue(v.Right, bound, rules, errs)
	case *Prefix:
		checkConfigValue(v.Expr, bound, rules, errs)
	case *Conditional:
		checkConfigValue(v.Cond, bound, rules, errs)
		checkConfigValue(v.Then, bound, rules, errs)
		checkConfigValue(v.Else, bound, rules, errs)
	case *Comprehension:
		// The source is evaluated before the binding exists; the key, value,
		// and filter see the bound names.
		checkConfigValue(v.Source, bound, rules, errs)
		inner := withBound(bound, v.Names)
		checkConfigValue(v.Key, inner, rules, errs)
		checkConfigValue(v.Value, inner, rules, errs)
		checkConfigValue(v.Filter, inner, rules, errs)
	case *Ident:
		if !bound[v.Name] {
			errs.Addf(ErrResolve, v.S.Start, rules.refError(""), v.Name)
		}
	case *DotPath:
		root := ""
		if v.Root != nil {
			root = v.Root.Name
		}
		switch {
		case bound[root]:
		case rules.allowVar && root == "var":
		case root == "local":
			name := ""
			if len(v.Segments) > 0 {
				name = v.Segments[0].Name
			}
			if name == "" {
				errs.Addf(ErrResolve, v.S.Start, "local reference needs a name")
				return
			}
			if !rules.locals[name] {
				errs.Addf(ErrResolve, v.S.Start, "unknown local %s", name)
				return
			}
		default:
			errs.Addf(ErrResolve, v.S.Start, rules.refError(root), dotPathString(v))
			return
		}
		for _, seg := range v.Segments {
			checkConfigValue(seg.Index, bound, rules, errs)
		}
	case *Call:
		errs.Addf(ErrResolve, v.S.Start,
			"config values must be static, but %s is a function call", callName(v))
	default:
		errs.Addf(ErrResolve, e.Span().Start, "config inputs must be static values")
	}
}

// checkConfigLocalCycles reports locals that reference themselves
// through the locals block, directly or via other locals. The walk
// mirrors the factory-side check: a depth-first visit over the
// local-to-local edges, reporting at the entry whose visit found the
// loop.
func checkConfigLocalCycles(block *ObjectLit, errs *ErrorList) {
	graph := map[string][]string{}
	pos := map[string]Position{}
	var order []string
	for _, fld := range block.Fields {
		if fld.Key.Kind != FieldIdent || fld.Key.IsMeta() {
			continue
		}
		name := fld.Key.Name
		graph[name] = configLocalRefs(fld.Value)
		pos[name] = fld.Key.S.Start
		order = append(order, name)
	}
	const (
		unvisited = 0
		active    = 1
		done      = 2
	)
	visiting := map[string]int{}
	var visit func(string) bool
	visit = func(name string) bool {
		visiting[name] = active
		for _, ref := range graph[name] {
			if _, isLocal := graph[ref]; !isLocal {
				continue
			}
			if visiting[ref] == active {
				return true
			}
			if visiting[ref] == unvisited && visit(ref) {
				return true
			}
		}
		visiting[name] = done
		return false
	}
	for _, name := range order {
		if visiting[name] == unvisited && visit(name) {
			errs.Addf(ErrResolve, pos[name], "local %s is part of a cycle", name)
		}
	}
}

// configLocalRefs collects the local names an expression references.
func configLocalRefs(e Expr) []string {
	var names []string
	Walk(e, func(x Expr) {
		dp, ok := x.(*DotPath)
		if !ok || dp.Root == nil || dp.Root.Name != "local" {
			return
		}
		if len(dp.Segments) > 0 && dp.Segments[0].Name != "" {
			names = append(names, dp.Segments[0].Name)
		}
	})
	return names
}

// withBound returns bound extended with names. It copies so a sibling
// expression does not see a comprehension's bindings.
func withBound(bound map[string]bool, names []string) map[string]bool {
	if len(names) == 0 {
		return bound
	}
	out := make(map[string]bool, len(bound)+len(names))
	for k := range bound {
		out[k] = true
	}
	for _, n := range names {
		out[n] = true
	}
	return out
}

func callName(c *Call) string {
	if c.Library != nil && c.Func != nil {
		return c.Library.Name + "." + c.Func.Name
	}
	if c.Callee != nil {
		return c.Callee.Name
	}
	return "call"
}

func dotPathString(d *DotPath) string {
	if d.Root == nil {
		return "reference"
	}
	parts := []string{d.Root.Name}
	for _, seg := range d.Segments {
		if seg.Name != "" {
			parts = append(parts, seg.Name)
		}
	}
	return strings.Join(parts, ".")
}

func importedAliases(f *File) map[string]struct{} {
	out := map[string]struct{}{}
	if f.Body == nil {
		return out
	}
	for _, fld := range f.Body.Fields {
		if fld.Key.Kind != FieldIdent || fld.Key.Name != "imports" {
			continue
		}
		obj, ok := fld.Value.(*ObjectLit)
		if !ok {
			return out
		}
		for _, imp := range obj.Fields {
			if imp.Key.Kind == FieldIdent && !imp.Key.IsMeta() {
				out[imp.Key.Name] = struct{}{}
			}
		}
		return out
	}
	return out
}

func indexTopLevelBlocks(f *File) map[string]Expr {
	out := make(map[string]Expr, len(f.Body.Fields))
	for _, fld := range f.Body.Fields {
		if fld.Key.Kind == FieldIdent && !fld.Key.IsMeta() {
			out[fld.Key.Name] = fld.Value
		}
	}
	return out
}

func mergeErrors(dst, src *ErrorList) {
	for _, e := range src.Errors() {
		dst.Add(e)
	}
}

// ValidateImports checks an `imports:` block: every entry is an
// identifier alias bound to a quoted string source URL or local path.
func ValidateImports(block *ObjectLit) *ErrorList {
	return validateAliasToString(block, "import", "source URL or local path")
}

// ValidateManifestRequires checks a manifest `requires:` block: every
// entry binds a quoted dependency id (a repo URL with an optional
// `//subdir`) to a quoted version floor. The id and version strings are
// not parsed here; resolution validates the URL and the semver floor.
func ValidateManifestRequires(block *ObjectLit) *ErrorList {
	return validateManifestEntries(block, "requires", "version")
}

// ValidateManifestReplace checks a manifest `replace:` block: every entry
// binds a quoted dependency id (a repo URL) to a quoted local path. The
// id and path strings are not parsed here; resolution validates the URL
// and that the path holds a library.
func ValidateManifestReplace(block *ObjectLit) *ErrorList {
	return validateManifestEntries(block, "replace", "local path")
}

// validateManifestEntries checks a manifest block whose entries bind a
// quoted dependency id to a quoted string value. blockName names the block
// and valueDesc names the value in error messages.
func validateManifestEntries(block *ObjectLit, blockName, valueDesc string) *ErrorList {
	errs := NewErrorList(0)
	seen := make(map[string]Position, len(block.Fields))
	for _, fld := range block.Fields {
		if fld.Key.Kind != FieldString {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"%s: dependency id must be a quoted string, got bare identifier %q",
				blockName, fld.Key.Name)
			continue
		}
		id := fld.Key.String
		if prev, dup := seen[id]; dup {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"%s: duplicate dependency %q (first defined at %s)", blockName, id, prev)
			continue
		}
		seen[id] = fld.Key.S.Start
		if _, ok := fld.Value.(*StringLit); !ok {
			errs.Addf(ErrSchema, fld.Value.Span().Start,
				"%s: dependency %q: %s must be a quoted string, got %s",
				blockName, id, valueDesc, exprKind(fld.Value))
		}
	}
	return errs
}

func validateAliasToString(block *ObjectLit, what, valueDesc string) *ErrorList {
	errs := NewErrorList(0)
	seen := make(map[string]Position, len(block.Fields))
	for _, fld := range block.Fields {
		if fld.Key.Kind == FieldString {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"%s name must be a bare identifier, got quoted string %q",
				what, fld.Key.String)
			continue
		}
		if fld.Key.IsMeta() {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"@-prefixed key %q is not a valid %s name", fld.Key.Name, what)
			continue
		}
		name := fld.Key.Name
		if prev, dup := seen[name]; dup {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"duplicate %s %q (first defined at %s)", what, name, prev)
			continue
		}
		seen[name] = fld.Key.S.Start
		if _, ok := fld.Value.(*StringLit); !ok {
			errs.Addf(ErrSchema, fld.Value.Span().Start,
				"%s %q: value must be a quoted-string %s, got %s",
				what, name, valueDesc, exprKind(fld.Value))
		}
	}
	return errs
}

// validateConstraintCommonKey rejects quoted string keys, `@`-prefixed
// keys, and duplicates - the checks every constraint kind shares before
// per-kind dispatch. Returns false when the field should be skipped.
func validateConstraintCommonKey(
	idx int, f *Field, seen map[string]Position, errs *ErrorList,
) bool {
	if f.Key.Kind == FieldString {
		errs.Addf(ErrSchema, f.Key.S.Start,
			"constraints[%d]: key must be an identifier, got quoted string %q",
			idx, f.Key.String)
		return false
	}
	name := f.Key.Name
	if f.Key.IsMeta() {
		errs.Addf(ErrSchema, f.Key.S.Start,
			"constraints[%d]: meta key %q not allowed", idx, name)
		return false
	}
	if prev, dup := seen[name]; dup {
		errs.Addf(ErrSchema, f.Key.S.Start,
			"constraints[%d]: duplicate key %q (first defined at %s)", idx, name, prev)
		return false
	}
	seen[name] = f.Key.S.Start
	return true
}

// The @-keys a node body may hold, greenlisted by kind. The kind is the
// block the body sits in (resources, data, or actions). Any @-prefixed key
// not on the kind's greenlist is a compile error, so a misspelled or
// not-yet-implemented meta key is caught early rather than silently ignored.
//
// resource, data, and action share a base set; an action body also allows
// @trigger. @lock and @timeout are allowed on any node (they bound apply
// in a kind-blind way), though neither affects a data source read at plan.
// A composite call site may sit in any of the three blocks, so
// @configurations is greenlisted everywhere; whether a key suits a leaf or
// a composite is a finer check done during resolution.
var (
	resourceBodyGreenlist = metaKeySet(
		"@configuration", "@configurations", "@depends-on", "@for-each", "@lock", "@timeout")
	dataBodyGreenlist = metaKeySet(
		"@configuration", "@configurations", "@depends-on", "@for-each", "@lock", "@timeout")
	actionBodyGreenlist = metaKeySet(
		"@configuration", "@configurations", "@depends-on", "@for-each",
		"@lock", "@timeout", "@trigger")
)

func metaKeySet(keys ...string) map[string]bool {
	set := make(map[string]bool, len(keys))
	for _, k := range keys {
		set[k] = true
	}
	return set
}

// ValidateResources checks a `resources:` block: every entry is keyed by
// a dotted alias.type.name and its body is an object.
func ValidateResources(block *ObjectLit) *ErrorList {
	return validateDeclBlock(block, "resource", resourceBodyGreenlist)
}

// ValidateDataSources checks the shape of a `data:` block.
func ValidateDataSources(block *ObjectLit) *ErrorList {
	return validateDeclBlock(block, "data source", dataBodyGreenlist)
}

// ValidateActions checks the shape of an `actions:` block.
func ValidateActions(block *ObjectLit) *ErrorList {
	return validateDeclBlock(block, "action", actionBodyGreenlist)
}

// validateDeclBlock checks one declaration block. Every entry is keyed by
// a dotted alias.type.name path and carries an object body whose only meta
// keys are those in greenlist. A bare or quoted key reports that the
// dotted form is required.
func validateDeclBlock(block *ObjectLit, what string, greenlist map[string]bool) *ErrorList {
	errs := NewErrorList(0)
	seen := make(map[string]Position, len(block.Fields))
	for _, fld := range block.Fields {
		if fld.Key.Kind != FieldPath {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"%s must be declared with a dotted alias.type.name key", what)
			continue
		}
		if len(fld.Key.Path) != 3 {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"%s key %s must have three segments: alias.type.name",
				what, strings.Join(fld.Key.Path, "."))
			continue
		}
		key := strings.Join(fld.Key.Path, ".")
		if prev, dup := seen[key]; dup {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"duplicate %s %s (first defined at %s)", what, key, prev)
			continue
		}
		seen[key] = fld.Key.S.Start
		body, ok := fld.Value.(*ObjectLit)
		if !ok {
			errs.Addf(ErrSchema, fld.Value.Span().Start,
				"%s %s: body must be an object", what, key)
			continue
		}
		for _, bodyFld := range body.Fields {
			if bodyFld.Key.Kind != FieldIdent || !bodyFld.Key.IsMeta() {
				continue
			}
			if !greenlist[bodyFld.Key.Name] {
				errs.Addf(ErrSchema, bodyFld.Key.S.Start,
					"%s %s: meta key %q is not allowed",
					what, key, bodyFld.Key.Name)
				continue
			}
			if bodyFld.Key.Name == "@timeout" {
				checkTimeoutValue(bodyFld, what, key, errs)
			}
		}
	}
	return errs
}

// checkTimeoutValue reports an error when a body's `@timeout:` value is not
// a duration string like '30s'. A malformed timeout is caught here rather
// than silently becoming no limit at apply.
func checkTimeoutValue(fld *Field, what, key string, errs *ErrorList) {
	s, ok := fld.Value.(*StringLit)
	if !ok {
		errs.Addf(ErrSchema, fld.Value.Span().Start,
			"%s %s: @timeout must be a duration string like '30s'",
			what, key)
		return
	}
	if _, err := time.ParseDuration(s.Value); err != nil {
		errs.Addf(ErrSchema, fld.Value.Span().Start,
			"%s %s: @timeout %q is not a valid duration",
			what, key, s.Value)
	}
}

// ValidateStateConfig checks a state: block in a config file. The block
// must have exactly one @backend: meta-key whose value is a bare backend
// name such as s3, plus any number of body fields keyed by bare
// identifiers. Body values must be static config values, with the file's
// locals referenceable; they are not type-checked here, since the
// resolver decodes them against each backend's declared configuration.
func ValidateStateConfig(block *ObjectLit, locals map[string]bool) *ErrorList {
	return validateBackendBlock(block, "state", "@backend", "s3", locals)
}

// ValidateEncryptionConfig checks an `encryption:` block in a config
// file. Same rules as ValidateStateConfig but with `@key-source:` in
// place of `@backend:`.
func ValidateEncryptionConfig(block *ObjectLit, locals map[string]bool) *ErrorList {
	return validateBackendBlock(block, "encryption", "@key-source", "kms", locals)
}

func validateBackendBlock(
	block *ObjectLit, what, metaKey, example string, locals map[string]bool,
) *ErrorList {
	errs := NewErrorList(0)
	seen := make(map[string]Position, len(block.Fields))
	var metaPos Position
	var metaSet bool
	for _, fld := range block.Fields {
		if fld.Key.Kind == FieldString {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"%s block key must be a bare identifier, got quoted string %q",
				what, fld.Key.String)
			continue
		}
		if fld.Key.IsMeta() {
			if fld.Key.Name == metaKey {
				if metaSet {
					errs.Addf(ErrSchema, fld.Key.S.Start,
						"%s block: duplicate %s (first defined at %s)",
						what, metaKey, metaPos)
					continue
				}
				metaSet = true
				metaPos = fld.Key.S.Start
				if err := validateResolverRefValue(fld.Value, example); err != nil {
					errs.Addf(ErrSchema, fld.Value.Span().Start,
						"%s block: %s: %s", what, metaKey, err.Error())
				}
				continue
			}
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"%s block: unknown meta-key %q", what, fld.Key.Name)
			continue
		}
		name := fld.Key.Name
		if prev, dup := seen[name]; dup {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"%s block: duplicate key %q (first defined at %s)", what, name, prev)
			continue
		}
		seen[name] = fld.Key.S.Start
		if what == "state" && name == "encryption" {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"state block: encryption is its own top-level block; move it out of state")
		}
		checkConfigValue(fld.Value, map[string]bool{}, staticConfigRules{locals: locals}, errs)
	}
	if !metaSet {
		errs.Addf(ErrSchema, block.S.Start,
			"%s block: missing required %s", what, metaKey)
	}
	return errs
}

func validateResolverRefValue(expr Expr, example string) error {
	switch expr.(type) {
	case *Ident:
		return nil
	case *DotPath:
		return fmt.Errorf("use a bare name like %s, not a qualified reference", example)
	default:
		return fmt.Errorf("expected a bare name like %s, got %s", example, exprKind(expr))
	}
}
