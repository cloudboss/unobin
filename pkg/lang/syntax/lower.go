package syntax

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang/parse"
)

func LowerFile(f *parse.File) (*File, *parse.ErrorList) {
	return lowerFile(f, lowerMode{})
}

func lowerFile(f *parse.File, mode lowerMode) (*File, *parse.ErrorList) {
	errs := parse.NewErrorList(0)
	if f == nil {
		errs.Addf(parse.ErrSchema, parse.Position{},
			"internal error: parser returned no UB file")
		return &File{}, errs
	}

	if mode.path == "" {
		mode.path = f.Path
	}
	out := &File{
		S:        f.S,
		Path:     f.Path,
		Comments: f.Comments,
	}
	validateUniqueObjectFields(f.Body, errs)
	if errs.Len() > 0 {
		return out, errs
	}
	if lowerSourceDeclaredFile(f, out, errs, mode) {
		return out, errs
	}

	out.Kind = FileUnknown
	errs.Addf(parse.ErrSchema, f.S.Start,
		"cannot determine UB file role from %s; expected source-declared factory, "+
			"stack, manifest, lock, or exported library file",
		f.Kind)

	return out, errs
}

type lowerMode struct {
	path   string
	source []byte
}

type sourceFileRole struct {
	name string
	fld  *parse.Field
}

func lowerSourceDeclaredFile(
	f *parse.File,
	out *File,
	errs *parse.ErrorList,
	mode lowerMode,
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
		case "factory", "stack", "manifest", "lock":
			roles = append(roles, sourceFileRole{name: fld.Key.Name, fld: fld})
		}
	}
	if _, reserved := reservedSourceFileRole(f.Path); reserved {
		if len(roles) > 1 {
			lowerSourceDeclaredRole(f, out, roles, errs, mode)
			return true
		}
		if !validateSourceDeclaredPath(f, roles, errs) {
			return true
		}
		lowerSourceDeclaredRole(f, out, roles, errs, mode)
		return true
	}
	if len(roles) > 1 {
		lowerSourceDeclaredRole(f, out, roles, errs, mode)
		return true
	}
	if !validateSourceDeclaredPath(f, roles, errs) {
		return true
	}
	if len(roles) == 1 && len(f.Body.Fields) == 1 {
		lowerSourceDeclaredRole(f, out, roles, errs, mode)
		return true
	}
	if len(roles) > 0 && f.Kind == parse.FileUnknown {
		lowerSourceDeclaredRole(f, out, roles, errs, mode)
		return true
	}
	if selectorBody {
		out.Kind = FileLibrary
		out.Library = lowerLibraryFile(f, errs, mode)
		return true
	}
	return false
}

func validateSourceDeclaredPath(
	f *parse.File,
	roles []sourceFileRole,
	errs *parse.ErrorList,
) bool {
	if want, ok := reservedSourceFileRole(f.Path); ok {
		if len(roles) == 0 || roles[0].name != want {
			errs.Addf(parse.ErrSchema, f.S.Start,
				"%s must declare %s", filepath.Base(f.Path), want)
			return false
		}
		return true
	}
	if len(roles) == 0 {
		return true
	}
	name := roles[0].name
	if filename, ok := sourceRoleFilename(name); ok {
		errs.Addf(parse.ErrSchema, roles[0].fld.Key.S.Start,
			"%s declaration must be in %s", name, filename)
		return false
	}
	return true
}

func reservedSourceFileRole(path string) (string, bool) {
	switch filepath.Base(path) {
	case "factory.ub":
		return "factory", true
	case "manifest.ub":
		return "manifest", true
	case "lock.ub":
		return "lock", true
	default:
		return "", false
	}
}

func sourceRoleFilename(role string) (string, bool) {
	switch role {
	case "factory":
		return "factory.ub", true
	case "manifest":
		return "manifest.ub", true
	case "lock":
		return "lock.ub", true
	default:
		return "", false
	}
}

