package value

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
)

type jsonMember struct {
	name  string
	value json.RawMessage
}

func Decode(data []byte) (Value, error) {
	return decodeValue(data, "$")
}

func (v Value) MarshalJSON() ([]byte, error) {
	if err := v.validate("$"); err != nil {
		return nil, err
	}
	return v.appendJSON(nil), nil
}

func (v *Value) UnmarshalJSON(data []byte) error {
	decoded, err := decodeValue(data, "$")
	if err != nil {
		return err
	}
	*v = decoded
	return nil
}

func (v Value) appendJSON(dst []byte) []byte {
	dst = append(dst, `{"kind":`...)
	dst = appendJSONString(dst, string(v.kind))
	switch v.kind {
	case KindAbsent, KindNull:
	case KindBoolean:
		dst = append(dst, `,"value":`...)
		dst = strconv.AppendBool(dst, v.boolean)
	case KindString:
		dst = append(dst, `,"value":`...)
		dst = appendJSONString(dst, v.text)
	case KindInteger:
		dst = append(dst, `,"value":`...)
		dst = appendJSONString(dst, strconv.FormatInt(v.integer, 10))
	case KindNumber:
		dst = append(dst, `,"value":`...)
		dst = appendJSONString(dst, strconv.FormatFloat(v.number, 'g', -1, 64))
	case KindList:
		dst = append(dst, `,"items":[`...)
		for i, item := range v.items {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = item.appendJSON(dst)
		}
		dst = append(dst, ']')
	case KindMap:
		dst = appendNamedValues(dst, "entries", "key", v.named)
	case KindObject:
		dst = appendNamedValues(dst, "fields", "name", v.named)
	case KindPending:
		dst = append(dst, `,"refs":[`...)
		for i, ref := range v.refs {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendJSONString(dst, ref)
		}
		dst = append(dst, ']')
	}
	return append(dst, '}')
}

func appendNamedValues(dst []byte, member, label string, values []namedValue) []byte {
	dst = append(dst, ',')
	dst = appendJSONString(dst, member)
	dst = append(dst, ':', '[')
	for i, item := range values {
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = append(dst, '{')
		dst = appendJSONString(dst, label)
		dst = append(dst, ':')
		dst = appendJSONString(dst, item.name)
		dst = append(dst, `,"value":`...)
		dst = item.value.appendJSON(dst)
		dst = append(dst, '}')
	}
	return append(dst, ']')
}

func appendJSONString(dst []byte, value string) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return append(dst, encoded...)
}

func decodeValue(data []byte, path string) (Value, error) {
	members, err := decodeObject(data, path)
	if err != nil {
		return Value{}, err
	}
	kindRaw, ok := memberValue(members, "kind")
	if !ok {
		return Value{}, fmt.Errorf("%s.kind: member is required", path)
	}
	kindText, err := decodeString(kindRaw, path+".kind")
	if err != nil {
		return Value{}, err
	}
	kind := Kind(kindText)
	if !validKind(kind) {
		return Value{}, fmt.Errorf("%s.kind: unknown kind %q", path, kind)
	}

	payloadName := payloadMember(kind)
	allowed := map[string]bool{"kind": true}
	if payloadName != "" {
		allowed[payloadName] = true
	}
	if err := rejectUnknownMembers(members, allowed, path); err != nil {
		return Value{}, err
	}
	if payloadName == "" {
		return Value{kind: kind}, nil
	}
	payload, ok := memberValue(members, payloadName)
	if !ok {
		return Value{}, fmt.Errorf("%s.%s: member is required", path, payloadName)
	}
	payloadPath := path + "." + payloadName

	switch kind {
	case KindBoolean:
		v, err := decodeBoolean(payload, payloadPath)
		if err != nil {
			return Value{}, err
		}
		return Boolean(v), nil
	case KindString:
		v, err := decodeString(payload, payloadPath)
		if err != nil {
			return Value{}, err
		}
		return String(v), nil
	case KindInteger:
		return decodeInteger(payload, payloadPath)
	case KindNumber:
		return decodeNumber(payload, payloadPath)
	case KindList:
		return decodeList(payload, payloadPath)
	case KindMap:
		return decodeNamedValue(kind, payload, payloadPath, "key")
	case KindObject:
		return decodeNamedValue(kind, payload, payloadPath, "name")
	case KindPending:
		return decodePending(payload, payloadPath)
	}
	panic("unreachable")
}

func validKind(kind Kind) bool {
	switch kind {
	case KindAbsent, KindNull, KindBoolean, KindString, KindInteger,
		KindNumber, KindList, KindMap, KindObject, KindPending:
		return true
	default:
		return false
	}
}

func payloadMember(kind Kind) string {
	switch kind {
	case KindAbsent, KindNull:
		return ""
	case KindList:
		return "items"
	case KindMap:
		return "entries"
	case KindObject:
		return "fields"
	case KindPending:
		return "refs"
	default:
		return "value"
	}
}

func decodeInteger(data []byte, path string) (Value, error) {
	text, err := decodeString(data, path)
	if err != nil {
		return Value{}, err
	}
	v, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return Value{}, fmt.Errorf("%s: invalid integer %q", path, text)
	}
	if strconv.FormatInt(v, 10) != text {
		return Value{}, fmt.Errorf("%s: noncanonical integer %q", path, text)
	}
	return Integer(v), nil
}

func decodeNumber(data []byte, path string) (Value, error) {
	text, err := decodeString(data, path)
	if err != nil {
		return Value{}, err
	}
	v, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return Value{}, fmt.Errorf("%s: invalid number %q", path, text)
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return Value{}, fmt.Errorf("%s: number must be finite", path)
	}
	if strconv.FormatFloat(v, 'g', -1, 64) != text {
		return Value{}, fmt.Errorf("%s: noncanonical number %q", path, text)
	}
	return Value{kind: KindNumber, number: v}, nil
}

