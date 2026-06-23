package runtime

import (
	"maps"
	"slices"

	"github.com/cloudboss/unobin/pkg/stateref"
)

// stepGraph is the apply-time view of step-to-step dependencies. It is
// derived from the plan's step addresses and the executor's DAG edges
// (template-form). Each entry in indegree counts how many predecessors
// have not yet completed. dependents names who depends on this step.
// locks names the `@lock:` value carried by each step (empty for
// steps not under a named lock). pairKey records the dep templates a
// step's body references with an `[@each.key]` selector, which lets
// the builder narrow the cartesian fan-out down to same-key pairs.
type stepGraph struct {
	indegree   map[string]int
	dependents map[string][]string
	locks      map[string]string
	pairKey    map[string]map[string]bool
}

// buildStepGraph translates the template-form DAG edges into instance-
// form step edges. For every step S at address `addr`:
//
//   - Each template-form predecessor T_dep of templateAddress(addr)
//     contributes one edge per instance step of T_dep whose `['key']`
//     positions agree with S's key positions at every shared template
//     ancestor. This keeps `@for-each` composite siblings on the same
//     instance from cross-linking with other instances' internals.
//
//   - Steps whose template address is not a DAG node (orphan destroy
//     entries from prior state, NodeOutput placeholders that may have
//     been pruned) get no predecessors and become roots.
func buildStepGraph(pf *PlanFile, dag *DAG) *stepGraph {
	addresses := make([]string, len(pf.Steps))
	for i := range pf.Steps {
		addresses[i] = pf.Steps[i].Address
	}
	pairKey := map[string]map[string]bool{}
	for _, addr := range addresses {
		node, ok := dag.Nodes[templateAddress(addr)]
		if !ok {
			continue
		}
		if pk := pairKeyDeps(node.Body, dag.Nodes, node.Composite); pk != nil {
			pairKey[addr] = pk
		}
	}
	destroying := make(map[string]bool, len(pf.Steps))
	for i := range pf.Steps {
		if pf.Steps[i].Decision == DecisionDestroy {
			destroying[pf.Steps[i].Address] = true
		}
	}
	g := buildStepGraphWithPairKey(addresses, dag, pairKey, destroying)
	for _, addr := range addresses {
		node, ok := dag.Nodes[templateAddress(addr)]
		if !ok {
			continue
		}
		if node.LockName != "" {
			g.locks[addr] = node.LockName
		}
	}
	addDestroyEdges(g, pf.Steps)
	return g
}

// addDestroyEdges reverses the dependency edges between destroy steps
// so a resource is deleted before the resources it depended on. A
// step's recorded DependsOn names the entries it was created after; for
// destroy that order flips, so each dependency that is also being
// destroyed waits for the dependent to finish. Dependencies that are
// not being destroyed in this plan add no edge.
func addDestroyEdges(g *stepGraph, steps []PlanStep) {
	destroying := make(map[string]bool, len(steps))
	for i := range steps {
		if steps[i].Decision == DecisionDestroy {
			destroying[steps[i].Address] = true
		}
	}
	for i := range steps {
		s := &steps[i]
		if s.Decision != DecisionDestroy {
			continue
		}
		for _, dep := range s.DependsOn {
			if !destroying[dep] {
				continue
			}
			g.dependents[s.Address] = append(g.dependents[s.Address], dep)
			g.indegree[dep]++
		}
	}
}

// buildStepGraphFromAddresses is the testable entry point that mirrors
// buildStepGraph but takes the bare list of step addresses so a test
// does not need to construct a PlanFile. Pair-key narrowing is not
// applied here; callers that have a pairKey map use the internal
// buildStepGraphWithPairKey.
func buildStepGraphFromAddresses(addresses []string, dag *DAG) *stepGraph {
	return buildStepGraphWithPairKey(addresses, dag, nil, nil)
}

// buildStepGraphWithPairKey builds the instance-form forward edges. A
// step in destroying gets no forward edges: a destroy step's ordering
// comes only from addDestroyEdges, which reverses the recorded
// dependencies. Without this a destroy whose source is still present
// would pick up both the forward edge and its reverse and deadlock.
func buildStepGraphWithPairKey(
	addresses []string, dag *DAG, pairKey map[string]map[string]bool,
	destroying map[string]bool,
) *stepGraph {
	g := &stepGraph{
		indegree:   make(map[string]int, len(addresses)),
		dependents: make(map[string][]string, len(addresses)),
		locks:      map[string]string{},
		pairKey:    map[string]map[string]bool{},
	}
	maps.Copy(g.pairKey, pairKey)
	for _, a := range addresses {
		g.indegree[a] = 0
	}
	instancesByTemplate := make(map[string][]string, len(addresses))
	for _, a := range addresses {
		t := templateAddress(a)
		instancesByTemplate[t] = append(instancesByTemplate[t], a)
	}
	for _, a := range addresses {
		if destroying[a] {
			continue
		}
		t := templateAddress(a)
		sPath := keyPath(a)
		stepPairs := g.pairKey[a]
		for _, depTemplate := range dag.Edges[t] {
			if _, ok := dag.Nodes[depTemplate]; !ok {
				continue
			}
			narrow := stepPairs[depTemplate] && len(sPath) == 1
			for _, depInstance := range instancesByTemplate[depTemplate] {
				if depInstance == a {
					continue
				}
				if !keyPathsAgree(sPath, keyPath(depInstance)) {
					continue
				}
				if narrow && !pairKeyMatches(sPath, keyPath(depInstance)) {
					continue
				}
				g.dependents[depInstance] = append(g.dependents[depInstance], a)
				g.indegree[a]++
			}
		}
	}
	return g
}

