package syntax

import (
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/stateref"
)

var (
	resourceBodyMeta = map[string]bool{
		"@depends-on": true,
		"@for-each":   true,
		"@lock":       true,
		"@timeout":    true,
	}
	dataBodyMeta = map[string]bool{
		"@depends-on": true,
		"@for-each":   true,
		"@lock":       true,
		"@timeout":    true,
	}
	actionBodyMeta = map[string]bool{
		"@depends-on": true,
		"@for-each":   true,
		"@lock":       true,
		"@timeout":    true,
		"@trigger":    true,
	}
)

func ValidateFile(f *File) *parse.ErrorList {
	errs := parse.NewErrorList(0)
	if f == nil {
		errs.Addf(parse.ErrSchema, parse.Position{}, "cannot validate nil UB syntax file")
		return errs
	}
	switch f.Kind {
	case FileFactory:
		if f.Factory == nil {
			errs.Addf(parse.ErrSchema, f.S.Start, "factory file is missing factory body")
			return errs
		}
		validateFactoryBody(f.Factory.Body, errs)
	case FileStack:
		validateStackFile(f.Stack, f.S.Start, errs)
	case FileProject:
		validateProjectFile(f.Project, f.S.Start, errs)
	case FileProjectLock:
		validateProjectLockFile(f.ProjectLock, f.S.Start, errs)
	case FileLibrary:
		validateLibraryFile(f.Library, f.S.Start, errs)
	default:
		errs.Addf(parse.ErrSchema, f.S.Start,
			"cannot validate UB syntax file: file kind is unknown")
	}
	return errs
}

func validateFactoryBody(body FactoryBody, errs *parse.ErrorList) {
	validateFactoryBodyTyped(body, errs)
}

func validateFactoryBodyTyped(body FactoryBody, errs *parse.ErrorList) {
	inputs := inputDeclsObject(body.Inputs)
	mergeErrors(errs, lang.ValidateInputDeclarations(inputs))
	mergeErrors(errs, lang.ValidateLocals(localDeclsObject(body.Locals)))
	constraints := constraintDeclsArray(body.Constraints)
	mergeErrors(errs, lang.ValidateConstraints(constraints))
	mergeErrors(errs, lang.ValidateConstraintReferences(constraints, inputs))
	mergeErrors(errs, lang.ValidateImports(importDeclsObject(body.Imports)))
	validateLibraryConfigTypePlacement(body.Inputs, errs)
	validateLibraryConfigDecls(body.LibraryConfigs, errs)
	validateStateMoves(body.StateMoves, errs)
	validateNodeDecls(body.Resources, "resource", resourceBodyMeta, errs)
	validateNodeDecls(body.Data, "data source", dataBodyMeta, errs)
	validateNodeDecls(body.Actions, "action", actionBodyMeta, errs)
	mergeErrors(errs, lang.ValidateOutputs(outputDeclsObject(body.Outputs)))
	validateFactoryComprehensionBindings(body, errs)
	validateFactoryCalls(body, errs)
}

func validateStackFile(stack *StackFile, pos parse.Position, errs *parse.ErrorList) {
	if stack == nil {
		errs.Addf(parse.ErrSchema, pos, "stack file is missing stack body")
		return
	}
	locals := localDeclsObject(stack.Locals)
	localNames := stackLocalNames(stack.Locals)
	mergeErrors(errs, lang.ValidateLocals(locals))
	mergeErrors(errs, lang.ValidateStackLocals(locals))
	if stack.Factory != nil {
		validateStackFactory(stack.Factory, localNames, errs)
	}
	if stack.State != nil {
		validateStackResolverBody(stack.State.Body, stackResolverBodyRule{
			role:    "state",
			metaKey: "@backend",
			example: "state: local { ... }",
			locals:  localNames,
		}, errs)
	}
	if stack.Encryption == nil {
		errs.Addf(parse.ErrSchema, stack.S.Start, "stack file requires encryption")
		return
	}
	validateStackResolverBody(stack.Encryption.Body, stackResolverBodyRule{
		role:    "encryption",
		metaKey: "@key-source",
		example: "encryption: noop { ... }",
		locals:  localNames,
	}, errs)
}

type stackResolverBodyRule struct {
	role    string
	metaKey string
	example string
	locals  map[string]bool
}

