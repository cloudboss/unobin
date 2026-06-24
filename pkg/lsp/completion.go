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
	if projects == nil {
		projects = NewProjectCache("")
	}
	if list, ok := completionForSourceContext(text, offset); ok {
		return list, nil
	}
	file, err := parseCompletionSource(path, text, offset)
	if err != nil {
		return protocol.CompletionList{}, protocol.InvalidParams(err.Error())
	}
	body, hasScope := definitionBodyForOffset(file, offset)
	decls := definitionDeclsForBody(body)
	if hasScope {
		list, found, err := completionAtOffset(
			path, text, offset, body, decls, projects,
		)
		if found || err != nil {
			if err != nil {
				return protocol.CompletionList{}, protocol.InternalError(err)
			}
			return list, nil
		}
	}
	tok := tokenAtOffset(text, offset)
	if tok.text == "" {
		return completionList(rootCompletionItems()), nil
	}
	list, err := completionForToken(path, text, offset, tok.text, decls, projects)
	if err != nil {
		return protocol.CompletionList{}, protocol.InternalError(err)
	}
	return list, nil
}

func completionForSourceContext(
	text string,
	offset int,
) (protocol.CompletionList, bool) {
	if strings.TrimSpace(text[:offset]) == "" {
		return completionList(fileRoleCompletionItems()), true
	}
	prefix := currentLinePrefix(text, offset)
	if typeValueCompletionContext(prefix) {
		return completionList(typeCompletionItems()), true
	}
	if selectorValueCompletionContext(prefix, "state") {
		return completionList(stackStateCompletionItems()), true
	}
	if selectorValueCompletionContext(prefix, "encryption") {
		return completionList(stackEncryptionCompletionItems()), true
	}
	if projectRequirementCompletionContext(text, offset) {
		return completionList(projectRequirementCompletionItems()), true
	}
	if projectBlockCompletionContext(text, offset) {
		return completionList(projectBlockCompletionItems()), true
	}
	if stackBlockCompletionContext(text, offset) {
		return completionList(stackBlockCompletionItems()), true
	}
	if inputDeclarationCompletionContext(text, offset) {
		return completionList(inputDeclarationCompletionItems()), true
	}
	if factoryBlockCompletionContext(text, offset) {
		return completionList(factoryBlockCompletionItems()), true
	}
	return protocol.CompletionList{}, false
}

func currentLinePrefix(text string, offset int) string {
	start := strings.LastIndex(text[:offset], "\n") + 1
	return text[start:offset]
}

func typeValueCompletionContext(prefix string) bool {
	before, ok := strings.CutSuffix(prefix, "type: ")
	return ok && strings.TrimSpace(before) == ""
}

func selectorValueCompletionContext(prefix string, key string) bool {
	before, ok := strings.CutSuffix(prefix, key+": ")
	return ok && strings.TrimSpace(before) == ""
}

func currentLineSuffix(text string, offset int) string {
	end := strings.Index(text[offset:], "\n")
	if end < 0 {
		return text[offset:]
	}
	return text[offset : offset+end]
}

func inputDeclarationCompletionContext(text string, offset int) bool {
	if !insideNamedBlock(text, offset, "inputs") ||
		typeValueCompletionContext(currentLinePrefix(text, offset)) {
		return false
	}
	line := strings.TrimSpace(currentLinePrefix(text, offset))
	candidate := line
	if candidate == "" {
		candidate = strings.TrimSpace(currentLineSuffix(text, offset))
	}
	if candidate == "" {
		return true
	}
	for _, key := range []string{"type", "description", "default", "sensitive"} {
		if strings.HasPrefix(key, candidate) || strings.HasPrefix(candidate, key+":") {
			return true
		}
	}
	return false
}

func projectRequirementCompletionContext(text string, offset int) bool {
	if !insideNamedBlock(text, offset, "requires") {
		return false
	}
	return completionCandidateMatches(text, offset, []string{"version", "indirect"})
}

