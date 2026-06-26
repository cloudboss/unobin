package lsp

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/check"
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
	if list, ok := inputDeclarationSourceCompletions(text, offset); ok {
		return list, nil
	}
	file, err := parseCompletionSource(path, text, offset)
	if err != nil {
		return completionList([]protocol.CompletionItem{}), nil
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
	list, err := completionForToken(path, text, offset, tok.text, body, decls, projects)
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
	if inputDeclarationTypeValueCompletionContext(text, offset) ||
		typeValueCompletionContext(prefix) {
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
	if inputDeclarationLineStartCompletionContext(text, offset) {
		return completionList(inputDeclarationCompletionItems(text, offset)), true
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

func inputDeclarationLineStartCompletionContext(text string, offset int) bool {
	if typeValueCompletionContext(currentLinePrefix(text, offset)) ||
		!insideInputDeclarationObject(text, offset) ||
		strings.TrimSpace(currentLinePrefix(text, offset)) != "" {
		return false
	}
	candidate := strings.TrimSpace(currentLineSuffix(text, offset))
	if candidate == "" {
		return false
	}
	for _, key := range []string{"type", "description", "default", "@sensitive"} {
		if strings.HasPrefix(candidate, key+":") {
			return true
		}
	}
	return false
}

func inputDeclarationSourceCompletions(
	text string,
	offset int,
) (protocol.CompletionList, bool) {
	if inputDeclarationTypeValueCompletionContext(text, offset) {
		return completionList(typeCompletionItems()), true
	}
	if !inputDeclarationCompletionContext(text, offset) {
		return protocol.CompletionList{}, false
	}
	items := inputDeclarationSourceCompletionItems(text, offset)
	return completionList(items), true
}

func inputDeclarationSourceCompletionItems(
	text string,
	offset int,
) []protocol.CompletionItem {
	items := namedFieldCompletionItems(
		[]string{"type", "description", "default", "@sensitive"},
		completionObjectFieldKeyPrefix(text, offset),
		inputDeclarationPresentKeys(text, offset),
	)
	return withMetaKeyTextEdits(text, offset, items)
}

func inputDeclarationCompletionContext(text string, offset int) bool {
	if !insideInputDeclarationObject(text, offset) {
		return false
	}
	_, afterColon, valueStarted, ok := inputDeclarationFieldAtOffset(text, offset)
	if !ok || (afterColon && !valueStarted) {
		return false
	}
	return completionObjectCandidateMatches(
		text, offset, []string{"type", "description", "default", "@sensitive"},
	)
}

func inputDeclarationTypeValueCompletionContext(text string, offset int) bool {
	if !insideInputDeclarationObject(text, offset) {
		return false
	}
	name, afterColon, valueStarted, ok := inputDeclarationFieldAtOffset(text, offset)
	return ok && afterColon && !valueStarted && name == "type"
}

func completionObjectCandidateMatches(text string, offset int, keys []string) bool {
	candidate := completionObjectFieldKeyPrefix(text, offset)
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

func completionObjectFieldKeyPrefix(text string, offset int) string {
	name, afterColon, _, ok := inputDeclarationFieldAtOffset(text, offset)
	if !ok || afterColon {
		return ""
	}
	return name
}

func insideInputDeclarationObject(text string, offset int) bool {
	if !insideNamedBlock(text, offset, "inputs") {
		return false
	}
	open := nearestOpenObject(text, offset)
	if open < 0 {
		return false
	}
	key, ok := objectFieldKeyBeforeOpen(text, open)
	if !ok {
		return false
	}
	switch key {
	case "inputs", "type", "description", "default", "@sensitive":
		return false
	default:
		return true
	}
}

func nearestOpenObject(text string, offset int) int {
	depth := make([]int, 0)
	for i, r := range text[:offset] {
		switch r {
		case '{':
			depth = append(depth, i)
		case '}':
			if len(depth) > 0 {
				depth = depth[:len(depth)-1]
			}
		}
	}
	if len(depth) == 0 {
		return -1
	}
	return depth[len(depth)-1]
}

func objectFieldKeyBeforeOpen(text string, open int) (string, bool) {
	i := open - 1
	for i >= 0 && isSpaceByte(text[i]) {
		i--
	}
	if i < 0 || text[i] != ':' {
		return "", false
	}
	i--
	for i >= 0 && isSpaceByte(text[i]) {
		i--
	}
	end := i + 1
	for i >= 0 && isSymbolByte(text[i]) {
		i--
	}
	start := i + 1
	if start >= end {
		return "", false
	}
	return text[start:end], true
}

func inputDeclarationFieldAtOffset(text string, offset int) (string, bool, bool, bool) {
	open := nearestOpenObject(text, offset)
	if open < 0 {
		return "", false, false, false
	}
	name := ""
	afterColon := false
	valueStarted := false
	for i := open + 1; i < offset && i < len(text); {
		switch {
		case isSpaceByte(text[i]):
			i++
			continue
		case text[i] == '#':
			i = skipLineComment(text, i)
			continue
		case text[i] == '\'':
			if afterColon {
				valueStarted = true
			}
			i = skipSingleQuotedString(text, i)
			continue
		case text[i] == '{':
			if afterColon {
				valueStarted = true
			}
			i = matchingObjectClose(text, i) + 1
			continue
		case !isSymbolByte(text[i]):
			if afterColon {
				valueStarted = true
			}
			i++
			continue
		}
		start := i
		for i < offset && i < len(text) && isSymbolByte(text[i]) {
			i++
		}
		candidate := text[start:i]
		j := i
		for j < offset && j < len(text) && isSpaceByte(text[j]) {
			j++
		}
		if j < offset && j < len(text) && text[j] == ':' {
			name = candidate
			afterColon = true
			valueStarted = false
			i = j + 1
			continue
		}
		if i >= offset {
			if afterColon && !valueStarted {
				return name, true, true, true
			}
			return candidate, false, false, true
		}
		if afterColon {
			valueStarted = true
		}
	}
	if name == "" {
		return "", false, false, false
	}
	return name, afterColon, valueStarted, true
}

func inputDeclarationPresentKeys(text string, offset int) map[string]bool {
	present := map[string]bool{}
	open := nearestOpenObject(text, offset)
	if open < 0 {
		return present
	}
	close := matchingObjectClose(text, open)
	for i := open + 1; i < close; {
		switch {
		case isSpaceByte(text[i]):
			i++
			continue
		case text[i] == '#':
			i = skipLineComment(text, i)
			continue
		case text[i] == '\'':
			i = skipSingleQuotedString(text, i)
			continue
		case text[i] == '{':
			i = matchingObjectClose(text, i) + 1
			continue
		case !isSymbolByte(text[i]):
			i++
			continue
		}
		start := i
		for i < close && isSymbolByte(text[i]) {
			i++
		}
		name := text[start:i]
		j := i
		for j < close && isSpaceByte(text[j]) {
			j++
		}
		if j < close && text[j] == ':' {
			present[name] = true
		}
	}
	return present
}

func matchingObjectClose(text string, open int) int {
	depth := 0
	for i := open; i < len(text); i++ {
		switch text[i] {
		case '#':
			i = skipLineComment(text, i) - 1
		case '\'':
			i = skipSingleQuotedString(text, i) - 1
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(text)
}

func skipLineComment(text string, offset int) int {
	for offset < len(text) && text[offset] != '\n' {
		offset++
	}
	return offset
}

func skipSingleQuotedString(text string, offset int) int {
	offset++
	for offset < len(text) {
		if text[offset] == '\\' {
			offset += 2
			continue
		}
		if text[offset] == '\'' {
			return offset + 1
		}
		offset++
	}
	return offset
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
	candidate := completionCandidatePrefix(text, offset)
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

func completionCandidatePrefix(text string, offset int) string {
	line := strings.TrimSpace(currentLinePrefix(text, offset))
	if line != "" {
		return line
	}
	return strings.TrimSpace(currentLineSuffix(text, offset))
}

func completionFieldKeyPrefix(text string, offset int) string {
	candidate := strings.TrimSpace(currentLinePrefix(text, offset))
	before, _, ok := strings.Cut(candidate, ":")
	if ok {
		return strings.TrimSpace(before)
	}
	return candidate
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
	for _, insertion := range []string{"complete", ": null", "complete: null"} {
		repaired := text[:offset] + insertion + text[offset:]
		file, err := syntax.ParseSource(path, []byte(repaired))
		if err == nil {
			return file, nil
		}
	}
	return nil, err
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
	if list, found := inputDeclarationValueCompletions(offset, body.Inputs); found {
		return list, true, nil
	}
	if list, found := inputDeclarationFieldCompletions(text, offset, body.Inputs); found {
		return list, true, nil
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
	for _, output := range body.Outputs {
		if list, found := outputValueCompletions(offset, output.Body); found {
			return list, true, nil
		}
		fieldPath, ok := objectKeyPathAtOffset(text, output.Body, offset)
		if ok {
			list, found := outputFieldCompletions(text, offset, output.Body, fieldPath)
			if found {
				return list, true, nil
			}
		}
		if objectBodyKeyCompletionContext(text, output.Body, offset) {
			fieldPrefix := strings.TrimSpace(currentObjectEntryPrefix(text, offset))
			list, found := outputFieldCompletions(text, offset, output.Body, fieldPrefix)
			if found {
				return list, true, nil
			}
		}
	}
	for _, node := range allNodes(*body) {
		if tokenAtOffset(text, offset).text == "" {
			valuePath, ok := objectValuePathAtOffset(node.Body, offset)
			if ok {
				return nodeValueCompletions(path, body, node, valuePath, decls, projects)
			}
		}
		fieldPath, ok := objectKeyPathAtOffset(text, node.Body, offset)
		if ok {
			return nodeFieldCompletions(path, text, offset, node, fieldPath, decls, projects)
		}
		if objectBodyKeyCompletionContext(text, node.Body, offset) {
			fieldPrefix := strings.TrimSpace(currentObjectEntryPrefix(text, offset))
			return nodeFieldCompletions(path, text, offset, node, fieldPrefix, decls, projects)
		}
	}
	return protocol.CompletionList{}, false, nil
}

func inputDeclarationValueCompletions(
	offset int,
	inputs []syntax.InputDecl,
) (protocol.CompletionList, bool) {
	for _, input := range inputs {
		fieldPath, ok := objectValuePathAtOffset(input.Body, offset)
		if !ok || fieldParentPath(fieldPath) != "" {
			continue
		}
		target, ok := inputDeclarationValueType(input, fieldPath)
		if !ok {
			return protocol.CompletionList{}, false
		}
		return completionList(staticValueCompletionItems(target)), true
	}
	return protocol.CompletionList{}, false
}

func inputDeclarationValueType(
	input syntax.InputDecl,
	fieldPath string,
) (typecheck.Type, bool) {
	switch fieldPath {
	case "@sensitive":
		return typecheck.TBoolean(), true
	case "default":
		return typecheck.FromLang(input.Type), true
	case "description":
		return typecheck.TString(), true
	default:
		return typecheck.Type{}, false
	}
}

func inputDeclarationFieldCompletions(
	text string,
	offset int,
	inputs []syntax.InputDecl,
) (protocol.CompletionList, bool) {
	for _, input := range inputs {
		if input.Body == nil || !spanContainsOffset(input.Body.S, offset) {
			continue
		}
		if fieldPrefix, ok := objectEntryPrefixAfterValue(text, input.Body, offset); ok {
			items := inputDeclarationFieldCompletionItems(
				text, offset, fieldPrefix, input.Body,
			)
			return completionList(items), true
		}
		fieldPath, ok := objectKeyPathAtOffset(text, input.Body, offset)
		if ok {
			if fieldParentPath(fieldPath) != "" {
				continue
			}
			items := inputDeclarationFieldCompletionItems(
				text, offset, fieldPath, input.Body,
			)
			return completionList(items), true
		}
		if !immediateObjectBodyKeyCompletionContext(text, input.Body, offset) {
			continue
		}
		fieldPrefix := strings.TrimSpace(currentObjectEntryPrefix(text, offset))
		items := inputDeclarationFieldCompletionItems(
			text, offset, fieldPrefix, input.Body,
		)
		return completionList(items), true
	}
	return protocol.CompletionList{}, false
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

func objectValuePathAtOffset(obj *parse.ObjectLit, offset int) (string, bool) {
	return objectValuePathAtOffsetPrefix(obj, offset, "")
}

func objectValuePathAtOffsetPrefix(
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
		if child, ok := field.Value.(*parse.ObjectLit); ok {
			if found, ok := objectValuePathAtOffsetPrefix(child, offset, path+"."); ok {
				return found, true
			}
		}
		if field.Decl != nil {
			if found, ok := objectValuePathAtOffsetPrefix(
				field.Decl.Body, offset, path+".",
			); ok {
				return found, true
			}
		}
		if fieldValueContainsOffset(field, offset) {
			return path, true
		}
	}
	return "", false
}

func fieldValueContainsOffset(field *parse.Field, offset int) bool {
	if field == nil || field.Value == nil {
		return false
	}
	span := field.Value.Span()
	start := span.Start.Offset
	end := span.End.Offset
	if end <= start {
		return offset == start
	}
	return offset >= start && offset <= end
}

func objectBodyKeyCompletionContext(text string, obj *parse.ObjectLit, offset int) bool {
	if obj == nil || !spanContainsOffset(obj.Span(), offset) {
		return false
	}
	prefix := strings.TrimSpace(currentObjectEntryPrefix(text, offset))
	if strings.Contains(prefix, ":") {
		return false
	}
	if prefix != "" {
		return true
	}
	suffix := strings.TrimSpace(currentLineSuffix(text, offset))
	return suffix == "" || strings.HasPrefix(suffix, "}")
}

func objectEntryPrefixAfterValue(
	text string,
	obj *parse.ObjectLit,
	offset int,
) (string, bool) {
	if obj == nil || !spanContainsOffset(obj.Span(), offset) ||
		objectChildContainsOffset(obj, offset) {
		return "", false
	}
	for i, field := range obj.Fields {
		if field.Value == nil {
			continue
		}
		end, ok := objectFieldValueEnd(text, obj, i)
		if !ok || end > offset {
			continue
		}
		between := text[end:offset]
		if strings.ContainsAny(between, "{}") || strings.Contains(between, ":") {
			continue
		}
		prefix := strings.TrimSpace(between)
		if strings.ContainsAny(prefix, " \t\r\n") {
			continue
		}
		return prefix, true
	}
	return "", false
}

func objectFieldValueEnd(text string, obj *parse.ObjectLit, index int) (int, bool) {
	if obj == nil || index < 0 || index >= len(obj.Fields) {
		return 0, false
	}
	field := obj.Fields[index]
	if field.Value == nil {
		return 0, false
	}
	start := field.Value.Span().Start.Offset
	if start < 0 || start > len(text) {
		return 0, false
	}
	end := field.Value.Span().End.Offset
	if end <= start || end > len(text) {
		end = obj.S.End.Offset - 1
		if index+1 < len(obj.Fields) {
			next := obj.Fields[index+1].S.Start.Offset
			if next > start && next <= len(text) {
				end = next
			}
		}
		if end < start || end > len(text) {
			return 0, false
		}
		for end > start && isSpaceByte(text[end-1]) {
			end--
		}
	}
	return end, true
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}

func immediateObjectBodyKeyCompletionContext(
	text string,
	obj *parse.ObjectLit,
	offset int,
) bool {
	if !objectBodyKeyCompletionContext(text, obj, offset) {
		return false
	}
	return !objectChildContainsOffset(obj, offset)
}

func objectChildContainsOffset(obj *parse.ObjectLit, offset int) bool {
	for _, field := range obj.Fields {
		if child, ok := field.Value.(*parse.ObjectLit); ok && spanContainsOffset(child.S, offset) {
			return true
		}
		if field.Decl != nil && field.Decl.Body != nil &&
			spanContainsOffset(field.Decl.Body.S, offset) {
			return true
		}
	}
	return false
}

func currentObjectEntryPrefix(text string, offset int) string {
	start := strings.LastIndex(text[:offset], "\n") + 1
	for _, delimiter := range []string{"{", "}"} {
		idx := strings.LastIndex(text[:offset], delimiter)
		if idx >= start {
			start = idx + 1
		}
	}
	return text[start:offset]
}

func completionForToken(
	path string,
	text string,
	offset int,
	token string,
	body *syntax.FactoryBody,
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
		selectorKind := selectorKindAtOffset(body, offset)
		if list, found, err := goSelectorCompletions(
			path, parts[0], selectorKind, decls, projects,
		); found || err != nil {
			return list, err
		}
		if list, found, err := compositeSelectorCompletions(
			path, parts[0], selectorKind, decls, projects,
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

func inputDeclarationCompletionItems(text string, offset int) []protocol.CompletionItem {
	return inputDeclarationFieldCompletionItems(
		text, offset, completionFieldKeyPrefix(text, offset), nil,
	)
}

func inputDeclarationFieldCompletionItems(
	text string,
	offset int,
	fieldPath string,
	obj *parse.ObjectLit,
) []protocol.CompletionItem {
	items := fieldKeyCompletionItems(
		[]string{"type", "description", "default", "@sensitive"}, fieldPath, obj,
	)
	return withMetaKeyTextEdits(text, offset, items)
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

func compositeSelectorCompletions(
	path string,
	alias string,
	selectorKind syntax.NodeKind,
	decls definitionDecls,
	projects *ProjectCache,
) (protocol.CompletionList, bool, error) {
	resolved, err := resolveImportAlias(path, alias, decls, projects)
	if err != nil || !resolved.found || !resolved.sourceOK {
		return protocol.CompletionList{}, false, err
	}
	names, err := compositeExportNames(resolved.source, selectorKind)
	if err != nil || len(names) == 0 {
		return protocol.CompletionList{}, len(names) > 0, err
	}
	return completionList(namedCompletionItems(names, protocol.CompletionItemKindFunction)), true, nil
}

func compositeExportNames(src *resolve.Source, kind syntax.NodeKind) ([]string, error) {
	if src == nil || src.Path == "" || kind == "" {
		return nil, nil
	}
	matches, err := filepath.Glob(filepath.Join(src.Path, "*.ub"))
	if err != nil {
		return nil, err
	}
	names := make([]string, 0)
	for _, path := range matches {
		text, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		file, err := syntax.ParseSource(path, text)
		if err != nil || file.Library == nil {
			continue
		}
		for _, composite := range file.Library.Exports {
			if composite.Kind == kind {
				names = append(names, composite.Name.Name)
			}
		}
	}
	return names, nil
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
		return mapKeys(schema.Functions)
	}
}

func selectorKindAtOffset(body *syntax.FactoryBody, offset int) syntax.NodeKind {
	if body == nil {
		return ""
	}
	for _, node := range allNodes(*body) {
		if spanContainsOffset(node.Selector.S, offset) {
			return node.Kind
		}
	}
	return ""
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

func nodeValueCompletions(
	path string,
	body *syntax.FactoryBody,
	node syntax.NodeDecl,
	fieldPath string,
	decls definitionDecls,
	projects *ProjectCache,
) (protocol.CompletionList, bool, error) {
	if list, found, err := goNodeValueCompletions(
		path, body, node, fieldPath, decls, projects,
	); found || err != nil {
		return list, found, err
	}
	if list, found, err := compositeNodeValueCompletions(
		path, body, node, fieldPath, decls, projects,
	); found || err != nil {
		return list, found, err
	}
	return completionList([]protocol.CompletionItem{}), true, nil
}

func nodeFieldCompletions(
	path string,
	text string,
	offset int,
	node syntax.NodeDecl,
	fieldPath string,
	decls definitionDecls,
	projects *ProjectCache,
) (protocol.CompletionList, bool, error) {
	metaItems := nodeMetaCompletionItems(text, offset, node.Kind, fieldPath, node.Body)
	if list, found, err := goNodeFieldCompletions(
		path, node, fieldPath, decls, projects,
	); found || err != nil {
		if err != nil {
			return list, found, err
		}
		return completionList(combineCompletionItems(list.Items, metaItems)), found, nil
	}
	if list, found, err := compositeNodeFieldCompletions(
		path, node, fieldPath, decls, projects,
	); found || err != nil {
		if err != nil {
			return list, found, err
		}
		return completionList(combineCompletionItems(list.Items, metaItems)), found, nil
	}
	if len(metaItems) > 0 {
		return completionList(metaItems), true, nil
	}
	return completionList([]protocol.CompletionItem{}), true, nil
}

func goNodeValueCompletions(
	path string,
	body *syntax.FactoryBody,
	node syntax.NodeDecl,
	fieldPath string,
	decls definitionDecls,
	projects *ProjectCache,
) (protocol.CompletionList, bool, error) {
	typeSchema, found, err := goNodeSchema(path, node, decls, projects)
	if err != nil || !found || typeSchema == nil {
		return protocol.CompletionList{}, found, err
	}
	target, ok := typeForFieldPath(typeSchema.Inputs, fieldPath)
	if !ok || !target.IsKnown() {
		return completionList([]protocol.CompletionItem{}), true, nil
	}
	items, err := valueCompletionItems(path, body, target, decls, projects)
	if err != nil {
		return protocol.CompletionList{}, true, err
	}
	return completionList(items), true, nil
}

func compositeNodeValueCompletions(
	path string,
	body *syntax.FactoryBody,
	node syntax.NodeDecl,
	fieldPath string,
	decls definitionDecls,
	projects *ProjectCache,
) (protocol.CompletionList, bool, error) {
	composite, found, err := compositeForNode(path, node, decls, projects)
	if err != nil || !found {
		return protocol.CompletionList{}, found, err
	}
	target, ok := typeForFieldPath(compositeInputTypes(composite.Body.Inputs), fieldPath)
	if !ok || !target.IsKnown() {
		return completionList([]protocol.CompletionItem{}), true, nil
	}
	items, err := valueCompletionItems(path, body, target, decls, projects)
	if err != nil {
		return protocol.CompletionList{}, true, err
	}
	return completionList(items), true, nil
}

func compositeNodeFieldCompletions(
	path string,
	node syntax.NodeDecl,
	fieldPath string,
	decls definitionDecls,
	projects *ProjectCache,
) (protocol.CompletionList, bool, error) {
	composite, found, err := compositeForNode(path, node, decls, projects)
	if err != nil || !found {
		return protocol.CompletionList{}, found, err
	}
	return completionList(fieldCompletionItems(
		compositeInputTypes(composite.Body.Inputs), fieldPath, node.Body,
	)), true, nil
}

func compositeForNode(
	path string,
	node syntax.NodeDecl,
	decls definitionDecls,
	projects *ProjectCache,
) (*syntax.CompositeDecl, bool, error) {
	resolved, err := resolveImportAlias(path, node.Selector.Alias.Name, decls, projects)
	if err != nil || !resolved.found || !resolved.sourceOK {
		return nil, false, err
	}
	return findCompositeDecl(resolved.source, node.Kind, node.Selector.Export.Name)
}

func findCompositeDecl(
	src *resolve.Source,
	kind syntax.NodeKind,
	export string,
) (*syntax.CompositeDecl, bool, error) {
	if src == nil || src.Path == "" {
		return nil, false, nil
	}
	matches, err := filepath.Glob(filepath.Join(src.Path, "*.ub"))
	if err != nil {
		return nil, false, err
	}
	for _, path := range matches {
		text, err := os.ReadFile(path)
		if err != nil {
			return nil, false, err
		}
		file, err := syntax.ParseSource(path, text)
		if err != nil || file.Library == nil {
			continue
		}
		for _, composite := range file.Library.Exports {
			if composite.Kind == kind && composite.Name.Name == export {
				return &composite, true, nil
			}
		}
	}
	return nil, false, nil
}

func compositeInputTypes(inputs []syntax.InputDecl) map[string]typecheck.Type {
	out := make(map[string]typecheck.Type, len(inputs))
	for _, input := range inputs {
		out[input.Name.Name] = typecheck.FromLang(input.Type)
	}
	return out
}

func staticValueCompletionItems(target typecheck.Type) []protocol.CompletionItem {
	items := scalarValueCompletionItems(target)
	slices.SortFunc(items, func(a, b protocol.CompletionItem) int {
		return strings.Compare(a.Label, b.Label)
	})
	return items
}

func scalarValueCompletionItems(target typecheck.Type) []protocol.CompletionItem {
	items := make([]protocol.CompletionItem, 0)
	if typecheck.Assignable(target, typecheck.TNull()) {
		items = append(items, protocol.CompletionItem{
			Label: "null",
			Kind:  protocol.CompletionItemKindKeyword,
		})
	}
	if typecheck.Assignable(target, typecheck.TBoolean()) {
		items = append(items,
			protocol.CompletionItem{Label: "false", Kind: protocol.CompletionItemKindKeyword},
			protocol.CompletionItem{Label: "true", Kind: protocol.CompletionItemKindKeyword},
		)
	}
	return items
}

func valueCompletionItems(
	path string,
	body *syntax.FactoryBody,
	target typecheck.Type,
	decls definitionDecls,
	projects *ProjectCache,
) ([]protocol.CompletionItem, error) {
	items := scalarValueCompletionItems(target)
	for _, input := range body.Inputs {
		candidate := typecheck.FromLang(input.Type)
		if completionTypeAssignable(target, candidate) {
			items = append(items, protocol.CompletionItem{
				Label: "input." + input.Name.Name,
				Kind:  protocol.CompletionItemKindVariable,
			})
		}
	}
	locals, err := localCompletionTypes(path, body, projects)
	if err != nil {
		return nil, err
	}
	names := mapKeys(decls.locals)
	slices.Sort(names)
	for _, name := range names {
		candidate, ok := locals[name]
		if ok && completionTypeAssignable(target, candidate) {
			items = append(items, protocol.CompletionItem{
				Label: "local." + name,
				Kind:  protocol.CompletionItemKindVariable,
			})
		}
	}
	slices.SortFunc(items, func(a, b protocol.CompletionItem) int {
		return strings.Compare(a.Label, b.Label)
	})
	return items, nil
}

func completionTypeAssignable(target typecheck.Type, candidate typecheck.Type) bool {
	if !target.IsKnown() || !candidate.IsKnown() {
		return false
	}
	return typecheck.Assignable(target, candidate)
}

func localCompletionTypes(
	path string,
	body *syntax.FactoryBody,
	projects *ProjectCache,
) (map[string]typecheck.Type, error) {
	out := map[string]typecheck.Type{}
	if body == nil || len(body.Locals) == 0 {
		return out, nil
	}
	libs, err := diagnosticLibraries(path, *body, projects)
	if err != nil {
		return nil, err
	}
	_ = check.NewSyntax(*body, libs).References(func(expr parse.Expr, typ typecheck.Type) {
		for _, local := range body.Locals {
			if expr == local.Value {
				out[local.Name.Name] = typ
				return
			}
		}
	})
	return out, nil
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
	return completionList(fieldCompletionItems(typeSchema.Inputs, fieldPath, node.Body)), true, nil
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
	return completionList(fieldCompletionItems(schema.Configuration, fieldPath, nil)), true, nil
}

func outputValueCompletions(
	offset int,
	obj *parse.ObjectLit,
) (protocol.CompletionList, bool) {
	fieldPath, ok := objectValuePathAtOffset(obj, offset)
	if !ok || fieldParentPath(fieldPath) != "" {
		return protocol.CompletionList{}, false
	}
	target, ok := outputValueType(fieldPath)
	if !ok {
		return protocol.CompletionList{}, false
	}
	return completionList(staticValueCompletionItems(target)), true
}

func outputValueType(fieldPath string) (typecheck.Type, bool) {
	switch fieldPath {
	case "@sensitive":
		return typecheck.TBoolean(), true
	case "description":
		return typecheck.TString(), true
	default:
		return typecheck.Type{}, false
	}
}

func outputFieldCompletions(
	text string,
	offset int,
	obj *parse.ObjectLit,
	fieldPath string,
) (protocol.CompletionList, bool) {
	if fieldParentPath(fieldPath) != "" {
		return protocol.CompletionList{}, false
	}
	items := fieldKeyCompletionItems(
		[]string{"value", "description", "@sensitive"}, fieldPath, obj,
	)
	return completionList(withMetaKeyTextEdits(text, offset, items)), true
}

func nodeMetaCompletionItems(
	text string,
	offset int,
	kind syntax.NodeKind,
	fieldPath string,
	obj *parse.ObjectLit,
) []protocol.CompletionItem {
	items := fieldKeyCompletionItems(nodeMetaKeyNames(kind), fieldPath, obj)
	return withMetaKeyTextEdits(text, offset, items)
}

func nodeMetaKeyNames(kind syntax.NodeKind) []string {
	names := []string{"@depends-on", "@for-each", "@lock", "@timeout"}
	if kind == syntax.NodeAction {
		names = append(names, "@trigger")
	}
	return names
}

func fieldKeyCompletionItems(
	names []string,
	fieldPath string,
	obj *parse.ObjectLit,
) []protocol.CompletionItem {
	parent := fieldParentPath(fieldPath)
	if parent != "" {
		return nil
	}
	prefix := fieldLeafPrefix(fieldPath)
	present := objectFieldNamesAtPath(obj, parent)
	if fieldPath != "" {
		delete(present, fieldPath)
	}
	return namedFieldCompletionItems(names, prefix, present)
}

func withMetaKeyTextEdits(
	text string,
	offset int,
	items []protocol.CompletionItem,
) []protocol.CompletionItem {
	replacement := completionReplacementRange(text, offset)
	out := make([]protocol.CompletionItem, 0, len(items))
	for _, item := range items {
		if !strings.HasPrefix(item.Label, "@") {
			out = append(out, item)
			continue
		}
		item.FilterText = item.Label
		item.TextEdit = &protocol.TextEdit{Range: replacement, NewText: item.Label}
		out = append(out, item)
	}
	return out
}

func completionReplacementRange(text string, offset int) protocol.Range {
	start := offset
	if tok := tokenAtOffset(text, offset); tok.text != "" && tok.start <= offset {
		start = tok.start
	}
	pos := OffsetToLSP(text, offset)
	return protocol.Range{Start: OffsetToLSP(text, start), End: pos}
}

func combineCompletionItems(groups ...[]protocol.CompletionItem) []protocol.CompletionItem {
	byLabel := map[string]protocol.CompletionItem{}
	labels := make([]string, 0)
	for _, group := range groups {
		for _, item := range group {
			if item.Label == "" {
				continue
			}
			if _, ok := byLabel[item.Label]; ok {
				continue
			}
			byLabel[item.Label] = item
			labels = append(labels, item.Label)
		}
	}
	slices.Sort(labels)
	items := make([]protocol.CompletionItem, 0, len(labels))
	for _, label := range labels {
		items = append(items, byLabel[label])
	}
	return items
}

func fieldCompletionItems(
	fields map[string]typecheck.Type,
	fieldPath string,
	obj *parse.ObjectLit,
) []protocol.CompletionItem {
	parent := fieldParentPath(fieldPath)
	prefix := fieldLeafPrefix(fieldPath)
	present := objectFieldNamesAtPath(obj, parent)
	if fieldPath != "" {
		delete(present, fieldPath)
	}
	if parent == "" {
		return namedFieldCompletionItems(mapKeys(fields), prefix, present)
	}
	typ, ok := typeForFieldPath(fields, parent)
	if !ok {
		return nil
	}
	typ = typ.Unwrap()
	names := make([]string, 0, len(typ.Fields))
	for _, field := range typ.Fields {
		names = append(names, field.Name)
	}
	return namedFieldCompletionItems(names, prefix, present)
}

func namedFieldCompletionItems(
	names []string,
	prefix string,
	present map[string]bool,
) []protocol.CompletionItem {
	names = append([]string(nil), names...)
	slices.Sort(names)
	names = slices.Compact(names)
	items := make([]protocol.CompletionItem, 0, len(names))
	for _, name := range names {
		if present[name] || (prefix != "" && !strings.HasPrefix(name, prefix)) {
			continue
		}
		items = append(items, protocol.CompletionItem{
			Label: name,
			Kind:  protocol.CompletionItemKindField,
		})
	}
	return items
}

func fieldParentPath(fieldPath string) string {
	idx := strings.LastIndex(fieldPath, ".")
	if idx < 0 {
		return ""
	}
	return fieldPath[:idx]
}

func fieldLeafPrefix(fieldPath string) string {
	idx := strings.LastIndex(fieldPath, ".")
	if idx < 0 {
		return fieldPath
	}
	return fieldPath[idx+1:]
}

func objectFieldNamesAtPath(obj *parse.ObjectLit, fieldPath string) map[string]bool {
	out := map[string]bool{}
	target := objectAtPath(obj, fieldPath)
	if target == nil {
		return out
	}
	for _, field := range target.Fields {
		name, ok := fieldKeyName(field.Key)
		if ok {
			out[name] = true
		}
	}
	return out
}

func objectAtPath(obj *parse.ObjectLit, fieldPath string) *parse.ObjectLit {
	if obj == nil || fieldPath == "" {
		return obj
	}
	current := obj
	for part := range strings.SplitSeq(fieldPath, ".") {
		if part == "" {
			return nil
		}
		var next *parse.ObjectLit
		for _, field := range current.Fields {
			name, ok := fieldKeyName(field.Key)
			if !ok || name != part {
				continue
			}
			next, _ = field.Value.(*parse.ObjectLit)
			if next == nil && field.Decl != nil {
				next = field.Decl.Body
			}
			break
		}
		if next == nil {
			return nil
		}
		current = next
	}
	return current
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
