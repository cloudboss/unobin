package configuration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	encodedvalue "github.com/cloudboss/unobin/pkg/encoding/value"
)

type Record struct {
	Address         string                 `json:"address"`
	LibraryPath     string                 `json:"library-path"`
	SchemaVersion   int                    `json:"schema-version"`
	SchemaDigest    string                 `json:"schema-digest"`
	Value           encodedvalue.Value     `json:"value"`
	SensitivePaths  []string               `json:"sensitive-paths"`
	SensitiveValues []SensitiveValueRecord `json:"sensitive-values"`
	Digest          string                 `json:"digest"`
}

type SensitiveValueRecord struct {
	Path string `json:"path"`
	ID   string `json:"id"`
}

type IDSource func() (string, error)

func Build(record Record, prior *Record, newID IDSource) (Record, error) {
	record.SensitivePaths = append([]string{}, record.SensitivePaths...)
	slices.Sort(record.SensitivePaths)
	record.SensitivePaths = slices.Compact(record.SensitivePaths)
	record.SensitiveValues = []SensitiveValueRecord{}

	leaves, err := sensitiveLeaves(record.Value, record.SensitivePaths)
	if err != nil {
		return Record{}, err
	}
	priorValues := map[string]encodedvalue.Value{}
	priorIDs := map[string]string{}
	if prior != nil {
		if err := prior.Validate(); err != nil {
			return Record{}, fmt.Errorf("prior configuration: %w", err)
		}
		for _, sensitive := range prior.SensitiveValues {
			value, err := valueAtPointer(prior.Value, sensitive.Path)
			if err != nil {
				return Record{}, fmt.Errorf("prior sensitive value %q: %w", sensitive.Path, err)
			}
			priorValues[sensitive.Path] = value
			priorIDs[sensitive.Path] = sensitive.ID
		}
	}

	reusedIDs := map[string]string{}
	usedIDs := map[string]bool{}
	for _, leaf := range leaves {
		priorValue, ok := priorValues[leaf.path]
		if !ok || !valuesEqual(priorValue, leaf.value) {
			continue
		}
		id := priorIDs[leaf.path]
		if usedIDs[id] {
			return Record{}, fmt.Errorf("reused sensitive value ID %q is not unique", id)
		}
		reusedIDs[leaf.path] = id
		usedIDs[id] = true
	}
	for _, leaf := range leaves {
		id := reusedIDs[leaf.path]
		if id == "" {
			if newID == nil {
				return Record{}, fmt.Errorf("sensitive value ID source is required")
			}
			for {
				id, err = newID()
				if err != nil {
					return Record{}, fmt.Errorf("generate sensitive value ID: %w", err)
				}
				if !validSensitiveID(id) {
					return Record{}, fmt.Errorf(
						"sensitive value ID must be 32 lowercase hexadecimal characters",
					)
				}
				if !usedIDs[id] {
					break
				}
			}
		}
		if reusedIDs[leaf.path] == "" {
			usedIDs[id] = true
		}
		record.SensitiveValues = append(record.SensitiveValues, SensitiveValueRecord{
			Path: leaf.path,
			ID:   id,
		})
	}

	record.Digest, err = Digest(record)
	if err != nil {
		return Record{}, err
	}
	if err := record.Validate(); err != nil {
		return Record{}, err
	}
	return record, nil
}

func Clone(record Record) Record {
	record.SensitivePaths = append([]string{}, record.SensitivePaths...)
	record.SensitiveValues = append([]SensitiveValueRecord{}, record.SensitiveValues...)
	return record
}

func (r Record) Validate() error {
	if r.LibraryPath == "" {
		return fmt.Errorf("library path is required")
	}
	if r.SchemaVersion < 1 {
		return fmt.Errorf("schema version must be greater than zero")
	}
	if !validSHA256(r.SchemaDigest) {
		return fmt.Errorf("schema digest must be a lowercase SHA-256 digest")
	}
	if r.Value.HasPending() {
		return fmt.Errorf("configuration value must be concrete")
	}
	if _, ok := r.Value.ObjectFields(); !ok {
		return fmt.Errorf("configuration value must be an object")
	}
	if err := validateSortedStrings(r.SensitivePaths, "sensitive paths"); err != nil {
		return err
	}
	leaves, err := sensitiveLeaves(r.Value, r.SensitivePaths)
	if err != nil {
		return err
	}
	if err := validateSensitiveValues(r.SensitiveValues); err != nil {
		return err
	}
	if len(leaves) != len(r.SensitiveValues) {
		return fmt.Errorf("sensitive values do not match sensitive leaves")
	}
	for i, leaf := range leaves {
		if r.SensitiveValues[i].Path != leaf.path {
			return fmt.Errorf("sensitive values do not match sensitive leaves")
		}
	}
	if !validSHA256(r.Digest) {
		return fmt.Errorf("digest must be a lowercase SHA-256 digest")
	}
	digest, err := Digest(r)
	if err != nil {
		return err
	}
	if r.Digest != digest {
		return fmt.Errorf("configuration digest does not match record contents")
	}
	return nil
}