func validateStackResolverBody(
	body *parse.ObjectLit,
	rule stackResolverBodyRule,
	errs *parse.ErrorList,
) {
	if body == nil {
		return
	}
	seen := make(map[string]parse.Position, len(body.Fields))
	staticBody := &parse.ObjectLit{S: body.S}
	for _, fld := range body.Fields {
		if fld.Key.Kind == parse.FieldString {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"%s body key must be a bare identifier, got quoted string %q",
				rule.role, fld.Key.String)
			continue
		}
		if fld.Key.IsMeta() {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"%s body uses %s as a meta key; write %s",
				rule.role, rule.metaKey, rule.example)
			continue
		}
		name := fld.Key.Name
		if prev, dup := seen[name]; dup {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"%s body: duplicate key %q (first defined at %s)", rule.role, name, prev)
			continue
		}
		seen[name] = fld.Key.S.Start
		if rule.role == "state" && name == "encryption" {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"state body: encryption is its own top-level block; move it out of state")
		}
		staticBody.Fields = append(staticBody.Fields, fld)
	}
	mergeErrors(errs, lang.ValidateStackInputs(staticBody, rule.locals))
}

func validateStackFactory(
	factory *StackFactoryBlock,
	locals map[string]bool,
	errs *parse.ErrorList,
) {
	cfg := &parse.ObjectLit{S: factory.S}
	if factory.Pin != nil {
		cfg.Fields = append(cfg.Fields, identField("pin", factory.S, factory.Pin))
	}
	if factory.Inputs != nil {
		cfg.Fields = append(cfg.Fields, identField("inputs", factory.Inputs.S, factory.Inputs))
	}
	mergeErrors(errs, lang.ValidateStackFactory(cfg, locals))
}

func validateProjectFile(project *ProjectFile, pos parse.Position, errs *parse.ErrorList) {
	if project == nil {
		errs.Addf(parse.ErrSchema, pos, "project file is missing project body")
		return
	}
	mergeErrors(errs, lang.ValidateProjectRequires(projectRequiresObject(project.Requires)))
	mergeErrors(errs, lang.ValidateProjectReplace(projectReplaceObject(project.Replace)))
}

func validateProjectLockFile(projectLock *ProjectLockFile, pos parse.Position, errs *parse.ErrorList) {
	if projectLock == nil {
		errs.Addf(parse.ErrSchema, pos, "project-lock file is missing project-lock body")
		return
	}
	if projectLock.Version == nil {
		errs.Addf(parse.ErrSchema, projectLock.S.Start, "project-lock: missing version")
	} else if projectLock.Version.ParsedInt != 1 {
		errs.Addf(parse.ErrSchema, projectLock.Version.S.Start,
			"project-lock version must be 1, got %d", projectLock.Version.ParsedInt)
	}
	if projectLock.Toolchain == nil {
		errs.Addf(parse.ErrSchema, projectLock.S.Start, "project-lock: missing toolchain")
	}
	for _, dep := range projectLock.Deps {
		if dep.Kind.Name == "ub" && dep.Hash != nil && !hasHashAlgorithm(dep.Hash.Value) {
			errs.Addf(parse.ErrSchema, dep.Hash.S.Start,
				"project-lock dependency %s: hash must include an algorithm prefix", dep.ID.Value)
		}
	}
}

func hasHashAlgorithm(hash string) bool {
	for i, r := range hash {
		if r == ':' {
			return i > 0 && i < len(hash)-1
		}
	}
	return false
}

func validateLibraryFile(library *LibraryFile, pos parse.Position, errs *parse.ErrorList) {
	if library == nil {
		errs.Addf(parse.ErrSchema, pos, "library file is missing library body")
		return
	}
	seen := make(map[string]parse.Position, len(library.Exports))
	for _, export := range library.Exports {
		key := string(export.Kind) + "." + export.Name.Name
		if prev, dup := seen[key]; dup {
			errs.Addf(parse.ErrSchema, export.Name.S.Start,
				"duplicate library export %s (first defined at %s)", key, prev)
			continue
		}
		seen[key] = export.Name.S.Start
		validateFactoryBody(export.Body, errs)
	}
}

func validateLibraryConfigTypePlacement(inputs []InputDecl, errs *parse.ErrorList) {
	for _, input := range inputs {
		if input.Type == nil {
			continue
		}
		if _, ok := input.Type.(*parse.TypeLibraryConfig); ok {
			continue
		}
		validateNestedLibraryConfigTypes(input.Name.Name, input.Type, errs)
	}
}

