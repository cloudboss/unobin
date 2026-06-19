package check

import (
	"maps"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

// checkTypes runs the type-compatibility pass over every body in
// the DAG once the reference checker has resolved identifiers. It
// walks each node's body, looks up the field's declared type from
// the resolved library's schema, and asks typecheck.Check to
// validate the expression against it. Composite output and
// constraint predicate bodies are walked too. Anything the schema
// cannot describe (composite outputs, unknown Go field types) comes
// back as typecheck.TUnknown and the inferrer skips silently.
func (c *referenceChecker) checkTypes() {
	for _, n := range c.dag.Nodes {
		switch n.Kind {
		case runtime.NodeResource, runtime.NodeData, runtime.NodeAction:
		case runtime.NodeConfiguration:
			c.checkConfigurationNode(n)
			continue
		default:
			continue
		}
		targets := c.bodyTargets(n)
		scope := c.scopeFor(n)
		c.checkBodyTypes(n.Body, targets, scope, n)
		c.checkRequiredPresence(n, targets)
	}
	c.checkLocalsBodyTypes()
	c.checkOutputBodyTypes()
	c.checkConstraintTypes()
}

// checkConfigurationNode validates one factory configuration. The
// alias must name an imported library that declares a configuration.
// When the configuration's field schema is known, every body field
// must be one the schema declares with an assignable type. Missing
// required fields are left to the runtime, where the stack file may
// supply an override. A whole-expression body checks against the
// schema as one object type; the comparison is open, so an extra field
// the checker cannot rule out is left for plan-time decode to reject.
func (c *referenceChecker) checkConfigurationNode(n *runtime.Node) {
	label := configurationNodeLabel(n)
	lib := c.libraries[""][n.Alias]
	if lib == nil {
		c.addf(n.Body.Span().Start,
			"%s: library %q is not imported", label, n.Alias)
		return
	}
	if !libraryKnown(lib) {
		return
	}
	if lib.Configuration == nil && (lib.Schema == nil || !lib.Schema.HasConfiguration) {
		c.addf(n.Body.Span().Start,
			"%s: library declares no configuration", label)
		return
	}
	if lib.Schema == nil || lib.Schema.Configuration == nil {
		return
	}
	schema := lib.Schema.Configuration
	scope := c.scopeFor(n)
	obj, ok := n.Body.(*lang.ObjectLit)
	if !ok {
		typecheck.Check(n.Body, configurationObjectType(schema), scope, c.errs)
		return
	}
	present := make(map[string]bool, len(obj.Fields))
	for _, fld := range obj.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		present[fld.Key.Name] = true
		target, ok := schema[fld.Key.Name]
		if !ok {
			c.addf(fld.Key.S.Start, "%s: unknown field %q", label, fld.Key.Name)
			continue
		}
		typecheck.Check(fld.Value, target, scope, c.errs)
	}
}

func configurationNodeLabel(n *runtime.Node) string {
	if n.Name == "default" {
		return "default configuration for " + n.Alias
	}
	return "configuration." + n.Name
}

// checkLocalsBodyTypes infers every local's expression with the real
// error list. Lazy lookups elsewhere infer a local with a discarded
// list, so a mistake inside a local is reported here, once, at its
// declaration.
func (c *referenceChecker) checkLocalsBodyTypes() {
	c.checkLocalsBlockTypes("")
	for _, n := range c.dag.Nodes {
		if !n.IsComposite() {
			continue
		}
		c.checkLocalsBlockTypes(n.Address)
	}
}

func (c *referenceChecker) checkLocalsBlockTypes(scope string) {
	exprs := c.localExprsFor(scope)
	if len(exprs) == 0 {
		return
	}
	s := &typecheck.Scope{
		Inputs:         c.scopeInputs(scope),
		LookupNode:     c.lookupNodeFor(scope),
		LookupFunction: c.lookupFunctionFor(scope),
		Observe:        c.observe,
	}
	s.LookupLocal = c.lookupLocalFor(scope, s)
	names := make([]string, 0, len(exprs))
	for name := range exprs {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		typecheck.Infer(exprs[name], typecheck.TUnknown(), s, c.errs)
	}
}

