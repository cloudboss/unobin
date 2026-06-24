package lsp

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/lsp/protocol"
	"github.com/cloudboss/unobin/pkg/resolve"
)

// DefinitionForText resolves a go-to-definition request.
func DefinitionForText(
	path string,
	text string,
	pos protocol.Position,
	projects *ProjectCache,
) ([]protocol.Location, *protocol.ResponseError) {
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
	if locations, found, err := definitionAtOffset(
		path, text, offset, file, decls, projects,
	); found || err != nil {
		if err != nil {
			return nil, protocol.InternalError(err)
		}
		return locations, nil
	}
	tok := tokenAtOffset(text, offset)
	if tok.text == "" {
		return []protocol.Location{}, nil
	}
	locations, err := definitionForToken(path, text, tok.text, decls, projects)
	if err != nil {
		return nil, protocol.InternalError(err)
	}
	return locations, nil
}

type definitionToken struct {
	text  string
	start int
	end   int
}

type definitionDecls struct {
	inputs  map[string]syntax.InputDecl
	locals  map[string]syntax.LocalDecl
	imports map[string]syntax.ImportDecl
	nodes   map[syntax.NodeKind]map[string]syntax.NodeDecl
}

type definitionTarget struct {
	path string
	text string
	span parse.Span
}

type resolvedImport struct {
	project  *Project
	source   *resolve.Source
	found    bool
	sourceOK bool
}

func definitionDeclsForFile(file *syntax.File) definitionDecls {
	decls := definitionDecls{
		inputs:  map[string]syntax.InputDecl{},
		locals:  map[string]syntax.LocalDecl{},
		imports: map[string]syntax.ImportDecl{},
		nodes: map[syntax.NodeKind]map[string]syntax.NodeDecl{
			syntax.NodeResource:   {},
			syntax.NodeDataSource: {},
			syntax.NodeAction:     {},
		},
	}
	if file == nil || file.Factory == nil {
		return decls
	}
	body := file.Factory.Body
	for _, input := range body.Inputs {
		decls.inputs[input.Name.Name] = input
	}
	for _, local := range body.Locals {
		decls.locals[local.Name.Name] = local
	}
	for _, imp := range body.Imports {
		decls.imports[imp.Alias.Name] = imp
	}
	for _, node := range body.Resources {
		decls.nodes[syntax.NodeResource][node.Name.Name] = node
	}
	for _, node := range body.Data {
		decls.nodes[syntax.NodeDataSource][node.Name.Name] = node
	}
	for _, node := range body.Actions {
		decls.nodes[syntax.NodeAction][node.Name.Name] = node
	}
	return decls
}

func definitionAtOffset(
	path string,
	text string,
	offset int,
	file *syntax.File,
	decls definitionDecls,
	projects *ProjectCache,
) ([]protocol.Location, bool, error) {
	if file == nil || file.Factory == nil {
		return nil, false, nil
	}
	body := file.Factory.Body
	for _, imp := range body.Imports {
		if spanContainsOffset(imp.Alias.S, offset) {
			return goImportAliasDefinition(path, imp.Alias.Name, decls, projects)
		}
	}
	if locations, found, err := libraryConfigInputDefinition(
		path, text, offset, body.Inputs, decls, projects,
	); found || err != nil {
		return locations, found, err
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
		return goConfigFieldDefinition(path, cfg.Alias.Name, fieldPath, decls, projects)
	}
	for _, node := range allNodes(body) {
		if spanContainsOffset(node.Selector.Alias.S, offset) {
			return goImportAliasDefinition(path, node.Selector.Alias.Name, decls, projects)
		}
		if spanContainsOffset(node.Selector.Export.S, offset) {
			return goNodeSelectorDefinition(path, node, decls, projects)
		}
		fieldPath, ok := objectKeyPathAtOffset(text, node.Body, offset)
		if !ok {
			continue
		}
		return goNodeFieldDefinition(path, node, fieldPath, decls, projects)
	}
	return nil, false, nil
}

func libraryConfigInputDefinition(
	path string,
	text string,
	offset int,
	inputs []syntax.InputDecl,
	decls definitionDecls,
	projects *ProjectCache,
) ([]protocol.Location, bool, error) {
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
		return goConfigFieldDefinitionForPath(
			path, lib.Path.Value, fieldPath, decls, projects,
		)
	}
	return nil, false, nil
}

