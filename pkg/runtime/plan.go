package runtime

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

// PendingValue marks a position in a plan step's recorded inputs where a
// forward reference kept the value from resolving at plan time. It holds the
// upstream source addresses the expression reads, so the plan renderer shows
// `<resource.X.field>` at the value's real position, including inside a list
// or object, rather than collapsing the whole field to one marker. It is
// recorded for display only: withoutPending resets every unresolved field for
// the plan walk's decisions, and knownFields removes it before seeding or the
// apply premise check, so a PendingValue never reaches a resource.
type PendingValue struct {
	Refs []string
}

// planEvalBody evaluates a body field by field against the plan-time scope. A
// field that resolves cleanly contributes its evaluated value to inputs. A
// field whose evaluation hits ErrEvalNotFound (because an upstream resource,
// action, or data source has not run yet) keeps its partial structure with a
// PendingValue at each unresolved position, and its referenced source
// addresses are recorded in unresolved. Apply re-evaluates the body against
// the live scope and returns a real error if a reference is genuinely invalid.
func planEvalBody(body lang.Expr, ec *EvalContext) (map[string]any, map[string][]string, error) {
	obj, ok := body.(*lang.ObjectLit)
	if !ok {
		return nil, nil, fmt.Errorf("body must be an object literal")
	}
	inputs := make(map[string]any, len(obj.Fields))
	var unresolved map[string][]string
	var locals map[string]lang.Expr
	if ec != nil && ec.locals != nil {
		locals = ec.locals.exprs
	}
	for _, fld := range obj.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		val, err := Eval(fld.Value, ec)
		if err == nil {
			inputs[fld.Key.Name] = val
			continue
		}
		if !errors.Is(err, ErrEvalNotFound) {
			return nil, nil, fmt.Errorf("field %q: %w", fld.Key.Name, err)
		}
		refs := deferredRefs(fld.Value, locals)
		if len(refs) == 0 {
			inputs[fld.Key.Name] = nil
			continue
		}
		inputs[fld.Key.Name] = partialValue(fld.Value, ec, locals)
		if unresolved == nil {
			unresolved = map[string][]string{}
		}
		unresolved[fld.Key.Name] = refs
	}
	return inputs, unresolved, nil
}

// partialValue rebuilds a body field whose full evaluation hit a forward
// reference, descending array and object literals so a resolved element keeps
// its value and an unresolved one becomes a PendingValue at its real position.
// A non-literal expression that cannot resolve becomes a single PendingValue
// holding the addresses it reads. The result is recorded for display only;
// withoutPending discards the whole field for every plan-walk decision.
func partialValue(e lang.Expr, ec *EvalContext, locals map[string]lang.Expr) any {
	switch v := e.(type) {
	case *lang.ArrayLit:
		out := make([]any, len(v.Elements))
		for i, el := range v.Elements {
			out[i] = partialElement(el, ec, locals)
		}
		return out
	case *lang.ObjectLit:
		out := make(map[string]any, len(v.Fields))
		for _, fld := range v.Fields {
			var key string
			switch {
			case fld.Key.Kind == lang.FieldIdent && !fld.Key.IsMeta():
				key = fld.Key.Name
			case fld.Key.Kind == lang.FieldString:
				key = fld.Key.String
			default:
				continue
			}
			out[key] = partialElement(fld.Value, ec, locals)
		}
		return out
	default:
		return PendingValue{Refs: deferredRefs(e, locals)}
	}
}

// partialElement evaluates one element of a partially-resolved literal: the
// real value when it resolves, otherwise its own partial structure.
func partialElement(e lang.Expr, ec *EvalContext, locals map[string]lang.Expr) any {
	if val, err := Eval(e, ec); err == nil {
		return val
	}
	return partialValue(e, ec, locals)
}

// withoutPending returns inputs with every still-unresolved field reset to
// nil, the form the plan walk's decisions, decode, and trigger hashing expect.
// Display placeholders live only in a step's recorded Inputs; a field waiting
// on an upstream reads as nil here, exactly as a wholly unevaluated field did
// before partial structure was kept.
func withoutPending(inputs map[string]any, unresolved map[string][]string) map[string]any {
	if len(unresolved) == 0 {
		return inputs
	}
	out := maps.Clone(inputs)
	for name := range unresolved {
		out[name] = nil
	}
	return out
}

// Decision tags one node's planned action.
type Decision string

const (
	DecisionCreate  Decision = "create"
	DecisionUpdate  Decision = "update"
	DecisionReplace Decision = "replace"
	DecisionDestroy Decision = "destroy"
	DecisionNoOp    Decision = "no-op"
	DecisionRerun   Decision = "rerun"
	DecisionSkip    Decision = "skip"
	DecisionRead    Decision = "read"
	DecisionEval    Decision = "eval"
)

