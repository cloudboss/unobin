package lang

import (
	"fmt"
	"strings"

	"github.com/cloudboss/unobin/pkg/encoding/ub"
)

// Render formats v as a UB literal expression on one line. The
// canonical set (string, bool, int64, float64, []any, map[string]any,
// nil) renders directly; types implementing ub.Marshaler control
// their own form; anything else falls through to Go's default
// formatter so callers never see an empty string.
func Render(v any) string {
	b, err := ub.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// RenderPretty formats v as UB syntax with indented multi-line
// expansion for non-empty maps and lists. Empty collections render
// inline as `{}` or `[]`. Atoms render exactly as Render would emit
// them.
func RenderPretty(v any) string {
	b, err := ub.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
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
	if s == "" {
		return false
	}
	i := 0
	if s[0] == '@' {
		i = 1
	}
	if i >= len(s) || !isLetter(s[i]) {
		return false
	}
	for j := i + 1; j < len(s); j++ {
		c := s[j]
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