func definitionForToken(
	path string,
	text string,
	token string,
	decls definitionDecls,
	projects *ProjectCache,
) ([]protocol.Location, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return []protocol.Location{}, nil
	}
	switch parts[0] {
	case "input":
		if input, ok := decls.inputs[parts[1]]; ok {
			return locationsForTargets(definitionTarget{path: path, text: text, span: input.Name.S}), nil
		}
	case "local":
		if local, ok := decls.locals[parts[1]]; ok {
			return locationsForTargets(definitionTarget{path: path, text: text, span: local.Name.S}), nil
		}
	case string(syntax.NodeResource):
		return nodeReferenceDefinition(path, text, parts, syntax.NodeResource, decls, projects)
	case string(syntax.NodeDataSource):
		return nodeReferenceDefinition(path, text, parts, syntax.NodeDataSource, decls, projects)
	case string(syntax.NodeAction):
		return nodeReferenceDefinition(path, text, parts, syntax.NodeAction, decls, projects)
	default:
		if len(parts) == 2 {
			locations, err := selectorDefinition(path, parts[0], parts[1], decls, projects)
			if err != nil || len(locations) > 0 {
				return locations, err
			}
			if locations, found, err := goFunctionDefinition(
				path, parts[0], parts[1], decls, projects,
			); found || err != nil {
				return locations, err
			}
		}
	}
	return []protocol.Location{}, nil
}

func nodeReferenceDefinition(
	path string,
	text string,
	parts []string,
	kind syntax.NodeKind,
	decls definitionDecls,
	projects *ProjectCache,
) ([]protocol.Location, error) {
	node, ok := decls.nodes[kind][parts[1]]
	if !ok {
		return []protocol.Location{}, nil
	}
	if len(parts) >= 3 {
		if locations, found, err := goNodeRefFieldDefinition(
			path, node, parts[2], decls, projects,
		); found || err != nil {
			return locations, err
		}
		if target, ok, err := compositeTarget(path, node, parts[2], decls, projects); ok || err != nil {
			if err != nil {
				return nil, err
			}
			return locationsForTargets(target), nil
		}
		return []protocol.Location{}, nil
	}
	return locationsForTargets(definitionTarget{path: path, text: text, span: node.Name.S}), nil
}

func selectorDefinition(
	path string,
	alias string,
	export string,
	decls definitionDecls,
	projects *ProjectCache,
) ([]protocol.Location, error) {
	for _, byName := range decls.nodes {
		for _, node := range byName {
			if node.Selector.Alias.Name != alias || node.Selector.Export.Name != export {
				continue
			}
			if locations, found, err := goNodeSelectorDefinition(
				path, node, decls, projects,
			); found || err != nil {
				return locations, err
			}
			target, ok, err := compositeTarget(path, node, "", decls, projects)
			if err != nil {
				return nil, err
			}
			if ok {
				return locationsForTargets(target), nil
			}
		}
	}
	return []protocol.Location{}, nil
}

func compositeTarget(
	path string,
	node syntax.NodeDecl,
	output string,
	decls definitionDecls,
	projects *ProjectCache,
) (definitionTarget, bool, error) {
	resolved, err := resolveImportAlias(path, node.Selector.Alias.Name, decls, projects)
	if err != nil || !resolved.found || !resolved.sourceOK {
		return definitionTarget{}, false, err
	}
	return findCompositeTarget(resolved.source, node.Kind, node.Selector.Export.Name, output)
}

func findCompositeTarget(
	src *resolve.Source,
	kind syntax.NodeKind,
	export string,
	output string,
) (definitionTarget, bool, error) {
	if src == nil || src.Path == "" {
		return definitionTarget{}, false, nil
	}
	matches, err := filepath.Glob(filepath.Join(src.Path, "*.ub"))
	if err != nil {
		return definitionTarget{}, false, err
	}
	for _, path := range matches {
		text, err := os.ReadFile(path)
		if err != nil {
			return definitionTarget{}, false, err
		}
		file, err := syntax.ParseSource(path, text)
		if err != nil || file.Library == nil {
			continue
		}
		for _, composite := range file.Library.Exports {
			if composite.Kind != kind || composite.Name.Name != export {
				continue
			}
			if output == "" {
				return definitionTarget{path: path, text: string(text), span: composite.Name.S}, true, nil
			}
			for _, out := range composite.Body.Outputs {
				if out.Name.Name == output {
					return definitionTarget{path: path, text: string(text), span: out.Name.S}, true, nil
				}
			}
		}
	}
	return definitionTarget{}, false, nil
}

func goImportAliasDefinition(
	path string,
	alias string,
	decls definitionDecls,
	projects *ProjectCache,
) ([]protocol.Location, bool, error) {
	resolved, err := resolveImportAlias(path, alias, decls, projects)
	if err != nil || !resolved.found {
		return nil, false, err
	}
	index, found, err := goIndexForResolved(resolved)
	if err != nil || !found {
		return []protocol.Location{}, found, err
	}
	return goLocationDefinition(index.LibraryFunc)
}

