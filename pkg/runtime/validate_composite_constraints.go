package runtime

import (
	"cmp"
	"errors"
	"slices"
)

// ValidateCompositeConstraints checks every composite whose call arguments
// can be evaluated from factory inputs without reading resources or state.
func (e *Executor) ValidateCompositeConstraints() error {
	if e == nil || e.DAG == nil {
		return nil
	}
	rs, err := e.compositeValidationState()
	if err != nil {
		return err
	}
	var violations []error
	for _, boundary := range e.directCompositeChildren("") {
		found, err := e.validateCompositeBoundary(rs, boundary, boundary.Address)
		if err != nil {
			return err
		}
		violations = append(violations, found...)
	}
	return errors.Join(violations...)
}

func (e *Executor) compositeValidationState() (*runState, error) {
	rootAssets, err := e.rootAssetSet()
	if err != nil {
		return nil, err
	}
	return &runState{
		eval: &EvalContext{
			Inputs:     e.Inputs,
			Resources:  map[string]any{},
			Data:       map[string]any{},
			Actions:    map[string]any{},
			Libraries:  e.Libraries,
			Assets:     rootAssets,
			AssetCache: e.AssetCache,
			locals:     e.rootLocalScope(),
		},
		composites:       map[string]*EvalContext{},
		forEachInstances: map[string]map[string]any{},
	}, nil
}

func (e *Executor) validateCompositeBoundary(
	rs *runState,
	boundary *Node,
	address string,
) ([]error, error) {
	addresses, err := e.compositeValidationAddresses(rs, boundary, address)
	if errors.Is(err, ErrEvalNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var violations []error
	for _, instance := range addresses {
		scope, err := e.ensureCompositeScope(rs, instance)
		if errors.Is(err, ErrEvalNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, violation := range checkCompositeConstraintValues(boundary, scope).Errors() {
			violations = append(violations, violation)
		}
		for _, child := range e.directCompositeChildren(boundary.Address) {
			childAddress := rewriteAddress(child.Address, boundary.Address, instance)
			found, err := e.validateCompositeBoundary(rs, child, childAddress)
			if err != nil {
				return nil, err
			}
			violations = append(violations, found...)
		}
	}
	return violations, nil
}

func (e *Executor) compositeValidationAddresses(
	rs *runState,
	boundary *Node,
	address string,
) ([]string, error) {
	if boundary.ForEach == nil {
		return []string{address}, nil
	}
	parent, err := e.enclosingScope(rs, address)
	if err != nil {
		return nil, err
	}
	instances, err := forEachInstancesFor(rs, address, boundary.ForEach, parent)
	if err != nil {
		return nil, err
	}
	addresses := make([]string, 0, len(instances))
	for _, key := range sortedKeys(instances) {
		addresses = append(addresses, instanceAddress(address, key))
	}
	return addresses, nil
}

func (e *Executor) directCompositeChildren(parent string) []*Node {
	children := make([]*Node, 0)
	for _, node := range e.DAG.Nodes {
		if !node.IsComposite() || node.Composite != parent {
			continue
		}
		children = append(children, node)
	}
	slices.SortFunc(children, func(a, b *Node) int {
		return cmp.Compare(a.Address, b.Address)
	})
	return children
}
