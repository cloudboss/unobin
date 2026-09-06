package runtime

import (
	"fmt"
	"slices"

	"github.com/cloudboss/unobin/pkg/stateref"
)

// PlanGraphV2 returns the dependencies used by the apply scheduler.
func PlanGraphV2(plan *PlanFileV2, dag *DAG) ([]StepNode, error) {
	if plan == nil {
		return nil, fmt.Errorf("plan is required")
	}
	graph, _, err := buildApplyScheduleV2Graph(plan.Steps)
	if err != nil {
		return nil, err
	}
	dependencies := make(map[string][]string, len(plan.Steps))
	for dependency, dependents := range graph.dependents {
		for _, dependent := range dependents {
			dependencies[dependent] = append(dependencies[dependent], dependency)
		}
	}
	nodes := make([]StepNode, len(plan.Steps))
	for i, step := range plan.Steps {
		deps := dependencies[step.Address]
		slices.Sort(deps)
		node := StepNode{Address: step.Address, Kind: step.Kind,
			Composite: step.Operation.Kind == StepComposite, Decision: planStepV2Decision(step),
			DependsOn: slices.Compact(deps), LibraryPath: planStepV2LibraryPath(step)}
		if dag != nil {
			if source := dag.Nodes[templateAddress(step.Address)]; source != nil {
				node.ImportAlias = source.Alias
			}
		}
		if ref, err := stateref.ParseStateRef(step.Address); err == nil {
			segment := ref.Segments[len(ref.Segments)-1]
			node.Name, node.Parent = segmentName(segment), stateRefParent(ref)
			node.Category = string(segment.Category)
		}
		node.ExportKind = planStepV2Export(step)
		nodes[i] = node
	}
	return nodes, nil
}

func planStepV2Export(step PlanStepV2) string {
	switch operation := step.Operation; operation.Kind {
	case StepResource:
		if operation.Resource.Desired != nil {
			return operation.Resource.Desired.Binding.Export
		}
		return operation.Resource.Prior.Binding.Export
	case StepAction:
		if operation.Action.Desired != nil {
			return operation.Action.Desired.Binding.Export
		}
		return operation.Action.Prior.Binding.Export
	case StepDataSource:
		if operation.DataSource.Desired != nil {
			return operation.DataSource.Desired.Binding.Export
		}
		return operation.DataSource.Prior.Binding.Export
	case StepComposite:
		if operation.Composite.Desired != nil {
			return operation.Composite.Desired.Binding.Export
		}
		return operation.Composite.Prior.Binding.Export
	}
	return ""
}
