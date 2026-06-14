package syntax

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/parse"
)

func LowerFile(f *parse.File) (*File, *parse.ErrorList) {
	errs := parse.NewErrorList(0)
	if f == nil {
		errs.Addf(parse.ErrSchema, parse.Position{},
			"internal error: parser returned no UB file")
		return &File{}, errs
	}

	out := &File{
		S:        f.S,
		Path:     f.Path,
		Comments: f.Comments,
	}
	if lowerSourceDeclaredFile(f, out, errs) {
		return out, errs
	}

	switch f.Kind {
	case parse.FileFactory:
		out.Kind = FileFactory
		out.Factory = &FactoryFile{S: f.S, Body: lowerFactoryBody(f.Body, errs)}
	case parse.FileConfig:
		out.Kind = FileStack
		out.Stack = lowerStackFile(f.S, f.Body, errs)
	case parse.FileManifest:
		out.Kind = FileManifest
		out.Manifest = lowerManifestFile(f.S, f.Body, errs)
	case parse.FileExportedType:
		out.Kind = FileLibrary
		out.Library = lowerLibraryFile(f, errs)
	default:
		out.Kind = FileUnknown
		errs.Addf(parse.ErrSchema, f.S.Start,
			"cannot determine UB file role from %s; expected factory, stack, manifest, "+
				"or exported library file",
			f.Kind)
	}

	return out, errs
}

type sourceFileRole struct {
	name string
	fld  *parse.Field
}

func lowerSourceDeclaredFile(
	f *parse.File,
	out *File,
	errs *parse.ErrorList,
) bool {
	if f.Body == nil {
		return false
	}
	var roles []sourceFileRole
	var selectorBody bool
	for _, fld := range f.Body.Fields {
		if fld.Decl != nil {
			selectorBody = true
			continue
		}
		if fld.Key.Kind != parse.FieldIdent {
			continue
		}
		switch fld.Key.Name {
		case "factory", "stack", "manifest":
			roles = append(roles, sourceFileRole{name: fld.Key.Name, fld: fld})
		}
	}
	if len(roles) == 1 && len(f.Body.Fields) == 1 {
		lowerSourceDeclaredRole(f, out, roles, errs)
		return true
	}
	if len(roles) > 0 && f.Kind == parse.FileUnknown {
		lowerSourceDeclaredRole(f, out, roles, errs)
		return true
	}
	if selectorBody && f.Kind == parse.FileUnknown {
		out.Kind = FileLibrary
		out.Library = lowerLibraryFile(f, errs)
		return true
	}
	return false
}

func lowerSourceDeclaredRole(
	f *parse.File,
	out *File,
	roles []sourceFileRole,
	errs *parse.ErrorList,
) {
	first := roles[0]
	for _, role := range roles[1:] {
		errs.Addf(parse.ErrSchema, role.fld.Key.S.Start,
			"file must not declare both %s and %s", first.name, role.name)
	}
	if len(roles) > 1 {
		return
	}
	if len(f.Body.Fields) != 1 {
		errs.Addf(parse.ErrSchema, first.fld.Key.S.Start,
			"%s must be the only top-level file declaration", first.name)
		return
	}
	block := objectValue(first.fld, first.name, errs)
	if block == nil {
		return
	}
	switch first.name {
	case "factory":
		out.Kind = FileFactory
		out.Factory = &FactoryFile{S: first.fld.S, Body: lowerFactoryBody(block, errs)}
	case "stack":
		out.Kind = FileStack
		out.Stack = lowerStackFile(first.fld.S, block, errs)
	case "manifest":
		out.Kind = FileManifest
		out.Manifest = lowerManifestFile(first.fld.S, block, errs)
	}
}

