package parse

import (
	"fmt"
	"strings"
)

// processTripleQuoteBody decodes a triple-quoted string. text is the
// entire literal including its three-quote delimiters; startCol is the
// column of the opening quote; sigil is "" for the single-line form or
// one of "|", "|-", ">", ">-", "\\", "\\-" for the multi-line forms.
//
// Single-line form returns the body verbatim with no escape processing.
// Multi-line form strips the closing line's space-only indent prefix
// from every content line, dispatches on the sigil for mode (literal,
// folded, joined), and applies chomp (clip keeps one trailing newline,
// strip removes all trailing newlines).
func processTripleQuoteBody(text []byte, startCol int, sigil string) (string, error) {
	if len(text) < 6 {
		return "", fmt.Errorf("triple-quoted string too short")
	}
	body := text[3 : len(text)-3]
	if sigil == "" {
		return string(body), nil
	}
	skip := len(sigil) + 1
	if skip > len(body) {
		return "", fmt.Errorf("triple-quoted string body too short")
	}
	body = body[skip:]

	col := startCol
	for i := 0; i < len(text)-3; i++ {
		switch text[i] {
		case '\n':
			col = 1
		case '\r':
		default:
			col++
		}
	}
	stripN := col - 1
	if stripN < 0 {
		stripN = 0
	}

	raw := strings.Split(string(body), "\n")
	lines := make([]contentLine, 0, len(raw))
	for i, ln := range raw {
		ln = strings.TrimRight(ln, "\r")
		if i == len(raw)-1 {
			for j := 0; j < len(ln); j++ {
				if ln[j] != ' ' {
					return "", fmt.Errorf("triple-quoted string: closing indent must be spaces only")
				}
			}
			continue
		}
		if len(ln) < stripN {
			if strings.TrimSpace(ln) != "" {
				return "", fmt.Errorf("triple-quoted string: line is less indented than the closing quote")
			}
			lines = append(lines, contentLine{blank: true})
			continue
		}
		for j := 0; j < stripN; j++ {
			if ln[j] == '\t' {
				return "", fmt.Errorf("triple-quoted string: indent prefix must be spaces only, no tabs")
			}
			if ln[j] != ' ' {
				return "", fmt.Errorf("triple-quoted string: line is less indented than the closing quote")
			}
		}
		rest := strings.TrimRight(ln[stripN:], " \t")
		if rest == "" {
			lines = append(lines, contentLine{blank: true})
			continue
		}
		info := contentLine{text: rest}
		if rest[0] == ' ' || rest[0] == '\t' {
			info.more = true
		}
		lines = append(lines, info)
	}

	mode := strings.TrimSuffix(sigil, "-")
	chomp := strings.HasSuffix(sigil, "-")

	var value string
	switch mode {
	case "|":
		value = literalValue(lines)
	case ">":
		value = foldedValue(lines)
	case "\\":
		value = joinedValue(lines)
	default:
		return "", fmt.Errorf("unknown triple-quote sigil %q", sigil)
	}

	if chomp {
		value = strings.TrimRight(value, "\n")
	} else if value != "" {
		value = strings.TrimRight(value, "\n") + "\n"
	}
	return value, nil
}

// contentLine describes one content line of a multi-line triple-quoted
// string after the baseline indent has been stripped. blank means the
// line was empty or whitespace-only after the strip. more means the
// remaining content begins with whitespace, which marks the line as
// more-indented than the strip baseline.
type contentLine struct {
	text  string
	blank bool
	more  bool
}

// literalValue preserves every newline as-is. Each line contributes
// its content, lines are joined with "\n", and a trailing "\n" is
// appended when the body is non-empty so the resulting value ends on
// a newline boundary (clip semantics; strip is applied later).
func literalValue(lines []contentLine) string {
	if len(lines) == 0 {
		return ""
	}
	parts := make([]string, len(lines))
	for i, ln := range lines {
		if !ln.blank {
			parts[i] = ln.text
		}
	}
	return strings.Join(parts, "\n") + "\n"
}

// foldedValue applies a YAML-style fold pass. Adjacent regular-indent
// non-blank lines are joined by a single space; pairs that involve a
// more-indented line are joined by "\n"; a run of N blank lines
// between two non-blank lines contributes N "\n" and replaces any
// separator that would otherwise apply.
func foldedValue(lines []contentLine) string {
	first := -1
	for i, ln := range lines {
		if !ln.blank {
			first = i
			break
		}
	}
	if first < 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(lines[first].text)
	prev := first
	for j := first + 1; j < len(lines); j++ {
		if lines[j].blank {
			continue
		}
		blanks := j - prev - 1
		if blanks > 0 {
			b.WriteString(strings.Repeat("\n", blanks))
		} else if lines[prev].more || lines[j].more {
			b.WriteByte('\n')
		} else {
			b.WriteByte(' ')
		}
		b.WriteString(lines[j].text)
		prev = j
	}
	return b.String() + "\n"
}

// joinedValue concatenates non-blank lines with the empty separator.
// Blank lines contribute nothing. Trailing whitespace per line has
// already been trimmed at classification time; leading whitespace
// past the strip baseline is preserved as content.
func joinedValue(lines []contentLine) string {
	var b strings.Builder
	saw := false
	for _, ln := range lines {
		if ln.blank {
			continue
		}
		b.WriteString(ln.text)
		saw = true
	}
	if !saw {
		return ""
	}
	return b.String() + "\n"
}

// formFromSigil maps the parsed sigil text to its StringForm. The
// empty string is the single-line triple-quote form.
func formFromSigil(s string) StringForm {
	switch s {
	case "":
		return StringTripleQuoteSingleLine
	case "|":
		return StringLiteralClip
	case "|-":
		return StringLiteralStrip
	case ">":
		return StringFoldedClip
	case ">-":
		return StringFoldedStrip
	case "\\":
		return StringJoinedClip
	case "\\-":
		return StringJoinedStrip
	}
	return StringTripleQuoteSingleLine
}
