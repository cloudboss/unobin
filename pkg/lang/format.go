package lang

import (
	"fmt"
	"math"
	"strings"
)

// Format renders a parsed File back to canonical UB source. Comments
// captured during parsing are interleaved at their original
// positions; non-comment whitespace is normalized. Output is stable:
// re-parsing the result and feeding it back through Format yields the
// same bytes.
func Format(file *File) ([]byte, error) {
	w := &formatter{comments: file.Comments}
	if err := w.writeFile(file); err != nil {
		return nil, err
	}
	return []byte(w.buf.String()), nil
}

type formatter struct {
	buf      strings.Builder
	comments []Comment
	cIdx     int
	lastLine int
}

const fmtStep = "  "

func (w *formatter) writeFile(file *File) error {
	if file.Body == nil {
		return nil
	}
	if err := w.writeFields(file.Body.Fields, ""); err != nil {
		return err
	}
	w.flushBefore(math.MaxInt, "")
	return nil
}

// writeFields emits a list of object fields, applying value-column
// alignment within each group of consecutive single-line fields. A
// blank line in the source or a multi-line field breaks a group;
// intervening line comments do not.
func (w *formatter) writeFields(fields []*Field, indent string) error {
	i := 0
	for i < len(fields) {
		end, keyCol := w.findAlignmentGroup(fields, i)
		for k := i; k < end; k++ {
			field := fields[k]
			w.flushBefore(field.S.Start.Offset, indent)
			w.maybeBlankLine(field.S.Start.Line)
			if err := w.writeField(field, indent, keyCol); err != nil {
				return err
			}
			w.lastLine = valueEndLine(field.Value)
			w.flushTrailingOnLine(w.lastLine)
			w.buf.WriteByte('\n')
		}
		i = end
	}
	return nil
}

// findAlignmentGroup returns the half-open range [start, end) of
// fields that form an alignment group together with the max rendered
// key length to pad each member to. A group starts at start and
// extends while each next field is single-line and shares no blank
// source line with its predecessor.
func (w *formatter) findAlignmentGroup(fields []*Field, start int) (int, int) {
	maxKey := keyWidth(fields[start].Key)
	if !isSingleLineField(fields[start]) {
		return start + 1, maxKey
	}
	end := start + 1
	for end < len(fields) {
		if !isSingleLineField(fields[end]) {
			break
		}
		if w.hasBlankLineBetween(fields[end-1], fields[end]) {
			break
		}
		if k := keyWidth(fields[end].Key); k > maxKey {
			maxKey = k
		}
		end++
	}
	return end, maxKey
}

// hasBlankLineBetween reports whether the source has at least one
// blank line between prev and next. The walk considers comments that
// fall between the two fields so a run of comment-only lines does not
// look like a blank.
func (w *formatter) hasBlankLineBetween(prev, next *Field) bool {
	cursorLine := valueEndLine(prev.Value)
	nextOff := next.S.Start.Offset
	for k := w.cIdx; k < len(w.comments); k++ {
		c := w.comments[k]
		if c.S.Start.Offset <= prev.S.Start.Offset {
			continue
		}
		if c.S.Start.Offset >= nextOff {
			break
		}
		if c.S.Start.Line-cursorLine > 1 {
			return true
		}
		cursorLine = c.S.Start.Line
	}
	return next.S.Start.Line-cursorLine > 1
}

// writeField emits one field, padding the key column so the value
// starts at the same column as every other field in the group.
func (w *formatter) writeField(field *Field, indent string, keyCol int) error {
	w.buf.WriteString(indent)
	rendered := RenderKey(fieldKeyString(field.Key))
	w.buf.WriteString(rendered)
	w.buf.WriteByte(':')
	for n := keyCol - len(rendered) + 1; n > 0; n-- {
		w.buf.WriteByte(' ')
	}
	return w.writeExpr(field.Value, indent)
}

