package lang

import (
	"errors"
	"fmt"
)

// allowedTopLevelKeys is the set of identifier keys permitted at the
// top level of each file kind. A factory and an exported type body are
// identical. A config holds deployment identity, state config, input
// values, and library configurations.
var allowedTopLevelKeys = map[FileKind]map[string]struct{}{
	FileFactory: {
		"description": {},
		"inputs":      {},
		"locals":      {},
		"constraints": {},
		"imports":     {},
		"data":        {},
		"resources":   {},
		"actions":     {},
		"outputs":     {},
	},
	FileExportedType: {
		"description": {},
		"inputs":      {},
		"locals":      {},
		"constraints": {},
		"imports":     {},
		"data":        {},
		"resources":   {},
		"actions":     {},
		"outputs":     {},
	},
	FileConfig: {
		"factory":        {},
		"parallelism":    {},
		"state":          {},
		"inputs":         {},
		"configurations": {},
	},
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
		if _, ok := allowed[name]; !ok {
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
		if keyName == "type" {
			hasType = true
			if _, err := PromoteType(df.Value); err != nil {
				var pe *Error
				if errors.As(err, &pe) {
					errs.Add(pe)
				} else {
					errs.Addf(ErrType, df.Value.Span().Start, "input %q: %v", name, err)
				}
			}
		}
	}
	if !hasType {
		errs.Addf(ErrSchema, decl.S.Start, "input %q: missing required `type:` key", name)
	}
}

// fieldsBasedConstraintKinds carries a list of input names under `fields:`.
var fieldsBasedConstraintKinds = map[string]struct{}{
	"exactly-one-of":     {},
	"at-least-one-of":    {},
	"at-most-one-of":     {},
	"mutually-exclusive": {},
	"required-together":  {},
	"required-with":      {},
	"forbidden-with":     {},
}

// ValidateConstraints walks a `constraints:` array and checks each entry's
// shape per its declared `kind:`. Field-based kinds carry a nonempty
// `fields:` list of input names; the `predicate` kind carries `when:` and
// `require:` expressions plus an optional `message:`.
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
	default:
		errs.Addf(ErrSchema, kindIdent.S.Start,
			"constraints[%d]: unknown constraint kind %q", idx, kind)
	}
}

func isFieldsBasedKind(s string) bool {
	_, ok := fieldsBasedConstraintKinds[s]
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
			"constraints[%d]: `fields:` must be an array of input names", idx)
		return
	}
	if len(arr.Elements) == 0 {
		errs.Addf(ErrSchema, arr.S.Start,
			"constraints[%d]: `fields:` must not be empty", idx)
		return
	}
	for j, el := range arr.Elements {
		if _, ok := el.(*Ident); !ok {
			errs.Addf(ErrSchema, el.Span().Start,
				"constraints[%d].fields[%d]: must be an identifier referencing an input name",
				idx, j)
		}
	}
}

