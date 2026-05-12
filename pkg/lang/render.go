package lang

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Render formats v as a UB literal expression. Strings get single
// quotes with backslash escaping; lists, maps, and primitives use the
// canonical UB syntax. The result round trips through ParseSource for
// the value forms unobin natively produces (string, int64, float64,
// bool, nil, []any, map[string]any).
func Render(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return renderString(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.FormatInt(int64(x), 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case []any:
		parts := make([]string, len(x))
		for i, el := range x {
			parts[i] = Render(el)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, RenderKey(k)+": "+Render(x[k]))
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	}
	return fmt.Sprintf("%v", v)
}

// renderString emits s as a single-quoted UB string with backslash
// escapes for embedded quotes, backslashes, and the common control
// characters.
func renderString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('\'')
	for _, r := range s {
		switch r {
		case '\'':
			b.WriteString(`\'`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

// RenderKey returns k as a bare kebab-case identifier when it is a
// valid one, otherwise as a quoted string. Round trips cleanly through
// the parser either way.
func RenderKey(k string) string {
	if isKebabIdent(k) {
		return k
	}
	return renderString(k)
}

func isKebabIdent(s string) bool {
	if s == "" || !isLetter(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if !isLetter(c) && !isDigit(c) && c != '-' {
			return false
		}
	}
	last := s[len(s)-1]
	return isLetter(last) || isDigit(last)
}

func isLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