// isSingleLineField reports whether a field's value renders on a
// single line. Empty collections still count as single-line; only
// non-empty objects, non-empty arrays, multi-line strings, and
// non-empty type-object literals expand onto multiple lines.
func isSingleLineField(field *Field) bool {
	switch x := field.Value.(type) {
	case *ObjectLit:
		return len(x.Fields) == 0
	case *ArrayLit:
		return len(x.Elements) == 0
	case *StringLit:
		return !x.Form.IsMultiLine()
	case *TypeObject:
		return len(x.Fields) == 0
	}
	return true
}

func keyWidth(k FieldKey) int {
	return len(RenderKey(fieldKeyString(k)))
}

func fieldKeyString(k FieldKey) string {
	if k.Kind == FieldString {
		return k.String
	}
	return k.Name
}

func (w *formatter) writeExpr(expr Expr, indent string) error {
	switch x := expr.(type) {
	case *StringLit:
		return w.writeString(x, indent)
	case *NumberLit:
		w.buf.WriteString(x.Value)
	case *BoolLit:
		if x.Value {
			w.buf.WriteString("true")
		} else {
			w.buf.WriteString("false")
		}
	case *NullLit:
		w.buf.WriteString("null")
	case *Ident:
		w.buf.WriteString(x.Name)
	case *ObjectLit:
		return w.writeObject(x, indent)
	case *ArrayLit:
		return w.writeArray(x, indent)
	case *DotPath:
		return w.writeDotPath(x, indent)
	case *Call:
		return w.writeCall(x, indent)
	case *Infix:
		return w.writeInfix(x, indent)
	case *Prefix:
		return w.writePrefix(x, indent)
	case TypeExpr:
		return w.writeTypeExpr(x, indent)
	case nil:
		w.buf.WriteString("null")
	default:
		return fmt.Errorf("format: unsupported expression %T", expr)
	}
	return nil
}

func (w *formatter) writeString(s *StringLit, indent string) error {
	switch {
	case s.Form == StringBacktickSingleLine && canBacktickSingleLine(s.Value):
		w.buf.WriteByte('`')
		w.buf.WriteString(s.Value)
		w.buf.WriteByte('`')
		return nil
	case s.Form.IsMultiLine():
		return w.writeMultilineString(s, indent)
	}
	w.buf.WriteString(renderString(s.Value))
	return nil
}

func canBacktickSingleLine(v string) bool {
	return !strings.ContainsAny(v, "`\n\r")
}

func (w *formatter) writeMultilineString(s *StringLit, indent string) error {
	switch s.Form {
	case StringLiteralClip:
		return w.writeLiteralBacktick(s.Value, indent, true)
	case StringLiteralStrip:
		return w.writeLiteralBacktick(s.Value, indent, false)
	case StringFoldedClip:
		return w.writeFoldedBacktick(s.Value, indent, true)
	case StringFoldedStrip:
		return w.writeFoldedBacktick(s.Value, indent, false)
	case StringJoinedClip:
		return w.writeJoinedBacktick(s.Value, indent, true)
	case StringJoinedStrip:
		return w.writeJoinedBacktick(s.Value, indent, false)
	}
	return fmt.Errorf("format: unexpected string form %v", s.Form)
}

const fmtMaxColumn = 100

func (w *formatter) writeLiteralBacktick(value, indent string, clip bool) error {
	body := value
	sigil := "|-"
	if clip {
		body = strings.TrimSuffix(body, "\n")
		sigil = "|"
	}
	inner := indent + fmtStep
	w.buf.WriteByte('`')
	w.buf.WriteString(sigil)
	w.buf.WriteByte('\n')
	for _, line := range strings.Split(body, "\n") {
		if line == "" {
			w.buf.WriteByte('\n')
			continue
		}
		w.buf.WriteString(inner)
		w.buf.WriteString(line)
		w.buf.WriteByte('\n')
	}
	w.buf.WriteString(inner)
	w.buf.WriteByte('`')
	return nil
}

