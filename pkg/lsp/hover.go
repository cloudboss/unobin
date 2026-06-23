package lsp

import (
	"fmt"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/lsp/protocol"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

// HoverForText resolves a hover request.
func HoverForText(
	path string,
	text string,
	pos protocol.Position,
	projects *ProjectCache,
) (*protocol.Hover, *protocol.ResponseError) {
	file, err := syntax.ParseSource(path, []byte(text))
	if err != nil {
		return nil, protocol.InvalidParams(err.Error())
	}
	offset, ok := LSPToOffset(text, pos)
	if !ok {
		return nil, protocol.InvalidParams("invalid document position")
	}
	if projects == nil {
		projects = NewProjectCache("")
	}
	decls := definitionDeclsForFile(file)
	if hover, found, err := hoverAtOffset(path, offset, file, decls, projects); found || err != nil {
		if err != nil {
			return nil, protocol.InternalError(err)
		}
		return hover, nil
	}
	tok := tokenAtOffset(text, offset)
	if tok.text == "" {
		return nil, nil
	}
	hover, err := hoverForToken(path, tok.text, decls, projects)
	if err != nil {
		return nil, protocol.InternalError(err)
	}
	return hover, nil
}

func hoverAtOffset(
	path string,
	offset int,
	file *syntax.File,
	decls definitionDecls,
	projects *ProjectCache,
) (*protocol.Hover, bool, error) {
	if file == nil || file.Factory == nil {
		return nil, false, nil
	}
	body := file.Factory.Body
	if hover, found, err := libraryConfigInputHover(
		path, offset, body.Inputs, decls, projects,
	); found || err != nil {
		return hover, found, err
	}
	for _, cfg := range body.LibraryConfigs {
		obj, ok := cfg.Value.(*parse.ObjectLit)
		if !ok {
			continue
		}
		fieldPath, ok := objectKeyPathAtOffset(obj, offset)
		if !ok {
			continue
		}
		return goConfigFieldHover(path, cfg.Alias.Name, fieldPath, decls, projects)
	}
	for _, node := range allNodes(body) {
		fieldPath, ok := objectKeyPathAtOffset(node.Body, offset)
		if !ok {
			continue
		}
		return goNodeFieldHover(path, node, fieldPath, decls, projects)
	}
	return nil, false, nil
}

func libraryConfigInputHover(
	path string,
	offset int,
	inputs []syntax.InputDecl,
	decls definitionDecls,
	projects *ProjectCache,
) (*protocol.Hover, bool, error) {
	for _, input := range inputs {
		lib, ok := input.Type.(*parse.TypeLibraryConfig)
		if !ok || lib.Path == nil {
			continue
		}
		defaultObj := inputDefaultObject(input.Body)
		fieldPath, ok := objectKeyPathAtOffset(defaultObj, offset)
		if !ok {
			continue
		}
		return goConfigFieldHoverForPath(path, lib.Path.Value, fieldPath, decls, projects)
	}
	return nil, false, nil
}

func hoverForToken(
	path string,
	token string,
	decls definitionDecls,
	projects *ProjectCache,
) (*protocol.Hover, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, nil
	}
	switch parts[0] {
	case "input":
		if input, ok := decls.inputs[parts[1]]; ok {
			return plainHover(inputHoverText(input)), nil
		}
	case "local":
		if local, ok := decls.locals[parts[1]]; ok {
			return plainHover("local " + local.Name.Name), nil
		}
	case string(syntax.NodeResource):
		return hoverForNodeRef(path, parts, syntax.NodeResource, decls, projects)
	case string(syntax.NodeDataSource):
		return hoverForNodeRef(path, parts, syntax.NodeDataSource, decls, projects)
	case string(syntax.NodeAction):
		return hoverForNodeRef(path, parts, syntax.NodeAction, decls, projects)
	default:
		if len(parts) == 2 {
			if hover, found, err := goFunctionHover(
				path, parts[0], parts[1], decls, projects,
			); found || err != nil {
				return hover, err
			}
		}
	}
	return nil, nil
}

func hoverForNodeRef(
	path string,
	parts []string,
	kind syntax.NodeKind,
	decls definitionDecls,
	projects *ProjectCache,
) (*protocol.Hover, error) {
	node, ok := decls.nodes[kind][parts[1]]
	if !ok {
		return nil, nil
	}
	if len(parts) >= 3 {
		if hover, found, err := goNodeRefFieldHover(
			path, node, parts[2], decls, projects,
		); found || err != nil {
			return hover, err
		}
		return nil, nil
	}
	return plainHover(fmt.Sprintf("%s %s: %s.%s",
		node.Kind, node.Name.Name, node.Selector.Alias.Name, node.Selector.Export.Name)), nil
}