// PlanStep records one node's planned action. For resources, Inputs is
// the evaluated body. PriorOutputs is what state holds (nil for create
// or destroy of a resource that is not found). ObservedOutputs is what
// Resource.Read returned at plan time; it differs from PriorOutputs
// when the resource has drifted out of band. For actions, TriggerHash
// is the hash that determines whether to rerun or skip.
//
// UnresolvedInputs names the input fields whose plan-time evaluation
// hit a forward reference (an upstream node with no prior state). Each
// entry maps the field name to the source-side dot paths the body
// reads from. Apply re-evaluates these against the live scope.
type PlanStep struct {
	Address  string          `json:"address"`
	Kind     NodeKind        `json:"node-kind"`
	Selector *state.Selector `json:"selector,omitempty"`

	// Composite marks a step whose apply finalizes a composite call
	// site (a boundary) rather than a primitive leaf. A boundary's Kind
	// is its own resource/data/action kind, so the runtime cannot
	// tell it from a leaf by Kind alone; Node.IsComposite has source
	// body metadata, but a step has no body, so the bit is stored
	// explicitly in the plan file.
	Composite bool `json:"composite,omitempty"`

	Decision         Decision            `json:"decision"`
	Inputs           map[string]any      `json:"inputs,omitempty"`
	UnresolvedInputs map[string][]string `json:"unresolved-inputs,omitempty"`

	// DeferredConfig names the library-config node whose pending evaluation
	// kept this node's read from running at plan. The stored state is taken as
	// current for the decision; apply and the next plan see real values.
	DeferredConfig string `json:"deferred-config,omitempty"`

	// PriorInputs is the body the last apply evaluated, recorded so the plan
	// can show a changed field as `old -> new` rather than the new value
	// alone. Nil for a create, where there is no prior to compare against.
	PriorInputs map[string]any `json:"prior-inputs,omitempty"`

	PriorSelector   *state.Selector `json:"prior-selector,omitempty"`
	PriorOutputs    map[string]any  `json:"prior-outputs,omitempty"`
	ObservedOutputs map[string]any  `json:"observed-outputs,omitempty"`
	TriggerHash     string          `json:"trigger-hash,omitempty"`

	// ReplaceTriggers names the replace-forcing fields whose value changed,
	// the reason a step replaces rather than updates. The renderer tags each
	// with `(forces replacement)`. Empty unless Decision is replace.
	ReplaceTriggers []string `json:"replace-triggers,omitempty"`

	// DependsOn carries a destroy step's recorded dependencies from
	// prior state. Apply reverses these edges so a resource is deleted
	// before the resources it depended on.
	DependsOn []string `json:"depends-on,omitempty"`

	// AlreadyGone marks a destroy step whose resource was already
	// absent when the plan read it. Apply drops the entry from state
	// without calling Delete.
	AlreadyGone bool `json:"already-gone,omitempty"`

	// SensitiveInputs names the input fields whose value expression
	// reads from any sensitive source. Renderers replace the value
	// with a placeholder rather than printing the secret.
	SensitiveInputs []string `json:"sensitive-inputs,omitempty"`

	// SensitiveOutputs names the output fields of this step that are
	// sensitive. For a primitive resource/action it comes from the
	// library schema's tagged fields; for a composite call site it
	// comes from the composite type's `@sensitive` markers and from
	// propagation through its body.
	SensitiveOutputs []string `json:"sensitive-outputs,omitempty"`

	// regeneratesOutputs marks a step whose planned action produces a
	// new object (a resource replace, an action rerun), so every prior
	// output dies with the old one. The plan walk must not seed them: a
	// downstream reader sees those fields as unknown-until-apply
	// instead of values the apply is about to invalidate. An update is
	// not marked: it preserves the object, so its prior outputs stay
	// readable, and one that changes an output anyway is caught by the
	// apply-time premise check. Plan-walk state only; not part of the
	// plan file.
	regeneratesOutputs bool

	// mayChangeOutputs marks a step whose planned action could leave
	// any output different from prior state (changed resource inputs,
	// an action rerun). A data source reading behind an @depends-on
	// target waits for apply while this is set, even with settled
	// inputs. Plan-walk state only; not part of the plan file.
	mayChangeOutputs bool
}

// Drift reports whether the resource's observed outputs differ from
// the outputs in prior state. False for steps with no prior or no
// observation (Create, Destroy, Gone).
func (s *PlanStep) Drift() bool {
	if len(s.PriorOutputs) == 0 || len(s.ObservedOutputs) == 0 {
		return false
	}
	return !sameInputs(s.PriorOutputs, s.ObservedOutputs)
}

// Gone reports whether a resource with prior state was missing in the
// cloud at plan time. Encoded as Create with PriorOutputs set.
func (s *PlanStep) Gone() bool {
	return s.Kind == NodeResource &&
		s.Decision == DecisionCreate &&
		len(s.PriorOutputs) > 0
}

type PlannedEntryMove struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Plan is the readonly result of computing what an apply would do.
// StateRev is the snapshot rev the plan was computed against. Apply
// rejects the plan when the current rev no longer matches. Inputs
// captures the validated root inputs so apply can rebuild the same
// eval scope without re-reading the stack file.
type Plan struct {
	Factory    state.FactoryInfo
	Stack      string
	StateRev   string
	Inputs     map[string]any
	Steps      []*PlanStep
	StateMoves []PlannedEntryMove

	// Backend names the state backend the plan was computed against,
	// so apply can reconstruct the same backend without re-reading
	// the stack file. A nil value means the resolver's default (the local
	// backend under .unobin/state) was used.
	Backend *StateRef

	// Parallelism is the in-flight cap apply should honor. Zero means
	// the runtime's DefaultParallelism applies.
	Parallelism int

	// Destroy marks a teardown plan: every step is a destroy and apply
	// evaluates no outputs.
	Destroy bool
}

