package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

// ApplyPlan executes a previously computed PlanFile against the
// Executor's libraries, store, and parsed source. The DAG passed on the
// Executor is used for output expressions, while resource and action
// bodies come from the plan. The plan's stack identity must match the
// Executor's, and the prior state's rev must match what the plan was
// computed against. The stack's lock is held for the duration.
func (e *Executor) ApplyPlan(ctx context.Context, pf *PlanFile) (*ExecResult, error) {
	if e.Store == nil {
		return nil, errors.New("executor: Store is required")
	}
	if pf.Factory.Name != e.Factory.Name ||
		pf.Factory.Version != e.Factory.Version ||
		pf.Factory.ContentRevision != e.Factory.ContentRevision {
		return nil, fmt.Errorf(
			"plan was computed for %s %s (content-revision %s), this binary is %s %s (content-revision %s)",
			pf.Factory.Name, pf.Factory.Version, pf.Factory.ContentRevision,
			e.Factory.Name, e.Factory.Version, e.Factory.ContentRevision)
	}

	lock, err := e.Store.Lock(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	defer func() { _ = lock.Unlock() }()

	currentRev, _ := e.Store.CurrentRev()
	if currentRev != pf.StateRev {
		return nil, fmt.Errorf("state-rev drift: plan was computed against %q, "+
			"current is %q; must rerun the plan", pf.StateRev, currentRev)
	}

	rs, err := e.initRun()
	if err != nil {
		return nil, err
	}
	e.prepareApplySnapshot(rs)
	// The apply subcommand is invoked with only the plan file, so the
	// executor's own Inputs is typically empty. Seed root Vars from
	// the plan file so root-scope references like `var.X` resolve when
	// applyXxx re-evaluates a node body.
	if len(rs.eval.Vars) == 0 && len(pf.Inputs) > 0 {
		rs.eval.Vars = pf.Inputs
	}
	if err := e.seedPriorInternalConfigurations(rs.prior, rs.eval.Vars); err != nil {
		return nil, err
	}

	// Composite scopes seed from the plan: each composite step carries
	// its evaluated call site args as Inputs, so internals see the
	// right Vars without needing the root inputs again. Libraries comes
	// from the boundary node so functions invoked in the composite's
	// outputs or internals resolve against the composite's own imports.
	// A `@for-each` composite emits one step per instance, each at a
	// `<boundary>['<key>']` address; the cache key is the instance
	// address so distinct instances get distinct scopes.
	for i := range pf.Steps {
		step := &pf.Steps[i]
		if !step.Composite || step.Decision == DecisionDestroy {
			continue
		}
		boundary, ok := e.DAG.Nodes[templateAddress(step.Address)]
		if !ok {
			return nil, fmt.Errorf("composite %q: not in DAG", step.Address)
		}
		rs.composites[step.Address] = &EvalContext{
			Vars:      step.Inputs,
			Resources: make(map[string]any),
			Data:      make(map[string]any),
			Actions:   make(map[string]any),
			Libraries: compositeBodyLibraries(boundary, e.Libraries),
			locals:    newLocalScope(localsBlock(boundary.CompositeBody)),
		}
	}

	if err := e.runApplySchedule(ctx, rs, pf); err != nil {
		return nil, err
	}
	pruneStateEntries(rs.next, pf.Steps)
	// A destroy leaves nothing to read outputs from, so reconciliation
	// and output evaluation are skipped and the snapshot ends with no
	// outputs.
	if !pf.Destroy {
		e.reconcileChangedOutputs(ctx, rs, pf)
		if err := e.evalPlanOutputs(rs); err != nil {
			return nil, err
		}
	}
	rs.next.Outputs = rs.outputs

	rev, err := e.persist(rs)
	if err != nil {
		return nil, err
	}
	return &ExecResult{
		Outputs:    rs.outputs,
		Actions:    rs.eval.Actions,
		Data:       rs.eval.Data,
		WrittenRev: rev,
	}, nil
}

func (e *Executor) applyStep(ctx context.Context, rs *runState, step *PlanStep) error {
	// A node's @timeout bounds how long its step may run. The deadline is
	// read from the DAG node, so an orphan destroy (whose source node is
	// gone) is never bounded. On expiry the operation sees a cancelled
	// context and returns an error, which the scheduler handles like any
	// other step failure.
	if node, ok := e.DAG.Nodes[templateAddress(step.Address)]; ok && node.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, node.Timeout)
		defer cancel()
	}
	if step.Decision == DecisionDestroy {
		// Resources need their Delete called; action and library-call
		// records have no external lifecycle, so they are just removed.
		if step.Kind == NodeResource && !step.Composite {
			return e.applyDestroy(ctx, rs, step)
		}
		return e.removeRecord(rs, step)
	}
	if step.Composite {
		node, ok := e.DAG.Nodes[templateAddress(step.Address)]
		if !ok || !node.IsComposite() {
			return fmt.Errorf("composite: node %q not in DAG", step.Address)
		}
		return e.finalizeComposite(rs, node, step.Address, step.Inputs,
			step.SensitiveInputs, step.SensitiveOutputs)
	}
	switch step.Kind {
	case NodeAction:
		return e.applyAction(ctx, rs, step)
	case NodeResource:
		return e.applyResource(ctx, rs, step)
	case NodeData:
		return e.applyData(ctx, rs, step)
	case NodeOutput:
		return nil
	case NodeConfiguration:
		return e.applyConfiguration(rs, step)
	default:
		return fmt.Errorf("unknown step kind %q", step.Kind)
	}
}