func (w *formatter) writeFoldedBacktick(value, indent string, clip bool) error {
	body := value
	sigil := ">-"
	if clip {
		body = strings.TrimSuffix(body, "\n")
		sigil = ">"
	}
	inner := indent + fmtStep
	w.buf.WriteByte('`')
	w.buf.WriteString(sigil)
	w.buf.WriteByte('\n')
	width := fmtMaxColumn - len(inner)
	if width < 1 {
		width = 1
	}
	for i, seg := range strings.Split(body, "\n") {
		if i > 0 {
			w.buf.WriteByte('\n')
		}
		if seg == "" {
			continue
		}
		for _, line := range wordWrap(seg, width) {
			w.buf.WriteString(inner)
			w.buf.WriteString(line)
			w.buf.WriteByte('\n')
		}
	}
	w.buf.WriteString(inner)
	w.buf.WriteByte('`')
	return nil
}

func (w *formatter) writeJoinedBacktick(value, indent string, clip bool) error {
	body := value
	sigil := "\\-"
	if clip {
		body = strings.TrimSuffix(body, "\n")
		sigil = "\\"
	}
	inner := indent + fmtStep
	w.buf.WriteByte('`')
	w.buf.WriteString(sigil)
	w.buf.WriteByte('\n')
	width := fmtMaxColumn - len(inner)
	if width < 1 {
		width = 1
	}
	for _, line := range smartColumnBreak(body, width) {
		w.buf.WriteString(inner)
		w.buf.WriteString(line)
		w.buf.WriteByte('\n')
	}
	w.buf.WriteString(inner)
	w.buf.WriteByte('`')
	return nil
}

// wordWrap breaks s into lines no longer than width by splitting at the
// last space that fits. A word longer than width gets its own line and
// exceeds width; mid-word breaks are not introduced.
func wordWrap(s string, width int) []string {
	if s == "" {
		return nil
	}
	if width < 1 {
		width = 1
	}
	var lines []string
	for len(s) > width {
		breakAt := strings.LastIndex(s[:width+1], " ")
		if breakAt < 0 {
			breakAt = strings.Index(s, " ")
			if breakAt < 0 {
				break
			}
		}
		lines = append(lines, s[:breakAt])
		s = s[breakAt+1:]
	}
	if s != "" {
		lines = append(lines, s)
	}
	return lines
}

// joinedBreakChars are byte values smartColumnBreak prefers to break
// after when wrapping a joined-mode value. The set covers the common
// punctuation in URLs, ARNs, query strings, and similar punctuated
// strings.
const joinedBreakChars = "/?&#:;.,|@=_-+"

// smartColumnBreak chops s into chunks that each fit within maxWidth
// and aim to be of similar length. It picks the smallest line count
// that satisfies the width budget, then walks each ideal split point
// and prefers a position one past a joinedBreakChars character within
// half of the target length. If no such break is in range, it cuts
// at the ideal column - that is the fallback for blob content (e.g.
// base64) where any boundary is fine.
func smartColumnBreak(s string, maxWidth int) []string {
	if s == "" {
		return nil
	}
	if maxWidth < 1 {
		maxWidth = 1
	}
	if len(s) <= maxWidth {
		return []string{s}
	}
	n := (len(s) + maxWidth - 1) / maxWidth
	target := (len(s) + n - 1) / n
	tolerance := target / 2
	if tolerance < 1 {
		tolerance = 1
	}
	var lines []string
	pos := 0
	for i := 1; i < n; i++ {
		ideal := pos + target
		hardMax := pos + maxWidth
		if hardMax > len(s) {
			hardMax = len(s)
		}
		if ideal > hardMax {
			ideal = hardMax
		}
		hardMin := len(s) - (n-i)*maxWidth
		breakAt := findJoinedBreak(s, pos, ideal, tolerance, hardMin, hardMax)
		lines = append(lines, s[pos:breakAt])
		pos = breakAt
	}
	if pos < len(s) {
		lines = append(lines, s[pos:])
	}
	return lines
}