// Plan walks the DAG against prior state and returns the planned
// actions per node. Resources with prior state get their Read method
// invoked so the plan can report drift; no CRUD methods (Create,
// Update, Replace, Delete) are called and no actions run. Inputs that
// reference outputs of nodes about to run are evaluated against the
// prior state where available.
func (e *Executor) Plan(ctx context.Context) (*Plan, error) {
	if e.Store == nil {
		return nil, errors.New("executor: Store is required")
	}
	if err := e.CheckLibraryConfigs(); err != nil {
		return nil, err
	}
	rs, err := e.initRun()
	if err != nil {
		return nil, err
	}
	stateRev, _ := e.Store.CurrentRev()

	plan := &Plan{
		Factory:     e.Factory,
		Stack:       e.Store.Stack(),
		StateRev:    stateRev,
		Inputs:      e.Inputs,
		Parallelism: e.Parallelism,
		Destroy:     e.Destroy,
	}
	moves, err := e.applySourceEntryMoves(rs)
	if err != nil {
		return nil, err
	}
	plan.StateMoves = moves
	if err := e.seedPriorInternalConfigurations(rs.prior, e.Inputs); err != nil {
		return nil, err
	}

	// Seed the EvalContext with prior outputs so downstream evaluation
	// has something to bind to even when an upstream node would change.
	if err := e.seedFromPriorState(rs); err != nil {
		return nil, err
	}

	sensitivity := e.sensitivityAnalyzer()

	// A destroy plan wants nothing from source, so the desired-state
	// walk is skipped. With no live addresses, every prior leaf below
	// becomes a destroy step.
	liveAddresses := make(map[string]bool)
	var constraintErrs []error
	if !e.Destroy {
		rs.plannedByTemplate = map[string][]*PlanStep{}
		for _, addr := range rs.order {
			node := e.DAG.Nodes[addr]
			steps, err := e.planNodeSteps(ctx, rs, node)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", addr, err)
			}
			for _, step := range steps {
				step.SensitiveInputs = sensitivity.sensitiveInputs(node.Body, node.Composite)
				step.SensitiveOutputs = sensitivity.sensitiveOutputs(node)
				if !step.Composite {
					switch step.Kind {
					case NodeResource, NodeDataSource, NodeAction:
						if configAddr, pending := e.pendingInternalConfig(node); pending {
							step.DeferredConfig = configAddr
						}
					}
				}
				plan.Steps = append(plan.Steps, step)
				tmpl := templateAddress(step.Address)
				rs.plannedByTemplate[tmpl] = append(rs.plannedByTemplate[tmpl], step)
				liveAddresses[step.Address] = true
				if err := e.seedStepAttrs(rs, step); err != nil {
					return nil, fmt.Errorf("%s: %w", step.Address, err)
				}
				constraintErrs = append(constraintErrs, e.checkStepConstraints(step)...)
				constraintErrs = append(constraintErrs, e.checkCompositeConstraints(rs, step)...)
			}
		}

		if err := e.runPendingReads(ctx, rs); err != nil {
			return nil, err
		}
		if err := e.finalizePendingReads(rs); err != nil {
			return nil, err
		}
		upgradeActionRerun(plan.Steps, e.DAG, newScopeLocals(e.rootLocalExprs(), e.DAG.Nodes))
	}

	if len(constraintErrs) > 0 {
		return nil, errors.Join(constraintErrs...)
	}

	// Orphans: prior entries with no live address in this plan become
	// destroy steps. A normal plan only destroys orphaned leaf
	// resources; action and library-call records are cleaned up by
	// pruning. A destroy plan removes every record, so it emits a step
	// for each entry type.
	if rs.prior != nil {
		for _, prior := range rs.prior.Entries {
			if liveAddresses[prior.Address] {
				continue
			}
			kind, composite, ok := destroyEntryKind(prior.Type)
			if !ok {
				continue
			}
			if !e.Destroy && prior.Type != state.EntryLeaf {
				continue
			}
			plan.Steps = append(plan.Steps, &PlanStep{
				Address:      prior.Address,
				Kind:         kind,
				Selector:     selectorFromEntry(prior),
				Composite:    composite,
				Decision:     DecisionDestroy,
				Inputs:       prior.Inputs,
				PriorOutputs: prior.Outputs,
				DependsOn:    prior.DependsOn,
			})
		}
	}
	if err := e.readDestroySteps(ctx, plan.Steps); err != nil {
		return nil, err
	}
	return plan, nil
}

// destroyEntryKind maps a state entry type to the node kind its destroy
// step takes and whether that step is a composite boundary. Leaf
// entries delete a real resource; action and library-call records have
// no external lifecycle and are only removed from state. A library-call
// record does not remember its resource/data/action kind, but a
// destroy boundary never reads its kind (it is only removed), so the
// kind is left empty and the composite bit alone routes it.
func destroyEntryKind(t state.EntryType) (kind NodeKind, composite, ok bool) {
	switch t {
	case state.EntryLeaf:
		return NodeResource, false, true
	case state.EntryAction:
		return NodeAction, false, true
	case state.EntryData:
		return NodeDataSource, false, true
	case state.EntryLibraryCall:
		return "", true, true
	}
	return "", false, false
}

// readDestroySteps reads each destroy step's resource so the plan can
// tell an already-absent resource from one that still needs deleting.
// A read that comes back not-found marks the step AlreadyGone, so apply
// drops the entry without calling Delete. Reads run in parallel up to
// effectiveParallelism. This keeps destroy on the same "plan reads
// every resource with prior state" footing as a normal plan, and
// covers orphan destroys in a normal plan as well as a full teardown.
func (e *Executor) readDestroySteps(ctx context.Context, steps []*PlanStep) error {
	var jobs []*PlanStep
	for _, s := range steps {
		// Only resources have something in the world to read; action
		// and library-call records are removed without a read.
		if s.Decision == DecisionDestroy && s.Kind == NodeResource {
			jobs = append(jobs, s)
		}
	}
	if len(jobs) == 0 {
		return nil
	}
	gone := make([]bool, len(jobs))
	errs := make([]error, len(jobs))
	sem := make(chan struct{}, e.effectiveParallelism())
	var wg sync.WaitGroup
	for i, s := range jobs {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			gone[i], errs[i] = guard("reading this resource", true, func() (bool, error) {
				return e.readDestroyTarget(ctx, s)
			})
		})
	}
	wg.Wait()
	for i, s := range jobs {
		if errs[i] != nil {
			return fmt.Errorf("%s: read: %w", s.Address, errs[i])
		}
		s.AlreadyGone = gone[i]
	}
	return nil
}

