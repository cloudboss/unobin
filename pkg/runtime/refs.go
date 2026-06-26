package runtime

import (
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
)

// Refs returns the addresses an expression depends on, in source order
// with duplicates removed. Each returned address is the canonical form
// of another node: input.name, resource.name, data-source.name, or action.name.
// Field segments past the node address and @each.X bindings are skipped.
func Refs(e lang.Expr) []string {
	if e == nil {
		return nil
	}
	var out []string
	lang.ScanExpr(e, lang.ScanCallbacks{
		DotPath: func(dp *lang.DotPath, _ lang.ScanContext) lang.ScanDecision {
			if addr := RefAddress(dp); addr != "" {
				out = append(out, addr)
			}
			return lang.ScanContinue
		},
	})
	return dedupe(out)
}

// walkExpandingLocals visits every DotPath in e that is not itself a
// `local.<name>` reference. A local reference is expanded in place: the
// visitor instead sees the dotpaths the navigation actually reads,
// transitively through chained locals. When the reference navigates
// into the local (`local.X.field`), only that field's sources are
// visited, not every source the local references; see narrowLocal. A
// local that is being expanded higher in the chain is skipped, so a
// self-referential or cyclic locals block cannot loop forever here; the
// cycle itself is reported elsewhere.
func walkExpandingLocals(e lang.Expr, locals map[string]lang.Expr, visit func(*lang.DotPath)) {
	if e == nil {
		return
	}
	expanding := map[string]bool{}
	var walk func(lang.Expr)
	walk = func(expr lang.Expr) {
		lang.ScanExpr(expr, lang.ScanCallbacks{
			DotPath: func(dp *lang.DotPath, _ lang.ScanContext) lang.ScanDecision {
				if dp.Root.Name != "local" {
					visit(dp)
					return lang.ScanContinue
				}
				if len(dp.Segments) == 0 || dp.Segments[0].Name == "" {
					return lang.ScanSkipChildren
				}
				name := dp.Segments[0].Name
				if expanding[name] {
					return lang.ScanSkipChildren
				}
				sub, ok := locals[name]
				if !ok {
					return lang.ScanSkipChildren
				}
				expanding[name] = true
				for _, narrowed := range narrowLocal(sub, dp.Segments[1:]) {
					walk(narrowed)
				}
				delete(expanding, name)
				return lang.ScanSkipChildren
			},
		})
	}
	walk(e)
}

// narrowLocal returns the sub-expressions a `local.<name>` reference
// actually reads when it navigates into the local's value with the
// given trailing segments. A dot-path local grows the trailing segments
// onto its own path so only the trailed field's upstream is followed.
// An object literal selects the named field and keeps narrowing into
// it. Anything else (calls, comprehensions, conditionals, computed
// indexes) cannot be narrowed by static inspection, so the whole
// expression is returned and analysis stays conservative-correct.
func narrowLocal(expr lang.Expr, trailing []lang.DotSegment) []lang.Expr {
	if len(trailing) == 0 {
		return []lang.Expr{expr}
	}
	switch v := expr.(type) {
	case *lang.DotPath:
		return []lang.Expr{graftDotPath(v, trailing)}
	case *lang.ObjectLit:
		if trailing[0].Name == "" {
			return []lang.Expr{expr}
		}
		field := objectField(v, trailing[0].Name)
		if field == nil {
			return nil
		}
		return narrowLocal(field, trailing[1:])
	default:
		return []lang.Expr{expr}
	}
}

// graftDotPath returns a new DotPath that reads into path with extra
// trailing segments, as if the source had written path.<trailing>
// inline. The original path is left unmodified.
func graftDotPath(path *lang.DotPath, trailing []lang.DotSegment) *lang.DotPath {
	segs := make([]lang.DotSegment, 0, len(path.Segments)+len(trailing))
	segs = append(segs, path.Segments...)
	segs = append(segs, trailing...)
	return &lang.DotPath{Root: path.Root, Segments: segs}
}

// objectField returns the value of the named field in obj, or nil when
// no such field is declared. Meta keys (`@...`) are skipped.
func objectField(obj *lang.ObjectLit, name string) lang.Expr {
	for _, fld := range obj.Fields {
		if fld.Key.IsMeta() {
			continue
		}
		if fld.Key.Name == name || fld.Key.String == name {
			return fld.Value
		}
	}
	return nil
}

// refsWithLocals returns the node addresses e depends on, expanding
// every `local.<name>` reference into the refs of that local's own
// expression. When locals is empty this matches Refs.
func refsWithLocals(e lang.Expr, locals map[string]lang.Expr) []string {
	var out []string
	walkExpandingLocals(e, locals, func(dp *lang.DotPath) {
		if addr := RefAddress(dp); addr != "" {
			out = append(out, addr)
		}
	})
	return dedupe(out)
}

func refsWithLocalsInScope(
	e lang.Expr,
	locals map[string]lang.Expr,
	nodes map[string]*Node,
	scope string,
) []string {
	var out []string
	walkExpandingLocals(e, locals, func(dp *lang.DotPath) {
		if match, ok := RefMatchInScope(dp, nodes, scope); ok {
			out = append(out, match.Address)
		}
	})
	return dedupe(out)
}

