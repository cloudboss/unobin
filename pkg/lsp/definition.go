package lsp

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/lsp/protocol"
	"github.com/cloudboss/unobin/pkg/resolve"
)

// DefinitionForText resolves a UB-only go-to-definition request.
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
	tok := tokenAtOffset(text, offset)
	if tok.text == "" {
		return []protocol.Location{}, nil
	}
	if projects == nil {
		projects = NewProjectCache("")
	}
	decls := definitionDeclsForFile(file)
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
			return selectorDefinition(path, parts[0], parts[1], decls, projects)
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
		if target, ok, err := compositeTarget(path, node, parts[2], decls, projects); ok || err != nil {
			if err != nil {
				return nil, err
			}
			return locationsForTargets(target), nil
		}
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
	imp, ok := decls.imports[node.Selector.Alias.Name]
	if !ok || imp.Ref == nil {
		return definitionTarget{}, false, nil
	}
	ref, err := resolve.ParseImportRef(imp.Ref.Value)
	if err != nil {
		return definitionTarget{}, false, err
	}
	project, err := projects.ProjectForPath(path)
	if err != nil {
		return definitionTarget{}, false, err
	}
	src, ok, err := project.Resolver.ResolveNoFetch(ref)
	if err != nil || !ok {
		return definitionTarget{}, false, err
	}
	return findCompositeTarget(src, node.Kind, node.Selector.Export.Name, output)
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