// readDestroyTarget resolves a destroy step's library from its selector
// and reads the resource, reporting whether it is already gone. It
// needs no DAG node, so it works for an orphan whose source has been
// removed as well as for a full teardown.
func (e *Executor) readDestroyTarget(ctx context.Context, step *PlanStep) (bool, error) {
	alias, typeName, ok := stepSelectorParts(step)
	if !ok {
		return false, fmt.Errorf("missing selector for %q", step.Address)
	}
	lib, ok := e.librariesForAddress(step.Address)[alias]
	if !ok {
		return false, fmt.Errorf("library %q is not imported", alias)
	}
	rt, ok := lib.Resources[typeName]
	if !ok {
		return false, fmt.Errorf("library %s has no resource %q", alias, typeName)
	}
	cfg, err := e.configForStateAddress(step.Address, alias)
	if err != nil {
		return false, err
	}
	_, err = readObserved(ctx, rt, alias,
		cfg, step.Inputs, step.PriorOutputs)
	if errors.Is(err, ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

// planNodeSteps wraps planNode so a single per-node planner can emit
// multiple steps. A `@for-each` template (resource, action, data
// source, or composite) fans out into one step per instance;
// everything else returns a one-element slice (or nil to skip the
// node). Nodes inside a `@for-each` composite are skipped here: their
// boundary's planner emits per-instance subtrees for them.
func (e *Executor) planNodeSteps(
	ctx context.Context, rs *runState, n *Node,
) ([]*PlanStep, error) {
	if e.insideForEachComposite(n) {
		return nil, nil
	}
	if n.ForEach != nil {
		if n.IsComposite() {
			return e.planForEachComposite(ctx, rs, n)
		}
		return e.planForEachLeaf(ctx, rs, n)
	}
	step, err := e.planNode(ctx, rs, n)
	if err != nil {
		return nil, err
	}
	if step == nil {
		return nil, nil
	}
	return []*PlanStep{step}, nil
}

// resourceRegistration returns the registration backing a resource
// node, with an error naming a missing import or type.
func (e *Executor) resourceRegistration(n *Node) (ResourceRegistration, error) {
	lib, ok := e.librariesFor(n)[n.Alias]
	if !ok {
		return nil, fmt.Errorf("library %q is not imported", n.Alias)
	}
	rt, ok := lib.Resources[n.Type]
	if !ok {
		return nil, fmt.Errorf("library %s has no resource %q", n.Alias, n.Type)
	}
	return rt, nil
}

// actionRegistration mirrors resourceRegistration for actions.
func (e *Executor) actionRegistration(n *Node) (ActionRegistration, error) {
	lib, ok := e.librariesFor(n)[n.Alias]
	if !ok {
		return nil, fmt.Errorf("library %q is not imported", n.Alias)
	}
	at, ok := lib.Actions[n.Type]
	if !ok {
		return nil, fmt.Errorf("library %s has no action %q", n.Alias, n.Type)
	}
	return at, nil
}

// dataRegistration mirrors resourceRegistration for data sources.
func (e *Executor) dataRegistration(n *Node) (DataSourceRegistration, error) {
	lib, ok := e.librariesFor(n)[n.Alias]
	if !ok {
		return nil, fmt.Errorf("library %q is not imported", n.Alias)
	}
	dt, ok := lib.DataSources[n.Type]
	if !ok {
		return nil, fmt.Errorf("library %s has no data source %q", n.Alias, n.Type)
	}
	return dt, nil
}

// planOneInstance plans one instance of a leaf node against scope at
// addr, resolving the node's registration by kind. The plain path
// passes the node's own address; the for-each path passes a child
// scope with `@each` bound and a `['<key>']` address.
func (e *Executor) planOneInstance(
	ctx context.Context, rs *runState, n *Node, scope *EvalContext, addr string,
) (*PlanStep, error) {
	switch n.Kind {
	case NodeResource:
		rt, err := e.resourceRegistration(n)
		if err != nil {
			return nil, err
		}
		return e.planOneResource(rs, n, rt, scope, addr)
	case NodeAction:
		if _, err := e.actionRegistration(n); err != nil {
			return nil, err
		}
		return e.planOneAction(rs, n, scope, addr)
	case NodeDataSource:
		if _, err := e.dataRegistration(n); err != nil {
			return nil, err
		}
		return e.planOneData(ctx, rs, n, scope, addr)
	}
	return nil, fmt.Errorf("%s: unsupported node kind %q", addr, n.Kind)
}

func (e *Executor) seedFromPriorState(rs *runState) error {
	if rs.prior == nil {
		return nil
	}
	for _, ent := range rs.prior.Entries {
		switch ent.Type {
		case state.EntryAction:
			scope, err := e.scopeForAddress(rs, ent.Address)
			if err != nil {
				return err
			}
			if scope == nil {
				continue
			}
			tmpl, instKey := splitInstanceAddress(ent.Address)
			if instKey == "" {
				seedAddress(scope.Actions, tmpl, ent.Outputs)
			} else {
				seedAddressInstance(scope.Actions, tmpl, instKey, ent.Outputs)
			}
		case state.EntryLeaf:
			scope, err := e.scopeForAddress(rs, ent.Address)
			if err != nil {
				return err
			}
			if scope == nil {
				continue
			}
			tmpl, instKey := splitInstanceAddress(ent.Address)
			if instKey == "" {
				seedAddress(scope.Resources, tmpl, ent.Outputs)
			} else {
				seedAddressInstance(scope.Resources, tmpl, instKey, ent.Outputs)
			}
		}
	}
	return nil
}

// seedStepAttrs makes a planned leaf node's attribute view visible to
// the nodes that follow it in the topological walk, so a body that
// reads an input of an upstream node resolves it at plan time rather
// than deferring to apply. The view is the node's known inputs laid
// under any prior outputs, matching the apply-time merge. Only inputs
// that are actually known are seeded: a field still waiting on an
// upstream is left out so a reader sees it as unknown-until-apply
// rather than a misleading nil. A composite boundary seeds its
// declared outputs as far as they evaluate; a destroy seeds nothing.
func (e *Executor) seedStepAttrs(rs *runState, step *PlanStep) error {
	if step.Decision == DecisionDestroy {
		return nil
	}
	if step.Composite {
		return e.seedCompositeOutputs(rs, step)
	}
	switch step.Kind {
	case NodeResource, NodeDataSource, NodeAction:
	default:
		return nil
	}
	scope, err := e.scopeForAddress(rs, step.Address)
	if err != nil || scope == nil {
		return err
	}
	tmpl, instKey := splitInstanceAddress(step.Address)
	target := scopeMapForKind(scope, step.Kind)
	// A data source read during the plan seeds what it observed; at
	// this point in the walk a resource's drift read has not run yet,
	// so resources still seed their prior outputs. A step whose apply
	// regenerates the object seeds none: every prior output dies with
	// it, so readers wait instead. An update preserves the object, so
	// its computed-only outputs stay readable; its declared fields come
	// from the body, since a same-named prior output echoes exactly the
	// value the update is about to set.
	outputs := step.PriorOutputs
	if step.ObservedOutputs != nil {
		outputs = step.ObservedOutputs
	}
	if step.regeneratesOutputs {
		outputs = nil
	} else if step.mayChangeOutputs {
		outputs = withoutDeclared(outputs, step.Inputs)
	}
	attrs := mergeAttrs(knownFields(step, step.Inputs), outputs)
	if instKey == "" {
		seedAddress(target, tmpl, attrs)
	} else {
		seedAddressInstance(target, tmpl, instKey, attrs)
	}
	return nil
}

// seedCompositeOutputs makes a composite boundary's outputs visible
// to the nodes that follow it in the walk. The boundary plans after
// its internals, so its scope already holds what each internal
// seeded; the outputs block is reduced against that scope and the
// result seeded at the call site in the boundary's enclosing scope.
// A field reading a value not yet known is left out, so a reader of
// it waits for apply exactly as it would for the internal itself.
func (e *Executor) seedCompositeOutputs(rs *runState, step *PlanStep) error {
	node, ok := e.DAG.Nodes[templateAddress(step.Address)]
	if !ok || !node.IsComposite() {
		return nil
	}
	scope, ok := rs.composites[step.Address]
	if !ok || scope == nil {
		return nil
	}
	outputs, err := planCompositeOutputs(node, scope)
	if err != nil {
		return err
	}
	if outputs == nil {
		return nil
	}
	parent, err := e.enclosingScope(rs, step.Address)
	if err != nil {
		return err
	}
	target := scopeMapForKind(parent, node.Kind)
	tmpl, instKey := splitInstanceAddress(step.Address)
	if instKey == "" {
		seedAddress(target, tmpl, outputs)
	} else {
		seedAddressInstance(target, tmpl, instKey, outputs)
	}
	return nil
}

// withoutDeclared returns outputs minus the fields the body declares.
// A declared field's plan-time value is the body's, whether settled or
// pending, so a stale same-named output must not be read in its place.
func withoutDeclared(outputs, declared map[string]any) map[string]any {
	if len(outputs) == 0 {
		return outputs
	}
	out := make(map[string]any, len(outputs))
	for name, value := range outputs {
		if _, ok := declared[name]; ok {
			continue
		}
		out[name] = value
	}
	return out
}

// knownFields returns inputs minus the fields the step left
// unresolved at plan time. Seeding omits them so a reader sees
// unknown-until-apply instead of a misleading nil, and the apply
// premise check skips them since they are expected to settle there.
func knownFields(step *PlanStep, inputs map[string]any) map[string]any {
	if len(step.UnresolvedInputs) == 0 {
		return inputs
	}
	known := make(map[string]any, len(inputs))
	for name, value := range inputs {
		if _, pending := step.UnresolvedInputs[name]; pending {
			continue
		}
		known[name] = value
	}
	return known
}

// scopeForAddress returns the scope a state entry belongs to. Entries
// addressed inside a composite (their address contains `/`) seed the
// scope of their direct enclosing composite, which is everything up
// to the last `/`. The callsite may carry a `['key']` suffix when the
// composite has `@for-each`; ensureCompositeScope builds the
// per-instance scope from it. When a prior entry's composite has been
// removed from source, its boundary is not in the DAG, so there is no
// scope to seed and the entry is skipped (a nil scope tells the
// caller to move on).
func (e *Executor) scopeForAddress(rs *runState, addr string) (*EvalContext, error) {
	if i := strings.LastIndex(addr, "/"); i >= 0 {
		callSite := addr[:i]
		if _, ok := e.DAG.Nodes[templateAddress(callSite)]; !ok {
			return nil, nil
		}
		scope, err := e.ensureCompositeScope(rs, callSite)
		if err != nil {
			if errors.Is(err, ErrInstanceGone) {
				return nil, nil
			}
			return nil, err
		}
		return scope, nil
	}
	return rs.eval, nil
}

func seedAddress(target map[string]any, addr string, value map[string]any) {
	path, ok := addressValuePath(addr)
	if !ok {
		return
	}
	seedPath(target, path, value)
}

func seedAddressInstance(target map[string]any, addr, key string, value map[string]any) {
	path, ok := addressValuePath(addr)
	if !ok {
		return
	}
	seedPathInstance(target, path, key, value)
}

func seedPath(target map[string]any, path []string, value map[string]any) {
	if len(path) == 0 {
		return
	}
	m := target
	for _, part := range path[:len(path)-1] {
		m = getOrCreate(m, part)
	}
	m[path[len(path)-1]] = value
}

func seedPathInstance(target map[string]any, path []string, key string, value map[string]any) {
	if len(path) == 0 {
		return
	}
	m := target
	for _, part := range path {
		m = getOrCreate(m, part)
	}
	m[key] = value
}

// SplitInstanceAddress separates a `<template>['<key>']` address into
// its template part and the instance key. Non-instance addresses
// return unchanged with an empty key.
func SplitInstanceAddress(addr string) (template, key string) {
	return splitInstanceAddress(addr)
}

// splitInstanceAddress is the package-internal version used by Plan
// and ApplyPlan. It is also exposed via SplitInstanceAddress for the
// renderer in `pkg/runner`.
func splitInstanceAddress(addr string) (template, key string) {
	if !strings.HasSuffix(addr, "']") {
		return addr, ""
	}
	idx := strings.LastIndex(addr, "['")
	if idx < 0 {
		return addr, ""
	}
	return addr[:idx], addr[idx+2 : len(addr)-2]
}

// isUpstreamChange reports whether the named decision implies the
// referenced node's outputs may differ from what's in prior state.
func isUpstreamChange(d Decision) bool {
	switch d {
	case DecisionCreate, DecisionUpdate, DecisionReplace, DecisionDestroy, DecisionRerun:
		return true
	}
	return false
}

func (e *Executor) planLibraryConfig(rs *runState, n *Node) (*PlanStep, error) {
	return e.planConfigNode(rs, n)
}

func (e *Executor) planConfigNode(rs *runState, n *Node) (*PlanStep, error) {
	step := &PlanStep{Address: n.Address, Kind: n.Kind, Decision: DecisionEval}
	scope, err := e.scopeForAddress(rs, n.Address)
	if err != nil {
		return nil, err
	}
	var inputs map[string]any
	if _, ok := n.Body.(*lang.ObjectLit); ok {
		var unresolved map[string][]string
		inputs, unresolved, err = planEvalBody(n.Body, scope)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", n.Address, err)
		}
		step.Inputs = inputs
		if len(unresolved) > 0 {
			step.UnresolvedInputs = unresolved
			return step, nil
		}
	} else {
		inputs, err = evalConfigurationBody(n.Body, scope)
		if errors.Is(err, ErrEvalNotFound) {
			return step, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", n.Address, err)
		}
		step.Inputs = inputs
	}
	lib, ok := e.librariesFor(n)[n.Alias]
	if !ok || lib.Configuration == nil {
		return nil, fmt.Errorf("%s: library %q declares no configuration",
			n.Address, n.Alias)
	}
	decoded, err := cfg.Decode(lib.Configuration, inputs)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", n.Address, err)
	}
	e.storeInternalConfiguration(n.Address, decoded)
	return step, nil
}

