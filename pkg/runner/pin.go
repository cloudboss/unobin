package runner

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/spf13/cobra"
)

func newPinCmd(info Info) *cobra.Command {
	var (
		configPath              string
		versionOverride         string
		contentRevisionOverride string
	)
	cmd := &cobra.Command{
		Use:   "pin",
		Short: "Add this binary's identity to config.ub",
		RunE: func(cmd *cobra.Command, args []string) error {
			return doPin(cmd, info, configPath, versionOverride, contentRevisionOverride)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "",
		"Path to the config.ub to pin into.")
	cmd.Flags().StringVar(&versionOverride, "version", "",
		"Pin this version instead of the binary's own.")
	cmd.Flags().StringVar(&contentRevisionOverride, "content-revision", "",
		"Pin this content-revision instead of the binary's own.")
	return cmd
}

func doPin(
	cmd *cobra.Command, info Info, configPath, versionOverride, contentRevisionOverride string,
) error {
	if configPath == "" {
		return fmt.Errorf("--config is required")
	}
	version := versionOverride
	if version == "" {
		version = info.FactoryVersion
	}
	revision := contentRevisionOverride
	if revision == "" {
		revision = info.ContentRevision
	}
	if version == "" || revision == "" {
		return fmt.Errorf(
			"this binary has no embedded version or content-revision; " +
				"pass --version and --content-revision to pin another binary's identity")
	}
	src, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	updated, action, err := pinFile(src, info.LibraryPath, version, revision)
	if err != nil {
		return fmt.Errorf("config %s: %w", configPath, err)
	}
	if action == pinActionAlreadyPinned {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"%s already pins %s (content-revision %s).\n", configPath, version, revision)
		return nil
	}
	if err := lang.WriteCanonical(configPath, updated); err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(),
		"Pinned %s (content-revision %s) in %s (%s).\n", version, revision, configPath, action)
	return nil
}

// pinFile is the pure splice. It returns the updated source bytes plus a
// short human-readable action describing what changed; the returned
// action is pinActionAlreadyPinned when the entry was already present
// and the source bytes are unchanged.
func pinFile(src []byte, libraryPath, version, revision string) ([]byte, string, error) {
	f, err := lang.ParseSource("config.ub", src)
	if err != nil {
		return nil, "", err
	}
	factoryField := findField(f.Body, "factory")
	if factoryField == nil {
		return prependFactoryBlock(src, libraryPath, version, revision)
	}
	factoryObj, ok := factoryField.Value.(*lang.ObjectLit)
	if !ok {
		return nil, "", fmt.Errorf("`factory:` must be an object")
	}
	if mp := findField(factoryObj, "library-path"); mp != nil {
		existing, ok := mp.Value.(*lang.StringLit)
		if !ok {
			return nil, "", fmt.Errorf("`factory.library-path` must be a string")
		}
		if libraryPath != "" && existing.Value != libraryPath {
			return nil, "", fmt.Errorf(
				"factory.library-path %q does not match this binary %q",
				existing.Value, libraryPath)
		}
	}
	svField := findField(factoryObj, "supported-versions")
	if svField == nil {
		return fillFactoryBlock(src, factoryObj, libraryPath, version, revision)
	}
	svArr, ok := svField.Value.(*lang.ArrayLit)
	if !ok {
		return nil, "", fmt.Errorf("`factory.supported-versions` must be a list")
	}
	for _, el := range svArr.Elements {
		if entryMatches(el, version, revision) {
			return src, pinActionAlreadyPinned, nil
		}
	}
	return appendVersionEntry(src, svField, svArr, version, revision)
}

func prependFactoryBlock(src []byte, libraryPath, version, revision string) ([]byte, string, error) {
	block := renderFactoryBlock(libraryPath, version, revision)
	if len(src) == 0 {
		return []byte(block), pinActionAddedFactoryBlock, nil
	}
	out := make([]byte, 0, len(block)+1+len(src))
	out = append(out, block...)
	out = append(out, '\n')
	out = append(out, src...)
	return out, pinActionAddedFactoryBlock, nil
}

