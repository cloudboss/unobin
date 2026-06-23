package lsp

import (
	"strings"

	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/lsp/protocol"
)

// DocumentSymbolsForText parses UB text and returns document symbols.
func DocumentSymbolsForText(
	path string,
	text string,
) ([]protocol.DocumentSymbol, *protocol.ResponseError) {
	file, err := syntax.ParseSource(path, []byte(text))
	if err != nil {
		return nil, protocol.InvalidParams(err.Error())
	}
	return documentSymbols(file, text), nil
}

func documentSymbols(file *syntax.File, text string) []protocol.DocumentSymbol {
	if file == nil {
		return nil
	}
	switch file.Kind {
	case syntax.FileFactory:
		if file.Factory == nil {
			return nil
		}
		return factorySymbols(text, file.Factory.Body)
	case syntax.FileStack:
		return stackSymbols(text, file.Stack)
	case syntax.FileProject:
		return projectSymbols(text, file.Project)
	case syntax.FileProjectLock:
		return projectLockSymbols(text, file.ProjectLock)
	case syntax.FileLibrary:
		return librarySymbols(text, file.Library)
	default:
		return nil
	}
}

func factorySymbols(text string, body syntax.FactoryBody) []protocol.DocumentSymbol {
	symbols := make([]protocol.DocumentSymbol, 0,
		len(body.Inputs)+len(body.Locals)+len(body.Constraints)+len(body.Imports)+
			len(body.LibraryConfigs)+len(body.StateMoves)+len(body.Resources)+
			len(body.Data)+len(body.Actions)+len(body.Outputs))
	for _, input := range body.Inputs {
		symbols = append(symbols, symbolFromSpan(text, "input."+input.Name.Name,
			protocol.SymbolKindVariable, input.Name.S))
	}
	for _, local := range body.Locals {
		symbols = append(symbols, symbolFromSpan(text, "local."+local.Name.Name,
			protocol.SymbolKindVariable, local.Name.S))
	}
	for _, constraint := range body.Constraints {
		symbols = append(symbols, symbolFromSpan(text, "constraint",
			protocol.SymbolKindFunction, constraint.S))
	}
	for _, imp := range body.Imports {
		symbols = append(symbols, symbolFromSpan(text, "import."+imp.Alias.Name,
			protocol.SymbolKindModule, imp.Alias.S))
	}
	for _, config := range body.LibraryConfigs {
		symbols = append(symbols, symbolFromSpan(text, "library-config."+config.Alias.Name,
			protocol.SymbolKindField, config.Alias.S))
	}
	for _, move := range body.StateMoves {
		symbols = append(symbols, symbolFromSpan(text, stateMoveSymbolName(move),
			protocol.SymbolKindField, stateMoveSymbolSpan(move)))
	}
	for _, node := range body.Resources {
		symbols = append(symbols, nodeSymbol(text, node))
	}
	for _, node := range body.Data {
		symbols = append(symbols, nodeSymbol(text, node))
	}
	for _, node := range body.Actions {
		symbols = append(symbols, nodeSymbol(text, node))
	}
	for _, output := range body.Outputs {
		symbols = append(symbols, symbolFromSpan(text, "output."+output.Name.Name,
			protocol.SymbolKindVariable, output.Name.S))
	}
	return symbols
}

func stackSymbols(text string, stack *syntax.StackFile) []protocol.DocumentSymbol {
	if stack == nil {
		return nil
	}
	var symbols []protocol.DocumentSymbol
	if stack.Factory != nil && stack.Factory.Inputs != nil {
		for _, field := range stack.Factory.Inputs.Fields {
			name := "factory.input." + fieldKeyDisplay(field.Key)
			symbols = append(symbols, symbolFromSpan(text, name,
				protocol.SymbolKindVariable, field.Key.S))
		}
	}
	if stack.State != nil {
		symbols = append(symbols, symbolFromSpan(text, "state."+stack.State.Selector.Name,
			protocol.SymbolKindField, stack.State.Selector.S))
	}
	if stack.Encryption != nil {
		name := "encryption." + stack.Encryption.Selector.Name
		symbols = append(symbols, symbolFromSpan(text, name,
			protocol.SymbolKindField, stack.Encryption.Selector.S))
	}
	for _, local := range stack.Locals {
		symbols = append(symbols, symbolFromSpan(text, "local."+local.Name.Name,
			protocol.SymbolKindVariable, local.Name.S))
	}
	if stack.Parallelism != nil {
		symbols = append(symbols, symbolFromSpan(text, "parallelism",
			protocol.SymbolKindField, stack.Parallelism.Span()))
	}
	return symbols
}

