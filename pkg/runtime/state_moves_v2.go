package runtime

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func ApplyEntryMovesV2(
	snapshot *state.SnapshotV2,
	dag *DAG,
	libraries map[string]*Library,
	specs []EntryMoveSpec,
	mode EntryMoveMode,
) (*state.SnapshotV2, []EntryMoveResult, error) {
	if mode != EntryMoveStrict && mode != EntryMoveIdempotent {
		return nil, nil, fmt.Errorf("invalid state move mode %d", mode)
	}
	if snapshot == nil {
		if _, err := normalizeEntryMoveSpecs(specs); err != nil {
			return nil, nil, err
		}
		if mode == EntryMoveStrict && len(specs) > 0 {
			return nil, nil, fmt.Errorf("state move: no current state")
		}
		return nil, nil, nil
	}
	next, err := snapshot.Clone()
	if err != nil {
		return nil, nil, err
	}
	exact, prefixes, err := prepareEntryMoveRulesV2(snapshot, dag, specs)
	if err != nil {
		return nil, nil, err
	}
	if mode == EntryMoveStrict {
		for _, spec := range specs {
			if snapshot.Find(spec.From.Address) == nil {
				return nil, nil, fmt.Errorf("no entry at %s", spec.From.Address)
			}
		}
	}
	results := make([]EntryMoveResult, 0)
	planned := make([]PlannedEntryMove, 0)
	for _, entry := range snapshot.Entries {
		from := EntryRef{Address: entry.Address}
		to, changed := entryMoveTargetForRef(from, exact, prefixes)
		if !changed || SameEntryRef(from, to) {
			continue
		}
		node, err := entryMoveTargetNode(dag, to)
		if err != nil {
			return nil, nil, err
		}
		if err := validateEntryMoveTargetV2(entry, node, dag, libraries); err != nil {
			return nil, nil, err
		}
		results = append(results, EntryMoveResult{From: from, To: to})
		planned = append(planned, PlannedEntryMove{From: from.Address, To: to.Address})
	}
	if err := validatePlanMoves(planned); err != nil {
		return nil, nil, err
	}
	if err := relocateSnapshotEntriesV2(next, planned); err != nil {
		return nil, nil, err
	}
	return next, results, nil
}

func prepareEntryMoveRulesV2(
	snapshot *state.SnapshotV2,
	dag *DAG,
	specs []EntryMoveSpec,
) (map[string]normalizedEntryMove, []normalizedEntryMove, error) {
	for _, spec := range specs {
		if _, err := ParseEntryRef(spec.From.Address); err != nil {
			return nil, nil, fmt.Errorf("state move source: %w", err)
		}
		if _, err := ParseEntryRef(spec.To.Address); err != nil {
			return nil, nil, fmt.Errorf("state move destination: %w", err)
		}
	}
	moves, err := normalizeEntryMoveSpecs(specs)
	if err != nil {
		return nil, nil, err
	}
	exact := make(map[string]normalizedEntryMove, len(moves))
	var prefixes []normalizedEntryMove
	for _, move := range moves {
		node, err := entryMoveTargetNode(dag, move.To)
		if err != nil {
			return nil, nil, err
		}
		entry := snapshot.Find(move.From.Address)
		move.Prefix = node.IsComposite() || entry != nil && entry.Kind == state.StateComposite
		exact[move.From.Address] = move
		if move.Prefix {
			prefixes = append(prefixes, move)
		}
	}
	return exact, prefixes, nil
}

func validateEntryMoveTargetV2(
	entry state.StateEntryV2,
	node *Node,
	dag *DAG,
	libraries map[string]*Library,
) error {
	var binding Binding
	var kind NodeKind
	switch entry.Kind {
	case state.StateResource:
		binding, kind = entry.Payload.Resource.Target.Binding, NodeResource
	case state.StateDataSource:
		binding, kind = entry.Payload.DataSource.Binding, NodeDataSource
	case state.StateAction:
		binding, kind = entry.Payload.Action.Binding, NodeAction
	case state.StateComposite:
		binding = entry.Payload.Composite.Binding
		kind = NodeKind(entry.Payload.Composite.Category)
	default:
		return fmt.Errorf("unsupported state entry kind %q", entry.Kind)
	}
	if node.Kind != kind || node.IsComposite() != (entry.Kind == state.StateComposite) {
		return fmt.Errorf("%s entry cannot move to %s at %s", entry.Kind, node.Kind, node.Address)
	}
	path := node.LibraryPath
	if path == "" {
		library := entryMoveLibrariesForNode(node, dag, libraries)[node.Alias]
		if library == nil {
			return fmt.Errorf("%s: library %q is not imported", node.Address, node.Alias)
		}
		path = library.LibraryPath
	}
	if binding != (Binding{LibraryPath: path, Export: node.Type}) {
		return fmt.Errorf("%s cannot move to %s: implementation differs", entry.Address, node.Address)
	}
	return nil
}

func (e *Executor) sourceEntryMoveSpecsV2(snapshot *state.SnapshotV2) ([]EntryMoveSpec, error) {
	if e == nil || e.DAG == nil {
		return nil, fmt.Errorf("executor dependency graph is required")
	}
	specs, err := e.rootEntryMoveSpecs()
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return specs, nil
	}
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	for _, entry := range snapshot.Entries {
		if entry.Kind != state.StateComposite {
			continue
		}
		exact, prefixes, err := prepareEntryMoveRulesV2(snapshot, e.DAG, specs)
		if err != nil {
			return nil, err
		}
		from := EntryRef{Address: entry.Address}
		to, changed := entryMoveTargetForRef(from, exact, prefixes)
		if !changed {
			to = from
		}
		node := e.DAG.Nodes[templateAddress(to.Address)]
		if node == nil || !node.IsComposite() {
			continue
		}
		if err := validateEntryMoveTargetV2(entry, node, e.DAG, e.Libraries); err != nil {
			continue
		}
		relative, err := syntaxEntryMoveSpecs(node.CompositeSyntaxBody.StateMoves)
		if err != nil {
			return nil, err
		}
		for _, spec := range relative {
			specs = append(specs, EntryMoveSpec{
				From: prefixedEntryRef(from.Address, spec.From),
				To:   prefixedEntryRef(to.Address, spec.To),
			})
		}
	}
	return specs, nil
}

func (e *Executor) planSourceEntryMovesV2(
	snapshot *state.SnapshotV2,
) (*state.SnapshotV2, []PlannedEntryMove, error) {
	specs, err := e.sourceEntryMoveSpecsV2(snapshot)
	if err != nil {
		return nil, nil, err
	}
	next, results, err := ApplyEntryMovesV2(
		snapshot, e.DAG, e.Libraries, specs, EntryMoveIdempotent,
	)
	if err != nil {
		return nil, nil, err
	}
	moves := make([]PlannedEntryMove, len(results))
	for i, result := range results {
		moves[i] = PlannedEntryMove{From: result.From.Address, To: result.To.Address}
	}
	return next, moves, nil
}
