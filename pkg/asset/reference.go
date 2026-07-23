package asset

import (
	"encoding/base64"
	"fmt"
	"strings"
)

const referencePrefix = "unobin-asset:v1:"

func (s *Set) Value(name, internalPath string) (Value, error) {
	item, ok := s.Asset(name)
	if !ok {
		return Value{}, fmt.Errorf(`asset %q is not in asset set`, name)
	}
	entry, ok := item.Entry(internalPath)
	if !ok {
		return Value{}, fmt.Errorf(
			`asset %q has no entry %q`,
			name,
			internalPath,
		)
	}
	return valueForEntry(name, internalPath, entry), nil
}

func ParseReference(token string) (Reference, bool) {
	rest, ok := strings.CutPrefix(token, referencePrefix)
	if !ok {
		return Reference{}, false
	}
	kindValue, rest, ok := strings.Cut(rest, ":")
	if !ok {
		return Reference{}, false
	}
	kind := ReferenceKind(kindValue)
	if kind != ReferenceKindPath && kind != ReferenceKindContent {
		return Reference{}, false
	}
	identity, key, ok := strings.Cut(rest, ":")
	if !ok || !validSHA256(identity) || key == "" || strings.Contains(key, ":") {
		return Reference{}, false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(key)
	if err != nil {
		return Reference{}, false
	}
	nameBytes, pathBytes, ok := strings.Cut(string(decoded), "\x00")
	if !ok ||
		strings.Contains(pathBytes, "\x00") ||
		!validAssetName(nameBytes) ||
		pathBytes != "" && !validInternalPath(pathBytes) {
		return Reference{}, false
	}
	return Reference{
		Token:         token,
		Kind:          kind,
		EntryIdentity: identity,
		AssetName:     nameBytes,
		InternalPath:  pathBytes,
	}, true
}

func DisplayReference(value string) string {
	reference, ok := ParseReference(value)
	if !ok {
		return value
	}
	var display strings.Builder
	display.WriteString("<asset.")
	display.WriteString(reference.AssetName)
	if reference.InternalPath != "" {
		display.WriteByte('[')
		writeQuotedReferencePath(&display, reference.InternalPath)
		display.WriteByte(']')
	}
	display.WriteByte('.')
	display.WriteString(string(reference.Kind))
	display.WriteByte('>')
	return display.String()
}

func valueForEntry(name, internalPath string, entry *Entry) Value {
	return Value{
		Path: PathRef(referenceToken(
			ReferenceKindPath,
			entry.EntryIdentity,
			name,
			internalPath,
		)),
		Content: ContentRef(referenceToken(
			ReferenceKindContent,
			entry.EntryIdentity,
			name,
			internalPath,
		)),
		ContentSHA256: entry.ContentSHA256,
		Mode:          entry.Mode,
	}
}

func referenceToken(
	kind ReferenceKind,
	entryIdentity string,
	name string,
	internalPath string,
) string {
	key := base64.RawURLEncoding.EncodeToString([]byte(name + "\x00" + internalPath))
	return referencePrefix + string(kind) + ":" + entryIdentity + ":" + key
}

func writeQuotedReferencePath(output *strings.Builder, value string) {
	output.WriteByte('\'')
	for _, char := range value {
		switch char {
		case '\'':
			output.WriteString(`\'`)
		case '\\':
			output.WriteString(`\\`)
		case '\n':
			output.WriteString(`\n`)
		case '\r':
			output.WriteString(`\r`)
		case '\t':
			output.WriteString(`\t`)
		default:
			output.WriteRune(char)
		}
	}
	output.WriteByte('\'')
}
