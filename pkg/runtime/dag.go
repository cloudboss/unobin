package runtime

import "github.com/cloudboss/unobin/pkg/lang"

// DAG is a stack's runtime dependency graph: every addressable node
// indexed by its address, and the list of node addresses each one
// depends on, collected from references in the body and from any
// `@depends-on` meta key.
type DAG struct {
	Nodes map[string]*Node
	Edges map[string][]string
}

// BuildDAG walks a parsed stack file and returns its dependency graph.
// The file is assumed to be validated.
func BuildDAG(f *lang.File) *DAG {
	nodes := ExtractNodes(f)
	g := &DAG{
		Nodes: make(map[string]*Node, len(nodes)),
		Edges: make(map[string][]string, len(nodes)),
	}
	for _, n := range nodes {
		g.Nodes[n.Address] = n
		g.Edges[n.Address] = dependenciesOf(n)
	}
	return g
}

func dependenciesOf(n *Node) []string {
	deps := Refs(n.Body)
	if obj, ok := n.Body.(*lang.ObjectLit); ok {
		for _, fld := range obj.Fields {
			if fld.Key.Kind != lang.FieldIdent || fld.Key.Name != "@depends-on" {
				continue
			}
			arr, ok := fld.Value.(*lang.ArrayLit)
			if !ok {
				continue
			}
			for _, el := range arr.Elements {
				dp, ok := el.(*lang.DotPath)
				if !ok {
					continue
				}
				if addr := refAddress(dp); addr != "" {
					deps = append(deps, addr)
				}
			}
		}
	}
	return dedupe(deps)
}
