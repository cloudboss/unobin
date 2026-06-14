package syntax

import "github.com/cloudboss/unobin/pkg/lang/parse"

type FileKind int

const (
	FileUnknown FileKind = iota
	FileFactory
	FileStack
	FileManifest
	FileLibrary
)

type NodeKind string

const (
	NodeResource NodeKind = "resource"
	NodeData     NodeKind = "data"
	NodeAction   NodeKind = "action"
)

type Ident struct {
	S    parse.Span
	Name string
}

type StringKey struct {
	S     parse.Span
	Value string
}

type File struct {
	S        parse.Span
	Kind     FileKind
	Path     string
	Factory  *FactoryFile
	Stack    *StackFile
	Manifest *ManifestFile
	Library  *LibraryFile
	Comments []parse.Comment
}

type FactoryFile struct {
	S    parse.Span
	Body FactoryBody
}

type StackFile struct {
	S           parse.Span
	Factory     *StackFactoryBlock
	State       *StateDecl
	Encryption  *EncryptionDecl
	Locals      []LocalDecl
	Parallelism parse.Expr
}

type ManifestFile struct {
	S             parse.Span
	UnobinVersion *parse.StringLit
	Requires      []ManifestRequire
	Replace       []ManifestReplace
}

type LibraryFile struct {
	S       parse.Span
	Exports []CompositeDecl
}

type FactoryBody struct {
	S              parse.Span
	Description    *parse.StringLit
	Inputs         []InputDecl
	Locals         []LocalDecl
	Constraints    []ConstraintDecl
	Imports        []ImportDecl
	Configurations []ConfigurationDecl
	Resources      []NodeDecl
	Data           []NodeDecl
	Actions        []NodeDecl
	Outputs        []OutputDecl
}

type StackFactoryBlock struct {
	S              parse.Span
	Pin            *parse.ObjectLit
	Inputs         *parse.ObjectLit
	Configurations []ConfigurationValue
}

type InputDecl struct {
	S    parse.Span
	Name Ident
	Body *parse.ObjectLit
	Type parse.TypeExpr
}

type LocalDecl struct {
	S     parse.Span
	Name  Ident
	Value parse.Expr
}

type ConstraintDecl struct {
	S     parse.Span
	Value parse.Expr
}

type ImportDecl struct {
	S     parse.Span
	Alias Ident
	Ref   *parse.StringLit
}

type OutputDecl struct {
	S    parse.Span
	Name Ident
	Body *parse.ObjectLit
}

type NodeDecl struct {
	S        parse.Span
	Kind     NodeKind
	Name     Ident
	Selector NodeSelector
	Body     *parse.ObjectLit
}

type NodeSelector struct {
	S      parse.Span
	Alias  Ident
	Export Ident
}

type ConfigurationDecl struct {
	S        parse.Span
	Name     *Ident
	Selector Ident
	Body     *parse.ObjectLit
	Value    parse.Expr
}

type ConfigurationValue struct {
	S        parse.Span
	Name     *Ident
	Selector Ident
	Body     *parse.ObjectLit
	Value    parse.Expr
}

type StateDecl struct {
	S        parse.Span
	Selector Ident
	Body     *parse.ObjectLit
}

type EncryptionDecl struct {
	S        parse.Span
	Selector Ident
	Body     *parse.ObjectLit
}

type CompositeDecl struct {
	S    parse.Span
	Name Ident
	Kind NodeKind
	Body FactoryBody
}

type ManifestRequire struct {
	S       parse.Span
	ID      StringKey
	Version *parse.StringLit
}

type ManifestReplace struct {
	S    parse.Span
	ID   StringKey
	Path *parse.StringLit
}