func findJoinedBreak(s string, start, ideal, tolerance, hardMin, hardMax int) int {
	lo := ideal - tolerance
	if lo <= start {
		lo = start + 1
	}
	if lo < hardMin {
		lo = hardMin
	}
	hi := ideal + tolerance
	if hi > hardMax {
		hi = hardMax
	}
	best := -1
	bestDist := 0
	for j := lo; j <= hi; j++ {
		if !isJoinedBreakChar(s[j-1]) {
			continue
		}
		dist := j - ideal
		if dist < 0 {
			dist = -dist
		}
		if best < 0 || dist < bestDist {
			best = j
			bestDist = dist
		}
	}
	if best >= 0 {
		return best
	}
	if ideal < hardMin {
		return hardMin
	}
	return ideal
}

func isJoinedBreakChar(b byte) bool {
	return strings.IndexByte(joinedBreakChars, b) >= 0
}

func (w *formatter) writeObject(o *ObjectLit, indent string) error {
	if len(o.Fields) == 0 {
		w.buf.WriteString("{}")
		return nil
	}
	inner := indent + fmtStep
	w.buf.WriteByte('{')
	w.buf.WriteByte('\n')
	w.lastLine = o.S.Start.Line
	if err := w.writeFields(o.Fields, inner); err != nil {
		return err
	}
	w.flushBefore(o.S.End.Offset, inner)
	w.buf.WriteString(indent)
	w.buf.WriteByte('}')
	w.lastLine = o.S.End.Line
	return nil
}

func (w *formatter) writeArray(a *ArrayLit, indent string) error {
	if len(a.Elements) == 0 {
		w.buf.WriteString("[]")
		return nil
	}
	inner := indent + fmtStep
	w.buf.WriteByte('[')
	w.buf.WriteByte('\n')
	w.lastLine = a.S.Start.Line
	for _, elem := range a.Elements {
		w.flushBefore(elem.Span().Start.Offset, inner)
		w.buf.WriteString(inner)
		if err := w.writeExpr(elem, inner); err != nil {
			return err
		}
		w.buf.WriteByte(',')
		w.flushTrailingOnLine(valueEndLine(elem))
		w.buf.WriteByte('\n')
	}
	w.flushBefore(a.S.End.Offset, inner)
	w.buf.WriteString(indent)
	w.buf.WriteByte(']')
	w.lastLine = a.S.End.Line
	return nil
}

func (w *formatter) writeDotPath(dp *DotPath, indent string) error {
	if dp.Root != nil {
		w.buf.WriteString(dp.Root.Name)
	}
	for _, seg := range dp.Segments {
		if seg.Index != nil {
			w.buf.WriteByte('[')
			if err := w.writeExpr(seg.Index, indent); err != nil {
				return err
			}
			w.buf.WriteByte(']')
			continue
		}
		w.buf.WriteByte('.')
		w.buf.WriteString(seg.Name)
	}
	return nil
}

func (w *formatter) writeCall(c *Call, indent string) error {
	switch {
	case c.Module != nil && c.Func != nil:
		w.buf.WriteString(c.Module.Name)
		w.buf.WriteByte('.')
		w.buf.WriteString(c.Func.Name)
	case c.Callee != nil:
		w.buf.WriteString(c.Callee.Name)
	}
	w.buf.WriteByte('(')
	for i, arg := range c.Args {
		if i > 0 {
			w.buf.WriteString(", ")
		}
		if err := w.writeExpr(arg, indent); err != nil {
			return err
		}
	}
	w.buf.WriteByte(')')
	return nil
}

func (w *formatter) writeInfix(i *Infix, indent string) error {
	if err := w.writeExpr(i.Left, indent); err != nil {
		return err
	}
	w.buf.WriteByte(' ')
	w.buf.WriteString(i.Op)
	w.buf.WriteByte(' ')
	return w.writeExpr(i.Right, indent)
}

func (w *formatter) writePrefix(p *Prefix, indent string) error {
	w.buf.WriteString(p.Op)
	return w.writeExpr(p.Expr, indent)
}

