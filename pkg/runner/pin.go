package runner

import (
	"fmt"
	"os"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
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
		Short: "Add this binary's identity to a stack file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return doPin(cmd, info, configPath, versionOverride, contentRevisionOverride)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "",
		"Path to the stack file to pin into.")
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
	f, err := lang.ParseSource("stack.ub", src)
	if err != nil {
		return nil, "", err
	}
	if stackField := findField(f.Body, "stack"); stackField != nil {
		return pinSourceStackFile(src, f, stackField, libraryPath, version, revision)
	}
	f.Kind = lang.FileConfig
	if errs := lang.ValidateFile(f); errs.Len() > 0 {
		return nil, "", errs.Err()
	}
	factoryField := findField(f.Body, "factory")
	if factoryField == nil {
		return prependFactoryBlock(src, libraryPath, version, revision)
	}
	return pinFactoryField(src, factoryField, libraryPath, version, revision)
}

func pinSourceStackFile(
	src []byte,
	f *lang.File,
	stackField *lang.Field,
	libraryPath string,
	version string,
	revision string,
) ([]byte, string, error) {
	sf, serrs := syntax.LowerFile(f)
	if serrs.Len() > 0 {
		return nil, "", serrs.Err()
	}
	if sf.Kind != syntax.FileStack {
		return nil, "", fmt.Errorf("`stack:` must be the only top-level declaration")
	}
	if verrs := syntax.ValidateFile(sf); verrs.Len() > 0 {
		return nil, "", verrs.Err()
	}
	stackObj, ok := stackField.Value.(*lang.ObjectLit)
	if !ok {
		return nil, "", fmt.Errorf("`stack:` must be an object")
	}
	factoryField := findField(stackObj, "factory")
	if factoryField == nil {
		out, err := spliceIntoBlock(src, stackObj,
			renderFactoryBlock(libraryPath, version, revision), "stack block")
		if err != nil {
			return nil, "", err
		}
		return out, pinActionAddedFactoryBlock, nil
	}
	return pinFactoryField(src, factoryField, libraryPath, version, revision)
}

func pinFactoryField(
	src []byte,
	factoryField *lang.Field,
	libraryPath string,
	version string,
	revision string,
) ([]byte, string, error) {
	factoryObj, ok := factoryField.Value.(*lang.ObjectLit)
	if !ok {
		return nil, "", fmt.Errorf("`factory:` must be an object")
	}
	pinField := findField(factoryObj, "pin")
	if pinField == nil {
		out, err := spliceIntoBlock(src, factoryObj,
			renderPinBlock(libraryPath, version, revision), "factory block")
		if err != nil {
			return nil, "", err
		}
		return out, pinActionAddedPin, nil
	}
	pinObj, ok := pinField.Value.(*lang.ObjectLit)
	if !ok {
		return nil, "", fmt.Errorf("`factory.pin:` must be an object")
	}
	if mp := findField(pinObj, "library-path"); mp != nil {
		existing, ok := mp.Value.(*lang.StringLit)
		if !ok {
			return nil, "", fmt.Errorf("`factory.pin.library-path` must be a string")
		}
		if libraryPath != "" && existing.Value != libraryPath {
			return nil, "", fmt.Errorf(
				"factory.pin.library-path %q does not match this binary %q",
				existing.Value, libraryPath)
		}
	}
	svField := findField(pinObj, "supported-versions")
	if svField == nil {
		return fillPinBlock(src, pinObj, libraryPath, version, revision)
	}
	svArr, ok := svField.Value.(*lang.ArrayLit)
	if !ok {
		return nil, "", fmt.Errorf("`factory.pin.supported-versions` must be a list")
	}
	for _, el := range svArr.Elements {
		if entryMatches(el, version, revision) {
			return src, pinActionAlreadyPinned, nil
		}
	}
	return appendVersionEntry(src, svArr, version, revision)
}