// applyConfiguration evaluates an internal configuration's fields and
// decodes them into the library's configuration struct, making the
// value available to every consumer that selects it. The scheduler
// dispatches it after the nodes its expressions read, so evaluation
// sees settled values.
func (e *Executor) applyConfiguration(rs *runState, step *PlanStep) error {
	node, ok := e.DAG.Nodes[step.Address]
	if !ok {
		return fmt.Errorf("%s: not in the graph", step.Address)
	}
	if e.configurationOverridden(node.Alias, node.Name) {
		return nil
	}
	rs.mu.Lock()
	raw, err := evalConfigurationBody(node.Body, rs.eval)
	rs.mu.Unlock()
	if err != nil {
		return fmt.Errorf("%s: %w", step.Address, err)
	}
	lib, ok := e.librariesFor(node)[node.Alias]
	if !ok || lib.Configuration == nil {
		return fmt.Errorf("%s: library %q declares no configuration",
			step.Address, node.Alias)
	}
	decoded, err := cfg.Decode(lib.Configuration, raw)
	if err != nil {
		return fmt.Errorf("%s: %w", step.Address, err)
	}
	e.storeInternalConfiguration(step.Address, decoded)
	return nil
}

func (e *Executor) applyAction(ctx context.Context, rs *runState, step *PlanStep) error {
	prep, err := e.prepareStep(rs, step.Address)
	if err != nil {
		return err
	}
	at, err := e.actionRegistration(prep.node)
	if err != nil {
		return err
	}
	var outputs map[string]any
	switch step.Decision {
	case DecisionSkip:
		outputs = step.PriorOutputs
	case DecisionRerun:
		receiver := at.NewReceiver()
		if err := Decode(receiver, prep.inputs); err != nil {
			return err
		}
		result, err := at.Run(ctx, receiver, e.configFor(prep.node))
		if err != nil {
			return err
		}
		outputs = mapify(result)
	default:
		return fmt.Errorf("action: unexpected decision %q", step.Decision)
	}

	rs.mu.Lock()
	defer rs.mu.Unlock()
	attrs := mergeAttrs(prep.inputs, outputs)
	if prep.instKey == "" {
		storeNested(prep.parent.Actions, prep.node, attrs)
	} else {
		seedAddressInstance(prep.parent.Actions, prep.node.Address, prep.instKey, attrs)
	}

	// Recompute the trigger hash with the fresh upstream state so the
	// next plan compares against an accurate hash.
	hash := step.TriggerHash
	if t, err := ComputeTrigger(prep.node, prep.inputs, prep.scope); err == nil && !t.AlwaysRerun {
		hash = t.Hash
	}

	upsertEntry(rs.next, &state.Entry{
		Address:          step.Address,
		Type:             state.EntryAction,
		Kind:             string(prep.node.Kind),
		Selector:         selectorForNode(prep.node),
		TriggerHash:      hash,
		Inputs:           prep.inputs,
		Outputs:          outputs,
		SensitiveInputs:  step.SensitiveInputs,
		SensitiveOutputs: step.SensitiveOutputs,
		DependsOn:        rs.dependsOn[step.Address],
	})
	return nil
}

