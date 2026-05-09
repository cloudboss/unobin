package runtime

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/lang"
)

// EvalContext supplies the values that addresses resolve against. Vars
// is the validated `inputs:` map after `config.ub` and `UB_VAR_*` env
// overrides. Resources, Data, and Actions hold the outputs of nodes
// that have already executed, indexed by their source address path.
type EvalContext struct {
	Vars      map[string]any
	Resources map[string]any
	Data      map[string]any
	Actions   map[string]any
}

// Eval reduces a parsed expression to a Go value. Supported are
// literals, bare identifiers (as their name string); array and object
// literals (recursive); and the `var.X[.Y...]` address form.
func Eval(e lang.Expr, ctx *EvalContext) (any, error) {
	switch v := e.(type) {
	case *lang.StringLit:
		return v.Value, nil
	case *lang.NumberLit:
		if v.IsFloat {
			return v.ParsedFloat, nil
		}
		return v.ParsedInt, nil
	case *lang.BoolLit:
		return v.Value, nil
	case *lang.NullLit:
		return nil, nil
	case *lang.Ident:
		return v.Name, nil
	case *lang.ArrayLit:
		return evalArray(v, ctx)
	case *lang.ObjectLit:
		return evalObject(v, ctx)
	case *lang.DotPath:
		return evalDotPath(v, ctx)
	default:
		return nil, fmt.Errorf("eval: unsupported expression %T", e)
	}
}

func evalArray(a *lang.ArrayLit, ctx *EvalContext) ([]any, error) {
	out := make([]any, 0, len(a.Elements))
	for i, el := range a.Elements {
		val, err := Eval(el, ctx)
		if err != nil {
			return nil, fmt.Errorf("eval: array[%d]: %w", i, err)
		}
		out = append(out, val)
	}
	return out, nil
}

func evalObject(o *lang.ObjectLit, ctx *EvalContext) (map[string]any, error) {
	out := make(map[string]any, len(o.Fields))
	for _, fld := range o.Fields {
		var key string
		switch fld.Key.Kind {
		case lang.FieldIdent:
			key = fld.Key.Name
		case lang.FieldString:
			key = fld.Key.String
		}
		val, err := Eval(fld.Value, ctx)
		if err != nil {
			return nil, fmt.Errorf("eval: field %q: %w", key, err)
		}
		out[key] = val
	}
	return out, nil
}

func evalDotPath(p *lang.DotPath, ctx *EvalContext) (any, error) {
	var root any
	switch p.Root.Name {
	case "var":
		root = ctx.Vars
	case "resource":
		root = ctx.Resources
	case "data":
		root = ctx.Data
	case "action":
		root = ctx.Actions
	default:
		return nil, fmt.Errorf("eval: unknown address root %q", p.Root.Name)
	}
	cur := root
	path := p.Root.Name
	for _, seg := range p.Segments {
		var step string
		switch {
		case seg.Name != "":
			step = seg.Name
		case seg.Index != nil:
			idx, err := Eval(seg.Index, ctx)
			if err != nil {
				return nil, err
			}
			s, ok := idx.(string)
			if !ok {
				return nil, fmt.Errorf("eval: index must be a string, got %T", idx)
			}
			step = s
		}
		path = path + "." + step
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("eval: cannot navigate into %T at %s", cur, path)
		}
		next, exists := m[step]
		if !exists {
			return nil, fmt.Errorf("eval: %s: not found", path)
		}
		cur = next
	}
	return cur, nil
}
