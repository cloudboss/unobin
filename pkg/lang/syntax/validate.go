package syntax

import (
	"time"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/parse"
)

var (
	resourceBodyMeta = map[string]bool{
		"@configuration":  true,
		"@configurations": true,
		"@depends-on":     true,
		"@for-each":       true,
		"@lock":           true,
		"@timeout":        true,
	}
	dataBodyMeta = map[string]bool{
		"@configuration":  true,
		"@configurations": true,
		"@depends-on":     true,
		"@for-each":       true,
		"@lock":           true,
		"@timeout":        true,
	}
	actionBodyMeta = map[string]bool{
		"@configuration":  true,
		"@configurations": true,
		"@depends-on":     true,
		"@for-each":       true,
		"@lock":           true,
		"@timeout":        true,
		"@trigger":        true,
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
	validateConfigurationDecls(body.Configurations, errs)
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
	localNames := configLocalNames(stack.Locals)
	mergeErrors(errs, lang.ValidateLocals(locals))
	mergeErrors(errs, lang.ValidateConfigLocals(locals))
	if stack.Factory != nil {
		validateStackFactory(stack.Factory, localNames, errs)
	}
	if stack.State != nil {
		mergeErrors(errs, lang.ValidateStateConfig(
			resolverConfigObject(stack.State.S, "@backend", stack.State.Selector, stack.State.Body),
			localNames,
		))
	}
	if stack.Encryption == nil {
		errs.Addf(parse.ErrSchema, stack.S.Start, "stack file requires encryption")
		return
	}
	mergeErrors(errs, lang.ValidateEncryptionConfig(
		resolverConfigObject(
			stack.Encryption.S,
			"@key-source",
			stack.Encryption.Selector,
			stack.Encryption.Body,
		),
		localNames,
	))
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
	mergeErrors(errs, lang.ValidateConfigFactory(cfg, locals))
	validateConfigurationValues(factory.Configurations, errs)
	if len(factory.Configurations) == 0 {
		return
	}
	configurations := configurationValuesObject(factory.Configurations)
	wrapped := &parse.ObjectLit{S: factory.S}
	wrapped.Fields = append(wrapped.Fields,
		identField("configurations", configurations.S, configurations))
	mergeErrors(errs, lang.ValidateConfigFactory(wrapped, locals))
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
		Body: FactoryBodyObject(body),
	}
}

// FactoryBodyObject returns the generic AST object for a lowered factory body.
func FactoryBodyObject(body FactoryBody) *parse.ObjectLit {
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
		configurations := configurationDeclsObject(body.Configurations)
		obj.Fields = append(obj.Fields,
			identField("configurations", configurations.S, configurations))
	}
	if len(body.Resources) > 0 {
		resources := nodeDeclsObject(body.Resources)
		obj.Fields = append(obj.Fields, identField("resources", resources.S, resources))
	}
	if len(body.Data) > 0 {
		data := nodeDeclsObject(body.Data)
		obj.Fields = append(obj.Fields, identField("data", data.S, data))
	}
	if len(body.Actions) > 0 {
		actions := nodeDeclsObject(body.Actions)
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
		value := configurationDeclValue(decl)
		obj.Fields = append(obj.Fields,
			identField(configurationDeclLabel(decl), decl.Selector.S, value))
	}
	return obj
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

func resolverConfigObject(
	span parse.Span,
	metaKey string,
	selector Ident,
	body *parse.ObjectLit,
) *parse.ObjectLit {
	out := &parse.ObjectLit{S: span}
	out.Fields = append(out.Fields, identField(metaKey, selector.S, &parse.Ident{
		S:    selector.S,
		Name: selector.Name,
	}))
	if body != nil {
		out.Fields = append(out.Fields, body.Fields...)
	}
	return out
}

func configLocalNames(decls []LocalDecl) map[string]bool {
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