func decodeList(data []byte, path string) (Value, error) {
	rawItems, err := decodeArray(data, path)
	if err != nil {
		return Value{}, err
	}
	items := make([]Value, 0, len(rawItems))
	for i, raw := range rawItems {
		item, err := decodeValue(raw, fmt.Sprintf("%s[%d]", path, i))
		if err != nil {
			return Value{}, err
		}
		items = append(items, item)
	}
	return Value{kind: KindList, items: items}, nil
}

func decodeNamedValue(kind Kind, data []byte, path, label string) (Value, error) {
	rawItems, err := decodeArray(data, path)
	if err != nil {
		return Value{}, err
	}
	items := make([]namedValue, 0, len(rawItems))
	for i, raw := range rawItems {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		members, err := decodeObject(raw, itemPath)
		if err != nil {
			return Value{}, err
		}
		if err := rejectUnknownMembers(
			members,
			map[string]bool{label: true, "value": true},
			itemPath,
		); err != nil {
			return Value{}, err
		}
		nameRaw, ok := memberValue(members, label)
		if !ok {
			return Value{}, fmt.Errorf("%s.%s: member is required", itemPath, label)
		}
		name, err := decodeString(nameRaw, itemPath+"."+label)
		if err != nil {
			return Value{}, err
		}
		if len(items) > 0 {
			previous := items[len(items)-1].name
			switch {
			case name == previous:
				return Value{}, fmt.Errorf("%s.%s: duplicate %s", itemPath, label, label)
			case name < previous:
				memberName := "entries"
				if kind == KindObject {
					memberName = "fields"
				}
				return Value{}, fmt.Errorf(
					"%s.%s: %s are not sorted", itemPath, label, memberName,
				)
			}
		}
		valueRaw, ok := memberValue(members, "value")
		if !ok {
			return Value{}, fmt.Errorf("%s.value: member is required", itemPath)
		}
		itemValue, err := decodeValue(valueRaw, itemPath+".value")
		if err != nil {
			return Value{}, err
		}
		items = append(items, namedValue{name: name, value: itemValue})
	}
	return Value{kind: kind, named: items}, nil
}

func decodePending(data []byte, path string) (Value, error) {
	rawRefs, err := decodeArray(data, path)
	if err != nil {
		return Value{}, err
	}
	if len(rawRefs) == 0 {
		return Value{}, fmt.Errorf("%s: at least one reference is required", path)
	}
	refs := make([]string, 0, len(rawRefs))
	for i, raw := range rawRefs {
		refPath := fmt.Sprintf("%s[%d]", path, i)
		ref, err := decodeString(raw, refPath)
		if err != nil {
			return Value{}, err
		}
		if ref == "" {
			return Value{}, fmt.Errorf("%s: reference is empty", refPath)
		}
		if len(refs) > 0 {
			previous := refs[len(refs)-1]
			switch {
			case ref == previous:
				return Value{}, fmt.Errorf("%s: duplicate reference", refPath)
			case ref < previous:
				return Value{}, fmt.Errorf("%s: references are not sorted", refPath)
			}
		}
		refs = append(refs, ref)
	}
	return Value{kind: KindPending, refs: refs}, nil
}

func decodeObject(data []byte, path string) ([]jsonMember, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	token, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("%s: expected object: %w", path, err)
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return nil, fmt.Errorf("%s: expected object", path)
	}

	members := []jsonMember{}
	seen := map[string]bool{}
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%s: decode member name: %w", path, err)
		}
		name, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("%s: member name is not a string", path)
		}
		if seen[name] {
			return nil, fmt.Errorf("%s.%s: duplicate member", path, name)
		}
		seen[name] = true

		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, fmt.Errorf("%s.%s: decode value: %w", path, name, err)
		}
		members = append(members, jsonMember{name: name, value: raw})
	}
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("%s: decode object: %w", path, err)
	}
	if err := requireEOF(dec, path); err != nil {
		return nil, err
	}
	return members, nil
}

func decodeArray(data []byte, path string) ([]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	token, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("%s: expected array: %w", path, err)
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '[' {
		return nil, fmt.Errorf("%s: expected array", path)
	}

	values := []json.RawMessage{}
	for dec.More() {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, fmt.Errorf("%s[%d]: decode value: %w", path, len(values), err)
		}
		values = append(values, raw)
	}
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("%s: decode array: %w", path, err)
	}
	if err := requireEOF(dec, path); err != nil {
		return nil, err
	}
	return values, nil
}

func requireEOF(dec *json.Decoder, path string) error {
	if _, err := dec.Token(); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("%s: decode JSON: %w", path, err)
	}
	return fmt.Errorf("%s: unexpected value after JSON value", path)
}

func memberValue(members []jsonMember, name string) (json.RawMessage, bool) {
	for _, member := range members {
		if member.name == name {
			return member.value, true
		}
	}
	return nil, false
}

func rejectUnknownMembers(members []jsonMember, allowed map[string]bool, path string) error {
	for _, member := range members {
		if !allowed[member.name] {
			return fmt.Errorf("%s.%s: unknown member", path, member.name)
		}
	}
	return nil
}

func decodeString(data []byte, path string) (string, error) {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return "", fmt.Errorf("%s: expected string", path)
	}
	return value, nil
}

func decodeBoolean(data []byte, path string) (bool, error) {
	var value bool
	if err := json.Unmarshal(data, &value); err != nil {
		return false, fmt.Errorf("%s: expected boolean", path)
	}
	return value, nil
}
