package ub

import (
	"strings"

	"github.com/cloudboss/unobin/pkg/lang/parse"
)

// Tag is a field's parsed ub struct tag. Name is the tag's own name
// and may be empty; FieldName resolves the kebab-case fallback.
// Unknown collects options outside the recognized set so the schema
// reader can warn on a typo such as "sensitiv" instead of quietly
// accepting it.
type Tag struct {
	Name      string
	Skip      bool
	Omitempty bool
	Sensitive bool
	Unknown   []string
}

// ParseTag reads the value of a ub struct tag: a name, possibly
// empty, followed by comma-separated options. A value of exactly "-"
// skips the field. The name and each option are trimmed of spaces;
// omitempty, squash, and sensitive are the recognized options. Every
// reader of the tag resolves it through here, so the schema a factory
// compiles against and the values it encodes and decodes at run time
// follow one grammar.
func ParseTag(value string) Tag {
	if value == "-" {
		return Tag{Skip: true}
	}
	parts := strings.Split(value, ",")
	t := Tag{Name: strings.TrimSpace(parts[0])}
	for _, opt := range parts[1:] {
		switch strings.TrimSpace(opt) {
		case "omitempty":
			t.Omitempty = true
		case "sensitive":
			t.Sensitive = true
		case "squash", "":
		default:
			t.Unknown = append(t.Unknown, strings.TrimSpace(opt))
		}
	}
	return t
}

// FieldName is the map key for a field under the ub tag convention:
// the tag's name, or the kebab-cased Go field name when the tag
// names nothing.
func (t Tag) FieldName(goName string) string {
	if t.Name != "" {
		return t.Name
	}
	return parse.PascalToKebab(goName)
}
