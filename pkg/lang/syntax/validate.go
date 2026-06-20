package syntax

import (
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
	case FileManifest:
		validateManifestFile(f.Manifest, f.S.Start, errs)
	case FileLock:
		validateLockFile(f.Lock, f.S.Start, errs)
	case FileLibrary:
		validateLibraryFile(f.Library, f.S.Start, errs)
	default:
		errs.Addf(parse.ErrSchema, f.S.Start,
			"cannot validate UB syntax file: file kind is unknown")
	}
	return errs
}

func validateFactoryBody(body FactoryBody, errs *parse.ErrorList) {
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
	validateNodeDecls(body.Data, "data", dataBodyMeta, errs)
	validateNodeDecls(body.Actions, "action", actionBodyMeta, errs)
	mergeErrors(errs, lang.ValidateOutputs(outputDeclsObject(body.Outputs)))
	mergeErrors(errs, lang.ValidateComprehensionBindings(parseFactoryBody(body)))
	mergeErrors(errs, lang.ValidateCalls(parseFactoryBody(body)))
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
			role:         "state",
			selectorName: "backend",
			example:      "state: local { ... }",
			locals:       localNames,
		}, errs)
	}
	if stack.Encryption == nil {
		errs.Addf(parse.ErrSchema, stack.S.Start, "stack file requires encryption")
		return
	}
	validateStackResolverBody(stack.Encryption.Body, stackResolverBodyRule{
		role:         "encryption",
		selectorName: "key-source",
		example:      "encryption: noop { ... }",
		locals:       localNames,
	}, errs)
}

type stackResolverBodyRule struct {
	role         string
	selectorName string
	example      string
	locals       map[string]bool
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
				"%s body uses a %s selector before the body, "+
					"not a body meta key; write %s",
				rule.role, rule.selectorName, rule.example)
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

func validateManifestFile(manifest *ManifestFile, pos parse.Position, errs *parse.ErrorList) {
	if manifest == nil {
		errs.Addf(parse.ErrSchema, pos, "manifest file is missing manifest body")
		return
	}
	mergeErrors(errs, lang.ValidateManifestRequires(manifestRequiresObject(manifest.Requires)))
	mergeErrors(errs, lang.ValidateManifestReplace(manifestReplaceObject(manifest.Replace)))
}

