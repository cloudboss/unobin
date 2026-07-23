package lsp

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cloudboss/unobin/pkg/asset"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/lsp/protocol"
)

var assetAttributeNames = []string{"content", "content-sha256", "mode", "path"}

func assetPathAtOffset(
	text string,
	body *syntax.FactoryBody,
	offset int,
) *parse.DotPath {
	var found *parse.DotPath
	walkFactoryBodyExpressions(body, func(expr parse.Expr) {
		path, ok := expr.(*parse.DotPath)
		if !ok || path.Root == nil || path.Root.Name != "asset" ||
			!assetPathContainsOffset(text, path, offset) {
			return
		}
		found = path
	})
	return found
}

func assetPathContainsOffset(
	text string,
	path *parse.DotPath,
	offset int,
) bool {
	if path == nil {
		return false
	}
	start := path.S.Start.Offset
	if start < 0 || start >= len(text) {
		return false
	}
	end := start
	for end < len(text) && isSymbolByte(text[end]) {
		end++
	}
	for end < len(text) && text[end] == '[' {
		end++
		for end < len(text) && isSpaceByte(text[end]) {
			end++
		}
		if end < len(text) && text[end] == '\'' {
			end = singleQuotedLiteralEnd(text, end)
		} else {
			for end < len(text) && text[end] != ']' {
				end++
			}
		}
		for end < len(text) && isSpaceByte(text[end]) {
			end++
		}
		if end < len(text) && text[end] == ']' {
			end++
		}
		for end < len(text) && isSymbolByte(text[end]) {
			end++
		}
	}
	return offset >= start && offset <= end
}

func walkFactoryBodyExpressions(body *syntax.FactoryBody, visit func(parse.Expr)) {
	if body == nil {
		return
	}
	walk := func(expr parse.Expr) {
		lang.Walk(expr, visit)
	}
	for _, input := range body.Inputs {
		walk(input.Body)
	}
	for _, local := range body.Locals {
		walk(local.Value)
	}
	for _, constraint := range body.Constraints {
		walk(constraint.Value)
	}
	for _, config := range body.LibraryConfigs {
		walk(config.Value)
	}
	for _, node := range allNodes(*body) {
		walk(node.Body)
	}
	for _, output := range body.Outputs {
		walk(output.Body)
	}
}

func assetCompletionAtOffset(
	path string,
	text string,
	offset int,
	body *syntax.FactoryBody,
	decls definitionDecls,
	projects *ProjectCache,
) (protocol.CompletionList, bool) {
	reference := assetPathAtOffset(text, body, offset)
	if reference == nil || len(reference.Segments) == 0 {
		return protocol.CompletionList{}, false
	}
	segments := reference.Segments
	if countAssetSelections(segments) > 1 {
		return completionList(nil), true
	}
	nameSegment := segments[0]
	if nameSegment.Name == "" {
		return completionList(nil), true
	}
	if len(segments) == 1 {
		items := namedCompletionItems(
			mapKeys(decls.assets),
			protocol.CompletionItemKindVariable,
		)
		return completionList(items), true
	}
	if segments[1].Index != nil {
		if assetIndexContainsOffset(text, segments[1].Index, offset) {
			items := assetInternalPathCompletionItems(
				path,
				text,
				body,
				nameSegment.Name,
				segments[1].Index,
				projects,
			)
			return completionList(items), true
		}
		if len(segments) == 2 {
			return protocol.CompletionList{}, false
		}
		if segments[2].Name != "" {
			return completionList(assetAttributeCompletionItems()), true
		}
		return completionList(nil), true
	}
	if segments[1].Name != "" {
		return completionList(assetAttributeCompletionItems()), true
	}
	return completionList(nil), true
}

func assetIndexContainsOffset(text string, index parse.Expr, offset int) bool {
	if index == nil {
		return false
	}
	start := index.Span().Start.Offset
	if start < 0 || start >= len(text) {
		return false
	}
	end := symbolEnd(text, start)
	if text[start] == '\'' {
		end = singleQuotedLiteralEnd(text, start)
	}
	return offset >= start && offset < end
}

func countAssetSelections(segments []parse.DotSegment) int {
	count := 0
	for _, segment := range segments {
		if segment.Index != nil {
			count++
		}
	}
	return count
}

func assetAttributeCompletionItems() []protocol.CompletionItem {
	return namedCompletionItems(assetAttributeNames, protocol.CompletionItemKindField)
}

