package runtime

import (
	"fmt"
	"strings"

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
	lang.Walk(e, func(node lang.Expr) {
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

// deferredRefs returns the full dotted source paths an expression
// reads from. Unlike Refs, the trailing field segments are preserved
// so the renderer can show `<resource.aws.vpc.main.id>` rather than
// the bare node address. `@each` bindings are skipped because they
// resolve from the for-each scope, not from an upstream node.
func deferredRefs(e lang.Expr) []string {
	if e == nil {
		return nil
	}
	var out []string
	lang.Walk(e, func(node lang.Expr) {
		dp, ok := node.(*lang.DotPath)
		if !ok {
			return
		}
		switch dp.Root.Name {
		case "var", "resource", "data", "action":
		default:
			return
		}
		if path := dotPathString(dp); path != "" {
			out = append(out, path)
		}
	})
	return dedupe(out)
}

// dotPathString renders a dotted reference back to its source form.
// Named segments are joined with `.`; indexed segments preserve the
// `['<key>']` form when the index is a string literal, and otherwise
// collapse to `[...]` so the path stays readable.
func dotPathString(p *lang.DotPath) string {
	var b strings.Builder
	b.WriteString(p.Root.Name)
	for _, seg := range p.Segments {
		switch {
		case seg.Name != "":
			b.WriteByte('.')
			b.WriteString(seg.Name)
		case seg.Index != nil:
			if s, ok := seg.Index.(*lang.StringLit); ok {
				b.WriteString("['")
				b.WriteString(s.Value)
				b.WriteString("']")
			} else {
				b.WriteString("[...]")
			}
		}
	}
	return b.String()
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