func lowerFactoryBody(block *parse.ObjectLit, errs *parse.ErrorList) FactoryBody {
	var body FactoryBody
	if block == nil {
		return body
	}
	body.S = block.S
	for _, fld := range block.Fields {
		name, ok := fieldName(fld, "factory field", errs)
		if !ok {
			continue
		}
		switch name.Name {
		case "description":
			body.Description = stringValue(fld, "description", errs)
		case "inputs":
			if obj := objectValue(fld, "inputs", errs); obj != nil {
				body.Inputs = lowerInputs(obj, errs)
			}
		case "locals":
			if obj := objectValue(fld, "locals", errs); obj != nil {
				body.Locals = lowerLocals(obj, "local", errs)
			}
		case "constraints":
			if arr := arrayValue(fld, "constraints", errs); arr != nil {
				body.Constraints = lowerConstraints(arr)
			}
		case "imports":
			if obj := objectValue(fld, "imports", errs); obj != nil {
				body.Imports = lowerImports(obj, errs)
			}
		case "configurations":
			if obj := objectValue(fld, "configurations", errs); obj != nil {
				body.Configurations = lowerConfigurationDecls(obj, errs)
			}
		case "resources":
			if obj := objectValue(fld, "resources", errs); obj != nil {
				body.Resources = lowerNodes(obj, NodeResource, errs)
			}
		case "data":
			if obj := objectValue(fld, "data", errs); obj != nil {
				body.Data = lowerNodes(obj, NodeData, errs)
			}
		case "actions":
			if obj := objectValue(fld, "actions", errs); obj != nil {
				body.Actions = lowerNodes(obj, NodeAction, errs)
			}
		case "outputs":
			if obj := objectValue(fld, "outputs", errs); obj != nil {
				body.Outputs = lowerOutputs(obj, errs)
			}
		default:
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"%q is not a valid factory field", name.Name)
		}
	}
	return body
}

func lowerStackFile(
	span parse.Span,
	block *parse.ObjectLit,
	errs *parse.ErrorList,
) *StackFile {
	stack := &StackFile{S: span}
	if block == nil {
		return stack
	}
	for _, fld := range block.Fields {
		name, ok := fieldName(fld, "stack field", errs)
		if !ok {
			continue
		}
		switch name.Name {
		case "factory":
			if obj := objectValue(fld, "factory", errs); obj != nil {
				stack.Factory = lowerStackFactory(fld.S, obj, errs)
			}
		case "locals":
			if obj := objectValue(fld, "locals", errs); obj != nil {
				stack.Locals = lowerLocals(obj, "local", errs)
			}
		case "state":
			if fld.Decl != nil {
				stack.State = lowerStateSelectorDecl(fld, errs)
			} else if obj := objectValue(fld, "state", errs); obj != nil {
				stack.State = lowerStateDecl(fld.S, obj, errs)
			}
		case "encryption":
			if fld.Decl != nil {
				stack.Encryption = lowerEncryptionSelectorDecl(fld, errs)
			} else if obj := objectValue(fld, "encryption", errs); obj != nil {
				stack.Encryption = lowerEncryptionDecl(fld.S, obj, errs)
			}
		case "parallelism":
			stack.Parallelism = fld.Value
		default:
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"%q is not a valid stack field", name.Name)
		}
	}
	return stack
}

func lowerStackFactory(
	span parse.Span,
	block *parse.ObjectLit,
	errs *parse.ErrorList,
) *StackFactoryBlock {
	factory := &StackFactoryBlock{S: span}
	for _, fld := range block.Fields {
		name, ok := fieldName(fld, "stack factory field", errs)
		if !ok {
			continue
		}
		switch name.Name {
		case "pin":
			factory.Pin = objectValue(fld, "factory.pin", errs)
		case "inputs":
			factory.Inputs = objectValue(fld, "factory.inputs", errs)
		case "configurations":
			if obj := objectValue(fld, "factory.configurations", errs); obj != nil {
				factory.Configurations = lowerConfigurationValues(obj, errs)
			}
		default:
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"%q is not a valid stack factory field", name.Name)
		}
	}
	return factory
}

