package parse

// Node is the root of the AST hierarchy. Every node knows its source span;
// the End may be the zero Position when only a starting point is known.
type Node interface {
	Span() Span
}

// File is the top level container for a parsed .ub source file. The Kind is
// determined after parsing by inspecting which top level keys are present and
// matching them against the known file types (stack, library, exported type,
// config). Until that classification step runs, a File's Kind is FileUnknown.
type File struct {
	S    Span
	Kind FileKind
	Path string
	Body *ObjectLit // The top level body is always an object.
	// Comments is every `#` line comment captured during parsing, in
	// source order. The format renderer interleaves them by position;
	// the runtime and type checker ignore them.
	Comments []Comment
}

// Comment is a single `# ...` line comment as recorded during parsing.
// The Text field includes the leading `#` and runs to (but does not
// include) the terminating newline.
type Comment struct {
	S    Span
	Text string
}

func (f *File) Span() Span { return f.S }

// FileKind is the classification tag for a parsed file.
type FileKind int

const (
	FileUnknown FileKind = iota
	FileFactory
	FileExportedType // A lowered composite body inside a library.
	FileStack        // A stack file for factory inputs and state settings.
	FileManifest     // The manifest.ub file declaring dependency floors.
)

func (k FileKind) String() string {
	switch k {
	case FileFactory:
		return "factory"
	case FileExportedType:
		return "exported-type"
	case FileStack:
		return "stack"
	case FileManifest:
		return "manifest"
	default:
		return "unknown"
	}
}

// Expr is any node that produces a value. Object and array literals are
// expressions, as are dotted paths, calls, infix and prefix operations,
// type expressions, and bare identifiers (which act as enum values).
type Expr interface {
	Node
	exprNode()
}

// ObjectLit is a brace delimited map: `{ key1: value1 key2: value2 }`.
// Fields are whitespace separated (newlines or spaces); the language has
// no commas. Keys preserve source order (operators and humans both rely on
// that).
type ObjectLit struct {
	S      Span
	Fields []*Field
	Source []byte
}

func (n *ObjectLit) Span() Span { return n.S }
func (n *ObjectLit) exprNode()  {}

// Field is one entry in an ObjectLit. It is either a value field (`key:
// value`) or a selector-body declaration (`name: selector { ... }` or
// `selector { ... }`).
type Field struct {
	S     Span
	Key   FieldKey
	Value Expr
	Decl  *SelectorBody
}

func (n *Field) Span() Span { return n.S }

// SelectorBody is a declaration whose body is classified by a selector.
// Default is true for selector defaults such as `greet { ... }`, where the
// selector itself is the declaration head.
type SelectorBody struct {
	S        Span
	Default  bool
	Selector Selector
	Body     *ObjectLit
}

func (n *SelectorBody) Span() Span { return n.S }

// Selector is one or more identifier parts separated by dots.
type Selector struct {
	S     Span
	Parts []Ident
}

// FieldKey distinguishes the key forms an object field can have.
//
// Kind == FieldIdent: bare identifier (possibly @-prefixed).
// Kind == FieldString: quoted string literal.
// Kind == FieldPath: dotted identifier path, such as aws.iam-role.it. Only
// a resource, data, or action declaration head uses this form.
//
// The post-parse pass is responsible for deciding whether a given key is
// permitted at its position (e.g., closed set enum identifier vs free form
// string vs meta key).
type FieldKey struct {
	S      Span
	Kind   FieldKeyKind
	Name   string   // Identifier text, including any leading `@`.
	String string   // Raw string content, when Kind == FieldString.
	Path   []string // Dotted segments, when Kind == FieldPath.
}

// IsMeta reports whether the key is a `@`-prefixed meta key.
func (k FieldKey) IsMeta() bool {
	return k.Kind == FieldIdent && len(k.Name) > 0 && k.Name[0] == '@'
}

type FieldKeyKind int

const (
	FieldIdent FieldKeyKind = iota
	FieldString
	FieldPath
)

