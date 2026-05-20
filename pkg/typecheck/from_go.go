package typecheck

import "strings"

// FromGoString parses a Go type expression as rendered by goschema's
// typeString helper (e.g. `string`, `int64`, `[]string`,
// `map[string]string`, `*int64`, `time.Duration`) into a semantic
// Type. Anything the converter does not recognize comes back as
// Unknown so the checker skips silently rather than reporting a
// spurious mismatch.
func FromGoString(s string) Type {
	s = strings.TrimSpace(s)
	if s == "" {
		return TUnknown()
	}
	if strings.HasPrefix(s, "*") {
		return TOptional(FromGoString(s[1:]))
	}
	if strings.HasPrefix(s, "[]") {
		return TList(FromGoString(s[2:]))
	}
	if strings.HasPrefix(s, "map[") {
		key, value, ok := splitMapType(s)
		if !ok || key != "string" {
			return TUnknown()
		}
		return TMap(FromGoString(value))
	}
	switch s {
	case "string":
		return TString()
	case "bool":
		return TBoolean()
	case "any", "interface{}":
		return TAny()
	case
		"int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "byte", "rune":
		return TInteger()
	case "float32", "float64":
		return TNumber()
	case "time.Duration":
		return TInteger()
	}
	return TUnknown()
}

// splitMapType breaks "map[K]V" into ("K", "V", true). Returns
// ok=false when the brackets are unbalanced. The key portion is the
// substring between the matching `[` and `]`; everything past the
// closing bracket is the value.
func splitMapType(s string) (key, value string, ok bool) {
	if !strings.HasPrefix(s, "map[") {
		return "", "", false
	}
	depth := 0
	for i := 3; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return s[4:i], s[i+1:], true
			}
		}
	}
	return "", "", false
}
