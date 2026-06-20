package syntax

import (
	"time"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/parse"
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
	validateNoConfigurationDecls(body.Configurations, errs)
	validateLibraryConfigTypePlacement(body.Inputs, errs)
	validateLibraryConfigDecls(body.LibraryConfigs, errs)
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
	validateNoStackConfigurationValues(factory.Configurations, errs)
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

func validateNoConfigurationDecls(decls []ConfigurationDecl, errs *parse.ErrorList) {
	for _, decl := range decls {
		errs.Addf(parse.ErrSchema, decl.S.Start,
			"configurations block is not supported; use library-configs")
	}
}

func validateNoStackConfigurationValues(values []ConfigurationValue, errs *parse.ErrorList) {
	for _, value := range values {
		errs.Addf(parse.ErrSchema, value.S.Start,
			"factory.configurations is not supported; pass library configs through factory.inputs")
	}
}

func validateConfigurationDecls(decls []ConfigurationDecl, errs *parse.ErrorList) {
	seenNames := map[string]parse.Position{}
	seenDefaults := map[string]parse.Position{}
	for _, decl := range decls {
		label := configurationDeclLabel(decl)
		if decl.Name == nil {
			checkDuplicateDefault(decl.Selector, seenDefaults, errs)
		} else if prev, dup := seenNames[decl.Name.Name]; dup {
			errs.Addf(parse.ErrSchema, decl.Name.S.Start,
				"duplicate configuration name %q (first defined at %s)",
				decl.Name.Name, prev)
		} else {
			seenNames[decl.Name.Name] = decl.Name.S.Start
		}
		validateConfigurationBody(label, decl.Body, errs)
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

func validateConfigurationValues(values []ConfigurationValue, errs *parse.ErrorList) {
	seenNames := map[string]parse.Position{}
	seenDefaults := map[string]parse.Position{}
	for _, value := range values {
		if value.Name == nil {
			checkDuplicateDefault(value.Selector, seenDefaults, errs)
		} else if prev, dup := seenNames[value.Name.Name]; dup {
			errs.Addf(parse.ErrSchema, value.Name.S.Start,
				"duplicate configuration name %q (first defined at %s)",
				value.Name.Name, prev)
		} else {
			seenNames[value.Name.Name] = value.Name.S.Start
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

func validateStackConfigurationValues(
	values []ConfigurationValue,
	locals map[string]bool,
	errs *parse.ErrorList,
) {
	validateConfigurationValues(values, errs)
	for _, value := range values {
		expr := configurationValueExpr(value)
		if expr == nil {
			continue
		}
		if body, ok := expr.(*parse.ObjectLit); ok {
			validateConfigurationBody(configurationValueLabel(value), body, errs)
			mergeErrors(errs, lang.ValidateStackInputs(body, locals))
			continue
		}
		block := &parse.ObjectLit{S: value.S}
		block.Fields = append(block.Fields, identField("value", value.S, expr))
		mergeErrors(errs, lang.ValidateStackInputs(block, locals))
	}
}

func checkDuplicateDefault(
	selector Ident,
	seen map[string]parse.Position,
	errs *parse.ErrorList,
) {
	if prev, dup := seen[selector.Name]; dup {
		errs.Addf(parse.ErrSchema, selector.S.Start,
			"duplicate default configuration for %q (first defined at %s)",
			selector.Name, prev)
		return
	}
	seen[selector.Name] = selector.S.Start
}

func validateConfigurationBody(label string, body *parse.ObjectLit, errs *parse.ErrorList) {
	if body == nil {
		return
	}
	for _, fld := range body.Fields {
		if fld.Key.Kind == parse.FieldIdent && fld.Key.IsMeta() {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"configuration %s: meta key %q is not allowed", label, fld.Key.Name)
		}
	}
}

func configurationDeclLabel(decl ConfigurationDecl) string {
	if decl.Name != nil {
		return decl.Name.Name
	}
	return decl.Selector.Name
}

func configurationValueLabel(value ConfigurationValue) string {
	if value.Name != nil {
		return value.Name.Name
	}
	return value.Selector.Name
}

func validateConfigurationRefs(body FactoryBody, errs *parse.ErrorList) {
	refs := configurationRefs(body.Configurations)
	for _, decl := range body.Configurations {
		label := configurationDeclLabel(decl)
		validateConfigurationBodyRefs(label, decl.Body, refs, errs)
	}
	validateConfigurationNodeRefs(body.Resources, refs, errs)
	validateConfigurationNodeRefs(body.Data, refs, errs)
	validateConfigurationNodeRefs(body.Actions, refs, errs)
}

func validateConfigurationBodyRefs(
	label string,
	body *parse.ObjectLit,
	refs configurationRefIndex,
	errs *parse.ErrorList,
) {
	if body == nil {
		return
	}
	lang.Walk(body, func(expr parse.Expr) {
		dp, ok := expr.(*parse.DotPath)
		if !ok || dp.Root == nil || dp.Root.Name != "configuration" {
			return
		}
		validateConfigurationPath(dp, refs, "configuration "+label, errs)
	})
}

func validateConfigurationNodeRefs(
	nodes []NodeDecl,
	refs configurationRefIndex,
	errs *parse.ErrorList,
) {
	for _, node := range nodes {
		validateConfigurationNodeBodyRefs(node, refs, errs)
	}
}

func validateConfigurationNodeBodyRefs(
	node NodeDecl,
	refs configurationRefIndex,
	errs *parse.ErrorList,
) {
	if node.Body == nil {
		return
	}
	for _, fld := range node.Body.Fields {
		if fld.Key.Kind != parse.FieldIdent {
			continue
		}
		switch fld.Key.Name {
		case "@configuration":
			ref, ok := validateConfigurationSelection(
				fld.Value, refs, "@configuration", errs)
			if ok && ref.alias != node.Selector.Alias.Name {
				errs.Addf(parse.ErrSchema, fld.Value.Span().Start,
					"@configuration %s: selector %q does not match node selector %q",
					ref.name, ref.alias, node.Selector.Alias.Name)
			}
		case "@configurations":
			validateConfigurationsSelection(fld.Value, refs, errs)
		}
	}
}

func validateConfigurationsSelection(
	expr parse.Expr,
	refs configurationRefIndex,
	errs *parse.ErrorList,
) {
	obj, ok := expr.(*parse.ObjectLit)
	if !ok {
		return
	}
	for _, entry := range obj.Fields {
		if entry.Key.Kind != parse.FieldIdent {
			continue
		}
		ref, ok := validateConfigurationSelection(
			entry.Value, refs, "@configurations."+entry.Key.Name, errs)
		if ok && ref.alias != entry.Key.Name {
			errs.Addf(parse.ErrSchema, entry.Value.Span().Start,
				"@configurations.%s: selector %q does not match remap key",
				entry.Key.Name, ref.alias)
		}
	}
}

func validateConfigurationSelection(
	expr parse.Expr,
	refs configurationRefIndex,
	what string,
	errs *parse.ErrorList,
) (configurationRef, bool) {
	dp, ok := expr.(*parse.DotPath)
	if !ok || dp.Root == nil || dp.Root.Name != "configuration" {
		errs.Addf(parse.ErrSchema, expr.Span().Start,
			"%s takes configuration.<name>", what)
		return configurationRef{}, false
	}
	if rejectConfigurationAliasPath(dp, refs, what, errs) {
		return configurationRef{}, false
	}
	if len(dp.Segments) != 1 || !simpleDotSegment(dp.Segments[0]) {
		errs.Addf(parse.ErrSchema, dp.S.Start,
			"%s takes configuration.<name>", what)
		return configurationRef{}, false
	}
	return namedConfigurationRef(dp, refs, what, errs)
}

func validateConfigurationPath(
	dp *parse.DotPath,
	refs configurationRefIndex,
	what string,
	errs *parse.ErrorList,
) (configurationRef, bool) {
	if len(dp.Segments) < 1 || !simpleDotSegment(dp.Segments[0]) {
		errs.Addf(parse.ErrSchema, dp.S.Start,
			"%s takes configuration.<name>", what)
		return configurationRef{}, false
	}
	if rejectConfigurationAliasPath(dp, refs, what, errs) {
		return configurationRef{}, false
	}
	return namedConfigurationRef(dp, refs, what, errs)
}

func rejectConfigurationAliasPath(
	dp *parse.DotPath,
	refs configurationRefIndex,
	what string,
	errs *parse.ErrorList,
) bool {
	if len(dp.Segments) < 2 || !simpleDotSegment(dp.Segments[1]) {
		return false
	}
	if ref, ok := refs.named[dp.Segments[1].Name]; ok && ref.alias == dp.Segments[0].Name {
		errs.Addf(parse.ErrSchema, dp.S.Start,
			"%s takes configuration.%s; named configurations already declare their selector",
			what, dp.Segments[1].Name)
		return true
	}
	return false
}

func namedConfigurationRef(
	dp *parse.DotPath,
	refs configurationRefIndex,
	what string,
	errs *parse.ErrorList,
) (configurationRef, bool) {
	ref, ok := refs.named[dp.Segments[0].Name]
	if !ok {
		errs.Addf(parse.ErrSchema, dp.S.Start,
			"%s names unknown configuration %q", what, dp.Segments[0].Name)
		return configurationRef{}, false
	}
	return ref, true
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
		Body: factoryBodyObject(body, configurationDeclsObject, nodeDeclsObject),
	}
}

func factoryBodyObject(
	body FactoryBody,
	configurations func([]ConfigurationDecl) *parse.ObjectLit,
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
	if len(body.Configurations) > 0 {
		cfgs := configurations(body.Configurations)
		obj.Fields = append(obj.Fields, identField("configurations", cfgs.S, cfgs))
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

func configurationDeclsObject(decls []ConfigurationDecl) *parse.ObjectLit {
	obj := &parse.ObjectLit{}
	if len(decls) > 0 {
		obj.S = decls[0].S
	}
	for _, decl := range decls {
		key := []string{decl.Selector.Name, "default"}
		if decl.Name != nil {
			key[1] = decl.Name.Name
		}
		obj.Fields = append(obj.Fields,
			pathField(key, decl.Selector.S, configurationDeclValue(decl)))
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

func configurationDeclsSelectorObject(decls []ConfigurationDecl) *parse.ObjectLit {
	obj := &parse.ObjectLit{}
	if len(decls) > 0 {
		obj.S = decls[0].S
	}
	for _, decl := range decls {
		obj.Fields = append(obj.Fields, configurationSelectorField(decl))
	}
	return obj
}

func configurationSelectorField(decl ConfigurationDecl) *parse.Field {
	fld := &parse.Field{
		S: decl.S,
		Decl: &parse.SelectorBody{
			S: decl.S,
			Selector: parse.Selector{
				S:     decl.Selector.S,
				Parts: []parse.Ident{{S: decl.Selector.S, Name: decl.Selector.Name}},
			},
			Body: decl.Body,
		},
	}
	if decl.Name == nil {
		fld.Key = parse.FieldKey{S: decl.Selector.S, Kind: parse.FieldIdent, Name: decl.Selector.Name}
		fld.Decl.Default = true
		return fld
	}
	fld.Key = parse.FieldKey{S: decl.Name.S, Kind: parse.FieldIdent, Name: decl.Name.Name}
	return fld
}

func configurationDeclValue(decl ConfigurationDecl) parse.Expr {
	if decl.Value != nil {
		return decl.Value
	}
	return decl.Body
}

func configurationValuesObject(values []ConfigurationValue) *parse.ObjectLit {
	obj := &parse.ObjectLit{}
	if len(values) > 0 {
		obj.S = values[0].S
	}
	for _, value := range values {
		key := []string{value.Selector.Name, "default"}
		if value.Name != nil {
			key[1] = value.Name.Name
		}
		obj.Fields = append(obj.Fields, pathField(key, value.S, configurationValueExpr(value)))
	}
	return obj
}

func configurationValueExpr(value ConfigurationValue) parse.Expr {
	if value.Value != nil {
		return value.Value
	}
	return value.Body
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
		obj.Fields = append(obj.Fields, stringField(decl.ID.Value, decl.ID.S, decl.Version))
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

type configurationRefIndex struct {
	named map[string]configurationRef
	pairs map[string]bool
}

type configurationRef struct {
	alias string
	name  string
}

func configurationRefs(decls []ConfigurationDecl) configurationRefIndex {
	refs := configurationRefIndex{
		named: map[string]configurationRef{},
		pairs: map[string]bool{},
	}
	for _, decl := range decls {
		name := "default"
		if decl.Name != nil {
			name = decl.Name.Name
			refs.named[name] = configurationRef{alias: decl.Selector.Name, name: name}
		}
		refs.pairs[decl.Selector.Name+"."+name] = true
	}
	return refs
}

func simpleDotSegment(seg parse.DotSegment) bool {
	return seg.Name != "" && seg.Index == nil && !seg.Splat && !seg.Guarded
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
