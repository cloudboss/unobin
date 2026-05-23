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
	var b strings.Builder
	if err := encodeNode(&b, n); err != nil {
		return "", err
	}
	return b.String(), nil
}

func encodeNode(b *strings.Builder, n lang.Node) error {
	switch x := n.(type) {
	case *lang.File:
		return encodeFile(b, x)
	case *lang.Field:
		return encodeField(b, x)
	case *lang.ObjectLit:
		return encodeObjectLit(b, x)
	case *lang.ArrayLit:
		return encodeArrayLit(b, x)
	case *lang.StringLit:
		return encodeStringLit(b, x)
	case *lang.NumberLit:
		return encodeNumberLit(b, x)
	case *lang.BoolLit:
		return encodeBoolLit(b, x)
	case *lang.NullLit:
		return encodeNullLit(b, x)
	case *lang.Ident:
		return encodeIdent(b, x)
	case *lang.DotPath:
		return encodeDotPath(b, x)
	case *lang.Call:
		return encodeCall(b, x)
	case *lang.Infix:
		return encodeInfix(b, x)
	case *lang.Prefix:
		return encodePrefix(b, x)
	case *lang.Conditional:
		return encodeConditional(b, x)
	case *lang.Comprehension:
		return encodeComprehension(b, x)
	case *lang.TypeAtomic:
		return encodeTypeAtomic(b, x)
	case *lang.TypeList:
		return encodeTypeList(b, x)
	case *lang.TypeSet:
		return encodeTypeSet(b, x)
	case *lang.TypeMap:
		return encodeTypeMap(b, x)
	case *lang.TypeObject:
		return encodeTypeObject(b, x)
	case *lang.TypeTuple:
		return encodeTypeTuple(b, x)
	case *lang.TypeOptional:
		return encodeTypeOptional(b, x)
	case nil:
		b.WriteString("nil")
		return nil
	default:
		return fmt.Errorf("encode: unsupported node type %T", n)
	}
}

func encodeFile(b *strings.Builder, n *lang.File) error {
	b.WriteString("&lang.File{")
	fmt.Fprintf(b, "Kind: %s", fileKindIdent(n.Kind))
	if n.Path != "" {
		fmt.Fprintf(b, ", Path: %s", strconv.Quote(n.Path))
	}
	if n.Body != nil {
		b.WriteString(", Body: ")
		if err := encodeObjectLit(b, n.Body); err != nil {
			return err
		}
	}
	b.WriteString("}")
	return nil
}

func encodeField(b *strings.Builder, n *lang.Field) error {
	b.WriteString("{Key: ")
	encodeFieldKey(b, n.Key)
	b.WriteString(", Value: ")
	if err := encodeNode(b, n.Value); err != nil {
		return err
	}
	b.WriteString("}")
	return nil
}

func encodeFieldKey(b *strings.Builder, k lang.FieldKey) {
	b.WriteString("lang.FieldKey{Kind: ")
	switch k.Kind {
	case lang.FieldString:
		b.WriteString("lang.FieldString")
	default:
		b.WriteString("lang.FieldIdent")
	}
	if k.Name != "" {
		fmt.Fprintf(b, ", Name: %s", strconv.Quote(k.Name))
	}
	if k.String != "" {
		fmt.Fprintf(b, ", String: %s", strconv.Quote(k.String))
	}
	b.WriteString("}")
}

func encodeObjectLit(b *strings.Builder, n *lang.ObjectLit) error {
	b.WriteString("&lang.ObjectLit{Fields: []*lang.Field{")
	for i, f := range n.Fields {
		if i > 0 {
			b.WriteString(", ")
		}
		if err := encodeField(b, f); err != nil {
			return err
		}
	}
	b.WriteString("}}")
	return nil
}

func encodeArrayLit(b *strings.Builder, n *lang.ArrayLit) error {
	b.WriteString("&lang.ArrayLit{Elements: []lang.Expr{")
	for i, e := range n.Elements {
		if i > 0 {
			b.WriteString(", ")
		}
		if err := encodeNode(b, e); err != nil {
			return err
		}
	}
	b.WriteString("}}")
	return nil
}