func validateNestedLibraryConfigTypes(
	input string,
	t parse.TypeExpr,
	errs *parse.ErrorList,
) {
	switch v := t.(type) {
	case *parse.TypeLibraryConfig:
		errs.Addf(parse.ErrSchema, v.S.Start,
			"input %q: library-config is only valid as the direct input type", input)
	case *parse.TypeList:
		validateNestedLibraryConfigTypes(input, v.Elem, errs)
	case *parse.TypeMap:
		validateNestedLibraryConfigTypes(input, v.Elem, errs)
	case *parse.TypeOptional:
		validateNestedLibraryConfigTypes(input, v.Elem, errs)
	case *parse.TypeTuple:
		for _, elem := range v.Elements {
			validateNestedLibraryConfigTypes(input, elem, errs)
		}
	case *parse.TypeObject:
		for _, field := range v.Fields {
			if field.Type != nil {
				validateNestedLibraryConfigTypes(input, field.Type, errs)
			}
			validateNestedLibraryConfigDecl(input, field.Decl, errs)
		}
	}
}

func validateNestedLibraryConfigDecl(
	input string,
	decl *parse.ObjectLit,
	errs *parse.ErrorList,
) {
	if decl == nil {
		return
	}
	for _, fld := range decl.Fields {
		if fld.Key.Kind != parse.FieldIdent || fld.Key.Name != "type" {
			continue
		}
		if t, ok := fld.Value.(parse.TypeExpr); ok {
			validateNestedLibraryConfigTypes(input, t, errs)
		}
	}
}

func validateLibraryConfigDecls(decls []LibraryConfigDecl, errs *parse.ErrorList) {
	seen := map[string]parse.Position{}
	for _, decl := range decls {
		if prev, dup := seen[decl.Alias.Name]; dup {
			errs.Addf(parse.ErrSchema, decl.Alias.S.Start,
				"duplicate library config %q (first defined at %s)",
				decl.Alias.Name, prev)
			continue
		}
		seen[decl.Alias.Name] = decl.Alias.S.Start
	}
}

func validateStateMoves(decls []StateMoveDecl, errs *parse.ErrorList) {
	refs := make([]stateMoveRefs, 0, len(decls))
	seen := map[string]parse.Position{}
	for i, decl := range decls {
		if decl.From == nil || decl.To == nil {
			continue
		}
		from, ok := validateStateMoveRef(i, "from", decl.From, errs)
		if !ok {
			continue
		}
		to, ok := validateStateMoveRef(i, "to", decl.To, errs)
		if !ok {
			continue
		}
		if stateref.Same(from, to) {
			errs.Addf(parse.ErrSchema, decl.From.S.Start,
				"state-moves[%d]: from and to must differ", i)
			continue
		}
		if prev, dup := seen[from.String()]; dup {
			errs.Addf(parse.ErrSchema, decl.From.S.Start,
				"state-moves[%d]: duplicate from %s (first defined at %s)",
				i, from.String(), prev)
			continue
		}
		seen[from.String()] = decl.From.S.Start
		refs = append(refs, stateMoveRefs{index: i, from: from, to: to, pos: decl.From.S.Start})
	}
	validateStateMoveCycles(refs, errs)
}

type stateMoveRefs struct {
	index int
	from  stateref.EntryRef
	to    stateref.EntryRef
	pos   parse.Position
}

func validateStateMoveRef(
	i int,
	field string,
	moveRef *StateMoveRef,
	errs *parse.ErrorList,
) (stateref.EntryRef, bool) {
	if moveRef.Ref.Address == "" {
		errs.Addf(parse.ErrSchema, moveRef.S.Start,
			"state-moves[%d].%s: expected state ref", i, field)
		return stateref.EntryRef{}, false
	}
	return moveRef.Ref, true
}

func validateStateMoveCycles(refs []stateMoveRefs, errs *parse.ErrorList) {
	edges := map[string]stateref.EntryRef{}
	for _, ref := range refs {
		edges[ref.from.String()] = ref.to
	}
	for _, ref := range refs {
		seen := map[string]int{}
		var path []string
		cur := ref.from
		for {
			key := cur.String()
			if idx, ok := seen[key]; ok {
				cycle := append(path[idx:], key)
				errs.Addf(parse.ErrSchema, ref.pos,
					"state-moves[%d]: cycle: %s", ref.index, strings.Join(cycle, " -> "))
				break
			}
			next, ok := edges[key]
			if !ok {
				break
			}
			seen[key] = len(path)
			path = append(path, key)
			cur = next
		}
		delete(edges, ref.from.String())
	}
}