func (e *Executor) planNode(ctx context.Context, rs *runState, n *Node) (*PlanStep, error) {
	if n.IsComposite() {
		return e.planComposite(rs, n)
	}
	switch n.Kind {
	case NodeResource, NodeAction, NodeDataSource:
		scope, err := e.scopeFor(rs, n)
		if err != nil {
			return nil, err
		}
		return e.planOneInstance(ctx, rs, n, scope, n.Address)
	case NodeOutput:
		return &PlanStep{Address: n.Address, Kind: n.Kind, Decision: DecisionEval}, nil
	case NodeLibraryConfig:
		return e.planLibraryConfig(rs, n)
	default:
		return nil, fmt.Errorf("unknown node kind %q", n.Kind)
	}
}

// checkStepConstraints validates a leaf node's evaluated args against its
// Go type's constraints, returning one error per violation prefixed with
// the node address. Each rule runs as soon as every field it reads
// resolved at plan, so a forward-reference arg (unknown until apply)
// never causes a false violation: only the rules that read a pending
// field wait for apply. A missing optional arg evaluates to null in a
// predicate rather than erroring. Composite boundaries and types that
// declare no constraint contribute nothing.
func (e *Executor) checkStepConstraints(step *PlanStep) []error {
	if step.Composite {
		return nil
	}
	switch step.Kind {
	case NodeResource, NodeDataSource, NodeAction:
	default:
		return nil
	}
	tmpl := templateAddress(step.Address)
	alias, typeName, ok := stepSelectorParts(step)
	if !ok {
		return nil
	}
	lib, ok := e.librariesForAddress(tmpl)[alias]
	if !ok || lib == nil {
		return nil
	}
	specs := lib.Constraints[string(step.Kind)+"."+typeName]
	if len(specs) == 0 {
		return nil
	}
	deferred := make(map[string]bool, len(step.UnresolvedInputs))
	for name := range step.UnresolvedInputs {
		deferred[name] = true
	}
	entries, perr := lang.ParseSpecs(specs)
	values := make(map[string]any, len(step.Inputs))
	maps.Copy(values, step.Inputs)
	eval := func(ex lang.Expr, binds []lang.EachBinding) (any, error) {
		ctx := &EvalContext{Inputs: values, MissingAsNull: true}
		ApplyBindings(ctx, binds)
		v, err := Eval(ex, ctx)
		if errors.Is(err, ErrEvalNotFound) {
			return nil, nil
		}
		return v, err
	}
	var out []error
	for _, er := range perr.Errors() {
		out = append(out, fmt.Errorf("%s: %v", step.Address, er))
	}
	for i, c := range entries {
		if c.ReadsAny(deferred) {
			continue
		}
		checked := lang.CheckConstraintEntry(i, c, values, eval, lang.DisplayNodeRelative)
		for _, er := range checked.Errors() {
			out = append(out, fmt.Errorf("%s: %v", step.Address, er))
		}
	}
	return out
}