func lowerManifestFile(
	span parse.Span,
	block *parse.ObjectLit,
	errs *parse.ErrorList,
) *ManifestFile {
	manifest := &ManifestFile{S: span}
	if block == nil {
		return manifest
	}
	for _, fld := range block.Fields {
		name, ok := fieldName(fld, "manifest field", errs)
		if !ok {
			continue
		}
		switch name.Name {
		case "unobin-version":
			manifest.UnobinVersion = stringValue(fld, "unobin-version", errs)
		case "requires":
			if obj := objectValue(fld, "requires", errs); obj != nil {
				manifest.Requires = lowerManifestRequires(obj, errs)
			}
		case "replace":
			if obj := objectValue(fld, "replace", errs); obj != nil {
				manifest.Replace = lowerManifestReplace(obj, errs)
			}
		default:
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"%q is not a valid manifest field", name.Name)
		}
	}
	return manifest
}

func lowerLibraryFile(f *parse.File, errs *parse.ErrorList) *LibraryFile {
	library := &LibraryFile{S: f.S}
	if hasSelectorBody(f.Body) {
		library.Exports = lowerCompositeDecls(f.Body, errs)
		return library
	}
	name, kind, ok := compositeNameFromPath(f.Path, f.S.Start, errs)
	if !ok {
		return library
	}
	library.Exports = append(library.Exports, CompositeDecl{
		S:    f.S,
		Name: name,
		Kind: kind,
		Body: lowerFactoryBody(f.Body, errs),
	})
	return library
}

func hasSelectorBody(block *parse.ObjectLit) bool {
	if block == nil {
		return false
	}
	for _, fld := range block.Fields {
		if fld.Decl != nil {
			return true
		}
	}
	return false
}

func lowerCompositeDecls(block *parse.ObjectLit, errs *parse.ErrorList) []CompositeDecl {
	exports := make([]CompositeDecl, 0, len(block.Fields))
	seen := make(map[string]parse.Position, len(block.Fields))
	for _, fld := range block.Fields {
		if fld.Decl == nil {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"library export must be written as name: resource { ... }")
			continue
		}
		if fld.Decl.Default {
			errs.Addf(parse.ErrSchema, fld.S.Start,
				"library export must include a name before the selector")
			continue
		}
		name, ok := fieldName(fld, "library export name", errs)
		if !ok {
			continue
		}
		kind, ok := compositeKind(fld.Decl.Selector, errs)
		if !ok {
			continue
		}
		key := string(kind) + "." + name.Name
		if prev, dup := seen[key]; dup {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"duplicate library export %s (first defined at %s)", key, prev)
			continue
		}
		seen[key] = fld.Key.S.Start
		exports = append(exports, CompositeDecl{
			S:    fld.S,
			Name: name,
			Kind: kind,
			Body: lowerFactoryBody(fld.Decl.Body, errs),
		})
	}
	return exports
}

func compositeKind(sel parse.Selector, errs *parse.ErrorList) (NodeKind, bool) {
	id, ok := selectorIdent(sel, "library export selector", errs)
	if !ok {
		return "", false
	}
	switch id.Name {
	case string(NodeResource):
		return NodeResource, true
	case string(NodeData):
		return NodeData, true
	case string(NodeAction):
		return NodeAction, true
	default:
		errs.Addf(parse.ErrSchema, id.S.Start,
			"library export selector must be resource, data, or action")
		return "", false
	}
}

func compositeNameFromPath(
	path string,
	pos parse.Position,
	errs *parse.ErrorList,
) (Ident, NodeKind, bool) {
	base := filepath.Base(path)
	if !strings.HasSuffix(base, ".ub") {
		errs.Addf(parse.ErrSchema, pos,
			"library export filename must end in .ub")
		return Ident{}, "", false
	}
	stem := strings.TrimSuffix(base, ".ub")
	for prefix, kind := range map[string]NodeKind{
		"resource-": NodeResource,
		"data-":     NodeData,
		"action-":   NodeAction,
	} {
		if name, ok := strings.CutPrefix(stem, prefix); ok && name != "" {
			return Ident{S: parse.Span{Start: pos, End: pos}, Name: name}, kind, true
		}
	}
	errs.Addf(parse.ErrSchema, pos,
		"library export filename must be resource-<name>.ub, data-<name>.ub, "+
			"or action-<name>.ub")
	return Ident{}, "", false
}

