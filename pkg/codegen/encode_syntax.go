package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

// SyntaxSpanNamer names a compact generated expression for one source span.
type SyntaxSpanNamer func(parse.Span) string

// EncodeSyntaxFactoryBody renders a typed factory or composite body as a Go
// expression. Source positions are omitted; runtime graph extraction only needs
// declaration names, selectors, and expression bodies.
func EncodeSyntaxFactoryBody(n syntax.FactoryBody) (string, error) {
	var b strings.Builder
	if err := encodeSyntaxFactoryBody(&b, n, nil); err != nil {
		return "", err
	}
	return b.String(), nil
}

// EncodeSyntaxFactoryBodyWithSpans renders a body while preserving source spans.
func EncodeSyntaxFactoryBodyWithSpans(
	n syntax.FactoryBody,
	spanName SyntaxSpanNamer,
) (string, error) {
	var b strings.Builder
	if err := encodeSyntaxFactoryBody(&b, n, spanName); err != nil {
		return "", err
	}
	return b.String(), nil
}

func encodeSyntaxFactoryBody(
	b *strings.Builder,
	n syntax.FactoryBody,
	spanName SyntaxSpanNamer,
) error {
	b.WriteString("syntax.FactoryBody{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	if n.Description != nil {
		fields.next(b, "Description")
		s, err := encodeNodeString(n.Description, spanName)
		if err != nil {
			return err
		}
		b.WriteString(s)
	}
	if len(n.Inputs) > 0 {
		fields.next(b, "Inputs")
		if err := encodeSyntaxInputs(b, n.Inputs, spanName); err != nil {
			return err
		}
	}
	if len(n.Locals) > 0 {
		fields.next(b, "Locals")
		if err := encodeSyntaxLocals(b, n.Locals, spanName); err != nil {
			return err
		}
	}
	if len(n.Constraints) > 0 {
		fields.next(b, "Constraints")
		if err := encodeSyntaxConstraints(b, n.Constraints, spanName); err != nil {
			return err
		}
	}
	if len(n.Imports) > 0 {
		fields.next(b, "Imports")
		if err := encodeSyntaxImports(b, n.Imports, spanName); err != nil {
			return err
		}
	}
	if len(n.LibraryConfigs) > 0 {
		fields.next(b, "LibraryConfigs")
		if err := encodeSyntaxLibraryConfigs(b, n.LibraryConfigs, spanName); err != nil {
			return err
		}
	}
	if len(n.StateMoves) > 0 {
		fields.next(b, "StateMoves")
		if err := encodeSyntaxStateMoves(b, n.StateMoves, spanName); err != nil {
			return err
		}
	}
	if len(n.Resources) > 0 {
		fields.next(b, "Resources")
		if err := encodeSyntaxNodes(b, n.Resources, spanName); err != nil {
			return err
		}
	}
	if len(n.Data) > 0 {
		fields.next(b, "Data")
		if err := encodeSyntaxNodes(b, n.Data, spanName); err != nil {
			return err
		}
	}
	if len(n.Actions) > 0 {
		fields.next(b, "Actions")
		if err := encodeSyntaxNodes(b, n.Actions, spanName); err != nil {
			return err
		}
	}
	if len(n.Outputs) > 0 {
		fields.next(b, "Outputs")
		if err := encodeSyntaxOutputs(b, n.Outputs, spanName); err != nil {
			return err
		}
	}
	b.WriteString("}")
	return nil
}

type syntaxFieldWriter struct {
	wrote bool
}

func (w *syntaxFieldWriter) next(b *strings.Builder, name string) {
	if w.wrote {
		b.WriteString(", ")
	}
	w.wrote = true
	b.WriteString(name)
	b.WriteString(": ")
}

func writeSpanField(
	b *strings.Builder,
	fields *syntaxFieldWriter,
	span parse.Span,
	spanName SyntaxSpanNamer,
) {
	if spanName == nil {
		return
	}
	fields.next(b, "S")
	b.WriteString(spanName(span))
}

