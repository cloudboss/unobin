package lang

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// FormatOptions configures Format behavior. The zero value means
// "use defaults": MaxColumn is taken to be DefaultMaxColumn, and
// WrapStrings is false.
type FormatOptions struct {
	// MaxColumn is the soft target line width. The formatter prefers
	// to break long lines so no rendered line exceeds this width. Some
	// constructs (a literal-mode triple-quoted string, or a single
	// token that won't fit anywhere) can still go past this width.
	MaxColumn int

	// WrapStrings, when true, lets the formatter rewrite an overflowing
	// single-quoted string as a folded (>-) or joined (\-) triple-quoted
	// string so the body wraps within the line budget. When false, a
	// single-quoted string keeps its form even when it overflows.
	WrapStrings bool
}

// DefaultMaxColumn is the line width used when FormatOptions.MaxColumn
// is unset.
const DefaultMaxColumn = 100

// Format renders a parsed File back to canonical UB source using the
// default options. Comments captured during parsing are interleaved
// at their original positions; non-comment whitespace is normalized.
// Output is stable: re-parsing the result and feeding it back through
// Format yields the same bytes.
func Format(file *File) ([]byte, error) {
	return FormatWith(file, FormatOptions{})
}

// FormatWith renders a parsed File with the supplied options. Zero
// values fall back to their package defaults.
func FormatWith(file *File, opts FormatOptions) ([]byte, error) {
	maxColumn := opts.MaxColumn
	if maxColumn <= 0 {
		maxColumn = DefaultMaxColumn
	}
	w := &formatter{
		comments:    file.Comments,
		maxColumn:   maxColumn,
		wrapStrings: opts.WrapStrings,
	}
	if err := w.writeFile(file); err != nil {
		return nil, err
	}
	return []byte(w.buf.String()), nil
}

