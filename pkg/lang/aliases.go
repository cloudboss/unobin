package lang

import "github.com/cloudboss/unobin/pkg/lang/parse"

type (
	Node             = parse.Node
	File             = parse.File
	FileKind         = parse.FileKind
	Field            = parse.Field
	FieldKey         = parse.FieldKey
	FieldKeyKind     = parse.FieldKeyKind
	StringForm       = parse.StringForm
	Expr             = parse.Expr
	ObjectLit        = parse.ObjectLit
	ArrayLit         = parse.ArrayLit
	StringLit        = parse.StringLit
	NumberLit        = parse.NumberLit
	BoolLit          = parse.BoolLit
	NullLit          = parse.NullLit
	Ident            = parse.Ident
	DotPath          = parse.DotPath
	DotSegment       = parse.DotSegment
	Call             = parse.Call
	Infix            = parse.Infix
	Prefix           = parse.Prefix
	TypeExpr         = parse.TypeExpr
	TypeAtomic       = parse.TypeAtomic
	TypeList         = parse.TypeList
	TypeSet          = parse.TypeSet
	TypeMap          = parse.TypeMap
	TypeObject       = parse.TypeObject
	TypeObjectField  = parse.TypeObjectField
	TypeTuple        = parse.TypeTuple
	TypeOptional     = parse.TypeOptional
	Comment          = parse.Comment
	Span             = parse.Span
	Position         = parse.Position
	Error            = parse.Error
	ErrorList        = parse.ErrorList
	ErrorKind        = parse.ErrorKind
)

const (
	FileUnknown      = parse.FileUnknown
	FileStack        = parse.FileStack
	FileModule       = parse.FileModule
	FileExportedType = parse.FileExportedType
	FileConfig       = parse.FileConfig

	FieldIdent  = parse.FieldIdent
	FieldString = parse.FieldString

	StringSingleQuoted       = parse.StringSingleQuoted
	StringBacktickSingleLine = parse.StringBacktickSingleLine
	StringLiteralClip        = parse.StringLiteralClip
	StringLiteralStrip       = parse.StringLiteralStrip
	StringFoldedClip         = parse.StringFoldedClip
	StringFoldedStrip        = parse.StringFoldedStrip
	StringJoinedClip         = parse.StringJoinedClip
	StringJoinedStrip        = parse.StringJoinedStrip

	ErrUnknown = parse.ErrUnknown
	ErrParse   = parse.ErrParse
	ErrLex     = parse.ErrLex
	ErrSchema  = parse.ErrSchema
	ErrType    = parse.ErrType
	ErrResolve = parse.ErrResolve
)

var (
	NewErrorList   = parse.NewErrorList
	Errorf         = parse.Errorf
	PascalToKebab  = parse.PascalToKebab
)

// ParseSource reads .ub source from b, returns the parsed File, and
// classifies the result via ClassifyByFilename so callers get a
// File.Kind without a separate step. The classification is what
// distinguishes this from parse.ParseSource.
func ParseSource(path string, b []byte) (*File, error) {
	f, err := parse.ParseSource(path, b)
	if err != nil {
		return nil, err
	}
	f.Kind = ClassifyByFilename(path)
	return f, nil
}