// fillFactoryBlock inserts the missing `supported-versions:` (and
// `library-path:` if the binary has one and the block does not declare
// it) into an existing `factory:` block. An empty block is rewritten to
// canonical multi-line form so the new fields do not sit on the same
// line as the opening brace.
func fillFactoryBlock(
	src []byte, factoryObj *lang.ObjectLit, libraryPath, version, revision string,
) ([]byte, string, error) {
	openIdx := factoryObj.S.Start.Offset
	closeIdx := findMatchingClose(src, openIdx)
	if closeIdx < 0 {
		return nil, "", fmt.Errorf("could not locate closing `}` of factory block")
	}
	parentIndent := lineIndent(src, openIdx)
	childInd := parentIndent + "  "
	if len(factoryObj.Fields) > 0 {
		childInd = lineIndent(src, factoryObj.Fields[0].S.Start.Offset)
	}
	var b strings.Builder
	if libraryPath != "" && findField(factoryObj, "library-path") == nil {
		fmt.Fprintf(&b, "%slibrary-path: '%s'\n", childInd, libraryPath)
	}
	fmt.Fprintf(&b, "%ssupported-versions: [\n%s  { version: '%s', content-revision: '%s' },\n%s]\n",
		childInd, childInd, version, revision, childInd)
	if len(factoryObj.Fields) == 0 {
		return spliceReplace(src, openIdx+1, closeIdx, "\n"+b.String()+parentIndent),
			pinActionAddedSupportedVersions, nil
	}
	return spliceBefore(src, closeIdx, b.String()),
		pinActionAddedSupportedVersions, nil
}

func appendVersionEntry(
	src []byte, svField *lang.Field, svArr *lang.ArrayLit, version, revision string,
) ([]byte, string, error) {
	openIdx := svArr.S.Start.Offset
	closeIdx := findMatchingClose(src, openIdx)
	if closeIdx < 0 {
		return nil, "", fmt.Errorf("could not locate closing `]` of supported-versions")
	}
	entry := fmt.Sprintf("{ version: '%s', content-revision: '%s' }", version, revision)
	base := lineIndent(src, svField.S.Start.Offset)
	// An empty or inline list is rewritten to the canonical multi-line
	// form. Inserting before the `]` of a single-line list would leave the
	// new entry as a bare object beside the field. Each existing entry
	// keeps its own source text.
	if len(svArr.Elements) == 0 || bytes.IndexByte(src[openIdx:closeIdx], '\n') < 0 {
		var b strings.Builder
		b.WriteByte('\n')
		for _, el := range svArr.Elements {
			sp := el.Span()
			fmt.Fprintf(&b, "%s  %s,\n", base, src[sp.Start.Offset:sp.End.Offset])
		}
		fmt.Fprintf(&b, "%s  %s,\n%s", base, entry, base)
		return spliceReplace(src, openIdx+1, closeIdx, b.String()),
			pinActionAppendedEntry, nil
	}
	entryIndent := lineIndent(src, svArr.Elements[0].Span().Start.Offset)
	// Walk back from `]` to the start of its line.
	closeLineStart := closeIdx
	for closeLineStart > 0 && src[closeLineStart-1] != '\n' {
		closeLineStart--
	}
	// Find the previous non-whitespace byte; if it is not a comma, the
	// preceding entry needs one before we add ours.
	prev := closeLineStart - 1
	for prev > 0 {
		c := src[prev]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			prev--
			continue
		}
		break
	}
	out := src
	insertAt := closeLineStart
	if prev >= 0 && src[prev] != ',' {
		out = spliceBefore(out, prev+1, ",")
		insertAt++
	}
	insert := fmt.Sprintf("%s%s,\n", entryIndent, entry)
	out = spliceBefore(out, insertAt, insert)
	return out, pinActionAppendedEntry, nil
}