func encodeStringLit(b *strings.Builder, n *lang.StringLit) error {
	b.WriteString("&lang.StringLit{Value: ")
	b.WriteString(strconv.Quote(n.Value))
	if n.Form != lang.StringSingleQuoted {
		b.WriteString(", Form: lang.")
		b.WriteString(n.Form.String())
	}
	b.WriteString("}")
	return nil
}

func encodeNumberLit(b *strings.Builder, n *lang.NumberLit) error {
	b.WriteString("&lang.NumberLit{Value: ")
	b.WriteString(strconv.Quote(n.Value))
	if n.IsFloat {
		fmt.Fprintf(b, ", IsFloat: true, ParsedFloat: %s", strconv.FormatFloat(n.ParsedFloat, 'g', -1, 64))
	} else {
		fmt.Fprintf(b, ", ParsedInt: %d", n.ParsedInt)
	}
	b.WriteString("}")
	return nil
}

func encodeBoolLit(b *strings.Builder, n *lang.BoolLit) error {
	fmt.Fprintf(b, "&lang.BoolLit{Value: %t}", n.Value)
	return nil
}

func encodeNullLit(b *strings.Builder, _ *lang.NullLit) error {
	b.WriteString("&lang.NullLit{}")
	return nil
}

func encodeIdent(b *strings.Builder, n *lang.Ident) error {
	fmt.Fprintf(b, "&lang.Ident{Name: %s}", strconv.Quote(n.Name))
	return nil
}