// checkRequiredPresence reports body fields a node's schema requires
// but the body leaves out. A field is required when its declared type
// is not optional and no default is declared for it; an Unknown type
// stays unchecked, since it may stand for a type the schema cannot
// describe. Nodes whose schema cannot be located check nothing, so a
// missing-schema library does not block compile.
func (c *referenceChecker) checkRequiredPresence(
	n *runtime.Node, targets map[string]typecheck.Type,
) {
	if len(targets) == 0 {
		return
	}
	obj, ok := n.Body.(*lang.ObjectLit)
	if !ok {
		return
	}
	present := make(map[string]bool, len(obj.Fields))
	for _, fld := range obj.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		present[fld.Key.Name] = true
	}
	defaulted := c.defaultedInputs(n)
	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		t := targets[name]
		if t.Kind == typecheck.Optional || t.Kind == typecheck.Unknown {
			continue
		}
		if present[name] || defaulted[name] {
			continue
		}
		c.addf(n.Body.Span().Start,
			"missing required input %q on %s.%s", name, n.Alias, n.Type)
	}
}

// defaultedInputs returns the top-level input names the node's Go type
// declares a default for, Optional markers included. A nested default
// does not excuse its top-level parent. A composite declares
// optionality in its inputs block instead, so its set is empty.
func (c *referenceChecker) defaultedInputs(n *runtime.Node) map[string]bool {
	if n.IsComposite() {
		return nil
	}
	ts := c.lookupTypeSchema(n)
	if ts == nil {
		return nil
	}
	out := make(map[string]bool, len(ts.Defaults))
	for _, d := range ts.Defaults {
		rest, ok := strings.CutPrefix(d.Field, "var.")
		if !ok {
			continue
		}
		if !strings.Contains(rest, ".") {
			out[rest] = true
		}
	}
	return out
}

// bodyTargets returns the per-field type targets for a node body.
// Returns nil when the node's schema cannot be located; the caller
// then runs the body's expressions with no target (free inference)
// so missing-schema libraries do not block compile.
func (c *referenceChecker) bodyTargets(n *runtime.Node) map[string]typecheck.Type {
	if n.IsComposite() {
		return c.compositeInputTargets(n)
	}
	return c.goInputTargets(n)
}

func (c *referenceChecker) compositeInputTargets(n *runtime.Node) map[string]typecheck.Type {
	if n.CompositeSyntaxBody == nil {
		return nil
	}
	return inputTargets(c.syntaxInputFields(n.Address, n.CompositeSyntaxBody.Inputs))
}

func inputTargets(fields []typecheck.ObjectField) map[string]typecheck.Type {
	out := make(map[string]typecheck.Type, len(fields))
	for _, f := range fields {
		t := f.Type
		if f.Optional {
			t = typecheck.TOptional(t)
		}
		out[f.Name] = t
	}
	return out
}

func (c *referenceChecker) syntaxInputFields(
	scope string,
	decls []syntax.InputDecl,
) []typecheck.ObjectField {
	fields := make([]typecheck.ObjectField, 0, len(decls))
	for _, decl := range decls {
		inner, optional, defaulted := syntaxInputType(decl)
		fields = append(fields, typecheck.ObjectField{
			Name:      decl.Name.Name,
			Type:      c.typeFromLangInput(scope, inner),
			Optional:  optional,
			Defaulted: defaulted,
		})
	}
	return fields
}

func syntaxInputType(decl syntax.InputDecl) (lang.TypeExpr, bool, bool) {
	if opt, ok := decl.Type.(*lang.TypeOptional); ok {
		return opt.Elem, true, false
	}
	if inputDeclHasDefault(decl.Body) {
		return decl.Type, true, true
	}
	return decl.Type, false, false
}

func (c *referenceChecker) typeFromLangInput(scope string, t lang.TypeExpr) typecheck.Type {
	lib, ok := t.(*lang.TypeLibraryConfig)
	if !ok {
		return typecheck.FromLang(t)
	}
	return c.libraryConfigInputType(scope, lib)
}

