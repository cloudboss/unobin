package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudboss/unobin/pkg/state"
)

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
// or destroy of a resource that is not found). For actions, TriggerHash
// is the hash that determines whether to rerun or skip.
type PlanStep struct {
	Address      string         `json:"address"`
	Kind         NodeKind       `json:"kind"`
	Decision     Decision       `json:"decision"`
	Inputs       map[string]any `json:"inputs,omitempty"`
	PriorOutputs map[string]any `json:"prior-outputs,omitempty"`
	TriggerHash  string         `json:"trigger-hash,omitempty"`
}

// Plan is the readonly result of computing what an apply would do.
// StateRev is the snapshot rev the plan was computed against. Apply
// rejects the plan when the current rev no longer matches.
type Plan struct {
	Stack        state.StackInfo
	DeploymentID string
	StateRev     string
	Steps        []*PlanStep
}

// Plan walks the DAG against prior state and returns the planned
// actions per node. It does not execute anything: no resource CRUD
// methods are called, no actions run. Inputs that reference outputs of
// nodes about to run are evaluated against the prior state where
// available.
func (e *Executor) Plan(_ context.Context) (*Plan, error) {
	if e.Store == nil {
		return nil, errors.New("executor: Store is required")
	}
	order, err := e.DAG.TopologicalOrder()
	if err != nil {
		return nil, err
	}
	rs, err := e.initRun()
	if err != nil {
		return nil, err
	}
	stateRev, _ := e.Store.CurrentRev()

	plan := &Plan{
		Stack:        e.Stack,
		DeploymentID: e.Store.DeploymentID,
		StateRev:     stateRev,
	}

	// Seed the EvalContext with prior outputs so downstream evaluation
	// has something to bind to even when an upstream node would change.
	seedFromPriorState(rs)

	addressDecision := make(map[string]Decision)
	for _, addr := range order {
		node := e.DAG.Nodes[addr]
		step, err := e.planNode(rs, node)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", addr, err)
		}
		if step == nil {
			continue
		}
		// An action whose upstream is changing must rerun, even when its
		// own inputs and trigger value (against prior state) match.
		if step.Kind == NodeAction && step.Decision == DecisionSkip {
			for _, ref := range Refs(node.Body) {
				if isUpstreamChange(addressDecision[ref]) {
					step.Decision = DecisionRerun
					step.TriggerHash = ""
					break
				}
			}
		}
		plan.Steps = append(plan.Steps, step)
		addressDecision[step.Address] = step.Decision
	}

	// Orphans: prior leaf entries with no source resource node.
	if rs.prior != nil {
		for _, prior := range rs.prior.Entries {
			if prior.Type != state.EntryLeaf {
				continue
			}
			if _, ok := e.DAG.Nodes[prior.Address]; ok {
				continue
			}
			plan.Steps = append(plan.Steps, &PlanStep{
				Address:      prior.Address,
				Kind:         NodeResource,
				Decision:     DecisionDestroy,
				Inputs:       prior.Inputs,
				PriorOutputs: prior.Outputs,
			})
		}
	}
	return plan, nil
}

func seedFromPriorState(rs *runState) {
	if rs.prior == nil {
		return
	}
	for _, ent := range rs.prior.Entries {
		switch ent.Type {
		case state.EntryAction:
			ns, kind, name, ok := parseActionAddress(ent.Address)
			if !ok {
				continue
			}
			seedNested(rs.eval.Actions, ns, kind, name, ent.Outputs)
		case state.EntryLeaf:
			ns, typeName, name, ok := parseResourceAddress(ent.Address)
			if !ok {
				continue
			}
			seedNested(rs.eval.Resources, ns, typeName, name, ent.Outputs)
		}
	}
}

func seedNested(target map[string]any, ns, typeName, name string, value map[string]any) {
	nsMap := getOrCreate(target, ns)
	typeMap := getOrCreate(nsMap, typeName)
	typeMap[name] = value
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

func parseActionAddress(addr string) (ns, kind, name string, ok bool) {
	parts := strings.SplitN(addr, ".", 4)
	if len(parts) != 4 || parts[0] != "action" {
		return "", "", "", false
	}
	return parts[1], parts[2], parts[3], true
}

func (e *Executor) planNode(rs *runState, n *Node) (*PlanStep, error) {
	switch n.Kind {
	case NodeAction:
		return e.planAction(rs, n)
	case NodeResource:
		return e.planResource(rs, n)
	case NodeData:
		inputs, err := evalBody(n.Body, rs.eval)
		if err != nil {
			return nil, err
		}
		return &PlanStep{
			Address:  n.Address,
			Kind:     n.Kind,
			Decision: DecisionRead,
			Inputs:   inputs,
		}, nil
	case NodeOutput:
		return &PlanStep{Address: n.Address, Kind: n.Kind, Decision: DecisionEval}, nil
	default:
		return nil, fmt.Errorf("unknown node kind %q", n.Kind)
	}
}

func (e *Executor) planAction(rs *runState, n *Node) (*PlanStep, error) {
	mod, ok := e.Modules[n.NS]
	if !ok {
		return nil, fmt.Errorf("module %q is not imported", n.NS)
	}
	if _, ok := mod.Actions[n.Type]; !ok {
		return nil, fmt.Errorf("module %s has no action %q", n.NS, n.Type)
	}
	inputs, err := evalBody(n.Body, rs.eval)
	if err != nil {
		return nil, err
	}
	trigger, err := ComputeTrigger(n, inputs, rs.eval)
	if err != nil {
		return nil, err
	}

	var prior *state.Entry
	if rs.prior != nil {
		prior = rs.prior.Find(n.Address)
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
		Address:      n.Address,
		Kind:         n.Kind,
		Decision:     dec,
		Inputs:       inputs,
		PriorOutputs: priorOut,
		TriggerHash:  trigger.Hash,
	}, nil
}

func (e *Executor) planResource(rs *runState, n *Node) (*PlanStep, error) {
	mod, ok := e.Modules[n.NS]
	if !ok {
		return nil, fmt.Errorf("module %q is not imported", n.NS)
	}
	rt, ok := mod.Resources[n.Type]
	if !ok {
		return nil, fmt.Errorf("module %s has no resource %q", n.NS, n.Type)
	}
	inputs, err := evalBody(n.Body, rs.eval)
	if err != nil {
		return nil, err
	}

	var prior *state.Entry
	if rs.prior != nil {
		prior = rs.prior.Find(n.Address)
	}
	step := &PlanStep{
		Address: n.Address,
		Kind:    n.Kind,
		Inputs:  inputs,
	}
	switch {
	case prior == nil:
		step.Decision = DecisionCreate
	case sameInputs(prior.Inputs, inputs):
		step.Decision = DecisionNoOp
		step.PriorOutputs = prior.Outputs
	default:
		step.PriorOutputs = prior.Outputs
		probe := rt.New()
		if err := Decode(probe, inputs); err != nil {
			return nil, err
		}
		if needsReplace(probe, prior.Inputs, inputs) {
			step.Decision = DecisionReplace
		} else {
			step.Decision = DecisionUpdate
		}
	}
	return step, nil
}