// checkCompositeConstraints validates a composite boundary's own
// `constraints:` block, returning one error per violation prefixed with
// the boundary address. A composite's constraints are its input contract,
// checked the same way the factory checks its own: against the inputs
// alone. The call site's args were evaluated when the boundary scope was
// built, so a forward-reference arg makes that fail earlier and never
// reaches here. A composite applies no defaults, so every declared input
// the call site left out is filled with null first; an unset input then
// reads as null in a predicate, the same as an unset optional input does
// for the factory. Nodes other than composites contribute nothing, as do
// composites with no scope or no constraints.
func (e *Executor) checkCompositeConstraints(rs *runState, step *PlanStep) []error {
	if !step.Composite {
		return nil
	}
	node, ok := e.DAG.Nodes[templateAddress(step.Address)]
	if !ok || !node.IsComposite() {
		return nil
	}
	constraints := compositeConstraints(node)
	if constraints == nil || len(constraints.Elements) == 0 {
		return nil
	}
	scope, ok := rs.composites[step.Address]
	if !ok || scope == nil {
		return nil
	}
	values := make(map[string]any, len(scope.Inputs))
	for name := range compositeInputNames(node) {
		values[name] = nil
	}
	maps.Copy(values, scope.Inputs)
	eval := func(ex lang.Expr, binds []lang.EachBinding) (any, error) {
		ctx := &EvalContext{Inputs: values, Libraries: node.Libraries, MissingAsNull: true}
		ApplyBindings(ctx, binds)
		return Eval(ex, ctx)
	}
	var out []error
	for _, er := range lang.CheckConstraints(
		constraints, values, eval, lang.DisplayNodeRelative,
	).Errors() {
		out = append(out, fmt.Errorf("%s: %v", step.Address, er))
	}
	return out
}

// planComposite plans the composite boundary. The call site args are
// evaluated against the boundary's enclosing scope (root for top-level
// boundaries, the outer composite's scope for nested ones) by
// constructing the composite scope; its Inputs are those evaluated args.
// The boundary itself does no CRUD: its decision is Eval and its
// outputs are computed at apply time after the internals run.
func (e *Executor) planComposite(rs *runState, n *Node) (*PlanStep, error) {
	scope, err := e.ensureCompositeScope(rs, n.Address)
	if err != nil {
		return nil, err
	}
	var prior *state.Entry
	if rs.prior != nil {
		prior = rs.prior.Find(n.Address)
	}
	var priorOut map[string]any
	if prior != nil {
		priorOut = prior.Outputs
	}
	return &PlanStep{
		Address:      n.Address,
		Kind:         n.Kind,
		Selector:     selectorForNode(n),
		Composite:    true,
		Decision:     DecisionEval,
		Inputs:       scope.Inputs,
		PriorOutputs: priorOut,
	}, nil
}