// ArrayLit is a bracket-delimited list: `[ v1 v2 v3 ]`. Elements are
// whitespace separated.
type ArrayLit struct {
	S        Span
	Elements []Expr
}

func (n *ArrayLit) Span() Span { return n.S }
func (n *ArrayLit) exprNode()  {}

// StringLit is a string literal. Three families of source form produce
// this node, tracked on Form:
//
//   - Single quoted: 'hello\nworld'. Backslash escapes are processed
//     during parsing, so Value holds the decoded content. Double quotes
//     are not a string delimiter.
//   - Single-line triple-quoted (delimited by three single quotes). The
//     body has no newline, no escape processing, and is verbatim.
//   - Multi-line triple-quoted with a sigil that selects mode (literal
//     / folded / joined) and chomp (clip / strip). Value holds the
//     dedented and mode-processed content.
//
// The formatter dispatches on Form to choose the source form when
// re-emitting; the runtime and type-checker only read Value.
type StringLit struct {
	S     Span
	Value string
	Form  StringForm
}

func (n *StringLit) Span() Span { return n.S }
func (n *StringLit) exprNode()  {}

// InterpolatedString is an interpolated string from the `$'...'` form.
// Parts run left to right, alternating literal text and `{{ expr }}`
// slots. Form records the underlying string form so the formatter
// re-emits the right delimiter. A slot's value must evaluate to a scalar;
// the type checker enforces that.
type InterpolatedString struct {
	S     Span
	Parts []InterpolatedPart
	Form  StringForm
}

func (n *InterpolatedString) Span() Span { return n.S }
func (n *InterpolatedString) exprNode()  {}

// InterpolatedPart is one segment of an InterpolatedString. When Expr is
// nil it is a literal run carried in Lit. Otherwise it is a `{{ Expr }}`
// slot, rendered through the Go printf verb in Verb (e.g. "%03d") when
// Verb is non-empty, or with the default rendering when Verb is empty.
type InterpolatedPart struct {
	S    Span
	Lit  string
	Expr Expr
	Verb string
}

// StringForm distinguishes the source form a StringLit was parsed from
// and tells the formatter which form to re-emit. The zero value is
// StringSingleQuoted.
type StringForm int

const (
	StringSingleQuoted StringForm = iota
	StringTripleQuoteSingleLine
	StringLiteralClip
	StringLiteralStrip
	StringFoldedClip
	StringFoldedStrip
	StringJoinedClip
	StringJoinedStrip
)

// IsMultiLine reports whether the form occupies multiple source lines.
// It returns true for the six sigil-bearing triple-quote forms and false
// for single-quoted and single-line triple-quote.
func (f StringForm) IsMultiLine() bool {
	switch f {
	case StringLiteralClip, StringLiteralStrip,
		StringFoldedClip, StringFoldedStrip,
		StringJoinedClip, StringJoinedStrip:
		return true
	}
	return false
}

// String returns the constant's identifier. Used by codegen to emit a
// human-readable form constant in generated source.
func (f StringForm) String() string {
	switch f {
	case StringSingleQuoted:
		return "StringSingleQuoted"
	case StringTripleQuoteSingleLine:
		return "StringTripleQuoteSingleLine"
	case StringLiteralClip:
		return "StringLiteralClip"
	case StringLiteralStrip:
		return "StringLiteralStrip"
	case StringFoldedClip:
		return "StringFoldedClip"
	case StringFoldedStrip:
		return "StringFoldedStrip"
	case StringJoinedClip:
		return "StringJoinedClip"
	case StringJoinedStrip:
		return "StringJoinedStrip"
	}
	return "StringSingleQuoted"
}

// NumberLit covers both integers and floats - the type system narrows to
// `integer` when a constraint demands it. Value is the canonical text from
// source (preserving trailing zeros etc.). ParsedFloat / ParsedInt hold the
// numeric form. IsFloat distinguishes them.
type NumberLit struct {
	S           Span
	Value       string
	IsFloat     bool
	ParsedInt   int64
	ParsedFloat float64
}

