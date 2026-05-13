package runtime

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/lang"
)

// NodeKind tags a Node with its source block.
type NodeKind string

const (
	NodeResource  NodeKind = "resource"
	NodeData      NodeKind = "data"
	NodeAction    NodeKind = "action"
	NodeOutput    NodeKind = "output"
	NodeComposite NodeKind = "composite"
)

// Node is one addressable element of a stack: a single resource instance,
// data source, action, output, or composite call site. Address is the
// canonical dotted form the language uses to reference the node from
// elsewhere such as `resource.aws.vpc.main` or `output.cluster-arn`. Body
// is the source expression: an ObjectLit for resources/data/actions/
// composites, any Expr for outputs.
//
// A node inside a composite carries the call site address in
// Composite so the runtime evaluates its body against the composite's
// scope rather than the root. Its address looks like
// `resource.<call site>/<ns>.<type>.<name>`, with the call site as a
// prefix joined by a single `/`. For a composite that itself calls
// another composite the chain continues:
// `resource.<outer>/<inner-rel>/<deepest-rel>`, and each node's
// Composite names its direct enclosing call site.
//
// CompositeBody and Modules are set only on NodeComposite.
// CompositeBody points to the composite type's full body so the
// runtime can evaluate the `outputs:` block once the internals
// complete. Modules is the composite's resolved import table; the
// runtime resolves composite-internal node lookups against this map
// rather than the stack root's, so a composite can be reused without
// the caller importing every module it transitively uses.
type Node struct {
	Address       string
	Kind          NodeKind
	NS            string
	Type          string
	Name          string
	Body          lang.Expr
	Composite     string
	CompositeBody *lang.File
	Modules       map[string]*Module
	ForEach       lang.Expr
}

// ExtractNodes walks a parsed stack or exported-type file and returns every
// addressable node in source order. The file's shape is assumed to be
// validated. Malformed subtrees are skipped silently rather than reported
// as they should be validated with `lang.ValidateFile` first.
//
// mods is the imported-module table keyed by alias. It is consulted to
// distinguish primitive resource call sites from composite call sites;
// composites expand into a NodeComposite plus internal nodes. A nil
// or empty mods skips the composite check, in which case every node in
// `resources:` is treated as a primitive.
func ExtractNodes(f *lang.File, mods map[string]*Module) []*Node {
	return extractNodes(f, "", mods)
}

// extractNodes is the recursive workhorse. parent is the address of the
// enclosing composite call site, or "" at root; each non-output node
// gets its Composite set to parent, and resource/data/action addresses
// are prefixed with `parent + "/"` when parent is non-empty. Output
// blocks are only emitted at root: a composite's `outputs:` block is
// consumed by `evalCompositeOutputs` at apply time, not turned into
// DAG nodes.
func extractNodes(f *lang.File, parent string, mods map[string]*Module) []*Node {
	if f == nil || f.Body == nil {
		return nil
	}
	var nodes []*Node
	blocks := topLevelMap(f.Body)
	if obj, ok := blocks["resources"].(*lang.ObjectLit); ok {
		nodes = append(nodes, extractResources(obj, parent, mods)...)
	}
	if obj, ok := blocks["data"].(*lang.ObjectLit); ok {
		nodes = append(nodes, extractNested(obj, NodeData, parent)...)
	}
	if obj, ok := blocks["actions"].(*lang.ObjectLit); ok {
		nodes = append(nodes, extractNested(obj, NodeAction, parent)...)
	}
	if parent == "" {
		if obj, ok := blocks["outputs"].(*lang.ObjectLit); ok {
			nodes = append(nodes, extractOutputs(obj)...)
		}
	}
	return nodes
}

func extractResources(block *lang.ObjectLit, parent string, mods map[string]*Module) []*Node {
	var out []*Node
	for _, ns := range block.Fields {
		if ns.Key.Kind != lang.FieldIdent || ns.Key.IsMeta() {
			continue
		}
		nsObj, ok := ns.Value.(*lang.ObjectLit)
		if !ok {
			continue
		}
		for _, t := range nsObj.Fields {
			if t.Key.Kind != lang.FieldIdent || t.Key.IsMeta() {
				continue
			}
			tObj, ok := t.Value.(*lang.ObjectLit)
			if !ok {
				continue
			}
			composite := lookupComposite(mods, ns.Key.Name, t.Key.Name)
			for _, n := range tObj.Fields {
				if n.Key.Kind != lang.FieldIdent || n.Key.IsMeta() {
					continue
				}
				addr := composeResourceAddress(parent, ns.Key.Name, t.Key.Name, n.Key.Name)
				if composite != nil {
					out = append(out, expandComposite(addr, parent,
						ns.Key.Name, t.Key.Name, n.Key.Name,
						n.Value, composite, mods)...)
					continue
				}
				out = append(out, &Node{
					Address:   addr,
					Kind:      NodeResource,
					NS:        ns.Key.Name,
					Type:      t.Key.Name,
					Name:      n.Key.Name,
					Body:      n.Value,
					Composite: parent,
					ForEach:   extractForEach(n.Value),
				})
			}
		}
	}
	return out
}