func (c *referenceChecker) libraryConfigInputType(
	scope string,
	t *lang.TypeLibraryConfig,
) typecheck.Type {
	if t == nil || t.Path == nil {
		return typecheck.TUnknown()
	}
	path := t.Path.Value
	libs := c.libraries[scope]
	if libs == nil {
		c.addf(t.Path.S.Start, "library-config %q has no resolved imports", path)
		return typecheck.TUnknown()
	}
	imports := c.importsForScope(scope)
	var out typecheck.Type
	matched := false
	for _, imp := range imports {
		if imp.Ref == nil || imp.Ref.Value != path {
			continue
		}
		matched = true
		lib := libs[imp.Alias.Name]
		if lib == nil {
			c.addf(t.Path.S.Start, "library-config %q: alias %q is unresolved",
				path, imp.Alias.Name)
			continue
		}
		got, ok := libraryConfigType(path, lib)
		if !ok {
			c.addf(t.Path.S.Start, "library-config %q: alias %q declares no config",
				path, imp.Alias.Name)
			continue
		}
		if !out.IsKnown() && out.Kind == typecheck.Unknown {
			out = got
			continue
		}
		if !out.Equal(got) {
			c.addf(t.Path.S.Start,
				"library-config %q: aliases disagree on config schema", path)
			return typecheck.TUnknown()
		}
	}
	if !matched {
		c.addf(t.Path.S.Start, "library-config path %q is not imported in this body", path)
		return typecheck.TUnknown()
	}
	if out.Kind == typecheck.Unknown {
		return typecheck.TUnknown()
	}
	return out
}

func (c *referenceChecker) importsForScope(scope string) []syntax.ImportDecl {
	if scope == "" {
		if c.rootSyntax == nil {
			return nil
		}
		return c.rootSyntax.Imports
	}
	node, ok := c.dag.Nodes[scope]
	if !ok || node.CompositeSyntaxBody == nil {
		return nil
	}
	return node.CompositeSyntaxBody.Imports
}

func libraryConfigType(path string, lib *runtime.Library) (typecheck.Type, bool) {
	if lib == nil {
		return typecheck.TUnknown(), false
	}
	if lib.Schema != nil && lib.Schema.HasConfiguration {
		fields := lib.Schema.ConfigurationFields
		if fields == nil && lib.Schema.Configuration != nil {
			fields = configurationFieldsFromMap(lib.Schema.Configuration)
		}
		if fields == nil {
			return typecheck.TUnknown(), false
		}
		digest := lib.Schema.ConfigurationDigest
		if digest == "" {
			digest = cfg.DigestView(fields, lib.Schema.ConfigurationDefaults)
		}
		return typecheck.TLibraryConfig(path, path, digest, fields), true
	}
	if lib.Configuration == nil {
		return typecheck.TUnknown(), false
	}
	view, err := cfg.View(lib.Configuration)
	if err != nil {
		return typecheck.TUnknown(), false
	}
	return typecheck.TLibraryConfig(path, path, view.SchemaDigest, view.Fields), true
}

func configurationFieldsFromMap(schema map[string]typecheck.Type) []typecheck.ObjectField {
	fields := make([]typecheck.ObjectField, 0, len(schema))
	for _, name := range slices.Sorted(maps.Keys(schema)) {
		t := schema[name]
		fields = append(fields, typecheck.ObjectField{
			Name:     name,
			Type:     t.Unwrap(),
			Optional: t.Kind == typecheck.Optional,
		})
	}
	return fields
}

func inputDeclHasDefault(decl *lang.ObjectLit) bool {
	if decl == nil {
		return false
	}
	for _, fld := range decl.Fields {
		if fld.Key.Kind == lang.FieldIdent && fld.Key.Name == "default" {
			return true
		}
	}
	return false
}

func (c *referenceChecker) goInputTargets(n *runtime.Node) map[string]typecheck.Type {
	ts := c.lookupTypeSchema(n)
	if ts == nil || ts.Inputs == nil {
		return nil
	}
	return ts.Inputs
}

func (c *referenceChecker) lookupTypeSchema(n *runtime.Node) *runtime.TypeSchema {
	libs := c.libraries[n.Composite]
	if libs == nil {
		return nil
	}
	lib := libs[n.Alias]
	if lib == nil || lib.Schema == nil {
		return nil
	}
	switch n.Kind {
	case runtime.NodeResource:
		return lib.Schema.Resources[n.Type]
	case runtime.NodeData:
		return lib.Schema.DataSources[n.Type]
	case runtime.NodeAction:
		return lib.Schema.Actions[n.Type]
	}
	return nil
}