func (n *NumberLit) Span() Span { return n.S }
func (n *NumberLit) exprNode()  {}

// BoolLit is `true` or `false`.
type BoolLit struct {
	S     Span
	Value bool
}

func (n *BoolLit) Span() Span { return n.S }
func (n *BoolLit) exprNode()  {}

// NullLit is the `null` keyword.
type NullLit struct {
	S Span
}

func (n *NullLit) Span() Span { return n.S }
func (n *NullLit) exprNode()  {}

// Ident is a bare identifier appearing at value position. Its meaning
// depends on context: an enum value (e.g., `type: string`), a closed set
// constraint kind (`kind: required-together`), or a field name reference
// inside a `fields:` list. The parser doesn't disambiguate; the type
// checker / schema validator does.
type Ident struct {
	S    Span
	Name string
}

func (n *Ident) Span() Span { return n.S }
func (n *Ident) exprNode()  {}

// DotPath is a dot-separated address like `var.region`,
// `resource.app.id`, or `data.ami.id`. Segments after the root navigate
// by name (`.id`), by a string key or integer position
// (`["alpha"]`, `[0]`), or project over a list with a splat (`[*]`). The
// first segment (Root) is one of the reserved address roots: var, data,
// resource, action, @each.
type DotPath struct {
	S        Span
	Root     *Ident
	Segments []DotSegment
}

func (n *DotPath) Span() Span { return n.S }
func (n *DotPath) exprNode()  {}

// DotSegment is one piece of a DotPath following the root: a name
// (`.foo`), an index (`["alpha"]` or `[0]`), or a splat (`[*]`) that
// projects the segments to its right over each element of a list.
type DotSegment struct {
	S     Span
	Name  string // Set when this segment is `.name` or `?.name`.
	Index Expr   // Set when this segment is `[expr]`, otherwise nil.
	Splat bool   // Set when this segment is `[*]`.
	// Guarded is set when this segment is `?.name`: a null value
	// stops the navigation and the whole path reads as null, so the
	// path's type is optional.
	Guarded bool
}

// Call is a function call: `format('%s-%s' a b)`. Args are whitespace-
// separated. The callee is either a bare identifier (built-in: `range`,
// `format`, etc.) or a qualified dotted function name. For now we model
// the callee as its raw text; the resolver disambiguates.
type Call struct {
	S       Span
	Callee  *Ident // Simple name; nil if Library is set.
	Library *Ident // Library alias (e.g., "lib") when callee is `lib.foo`.
	Func    *Ident // Function name in library-qualified form.
	Args    []Expr
}

func (n *Call) Span() Span { return n.S }
func (n *Call) exprNode()  {}

// Infix is a binary operation: a + b, a == b, a && b. The Op field is the
// raw operator text.
type Infix struct {
	S     Span
	Op    string
	Left  Expr
	Right Expr
}

func (n *Infix) Span() Span { return n.S }
func (n *Infix) exprNode()  {}

// Prefix is a unary operation: !a, -a.
type Prefix struct {
	S    Span
	Op   string
	Expr Expr
}

func (n *Prefix) Span() Span { return n.S }
func (n *Prefix) exprNode()  {}

// Conditional is `if cond then a else b`. The else branch is mandatory;
// chaining `else if` falls out of Else holding another Conditional. The
// runtime evaluates Cond, then only the taken branch (short circuit), so
// the dead branch never runs. The static dependency set is the union of
// refs in both branches, since the graph is built before Cond is known.
type Conditional struct {
	S    Span
	Cond Expr
	Then Expr
	Else Expr
}

func (n *Conditional) Span() Span { return n.S }
func (n *Conditional) exprNode()  {}

// ComprehensionKind distinguishes the list form `[ for x in xs : elem ]`
// from the map form `{ for x in xs : key => val }`.
type ComprehensionKind int

const (
	CompList ComprehensionKind = iota
	CompMap
)

// String returns the constant's identifier. Used by codegen to emit a
// human-readable kind constant in generated source.
func (k ComprehensionKind) String() string {
	if k == CompMap {
		return "CompMap"
	}
	return "CompList"
}

