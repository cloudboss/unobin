package runner

import (
	"fmt"
	"os"
	"strings"

	ufs "github.com/cloudboss/unobin/pkg/fs"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/spf13/cobra"
)

func newPinCmd(info Info) *cobra.Command {
	var (
		configPath      string
		versionOverride string
		commitOverride  string
	)
	cmd := &cobra.Command{
		Use:   "pin",
		Short: "Add this binary's identity to config.ub",
		RunE: func(cmd *cobra.Command, args []string) error {
			return doPin(cmd, info, configPath, versionOverride, commitOverride)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "",
		"Path to the config.ub to pin into.")
	cmd.Flags().StringVar(&versionOverride, "version", "",
		"Pin this version instead of the binary's own.")
	cmd.Flags().StringVar(&commitOverride, "commit", "",
		"Pin this commit instead of the binary's own.")
	return cmd
}

func doPin(
	cmd *cobra.Command, info Info, configPath, versionOverride, commitOverride string,
) error {
	if configPath == "" {
		return fmt.Errorf("--config is required")
	}
	version := versionOverride
	if version == "" {
		version = info.StackVersion
	}
	commit := commitOverride
	if commit == "" {
		commit = info.StackCommit
	}
	if version == "" || commit == "" {
		return fmt.Errorf(
			"this binary has no embedded version or commit; " +
				"pass --version and --commit to pin another binary's identity")
	}
	src, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	updated, action, err := pinFile(src, info.ModulePath, version, commit)
	if err != nil {
		return fmt.Errorf("config %s: %w", configPath, err)
	}
	if action == pinActionAlreadyPinned {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"%s already pins %s (commit %s).\n", configPath, version, commit)
		return nil
	}
	if err := ufs.WriteFileAtomic(configPath, updated, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(),
		"Pinned %s (commit %s) in %s (%s).\n", version, commit, configPath, action)
	return nil
}

// pinFile is the pure splice. It returns the updated source bytes plus a
// short human-readable action describing what changed; the returned
// action is pinActionAlreadyPinned when the entry was already present
// and the source bytes are unchanged.
func pinFile(src []byte, modulePath, version, commit string) ([]byte, string, error) {
	f, err := lang.ParseSource("config.ub", src)
	if err != nil {
		return nil, "", err
	}
	stackField := findField(f.Body, "stack")
	if stackField == nil {
		return prependStackBlock(src, modulePath, version, commit)
	}
	stackObj, ok := stackField.Value.(*lang.ObjectLit)
	if !ok {
		return nil, "", fmt.Errorf("`stack:` must be an object")
	}
	if mp := findField(stackObj, "module-path"); mp != nil {
		existing, ok := mp.Value.(*lang.StringLit)
		if !ok {
			return nil, "", fmt.Errorf("`stack.module-path` must be a string")
		}
		if modulePath != "" && existing.Value != modulePath {
			return nil, "", fmt.Errorf(
				"stack.module-path %q does not match this binary %q",
				existing.Value, modulePath)
		}
	}
	svField := findField(stackObj, "supported-versions")
	if svField == nil {
		return fillStackBlock(src, stackObj, modulePath, version, commit)
	}
	svArr, ok := svField.Value.(*lang.ArrayLit)
	if !ok {
		return nil, "", fmt.Errorf("`stack.supported-versions` must be a list")
	}
	for _, el := range svArr.Elements {
		if entryMatches(el, version, commit) {
			return src, pinActionAlreadyPinned, nil
		}
	}
	return appendVersionEntry(src, svField, svArr, version, commit)
}

func prependStackBlock(src []byte, modulePath, version, commit string) ([]byte, string, error) {
	block := renderStackBlock(modulePath, version, commit)
	if len(src) == 0 {
		return []byte(block), pinActionAddedStackBlock, nil
	}
	out := make([]byte, 0, len(block)+1+len(src))
	out = append(out, block...)
	out = append(out, '\n')
	out = append(out, src...)
	return out, pinActionAddedStackBlock, nil
}

// fillStackBlock inserts the missing `supported-versions:` (and
// `module-path:` if the binary has one and the block does not declare
// it) into an existing `stack:` block. An empty block is rewritten to
// canonical multi-line form so the new fields do not sit on the same
// line as the opening brace.
func fillStackBlock(
	src []byte, stackObj *lang.ObjectLit, modulePath, version, commit string,
) ([]byte, string, error) {
	openIdx := stackObj.S.Start.Offset
	closeIdx := findMatchingClose(src, openIdx)
	if closeIdx < 0 {
		return nil, "", fmt.Errorf("could not locate closing `}` of stack block")
	}
	parentIndent := lineIndent(src, openIdx)
	childInd := parentIndent + "  "
	if len(stackObj.Fields) > 0 {
		childInd = lineIndent(src, stackObj.Fields[0].S.Start.Offset)
	}
	var b strings.Builder
	if modulePath != "" && findField(stackObj, "module-path") == nil {
		fmt.Fprintf(&b, "%smodule-path: '%s'\n", childInd, modulePath)
	}
	fmt.Fprintf(&b, "%ssupported-versions: [\n%s  { version: '%s', commit: '%s' },\n%s]\n",
		childInd, childInd, version, commit, childInd)
	if len(stackObj.Fields) == 0 {
		return spliceReplace(src, openIdx+1, closeIdx, "\n"+b.String()+parentIndent),
			pinActionAddedSupportedVersions, nil
	}
	return spliceBefore(src, closeIdx, b.String()),
		pinActionAddedSupportedVersions, nil
}

func appendVersionEntry(
	src []byte, svField *lang.Field, svArr *lang.ArrayLit, version, commit string,
) ([]byte, string, error) {
	closeIdx := findMatchingClose(src, svArr.S.Start.Offset)
	if closeIdx < 0 {
		return nil, "", fmt.Errorf("could not locate closing `]` of supported-versions")
	}
	entry := fmt.Sprintf("{ version: '%s', commit: '%s' }", version, commit)
	if len(svArr.Elements) == 0 {
		base := lineIndent(src, svField.S.Start.Offset)
		body := fmt.Sprintf("\n%s  %s,\n%s", base, entry, base)
		return spliceReplace(src, svArr.S.Start.Offset+1, closeIdx, body),
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
// and `commit` string fields equal the given values.
func entryMatches(el lang.Expr, version, commit string) bool {
	obj, ok := el.(*lang.ObjectLit)
	if !ok {
		return false
	}
	var gotVersion, gotCommit string
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
		case "commit":
			gotCommit = s.Value
		}
	}
	return gotVersion == version && gotCommit == commit
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
	pinActionAddedStackBlock        = "added stack block"
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

// renderStackBlock returns the canonical full `stack:` block. The
// closing newline is included; callers append their own separator if
// they want a blank line between this block and the rest of the file.
// Entries always carry a trailing comma so the canonical shape matches
// the form `pin` produces when it appends later.
func renderStackBlock(modulePath, version, commit string) string {
	var b strings.Builder
	b.WriteString("stack: {\n")
	if modulePath != "" {
		fmt.Fprintf(&b, "  module-path: '%s'\n", modulePath)
	}
	fmt.Fprintf(&b,
		"  supported-versions: [\n    { version: '%s', commit: '%s' },\n  ]\n}\n",
		version, commit)
	return b.String()
}