func projectSymbols(text string, project *syntax.ProjectFile) []protocol.DocumentSymbol {
	if project == nil {
		return nil
	}
	var symbols []protocol.DocumentSymbol
	if project.UnobinVersion != nil {
		symbols = append(symbols, symbolFromSpan(text, "unobin-version",
			protocol.SymbolKindField, project.UnobinVersion.Span()))
	}
	for _, require := range project.Requires {
		symbols = append(symbols, symbolFromSpan(text, "requires."+require.ID.Value,
			protocol.SymbolKindModule, require.ID.S))
	}
	for _, replace := range project.Replace {
		symbols = append(symbols, symbolFromSpan(text, "replace."+replace.ID.Value,
			protocol.SymbolKindModule, replace.ID.S))
	}
	return symbols
}

func projectLockSymbols(
	text string,
	projectLock *syntax.ProjectLockFile,
) []protocol.DocumentSymbol {
	if projectLock == nil {
		return nil
	}
	var symbols []protocol.DocumentSymbol
	if projectLock.Version != nil {
		symbols = append(symbols, symbolFromSpan(text, "version",
			protocol.SymbolKindField, projectLock.Version.Span()))
	}
	if projectLock.Toolchain != nil &&
		projectLock.Toolchain.UnobinVersion != nil {
		symbols = append(symbols, symbolFromSpan(text, "toolchain.unobin-version",
			protocol.SymbolKindField, projectLock.Toolchain.UnobinVersion.Span()))
	}
	for _, dep := range projectLock.Deps {
		symbols = append(symbols, symbolFromSpan(text, "deps."+dep.ID.Value,
			protocol.SymbolKindModule, dep.ID.S))
	}
	return symbols
}

func librarySymbols(text string, library *syntax.LibraryFile) []protocol.DocumentSymbol {
	if library == nil {
		return nil
	}
	symbols := make([]protocol.DocumentSymbol, 0, len(library.Exports))
	for _, export := range library.Exports {
		name := string(export.Kind) + "." + export.Name.Name
		symbols = append(symbols, symbolFromSpan(text, name,
			protocol.SymbolKindClass, export.Name.S))
	}
	return symbols
}

func nodeSymbol(text string, node syntax.NodeDecl) protocol.DocumentSymbol {
	name := string(node.Kind) + "." + node.Name.Name
	return symbolFromSpan(text, name, protocol.SymbolKindClass, node.Name.S)
}

func stateMoveSymbolName(move syntax.StateMoveDecl) string {
	if move.From == nil || move.To == nil {
		return "state-move"
	}
	return "state-move." + move.From.Ref.String() + " -> " + move.To.Ref.String()
}

func stateMoveSymbolSpan(move syntax.StateMoveDecl) parse.Span {
	if move.From != nil {
		return move.From.S
	}
	return move.S
}

func fieldKeyDisplay(key parse.FieldKey) string {
	switch key.Kind {
	case parse.FieldString:
		return key.String
	case parse.FieldPath:
		return strings.Join(key.Path, ".")
	default:
		return key.Name
	}
}

func symbolFromSpan(
	text string,
	name string,
	kind protocol.SymbolKind,
	span parse.Span,
) protocol.DocumentSymbol {
	rangeText := rangeFromSpan(text, span)
	return protocol.DocumentSymbol{
		Name:           name,
		Kind:           kind,
		Range:          rangeText,
		SelectionRange: rangeText,
	}
}

func rangeFromSpan(text string, span parse.Span) protocol.Range {
	start := span.Start.Offset
	end := start
	if !span.End.IsZero() {
		end = span.End.Offset
	}
	if end <= start {
		end = symbolEnd(text, start)
	}
	if end < start {
		end = start
	}
	return protocol.Range{
		Start: OffsetToLSP(text, start),
		End:   OffsetToLSP(text, end),
	}
}

func symbolEnd(text string, start int) int {
	if start < 0 || start >= len(text) {
		return start
	}
	if text[start] == '\'' {
		for i := start + 1; i < len(text); i++ {
			if text[i] == '\'' && text[i-1] != '\\' {
				return i + 1
			}
		}
		return start
	}
	end := start
	for end < len(text) && isSymbolByte(text[end]) {
		end++
	}
	return end
}

func isSymbolByte(b byte) bool {
	return b == '@' || b == '-' || b == '.' || b == '_' ||
		(b >= '0' && b <= '9') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= 'a' && b <= 'z')
}