func lowerInputs(block *parse.ObjectLit, errs *parse.ErrorList) []InputDecl {
	inputs := make([]InputDecl, 0, len(block.Fields))
	seen := make(map[string]parse.Position, len(block.Fields))
	for _, fld := range block.Fields {
		name, ok := fieldName(fld, "input name", errs)
		if !ok {
			continue
		}
		if prev, dup := seen[name.Name]; dup {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"duplicate input %q (first defined at %s)", name.Name, prev)
			continue
		}
		seen[name.Name] = fld.Key.S.Start
		body := objectValue(fld, "input "+name.Name, errs)
		if body == nil {
			continue
		}
		inputs = append(inputs, InputDecl{
			S:    fld.S,
			Name: name,
			Body: body,
			Type: lowerInputType(name.Name, body, errs),
		})
	}
	return inputs
}

func lowerInputType(name string, body *parse.ObjectLit, errs *parse.ErrorList) parse.TypeExpr {
	var out parse.TypeExpr
	var found bool
	for _, fld := range body.Fields {
		if fld.Key.Kind != parse.FieldIdent || fld.Key.Name != "type" {
			continue
		}
		if found {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"input %q: duplicate key %q", name, "type")
			continue
		}
		found = true
		t, err := lang.PromoteType(fld.Value)
		if err != nil {
			var perr *parse.Error
			if errors.As(err, &perr) {
				errs.Add(perr)
			} else {
				errs.Addf(parse.ErrType, fld.Value.Span().Start,
					"input %q: %v", name, err)
			}
			continue
		}
		out = t
	}
	if !found {
		errs.Addf(parse.ErrSchema, body.S.Start,
			"input %q: missing required `type:` key", name)
	}
	return out
}

func lowerLocals(block *parse.ObjectLit, what string, errs *parse.ErrorList) []LocalDecl {
	locals := make([]LocalDecl, 0, len(block.Fields))
	seen := make(map[string]parse.Position, len(block.Fields))
	for _, fld := range block.Fields {
		name, ok := fieldName(fld, what+" name", errs)
		if !ok {
			continue
		}
		if prev, dup := seen[name.Name]; dup {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"duplicate %s %q (first defined at %s)", what, name.Name, prev)
			continue
		}
		seen[name.Name] = fld.Key.S.Start
		locals = append(locals, LocalDecl{S: fld.S, Name: name, Value: fld.Value})
	}
	return locals
}

func lowerConstraints(arr *parse.ArrayLit) []ConstraintDecl {
	constraints := make([]ConstraintDecl, 0, len(arr.Elements))
	for _, elem := range arr.Elements {
		constraints = append(constraints, ConstraintDecl{
			S:     elem.Span(),
			Value: elem,
		})
	}
	return constraints
}

func lowerImports(block *parse.ObjectLit, errs *parse.ErrorList) []ImportDecl {
	imports := make([]ImportDecl, 0, len(block.Fields))
	seen := make(map[string]parse.Position, len(block.Fields))
	for _, fld := range block.Fields {
		alias, ok := fieldName(fld, "import alias", errs)
		if !ok {
			continue
		}
		if prev, dup := seen[alias.Name]; dup {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"duplicate import %q (first defined at %s)", alias.Name, prev)
			continue
		}
		seen[alias.Name] = fld.Key.S.Start
		ref := stringValue(fld, "import "+alias.Name, errs)
		if ref == nil {
			continue
		}
		imports = append(imports, ImportDecl{S: fld.S, Alias: alias, Ref: ref})
	}
	return imports
}

