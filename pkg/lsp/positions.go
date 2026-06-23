package lsp

import (
	"sort"
	"unicode/utf8"

	"github.com/cloudboss/unobin/pkg/lsp/protocol"
)

// OffsetToLSP converts a byte offset to a zero-based UTF-16 LSP position.
func OffsetToLSP(text string, offset int) protocol.Position {
	offset = clampToUTF8Boundary(text, offset)
	lines := buildLineInfo(text)
	lineIndex := lineForOffset(lines, offset)
	line := lines[lineIndex]
	lineOffset := min(offset, line.End)
	return protocol.Position{
		Line:      uint32(lineIndex),
		Character: uint32(utf16Len(text[line.Start:lineOffset])),
	}
}

// LSPToOffset converts a zero-based UTF-16 LSP position to a byte offset.
func LSPToOffset(text string, pos protocol.Position) (int, bool) {
	lines := buildLineInfo(text)
	if int(pos.Line) >= len(lines) {
		return 0, false
	}
	line := lines[pos.Line]
	target := int(pos.Character)
	units := 0
	for offset := line.Start; offset < line.End; {
		if units == target {
			return offset, true
		}
		r, size := utf8.DecodeRuneInString(text[offset:line.End])
		nextUnits := units + runeUTF16Len(r)
		if nextUnits > target {
			return 0, false
		}
		offset += size
		units = nextUnits
	}
	if units != target {
		return 0, false
	}
	return line.End, true
}

func clampToUTF8Boundary(text string, offset int) int {
	if offset <= 0 {
		return 0
	}
	if offset >= len(text) {
		return len(text)
	}
	for offset > 0 && !utf8.RuneStart(text[offset]) {
		offset--
	}
	return offset
}

func lineForOffset(lines []LineInfo, offset int) int {
	line := sort.Search(len(lines), func(i int) bool {
		return lines[i].Start > offset
	}) - 1
	if line < 0 {
		return 0
	}
	return line
}

func utf16Len(text string) int {
	units := 0
	for _, r := range text {
		units += runeUTF16Len(r)
	}
	return units
}

func runeUTF16Len(r rune) int {
	if r > 0xffff {
		return 2
	}
	return 1
}