func (w *formatter) writeTypeExpr(t TypeExpr, indent string) error {
	switch x := t.(type) {
	case *TypeAtomic:
		w.buf.WriteString(x.Name)
	case *TypeList:
		w.buf.WriteString("list(")
		if err := w.writeTypeExpr(x.Elem, indent); err != nil {
			return err
		}
		w.buf.WriteByte(')')
	case *TypeSet:
		w.buf.WriteString("set(")
		if err := w.writeTypeExpr(x.Elem, indent); err != nil {
			return err
		}
		w.buf.WriteByte(')')
	case *TypeMap:
		w.buf.WriteString("map(")
		if err := w.writeTypeExpr(x.Elem, indent); err != nil {
			return err
		}
		w.buf.WriteByte(')')
	case *TypeObject:
		return w.writeTypeObject(x, indent)
	case *TypeTuple:
		w.buf.WriteString("tuple([")
		for i, elem := range x.Elements {
			if i > 0 {
				w.buf.WriteString(", ")
			}
			if err := w.writeTypeExpr(elem, indent); err != nil {
				return err
			}
		}
		w.buf.WriteString("])")
	case *TypeOptional:
		w.buf.WriteString("optional(")
		if err := w.writeTypeExpr(x.Elem, indent); err != nil {
			return err
		}
		if x.Default != nil {
			w.buf.WriteByte(' ')
			if err := w.writeExpr(x.Default, indent); err != nil {
				return err
			}
		}
		w.buf.WriteByte(')')
	default:
		return fmt.Errorf("format: unsupported type expression %T", t)
	}
	return nil
}

func (w *formatter) writeTypeObject(o *TypeObject, indent string) error {
	if len(o.Fields) == 0 {
		w.buf.WriteString("object({})")
		return nil
	}
	inner := indent + fmtStep
	w.buf.WriteString("object({\n")
	for _, field := range o.Fields {
		w.buf.WriteString(inner)
		w.buf.WriteString(RenderKey(field.Name))
		w.buf.WriteString(": ")
		switch {
		case field.Decl != nil:
			if err := w.writeObject(field.Decl, inner); err != nil {
				return err
			}
		case field.Type != nil:
			if err := w.writeTypeExpr(field.Type, inner); err != nil {
				return err
			}
		}
		w.buf.WriteByte('\n')
	}
	w.buf.WriteString(indent)
	w.buf.WriteString("})")
	return nil
}

// flushBefore emits any pending comments whose start offset is less
// than target on their own line at the given indent, preserving a
// blank-line gap from the prior item when the source had one.
func (w *formatter) flushBefore(target int, indent string) {
	for w.cIdx < len(w.comments) && w.comments[w.cIdx].S.Start.Offset < target {
		c := w.comments[w.cIdx]
		w.maybeBlankLine(c.S.Start.Line)
		w.buf.WriteString(indent)
		w.buf.WriteString(c.Text)
		w.buf.WriteByte('\n')
		w.lastLine = c.S.Start.Line
		w.cIdx++
	}
}

// flushTrailingOnLine emits any pending comments whose start line
// matches the given source line as inline trailing comments,
// separated from the preceding token by two spaces.
func (w *formatter) flushTrailingOnLine(line int) {
	for w.cIdx < len(w.comments) && w.comments[w.cIdx].S.Start.Line == line {
		w.buf.WriteString("  ")
		w.buf.WriteString(w.comments[w.cIdx].Text)
		w.cIdx++
	}
}

// maybeBlankLine emits a single blank separator line when the source
// had at least one blank line between the last emitted item (a
// sibling value, a comment, or a collection's closing delimiter) and
// the line the next token starts on.
func (w *formatter) maybeBlankLine(nextLine int) {
	if w.lastLine > 0 && nextLine-w.lastLine > 1 {
		w.buf.WriteByte('\n')
	}
}

func valueEndLine(e Expr) int {
	s := e.Span()
	if s.End.Line > 0 {
		return s.End.Line
	}
	return s.Start.Line
}
