package check

import (
	"fmt"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func BenchmarkReferencesManyCompositeScopes(b *testing.B) {
	checker := NewSyntax(benchmarkRootBody(400), benchmarkLibraries())
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		errs := checker.References(nil)
		if errs.Len() != 0 {
			b.Fatalf("References returned diagnostics: %v", errs.Messages())
		}
	}
}

func benchmarkLibraries() map[string]*runtime.Library {
	body := benchmarkCompositeBody()
	return map[string]*runtime.Library{
		"outer": {
			ResourceComposites: map[string]*runtime.CompositeType{
				"greeting": {
					Name:       "greeting",
					SyntaxBody: &body,
					Libraries: map[string]*runtime.Library{
						"local": {},
					},
				},
			},
		},
	}
}

func benchmarkRootBody(count int) syntax.FactoryBody {
	body := syntax.FactoryBody{
		Resources: make([]syntax.NodeDecl, 0, count),
	}
	for i := range count {
		body.Resources = append(body.Resources, syntax.NodeDecl{
			Kind: syntax.NodeResource,
			Name: syntax.Ident{Name: fmt.Sprintf("app-%03d", i)},
			Selector: syntax.NodeSelector{
				Alias:  syntax.Ident{Name: "outer"},
				Export: syntax.Ident{Name: "greeting"},
			},
			Body: benchmarkObject(
				benchmarkField("path", &lang.StringLit{Value: fmt.Sprintf("/tmp/app-%03d", i)}),
			),
		})
	}
	return body
}

func benchmarkCompositeBody() syntax.FactoryBody {
	return syntax.FactoryBody{
		Inputs: []syntax.InputDecl{{
			Name: syntax.Ident{Name: "path"},
			Body: benchmarkObject(benchmarkField("type", &lang.TypeAtomic{Name: "string"})),
			Type: &lang.TypeAtomic{Name: "string"},
		}},
		Locals: []syntax.LocalDecl{{
			Name:  syntax.Ident{Name: "target"},
			Value: benchmarkPath("input", "path"),
		}},
		Constraints: []syntax.ConstraintDecl{{
			Value: benchmarkObject(
				benchmarkField("when", &lang.BoolLit{Value: true}),
				benchmarkField("require", &lang.BoolLit{Value: true}),
			),
		}},
		Resources: []syntax.NodeDecl{{
			Kind: syntax.NodeResource,
			Name: syntax.Ident{Name: "file"},
			Selector: syntax.NodeSelector{
				Alias:  syntax.Ident{Name: "local"},
				Export: syntax.Ident{Name: "fs-file"},
			},
			Body: benchmarkObject(
				benchmarkField("path", benchmarkPath("local", "target")),
			),
		}},
		Outputs: []syntax.OutputDecl{{
			Name: syntax.Ident{Name: "path"},
			Body: benchmarkObject(
				benchmarkField("value", benchmarkPath("resource", "file", "path")),
			),
		}},
	}
}

func benchmarkObject(fields ...*lang.Field) *lang.ObjectLit {
	return &lang.ObjectLit{Fields: fields}
}

func benchmarkField(name string, value lang.Expr) *lang.Field {
	return &lang.Field{
		Key:   lang.FieldKey{Kind: lang.FieldIdent, Name: name},
		Value: value,
	}
}

func benchmarkPath(root string, names ...string) *lang.DotPath {
	segments := make([]lang.DotSegment, 0, len(names))
	for _, name := range names {
		segments = append(segments, lang.DotSegment{Name: name})
	}
	return &lang.DotPath{
		Root:     &lang.Ident{Name: root},
		Segments: segments,
	}
}