type formatter struct {
	buf         strings.Builder
	comments    []Comment
	cIdx        int
	lastLine    int
	maxColumn   int
	wrapStrings bool
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
		end, keyCol := w.findAlignmentGroup(fields, i, indent)
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
// key length to pad each member to. A group extends while each next
// field is single-line, shares no blank line with its predecessor,
// and every member's value still fits inline at the new shared
// column.
func (w *formatter) findAlignmentGroup(fields []*Field, start int, indent string) (int, int) {
	maxKey := keyWidth(fields[start].Key)
	if !w.isSingleLineField(fields[start]) {
		return start + 1, maxKey
	}
	end := start + 1
	for end < len(fields) {
		if !w.isSingleLineField(fields[end]) {
			break
		}
		if w.hasBlankLineBetween(fields[end-1], fields[end]) {
			break
		}
		newMaxKey := maxKey
		if k := keyWidth(fields[end].Key); k > newMaxKey {
			newMaxKey = k
		}
		column := len(indent) + newMaxKey + 2
		allFit := true
		for j := start; j <= end; j++ {
			if !w.fitsAtColumn(fields[j].Value, column) {
				allFit = false
				break
			}
		}
		if !allFit {
			break
		}
		maxKey = newMaxKey
		end++
	}
	return end, maxKey
}

// fitsAtColumn reports whether a value would render inline at the
// given column. Atoms and string literals are treated as always
// fitting (they render on one line regardless of width). Values that
// have a multi-line alternative (collections, calls) must satisfy the
// formatter's column budget so they don't expand inside an alignment
// group that has already padded the short-key siblings.
func (w *formatter) fitsAtColumn(e Expr, column int) bool {
	switch e.(type) {
	case *ObjectLit, *ArrayLit, *Call, *Conditional, *Comprehension:
		return w.fitsOnLine(e, column)
	}
	return true
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

// column returns the column of the next character to be written,
// counted from the start of the current line.
func (w *formatter) column() int {
	s := w.buf.String()
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return len(s) - i - 1
	}
	return len(s)
}

// isSingleLineField reports whether a field's value renders on a
// single line. Empty collections count as single-line; objects,
// arrays, and calls with a renderable inline form also count (the
// renderer decides at write time whether the column budget allows it).
// Multi-line strings, calls with a forced-multi-line argument, and
// non-empty type-object literals always expand onto multiple lines.
func (w *formatter) isSingleLineField(field *Field) bool {
	switch x := field.Value.(type) {
	case *ObjectLit:
		return w.singleLineWidth(x) >= 0
	case *ArrayLit:
		return w.singleLineWidth(x) >= 0
	case *StringLit:
		return !x.Form.IsMultiLine()
	case *InterpolatedString:
		return w.singleLineWidth(x) >= 0
	case *Call:
		return w.singleLineWidth(x) >= 0
	case *Conditional:
		return w.singleLineWidth(x) >= 0
	case *Comprehension:
		return w.singleLineWidth(x) >= 0
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

// singleLineWidth returns the rendered width of e if it can be emitted
// on a single line, or -1 if the subtree forces a multi-line form. The
// multi-line forcers are: a multi-line triple-quoted string anywhere in
// the subtree, a comment whose source offset falls inside the span of any
// enclosing collection, and a non-empty type-object literal.
func (w *formatter) singleLineWidth(e Expr) int {
	switch x := e.(type) {
	case *StringLit:
		if x.Form.IsMultiLine() {
			return -1
		}
		return stringInlineWidth(x)
	case *InterpolatedString:
		return w.interpolatedInlineWidth(x)
	case *NumberLit:
		return len(x.Value)
	case *BoolLit:
		if x.Value {
			return len("true")
		}
		return len("false")
	case *NullLit:
		return len("null")
	case *Ident:
		return len(x.Name)
	case *ObjectLit:
		return w.objectInlineWidth(x)
	case *ArrayLit:
		return w.arrayInlineWidth(x)
	case *DotPath:
		return w.dotPathWidth(x)
	case *Call:
		return w.callInlineWidth(x)
	case *Infix:
		l := w.singleLineWidth(x.Left)
		if l < 0 {
			return -1
		}
		r := w.singleLineWidth(x.Right)
		if r < 0 {
			return -1
		}
		return l + 1 + len(x.Op) + 1 + r
	case *Prefix:
		i := w.singleLineWidth(x.Expr)
		if i < 0 {
			return -1
		}
		return len(x.Op) + i
	case *Conditional:
		return w.conditionalInlineWidth(x)
	case *Comprehension:
		return w.comprehensionInlineWidth(x)
	case TypeExpr:
		return w.typeExprWidth(x)
	case nil:
		return len("null")
	}
	return -1
}

// conditionalInlineWidth returns the single-line width of `if cond then
// a else b`, or -1 when any part forces a multi-line form.
func (w *formatter) conditionalInlineWidth(c *Conditional) int {
	cw := w.singleLineWidth(c.Cond)
	if cw < 0 {
		return -1
	}
	tw := w.singleLineWidth(c.Then)
	if tw < 0 {
		return -1
	}
	ew := w.singleLineWidth(c.Else)
	if ew < 0 {
		return -1
	}
	return len("if ") + cw + len(" then ") + tw + len(" else ") + ew
}

// comprehensionInlineWidth returns the single-line width of a list or
// map comprehension, or -1 when any part forces a multi-line form.
func (w *formatter) comprehensionInlineWidth(c *Comprehension) int {
	if w.hasCommentInSpan(c.S.Start.Offset, c.S.End.Offset) {
		return -1
	}
	srcW := w.singleLineWidth(c.Source)
	if srcW < 0 {
		return -1
	}
	valW := w.singleLineWidth(c.Value)
	if valW < 0 {
		return -1
	}
	// open + " for " + names + " in " + source + " : "
	total := 1 + len(" for ") + len(strings.Join(c.Names, ", ")) + len(" in ") + srcW + len(" : ")
	if c.Kind == CompMap {
		keyW := w.singleLineWidth(c.Key)
		if keyW < 0 {
			return -1
		}
		total += keyW + len(" => ")
	}
	total += valW
	if c.Group {
		total += len("...")
	}
	if c.Filter != nil {
		fw := w.singleLineWidth(c.Filter)
		if fw < 0 {
			return -1
		}
		total += len(" when ") + fw
	}
	return total + len(" ") + 1 // trailing space + close delimiter
}

// fitsOnLine reports whether e's single-line form fits between column
// and the formatter's max column.
func (w *formatter) fitsOnLine(e Expr, column int) bool {
	width := w.singleLineWidth(e)
	if width < 0 {
		return false
	}
	return column+width <= w.maxColumn
}

func stringInlineWidth(s *StringLit) int {
	if s.Form == StringTripleQuoteSingleLine && canTripleQuoteSingleLine(s.Value) {
		return 6 + len(s.Value)
	}
	return len(renderString(s.Value))
}

func (w *formatter) objectInlineWidth(o *ObjectLit) int {
	if len(o.Fields) == 0 {
		return 2
	}
	if w.hasCommentInSpan(o.S.Start.Offset, o.S.End.Offset) {
		return -1
	}
	total := 4
	for i, f := range o.Fields {
		vw := w.singleLineWidth(f.Value)
		if vw < 0 {
			return -1
		}
		total += keyWidth(f.Key) + 2 + vw
		if i > 0 {
			total += 2
		}
	}
	return total
}

func (w *formatter) arrayInlineWidth(a *ArrayLit) int {
	if len(a.Elements) == 0 {
		return 2
	}
	if w.hasCommentInSpan(a.S.Start.Offset, a.S.End.Offset) {
		return -1
	}
	total := 2
	for i, el := range a.Elements {
		ew := w.singleLineWidth(el)
		if ew < 0 {
			return -1
		}
		total += ew
		if i > 0 {
			total += 2
		}
	}
	return total
}

func (w *formatter) dotPathWidth(dp *DotPath) int {
	total := 0
	if dp.Root != nil {
		total += len(dp.Root.Name)
	}
	for _, seg := range dp.Segments {
		if seg.Splat {
			total += 3
			continue
		}
		if seg.Index != nil {
			iw := w.singleLineWidth(seg.Index)
			if iw < 0 {
				return -1
			}
			total += 2 + iw
			continue
		}
		total += 1 + len(seg.Name)
	}
	return total
}

func (w *formatter) callInlineWidth(c *Call) int {
	total := 2
	switch {
	case c.Library != nil && c.Func != nil:
		total += len(c.Library.Name) + 1 + len(c.Func.Name)
	case c.Callee != nil:
		total += len(c.Callee.Name)
	}
	for i, a := range c.Args {
		aw := w.singleLineWidth(a)
		if aw < 0 {
			return -1
		}
		total += aw
		if i > 0 {
			total += 2
		}
	}
	return total
}

func (w *formatter) typeExprWidth(t TypeExpr) int {
	switch x := t.(type) {
	case *TypeAtomic:
		return len(x.Name)
	case *TypeList:
		i := w.typeExprWidth(x.Elem)
		if i < 0 {
			return -1
		}
		return len("list(") + i + 1
	case *TypeSet:
		i := w.typeExprWidth(x.Elem)
		if i < 0 {
			return -1
		}
		return len("set(") + i + 1
	case *TypeMap:
		i := w.typeExprWidth(x.Elem)
		if i < 0 {
			return -1
		}
		return len("map(") + i + 1
	case *TypeObject:
		if len(x.Fields) == 0 {
			return len("object({})")
		}
		return -1
	case *TypeTuple:
		total := len("tuple([])")
		for i, el := range x.Elements {
			ew := w.typeExprWidth(el)
			if ew < 0 {
				return -1
			}
			total += ew
			if i > 0 {
				total += 2
			}
		}
		return total
	case *TypeOptional:
		i := w.typeExprWidth(x.Elem)
		if i < 0 {
			return -1
		}
		total := len("optional(") + i + 1
		if x.Default != nil {
			d := w.singleLineWidth(x.Default)
			if d < 0 {
				return -1
			}
			total += 2 + d
		}
		return total
	}
	return -1
}

// hasCommentInSpan reports whether any not-yet-flushed comment's source
// offset falls within [start, end).
func (w *formatter) hasCommentInSpan(start, end int) bool {
	for k := w.cIdx; k < len(w.comments); k++ {
		off := w.comments[k].S.Start.Offset
		if off >= end {
			return false
		}
		if off >= start {
			return true
		}
	}
	return false
}

func (w *formatter) writeExpr(expr Expr, indent string) error {
	switch x := expr.(type) {
	case *StringLit:
		return w.writeString(x, indent)
	case *InterpolatedString:
		return w.writeInterpolated(x, indent)
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
	case *Conditional:
		return w.writeConditional(x, indent)
	case *Comprehension:
		return w.writeComprehension(x, indent)
	case TypeExpr:
		return w.writeTypeExpr(x, indent)
	case nil:
		w.buf.WriteString("null")
	default:
		return fmt.Errorf("format: unsupported expression %T", expr)
	}
	return nil
}

// writeInterpolated renders an interpolated string in its own form. A
// single-quoted or single-line triple-quoted value renders inline (a slot
// is `{{ expr }}`, with `:verb` when it carries a printf directive). A
// multi-line form re-emits the triple-quote layout. With WrapStrings, an
// overflowing single-quoted value is rewritten as a folded or joined
// triple-quote.
func (w *formatter) writeInterpolated(s *InterpolatedString, indent string) error {
	if s.Form.IsMultiLine() {
		return w.writeInterpolatedMultiline(s, s.Form, indent)
	}
	if s.Form == StringSingleQuoted && w.wrapStrings && w.shouldWrapInterpolated(s) {
		form := StringJoinedStrip
		if w.interpolatedHasSpace(s) {
			form = StringFoldedStrip
		}
		return w.writeInterpolatedMultiline(s, form, indent)
	}
	opener, closer, esc := "$'", "'", escapeInterpolatedLiteral
	if s.Form == StringTripleQuoteSingleLine {
		opener, closer, esc = "$'''", "'''", escapeTripleInterpolatedLiteral
	}
	w.buf.WriteString(opener)
	for _, part := range s.Parts {
		if part.Expr == nil {
			w.buf.WriteString(esc(part.Lit))
			continue
		}
		w.buf.WriteString("{{ ")
		if err := w.writeExpr(part.Expr, ""); err != nil {
			return err
		}
		if part.Verb != "" {
			w.buf.WriteByte(':')
			w.buf.WriteString(part.Verb)
		}
		w.buf.WriteString(" }}")
	}
	w.buf.WriteString(closer)
	return nil
}

// writeInterpolatedMultiline emits an interpolated string in a multi-line
// triple-quote form. Each slot is rendered to its source and stood in for
// by a width-matched, spaceless sentinel so the existing fold/join wrap
// measures and breaks correctly; the real slot source is spliced back in
// afterward, never split across a line.
func (w *formatter) writeInterpolatedMultiline(
	s *InterpolatedString, form StringForm, indent string,
) error {
	var slotSrc []string
	var val strings.Builder
	for _, part := range s.Parts {
		if part.Expr == nil {
			val.WriteString(escapeTripleInterpolatedLiteral(part.Lit))
			continue
		}
		src, err := w.slotSource(part)
		if err != nil {
			return err
		}
		val.WriteString(paddedSentinel(len(slotSrc), len(src)))
		slotSrc = append(slotSrc, src)
	}
	tmp := &formatter{maxColumn: w.maxColumn, wrapStrings: w.wrapStrings}
	syn := &StringLit{Value: val.String(), Form: form}
	if err := tmp.writeMultilineString(syn, indent); err != nil {
		return err
	}
	w.buf.WriteByte('$')
	w.buf.WriteString(substituteSentinels(tmp.buf.String(), slotSrc))
	return nil
}

// slotSource renders a slot to its `{{ expr }}` source form, with a
// `:verb` suffix when the slot carries a printf directive.
func (w *formatter) slotSource(part InterpolatedPart) (string, error) {
	tmp := &formatter{maxColumn: w.maxColumn, wrapStrings: w.wrapStrings}
	tmp.buf.WriteString("{{ ")
	if err := tmp.writeExpr(part.Expr, ""); err != nil {
		return "", err
	}
	if part.Verb != "" {
		tmp.buf.WriteByte(':')
		tmp.buf.WriteString(part.Verb)
	}
	tmp.buf.WriteString(" }}")
	return tmp.buf.String(), nil
}

// shouldWrapInterpolated reports whether an overflowing single-quoted
// interpolated string may be rewritten as a triple-quote. A literal run
// holding a triple-quote run or a newline blocks the rewrite.
func (w *formatter) shouldWrapInterpolated(s *InterpolatedString) bool {
	for _, part := range s.Parts {
		if part.Expr != nil {
			continue
		}
		if strings.Contains(part.Lit, "'''") || strings.ContainsAny(part.Lit, "\n\r") {
			return false
		}
	}
	width := w.interpolatedInlineWidth(s)
	return width >= 0 && w.column()+width > w.maxColumn
}

// interpolatedHasSpace reports whether any literal run contains a space,
// which picks the folded form over the joined form when wrapping.
func (w *formatter) interpolatedHasSpace(s *InterpolatedString) bool {
	for _, part := range s.Parts {
		if part.Expr == nil && strings.ContainsRune(part.Lit, ' ') {
			return true
		}
	}
	return false
}

func (w *formatter) interpolatedInlineWidth(s *InterpolatedString) int {
	if s.Form.IsMultiLine() {
		return -1
	}
	opener, closer, esc := "$'", "'", escapeInterpolatedLiteral
	if s.Form == StringTripleQuoteSingleLine {
		opener, closer, esc = "$'''", "'''", escapeTripleInterpolatedLiteral
	}
	n := len(opener) + len(closer)
	for _, part := range s.Parts {
		if part.Expr == nil {
			n += len(esc(part.Lit))
			continue
		}
		ew := w.singleLineWidth(part.Expr)
		if ew < 0 {
			return -1
		}
		n += len("{{ ") + ew + len(" }}")
		if part.Verb != "" {
			n += 1 + len(part.Verb)
		}
	}
	return n
}

// paddedSentinel returns a spaceless marker for slot idx, padded to width
// columns with a filler byte so the wrap measures the slot at its true
// rendered width. NUL delimits the index and \x01 is the filler; neither
// is a space nor a join break character, so neither fold nor join splits
// it, and neither occurs in source.
func paddedSentinel(idx, width int) string {
	core := "\x00" + strconv.Itoa(idx) + "\x00"
	if len(core) >= width {
		return core
	}
	return core + strings.Repeat("\x01", width-len(core))
}

// substituteSentinels replaces each paddedSentinel run in s with the
// matching slot source.
func substituteSentinels(s string, slotSrc []string) string {
	if !strings.ContainsRune(s, 0) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != 0 {
			b.WriteByte(s[i])
			i++
			continue
		}
		j := i + 1
		for j < len(s) && s[j] != 0 {
			j++
		}
		k := j + 1
		for k < len(s) && s[k] == 1 {
			k++
		}
		if idx, err := strconv.Atoi(s[i+1 : j]); err == nil && idx >= 0 && idx < len(slotSrc) {
			b.WriteString(slotSrc[idx])
		}
		i = k
	}
	return b.String()
}

// escapeTripleInterpolatedLiteral renders a literal run for a triple-
// quoted interpolated string: the body is raw, so only a `{{` is escaped
// (to `\{{`) so it does not reopen a slot when re-parsed.
func escapeTripleInterpolatedLiteral(s string) string {
	if !strings.Contains(s, "{{") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '{' && i+1 < len(s) && s[i+1] == '{' {
			b.WriteString(`\{{`)
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// escapeInterpolatedLiteral renders a literal run back to its source
// form. It is the inverse of the interpolated-literal decoder: the
// recognized escapes are re-applied, and a `{{` is written as `\{{` so
// it does not reopen a slot when re-parsed. A single `{` stays literal.
func escapeInterpolatedLiteral(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\':
			b.WriteString(`\\`)
		case '\'':
			b.WriteString(`\'`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		case 0:
			b.WriteString(`\0`)
		case '{':
			if i+1 < len(s) && s[i+1] == '{' {
				b.WriteString(`\{{`)
				i++
				continue
			}
			b.WriteByte('{')
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// writeConditional renders `if cond then a else b` inline when it fits;
// otherwise it breaks before then and else, indented one step under the
// if.
func (w *formatter) writeConditional(c *Conditional, indent string) error {
	if w.fitsOnLine(c, w.column()) {
		return w.writeConditionalInline(c)
	}
	inner := indent + fmtStep
	w.buf.WriteString("if ")
	if err := w.writeExpr(c.Cond, indent); err != nil {
		return err
	}
	w.buf.WriteByte('\n')
	w.buf.WriteString(inner)
	w.buf.WriteString("then ")
	if err := w.writeExpr(c.Then, inner); err != nil {
		return err
	}
	w.buf.WriteByte('\n')
	w.buf.WriteString(inner)
	w.buf.WriteString("else ")
	return w.writeExpr(c.Else, inner)
}

func (w *formatter) writeConditionalInline(c *Conditional) error {
	w.buf.WriteString("if ")
	if err := w.writeExpr(c.Cond, ""); err != nil {
		return err
	}
	w.buf.WriteString(" then ")
	if err := w.writeExpr(c.Then, ""); err != nil {
		return err
	}
	w.buf.WriteString(" else ")
	return w.writeExpr(c.Else, "")
}

// writeComprehension renders a list or map comprehension inline when it
// fits; otherwise it puts the header, body, and filter on their own
// lines indented one step in.
func (w *formatter) writeComprehension(c *Comprehension, indent string) error {
	if w.fitsOnLine(c, w.column()) {
		return w.writeComprehensionInline(c)
	}
	open, closer := comprehensionDelims(c.Kind)
	inner := indent + fmtStep
	w.buf.WriteString(open)
	w.buf.WriteByte('\n')
	w.buf.WriteString(inner)
	if err := w.writeComprehensionHeader(c, inner); err != nil {
		return err
	}
	w.buf.WriteByte('\n')
	w.buf.WriteString(inner)
	if err := w.writeComprehensionBody(c, inner); err != nil {
		return err
	}
	if c.Filter != nil {
		w.buf.WriteByte('\n')
		w.buf.WriteString(inner)
		w.buf.WriteString("when ")
		if err := w.writeExpr(c.Filter, inner); err != nil {
			return err
		}
	}
	w.buf.WriteByte('\n')
	w.buf.WriteString(indent)
	w.buf.WriteString(closer)
	return nil
}

func (w *formatter) writeComprehensionInline(c *Comprehension) error {
	open, closer := comprehensionDelims(c.Kind)
	w.buf.WriteString(open)
	w.buf.WriteByte(' ')
	if err := w.writeComprehensionHeader(c, ""); err != nil {
		return err
	}
	w.buf.WriteByte(' ')
	if err := w.writeComprehensionBody(c, ""); err != nil {
		return err
	}
	if c.Filter != nil {
		w.buf.WriteString(" when ")
		if err := w.writeExpr(c.Filter, ""); err != nil {
			return err
		}
	}
	w.buf.WriteByte(' ')
	w.buf.WriteString(closer)
	return nil
}

// writeComprehensionHeader writes `for <names> in <source> :`.
func (w *formatter) writeComprehensionHeader(c *Comprehension, indent string) error {
	w.buf.WriteString("for ")
	w.buf.WriteString(strings.Join(c.Names, ", "))
	w.buf.WriteString(" in ")
	if err := w.writeExpr(c.Source, indent); err != nil {
		return err
	}
	w.buf.WriteString(" :")
	return nil
}

// writeComprehensionBody writes the produced element: `value` for a
// list, `key => value` for a map, with a trailing `...` when grouping.
func (w *formatter) writeComprehensionBody(c *Comprehension, indent string) error {
	if c.Kind == CompMap {
		if err := w.writeExpr(c.Key, indent); err != nil {
			return err
		}
		w.buf.WriteString(" => ")
	}
	if err := w.writeExpr(c.Value, indent); err != nil {
		return err
	}
	if c.Group {
		w.buf.WriteString("...")
	}
	return nil
}

func comprehensionDelims(kind ComprehensionKind) (string, string) {
	if kind == CompMap {
		return "{", "}"
	}
	return "[", "]"
}

func (w *formatter) writeString(s *StringLit, indent string) error {
	switch {
	case s.Form == StringTripleQuoteSingleLine && canTripleQuoteSingleLine(s.Value):
		w.buf.WriteString("'''")
		w.buf.WriteString(s.Value)
		w.buf.WriteString("'''")
		return nil
	case s.Form.IsMultiLine():
		return w.writeMultilineString(s, indent)
	}
	if w.wrapStrings && w.shouldWrapSingleQuoted(s) {
		if strings.ContainsRune(s.Value, ' ') {
			return w.writeFoldedTriple(s.Value, indent, false)
		}
		return w.writeJoinedTriple(s.Value, indent, false)
	}
	w.buf.WriteString(renderString(s.Value))
	return nil
}

// shouldWrapSingleQuoted reports whether a single-quoted string can
// and should be rewritten in folded or joined triple-quote form.
// Bodies containing a triple-quote run or a newline cannot be carried in
// either form and are left alone. Bodies that already fit on the current
// line stay single-quoted regardless of the wrapStrings setting.
func (w *formatter) shouldWrapSingleQuoted(s *StringLit) bool {
	if strings.Contains(s.Value, "'''") || strings.ContainsAny(s.Value, "\n\r") {
		return false
	}
	return w.column()+stringInlineWidth(s) > w.maxColumn
}

func canTripleQuoteSingleLine(v string) bool {
	return !strings.Contains(v, "'''") && !strings.ContainsAny(v, "\n\r")
}

func (w *formatter) writeMultilineString(s *StringLit, indent string) error {
	switch s.Form {
	case StringLiteralClip:
		return w.writeLiteralTriple(s.Value, indent, true)
	case StringLiteralStrip:
		return w.writeLiteralTriple(s.Value, indent, false)
	case StringFoldedClip:
		return w.writeFoldedTriple(s.Value, indent, true)
	case StringFoldedStrip:
		return w.writeFoldedTriple(s.Value, indent, false)
	case StringJoinedClip:
		return w.writeJoinedTriple(s.Value, indent, true)
	case StringJoinedStrip:
		return w.writeJoinedTriple(s.Value, indent, false)
	}
	return fmt.Errorf("format: unexpected string form %v", s.Form)
}

func (w *formatter) writeLiteralTriple(value, indent string, clip bool) error {
	body := value
	sigil := "|-"
	if clip {
		body = strings.TrimSuffix(body, "\n")
		sigil = "|"
	}
	inner := indent + fmtStep
	w.buf.WriteString("'''")
	w.buf.WriteString(sigil)
	w.buf.WriteByte('\n')
	for line := range strings.SplitSeq(body, "\n") {
		if line == "" {
			w.buf.WriteByte('\n')
			continue
		}
		w.buf.WriteString(inner)
		w.buf.WriteString(line)
		w.buf.WriteByte('\n')
	}
	w.buf.WriteString(inner)
	w.buf.WriteString("'''")
	return nil
}

func (w *formatter) writeFoldedTriple(value, indent string, clip bool) error {
	body := value
	sigil := ">-"
	if clip {
		body = strings.TrimSuffix(body, "\n")
		sigil = ">"
	}
	inner := indent + fmtStep
	w.buf.WriteString("'''")
	w.buf.WriteString(sigil)
	w.buf.WriteByte('\n')
	width := max(w.maxColumn-len(inner), 1)
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
	w.buf.WriteString("'''")
	return nil
}

func (w *formatter) writeJoinedTriple(value, indent string, clip bool) error {
	body := value
	sigil := "\\-"
	if clip {
		body = strings.TrimSuffix(body, "\n")
		sigil = "\\"
	}
	inner := indent + fmtStep
	w.buf.WriteString("'''")
	w.buf.WriteString(sigil)
	w.buf.WriteByte('\n')
	width := max(w.maxColumn-len(inner), 1)
	for _, line := range smartColumnBreak(body, width) {
		w.buf.WriteString(inner)
		w.buf.WriteString(line)
		w.buf.WriteByte('\n')
	}
	w.buf.WriteString(inner)
	w.buf.WriteString("'''")
	return nil
}

// wordWrap breaks s at spaces into lines of roughly equal length. It
// first finds the fewest lines that fit within width (greedy word-wrap
// is optimal for that), then the smallest per-line cap that still hits
// that line count, so the lines come out as even as the word
// boundaries allow. This mirrors the even distribution the joined-mode
// wrapper produces. A word longer than width gets its own line and
// overflows; mid-word breaks are never introduced.
func wordWrap(s string, width int) []string {
	if s == "" {
		return nil
	}
	if width < 1 {
		width = 1
	}
	lines := wordWrapAt(s, width)
	if len(lines) <= 1 {
		return lines
	}
	target := len(lines)
	lo, hi := 1, width
	for lo < hi {
		mid := (lo + hi) / 2
		if len(wordWrapAt(s, mid)) <= target {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return wordWrapAt(s, lo)
}

// wordWrapAt is greedy word-wrap: each line takes as many space-separated
// words as fit within width, breaking at the last space that fits. A
// word longer than width gets its own line and exceeds width.
func wordWrapAt(s string, width int) []string {
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
	tolerance := max(target/2, 1)
	var lines []string
	pos := 0
	for i := 1; i < n; i++ {
		ideal := pos + target
		hardMax := min(pos+maxWidth, len(s))
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
	hi := min(ideal+tolerance, hardMax)
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
	if w.fitsOnLine(o, w.column()) {
		return w.writeObjectInline(o)
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

func (w *formatter) writeObjectInline(o *ObjectLit) error {
	w.buf.WriteString("{ ")
	for i, f := range o.Fields {
		if i > 0 {
			w.buf.WriteString(", ")
		}
		w.buf.WriteString(RenderKey(fieldKeyString(f.Key)))
		w.buf.WriteString(": ")
		if err := w.writeExpr(f.Value, ""); err != nil {
			return err
		}
	}
	w.buf.WriteString(" }")
	return nil
}

func (w *formatter) writeArray(a *ArrayLit, indent string) error {
	if len(a.Elements) == 0 {
		w.buf.WriteString("[]")
		return nil
	}
	if w.fitsOnLine(a, w.column()) {
		return w.writeArrayInline(a)
	}
	inner := indent + fmtStep
	w.buf.WriteByte('[')
	w.buf.WriteByte('\n')
	w.lastLine = a.S.Start.Line
	hasComments := w.hasCommentInSpan(a.S.Start.Offset, a.S.End.Offset)
	if !hasComments && elementsAreAtoms(a.Elements) {
		return w.writeArrayPacked(a, indent, inner)
	}
	return w.writeArrayPerLine(a, indent, inner)
}

func (w *formatter) writeArrayInline(a *ArrayLit) error {
	w.buf.WriteByte('[')
	for i, elem := range a.Elements {
		if i > 0 {
			w.buf.WriteString(", ")
		}
		if err := w.writeExpr(elem, ""); err != nil {
			return err
		}
	}
	w.buf.WriteByte(']')
	return nil
}

func (w *formatter) writeArrayPerLine(a *ArrayLit, indent, inner string) error {
	for _, elem := range a.Elements {
		w.flushBefore(elem.Span().Start.Offset, inner)
		w.maybeBlankLine(elem.Span().Start.Line)
		w.buf.WriteString(inner)
		if err := w.writeExpr(elem, inner); err != nil {
			return err
		}
		w.buf.WriteByte(',')
		w.lastLine = valueEndLine(elem)
		w.flushTrailingOnLine(w.lastLine)
		w.buf.WriteByte('\n')
	}
	w.flushBefore(a.S.End.Offset, inner)
	w.buf.WriteString(indent)
	w.buf.WriteByte(']')
	w.lastLine = a.S.End.Line
	return nil
}

// writeArrayPacked groups atom elements onto lines whose widths are
// as even as possible, with a trailing comma on every element.
// Comments inside the array force the per-line path instead, so this
// code path never has to interleave them.
func (w *formatter) writeArrayPacked(a *ArrayLit, indent, inner string) error {
	widths := make([]int, len(a.Elements))
	for i, e := range a.Elements {
		widths[i] = w.singleLineWidth(e) + 1
	}
	budget := w.maxColumn - len(inner)
	cap := evenLineCap(widths, budget)

	w.buf.WriteString(inner)
	if err := w.writeExpr(a.Elements[0], inner); err != nil {
		return err
	}
	w.buf.WriteByte(',')
	col := widths[0]
	for i := 1; i < len(a.Elements); i++ {
		proposed := col + 1 + widths[i]
		if proposed > cap {
			w.buf.WriteByte('\n')
			w.buf.WriteString(inner)
			col = widths[i]
		} else {
			w.buf.WriteByte(' ')
			col = proposed
		}
		if err := w.writeExpr(a.Elements[i], inner); err != nil {
			return err
		}
		w.buf.WriteByte(',')
	}
	w.buf.WriteByte('\n')
	w.buf.WriteString(indent)
	w.buf.WriteByte(']')
	w.lastLine = a.S.End.Line
	return nil
}

// evenLineCap returns the smallest per-line cap that fits the items
// into the same number of lines as a greedy pack at budget would.
// Items are separated by a single space within a line; each width
// already includes its trailing comma. The returned cap may exceed
// budget when a single item is wider than budget (the item still
// gets its own line, overflowing the user's max).
func evenLineCap(widths []int, budget int) int {
	if len(widths) <= 1 {
		return budget
	}
	maxw := widths[0]
	for _, w := range widths[1:] {
		if w > maxw {
			maxw = w
		}
	}
	if maxw > budget {
		budget = maxw
	}
	lines := linesNeeded(widths, budget)
	lo, hi := maxw, budget
	for lo < hi {
		mid := (lo + hi) / 2
		if linesNeeded(widths, mid) <= lines {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo
}

func linesNeeded(widths []int, cap int) int {
	if len(widths) == 0 {
		return 0
	}
	lines := 1
	cur := widths[0]
	for i := 1; i < len(widths); i++ {
		if cur+1+widths[i] > cap {
			lines++
			cur = widths[i]
		} else {
			cur += 1 + widths[i]
		}
	}
	return lines
}

func isAtom(e Expr) bool {
	switch x := e.(type) {
	case *NumberLit, *BoolLit, *NullLit, *Ident:
		return true
	case *StringLit:
		return !x.Form.IsMultiLine()
	}
	return false
}

func elementsAreAtoms(elems []Expr) bool {
	for _, e := range elems {
		if !isAtom(e) {
			return false
		}
	}
	return true
}

func (w *formatter) writeDotPath(dp *DotPath, indent string) error {
	if dp.Root != nil {
		w.buf.WriteString(dp.Root.Name)
	}
	for _, seg := range dp.Segments {
		if seg.Splat {
			w.buf.WriteString("[*]")
			continue
		}
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
	w.writeCallHeader(c)
	w.buf.WriteByte('(')
	if len(c.Args) == 0 {
		w.buf.WriteByte(')')
		return nil
	}
	if w.callArgsFitInline(c) {
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
	w.buf.WriteByte('\n')
	inner := indent + fmtStep
	if elementsAreAtoms(c.Args) {
		return w.writeCallArgsPacked(c, indent, inner)
	}
	return w.writeCallArgsPerLine(c, indent, inner)
}

func (w *formatter) writeCallHeader(c *Call) {
	switch {
	case c.Library != nil && c.Func != nil:
		w.buf.WriteString(c.Library.Name)
		w.buf.WriteByte('.')
		w.buf.WriteString(c.Func.Name)
	case c.Callee != nil:
		w.buf.WriteString(c.Callee.Name)
	}
}

// callArgsFitInline reports whether the call's args, joined by ", ",
// plus the closing paren, fit on the current line. The header and
// open paren have already been written, so the running column already
// accounts for them.
func (w *formatter) callArgsFitInline(c *Call) bool {
	width := 1
	for i, arg := range c.Args {
		aw := w.singleLineWidth(arg)
		if aw < 0 {
			return false
		}
		width += aw
		if i > 0 {
			width += 2
		}
	}
	return w.column()+width <= w.maxColumn
}

func (w *formatter) writeCallArgsPerLine(c *Call, indent, inner string) error {
	for _, arg := range c.Args {
		w.buf.WriteString(inner)
		if err := w.writeExpr(arg, inner); err != nil {
			return err
		}
		w.buf.WriteByte(',')
		w.buf.WriteByte('\n')
	}
	w.buf.WriteString(indent)
	w.buf.WriteByte(')')
	return nil
}

func (w *formatter) writeCallArgsPacked(c *Call, indent, inner string) error {
	widths := make([]int, len(c.Args))
	for i, a := range c.Args {
		widths[i] = w.singleLineWidth(a) + 1
	}
	budget := w.maxColumn - len(inner)
	cap := evenLineCap(widths, budget)

	w.buf.WriteString(inner)
	if err := w.writeExpr(c.Args[0], inner); err != nil {
		return err
	}
	w.buf.WriteByte(',')
	col := widths[0]
	for i := 1; i < len(c.Args); i++ {
		proposed := col + 1 + widths[i]
		if proposed > cap {
			w.buf.WriteByte('\n')
			w.buf.WriteString(inner)
			col = widths[i]
		} else {
			w.buf.WriteByte(' ')
			col = proposed
		}
		if err := w.writeExpr(c.Args[i], inner); err != nil {
			return err
		}
		w.buf.WriteByte(',')
	}
	w.buf.WriteByte('\n')
	w.buf.WriteString(indent)
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
