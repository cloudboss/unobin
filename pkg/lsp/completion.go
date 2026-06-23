package lsp

import (
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/lsp/protocol"
	"github.com/cloudboss/unobin/pkg/resolve"
	ubruntime "github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

// CompleteForText resolves a completion request.
func CompleteForText(
	path string,
	text string,
	pos protocol.Position,
	projects *ProjectCache,
) (protocol.CompletionList, *protocol.ResponseError) {
	offset, ok := LSPToOffset(text, pos)
	if !ok {
		return protocol.CompletionList{}, protocol.InvalidParams("invalid document position")
	}
	file, err := parseCompletionSource(path, text, offset)
	if err != nil {
		return protocol.CompletionList{}, protocol.InvalidParams(err.Error())
	}
	if projects == nil {
		projects = NewProjectCache("")
	}
	decls := definitionDeclsForFile(file)
	if list, found, err := completionAtOffset(
		path, offset, file, decls, projects,
	); found || err != nil {
		if err != nil {
			return protocol.CompletionList{}, protocol.InternalError(err)
		}
		return list, nil
	}
	tok := tokenAtOffset(text, offset)
	if tok.text == "" {
		return completionList(rootCompletionItems()), nil
	}
	list, err := completionForToken(path, tok.text, decls, projects)
	if err != nil {
		return protocol.CompletionList{}, protocol.InternalError(err)
	}
	return list, nil
}

func parseCompletionSource(path string, text string, offset int) (*syntax.File, error) {
	file, err := syntax.ParseSource(path, []byte(text))
	if err == nil {
		return file, nil
	}
	repaired := text[:offset] + "complete" + text[offset:]
	return syntax.ParseSource(path, []byte(repaired))
}

func completionAtOffset(
	path string,
	offset int,
	file *syntax.File,
	decls definitionDecls,
	projects *ProjectCache,
) (protocol.CompletionList, bool, error) {
	if file == nil || file.Factory == nil {
		return protocol.CompletionList{}, false, nil
	}
	body := file.Factory.Body
	if list, found, err := libraryConfigInputCompletions(
		path, offset, body.Inputs, decls, projects,
	); found || err != nil {
		return list, found, err
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
		return goConfigFieldCompletions(path, cfg.Alias.Name, fieldPath, decls, projects)
	}
	for _, node := range allNodes(body) {
		fieldPath, ok := objectKeyPathAtOffset(node.Body, offset)
		if !ok {
			continue
		}
		return goNodeFieldCompletions(path, node, fieldPath, decls, projects)
	}
	return protocol.CompletionList{}, false, nil
}

func libraryConfigInputCompletions(
	path string,
	offset int,
	inputs []syntax.InputDecl,
	decls definitionDecls,
	projects *ProjectCache,
) (protocol.CompletionList, bool, error) {
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
		return goConfigFieldCompletionsForPath(
			path, lib.Path.Value, fieldPath, decls, projects,
		)
	}
	return protocol.CompletionList{}, false, nil
}

func completionForToken(
	path string,
	token string,
	decls definitionDecls,
	projects *ProjectCache,
) (protocol.CompletionList, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return completionList(rootCompletionItems()), nil
	}
	if len(parts) > 2 {
		return protocol.CompletionList{}, nil
	}
	switch parts[0] {
	case "input":
		items := namedCompletionItems(
			mapKeys(decls.inputs), protocol.CompletionItemKindVariable,
		)
		return completionList(items), nil
	case "local":
		items := namedCompletionItems(
			mapKeys(decls.locals), protocol.CompletionItemKindVariable,
		)
		return completionList(items), nil
	case string(syntax.NodeResource):
		return completionList(nodeNameItems(decls.nodes[syntax.NodeResource])), nil
	case string(syntax.NodeDataSource):
		return completionList(nodeNameItems(decls.nodes[syntax.NodeDataSource])), nil
	case string(syntax.NodeAction):
		return completionList(nodeNameItems(decls.nodes[syntax.NodeAction])), nil
	default:
		if list, found, err := goSelectorCompletions(
			path, parts[0], decls, projects,
		); found || err != nil {
			return list, err
		}
	}
	return protocol.CompletionList{}, nil
}

func rootCompletionItems() []protocol.CompletionItem {
	return []protocol.CompletionItem{
		{Label: "input", Kind: protocol.CompletionItemKindKeyword},
		{Label: "local", Kind: protocol.CompletionItemKindKeyword},
		{Label: "resource", Kind: protocol.CompletionItemKindKeyword},
		{Label: "data-source", Kind: protocol.CompletionItemKindKeyword},
		{Label: "action", Kind: protocol.CompletionItemKindKeyword},
		{Label: "@core", Kind: protocol.CompletionItemKindKeyword},
	}
}

func nodeNameItems(nodes map[string]syntax.NodeDecl) []protocol.CompletionItem {
	return namedCompletionItems(mapKeys(nodes), protocol.CompletionItemKindVariable)
}

func namedCompletionItems(
	names []string,
	kind protocol.CompletionItemKind,
) []protocol.CompletionItem {
	names = append([]string(nil), names...)
	slices.Sort(names)
	names = slices.Compact(names)
	items := make([]protocol.CompletionItem, 0, len(names))
	for _, name := range names {
		items = append(items, protocol.CompletionItem{Label: name, Kind: kind})
	}
	return items
}

func completionList(items []protocol.CompletionItem) protocol.CompletionList {
	return protocol.CompletionList{Items: items}
}

func mapKeys[T any](m map[string]T) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	return names
}

