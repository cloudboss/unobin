// Package graphprint renders a runtime DAG as either a plain
// indented listing or a Graphviz DOT document. Both unobin's stack
// binaries (via pkg/runner) and the dev CLI (cmd/unobin/root/print-graph)
// share these renderers so the two paths produce identical output for
// the same graph.
package graphprint

import (
	"fmt"
	"io"
	"sort"

	"github.com/cloudboss/unobin/pkg/runtime"
)

// Plain writes the DAG to out as an indented listing: one node per
// stanza, with each outgoing edge prefixed by "->". Addresses are
// sorted lexicographically so the output is stable across runs.
func Plain(out io.Writer, dag *runtime.DAG) {
	addrs := sortedNodeAddresses(dag)
	for i, a := range addrs {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintln(out, a)
		deps := append([]string{}, dag.Edges[a]...)
		sort.Strings(deps)
		for _, d := range deps {
			fmt.Fprintf(out, "  -> %s\n", d)
		}
	}
}

// DOT writes the DAG to out as a Graphviz directed graph named name.
// Edges to non-node addresses (like `var.X`) are skipped so the
// rendered graph contains only nodes the runtime actually executes.
func DOT(out io.Writer, dag *runtime.DAG, name string) {
	fmt.Fprintf(out, "digraph %q {\n", name)
	addrs := sortedNodeAddresses(dag)
	for _, a := range addrs {
		fmt.Fprintf(out, "  %q;\n", a)
	}
	for _, from := range addrs {
		deps := append([]string{}, dag.Edges[from]...)
		sort.Strings(deps)
		for _, dep := range deps {
			if _, ok := dag.Nodes[dep]; !ok {
				continue
			}
			fmt.Fprintf(out, "  %q -> %q;\n", from, dep)
		}
	}
	fmt.Fprintln(out, "}")
}

func sortedNodeAddresses(dag *runtime.DAG) []string {
	addrs := make([]string, 0, len(dag.Nodes))
	for a := range dag.Nodes {
		addrs = append(addrs, a)
	}
	sort.Strings(addrs)
	return addrs
}