func encodeSyntaxInputs(
	b *strings.Builder,
	decls []syntax.InputDecl,
	spanName SyntaxSpanNamer,
) error {
	b.WriteString("[]syntax.InputDecl{")
	for i, decl := range decls {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("{")
		fields := syntaxFieldWriter{}
		writeSpanField(b, &fields, decl.S, spanName)
		fields.next(b, "Name")
		encodeSyntaxIdent(b, decl.Name, spanName)
		if decl.Body != nil {
			body, err := encodeNodeString(decl.Body, spanName)
			if err != nil {
				return err
			}
			fields.next(b, "Body")
			b.WriteString(body)
		}
		if decl.Type != nil {
			typ, err := encodeNodeString(decl.Type, spanName)
			if err != nil {
				return err
			}
			fields.next(b, "Type")
			b.WriteString(typ)
		}
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxLocals(
	b *strings.Builder,
	decls []syntax.LocalDecl,
	spanName SyntaxSpanNamer,
) error {
	b.WriteString("[]syntax.LocalDecl{")
	for i, decl := range decls {
		if i > 0 {
			b.WriteString(", ")
		}
		value, err := encodeNodeString(decl.Value, spanName)
		if err != nil {
			return err
		}
		b.WriteString("{")
		fields := syntaxFieldWriter{}
		writeSpanField(b, &fields, decl.S, spanName)
		fields.next(b, "Name")
		encodeSyntaxIdent(b, decl.Name, spanName)
		fields.next(b, "Value")
		b.WriteString(value)
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxConstraints(
	b *strings.Builder,
	decls []syntax.ConstraintDecl,
	spanName SyntaxSpanNamer,
) error {
	b.WriteString("[]syntax.ConstraintDecl{")
	for i, decl := range decls {
		if i > 0 {
			b.WriteString(", ")
		}
		value, err := encodeNodeString(decl.Value, spanName)
		if err != nil {
			return err
		}
		b.WriteString("{")
		fields := syntaxFieldWriter{}
		writeSpanField(b, &fields, decl.S, spanName)
		fields.next(b, "Value")
		b.WriteString(value)
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxImports(
	b *strings.Builder,
	decls []syntax.ImportDecl,
	spanName SyntaxSpanNamer,
) error {
	b.WriteString("[]syntax.ImportDecl{")
	for i, decl := range decls {
		if i > 0 {
			b.WriteString(", ")
		}
		ref, err := encodeNodeString(decl.Ref, spanName)
		if err != nil {
			return err
		}
		b.WriteString("{")
		fields := syntaxFieldWriter{}
		writeSpanField(b, &fields, decl.S, spanName)
		fields.next(b, "Alias")
		encodeSyntaxIdent(b, decl.Alias, spanName)
		fields.next(b, "Ref")
		b.WriteString(ref)
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxLibraryConfigs(
	b *strings.Builder,
	decls []syntax.LibraryConfigDecl,
	spanName SyntaxSpanNamer,
) error {
	b.WriteString("[]syntax.LibraryConfigDecl{")
	for i, decl := range decls {
		if i > 0 {
			b.WriteString(", ")
		}
		value, err := encodeNodeString(decl.Value, spanName)
		if err != nil {
			return err
		}
		b.WriteString("{")
		fields := syntaxFieldWriter{}
		writeSpanField(b, &fields, decl.S, spanName)
		fields.next(b, "Alias")
		encodeSyntaxIdent(b, decl.Alias, spanName)
		fields.next(b, "Value")
		b.WriteString(value)
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxStateMoves(
	b *strings.Builder,
	decls []syntax.StateMoveDecl,
	spanName SyntaxSpanNamer,
) error {
	b.WriteString("[]syntax.StateMoveDecl{")
	for i, decl := range decls {
		if i > 0 {
			b.WriteString(", ")
		}
		fields := syntaxFieldWriter{}
		b.WriteString("{")
		writeSpanField(b, &fields, decl.S, spanName)
		if decl.From != nil {
			fields.next(b, "From")
			encodeSyntaxStateMoveRef(b, *decl.From, spanName)
		}
		if decl.To != nil {
			fields.next(b, "To")
			encodeSyntaxStateMoveRef(b, *decl.To, spanName)
		}
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxStateMoveRef(
	b *strings.Builder,
	ref syntax.StateMoveRef,
	spanName SyntaxSpanNamer,
) {
	b.WriteString("&syntax.StateMoveRef{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, ref.S, spanName)
	fields.next(b, "Ref")
	b.WriteString("runtime.EntryRef{Address: ")
	b.WriteString(strconv.Quote(ref.Ref.Address))
	b.WriteString("}}")
}

func encodeSyntaxNodes(
	b *strings.Builder,
	decls []syntax.NodeDecl,
	spanName SyntaxSpanNamer,
) error {
	b.WriteString("[]syntax.NodeDecl{")
	for i, decl := range decls {
		if i > 0 {
			b.WriteString(", ")
		}
		body, err := encodeNodeString(decl.Body, spanName)
		if err != nil {
			return err
		}
		b.WriteString("{")
		fields := syntaxFieldWriter{}
		writeSpanField(b, &fields, decl.S, spanName)
		fields.next(b, "Kind")
		fmt.Fprintf(b, "syntax.NodeKind(%s)", strconv.Quote(string(decl.Kind)))
		fields.next(b, "Name")
		encodeSyntaxIdent(b, decl.Name, spanName)
		fields.next(b, "Selector")
		encodeSyntaxNodeSelector(b, decl.Selector, spanName)
		fields.next(b, "Body")
		b.WriteString(body)
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxOutputs(
	b *strings.Builder,
	decls []syntax.OutputDecl,
	spanName SyntaxSpanNamer,
) error {
	b.WriteString("[]syntax.OutputDecl{")
	for i, decl := range decls {
		if i > 0 {
			b.WriteString(", ")
		}
		body, err := encodeNodeString(decl.Body, spanName)
		if err != nil {
			return err
		}
		b.WriteString("{")
		fields := syntaxFieldWriter{}
		writeSpanField(b, &fields, decl.S, spanName)
		fields.next(b, "Name")
		encodeSyntaxIdent(b, decl.Name, spanName)
		fields.next(b, "Body")
		b.WriteString(body)
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxNodeSelector(
	b *strings.Builder,
	n syntax.NodeSelector,
	spanName SyntaxSpanNamer,
) {
	b.WriteString("syntax.NodeSelector{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	fields.next(b, "Alias")
	encodeSyntaxIdent(b, n.Alias, spanName)
	fields.next(b, "Export")
	encodeSyntaxIdent(b, n.Export, spanName)
	b.WriteString("}")
}

func encodeSyntaxIdent(b *strings.Builder, n syntax.Ident, spanName SyntaxSpanNamer) {
	b.WriteString("syntax.Ident{")
	fields := syntaxFieldWriter{}
	writeSpanField(b, &fields, n.S, spanName)
	fields.next(b, "Name")
	b.WriteString(strconv.Quote(n.Name))
	b.WriteString("}")
}