func (c *referenceChecker) scopeFor(n *runtime.Node) *typecheck.Scope {
	inputs := c.scopeInputs(n.Composite)
	scope := &typecheck.Scope{
		Inputs:                 inputs,
		LookupNode:             c.lookupNodeFor(n.Composite),
		LookupFunction:         c.lookupFunctionFor(n.Composite),
		LookupConfiguration:    c.lookupConfigurationFor(n.Composite),
		LookupConfigurationRef: c.lookupConfigurationRef,
		Observe:                c.observe,
	}
	scope.LookupLocal = c.lookupLocalFor(n.Composite, scope)
	return scope
}

// lookupConfigurationFor returns a resolver that types a configuration
// reference's value from the aliased library's configuration schema.
// Schema fields come back as one object type, optional fields marked,
// in sorted order so derived diagnostics stay stable.
func (c *referenceChecker) lookupConfigurationFor(
	scope string,
) func(alias string) (typecheck.Type, bool) {
	return func(alias string) (typecheck.Type, bool) {
		libs := c.libraries[scope]
		if libs == nil || libs[alias] == nil {
			return typecheck.TUnknown(), false
		}
		lib := libs[alias]
		if lib.Schema == nil || lib.Schema.Configuration == nil {
			return typecheck.TUnknown(), false
		}
		return configurationObjectType(lib.Schema.Configuration), true
	}
}

func (c *referenceChecker) lookupConfigurationRef(name string) (string, bool) {
	ref, ok := c.configurationRefs[name]
	if !ok {
		return "", false
	}
	return ref.Alias, true
}

// configurationObjectType folds a configuration schema's field map
// into the object type a whole configuration value has.
func configurationObjectType(schema map[string]typecheck.Type) typecheck.Type {
	fields := make([]typecheck.ObjectField, 0, len(schema))
	for _, name := range slices.Sorted(maps.Keys(schema)) {
		t := schema[name]
		fields = append(fields, typecheck.ObjectField{
			Name:     name,
			Type:     t.Unwrap(),
			Optional: t.Kind == typecheck.Optional,
		})
	}
	return typecheck.TObject(fields)
}

// lookupFunctionFor resolves a qualified function call in the given
// scope to its declared signature, so the inferrer can check argument
// types and use the result type. @core resolves against the language's
// own table; a missing library or schema resolves nothing, leaving the
// call to infer Unknown.
func (c *referenceChecker) lookupFunctionFor(
	scope string,
) func(library, name string) (typecheck.FuncSig, bool) {
	return func(library, name string) (typecheck.FuncSig, bool) {
		if library == lang.CoreNamespace {
			sig, ok := runtime.CoreFunctionSigs()[name]
			return sig, ok
		}
		libs := c.libraries[scope]
		if libs == nil {
			return typecheck.FuncSig{}, false
		}
		lib := libs[library]
		if lib == nil || lib.Schema == nil {
			return typecheck.FuncSig{}, false
		}
		sig, ok := lib.Schema.Functions[name]
		return sig, ok
	}
}

// lookupLocalFor returns a resolver that infers the type of a local in
// the given scope. The local's expression is inferred against the same
// scope, so a local may read inputs, nodes, and other locals. Results
// are memoized; a local caught mid-inference (a cycle, already reported
// by the reference checker) yields Unknown rather than looping.
func (c *referenceChecker) lookupLocalFor(
	scope string,
	sc *typecheck.Scope,
) typecheck.LookupLocalFn {
	exprs := c.localExprsFor(scope)
	memo := map[string]typecheck.Type{}
	forcing := map[string]bool{}
	return func(name string) (typecheck.Type, bool) {
		expr, ok := exprs[name]
		if !ok {
			return typecheck.Type{}, false
		}
		if t, done := memo[name]; done {
			return t, true
		}
		if forcing[name] {
			return typecheck.TUnknown(), true
		}
		forcing[name] = true
		t := typecheck.Infer(expr, typecheck.TUnknown(), sc, lang.NewErrorList(0))
		delete(forcing, name)
		memo[name] = t
		return t, true
	}
}

