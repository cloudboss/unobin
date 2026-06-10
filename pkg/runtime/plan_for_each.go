package runtime

import (
	"context"
	"fmt"
	"strings"
)

// insideForEachComposite reports whether any composite call site in
// n's ancestry is itself a `@for-each` template. Such nodes are
// planned per-instance by their boundary's planner, not on their own.
func (e *Executor) insideForEachComposite(n *Node) bool {
	return underForEachComposite(e.DAG.Nodes, n)
}

// planForEachAction plans one action step per iterable key. Mirrors
// planForEachResource: each instance evaluates against a child scope
// with `@each.key` / `@each.value` bound, and its state address gets
// a `['<key>']` suffix.
func (e *Executor) planForEachAction(rs *runState, n *Node) ([]*PlanStep, error) {
	lib, ok := e.librariesFor(n)[n.Alias]
	if !ok {
		return nil, fmt.Errorf("library %q is not imported", n.Alias)
	}
	if _, ok := lib.Actions[n.Type]; !ok {
		return nil, fmt.Errorf("library %s has no action %q", n.Alias, n.Type)
	}
	scope, err := e.scopeFor(rs, n)
	if err != nil {
		return nil, err
	}
	instances, err := forEachInstancesFor(rs, n.Address, n.ForEach, scope)
	if err != nil {
		return nil, err
	}
	var steps []*PlanStep
	for _, key := range sortedKeys(instances) {
		inst := childScopeWithEach(scope, key, instances[key])
		addr := instanceAddress(n.Address, key)
		step, err := e.planOneAction(rs, n, inst, addr)
		if err != nil {
			return nil, fmt.Errorf("@for-each[%q]: %w", key, err)
		}
		steps = append(steps, step)
	}
	return steps, nil
}

// planForEachData plans one data source step per iterable key.
func (e *Executor) planForEachData(
	ctx context.Context, rs *runState, n *Node,
) ([]*PlanStep, error) {
	lib, ok := e.librariesFor(n)[n.Alias]
	if !ok {
		return nil, fmt.Errorf("library %q is not imported", n.Alias)
	}
	if _, ok := lib.DataSources[n.Type]; !ok {
		return nil, fmt.Errorf("library %s has no data source %q", n.Alias, n.Type)
	}
	scope, err := e.scopeFor(rs, n)
	if err != nil {
		return nil, err
	}
	instances, err := forEachInstancesFor(rs, n.Address, n.ForEach, scope)
	if err != nil {
		return nil, err
	}
	var steps []*PlanStep
	for _, key := range sortedKeys(instances) {
		inst := childScopeWithEach(scope, key, instances[key])
		addr := instanceAddress(n.Address, key)
		step, err := e.planOneData(ctx, rs, n, inst, addr)
		if err != nil {
			return nil, fmt.Errorf("@for-each[%q]: %w", key, err)
		}
		steps = append(steps, step)
	}
	return steps, nil
}

// planForEachComposite expands a `@for-each` composite call site into
// one full subtree per iterable key. For each key it ensures the
// per-instance composite scope (whose Vars are the args evaluated
// with `@each` bound) is built, then plans every template-internal
// node of the boundary under a per-instance address, finishing with
// the boundary's own per-instance step.
//
// The plan-step order within an instance mirrors topological order:
// internals first, boundary last, so subsequent apply lookups find
// the per-instance scope populated by the time the boundary
// finalizes its outputs.
func (e *Executor) planForEachComposite(
	ctx context.Context, rs *runState, boundary *Node,
) ([]*PlanStep, error) {
	parent, err := e.scopeFor(rs, boundary)
	if err != nil {
		return nil, err
	}
	instances, err := forEachInstancesFor(rs, boundary.Address, boundary.ForEach, parent)
	if err != nil {
		return nil, err
	}
	internals := e.compositeInternalsInOrder(rs, boundary.Address)
	var steps []*PlanStep
	for _, key := range sortedKeys(instances) {
		instAddr := instanceAddress(boundary.Address, key)
		if _, err := e.ensureCompositeScope(rs, instAddr); err != nil {
			return nil, fmt.Errorf("@for-each[%q]: %w", key, err)
		}
		for _, internal := range internals {
			rewritten := rewriteAddress(internal.Address, boundary.Address, instAddr)
			subSteps, err := e.planInternalUnder(ctx, rs, internal, rewritten, instAddr)
			if err != nil {
				return nil, fmt.Errorf("@for-each[%q]: %w", key, err)
			}
			steps = append(steps, subSteps...)
		}
		scope, _ := e.ensureCompositeScope(rs, instAddr)
		var priorOut map[string]any
		if rs.prior != nil {
			if prior := rs.prior.Find(instAddr); prior != nil {
				priorOut = prior.Outputs
			}
		}
		steps = append(steps, &PlanStep{
			Address:      instAddr,
			Kind:         boundary.Kind,
			Composite:    true,
			Decision:     DecisionEval,
			Inputs:       scope.Vars,
			PriorOutputs: priorOut,
		})
	}
	return steps, nil
}