func validatePredicateConstraint(idx int, obj *ObjectLit, errs *ErrorList) {
	var hasWhen, hasRequire bool
	seen := make(map[string]Position, len(obj.Fields))
	for _, f := range obj.Fields {
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

// ValidateOutputs checks an `outputs:` block. Every entry is a
// bare identifier name bound to an object wrapper of the form
// `{ value: expr }`, optionally carrying `@sensitive: true`. The
// wrapper exists so per-output metadata keys can ride alongside
// the value without ambiguity.
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
// `value:` key plus, optionally, `@sensitive: true`.
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
		if keyName != "value" {
			errs.Addf(ErrSchema, df.Key.S.Start,
				"output %q: unknown wrapper key %q (allowed: value)", name, keyName)
			continue
		}
		if prev, dup := innerSeen[keyName]; dup {
			errs.Addf(ErrSchema, df.Key.S.Start,
				"output %q: duplicate key %q (first defined at %s)", name, keyName, prev)
			continue
		}
		innerSeen[keyName] = df.Key.S.Start
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

// ValidateConstraintReferences checks that every name in the `fields:`
// list of each constraint corresponds to a declared input. Constraint
// entries with the wrong shape are skipped.
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
			id, ok := el.(*Ident)
			if !ok {
				continue
			}
			if _, exists := known[id.Name]; !exists {
				errs.Addf(ErrResolve, id.S.Start,
					"constraints[%d].fields[%d]: input %q not declared in `inputs:`",
					i, j, id.Name)
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
		mergeErrors(errs, ValidateLibraryCallAliases(f))
	case FileConfig:
		if obj, ok := blocks["state"].(*ObjectLit); ok {
			mergeErrors(errs, ValidateStateConfig(obj))
		}
	}
	return errs
}

// ValidateLibraryCallAliases walks every expression in f looking for
// `<alias>.<func>(...)` calls and rejects any whose alias is missing
// from the file's `imports:` block. The function's existence in the
// library is not checked here; that's a runtime concern because the
// library's actual function set lives in compiled Go code.
func ValidateLibraryCallAliases(f *File) *ErrorList {
	errs := NewErrorList(0)
	imports := importedAliases(f)
	Walk(f.Body, func(e Expr) {
		c, ok := e.(*Call)
		if !ok || c.Library == nil {
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
// @lock and @trigger. A composite call site may sit in any of the three
// blocks, so @configurations is greenlisted everywhere; whether a key suits
// a leaf or a composite is a finer check done during resolution.
var (
	resourceBodyGreenlist = metaKeySet(
		"@configuration", "@configurations", "@depends-on", "@for-each")
	dataBodyGreenlist = metaKeySet(
		"@configuration", "@configurations", "@depends-on", "@for-each")
	actionBodyGreenlist = metaKeySet(
		"@configuration", "@configurations", "@depends-on", "@for-each", "@lock", "@trigger")
)

func metaKeySet(keys ...string) map[string]bool {
	set := make(map[string]bool, len(keys))
	for _, k := range keys {
		set[k] = true
	}
	return set
}

// ValidateResources checks a `resources:` block level by level: alias,
// type name, instance name, body.
func ValidateResources(block *ObjectLit) *ErrorList {
	return validateNestedTypeBlock(block, "resource", resourceBodyGreenlist)
}

// ValidateDataSources checks the shape of a `data:` block.
func ValidateDataSources(block *ObjectLit) *ErrorList {
	return validateNestedTypeBlock(block, "data source", dataBodyGreenlist)
}

// ValidateActions checks the shape of an `actions:` block.
func ValidateActions(block *ObjectLit) *ErrorList {
	return validateNestedTypeBlock(block, "action", actionBodyGreenlist)
}

func validateNestedTypeBlock(block *ObjectLit, what string, greenlist map[string]bool) *ErrorList {
	errs := NewErrorList(0)
	seenAlias := make(map[string]Position, len(block.Fields))
	for _, aliasFld := range block.Fields {
		if !checkBareIdentKey(aliasFld, seenAlias, what+" alias", errs) {
			continue
		}
		aliasObj, ok := aliasFld.Value.(*ObjectLit)
		if !ok {
			errs.Addf(ErrSchema, aliasFld.Value.Span().Start,
				"%s alias %q: must be an object of type names",
				what, aliasFld.Key.Name)
			continue
		}
		seenType := make(map[string]Position, len(aliasObj.Fields))
		for _, typeFld := range aliasObj.Fields {
			if !checkBareIdentKey(typeFld, seenType, what+" type", errs) {
				continue
			}
			typeObj, ok := typeFld.Value.(*ObjectLit)
			if !ok {
				errs.Addf(ErrSchema, typeFld.Value.Span().Start,
					"%s %s.%s: must be an object of instance names",
					what, aliasFld.Key.Name, typeFld.Key.Name)
				continue
			}
			seenName := make(map[string]Position, len(typeObj.Fields))
			for _, nameFld := range typeObj.Fields {
				if !checkBareIdentKey(nameFld, seenName, what+" name", errs) {
					continue
				}
				body, ok := nameFld.Value.(*ObjectLit)
				if !ok {
					errs.Addf(ErrSchema, nameFld.Value.Span().Start,
						"%s %s.%s.%s: body must be an object",
						what, aliasFld.Key.Name, typeFld.Key.Name, nameFld.Key.Name)
					continue
				}
				for _, bodyFld := range body.Fields {
					if bodyFld.Key.Kind != FieldIdent || !bodyFld.Key.IsMeta() {
						continue
					}
					if !greenlist[bodyFld.Key.Name] {
						errs.Addf(ErrSchema, bodyFld.Key.S.Start,
							"%s %s.%s.%s: meta key %q is not allowed",
							what, aliasFld.Key.Name, typeFld.Key.Name, nameFld.Key.Name,
							bodyFld.Key.Name)
					}
				}
			}
		}
	}
	return errs
}

func checkBareIdentKey(f *Field, seen map[string]Position, what string, errs *ErrorList) bool {
	if f.Key.Kind == FieldString {
		errs.Addf(ErrSchema, f.Key.S.Start,
			"%s key must be a bare identifier, got quoted string %q",
			what, f.Key.String)
		return false
	}
	name := f.Key.Name
	if f.Key.IsMeta() {
		errs.Addf(ErrSchema, f.Key.S.Start,
			"@-prefixed key %q is not a valid %s key", name, what)
		return false
	}
	if prev, dup := seen[name]; dup {
		errs.Addf(ErrSchema, f.Key.S.Start,
			"duplicate %s %q (first defined at %s)", what, name, prev)
		return false
	}
	seen[name] = f.Key.S.Start
	return true
}

// ValidateStateConfig checks the structure of a `state:` block in a
// config file. The block must carry exactly one `@backend:` meta-key
// whose value is a bare identifier (`local`) or a two-segment
// alias-qualified reference (`aws.s3`). It may carry a nested
// `encryption:` object of the same form with `@key-source:`, plus any
// number of body fields keyed by bare identifiers. Body values are not
// type-checked here; the resolver decodes them against each backend's
// declared configuration.
func ValidateStateConfig(block *ObjectLit) *ErrorList {
	return validateBackendBlock(block, "state", "@backend")
}

// ValidateEncryptionConfig checks the structure of an `encryption:`
// sub-block nested inside a `state:` block. Same rules as
// ValidateStateConfig but with `@key-source:` in place of `@backend:`
// and no further nested blocks.
func ValidateEncryptionConfig(block *ObjectLit) *ErrorList {
	return validateBackendBlock(block, "encryption", "@key-source")
}

func validateBackendBlock(block *ObjectLit, what, metaKey string) *ErrorList {
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
				if err := validateResolverRefValue(fld.Value); err != nil {
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
			sub, ok := fld.Value.(*ObjectLit)
			if !ok {
				errs.Addf(ErrSchema, fld.Value.Span().Start,
					"state block: encryption must be an object, got %s",
					exprKind(fld.Value))
				continue
			}
			mergeErrors(errs, ValidateEncryptionConfig(sub))
		}
	}
	if !metaSet {
		errs.Addf(ErrSchema, block.S.Start,
			"%s block: missing required %s", what, metaKey)
	}
	return errs
}

func validateResolverRefValue(expr Expr) error {
	switch v := expr.(type) {
	case *Ident:
		return nil
	case *DotPath:
		if v.Root == nil || len(v.Segments) != 1 || v.Segments[0].Name == "" {
			return errors.New("expected `name` or `alias.name`")
		}
		return nil
	default:
		return fmt.Errorf("expected `name` or `alias.name`, got %s", exprKind(expr))
	}
}