func (c *referenceChecker) localExprsFor(scope string) map[string]lang.Expr {
	if scope == "" {
		if c.rootSyntax != nil {
			return syntaxLocalExprs(c.rootSyntax.Locals)
		}
		return nil
	}
	node, ok := c.dag.Nodes[scope]
	if !ok {
		return nil
	}
	if node.CompositeSyntaxBody == nil {
		return nil
	}
	return syntaxLocalExprs(node.CompositeSyntaxBody.Locals)
}

func syntaxLocalExprs(decls []syntax.LocalDecl) map[string]lang.Expr {
	out := map[string]lang.Expr{}
	for _, decl := range decls {
		out[decl.Name.Name] = decl.Value
	}
	return out
}

func (c *referenceChecker) scopeInputs(scope string) []typecheck.ObjectField {
	if scope == "" {
		if c.rootSyntax != nil {
			return c.syntaxInputFields("", c.rootSyntax.Inputs)
		}
		return nil
	}
	node, ok := c.dag.Nodes[scope]
	if !ok || node.CompositeSyntaxBody == nil {
		return nil
	}
	return c.syntaxInputFields(scope, node.CompositeSyntaxBody.Inputs)
}

func (c *referenceChecker) lookupNodeFor(scope string) typecheck.LookupNodeFn {
	return func(kind, alias, typ, name string) (typecheck.Type, bool) {
		ref := kind + "." + name
		if alias != "" || typ != "" {
			ref = kind + "." + alias + "." + typ + "." + name
		}
		node, ok := c.dag.Nodes[runtime.ScopeRef(ref, scope)]
		if !ok {
			return typecheck.Type{}, false
		}
		return c.nodeAttrType(node), true
	}
}

// compositeOutputTypes infers the types of a composite node's
// declared outputs in the composite's own scope, memoized per node.
// Inference runs with a discarded error list: the outputs block is
// already checked with the real one by checkOutputBodyTypes, so a
// reference to a broken output does not repeat the mistake at every
// read. A re-entrant lookup returns nil rather than recursing.
func (c *referenceChecker) compositeOutputTypes(node *runtime.Node) map[string]typecheck.Type {
	if c.compositeOutputs == nil {
		c.compositeOutputs = map[*runtime.Node]map[string]typecheck.Type{}
		c.forcingComposite = map[*runtime.Node]bool{}
	}
	if types, done := c.compositeOutputs[node]; done {
		return types
	}
	if c.forcingComposite[node] {
		return nil
	}
	c.forcingComposite[node] = true
	types := c.inferCompositeOutputs(node)
	delete(c.forcingComposite, node)
	c.compositeOutputs[node] = types
	return types
}

func (c *referenceChecker) inferCompositeOutputs(node *runtime.Node) map[string]typecheck.Type {
	s := &typecheck.Scope{
		Inputs:         c.scopeInputs(node.Address),
		LookupNode:     c.lookupNodeFor(node.Address),
		LookupFunction: c.lookupFunctionFor(node.Address),
	}
	s.LookupLocal = c.lookupLocalFor(node.Address, s)
	discard := lang.NewErrorList(0)
	if node.CompositeSyntaxBody == nil {
		return nil
	}
	return inferSyntaxOutputs(node.CompositeSyntaxBody.Outputs, s, discard)
}

func inferSyntaxOutputs(
	outputs []syntax.OutputDecl,
	s *typecheck.Scope,
	errs *lang.ErrorList,
) map[string]typecheck.Type {
	out := make(map[string]typecheck.Type, len(outputs))
	for _, decl := range outputs {
		expr := lang.OutputValueExpr(decl.Body)
		if expr == nil {
			out[decl.Name.Name] = typecheck.TUnknown()
			continue
		}
		out[decl.Name.Name] = typecheck.Infer(expr, typecheck.TUnknown(), s, errs)
	}
	return out
}