// planOneAction plans a single action instance against the given scope
// and state address. Used both by the plain action path
// (scope == parent, addr == n.Address) and by the for-each path
// (scope has @each bound, addr has the `['<key>']` suffix).
func (e *Executor) planOneAction(
	rs *runState, n *Node, scope *EvalContext, addr string,
) (*PlanStep, error) {
	display, unresolved, err := planEvalBody(n.Body, scope)
	if err != nil {
		return nil, err
	}
	if err := e.applyInputDefaults(n, display, unresolved); err != nil {
		return nil, err
	}
	inputs := withoutPending(display, unresolved)
	trigger, err := ComputeTrigger(n, inputs, scope)
	if err != nil {
		return nil, err
	}

	var prior *state.Entry
	if rs.prior != nil {
		prior = rs.prior.Find(addr)
	}
	dec := DecisionRerun
	var priorOut map[string]any
	if prior != nil {
		priorOut = prior.Outputs
	}
	if !trigger.AlwaysRerun && prior != nil && prior.TriggerHash != "" &&
		prior.TriggerHash == trigger.Hash {
		dec = DecisionSkip
	}
	return &PlanStep{
		Address:            addr,
		Kind:               n.Kind,
		Selector:           selectorForNode(n),
		Decision:           dec,
		Inputs:             display,
		UnresolvedInputs:   unresolved,
		PriorOutputs:       priorOut,
		TriggerHash:        trigger.Hash,
		regeneratesOutputs: dec == DecisionRerun,
		mayChangeOutputs:   dec == DecisionRerun,
	}, nil
}

// pendingRead is the per-resource read job queued by planOneResource
// during Plan's serial walk. runPendingReads fans them out across
// workers; finalizePendingReads then sets each step's Decision and
// ObservedOutputs from the result.
type pendingRead struct {
	step         *PlanStep
	rt           ResourceRegistration
	alias        string
	cfg          any
	inputs       map[string]any
	priorOutputs map[string]any
	observed     map[string]any
	err          error
}

// planOneResource plans a single resource instance against the given
// scope and state address. Used both by the plain resource path
// (scope == parent, addr == n.Address) and by the for-each path
// (scope has @each bound, addr has the `['<key>']` suffix). When the
// resource has prior state, the Read is deferred onto rs.pendingReads
// for parallel execution; the returned step's Decision is left blank
// until finalizePendingReads runs.
func (e *Executor) planOneResource(
	rs *runState, n *Node, rt ResourceRegistration,
	scope *EvalContext, addr string,
) (*PlanStep, error) {
	display, unresolved, err := planEvalBody(n.Body, scope)
	if err != nil {
		return nil, err
	}
	if err := e.applyInputDefaults(n, display, unresolved); err != nil {
		return nil, err
	}
	var prior *state.Entry
	if rs.prior != nil {
		prior = rs.prior.Find(addr)
	}
	step := &PlanStep{
		Address:          addr,
		Kind:             n.Kind,
		Selector:         selectorForNode(n),
		Inputs:           display,
		UnresolvedInputs: unresolved,
	}
	if prior == nil {
		step.Decision = DecisionCreate
		return step, nil
	}
	inputs := withoutPending(display, unresolved)
	priorSelector := selectorFromEntry(prior)
	if !sameSelector(priorSelector, step.Selector) {
		priorRT, priorAlias, err := e.resourceRegistrationForSelector(addr, priorSelector)
		if err != nil {
			return nil, err
		}
		migrated, err := migrateEntry(priorRT, priorAlias, prior.SchemaVersion,
			MigrationState{Inputs: prior.Inputs, Outputs: prior.Outputs})
		if err != nil {
			return nil, err
		}
		step.PriorSelector = priorSelector
		step.PriorInputs = cloneMap(migrated.Inputs)
		step.PriorOutputs = migrated.Outputs
		step.Decision = DecisionReplace
		step.regeneratesOutputs = true
		step.mayChangeOutputs = true
		return step, nil
	}
	migrated, err := migrateEntry(rt, n.Alias, prior.SchemaVersion,
		MigrationState{Inputs: prior.Inputs, Outputs: prior.Outputs})
	if err != nil {
		return nil, err
	}
	// Overlay the type's current declared defaults onto the prior inputs so
	// a field given a default since the prior was written is compared as
	// that default rather than as absent. Without this, the addition reads
	// as a change and forces a vacuous update on every existing instance.
	// Copy first: with no schema bump the migrated inputs alias the prior
	// snapshot's own map.
	priorInputs := cloneMap(migrated.Inputs)
	if priorInputs == nil {
		priorInputs = map[string]any{}
	}
	if err := e.applyInputDefaults(n, priorInputs, nil); err != nil {
		return nil, err
	}
	step.PriorOutputs = migrated.Outputs
	step.PriorInputs = priorInputs
	step.mayChangeOutputs = !sameInputs(priorInputs, inputs)
	// Whether changed inputs force a replace is decided here, mid-walk,
	// from inputs alone: downstream nodes plan next and need to know
	// whether this node's outputs survive. A replace-marked field still
	// waiting on an upstream compares as changed, since the value it
	// settles to cannot be assumed equal to the prior one.
	if step.mayChangeOutputs {
		probe := rt.NewReceiver()
		if err := Decode(probe, inputs); err != nil {
			return nil, err
		}
		step.ReplaceTriggers = changedReplaceFields(rt.ReplaceFields(probe), priorInputs, inputs)
		step.regeneratesOutputs = len(step.ReplaceTriggers) > 0
	}
	// A pending internal configuration means the read cannot run: there
	// is nothing valid to hand the API client. The stored state stands
	// in for the observed world, so drift goes unchecked this plan and
	// the decision comes from the input diff alone.
	if configAddr, pending := e.pendingInternalConfig(n); pending {
		step.DeferredConfig = configAddr
		switch {
		case step.regeneratesOutputs:
			step.Decision = DecisionReplace
		case step.mayChangeOutputs:
			step.Decision = DecisionUpdate
		default:
			step.Decision = DecisionNoOp
		}
		return step, nil
	}
	rs.pendingReads = append(rs.pendingReads, &pendingRead{
		step:         step,
		rt:           rt,
		alias:        n.Alias,
		cfg:          e.configFor(n),
		inputs:       inputs,
		priorOutputs: migrated.Outputs,
	})
	return step, nil
}

// runPendingReads runs every queued resource Read concurrently, up to
// effectiveParallelism. The result is stashed on each pendingRead so
// finalizePendingReads can pick between ErrNotFound (which maps to
// Create) and a real read failure (which propagates).
func (e *Executor) runPendingReads(ctx context.Context, rs *runState) error {
	if len(rs.pendingReads) == 0 {
		return nil
	}
	sem := make(chan struct{}, e.effectiveParallelism())
	var wg sync.WaitGroup
	for _, pr := range rs.pendingReads {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			pr.observed, pr.err = guard("reading this resource", true, func() (map[string]any, error) {
				return readObserved(ctx, pr.rt, pr.alias, pr.cfg, pr.inputs, pr.priorOutputs)
			})
		})
	}
	wg.Wait()
	return nil
}