func goSelectorCompletions(
	path string,
	alias string,
	decls definitionDecls,
	projects *ProjectCache,
) (protocol.CompletionList, bool, error) {
	resolved, err := resolveImportAlias(path, alias, decls, projects)
	if err != nil || !resolved.found {
		return protocol.CompletionList{}, false, err
	}
	schema, found, err := goSchemaForResolved(resolved)
	if err != nil || !found {
		return protocol.CompletionList{}, found, err
	}
	names := mapKeys(schema.Resources)
	names = append(names, mapKeys(schema.DataSources)...)
	names = append(names, mapKeys(schema.Actions)...)
	names = append(names, mapKeys(schema.Functions)...)
	items := namedCompletionItems(names, protocol.CompletionItemKindFunction)
	return completionList(items), true, nil
}

func goNodeFieldCompletions(
	path string,
	node syntax.NodeDecl,
	fieldPath string,
	decls definitionDecls,
	projects *ProjectCache,
) (protocol.CompletionList, bool, error) {
	typeSchema, found, err := goNodeSchema(path, node, decls, projects)
	if err != nil || !found {
		return protocol.CompletionList{}, found, err
	}
	return completionList(fieldCompletionItems(typeSchema.Inputs, fieldPath)), true, nil
}

func goConfigFieldCompletionsForPath(
	path string,
	libraryPath string,
	fieldPath string,
	decls definitionDecls,
	projects *ProjectCache,
) (protocol.CompletionList, bool, error) {
	for alias, imp := range decls.imports {
		if imp.Ref == nil || imp.Ref.Value != libraryPath {
			continue
		}
		return goConfigFieldCompletions(path, alias, fieldPath, decls, projects)
	}
	return protocol.CompletionList{}, true, nil
}

func goConfigFieldCompletions(
	path string,
	alias string,
	fieldPath string,
	decls definitionDecls,
	projects *ProjectCache,
) (protocol.CompletionList, bool, error) {
	resolved, err := resolveImportAlias(path, alias, decls, projects)
	if err != nil || !resolved.found {
		return protocol.CompletionList{}, false, err
	}
	schema, found, err := goSchemaForResolved(resolved)
	if err != nil || !found {
		return protocol.CompletionList{}, found, err
	}
	return completionList(fieldCompletionItems(schema.Configuration, fieldPath)), true, nil
}

func fieldCompletionItems(
	fields map[string]typecheck.Type,
	fieldPath string,
) []protocol.CompletionItem {
	parent := fieldParentPath(fieldPath)
	if parent == "" {
		return namedCompletionItems(mapKeys(fields), protocol.CompletionItemKindField)
	}
	typ, ok := typeForFieldPath(fields, parent)
	if !ok {
		return nil
	}
	typ = typ.Unwrap()
	items := make([]protocol.CompletionItem, 0, len(typ.Fields))
	for _, field := range typ.Fields {
		items = append(items, protocol.CompletionItem{
			Label: field.Name,
			Kind:  protocol.CompletionItemKindField,
		})
	}
	slices.SortFunc(items, func(a, b protocol.CompletionItem) int {
		return strings.Compare(a.Label, b.Label)
	})
	return items
}

func fieldParentPath(fieldPath string) string {
	idx := strings.LastIndex(fieldPath, ".")
	if idx < 0 {
		return ""
	}
	return fieldPath[:idx]
}

func typeForFieldPath(
	fields map[string]typecheck.Type,
	fieldPath string,
) (typecheck.Type, bool) {
	parts := strings.Split(fieldPath, ".")
	if len(parts) == 0 || parts[0] == "" {
		return typecheck.Type{}, false
	}
	typ, ok := fields[parts[0]]
	if !ok {
		return typecheck.Type{}, false
	}
	for _, part := range parts[1:] {
		typ = typ.Unwrap()
		field, ok := typ.Field(part)
		if !ok {
			return typecheck.Type{}, false
		}
		typ = field.Type
		if field.Optional {
			typ = typecheck.TOptional(typ)
		}
	}
	return typ, true
}

func goNodeSchema(
	path string,
	node syntax.NodeDecl,
	decls definitionDecls,
	projects *ProjectCache,
) (*ubruntime.TypeSchema, bool, error) {
	resolved, err := resolveImportAlias(path, node.Selector.Alias.Name, decls, projects)
	if err != nil || !resolved.found {
		return nil, false, err
	}
	schema, found, err := goSchemaForResolved(resolved)
	if err != nil || !found {
		return nil, found, err
	}
	typeSchema := schemaForNode(schema, node.Kind, node.Selector.Export.Name)
	if typeSchema == nil {
		return nil, true, nil
	}
	return typeSchema, true, nil
}

func schemaForNode(
	schema *ubruntime.LibrarySchema,
	kind syntax.NodeKind,
	name string,
) *ubruntime.TypeSchema {
	if schema == nil {
		return nil
	}
	switch kind {
	case syntax.NodeResource:
		return schema.Resources[name]
	case syntax.NodeDataSource:
		return schema.DataSources[name]
	case syntax.NodeAction:
		return schema.Actions[name]
	default:
		return nil
	}
}

func goSchemaForResolved(
	resolved resolvedImport,
) (*ubruntime.LibrarySchema, bool, error) {
	if !resolved.sourceOK {
		return nil, true, nil
	}
	if resolved.source == nil || resolved.source.Path == "" || !resolve.IsGoLibrary(resolved.source) {
		return nil, false, nil
	}
	schema, _, _, err := resolved.project.GoIndex.Read(resolved.source.Path)
	if err != nil {
		return nil, true, err
	}
	return schema, true, nil
}