// nodeAttrType builds an Object Type describing what a node exposes to
// references: for a Go-backed leaf, its inputs laid under its outputs,
// matching the runtime merge so a reference to a plain input type-checks
// without the resource echoing it into its output struct. Outputs win on
// a name collision. goschema has already expanded nested struct types so
// the descender can walk through them. A composite node contributes an
// Object of its declared outputs with their inferred types.
func (c *referenceChecker) nodeAttrType(node *runtime.Node) typecheck.Type {
	if node == nil {
		return typecheck.TUnknown()
	}
	if node.IsComposite() {
		types := c.compositeOutputTypes(node)
		names := make([]string, 0, len(types))
		for name := range types {
			names = append(names, name)
		}
		slices.Sort(names)
		fields := make([]typecheck.ObjectField, 0, len(names))
		for _, name := range names {
			fields = append(fields, typecheck.ObjectField{
				Name: name,
				Type: types[name],
			})
		}
		return typecheck.TObject(fields)
	}
	libs := c.libraries[node.Composite]
	if libs == nil {
		return typecheck.TUnknown()
	}
	lib := libs[node.Alias]
	if lib == nil || lib.Schema == nil {
		return typecheck.TUnknown()
	}
	var ts *runtime.TypeSchema
	switch node.Kind {
	case runtime.NodeResource:
		ts = lib.Schema.Resources[node.Type]
	case runtime.NodeData:
		ts = lib.Schema.DataSources[node.Type]
	case runtime.NodeAction:
		ts = lib.Schema.Actions[node.Type]
	}
	if ts == nil {
		return typecheck.TUnknown()
	}
	fields := make([]typecheck.ObjectField, 0, len(ts.Inputs)+len(ts.Outputs))
	at := make(map[string]int, len(ts.Inputs)+len(ts.Outputs))
	for name, t := range ts.Inputs {
		at[name] = len(fields)
		fields = append(fields, typecheck.ObjectField{Name: name, Type: t})
	}
	for name, t := range ts.Outputs {
		if i, ok := at[name]; ok {
			fields[i].Type = t
			continue
		}
		fields = append(fields, typecheck.ObjectField{Name: name, Type: t})
	}
	if len(fields) == 0 {
		return typecheck.TUnknown()
	}
	return typecheck.TObject(fields)
}

func (c *referenceChecker) checkBodyTypes(
	body lang.Expr,
	targets map[string]typecheck.Type,
	scope *typecheck.Scope,
	owner *runtime.Node,
) {
	obj, ok := body.(*lang.ObjectLit)
	if !ok {
		return
	}
	each := eachBindingFromBody(obj, scope, c.errs)
	for _, fld := range obj.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		target := typecheck.TUnknown()
		if targets != nil {
			t, ok := targets[fld.Key.Name]
			if ok {
				target = t
			} else if owner != nil && !owner.IsComposite() {
				c.addf(fld.Key.S.Start,
					`unknown field %q on %s.%s`,
					fld.Key.Name, owner.Alias, owner.Type)
				continue
			}
		}
		fieldScope := scope
		if each != nil {
			s := *scope
			s.Each = each
			fieldScope = &s
		}
		typecheck.Check(fld.Value, target, fieldScope, c.errs)
	}
}

// eachBindingFromBody inspects an object literal for an @for-each
// meta key and returns the type pair bound by the iteration. The
// inferrer walks the @for-each value expression in the parent scope
// (no @each binding yet) so the typing reflects what the body sees
// during iteration.
func eachBindingFromBody(
	obj *lang.ObjectLit, scope *typecheck.Scope, errs *lang.ErrorList,
) *typecheck.EachBinding {
	for _, fld := range obj.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.Name != "@for-each" {
			continue
		}
		t := typecheck.Infer(fld.Value, typecheck.TUnknown(), scope, errs)
		checkFanOutIterable(t, fld.Value.Span().Start, errs)
		return eachBindingFromType(t)
	}
	return nil
}

