package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

// EncodeSyntaxFactoryBody renders a typed factory or composite body as a Go
// expression. Source positions are omitted; runtime graph extraction only needs
// declaration names, selectors, and expression bodies.
func EncodeSyntaxFactoryBody(n syntax.FactoryBody) (string, error) {
	var b strings.Builder
	if err := encodeSyntaxFactoryBody(&b, n); err != nil {
		return "", err
	}
	return b.String(), nil
}

func encodeSyntaxFactoryBody(b *strings.Builder, n syntax.FactoryBody) error {
	b.WriteString("syntax.FactoryBody{")
	fields := syntaxFieldWriter{}
	if n.Description != nil {
		fields.next(b, "Description")
		s, err := EncodeNode(n.Description)
		if err != nil {
			return err
		}
		b.WriteString(s)
	}
	if len(n.Inputs) > 0 {
		fields.next(b, "Inputs")
		if err := encodeSyntaxInputs(b, n.Inputs); err != nil {
			return err
		}
	}
	if len(n.Locals) > 0 {
		fields.next(b, "Locals")
		if err := encodeSyntaxLocals(b, n.Locals); err != nil {
			return err
		}
	}
	if len(n.Constraints) > 0 {
		fields.next(b, "Constraints")
		if err := encodeSyntaxConstraints(b, n.Constraints); err != nil {
			return err
		}
	}
	if len(n.Imports) > 0 {
		fields.next(b, "Imports")
		if err := encodeSyntaxImports(b, n.Imports); err != nil {
			return err
		}
	}
	if len(n.Configurations) > 0 {
		fields.next(b, "Configurations")
		if err := encodeSyntaxConfigurations(b, n.Configurations); err != nil {
			return err
		}
	}
	if len(n.LibraryConfigs) > 0 {
		fields.next(b, "LibraryConfigs")
		if err := encodeSyntaxLibraryConfigs(b, n.LibraryConfigs); err != nil {
			return err
		}
	}
	if len(n.Resources) > 0 {
		fields.next(b, "Resources")
		if err := encodeSyntaxNodes(b, n.Resources); err != nil {
			return err
		}
	}
	if len(n.Data) > 0 {
		fields.next(b, "Data")
		if err := encodeSyntaxNodes(b, n.Data); err != nil {
			return err
		}
	}
	if len(n.Actions) > 0 {
		fields.next(b, "Actions")
		if err := encodeSyntaxNodes(b, n.Actions); err != nil {
			return err
		}
	}
	if len(n.Outputs) > 0 {
		fields.next(b, "Outputs")
		if err := encodeSyntaxOutputs(b, n.Outputs); err != nil {
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

func encodeSyntaxInputs(b *strings.Builder, decls []syntax.InputDecl) error {
	b.WriteString("[]syntax.InputDecl{")
	for i, decl := range decls {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("{Name: ")
		encodeSyntaxIdent(b, decl.Name)
		if decl.Body != nil {
			body, err := EncodeNode(decl.Body)
			if err != nil {
				return err
			}
			b.WriteString(", Body: ")
			b.WriteString(body)
		}
		if decl.Type != nil {
			typ, err := EncodeNode(decl.Type)
			if err != nil {
				return err
			}
			b.WriteString(", Type: ")
			b.WriteString(typ)
		}
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxLocals(b *strings.Builder, decls []syntax.LocalDecl) error {
	b.WriteString("[]syntax.LocalDecl{")
	for i, decl := range decls {
		if i > 0 {
			b.WriteString(", ")
		}
		value, err := EncodeNode(decl.Value)
		if err != nil {
			return err
		}
		b.WriteString("{Name: ")
		encodeSyntaxIdent(b, decl.Name)
		b.WriteString(", Value: ")
		b.WriteString(value)
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxConstraints(b *strings.Builder, decls []syntax.ConstraintDecl) error {
	b.WriteString("[]syntax.ConstraintDecl{")
	for i, decl := range decls {
		if i > 0 {
			b.WriteString(", ")
		}
		value, err := EncodeNode(decl.Value)
		if err != nil {
			return err
		}
		b.WriteString("{Value: ")
		b.WriteString(value)
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxImports(b *strings.Builder, decls []syntax.ImportDecl) error {
	b.WriteString("[]syntax.ImportDecl{")
	for i, decl := range decls {
		if i > 0 {
			b.WriteString(", ")
		}
		ref, err := EncodeNode(decl.Ref)
		if err != nil {
			return err
		}
		b.WriteString("{Alias: ")
		encodeSyntaxIdent(b, decl.Alias)
		b.WriteString(", Ref: ")
		b.WriteString(ref)
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxConfigurations(b *strings.Builder, decls []syntax.ConfigurationDecl) error {
	b.WriteString("[]syntax.ConfigurationDecl{")
	for i, decl := range decls {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("{")
		fields := syntaxFieldWriter{}
		if decl.Name != nil {
			fields.next(b, "Name")
			b.WriteString("&")
			encodeSyntaxIdent(b, *decl.Name)
		}
		fields.next(b, "Selector")
		encodeSyntaxIdent(b, decl.Selector)
		if decl.Body != nil {
			body, err := EncodeNode(decl.Body)
			if err != nil {
				return err
			}
			fields.next(b, "Body")
			b.WriteString(body)
		}
		if decl.Value != nil {
			value, err := EncodeNode(decl.Value)
			if err != nil {
				return err
			}
			fields.next(b, "Value")
			b.WriteString(value)
		}
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxLibraryConfigs(b *strings.Builder, decls []syntax.LibraryConfigDecl) error {
	b.WriteString("[]syntax.LibraryConfigDecl{")
	for i, decl := range decls {
		if i > 0 {
			b.WriteString(", ")
		}
		value, err := EncodeNode(decl.Value)
		if err != nil {
			return err
		}
		b.WriteString("{Alias: ")
		encodeSyntaxIdent(b, decl.Alias)
		b.WriteString(", Value: ")
		b.WriteString(value)
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxNodes(b *strings.Builder, decls []syntax.NodeDecl) error {
	b.WriteString("[]syntax.NodeDecl{")
	for i, decl := range decls {
		if i > 0 {
			b.WriteString(", ")
		}
		body, err := EncodeNode(decl.Body)
		if err != nil {
			return err
		}
		fmt.Fprintf(b, "{Kind: syntax.NodeKind(%s), Name: ", strconv.Quote(string(decl.Kind)))
		encodeSyntaxIdent(b, decl.Name)
		b.WriteString(", Selector: syntax.NodeSelector{Alias: ")
		encodeSyntaxIdent(b, decl.Selector.Alias)
		b.WriteString(", Export: ")
		encodeSyntaxIdent(b, decl.Selector.Export)
		b.WriteString("}, Body: ")
		b.WriteString(body)
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxOutputs(b *strings.Builder, decls []syntax.OutputDecl) error {
	b.WriteString("[]syntax.OutputDecl{")
	for i, decl := range decls {
		if i > 0 {
			b.WriteString(", ")
		}
		body, err := EncodeNode(decl.Body)
		if err != nil {
			return err
		}
		b.WriteString("{Name: ")
		encodeSyntaxIdent(b, decl.Name)
		b.WriteString(", Body: ")
		b.WriteString(body)
		b.WriteString("}")
	}
	b.WriteString("}")
	return nil
}

func encodeSyntaxIdent(b *strings.Builder, n syntax.Ident) {
	b.WriteString("syntax.Ident{Name: ")
	b.WriteString(strconv.Quote(n.Name))
	b.WriteString("}")
}
