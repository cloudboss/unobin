package lang

import "github.com/cloudboss/unobin/pkg/lang/parse"

type (
	Node               = parse.Node
	File               = parse.File
	FileKind           = parse.FileKind
	Field              = parse.Field
	FieldKey           = parse.FieldKey
	FieldKeyKind       = parse.FieldKeyKind
	SelectorBody       = parse.SelectorBody
	Selector           = parse.Selector
	StringForm         = parse.StringForm
	Expr               = parse.Expr
	ObjectLit          = parse.ObjectLit
	ArrayLit           = parse.ArrayLit
	StringLit          = parse.StringLit
	InterpolatedString = parse.InterpolatedString
	InterpolatedPart   = parse.InterpolatedPart
	NumberLit          = parse.NumberLit
	BoolLit            = parse.BoolLit
	NullLit            = parse.NullLit
	Ident              = parse.Ident
	DotPath            = parse.DotPath
	DotSegment         = parse.DotSegment
	Call               = parse.Call
	Infix              = parse.Infix
	Prefix             = parse.Prefix
	Conditional        = parse.Conditional
	Comprehension      = parse.Comprehension
	ComprehensionKind  = parse.ComprehensionKind
	TypeExpr           = parse.TypeExpr
	TypeAtomic         = parse.TypeAtomic
	TypeList           = parse.TypeList
	TypeMap            = parse.TypeMap
	TypeObject         = parse.TypeObject
	TypeObjectField    = parse.TypeObjectField
	TypeTuple          = parse.TypeTuple
	TypeOptional       = parse.TypeOptional
	Comment            = parse.Comment
	Span               = parse.Span
	Position           = parse.Position
	Error              = parse.Error
	ErrorList          = parse.ErrorList
	ErrorKind          = parse.ErrorKind
)

const (
	FileUnknown      = parse.FileUnknown
	FileFactory      = parse.FileFactory
	FileExportedType = parse.FileExportedType
	FileConfig       = parse.FileConfig
	FileManifest     = parse.FileManifest

	FieldIdent  = parse.FieldIdent
	FieldString = parse.FieldString
	FieldPath   = parse.FieldPath

	CompList = parse.CompList
	CompMap  = parse.CompMap

	StringSingleQuoted          = parse.StringSingleQuoted
	StringTripleQuoteSingleLine = parse.StringTripleQuoteSingleLine
	StringLiteralClip           = parse.StringLiteralClip
	StringLiteralStrip          = parse.StringLiteralStrip
	StringFoldedClip            = parse.StringFoldedClip
	StringFoldedStrip           = parse.StringFoldedStrip
	StringJoinedClip            = parse.StringJoinedClip
	StringJoinedStrip           = parse.StringJoinedStrip

	ErrUnknown = parse.ErrUnknown
	ErrParse   = parse.ErrParse
	ErrLex     = parse.ErrLex
	ErrSchema  = parse.ErrSchema
	ErrType    = parse.ErrType
	ErrResolve = parse.ErrResolve
)

var (
	NewErrorList  = parse.NewErrorList
	Errorf        = parse.Errorf
	PascalToKebab = parse.PascalToKebab
)

// ParseSource reads .ub source from b and returns the parsed File.
// Source role classification belongs to pkg/lang/syntax; callers that
// intentionally use the generic file validator set File.Kind explicitly.
func ParseSource(path string, b []byte) (*File, error) {
	return parse.ParseSource(path, b)
}

// ParseExpr parses a single unobin expression from b. It wraps
// parse.ParseExpr so callers depend on pkg/lang alone, such as goschema
// synthesizing a constraint's when/require expression from Go source.
func ParseExpr(path string, b []byte) (Expr, error) {
	return parse.ParseExpr(path, b)
}

// ParseType parses a single unobin type expression from b.
func ParseType(path string, b []byte) (TypeExpr, error) {
	return parse.ParseType(path, b)
}

// ParseTypeAt parses a single unobin type expression whose first byte
// starts at base in the source file.
func ParseTypeAt(path string, b []byte, base Position) (TypeExpr, error) {
	return parse.ParseTypeAt(path, b, base)
}