func validateSortedStrings(values []string, subject string) error {
	for i, value := range values {
		if i > 0 && value <= values[i-1] {
			return fmt.Errorf("%s must be unique and sorted", subject)
		}
	}
	return nil
}

func ValidatePathSyntax(paths []string, subject string) error {
	if err := validateSortedStrings(paths, subject+" paths"); err != nil {
		return err
	}
	for _, path := range paths {
		if _, err := parsePointer(path); err != nil {
			return fmt.Errorf("%s path %q: %w", subject, path, err)
		}
	}
	return nil
}

func ValidatePaths(
	value encodedvalue.Value,
	paths []string,
	subject string,
) error {
	if err := ValidatePathSyntax(paths, subject); err != nil {
		return err
	}
	for _, path := range paths {
		if _, err := valueAtPointer(value, path); err != nil {
			return fmt.Errorf("%s path %q: %w", subject, path, err)
		}
	}
	return nil
}

func validateSensitiveValues(values []SensitiveValueRecord) error {
	ids := make(map[string]bool, len(values))
	for i, value := range values {
		if i > 0 && value.Path <= values[i-1].Path {
			return fmt.Errorf("sensitive values must be unique and sorted")
		}
		if !validSensitiveID(value.ID) {
			return fmt.Errorf(
				"sensitive value ID must be 32 lowercase hexadecimal characters",
			)
		}
		if ids[value.ID] {
			return fmt.Errorf("sensitive value IDs must be unique")
		}
		ids[value.ID] = true
	}
	return nil
}