func validateLockFile(lock *LockFile, pos parse.Position, errs *parse.ErrorList) {
	if lock == nil {
		errs.Addf(parse.ErrSchema, pos, "lock file is missing lock body")
		return
	}
	if lock.Version == nil {
		errs.Addf(parse.ErrSchema, lock.S.Start, "lock: missing version")
	} else if lock.Version.ParsedInt != 1 {
		errs.Addf(parse.ErrSchema, lock.Version.S.Start,
			"lock version must be 1, got %d", lock.Version.ParsedInt)
	}
	if lock.Toolchain == nil {
		errs.Addf(parse.ErrSchema, lock.S.Start, "lock: missing toolchain")
	}
	for _, dep := range lock.Deps {
		if dep.Kind.Name == "ub" && dep.Hash != nil && !hasHashAlgorithm(dep.Hash.Value) {
			errs.Addf(parse.ErrSchema, dep.Hash.S.Start,
				"lock dependency %s: hash must include an algorithm prefix", dep.ID.Value)
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
	lit *parse.StringLit,
	errs *parse.ErrorList,
) (stateref.EntryRef, bool) {
	ref, err := stateref.Parse(lit.Value)
	if err != nil {
		errs.Addf(parse.ErrSchema, lit.S.Start,
			"state-moves[%d].%s: %v", i, field, err)
		return stateref.EntryRef{}, false
	}
	return ref, true
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
		if fld.Key.Name == "@timeout" {
			validateTimeout(fld, what, name, errs)
		}
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

func parseFactoryBody(body FactoryBody) *parse.File {
	return &parse.File{
		S:    body.S,
		Kind: parse.FileFactory,
		Body: factoryBodyObject(body, nodeDeclsObject),
	}
}

func factoryBodyObject(
	body FactoryBody,
	nodes func([]NodeDecl) *parse.ObjectLit,
) *parse.ObjectLit {
	obj := &parse.ObjectLit{S: body.S}
	if body.Description != nil {
		obj.Fields = append(obj.Fields,
			identField("description", body.Description.S, body.Description))
	}
	if len(body.Inputs) > 0 {
		inputs := inputDeclsObject(body.Inputs)
		obj.Fields = append(obj.Fields, identField("inputs", inputs.S, inputs))
	}
	if len(body.Locals) > 0 {
		locals := localDeclsObject(body.Locals)
		obj.Fields = append(obj.Fields, identField("locals", locals.S, locals))
	}
	if len(body.Constraints) > 0 {
		constraints := constraintDeclsArray(body.Constraints)
		obj.Fields = append(obj.Fields,
			identField("constraints", constraints.S, constraints))
	}
	if len(body.Imports) > 0 {
		imports := importDeclsObject(body.Imports)
		obj.Fields = append(obj.Fields, identField("imports", imports.S, imports))
	}
	if len(body.LibraryConfigs) > 0 {
		cfgs := libraryConfigDeclsObject(body.LibraryConfigs)
		obj.Fields = append(obj.Fields, identField("library-configs", cfgs.S, cfgs))
	}
	if len(body.Resources) > 0 {
		resources := nodes(body.Resources)
		obj.Fields = append(obj.Fields, identField("resources", resources.S, resources))
	}
	if len(body.Data) > 0 {
		data := nodes(body.Data)
		obj.Fields = append(obj.Fields, identField("data", data.S, data))
	}
	if len(body.Actions) > 0 {
		actions := nodes(body.Actions)
		obj.Fields = append(obj.Fields, identField("actions", actions.S, actions))
	}
	if len(body.Outputs) > 0 {
		outputs := outputDeclsObject(body.Outputs)
		obj.Fields = append(obj.Fields, identField("outputs", outputs.S, outputs))
	}
	return obj
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

func nodeDeclsObject(decls []NodeDecl) *parse.ObjectLit {
	obj := &parse.ObjectLit{}
	if len(decls) > 0 {
		obj.S = decls[0].S
	}
	for _, decl := range decls {
		obj.Fields = append(obj.Fields, pathField([]string{
			decl.Selector.Alias.Name,
			decl.Selector.Export.Name,
			decl.Name.Name,
		}, decl.S, decl.Body))
	}
	return obj
}

func nodeDeclsSelectorObject(decls []NodeDecl) *parse.ObjectLit {
	obj := &parse.ObjectLit{}
	if len(decls) > 0 {
		obj.S = decls[0].S
	}
	for _, decl := range decls {
		obj.Fields = append(obj.Fields, selectorField(decl))
	}
	return obj
}

func selectorField(decl NodeDecl) *parse.Field {
	return &parse.Field{
		S: decl.S,
		Key: parse.FieldKey{
			S:    decl.Name.S,
			Kind: parse.FieldIdent,
			Name: decl.Name.Name,
		},
		Decl: &parse.SelectorBody{
			S: decl.S,
			Selector: parse.Selector{
				S: decl.Selector.S,
				Parts: []parse.Ident{
					{S: decl.Selector.Alias.S, Name: decl.Selector.Alias.Name},
					{S: decl.Selector.Export.S, Name: decl.Selector.Export.Name},
				},
			},
			Body: decl.Body,
		},
	}
}

func manifestRequiresObject(decls []ManifestRequire) *parse.ObjectLit {
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

func manifestReplaceObject(decls []ManifestReplace) *parse.ObjectLit {
	obj := &parse.ObjectLit{}
	if len(decls) > 0 {
		obj.S = decls[0].S
	}
	for _, decl := range decls {
		obj.Fields = append(obj.Fields, stringField(decl.ID.Value, decl.ID.S, decl.Path))
	}
	return obj
}

func expandShortNodeRefs(obj *parse.ObjectLit, body FactoryBody) {
	selectors := map[string]map[string][]string{
		"resource": nodeSelectorRefs(body.Resources),
		"data":     nodeSelectorRefs(body.Data),
		"action":   nodeSelectorRefs(body.Actions),
	}
	lang.Walk(obj, func(expr parse.Expr) {
		dp, ok := expr.(*parse.DotPath)
		if !ok || dp.Root == nil || len(dp.Segments) == 0 {
			return
		}
		byName := selectors[dp.Root.Name]
		if len(byName) == 0 {
			return
		}
		first := dp.Segments[0]
		if first.Name == "" || first.Index != nil || first.Splat || first.Guarded {
			return
		}
		prefix := byName[first.Name]
		if len(prefix) == 0 {
			return
		}
		dp.Segments = append(selectorSegments(first.S, prefix), dp.Segments[1:]...)
	})
}

func nodeSelectorRefs(nodes []NodeDecl) map[string][]string {
	out := make(map[string][]string, len(nodes))
	for _, node := range nodes {
		out[node.Name.Name] = []string{
			node.Selector.Alias.Name,
			node.Selector.Export.Name,
			node.Name.Name,
		}
	}
	return out
}

func selectorSegments(span parse.Span, names []string) []parse.DotSegment {
	out := make([]parse.DotSegment, 0, len(names))
	for _, name := range names {
		out = append(out, parse.DotSegment{S: span, Name: name})
	}
	return out
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

func pathField(path []string, span parse.Span, value parse.Expr) *parse.Field {
	return &parse.Field{
		S: span,
		Key: parse.FieldKey{
			S:    span,
			Kind: parse.FieldPath,
			Path: path,
		},
		Value: value,
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
