package runtime

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func (e *Executor) newStateScopes(
	snapshot *state.Snapshot, inputs map[string]any,
) (*runState, error) {
	root, err := e.newEvalScope(nil, inputs)
	if err != nil {
		return nil, err
	}
	rs := &runState{
		eval:             root,
		composites:       map[string]*EvalContext{},
		forEachInstances: map[string]map[string]any{},
		scopeSnapshot:    indexSnapshotEntries(snapshot),
		scopeChildren:    map[string][]*state.Entry{},
	}
	if snapshot != nil {
		for _, entry := range snapshot.Entries {
			parent := DirectParent(entry.Address)
			rs.scopeChildren[parent] = append(rs.scopeChildren[parent], entry)
		}
	}
	seedScopeEntries(root, rs.scopeChildren[""])
	return rs, nil
}

func seedScopeEntries(scope *EvalContext, entries []*state.Entry) {
	for _, entry := range entries {
		category := Category(entry.Category)
		if category != NodeResource && category != NodeDataSource && category != NodeAction {
			continue
		}
		attrs := cloneMap(entry.Outputs)
		if !entry.Composite {
			attrs = mergeAttrs(entry.Inputs, entry.Outputs)
		}
		address, key := splitInstanceAddress(entry.Address)
		target := scopeMapForCategory(scope, category)
		if key == "" {
			seedAddress(target, address, attrs)
		} else {
			seedAddressInstance(target, address, key, attrs)
		}
	}
}

func (e *Executor) scopeBoundary(
	rs *runState, address string, parent *EvalContext,
) (*Node, error) {
	if boundary := e.DAG.Nodes[declarationAddress(address)]; boundary != nil {
		return boundary, nil
	}
	if rs.scopeSnapshot == nil {
		return nil, fmt.Errorf("composite %s: boundary node not in DAG", address)
	}
	entry := rs.scopeSnapshot.find(address)
	if entry == nil || !entry.Composite {
		return nil, fmt.Errorf("composite %s: boundary entry not in state", address)
	}
	alias, export, ok := entryBindingParts(entry)
	if !ok {
		return nil, fmt.Errorf("composite %s: missing binding", address)
	}
	library := parent.Libraries[alias]
	if library == nil {
		return nil, fmt.Errorf("composite %s: library %q is not imported", address, alias)
	}
	category := Category(entry.Category)
	composite := library.Composite(category, export)
	if composite == nil || composite.SyntaxBody == nil {
		return nil, fmt.Errorf("composite %s: library %s has no %s %q",
			address, alias, category, export)
	}
	boundary := &Node{
		Address:             declarationAddress(address),
		Kind:                category,
		CompositeSyntaxBody: composite.SyntaxBody,
		Libraries:           composite.Libraries,
		AssetSetID:          composite.AssetSetID,
	}
	boundary.Libraries = compositeBodyLibraries(boundary, parent.Libraries)
	return boundary, nil
}

func (e *Executor) stateScopeInputs(
	rs *runState, boundary *Node, address string, parent *EvalContext,
) (map[string]any, error) {
	saved := map[string]any{}
	if entry := rs.scopeSnapshot.find(address); entry != nil {
		saved = entry.Inputs
	}
	if boundary.Body == nil {
		return cloneMap(saved), nil
	}
	scope := parent
	declaration, key := splitInstanceAddress(address)
	missingEach := false
	if key != "" {
		instances, err := forEachInstancesFor(rs, declaration, boundary.ForEach, parent)
		if err != nil && !errors.Is(err, ErrEvalNotFound) {
			return nil, err
		}
		if value, ok := instances[key]; ok {
			scope = childScopeWithEach(parent, key, value)
		} else {
			missingEach = true
		}
	}
	body, ok := boundary.Body.(*lang.ObjectLit)
	if !ok {
		return nil, fmt.Errorf("call arguments must be an object literal")
	}
	inputs := map[string]any{}
	for _, field := range body.Fields {
		if field.Key.Kind != lang.FieldIdent || field.Key.IsMeta() {
			continue
		}
		var value any
		var err error
		if missingEach && referencesEach(field.Value) {
			err = ErrEvalNotFound
		} else {
			value, err = Eval(field.Value, scope)
		}
		if errors.Is(err, ErrEvalNotFound) {
			if prior, ok := saved[field.Key.Name]; ok {
				inputs[field.Key.Name] = cloneValue(prior)
				continue
			}
		}
		if err != nil {
			return nil, diagnostic.Context(field.Key.Name, err)
		}
		inputs[field.Key.Name] = value
	}
	return inputs, nil
}