func (e *Executor) applyResource(ctx context.Context, rs *runState, step *PlanStep) error {
	prep, err := e.prepareStep(rs, step.Address)
	if err != nil {
		return err
	}
	// The plan diffed these inputs and showed the result; apply holds
	// the live evaluation to them. A concrete field that re-evaluates
	// differently means the decision was computed from a premise that
	// no longer holds, and the answer is a fresh plan.
	planned := knownFields(step, step.Inputs)
	applied := knownFields(step, prep.inputs)
	if !sameInputs(planned, applied) {
		return fmt.Errorf(
			"resource %s inputs changed since the plan was computed; plan again\n%s",
			step.Address, diffFields(planned, applied, step.SensitiveInputs))
	}
	rt, err := e.resourceRegistration(prep.node)
	if err != nil {
		return err
	}

	receiver := rt.NewReceiver()
	if err := Decode(receiver, prep.inputs); err != nil {
		return err
	}
	var outputs map[string]any
	switch step.Decision {
	case DecisionCreate:
		result, err := rt.Create(ctx, receiver, e.configFor(prep.node))
		if err != nil {
			return err
		}
		outputs = mapify(result)
	case DecisionNoOp:
		outputs = step.PriorOutputs
	case DecisionUpdate:
		result, err := rt.Update(ctx, receiver, e.configFor(prep.node),
			step.PriorInputs, step.PriorOutputs, step.ObservedOutputs)
		if err != nil {
			return err
		}
		outputs = mapify(result)
	case DecisionReplace:
		cfg := e.configFor(prep.node)
		deleteRT := rt
		deleteReceiver := receiver
		deleteCfg := cfg
		if step.PriorSelector != nil && !sameSelector(step.PriorSelector, selectorForNode(prep.node)) {
			priorRT, priorAlias, err := e.resourceRegistrationForSelector(
				step.Address, step.PriorSelector,
			)
			if err != nil {
				return err
			}
			deleteRT = priorRT
			deleteReceiver = priorRT.NewReceiver()
			if err := Decode(deleteReceiver, step.PriorInputs); err != nil {
				return fmt.Errorf("replace: decode prior: %w", err)
			}
			priorCfg, err := e.configForRef(step.Configuration, priorAlias)
			if err != nil {
				return err
			}
			deleteCfg = priorCfg
		}
		if err := deleteRT.Delete(ctx, deleteReceiver, deleteCfg, step.PriorOutputs); err != nil {
			return fmt.Errorf("replace: delete prior: %w", err)
		}
		result, err := rt.Create(ctx, receiver, cfg)
		if err != nil {
			return fmt.Errorf("replace: create: %w", err)
		}
		outputs = mapify(result)
	default:
		return fmt.Errorf("resource: unexpected decision %q", step.Decision)
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	attrs := mergeAttrs(prep.inputs, outputs)
	if prep.instKey == "" {
		storeNested(prep.parent.Resources, prep.node, attrs)
	} else {
		seedAddressInstance(prep.parent.Resources, prep.node.Address, prep.instKey, attrs)
	}
	upsertEntry(rs.next, &state.Entry{
		Address:          step.Address,
		Type:             state.EntryLeaf,
		Kind:             string(prep.node.Kind),
		Selector:         selectorForNode(prep.node),
		SchemaVersion:    rt.SchemaVersion(),
		Configuration:    e.configRefString(prep.node),
		Inputs:           prep.inputs,
		Outputs:          outputs,
		SensitiveInputs:  step.SensitiveInputs,
		SensitiveOutputs: step.SensitiveOutputs,
		DependsOn:        rs.dependsOn[step.Address],
	})
	switch step.Decision {
	case DecisionCreate, DecisionUpdate, DecisionReplace:
		_, err := e.persist(rs)
		return err
	}
	return nil
}

// instanceScope returns the scope a step body should be evaluated
// against. For a non-for-each step it returns parent unchanged. For a
// for-each instance it evaluates the iterable (shared across the
// node's instances via rs), looks up the bound value for instKey, and
// returns a child scope with `@each.key` and `@each.value` set.
func instanceScope(
	rs *runState, node *Node, parent *EvalContext, instKey string,
) (*EvalContext, error) {
	if instKey == "" {
		return parent, nil
	}
	instances, err := forEachInstancesFor(rs, node.Address, node.ForEach, parent)
	if err != nil {
		return nil, err
	}
	value, ok := instances[instKey]
	if !ok {
		return nil, fmt.Errorf("@for-each instance %q no longer in iterable", instKey)
	}
	return childScopeWithEach(parent, instKey, value), nil
}

// applyDestroy deletes a resource and drops it from state. The selector
// identifies the library, so this works whether the resource was
// orphaned (removed from source) or is part of a full teardown.
func (e *Executor) applyDestroy(ctx context.Context, rs *runState, step *PlanStep) error {
	// The plan read this resource and found it already absent, so there
	// is nothing to delete; just drop it from state.
	if step.AlreadyGone {
		return e.removeRecord(rs, step)
	}
	alias, typeName, ok := stepSelectorParts(step)
	if !ok {
		return fmt.Errorf("destroy: missing selector for %q", step.Address)
	}
	lib, ok := e.librariesForAddress(step.Address)[alias]
	if !ok {
		return fmt.Errorf("library %q is not imported", alias)
	}
	rt, ok := lib.Resources[typeName]
	if !ok {
		return fmt.Errorf("library %s has no resource %q", alias, typeName)
	}
	receiver := rt.NewReceiver()
	if err := Decode(receiver, step.Inputs); err != nil {
		return err
	}
	cfg, err := e.configForRef(step.Configuration, alias)
	if err != nil {
		return err
	}
	if err := rt.Delete(ctx, receiver, cfg, step.PriorOutputs); err != nil {
		return err
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	removeEntry(rs.next, step.Address)
	_, err = e.persist(rs)
	return err
}

// removeRecord drops a state entry that has no external lifecycle - an
// action record, a composite library-call record, or a resource the
// plan already found absent - during a destroy. There is nothing to
// delete; the record is simply removed and the new snapshot written.
func (e *Executor) removeRecord(rs *runState, step *PlanStep) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	removeEntry(rs.next, step.Address)
	_, err := e.persist(rs)
	return err
}

// stepPrep is the slice of per-step state that prepareStep evaluates
// under the run state's mutex. Each field is detached from any locked
// map: callers may pass inputs to a CRUD method without holding the
// lock, and may use parent / scope to write outputs back after retaking
// the lock.
type stepPrep struct {
	node    *Node
	parent  *EvalContext
	scope   *EvalContext
	instKey string
	inputs  map[string]any
}

// prepareStep takes rs.mu, resolves the step's DAG node and its scope,
// evaluates the body against that scope, and releases the lock. The
// returned stepPrep gives the caller everything needed to run CRUD
// without holding the lock; the caller retakes rs.mu before writing
// outputs back into scope.
func (e *Executor) prepareStep(rs *runState, addr string) (*stepPrep, error) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	tmpl, instKey := splitInstanceAddress(addr)
	node, parent, err := e.nodeAndScope(rs, tmpl)
	if err != nil {
		return nil, err
	}
	scope, err := instanceScope(rs, node, parent, instKey)
	if err != nil {
		return nil, err
	}
	inputs, err := evalBody(node.Body, scope)
	if err != nil {
		return nil, err
	}
	if err := e.applyInputDefaults(node, inputs, nil); err != nil {
		return nil, err
	}
	return &stepPrep{
		node:    node,
		parent:  parent,
		scope:   scope,
		instKey: instKey,
		inputs:  inputs,
	}, nil
}