// entryPersisted reports whether a step's entry is a destroy-ordering
// target: resources (unless being destroyed), actions, and composite
// call sites. Data reads, output values, and configuration
// evaluations are not ordering targets, so dependencies through them
// collapse to their persisted predecessors.
func entryPersisted(s *PlanStep) bool {
	if s.Composite {
		return true
	}
	switch s.Kind {
	case NodeAction:
		return true
	case NodeResource:
		return s.Decision != DecisionDestroy
	default:
		return false
	}
}

// persistedDependsOn computes, for each plan step that becomes a state
// entry, the addresses of the other entries it depends on. The step
// graph's edges run from a dependency to its dependents, so they are
// inverted here. A predecessor that is not itself persisted (a
// configuration evaluation, data read, or output) is collapsed through
// to its own persisted predecessors, so every recorded address names
// an entry destroy can sequence against. Destroy ordering reverses
// these edges.
func persistedDependsOn(g *stepGraph, steps []PlanStep) map[string][]string {
	persisted := make(map[string]bool, len(steps))
	for i := range steps {
		if entryPersisted(&steps[i]) {
			persisted[steps[i].Address] = true
		}
	}
	preds := make(map[string][]string, len(steps))
	for dep, dependents := range g.dependents {
		for _, d := range dependents {
			preds[d] = append(preds[d], dep)
		}
	}
	out := make(map[string][]string, len(persisted))
	for addr := range persisted {
		seen := map[string]bool{}
		var collapsed []string
		stack := append([]string(nil), preds[addr]...)
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[p] {
				continue
			}
			seen[p] = true
			if persisted[p] {
				collapsed = append(collapsed, p)
				continue
			}
			stack = append(stack, preds[p]...)
		}
		if len(collapsed) > 0 {
			slices.Sort(collapsed)
			out[addr] = collapsed
		}
	}
	return out
}

// pairKeyMatches reports whether step s has at least one key segment
// in common with the dep instance d. The narrowing only kicks in when
// the body referenced the dep with `[@each.key]`, which means the
// caller wants the dep instance whose key equals the step's own key.
// We accept any positional match: if any of s's keys appears among
// d's keys, treat it as the pair. Stricter "same position" checks
// would require knowing which for-each level the source @each.key
// bound to, which the address form does not record.
func pairKeyMatches(s, d []keyPosition) bool {
	if len(s) == 0 || len(d) == 0 {
		return false
	}
	for _, sp := range s {
		for _, dp := range d {
			if sp.key == dp.key {
				return true
			}
		}
	}
	return false
}

// keyPosition pairs a template-form address prefix with the instance
// key bound at that position. The prefix names where in an address the
// key segment lives, so two addresses with overlapping prefixes can be
// compared for instance-key agreement.
type keyPosition struct {
	at, key string
}

// keyPath extracts the (template-prefix, key) positions from addr in
// outer-to-inner order. An address with no `['key']` segments returns
// nil. The template prefix at each position is the address rebuilt
// using template-form for every prior segment plus the current
// segment's template form.
func keyPath(addr string) []keyPosition {
	ref, err := stateref.ParseStateRef(addr)
	if err != nil {
		return nil
	}
	tmpl := make([]stateref.StateAddressSegment, 0, len(ref.Segments))
	var out []keyPosition
	for _, segment := range ref.Segments {
		key := segment.Key
		segment.Key = nil
		tmpl = append(tmpl, segment)
		if key == nil {
			continue
		}
		prefix := stateref.StateRef{Segments: tmpl}.String()
		out = append(out, keyPosition{at: prefix, key: key.Value})
	}
	return out
}

// keyPathsAgree reports whether two key paths can describe instances
// sharing the same for-each composite ancestor. At every prefix that
// appears in both paths, the keys must match. Prefixes that appear in
// only one path are not a constraint, since one address has a key at
// a position the other does not name.
func keyPathsAgree(a, b []keyPosition) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	bAt := make(map[string]string, len(b))
	for _, kp := range b {
		bAt[kp.at] = kp.key
	}
	for _, kp := range a {
		if bKey, ok := bAt[kp.at]; ok && bKey != kp.key {
			return false
		}
	}
	return true
}
