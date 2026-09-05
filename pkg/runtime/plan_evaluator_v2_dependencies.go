package runtime

import (
	"fmt"
	"slices"
)

func (e *Executor) planEvaluationV2NodeDependencies(
	evaluation *planEvaluationV2,
	node *Node,
) ([]string, error) {
	if e == nil || e.DAG == nil {
		return nil, fmt.Errorf("executor dependency graph is required")
	}
	if evaluation == nil || evaluation.run == nil || evaluation.run.eval == nil ||
		evaluation.run.forEachInstances == nil {
		return nil, fmt.Errorf("version 2 plan evaluation is required")
	}
	if node == nil {
		return nil, fmt.Errorf("planning node is required")
	}
	kind := node.Kind
	if kind == NodeLibraryConfig {
		kind = NodeLibraryConfiguration
	}
	if err := validateNodeAddress(node.Address, kind); err != nil {
		return nil, err
	}
	sourceAddress := templateAddress(node.Address)
	if node.Kind == NodeLibraryConfig {
		sourceAddress = libraryConfigNodeAddress(node.Composite, node.Alias)
	}
	edges := planEvaluationV2Dependencies(e.DAG.Edges[sourceAddress])
	dependencies := make([]string, 0, len(edges))
	for _, edge := range edges {
		dependency := edge
		if node.IsComposite() {
			dependency = rewriteAddress(dependency, sourceAddress, node.Address)
		}
		parent := DirectParent(node.Address)
		for scope := node.Composite; scope != ""; scope = DirectParent(scope) {
			dependency = rewriteAddress(dependency, scope, parent)
			parent = DirectParent(parent)
		}
		instances, err := e.expandPlanEvaluationV2Dependency(evaluation, dependency)
		if err != nil {
			return nil, fmt.Errorf("%s: dependency %s: %w", node.Address, edge, err)
		}
		dependencies = append(dependencies, instances...)
	}
	slices.Sort(dependencies)
	return slices.Compact(dependencies), nil
}

func (e *Executor) expandPlanEvaluationV2Dependency(
	evaluation *planEvaluationV2,
	address string,
) ([]string, error) {
	parent := DirectParent(address)
	addresses := []string{address}
	if parent != "" {
		parents, err := e.expandPlanEvaluationV2Dependency(evaluation, parent)
		if err != nil {
			return nil, err
		}
		addresses = make([]string, 0, len(parents))
		for _, instance := range parents {
			addresses = append(addresses, instance+address[len(parent):])
		}
	}
	dependencies := make([]string, 0, len(addresses))
	for _, instance := range addresses {
		source := e.DAG.Nodes[templateAddress(instance)]
		_, _, keyed := splitEntryKey(instance)
		if source == nil || source.ForEach == nil || keyed {
			dependencies = append(dependencies, instance)
			continue
		}
		node := *source
		node.Address = instance
		nodes, err := e.expandPlanEvaluationV2Node(evaluation, &node)
		if err != nil {
			return nil, err
		}
		for _, node := range nodes {
			dependencies = append(dependencies, node.Address)
		}
	}
	return dependencies, nil
}