func extractNested(block *lang.ObjectLit, kind NodeKind, parent string) []*Node {
	var out []*Node
	for _, ns := range block.Fields {
		if ns.Key.Kind != lang.FieldIdent || ns.Key.IsMeta() {
			continue
		}
		nsObj, ok := ns.Value.(*lang.ObjectLit)
		if !ok {
			continue
		}
		for _, t := range nsObj.Fields {
			if t.Key.Kind != lang.FieldIdent || t.Key.IsMeta() {
				continue
			}
			tObj, ok := t.Value.(*lang.ObjectLit)
			if !ok {
				continue
			}
			for _, n := range tObj.Fields {
				if n.Key.Kind != lang.FieldIdent || n.Key.IsMeta() {
					continue
				}
				out = append(out, &Node{
					Address: composeKindAddress(parent, kind,
						ns.Key.Name, t.Key.Name, n.Key.Name),
					Kind:      kind,
					NS:        ns.Key.Name,
					Type:      t.Key.Name,
					Name:      n.Key.Name,
					Body:      n.Value,
					Composite: parent,
					ForEach:   extractForEach(n.Value),
				})
			}
		}
	}
	return out
}

// extractForEach returns the iterable expression from a body's
// `@for-each:` field, or nil if the body has none. Non-object bodies
// (which the validator rejects elsewhere) yield nil too.
func extractForEach(body lang.Expr) lang.Expr {
	obj, ok := body.(*lang.ObjectLit)
	if !ok {
		return nil
	}
	for _, fld := range obj.Fields {
		if fld.Key.Kind == lang.FieldIdent && fld.Key.Name == "@for-each" {
			return fld.Value
		}
	}
	return nil
}

func lookupComposite(mods map[string]*Module, alias, typ string) *CompositeType {
	if mods == nil {
		return nil
	}
	mod, ok := mods[alias]
	if !ok || mod.Composites == nil {
		return nil
	}
	return mod.Composites[typ]
}

// expandComposite emits the boundary node and the internal sub nodes
// for a composite call site. The boundary node sits at the call site
// address and carries the call site args as Body; the runtime
// evaluates the composite's `outputs:` block via CompositeBody once
// the internals complete. parent is the boundary's enclosing call
// site (empty for top-level call sites; the outer call site address
// for nested ones), and is recorded on the boundary's Composite
// field so the boundary's args evaluate against the right scope.
//
// Internals come from a recursive walk of the composite's body with
// the new call site address as the parent prefix, so nested addresses
// build up like `outer/inner-rel/leaf-rel` and each internal's
// Composite names its direct enclosing call site. The composite's
// own Modules table drives that recursion so a composite that calls
// another composite expands against its own imports rather than the
// caller's. The boundary node also carries the Modules table itself
// so the runtime can resolve internal node lookups against it. When
// a composite has no Modules set, fallMods is used as a fallback
// for tests that build composites directly without populating
// imports.
func expandComposite(callSiteAddr, parent, ns, typ, name string,
	args lang.Expr, composite *CompositeType, fallMods map[string]*Module) []*Node {
	scopeMods := composite.Modules
	if scopeMods == nil {
		scopeMods = fallMods
	}
	out := []*Node{{
		Address:       callSiteAddr,
		Kind:          NodeComposite,
		NS:            ns,
		Type:          typ,
		Name:          name,
		Body:          args,
		Composite:     parent,
		CompositeBody: composite.Body,
		Modules:       scopeMods,
		ForEach:       extractForEach(args),
	}}
	out = append(out, extractNodes(composite.Body, callSiteAddr, scopeMods)...)
	return out
}

func extractOutputs(block *lang.ObjectLit) []*Node {
	var out []*Node
	for _, fld := range block.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		out = append(out, &Node{
			Address: "output." + fld.Key.Name,
			Kind:    NodeOutput,
			Name:    fld.Key.Name,
			Body:    fld.Value,
		})
	}
	return out
}

func topLevelMap(body *lang.ObjectLit) map[string]lang.Expr {
	out := make(map[string]lang.Expr, len(body.Fields))
	for _, f := range body.Fields {
		if f.Key.Kind == lang.FieldIdent && !f.Key.IsMeta() {
			out[f.Key.Name] = f.Value
		}
	}
	return out
}

// composeResourceAddress builds a resource node's address. At root the
// shape is `resource.<ns>.<type>.<name>`. Inside a composite the inner
// part drops the leading `resource.` to fit the spec form
// `<call-site>/<ns>.<type>.<name>`.
func composeResourceAddress(parent, ns, typ, name string) string {
	if parent == "" {
		return fmt.Sprintf("resource.%s.%s.%s", ns, typ, name)
	}
	return fmt.Sprintf("%s/%s.%s.%s", parent, ns, typ, name)
}

// composeKindAddress builds a data or action node's address. Data and
// action addresses keep their kind prefix in the joined form: at root
// it is `<kind>.<ns>.<type>.<name>`, inside a composite it is
// `<call-site>/<kind>.<ns>.<type>.<name>`.
func composeKindAddress(parent string, kind NodeKind, ns, typ, name string) string {
	if parent == "" {
		return fmt.Sprintf("%s.%s.%s.%s", kind, ns, typ, name)
	}
	return fmt.Sprintf("%s/%s.%s.%s.%s", parent, kind, ns, typ, name)
}