func prependFactoryBlock(
	src []byte,
	libraryPath string,
	version string,
	revision string,
) ([]byte, string, error) {
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

// fillPinBlock inserts the missing `supported-versions:` (and
// `library-path:` if the binary has one and the block does not declare
// it) into an existing `factory.pin:` block.
func fillPinBlock(
	src []byte, pinObj *lang.ObjectLit, libraryPath, version, revision string,
) ([]byte, string, error) {
	var b strings.Builder
	if libraryPath != "" && findField(pinObj, "library-path") == nil {
		fmt.Fprintf(&b, "library-path: '%s'\n", libraryPath)
	}
	fmt.Fprintf(&b, "supported-versions: [\n{ version: '%s', content-revision: '%s' },\n]\n",
		version, revision)
	out, err := spliceIntoBlock(src, pinObj, b.String(), "pin block")
	if err != nil {
		return nil, "", err
	}
	return out, pinActionAddedSupportedVersions, nil
}

// spliceIntoBlock inserts body on a new line after the object
// literal's last content byte (the opening brace when the block is
// empty). The result is parseable, not pretty: splice output reaches
// disk through WriteCanonical, so the formatter owns indentation and
// alignment, and the body's own line breaks are what keep its blocks
// expanded. Inserting after the content rather than before the closing
// brace avoids leaving a blank line, which the formatter would keep.
// closeName names the block in the error when the closing brace cannot
// be found.
func spliceIntoBlock(src []byte, obj *lang.ObjectLit, body, closeName string) ([]byte, error) {
	openIdx := obj.S.Start.Offset
	closeIdx := findMatchingClose(src, openIdx)
	if closeIdx < 0 {
		return nil, fmt.Errorf("could not locate closing `}` of %s", closeName)
	}
	prev := closeIdx - 1
	for prev > openIdx && isBlank(src[prev]) {
		prev--
	}
	return spliceBefore(src, prev+1, "\n"+strings.TrimSuffix(body, "\n")), nil
}

func isBlank(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

// appendVersionEntry adds one entry to an existing supported-versions
// list, giving the preceding entry the comma the array grammar needs
// when its source text lacks one. The entry goes on its own new line
// directly after the last content byte, so an empty list comes out
// author-expanded and no blank line is left for the formatter to keep.
func appendVersionEntry(
	src []byte, svArr *lang.ArrayLit, version, revision string,
) ([]byte, string, error) {
	openIdx := svArr.S.Start.Offset
	closeIdx := findMatchingClose(src, openIdx)
	if closeIdx < 0 {
		return nil, "", fmt.Errorf("could not locate closing `]` of supported-versions")
	}
	prev := closeIdx - 1
	for prev > openIdx && isBlank(src[prev]) {
		prev--
	}
	insert := fmt.Sprintf("\n{ version: '%s', content-revision: '%s' },", version, revision)
	if src[prev] != '[' && src[prev] != ',' {
		insert = "," + insert
	}
	return spliceBefore(src, prev+1, insert), pinActionAppendedEntry, nil
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
	pinActionAddedPin               = "added pin block"
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

// renderPinBlock returns the `pin:` sub-block as a draft for the
// formatter: line breaks mark the blocks that stay expanded, and
// indentation is left to Canonicalize.
func renderPinBlock(libraryPath, version, revision string) string {
	var b strings.Builder
	b.WriteString("pin: {\n")
	if libraryPath != "" {
		fmt.Fprintf(&b, "library-path: '%s'\n", libraryPath)
	}
	fmt.Fprintf(&b, "supported-versions: [\n{ version: '%s', content-revision: '%s' },\n]\n",
		version, revision)
	b.WriteString("}\n")
	return b.String()
}

// renderFactoryBlock returns a draft `factory:` block with its pin
// sub-block, in the same formatter-bound form as renderPinBlock.
func renderFactoryBlock(libraryPath, version, revision string) string {
	return "factory: {\n" + renderPinBlock(libraryPath, version, revision) + "}\n"
}