func lowerConfigurationDecls(
	block *parse.ObjectLit,
	errs *parse.ErrorList,
) []ConfigurationDecl {
	entries := make([]ConfigurationDecl, 0, len(block.Fields))
	seen := make(map[string]parse.Position, len(block.Fields))
	for _, fld := range block.Fields {
		if fld.Decl != nil {
			entry, ok := lowerConfigurationDecl(fld, errs)
			if !ok {
				continue
			}
			key := configurationDeclKey(entry)
			if prev, dup := seen[key]; dup {
				errs.Addf(parse.ErrSchema, fld.Key.S.Start,
					"duplicate configuration %s (first defined at %s)", key, prev)
				continue
			}
			seen[key] = fld.Key.S.Start
			entries = append(entries, entry)
			continue
		}
		selector, name, ok := configurationKey(fld, errs)
		if !ok {
			continue
		}
		key := selector.Name + "." + name.Name
		if prev, dup := seen[key]; dup {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"duplicate configuration %s (first defined at %s)", key, prev)
			continue
		}
		seen[key] = fld.Key.S.Start
		body, _ := fld.Value.(*parse.ObjectLit)
		entries = append(entries, ConfigurationDecl{
			S:        fld.S,
			Name:     &name,
			Selector: selector,
			Body:     body,
			Value:    fld.Value,
		})
	}
	return entries
}

func lowerConfigurationDecl(
	fld *parse.Field,
	errs *parse.ErrorList,
) (ConfigurationDecl, bool) {
	selector, ok := selectorIdent(fld.Decl.Selector, "configuration selector", errs)
	if !ok {
		return ConfigurationDecl{}, false
	}
	entry := ConfigurationDecl{
		S:        fld.S,
		Selector: selector,
		Body:     fld.Decl.Body,
	}
	if !fld.Decl.Default {
		name, ok := fieldName(fld, "configuration name", errs)
		if !ok {
			return ConfigurationDecl{}, false
		}
		entry.Name = &name
	}
	return entry, true
}

func configurationDeclKey(entry ConfigurationDecl) string {
	if entry.Name == nil {
		return entry.Selector.Name
	}
	return entry.Selector.Name + "." + entry.Name.Name
}

func lowerConfigurationValues(
	block *parse.ObjectLit,
	errs *parse.ErrorList,
) []ConfigurationValue {
	entries := make([]ConfigurationValue, 0, len(block.Fields))
	seen := make(map[string]parse.Position, len(block.Fields))
	for _, fld := range block.Fields {
		if fld.Decl != nil {
			entry, ok := lowerConfigurationValue(fld, errs)
			if !ok {
				continue
			}
			key := configurationValueKey(entry)
			if prev, dup := seen[key]; dup {
				errs.Addf(parse.ErrSchema, fld.Key.S.Start,
					"duplicate configuration %s (first defined at %s)", key, prev)
				continue
			}
			seen[key] = fld.Key.S.Start
			entries = append(entries, entry)
			continue
		}
		selector, name, ok := configurationKey(fld, errs)
		if !ok {
			continue
		}
		key := selector.Name + "." + name.Name
		if prev, dup := seen[key]; dup {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"duplicate configuration %s (first defined at %s)", key, prev)
			continue
		}
		seen[key] = fld.Key.S.Start
		body, _ := fld.Value.(*parse.ObjectLit)
		entries = append(entries, ConfigurationValue{
			S:        fld.S,
			Name:     &name,
			Selector: selector,
			Body:     body,
			Value:    fld.Value,
		})
	}
	return entries
}

func lowerConfigurationValue(
	fld *parse.Field,
	errs *parse.ErrorList,
) (ConfigurationValue, bool) {
	selector, ok := selectorIdent(fld.Decl.Selector, "configuration selector", errs)
	if !ok {
		return ConfigurationValue{}, false
	}
	entry := ConfigurationValue{
		S:        fld.S,
		Selector: selector,
		Body:     fld.Decl.Body,
	}
	if !fld.Decl.Default {
		name, ok := fieldName(fld, "configuration name", errs)
		if !ok {
			return ConfigurationValue{}, false
		}
		entry.Name = &name
	}
	return entry, true
}

