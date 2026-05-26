package docgen

import (
	"fmt"
	"go/ast"
	"go/doc"
	"go/doc/comment"
	"go/format"
	"go/token"
	"strings"
)

// APIReference renders a parsed package's exported API as a Markdown page:
// the package documentation, then its constants, variables, functions, and
// types, with each type followed by its own constructors and methods.
// go/doc sorts every group by name, so the output is deterministic.
func APIReference(pkg *doc.Package, fset *token.FileSet) string {
	r := apiRenderer{pkg: pkg, fset: fset, printer: pkg.Printer()}
	var b strings.Builder
	fmt.Fprintf(&b, "# package %s\n", pkg.Name)
	fmt.Fprintf(&b, "\n```\nimport \"%s\"\n```\n", pkg.ImportPath)
	if d := r.comment(pkg.Doc); d != "" {
		fmt.Fprintf(&b, "\n%s", d)
	}
	r.values(&b, "Constants", pkg.Consts)
	r.values(&b, "Variables", pkg.Vars)
	if len(pkg.Funcs) > 0 {
		b.WriteString("\n## Functions\n")
		for _, fn := range pkg.Funcs {
			r.function(&b, fn, 3)
		}
	}
	if len(pkg.Types) > 0 {
		b.WriteString("\n## Types\n")
		for _, t := range pkg.Types {
			r.typ(&b, t)
		}
	}
	return b.String()
}

type apiRenderer struct {
	pkg     *doc.Package
	fset    *token.FileSet
	printer *comment.Printer
}

// comment renders a Go doc comment to Markdown, or "" when it is empty.
func (r apiRenderer) comment(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return string(r.printer.Markdown(r.pkg.Parser().Parse(text)))
}

// code renders an AST node as a fenced Go code block, dropping any doc
// comment attached to the node so it is not repeated below the signature.
func (r apiRenderer) code(node ast.Node) string {
	var b strings.Builder
	_ = format.Node(&b, r.fset, node)
	return "```go\n" + b.String() + "\n```\n"
}

func (r apiRenderer) values(b *strings.Builder, heading string, vals []*doc.Value) {
	if len(vals) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n", heading)
	for _, v := range vals {
		decl := *v.Decl
		decl.Doc = nil
		fmt.Fprintf(b, "\n%s", r.code(&decl))
		if d := r.comment(v.Doc); d != "" {
			fmt.Fprintf(b, "\n%s", d)
		}
	}
}

func (r apiRenderer) function(b *strings.Builder, fn *doc.Func, level int) {
	title := "func " + fn.Name
	if fn.Recv != "" {
		title = fmt.Sprintf("func (%s) %s", fn.Recv, fn.Name)
	}
	fmt.Fprintf(b, "\n%s %s\n", strings.Repeat("#", level), title)
	sig := *fn.Decl
	sig.Doc = nil
	sig.Body = nil
	fmt.Fprintf(b, "\n%s", r.code(&sig))
	if d := r.comment(fn.Doc); d != "" {
		fmt.Fprintf(b, "\n%s", d)
	}
}

func (r apiRenderer) typ(b *strings.Builder, t *doc.Type) {
	fmt.Fprintf(b, "\n### type %s\n", t.Name)
	decl := *t.Decl
	decl.Doc = nil
	fmt.Fprintf(b, "\n%s", r.code(&decl))
	if d := r.comment(t.Doc); d != "" {
		fmt.Fprintf(b, "\n%s", d)
	}
	for _, fn := range t.Funcs {
		r.function(b, fn, 4)
	}
	for _, m := range t.Methods {
		r.function(b, m, 4)
	}
}