func inputHoverText(input syntax.InputDecl) string {
	parts := []string{fmt.Sprintf("input %s: %s", input.Name.Name, typeExprString(input.Type))}
	if description := inputDescription(input.Body); description != "" {
		parts = append(parts, description)
	}
	return strings.Join(parts, "\n")
}

func inputDescription(body *parse.ObjectLit) string {
	if body == nil {
		return ""
	}
	for _, field := range body.Fields {
		name, ok := fieldKeyName(field.Key)
		if !ok || name != "description" {
			continue
		}
		lit, ok := field.Value.(*parse.StringLit)
		if ok {
			return lit.Value
		}
	}
	return ""
}

func typeExprString(t parse.TypeExpr) string {
	if lib, ok := t.(*parse.TypeLibraryConfig); ok && lib.Path != nil {
		return "library-config('" + strings.ReplaceAll(lib.Path.Value, "'", `\'`) + "')"
	}
	return typecheck.FromLang(t).String()
}

func goNodeFieldHover(
	path string,
	node syntax.NodeDecl,
	fieldPath string,
	decls definitionDecls,
	projects *ProjectCache,
) (*protocol.Hover, bool, error) {
	typeSchema, found, err := goNodeSchema(path, node, decls, projects)
	if err != nil || !found {
		return nil, found, err
	}
	typ, ok := typeForFieldPath(typeSchema.Inputs, fieldPath)
	if !ok {
		return nil, true, nil
	}
	return plainHover(fieldHoverText(fieldPath, typ)), true, nil
}

func goNodeRefFieldHover(
	path string,
	node syntax.NodeDecl,
	fieldPath string,
	decls definitionDecls,
	projects *ProjectCache,
) (*protocol.Hover, bool, error) {
	typeSchema, found, err := goNodeSchema(path, node, decls, projects)
	if err != nil || !found {
		return nil, found, err
	}
	if typ, ok := typeForFieldPath(typeSchema.Outputs, fieldPath); ok {
		return plainHover(fieldHoverText(fieldPath, typ)), true, nil
	}
	if typ, ok := typeForFieldPath(typeSchema.Inputs, fieldPath); ok {
		return plainHover(fieldHoverText(fieldPath, typ)), true, nil
	}
	return nil, true, nil
}

func goConfigFieldHoverForPath(
	path string,
	libraryPath string,
	fieldPath string,
	decls definitionDecls,
	projects *ProjectCache,
) (*protocol.Hover, bool, error) {
	for alias, imp := range decls.imports {
		if imp.Ref == nil || imp.Ref.Value != libraryPath {
			continue
		}
		return goConfigFieldHover(path, alias, fieldPath, decls, projects)
	}
	return nil, true, nil
}

func goConfigFieldHover(
	path string,
	alias string,
	fieldPath string,
	decls definitionDecls,
	projects *ProjectCache,
) (*protocol.Hover, bool, error) {
	resolved, err := resolveImportAlias(path, alias, decls, projects)
	if err != nil || !resolved.found {
		return nil, false, err
	}
	schema, found, err := goSchemaForResolved(resolved)
	if err != nil || !found {
		return nil, found, err
	}
	typ, ok := typeForFieldPath(schema.Configuration, fieldPath)
	if !ok {
		return nil, true, nil
	}
	return plainHover(fieldHoverText(fieldPath, typ)), true, nil
}

func goFunctionHover(
	path string,
	alias string,
	name string,
	decls definitionDecls,
	projects *ProjectCache,
) (*protocol.Hover, bool, error) {
	resolved, err := resolveImportAlias(path, alias, decls, projects)
	if err != nil || !resolved.found {
		return nil, false, err
	}
	schema, found, err := goSchemaForResolved(resolved)
	if err != nil || !found {
		return nil, found, err
	}
	sig, ok := schema.Functions[name]
	if !ok {
		return nil, true, nil
	}
	return plainHover(functionSignature(name, sig)), true, nil
}

func fieldHoverText(fieldPath string, typ typecheck.Type) string {
	return fmt.Sprintf("%s: %s", lastFieldName(fieldPath), typ.String())
}

func lastFieldName(fieldPath string) string {
	idx := strings.LastIndex(fieldPath, ".")
	if idx < 0 {
		return fieldPath
	}
	return fieldPath[idx+1:]
}

func functionSignature(name string, sig typecheck.FuncSig) string {
	params := make([]string, 0, len(sig.Params)+1)
	for _, param := range sig.Params {
		params = append(params, param.String())
	}
	if sig.Variadic != nil {
		params = append(params, "..."+sig.Variadic.String())
	}
	return fmt.Sprintf("%s(%s) %s", name, strings.Join(params, ", "), sig.Result.String())
}

func plainHover(value string) *protocol.Hover {
	return &protocol.Hover{
		Contents: protocol.MarkupContent{Kind: protocol.MarkupKindPlainText, Value: value},
	}
}