// nodeAndScope resolves a per-instance step address to its DAG
// template node and the scope its body should be evaluated against.
// Any `['key']` segments in addr are stripped to find the node;
// segments before the last `/` survive into the parent address so
// composite-internal nodes pick the right per-instance scope.
func (e *Executor) nodeAndScope(rs *runState, addr string) (*Node, *EvalContext, error) {
	node, ok := e.DAG.Nodes[templateAddress(addr)]
	if !ok {
		return nil, nil, fmt.Errorf("address %q not in DAG", addr)
	}
	scope, err := e.enclosingScope(rs, addr)
	if err != nil {
		return nil, nil, err
	}
	return node, scope, nil
}

func (e *Executor) applyData(ctx context.Context, rs *runState, step *PlanStep) error {
	prep, err := e.prepareStep(rs, step.Address)
	if err != nil {
		return err
	}
	dt, err := e.dataRegistration(prep.node)
	if err != nil {
		return err
	}
	receiver := dt.NewReceiver()
	if err := Decode(receiver, prep.inputs); err != nil {
		return err
	}
	result, err := dt.Read(ctx, receiver, e.configFor(prep.node))
	if err != nil {
		return err
	}
	outputs := mapify(result)
	// The plan read this data source and showed those values; apply
	// holds the world to them. A value that moved in between means the
	// plan's premises no longer hold, so the answer is a fresh plan,
	// not a quiet apply of something never shown.
	if step.ObservedOutputs != nil && !sameInputs(step.ObservedOutputs, outputs) {
		return fmt.Errorf("data source %s changed since the plan was computed; plan again\n%s",
			step.Address,
			diffFields(step.ObservedOutputs, outputs, step.SensitiveOutputs))
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	attrs := mergeAttrs(prep.inputs, outputs)
	if prep.instKey == "" {
		storeNested(prep.parent.Data, prep.node, attrs)
	} else {
		seedAddressInstance(prep.parent.Data, prep.node.Address, prep.instKey, attrs)
	}
	upsertEntry(rs.next, &state.Entry{
		Address:          step.Address,
		Type:             state.EntryData,
		Kind:             string(prep.node.Kind),
		Selector:         selectorForNode(prep.node),
		Inputs:           prep.inputs,
		Outputs:          outputs,
		SensitiveInputs:  step.SensitiveInputs,
		SensitiveOutputs: step.SensitiveOutputs,
		DependsOn:        rs.dependsOn[step.Address],
	})
	return nil
}

// diffFields renders the fields whose value differs between two maps,
// one per line as `name: planned -> applied`, with sensitive fields
// masked and long values shortened. Field names are sorted so the
// report reads stably.
func diffFields(planned, applied map[string]any, sensitive []string) string {
	names := map[string]bool{}
	for k := range planned {
		names[k] = true
	}
	for k := range applied {
		names[k] = true
	}
	masked := map[string]bool{}
	for _, k := range sensitive {
		masked[k] = true
	}
	var lines []string
	for _, k := range slices.Sorted(maps.Keys(names)) {
		if sameValue(planned[k], applied[k]) {
			continue
		}
		if masked[k] {
			lines = append(lines, fmt.Sprintf("  %s: (sensitive)", k))
			continue
		}
		lines = append(lines, fmt.Sprintf("  %s: %s -> %s",
			k, shortValue(planned[k]), shortValue(applied[k])))
	}
	return strings.Join(lines, "\n")
}

func shortValue(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	const max = 120
	if len(b) > max {
		return string(b[:max]) + "..."
	}
	return string(b)
}

// evalPlanOutputs evaluates each output node from the source against the
// runtime context built up while applying the plan.
func (e *Executor) evalPlanOutputs(rs *runState) error {
	for _, n := range e.DAG.Nodes {
		if n.Kind != NodeOutput {
			continue
		}
		val, err := Eval(n.Body, rs.eval)
		if err != nil {
			return fmt.Errorf("%s: %w", n.Address, err)
		}
		rs.outputs[n.Name] = val
	}
	return nil
}
