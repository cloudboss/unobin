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

// StringLit is a string literal. Two source forms produce this node:
//
//   - Single quoted: `'hello\nworld'`. Backslash escapes are processed
//     during parsing, so Value holds the decoded content. Double quotes
//     are not a string delimiter.
//   - Backtick multiline: " ` ... ` ". Source indentation is *not* part
//     of the string value. The closing backtick's column is the strip
//     baseline; each content line's leading whitespace up to that column
//     is removed and Value holds the dedented content. The first newline
//     after the opening backtick is dropped. Lines with less indentation
//     than the baseline are a parse error.
//
// The Multiline flag distinguishes the two source forms (informational -
// the Value is already canonical either way).
type StringLit struct {
	S         Span
	Value     string
	Multiline bool
}

func (n *StringLit) Span() Span { return n.S }
func (n *StringLit) exprNode()  {}

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
