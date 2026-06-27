package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
)

// EncodeNode renders a parsed `lang` AST node as a Go expression that
// constructs an equivalent value. The expression evaluates to the
// matching `lang` pointer type (`*lang.File` for a File, `*lang.Field`
// for a Field, the matching `*lang.X` for any expression node).
//
// Source positions are not encoded; the produced node has zero Span
// values. The encoder handles every plain expression node and every
// `TypeExpr` node; callers must not pass a node kind the parser does
// not produce as part of a body.
func EncodeNode(n lang.Node) (string, error) {
	return encodeNodeString(n, nil)
}

func encodeNodeString(n lang.Node, spanName SyntaxSpanNamer) (string, error) {
	var b strings.Builder
	if err := encodeNodeWithSpans(&b, n, spanName); err != nil {
		return "", err
	}
	return b.String(), nil
}

func encodeNode(b *strings.Builder, n lang.Node) error {
	return encodeNodeWithSpans(b, n, nil)
}

func encodeNodeWithSpans(b *strings.Builder, n lang.Node, spanName SyntaxSpanNamer) error {
	switch x := n.(type) {
	case *lang.File:
		return encodeFile(b, x, spanName)
	case *lang.Field:
		return encodeField(b, x, true, spanName)
	case *lang.SelectorBody:
		return encodeSelectorBody(b, x, spanName)
	case *lang.ObjectLit:
		return encodeObjectLit(b, x, spanName)
	case *lang.ArrayLit:
		return encodeArrayLit(b, x, spanName)
	case *lang.StringLit:
		return encodeStringLit(b, x, spanName)
	case *lang.InterpolatedString:
		return encodeInterpolatedString(b, x, spanName)
	case *lang.NumberLit:
		return encodeNumberLit(b, x, spanName)
	case *lang.BoolLit:
		return encodeBoolLit(b, x, spanName)
	case *lang.NullLit:
		return encodeNullLit(b, x, spanName)
	case *lang.Ident:
		return encodeIdent(b, x, spanName)
	case *lang.DotPath:
		return encodeDotPath(b, x, spanName)
	case *lang.Call:
		return encodeCall(b, x, spanName)
	case *lang.Infix:
		return encodeInfix(b, x, spanName)
	case *lang.Prefix:
		return encodePrefix(b, x, spanName)
	case *lang.Conditional:
		return encodeConditional(b, x, spanName)
	case *lang.Comprehension:
		return encodeComprehension(b, x, spanName)
	case *lang.TypeAtomic:
		return encodeTypeAtomic(b, x, spanName)
	case *lang.TypeList:
		return encodeTypeList(b, x, spanName)
	case *lang.TypeMap:
		return encodeTypeMap(b, x, spanName)
	case *lang.TypeObject:
		return encodeTypeObject(b, x, spanName)
	case *lang.TypeTuple:
		return encodeTypeTuple(b, x, spanName)
	case *lang.TypeOptional:
		return encodeTypeOptional(b, x, spanName)
	case *lang.TypeLibraryConfig:
		return encodeTypeLibraryConfig(b, x, spanName)
	case nil:
		b.WriteString("nil")
		return nil
	default:
		return fmt.Errorf("encode: unsupported node type %T", n)
	}
}