func encodeDotPath(b *strings.Builder, n *lang.DotPath) error {
	b.WriteString("&lang.DotPath{")
	if n.Root != nil {
		b.WriteString("Root: ")
		if err := encodeIdent(b, n.Root); err != nil {
			return err
		}
	}
	if len(n.Segments) > 0 {
		if n.Root != nil {
			b.WriteString(", ")
		}
		b.WriteString("Segments: []lang.DotSegment{")
		for i, s := range n.Segments {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString("{")
			if s.Name != "" {
				fmt.Fprintf(b, "Name: %s", strconv.Quote(s.Name))
			}
			if s.Index != nil {
				if s.Name != "" {
					b.WriteString(", ")
				}
				b.WriteString("Index: ")
				if err := encodeNode(b, s.Index); err != nil {
					return err
				}
			}
			b.WriteString("}")
		}
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeCall(b *strings.Builder, n *lang.Call) error {
	b.WriteString("&lang.Call{")
	first := true
	if n.Callee != nil {
		b.WriteString("Callee: ")
		if err := encodeIdent(b, n.Callee); err != nil {
			return err
		}
		first = false
	}
	if n.Module != nil {
		if !first {
			b.WriteString(", ")
		}
		b.WriteString("Module: ")
		if err := encodeIdent(b, n.Module); err != nil {
			return err
		}
		first = false
	}
	if n.Func != nil {
		if !first {
			b.WriteString(", ")
		}
		b.WriteString("Func: ")
		if err := encodeIdent(b, n.Func); err != nil {
			return err
		}
		first = false
	}
	if len(n.Args) > 0 {
		if !first {
			b.WriteString(", ")
		}
		b.WriteString("Args: []lang.Expr{")
		for i, a := range n.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			if err := encodeNode(b, a); err != nil {
				return err
			}
		}
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeInfix(b *strings.Builder, n *lang.Infix) error {
	b.WriteString("&lang.Infix{Op: ")
	b.WriteString(strconv.Quote(n.Op))
	b.WriteString(", Left: ")
	if err := encodeNode(b, n.Left); err != nil {
		return err
	}
	b.WriteString(", Right: ")
	if err := encodeNode(b, n.Right); err != nil {
		return err
	}
	b.WriteString("}")
	return nil
}

func encodePrefix(b *strings.Builder, n *lang.Prefix) error {
	b.WriteString("&lang.Prefix{Op: ")
	b.WriteString(strconv.Quote(n.Op))
	b.WriteString(", Expr: ")
	if err := encodeNode(b, n.Expr); err != nil {
		return err
	}
	b.WriteString("}")
	return nil
}

func encodeConditional(b *strings.Builder, n *lang.Conditional) error {
	b.WriteString("&lang.Conditional{Cond: ")
	if err := encodeNode(b, n.Cond); err != nil {
		return err
	}
	b.WriteString(", Then: ")
	if err := encodeNode(b, n.Then); err != nil {
		return err
	}
	b.WriteString(", Else: ")
	if err := encodeNode(b, n.Else); err != nil {
		return err
	}
	b.WriteString("}")
	return nil
}

func encodeComprehension(b *strings.Builder, n *lang.Comprehension) error {
	fmt.Fprintf(b, "&lang.Comprehension{Kind: lang.%s", n.Kind.String())
	if len(n.Names) > 0 {
		b.WriteString(", Names: []string{")
		for i, name := range n.Names {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(strconv.Quote(name))
		}
		b.WriteString("}")
	}
	b.WriteString(", Source: ")
	if err := encodeNode(b, n.Source); err != nil {
		return err
	}
	if n.Key != nil {
		b.WriteString(", Key: ")
		if err := encodeNode(b, n.Key); err != nil {
			return err
		}
	}
	b.WriteString(", Value: ")
	if err := encodeNode(b, n.Value); err != nil {
		return err
	}
	if n.Group {
		b.WriteString(", Group: true")
	}
	if n.Filter != nil {
		b.WriteString(", Filter: ")
		if err := encodeNode(b, n.Filter); err != nil {
			return err
		}
	}
	b.WriteString("}")
	return nil
}

func encodeTypeAtomic(b *strings.Builder, n *lang.TypeAtomic) error {
	fmt.Fprintf(b, "&lang.TypeAtomic{Name: %s}", strconv.Quote(n.Name))
	return nil
}

func encodeTypeList(b *strings.Builder, n *lang.TypeList) error {
	b.WriteString("&lang.TypeList{Elem: ")
	if err := encodeNode(b, n.Elem); err != nil {
		return err
	}
	b.WriteString("}")
	return nil
}

func encodeTypeSet(b *strings.Builder, n *lang.TypeSet) error {
	b.WriteString("&lang.TypeSet{Elem: ")
	if err := encodeNode(b, n.Elem); err != nil {
		return err
	}
	b.WriteString("}")
	return nil
}

func encodeTypeMap(b *strings.Builder, n *lang.TypeMap) error {
	b.WriteString("&lang.TypeMap{Elem: ")
	if err := encodeNode(b, n.Elem); err != nil {
		return err
	}
	b.WriteString("}")
	return nil
}

func encodeTypeObject(b *strings.Builder, n *lang.TypeObject) error {
	b.WriteString("&lang.TypeObject{Fields: []*lang.TypeObjectField{")
	for i, f := range n.Fields {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(b, "{Name: %s", strconv.Quote(f.Name))
		if f.Type != nil {
			b.WriteString(", Type: ")
			if err := encodeNode(b, f.Type); err != nil {
				return err
			}
		}
		if f.Decl != nil {
			b.WriteString(", Decl: ")
			if err := encodeObjectLit(b, f.Decl); err != nil {
				return err
			}
		}
		b.WriteString("}")
	}
	b.WriteString("}}")
	return nil
}

func encodeTypeTuple(b *strings.Builder, n *lang.TypeTuple) error {
	b.WriteString("&lang.TypeTuple{Elements: []lang.TypeExpr{")
	for i, e := range n.Elements {
		if i > 0 {
			b.WriteString(", ")
		}
		if err := encodeNode(b, e); err != nil {
			return err
		}
	}
	b.WriteString("}}")
	return nil
}

func encodeTypeOptional(b *strings.Builder, n *lang.TypeOptional) error {
	b.WriteString("&lang.TypeOptional{Elem: ")
	if err := encodeNode(b, n.Elem); err != nil {
		return err
	}
	if n.Default != nil {
		b.WriteString(", Default: ")
		if err := encodeNode(b, n.Default); err != nil {
			return err
		}
	}
	b.WriteString("}")
	return nil
}

func fileKindIdent(k lang.FileKind) string {
	switch k {
	case lang.FileStack:
		return "lang.FileStack"
	case lang.FileModule:
		return "lang.FileModule"
	case lang.FileExportedType:
		return "lang.FileExportedType"
	case lang.FileConfig:
		return "lang.FileConfig"
	default:
		return "lang.FileUnknown"
	}
}