func configurationValueKey(entry ConfigurationValue) string {
	if entry.Name == nil {
		return entry.Selector.Name
	}
	return entry.Selector.Name + "." + entry.Name.Name
}

func configurationKey(
	fld *parse.Field,
	errs *parse.ErrorList,
) (Ident, Ident, bool) {
	if fld.Key.Kind != parse.FieldPath {
		errs.Addf(parse.ErrSchema, fld.Key.S.Start,
			"configuration must be declared with a dotted alias.name key")
		return Ident{}, Ident{}, false
	}
	if len(fld.Key.Path) != 2 {
		errs.Addf(parse.ErrSchema, fld.Key.S.Start,
			"configuration key %s must have two segments: alias.name",
			strings.Join(fld.Key.Path, "."))
		return Ident{}, Ident{}, false
	}
	return keyPart(fld.Key, fld.Key.Path[0]), keyPart(fld.Key, fld.Key.Path[1]), true
}

func lowerNodes(
	block *parse.ObjectLit,
	kind NodeKind,
	errs *parse.ErrorList,
) []NodeDecl {
	nodes := make([]NodeDecl, 0, len(block.Fields))
	seen := make(map[string]parse.Position, len(block.Fields))
	for _, fld := range block.Fields {
		if fld.Decl != nil {
			node, ok := lowerSelectorNode(fld, kind, errs)
			if !ok {
				continue
			}
			if prev, dup := seen[node.Name.Name]; dup {
				errs.Addf(parse.ErrSchema, fld.Key.S.Start,
					"duplicate %s %s (first defined at %s)", kind, node.Name.Name, prev)
				continue
			}
			seen[node.Name.Name] = fld.Key.S.Start
			nodes = append(nodes, node)
			continue
		}
		if fld.Key.Kind != parse.FieldPath {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"%s must be declared with a dotted alias.export.name key", kind)
			continue
		}
		if len(fld.Key.Path) != 3 {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"%s key %s must have three segments: alias.export.name",
				kind, strings.Join(fld.Key.Path, "."))
			continue
		}
		name := keyPart(fld.Key, fld.Key.Path[2])
		if prev, dup := seen[name.Name]; dup {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"duplicate %s %s (first defined at %s)", kind, name.Name, prev)
			continue
		}
		seen[name.Name] = fld.Key.S.Start
		body := objectValue(fld, string(kind)+" "+name.Name, errs)
		if body == nil {
			continue
		}
		nodes = append(nodes, NodeDecl{
			S:    fld.S,
			Kind: kind,
			Name: name,
			Selector: NodeSelector{
				S:      fld.Key.S,
				Alias:  keyPart(fld.Key, fld.Key.Path[0]),
				Export: keyPart(fld.Key, fld.Key.Path[1]),
			},
			Body: body,
		})
	}
	return nodes
}

func lowerSelectorNode(
	fld *parse.Field,
	kind NodeKind,
	errs *parse.ErrorList,
) (NodeDecl, bool) {
	if fld.Decl.Default {
		errs.Addf(parse.ErrSchema, fld.S.Start,
			"%s declaration must include a name before the selector", kind)
		return NodeDecl{}, false
	}
	name, ok := fieldName(fld, string(kind)+" name", errs)
	if !ok {
		return NodeDecl{}, false
	}
	selector, ok := nodeSelector(fld.Decl.Selector, string(kind)+" selector", errs)
	if !ok {
		return NodeDecl{}, false
	}
	return NodeDecl{
		S:        fld.S,
		Kind:     kind,
		Name:     name,
		Selector: selector,
		Body:     fld.Decl.Body,
	}, true
}