func lowerSourceDeclaredRole(
	f *parse.File,
	out *File,
	roles []sourceFileRole,
	errs *parse.ErrorList,
	mode lowerMode,
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
		out.Factory = &FactoryFile{
			S:    first.fld.S,
			Body: lowerFactoryBodyWithMode(block, errs, mode),
		}
	case "stack":
		out.Kind = FileStack
		out.Stack = lowerStackFile(first.fld.S, block, errs, mode)
	case "manifest":
		out.Kind = FileManifest
		out.Manifest = lowerManifestFile(first.fld.S, block, errs)
	case "lock":
		out.Kind = FileLock
		out.Lock = lowerLockFile(first.fld.S, block, errs)
	}
}

func lowerFactoryBodyWithMode(
	block *parse.ObjectLit,
	errs *parse.ErrorList,
	mode lowerMode,
) FactoryBody {
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
				body.Inputs = lowerInputs(obj, errs, mode)
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
		case "library-configs":
			if obj := objectValue(fld, "library-configs", errs); obj != nil {
				body.LibraryConfigs = lowerLibraryConfigDecls(obj, errs)
			}
		case "state-moves":
			if arr := arrayValue(fld, "state-moves", errs); arr != nil {
				body.StateMoves = lowerStateMoves(arr, errs)
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
	mode lowerMode,
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
				stack.Factory = lowerStackFactory(fld.S, obj, errs, mode)
			}
		case "locals":
			if obj := objectValue(fld, "locals", errs); obj != nil {
				stack.Locals = lowerLocals(obj, "local", errs)
			}
		case "state":
			if fld.Decl != nil {
				stack.State = lowerStateSelectorDecl(fld, errs)
			} else {
				errs.Addf(parse.ErrSchema, fld.Key.S.Start,
					"state must be written as state: <backend> { ... }")
			}
		case "encryption":
			if fld.Decl != nil {
				stack.Encryption = lowerEncryptionSelectorDecl(fld, errs)
			} else {
				errs.Addf(parse.ErrSchema, fld.Key.S.Start,
					"encryption must be written as encryption: <key-source> { ... }")
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
	mode lowerMode,
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

func lowerLockFile(span parse.Span, block *parse.ObjectLit, errs *parse.ErrorList) *LockFile {
	lock := &LockFile{S: span}
	if block == nil {
		return lock
	}
	seen := make(map[string]parse.Position, len(block.Fields))
	for _, fld := range block.Fields {
		name, ok := fieldName(fld, "lock field", errs)
		if !ok {
			continue
		}
		if prev, dup := seen[name.Name]; dup {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"lock: duplicate key %q (first defined at %s)", name.Name, prev)
			continue
		}
		seen[name.Name] = fld.Key.S.Start
		switch name.Name {
		case "version":
			lock.Version = numberValue(fld, "lock version", errs)
		case "toolchain":
			if obj := objectValue(fld, "lock toolchain", errs); obj != nil {
				lock.Toolchain = lowerLockToolchain(fld.S, obj, errs)
			}
		case "deps":
			if obj := objectValue(fld, "lock deps", errs); obj != nil {
				lock.Deps = lowerLockDeps(obj, errs)
			}
		default:
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"%q is not a valid lock field", name.Name)
		}
	}
	if _, ok := seen["version"]; !ok {
		errs.Addf(parse.ErrSchema, block.S.Start, "lock: missing version")
	}
	if _, ok := seen["toolchain"]; !ok {
		errs.Addf(parse.ErrSchema, block.S.Start, "lock: missing toolchain")
	}
	if _, ok := seen["deps"]; !ok {
		errs.Addf(parse.ErrSchema, block.S.Start, "lock: missing deps")
	}
	return lock
}

