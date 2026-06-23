package runtime

import (
	"slices"

	"github.com/cloudboss/unobin/pkg/stateref"
)

// StepNode is one plan step in the instance-form dependency graph
// PlanGraph returns: the step's identity plus the addresses it waits
// for under apply's scheduler.
type StepNode struct {
	Address     string   `json:"address"`
	Kind        NodeKind `json:"node-kind"`
	Composite   bool     `json:"composite,omitempty"`
	Decision    Decision `json:"decision"`
	DependsOn   []string `json:"depends-on,omitempty"`
	Category    string   `json:"category,omitempty"`
	ImportAlias string   `json:"import-alias,omitempty"`
	LibraryPath string   `json:"library-path,omitempty"`
	ExportKind  string   `json:"kind,omitempty"`
	Name        string   `json:"name,omitempty"`
	Parent      string   `json:"parent,omitempty"`
}

// PlanGraph returns the dependency graph apply schedules pf with: one
// node per plan step, in plan order, each listing the instance-form
// addresses it waits for. The edges are the scheduler's own, so a
// destroy step already waits on the reversal of its recorded
// dependencies. Each node's DependsOn is sorted.
func PlanGraph(pf *PlanFile, dag *DAG) []StepNode {
	g := buildStepGraph(pf, dag)
	deps := make(map[string][]string, len(pf.Steps))
	for dep, dependents := range g.dependents {
		for _, d := range dependents {
			deps[d] = append(deps[d], dep)
		}
	}
	nodes := make([]StepNode, len(pf.Steps))
	for i := range pf.Steps {
		s := &pf.Steps[i]
		d := deps[s.Address]
		slices.Sort(d)
		display := stepNodeDisplay(s)
		nodes[i] = StepNode{
			Address:     s.Address,
			Kind:        s.Kind,
			Composite:   s.Composite,
			Decision:    s.Decision,
			DependsOn:   slices.Compact(d),
			Category:    display.category,
			ImportAlias: display.importAlias,
			LibraryPath: display.libraryPath,
			ExportKind:  display.exportKind,
			Name:        display.name,
			Parent:      display.parent,
		}
	}
	return nodes
}

type stepNodeDisplayFields struct {
	category    string
	importAlias string
	libraryPath string
	exportKind  string
	name        string
	parent      string
}

func stepNodeDisplay(step *PlanStep) stepNodeDisplayFields {
	if step.Binding == nil {
		return stepNodeDisplayFields{}
	}
	ref, err := stateref.ParseStateRef(step.Address)
	if err != nil {
		return stepNodeDisplayFields{}
	}
	segment := ref.Segments[len(ref.Segments)-1]
	return stepNodeDisplayFields{
		category:    string(segment.Category),
		importAlias: step.Binding.Alias,
		libraryPath: step.Binding.LibraryPath,
		exportKind:  step.Binding.Export,
		name:        segmentName(segment),
		parent:      stateRefParent(ref),
	}
}

func segmentName(segment stateref.StateAddressSegment) string {
	if segment.Key == nil {
		return segment.Name
	}
	rendered := segment.String()
	return rendered[len(string(segment.Category))+1:]
}

func stateRefParent(ref stateref.StateRef) string {
	if len(ref.Segments) <= 1 {
		return ""
	}
	return stateref.StateRef{Segments: ref.Segments[:len(ref.Segments)-1]}.String()
}
