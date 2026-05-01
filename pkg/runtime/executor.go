package runtime

import (
	"context"
	"fmt"
	"reflect"

	"github.com/cloudboss/unobin/pkg/lang"
)

// Executor runs a DAG end to end: each node is processed in topological
// order, with body expressions evaluated against an EvalContext that
// grows as upstream nodes complete. Action and data source results feed
// back into the context for downstream references. Output values
// collect into the result.
type Executor struct {
	DAG     *DAG
	Modules map[string]*Module
	Inputs  map[string]any
}

// ExecResult is what the Executor produces: the outputs map, plus the
// Action and Data tables populated during the run for later inspection.
type ExecResult struct {
	Outputs map[string]any
	Actions map[string]any
	Data    map[string]any
}

// Run executes every node in dependency order and returns the result.
// The first error from a node aborts the run.
func (e *Executor) Run(ctx context.Context) (*ExecResult, error) {
	order, err := e.DAG.TopologicalOrder()
	if err != nil {
		return nil, err
	}
	eval := &EvalContext{
		Vars:      e.Inputs,
		Resources: make(map[string]any),
		Data:      make(map[string]any),
		Actions:   make(map[string]any),
	}
	outputs := make(map[string]any)
	for _, addr := range order {
		node := e.DAG.Nodes[addr]
		if err := e.runNode(ctx, node, eval, outputs); err != nil {
			return nil, fmt.Errorf("%s: %w", addr, err)
		}
	}
	return &ExecResult{
		Outputs: outputs,
		Actions: eval.Actions,
		Data:    eval.Data,
	}, nil
}

func (e *Executor) runNode(ctx context.Context, n *Node, ec *EvalContext, outputs map[string]any) error {
	switch n.Kind {
	case NodeAction:
		return e.runAction(ctx, n, ec)
	case NodeData:
		return e.runData(ctx, n, ec)
	case NodeOutput:
		val, err := Eval(n.Body, ec)
		if err != nil {
			return err
		}
		outputs[n.Name] = val
		return nil
	case NodeResource:
		return fmt.Errorf("resources are not handled by the executor yet")
	default:
		return fmt.Errorf("unknown node kind %q", n.Kind)
	}
}

func (e *Executor) runAction(ctx context.Context, n *Node, ec *EvalContext) error {
	mod, ok := e.Modules[n.NS]
	if !ok {
		return fmt.Errorf("module %q is not imported", n.NS)
	}
	at, ok := mod.Actions[n.Type]
	if !ok {
		return fmt.Errorf("module %s has no action %q", n.NS, n.Type)
	}
	inputs, err := evalBody(n.Body, ec)
	if err != nil {
		return err
	}
	action := at.New()
	if err := Decode(action, inputs); err != nil {
		return err
	}
	result, err := action.Run(ctx)
	if err != nil {
		return err
	}
	storeNested(ec.Actions, n, mapify(result))
	return nil
}

func (e *Executor) runData(ctx context.Context, n *Node, ec *EvalContext) error {
	mod, ok := e.Modules[n.NS]
	if !ok {
		return fmt.Errorf("module %q is not imported", n.NS)
	}
	dt, ok := mod.DataSources[n.Type]
	if !ok {
		return fmt.Errorf("module %s has no data source %q", n.NS, n.Type)
	}
	inputs, err := evalBody(n.Body, ec)
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
	storeNested(ec.Data, n, mapify(result))
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
