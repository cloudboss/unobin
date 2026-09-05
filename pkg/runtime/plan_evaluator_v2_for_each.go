package runtime

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/stateref"
)

func (e *Executor) expandPlanEvaluationV2Node(
	evaluation *planEvaluationV2,
	node *Node,
) ([]*Node, error) {
	if e == nil {
		return nil, fmt.Errorf("executor is required")
	}
	if e.DAG == nil {
		return nil, fmt.Errorf("dependency graph is required")
	}
	if evaluation == nil || evaluation.run == nil || evaluation.run.eval == nil ||
		evaluation.run.forEachInstances == nil {
		return nil, fmt.Errorf("version 2 plan evaluation is required")
	}
	if node == nil {
		return nil, fmt.Errorf("planning node is required")
	}
	if !validCompositeCategory(node.Kind) {
		return nil, fmt.Errorf(
			"%s: only resources, data sources, and actions can be expanded", node.Address,
		)
	}
	if err := validateNodeAddress(node.Address, node.Kind); err != nil {
		return nil, err
	}
	if node.ForEach == nil {
		cloned := *node
		return []*Node{&cloned}, nil
	}
	if _, _, keyed := splitEntryKey(node.Address); keyed {
		return nil, fmt.Errorf("%s: for-each node already has an instance key", node.Address)
	}
	source := e.DAG.Nodes[templateAddress(node.Address)]
	if source == nil || source.ForEach == nil {
		return nil, fmt.Errorf("%s: for-each template is required", node.Address)
	}
	scope, err := e.enclosingScope(evaluation.run, node.Address)
	if err != nil {
		return nil, err
	}
	instances, err := forEachInstancesFor(evaluation.run, node.Address, source.ForEach, scope)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", node.Address, err)
	}
	nodes := make([]*Node, 0, len(instances))
	for _, key := range sortedKeys(instances) {
		address, err := stateref.AppendInstanceKey(node.Address, key)
		if err != nil {
			return nil, fmt.Errorf("%s: for-each key %q: %w", node.Address, key, err)
		}
		instance := *node
		instance.Address = address
		instance.ForEach = nil
		nodes = append(nodes, &instance)
	}
	return nodes, nil
}

func (e *Executor) planEvaluationV2Scope(
	evaluation *planEvaluationV2,
	node *Node,
) (*EvalContext, error) {
	scope, err := e.enclosingScope(evaluation.run, node.Address)
	if err != nil {
		return nil, err
	}
	address, key, keyed := splitEntryKey(node.Address)
	if !keyed {
		return scope, nil
	}
	source := e.DAG.Nodes[templateAddress(node.Address)]
	if source == nil || source.ForEach == nil {
		return nil, fmt.Errorf("%s: for-each template is required", node.Address)
	}
	instances, err := forEachInstancesFor(evaluation.run, address, source.ForEach, scope)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", node.Address, err)
	}
	value, ok := instances[key]
	if !ok {
		return nil, fmt.Errorf("%s: %w", node.Address, ErrInstanceGone)
	}
	return childScopeWithEach(scope, key, value), nil
}