// compositeInternalsInOrder returns every DAG node whose Composite
// chain transitively contains the named boundary, in the run's
// topological order. Nested composites are included.
func (e *Executor) compositeInternalsInOrder(rs *runState, boundary string) []*Node {
	included := map[string]bool{}
	for _, addr := range rs.order {
		n := e.DAG.Nodes[addr]
		if n == nil {
			continue
		}
		cur := n.Composite
		for cur != "" {
			if cur == boundary {
				included[addr] = true
				break
			}
			b, ok := e.DAG.Nodes[cur]
			if !ok {
				break
			}
			cur = b.Composite
		}
	}
	out := make([]*Node, 0, len(included))
	for _, addr := range rs.order {
		if included[addr] {
			out = append(out, e.DAG.Nodes[addr])
		}
	}
	return out
}

// rewriteAddress substitutes the for-each boundary's template address
// with its per-instance form everywhere it appears as a prefix in
// addr. The boundary itself reduces to instAddr; an internal at
// `<boundary>/<inner>` becomes `<instAddr>/<inner>`.
func rewriteAddress(addr, boundary, instAddr string) string {
	if addr == boundary {
		return instAddr
	}
	prefix := boundary + "/"
	if strings.HasPrefix(addr, prefix) {
		return instAddr + "/" + addr[len(prefix):]
	}
	return addr
}

// planInternalUnder plans an internal node of a `@for-each`
// composite at its per-instance address. A leaf's scope comes from
// the cached per-instance composite scope (built lazily via
// scopeForAddress when its body is evaluated); a nested boundary
// builds its own scope, whose Vars are its call args evaluated
// against the enclosing instance.
func (e *Executor) planInternalUnder(
	ctx context.Context, rs *runState, n *Node, addr, instCallSite string,
) ([]*PlanStep, error) {
	if n.IsComposite() {
		own, err := e.ensureCompositeScope(rs, addr)
		if err != nil {
			return nil, err
		}
		var priorOut map[string]any
		if rs.prior != nil {
			if prior := rs.prior.Find(addr); prior != nil {
				priorOut = prior.Outputs
			}
		}
		return []*PlanStep{{
			Address:      addr,
			Kind:         n.Kind,
			Composite:    true,
			Decision:     DecisionEval,
			Inputs:       own.Vars,
			PriorOutputs: priorOut,
		}}, nil
	}
	scope, err := e.scopeForAddress(rs, addr)
	if err != nil {
		return nil, err
	}
	if scope == nil {
		return nil, fmt.Errorf("internal %q: no scope", addr)
	}
	switch n.Kind {
	case NodeResource:
		lib, ok := e.librariesFor(n)[n.Alias]
		if !ok {
			return nil, fmt.Errorf("library %q is not imported", n.Alias)
		}
		rt, ok := lib.Resources[n.Type]
		if !ok {
			return nil, fmt.Errorf("library %s has no resource %q", n.Alias, n.Type)
		}
		step, err := e.planOneResource(rs, n, rt, scope, addr)
		if err != nil {
			return nil, err
		}
		return []*PlanStep{step}, nil
	case NodeAction:
		step, err := e.planOneAction(rs, n, scope, addr)
		if err != nil {
			return nil, err
		}
		return []*PlanStep{step}, nil
	case NodeData:
		step, err := e.planOneData(ctx, rs, n, scope, addr)
		if err != nil {
			return nil, err
		}
		return []*PlanStep{step}, nil
	}
	return nil, fmt.Errorf("internal %q: unsupported kind %s", addr, n.Kind)
}

// planForEachResource plans one step per iterable key. The iterable is
// evaluated against the node's natural scope; each instance is planned
// against a child scope carrying its `@each.key` / `@each.value`
// binding, with its own state address.
func (e *Executor) planForEachResource(rs *runState, n *Node) ([]*PlanStep, error) {
	lib, ok := e.librariesFor(n)[n.Alias]
	if !ok {
		return nil, fmt.Errorf("library %q is not imported", n.Alias)
	}
	rt, ok := lib.Resources[n.Type]
	if !ok {
		return nil, fmt.Errorf("library %s has no resource %q", n.Alias, n.Type)
	}
	scope, err := e.scopeFor(rs, n)
	if err != nil {
		return nil, err
	}
	instances, err := forEachInstancesFor(rs, n.Address, n.ForEach, scope)
	if err != nil {
		return nil, err
	}
	var steps []*PlanStep
	for _, key := range sortedKeys(instances) {
		inst := childScopeWithEach(scope, key, instances[key])
		addr := instanceAddress(n.Address, key)
		step, err := e.planOneResource(rs, n, rt, inst, addr)
		if err != nil {
			return nil, fmt.Errorf("@for-each[%q]: %w", key, err)
		}
		steps = append(steps, step)
	}
	return steps, nil
}
