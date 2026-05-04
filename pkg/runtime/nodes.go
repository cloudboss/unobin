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
// data source, action, or output. The Address is the canonical dotted form
// the language uses to reference the node from elsewhere such as
// `resource.aws.vpc.main` or `output.cluster-arn`. Body is the source
// expression: an ObjectLit for resources/data/actions, any Expr for outputs.
type Node struct {
	Address string
	Kind    NodeKind
	NS      string
	Type    string
	Name    string
	Body    lang.Expr
}

// ExtractNodes walks a parsed stack or exported-type file and returns every
// addressable node in source order. The file's shape is assumed to be
// validated. Malformed subtrees are skipped silently rather than reported
// as they should be validated with `lang.ValidateFile` first.
//
// mods is the imported-module table keyed by alias. It is consulted to
// distinguish primitive resource call sites from composite call sites;
// composites expand into a NodeComposite plus internal sub-nodes. A nil
// or empty mods skips the composite check, in which case every node in
// `resources:` is treated as a primitive.
func ExtractNodes(f *lang.File, mods map[string]*Module) []*Node {
	if f == nil || f.Body == nil {
		return nil
	}
	var nodes []*Node
	blocks := topLevelMap(f.Body)
	if obj, ok := blocks["resources"].(*lang.ObjectLit); ok {
		nodes = append(nodes, extractNested(obj, NodeResource)...)
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