func lowerOutputs(block *parse.ObjectLit, errs *parse.ErrorList) []OutputDecl {
	outputs := make([]OutputDecl, 0, len(block.Fields))
	seen := make(map[string]parse.Position, len(block.Fields))
	for _, fld := range block.Fields {
		name, ok := fieldName(fld, "output name", errs)
		if !ok {
			continue
		}
		if prev, dup := seen[name.Name]; dup {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"duplicate output %q (first defined at %s)", name.Name, prev)
			continue
		}
		seen[name.Name] = fld.Key.S.Start
		body := objectValue(fld, "output "+name.Name, errs)
		if body == nil {
			continue
		}
		outputs = append(outputs, OutputDecl{S: fld.S, Name: name, Body: body})
	}
	return outputs
}

func lowerStateDecl(span parse.Span, block *parse.ObjectLit, errs *parse.ErrorList) *StateDecl {
	selector, body := lowerSelectorObject(block, "@backend", "state", errs)
	return &StateDecl{S: span, Selector: selector, Body: body}
}

func lowerStateSelectorDecl(fld *parse.Field, errs *parse.ErrorList) *StateDecl {
	selector, ok := selectorIdent(fld.Decl.Selector, "state selector", errs)
	if !ok {
		return &StateDecl{S: fld.S, Body: fld.Decl.Body}
	}
	return &StateDecl{S: fld.S, Selector: selector, Body: fld.Decl.Body}
}

func lowerEncryptionDecl(
	span parse.Span,
	block *parse.ObjectLit,
	errs *parse.ErrorList,
) *EncryptionDecl {
	selector, body := lowerSelectorObject(block, "@key-source", "encryption", errs)
	return &EncryptionDecl{S: span, Selector: selector, Body: body}
}

func lowerEncryptionSelectorDecl(fld *parse.Field, errs *parse.ErrorList) *EncryptionDecl {
	selector, ok := selectorIdent(fld.Decl.Selector, "encryption selector", errs)
	if !ok {
		return &EncryptionDecl{S: fld.S, Body: fld.Decl.Body}
	}
	return &EncryptionDecl{S: fld.S, Selector: selector, Body: fld.Decl.Body}
}

func lowerSelectorObject(
	block *parse.ObjectLit,
	metaKey string,
	what string,
	errs *parse.ErrorList,
) (Ident, *parse.ObjectLit) {
	var selector Ident
	var found bool
	body := &parse.ObjectLit{S: block.S}
	for _, fld := range block.Fields {
		if fld.Key.Kind == parse.FieldIdent && fld.Key.Name == metaKey {
			if found {
				errs.Addf(parse.ErrSchema, fld.Key.S.Start,
					"%s has duplicate %s selector", what, metaKey)
				continue
			}
			found = true
			id, ok := fld.Value.(*parse.Ident)
			if !ok {
				errs.Addf(parse.ErrSchema, fld.Value.Span().Start,
					"%s %s must be a bare identifier", what, metaKey)
				continue
			}
			selector = Ident{S: id.S, Name: id.Name}
			continue
		}
		body.Fields = append(body.Fields, fld)
	}
	if !found {
		errs.Addf(parse.ErrSchema, block.S.Start,
			"%s is missing required %s selector", what, metaKey)
	}
	return selector, body
}

func lowerManifestRequires(
	block *parse.ObjectLit,
	errs *parse.ErrorList,
) []ManifestRequire {
	requires := make([]ManifestRequire, 0, len(block.Fields))
	for _, fld := range block.Fields {
		id, ok := stringKey(fld, "dependency id", errs)
		if !ok {
			continue
		}
		version := stringValue(fld, "require "+id.Value, errs)
		if version == nil {
			continue
		}
		requires = append(requires, ManifestRequire{S: fld.S, ID: id, Version: version})
	}
	return requires
}