func assetInternalPathCompletionItems(
	path string,
	text string,
	body *syntax.FactoryBody,
	name string,
	index parse.Expr,
	projects *ProjectCache,
) []protocol.CompletionItem {
	literal, ok := index.(*parse.StringLit)
	if !ok {
		return nil
	}
	set := editorAssetSet(path, body, projects)
	item, ok := set.Asset(name)
	if !ok {
		return nil
	}
	root, ok := item.Entry("")
	if !ok || root.Kind != asset.EntryKindDirectory {
		return nil
	}
	replacement := assetStringLiteralRange(text, literal)
	items := make([]protocol.CompletionItem, 0, len(item.Entries())-1)
	for _, entry := range item.Entries() {
		if entry.InternalPath == "" {
			continue
		}
		quoted := "'" + strings.ReplaceAll(entry.InternalPath, "'", `\'`) + "'"
		items = append(items, protocol.CompletionItem{
			Label: entry.InternalPath,
			Kind:  protocol.CompletionItemKindField,
			TextEdit: &protocol.TextEdit{
				Range:   replacement,
				NewText: quoted,
			},
		})
	}
	return items
}

func assetStringLiteralRange(
	text string,
	literal *parse.StringLit,
) protocol.Range {
	if literal == nil {
		return protocol.Range{}
	}
	start := literal.S.Start.Offset
	end := start
	if start >= 0 && start < len(text) && text[start] == '\'' {
		end = singleQuotedLiteralEnd(text, start)
	}
	return protocol.Range{
		Start: OffsetToLSP(text, start),
		End:   OffsetToLSP(text, end),
	}
}

func editorAssetSet(
	path string,
	body *syntax.FactoryBody,
	projects *ProjectCache,
) *asset.Set {
	if body == nil || len(body.Assets) == 0 {
		return nil
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	source := diagnosticSource(path)
	projectDir := filepath.Dir(path)
	if projects != nil {
		project, err := projects.ProjectForPath(path)
		if err == nil {
			projectDir = project.Root
			source = diagnosticProjectSource(path, project.Root)
		}
	}
	key := editorAssetCacheKey(path, body)
	if set := projects.cachedAssetSet(key); set != nil {
		return set
	}
	captured, err := asset.Capture(
		source,
		diagnosticProjectSourceFile(path, projectDir),
		body.Assets,
		"",
	)
	if err != nil {
		return nil
	}
	var collection asset.Collection
	if err := collection.Add(captured); err != nil {
		return nil
	}
	set, _ := collection.Catalog().Set(captured.ID)
	projects.cacheAssetSet(key, set)
	return set
}

func editorAssetCacheKey(path string, body *syntax.FactoryBody) string {
	var key strings.Builder
	key.WriteString(filepath.Clean(path))
	for _, declaration := range body.Assets {
		key.WriteByte(0)
		key.WriteString(declaration.Name.Name)
		key.WriteByte(0)
		if declaration.Source != nil {
			key.WriteString(declaration.Source.Value)
		}
	}
	key.WriteByte(0)
	key.WriteString(strconv.Itoa(body.S.Start.Offset))
	key.WriteByte(':')
	key.WriteString(strconv.Itoa(body.S.End.Offset))
	return key.String()
}

func assetHover(
	path string,
	body *syntax.FactoryBody,
	reference *parse.DotPath,
	projects *ProjectCache,
) *protocol.Hover {
	name, internalPath, display, ok := assetReferenceTarget(reference)
	if !ok {
		return nil
	}
	set := editorAssetSet(path, body, projects)
	item, ok := set.Asset(name)
	if !ok {
		return nil
	}
	entry, ok := item.Entry(internalPath)
	if !ok {
		return nil
	}
	return plainHover(fmt.Sprintf(
		"%s\nkind: %s\nmode: %s\ncontent-sha256: %s",
		display,
		entry.Kind,
		entry.Mode,
		entry.ContentSHA256,
	))
}

func assetReferenceTarget(
	reference *parse.DotPath,
) (name string, internalPath string, display string, ok bool) {
	if reference == nil || len(reference.Segments) == 0 {
		return "", "", "", false
	}
	segments := reference.Segments
	name = segments[0].Name
	if name == "" {
		return "", "", "", false
	}
	display = "asset." + name
	if len(segments) < 2 || segments[1].Index == nil {
		return name, "", display, true
	}
	literal, ok := segments[1].Index.(*parse.StringLit)
	if !ok || countAssetSelections(segments) != 1 {
		return "", "", "", false
	}
	internalPath = literal.Value
	display += "['" + strings.ReplaceAll(internalPath, "'", `\'`) + "']"
	return name, internalPath, display, true
}
