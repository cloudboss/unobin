package runtime

import (
	"fmt"
	"sort"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func syntaxEntryMoveSpecs(decls []syntax.StateMoveDecl) ([]EntryMoveSpec, error) {
	specs := make([]EntryMoveSpec, 0, len(decls))
	for i, decl := range decls {
		if decl.From == nil || decl.To == nil {
			continue
		}
		from, err := ParseEntryRef(decl.From.Value)
		if err != nil {
			return nil, fmt.Errorf("state-moves[%d].from: %w", i, err)
		}
		to, err := ParseEntryRef(decl.To.Value)
		if err != nil {
			return nil, fmt.Errorf("state-moves[%d].to: %w", i, err)
		}
		specs = append(specs, EntryMoveSpec{From: from, To: to})
	}
	return specs, nil
}

func (e *Executor) rootEntryMoveSpecs() ([]EntryMoveSpec, error) {
	if e.SyntaxSource == nil {
		return nil, nil
	}
	return syntaxEntryMoveSpecs(e.SyntaxSource.StateMoves)
}

func (e *Executor) applySourceEntryMoves(rs *runState) ([]PlannedEntryMove, error) {
	specs, err := e.sourceEntryMoveSpecs(rs.prior)
	if err != nil {
		return nil, err
	}
	if len(specs) == 0 {
		return nil, nil
	}
	next, results, err := ApplyEntryMoves(
		rs.prior,
		e.DAG,
		e.Libraries,
		specs,
		EntryMoveIdempotent,
	)
	if err != nil {
		return nil, err
	}
	if next != nil {
		rs.prior = next
	}
	planned := make([]PlannedEntryMove, 0, len(results))
	for _, result := range results {
		planned = append(planned, PlannedEntryMove{
			From: result.From.String(),
			To:   result.To.String(),
		})
	}
	return planned, nil
}

func (e *Executor) sourceEntryMoveSpecs(prior *state.Snapshot) ([]EntryMoveSpec, error) {
	root, err := e.rootEntryMoveSpecs()
	if err != nil {
		return nil, err
	}
	composite, err := e.compositeEntryMoveSpecs(prior, root)
	if err != nil {
		return nil, err
	}
	return append(root, composite...), nil
}

func (e *Executor) compositeEntryMoveSpecs(
	prior *state.Snapshot,
	root []EntryMoveSpec,
) ([]EntryMoveSpec, error) {
	if prior == nil || e.DAG == nil {
		return nil, nil
	}
	var specs []EntryMoveSpec
	nodes := make([]*Node, 0, len(e.DAG.Nodes))
	for _, n := range e.DAG.Nodes {
		if n.IsComposite() && len(n.CompositeSyntaxBody.StateMoves) > 0 {
			nodes = append(nodes, n)
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Address < nodes[j].Address })
	for _, n := range nodes {
		relative, err := syntaxEntryMoveSpecs(n.CompositeSyntaxBody.StateMoves)
		if err != nil {
			return nil, err
		}
		for _, prefix := range e.compositeEntryMovePrefixes(prior, n, root) {
			for _, spec := range relative {
				specs = append(specs, EntryMoveSpec{
					From: prefixedEntryRef(prefix.from, spec.From),
					To:   prefixedEntryRef(prefix.to, spec.To),
				})
			}
		}
	}
	return specs, nil
}

type compositeEntryMovePrefix struct {
	from string
	to   string
}

func (e *Executor) compositeEntryMovePrefixes(
	prior *state.Snapshot,
	n *Node,
	root []EntryMoveSpec,
) []compositeEntryMovePrefix {
	var prefixes []compositeEntryMovePrefix
	seen := map[compositeEntryMovePrefix]bool{}
	add := func(prefix compositeEntryMovePrefix) {
		if !seen[prefix] {
			seen[prefix] = true
			prefixes = append(prefixes, prefix)
		}
	}
	for _, ent := range prior.Entries {
		if ent.Type != state.EntryLibraryCall {
			continue
		}
		if templateAddress(ent.Address) != n.Address || !sameSelector(ent.Selector, selectorForNode(n)) {
			continue
		}
		add(compositeEntryMovePrefix{from: ent.Address, to: ent.Address})
	}
	for _, spec := range normalizedRootEntryMoves(root) {
		if templateAddress(spec.To.Address) != n.Address {
			continue
		}
		if spec.To.Selector != *selectorForNode(n) {
			continue
		}
		add(compositeEntryMovePrefix{from: spec.From.Address, to: spec.To.Address})
	}
	sort.Slice(prefixes, func(i, j int) bool {
		if prefixes[i].from == prefixes[j].from {
			return prefixes[i].to < prefixes[j].to
		}
		return prefixes[i].from < prefixes[j].from
	})
	return prefixes
}

func normalizedRootEntryMoves(specs []EntryMoveSpec) []normalizedEntryMove {
	moves, err := normalizeEntryMoveSpecs(specs)
	if err != nil {
		return nil
	}
	return moves
}

func prefixedEntryRef(prefix string, ref EntryRef) EntryRef {
	return EntryRef{Selector: ref.Selector, Address: joinAddress(prefix, ref.Address)}
}

func (e *Executor) applyPlannedEntryMoves(rs *runState, moves []PlannedEntryMove) error {
	if len(moves) == 0 {
		return nil
	}
	specs := make([]EntryMoveSpec, 0, len(moves))
	for i, move := range moves {
		from, err := ParseEntryRef(move.From)
		if err != nil {
			return fmt.Errorf("state-moves[%d].from: %w", i, err)
		}
		to, err := ParseEntryRef(move.To)
		if err != nil {
			return fmt.Errorf("state-moves[%d].to: %w", i, err)
		}
		specs = append(specs, EntryMoveSpec{From: from, To: to})
	}
	next, _, err := ApplyEntryMoves(
		rs.next,
		e.DAG,
		e.Libraries,
		specs,
		EntryMoveStrict,
	)
	if err != nil {
		return err
	}
	if next != nil {
		rs.next = next
		rs.prior = cloneSnapshot(next)
	}
	return nil
}