func goNodeSelectorDefinition(
	path string,
	node syntax.NodeDecl,
	decls definitionDecls,
	projects *ProjectCache,
) ([]protocol.Location, bool, error) {
	resolved, err := resolveImportAlias(path, node.Selector.Alias.Name, decls, projects)
	if err != nil || !resolved.found {
		return nil, false, err
	}
	index, found, err := goIndexForResolved(resolved)
	if err != nil || !found {
		return []protocol.Location{}, found, err
	}
	loc, ok := index.Registrations[string(node.Kind)][node.Selector.Export.Name]
	if !ok {
		return []protocol.Location{}, true, nil
	}
	return goLocationDefinition(loc)
}

func goNodeFieldDefinition(
	path string,
	node syntax.NodeDecl,
	fieldPath string,
	decls definitionDecls,
	projects *ProjectCache,
) ([]protocol.Location, bool, error) {
	resolved, err := resolveImportAlias(path, node.Selector.Alias.Name, decls, projects)
	if err != nil || !resolved.found {
		return nil, false, err
	}
	index, found, err := goIndexForResolved(resolved)
	if err != nil || !found {
		return []protocol.Location{}, found, err
	}
	fields := index.InputFields[string(node.Kind)][node.Selector.Export.Name]
	loc, ok := fields[fieldPath]
	if !ok {
		return []protocol.Location{}, true, nil
	}
	return goLocationDefinition(loc)
}

func goNodeRefFieldDefinition(
	path string,
	node syntax.NodeDecl,
	fieldPath string,
	decls definitionDecls,
	projects *ProjectCache,
) ([]protocol.Location, bool, error) {
	resolved, err := resolveImportAlias(path, node.Selector.Alias.Name, decls, projects)
	if err != nil || !resolved.found {
		return nil, false, err
	}
	index, found, err := goIndexForResolved(resolved)
	if err != nil || !found {
		return []protocol.Location{}, found, err
	}
	fields := index.OutputFields[string(node.Kind)][node.Selector.Export.Name]
	if loc, ok := fields[fieldPath]; ok {
		return goLocationDefinition(loc)
	}
	fields = index.InputFields[string(node.Kind)][node.Selector.Export.Name]
	if loc, ok := fields[fieldPath]; ok {
		return goLocationDefinition(loc)
	}
	return []protocol.Location{}, true, nil
}

func goConfigFieldDefinitionForPath(
	path string,
	libraryPath string,
	fieldPath string,
	decls definitionDecls,
	projects *ProjectCache,
) ([]protocol.Location, bool, error) {
	for alias, imp := range decls.imports {
		if imp.Ref == nil || imp.Ref.Value != libraryPath {
			continue
		}
		return goConfigFieldDefinition(path, alias, fieldPath, decls, projects)
	}
	return []protocol.Location{}, true, nil
}

func goConfigFieldDefinition(
	path string,
	alias string,
	fieldPath string,
	decls definitionDecls,
	projects *ProjectCache,
) ([]protocol.Location, bool, error) {
	resolved, err := resolveImportAlias(path, alias, decls, projects)
	if err != nil || !resolved.found {
		return nil, false, err
	}
	index, found, err := goIndexForResolved(resolved)
	if err != nil || !found {
		return []protocol.Location{}, found, err
	}
	loc, ok := index.ConfigFields[fieldPath]
	if !ok {
		return []protocol.Location{}, true, nil
	}
	return goLocationDefinition(loc)
}

func goFunctionDefinition(
	path string,
	alias string,
	name string,
	decls definitionDecls,
	projects *ProjectCache,
) ([]protocol.Location, bool, error) {
	resolved, err := resolveImportAlias(path, alias, decls, projects)
	if err != nil || !resolved.found {
		return nil, false, err
	}
	index, found, err := goIndexForResolved(resolved)
	if err != nil || !found {
		return []protocol.Location{}, found, err
	}
	loc, ok := index.Functions[name]
	if !ok {
		return []protocol.Location{}, true, nil
	}
	return goLocationDefinition(loc)
}

func resolveImportAlias(
	path string,
	alias string,
	decls definitionDecls,
	projects *ProjectCache,
) (resolvedImport, error) {
	imp, ok := decls.imports[alias]
	if !ok || imp.Ref == nil {
		return resolvedImport{}, nil
	}
	ref, err := resolve.ParseImportRef(imp.Ref.Value)
	if err != nil {
		return resolvedImport{}, err
	}
	project, err := projects.ProjectForPath(path)
	if err != nil {
		return resolvedImport{}, err
	}
	src, ok, err := project.Resolver.ResolveNoFetch(ref)
	if err != nil {
		return resolvedImport{}, err
	}
	return resolvedImport{project: project, source: src, found: true, sourceOK: ok}, nil
}

func goIndexForResolved(resolved resolvedImport) (*goschema.SourceIndex, bool, error) {
	if !resolved.sourceOK {
		return &goschema.SourceIndex{}, true, nil
	}
	if resolved.source == nil || resolved.source.Path == "" || !resolve.IsGoLibrary(resolved.source) {
		return nil, false, nil
	}
	_, index, _, err := resolved.project.GoIndex.Read(resolved.source.Path)
	if err != nil {
		return nil, true, err
	}
	return index, true, nil
}

