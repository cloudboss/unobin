package graphprint

import (
	"fmt"
	"slices"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type Format string

const (
	FormatText   Format = "text"
	FormatJSON   Format = "json"
	FormatUnobin Format = "unobin"
	FormatDOT    Format = "dot"
)

func ParseFormat(value string) (Format, error) {
	switch Format(value) {
	case FormatText, FormatJSON, FormatUnobin, FormatDOT:
		return Format(value), nil
	default:
		return "", fmt.Errorf(
			"--format: unknown '%s' (want text, json, unobin, dot)", value,
		)
	}
}

func (f Format) Machine() bool {
	return f == FormatJSON || f == FormatUnobin
}

type Binding struct {
	LibraryPath *string `json:"library-path" ub:"library-path"`
	Alias       string  `json:"alias"        ub:"alias"`
	Export      string  `json:"export"       ub:"export"`
}

type Node struct {
	Address   string   `json:"address"   ub:"address"`
	Category  string   `json:"category"  ub:"category"`
	Binding   *Binding `json:"binding"   ub:"binding"`
	Composite bool     `json:"composite" ub:"composite"`
}

type Edge struct {
	From string `json:"from" ub:"from"`
	To   string `json:"to"   ub:"to"`
}

type Document struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	Name          string                  `json:"name"           ub:"name"`
	Nodes         []Node                  `json:"nodes"          ub:"nodes"`
	Edges         []Edge                  `json:"edges"          ub:"edges"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

func BuildDocument(
	dag *runtime.DAG,
	name string,
	diagnostics []diagnostic.Diagnostic,
) Document {
	document := Document{
		Kind:          "graph",
		FormatVersion: 1,
		Name:          name,
		Nodes:         []Node{},
		Edges:         []Edge{},
		Diagnostics:   diagnostic.Normalize(diagnostics),
	}
	if dag == nil {
		return document
	}
	for _, address := range sortedNodeAddresses(dag) {
		runtimeNode := dag.Nodes[address]
		node := Node{Address: address}
		if runtimeNode != nil {
			node.Category = string(runtimeNode.Kind)
			node.Binding = graphBinding(runtimeNode)
			node.Composite = runtimeNode.IsComposite()
		}
		document.Nodes = append(document.Nodes, node)
	}
	edges := map[Edge]struct{}{}
	for from, targets := range dag.Edges {
		for _, target := range targets {
			edges[Edge{From: from, To: target}] = struct{}{}
		}
	}
	for edge := range edges {
		document.Edges = append(document.Edges, edge)
	}
	slices.SortFunc(document.Edges, func(a, b Edge) int {
		if byFrom := compareString(a.From, b.From); byFrom != 0 {
			return byFrom
		}
		return compareString(a.To, b.To)
	})
	return document
}

func graphBinding(node *runtime.Node) *Binding {
	if node == nil || node.Alias == "" || node.Type == "" {
		return nil
	}
	switch node.Kind {
	case runtime.NodeResource, runtime.NodeDataSource, runtime.NodeAction:
	default:
		return nil
	}
	var libraryPath *string
	if node.LibraryPath != "" {
		value := node.LibraryPath
		libraryPath = &value
	}
	return &Binding{
		LibraryPath: libraryPath,
		Alias:       node.Alias,
		Export:      node.Type,
	}
}

func compareString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