func lowerManifestReplace(
	block *parse.ObjectLit,
	errs *parse.ErrorList,
) []ManifestReplace {
	replacements := make([]ManifestReplace, 0, len(block.Fields))
	for _, fld := range block.Fields {
		id, ok := stringKey(fld, "dependency id", errs)
		if !ok {
			continue
		}
		path := stringValue(fld, "replace "+id.Value, errs)
		if path == nil {
			continue
		}
		replacements = append(replacements, ManifestReplace{S: fld.S, ID: id, Path: path})
	}
	return replacements
}

func fieldName(fld *parse.Field, what string, errs *parse.ErrorList) (Ident, bool) {
	if fld.Key.Kind != parse.FieldIdent {
		errs.Addf(parse.ErrSchema, fld.Key.S.Start,
			"%s must be an identifier", what)
		return Ident{}, false
	}
	if fld.Key.IsMeta() {
		errs.Addf(parse.ErrSchema, fld.Key.S.Start,
			"%s must not be @-prefixed", what)
		return Ident{}, false
	}
	return Ident{S: fld.Key.S, Name: fld.Key.Name}, true
}

func stringKey(fld *parse.Field, what string, errs *parse.ErrorList) (StringKey, bool) {
	if fld.Key.Kind != parse.FieldString {
		errs.Addf(parse.ErrSchema, fld.Key.S.Start,
			"%s must be a quoted string", what)
		return StringKey{}, false
	}
	return StringKey{S: fld.Key.S, Value: fld.Key.String}, true
}

func stringValue(fld *parse.Field, what string, errs *parse.ErrorList) *parse.StringLit {
	if fld.Value == nil {
		errs.Addf(parse.ErrSchema, fld.S.Start,
			"%s must be a string literal", what)
		return nil
	}
	value, ok := fld.Value.(*parse.StringLit)
	if !ok {
		errs.Addf(parse.ErrSchema, fld.Value.Span().Start,
			"%s must be a string literal", what)
		return nil
	}
	return value
}

func objectValue(fld *parse.Field, what string, errs *parse.ErrorList) *parse.ObjectLit {
	if fld.Value == nil {
		errs.Addf(parse.ErrSchema, fld.S.Start,
			"%s must be an object", what)
		return nil
	}
	value, ok := fld.Value.(*parse.ObjectLit)
	if !ok {
		errs.Addf(parse.ErrSchema, fld.Value.Span().Start,
			"%s must be an object", what)
		return nil
	}
	return value
}

func arrayValue(fld *parse.Field, what string, errs *parse.ErrorList) *parse.ArrayLit {
	if fld.Value == nil {
		errs.Addf(parse.ErrSchema, fld.S.Start,
			"%s must be an array", what)
		return nil
	}
	value, ok := fld.Value.(*parse.ArrayLit)
	if !ok {
		errs.Addf(parse.ErrSchema, fld.Value.Span().Start,
			"%s must be an array", what)
		return nil
	}
	return value
}

func keyPart(key parse.FieldKey, name string) Ident {
	return Ident{S: key.S, Name: name}
}

func selectorIdent(sel parse.Selector, what string, errs *parse.ErrorList) (Ident, bool) {
	if len(sel.Parts) != 1 {
		errs.Addf(parse.ErrSchema, sel.S.Start,
			"%s must have one segment", what)
		return Ident{}, false
	}
	part := sel.Parts[0]
	return Ident{S: part.S, Name: part.Name}, true
}

func nodeSelector(
	sel parse.Selector,
	what string,
	errs *parse.ErrorList,
) (NodeSelector, bool) {
	if len(sel.Parts) != 2 {
		errs.Addf(parse.ErrSchema, sel.S.Start,
			"%s must have two segments: alias.export", what)
		return NodeSelector{}, false
	}
	return NodeSelector{
		S:      sel.S,
		Alias:  Ident{S: sel.Parts[0].S, Name: sel.Parts[0].Name},
		Export: Ident{S: sel.Parts[1].S, Name: sel.Parts[1].Name},
	}, true
}