func validateNodeDecls(
	nodes []NodeDecl,
	what string,
	allowed map[string]bool,
	errs *parse.ErrorList,
) {
	seen := make(map[string]parse.Position, len(nodes))
	for _, node := range nodes {
		if prev, dup := seen[node.Name.Name]; dup {
			errs.Addf(parse.ErrSchema, node.Name.S.Start,
				"duplicate %s %s (first defined at %s)", what, node.Name.Name, prev)
			continue
		}
		seen[node.Name.Name] = node.Name.S.Start
		if node.Body == nil {
			errs.Addf(parse.ErrSchema, node.S.Start,
				"%s %s: body must be an object", what, node.Name.Name)
			continue
		}
		validateNodeBody(node.Body, what, node.Name.Name, allowed, errs)
	}
}

func validateNodeBody(
	body *parse.ObjectLit,
	what string,
	name string,
	allowed map[string]bool,
	errs *parse.ErrorList,
) {
	seenMeta := make(map[string]parse.Position, len(body.Fields))
	for _, fld := range body.Fields {
		if fld.Key.Kind != parse.FieldIdent || !fld.Key.IsMeta() {
			continue
		}
		if prev, dup := seenMeta[fld.Key.Name]; dup {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"%s %s: duplicate meta key %q (first defined at %s)",
				what, name, fld.Key.Name, prev)
			continue
		}
		seenMeta[fld.Key.Name] = fld.Key.S.Start
		if !allowed[fld.Key.Name] {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"%s %s: meta key %q is not allowed", what, name, fld.Key.Name)
			continue
		}
		switch fld.Key.Name {
		case "@timeout":
			validateTimeout(fld, what, name, errs)
		case "@lock":
			validateLock(fld, what, name, errs)
		case "@depends-on":
			validateDependsOn(fld, what, name, errs)
		}
	}
}

func validateLock(
	fld *parse.Field,
	what string,
	name string,
	errs *parse.ErrorList,
) {
	if _, ok := fld.Value.(*parse.StringLit); ok {
		return
	}
	errs.Addf(parse.ErrSchema, fld.Value.Span().Start,
		"%s %s: @lock must be a string literal", what, name)
}

func validateDependsOn(
	fld *parse.Field,
	what string,
	name string,
	errs *parse.ErrorList,
) {
	arr, ok := fld.Value.(*parse.ArrayLit)
	if !ok {
		errs.Addf(parse.ErrSchema, fld.Value.Span().Start,
			"%s %s: @depends-on must be an array of resource, data-source, or action refs",
			what, name)
		return
	}
	for i, elem := range arr.Elements {
		path, ok := elem.(*parse.DotPath)
		if !ok {
			errs.Addf(parse.ErrSchema, elem.Span().Start,
				"%s %s: @depends-on[%d] must be an unquoted resource, data-source, or action ref",
				what, name, i)
			continue
		}
		if !isNodeRefRoot(path.Root) || len(path.Segments) == 0 || path.Segments[0].Name == "" {
			errs.Addf(parse.ErrSchema, path.Span().Start,
				"%s %s: @depends-on[%d] must name a resource, data-source, or action",
				what, name, i)
		}
	}
}

func isNodeRefRoot(root *parse.Ident) bool {
	if root == nil {
		return false
	}
	switch root.Name {
	case "resource", "data-source", "action":
		return true
	default:
		return false
	}
}

func validateTimeout(
	fld *parse.Field,
	what string,
	name string,
	errs *parse.ErrorList,
) {
	s, ok := fld.Value.(*parse.StringLit)
	if !ok {
		errs.Addf(parse.ErrSchema, fld.Value.Span().Start,
			"%s %s: @timeout must be a duration string like '30s'", what, name)
		return
	}
	if _, err := time.ParseDuration(s.Value); err != nil {
		errs.Addf(parse.ErrSchema, fld.Value.Span().Start,
			"%s %s: @timeout %q is not a valid duration", what, name, s.Value)
	}
}

func validateFactoryComprehensionBindings(body FactoryBody, errs *parse.ErrorList) {
	factoryBodyExprs(body, func(e parse.Expr) {
		checkFactoryComprehensionBindings(e, map[string]parse.Position{}, errs)
	})
}