// finalizePendingReads walks the queued reads in plan order and turns
// each (observed, err) pair into a step Decision: ErrNotFound becomes
// Create, any other error propagates, and a clean read picks between
// Replace, Update, and NoOp using the existing input-diff and
// drift-diff rules.
func (e *Executor) finalizePendingReads(rs *runState) error {
	for _, pr := range rs.pendingReads {
		if err := finalizeResourceRead(pr); err != nil {
			return fmt.Errorf("%s: %w", pr.step.Address, err)
		}
	}
	return nil
}

func finalizeResourceRead(pr *pendingRead) error {
	if errors.Is(pr.err, ErrNotFound) {
		pr.step.Decision = DecisionCreate
		return nil
	}
	if pr.err != nil {
		return fmt.Errorf("read: %w", pr.err)
	}
	pr.step.ObservedOutputs = pr.observed
	if pr.step.mayChangeOutputs {
		if pr.step.regeneratesOutputs {
			pr.step.Decision = DecisionReplace
		} else {
			pr.step.Decision = DecisionUpdate
		}
		return nil
	}
	if pr.step.Drift() {
		pr.step.Decision = DecisionUpdate
		return nil
	}
	pr.step.Decision = DecisionNoOp
	return nil
}

// upgradeActionRerun walks plan steps in order and turns a Skip action
// into a Rerun when any of its references points to a step whose
// decision implies an upstream change. A reference made through a local
// counts: the local is followed to the upstream it reads. The walk runs
// after all reads have finalized, so every step's Decision is set; it
// updates the per-address index in place so a transitive Skip->Rerun
// chain picks up the upgrade from an earlier step.
func upgradeActionRerun(steps []*PlanStep, dag *DAG, sl *scopeLocals) {
	addressDecision := make(map[string]Decision, len(steps))
	for _, step := range steps {
		addressDecision[step.Address] = step.Decision
	}
	for _, step := range steps {
		if step.Kind != NodeAction || step.Decision != DecisionSkip {
			continue
		}
		node := dag.Nodes[templateAddress(step.Address)]
		if node == nil {
			continue
		}
		for _, ref := range refsWithLocalsInScope(
			node.Body, sl.forScope(node.Composite), dag.Nodes, node.Composite,
		) {
			if isUpstreamChange(addressDecision[ref]) {
				step.Decision = DecisionRerun
				step.TriggerHash = ""
				addressDecision[step.Address] = DecisionRerun
				break
			}
		}
	}
}

// planOneData plans a single data source instance against the given
// scope and state address. A data source whose inputs all resolved is
// read here, during the plan, so everything downstream of it diffs a
// real value instead of a pending one; the result rides the step as
// ObservedOutputs and apply verifies the world still agrees. Inputs
// still waiting on an upstream node defer the read to apply, as
// before.
func (e *Executor) planOneData(
	ctx context.Context, rs *runState, n *Node, scope *EvalContext, addr string,
) (*PlanStep, error) {
	inputs, unresolved, err := planEvalBody(n.Body, scope)
	if err != nil {
		return nil, err
	}
	if err := e.applyInputDefaults(n, inputs, unresolved); err != nil {
		return nil, err
	}
	step := &PlanStep{
		Address:          addr,
		Kind:             n.Kind,
		Selector:         selectorForNode(n),
		Decision:         DecisionRead,
		Inputs:           inputs,
		UnresolvedInputs: unresolved,
	}
	if len(unresolved) > 0 || e.dependsOnChange(rs, n) {
		return step, nil
	}
	if configAddr, pending := e.pendingInternalConfig(n); pending {
		step.DeferredConfig = configAddr
		return step, nil
	}
	dt, err := e.dataRegistration(n)
	if err != nil {
		return nil, err
	}
	receiver := dt.NewReceiver()
	if err := Decode(receiver, inputs); err != nil {
		return nil, err
	}
	result, err := dt.Read(ctx, receiver, e.configFor(n))
	if err != nil {
		blameLibrary(err, n.Alias)
		return nil, fmt.Errorf("read: %w", err)
	}
	step.ObservedOutputs = mapify(result)
	return step, nil
}

// dependsOnChange reports whether the data source reads from a node
// with changes pending this plan: a create, an in-place update, a
// replace, or an action rerun. The read then defers to apply, the same
// way an unresolved input does, because the value it would observe only
// settles once that node has run; reading a node mid-change yields a
// stale plan the apply-time premise check would reject. A node merely
// read or left untouched is not pending, so a steady-state plan still
// reads its data sources and diffs real values. The reference set is
// the resolved DAG edges, which hold both body references and
// @depends-on targets, scoped through any composite call site; a target
// naming a composite covers every step inside it. The walk is
// topological, so each target is planned before the read asks.
func (e *Executor) dependsOnChange(rs *runState, n *Node) bool {
	for _, target := range e.DAG.Edges[n.Address] {
		for tmpl, steps := range rs.plannedByTemplate {
			if tmpl != target && !strings.HasPrefix(tmpl, target+"/") {
				continue
			}
			if slices.ContainsFunc(steps, changesOutputs) {
				return true
			}
		}
	}
	return false
}

// changesOutputs reports whether a step's object will move once it
// runs, so a value read from it before apply may go stale: a create, an
// action rerun, or any in-place update or replace, both of which flag
// mayChangeOutputs.
func changesOutputs(s *PlanStep) bool {
	return s.Decision == DecisionCreate || s.Decision == DecisionRerun || s.mayChangeOutputs
}

// readObserved decodes inputs onto a fresh resource and asks the
// library what's in the cloud for it. It returns the result in the
// same canonical map state uses, or ErrNotFound when the resource is
// gone.
func readObserved(
	ctx context.Context,
	rt ResourceRegistration,
	alias string,
	cfg any,
	inputs, priorOutputs map[string]any,
) (map[string]any, error) {
	receiver := rt.NewReceiver()
	if err := Decode(receiver, inputs); err != nil {
		return nil, err
	}
	result, err := rt.Read(ctx, receiver, cfg, priorOutputs)
	if err != nil {
		blameLibrary(err, alias)
		return nil, err
	}
	return mapify(result), nil
}
