package lang

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWalkAndScannerVisitSameExpressions(t *testing.T) {
	for _, name := range []string{
		"nested-expressions", "type-declarations", "conditional-branches",
		"comprehension-parts", "dot-path-index", "expr-scanner",
	} {
		t.Run(name, func(t *testing.T) {
			file := parseWalkFixture(t, name)
			if inputs := TopLevelBlock(file, "inputs"); inputs != nil {
				errs := ValidateInputDeclarations(inputs)
				require.Zero(t, errs.Len(), errs.Error())
			}
			var walked, scanned []Expr
			Walk(file.Body, func(expr Expr) { walked = append(walked, expr) })
			ScanExpr(file.Body, ScanCallbacks{Expr: func(expr Expr, _ ScanContext) ScanDecision {
				scanned = append(scanned, expr)
				return ScanContinue
			}})
			require.Equal(t, walked, scanned)
		})
	}
}

func TestWalkAndScannerTypeExpressions(t *testing.T) {
	str := &TypeAtomic{Name: "string"}
	integer := &TypeAtomic{Name: "integer"}
	boolean := &TypeAtomic{Name: "boolean"}
	list := &TypeList{Elem: str}
	optional := &TypeOptional{Elem: list}
	tuple := &TypeTuple{Elements: []TypeExpr{integer, boolean}}
	objectMap := &TypeMap{Elem: tuple}
	argument := &StringLit{Value: "default"}
	call := &Call{Callee: &Ident{Name: "pick"}, Args: []Expr{nil, argument}}
	decl := &ObjectLit{Fields: []*Field{{Value: call}}}
	expr := &TypeObject{Fields: []*TypeObjectField{
		{Name: "list", Type: optional},
		{Name: "map", Type: objectMap},
		{Name: "declaration", Decl: decl},
		{Name: "empty"},
	}}
	expected := []Expr{expr, optional, list, str, objectMap, tuple, integer, boolean,
		decl, call, argument}
	var walked, scanned []Expr
	Walk(expr, func(expr Expr) { walked = append(walked, expr) })
	require.Equal(t, expected, walked)
	ScanExpr(expr, ScanCallbacks{Expr: func(expr Expr, _ ScanContext) ScanDecision {
		scanned = append(scanned, expr)
		return ScanContinue
	}})
	require.Equal(t, expected, scanned)
}

func TestScannerRestoresNestedComprehensionBindings(t *testing.T) {
	path := func(name string) Expr { return &DotPath{Root: &Ident{Name: name}} }
	inner := &Comprehension{
		Names: []string{"inner"}, Source: path("inner-source"),
		Key: path("inner-key"), Value: path("inner-value"), Filter: path("inner-filter"),
	}
	outer := &Comprehension{
		Names: []string{"outer"}, Source: path("outer-source"),
		Key: path("outer-key"), Value: inner, Filter: path("outer-filter"),
	}
	expr := &ArrayLit{Elements: []Expr{outer, path("sibling")}}
	var visits []string
	ScanExpr(expr, ScanCallbacks{DotPath: func(path *DotPath, ctx ScanContext) ScanDecision {
		visits = append(visits, path.Root.Name+":"+strings.Join(ctx.ComprehensionBindings, ","))
		return ScanContinue
	}})
	require.Equal(t, []string{
		"outer-source:", "outer-key:outer", "inner-source:outer",
		"inner-key:outer,inner", "inner-value:outer,inner", "inner-filter:outer,inner",
		"outer-filter:outer", "sibling:",
	}, visits)
}

func TestScannerStopOverridesChildSkipping(t *testing.T) {
	for _, decisions := range [][2]ScanDecision{
		{ScanSkipChildren, ScanStop}, {ScanStop, ScanSkipChildren},
	} {
		t.Run(fmt.Sprint(decisions), func(t *testing.T) {
			argument := &StringLit{Value: "argument"}
			call := &Call{Args: []Expr{argument}}
			sibling := &StringLit{Value: "sibling"}
			expr := &ArrayLit{Elements: []Expr{call, sibling}}
			var visited []Expr
			var calls []*Call
			ScanExpr(expr, ScanCallbacks{
				Expr: func(expr Expr, _ ScanContext) ScanDecision {
					visited = append(visited, expr)
					if expr == call {
						return decisions[0]
					}
					return ScanContinue
				},
				Call: func(call *Call, _ ScanContext) ScanDecision {
					calls = append(calls, call)
					return decisions[1]
				},
			})
			require.Equal(t, []Expr{expr, call}, visited)
			require.Equal(t, []*Call{call}, calls)
		})
	}
}
