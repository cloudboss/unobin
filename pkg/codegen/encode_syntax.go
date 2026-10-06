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
	emitted, err := emitSyntaxFactoryBody(n, nil)
	return emitted.Literal, err
}

// EncodeSyntaxFactoryBodyWithSpans renders a body while preserving source spans.
func EncodeSyntaxFactoryBodyWithSpans(
	n syntax.FactoryBody,
	spanName SyntaxSpanNamer,
) (string, error) {
	emitted, err := emitSyntaxFactoryBody(n, spanName)
	return emitted.Literal, err
}

func encodeSyntaxFactoryBody(
	b *syntaxEmitter,
	n syntax.FactoryBody,
	spanName SyntaxSpanNamer,
) error {
	b.WriteString("syntax.FactoryBody{")
	fields := syntaxFieldWriter{}
	writeSpanField(&b.Builder, &fields, n.S, spanName)
	if n.Description != nil {
		fields.next(&b.Builder, "Description")
		s, err := b.node(n.Description, spanName)
		if err != nil {
			return err
		}
		b.WriteString(s)
	}
	if len(n.Assets) > 0 {
		fields.next(&b.Builder, "Assets")
		if err := encodeSyntaxAssets(b, n.Assets, spanName); err != nil {
			return err
		}
	}
	if len(n.Inputs) > 0 {
		fields.next(&b.Builder, "Inputs")
		if err := encodeSyntaxInputs(b, n.Inputs, spanName); err != nil {
			return err
		}
	}
	if len(n.Locals) > 0 {
		fields.next(&b.Builder, "Locals")
		if err := encodeSyntaxLocals(b, n.Locals, spanName); err != nil {
			return err
		}
	}
	if len(n.Constraints) > 0 {
		fields.next(&b.Builder, "Constraints")
		if err := encodeSyntaxConstraints(b, n.Constraints, spanName); err != nil {
			return err
		}
	}
	if len(n.Imports) > 0 {
		fields.next(&b.Builder, "Imports")
		if err := encodeSyntaxImports(b, n.Imports, spanName); err != nil {
			return err
		}
	}
	if len(n.LibraryConfigs) > 0 {
		fields.next(&b.Builder, "LibraryConfigs")
		if err := encodeSyntaxLibraryConfigs(b, n.LibraryConfigs, spanName); err != nil {
			return err
		}
	}
	if len(n.StateMoves) > 0 {
		fields.next(&b.Builder, "StateMoves")
		if err := encodeSyntaxStateMoves(b, n.StateMoves, spanName); err != nil {
			return err
		}
	}
	if len(n.Resources) > 0 {
		fields.next(&b.Builder, "Resources")
		if err := encodeSyntaxNodes(b, n.Resources, spanName); err != nil {
			return err
		}
	}
	if len(n.Data) > 0 {
		fields.next(&b.Builder, "Data")
		if err := encodeSyntaxNodes(b, n.Data, spanName); err != nil {
			return err
		}
	}
	if len(n.Actions) > 0 {
		fields.next(&b.Builder, "Actions")
		if err := encodeSyntaxNodes(b, n.Actions, spanName); err != nil {
			return err
		}
	}
	if len(n.Outputs) > 0 {
		fields.next(&b.Builder, "Outputs")
		if err := encodeSyntaxOutputs(b, n.Outputs, spanName); err != nil {
			return err
		}
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxAssets(
	b *syntaxEmitter,
	decls []syntax.AssetDecl,
	spanName SyntaxSpanNamer,
) error {
	b.WriteString("[]syntax.AssetDecl{")
	for i, decl := range decls {
		if i > 0 {
			b.WriteString(", ")
		}
		source, err := b.node(decl.Source, spanName)
		if err != nil {
			return err
		}
		b.WriteString("{")
		fields := syntaxFieldWriter{}
		writeSpanField(&b.Builder, &fields, decl.S, spanName)
		fields.next(&b.Builder, "Name")
		encodeSyntaxIdent(b, decl.Name, spanName)
		fields.next(&b.Builder, "Source")
		b.WriteString(source)
		b.WriteString("}")
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
	b *syntaxEmitter,
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
		writeSpanField(&b.Builder, &fields, decl.S, spanName)
		fields.next(&b.Builder, "Name")
		encodeSyntaxIdent(b, decl.Name, spanName)
		if decl.Body != nil {
			body, err := b.node(decl.Body, spanName)
			if err != nil {
				return err
			}
			fields.next(&b.Builder, "Body")
			b.WriteString(body)
		}
		if decl.Type != nil {
			typ, err := b.node(decl.Type, spanName)
			if err != nil {
				return err
			}
			fields.next(&b.Builder, "Type")
			b.WriteString(typ)
		}
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxLocals(
	b *syntaxEmitter,
	decls []syntax.LocalDecl,
	spanName SyntaxSpanNamer,
) error {
	b.WriteString("[]syntax.LocalDecl{")
	for i, decl := range decls {
		if i > 0 {
			b.WriteString(", ")
		}
		value, err := b.node(decl.Value, spanName)
		if err != nil {
			return err
		}
		b.WriteString("{")
		fields := syntaxFieldWriter{}
		writeSpanField(&b.Builder, &fields, decl.S, spanName)
		fields.next(&b.Builder, "Name")
		encodeSyntaxIdent(b, decl.Name, spanName)
		fields.next(&b.Builder, "Value")
		b.WriteString(value)
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxConstraints(
	b *syntaxEmitter,
	decls []syntax.ConstraintDecl,
	spanName SyntaxSpanNamer,
) error {
	b.WriteString("[]syntax.ConstraintDecl{")
	for i, decl := range decls {
		if i > 0 {
			b.WriteString(", ")
		}
		value, err := b.node(decl.Value, spanName)
		if err != nil {
			return err
		}
		b.WriteString("{")
		fields := syntaxFieldWriter{}
		writeSpanField(&b.Builder, &fields, decl.S, spanName)
		fields.next(&b.Builder, "Value")
		b.WriteString(value)
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxImports(
	b *syntaxEmitter,
	decls []syntax.ImportDecl,
	spanName SyntaxSpanNamer,
) error {
	b.WriteString("[]syntax.ImportDecl{")
	for i, decl := range decls {
		if i > 0 {
			b.WriteString(", ")
		}
		ref, err := b.node(decl.Ref, spanName)
		if err != nil {
			return err
		}
		b.WriteString("{")
		fields := syntaxFieldWriter{}
		writeSpanField(&b.Builder, &fields, decl.S, spanName)
		fields.next(&b.Builder, "Alias")
		encodeSyntaxIdent(b, decl.Alias, spanName)
		fields.next(&b.Builder, "Ref")
		b.WriteString(ref)
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxLibraryConfigs(
	b *syntaxEmitter,
	decls []syntax.LibraryConfigDecl,
	spanName SyntaxSpanNamer,
) error {
	b.WriteString("[]syntax.LibraryConfigDecl{")
	for i, decl := range decls {
		if i > 0 {
			b.WriteString(", ")
		}
		value, err := b.node(decl.Value, spanName)
		if err != nil {
			return err
		}
		b.WriteString("{")
		fields := syntaxFieldWriter{}
		writeSpanField(&b.Builder, &fields, decl.S, spanName)
		fields.next(&b.Builder, "Alias")
		encodeSyntaxIdent(b, decl.Alias, spanName)
		fields.next(&b.Builder, "Value")
		b.WriteString(value)
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxStateMoves(
	b *syntaxEmitter,
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
		writeSpanField(&b.Builder, &fields, decl.S, spanName)
		if decl.From != nil {
			fields.next(&b.Builder, "From")
			encodeSyntaxStateMoveRef(b, *decl.From, spanName)
		}
		if decl.To != nil {
			fields.next(&b.Builder, "To")
			encodeSyntaxStateMoveRef(b, *decl.To, spanName)
		}
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxStateMoveRef(
	b *syntaxEmitter,
	ref syntax.StateMoveRef,
	spanName SyntaxSpanNamer,
) {
	b.WriteString("&syntax.StateMoveRef{")
	fields := syntaxFieldWriter{}
	writeSpanField(&b.Builder, &fields, ref.S, spanName)
	fields.next(&b.Builder, "Ref")
	b.WriteString("runtime.EntryRef{Address: ")
	b.WriteString(strconv.Quote(ref.Ref.Address))
	b.WriteString("}}")
}

func encodeSyntaxNodes(
	b *syntaxEmitter,
	decls []syntax.NodeDecl,
	spanName SyntaxSpanNamer,
) error {
	b.WriteString("[]syntax.NodeDecl{")
	for i, decl := range decls {
		if i > 0 {
			b.WriteString(", ")
		}
		body, err := b.node(decl.Body, spanName)
		if err != nil {
			return err
		}
		b.WriteString("{")
		fields := syntaxFieldWriter{}
		writeSpanField(&b.Builder, &fields, decl.S, spanName)
		fields.next(&b.Builder, "Kind")
		fmt.Fprintf(b, "syntax.NodeKind(%s)", strconv.Quote(string(decl.Kind)))
		fields.next(&b.Builder, "Name")
		encodeSyntaxIdent(b, decl.Name, spanName)
		fields.next(&b.Builder, "Selector")
		encodeSyntaxNodeSelector(b, decl.Selector, spanName)
		fields.next(&b.Builder, "Body")
		b.WriteString(body)
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxOutputs(
	b *syntaxEmitter,
	decls []syntax.OutputDecl,
	spanName SyntaxSpanNamer,
) error {
	b.WriteString("[]syntax.OutputDecl{")
	for i, decl := range decls {
		if i > 0 {
			b.WriteString(", ")
		}
		body, err := b.node(decl.Body, spanName)
		if err != nil {
			return err
		}
		b.WriteString("{")
		fields := syntaxFieldWriter{}
		writeSpanField(&b.Builder, &fields, decl.S, spanName)
		fields.next(&b.Builder, "Name")
		encodeSyntaxIdent(b, decl.Name, spanName)
		fields.next(&b.Builder, "Body")
		b.WriteString(body)
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxNodeSelector(
	b *syntaxEmitter,
	n syntax.NodeSelector,
	spanName SyntaxSpanNamer,
) {
	b.WriteString("syntax.NodeSelector{")
	fields := syntaxFieldWriter{}
	writeSpanField(&b.Builder, &fields, n.S, spanName)
	fields.next(&b.Builder, "Alias")
	encodeSyntaxIdent(b, n.Alias, spanName)
	fields.next(&b.Builder, "Export")
	encodeSyntaxIdent(b, n.Export, spanName)
	b.WriteString("}")
}

func encodeSyntaxIdent(b *syntaxEmitter, n syntax.Ident, spanName SyntaxSpanNamer) {
	b.WriteString("syntax.Ident{")
	fields := syntaxFieldWriter{}
	writeSpanField(&b.Builder, &fields, n.S, spanName)
	fields.next(&b.Builder, "Name")
	b.WriteString(strconv.Quote(n.Name))
	b.WriteString("}")
}
