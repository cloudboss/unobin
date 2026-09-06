package e2etest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var errJSONOutput = errors.New("e2e: JSON output")

var (
	contentRevisionJSONRE = regexp.MustCompile(`^[0-9a-f]{12}$`)
	planDigestJSONRE      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	runViewTokenJSONRE    = regexp.MustCompile(`^/[0-9a-f]{32}/$`)
)

type jsonNodeKind uint8

const (
	jsonNull jsonNodeKind = iota
	jsonObject
	jsonArray
	jsonString
	jsonNumber
	jsonBoolean
)

type jsonNode struct {
	kind    jsonNodeKind
	object  []jsonMember
	array   []*jsonNode
	text    string
	number  json.Number
	boolean bool
}

type jsonMember struct {
	name  string
	value *jsonNode
}

type jsonNormalizer struct {
	repoRoot     string
	workspace    string
	revisions    map[string]string
	nextRevision int
}

func normalizeJSONOutput(output, repoRoot, workspace string) (string, error) {
	if !strings.HasSuffix(output, "\n") {
		return "", jsonOutputError("final newline is required")
	}
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	normalizer := jsonNormalizer{
		repoRoot: repoRoot, workspace: workspace, revisions: map[string]string{},
	}
	var result strings.Builder
	for i, line := range lines {
		if line == "" {
			return "", jsonOutputError("line %d is blank", i+1)
		}
		node, err := decodeJSONLine(line)
		if err != nil {
			return "", jsonOutputError("line %d: %v", i+1, err)
		}
		if node.kind != jsonObject {
			return "", jsonOutputError("line %d: top-level value must be an object", i+1)
		}
		normalizer.normalizeAliases(node)
		if err := normalizer.normalizeDynamic(node); err != nil {
			return "", jsonOutputError("line %d: %v", i+1, err)
		}
		if err := writeJSONNode(&result, node); err != nil {
			return "", jsonOutputError("line %d: encode: %v", i+1, err)
		}
		result.WriteByte('\n')
	}
	return result.String(), nil
}

func jsonOutputError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errJSONOutput, fmt.Sprintf(format, args...))
}

func decodeJSONLine(line string) (*jsonNode, error) {
	decoder := json.NewDecoder(strings.NewReader(line))
	decoder.UseNumber()
	node, err := decodeJSONNode(decoder, "$")
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("more than one top-level value")
		}
		return nil, fmt.Errorf("trailing input: %w", err)
	}
	return node, nil
}

func decodeJSONNode(decoder *json.Decoder, path string) (*jsonNode, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			return decodeJSONObject(decoder, path)
		case '[':
			return decodeJSONArray(decoder, path)
		default:
			return nil, fmt.Errorf("unexpected delimiter %q", value)
		}
	case nil:
		return &jsonNode{kind: jsonNull}, nil
	case string:
		return &jsonNode{kind: jsonString, text: value}, nil
	case json.Number:
		return &jsonNode{kind: jsonNumber, number: value}, nil
	case bool:
		return &jsonNode{kind: jsonBoolean, boolean: value}, nil
	default:
		return nil, fmt.Errorf("unsupported token %T", token)
	}
}

func decodeJSONObject(decoder *json.Decoder, path string) (*jsonNode, error) {
	node := &jsonNode{kind: jsonObject}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		name, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("object name at %s is %T", path, token)
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate object name %q at %s", name, path)
		}
		seen[name] = true
		value, err := decodeJSONNode(decoder, path+"."+name)
		if err != nil {
			return nil, err
		}
		node.object = append(node.object, jsonMember{name: name, value: value})
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	return node, nil
}