func checkFactoryComprehensionBindings(
	e parse.Expr,
	bound map[string]parse.Position,
	errs *parse.ErrorList,
) {
	switch v := e.(type) {
	case nil:
		return
	case *parse.ObjectLit:
		for _, fld := range v.Fields {
			checkFactoryComprehensionBindings(fld.Value, bound, errs)
		}
	case *parse.ArrayLit:
		for _, el := range v.Elements {
			checkFactoryComprehensionBindings(el, bound, errs)
		}
	case *parse.Call:
		for _, a := range v.Args {
			checkFactoryComprehensionBindings(a, bound, errs)
		}
	case *parse.Infix:
		checkFactoryComprehensionBindings(v.Left, bound, errs)
		checkFactoryComprehensionBindings(v.Right, bound, errs)
	case *parse.Prefix:
		checkFactoryComprehensionBindings(v.Expr, bound, errs)
	case *parse.DotPath:
		for _, seg := range v.Segments {
			checkFactoryComprehensionBindings(seg.Index, bound, errs)
		}
	case *parse.Conditional:
		checkFactoryComprehensionBindings(v.Cond, bound, errs)
		checkFactoryComprehensionBindings(v.Then, bound, errs)
		checkFactoryComprehensionBindings(v.Else, bound, errs)
	case *parse.Comprehension:
		checkFactoryComprehensionBindings(v.Source, bound, errs)
		inner := make(map[string]parse.Position, len(bound)+len(v.Names))
		maps.Copy(inner, bound)
		for i, n := range v.Names {
			if slices.Contains(v.Names[:i], n) {
				errs.Addf(parse.ErrSchema, v.S.Start, "comprehension binds %s twice", n)
				continue
			}
			if prev, dup := bound[n]; dup {
				errs.Addf(parse.ErrSchema, v.S.Start,
					"binding %s shadows an enclosing comprehension binding"+
						" (bound at %s); rename it", n, prev)
			}
			inner[n] = v.S.Start
		}
		checkFactoryComprehensionBindings(v.Key, inner, errs)
		checkFactoryComprehensionBindings(v.Value, inner, errs)
		checkFactoryComprehensionBindings(v.Filter, inner, errs)
	case *parse.InterpolatedString:
		for _, part := range v.Parts {
			checkFactoryComprehensionBindings(part.Expr, bound, errs)
		}
	}
}

func validateFactoryCalls(body FactoryBody, errs *parse.ErrorList) {
	imports := factoryImportAliases(body.Imports)
	factoryBodyExprs(body, func(root parse.Expr) {
		lang.Walk(root, func(e parse.Expr) {
			c, ok := e.(*parse.Call)
			if !ok {
				return
			}
			if c.Library == nil {
				pos := c.S.Start
				name := ""
				if c.Callee != nil {
					pos = c.Callee.S.Start
					name = c.Callee.Name
				}
				errs.Addf(parse.ErrResolve, pos,
					"function %q must be qualified with %s or an imported library,"+
						" e.g. %s.%s(...)",
					name, lang.CoreNamespace, lang.CoreNamespace, name)
				return
			}
			if c.Library.Name == lang.CoreNamespace {
				return
			}
			if strings.HasPrefix(c.Library.Name, "@") {
				errs.Addf(parse.ErrResolve, c.Library.S.Start,
					"%q is not a namespace; the language provides only %s",
					c.Library.Name, lang.CoreNamespace)
				return
			}
			if !imports[c.Library.Name] {
				errs.Addf(parse.ErrResolve, c.Library.S.Start,
					"library %q is not imported (called as %s.%s)",
					c.Library.Name, c.Library.Name, c.Func.Name)
			}
		})
	})
}

func factoryImportAliases(decls []ImportDecl) map[string]bool {
	out := make(map[string]bool, len(decls))
	for _, decl := range decls {
		out[decl.Alias.Name] = true
	}
	return out
}

func factoryBodyExprs(body FactoryBody, visit func(parse.Expr)) {
	for _, input := range body.Inputs {
		visit(input.Body)
	}
	for _, local := range body.Locals {
		visit(local.Value)
	}
	for _, constraint := range body.Constraints {
		visit(constraint.Value)
	}
	for _, cfg := range body.LibraryConfigs {
		visit(cfg.Value)
	}
	for _, node := range body.Resources {
		visit(node.Body)
	}
	for _, node := range body.Data {
		visit(node.Body)
	}
	for _, node := range body.Actions {
		visit(node.Body)
	}
	for _, output := range body.Outputs {
		visit(output.Body)
	}
}