func validSensitiveID(value string) bool {
	if len(value) != 32 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 16
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

type sensitiveLeaf struct {
	path  string
	value encodedvalue.Value
}

func sensitiveLeaves(
	value encodedvalue.Value,
	paths []string,
) ([]sensitiveLeaf, error) {
	leaves := map[string]encodedvalue.Value{}
	for _, path := range paths {
		selected, err := valueAtPointer(value, path)
		if err != nil {
			return nil, fmt.Errorf("sensitive path %q: %w", path, err)
		}
		collectLeaves(selected, path, leaves)
	}
	leafPaths := slices.Sorted(maps.Keys(leaves))
	result := make([]sensitiveLeaf, 0, len(leafPaths))
	for _, path := range leafPaths {
		result = append(result, sensitiveLeaf{path: path, value: leaves[path]})
	}
	return result, nil
}

func collectLeaves(
	value encodedvalue.Value,
	path string,
	leaves map[string]encodedvalue.Value,
) {
	switch value.Kind() {
	case encodedvalue.KindAbsent:
		return
	case encodedvalue.KindList:
		items, _ := value.Items()
		for i, item := range items {
			collectLeaves(item, appendPointer(path, strconv.Itoa(i)), leaves)
		}
	case encodedvalue.KindMap:
		entries, _ := value.MapEntries()
		for _, key := range slices.Sorted(maps.Keys(entries)) {
			collectLeaves(entries[key], appendPointer(path, key), leaves)
		}
	case encodedvalue.KindObject:
		fields, _ := value.ObjectFields()
		for _, name := range slices.Sorted(maps.Keys(fields)) {
			collectLeaves(fields[name], appendPointer(path, name), leaves)
		}
	default:
		leaves[path] = value
	}
}

func valueAtPointer(
	value encodedvalue.Value,
	pointer string,
) (encodedvalue.Value, error) {
	segments, err := parsePointer(pointer)
	if err != nil {
		return encodedvalue.Value{}, err
	}
	current := value
	for _, segment := range segments {
		switch current.Kind() {
		case encodedvalue.KindObject:
			fields, _ := current.ObjectFields()
			next, ok := fields[segment]
			if !ok {
				return encodedvalue.Value{}, fmt.Errorf("does not resolve")
			}
			current = next
		case encodedvalue.KindMap:
			entries, _ := current.MapEntries()
			next, ok := entries[segment]
			if !ok {
				return encodedvalue.Value{}, fmt.Errorf("does not resolve")
			}
			current = next
		case encodedvalue.KindList:
			index, err := parseListIndex(segment)
			if err != nil {
				return encodedvalue.Value{}, err
			}
			items, _ := current.Items()
			if index >= len(items) {
				return encodedvalue.Value{}, fmt.Errorf("does not resolve")
			}
			current = items[index]
		default:
			return encodedvalue.Value{}, fmt.Errorf("does not resolve")
		}
	}
	return current, nil
}

func parsePointer(pointer string) ([]string, error) {
	if pointer == "" {
		return nil, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("must be empty or start with /")
	}
	rawSegments := strings.Split(pointer[1:], "/")
	segments := make([]string, len(rawSegments))
	for i, raw := range rawSegments {
		var result strings.Builder
		for j := 0; j < len(raw); j++ {
			if raw[j] != '~' {
				result.WriteByte(raw[j])
				continue
			}
			if j+1 >= len(raw) {
				return nil, fmt.Errorf("invalid escape")
			}
			j++
			switch raw[j] {
			case '0':
				result.WriteByte('~')
			case '1':
				result.WriteByte('/')
			default:
				return nil, fmt.Errorf("invalid escape")
			}
		}
		segments[i] = result.String()
	}
	return segments, nil
}

func parseListIndex(segment string) (int, error) {
	if segment == "" || len(segment) > 1 && segment[0] == '0' {
		return 0, fmt.Errorf("noncanonical list index %q", segment)
	}
	for i := range len(segment) {
		if segment[i] < '0' || segment[i] > '9' {
			return 0, fmt.Errorf("noncanonical list index %q", segment)
		}
	}
	index, err := strconv.Atoi(segment)
	if err != nil {
		return 0, fmt.Errorf("noncanonical list index %q", segment)
	}
	return index, nil
}

func appendPointer(parent, segment string) string {
	segment = strings.ReplaceAll(segment, "~", "~0")
	segment = strings.ReplaceAll(segment, "/", "~1")
	return parent + "/" + segment
}

func valuesEqual(a, b encodedvalue.Value) bool {
	aJSON, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bJSON, err := json.Marshal(b)
	return err == nil && string(aJSON) == string(bJSON)
}

type digestPayload struct {
	LibraryPath    string          `json:"library-path"`
	SchemaVersion  int             `json:"schema-version"`
	SchemaDigest   string          `json:"schema-digest"`
	SensitivePaths []string        `json:"sensitive-paths"`
	Value          json.RawMessage `json:"value"`
}

func Digest(record Record) (string, error) {
	ids := make(map[string]string, len(record.SensitiveValues))
	for _, sensitive := range record.SensitiveValues {
		ids[sensitive.Path] = sensitive.ID
	}
	value, err := appendDigestValue(nil, record.Value, "", ids)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(digestPayload{
		LibraryPath:    record.LibraryPath,
		SchemaVersion:  record.SchemaVersion,
		SchemaDigest:   record.SchemaDigest,
		SensitivePaths: append([]string{}, record.SensitivePaths...),
		Value:          value,
	})
	if err != nil {
		return "", fmt.Errorf("encode configuration digest: %w", err)
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func appendDigestValue(
	dst []byte,
	value encodedvalue.Value,
	path string,
	ids map[string]string,
) ([]byte, error) {
	if id, ok := ids[path]; ok {
		dst = append(dst, `{"kind":"sensitive","id":`...)
		dst = appendJSONString(dst, id)
		return append(dst, '}'), nil
	}
	switch value.Kind() {
	case encodedvalue.KindList:
		dst = append(dst, `{"kind":"list","items":[`...)
		items, _ := value.Items()
		for i, item := range items {
			if i > 0 {
				dst = append(dst, ',')
			}
			var err error
			dst, err = appendDigestValue(dst, item, appendPointer(path, strconv.Itoa(i)), ids)
			if err != nil {
				return nil, err
			}
		}
		return append(dst, ']', '}'), nil
	case encodedvalue.KindMap:
		return appendDigestNamed(dst, value, path, ids, "map", "entries", "key")
	case encodedvalue.KindObject:
		return appendDigestNamed(dst, value, path, ids, "object", "fields", "name")
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode configuration value: %w", err)
		}
		return append(dst, encoded...), nil
	}
}

func appendDigestNamed(
	dst []byte,
	value encodedvalue.Value,
	path string,
	ids map[string]string,
	kind, member, label string,
) ([]byte, error) {
	var values map[string]encodedvalue.Value
	if kind == "map" {
		values, _ = value.MapEntries()
	} else {
		values, _ = value.ObjectFields()
	}
	dst = append(dst, `{"kind":`...)
	dst = appendJSONString(dst, kind)
	dst = append(dst, ',')
	dst = appendJSONString(dst, member)
	dst = append(dst, ':', '[')
	for i, name := range slices.Sorted(maps.Keys(values)) {
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = append(dst, '{')
		dst = appendJSONString(dst, label)
		dst = append(dst, ':')
		dst = appendJSONString(dst, name)
		dst = append(dst, `,"value":`...)
		var err error
		dst, err = appendDigestValue(dst, values[name], appendPointer(path, name), ids)
		if err != nil {
			return nil, err
		}
		dst = append(dst, '}')
	}
	return append(dst, ']', '}'), nil
}

func appendJSONString(dst []byte, value string) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return append(dst, encoded...)
}
