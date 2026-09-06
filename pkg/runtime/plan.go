package runtime

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/lang"
)

// PendingValue represents an unresolved expression during planning. Its
// source addresses identify the values that must resolve before apply can
// pass concrete inputs to a provider.
type PendingValue struct {
	Refs []string
}

func pendingValueRefs(value any) ([]string, bool) {
	var refs []string
	found := false
	var visit func(any)
	visit = func(current any) {
		switch current := current.(type) {
		case PendingValue:
			found = true
			refs = append(refs, current.Refs...)
		case []any:
			for _, element := range current {
				visit(element)
			}
		case map[string]any:
			keys := make([]string, 0, len(current))
			for key := range current {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			for _, key := range keys {
				visit(current[key])
			}
		}
	}
	visit(value)
	return dedupe(refs), found
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
			return nil, nil, diagnostic.Context(fmt.Sprintf("field %q", fld.Key.Name), err)
		}
		value, refs, err := partialValue(fld.Value, ec, locals)
		if err != nil {
			return nil, nil, diagnostic.Context(fmt.Sprintf("field %q", fld.Key.Name), err)
		}
		if len(refs) == 0 {
			inputs[fld.Key.Name] = nil
			continue
		}
		inputs[fld.Key.Name] = value
		if unresolved == nil {
			unresolved = map[string][]string{}
		}
		unresolved[fld.Key.Name] = refs
	}
	return inputs, unresolved, nil
}

// partialValue rebuilds a body field whose full evaluation hit a forward
// reference. It descends array and object literals, expands whole-value local
// references, and chooses a conditional branch when its condition is known.
// Resolved elements keep their values; unresolved elements become PendingValue
// markers at their real positions. Expressions that cannot be reduced safely
// remain one PendingValue holding the addresses they read. Resource target
// preparation encodes these partial values in the saved plan.
func partialValue(
	e lang.Expr,
	ec *EvalContext,
	locals map[string]lang.Expr,
) (any, []string, error) {
	evaluator := partialEvaluator{
		ec:        ec,
		locals:    locals,
		expanding: map[string]bool{},
	}
	return evaluator.element(e)
}

type partialEvaluator struct {
	ec        *EvalContext
	locals    map[string]lang.Expr
	expanding map[string]bool
}

func (p *partialEvaluator) value(e lang.Expr) (any, []string, error) {
	switch v := e.(type) {
	case *lang.ArrayLit:
		out := make([]any, len(v.Elements))
		var refs []string
		for i, el := range v.Elements {
			value, elementRefs, err := p.element(el)
			if err != nil {
				return nil, nil, diagnostic.Context(fmt.Sprintf("index %d", i), err)
			}
			out[i] = value
			refs = append(refs, elementRefs...)
		}
		return out, dedupe(refs), nil
	case *lang.ObjectLit:
		out := make(map[string]any, len(v.Fields))
		var refs []string
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
			value, fieldRefs, err := p.element(fld.Value)
			if err != nil {
				return nil, nil, diagnostic.Context(fmt.Sprintf("key %q", key), err)
			}
			out[key] = value
			refs = append(refs, fieldRefs...)
		}
		return out, dedupe(refs), nil
	case *lang.Conditional:
		cond, err := Eval(v.Cond, p.ec)
		if err != nil {
			if !errors.Is(err, ErrEvalNotFound) {
				return nil, nil, err
			}
			break
		}
		b, ok := cond.(bool)
		if !ok {
			return nil, nil, fmt.Errorf(
				"eval: if: condition must be a boolean, got %s",
				lang.TypeMessage(cond),
			)
		}
		if b {
			return p.element(v.Then)
		}
		return p.element(v.Else)
	case *lang.DotPath:
		if value, refs, err, ok := p.local(v); ok {
			return value, refs, err
		}
	}
	refs, err := pendingRefs(e, p.ec, p.locals)
	if err != nil {
		return nil, nil, err
	}
	return PendingValue{Refs: refs}, refs, nil
}

func (p *partialEvaluator) local(
	path *lang.DotPath,
) (any, []string, error, bool) {
	if path.Root.Name != "local" ||
		len(path.Segments) == 0 ||
		path.Segments[0].Name == "" {
		return nil, nil, nil, false
	}
	name := path.Segments[0].Name
	expr, ok := p.locals[name]
	if !ok {
		return nil, nil, nil, false
	}
	if p.expanding[name] {
		return nil, nil, fmt.Errorf(
			"eval: local %q refers to itself through a cycle",
			name,
		), true
	}
	p.expanding[name] = true
	defer delete(p.expanding, name)
	return p.at(expr, path.Segments[1:])
}

func (p *partialEvaluator) at(
	e lang.Expr,
	segments []lang.DotSegment,
) (any, []string, error, bool) {
	if len(segments) == 0 {
		value, refs, err := p.element(e)
		return value, refs, err, true
	}
	switch v := e.(type) {
	case *lang.ObjectLit:
		name := dotSegmentKey(segments[0])
		if name == "" {
			return nil, nil, nil, false
		}
		field := objectField(v, name)
		if field == nil {
			return nil, nil, nil, false
		}
		return p.at(field, segments[1:])
	case *lang.Conditional:
		cond, err := Eval(v.Cond, p.ec)
		if err != nil {
			if errors.Is(err, ErrEvalNotFound) {
				return nil, nil, nil, false
			}
			return nil, nil, err, true
		}
		b, ok := cond.(bool)
		if !ok {
			return nil, nil, fmt.Errorf(
				"eval: if: condition must be a boolean, got %s",
				lang.TypeMessage(cond),
			), true
		}
		if b {
			return p.at(v.Then, segments)
		}
		return p.at(v.Else, segments)
	case *lang.DotPath:
		value, refs, err := p.element(graftDotPath(v, segments))
		return value, refs, err, true
	default:
		return nil, nil, nil, false
	}
}

func (p *partialEvaluator) element(e lang.Expr) (any, []string, error) {
	value, err := evalExpr(e, p.ec)
	if err == nil {
		refs, _ := pendingValueRefs(value)
		return value, refs, nil
	}
	if !errors.Is(err, ErrEvalNotFound) {
		return nil, nil, err
	}
	return p.value(e)
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

type PlannedEntryMove struct {
	From string `json:"from"`
	To   string `json:"to"`
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
	callSite := DirectParent(addr)
	if callSite == "" {
		return rs.eval, nil
	}
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

func splitInstanceAddress(addr string) (template, key string) {
	if template, key, ok := splitEntryKey(addr); ok {
		return template, key
	}
	return addr, ""
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