func lowerLockToolchain(
	span parse.Span,
	block *parse.ObjectLit,
	errs *parse.ErrorList,
) *LockToolchain {
	toolchain := &LockToolchain{S: span}
	seen := make(map[string]parse.Position, len(block.Fields))
	for _, fld := range block.Fields {
		name, ok := fieldName(fld, "lock toolchain field", errs)
		if !ok {
			continue
		}
		if prev, dup := seen[name.Name]; dup {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"lock toolchain: duplicate key %q (first defined at %s)", name.Name, prev)
			continue
		}
		seen[name.Name] = fld.Key.S.Start
		switch name.Name {
		case "unobin-version":
			toolchain.UnobinVersion = stringValue(fld, "lock toolchain unobin-version", errs)
		default:
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"%q is not a valid lock toolchain field", name.Name)
		}
	}
	if _, ok := seen["unobin-version"]; !ok {
		errs.Addf(parse.ErrSchema, block.S.Start,
			"lock toolchain: missing unobin-version")
	}
	return toolchain
}

func lowerLibraryFile(f *parse.File, errs *parse.ErrorList, mode lowerMode) *LibraryFile {
	library := &LibraryFile{S: f.S}
	if hasSelectorBody(f.Body) {
		library.Exports = lowerCompositeDecls(f.Body, errs, mode)
		return library
	}
	errs.Addf(parse.ErrSchema, f.S.Start, "library file must contain composite declarations")
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

func lowerCompositeDecls(
	block *parse.ObjectLit,
	errs *parse.ErrorList,
	mode lowerMode,
) []CompositeDecl {
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
			Body: lowerFactoryBodyWithMode(fld.Decl.Body, errs, mode),
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

func lowerInputs(
	block *parse.ObjectLit,
	errs *parse.ErrorList,
	mode lowerMode,
) []InputDecl {
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
			Type: lowerInputType(name.Name, body, errs, mode),
		})
	}
	return inputs
}

func lowerInputType(
	name string,
	body *parse.ObjectLit,
	errs *parse.ErrorList,
	mode lowerMode,
) parse.TypeExpr {
	var out parse.TypeExpr
	var found bool
	for i, fld := range body.Fields {
		if fld.Key.Kind != parse.FieldIdent || fld.Key.Name != "type" {
			continue
		}
		if found {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"input %q: duplicate key %q", name, "type")
			continue
		}
		found = true
		t, err := parseInputTypeValue(name, body, i, mode)
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
		fld.Value = t
		out = t
		storeNestedTypeFields(name, t, errs, mode)
	}
	if !found {
		errs.Addf(parse.ErrSchema, body.S.Start,
			"input %q: missing required `type:` key", name)
	}
	return out
}

func parseInputTypeValue(
	name string,
	block *parse.ObjectLit,
	idx int,
	mode lowerMode,
) (parse.TypeExpr, error) {
	fld := block.Fields[idx]
	if t, ok := fld.Value.(parse.TypeExpr); ok {
		return t, nil
	}
	if src, ok := fieldValueSource(block, idx, mode.source); ok {
		t, err := parse.ParseTypeAt(mode.path, src, fld.Value.Span().Start)
		if err != nil {
			return nil, parse.Errorf(parse.ErrType, fld.Value.Span().Start,
				"input %q: %s", name, typeParseMessage(err))
		}
		return t, nil
	}
	return nil, fmt.Errorf("type field was not parsed from source")
}

func typeParseMessage(err error) string {
	msg := err.Error()
	_, rest, ok := strings.Cut(msg, ": rule ")
	if !ok {
		return msg
	}
	_, out, ok := strings.Cut(rest, ": ")
	if !ok {
		return msg
	}
	return out
}

func fieldValueSource(
	block *parse.ObjectLit,
	idx int,
	source []byte,
) ([]byte, bool) {
	if len(source) == 0 && block != nil {
		source = block.Source
	}
	if len(source) == 0 || block == nil || idx < 0 || idx >= len(block.Fields) {
		return nil, false
	}
	fld := block.Fields[idx]
	if fld.Value == nil {
		return nil, false
	}
	start := fld.Value.Span().Start.Offset
	end := block.S.End.Offset - 1
	if idx+1 < len(block.Fields) {
		end = block.Fields[idx+1].S.Start.Offset
	}
	if start < 0 || end < start || end > len(source) {
		return nil, false
	}
	return trimTypeSource(source[start:end]), true
}