func encodeFile(b *strings.Builder, n *lang.File, spanName SyntaxSpanNamer) error {
	b.WriteString("&lang.File{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	fields.next(b, "Kind")
	b.WriteString(fileKindIdent(n.Kind))
	if n.Path != "" {
		fields.next(b, "Path")
		b.WriteString(strconv.Quote(n.Path))
	}
	if n.Body != nil {
		fields.next(b, "Body")
		if err := encodeObjectLit(b, n.Body, spanName); err != nil {
			return err
		}
	}
	b.WriteString("}")
	return nil
}

func encodeField(b *strings.Builder, n *lang.Field, typed bool, spanName SyntaxSpanNamer) error {
	if typed {
		b.WriteString("&lang.Field")
	}
	b.WriteString("{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	fields.next(b, "Key")
	encodeFieldKey(b, n.Key, spanName)
	if n.Decl != nil {
		fields.next(b, "Decl")
		if err := encodeSelectorBody(b, n.Decl, spanName); err != nil {
			return err
		}
	} else {
		fields.next(b, "Value")
		if err := encodeNodeWithSpans(b, n.Value, spanName); err != nil {
			return err
		}
	}
	b.WriteString("}")
	return nil
}

func encodeFieldKey(b *strings.Builder, k lang.FieldKey, spanName SyntaxSpanNamer) {
	b.WriteString("lang.FieldKey{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, k.S, spanName)
	fields.next(b, "Kind")
	switch k.Kind {
	case lang.FieldString:
		b.WriteString("lang.FieldString")
	case lang.FieldPath:
		b.WriteString("lang.FieldPath")
	default:
		b.WriteString("lang.FieldIdent")
	}
	if k.Name != "" {
		fields.next(b, "Name")
		b.WriteString(strconv.Quote(k.Name))
	}
	if k.String != "" {
		fields.next(b, "String")
		b.WriteString(strconv.Quote(k.String))
	}
	if len(k.Path) > 0 {
		fields.next(b, "Path")
		b.WriteString("[]string{")
		for i, seg := range k.Path {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(strconv.Quote(seg))
		}
		b.WriteString("}")
	}
	b.WriteString("}")
}

func encodeSelectorBody(b *strings.Builder, n *lang.SelectorBody, spanName SyntaxSpanNamer) error {
	b.WriteString("&lang.SelectorBody{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	if n.Default {
		fields.next(b, "Default")
		b.WriteString("true")
	}
	fields.next(b, "Selector")
	encodeSelector(b, n.Selector, spanName)
	if n.Body != nil {
		fields.next(b, "Body")
		if err := encodeObjectLit(b, n.Body, spanName); err != nil {
			return err
		}
	}
	b.WriteString("}")
	return nil
}

func encodeSelector(b *strings.Builder, sel lang.Selector, spanName SyntaxSpanNamer) {
	b.WriteString("lang.Selector{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, sel.S, spanName)
	fields.next(b, "Parts")
	b.WriteString("[]lang.Ident{")
	for i, part := range sel.Parts {
		if i > 0 {
			b.WriteString(", ")
		}
		encodeIdentValue(b, part, spanName)
	}
	b.WriteString("}}")
}

func encodeObjectLit(b *strings.Builder, n *lang.ObjectLit, spanName SyntaxSpanNamer) error {
	b.WriteString("&lang.ObjectLit{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	fields.next(b, "Fields")
	b.WriteString("[]*lang.Field{")
	for i, f := range n.Fields {
		if i > 0 {
			b.WriteString(", ")
		}
		if err := encodeField(b, f, false, spanName); err != nil {
			return err
		}
	}
	b.WriteString("}}")
	return nil
}

func encodeArrayLit(b *strings.Builder, n *lang.ArrayLit, spanName SyntaxSpanNamer) error {
	b.WriteString("&lang.ArrayLit{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	fields.next(b, "Elements")
	b.WriteString("[]lang.Expr{")
	for i, e := range n.Elements {
		if i > 0 {
			b.WriteString(", ")
		}
		if err := encodeNodeWithSpans(b, e, spanName); err != nil {
			return err
		}
	}
	b.WriteString("}}")
	return nil
}

func encodeStringLit(b *strings.Builder, n *lang.StringLit, spanName SyntaxSpanNamer) error {
	b.WriteString("&lang.StringLit{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	fields.next(b, "Value")
	b.WriteString(strconv.Quote(n.Value))
	if n.Form != lang.StringSingleQuoted {
		fields.next(b, "Form")
		b.WriteString("lang.")
		b.WriteString(n.Form.String())
	}
	b.WriteString("}")
	return nil
}

func encodeInterpolatedString(
	b *strings.Builder,
	n *lang.InterpolatedString,
	spanName SyntaxSpanNamer,
) error {
	b.WriteString("&lang.InterpolatedString{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	if n.Form != lang.StringSingleQuoted {
		fields.next(b, "Form")
		b.WriteString("lang.")
		b.WriteString(n.Form.String())
	}
	fields.next(b, "Parts")
	b.WriteString("[]lang.InterpolatedPart{")
	for _, part := range n.Parts {
		if err := encodeInterpolatedPart(b, part, spanName); err != nil {
			return err
		}
		b.WriteString(", ")
	}
	b.WriteString("}}")
	return nil
}

func encodeInterpolatedPart(
	b *strings.Builder,
	part lang.InterpolatedPart,
	spanName SyntaxSpanNamer,
) error {
	b.WriteString("{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, part.S, spanName)
	if part.Expr == nil {
		fields.next(b, "Lit")
		b.WriteString(strconv.Quote(part.Lit))
	} else {
		fields.next(b, "Expr")
		if err := encodeNodeWithSpans(b, part.Expr, spanName); err != nil {
			return err
		}
		if part.Verb != "" {
			fields.next(b, "Verb")
			b.WriteString(strconv.Quote(part.Verb))
		}
	}
	b.WriteString("}")
	return nil
}

func encodeNumberLit(b *strings.Builder, n *lang.NumberLit, spanName SyntaxSpanNamer) error {
	b.WriteString("&lang.NumberLit{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	fields.next(b, "Value")
	b.WriteString(strconv.Quote(n.Value))
	if n.IsFloat {
		fields.next(b, "IsFloat")
		b.WriteString("true")
		fields.next(b, "ParsedFloat")
		b.WriteString(strconv.FormatFloat(n.ParsedFloat, 'g', -1, 64))
	} else {
		fields.next(b, "ParsedInt")
		fmt.Fprintf(b, "%d", n.ParsedInt)
	}
	b.WriteString("}")
	return nil
}

func encodeBoolLit(b *strings.Builder, n *lang.BoolLit, spanName SyntaxSpanNamer) error {
	b.WriteString("&lang.BoolLit{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	fields.next(b, "Value")
	fmt.Fprintf(b, "%t", n.Value)
	b.WriteString("}")
	return nil
}

func encodeNullLit(b *strings.Builder, n *lang.NullLit, spanName SyntaxSpanNamer) error {
	b.WriteString("&lang.NullLit{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	b.WriteString("}")
	return nil
}

func encodeIdent(b *strings.Builder, n *lang.Ident, spanName SyntaxSpanNamer) error {
	b.WriteByte('&')
	encodeIdentValue(b, *n, spanName)
	return nil
}

func encodeIdentValue(b *strings.Builder, n lang.Ident, spanName SyntaxSpanNamer) {
	b.WriteString("lang.Ident{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	fields.next(b, "Name")
	b.WriteString(strconv.Quote(n.Name))
	b.WriteString("}")
}

func encodeDotPath(b *strings.Builder, n *lang.DotPath, spanName SyntaxSpanNamer) error {
	b.WriteString("&lang.DotPath{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	if n.Root != nil {
		fields.next(b, "Root")
		if err := encodeIdent(b, n.Root, spanName); err != nil {
			return err
		}
	}
	if len(n.Segments) > 0 {
		fields.next(b, "Segments")
		b.WriteString("[]lang.DotSegment{")
		for i, s := range n.Segments {
			if i > 0 {
				b.WriteString(", ")
			}
			if err := encodeDotSegment(b, s, spanName); err != nil {
				return err
			}
		}
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeDotSegment(b *strings.Builder, s lang.DotSegment, spanName SyntaxSpanNamer) error {
	b.WriteString("{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, s.S, spanName)
	if s.Name != "" {
		fields.next(b, "Name")
		b.WriteString(strconv.Quote(s.Name))
	}
	if s.Index != nil {
		fields.next(b, "Index")
		if err := encodeNodeWithSpans(b, s.Index, spanName); err != nil {
			return err
		}
	}
	if s.Splat {
		fields.next(b, "Splat")
		b.WriteString("true")
	}
	if s.Guarded {
		fields.next(b, "Guarded")
		b.WriteString("true")
	}
	b.WriteString("}")
	return nil
}

func encodeCall(b *strings.Builder, n *lang.Call, spanName SyntaxSpanNamer) error {
	b.WriteString("&lang.Call{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	if n.Callee != nil {
		fields.next(b, "Callee")
		if err := encodeIdent(b, n.Callee, spanName); err != nil {
			return err
		}
	}
	if n.Library != nil {
		fields.next(b, "Library")
		if err := encodeIdent(b, n.Library, spanName); err != nil {
			return err
		}
	}
	if n.Func != nil {
		fields.next(b, "Func")
		if err := encodeIdent(b, n.Func, spanName); err != nil {
			return err
		}
	}
	if len(n.Args) > 0 {
		fields.next(b, "Args")
		b.WriteString("[]lang.Expr{")
		for i, a := range n.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			if err := encodeNodeWithSpans(b, a, spanName); err != nil {
				return err
			}
		}
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeInfix(b *strings.Builder, n *lang.Infix, spanName SyntaxSpanNamer) error {
	b.WriteString("&lang.Infix{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	fields.next(b, "Op")
	b.WriteString(strconv.Quote(n.Op))
	fields.next(b, "Left")
	if err := encodeNodeWithSpans(b, n.Left, spanName); err != nil {
		return err
	}
	fields.next(b, "Right")
	if err := encodeNodeWithSpans(b, n.Right, spanName); err != nil {
		return err
	}
	b.WriteString("}")
	return nil
}

func encodePrefix(b *strings.Builder, n *lang.Prefix, spanName SyntaxSpanNamer) error {
	b.WriteString("&lang.Prefix{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	fields.next(b, "Op")
	b.WriteString(strconv.Quote(n.Op))
	fields.next(b, "Expr")
	if err := encodeNodeWithSpans(b, n.Expr, spanName); err != nil {
		return err
	}
	b.WriteString("}")
	return nil
}

func encodeConditional(b *strings.Builder, n *lang.Conditional, spanName SyntaxSpanNamer) error {
	b.WriteString("&lang.Conditional{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	fields.next(b, "Cond")
	if err := encodeNodeWithSpans(b, n.Cond, spanName); err != nil {
		return err
	}
	fields.next(b, "Then")
	if err := encodeNodeWithSpans(b, n.Then, spanName); err != nil {
		return err
	}
	fields.next(b, "Else")
	if err := encodeNodeWithSpans(b, n.Else, spanName); err != nil {
		return err
	}
	b.WriteString("}")
	return nil
}

func encodeComprehension(
	b *strings.Builder,
	n *lang.Comprehension,
	spanName SyntaxSpanNamer,
) error {
	b.WriteString("&lang.Comprehension{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	fields.next(b, "Kind")
	fmt.Fprintf(b, "lang.%s", n.Kind.String())
	if len(n.Names) > 0 {
		fields.next(b, "Names")
		b.WriteString("[]string{")
		for i, name := range n.Names {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(strconv.Quote(name))
		}
		b.WriteString("}")
	}
	fields.next(b, "Source")
	if err := encodeNodeWithSpans(b, n.Source, spanName); err != nil {
		return err
	}
	if n.Key != nil {
		fields.next(b, "Key")
		if err := encodeNodeWithSpans(b, n.Key, spanName); err != nil {
			return err
		}
	}
	fields.next(b, "Value")
	if err := encodeNodeWithSpans(b, n.Value, spanName); err != nil {
		return err
	}
	if n.Group {
		fields.next(b, "Group")
		b.WriteString("true")
	}
	if n.Filter != nil {
		fields.next(b, "Filter")
		if err := encodeNodeWithSpans(b, n.Filter, spanName); err != nil {
			return err
		}
	}
	b.WriteString("}")
	return nil
}

func encodeTypeAtomic(b *strings.Builder, n *lang.TypeAtomic, spanName SyntaxSpanNamer) error {
	b.WriteString("&lang.TypeAtomic{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	fields.next(b, "Name")
	b.WriteString(strconv.Quote(n.Name))
	b.WriteString("}")
	return nil
}

func encodeTypeList(b *strings.Builder, n *lang.TypeList, spanName SyntaxSpanNamer) error {
	b.WriteString("&lang.TypeList{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	fields.next(b, "Elem")
	if err := encodeNodeWithSpans(b, n.Elem, spanName); err != nil {
		return err
	}
	b.WriteString("}")
	return nil
}

func encodeTypeMap(b *strings.Builder, n *lang.TypeMap, spanName SyntaxSpanNamer) error {
	b.WriteString("&lang.TypeMap{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	fields.next(b, "Elem")
	if err := encodeNodeWithSpans(b, n.Elem, spanName); err != nil {
		return err
	}
	b.WriteString("}")
	return nil
}

func encodeTypeObject(b *strings.Builder, n *lang.TypeObject, spanName SyntaxSpanNamer) error {
	b.WriteString("&lang.TypeObject{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	if n.Open {
		fields.next(b, "Open")
		b.WriteString("true")
	}
	fields.next(b, "Fields")
	b.WriteString("[]*lang.TypeObjectField{")
	for i, f := range n.Fields {
		if i > 0 {
			b.WriteString(", ")
		}
		if err := encodeTypeObjectField(b, f, spanName); err != nil {
			return err
		}
	}
	b.WriteString("}}")
	return nil
}

func encodeTypeObjectField(
	b *strings.Builder,
	f *lang.TypeObjectField,
	spanName SyntaxSpanNamer,
) error {
	b.WriteString("{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, f.S, spanName)
	fields.next(b, "Name")
	b.WriteString(strconv.Quote(f.Name))
	if f.Type != nil {
		fields.next(b, "Type")
		if err := encodeNodeWithSpans(b, f.Type, spanName); err != nil {
			return err
		}
	}
	if f.Decl != nil {
		fields.next(b, "Decl")
		if err := encodeObjectLit(b, f.Decl, spanName); err != nil {
			return err
		}
	}
	b.WriteString("}")
	return nil
}

func encodeTypeTuple(b *strings.Builder, n *lang.TypeTuple, spanName SyntaxSpanNamer) error {
	b.WriteString("&lang.TypeTuple{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	fields.next(b, "Elements")
	b.WriteString("[]lang.TypeExpr{")
	for i, e := range n.Elements {
		if i > 0 {
			b.WriteString(", ")
		}
		if err := encodeNodeWithSpans(b, e, spanName); err != nil {
			return err
		}
	}
	b.WriteString("}}")
	return nil
}

func encodeTypeOptional(b *strings.Builder, n *lang.TypeOptional, spanName SyntaxSpanNamer) error {
	b.WriteString("&lang.TypeOptional{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	fields.next(b, "Elem")
	if err := encodeNodeWithSpans(b, n.Elem, spanName); err != nil {
		return err
	}
	b.WriteString("}")
	return nil
}

func encodeTypeLibraryConfig(
	b *strings.Builder,
	n *lang.TypeLibraryConfig,
	spanName SyntaxSpanNamer,
) error {
	b.WriteString("&lang.TypeLibraryConfig{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	if n.Path != nil {
		fields.next(b, "Path")
		if err := encodeStringLit(b, n.Path, spanName); err != nil {
			return err
		}
	}
	b.WriteString("}")
	return nil
}

func fileKindIdent(k lang.FileKind) string {
	switch k {
	case lang.FileFactory:
		return "lang.FileFactory"
	case lang.FileExportedType:
		return "lang.FileExportedType"
	case lang.FileStack:
		return "lang.FileStack"
	default:
		return "lang.FileUnknown"
	}
}
