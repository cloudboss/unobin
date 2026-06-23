package stateref

import (
	"fmt"
	"strings"
)

type Category string

const (
	CategoryResource   Category = "resource"
	CategoryDataSource Category = "data-source"
	CategoryAction     Category = "action"
)

type StringKey struct {
	Value string
}

type StateAddressSegment struct {
	Category Category
	Name     string
	Key      *StringKey
}

type StateRef struct {
	Segments []StateAddressSegment
}

type EntryRef struct {
	Address string
}

func Parse(s string) (EntryRef, error) {
	ref, err := ParseStateRef(s)
	if err != nil {
		return EntryRef{}, err
	}
	return EntryRef{Address: ref.String()}, nil
}

func ParseStateRef(s string) (StateRef, error) {
	if s == "" {
		return StateRef{}, fmt.Errorf("expected state ref")
	}
	if containsOutsideKey(s, '@') {
		return StateRef{}, fmt.Errorf("expected state ref, got %s", s)
	}
	parts, err := splitSegments(s)
	if err != nil {
		return StateRef{}, err
	}
	segments := make([]StateAddressSegment, 0, len(parts))
	for _, part := range parts {
		segment, err := parseSegment(part)
		if err != nil {
			return StateRef{}, err
		}
		segments = append(segments, segment)
	}
	return StateRef{Segments: segments}, nil
}

func ValidateAddress(address string) error {
	_, err := ParseStateRef(address)
	return err
}

// AppendInstanceKey returns template with key set on its final segment.
func AppendInstanceKey(template string, key string) (string, error) {
	ref, err := ParseStateRef(template)
	if err != nil {
		return "", err
	}
	last := &ref.Segments[len(ref.Segments)-1]
	if last.Key != nil {
		return "", fmt.Errorf("state ref already has an instance key")
	}
	last.Key = &StringKey{Value: key}
	return ref.String(), nil
}

// SplitInstanceKey removes and returns the final segment's static key.
func SplitInstanceKey(address string) (template string, key string, ok bool, err error) {
	ref, err := ParseStateRef(address)
	if err != nil {
		return "", "", false, err
	}
	last := &ref.Segments[len(ref.Segments)-1]
	if last.Key == nil {
		return ref.String(), "", false, nil
	}
	key = last.Key.Value
	last.Key = nil
	return ref.String(), key, true, nil
}

// Template removes static keys from every segment in address.
func Template(address string) (string, error) {
	ref, err := ParseStateRef(address)
	if err != nil {
		return "", err
	}
	for i := range ref.Segments {
		ref.Segments[i].Key = nil
	}
	return ref.String(), nil
}

// Parent returns the slash parent of address, or empty for a root segment.
func Parent(address string) (string, error) {
	ref, err := ParseStateRef(address)
	if err != nil {
		return "", err
	}
	if len(ref.Segments) == 1 {
		return "", nil
	}
	return StateRef{Segments: ref.Segments[:len(ref.Segments)-1]}.String(), nil
}

func splitSegments(s string) ([]string, error) {
	parts := []string{}
	start := 0
	inKey := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inKey {
			if escaped {
				escaped = false
				continue
			}
			switch c {
			case '\\':
				escaped = true
			case '\'':
				if i+1 < len(s) && s[i+1] == ']' {
					inKey = false
					i++
				}
			}
			continue
		}
		switch c {
		case '[':
			inKey = true
		case '/':
			if i == start {
				return nil, fmt.Errorf("empty state ref segment")
			}
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	if start == len(s) {
		return nil, fmt.Errorf("empty state ref segment")
	}
	parts = append(parts, s[start:])
	return parts, nil
}

func containsOutsideKey(s string, target byte) bool {
	inKey := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inKey {
			if escaped {
				escaped = false
				continue
			}
			switch c {
			case '\\':
				escaped = true
			case '\'':
				if i+1 < len(s) && s[i+1] == ']' {
					inKey = false
					i++
				}
			}
			continue
		}
		if c == '[' {
			inKey = true
			continue
		}
		if c == target {
			return true
		}
	}
	return false
}

func parseSegment(s string) (StateAddressSegment, error) {
	dot := strings.IndexByte(s, '.')
	if dot <= 0 || dot == len(s)-1 {
		return StateAddressSegment{}, fmt.Errorf("address segment must be <category>.<name>")
	}
	category, ok := parseCategory(s[:dot])
	if !ok {
		return StateAddressSegment{}, fmt.Errorf("address root must be resource, data-source, or action")
	}
	rest := s[dot+1:]
	name := rest
	var key *StringKey
	if keyStart := strings.IndexByte(rest, '['); keyStart >= 0 {
		name = rest[:keyStart]
		parsed, err := parseKey(rest[keyStart:], s)
		if err != nil {
			return StateAddressSegment{}, err
		}
		key = &parsed
	}
	if strings.Contains(name, ".") {
		return StateAddressSegment{}, fmt.Errorf("state refs do not include field access")
	}
	if !validIdent(name) {
		return StateAddressSegment{}, fmt.Errorf("invalid state ref name %s", name)
	}
	return StateAddressSegment{Category: category, Name: name, Key: key}, nil
}

func parseCategory(s string) (Category, bool) {
	switch Category(s) {
	case CategoryResource, CategoryDataSource, CategoryAction:
		return Category(s), true
	default:
		return "", false
	}
}

func parseKey(s, context string) (StringKey, error) {
	if !strings.HasPrefix(s, "['") {
		return StringKey{}, fmt.Errorf("malformed instance key in %s", context)
	}
	var b strings.Builder
	for i := 2; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\n', '\r':
			return StringKey{}, fmt.Errorf("malformed instance key in %s", context)
		case '\\':
			if i+1 >= len(s) {
				return StringKey{}, fmt.Errorf("malformed instance key in %s", context)
			}
			i++
			b.WriteByte(s[i])
		case '\'':
			if i+1 >= len(s) || s[i+1] != ']' {
				return StringKey{}, fmt.Errorf("malformed instance key in %s", context)
			}
			if i+2 != len(s) {
				if s[i+2] == '.' {
					return StringKey{}, fmt.Errorf("state refs do not include field access")
				}
				return StringKey{}, fmt.Errorf("malformed instance key in %s", context)
			}
			return StringKey{Value: b.String()}, nil
		default:
			b.WriteByte(c)
		}
	}
	return StringKey{}, fmt.Errorf("malformed instance key in %s", context)
}

func validIdent(s string) bool {
	if s == "" || !asciiLetter(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if !asciiLetter(c) && !asciiDigit(c) && c != '-' {
			return false
		}
	}
	return asciiLetter(s[len(s)-1]) || asciiDigit(s[len(s)-1])
}

func asciiLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func asciiDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func (s StateRef) String() string {
	parts := make([]string, 0, len(s.Segments))
	for _, segment := range s.Segments {
		parts = append(parts, segment.String())
	}
	return strings.Join(parts, "/")
}

func (s StateAddressSegment) String() string {
	out := string(s.Category) + "." + s.Name
	if s.Key != nil {
		out += "['" + escapeKey(s.Key.Value) + "']"
	}
	return out
}

func escapeKey(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return strings.ReplaceAll(s, "'", "\\'")
}

func (r EntryRef) String() string {
	return r.Address
}

func Same(a, b EntryRef) bool {
	return a.Address == b.Address
}