func trimTypeSource(src []byte) []byte {
	out := bytes.TrimSpace(src)
	if len(out) > 0 && out[len(out)-1] == ',' {
		out = bytes.TrimSpace(out[:len(out)-1])
	}
	return out
}

func storeNestedTypeFields(
	name string,
	t parse.TypeExpr,
	errs *parse.ErrorList,
	mode lowerMode,
) {
	switch v := t.(type) {
	case *parse.TypeList:
		storeNestedTypeFields(name, v.Elem, errs, mode)
	case *parse.TypeMap:
		storeNestedTypeFields(name, v.Elem, errs, mode)
	case *parse.TypeOptional:
		storeNestedTypeFields(name, v.Elem, errs, mode)
	case *parse.TypeTuple:
		for _, elem := range v.Elements {
			storeNestedTypeFields(name, elem, errs, mode)
		}
	case *parse.TypeObject:
		for _, field := range v.Fields {
			fieldName := name + "." + field.Name
			if field.Decl != nil {
				lowerInputType(fieldName, field.Decl, errs, mode)
				continue
			}
			if field.Type != nil {
				storeNestedTypeFields(fieldName, field.Type, errs, mode)
			}
		}
	}
}

func lowerLocals(block *parse.ObjectLit, what string, errs *parse.ErrorList) []LocalDecl {
	locals := make([]LocalDecl, 0, len(block.Fields))
	for _, fld := range block.Fields {
		name, ok := fieldName(fld, what+" name", errs)
		if !ok {
			continue
		}
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

func lowerLibraryConfigDecls(
	block *parse.ObjectLit,
	errs *parse.ErrorList,
) []LibraryConfigDecl {
	decls := make([]LibraryConfigDecl, 0, len(block.Fields))
	seen := make(map[string]parse.Position, len(block.Fields))
	for _, fld := range block.Fields {
		alias, ok := fieldName(fld, "library config alias", errs)
		if !ok {
			continue
		}
		if fld.Decl != nil || fld.Value == nil {
			errs.Addf(parse.ErrSchema, fld.S.Start,
				"library config %s must be an expression", alias.Name)
			continue
		}
		if prev, dup := seen[alias.Name]; dup {
			errs.Addf(parse.ErrSchema, alias.S.Start,
				"duplicate library config %q (first defined at %s)", alias.Name, prev)
			continue
		}
		seen[alias.Name] = alias.S.Start
		decls = append(decls, LibraryConfigDecl{S: fld.S, Alias: alias, Value: fld.Value})
	}
	return decls
}

func lowerStateMoves(arr *parse.ArrayLit, errs *parse.ErrorList) []StateMoveDecl {
	moves := make([]StateMoveDecl, 0, len(arr.Elements))
	for i, elem := range arr.Elements {
		obj, ok := elem.(*parse.ObjectLit)
		if !ok {
			errs.Addf(parse.ErrSchema, elem.Span().Start,
				"state-moves[%d] must be an object", i)
			continue
		}
		moves = append(moves, lowerStateMove(i, obj, errs))
	}
	return moves
}

func lowerStateMove(i int, obj *parse.ObjectLit, errs *parse.ErrorList) StateMoveDecl {
	move := StateMoveDecl{S: obj.S}
	seen := make(map[string]parse.Position, len(obj.Fields))
	for _, fld := range obj.Fields {
		name, ok := fieldName(fld, fmt.Sprintf("state-moves[%d] field", i), errs)
		if !ok {
			continue
		}
		if prev, dup := seen[name.Name]; dup {
			errs.Addf(parse.ErrSchema, name.S.Start,
				"state-moves[%d]: duplicate field %q (first defined at %s)",
				i, name.Name, prev)
			continue
		}
		seen[name.Name] = name.S.Start
		switch name.Name {
		case "from":
			move.From = stringValue(fld, fmt.Sprintf("state-moves[%d].from", i), errs)
		case "to":
			move.To = stringValue(fld, fmt.Sprintf("state-moves[%d].to", i), errs)
		default:
			errs.Addf(parse.ErrSchema, name.S.Start,
				"state-moves[%d]: unknown field %q", i, name.Name)
		}
	}
	if _, ok := seen["from"]; !ok {
		errs.Addf(parse.ErrSchema, obj.S.Start, "state-moves[%d]: missing from", i)
	}
	if _, ok := seen["to"]; !ok {
		errs.Addf(parse.ErrSchema, obj.S.Start, "state-moves[%d]: missing to", i)
	}
	return move
}

func lowerNodes(
	block *parse.ObjectLit,
	kind NodeKind,
	errs *parse.ErrorList,
) []NodeDecl {
	nodes := make([]NodeDecl, 0, len(block.Fields))
	seen := make(map[string]parse.Position, len(block.Fields))
	for _, fld := range block.Fields {
		if fld.Decl == nil {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"%s must be written as name: alias.export { ... }", kind)
			continue
		}
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

func lowerStateSelectorDecl(fld *parse.Field, errs *parse.ErrorList) *StateDecl {
	selector, ok := selectorIdent(fld.Decl.Selector, "state selector", errs)
	if !ok {
		return &StateDecl{S: fld.S, Body: fld.Decl.Body}
	}
	return &StateDecl{S: fld.S, Selector: selector, Body: fld.Decl.Body}
}

func lowerEncryptionSelectorDecl(fld *parse.Field, errs *parse.ErrorList) *EncryptionDecl {
	selector, ok := selectorIdent(fld.Decl.Selector, "encryption selector", errs)
	if !ok {
		return &EncryptionDecl{S: fld.S, Body: fld.Decl.Body}
	}
	return &EncryptionDecl{S: fld.S, Selector: selector, Body: fld.Decl.Body}
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
		body, ok := fld.Value.(*parse.ObjectLit)
		if !ok {
			pos := fld.S.Start
			if fld.Value != nil {
				pos = fld.Value.Span().Start
			}
			errs.Addf(parse.ErrSchema, pos,
				"requires: dependency %q: value must be an object", id.Value)
			continue
		}
		requires = append(requires, lowerManifestRequire(fld.S, id, body, errs))
	}
	return requires
}

func lowerManifestRequire(
	span parse.Span,
	id StringKey,
	body *parse.ObjectLit,
	errs *parse.ErrorList,
) ManifestRequire {
	require := ManifestRequire{S: span, ID: id}
	seen := make(map[string]parse.Position, len(body.Fields))
	versionSeen := false
	for _, fld := range body.Fields {
		name, ok := fieldName(fld, "require field", errs)
		if !ok {
			continue
		}
		if prev, dup := seen[name.Name]; dup {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"requires: dependency %q: duplicate field %q (first defined at %s)",
				id.Value, name.Name, prev)
			continue
		}
		seen[name.Name] = fld.Key.S.Start
		switch name.Name {
		case "version":
			versionSeen = true
			require.Version = stringValue(fld, "require "+id.Value+": version", errs)
		case "indirect":
			require.Indirect = boolValue(fld, "require "+id.Value+": indirect", errs)
		default:
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"requires: dependency %q: %q is not a valid require field",
				id.Value, name.Name)
		}
	}
	if !versionSeen {
		errs.Addf(parse.ErrSchema, body.S.Start,
			"requires: dependency %q: missing version", id.Value)
	}
	return require
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

func lowerLockDeps(block *parse.ObjectLit, errs *parse.ErrorList) []LockDep {
	deps := make([]LockDep, 0, len(block.Fields))
	seen := make(map[string]parse.Position, len(block.Fields))
	for _, fld := range block.Fields {
		id, ok := stringKey(fld, "lock dependency id", errs)
		if !ok {
			continue
		}
		if prev, dup := seen[id.Value]; dup {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"duplicate lock dependency %q (first defined at %s)", id.Value, prev)
			continue
		}
		seen[id.Value] = fld.Key.S.Start
		body := objectValue(fld, "lock dependency "+id.Value, errs)
		if body == nil {
			continue
		}
		deps = append(deps, lowerLockDep(fld.S, id, body, errs))
	}
	return deps
}

