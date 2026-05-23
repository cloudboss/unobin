package parse

// Node is the root of the AST hierarchy. Every node knows its source span;
// the End may be the zero Position when only a starting point is known.
type Node interface {
	Span() Span
}

// File is the top level container for a parsed .ub source file. The Kind is
// determined after parsing by inspecting which top level keys are present and
// matching them against the known file types (stack, module, exported type,
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
	FileStack
	FileModule       // The module.ub manifest (only `description` and `exports`).
	FileExportedType // The <name>.ub inside a module.
	FileConfig       // The config .ub file for a stack.
)

func (k FileKind) String() string {
	switch k {
	case FileStack:
		return "stack"
	case FileModule:
		return "module"
	case FileExportedType:
		return "exported-type"
	case FileConfig:
		return "config"
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
}

func (n *ObjectLit) Span() Span { return n.S }
func (n *ObjectLit) exprNode()  {}

// Field is one entry in an ObjectLit. The key is either a bare identifier
// (the kebab-case kind, including `@`-prefixed meta keys) or a quoted
// string. The Meta flag is true iff the key starts with `@`.
type Field struct {
	S     Span
	Key   FieldKey
	Value Expr
}

func (n *Field) Span() Span { return n.S }

// FieldKey distinguishes the two key forms an object field can have.
//
// Kind == FieldIdent: bare identifier (possibly @-prefixed).
// Kind == FieldString: quoted string literal.
//
// The post-parse pass is responsible for deciding whether a given key is
// permitted at its position (e.g., closed set enum identifier vs free form
// string vs meta key).
type FieldKey struct {
	S      Span
	Kind   FieldKeyKind
	Name   string // Identifier text, including any leading `@`.
	String string // Raw string content, when Kind == FieldString.
}

// IsMeta reports whether the key is a `@`-prefixed meta key.
func (k FieldKey) IsMeta() bool {
	return k.Kind == FieldIdent && len(k.Name) > 0 && k.Name[0] == '@'
}

type FieldKeyKind int

const (
	FieldIdent FieldKeyKind = iota
	FieldString
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
//   - Single-line triple-quoted: ”'hello world”'. The body contains
//     no newline, no escape processing, and is returned verbatim.
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
// `resource.aws.vpc.main.id`, or `data.aws.ami.ubuntu.id`. Index segments
// (`["alpha"]`) are part of the path; the parser admits any expression
// inside the brackets but the schema only allows quoted string keys (no
// bare names, no integer indices). The first segment (Root) is one of
// the reserved address roots: var, data, resource, action, @each.
type DotPath struct {
	S        Span
	Root     *Ident
	Segments []DotSegment
}

func (n *DotPath) Span() Span { return n.S }
func (n *DotPath) exprNode()  {}

// DotSegment is one piece of a DotPath following the root. Either a name
// (`.foo`) or a string-keyed index (`["alpha"]`).
type DotSegment struct {
	S     Span
	Name  string // When this segment is `.name`.
	Index Expr   // When this segment is `[expr]`, otherwise nil.
}

// Call is a function call: `format('%s-%s' a b)`. Args are whitespace-
// separated. The callee is either a bare identifier (built-in: `range`,
// `format`, etc.) or a module-qualified dotted name (`alias.name`). For
// now we model the callee as its raw text; the resolver disambiguates.
type Call struct {
	S      Span
	Callee *Ident // Simple name; nil if Module is set.
	Module *Ident // Module alias (e.g., "lib") when callee is `lib.foo`.
	Func   *Ident // Function name in module-qualified form.
	Args   []Expr
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

// TypeAtomic names a primitive: string, number, integer, boolean, null, any.
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

// TypeSet is `set(T)`.
type TypeSet struct {
	S    Span
	Elem TypeExpr
}

func (n *TypeSet) Span() Span    { return n.S }
func (n *TypeSet) exprNode()     {}
func (n *TypeSet) typeExprNode() {}

// TypeMap is `map(T)`. Keys are always strings.
type TypeMap struct {
	S    Span
	Elem TypeExpr
}

func (n *TypeMap) Span() Span    { return n.S }
func (n *TypeMap) exprNode()     {}
func (n *TypeMap) typeExprNode() {}

// TypeObject is `object({ field1: T1  field2: T2  ... })`.
type TypeObject struct {
	S      Span
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

// TypeTuple is `tuple([T1 T2 T3])`.
type TypeTuple struct {
	S        Span
	Elements []TypeExpr
}

func (n *TypeTuple) Span() Span    { return n.S }
func (n *TypeTuple) exprNode()     {}
func (n *TypeTuple) typeExprNode() {}

// TypeOptional is `optional(T)` or `optional(T default-value)`.
//
// Optionality implies nullability - wrapping with `optional()` allows null
// values; bare types do not. Default is nil when not provided (the wrapper
// then defaults to null).
type TypeOptional struct {
	S       Span
	Elem    TypeExpr
	Default Expr // Is nil if not provided.
}

func (n *TypeOptional) Span() Span    { return n.S }
func (n *TypeOptional) exprNode()     {}
func (n *TypeOptional) typeExprNode() {}
