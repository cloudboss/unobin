package runtime

import "slices"

// StepNode is one plan step in the instance-form dependency graph
// PlanGraph returns: the step's identity plus the addresses it waits
// for under apply's scheduler.
type StepNode struct {
	Address   string   `json:"address"`
	Kind      NodeKind `json:"node-kind"`
	Composite bool     `json:"composite,omitempty"`
	Decision  Decision `json:"decision"`
	DependsOn []string `json:"depends-on,omitempty"`
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
		nodes[i] = StepNode{
			Address:   s.Address,
			Kind:      s.Kind,
			Composite: s.Composite,
			Decision:  s.Decision,
			DependsOn: slices.Compact(d),
		}
	}
	return nodes
}