// deferredRefs returns the full dotted source paths an expression
// reads from. Unlike Refs, the trailing field segments are preserved
// so the renderer can show `<resource.app.id>` rather than the bare
// node address. A `local.<name>` reference expands to the
// paths inside the local's own expression, so a plan field reading a
// local still shows the real upstream it is waiting on. `@each`
// bindings are skipped because they resolve from the for-each scope,
// not from an upstream node.
func deferredRefs(e lang.Expr, locals map[string]lang.Expr) []string {
	var out []string
	walkExpandingLocals(e, locals, func(dp *lang.DotPath) {
		switch dp.Root.Name {
		case "input", "resource", "data-source", "action":
			if path := DotPathString(dp); path != "" {
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
func DotPathString(p *lang.DotPath) string {
	var b strings.Builder
	b.WriteString(p.Root.Name)
	for _, seg := range p.Segments {
		switch {
		case seg.Splat:
			b.WriteString("[*]")
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

func RefAddress(p *lang.DotPath) string {
	switch p.Root.Name {
	case "input", "resource", "data-source", "action":
		if len(p.Segments) == 0 || p.Segments[0].Name == "" {
			return ""
		}
		return p.Root.Name + "." + p.Segments[0].Name
	default:
		return ""
	}
}

type RefMatch struct {
	Address  string
	Segments int
}

func RefMatchInScope(
	p *lang.DotPath,
	nodes map[string]*Node,
	scope string,
) (RefMatch, bool) {
	if p == nil || p.Root == nil {
		return RefMatch{}, false
	}
	if p.Root.Name == "input" {
		if len(p.Segments) == 0 || p.Segments[0].Name == "" {
			return RefMatch{}, false
		}
		return RefMatch{Address: "input." + p.Segments[0].Name, Segments: 1}, true
	}
	if p.Root.Name != "resource" && p.Root.Name != "data-source" && p.Root.Name != "action" {
		return RefMatch{}, false
	}
	for _, n := range []int{3, 1} {
		if len(p.Segments) < n || !plainSegments(p.Segments[:n]) {
			continue
		}
		addr := p.Root.Name + "." + segmentNames(p.Segments[:n])
		addr = ScopeRef(addr, scope)
		if _, ok := nodes[addr]; ok {
			return RefMatch{Address: addr, Segments: n}, true
		}
	}
	return RefMatch{}, false
}

func UnknownRefAddress(p *lang.DotPath, nodes map[string]*Node, scope string) string {
	if p == nil || p.Root == nil {
		return ""
	}
	if p.Root.Name != "resource" && p.Root.Name != "data-source" && p.Root.Name != "action" {
		return ""
	}
	if scopeHasShortNodes(nodes, scope, p.Root.Name) {
		if len(p.Segments) >= 1 && plainSegments(p.Segments[:1]) {
			return ScopeRef(p.Root.Name+"."+p.Segments[0].Name, scope)
		}
	}
	if len(p.Segments) >= 3 && plainSegments(p.Segments[:3]) {
		return ScopeRef(p.Root.Name+"."+segmentNames(p.Segments[:3]), scope)
	}
	if len(p.Segments) >= 1 && plainSegments(p.Segments[:1]) {
		return ScopeRef(p.Root.Name+"."+p.Segments[0].Name, scope)
	}
	return ""
}

func scopeHasShortNodes(nodes map[string]*Node, scope, root string) bool {
	prefix := ""
	if scope != "" {
		prefix = scope + "/"
	}
	prefix += root + "."
	for addr := range nodes {
		if !strings.HasPrefix(addr, prefix) {
			continue
		}
		rest := strings.TrimPrefix(addr, prefix)
		if !strings.Contains(rest, ".") && !strings.Contains(rest, "/") {
			return true
		}
	}
	return false
}

func plainSegments(segs []lang.DotSegment) bool {
	for _, seg := range segs {
		if seg.Name == "" || seg.Index != nil || seg.Splat || seg.Guarded {
			return false
		}
	}
	return true
}

func segmentNames(segs []lang.DotSegment) string {
	parts := make([]string, 0, len(segs))
	for _, seg := range segs {
		parts = append(parts, seg.Name)
	}
	return strings.Join(parts, ".")
}

// pairKeyDeps returns the set of template addresses an expression
// references with an `[@each.key]` index segment. For each entry in
// the result, the body says "depend on a specific instance of this
// template, the instance whose key matches my own for-each key,"
// which lets the apply scheduler narrow a cartesian fan-out down to
// a same-key pair. Refs that do not have an `[@each.key]` index segment
// are not included even if their template appears elsewhere indexed.
func pairKeyDeps(
	e lang.Expr,
	nodes map[string]*Node,
	scope string,
) map[string]bool {
	if e == nil {
		return nil
	}
	out := map[string]bool{}
	lang.ScanExpr(e, lang.ScanCallbacks{
		DotPath: func(dp *lang.DotPath, _ lang.ScanContext) lang.ScanDecision {
			match, ok := RefMatchInScope(dp, nodes, scope)
			if !ok {
				return lang.ScanContinue
			}
			if !strings.HasPrefix(match.Address, "resource.") &&
				!strings.HasPrefix(match.Address, "data-source.") &&
				!strings.HasPrefix(match.Address, "action.") {
				return lang.ScanContinue
			}
			if len(dp.Segments) <= match.Segments {
				return lang.ScanContinue
			}
			seg := dp.Segments[match.Segments]
			if seg.Index == nil {
				return lang.ScanContinue
			}
			idx, ok := seg.Index.(*lang.DotPath)
			if !ok || idx.Root == nil || idx.Root.Name != "@each" {
				return lang.ScanContinue
			}
			if len(idx.Segments) != 1 || idx.Segments[0].Name != "key" {
				return lang.ScanContinue
			}
			out[match.Address] = true
			return lang.ScanContinue
		},
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