func inputDeclsObject(decls []InputDecl) *parse.ObjectLit {
	obj := &parse.ObjectLit{}
	if len(decls) > 0 {
		obj.S = decls[0].S
	}
	for _, decl := range decls {
		obj.Fields = append(obj.Fields, identField(decl.Name.Name, decl.Name.S, decl.Body))
	}
	return obj
}

func localDeclsObject(decls []LocalDecl) *parse.ObjectLit {
	obj := &parse.ObjectLit{}
	if len(decls) > 0 {
		obj.S = decls[0].S
	}
	for _, decl := range decls {
		obj.Fields = append(obj.Fields, identField(decl.Name.Name, decl.Name.S, decl.Value))
	}
	return obj
}

func constraintDeclsArray(decls []ConstraintDecl) *parse.ArrayLit {
	arr := &parse.ArrayLit{}
	if len(decls) > 0 {
		arr.S = decls[0].S
	}
	for _, decl := range decls {
		arr.Elements = append(arr.Elements, decl.Value)
	}
	return arr
}

func importDeclsObject(decls []ImportDecl) *parse.ObjectLit {
	obj := &parse.ObjectLit{}
	if len(decls) > 0 {
		obj.S = decls[0].S
	}
	for _, decl := range decls {
		obj.Fields = append(obj.Fields, identField(decl.Alias.Name, decl.Alias.S, decl.Ref))
	}
	return obj
}

func outputDeclsObject(decls []OutputDecl) *parse.ObjectLit {
	obj := &parse.ObjectLit{}
	if len(decls) > 0 {
		obj.S = decls[0].S
	}
	for _, decl := range decls {
		obj.Fields = append(obj.Fields, identField(decl.Name.Name, decl.Name.S, decl.Body))
	}
	return obj
}

func libraryConfigDeclsObject(decls []LibraryConfigDecl) *parse.ObjectLit {
	obj := &parse.ObjectLit{}
	if len(decls) > 0 {
		obj.S = decls[0].S
	}
	for _, decl := range decls {
		obj.Fields = append(obj.Fields, identField(decl.Alias.Name, decl.Alias.S, decl.Value))
	}
	return obj
}

func projectRequiresObject(decls []ProjectRequire) *parse.ObjectLit {
	obj := &parse.ObjectLit{}
	if len(decls) > 0 {
		obj.S = decls[0].S
	}
	for _, decl := range decls {
		body := &parse.ObjectLit{S: decl.S}
		if decl.Version != nil {
			body.Fields = append(body.Fields,
				identField("version", decl.Version.S, decl.Version))
		}
		if decl.Indirect != nil {
			body.Fields = append(body.Fields,
				identField("indirect", decl.Indirect.S, decl.Indirect))
		}
		obj.Fields = append(obj.Fields, stringField(decl.ID.Value, decl.ID.S, body))
	}
	return obj
}

func projectReplaceObject(decls []ProjectReplace) *parse.ObjectLit {
	obj := &parse.ObjectLit{}
	if len(decls) > 0 {
		obj.S = decls[0].S
	}
	for _, decl := range decls {
		obj.Fields = append(obj.Fields, stringField(decl.ID.Value, decl.ID.S, decl.Path))
	}
	return obj
}

func stackLocalNames(decls []LocalDecl) map[string]bool {
	names := make(map[string]bool, len(decls))
	for _, decl := range decls {
		names[decl.Name.Name] = true
	}
	return names
}

func identField(name string, span parse.Span, value parse.Expr) *parse.Field {
	return &parse.Field{
		S: span,
		Key: parse.FieldKey{
			S:    span,
			Kind: parse.FieldIdent,
			Name: name,
		},
		Value: value,
	}
}

func stringField(value string, span parse.Span, expr parse.Expr) *parse.Field {
	return &parse.Field{
		S: span,
		Key: parse.FieldKey{
			S:      span,
			Kind:   parse.FieldString,
			String: value,
		},
		Value: expr,
	}
}

func mergeErrors(dst *parse.ErrorList, src *parse.ErrorList) {
	if src == nil {
		return
	}
	for _, err := range src.Errors() {
		dst.Add(err)
	}
}
