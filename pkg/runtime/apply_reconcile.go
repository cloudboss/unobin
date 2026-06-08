package runtime

import (
	"context"
	"sync"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

// reconcileChangedOutputs re-reads the resources this apply may have left
// stale and writes their settled outputs back to state and the eval
// scope. A resource's computed outputs are read at its own create or
// update time, so a sibling that mutates it later in the same apply -- a
// NAT gateway associating an Elastic IP, a listener filling a target
// group's load-balancer ARNs -- leaves the snapshot describing the
// resource as it stood before that side effect. The resources at risk
// are the dependencies of the steps that ran: a step can mutate only what
// it references, so its dependencies are the ones to re-read; the step's
// own outputs came back fresh from its create or update. Reads run after
// every step, so one pass settles them. Reconciliation only sharpens the
// snapshot: a read error, or a resource the cloud still reports absent
// right after creating it, leaves the applied outputs in place for the
// next plan to read again, and never fails an apply whose own work has
// already succeeded.
func (e *Executor) reconcileChangedOutputs(ctx context.Context, rs *runState, pf *PlanFile) {
	addrs := reconcileTargets(pf, rs.dependsOn)
	entries := make([]*state.Entry, 0, len(addrs))
	for _, addr := range addrs {
		if ent := rs.next.Find(addr); ent != nil {
			entries = append(entries, ent)
		}
	}
	if len(entries) == 0 {
		return
	}
	type reread struct {
		ent  *state.Entry
		gone bool
		err  error
	}
	results := make([]reread, len(entries))
	sem := make(chan struct{}, e.effectiveParallelism())
	var wg sync.WaitGroup
	for i, ent := range entries {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, ent *state.Entry) {
			defer func() { <-sem; wg.Done() }()
			var gone bool
			updated, err := guard("reconciling this resource", true, func() (*state.Entry, error) {
				u, g, rerr := e.refreshLeaf(ctx, ent)
				gone = g
				return u, rerr
			})
			results[i] = reread{ent: updated, gone: gone, err: err}
		}(i, ent)
	}
	wg.Wait()
	for _, r := range results {
		if r.err != nil || r.gone {
			continue
		}
		upsertEntry(rs.next, r.ent)
		e.seedReconciled(rs, r.ent)
	}
}

// reconcileTargets returns the addresses of the resources whose outputs
// this apply may have left stale: the persisted resource dependencies of
// every step that created, updated, replaced, or reran a node. Actions
// and composite call sites among those dependencies are left out, since
// only a primitive resource is re-read by address. The result holds one
// address per resource in step-then-dependency order.
func reconcileTargets(pf *PlanFile, dependsOn map[string][]string) []string {
	byAddr := make(map[string]*PlanStep, len(pf.Steps))
	for i := range pf.Steps {
		byAddr[pf.Steps[i].Address] = &pf.Steps[i]
	}
	seen := map[string]bool{}
	var out []string
	for i := range pf.Steps {
		if !stepMutates(&pf.Steps[i]) {
			continue
		}
		for _, dep := range dependsOn[pf.Steps[i].Address] {
			if seen[dep] {
				continue
			}
			d, ok := byAddr[dep]
			if !ok || !reconcilableLeaf(d) {
				continue
			}
			seen[dep] = true
			out = append(out, dep)
		}
	}
	return out
}

// stepMutates reports whether a step's apply could change another
// resource as a side effect. A create, update, replace, or action rerun
// can; a no-op, skip, plain read, or destroy cannot.
func stepMutates(s *PlanStep) bool {
	switch s.Decision {
	case DecisionCreate, DecisionUpdate, DecisionReplace, DecisionRerun:
		return true
	default:
		return false
	}
}

// reconcilableLeaf reports whether a step names a primitive resource
// entry that can be re-read by address: a resource, not a composite call
// site, and not being destroyed.
func reconcilableLeaf(s *PlanStep) bool {
	return s.Kind == NodeResource && !s.Composite && s.Decision != DecisionDestroy
}

// seedReconciled overwrites a reconciled resource's attributes in the
// eval scope, so a stack output evaluated after the reconcile reads its
// settled value rather than the one captured when the resource applied.
// A scope that can no longer be built (a composite instance no longer in
// its iterable) is left as is; the state entry already holds the settled
// outputs.
func (e *Executor) seedReconciled(rs *runState, ent *state.Entry) {
	parent, err := e.enclosingScope(rs, ent.Address)
	if err != nil {
		return
	}
	_, alias, typeName, name, ok := parseAddress(ent.Address)
	if !ok {
		return
	}
	attrs := mergeAttrs(ent.Inputs, ent.Outputs)
	if _, instKey := splitInstanceAddress(ent.Address); instKey != "" {
		seedInstance(parent.Resources, alias, typeName, name, instKey, attrs)
		return
	}
	aliasMap := getOrCreate(parent.Resources, alias)
	typeMap := getOrCreate(aliasMap, typeName)
	typeMap[name] = attrs
}