func projectBlockCompletionContext(text string, offset int) bool {
	return insideNamedBlock(text, offset, "project") &&
		nearestProjectChildBlockName(text, offset) == "" &&
		strings.TrimSpace(currentLinePrefix(text, offset)) == ""
}

func stackBlockCompletionContext(text string, offset int) bool {
	return insideNamedBlock(text, offset, "stack") &&
		nearestStackChildBlockName(text, offset) == "" &&
		strings.TrimSpace(currentLinePrefix(text, offset)) == ""
}

func factoryBlockCompletionContext(text string, offset int) bool {
	return insideNamedBlock(text, offset, "factory") &&
		nearestFactoryChildBlockName(text, offset) == "" &&
		strings.TrimSpace(currentLinePrefix(text, offset)) == ""
}

func completionCandidateMatches(text string, offset int, keys []string) bool {
	line := strings.TrimSpace(currentLinePrefix(text, offset))
	candidate := line
	if candidate == "" {
		candidate = strings.TrimSpace(currentLineSuffix(text, offset))
	}
	if candidate == "" {
		return true
	}
	for _, key := range keys {
		if strings.HasPrefix(key, candidate) || strings.HasPrefix(candidate, key+":") {
			return true
		}
	}
	return false
}

func insideNamedBlock(text string, offset int, name string) bool {
	prefix := text[:offset]
	marker := name + ":"
	start := strings.LastIndex(prefix, marker)
	if start < 0 {
		return false
	}
	open := strings.Index(prefix[start:], "{")
	if open < 0 {
		return false
	}
	depth := 0
	blockText := prefix[start+open:]
	for i, r := range blockText {
		switch r {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 && i < len(blockText)-1 {
				return false
			}
		}
	}
	return depth > 0
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
	text string,
	offset int,
	body *syntax.FactoryBody,
	decls definitionDecls,
	projects *ProjectCache,
) (protocol.CompletionList, bool, error) {
	if body == nil {
		return protocol.CompletionList{}, false, nil
	}
	if list, found, err := libraryConfigInputCompletions(
		path, text, offset, body.Inputs, decls, projects,
	); found || err != nil {
		return list, found, err
	}
	for _, cfg := range body.LibraryConfigs {
		obj, ok := cfg.Value.(*parse.ObjectLit)
		if !ok {
			continue
		}
		fieldPath, ok := objectKeyPathAtOffset(text, obj, offset)
		if !ok {
			continue
		}
		return goConfigFieldCompletions(path, cfg.Alias.Name, fieldPath, decls, projects)
	}
	for _, node := range allNodes(*body) {
		fieldPath, ok := objectKeyPathAtOffset(text, node.Body, offset)
		if ok {
			return goNodeFieldCompletions(path, node, fieldPath, decls, projects)
		}
		if spanContainsOffset(node.Body.Span(), offset) {
			return goNodeFieldCompletions(path, node, "", decls, projects)
		}
	}
	return protocol.CompletionList{}, false, nil
}