// Comprehension is a list or map comprehension over an iterable. Names
// holds one or two bound identifiers: one binds each element (list) or
// value (map); two binds index+element (list source) or key+value (map
// source), resolved by the source type at check time. The bound names
// are a new dot-path root class scoped to the body, so the reference
// walker excludes them from the dependency graph.
//
// For CompList, Value is the produced element and Key is nil. For
// CompMap, Key and Value are the produced pair, and Group reports a
// trailing `...` that collects same-key values into a list. Filter is
// the `when` predicate, nil when absent.
type Comprehension struct {
	S      Span
	Kind   ComprehensionKind
	Names  []string
	Source Expr
	Key    Expr
	Value  Expr
	Group  bool
	Filter Expr
}

func (n *Comprehension) Span() Span { return n.S }
func (n *Comprehension) exprNode()  {}

// TypeExpr is an expression in the type sub-grammar. Type expressions appear
// wherever a type is declared (input schema's `type:` field, object field
// types, etc.). They aren't usable as runtime values - the type checker
// rejects them outside type-position.
type TypeExpr interface {
	Expr
	typeExprNode()
}

// TypeAtomic names a primitive: string, number, integer, boolean, null, opaque.
type TypeAtomic struct {
	S    Span
	Name string
}

func (n *TypeAtomic) Span() Span    { return n.S }
func (n *TypeAtomic) exprNode()     {}
func (n *TypeAtomic) typeExprNode() {}

// TypeList is `list(T)`.
type TypeList struct {
	S    Span
	Elem TypeExpr
}

func (n *TypeList) Span() Span    { return n.S }
func (n *TypeList) exprNode()     {}
func (n *TypeList) typeExprNode() {}

// TypeMap is `map(T)`. Keys are always strings.
type TypeMap struct {
	S    Span
	Elem TypeExpr
}

func (n *TypeMap) Span() Span    { return n.S }
func (n *TypeMap) exprNode()     {}
func (n *TypeMap) typeExprNode() {}

// TypeObject is `object({ field1: T1  field2: T2  ... })`. Open is
// true when the type is wrapped in `open(...)`: a value may then hold
// fields beyond the declared ones, which pass through unread.
type TypeObject struct {
	S      Span
	Open   bool
	Fields []*TypeObjectField
}

func (n *TypeObject) Span() Span    { return n.S }
func (n *TypeObject) exprNode()     {}
func (n *TypeObject) typeExprNode() {}

// TypeObjectField is one field inside a TypeObject. The type may be a plain
// type expression or - when the field is declared in an `inputs:` block -
// a full input declaration (an object literal with `type:`, modifiers, etc.).
// At AST level we keep both possibilities; the schema validator disambiguates.
type TypeObjectField struct {
	S    Span
	Name string
	Type TypeExpr
	// Decl is set when the field's right-hand side is an input declaration
	// (an object literal) rather than a bare type expression. The two are
	// mutually exclusive.
	Decl *ObjectLit
}

// TypeTuple is tuple(T1, T2, ...).
type TypeTuple struct {
	S        Span
	Elements []TypeExpr
}

func (n *TypeTuple) Span() Span    { return n.S }
func (n *TypeTuple) exprNode()     {}
func (n *TypeTuple) typeExprNode() {}

// TypeOptional is optional(T).
//
// Optionality implies nullability - wrapping with optional() allows null
// values; bare types do not.
type TypeOptional struct {
	S    Span
	Elem TypeExpr
}

func (n *TypeOptional) Span() Span    { return n.S }
func (n *TypeOptional) exprNode()     {}
func (n *TypeOptional) typeExprNode() {}

// TypeLibraryConfig is library-config('<library-path>').
type TypeLibraryConfig struct {
	S    Span
	Path *StringLit
}

func (n *TypeLibraryConfig) Span() Span    { return n.S }
func (n *TypeLibraryConfig) exprNode()     {}
func (n *TypeLibraryConfig) typeExprNode() {}
