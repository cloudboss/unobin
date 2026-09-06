package runtime

import (
	"maps"

	"github.com/cloudboss/unobin/pkg/stateref"
)

// stepGraph is the apply-time view of step-to-step dependencies. It is
// derived from the plan's step addresses and the executor's DAG edges
// (template-form). Each entry in indegree counts how many predecessors
// have not yet completed. dependents names who depends on this step.
// locks names the `@lock:` value for each step (empty for steps not
// under a named lock). pairKey records the dep templates a
// step's body references with an `[@each.key]` index segment, which
// lets the builder narrow the cartesian fan-out down to same-key pairs.
type stepGraph struct {
	indegree   map[string]int
	dependents map[string][]string
	locks      map[string]string
	pairKey    map[string]map[string]bool
}

// buildStepGraphFromAddresses expands dependencies without narrowing
// references that select a matching instance key.
func buildStepGraphFromAddresses(addresses []string, dag *DAG) *stepGraph {
	return buildStepGraphWithPairKey(addresses, dag, nil, nil)
}

// buildStepGraphWithPairKey builds the instance-form forward edges. A
// step in destroying gets no forward edges because deletion reverses
// the recorded dependencies.
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