// checkFanOutIterable reports a node @for-each whose iterable can
// never fan out. The runtime iterates maps only, so each instance
// gets a stable key; the list error names the comprehension that
// builds the map, and a possibly-null iterable wants a fallback,
// since the runtime rejects null. An opaque iterable is closed off:
// iterating would read into a value that passes through unread.
func checkFanOutIterable(t typecheck.Type, pos lang.Position, errs *lang.ErrorList) {
	if t.Unwrap().Kind == typecheck.Opaque {
		errs.Addf(lang.ErrType, pos,
			"@for-each: iterable is opaque; declare its type, like map(...)")
		return
	}
	switch t.Kind {
	case typecheck.Unknown, typecheck.Map, typecheck.Object:
	case typecheck.Optional:
		switch t.Unwrap().Kind {
		case typecheck.Unknown, typecheck.Map, typecheck.Object,
			typecheck.List, typecheck.Tuple:
			errs.Addf(lang.ErrType, pos,
				"@for-each: iterable may be null; supply a fallback, like "+
					"m ?? {} (got %s)", t)
		default:
			errs.Addf(lang.ErrType, pos, "@for-each: iterable must be a map, got %s", t)
		}
	case typecheck.List, typecheck.Tuple:
		errs.Addf(lang.ErrType, pos,
			"@for-each: iterable must be a map, got %s; "+
				"turn a list into a map with { for n in ns : n => n }", t)
	default:
		errs.Addf(lang.ErrType, pos, "@for-each: iterable must be a map, got %s", t)
	}
}

func eachBindingFromType(t typecheck.Type) *typecheck.EachBinding {
	switch t.Kind {
	case typecheck.Map:
		value := typecheck.TUnknown()
		if t.Elem != nil {
			value = *t.Elem
		}
		return &typecheck.EachBinding{Key: typecheck.TString(), Value: value}
	case typecheck.Object:
		return &typecheck.EachBinding{
			Key:   typecheck.TString(),
			Value: typecheck.TUnknown(),
		}
	}
	return &typecheck.EachBinding{
		Key:   typecheck.TUnknown(),
		Value: typecheck.TUnknown(),
	}
}

// checkOutputBodyTypes walks the root and each composite's
// `outputs:` block. Output expressions have no declared target
// type, so the inferrer runs with TUnknown; the point is to let
// nested field references go through traverseSegments.
func (c *referenceChecker) checkOutputBodyTypes() {
	if c.rootSyntax != nil {
		c.checkSyntaxOutputsBlock(c.rootSyntax.Outputs, "")
	}
	for _, n := range c.dag.Nodes {
		if !n.IsComposite() {
			continue
		}
		c.checkSyntaxOutputsBlock(n.CompositeSyntaxBody.Outputs, n.Address)
	}
}

func (c *referenceChecker) checkSyntaxOutputsBlock(outputs []syntax.OutputDecl, scope string) {
	s := c.outputScope(scope)
	for _, decl := range outputs {
		typecheck.Infer(decl.Body, typecheck.TUnknown(), s, c.errs)
	}
}

func (c *referenceChecker) outputScope(scope string) *typecheck.Scope {
	s := &typecheck.Scope{
		Inputs:         c.scopeInputs(scope),
		LookupNode:     c.lookupNodeFor(scope),
		LookupFunction: c.lookupFunctionFor(scope),
		Observe:        c.observe,
	}
	s.LookupLocal = c.lookupLocalFor(scope, s)
	return s
}

// checkConstraintTypes runs the inferrer over each constraint's
// `when:` and `require:` expressions with TBoolean as the target so
// non-boolean predicates report a clear mismatch.
func (c *referenceChecker) checkConstraintTypes() {
	if c.rootSyntax != nil {
		c.checkSyntaxConstraintTypesBlock(c.rootSyntax.Constraints, "")
	}
	for _, n := range c.dag.Nodes {
		if !n.IsComposite() {
			continue
		}
		c.checkSyntaxConstraintTypesBlock(n.CompositeSyntaxBody.Constraints, n.Address)
	}
}

func (c *referenceChecker) checkSyntaxConstraintTypesBlock(
	decls []syntax.ConstraintDecl, scope string,
) {
	values := make([]lang.Expr, 0, len(decls))
	for _, decl := range decls {
		values = append(values, decl.Value)
	}
	c.checkConstraintTypeExprs(values, scope)
}

