package runtime

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/lang"
)

// Refs returns the addresses an expression depends on, in source order
// with duplicates removed. Each returned address is the canonical form
// of another node:
//
//	var.<name>
//	resource.<ns>.<type>.<name>
//	data.<ns>.<type>.<name>
//	action.<ns>.<type>.<name>
//
// Field segments past the node address (`.id`, `['key'].arn`) and
// `@each.X` bindings are skipped.
func Refs(e lang.Expr) []string {
	if e == nil {
		return nil
	}
	var out []string
	walkExpr(e, func(node lang.Expr) {
		dp, ok := node.(*lang.DotPath)
		if !ok {
			return
		}
		if addr := refAddress(dp); addr != "" {
			out = append(out, addr)
		}
	})
	return dedupe(out)
}

func refAddress(p *lang.DotPath) string {
	switch p.Root.Name {
	case "var":
		if len(p.Segments) == 0 || p.Segments[0].Name == "" {
			return ""
		}
		return "var." + p.Segments[0].Name
	case "resource", "data", "action":
		if len(p.Segments) < 3 {
			return ""
		}
		for i := 0; i < 3; i++ {
			if p.Segments[i].Name == "" {
				return ""
			}
		}
		return fmt.Sprintf("%s.%s.%s.%s",
			p.Root.Name,
			p.Segments[0].Name,
			p.Segments[1].Name,
			p.Segments[2].Name)
	default:
		return ""
	}
}

func walkExpr(e lang.Expr, visit func(lang.Expr)) {
	if e == nil {
		return
	}
	visit(e)
	switch v := e.(type) {
	case *lang.ObjectLit:
		for _, fld := range v.Fields {
			walkExpr(fld.Value, visit)
		}
	case *lang.ArrayLit:
		for _, el := range v.Elements {
			walkExpr(el, visit)
		}
	case *lang.Call:
		for _, arg := range v.Args {
			walkExpr(arg, visit)
		}
	case *lang.Infix:
		walkExpr(v.Left, visit)
		walkExpr(v.Right, visit)
	case *lang.Prefix:
		walkExpr(v.Expr, visit)
	case *lang.DotPath:
		for _, seg := range v.Segments {
			if seg.Index != nil {
				walkExpr(seg.Index, visit)
			}
		}
	}
}

func dedupe(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(s))
	out := make([]string, 0, len(s))
	for _, x := range s {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