func goLocationDefinition(
	loc goschema.GoLocation,
) ([]protocol.Location, bool, error) {
	if loc.Path == "" {
		return []protocol.Location{}, true, nil
	}
	text, err := os.ReadFile(loc.Path)
	if err != nil {
		return nil, true, err
	}
	source := string(text)
	end := goLocationEnd(source, loc.Offset)
	return []protocol.Location{{
		URI: PathToFileURI(loc.Path),
		Range: protocol.Range{
			Start: OffsetToLSP(source, loc.Offset),
			End:   OffsetToLSP(source, end),
		},
	}}, true, nil
}

func goLocationEnd(text string, start int) int {
	if start < 0 || start >= len(text) {
		return start
	}
	if text[start] == '"' {
		for i := start + 1; i < len(text); i++ {
			if text[i] == '"' && text[i-1] != '\\' {
				return i + 1
			}
		}
		return start
	}
	return symbolEnd(text, start)
}

func locationsForTargets(targets ...definitionTarget) []protocol.Location {
	locations := make([]protocol.Location, 0, len(targets))
	for _, target := range targets {
		locations = append(locations, protocol.Location{
			URI:   PathToFileURI(target.path),
			Range: rangeFromSpan(target.text, target.span),
		})
	}
	return locations
}

func allNodes(body syntax.FactoryBody) []syntax.NodeDecl {
	count := len(body.Resources) + len(body.Data) + len(body.Actions)
	nodes := make([]syntax.NodeDecl, 0, count)
	nodes = append(nodes, body.Resources...)
	nodes = append(nodes, body.Data...)
	nodes = append(nodes, body.Actions...)
	return nodes
}

func inputDefaultObject(body *parse.ObjectLit) *parse.ObjectLit {
	if body == nil {
		return nil
	}
	for _, field := range body.Fields {
		name, ok := fieldKeyName(field.Key)
		if !ok || name != "default" {
			continue
		}
		obj, ok := field.Value.(*parse.ObjectLit)
		if ok {
			return obj
		}
	}
	return nil
}

func objectKeyPathAtOffset(text string, obj *parse.ObjectLit, offset int) (string, bool) {
	return objectKeyPathAtOffsetPrefix(text, obj, offset, "")
}

func objectKeyPathAtOffsetPrefix(
	text string,
	obj *parse.ObjectLit,
	offset int,
	prefix string,
) (string, bool) {
	if obj == nil {
		return "", false
	}
	for _, field := range obj.Fields {
		name, ok := fieldKeyName(field.Key)
		if !ok {
			continue
		}
		path := prefix + name
		if fieldKeyContainsOffset(text, field.Key, offset) {
			return path, true
		}
		if child, ok := field.Value.(*parse.ObjectLit); ok {
			if found, ok := objectKeyPathAtOffsetPrefix(text, child, offset, path+"."); ok {
				return found, true
			}
		}
		if field.Decl != nil {
			if found, ok := objectKeyPathAtOffsetPrefix(
				text, field.Decl.Body, offset, path+".",
			); ok {
				return found, true
			}
		}
	}
	return "", false
}

func fieldKeyName(key parse.FieldKey) (string, bool) {
	switch key.Kind {
	case parse.FieldIdent:
		return key.Name, true
	case parse.FieldString:
		return key.String, true
	case parse.FieldPath:
		return strings.Join(key.Path, "."), true
	default:
		return "", false
	}
}

func fieldKeyContainsOffset(text string, key parse.FieldKey, offset int) bool {
	switch key.Kind {
	case parse.FieldIdent, parse.FieldPath:
		start := key.S.Start.Offset
		if start < 0 || start >= len(text) {
			return spanContainsOffset(key.S, offset)
		}
		return offset >= start && offset < symbolEnd(text, start)
	case parse.FieldString:
		return spanContainsOffset(key.S, offset)
	default:
		return false
	}
}

func spanContainsOffset(span parse.Span, offset int) bool {
	start := span.Start.Offset
	end := span.End.Offset
	if end <= start {
		return offset == start
	}
	return offset >= start && offset < end
}

func tokenAtOffset(text string, offset int) definitionToken {
	if offset < 0 || offset > len(text) {
		return definitionToken{}
	}
	if offset == len(text) && offset > 0 {
		offset--
	}
	start := offset
	for start > 0 && isSymbolByte(text[start-1]) {
		start--
	}
	end := offset
	for end < len(text) && isSymbolByte(text[end]) {
		end++
	}
	if start == end {
		return definitionToken{}
	}
	return definitionToken{text: text[start:end], start: start, end: end}
}
