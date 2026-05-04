package runtime

import (
	"fmt"
	"strings"

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
// prefix joined by a single `/`.
//
// CompositeBody is set only on NodeComposite. It points to the
// composite type's full body so the runtime can evaluate the
// `outputs:` block once the internals complete.
type Node struct {
	Address       string
	Kind          NodeKind
	NS            string
	Type          string
	Name          string
	Body          lang.Expr
	Composite     string
	CompositeBody *lang.File
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
	if f == nil || f.Body == nil {
		return nil
	}
	var nodes []*Node
	blocks := topLevelMap(f.Body)
	if obj, ok := blocks["resources"].(*lang.ObjectLit); ok {
		nodes = append(nodes, extractResources(obj, mods)...)
	}
	if obj, ok := blocks["data"].(*lang.ObjectLit); ok {
		nodes = append(nodes, extractNested(obj, NodeData)...)
	}
	if obj, ok := blocks["actions"].(*lang.ObjectLit); ok {
		nodes = append(nodes, extractNested(obj, NodeAction)...)
	}
	if obj, ok := blocks["outputs"].(*lang.ObjectLit); ok {
		nodes = append(nodes, extractOutputs(obj)...)
	}
	return nodes
}

func extractResources(block *lang.ObjectLit, mods map[string]*Module) []*Node {
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
				addr := fmt.Sprintf("resource.%s.%s.%s",
					ns.Key.Name, t.Key.Name, n.Key.Name)
				if composite != nil {
					out = append(out, expandComposite(addr,
						ns.Key.Name, t.Key.Name, n.Key.Name,
						n.Value, composite)...)
					continue
				}
				out = append(out, &Node{
					Address: addr,
					Kind:    NodeResource,
					NS:      ns.Key.Name,
					Type:    t.Key.Name,
					Name:    n.Key.Name,
					Body:    n.Value,
				})
			}
		}
	}
	return out
}

func extractNested(block *lang.ObjectLit, kind NodeKind) []*Node {
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
					Address: fmt.Sprintf("%s.%s.%s.%s",
						kind, ns.Key.Name, t.Key.Name, n.Key.Name),
					Kind: kind,
					NS:   ns.Key.Name,
					Type: t.Key.Name,
					Name: n.Key.Name,
					Body: n.Value,
				})
			}
		}
	}
	return out
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
// the internals complete. Each internal address prefixes the call
// site with `/`, drops the leading `resource.` from the inner part
// to fit the spec form, and carries the call site address in
// Composite so edge building and evaluation route through the
// composite's scope.
func expandComposite(callSiteAddr, ns, typ, name string,
	args lang.Expr, composite *CompositeType) []*Node {
	out := []*Node{{
		Address:       callSiteAddr,
		Kind:          NodeComposite,
		NS:            ns,
		Type:          typ,
		Name:          name,
		Body:          args,
		CompositeBody: composite.Body,
	}}
	for _, internal := range ExtractNodes(composite.Body, nil) {
		if internal.Kind == NodeOutput {
			continue
		}
		internal.Address = callSiteAddr + "/" + strings.TrimPrefix(internal.Address, "resource.")
		internal.Composite = callSiteAddr
		out = append(out, internal)
	}
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
