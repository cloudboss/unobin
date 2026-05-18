package runtime

import (
	"strings"
)

// stepGraph is the apply-time view of step-to-step dependencies. It is
// derived from the plan's step addresses and the executor's DAG edges
// (template-form). Each entry in indegree counts how many predecessors
// have not yet completed. dependents names who depends on this step.
// locks names the `@lock:` value carried by each step (empty for
// steps not under a named lock).
type stepGraph struct {
	indegree   map[string]int
	dependents map[string][]string
	locks      map[string]string
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
	g := buildStepGraphFromAddresses(addresses, dag)
	for _, addr := range addresses {
		if node, ok := dag.Nodes[templateAddress(addr)]; ok && node.LockName != "" {
			g.locks[addr] = node.LockName
		}
	}
	return g
}

// buildStepGraphFromAddresses is the testable entry point that mirrors
// buildStepGraph but takes the bare list of step addresses so a test
// does not need to construct a PlanFile.
func buildStepGraphFromAddresses(addresses []string, dag *DAG) *stepGraph {
	g := &stepGraph{
		indegree:   make(map[string]int, len(addresses)),
		dependents: make(map[string][]string, len(addresses)),
		locks:      map[string]string{},
	}
	for _, a := range addresses {
		g.indegree[a] = 0
	}
	instancesByTemplate := make(map[string][]string, len(addresses))
	for _, a := range addresses {
		t := templateAddress(a)
		instancesByTemplate[t] = append(instancesByTemplate[t], a)
	}
	for _, a := range addresses {
		t := templateAddress(a)
		sPath := keyPath(a)
		for _, depTemplate := range dag.Edges[t] {
			if _, ok := dag.Nodes[depTemplate]; !ok {
				continue
			}
			for _, depInstance := range instancesByTemplate[depTemplate] {
				if depInstance == a {
					continue
				}
				if !keyPathsAgree(sPath, keyPath(depInstance)) {
					continue
				}
				g.dependents[depInstance] = append(g.dependents[depInstance], a)
				g.indegree[a]++
			}
		}
	}
	return g
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
	if addr == "" || !strings.Contains(addr, "['") {
		return nil
	}
	parts := strings.Split(addr, "/")
	tmplParts := make([]string, len(parts))
	for i, p := range parts {
		t, _ := splitInstanceAddress(p)
		tmplParts[i] = t
	}
	var out []keyPosition
	for i, p := range parts {
		_, key := splitInstanceAddress(p)
		if key == "" {
			continue
		}
		prefix := strings.Join(tmplParts[:i+1], "/")
		out = append(out, keyPosition{at: prefix, key: key})
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