// entryMatches reports whether el is an object literal whose `version`
// and `content-revision` string fields equal the given values.
func entryMatches(el lang.Expr, version, revision string) bool {
	obj, ok := el.(*lang.ObjectLit)
	if !ok {
		return false
	}
	var gotVersion, gotRevision string
	for _, fld := range obj.Fields {
		if fld.Key.Kind != lang.FieldIdent {
			continue
		}
		s, ok := fld.Value.(*lang.StringLit)
		if !ok {
			continue
		}
		switch fld.Key.Name {
		case "version":
			gotVersion = s.Value
		case "content-revision":
			gotRevision = s.Value
		}
	}
	return gotVersion == version && gotRevision == revision
}

// spliceBefore inserts text at idx.
func spliceBefore(src []byte, idx int, text string) []byte {
	out := make([]byte, 0, len(src)+len(text))
	out = append(out, src[:idx]...)
	out = append(out, text...)
	out = append(out, src[idx:]...)
	return out
}

// spliceReplace replaces src[start:end] with text.
func spliceReplace(src []byte, start, end int, text string) []byte {
	out := make([]byte, 0, len(src)-(end-start)+len(text))
	out = append(out, src[:start]...)
	out = append(out, text...)
	out = append(out, src[end:]...)
	return out
}

const (
	pinActionAddedFactoryBlock      = "added factory block"
	pinActionAddedSupportedVersions = "added supported-versions"
	pinActionAppendedEntry          = "appended entry"
	pinActionAlreadyPinned          = "already pinned"
)

// findField returns the field with the given identifier key from an
// ObjectLit, or nil when the field is not present.
func findField(obj *lang.ObjectLit, name string) *lang.Field {
	if obj == nil {
		return nil
	}
	for _, fld := range obj.Fields {
		if fld.Key.Kind == lang.FieldIdent && fld.Key.Name == name {
			return fld
		}
	}
	return nil
}

// findMatchingClose returns the index of the delimiter that closes the
// `{` or `[` at openIdx. It tracks single-quoted strings (which cover
// triple-quoted strings too, since their content is bracketed by `'`
// runs) and `#` line comments, so braces or brackets inside those tokens
// do not affect the depth count. Returns -1 if no match is found (which
// should not happen on parser-validated input).
func findMatchingClose(src []byte, openIdx int) int {
	open := src[openIdx]
	var closeCh byte
	switch open {
	case '{':
		closeCh = '}'
	case '[':
		closeCh = ']'
	default:
		return -1
	}
	depth := 1
	i := openIdx + 1
	for i < len(src) {
		switch src[i] {
		case '\'':
			i++
			for i < len(src) && src[i] != '\'' {
				if src[i] == '\\' && i+1 < len(src) {
					i += 2
					continue
				}
				i++
			}
		case '#':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		case open:
			depth++
		case closeCh:
			depth--
			if depth == 0 {
				return i
			}
		}
		i++
	}
	return -1
}

// lineIndent returns the run of spaces and tabs at the start of the
// line containing offset.
func lineIndent(src []byte, offset int) string {
	start := offset
	for start > 0 && src[start-1] != '\n' {
		start--
	}
	end := start
	for end < len(src) && (src[end] == ' ' || src[end] == '\t') {
		end++
	}
	return string(src[start:end])
}

// renderFactoryBlock returns the canonical full `factory:` block. The
// closing newline is included; callers append their own separator if
// they want a blank line between this block and the rest of the file.
// Entries always carry a trailing comma so the canonical shape matches
// the form `pin` produces when it appends later.
func renderFactoryBlock(libraryPath, version, revision string) string {
	var b strings.Builder
	b.WriteString("factory: {\n")
	if libraryPath != "" {
		fmt.Fprintf(&b, "  library-path: '%s'\n", libraryPath)
	}
	fmt.Fprintf(&b,
		"  supported-versions: [\n    { version: '%s', content-revision: '%s' },\n  ]\n}\n",
		version, revision)
	return b.String()
}
