package syntax

import (
	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/stateref"
)

type FileKind int

const (
	FileUnknown FileKind = iota
	FileFactory
	FileStack
	FileProject
	FileProjectLock
	FileLibrary
)

type NodeKind string

const (
	NodeResource   NodeKind = "resource"
	NodeDataSource NodeKind = "data-source"
	NodeAction     NodeKind = "action"
)

type Ident struct {
	S    parse.Span
	Name string
}

type StringKey struct {
	S     parse.Span
	Value string
}

type SourceFileSpec struct {
	DisplayPath    string
	LibraryPath    string
	ProjectRelPath string
	PackageRelPath string
	LineStarts     []int
}

type File struct {
	S           parse.Span
	Kind        FileKind
	Path        string
	Factory     *FactoryFile
	Stack       *StackFile
	Project     *ProjectFile
	ProjectLock *ProjectLockFile
	Library     *LibraryFile
	Comments    []parse.Comment
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

type ProjectFile struct {
	S             parse.Span
	UnobinVersion *parse.StringLit
	Requires      []ProjectRequire
	Replace       []ProjectReplace
}

type ProjectLockFile struct {
	S         parse.Span
	Version   *parse.NumberLit
	Toolchain *ProjectLockToolchain
	Deps      []ProjectLockDep
}

type ProjectLockToolchain struct {
	S             parse.Span
	UnobinVersion *parse.StringLit
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
	LibraryConfigs []LibraryConfigDecl
	StateMoves     []StateMoveDecl
	Resources      []NodeDecl
	Data           []NodeDecl
	Actions        []NodeDecl
	Outputs        []OutputDecl
}

type StackFactoryBlock struct {
	S      parse.Span
	Pin    *parse.ObjectLit
	Inputs *parse.ObjectLit
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

type LibraryConfigDecl struct {
	S     parse.Span
	Alias Ident
	Value parse.Expr
}

type StateMoveDecl struct {
	S    parse.Span
	From *StateMoveRef
	To   *StateMoveRef
}

type StateMoveRef struct {
	S   parse.Span
	Ref stateref.EntryRef
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

type ProjectRequire struct {
	S        parse.Span
	ID       StringKey
	Version  *parse.StringLit
	Indirect *parse.BoolLit
}

type ProjectReplace struct {
	S    parse.Span
	ID   StringKey
	Path *parse.StringLit
}

type ProjectLockDep struct {
	S       parse.Span
	ID      StringKey
	Kind    Ident
	Version *parse.StringLit
	Commit  *parse.StringLit
	Hash    *parse.StringLit
}