func lowerLockDep(
	span parse.Span,
	id StringKey,
	block *parse.ObjectLit,
	errs *parse.ErrorList,
) LockDep {
	dep := LockDep{S: span, ID: id}
	seen := make(map[string]parse.Position, len(block.Fields))
	for _, fld := range block.Fields {
		name, ok := fieldName(fld, "lock dependency field", errs)
		if !ok {
			continue
		}
		if prev, dup := seen[name.Name]; dup {
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"lock dependency %s: duplicate key %q (first defined at %s)",
				id.Value, name.Name, prev)
			continue
		}
		seen[name.Name] = fld.Key.S.Start
		switch name.Name {
		case "kind":
			dep.Kind = identValue(fld, "lock dependency "+id.Value+": kind", errs)
		case "version":
			dep.Version = stringValue(fld, "lock dependency "+id.Value+": version", errs)
		case "commit":
			dep.Commit = stringValue(fld, "lock dependency "+id.Value+": commit", errs)
		case "hash":
			dep.Hash = stringValue(fld, "lock dependency "+id.Value+": hash", errs)
		default:
			errs.Addf(parse.ErrSchema, fld.Key.S.Start,
				"lock dependency %s: unknown key %q", id.Value, name.Name)
		}
	}
	validateLockDep(dep, seen, block.S.Start, errs)
	return dep
}

