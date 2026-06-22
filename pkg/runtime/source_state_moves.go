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
	known := append([]EntryMoveSpec(nil), root...)
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
		prefixes, err := e.compositeEntryMovePrefixes(prior, n, known)
		if err != nil {
			return nil, err
		}
		for _, prefix := range prefixes {
			for _, spec := range relative {
				expanded := EntryMoveSpec{
					From: prefixedEntryRef(prefix.from, spec.From),
					To:   prefixedEntryRef(prefix.to, spec.To),
				}
				specs = append(specs, expanded)
				known = append(known, expanded)
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
	specs []EntryMoveSpec,
) ([]compositeEntryMovePrefix, error) {
	var prefixes []compositeEntryMovePrefix
	seen := map[compositeEntryMovePrefix]bool{}
	byFrom := map[string]int{}
	add := func(prefix compositeEntryMovePrefix) {
		if idx, ok := byFrom[prefix.from]; ok {
			current := prefixes[idx]
			if current.to == prefix.to {
				return
			}
			if current.to == current.from && prefix.to != prefix.from {
				delete(seen, current)
				seen[prefix] = true
				prefixes[idx] = prefix
				return
			}
			if current.to != current.from && prefix.to == prefix.from {
				return
			}
		}
		if !seen[prefix] {
			seen[prefix] = true
			byFrom[prefix.from] = len(prefixes)
			prefixes = append(prefixes, prefix)
		}
	}
	selector := selectorForNode(n)
	for _, ent := range prior.Entries {
		if ent.Type != state.EntryLibraryCall {
			continue
		}
		if templateAddress(ent.Address) != n.Address || !sameSelector(ent.Selector, selector) {
			continue
		}
		add(compositeEntryMovePrefix{from: ent.Address, to: ent.Address})
	}
	moves, err := normalizeEntryMoveSpecs(specs)
	if err != nil {
		return nil, err
	}
	for _, spec := range moves {
		toTemplate := templateAddress(spec.To.Address)
		if toTemplate == n.Address {
			add(compositeEntryMovePrefix{from: spec.From.Address, to: spec.To.Address})
			continue
		}
		if !entryMoveHasAddressPrefix(n.Address, toTemplate) || n.Address == toTemplate {
			continue
		}
		toNode, err := entryMoveTargetNode(e.DAG, spec.To)
		if err != nil {
			return nil, err
		}
		if !toNode.IsComposite() {
			continue
		}
		suffix := n.Address[len(toTemplate):]
		fromAddress := spec.From.Address + suffix
		if !priorCompositeEntryHasSelector(prior, fromAddress, selector) {
			continue
		}
		add(compositeEntryMovePrefix{from: fromAddress, to: spec.To.Address + suffix})
	}
	sort.Slice(prefixes, func(i, j int) bool {
		if prefixes[i].from == prefixes[j].from {
			return prefixes[i].to < prefixes[j].to
		}
		return prefixes[i].from < prefixes[j].from
	})
	return prefixes, nil
}

func priorCompositeEntryHasSelector(
	prior *state.Snapshot,
	address string,
	selector *state.Selector,
) bool {
	for _, ent := range prior.Entries {
		if ent.Type != state.EntryLibraryCall {
			continue
		}
		if ent.Address == address && sameSelector(ent.Selector, selector) {
			return true
		}
	}
	return false
}

func prefixedEntryRef(prefix string, ref EntryRef) EntryRef {
	return EntryRef{Address: joinAddress(prefix, ref.Address)}
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
