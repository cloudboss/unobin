package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/state"
)

// Executor runs a DAG end to end: each node is processed in topological
// order, with body expressions evaluated against an EvalContext that
// grows as upstream nodes complete. Action and data source results feed
// back into the context for downstream references. Output values
// collect into the result.
//
// The Executor reads the prior snapshot from Store at the start of the
// run, uses it to skip actions whose trigger hash matches, and writes a
// fresh snapshot at the end. Store and Stack must be set.
type Executor struct {
	DAG     *DAG
	Modules map[string]*Module
	Inputs  map[string]any

	Store state.Backend
	Stack state.StackInfo
}

// ExecResult is what the Executor produces: the outputs map, the
// Action and Data tables populated during the run, and the rev of the
// snapshot written (empty when no Store was configured).
type ExecResult struct {
	Outputs    map[string]any
	Actions    map[string]any
	Data       map[string]any
	WrittenRev string
}

type runState struct {
	eval    *EvalContext
	outputs map[string]any
	prior   *state.Snapshot
	next    *state.Snapshot
}

// Run executes every node in dependency order and returns the result.
// The first error from a node aborts the run.
func (e *Executor) Run(ctx context.Context) (*ExecResult, error) {
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
	for _, addr := range order {
		node := e.DAG.Nodes[addr]
		if err := e.runNode(ctx, rs, node); err != nil {
			return nil, fmt.Errorf("%s: %w", addr, err)
		}
	}
	if err := e.deleteOrphans(ctx, rs); err != nil {
		return nil, err
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

func (e *Executor) initRun() (*runState, error) {
	rs := &runState{
		eval: &EvalContext{
			Vars:      e.Inputs,
			Resources: make(map[string]any),
			Data:      make(map[string]any),
			Actions:   make(map[string]any),
		},
		outputs: make(map[string]any),
		next:    state.NewSnapshot(e.Stack, e.Store.DeploymentID()),
	}
	prior, err := e.Store.Current()
	if err != nil && !errors.Is(err, state.ErrNoCurrent) {
		return nil, err
	}
	rs.prior = prior
	return rs, nil
}

func (e *Executor) persist(rs *runState) (string, error) {
	rev, err := e.Store.Write(rs.next)
	if err != nil {
		return "", err
	}
	if err := e.Store.SetCurrent(rev); err != nil {
		return "", err
	}
	return rev, nil
}

func (e *Executor) runNode(ctx context.Context, rs *runState, n *Node) error {
	switch n.Kind {
	case NodeAction:
		return e.runAction(ctx, rs, n)
	case NodeData:
		return e.runData(ctx, rs, n)
	case NodeOutput:
		val, err := Eval(n.Body, rs.eval)
		if err != nil {
			return err
		}
		rs.outputs[n.Name] = val
		return nil
	case NodeResource:
		return e.runResource(ctx, rs, n)
	default:
		return fmt.Errorf("unknown node kind %q", n.Kind)
	}
}

func (e *Executor) runResource(ctx context.Context, rs *runState, n *Node) error {
	mod, ok := e.Modules[n.NS]
	if !ok {
		return fmt.Errorf("module %q is not imported", n.NS)
	}
	rt, ok := mod.Resources[n.Type]
	if !ok {
		return fmt.Errorf("module %s has no resource %q", n.NS, n.Type)
	}
	inputs, err := evalBody(n.Body, rs.eval)
	if err != nil {
		return err
	}

	var prior *state.Entry
	if rs.prior != nil {
		prior = rs.prior.Find(n.Address)
	}

	resource := rt.New()
	if err := Decode(resource, inputs); err != nil {
		return err
	}

	var outputs map[string]any
	switch {
	case prior == nil:
		result, err := resource.Create(ctx)
		if err != nil {
			return err
		}
		outputs = mapify(result)
	case sameInputs(prior.Inputs, inputs):
		outputs = prior.Outputs
	case needsReplace(resource, prior.Inputs, inputs):
		if err := resource.Delete(ctx, prior.Outputs); err != nil {
			return fmt.Errorf("replace: delete prior: %w", err)
		}
		result, err := resource.Create(ctx)
		if err != nil {
			return fmt.Errorf("replace: create: %w", err)
		}
		outputs = mapify(result)
	default:
		result, err := resource.Update(ctx, prior.Outputs)
		if err != nil {
			return err
		}
		outputs = mapify(result)
	}
	storeNested(rs.eval.Resources, n, outputs)

	rs.next.Entries = append(rs.next.Entries, &state.Entry{
		Address:       n.Address,
		Type:          state.EntryLeaf,
		Kind:          n.Type,
		SchemaVersion: rt.SchemaVersion,
		Inputs:        inputs,
		Outputs:       outputs,
	})
	return nil
}

// sameInputs compares two input maps by their canonical JSON form so a
// state round trip, which renders integers as floats, doesn't show up as
// a change.
func sameInputs(a, b map[string]any) bool {
	aj, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bj, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(aj, bj)
}

// needsReplace reports whether any field in r.ReplaceFields() has a
// different canonical JSON value between prior and current inputs.
func needsReplace(r Resource, prior, current map[string]any) bool {
	for _, field := range r.ReplaceFields() {
		if !sameValue(prior[field], current[field]) {
			return true
		}
	}
	return false
}

func sameValue(a, b any) bool {
	aj, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bj, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(aj, bj)
}

// deleteOrphans destroys leaf entries that were in the prior snapshot
// but have no corresponding node in the new run. It runs after the main
// DAG walk and before the snapshot is written.
func (e *Executor) deleteOrphans(ctx context.Context, rs *runState) error {
	if rs.prior == nil {
		return nil
	}
	keep := make(map[string]bool, len(rs.next.Entries))
	for _, ent := range rs.next.Entries {
		keep[ent.Address] = true
	}
	for _, prior := range rs.prior.Entries {
		if prior.Type != state.EntryLeaf {
			continue
		}
		if keep[prior.Address] {
			continue
		}
		ns, typeName, _, ok := parseResourceAddress(prior.Address)
		if !ok {
			return fmt.Errorf("orphan %s: cannot parse address", prior.Address)
		}
		mod, ok := e.Modules[ns]
		if !ok {
			return fmt.Errorf("orphan %s: module %q is not imported", prior.Address, ns)
		}
		rt, ok := mod.Resources[typeName]
		if !ok {
			return fmt.Errorf("orphan %s: module %s has no resource %q",
				prior.Address, ns, typeName)
		}
		resource := rt.New()
		if err := Decode(resource, prior.Inputs); err != nil {
			return fmt.Errorf("orphan %s: %w", prior.Address, err)
		}
		if err := resource.Delete(ctx, prior.Outputs); err != nil {
			return fmt.Errorf("delete orphan %s: %w", prior.Address, err)
		}
	}
	return nil
}

func parseResourceAddress(addr string) (ns, typeName, name string, ok bool) {
	parts := strings.SplitN(addr, ".", 4)
	if len(parts) != 4 || parts[0] != "resource" {
		return "", "", "", false
	}
	return parts[1], parts[2], parts[3], true
}

func (e *Executor) runAction(ctx context.Context, rs *runState, n *Node) error {
	mod, ok := e.Modules[n.NS]
	if !ok {
		return fmt.Errorf("module %q is not imported", n.NS)
	}
	at, ok := mod.Actions[n.Type]
	if !ok {
		return fmt.Errorf("module %s has no action %q", n.NS, n.Type)
	}
	inputs, err := evalBody(n.Body, rs.eval)
	if err != nil {
		return err
	}
	trigger, err := ComputeTrigger(n, inputs, rs.eval)
	if err != nil {
		return err
	}

	var prior *state.Entry
	if rs.prior != nil {
		prior = rs.prior.Find(n.Address)
	}
	skip := !trigger.AlwaysRerun && prior != nil && prior.TriggerHash != "" &&
		prior.TriggerHash == trigger.Hash

	var outputs map[string]any
	if skip {
		outputs = prior.Outputs
	} else {
		action := at.New()
		if err := Decode(action, inputs); err != nil {
			return err
		}
		result, err := action.Run(ctx)
		if err != nil {
			return err
		}
		outputs = mapify(result)
	}
	storeNested(rs.eval.Actions, n, outputs)

	rs.next.Entries = append(rs.next.Entries, &state.Entry{
		Address:     n.Address,
		Type:        state.EntryAction,
		Kind:        n.Type,
		TriggerHash: trigger.Hash,
		Inputs:      inputs,
		Outputs:     outputs,
	})
	return nil
}

func (e *Executor) runData(ctx context.Context, rs *runState, n *Node) error {
	mod, ok := e.Modules[n.NS]
	if !ok {
		return fmt.Errorf("module %q is not imported", n.NS)
	}
	dt, ok := mod.DataSources[n.Type]
	if !ok {
		return fmt.Errorf("module %s has no data source %q", n.NS, n.Type)
	}
	inputs, err := evalBody(n.Body, rs.eval)
	if err != nil {
		return err
	}
	ds := dt.New()
	if err := Decode(ds, inputs); err != nil {
		return err
	}
	result, err := ds.Read(ctx)
	if err != nil {
		return err
	}
	storeNested(rs.eval.Data, n, mapify(result))
	return nil
}

// evalBody evaluates an object literal body to a map[string]any of input
// values. `@`-prefixed meta keys are runtime metadata and skipped.
func evalBody(body lang.Expr, ec *EvalContext) (map[string]any, error) {
	obj, ok := body.(*lang.ObjectLit)
	if !ok {
		return nil, fmt.Errorf("body must be an object literal")
	}
	out := make(map[string]any, len(obj.Fields))
	for _, fld := range obj.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		val, err := Eval(fld.Value, ec)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", fld.Key.Name, err)
		}
		out[fld.Key.Name] = val
	}
	return out, nil
}

// storeNested writes value at target[ns][type][name], creating intermediate
// maps as needed.
func storeNested(target map[string]any, n *Node, value map[string]any) {
	ns := getOrCreate(target, n.NS)
	typeMap := getOrCreate(ns, n.Type)
	typeMap[n.Name] = value
}

func getOrCreate(m map[string]any, key string) map[string]any {
	if v, ok := m[key]; ok {
		if mm, ok := v.(map[string]any); ok {
			return mm
		}
	}
	nm := make(map[string]any)
	m[key] = nm
	return nm
}

// mapify reduces a typed result struct to a map[string]any using its
// `mapstructure` field tags. Maps pass through; nil yields nil; anything
// else (non-struct, non-map) yields nil.
func mapify(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}
	rt := rv.Type()
	out := make(map[string]any, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if !field.IsExported() {
			continue
		}
		name := field.Tag.Get("mapstructure")
		if name == "" {
			name = field.Name
		}
		if name == "-" {
			continue
		}
		out[name] = rv.Field(i).Interface()
	}
	return out
}