func validateLockDep(
	dep LockDep,
	seen map[string]parse.Position,
	pos parse.Position,
	errs *parse.ErrorList,
) {
	if _, ok := seen["kind"]; !ok {
		errs.Addf(parse.ErrSchema, pos,
			"lock dependency %s: missing kind", dep.ID.Value)
		return
	}
	switch dep.Kind.Name {
	case "go":
		if dep.Hash != nil {
			errs.Addf(parse.ErrSchema, dep.Hash.S.Start,
				"lock dependency %s: go kind forbids hash", dep.ID.Value)
		}
	case "ub":
		if dep.Hash == nil {
			errs.Addf(parse.ErrSchema, pos,
				"lock dependency %s: ub kind requires hash", dep.ID.Value)
		}
	default:
		errs.Addf(parse.ErrSchema, dep.Kind.S.Start,
			"lock dependency %s: unknown kind %q", dep.ID.Value, dep.Kind.Name)
	}
	if _, ok := seen["version"]; !ok {
		errs.Addf(parse.ErrSchema, pos,
			"lock dependency %s: missing version", dep.ID.Value)
	}
	if _, ok := seen["commit"]; !ok {
		errs.Addf(parse.ErrSchema, pos,
			"lock dependency %s: missing commit", dep.ID.Value)
	}
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

func identValue(fld *parse.Field, what string, errs *parse.ErrorList) Ident {
	if fld.Value == nil {
		errs.Addf(parse.ErrSchema, fld.S.Start,
			"%s must be a bare identifier", what)
		return Ident{}
	}
	value, ok := fld.Value.(*parse.Ident)
	if !ok {
		errs.Addf(parse.ErrSchema, fld.Value.Span().Start,
			"%s must be a bare identifier", what)
		return Ident{}
	}
	return Ident{S: value.S, Name: value.Name}
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

func numberValue(fld *parse.Field, what string, errs *parse.ErrorList) *parse.NumberLit {
	if fld.Value == nil {
		errs.Addf(parse.ErrSchema, fld.S.Start,
			"%s must be an integer", what)
		return nil
	}
	value, ok := fld.Value.(*parse.NumberLit)
	if !ok || value.IsFloat {
		errs.Addf(parse.ErrSchema, fld.Value.Span().Start,
			"%s must be an integer", what)
		return nil
	}
	return value
}

func boolValue(fld *parse.Field, what string, errs *parse.ErrorList) *parse.BoolLit {
	if fld.Value == nil {
		errs.Addf(parse.ErrSchema, fld.S.Start,
			"%s must be a boolean literal", what)
		return nil
	}
	value, ok := fld.Value.(*parse.BoolLit)
	if !ok {
		errs.Addf(parse.ErrSchema, fld.Value.Span().Start,
			"%s must be a boolean literal", what)
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
