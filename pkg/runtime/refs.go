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

// walkExpandingLocals visits every DotPath in e that is not itself a
// `local.<name>` reference. A local reference is expanded in place:
// the visitor instead sees the dotpaths inside that local's own
// expression, transitively through chained locals. Each local is
// expanded at most once, so a self-referential or cyclic locals block
// cannot loop forever here; the cycle itself is reported elsewhere.
func walkExpandingLocals(e lang.Expr, locals map[string]lang.Expr, visit func(*lang.DotPath)) {
	if e == nil {
		return
	}
	expanded := map[string]bool{}
	var walk func(lang.Expr)
	walk = func(expr lang.Expr) {
		lang.Walk(expr, func(node lang.Expr) {
			dp, ok := node.(*lang.DotPath)
			if !ok {
				return
			}
			if dp.Root.Name == "local" {
				if len(dp.Segments) == 0 || dp.Segments[0].Name == "" {
					return
				}
				name := dp.Segments[0].Name
				if expanded[name] {
					return
				}
				expanded[name] = true
				if sub, ok := locals[name]; ok {
					walk(sub)
				}
				return
			}
			visit(dp)
		})
	}
	walk(e)
}

// refsWithLocals returns the node addresses e depends on, expanding
// every `local.<name>` reference into the refs of that local's own
// expression. When locals is empty this matches Refs.
func refsWithLocals(e lang.Expr, locals map[string]lang.Expr) []string {
	var out []string
	walkExpandingLocals(e, locals, func(dp *lang.DotPath) {
		if addr := refAddress(dp); addr != "" {
			out = append(out, addr)
		}
	})
	return dedupe(out)
}

// deferredRefs returns the full dotted source paths an expression
// reads from. Unlike Refs, the trailing field segments are preserved
// so the renderer can show `<resource.aws.vpc.main.id>` rather than
// the bare node address. A `local.<name>` reference expands to the
// paths inside the local's own expression, so a plan field reading a
// local still shows the real upstream it is waiting on. `@each`
// bindings are skipped because they resolve from the for-each scope,
// not from an upstream node.
func deferredRefs(e lang.Expr, locals map[string]lang.Expr) []string {
	var out []string
	walkExpandingLocals(e, locals, func(dp *lang.DotPath) {
		switch dp.Root.Name {
		case "var", "resource", "data", "action":
			if path := dotPathString(dp); path != "" {
				out = append(out, path)
			}
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

// pairKeyDeps returns the set of template addresses an expression
// references with an `[@each.key]` index segment. For each entry in
// the result, the body says "depend on a specific instance of this
// template, the instance whose key matches my own for-each key,"
// which lets the apply scheduler narrow a cartesian fan-out down to
// a same-key pair. Refs that do not carry an `[@each.key]` selector
// are not included even if their template appears elsewhere indexed.
func pairKeyDeps(e lang.Expr) map[string]bool {
	if e == nil {
		return nil
	}
	out := map[string]bool{}
	lang.Walk(e, func(node lang.Expr) {
		dp, ok := node.(*lang.DotPath)
		if !ok {
			return
		}
		addr := refAddress(dp)
		if addr == "" {
			return
		}
		if !strings.HasPrefix(addr, "resource.") &&
			!strings.HasPrefix(addr, "data.") &&
			!strings.HasPrefix(addr, "action.") {
			return
		}
		if len(dp.Segments) <= 3 {
			return
		}
		seg := dp.Segments[3]
		if seg.Index == nil {
			return
		}
		idx, ok := seg.Index.(*lang.DotPath)
		if !ok || idx.Root == nil || idx.Root.Name != "@each" {
			return
		}
		if len(idx.Segments) != 1 || idx.Segments[0].Name != "key" {
			return
		}
		out[addr] = true
	})
	if len(out) == 0 {
		return nil
	}
	return out
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