func decodeJSONArray(decoder *json.Decoder, path string) (*jsonNode, error) {
	node := &jsonNode{kind: jsonArray}
	for index := 0; decoder.More(); index++ {
		value, err := decodeJSONNode(decoder, fmt.Sprintf("%s[%d]", path, index))
		if err != nil {
			return nil, err
		}
		node.array = append(node.array, value)
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	return node, nil
}

func (n *jsonNormalizer) normalizeAliases(node *jsonNode) {
	switch node.kind {
	case jsonObject:
		for _, member := range node.object {
			n.normalizeAliases(member.value)
		}
	case jsonArray:
		for _, value := range node.array {
			n.normalizeAliases(value)
		}
	case jsonString:
		node.text = replaceJSONAliases(node.text, n.repoRoot, n.workspace)
	}
}

func replaceJSONAliases(value, repoRoot, workspace string) string {
	type replacement struct {
		from string
		to   string
	}
	var replacements []replacement
	if repoRoot != "" {
		replacements = append(replacements, replacement{from: repoRoot, to: "<repo>"})
	}
	for _, alias := range workspaceAliases(workspace) {
		replacements = append(replacements, replacement{from: alias, to: "<workspace>"})
	}
	sort.SliceStable(replacements, func(i, j int) bool {
		return len(replacements[i].from) > len(replacements[j].from)
	})
	for _, replacement := range replacements {
		value = strings.ReplaceAll(value, replacement.from, replacement.to)
	}
	return value
}

func (n *jsonNormalizer) normalizeDynamic(root *jsonNode) error {
	kind, err := requiredJSONString(root, "kind")
	if err != nil {
		return err
	}
	if kind == "" {
		return errors.New("kind must not be empty")
	}
	version, ok := jsonField(root, "format-version")
	if !ok || version.kind != jsonNumber || !integerJSONNumber(version.number.String()) {
		return errors.New("format-version must be an integer")
	}
	if factory, ok := jsonField(root, "factory"); ok && factory.kind == jsonObject {
		if err := normalizeRevisionField(factory, "content-revision", "<revision>"); err != nil {
			return fmt.Errorf("factory.%w", err)
		}
	}
	if err := normalizeRevisionField(root, "content-revision", "<revision>"); err != nil {
		return err
	}
	if err := normalizePlanDigest(root); err != nil {
		return err
	}
	if err := normalizeNonEmptyString(root, "state-rev", func(string) string {
		return "<revision>"
	}); err != nil {
		return err
	}
	for _, field := range []string{"timestamp", "started-at", "finished-at"} {
		if err := normalizeTimeField(root, field); err != nil {
			return err
		}
	}
	if err := normalizeElapsedField(root); err != nil {
		return err
	}
	if err := normalizeCompileDiagnosticRevisions(root); err != nil {
		return err
	}
	switch kind {
	case "state-snapshots":
		if err := n.normalizeStateSnapshots(root); err != nil {
			return err
		}
	case "state-gc-result":
		for _, field := range []string{"current", "failed-revision"} {
			if err := normalizeNonEmptyString(root, field, n.numberedRevision); err != nil {
				return err
			}
		}
	case "apply-ui":
		if err := normalizeRunViewURL(root); err != nil {
			return err
		}
	}
	return nil
}

func normalizeCompileDiagnosticRevisions(root *jsonNode) error {
	diagnostics, ok := jsonField(root, "diagnostics")
	if !ok {
		return nil
	}
	if diagnostics.kind != jsonArray {
		return errors.New("diagnostics must be an array")
	}
	for index, value := range diagnostics.array {
		if value.kind != jsonObject {
			return fmt.Errorf("diagnostics[%d] must be an object", index)
		}
		code, ok := jsonField(value, "code")
		if !ok || code.kind != jsonString || code.text != "unobin.compile.built" {
			continue
		}
		message, ok := jsonField(value, "message")
		if !ok || message.kind != jsonString {
			return fmt.Errorf("diagnostics[%d].message must be a string", index)
		}
		message.text = contentRevisionTextRE.ReplaceAllString(
			message.text, "content-revision <revision>",
		)
	}
	return nil
}

func integerJSONNumber(value string) bool {
	if value == "" {
		return false
	}
	if value[0] == '-' {
		value = value[1:]
	}
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func requiredJSONString(object *jsonNode, name string) (string, error) {
	value, ok := jsonField(object, name)
	if !ok || value.kind != jsonString {
		return "", fmt.Errorf("%s must be a string", name)
	}
	return value.text, nil
}

func jsonField(object *jsonNode, name string) (*jsonNode, bool) {
	if object == nil || object.kind != jsonObject {
		return nil, false
	}
	for _, member := range object.object {
		if member.name == name {
			return member.value, true
		}
	}
	return nil, false
}

func normalizeRevisionField(object *jsonNode, name, replacement string) error {
	value, ok := jsonField(object, name)
	if !ok || value.kind == jsonNull {
		return nil
	}
	if value.kind != jsonString || !contentRevisionJSONRE.MatchString(value.text) {
		return fmt.Errorf("%s must be 12 lowercase hexadecimal characters", name)
	}
	value.text = replacement
	return nil
}

func normalizePlanDigest(root *jsonNode) error {
	value, ok := jsonField(root, "plan-digest")
	if !ok || value.kind == jsonNull {
		return nil
	}
	if value.kind != jsonString || !planDigestJSONRE.MatchString(value.text) {
		return errors.New("plan-digest must be sha256 followed by 64 lowercase hexadecimal characters")
	}
	value.text = "sha256:<digest>"
	return nil
}

func normalizeNonEmptyString(
	object *jsonNode,
	name string,
	replace func(string) string,
) error {
	value, ok := jsonField(object, name)
	if !ok || value.kind == jsonNull {
		return nil
	}
	if value.kind != jsonString || value.text == "" {
		return fmt.Errorf("%s must be a non-empty string", name)
	}
	value.text = replace(value.text)
	return nil
}

func normalizeTimeField(root *jsonNode, name string) error {
	value, ok := jsonField(root, name)
	if !ok || value.kind == jsonNull {
		return nil
	}
	if value.kind != jsonString || !strings.HasSuffix(value.text, "Z") {
		return fmt.Errorf("%s must be an RFC 3339 Nano UTC timestamp", name)
	}
	if _, err := time.Parse(time.RFC3339Nano, value.text); err != nil {
		return fmt.Errorf("%s must be an RFC 3339 Nano UTC timestamp", name)
	}
	value.text = "<timestamp>"
	return nil
}

func normalizeElapsedField(root *jsonNode) error {
	value, ok := jsonField(root, "elapsed")
	if !ok || value.kind == jsonNull {
		return nil
	}
	if value.kind != jsonString || len(value.text) > 32 {
		return errors.New("elapsed must be a valid short duration")
	}
	duration, err := time.ParseDuration(value.text)
	if err != nil || duration < 0 {
		return errors.New("elapsed must be a valid short duration")
	}
	value.text = "<elapsed>"
	return nil
}

func (n *jsonNormalizer) normalizeStateSnapshots(root *jsonNode) error {
	if err := normalizeNonEmptyString(root, "current", n.numberedRevision); err != nil {
		return err
	}
	snapshots, ok := jsonField(root, "snapshots")
	if !ok {
		return nil
	}
	if snapshots.kind != jsonArray {
		return errors.New("snapshots must be an array")
	}
	for i, snapshot := range snapshots.array {
		if snapshot.kind != jsonObject {
			return fmt.Errorf("snapshots[%d] must be an object", i)
		}
		if err := normalizeNonEmptyString(snapshot, "revision", n.numberedRevision); err != nil {
			return fmt.Errorf("snapshots[%d].%w", i, err)
		}
	}
	return nil
}

func (n *jsonNormalizer) numberedRevision(revision string) string {
	if normalized, ok := n.revisions[revision]; ok {
		return normalized
	}
	n.nextRevision++
	normalized := fmt.Sprintf("<revision-%d>", n.nextRevision)
	n.revisions[revision] = normalized
	return normalized
}

func normalizeRunViewURL(root *jsonNode) error {
	value, ok := jsonField(root, "url")
	if !ok || value.kind == jsonNull {
		return nil
	}
	if value.kind != jsonString {
		return errors.New("url must be a loopback HTTP run-view URL")
	}
	parsed, err := url.Parse(value.text)
	if err != nil || parsed.Scheme != "http" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("url must be a loopback HTTP run-view URL")
	}
	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil || host != "127.0.0.1" || port == "" ||
		!runViewTokenJSONRE.MatchString(parsed.Path) {
		return errors.New("url must be a loopback HTTP run-view URL")
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return errors.New("url must be a loopback HTTP run-view URL")
	}
	value.text = "<run-view>"
	return nil
}

func writeJSONNode(out *strings.Builder, node *jsonNode) error {
	switch node.kind {
	case jsonNull:
		out.WriteString("null")
	case jsonObject:
		out.WriteByte('{')
		for i, member := range node.object {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := writeJSONString(out, member.name); err != nil {
				return err
			}
			out.WriteByte(':')
			if err := writeJSONNode(out, member.value); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	case jsonArray:
		out.WriteByte('[')
		for i, value := range node.array {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := writeJSONNode(out, value); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case jsonString:
		return writeJSONString(out, node.text)
	case jsonNumber:
		out.WriteString(node.number.String())
	case jsonBoolean:
		out.WriteString(strconv.FormatBool(node.boolean))
	default:
		return fmt.Errorf("unknown JSON node kind %d", node.kind)
	}
	return nil
}

func writeJSONString(out *strings.Builder, value string) error {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return err
	}
	out.Write(bytes.TrimSuffix(encoded.Bytes(), []byte{'\n'}))
	return nil
}