func referencesEach(expression lang.Expr) bool {
	found := false
	lang.ScanExpr(expression, lang.ScanCallbacks{
		DotPath: func(path *lang.DotPath, _ lang.ScanContext) lang.ScanDecision {
			if path.Root.Name == "@each" {
				found = true
			}
			return lang.ScanContinue
		},
	})
	return found
}

func (e *Executor) seedPriorInternalConfigurations(
	prior *state.Snapshot, inputs map[string]any,
) error {
	if prior == nil || e.DAG == nil {
		return nil
	}
	rs, err := e.newStateScopes(prior, inputs)
	if err != nil {
		return err
	}
	owners := map[string]bool{"": true}
	for _, entry := range prior.Entries {
		if entry.Composite || entry.Category != string(NodeResource) {
			continue
		}
		for parent := DirectParent(entry.Address); parent != ""; parent = DirectParent(parent) {
			owners[parent] = true
		}
	}
	for _, owner := range slices.Sorted(maps.Keys(owners)) {
		scope := rs.eval
		var boundary *Node
		if owner != "" {
			scope, err = e.ensureCompositeScope(rs, owner)
			if err != nil {
				return err
			}
			parent, err := e.enclosingScope(rs, owner)
			if err != nil {
				return err
			}
			boundary, err = e.scopeBoundary(rs, owner, parent)
			if err != nil {
				return err
			}
		}
		e.internalMu.Lock()
		if e.priorScopeLibraries == nil {
			e.priorScopeLibraries = map[string]map[string]*Library{}
		}
		e.priorScopeLibraries[owner] = scope.Libraries
		e.internalMu.Unlock()
		for _, node := range e.scopeConfigurationNodes(owner, boundary) {
			if err := e.seedPriorConfigNode(node, scope); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Executor) scopeConfigurationNodes(owner string, boundary *Node) []*Node {
	if boundary != nil {
		return extractSyntaxLibraryConfigs(boundary.CompositeSyntaxBody.LibraryConfigs, owner)
	}
	var nodes []*Node
	for _, address := range slices.Sorted(maps.Keys(e.DAG.Nodes)) {
		node := e.DAG.Nodes[address]
		if node.Category() == NodeLibraryConfig && node.Composite == "" {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func (e *Executor) seedPriorConfigNode(node *Node, scope *EvalContext) error {
	e.internalMu.Lock()
	if e.priorConfigurationBindings == nil {
		e.priorConfigurationBindings = map[string]bool{}
	}
	e.priorConfigurationBindings[node.Address] = true
	e.internalMu.Unlock()
	raw, err := evalConfigurationBody(node.Body, scope)
	if errors.Is(err, ErrEvalNotFound) {
		return nil
	}
	if err != nil {
		return diagnostic.Context(node.Address, err)
	}
	library := scope.Libraries[node.Alias]
	if library == nil || library.Configuration == nil {
		return fmt.Errorf("%s: library %q declares no configuration", node.Address, node.Alias)
	}
	decoded, err := e.decodeLibraryConfig(library, raw)
	if err != nil {
		return diagnostic.Context(node.Address, err)
	}
	e.internalMu.Lock()
	if e.priorInternalConfigurations == nil {
		e.priorInternalConfigurations = map[string]any{}
	}
	e.priorInternalConfigurations[node.Address] = decoded
	e.internalMu.Unlock()
	return nil
}