func (c *referenceChecker) checkConstraintTypeExprs(values []lang.Expr, scope string) {
	// Constraints evaluate with MissingAsNull, so navigating into a
	// possibly-null input reads as null there instead of failing; the
	// checker mirrors that mode.
	s := &typecheck.Scope{
		Inputs:         c.scopeInputs(scope),
		LookupNode:     c.lookupNodeFor(scope),
		LookupFunction: c.lookupFunctionFor(scope),
		MissingAsNull:  true,
		Observe:        c.observe,
	}
	for _, e := range values {
		obj, ok := e.(*lang.ObjectLit)
		if !ok {
			continue
		}
		entryScope := s
		if forEach := constraintForEach(obj); forEach != nil {
			withEach := *s
			t := typecheck.Infer(forEach, typecheck.TUnknown(), s, c.errs)
			if bareConstraintIterable(forEach) {
				checkConstraintIterable(t, forEach.Span().Start, c.errs)
			}
			withEach.Each = eachBindingFor(t)
			entryScope = &withEach
		}
		// The runtime only evaluates require when when held, so a null
		// test in when narrows the references require reads.
		var whenExpr, requireExpr lang.Expr
		for _, fld := range obj.Fields {
			if fld.Key.Kind != lang.FieldIdent {
				continue
			}
			switch fld.Key.Name {
			case "when":
				whenExpr = fld.Value
			case "require":
				requireExpr = fld.Value
			}
		}
		if whenExpr != nil {
			typecheck.Check(whenExpr, typecheck.TBoolean(), entryScope, c.errs)
		}
		if requireExpr != nil {
			requireScope := entryScope
			if whenExpr != nil {
				requireScope = typecheck.NarrowedWhere(entryScope, whenExpr)
			}
			typecheck.Check(requireExpr, typecheck.TBoolean(), requireScope, c.errs)
		}
	}
}

// bareConstraintIterable reports whether a constraint @for-each value
// gets the iterable kind check. The chained form (an array of level
// objects) validates its levels elsewhere, and a dot path rooted
// outside var already failed the inputs-only rule, so typing it on
// top would report the same mistake twice.
func bareConstraintIterable(forEach lang.Expr) bool {
	switch fe := forEach.(type) {
	case *lang.ArrayLit:
		return false
	case *lang.DotPath:
		return fe.Root == nil || fe.Root.Name == "var"
	}
	return true
}

// checkConstraintIterable reports a bare constraint @for-each whose
// iterable is not a list or a map, the kinds the predicate runtime
// iterates. An optional iterable stays legal: the predicate runtime
// skips a null iterable, so the entry is vacuously satisfied. An
// opaque iterable is closed off: iterating would read into a value
// that passes through unread.
func checkConstraintIterable(t typecheck.Type, pos lang.Position, errs *lang.ErrorList) {
	if t.Unwrap().Kind == typecheck.Opaque {
		errs.Addf(lang.ErrType, pos,
			"@for-each: iterable is opaque; declare its type, like list(...) or map(...)")
		return
	}
	switch t.Unwrap().Kind {
	case typecheck.Unknown, typecheck.List,
		typecheck.Map, typecheck.Object, typecheck.Tuple:
		return
	}
	errs.Addf(lang.ErrType, pos, "@for-each: iterable must be a list or a map, got %s", t)
}

// eachBindingFor maps an @for-each iterable's inferred type onto the
// @each binding: a list binds an integer key and the element type, a
// map binds a string key and the value type, anything else binds
// Unknown so the entry still checks without claiming a type.
func eachBindingFor(iterable typecheck.Type) *typecheck.EachBinding {
	switch iterable.Kind {
	case typecheck.List:
		if iterable.Elem != nil {
			return &typecheck.EachBinding{Key: typecheck.TInteger(), Value: *iterable.Elem}
		}
	case typecheck.Map:
		if iterable.Elem != nil {
			return &typecheck.EachBinding{Key: typecheck.TString(), Value: *iterable.Elem}
		}
	case typecheck.Object:
		return &typecheck.EachBinding{Key: typecheck.TString(), Value: typecheck.TUnknown()}
	case typecheck.Tuple:
		return &typecheck.EachBinding{Key: typecheck.TInteger(), Value: typecheck.TUnknown()}
	}
	return &typecheck.EachBinding{Key: typecheck.TUnknown(), Value: typecheck.TUnknown()}
}
