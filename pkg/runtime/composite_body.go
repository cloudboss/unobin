package runtime

import (
	"errors"
	"fmt"

	"github.com/cloudboss/unobin/pkg/lang"
)

type compositeOutputDecl struct {
	name string
	body lang.Expr
}

func compositeLocalScope(n *Node) *localScope {
	return newLocalScopeFromMap(compositeLocalExprs(n))
}

func compositeLocalExprs(n *Node) map[string]lang.Expr {
	if n != nil && n.CompositeSyntaxBody != nil {
		return syntaxLocalMap(n.CompositeSyntaxBody.Locals)
	}
	if n == nil {
		return nil
	}
	return lang.FieldMap(localsBlock(n.CompositeBody))
}

func compositeInputNames(n *Node) map[string]bool {
	out := map[string]bool{}
	if n == nil {
		return out
	}
	if n.CompositeSyntaxBody != nil {
		for _, decl := range n.CompositeSyntaxBody.Inputs {
			out[decl.Name.Name] = true
		}
		return out
	}
	return InputNames(n.CompositeBody)
}

func compositeConstraints(n *Node) *lang.ArrayLit {
	if n == nil {
		return nil
	}
	if n.CompositeSyntaxBody != nil {
		values := make([]lang.Expr, 0, len(n.CompositeSyntaxBody.Constraints))
		for _, decl := range n.CompositeSyntaxBody.Constraints {
			values = append(values, decl.Value)
		}
		return &lang.ArrayLit{Elements: values}
	}
	if n.CompositeBody == nil || n.CompositeBody.Body == nil {
		return nil
	}
	arr, _ := lang.FieldMap(n.CompositeBody.Body)["constraints"].(*lang.ArrayLit)
	return arr
}

func compositeOutputs(n *Node) []compositeOutputDecl {
	if n == nil {
		return nil
	}
	if n.CompositeSyntaxBody != nil {
		out := make([]compositeOutputDecl, 0, len(n.CompositeSyntaxBody.Outputs))
		for _, decl := range n.CompositeSyntaxBody.Outputs {
			out = append(out, compositeOutputDecl{name: decl.Name.Name, body: decl.Body})
		}
		return out
	}
	if n.CompositeBody == nil || n.CompositeBody.Body == nil {
		return nil
	}
	outBlock := lang.TopLevelBlock(n.CompositeBody, "outputs")
	if outBlock == nil {
		return nil
	}
	out := make([]compositeOutputDecl, 0, len(outBlock.Fields))
	for _, fld := range outBlock.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		out = append(out, compositeOutputDecl{name: fld.Key.Name, body: fld.Value})
	}
	return out
}

func planCompositeOutputs(n *Node, scope *EvalContext) (map[string]any, error) {
	return evalCompositeOutputDecls(compositeOutputs(n), scope, true)
}

func evalCompositeOutputs(n *Node, scope *EvalContext) (map[string]any, error) {
	return evalCompositeOutputDecls(compositeOutputs(n), scope, false)
}

func evalCompositeOutputDecls(
	outputs []compositeOutputDecl,
	scope *EvalContext,
	deferMissing bool,
) (map[string]any, error) {
	if len(outputs) == 0 {
		return nil, nil
	}
	out := make(map[string]any, len(outputs))
	for _, output := range outputs {
		inner := lang.OutputValueExpr(output.body)
		if inner == nil {
			return nil, fmt.Errorf("composite output %q: missing wrapper", output.name)
		}
		val, err := Eval(inner, scope)
		if deferMissing && errors.Is(err, ErrEvalNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("composite output %q: %w", output.name, err)
		}
		out[output.name] = val
	}
	return out, nil
}
