package lang

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExprScannerVisitsDotPathsAndCallsInSourceOrder(t *testing.T) {
	f := parseWalkFixture(t, "expr-scanner")

	var visits []string
	ScanExpr(f.Body, ScanCallbacks{
		DotPath: func(path *DotPath, _ ScanContext) ScanDecision {
			parts := []string{path.Root.Name}
			for _, seg := range path.Segments {
				if seg.Name != "" {
					parts = append(parts, seg.Name)
				}
			}
			visits = append(visits, "path:"+strings.Join(parts, "."))
			return ScanContinue
		},
		Call: func(call *Call, _ ScanContext) ScanDecision {
			name := ""
			if call.Callee != nil {
				name = call.Callee.Name
			} else if call.Library != nil && call.Func != nil {
				name = call.Library.Name + "." + call.Func.Name
			}
			visits = append(visits, "call:"+name)
			return ScanContinue
		},
	})

	require.Equal(t, []string{
		"call:format",
		"path:input.prefix",
		"path:input.items.id",
		"path:input.enabled",
		"path:input.label",
		"path:input.fallback",
		"path:input.items",
		"call:format",
		"path:item.id",
		"path:input.enabled",
		"path:input.prefix",
		"call:format",
		"path:input.label",
	}, visits)
}

func TestExprScannerCanSkipNestedExpression(t *testing.T) {
	expr := &ObjectLit{Fields: []*Field{
		{Value: &DotPath{
			Root: &Ident{Name: "local"},
			Segments: []DotSegment{
				{Name: "lookup"},
				{Index: &DotPath{Root: &Ident{Name: "input"}, Segments: []DotSegment{{Name: "key"}}}},
			},
		}},
		{Value: &DotPath{Root: &Ident{Name: "input"}, Segments: []DotSegment{{Name: "next"}}}},
	}}

	var roots []string
	ScanExpr(expr, ScanCallbacks{
		DotPath: func(path *DotPath, _ ScanContext) ScanDecision {
			roots = append(roots, path.Root.Name)
			if path.Root.Name == "local" {
				return ScanSkipChildren
			}
			return ScanContinue
		},
	})

	require.Equal(t, []string{"local", "input"}, roots)
}

func TestExprScannerReportsComprehensionBindings(t *testing.T) {
	f := parseWalkFixture(t, "expr-scanner")

	var visits []string
	ScanExpr(f.Body, ScanCallbacks{
		DotPath: func(path *DotPath, ctx ScanContext) ScanDecision {
			if path.Root.Name != "input" && path.Root.Name != "item" {
				return ScanContinue
			}
			parts := []string{path.Root.Name}
			for _, seg := range path.Segments {
				if seg.Name != "" {
					parts = append(parts, seg.Name)
				}
			}
			visits = append(visits,
				fmt.Sprintf("%s:%s", strings.Join(parts, "."), strings.Join(ctx.ComprehensionBindings, ",")))
			return ScanContinue
		},
	})

	require.Contains(t, visits, "input.items:")
	require.Contains(t, visits, "item.id:item")
	require.Contains(t, visits, "input.enabled:item")
}

func TestExprScannerNilIsNoOp(t *testing.T) {
	called := false
	ScanExpr(nil, ScanCallbacks{Expr: func(Expr, ScanContext) ScanDecision {
		called = true
		return ScanContinue
	}})
	require.False(t, called)
}

func BenchmarkWalkLargeExpression(b *testing.B) {
	expr := largeExprScannerExpression(1000)
	b.ReportAllocs()
	for b.Loop() {
		count := 0
		Walk(expr, func(e Expr) {
			switch e.(type) {
			case *DotPath, *Call:
				count++
			}
		})
		if count == 0 {
			b.Fatal("scanner fixture produced no visits")
		}
	}
}

func BenchmarkExprScannerLargeExpression(b *testing.B) {
	expr := largeExprScannerExpression(1000)
	b.ReportAllocs()
	for b.Loop() {
		count := 0
		ScanExpr(expr, ScanCallbacks{
			DotPath: func(*DotPath, ScanContext) ScanDecision {
				count++
				return ScanContinue
			},
			Call: func(*Call, ScanContext) ScanDecision {
				count++
				return ScanContinue
			},
		})
		if count == 0 {
			b.Fatal("scanner fixture produced no visits")
		}
	}
}

func largeExprScannerExpression(fields int) Expr {
	out := &ObjectLit{Fields: make([]*Field, 0, fields)}
	for i := range fields {
		name := fmt.Sprintf("field-%d", i)
		out.Fields = append(out.Fields, &Field{
			Key: FieldKey{Kind: FieldIdent, Name: name},
			Value: &ArrayLit{Elements: []Expr{
				&Call{
					Callee: &Ident{Name: "format"},
					Args: []Expr{
						&StringLit{Value: "%s"},
						&DotPath{Root: &Ident{Name: "input"}, Segments: []DotSegment{{Name: "name"}}},
					},
				},
				&Conditional{
					Cond: &DotPath{Root: &Ident{Name: "input"}, Segments: []DotSegment{{Name: "enabled"}}},
					Then: &DotPath{Root: &Ident{Name: "resource"}, Segments: []DotSegment{
						{Name: "app"}, {Name: "id"},
					}},
					Else: &DotPath{Root: &Ident{Name: "data-source"}, Segments: []DotSegment{
						{Name: "ami"}, {Name: "id"},
					}},
				},
				&Comprehension{
					Kind:   CompList,
					Names:  []string{"item"},
					Source: &DotPath{Root: &Ident{Name: "input"}, Segments: []DotSegment{{Name: "items"}}},
					Value: &Call{
						Callee: &Ident{Name: "format"},
						Args: []Expr{
							&StringLit{Value: "%s"},
							&DotPath{Root: &Ident{Name: "item"}, Segments: []DotSegment{{Name: "id"}}},
						},
					},
					Filter: &DotPath{Root: &Ident{Name: "input"}, Segments: []DotSegment{{Name: "enabled"}}},
				},
				&InterpolatedString{Parts: []InterpolatedPart{
					{Lit: "prefix "},
					{Expr: &DotPath{Root: &Ident{Name: "input"}, Segments: []DotSegment{{Name: "prefix"}}}},
				}},
			}},
		})
	}
	return out
}