func libraryConfigInputCompletions(
	path string,
	text string,
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
		fieldPath, ok := objectKeyPathAtOffset(text, defaultObj, offset)
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
	text string,
	offset int,
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
			path, parts[0], selectorKindAtOffset(text, offset), decls, projects,
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

func fileRoleCompletionItems() []protocol.CompletionItem {
	return keywordCompletionItems("factory", "stack", "project", "project-lock")
}

func factoryBlockCompletionItems() []protocol.CompletionItem {
	return keywordCompletionItems(
		"inputs", "imports", "library-configs", "resources", "data-sources",
		"actions", "outputs", "constraints", "state-moves", "locals",
	)
}

func stackBlockCompletionItems() []protocol.CompletionItem {
	return keywordCompletionItems("factory", "state", "encryption", "locals", "parallelism")
}

func projectBlockCompletionItems() []protocol.CompletionItem {
	return keywordCompletionItems("unobin-version", "requires", "replace")
}

func projectRequirementCompletionItems() []protocol.CompletionItem {
	return keywordCompletionItems("version", "indirect")
}

func stackStateCompletionItems() []protocol.CompletionItem {
	return keywordCompletionItems("local", "s3")
}

func stackEncryptionCompletionItems() []protocol.CompletionItem {
	return keywordCompletionItems("env-key", "kms", "noop")
}

func inputDeclarationCompletionItems() []protocol.CompletionItem {
	return keywordCompletionItems("type", "description", "default", "sensitive")
}

func typeCompletionItems() []protocol.CompletionItem {
	return keywordCompletionItems(
		"string", "number", "integer", "boolean", "null", "opaque", "object",
		"list", "map", "tuple", "optional", "open", "library-config",
	)
}

func keywordCompletionItems(labels ...string) []protocol.CompletionItem {
	items := make([]protocol.CompletionItem, 0, len(labels))
	for _, label := range labels {
		items = append(items, protocol.CompletionItem{
			Label: label,
			Kind:  protocol.CompletionItemKindKeyword,
		})
	}
	return items
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
	selectorKind syntax.NodeKind,
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
	names := selectorCompletionNames(schema, selectorKind)
	items := namedCompletionItems(names, protocol.CompletionItemKindFunction)
	return completionList(items), true, nil
}

func selectorCompletionNames(
	schema *ubruntime.LibrarySchema,
	selectorKind syntax.NodeKind,
) []string {
	switch selectorKind {
	case syntax.NodeResource:
		return mapKeys(schema.Resources)
	case syntax.NodeDataSource:
		return mapKeys(schema.DataSources)
	case syntax.NodeAction:
		return mapKeys(schema.Actions)
	default:
		names := mapKeys(schema.Resources)
		names = append(names, mapKeys(schema.DataSources)...)
		names = append(names, mapKeys(schema.Actions)...)
		names = append(names, mapKeys(schema.Functions)...)
		return names
	}
}

func selectorKindAtOffset(text string, offset int) syntax.NodeKind {
	switch nearestBlockName(text, offset) {
	case "resources":
		return syntax.NodeResource
	case "data-sources":
		return syntax.NodeDataSource
	case "actions":
		return syntax.NodeAction
	default:
		return ""
	}
}

func nearestBlockName(text string, offset int) string {
	return nearestBlockNameFrom(text, offset, []string{"resources", "data-sources", "actions"})
}

func nearestFactoryChildBlockName(text string, offset int) string {
	return nearestBlockNameFrom(text, offset, []string{
		"inputs", "imports", "library-configs", "resources", "data-sources",
		"actions", "outputs", "constraints", "state-moves", "locals",
	})
}

func nearestStackChildBlockName(text string, offset int) string {
	return nearestBlockNameFrom(text, offset, []string{
		"factory", "state", "encryption", "locals", "parallelism",
	})
}

func nearestProjectChildBlockName(text string, offset int) string {
	return nearestBlockNameFrom(text, offset, []string{
		"unobin-version", "requires", "replace",
	})
}

func nearestBlockNameFrom(text string, offset int, names []string) string {
	prefix := text[:offset]
	bestName := ""
	bestOffset := -1
	for _, name := range names {
		idx := strings.LastIndex(prefix, name+":")
		if idx > bestOffset {
			bestName = name
			bestOffset = idx
		}
	}
	return bestName
}

func goNodeFieldCompletions(
	path string,
	node syntax.NodeDecl,
	fieldPath string,
	decls definitionDecls,
	projects *ProjectCache,
) (protocol.CompletionList, bool, error) {
	typeSchema, found, err := goNodeSchema(path, node, decls, projects)
	if err != nil || !found || typeSchema == nil {
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
		return &ubruntime.LibrarySchema{}, true, nil
	}
	if resolved.source == nil || resolved.source.Path == "" || !resolve.IsGoLibrary(resolved.source) {
		return nil, false, nil
	}
	resolved.project.EnsureGoModuleRoot(resolved.source)
	schema, _, _, err := resolved.project.GoIndex.Read(resolved.source.Path)
	if err != nil {
		return nil, true, err
	}
	return schema, true, nil
}
